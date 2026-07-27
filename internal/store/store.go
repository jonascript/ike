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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gofrs/flock"

	"github.com/jonascript/ike/internal/task"
)

const (
	// dataFileMode keeps the matrix private. Task titles and the archive are
	// personal, and the file used to land as 0644 — readable by every other
	// user on the machine. Because writes go through a temp file and a rename,
	// which replaces the inode, an existing 0644 file becomes 0600 on the next
	// write without needing a migration.
	dataFileMode = 0o600
	// dataDirMode matches, so a directory listing cannot leak task counts or
	// the existence of a second matrix either.
	dataDirMode = 0o700
)

// lockTimeout bounds how long a mutation waits for another ike process. An
// unbounded wait meant one hung holder made every later `ike` invocation block
// forever, printing nothing at all. A variable so tests can shorten it.
var lockTimeout = 5 * time.Second

// lockRetryInterval is how often to retry while waiting for the lock.
const lockRetryInterval = 25 * time.Millisecond

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
//
// The result is sanitized for terminal output, because Of is the single choke
// point every frontend renders through: a label from a hand-edited or older
// data file cannot repaint the screen.
func (l Labels) Of(q task.Quadrant) string {
	if custom, ok := l[q]; ok && custom != "" {
		return task.SanitizeDisplay(custom)
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

// ErrMCPDisabled is returned to an MCP client whose access has been revoked.
// It is a distinct sentinel so the transport can report a permission problem
// rather than a data problem.
var ErrMCPDisabled = errors.New("MCP access is off for this matrix; run `ike mcp enable` to allow it")

// Store reads and writes the tasks file at a fixed path.
type Store struct {
	path     string
	lockPath string

	// requireMCP marks this Store as serving an MCP client. Every read and
	// every mutation then re-checks Data.MCPEnabled, so `ike mcp disable`
	// revokes a session that is already connected. Checking only at startup
	// meant a long-lived client kept full access for as long as it stayed
	// connected — hours or days — while `ike mcp status` reported "off".
	// The gate lives here rather than in the CLI because process lifetime
	// bypasses anything checked once before serving.
	requireMCP bool
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

// ForMCP returns a view of s that re-checks the access gate on every read and
// mutation, and keeps the data file's location out of the errors it returns.
// The caller keeps the ungated Store, so `ike mcp status` can still report the
// setting after access is revoked.
func (s *Store) ForMCP() *Store {
	gated := *s
	gated.requireMCP = true
	return &gated
}

// gate reports whether this Store may still touch d.
func (s *Store) gate(d Data) error {
	if s.requireMCP && !d.MCPEnabled {
		return ErrMCPDisabled
	}
	return nil
}

// redact removes the data file's location from an error bound for an MCP
// client, which would otherwise disclose the OS username and home layout. The
// original error stays reachable through errors.Is and errors.As.
func (s *Store) redact(err error) error {
	if err == nil || !s.requireMCP {
		return err
	}
	msg := err.Error()
	for _, p := range []string{s.lockPath, s.path} {
		msg = strings.ReplaceAll(msg, p, filepath.Base(p))
	}
	if dir := filepath.Dir(s.path); dir != "." {
		msg = strings.ReplaceAll(msg, dir, "…")
	}
	if msg == err.Error() {
		return err
	}
	return &redactedError{msg: msg, err: err}
}

type redactedError struct {
	msg string
	err error
}

func (e *redactedError) Error() string { return e.msg }
func (e *redactedError) Unwrap() error { return e.err }

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
	d, err := readFile(s.path)
	if err != nil {
		return Data{}, s.redact(err)
	}
	if err := s.gate(d); err != nil {
		return Data{}, err
	}
	return d, nil
}

// Mutate applies fn to a freshly-read copy of the data under an exclusive
// lock, then atomically writes the result. It returns the post-mutation data.
func (s *Store) Mutate(fn func(*Data) error) (data Data, err error) {
	if err := os.MkdirAll(filepath.Dir(s.path), dataDirMode); err != nil {
		return Data{}, s.redact(err)
	}

	lock := flock.New(s.lockPath)
	ctx, cancel := context.WithTimeout(context.Background(), lockTimeout)
	defer cancel()
	locked, lerr := lock.TryLockContext(ctx, lockRetryInterval)
	if !locked {
		// A timeout arrives as context.DeadlineExceeded rather than a false
		// return, and "context deadline exceeded" tells the reader nothing
		// about what to do.
		if lerr == nil || errors.Is(lerr, context.DeadlineExceeded) {
			return Data{}, s.redact(fmt.Errorf(
				"timed out after %s waiting for another ike process to release %s",
				lockTimeout, s.lockPath))
		}
		return Data{}, s.redact(fmt.Errorf("locking %s: %w", s.lockPath, lerr))
	}
	defer func() {
		// Surface an unlock failure only if the mutation itself succeeded, so
		// it never masks the more useful error.
		if uerr := lock.Unlock(); uerr != nil && err == nil {
			err = s.redact(fmt.Errorf("releasing %s: %w", s.lockPath, uerr))
		}
	}()

	data, err = readFile(s.path)
	if err != nil {
		return Data{}, s.redact(err)
	}
	// Re-checked inside the lock against the state just read, so a revocation
	// that landed after this process started is honored.
	if err = s.gate(data); err != nil {
		return Data{}, err
	}
	if err = fn(&data); err != nil {
		return Data{}, err
	}
	if err = writeFileAtomic(s.path, data); err != nil {
		return Data{}, s.redact(err)
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

	if err := writeBackup(path); err != nil {
		return err
	}

	// A random name created with O_EXCL, rather than path+".tmp". IKE_DATA_FILE
	// can point anywhere, including a shared directory, and opening a
	// predictable name follows symlinks — so a pre-created path+".tmp" symlink
	// would redirect this write and truncate whatever it pointed at.
	// os.CreateTemp opens with 0600, so the replacement file is private.
	f, err := os.CreateTemp(filepath.Dir(path), ".tasks-*.json")
	if err != nil {
		return err
	}
	tmp := f.Name()
	committed := false
	defer func() {
		_ = f.Close() // no-op after the successful Close below
		if !committed {
			_ = os.Remove(tmp)
		}
	}()

	if _, err := f.Write(b); err != nil {
		return err
	}
	// fsync before the rename. Rename is atomic with respect to concurrent
	// readers, but without this the rename metadata can reach disk before the
	// data blocks, so a crash or power loss can leave a zero-length or
	// truncated tasks.json — with the whole matrix in it.
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	committed = true

	// fsync the directory so the rename itself survives a crash. Best effort:
	// some filesystems reject fsync on a directory, and a durability
	// refinement must never be the reason a task fails to save.
	if dir, err := os.Open(filepath.Dir(path)); err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	return nil
}

// writeBackup keeps the previous contents alongside the data file before they
// are replaced. Reads already refuse to overwrite a file they cannot parse, so
// data is never silently clobbered — but there was no way back either, leaving
// hand-editing JSON as the only recovery. This turns total loss into losing
// one mutation.
func writeBackup(path string) error {
	prev, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := os.WriteFile(path+".bak", prev, dataFileMode); err != nil {
		return fmt.Errorf("writing backup: %w", err)
	}
	// WriteFile only applies the mode when creating, so tighten a .bak left
	// behind by an older build.
	_ = os.Chmod(path+".bak", dataFileMode)
	return nil
}
