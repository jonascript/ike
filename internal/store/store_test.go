package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jonascript/ike/internal/task"
)

// withLockTimeout temporarily changes how long a mutation waits for the lock,
// returning the function that restores it. Tests about contention need a bound
// generous enough that a loaded machine cannot make them fail for the wrong
// reason; the one test about the timeout itself shrinks it instead.
func withLockTimeout(d time.Duration) func() {
	old := lockTimeout
	lockTimeout = d
	return func() { lockTimeout = old }
}

func testStore(t *testing.T) *Store {
	t.Helper()
	return OpenAt(filepath.Join(t.TempDir(), "tasks.json"))
}

func TestAddListRoundTrip(t *testing.T) {
	s := testStore(t)

	a, _, err := s.Add("fix prod bug", task.Do)
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != 1 {
		t.Errorf("first ID = %d, want 1", a.ID)
	}
	b, _, err := s.Add("plan roadmap", task.Schedule)
	if err != nil {
		t.Fatal(err)
	}
	if b.ID != 2 {
		t.Errorf("second ID = %d, want 2", b.ID)
	}

	all, err := s.List(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("List(0) len = %d, want 2", len(all))
	}
	q1, err := s.List(task.Do)
	if err != nil {
		t.Fatal(err)
	}
	if len(q1) != 1 || q1[0].Title != "fix prod bug" {
		t.Errorf("List(Do) = %+v, want the one Do task", q1)
	}
}

func TestAddValidates(t *testing.T) {
	s := testStore(t)
	if _, _, err := s.Add("  ", task.Do); err == nil {
		t.Error("Add with blank title should fail")
	}
	if _, _, err := s.Add("x", 9); err == nil {
		t.Error("Add with bad quadrant should fail")
	}
}

func TestCompleteMovesToArchiveAndKeepsID(t *testing.T) {
	s := testStore(t)
	a, _, _ := s.Add("one", task.Do)
	s.Add("two", task.Do)

	done, _, err := s.Complete(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if done.DoneAt == nil {
		t.Error("Complete should stamp DoneAt")
	}

	active, _ := s.List(0)
	if len(active) != 1 {
		t.Errorf("active len = %d, want 1", len(active))
	}
	arch, _ := s.ListArchive()
	if len(arch) != 1 || arch[0].ID != a.ID {
		t.Errorf("archive = %+v, want task %d", arch, a.ID)
	}

	// IDs are never reused, even after completion.
	c, _, _ := s.Add("three", task.Do)
	if c.ID != 3 {
		t.Errorf("ID after completion = %d, want 3", c.ID)
	}

	if _, _, err := s.Complete(a.ID); err == nil {
		t.Error("completing an archived task should fail")
	}
}

func TestMoveRenameDelete(t *testing.T) {
	s := testStore(t)
	a, _, _ := s.Add("one", task.Do)

	m, _, err := s.Move(a.ID, task.Eliminate)
	if err != nil || m.Quadrant != task.Eliminate {
		t.Errorf("Move = %+v, %v", m, err)
	}
	if _, _, err := s.Move(a.ID, 7); err == nil {
		t.Error("Move to invalid quadrant should fail")
	}

	r, _, err := s.Rename(a.ID, "renamed")
	if err != nil || r.Title != "renamed" {
		t.Errorf("Rename = %+v, %v", r, err)
	}
	if _, _, err := s.Rename(a.ID, ""); err == nil {
		t.Error("Rename to empty should fail")
	}

	if _, _, err := s.Delete(a.ID); err != nil {
		t.Fatal(err)
	}
	active, _ := s.List(0)
	arch, _ := s.ListArchive()
	if len(active) != 0 || len(arch) != 0 {
		t.Error("Delete should remove permanently, not archive")
	}
	if _, _, err := s.Delete(a.ID); err == nil {
		t.Error("deleting a missing task should fail")
	}
}

func TestMissingFileIsEmpty(t *testing.T) {
	s := testStore(t)
	d, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if d.NextID != 1 || len(d.Tasks) != 0 || len(d.Archive) != 0 {
		t.Errorf("empty load = %+v", d)
	}
}

func TestCorruptAndWrongVersion(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "tasks.json")

	os.WriteFile(p, []byte("{not json"), 0o644)
	if _, err := OpenAt(p).Load(); err == nil {
		t.Error("corrupt file should error")
	}

	os.WriteFile(p, []byte(`{"version": 99, "next_id": 1}`), 0o644)
	if _, err := OpenAt(p).Load(); err == nil {
		t.Error("unknown version should error")
	}

	// A mutation must not clobber a corrupt file either.
	os.WriteFile(p, []byte("{not json"), 0o644)
	if _, _, err := OpenAt(p).Add("x", task.Do); err == nil {
		t.Error("Add over corrupt file should error")
	}
}

func TestConcurrentWriters(t *testing.T) {
	// What is being tested is that no update is lost, not that 80 queued writes
	// finish inside the default 5s. On a slow two-core CI runner they do not, and
	// the test failed with "lost updates" when what actually happened was that
	// half the writers gave up waiting — a real but entirely different thing.
	// Raising the bound here keeps the test about the lock rather than the clock.
	defer withLockTimeout(30 * time.Second)()

	dir := t.TempDir()
	p := filepath.Join(dir, "tasks.json")

	const writers = 8
	const perWriter = 10

	var wg sync.WaitGroup
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s := OpenAt(p) // separate Store instances, like separate processes
			for i := 0; i < perWriter; i++ {
				if _, _, err := s.Add("t", task.Do); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	wg.Wait()

	d, err := OpenAt(p).Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Tasks) != writers*perWriter {
		t.Errorf("task count = %d, want %d (lost updates)", len(d.Tasks), writers*perWriter)
	}
	seen := map[int]bool{}
	for _, tk := range d.Tasks {
		if seen[tk.ID] {
			t.Errorf("duplicate ID %d", tk.ID)
		}
		seen[tk.ID] = true
	}
}

// ModTime is the TUI's only change signal, so every kind of write must move
// it: a task mutation now lands in a space file, not the polled tasks.json,
// and a signal that missed those would leave an open TUI stale forever.
func TestModTimeMovesOnEveryKindOfWrite(t *testing.T) {
	s := testStore(t)

	if m, err := s.ModTime(); err != nil || m != 0 {
		t.Fatalf("ModTime before any write = %d, %v; want 0", m, err)
	}

	last := int64(0)
	bump := func(step string, op func() error) {
		t.Helper()
		// Coarse-mtime filesystems need real time between writes for the
		// signal to be observable at all.
		time.Sleep(10 * time.Millisecond)
		if err := op(); err != nil {
			t.Fatalf("%s: %v", step, err)
		}
		m, err := s.ModTime()
		if err != nil {
			t.Fatalf("%s: ModTime: %v", step, err)
		}
		if m <= last {
			t.Errorf("%s did not move ModTime (%d -> %d)", step, last, m)
		}
		last = m
	}

	bump("add", func() error { _, _, err := s.Add("x", task.Do); return err })
	bump("second add", func() error { _, _, err := s.Add("y", task.Do); return err })
	bump("space new", func() error { _, err := s.NewSpace("other"); return err })
	bump("space use", func() error { _, err := s.UseSpace("other"); return err })
	bump("space rm", func() error { _, err := s.RemoveSpace("other", false); return err })
}

// rawFile decodes the data file as plain JSON, for assertions about the shape
// on disk rather than the shape in memory.
func rawFile(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// onDiskVersion reports the schema version the file currently carries.
func onDiskVersion(t *testing.T, path string) int {
	t.Helper()
	v, ok := rawFile(t, path)["version"].(float64)
	if !ok {
		t.Fatalf("%s has no version", path)
	}
	return int(v)
}

// rawSpace returns one space's body from its file on disk.
func rawSpace(t *testing.T, path, name string) map[string]any {
	t.Helper()
	return rawFile(t, filepath.Join(spacesDir(path), encodeSpaceFilename(name)))
}

func TestFileFormat(t *testing.T) {
	s := testStore(t)
	s.Add("x", task.Do)

	// The manifest holds the document-level fields, and deliberately no
	// "spaces" key: a version-4 binary must refuse it on the version check,
	// not decode an empty matrix it was about to overwrite.
	m := rawFile(t, s.Path())
	for _, k := range []string{"version", "current"} {
		if _, ok := m[k]; !ok {
			t.Errorf("manifest missing top-level key %q", k)
		}
	}
	for _, k := range []string{"spaces", "tasks", "next_id"} {
		if _, ok := m[k]; ok {
			t.Errorf("manifest should not hold key %q", k)
		}
	}
	// The matrix lives in the space's own file, which is self-describing —
	// the same shape `ike space export` writes.
	body := rawSpace(t, s.Path(), defaultSpace)
	for _, k := range []string{"version", "name", "next_id", "tasks"} {
		if _, ok := body[k]; !ok {
			t.Errorf("space file missing key %q", k)
		}
	}
	// The derived fields describe the document, not the matrix, and the
	// consent flags must have no way into a file that export can copy.
	for _, k := range []string{"space", "spaces", "all_spaces", "current", "mcp_enabled", "agent_enabled"} {
		if _, ok := body[k]; ok {
			t.Errorf("space file should not persist key %q", k)
		}
	}
}

// A file from before spaces existed becomes a single-space file, and nothing in
// it is lost on the way.
func TestUpgradesSingleMatrixFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.json")
	v3 := `{
	  "version": 3,
	  "next_id": 7,
	  "tasks": [{"id": 1, "title": "active", "quadrant": 1, "rank": 1024}],
	  "archive": [{"id": 2, "title": "done", "quadrant": 2, "done_at": "2026-07-01T10:00:00Z"}],
	  "quadrant_labels": {"1": "Firefighting"},
	  "undo": [{"label": "add \"active\"", "tasks": []}],
	  "mcp_enabled": true
	}`
	if err := os.WriteFile(path, []byte(v3), 0o600); err != nil {
		t.Fatal(err)
	}
	s := OpenAt(path)

	d, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if d.Space != defaultSpace {
		t.Errorf("space = %q, want %q", d.Space, defaultSpace)
	}
	if d.NextID != 7 {
		t.Errorf("next_id = %d, want 7 preserved", d.NextID)
	}
	if len(d.Tasks) != 1 || len(d.Archive) != 1 {
		t.Errorf("tasks = %d, archive = %d, want 1 and 1", len(d.Tasks), len(d.Archive))
	}
	if got := d.Labels.Of(task.Do); got != "Firefighting" {
		t.Errorf("label = %q, want the rename preserved", got)
	}
	// v3 history is still readable, unlike v2's, so it survives the upgrade.
	if len(d.Undo) != 1 {
		t.Errorf("undo = %d, want 1 preserved", len(d.Undo))
	}
	if !d.MCPAllowed {
		t.Error("mcp_enabled should survive the upgrade")
	}

	// Writing persists the new shape, and the gate lands on the document rather
	// than inside the space.
	if _, _, err := s.Add("after", task.Do); err != nil {
		t.Fatal(err)
	}
	if v := onDiskVersion(t, path); v != currentVersion {
		t.Errorf("on-disk version = %d, want %d", v, currentVersion)
	}
	if enabled, _ := rawFile(t, path)["mcp_enabled"].(bool); !enabled {
		t.Error("mcp_enabled should be a document-level key after the upgrade")
	}
}

// The v4→v5 migration happens on the first write, never on read, and the
// monolith survives it twice over: as the rolling .bak of the manifest that
// replaced it, and as a one-time .pre-v5.bak that nothing ever overwrites.
func TestMigrationSplitsMonolithOnFirstWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.json")
	v4 := `{
	  "version": 4,
	  "current": "work",
	  "mcp_enabled": true,
	  "spaces": {
	    "work": {"next_id": 2, "tasks": [{"id": 1, "title": "a", "quadrant": 1, "rank": 1024}]},
	    "personal": {"next_id": 1, "tasks": []}
	  }
	}`
	if err := os.WriteFile(path, []byte(v4), 0o600); err != nil {
		t.Fatal(err)
	}
	s := OpenAt(path)

	// Reads serve the old layout indefinitely and write nothing.
	if _, err := s.Load(); err != nil {
		t.Fatal(err)
	}
	if v := onDiskVersion(t, path); v != 4 {
		t.Errorf("a read migrated the file to version %d", v)
	}
	if _, err := os.Stat(spacesDir(path)); !os.IsNotExist(err) {
		t.Error("a read created the spaces directory")
	}

	// The first mutation migrates the whole document.
	if _, _, err := s.InSpace("personal").Add("x", task.Do); err != nil {
		t.Fatal(err)
	}
	if v := onDiskVersion(t, path); v != currentVersion {
		t.Errorf("on-disk version = %d, want %d", v, currentVersion)
	}
	m := rawFile(t, path)
	if _, ok := m["spaces"]; ok {
		t.Error("the manifest still holds a spaces key")
	}
	if cur, _ := m["current"].(string); cur != "work" {
		t.Errorf("current = %q, want %q preserved", cur, "work")
	}
	if on, _ := m["mcp_enabled"].(bool); !on {
		t.Error("mcp_enabled should survive the migration")
	}
	work := rawSpace(t, path, "work")
	if tasks, _ := work["tasks"].([]any); len(tasks) != 1 {
		t.Errorf("work tasks = %v, want the monolith's task", work["tasks"])
	}
	personal := rawSpace(t, path, "personal")
	if tasks, _ := personal["tasks"].([]any); len(tasks) != 1 {
		t.Errorf("personal tasks = %v, want the added task", personal["tasks"])
	}

	// The pre-migration monolith is kept, byte for byte, and a later write
	// does not touch it — the rolling .bak is clobbered by the very next
	// document-level change, so this copy is the durable escape hatch.
	bak, err := os.ReadFile(path + preV5BackupSuffix)
	if err != nil {
		t.Fatal(err)
	}
	if string(bak) != v4 {
		t.Errorf("pre-v5 backup = %s, want the original monolith", bak)
	}
	if _, _, err := s.Add("later", task.Do); err != nil {
		t.Fatal(err)
	}
	bak2, _ := os.ReadFile(path + preV5BackupSuffix)
	if string(bak2) != v4 {
		t.Error("a later write rewrote the pre-v5 backup")
	}
}

// A migration that crashed before its commit point — the manifest write —
// leaves space files beside a still-authoritative monolith. The next
// migration must clear them, or a space deleted since the crash would
// resurrect beside the real ones.
func TestMigrationClearsDebrisOfACrashedMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.json")
	v4 := `{"version": 4, "current": "work",
	  "spaces": {"work": {"next_id": 2, "tasks": [{"id": 1, "title": "real", "quadrant": 1, "rank": 1024}]}}}`
	if err := os.WriteFile(path, []byte(v4), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(spacesDir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	stale := `{"version": 5, "name": "deleted-since", "next_id": 9, "tasks": []}`
	staleWork := `{"version": 5, "name": "work", "next_id": 9, "tasks": []}`
	os.WriteFile(filepath.Join(spacesDir(path), "deleted-since.json"), []byte(stale), 0o600)
	os.WriteFile(filepath.Join(spacesDir(path), "work.json"), []byte(staleWork), 0o600)

	s := OpenAt(path)
	if _, _, err := s.Add("x", task.Do); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(spacesDir(path), "deleted-since.json")); !os.IsNotExist(err) {
		t.Error("crash debris survived the migration and resurrected a space")
	}
	work := rawSpace(t, path, "work")
	if id, _ := work["next_id"].(float64); int(id) != 3 {
		t.Errorf("work next_id = %v, want the monolith's state (3), not the debris", work["next_id"])
	}
}

// One corrupt space file must cost that one space, not the document — that is
// the point of the split. The others keep working, the broken one is listed
// rather than hidden, no write touches its file, and removing it with force
// is the recovery affordance.
func TestCorruptSpaceFileDegradesGracefully(t *testing.T) {
	s := testStore(t)
	if _, _, err := s.Add("keep", task.Do); err != nil {
		t.Fatal(err)
	}
	if _, err := s.NewSpace("broken"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.InSpace("broken").Add("doomed", task.Do); err != nil {
		t.Fatal(err)
	}
	brokenPath := filepath.Join(spacesDir(s.Path()), encodeSpaceFilename("broken"))
	if err := os.WriteFile(brokenPath, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	// The healthy space still loads, and the listing shows both — a space
	// silently missing from the picker is how loss goes unnoticed.
	d, err := s.Load()
	if err != nil {
		t.Fatalf("the healthy space should load: %v", err)
	}
	if len(d.AllSpaces) != 2 {
		t.Fatalf("AllSpaces = %+v, want both spaces listed", d.AllSpaces)
	}
	if !d.AllSpaces[0].Unreadable || d.AllSpaces[0].Name != "broken" {
		t.Errorf("AllSpaces[0] = %+v, want broken marked unreadable", d.AllSpaces[0])
	}

	// Resolving the broken space names the real problem, not a typo hunt.
	if _, err := s.InSpace("broken").Load(); err == nil || !strings.Contains(err.Error(), "unreadable") {
		t.Errorf("loading the broken space = %v, want an unreadable error", err)
	}
	if _, _, err := s.InSpace("broken").Add("x", task.Do); err == nil {
		t.Error("mutating an unreadable space should fail")
	}

	// A write to the healthy space leaves the corrupt bytes exactly as they
	// are: they may be recoverable by hand, and nothing may foreclose that.
	if _, _, err := s.Add("more", task.Do); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(brokenPath)
	if err != nil {
		t.Fatalf("a write removed the unreadable file: %v", err)
	}
	if string(after) != "{not json" {
		t.Errorf("a write altered the unreadable file: %q", after)
	}

	// Removal needs force, and even then the file is renamed, not deleted.
	if _, err := s.RemoveSpace("broken", false); err == nil {
		t.Error("removing an unreadable space without force should fail")
	}
	removed, err := s.RemoveSpace("broken", true)
	if err != nil {
		t.Fatal(err)
	}
	if !removed.Unreadable || removed.Name != "broken" {
		t.Errorf("removed = %+v, want the unreadable space", removed)
	}
	if _, err := os.Stat(brokenPath); !os.IsNotExist(err) {
		t.Error("the unreadable file is still in the spaces directory")
	}
	if bak, err := os.ReadFile(brokenPath + ".bak"); err != nil || string(bak) != "{not json" {
		t.Errorf("the unreadable file should survive as .bak: %q, %v", bak, err)
	}
	if d, err := s.Load(); err != nil || len(d.AllSpaces) != 1 {
		t.Errorf("after removal: %+v, %v; want one healthy space", d.AllSpaces, err)
	}
}

// A corrupt file behind the *current* space must fail loudly, not quietly
// repair Current toward some other space — the user has to be told the space
// they work in needs attention.
func TestCorruptCurrentSpaceStaysCurrent(t *testing.T) {
	s := testStore(t)
	if _, _, err := s.Add("x", task.Do); err != nil {
		t.Fatal(err)
	}
	if _, err := s.NewSpace("other"); err != nil {
		t.Fatal(err)
	}
	defaultPath := filepath.Join(spacesDir(s.Path()), encodeSpaceFilename(defaultSpace))
	if err := os.WriteFile(defaultPath, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := s.Load(); err == nil || !strings.Contains(err.Error(), "unreadable") {
		t.Errorf("bare Load = %v, want the unreadable error for the current space", err)
	}
	if _, err := s.InSpace("other").Load(); err != nil {
		t.Errorf("the other space should still load: %v", err)
	}
	// A write elsewhere must not move Current off the broken space.
	if _, _, err := s.InSpace("other").Add("y", task.Do); err != nil {
		t.Fatal(err)
	}
	if cur, _ := rawFile(t, s.Path())["current"].(string); cur != defaultSpace {
		t.Errorf("current = %q; a write repaired it away from the unreadable space", cur)
	}
}

// Even with every space unreadable the document still answers what it can:
// the listing, and both consent readers.
func TestAllSpacesCorruptStillListsAndAnswersConsent(t *testing.T) {
	s := testStore(t)
	if _, _, err := s.Add("x", task.Do); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetMCPEnabled(true); err != nil {
		t.Fatal(err)
	}
	defaultPath := filepath.Join(spacesDir(s.Path()), encodeSpaceFilename(defaultSpace))
	if err := os.WriteFile(defaultPath, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}

	infos, err := s.ListSpaces()
	if err != nil {
		t.Fatalf("ListSpaces with every space corrupt: %v", err)
	}
	if len(infos) != 1 || !infos[0].Unreadable {
		t.Errorf("infos = %+v, want the one unreadable space", infos)
	}
	if on, err := s.MCPEnabled(); err != nil || !on {
		t.Errorf("MCPEnabled = %v, %v; consent must stay readable", on, err)
	}
	if _, err := s.Load(); err == nil {
		t.Error("loading an unreadable space should still fail")
	}
}

// A v4 file with no spaces is a truncated or hand-mangled file, not an empty
// matrix. Accepting it would mean the next write erased whatever was there.
func TestVersionFourWithNoSpacesIsAnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.json")
	for _, body := range []string{
		`{"version": 4}`,
		`{"version": 4, "spaces": {}}`,
		`{"version": 4, "current": "default", "spaces": {"default": null}}`,
	} {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := OpenAt(path).Load(); err == nil {
			t.Errorf("%s should not load", body)
		}
	}
}

// A current that names nothing is repaired on read rather than breaking every
// command, but an explicitly requested missing space still fails.
func TestDanglingCurrentIsRepairedOnRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.json")
	body := `{
	  "version": 4,
	  "current": "gone",
	  "spaces": {
	    "work": {"next_id": 1, "tasks": []},
	    "home": {"next_id": 1, "tasks": []}
	  }
	}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	d, err := OpenAt(path).Load()
	if err != nil {
		t.Fatalf("a dangling current should be repaired, not fatal: %v", err)
	}
	if d.Space != "home" {
		t.Errorf("space = %q, want the alphabetically first survivor %q", d.Space, "home")
	}
	if _, err := OpenAt(path).InSpace("gone").Load(); err == nil {
		t.Error("an explicitly requested missing space should still error")
	}
}
