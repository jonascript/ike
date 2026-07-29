package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/jonascript/ike/internal/task"
)

// archiveIDs returns the archived task IDs, sorted, for stable comparison.
func archiveIDs(t *testing.T, s *Store) []int {
	t.Helper()
	d, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]int, 0, len(d.Archive))
	for _, a := range d.Archive {
		ids = append(ids, a.ID)
	}
	slices.Sort(ids)
	return ids
}

func activeIDs(t *testing.T, s *Store) []int {
	t.Helper()
	d, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]int, 0, len(d.Tasks))
	for _, tk := range d.Tasks {
		ids = append(ids, tk.ID)
	}
	slices.Sort(ids)
	return ids
}

// The archive is no longer copied into every snapshot. This is the property that
// replaced it: a complete is reversed by noticing the task is active again.
func TestSnapshotsDoNotStoreTheArchive(t *testing.T) {
	s := testStore(t)
	for i := range 3 {
		if _, _, err := s.Add("task", task.Do); err != nil {
			t.Fatal(i, err)
		}
	}
	if _, _, err := s.Complete(1); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Add("after", task.Schedule); err != nil {
		t.Fatal(err)
	}

	d, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Undo) == 0 {
		t.Fatal("no undo history recorded")
	}
	for i, snap := range d.Undo {
		if snap.ArchiveEntry != nil {
			t.Errorf("undo[%d] (%s) stores an archive entry; only a restore should",
				i, snap.Label)
		}
	}

	// And nothing archive-shaped reaches the file for these mutations.
	raw, err := os.ReadFile(s.Path())
	if err != nil {
		t.Fatal(err)
	}
	var onDisk struct {
		Undo []map[string]json.RawMessage `json:"undo"`
	}
	if err := json.Unmarshal(raw, &onDisk); err != nil {
		t.Fatal(err)
	}
	for i, snap := range onDisk.Undo {
		if _, ok := snap["archive"]; ok {
			t.Errorf("undo[%d] on disk still has an \"archive\" key", i)
		}
	}
}

func TestUndoRedoCompleteWithoutStoredArchive(t *testing.T) {
	s := testStore(t)
	if _, _, err := s.Add("finish me", task.Do); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Complete(1); err != nil {
		t.Fatal(err)
	}
	if got := archiveIDs(t, s); !slices.Equal(got, []int{1}) {
		t.Fatalf("archive after complete = %v, want [1]", got)
	}

	// Undo: the task comes back and must not remain archived as well.
	if _, _, err := s.Undo(); err != nil {
		t.Fatal(err)
	}
	if got := activeIDs(t, s); !slices.Equal(got, []int{1}) {
		t.Errorf("active after undo = %v, want [1]", got)
	}
	if got := archiveIDs(t, s); len(got) != 0 {
		t.Errorf("archive after undo = %v, want empty (task is active again)", got)
	}

	// Redo: back to archived, with its completion stamp intact.
	if _, _, err := s.Redo(); err != nil {
		t.Fatal(err)
	}
	if got := activeIDs(t, s); len(got) != 0 {
		t.Errorf("active after redo = %v, want empty", got)
	}
	if got := archiveIDs(t, s); !slices.Equal(got, []int{1}) {
		t.Errorf("archive after redo = %v, want [1]", got)
	}
	d, _ := s.Load()
	if d.Archive[0].DoneAt == nil {
		t.Error("redone complete lost its DoneAt stamp")
	}
}

// The empty-archive case is the one a nil slice would have got wrong: undoing
// the first ever complete has to clear the archive, not leave it alone.
func TestUndoFirstCompleteOnEmptyArchive(t *testing.T) {
	s := testStore(t)
	if _, _, err := s.Add("only task", task.Do); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Complete(1); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Undo(); err != nil {
		t.Fatal(err)
	}

	if got := archiveIDs(t, s); len(got) != 0 {
		t.Fatalf("archive = %v, want empty; the task must not be both active and archived", got)
	}
	if got := activeIDs(t, s); !slices.Equal(got, []int{1}) {
		t.Fatalf("active = %v, want [1]", got)
	}
}

func TestUndoRedoRestoreKeepsTheArchivedEntry(t *testing.T) {
	s := testStore(t)
	if _, _, err := s.Add("round trip", task.Delegate); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Complete(1); err != nil {
		t.Fatal(err)
	}
	d, _ := s.Load()
	doneAt := *d.Archive[0].DoneAt

	if _, _, err := s.Restore(1); err != nil {
		t.Fatal(err)
	}
	if got := archiveIDs(t, s); len(got) != 0 {
		t.Fatalf("archive after restore = %v, want empty", got)
	}

	// Undoing a restore must put the entry back *with its DoneAt*, which is the
	// one thing the task list cannot supply.
	if _, _, err := s.Undo(); err != nil {
		t.Fatal(err)
	}
	d, _ = s.Load()
	if len(d.Archive) != 1 {
		t.Fatalf("archive after undo = %v, want the entry back", archiveIDs(t, s))
	}
	if d.Archive[0].DoneAt == nil || !d.Archive[0].DoneAt.Equal(doneAt) {
		t.Errorf("DoneAt = %v, want %v preserved through undo", d.Archive[0].DoneAt, doneAt)
	}
	if got := activeIDs(t, s); len(got) != 0 {
		t.Errorf("active after undo of restore = %v, want empty", got)
	}

	// Redo makes it active again and removes it from the archive.
	if _, _, err := s.Redo(); err != nil {
		t.Fatal(err)
	}
	if got := archiveIDs(t, s); len(got) != 0 {
		t.Errorf("archive after redo of restore = %v, want empty", got)
	}
	if got := activeIDs(t, s); !slices.Equal(got, []int{1}) {
		t.Errorf("active after redo of restore = %v, want [1]", got)
	}
}

// Undoing an unrelated change must leave the archive exactly as it is: the
// reconstruction only touches entries whose task became active again.
func TestUndoOfNonArchiveOpLeavesArchiveAlone(t *testing.T) {
	s := testStore(t)
	for range 3 {
		if _, _, err := s.Add("task", task.Do); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := s.Complete(1); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Complete(2); err != nil {
		t.Fatal(err)
	}
	// Several non-archive mutations on top.
	if _, _, err := s.Add("new", task.Schedule); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Rename(3, "renamed"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Move(3, task.Eliminate); err != nil {
		t.Fatal(err)
	}

	for i := range 3 {
		if _, _, err := s.Undo(); err != nil {
			t.Fatalf("undo %d: %v", i, err)
		}
		if got := archiveIDs(t, s); !slices.Equal(got, []int{1, 2}) {
			t.Fatalf("after undo %d the archive = %v, want [1 2] untouched", i, got)
		}
	}
}

// Walking the whole history back and forward must return the exact same state,
// which is the real guarantee the reconstruction has to provide.
func TestInterleavedHistoryRoundTrips(t *testing.T) {
	s := testStore(t)
	steps := []func() error{
		func() error { _, _, e := s.Add("a", task.Do); return e },
		func() error { _, _, e := s.Add("b", task.Schedule); return e },
		func() error { _, _, e := s.Add("c", task.Delegate); return e },
		func() error { _, _, e := s.Complete(1); return e },
		func() error { _, _, e := s.Add("d", task.Do); return e },
		func() error { _, _, e := s.Complete(2); return e },
		func() error { _, _, e := s.Restore(1); return e },
		func() error { _, _, e := s.Move(3, task.Eliminate); return e },
		func() error { _, _, e := s.Complete(3); return e },
		func() error { _, _, e := s.Restore(2); return e },
	}
	for i, step := range steps {
		if err := step(); err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
	}

	final, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	wantActive, wantArchive := activeIDs(t, s), archiveIDs(t, s)

	// All the way back.
	for i := range steps {
		if _, _, err := s.Undo(); err != nil {
			t.Fatalf("undo %d: %v", i, err)
		}
	}
	if got := activeIDs(t, s); len(got) != 0 {
		t.Errorf("after undoing everything, active = %v, want empty", got)
	}
	if got := archiveIDs(t, s); len(got) != 0 {
		t.Errorf("after undoing everything, archive = %v, want empty", got)
	}

	// And all the way forward again.
	for i := range steps {
		if _, _, err := s.Redo(); err != nil {
			t.Fatalf("redo %d: %v", i, err)
		}
	}
	if got := activeIDs(t, s); !slices.Equal(got, wantActive) {
		t.Errorf("active after full redo = %v, want %v", got, wantActive)
	}
	if got := archiveIDs(t, s); !slices.Equal(got, wantArchive) {
		t.Errorf("archive after full redo = %v, want %v", got, wantArchive)
	}

	got, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	// Completion stamps have to survive the whole round trip too.
	for _, want := range final.Archive {
		i := slices.IndexFunc(got.Archive, func(a task.Task) bool { return a.ID == want.ID })
		if i < 0 {
			t.Errorf("archived task %d missing after round trip", want.ID)
			continue
		}
		if (got.Archive[i].DoneAt == nil) != (want.DoneAt == nil) ||
			(want.DoneAt != nil && !got.Archive[i].DoneAt.Equal(*want.DoneAt)) {
			t.Errorf("task %d DoneAt = %v, want %v", want.ID, got.Archive[i].DoneAt, want.DoneAt)
		}
	}
}

// A pre-v3 file stored a whole archive per snapshot and no ArchiveEntry, so its
// history cannot be replayed safely. The tasks must survive; the history is
// dropped on purpose.
func TestUpgradeFromV2DropsHistoryButKeepsTasks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.json")
	v2 := `{
	  "version": 2,
	  "next_id": 4,
	  "tasks": [{"id": 1, "title": "active", "quadrant": 1, "rank": 1024}],
	  "archive": [{"id": 2, "title": "done", "quadrant": 2, "done_at": "2026-07-01T10:00:00Z"}],
	  "undo": [{"label": "restore \"x\"", "tasks": [], "archive": [{"id": 3, "title": "gone", "quadrant": 1}]}],
	  "redo": [{"label": "complete \"y\"", "tasks": [], "archive": []}]
	}`
	if err := os.WriteFile(path, []byte(v2), 0o600); err != nil {
		t.Fatal(err)
	}
	s := OpenAt(path)

	d, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if d.Version != currentVersion {
		t.Errorf("version = %d, want %d after upgrade", d.Version, currentVersion)
	}
	if len(d.Tasks) != 1 || d.Tasks[0].Title != "active" {
		t.Errorf("tasks = %+v, want the active task preserved", d.Tasks)
	}
	if len(d.Archive) != 1 || d.Archive[0].Title != "done" {
		t.Errorf("archive = %+v, want the archived task preserved", d.Archive)
	}
	if len(d.Undo) != 0 || len(d.Redo) != 0 {
		t.Errorf("history = %d undo / %d redo, want both dropped on upgrade",
			len(d.Undo), len(d.Redo))
	}
	if _, _, err := s.Undo(); err == nil {
		t.Error("undo should report nothing to undo after an upgrade")
	}

	// The upgrade persists, and normal operation resumes.
	if _, _, err := s.Add("post upgrade", task.Do); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Undo(); err != nil {
		t.Errorf("undo of a post-upgrade change: %v", err)
	}
}

// A task with a quadrant outside 1-4 used to persist through every write while
// appearing in no human-facing view — not the TUI, not `ike list` — yet still
// showing up in `--json`.
func TestOutOfRangeQuadrantIsMadeVisible(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.json")
	raw := `{
	  "version": 3,
	  "next_id": 6,
	  "tasks": [
	    {"id": 1, "title": "normal", "quadrant": 1, "rank": 1024},
	    {"id": 2, "title": "zero quadrant", "quadrant": 0},
	    {"id": 3, "title": "too high", "quadrant": 7},
	    {"id": 4, "title": "negative", "quadrant": -2}
	  ],
	  "archive": [{"id": 5, "title": "archived oddity", "quadrant": 9, "done_at": "2026-07-01T10:00:00Z"}]
	}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	s := OpenAt(path)

	d, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, tk := range d.Tasks {
		if !tk.Quadrant.Valid() {
			t.Errorf("task %d (%q) still has quadrant %d", tk.ID, tk.Title, tk.Quadrant)
		}
	}
	for _, a := range d.Archive {
		if !a.Quadrant.Valid() {
			t.Errorf("archived task %d still has quadrant %d", a.ID, a.Quadrant)
		}
	}

	// Every task is now reachable through the grouped view the CLI and TUI use.
	var seen int
	for q := task.Do; q <= task.Eliminate; q++ {
		seen += len(d.List(q))
	}
	if seen != len(d.Tasks) {
		t.Errorf("grouped views show %d of %d tasks", seen, len(d.Tasks))
	}

	// Rescued tasks land in Eliminate and get a rank, so ordering still works.
	rescued := d.List(task.Eliminate)
	if len(rescued) != 3 {
		t.Fatalf("Eliminate holds %d tasks, want the 3 rescued ones", len(rescued))
	}
	for _, tk := range rescued {
		if tk.Rank == 0 {
			t.Errorf("rescued task %d has no rank, so it cannot be reordered", tk.ID)
		}
	}

	// The repair persists rather than being re-done on every read.
	if _, _, err := s.Add("trigger a write", task.Do); err != nil {
		t.Fatal(err)
	}
	var onDisk struct {
		Tasks []struct {
			ID       int `json:"id"`
			Quadrant int `json:"quadrant"`
		} `json:"tasks"`
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &onDisk); err != nil {
		t.Fatal(err)
	}
	for _, tk := range onDisk.Tasks {
		if tk.Quadrant < 1 || tk.Quadrant > 4 {
			t.Errorf("task %d on disk still has quadrant %d", tk.ID, tk.Quadrant)
		}
	}
}

// Undoing into a hand-edited snapshot must not reintroduce an invisible task.
func TestOutOfRangeQuadrantInSnapshotIsClamped(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.json")
	raw := `{
	  "version": 3,
	  "next_id": 3,
	  "tasks": [{"id": 1, "title": "current", "quadrant": 1, "rank": 1024}],
	  "archive": [],
	  "undo": [{"label": "something", "tasks": [{"id": 2, "title": "hidden", "quadrant": 8}]}]
	}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	s := OpenAt(path)

	if _, _, err := s.Undo(); err != nil {
		t.Fatal(err)
	}
	d, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Tasks) != 1 {
		t.Fatalf("tasks after undo = %+v", d.Tasks)
	}
	if !d.Tasks[0].Quadrant.Valid() {
		t.Errorf("undo restored a task with quadrant %d", d.Tasks[0].Quadrant)
	}
}
