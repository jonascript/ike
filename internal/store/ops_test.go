package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/jonascript/ike/internal/task"
)

// titles returns the titles of q's tasks in display order.

func titlesOf(ts []task.Task) []string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = t.Title
	}
	return out
}

// An empty archive must marshal as [] rather than null: `ike archive --json`
// is a machine interface, and a nil slice would change its shape.

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// titles returns the titles of q's tasks in display order.
func titles(t *testing.T, s *Store, q task.Quadrant) []string {
	t.Helper()
	ts, err := s.List(q)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, len(ts))
	for i, tk := range ts {
		out[i] = tk.Title
	}
	return out
}

func TestRestoreUnarchives(t *testing.T) {
	s := testStore(t)
	added, _, err := s.Add("write the thing", task.Delegate)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Complete(added.ID); err != nil {
		t.Fatal(err)
	}

	restored, _, err := s.Restore(added.ID)
	if err != nil {
		t.Fatal(err)
	}
	if restored.DoneAt != nil {
		t.Errorf("restored task still has DoneAt = %v", restored.DoneAt)
	}
	if restored.Quadrant != task.Delegate {
		t.Errorf("restored to quadrant %v, want Delegate", restored.Quadrant)
	}
	if restored.ID != added.ID {
		t.Errorf("restored ID = %d, want %d (IDs are stable)", restored.ID, added.ID)
	}

	d, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Tasks) != 1 || len(d.Archive) != 0 {
		t.Errorf("after restore: %d active, %d archived; want 1 and 0", len(d.Tasks), len(d.Archive))
	}
}

func TestRestoreUnknownID(t *testing.T) {
	s := testStore(t)
	a, _, _ := s.Add("still active", task.Do)
	if _, _, err := s.Restore(a.ID); err == nil {
		t.Error("restoring an active (not archived) task should error")
	}
	if _, _, err := s.Restore(999); err == nil {
		t.Error("restoring an unknown id should error")
	}
}

func TestRestoreGoesToBottom(t *testing.T) {
	s := testStore(t)
	a, _, _ := s.Add("first", task.Do)
	s.Add("second", task.Do)
	if _, _, err := s.Complete(a.ID); err != nil {
		t.Fatal(err)
	}
	s.Add("third", task.Do)
	if _, _, err := s.Restore(a.ID); err != nil {
		t.Fatal(err)
	}
	got := titles(t, s, task.Do)
	want := []string{"second", "third", "first"}
	if !equal(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
}

func TestReorderWithinQuadrant(t *testing.T) {
	s := testStore(t)
	s.Add("a", task.Schedule)
	b, _, _ := s.Add("b", task.Schedule)
	c, _, _ := s.Add("c", task.Schedule)

	if _, _, err := s.Reorder(c.ID, -1); err != nil {
		t.Fatal(err)
	}
	if got, want := titles(t, s, task.Schedule), []string{"a", "c", "b"}; !equal(got, want) {
		t.Fatalf("after c up: %v, want %v", got, want)
	}

	if _, _, err := s.Reorder(b.ID, ToTop); err != nil {
		t.Fatal(err)
	}
	if got, want := titles(t, s, task.Schedule), []string{"b", "a", "c"}; !equal(got, want) {
		t.Fatalf("after b to top: %v, want %v", got, want)
	}

	if _, _, err := s.Reorder(b.ID, ToBottom); err != nil {
		t.Fatal(err)
	}
	if got, want := titles(t, s, task.Schedule), []string{"a", "c", "b"}; !equal(got, want) {
		t.Fatalf("after b to bottom: %v, want %v", got, want)
	}
}

func TestReorderClampsAtEnds(t *testing.T) {
	s := testStore(t)
	a, _, _ := s.Add("a", task.Do)
	s.Add("b", task.Do)

	// Already at the top; moving up is a no-op, not an error.
	if _, _, err := s.Reorder(a.ID, -1); err != nil {
		t.Fatal(err)
	}
	if got, want := titles(t, s, task.Do), []string{"a", "b"}; !equal(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}

	// A no-op reorder must not consume an undo slot.
	d, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if UndoLabel(d) != `add "b"` {
		t.Errorf("undo top = %q, want the add; a no-op reorder should not record", UndoLabel(d))
	}
}

func TestReorderDoesNotLeakAcrossQuadrants(t *testing.T) {
	s := testStore(t)
	s.Add("do-1", task.Do)
	sched, _, _ := s.Add("sched-1", task.Schedule)
	s.Add("sched-2", task.Schedule)

	if _, _, err := s.Reorder(sched.ID, ToBottom); err != nil {
		t.Fatal(err)
	}
	if got, want := titles(t, s, task.Do), []string{"do-1"}; !equal(got, want) {
		t.Errorf("Do quadrant changed: %v, want %v", got, want)
	}
	if got, want := titles(t, s, task.Schedule), []string{"sched-2", "sched-1"}; !equal(got, want) {
		t.Errorf("Schedule order = %v, want %v", got, want)
	}
}

func TestReorderSurvivesManyMoves(t *testing.T) {
	s := testStore(t)
	a, _, _ := s.Add("a", task.Do)
	s.Add("b", task.Do)

	// Ranks are rewritten per quadrant, so swapping repeatedly must not drift.
	for i := 0; i < 200; i++ {
		if _, _, err := s.Reorder(a.ID, 1); err != nil {
			t.Fatal(err)
		}
		if _, _, err := s.Reorder(a.ID, -1); err != nil {
			t.Fatal(err)
		}
	}
	if got, want := titles(t, s, task.Do), []string{"a", "b"}; !equal(got, want) {
		t.Errorf("order after 200 round trips = %v, want %v", got, want)
	}
	d, _ := s.Load()
	for _, tk := range d.Tasks {
		if tk.Rank <= 0 {
			t.Errorf("task %d has non-positive rank %v", tk.ID, tk.Rank)
		}
	}
}

func TestUpgradesVersionOneFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "tasks.json")
	// A version-1 file: no ranks, no undo stack.
	v1 := `{
	  "version": 1,
	  "next_id": 4,
	  "tasks": [
	    {"id": 3, "title": "third", "quadrant": 1, "created_at": "2026-01-03T00:00:00Z"},
	    {"id": 1, "title": "first", "quadrant": 1, "created_at": "2026-01-01T00:00:00Z"},
	    {"id": 2, "title": "second", "quadrant": 1, "created_at": "2026-01-02T00:00:00Z"}
	  ],
	  "archive": []
	}`
	if err := os.WriteFile(p, []byte(v1), 0o644); err != nil {
		t.Fatal(err)
	}
	s := OpenAt(p)

	// Unranked tasks fall back to ID order.
	if got, want := titles(t, s, task.Do), []string{"first", "second", "third"}; !equal(got, want) {
		t.Fatalf("v1 order = %v, want %v", got, want)
	}

	// Reordering works immediately, and the write persists version 2 + ranks.
	if _, _, err := s.Reorder(3, ToTop); err != nil {
		t.Fatal(err)
	}
	if got, want := titles(t, s, task.Do), []string{"third", "first", "second"}; !equal(got, want) {
		t.Fatalf("after reorder = %v, want %v", got, want)
	}

	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if v, _ := m["version"].(float64); int(v) != currentVersion {
		t.Errorf("on-disk version = %v, want %d", m["version"], currentVersion)
	}
}

// Labels.Of is the single choke point every frontend renders a quadrant name
// through, so it sanitizes. SetQuadrantLabel rejects control characters on the
// way in; this covers a label that reached the file some other way — an older
// build, a hand edit, or a synced copy.

// Archive order is defined once, in Data.ListArchive, and both `ike archive`
// and the TUI's archive view render it. The TUI used to reverse the stored
// slice instead, which agrees only while every archived task carries a
// completion stamp — a file written by an older build, or hand-edited, need
// not. This pins the definition that both now share.
func TestListArchiveOrdersUnstampedTasksByID(t *testing.T) {
	done := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	d := Data{Archive: []task.Task{
		{ID: 1, Title: "no stamp, low id"},
		{ID: 3, Title: "stamped", DoneAt: &done},
		{ID: 2, Title: "no stamp, high id"},
	}}

	var got []int
	for _, t := range d.ListArchive() {
		got = append(got, t.ID)
	}
	// Descending by ID wherever a completion time is missing, newest first
	// where it is present — never the raw slice order.
	want := []int{3, 2, 1}
	if !slices.Equal(got, want) {
		t.Errorf("ListArchive order = %v, want %v", got, want)
	}
}

// The store's own listing and the pure form over the same data must agree,
// since the frontends split between them.

// The store's own listing and the pure form over the same data must agree,
// since the frontends split between them.
func TestStoreListMatchesDataList(t *testing.T) {
	s := testStore(t)
	for _, title := range []string{"a", "b", "c"} {
		if _, _, err := s.Add(title, task.Schedule); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := s.Add("urgent", task.Do); err != nil {
		t.Fatal(err)
	}

	d, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []task.Quadrant{0, task.Do, task.Schedule} {
		viaStore, err := s.List(q)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(titlesOf(viaStore), titlesOf(d.List(q))) {
			t.Errorf("quadrant %d: Store.List = %v, Data.List = %v",
				q, titlesOf(viaStore), titlesOf(d.List(q)))
		}
	}
}

// An empty archive must marshal as [] rather than null: `ike archive --json`
// is a machine interface, and a nil slice would change its shape.
func TestListArchiveIsNeverNil(t *testing.T) {
	if got := (Data{}).ListArchive(); got == nil {
		t.Error("ListArchive() on an empty archive returned nil, want an empty slice")
	}
	if got := (Data{}).List(task.Do); got == nil {
		t.Error("List() with no tasks returned nil, want an empty slice")
	}
}
