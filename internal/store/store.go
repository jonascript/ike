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
// per-task ranks and the undo stack; version 3 stopped copying the whole
// archive into every snapshot; version 4 wrapped the matrix in a document that
// can hold several of them; version 5 split the document across files — a
// manifest at the data path and one file per space beside it — so one corrupt
// space cannot take the others with it. Older files are upgraded in memory on
// read and persisted at the current version by the next write.
//
// 3, 4, and 5 are real bumps rather than fields added in place — the approach
// taken for `redo` — because the failure modes differ. Losing redo history to
// an older binary is harmless; an older binary reading a v3 file would find no
// "archive" in a snapshot, decode it as empty, and wipe the archive on the next
// undo, and one reading a v4 file would find no top-level "tasks" at all and
// see an empty matrix it was about to overwrite. A v5 manifest deliberately has
// no "spaces" key for the same reason: a v4 binary must refuse it outright
// rather than decode an empty matrix it was about to make permanent.
const currentVersion = 5

// defaultSpace names the space a single-matrix file is upgraded into, and the
// one a fresh file starts with.
const defaultSpace = "default"

// File is the on-disk document: one or more independent matrices, and which of
// them is current. Everything a mutation can touch lives inside a space, so
// spaces are fully independent — tasks, archive, quadrant labels, the ID
// counter, and both history stacks.
//
// MCPEnabled is the exception, and is deliberately document-wide. It is a
// consent decision about a file ("I have decided to let my agent manage this"),
// not a property of one matrix, and a per-space gate would mean an agent
// holding access to one space could still see the names of the others.
//
// AgentEnabled is the second consent flag, and is deliberately separate from
// MCPEnabled rather than folded into one "allow agents" setting. They are
// different decisions with different blast radii: MCPEnabled means an agent
// already running may read and edit this task list, while AgentEnabled means
// ike itself may start a process that edits files in a directory. Agreeing to
// the first is not agreeing to the second, and someone who wants their agent to
// tidy their matrix should not thereby have granted `ike delegate`.
type File struct {
	Version      int              `json:"version"`
	Current      string           `json:"current"`
	Spaces       map[string]*Data `json:"spaces"`
	MCPEnabled   bool             `json:"mcp_enabled,omitempty"`
	AgentEnabled bool             `json:"agent_enabled,omitempty"`

	// The fields below are read-state: what readTree saw on disk, carried so
	// the write side can tell what actually changed. None of them are part of
	// the document, and File itself is never marshaled wholesale at version 5
	// — the json tags above remain for reading version-4 envelopes.

	// onDiskVersion is the version the document file held when read, or 0 if
	// there was none. The write side migrates when it is 1 through 4.
	onDiskVersion int
	// rawDoc holds the document file's bytes as read — the monolith to back
	// up before a migration, or the manifest to compare against for a
	// dirty check.
	rawDoc []byte
	// rawSpace holds each space file's bytes as read, keyed by space name.
	// A space whose current marshaling matches is not rewritten, which is
	// what confines a write to the spaces it touched.
	rawSpace map[string][]byte
	// fileFor records which filename each space was actually read from,
	// which is not always the encoding of its name: the embedded name is
	// canonical, and a hand-copied or normalization-mangled filename is
	// repaired on the next write rather than trusted.
	fileFor map[string]string
	// corrupt lists spaces whose files could not be parsed, by display name.
	// They are absent from Spaces and from rawSpace, so no write can touch
	// their files; reads surface them instead of failing the whole document.
	corrupt map[string]corruptSpace
	// removeCorrupt names files in the spaces directory that RemoveSpace,
	// with force, has condemned. The only way an unreadable space's file is
	// ever touched, and even then it is renamed to .bak rather than deleted.
	removeCorrupt []string
	// standalone marks a Store opened directly on a single space file — an
	// export handed to --file. The document then has exactly that space,
	// writes go back to the same file, and space lifecycle operations refuse.
	standalone bool
}

// Data is one space: a complete matrix, and the unit every operation acts on.
//
// The three trailing fields are derived from the enclosing File on every read
// and every mutation, and are never persisted. They exist because frontends
// must render from the Data an operation returns rather than reading the file
// again — a follow-up read observes a *later* file state — and rendering the
// space header, the space picker, or the MCP marker needs facts that live on
// the document rather than in the matrix.
type Data struct {
	NextID  int         `json:"next_id"`
	Tasks   []task.Task `json:"tasks"`
	Archive []task.Task `json:"archive"`
	Labels  Labels      `json:"quadrant_labels,omitempty"`
	Undo    []Snapshot  `json:"undo,omitempty"`
	Redo    []Snapshot  `json:"redo,omitempty"`

	// Space is the name of the space this Data was read from.
	Space string `json:"-"`
	// AllSpaces describes every space in the file, sorted by name. It carries
	// counts only: cloning per-task state N times is exactly what
	// Snapshot.ArchiveEntry exists to undo.
	AllSpaces []SpaceInfo `json:"-"`
	// MCPAllowed reports whether agent access is on for the whole file.
	//
	// It is named differently from File.MCPEnabled on purpose. While the flag
	// lived here, `d.MCPEnabled = on` inside a Mutate callback was how it was
	// set; with the flag on the document that assignment would still compile,
	// still report success, and persist nothing. The rename makes it a
	// compile error instead of a silent no-op.
	MCPAllowed bool `json:"-"`
	// AgentAllowed reports whether delegation is on for the whole file. It is
	// named apart from File.AgentEnabled for the same reason MCPAllowed is.
	//
	// Display only: a frontend renders the ambient marker from it, but the
	// decision to actually start a run reads the flag fresh, so `ike agent
	// disable` in another terminal takes effect without waiting for a poll.
	AgentAllowed bool `json:"-"`
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
	Label  string      `json:"label"`
	Tasks  []task.Task `json:"tasks"`
	Labels Labels      `json:"quadrant_labels,omitempty"`

	// ArchiveEntry is the one archived task that cannot be reconstructed from
	// Tasks, and is nil for almost every snapshot.
	//
	// The archive is not stored here, because it does not need to be. A task is
	// never active and archived at the same time, so restoring Tasks already says
	// which archive entries must go: any whose ID is active again. That alone
	// reverses a complete. Only Restore takes an entry *out* of the archive, and
	// only its DoneAt is then unrecoverable — so only that single entry is kept.
	//
	// Copying the whole archive into every snapshot is what this replaces. With
	// 1500 archived tasks and two 20-deep stacks it meant a ~21x blow-up,
	// measured: 276K of real data became 8MB, re-marshaled on every mutation
	// (79ms per `ike add`) and re-parsed by the TUI on every poll.
	ArchiveEntry *task.Task `json:"archive_entry,omitempty"`
}

// emptyFile is a fresh document: one empty space, which is current.
func emptyFile() File {
	return File{
		Version: currentVersion,
		Current: defaultSpace,
		Spaces:  map[string]*Data{defaultSpace: {NextID: 1}},
	}
}

// ErrMCPDisabled is returned to an MCP client whose access has been revoked.
// It is a distinct sentinel so the transport can report a permission problem
// rather than a data problem.
var ErrMCPDisabled = errors.New("MCP access is off for this matrix; run `ike mcp enable` to allow it")

// Store reads and writes the tasks file at a fixed path.
type Store struct {
	path     string
	lockPath string

	// space pins this Store to one space. Empty means "whichever the file
	// records as current", resolved at read time — so a plain `ike list`
	// follows `ike space use`, while a Store pinned with InSpace keeps acting
	// on the same matrix no matter what another frontend switches to.
	space string

	// requireMCP marks this Store as serving an MCP client. Every read and
	// every mutation then re-checks File.MCPEnabled, so `ike mcp disable`
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

// OpenPath returns a Store at a user-supplied path, validated the same way an
// IKE_DATA_FILE override is. It backs `ike --file`.
func OpenPath(source, path string) (*Store, error) {
	p, err := CheckPath(source, path)
	if err != nil {
		return nil, err
	}
	return OpenAt(p), nil
}

// Path returns the data file path.
func (s *Store) Path() string { return s.path }

// Pinned returns the space this Store is pinned to, or "" if it follows the
// file's current space. It lets a caller tell the two apart — the MCP server
// refuses a request naming a different space than the one it was launched for.
func (s *Store) Pinned() string { return s.space }

// ForMCP returns a view of s that re-checks the access gate on every read and
// mutation, and keeps the data file's location out of the errors it returns.
// The caller keeps the ungated Store, so `ike mcp status` can still report the
// setting after access is revoked.
func (s *Store) ForMCP() *Store {
	gated := *s
	gated.requireMCP = true
	return &gated
}

// InSpace returns a view of s pinned to one space, so it keeps acting on that
// matrix whatever another frontend makes current. An empty name follows the
// file's current space, which is what an unpinned Store already does — so
// callers can pass a flag value straight through without branching.
//
// It never creates: naming a space that is not there fails on the next read or
// mutation, before any write.
func (s *Store) InSpace(name string) *Store {
	pinned := *s
	pinned.space = name
	return &pinned
}

// gate reports whether this Store may still touch f.
//
// It is checked before the space is resolved, so a client whose access was
// revoked is told exactly that rather than whether the space it asked for
// exists — the error would otherwise enumerate the file's spaces for it.
func (s *Store) gate(f *File) error {
	if s.requireMCP && !f.MCPEnabled {
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

// ModTime returns the newest mtime across the data file and the spaces
// directory, or zero if neither exists. It is the TUI's only change signal,
// polled every couple of seconds, so it must stay cheap — two stats — while
// still moving on every kind of write: a task mutation lands a rename inside
// the spaces directory, which bumps the directory's mtime; a space created,
// removed, or renamed changes the directory listing, likewise; and a `space
// use` or consent change rewrites the manifest itself. The one thing it no
// longer sees is an in-place hand edit of a space file, which was never the
// contract.
func (s *Store) ModTime() (mtime int64, err error) {
	for _, p := range []string{s.path, spacesDir(s.path)} {
		fi, serr := os.Stat(p)
		if errors.Is(serr, os.ErrNotExist) {
			continue
		}
		if serr != nil {
			return 0, serr
		}
		mtime = max(mtime, fi.ModTime().UnixNano())
	}
	return mtime, nil
}

// Load reads this Store's space without taking the write lock.
func (s *Store) Load() (Data, error) {
	f, name, d, err := s.loadResolved(s.space)
	if err != nil {
		return Data{}, err
	}
	return f.dataFor(name, d), nil
}

// loadFile reads the document and checks the gate — everything a read does
// before it needs to know which space it is about.
//
// Operations that describe the file rather than a matrix stop here. Listing the
// spaces must keep working when the pinned one does not exist, since a listing
// is how you find that out.
func (s *Store) loadFile() (File, error) {
	f, err := readTree(s.path)
	if err != nil {
		return File{}, s.redact(err)
	}
	if err := s.gate(&f); err != nil {
		return File{}, err
	}
	return f, nil
}

// loadResolved is loadFile plus one resolved space, returned alongside the
// document so a caller can use both.
//
// It exists so this ordering has one implementation. The gate runs before
// resolution deliberately — a revoked client must be told access is off rather
// than whether the space it named exists — and that ordering was previously
// retyped in each read path, where a second copy could drift with nothing to
// catch it.
func (s *Store) loadResolved(space string) (File, string, *Data, error) {
	f, err := s.loadFile()
	if err != nil {
		return File{}, "", nil, err
	}
	name, d, err := f.resolve(space)
	if err != nil {
		return File{}, "", nil, s.redact(err)
	}
	return f, name, d, nil
}

// Mutate applies fn to a freshly-read copy of this Store's space under an
// exclusive lock, then atomically writes the whole document. It returns the
// post-mutation data for that space.
func (s *Store) Mutate(fn func(*Data) error) (Data, error) {
	return s.mutateSpace(func(_ string, d *Data) error { return fn(d) })
}

// mutateSpace is Mutate with the resolved space name handed to the callback.
//
// It exists for the plan operations, which write a file named after the space
// (plans.go) and so cannot use Mutate: Data.Space is derived by dataFor *after*
// fn returns, so inside a Mutate callback it is still empty. Reading the name
// separately would mean resolving twice, and the second resolution would be
// outside this lock — a `ike space use` landing in between would file the plan
// under the wrong space.
//
// Everything else about the two is identical, so Mutate delegates here rather
// than the pair growing two copies of the resolve-then-render sequence.
func (s *Store) mutateSpace(fn func(name string, d *Data) error) (Data, error) {
	var out Data
	_, err := s.mutateFile(func(f *File) error {
		// Resolved inside the lock, and before fn, so a mutation naming a space
		// that is not there fails without writing anything.
		name, d, err := f.resolve(s.space)
		if err != nil {
			return err
		}
		if err := fn(name, d); err != nil {
			return err
		}
		out = f.dataFor(name, d)
		return nil
	})
	if err != nil {
		return Data{}, err
	}
	return out, nil
}

// mutateFile applies fn to a freshly-read copy of the whole document under an
// exclusive lock, then atomically writes it. It is the only write path: Mutate
// and the space operations both go through it, so the lock discipline and the
// five durability properties below have exactly one implementation.
func (s *Store) mutateFile(fn func(*File) error) (file File, err error) {
	if err := os.MkdirAll(filepath.Dir(s.path), dataDirMode); err != nil {
		return File{}, s.redact(err)
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
			return File{}, s.redact(fmt.Errorf(
				"timed out after %s waiting for another ike process to release %s",
				lockTimeout, s.lockPath))
		}
		return File{}, s.redact(fmt.Errorf("locking %s: %w", s.lockPath, lerr))
	}
	defer func() {
		// Surface an unlock failure only if the mutation itself succeeded, so
		// it never masks the more useful error.
		if uerr := lock.Unlock(); uerr != nil && err == nil {
			err = s.redact(fmt.Errorf("releasing %s: %w", s.lockPath, uerr))
		}
	}()

	file, err = readTree(s.path)
	if err != nil {
		return File{}, s.redact(err)
	}
	// Re-checked inside the lock against the state just read, so a revocation
	// that landed after this process started is honored.
	if err = s.gate(&file); err != nil {
		return File{}, err
	}
	if err = fn(&file); err != nil {
		return File{}, err
	}
	if err = writeTree(s.path, &file); err != nil {
		return File{}, s.redact(err)
	}
	return file, nil
}

// writeBytesAtomic replaces path with b, atomically and durably. It is the body
// writeFileAtomic used to hold inline, lifted out so that the plan sidecars
// (plans.go) get the same four guarantees rather than a second, untested copy
// of them: a private temp file, fsync before the rename, an atomic rename, and
// a best-effort directory fsync after.
//
// This is deliberately *not* a second write path in the sense CLAUDE.md
// forbids. mutateFile still owns the lock, the re-read, and the gate; this owns
// only the bytes-to-disk step, and mutateFile reaches it through
// writeFileAtomic exactly as before. durability_test.go pins the behavior and
// passes unchanged, which is the check that the lift was faithful.
//
// pattern is the os.CreateTemp pattern, so the leftovers of an interrupted
// write are recognizable as belonging to the file they were replacing.
func writeBytesAtomic(path, pattern string, b []byte) error {
	// A random name created with O_EXCL, rather than path+".tmp". IKE_DATA_FILE
	// can point anywhere, including a shared directory, and opening a
	// predictable name follows symlinks — so a pre-created path+".tmp" symlink
	// would redirect this write and truncate whatever it pointed at.
	// os.CreateTemp opens with 0600, so the replacement file is private.
	f, err := os.CreateTemp(filepath.Dir(path), pattern)
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
