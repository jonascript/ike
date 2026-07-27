package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jonascript/ike/internal/task"
)

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

func TestRestoreUnarchives(t *testing.T) {
	s := testStore(t)
	added, err := s.Add("write the thing", task.Delegate)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Complete(added.ID); err != nil {
		t.Fatal(err)
	}

	restored, err := s.Restore(added.ID)
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
	a, _ := s.Add("still active", task.Do)
	if _, err := s.Restore(a.ID); err == nil {
		t.Error("restoring an active (not archived) task should error")
	}
	if _, err := s.Restore(999); err == nil {
		t.Error("restoring an unknown id should error")
	}
}

func TestRestoreGoesToBottom(t *testing.T) {
	s := testStore(t)
	a, _ := s.Add("first", task.Do)
	s.Add("second", task.Do)
	if _, err := s.Complete(a.ID); err != nil {
		t.Fatal(err)
	}
	s.Add("third", task.Do)
	if _, err := s.Restore(a.ID); err != nil {
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
	b, _ := s.Add("b", task.Schedule)
	c, _ := s.Add("c", task.Schedule)

	if _, err := s.Reorder(c.ID, -1); err != nil {
		t.Fatal(err)
	}
	if got, want := titles(t, s, task.Schedule), []string{"a", "c", "b"}; !equal(got, want) {
		t.Fatalf("after c up: %v, want %v", got, want)
	}

	if _, err := s.Reorder(b.ID, ToTop); err != nil {
		t.Fatal(err)
	}
	if got, want := titles(t, s, task.Schedule), []string{"b", "a", "c"}; !equal(got, want) {
		t.Fatalf("after b to top: %v, want %v", got, want)
	}

	if _, err := s.Reorder(b.ID, ToBottom); err != nil {
		t.Fatal(err)
	}
	if got, want := titles(t, s, task.Schedule), []string{"a", "c", "b"}; !equal(got, want) {
		t.Fatalf("after b to bottom: %v, want %v", got, want)
	}
}

func TestReorderClampsAtEnds(t *testing.T) {
	s := testStore(t)
	a, _ := s.Add("a", task.Do)
	s.Add("b", task.Do)

	// Already at the top; moving up is a no-op, not an error.
	if _, err := s.Reorder(a.ID, -1); err != nil {
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
	sched, _ := s.Add("sched-1", task.Schedule)
	s.Add("sched-2", task.Schedule)

	if _, err := s.Reorder(sched.ID, ToBottom); err != nil {
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
	a, _ := s.Add("a", task.Do)
	s.Add("b", task.Do)

	// Ranks are rewritten per quadrant, so swapping repeatedly must not drift.
	for i := 0; i < 200; i++ {
		if _, err := s.Reorder(a.ID, 1); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Reorder(a.ID, -1); err != nil {
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

func TestQuadrantLabelDefaults(t *testing.T) {
	s := testStore(t)
	labels, err := s.QuadrantLabels()
	if err != nil {
		t.Fatal(err)
	}
	want := map[task.Quadrant]string{
		task.Do:        "Do It First",
		task.Schedule:  "Schedule It",
		task.Delegate:  "Delegate It",
		task.Eliminate: "Consider Eliminating It",
	}
	for q, w := range want {
		if got := labels.Of(q); got != w {
			t.Errorf("quadrant %d label = %q, want %q", q, got, w)
		}
		if labels.IsCustom(q) {
			t.Errorf("quadrant %d reported as custom before any rename", q)
		}
	}
}

func TestSetQuadrantLabel(t *testing.T) {
	s := testStore(t)
	got, err := s.SetQuadrantLabel(task.Do, "Firefighting")
	if err != nil {
		t.Fatal(err)
	}
	if got != "Firefighting" {
		t.Errorf("SetQuadrantLabel returned %q", got)
	}

	labels, err := s.QuadrantLabels()
	if err != nil {
		t.Fatal(err)
	}
	if labels.Of(task.Do) != "Firefighting" {
		t.Errorf("label = %q, want Firefighting", labels.Of(task.Do))
	}
	if !labels.IsCustom(task.Do) {
		t.Error("renamed quadrant should report as custom")
	}
	// Renaming one quadrant must not disturb the others.
	if labels.Of(task.Schedule) != "Schedule It" {
		t.Errorf("Schedule label = %q, want the default", labels.Of(task.Schedule))
	}
}

func TestSetQuadrantLabelResetsOnBlank(t *testing.T) {
	s := testStore(t)
	if _, err := s.SetQuadrantLabel(task.Eliminate, "Junk"); err != nil {
		t.Fatal(err)
	}
	got, err := s.SetQuadrantLabel(task.Eliminate, "   ")
	if err != nil {
		t.Fatal(err)
	}
	if got != "Consider Eliminating It" {
		t.Errorf("reset returned %q, want the default", got)
	}
	d, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	// The override is removed, not stored as an empty string.
	if _, ok := d.Labels[task.Eliminate]; ok {
		t.Error("reset should delete the override, not blank it")
	}
}

func TestSetQuadrantLabelValidates(t *testing.T) {
	s := testStore(t)
	if _, err := s.SetQuadrantLabel(9, "Nope"); err == nil {
		t.Error("invalid quadrant should error")
	}
	long := strings.Repeat("x", task.MaxLabelLen+1)
	if _, err := s.SetQuadrantLabel(task.Do, long); err == nil {
		t.Error("over-long label should error")
	}
	labels, _ := s.QuadrantLabels()
	if labels.Of(task.Do) != "Do It First" {
		t.Errorf("failed rename changed the label to %q", labels.Of(task.Do))
	}
}

func TestQuadrantLabelIsUndoable(t *testing.T) {
	s := testStore(t)
	if _, err := s.SetQuadrantLabel(task.Delegate, "Hand Off"); err != nil {
		t.Fatal(err)
	}
	label, err := s.Undo()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(label, "Hand Off") {
		t.Errorf("undo label = %q, want it to name the rename", label)
	}
	labels, _ := s.QuadrantLabels()
	if labels.Of(task.Delegate) != "Delegate It" {
		t.Errorf("after undo, label = %q, want the default back", labels.Of(task.Delegate))
	}

	if _, err := s.Redo(); err != nil {
		t.Fatal(err)
	}
	labels, _ = s.QuadrantLabels()
	if labels.Of(task.Delegate) != "Hand Off" {
		t.Errorf("after redo, label = %q, want Hand Off", labels.Of(task.Delegate))
	}
}

func TestUndoOfTaskChangeLeavesLabelsAlone(t *testing.T) {
	s := testStore(t)
	if _, err := s.SetQuadrantLabel(task.Do, "Firefighting"); err != nil {
		t.Fatal(err)
	}
	a, _ := s.Add("something", task.Do)
	if _, err := s.Delete(a.ID); err != nil {
		t.Fatal(err)
	}
	// Undoing the delete must not roll the rename back with it.
	if _, err := s.Undo(); err != nil {
		t.Fatal(err)
	}
	labels, _ := s.QuadrantLabels()
	if labels.Of(task.Do) != "Firefighting" {
		t.Errorf("label = %q, want the rename to survive an unrelated undo", labels.Of(task.Do))
	}
}

func TestSetSameLabelIsANoOp(t *testing.T) {
	s := testStore(t)
	s.Add("anchor", task.Do)
	if _, err := s.SetQuadrantLabel(task.Do, "Do It First"); err != nil {
		t.Fatal(err)
	}
	d, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if UndoLabel(d) != `add "anchor"` {
		t.Errorf("undo top = %q; setting the label it already has should not record", UndoLabel(d))
	}
}

func TestMCPDisabledByDefault(t *testing.T) {
	s := testStore(t)
	on, err := s.MCPEnabled()
	if err != nil {
		t.Fatal(err)
	}
	if on {
		t.Error("a fresh matrix should have MCP access off")
	}
	// Adding tasks must not switch it on by side effect.
	s.Add("a task", task.Do)
	if on, _ := s.MCPEnabled(); on {
		t.Error("MCP access turned on by an unrelated write")
	}
}

func TestMCPEnabledPersists(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "tasks.json")

	changed, err := OpenAt(p).SetMCPEnabled(true)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Error("enabling from the default should report a change")
	}
	// A separate Store instance, as a later process would see it.
	if on, _ := OpenAt(p).MCPEnabled(); !on {
		t.Error("setting did not survive a reload")
	}

	changed, err = OpenAt(p).SetMCPEnabled(true)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Error("enabling twice should report no change")
	}

	if _, err := OpenAt(p).SetMCPEnabled(false); err != nil {
		t.Fatal(err)
	}
	if on, _ := OpenAt(p).MCPEnabled(); on {
		t.Error("disable did not stick")
	}
}

func TestMCPSettingIsNotUndoable(t *testing.T) {
	s := testStore(t)
	s.Add("a task", task.Do)
	if _, err := s.SetMCPEnabled(true); err != nil {
		t.Fatal(err)
	}

	// Undo must reach past the permission change to the task change, and must
	// not restore the old permission on the way.
	label, err := s.Undo()
	if err != nil {
		t.Fatal(err)
	}
	if label != `add "a task"` {
		t.Errorf("undo label = %q; the MCP setting should not occupy an undo slot", label)
	}
	if on, _ := s.MCPEnabled(); !on {
		t.Error("undo switched MCP access back off")
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
		if _, err := s.Undo(); err != nil {
			break
		}
		if on, _ := s.MCPEnabled(); on {
			t.Fatalf("undo %d re-enabled revoked MCP access", i)
		}
	}
	for i := 0; i < 5; i++ {
		if _, err := s.Redo(); err != nil {
			break
		}
		if on, _ := s.MCPEnabled(); on {
			t.Fatalf("redo %d re-enabled revoked MCP access", i)
		}
	}
}

func TestUndoRestoresPreviousState(t *testing.T) {
	s := testStore(t)
	a, _ := s.Add("keep me", task.Do)

	if _, err := s.Complete(a.ID); err != nil {
		t.Fatal(err)
	}
	label, err := s.Undo()
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
	if _, err := s.Undo(); err != nil {
		t.Fatal(err)
	}
	d, _ = s.Load()
	if len(d.Tasks) != 0 {
		t.Errorf("after second undo: %d active, want 0", len(d.Tasks))
	}
}

func TestUndoOfDeleteBringsTaskBack(t *testing.T) {
	s := testStore(t)
	a, _ := s.Add("oops", task.Do)
	if _, err := s.Delete(a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Undo(); err != nil {
		t.Fatal(err)
	}
	got := titles(t, s, task.Do)
	if !equal(got, []string{"oops"}) {
		t.Errorf("after undoing delete: %v, want [oops]", got)
	}
}

func TestUndoDoesNotReuseIDs(t *testing.T) {
	s := testStore(t)
	a, _ := s.Add("first", task.Do)
	if _, err := s.Undo(); err != nil { // undo the add
		t.Fatal(err)
	}
	b, err := s.Add("second", task.Do)
	if err != nil {
		t.Fatal(err)
	}
	if b.ID == a.ID {
		t.Errorf("ID %d was reused after undo; IDs must stay monotonic", b.ID)
	}
}

func TestUndoEmptyStack(t *testing.T) {
	s := testStore(t)
	if _, err := s.Undo(); err == nil {
		t.Error("undo with nothing to undo should error")
	}
}

func TestRepeatedUndoWalksBack(t *testing.T) {
	s := testStore(t)
	s.Add("a", task.Do)
	s.Add("b", task.Do)
	if _, err := s.Undo(); err != nil { // undoes add "b"
		t.Fatal(err)
	}
	label, err := s.Undo() // must undo add "a", not re-apply add "b"
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
	a, _ := s.Add("ship it", task.Do)
	if _, err := s.Complete(a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Undo(); err != nil {
		t.Fatal(err)
	}
	if arch, _ := s.ListArchive(); len(arch) != 0 {
		t.Fatal("undo should have un-archived the task")
	}

	label, err := s.Redo()
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
	b, _ := s.Add("b", task.Do)
	s.Add("c", task.Do)
	if _, err := s.Reorder(b.ID, ToTop); err != nil {
		t.Fatal(err)
	}
	want := titles(t, s, task.Do)

	if _, err := s.Undo(); err != nil {
		t.Fatal(err)
	}
	if got := titles(t, s, task.Do); equal(got, want) {
		t.Fatal("undo did not change the order")
	}
	if _, err := s.Redo(); err != nil {
		t.Fatal(err)
	}
	if got := titles(t, s, task.Do); !equal(got, want) {
		t.Errorf("after undo+redo: %v, want %v (ordering must survive the round trip)", got, want)
	}
}

func TestNewChangeDiscardsRedo(t *testing.T) {
	s := testStore(t)
	a, _ := s.Add("first", task.Do)
	if _, err := s.Complete(a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Undo(); err != nil {
		t.Fatal(err)
	}

	// Diverging from the undone branch must strand it: redoing the complete
	// here would clobber the task added below.
	if _, err := s.Add("second", task.Do); err != nil {
		t.Fatal(err)
	}
	d, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Redo) != 0 {
		t.Errorf("redo stack = %d entries, want 0 after a new change", len(d.Redo))
	}
	if _, err := s.Redo(); err == nil {
		t.Error("redo after a diverging change should error")
	}
	if got, want := titles(t, s, task.Do), []string{"first", "second"}; !equal(got, want) {
		t.Errorf("tasks = %v, want %v", got, want)
	}
}

func TestRedoAcrossFrontendsIsDiscardedToo(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "tasks.json")

	a, err := OpenAt(p).Add("mine", task.Do)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := OpenAt(p).Delete(a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenAt(p).Undo(); err != nil {
		t.Fatal(err)
	}
	// A different "process" (CLI or MCP agent) writes.
	if _, err := OpenAt(p).Add("theirs", task.Schedule); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenAt(p).Redo(); err == nil {
		t.Error("a write from another frontend should invalidate redo")
	}
}

func TestRedoMultipleSteps(t *testing.T) {
	s := testStore(t)
	s.Add("a", task.Do)
	s.Add("b", task.Do)
	s.Add("c", task.Do)

	for i := 0; i < 3; i++ {
		if _, err := s.Undo(); err != nil {
			t.Fatalf("undo %d: %v", i, err)
		}
	}
	if got := titles(t, s, task.Do); len(got) != 0 {
		t.Fatalf("after three undos: %v, want nothing", got)
	}

	// Redo must walk forward the same distance. This is what breaks if Redo
	// clears the redo stack via pushUndo.
	for i := 0; i < 3; i++ {
		if _, err := s.Redo(); err != nil {
			t.Fatalf("redo %d: %v", i, err)
		}
	}
	if got, want := titles(t, s, task.Do), []string{"a", "b", "c"}; !equal(got, want) {
		t.Errorf("after three redos: %v, want %v", got, want)
	}
	if _, err := s.Redo(); err == nil {
		t.Error("a fourth redo should error")
	}
}

func TestRedoIsUndoable(t *testing.T) {
	s := testStore(t)
	a, _ := s.Add("wobble", task.Do)
	s.Delete(a.ID)
	s.Undo() // task is back
	if _, err := s.Redo(); err != nil {
		t.Fatal(err)
	}
	if got := titles(t, s, task.Do); len(got) != 0 {
		t.Fatalf("redo should have re-deleted: %v", got)
	}
	if _, err := s.Undo(); err != nil {
		t.Fatal(err)
	}
	if got, want := titles(t, s, task.Do), []string{"wobble"}; !equal(got, want) {
		t.Errorf("undo after redo = %v, want %v", got, want)
	}
}

func TestRedoEmptyStack(t *testing.T) {
	s := testStore(t)
	s.Add("a", task.Do)
	if _, err := s.Redo(); err == nil {
		t.Error("redo with nothing undone should error")
	}
}

func TestRedoStackIsCapped(t *testing.T) {
	s := testStore(t)
	for i := 0; i < undoDepth+5; i++ {
		if _, err := s.Add("t", task.Do); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < undoDepth; i++ {
		if _, err := s.Undo(); err != nil {
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
		if _, err := s.Add("t", task.Do); err != nil {
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
	a, err := OpenAt(p).Add("cross-process", task.Do)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := OpenAt(p).Complete(a.ID); err != nil {
		t.Fatal(err)
	}
	// ...and another undoes it.
	label, err := OpenAt(p).Undo()
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
	a, _ := s.Add("valid", task.Do)
	if _, err := s.Rename(a.ID, "   "); err == nil {
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
	if _, err := s.Reorder(3, ToTop); err != nil {
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
func TestLabelsOfSanitizesUntrustedFile(t *testing.T) {
	l := Labels{task.Do: "red\x1b[31m", task.Schedule: "two\nlines"}

	got := l.Of(task.Do)
	if strings.ContainsRune(got, 0x1b) {
		t.Errorf("Of(Do) = %q, still contains an escape", got)
	}
	if !strings.Contains(got, "red") {
		t.Errorf("Of(Do) = %q, dropped visible text", got)
	}
	if got := l.Of(task.Schedule); strings.ContainsRune(got, '\n') {
		t.Errorf("Of(Schedule) = %q, still contains a newline", got)
	}
	// An unrenamed quadrant still falls back to its default.
	if got, want := l.Of(task.Delegate), task.Delegate.Label(); got != want {
		t.Errorf("Of(Delegate) = %q, want the default %q", got, want)
	}
	// The stored label itself is untouched; only the rendered form changes.
	if l[task.Do] != "red\x1b[31m" {
		t.Error("Of must not rewrite the stored label")
	}
}

// The gate used to be checked once, before serving. A client that stayed
// connected therefore kept full access after `ike mcp disable`, while
// `ike mcp status` reported "off". These pin the data-layer check that closed
// that window.
func TestMCPGateRevokesLiveSession(t *testing.T) {
	s := testStore(t)
	if _, err := s.SetMCPEnabled(true); err != nil {
		t.Fatal(err)
	}

	// The session a connected client holds.
	live := s.ForMCP()
	if _, err := live.Add("while enabled", task.Do); err != nil {
		t.Fatalf("Add with access on: %v", err)
	}
	if _, err := live.Load(); err != nil {
		t.Fatalf("Load with access on: %v", err)
	}

	// Revoked from another process, on the same file, mid-session.
	if _, err := s.SetMCPEnabled(false); err != nil {
		t.Fatal(err)
	}

	if _, err := live.Add("after revoke", task.Do); !errors.Is(err, ErrMCPDisabled) {
		t.Errorf("Add after revoke: err = %v, want ErrMCPDisabled", err)
	}
	if _, err := live.Load(); !errors.Is(err, ErrMCPDisabled) {
		t.Errorf("Load after revoke: err = %v, want ErrMCPDisabled", err)
	}
	if _, err := live.Complete(1); !errors.Is(err, ErrMCPDisabled) {
		t.Errorf("Complete after revoke: err = %v, want ErrMCPDisabled", err)
	}
	if _, err := live.Delete(1); !errors.Is(err, ErrMCPDisabled) {
		t.Errorf("Delete after revoke: err = %v, want ErrMCPDisabled", err)
	}
	if _, err := live.Undo(); !errors.Is(err, ErrMCPDisabled) {
		t.Errorf("Undo after revoke: err = %v, want ErrMCPDisabled", err)
	}

	// The revoked mutations must not have landed, and the owner's own access is
	// unaffected — that is what `ike mcp status` and the TUI keep using.
	d, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Tasks) != 1 || d.Tasks[0].Title != "while enabled" {
		t.Errorf("tasks = %+v, want only the pre-revoke task", d.Tasks)
	}

	// Re-enabling restores the same session, without reconnecting.
	if _, err := s.SetMCPEnabled(true); err != nil {
		t.Fatal(err)
	}
	if _, err := live.Load(); err != nil {
		t.Errorf("Load after re-enable: %v", err)
	}
}

// A store that was never marked for MCP use is unaffected by the setting, so
// the TUI and CLI keep working while agent access is off.
func TestGateDoesNotAffectOwnerAccess(t *testing.T) {
	s := testStore(t)
	if enabled, err := s.MCPEnabled(); err != nil || enabled {
		t.Fatalf("MCPEnabled() = %v, %v; want false, nil", enabled, err)
	}
	if _, err := s.Add("owner task", task.Do); err != nil {
		t.Errorf("owner Add with MCP off: %v", err)
	}
	if _, err := s.Load(); err != nil {
		t.Errorf("owner Load with MCP off: %v", err)
	}
}

func TestMCPErrorsOmitDataFilePath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.json")
	if err := os.WriteFile(path, []byte("{ not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	gated := OpenAt(path).ForMCP()

	_, err := gated.Load()
	if err == nil {
		t.Fatal("expected a parse error")
	}
	if strings.Contains(err.Error(), dir) {
		t.Errorf("error discloses the data file's directory to the agent: %v", err)
	}
	// The file name still appears, so the message stays actionable.
	if !strings.Contains(err.Error(), "tasks.json") {
		t.Errorf("error should still name the file: %v", err)
	}

	// The owner's view keeps the full path, which is what makes a CLI error
	// useful.
	if _, err := OpenAt(path).Load(); err == nil || !strings.Contains(err.Error(), dir) {
		t.Errorf("ungated error should keep the full path: %v", err)
	}
}
