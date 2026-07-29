package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/jonascript/ike/internal/task"
)

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

func TestFileFormat(t *testing.T) {
	s := testStore(t)
	s.Add("x", task.Do)
	b, err := os.ReadFile(s.Path())
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"version", "next_id", "tasks"} {
		if _, ok := m[k]; !ok {
			t.Errorf("file missing key %q", k)
		}
	}
}
