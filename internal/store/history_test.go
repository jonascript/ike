package store

import (
	"path/filepath"
	"testing"

	"github.com/jonascript/ike/internal/task"
)

// titles returns the titles of q's tasks in display order.

func TestUndoOfTaskChangeLeavesLabelsAlone(t *testing.T) {
	s := testStore(t)
	if _, _, err := s.SetQuadrantLabel(task.Do, "Firefighting"); err != nil {
		t.Fatal(err)
	}
	a, _, _ := s.Add("something", task.Do)
	if _, _, err := s.Delete(a.ID); err != nil {
		t.Fatal(err)
	}
	// Undoing the delete must not roll the rename back with it.
	if _, _, err := s.Undo(); err != nil {
		t.Fatal(err)
	}
	labels, _ := s.QuadrantLabels()
	if labels.Of(task.Do) != "Firefighting" {
		t.Errorf("label = %q, want the rename to survive an unrelated undo", labels.Of(task.Do))
	}
}

func TestUndoCannotReopenRevokedAccess(t *testing.T) {
	s := testStore(t)
	if _, err := s.SetMCPEnabled(true); err != nil {
		t.Fatal(err)
	}
	s.Add("a task", task.Do)
	if _, err := s.SetMCPEnabled(false); err != nil {
		t.Fatal(err)
	}

	// No amount of undo or redo may hand access back.
	for i := 0; i < 5; i++ {
		if _, _, err := s.Undo(); err != nil {
			break
		}
		if on, _ := s.MCPEnabled(); on {
			t.Fatalf("undo %d re-enabled revoked MCP access", i)
		}
	}
	for i := 0; i < 5; i++ {
		if _, _, err := s.Redo(); err != nil {
			break
		}
		if on, _ := s.MCPEnabled(); on {
			t.Fatalf("redo %d re-enabled revoked MCP access", i)
		}
	}
}

func TestUndoRestoresPreviousState(t *testing.T) {
	s := testStore(t)
	a, _, _ := s.Add("keep me", task.Do)

	if _, _, err := s.Complete(a.ID); err != nil {
		t.Fatal(err)
	}
	label, _, err := s.Undo()
	if err != nil {
		t.Fatal(err)
	}
	if label != `complete "keep me"` {
		t.Errorf("label = %q, want the complete label", label)
	}
	d, _ := s.Load()
	if len(d.Tasks) != 1 || len(d.Archive) != 0 {
		t.Fatalf("after undo: %d active, %d archived; want 1 and 0", len(d.Tasks), len(d.Archive))
	}

	// Undo the add itself.
	if _, _, err := s.Undo(); err != nil {
		t.Fatal(err)
	}
	d, _ = s.Load()
	if len(d.Tasks) != 0 {
		t.Errorf("after second undo: %d active, want 0", len(d.Tasks))
	}
}

func TestUndoOfDeleteBringsTaskBack(t *testing.T) {
	s := testStore(t)
	a, _, _ := s.Add("oops", task.Do)
	if _, _, err := s.Delete(a.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Undo(); err != nil {
		t.Fatal(err)
	}
	got := titles(t, s, task.Do)
	if !equal(got, []string{"oops"}) {
		t.Errorf("after undoing delete: %v, want [oops]", got)
	}
}

func TestUndoDoesNotReuseIDs(t *testing.T) {
	s := testStore(t)
	a, _, _ := s.Add("first", task.Do)
	if _, _, err := s.Undo(); err != nil { // undo the add
		t.Fatal(err)
	}
	b, _, err := s.Add("second", task.Do)
	if err != nil {
		t.Fatal(err)
	}
	if b.ID == a.ID {
		t.Errorf("ID %d was reused after undo; IDs must stay monotonic", b.ID)
	}
}

func TestUndoEmptyStack(t *testing.T) {
	s := testStore(t)
	if _, _, err := s.Undo(); err == nil {
		t.Error("undo with nothing to undo should error")
	}
}

func TestRepeatedUndoWalksBack(t *testing.T) {
	s := testStore(t)
	s.Add("a", task.Do)
	s.Add("b", task.Do)
	if _, _, err := s.Undo(); err != nil { // undoes add "b"
		t.Fatal(err)
	}
	label, _, err := s.Undo() // must undo add "a", not re-apply add "b"
	if err != nil {
		t.Fatal(err)
	}
	if label != `add "a"` {
		t.Errorf("second undo label = %q, want `add \"a\"`", label)
	}
	if got := titles(t, s, task.Do); len(got) != 0 {
		t.Errorf("after two undos: %v, want nothing", got)
	}
}

func TestRedoReappliesUndoneChange(t *testing.T) {
	s := testStore(t)
	a, _, _ := s.Add("ship it", task.Do)
	if _, _, err := s.Complete(a.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Undo(); err != nil {
		t.Fatal(err)
	}
	if arch, _ := s.ListArchive(); len(arch) != 0 {
		t.Fatal("undo should have un-archived the task")
	}

	label, _, err := s.Redo()
	if err != nil {
		t.Fatal(err)
	}
	if label != `complete "ship it"` {
		t.Errorf("redo label = %q, want the complete label", label)
	}
	active, _ := s.List(0)
	arch, _ := s.ListArchive()
	if len(active) != 0 || len(arch) != 1 {
		t.Errorf("after redo: active=%d archive=%d, want 0/1", len(active), len(arch))
	}
}

func TestRedoRoundTripIsExact(t *testing.T) {
	s := testStore(t)
	s.Add("a", task.Do)
	b, _, _ := s.Add("b", task.Do)
	s.Add("c", task.Do)
	if _, _, err := s.Reorder(b.ID, ToTop); err != nil {
		t.Fatal(err)
	}
	want := titles(t, s, task.Do)

	if _, _, err := s.Undo(); err != nil {
		t.Fatal(err)
	}
	if got := titles(t, s, task.Do); equal(got, want) {
		t.Fatal("undo did not change the order")
	}
	if _, _, err := s.Redo(); err != nil {
		t.Fatal(err)
	}
	if got := titles(t, s, task.Do); !equal(got, want) {
		t.Errorf("after undo+redo: %v, want %v (ordering must survive the round trip)", got, want)
	}
}

func TestNewChangeDiscardsRedo(t *testing.T) {
	s := testStore(t)
	a, _, _ := s.Add("first", task.Do)
	if _, _, err := s.Complete(a.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Undo(); err != nil {
		t.Fatal(err)
	}

	// Diverging from the undone branch must strand it: redoing the complete
	// here would clobber the task added below.
	if _, _, err := s.Add("second", task.Do); err != nil {
		t.Fatal(err)
	}
	d, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Redo) != 0 {
		t.Errorf("redo stack = %d entries, want 0 after a new change", len(d.Redo))
	}
	if _, _, err := s.Redo(); err == nil {
		t.Error("redo after a diverging change should error")
	}
	if got, want := titles(t, s, task.Do), []string{"first", "second"}; !equal(got, want) {
		t.Errorf("tasks = %v, want %v", got, want)
	}
}

func TestRedoAcrossFrontendsIsDiscardedToo(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "tasks.json")

	a, _, err := OpenAt(p).Add("mine", task.Do)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := OpenAt(p).Delete(a.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := OpenAt(p).Undo(); err != nil {
		t.Fatal(err)
	}
	// A different "process" (CLI or MCP agent) writes.
	if _, _, err := OpenAt(p).Add("theirs", task.Schedule); err != nil {
		t.Fatal(err)
	}
	if _, _, err := OpenAt(p).Redo(); err == nil {
		t.Error("a write from another frontend should invalidate redo")
	}
}

func TestRedoMultipleSteps(t *testing.T) {
	s := testStore(t)
	s.Add("a", task.Do)
	s.Add("b", task.Do)
	s.Add("c", task.Do)

	for i := 0; i < 3; i++ {
		if _, _, err := s.Undo(); err != nil {
			t.Fatalf("undo %d: %v", i, err)
		}
	}
	if got := titles(t, s, task.Do); len(got) != 0 {
		t.Fatalf("after three undos: %v, want nothing", got)
	}

	// Redo must walk forward the same distance. This is what breaks if Redo
	// clears the redo stack via pushUndo.
	for i := 0; i < 3; i++ {
		if _, _, err := s.Redo(); err != nil {
			t.Fatalf("redo %d: %v", i, err)
		}
	}
	if got, want := titles(t, s, task.Do), []string{"a", "b", "c"}; !equal(got, want) {
		t.Errorf("after three redos: %v, want %v", got, want)
	}
	if _, _, err := s.Redo(); err == nil {
		t.Error("a fourth redo should error")
	}
}

func TestRedoIsUndoable(t *testing.T) {
	s := testStore(t)
	a, _, _ := s.Add("wobble", task.Do)
	s.Delete(a.ID)
	s.Undo() // task is back
	if _, _, err := s.Redo(); err != nil {
		t.Fatal(err)
	}
	if got := titles(t, s, task.Do); len(got) != 0 {
		t.Fatalf("redo should have re-deleted: %v", got)
	}
	if _, _, err := s.Undo(); err != nil {
		t.Fatal(err)
	}
	if got, want := titles(t, s, task.Do), []string{"wobble"}; !equal(got, want) {
		t.Errorf("undo after redo = %v, want %v", got, want)
	}
}

func TestRedoEmptyStack(t *testing.T) {
	s := testStore(t)
	s.Add("a", task.Do)
	if _, _, err := s.Redo(); err == nil {
		t.Error("redo with nothing undone should error")
	}
}

func TestRedoStackIsCapped(t *testing.T) {
	s := testStore(t)
	for i := 0; i < undoDepth+5; i++ {
		if _, _, err := s.Add("t", task.Do); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < undoDepth; i++ {
		if _, _, err := s.Undo(); err != nil {
			t.Fatalf("undo %d: %v", i, err)
		}
	}
	d, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Redo) != undoDepth {
		t.Errorf("redo stack depth = %d, want %d", len(d.Redo), undoDepth)
	}
}

func TestUndoStackIsCapped(t *testing.T) {
	s := testStore(t)
	for i := 0; i < undoDepth+15; i++ {
		if _, _, err := s.Add("t", task.Do); err != nil {
			t.Fatal(err)
		}
	}
	d, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Undo) != undoDepth {
		t.Errorf("undo stack depth = %d, want %d", len(d.Undo), undoDepth)
	}
}

func TestUndoSurvivesReload(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "tasks.json")

	// One "process" makes a change...
	a, _, err := OpenAt(p).Add("cross-process", task.Do)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := OpenAt(p).Complete(a.ID); err != nil {
		t.Fatal(err)
	}
	// ...and another undoes it.
	label, _, err := OpenAt(p).Undo()
	if err != nil {
		t.Fatal(err)
	}
	if label != `complete "cross-process"` {
		t.Errorf("label = %q", label)
	}
	d, _ := OpenAt(p).Load()
	if len(d.Tasks) != 1 {
		t.Errorf("undo across processes left %d active tasks, want 1", len(d.Tasks))
	}
}

func TestFailedMutationDoesNotRecordUndo(t *testing.T) {
	s := testStore(t)
	a, _, _ := s.Add("valid", task.Do)
	if _, _, err := s.Rename(a.ID, "   "); err == nil {
		t.Fatal("rename to blank should fail")
	}
	d, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Undo) != 1 || UndoLabel(d) != `add "valid"` {
		t.Errorf("undo stack = %d entries, top %q; a failed rename should not record",
			len(d.Undo), UndoLabel(d))
	}
}
