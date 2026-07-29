package store

import (
	"strings"
	"testing"

	"github.com/jonascript/ike/internal/task"
)

// titles returns the titles of q's tasks in display order.

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
	got, _, err := s.SetQuadrantLabel(task.Do, "Firefighting")
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
	if _, _, err := s.SetQuadrantLabel(task.Eliminate, "Junk"); err != nil {
		t.Fatal(err)
	}
	got, _, err := s.SetQuadrantLabel(task.Eliminate, "   ")
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
	if _, _, err := s.SetQuadrantLabel(9, "Nope"); err == nil {
		t.Error("invalid quadrant should error")
	}
	long := strings.Repeat("x", task.MaxLabelLen+1)
	if _, _, err := s.SetQuadrantLabel(task.Do, long); err == nil {
		t.Error("over-long label should error")
	}
	labels, _ := s.QuadrantLabels()
	if labels.Of(task.Do) != "Do It First" {
		t.Errorf("failed rename changed the label to %q", labels.Of(task.Do))
	}
}

func TestQuadrantLabelIsUndoable(t *testing.T) {
	s := testStore(t)
	if _, _, err := s.SetQuadrantLabel(task.Delegate, "Hand Off"); err != nil {
		t.Fatal(err)
	}
	label, _, err := s.Undo()
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

	if _, _, err := s.Redo(); err != nil {
		t.Fatal(err)
	}
	labels, _ = s.QuadrantLabels()
	if labels.Of(task.Delegate) != "Hand Off" {
		t.Errorf("after redo, label = %q, want Hand Off", labels.Of(task.Delegate))
	}
}

func TestSetSameLabelIsANoOp(t *testing.T) {
	s := testStore(t)
	s.Add("anchor", task.Do)
	if _, _, err := s.SetQuadrantLabel(task.Do, "Do It First"); err != nil {
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
