package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jonascript/ike/internal/task"
)

const samplePlan = "## Goal\n\nShip it.\n\n- [ ] step one\n- [ ] step two\n"

func TestSetPlanRoundTrips(t *testing.T) {
	s := testStore(t)
	a, _, _ := s.Add("ship v2", task.Do)

	got, d, err := s.SetPlan(a.ID, samplePlan)
	if err != nil {
		t.Fatal(err)
	}
	if !got.HasPlan() {
		t.Error("the returned task should carry a PlanAt stamp")
	}
	// The post-mutation Data is what a frontend renders from, so the stamp has
	// to be visible there too rather than only on the returned task.
	if ts := d.List(task.Do); len(ts) != 1 || !ts[0].HasPlan() {
		t.Error("the returned Data should show the task as planned")
	}

	body, err := s.Plan(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if body != samplePlan {
		t.Errorf("Plan() = %q, want %q", body, samplePlan)
	}
}

// The whole reason plans are sidecar files is that Snapshot copies the task
// list wholesale, so a plan body on the Task would be cloned into every
// snapshot. This is the check that no plan text reached the data file.
func TestPlanBodyStaysOutOfTheDataFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "tasks.json")
	s := OpenAt(p)

	a, _, _ := s.Add("ship v2", task.Do)
	if _, _, err := s.SetPlan(a.ID, samplePlan); err != nil {
		t.Fatal(err)
	}
	// Enough mutations to push snapshots onto the history stack.
	for i := 0; i < 5; i++ {
		s.Add("filler", task.Schedule)
	}

	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "Ship it.") {
		t.Error("the plan body reached tasks.json; it belongs in the sidecar, " +
			"or every undo snapshot carries a copy of it")
	}
	// The stamp, by contrast, must be there — it is what marks the task planned.
	if !strings.Contains(string(b), "plan_at") {
		t.Error("the PlanAt stamp should persist in the data file")
	}
}

func TestPlanFileIsPrivate(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "tasks.json")
	s := OpenAt(p)
	a, _, _ := s.Add("ship v2", task.Do)
	if _, _, err := s.SetPlan(a.ID, samplePlan); err != nil {
		t.Fatal(err)
	}

	// A plan is as personal as the matrix, and is written through the same
	// atomic path, so it inherits the same modes.
	fi, err := os.Stat(filepath.Join(p+planDirSuffix, "default", "1.md"))
	if err != nil {
		t.Fatal(err)
	}
	if got := fi.Mode().Perm(); got != dataFileMode {
		t.Errorf("plan file mode = %#o, want %#o", got, dataFileMode)
	}
	di, err := os.Stat(filepath.Join(p+planDirSuffix, "default"))
	if err != nil {
		t.Fatal(err)
	}
	if got := di.Mode().Perm(); got != dataDirMode {
		t.Errorf("plan dir mode = %#o, want %#o", got, dataDirMode)
	}
}

func TestSetPlanReplacesAndClears(t *testing.T) {
	s := testStore(t)
	a, _, _ := s.Add("ship v2", task.Do)

	s.SetPlan(a.ID, "first")
	s.SetPlan(a.ID, "second")
	if body, _ := s.Plan(a.ID); body != "second" {
		t.Errorf("Plan() = %q, want the replacement", body)
	}

	// A blank body clears, the way a blank quadrant label resets to the default.
	got, _, err := s.SetPlan(a.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if got.HasPlan() {
		t.Error("a blank plan should clear the stamp")
	}
	if body, _ := s.Plan(a.ID); body != "" {
		t.Errorf("Plan() = %q after clearing, want empty", body)
	}
}

func TestSetPlanRejectsBadInput(t *testing.T) {
	s := testStore(t)
	a, _, _ := s.Add("ship v2", task.Do)

	if _, _, err := s.SetPlan(a.ID, "plan\x1b[2Kwith an escape"); err == nil {
		t.Error("a plan carrying an escape sequence should be refused")
	}
	if _, _, err := s.SetPlan(a.ID, strings.Repeat("x", task.MaxPlanLen+1)); err == nil {
		t.Error("an over-long plan should be refused")
	}
	if _, _, err := s.SetPlan(999, samplePlan); err == nil {
		t.Error("a plan for a task that does not exist should be refused")
	}
	// A refused plan leaves nothing behind.
	if body, _ := s.Plan(a.ID); body != "" {
		t.Errorf("a rejected plan was written anyway: %q", body)
	}
}

// A plan is a property of a task, so an unrelated mutation being undone must
// not take it with it.
func TestPlanSurvivesUnrelatedUndo(t *testing.T) {
	s := testStore(t)
	a, _, _ := s.Add("ship v2", task.Do)
	s.SetPlan(a.ID, samplePlan)

	b, _, _ := s.Add("something else", task.Schedule)
	if _, _, err := s.Complete(b.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Undo(); err != nil {
		t.Fatal(err)
	}

	body, err := s.Plan(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if body != samplePlan {
		t.Errorf("Plan() = %q after an unrelated undo, want it intact", body)
	}
	d, _ := s.Load()
	if ts := d.List(task.Do); len(ts) != 1 || !ts[0].HasPlan() {
		t.Error("the PlanAt stamp was lost to an unrelated undo")
	}
}

// Attaching a plan is itself undoable, since it goes through pushUndo.
func TestUndoRemovesThePlanStamp(t *testing.T) {
	s := testStore(t)
	a, _, _ := s.Add("ship v2", task.Do)
	s.SetPlan(a.ID, samplePlan)

	if _, _, err := s.Undo(); err != nil {
		t.Fatal(err)
	}
	d, _ := s.Load()
	if ts := d.List(task.Do); len(ts) != 1 || ts[0].HasPlan() {
		t.Error("undoing the plan should clear the stamp")
	}
}

// Clearing a plan on a task that has none is a no-op, and must not push a
// snapshot — `ike undo` should describe changes that happened.
func TestClearPlanWithNoPlanRecordsNothing(t *testing.T) {
	s := testStore(t)
	a, _, _ := s.Add("ship v2", task.Do)

	before, _ := s.Load()
	if _, _, err := s.ClearPlan(a.ID); err != nil {
		t.Fatal(err)
	}
	after, _ := s.Load()
	if len(after.Undo) != len(before.Undo) {
		t.Errorf("undo depth went %d → %d; a no-op clear recorded history",
			len(before.Undo), len(after.Undo))
	}
}

// Plans are keyed by space, because task IDs are per space: task 1 in `work`
// and task 1 in `personal` are different tasks.
func TestPlansAreScopedToTheirSpace(t *testing.T) {
	s := testStore(t)
	if _, err := s.NewSpace("work"); err != nil {
		t.Fatal(err)
	}

	def := s.InSpace("default")
	work := s.InSpace("work")
	a, _, _ := def.Add("default task", task.Do)
	b, _, _ := work.Add("work task", task.Do)
	if a.ID != b.ID {
		t.Fatalf("expected both spaces to number from the same start, got %d and %d", a.ID, b.ID)
	}

	def.SetPlan(a.ID, "the default plan")
	work.SetPlan(b.ID, "the work plan")

	if got, _ := def.Plan(a.ID); got != "the default plan" {
		t.Errorf("default space Plan() = %q", got)
	}
	if got, _ := work.Plan(b.ID); got != "the work plan" {
		t.Errorf("work space Plan() = %q", got)
	}
}

// A stamp with no file behind it is reachable from a restored backup or a copy
// that left the sidecar directory behind. Reading the matrix must still work.
func TestPlanToleratesAMissingFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "tasks.json")
	s := OpenAt(p)
	a, _, _ := s.Add("ship v2", task.Do)
	s.SetPlan(a.ID, samplePlan)

	if err := os.RemoveAll(p + planDirSuffix); err != nil {
		t.Fatal(err)
	}
	body, err := s.Plan(a.ID)
	if err != nil {
		t.Fatalf("a missing plan file should read as empty, got %v", err)
	}
	if body != "" {
		t.Errorf("Plan() = %q, want empty", body)
	}
}

// The plan file is plain text on disk, so it can carry anything a hand edit or
// a synced copy put there. It is sanitized on the way out for the same reason
// DisplayTitle sanitizes a title.
func TestPlanSanitizesOnRead(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "tasks.json")
	s := OpenAt(p)
	a, _, _ := s.Add("ship v2", task.Do)
	s.SetPlan(a.ID, "placeholder")

	raw := "real\x1b[2K\rFAKE\n\nsecond line\n"
	if err := os.WriteFile(filepath.Join(p+planDirSuffix, "default", "1.md"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}

	body, err := s.Plan(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsRune(body, 0x1b) || strings.ContainsRune(body, '\r') {
		t.Errorf("Plan() = %q, still carries control characters", body)
	}
	// Structure survives; only the control bytes are replaced.
	if !strings.Contains(body, "\n\nsecond line") {
		t.Errorf("Plan() = %q, lost its line structure", body)
	}
}

func TestSetDir(t *testing.T) {
	s := testStore(t)
	a, _, _ := s.Add("ship v2", task.Do)
	dir := t.TempDir()

	got, _, err := s.SetDir(a.ID, "--dir", dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Dir != dir {
		t.Errorf("Dir = %q, want %q", got.Dir, dir)
	}

	d, _ := s.Load()
	if ts := d.List(task.Do); len(ts) != 1 || ts[0].Dir != dir {
		t.Error("the directory did not persist")
	}

	// Blank clears.
	if got, _, err := s.SetDir(a.ID, "--dir", ""); err != nil || got.Dir != "" {
		t.Errorf("clearing gave Dir=%q err=%v", got.Dir, err)
	}
}

func TestSetDirRejectsBadPaths(t *testing.T) {
	s := testStore(t)
	a, _, _ := s.Add("ship v2", task.Do)
	file := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		dir  string
	}{
		{"relative", "some/where"},
		{"unexpanded tilde", "~/dev/ike"},
		{"does not exist", filepath.Join(t.TempDir(), "nope")},
		{"not a directory", file},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, _, err := s.SetDir(a.ID, "--dir", c.dir); err == nil {
				t.Errorf("SetDir(%q) should have been refused", c.dir)
			}
		})
	}
}

// Deleting a task deliberately leaves its plan, so undo can bring both back.
// PrunePlans is the explicit sweep for the orphans that leaves behind.
func TestDeleteKeepsThePlanAndPruneRemovesIt(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "tasks.json")
	s := OpenAt(p)
	a, _, _ := s.Add("ship v2", task.Do)
	s.SetPlan(a.ID, samplePlan)

	if _, _, err := s.Delete(a.ID); err != nil {
		t.Fatal(err)
	}
	planFile := filepath.Join(p+planDirSuffix, "default", "1.md")
	if _, err := os.Stat(planFile); err != nil {
		t.Fatalf("delete removed the plan file; undo would bring the task back empty: %v", err)
	}
	// And undo does bring it back, plan and all.
	if _, _, err := s.Undo(); err != nil {
		t.Fatal(err)
	}
	if body, _ := s.Plan(a.ID); body != samplePlan {
		t.Errorf("after undoing the delete, Plan() = %q", body)
	}

	// Now really delete it, and sweep.
	s.Delete(a.ID)
	n, err := s.PrunePlans()
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("PrunePlans() removed %d, want 1", n)
	}
	if _, err := os.Stat(planFile); !os.IsNotExist(err) {
		t.Error("PrunePlans left the orphan behind")
	}
}

// An archived task still owns its plan: Restore brings it back active.
func TestPruneKeepsArchivedTasksPlans(t *testing.T) {
	s := testStore(t)
	a, _, _ := s.Add("ship v2", task.Do)
	s.SetPlan(a.ID, samplePlan)
	if _, _, err := s.Complete(a.ID); err != nil {
		t.Fatal(err)
	}

	n, err := s.PrunePlans()
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("PrunePlans() removed %d; an archived task keeps its plan", n)
	}
}

func TestPlanFileID(t *testing.T) {
	cases := []struct {
		name string
		want int
		ok   bool
	}{
		{"1.md", 1, true},
		{"42.md", 42, true},
		{"0.md", 0, false},
		{"-1.md", 0, false},
		{"notanumber.md", 0, false},
		{"1.txt", 0, false},
		{"1", 0, false},
		{"README", 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := planFileID(c.name)
			if got != c.want || ok != c.ok {
				t.Errorf("planFileID(%q) = %d, %v; want %d, %v", c.name, got, ok, c.want, c.ok)
			}
		})
	}
}
