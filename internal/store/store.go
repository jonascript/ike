// Package store persists ike's tasks to a single JSON file and provides
// the shared business operations used by the TUI, CLI, and MCP frontends.
//
// Concurrency model: every mutation takes an exclusive advisory lock on a
// sidecar lock file, re-reads the data file fresh inside the lock, applies
// the change, and atomically replaces the file via rename. Concurrent
// writers (e.g. an open TUI and the MCP server) therefore never lose
// updates. The lock is on a sidecar because renaming the data file itself
// would replace the locked inode.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gofrs/flock"

	"github.com/jonascript/ike/internal/task"
)

// currentVersion is the schema version this build writes. Version 2 added
// per-task ranks and the undo stack; version 1 files are upgraded in memory
// on read and persisted at version 2 by the next write.
const currentVersion = 2

// Data is the full on-disk state.
type Data struct {
	Version int         `json:"version"`
	NextID  int         `json:"next_id"`
	Tasks   []task.Task `json:"tasks"`
	Archive []task.Task `json:"archive"`
	Labels  Labels      `json:"quadrant_labels,omitempty"`
	Undo    []Snapshot  `json:"undo,omitempty"`
	Redo    []Snapshot  `json:"redo,omitempty"`

	// MCPEnabled gates the MCP server's access to this matrix. The zero value
	// is false, so a fresh install — and any data file written before this
	// field existed — starts with agent access switched off until the owner
	// opts in. It is intentionally absent from Snapshot: undo moves tasks
	// around, and must never quietly change who can reach them.
	MCPEnabled bool `json:"mcp_enabled,omitempty"`
}

// Labels holds user-chosen names for quadrants. It is sparse: only quadrants
// renamed away from their default appear, so the defaults can change in a
// later release without rewriting anyone's data file.
type Labels map[task.Quadrant]string

// Of returns the display name for q — the custom label if one is set, else the
// built-in default. It is safe to call on a nil Labels, which is the common
// case, so frontends can always render with d.Labels.Of(q).
func (l Labels) Of(q task.Quadrant) string {
	if custom, ok := l[q]; ok && custom != "" {
		return custom
	}
	return q.Label()
}

// IsCustom reports whether q has been renamed away from its default.
func (l Labels) IsCustom(q task.Quadrant) bool {
	custom, ok := l[q]
	return ok && custom != "" && custom != q.Label()
}

// Snapshot is a copy of the mutable state at one point in time, with a label
// naming the change it sits either side of — e.g. `complete "ship v2"`. On the
// undo stack it holds the state before that change; on the redo stack, after.
type Snapshot struct {
	Label   string      `json:"label"`
	Tasks   []task.Task `json:"tasks"`
	Archive []task.Task `json:"archive"`
	Labels  Labels      `json:"quadrant_labels,omitempty"`
}

func emptyData() Data {
	return Data{Version: currentVersion, NextID: 1}
}

// Store reads and writes the tasks file at a fixed path.
type Store struct {
	path     string
	lockPath string
}

// Open returns a Store at the default (XDG or IKE_DATA_FILE) location.
func Open() (*Store, error) {
	p, err := DataFile()
	if err != nil {
		return nil, err
	}
	return OpenAt(p), nil
}

// OpenAt returns a Store for an explicit file path (used by tests).
func OpenAt(path string) *Store {
	return &Store{path: path, lockPath: path + ".lock"}
}

// Path returns the data file path.
func (s *Store) Path() string { return s.path }

// ModTime returns the data file's mtime, or the zero time if it does not exist.
func (s *Store) ModTime() (mtime int64, err error) {
	fi, err := os.Stat(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return fi.ModTime().UnixNano(), nil
}

// Load reads the current data without taking the write lock.
func (s *Store) Load() (Data, error) {
	return readFile(s.path)
}

// Mutate applies fn to a freshly-read copy of the data under an exclusive
// lock, then atomically writes the result. It returns the post-mutation data.
func (s *Store) Mutate(fn func(*Data) error) (Data, error) {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return Data{}, err
	}
	lock := flock.New(s.lockPath)
	if err := lock.Lock(); err != nil {
		return Data{}, fmt.Errorf("locking %s: %w", s.lockPath, err)
	}
	defer lock.Unlock()

	data, err := readFile(s.path)
	if err != nil {
		return Data{}, err
	}
	if err := fn(&data); err != nil {
		return Data{}, err
	}
	if err := writeFileAtomic(s.path, data); err != nil {
		return Data{}, err
	}
	return data, nil
}

func readFile(path string) (Data, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return emptyData(), nil
	}
	if err != nil {
		return Data{}, err
	}
	var d Data
	if err := json.Unmarshal(b, &d); err != nil {
		return Data{}, fmt.Errorf("parsing %s: %w", path, err)
	}
	if d.Version < 1 || d.Version > currentVersion {
		return Data{}, fmt.Errorf("%s has unsupported version %d (expected %d)", path, d.Version, currentVersion)
	}
	// Older files are upgraded in memory; the next write persists the upgrade.
	d.Version = currentVersion
	if d.NextID < 1 {
		d.NextID = 1
	}
	normalizeRanks(&d)
	return d, nil
}

func writeFileAtomic(path string, d Data) error {
	b, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
