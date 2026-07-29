package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/jonascript/ike/internal/store"
	"github.com/jonascript/ike/internal/task"
)

func testModel(t *testing.T) (Model, *store.Store) {
	t.Helper()
	s := store.OpenAt(filepath.Join(t.TempDir(), "tasks.json"))
	m, err := New(s)
	if err != nil {
		t.Fatal(err)
	}
	m.width, m.height = 80, 24
	return m, s
}

func press(t *testing.T, m Model, keys ...string) Model {
	t.Helper()
	for _, k := range keys {
		msg := keyFor(k)
		next, _ := m.Update(msg)
		m = next.(Model)
	}
	return m
}

func keyFor(k string) tea.KeyPressMsg {
	switch k {
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "shift+tab":
		return tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	default:
		r := []rune(k)[0]
		return tea.KeyPressMsg{Code: r, Text: k}
	}
}

func typeText(t *testing.T, m Model, text string) Model {
	t.Helper()
	for _, r := range text {
		next, _ := m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		m = next.(Model)
	}
	return m
}

func TestQuadrantNavigation(t *testing.T) {
	m, _ := testModel(t)
	if m.focus != task.Do {
		t.Fatalf("initial focus = %v, want Do", m.focus)
	}
	m = press(t, m, "3")
	if m.focus != task.Delegate {
		t.Errorf("focus after '3' = %v, want Delegate", m.focus)
	}
	m = press(t, m, "tab", "tab")
	if m.focus != task.Do {
		t.Errorf("focus after tab tab from Delegate = %v, want Do (wraps)", m.focus)
	}
	m = press(t, m, "shift+tab")
	if m.focus != task.Eliminate {
		t.Errorf("focus after shift+tab from Do = %v, want Eliminate", m.focus)
	}
}

func TestAddTaskFlow(t *testing.T) {
	m, s := testModel(t)
	m = press(t, m, "2", "a")
	if m.mode != modeInput {
		t.Fatalf("mode after 'a' = %v, want modeInput", m.mode)
	}
	m = typeText(t, m, "plan roadmap")
	m = press(t, m, "enter")
	if m.mode != modeNormal {
		t.Fatalf("mode after enter = %v, want modeNormal", m.mode)
	}

	tasks, _ := s.List(task.Schedule)
	if len(tasks) != 1 || tasks[0].Title != "plan roadmap" {
		t.Errorf("store tasks = %+v, want the added task in Schedule", tasks)
	}
	if got := m.data.List(task.Schedule); len(got) != 1 {
		t.Errorf("model shows %d tasks in Schedule, want 1", len(got))
	}
}

func TestAddCancelled(t *testing.T) {
	m, s := testModel(t)
	m = press(t, m, "a")
	m = typeText(t, m, "oops")
	m = press(t, m, "esc")
	if m.mode != modeNormal {
		t.Fatalf("mode after esc = %v, want modeNormal", m.mode)
	}
	tasks, _ := s.List(0)
	if len(tasks) != 0 {
		t.Errorf("canceled add still wrote %d tasks", len(tasks))
	}
}

func TestEditTask(t *testing.T) {
	m, s := testModel(t)
	s.Add("old title", task.Do)
	m.refreshFromStore(t)
	m = press(t, m, "1", "e")
	if m.mode != modeInput || m.editingID == 0 {
		t.Fatalf("edit mode not entered: mode=%v editing=%d", m.mode, m.editingID)
	}
	if m.input.Value() != "old title" {
		t.Errorf("input prefilled with %q, want old title", m.input.Value())
	}
	m.input.SetValue("new title")
	m = press(t, m, "enter")
	tasks, _ := s.List(0)
	if len(tasks) != 1 || tasks[0].Title != "new title" {
		t.Errorf("tasks after edit = %+v", tasks)
	}
}

func TestRenameQuadrant(t *testing.T) {
	m, s := testModel(t)
	m = press(t, m, "3", "t")
	if m.mode != modeInput || m.labelTarget != task.Delegate {
		t.Fatalf("rename mode not entered: mode=%v target=%v", m.mode, m.labelTarget)
	}
	// The input is prefilled with the current name, so it can be edited.
	if m.input.Value() != "Delegate It" {
		t.Errorf("input prefilled with %q, want the current name", m.input.Value())
	}

	m.input.SetValue("Hand Off")
	m = press(t, m, "enter")
	if m.mode != modeNormal || m.labelTarget != 0 {
		t.Errorf("rename did not exit cleanly: mode=%v target=%v", m.mode, m.labelTarget)
	}

	labels, err := s.QuadrantLabels()
	if err != nil {
		t.Fatal(err)
	}
	if labels.Of(task.Delegate) != "Hand Off" {
		t.Errorf("stored label = %q, want Hand Off", labels.Of(task.Delegate))
	}
	if out := m.render(); !strings.Contains(out, "Hand Off") {
		t.Errorf("matrix does not show the new name:\n%s", out)
	}
}

func TestRenameQuadrantToBlankRestoresDefault(t *testing.T) {
	m, s := testModel(t)
	if _, _, err := s.SetQuadrantLabel(task.Do, "Firefighting"); err != nil {
		t.Fatal(err)
	}
	m.refreshFromStore(t)

	m = press(t, m, "1", "t")
	m.input.SetValue("")
	m = press(t, m, "enter")

	labels, _ := s.QuadrantLabels()
	if labels.Of(task.Do) != "Do It First" {
		t.Errorf("label = %q, want the default restored", labels.Of(task.Do))
	}
}

func TestRenameQuadrantCancelled(t *testing.T) {
	m, s := testModel(t)
	m = press(t, m, "2", "t")
	m.input.SetValue("Nope")
	m = press(t, m, "esc")
	if m.mode != modeNormal || m.labelTarget != 0 {
		t.Errorf("esc did not leave rename mode: mode=%v target=%v", m.mode, m.labelTarget)
	}
	labels, _ := s.QuadrantLabels()
	if labels.Of(task.Schedule) != "Schedule It" {
		t.Errorf("canceled rename still wrote %q", labels.Of(task.Schedule))
	}
}

func TestRenameQuadrantDoesNotAddATask(t *testing.T) {
	m, s := testModel(t)
	m = press(t, m, "1", "t")
	m.input.SetValue("Firefighting")
	m = press(t, m, "enter")

	tasks, _ := s.List(0)
	if len(tasks) != 0 {
		t.Errorf("renaming a quadrant created %d tasks", len(tasks))
	}
}

func TestAddStillWorksAfterRenaming(t *testing.T) {
	m, s := testModel(t)
	m = press(t, m, "1", "t")
	m.input.SetValue("Firefighting")
	m = press(t, m, "enter")

	// labelTarget must be cleared, or the next `a` would rename instead of add.
	m = press(t, m, "a")
	m = typeText(t, m, "a real task")
	m = press(t, m, "enter")

	tasks, _ := s.List(task.Do)
	if len(tasks) != 1 || tasks[0].Title != "a real task" {
		t.Errorf("tasks = %v, want the added task", titlesOf(tasks))
	}
	labels, _ := s.QuadrantLabels()
	if labels.Of(task.Do) != "Firefighting" {
		t.Errorf("label = %q, want it unchanged by the add", labels.Of(task.Do))
	}
}

func TestQuadrantHeadersAreJustNames(t *testing.T) {
	m, s := testModel(t)
	if _, _, err := s.SetQuadrantLabel(task.Do, "Firefighting"); err != nil {
		t.Fatal(err)
	}
	m.refreshFromStore(t)
	out := m.render()

	if !strings.Contains(out, "1 · Firefighting") {
		t.Errorf("header missing the quadrant name:\n%s", out)
	}
	// The old descriptive nicknames are gone from the interface entirely.
	for _, gone := range []string{"Emergencies", "Planning", "Interruptions", "Time-wasters"} {
		if strings.Contains(out, gone) {
			t.Errorf("render still shows the %q description", gone)
		}
	}
}

func TestCompleteTask(t *testing.T) {
	m, s := testModel(t)
	s.Add("finish me", task.Do)
	m = press(t, m, "1")
	m.refreshFromStore(t)
	m = press(t, m, "x")

	active, _ := s.List(0)
	arch, _ := s.ListArchive()
	if len(active) != 0 || len(arch) != 1 {
		t.Errorf("after complete: active=%d archive=%d, want 0/1", len(active), len(arch))
	}
}

func TestMoveTask(t *testing.T) {
	m, s := testModel(t)
	s.Add("shift me", task.Do)
	m.refreshFromStore(t)
	m = press(t, m, "1", "m")
	if m.mode != modeMove {
		t.Fatalf("mode after 'm' = %v, want modeMove", m.mode)
	}
	m = press(t, m, "4")
	tasks, _ := s.List(task.Eliminate)
	if len(tasks) != 1 {
		t.Errorf("task not moved to Eliminate")
	}
	if m.mode != modeNormal {
		t.Errorf("mode after move = %v, want modeNormal", m.mode)
	}
}

func TestDeleteNeedsConfirmation(t *testing.T) {
	m, s := testModel(t)
	s.Add("doomed", task.Do)
	m.refreshFromStore(t)
	m = press(t, m, "1", "d")

	tasks, _ := s.List(0)
	if len(tasks) != 1 {
		t.Fatal("single 'd' should not delete")
	}
	// An unrelated key clears the pending delete.
	m = press(t, m, "j", "d")
	tasks, _ = s.List(0)
	if len(tasks) != 1 {
		t.Fatal("'d' after cancel should not delete")
	}
	m = press(t, m, "d")
	tasks, _ = s.List(0)
	if len(tasks) != 0 {
		t.Error("second consecutive 'd' should delete")
	}
	arch, _ := s.ListArchive()
	if len(arch) != 0 {
		t.Error("delete must not archive")
	}
}

func TestArchiveView(t *testing.T) {
	m, s := testModel(t)
	s.Add("done thing", task.Do)
	s.Complete(1)
	m.refreshFromStore(t)

	m = press(t, m, "v")
	if m.mode != modeArchive {
		t.Fatalf("mode after 'v' = %v, want modeArchive", m.mode)
	}
	out := m.render()
	if !strings.Contains(out, "done thing") {
		t.Errorf("archive view missing task title:\n%s", out)
	}
	m = press(t, m, "esc")
	if m.mode != modeNormal {
		t.Errorf("esc should leave archive view")
	}
}

func TestReorderKeysMoveTaskAndCursor(t *testing.T) {
	m, s := testModel(t)
	s.Add("a", task.Do)
	s.Add("b", task.Do)
	s.Add("c", task.Do)
	m.refreshFromStore(t)

	// Select "c" (index 2), then move it to the top with two K presses.
	m = press(t, m, "1", "j", "j")
	if sel, _ := m.selected(); sel.Title != "c" {
		t.Fatalf("selected %q, want c", sel.Title)
	}
	m = press(t, m, "K", "K")

	ts, _ := s.List(task.Do)
	if len(ts) != 3 || ts[0].Title != "c" {
		t.Fatalf("store order = %v, want c first", titlesOf(ts))
	}
	// The cursor follows the task it moved.
	if sel, _ := m.selected(); sel.Title != "c" {
		t.Errorf("selection after reorder = %q, want c", sel.Title)
	}
	if m.cursor[task.Do] != 0 {
		t.Errorf("cursor = %d, want 0", m.cursor[task.Do])
	}

	// And back down to the bottom.
	m = press(t, m, "J", "J")
	ts, _ = s.List(task.Do)
	if ts[2].Title != "c" {
		t.Errorf("store order = %v, want c last", titlesOf(ts))
	}
}

func TestUndoKey(t *testing.T) {
	m, s := testModel(t)
	s.Add("finish me", task.Do)
	m.refreshFromStore(t)
	m = press(t, m, "1", "x")
	if arch, _ := s.ListArchive(); len(arch) != 1 {
		t.Fatal("task should be archived before undo")
	}

	m = press(t, m, "u")
	active, _ := s.List(0)
	arch, _ := s.ListArchive()
	if len(active) != 1 || len(arch) != 0 {
		t.Errorf("after undo: active=%d archive=%d, want 1/0", len(active), len(arch))
	}
	if !strings.Contains(m.status, "undid") {
		t.Errorf("status = %q, want it to mention the undo", m.status)
	}
	if len(m.data.List(task.Do)) != 1 {
		t.Error("model did not re-render the restored task")
	}
}

func TestRedoKey(t *testing.T) {
	m, s := testModel(t)
	s.Add("finish me", task.Do)
	m.refreshFromStore(t)
	m = press(t, m, "1", "x", "u")
	if active, _ := s.List(0); len(active) != 1 {
		t.Fatal("undo should have brought the task back")
	}

	m = press(t, m, "U")
	active, _ := s.List(0)
	arch, _ := s.ListArchive()
	if len(active) != 0 || len(arch) != 1 {
		t.Errorf("after redo: active=%d archive=%d, want 0/1", len(active), len(arch))
	}
	if !strings.Contains(m.status, "redid") {
		t.Errorf("status = %q, want it to mention the redo", m.status)
	}
}

func TestRedoHintOnlyShownWhenAvailable(t *testing.T) {
	m, s := testModel(t)
	s.Add("a task", task.Do)
	m.refreshFromStore(t)
	if out := m.render(); strings.Contains(out, "U redo") {
		t.Error("redo hint shown with nothing to redo")
	}

	m = press(t, m, "1", "u")
	if out := m.render(); !strings.Contains(out, "U redo") {
		t.Errorf("redo hint missing after an undo:\n%s", out)
	}

	// A new change discards the redo stack, so the hint goes away again.
	m = press(t, m, "a")
	m = typeText(t, m, "something new")
	m = press(t, m, "enter")
	if out := m.render(); strings.Contains(out, "U redo") {
		t.Error("redo hint still shown after a diverging change")
	}
}

func TestUndoKeyWithNothingToUndo(t *testing.T) {
	m, _ := testModel(t)
	m = press(t, m, "u")
	if m.status == "" {
		t.Error("undo with an empty stack should report why in the status line")
	}
}

func TestRestoreFromArchiveView(t *testing.T) {
	m, s := testModel(t)
	s.Add("done thing", task.Do)
	s.Complete(1)
	m.refreshFromStore(t)

	m = press(t, m, "v")
	if m.mode != modeArchive {
		t.Fatalf("mode = %v, want modeArchive", m.mode)
	}
	m = press(t, m, "r")

	active, _ := s.List(task.Do)
	arch, _ := s.ListArchive()
	if len(active) != 1 || len(arch) != 0 {
		t.Fatalf("after restore: active=%d archive=%d, want 1/0", len(active), len(arch))
	}
	if active[0].DoneAt != nil {
		t.Error("restored task should have no completion time")
	}
	if !strings.Contains(m.status, "restored") {
		t.Errorf("status = %q, want it to mention the restore", m.status)
	}
	if m.mode != modeArchive {
		t.Error("restoring should stay in the archive view so several can be restored")
	}
}

func TestRestoreTargetsTheSelectedArchiveRow(t *testing.T) {
	m, s := testModel(t)
	s.Add("older", task.Do)
	s.Add("newer", task.Do)
	s.Complete(1) // older
	s.Complete(2) // newer
	m.refreshFromStore(t)

	// The archive lists newest first, so row 1 is "older".
	m = press(t, m, "v", "j", "r")

	active, _ := s.List(task.Do)
	if len(active) != 1 || active[0].Title != "older" {
		t.Errorf("restored %v, want just [older]", titlesOf(active))
	}
}

func TestMCPIndicatorOnlyWhenEnabled(t *testing.T) {
	m, s := testModel(t)
	s.Add("a task", task.Do)
	m.refreshFromStore(t)

	if out := m.render(); strings.Contains(out, mcpIndicator) {
		t.Errorf("indicator shown while MCP access is off:\n%s", out)
	}

	if _, err := s.SetMCPEnabled(true); err != nil {
		t.Fatal(err)
	}
	m.refreshFromStore(t)
	if out := m.render(); !strings.Contains(out, mcpIndicator) {
		t.Errorf("indicator missing while MCP access is on:\n%s", out)
	}

	// Turning it off elsewhere clears the marker on the next refresh.
	if _, err := s.SetMCPEnabled(false); err != nil {
		t.Fatal(err)
	}
	m.refreshFromStore(t)
	if out := m.render(); strings.Contains(out, mcpIndicator) {
		t.Error("indicator survived MCP access being revoked")
	}
}

func TestMCPIndicatorHiddenWhileTyping(t *testing.T) {
	m, s := testModel(t)
	if _, err := s.SetMCPEnabled(true); err != nil {
		t.Fatal(err)
	}
	m.refreshFromStore(t)

	m = press(t, m, "a")
	if m.mode != modeInput {
		t.Fatal("expected input mode")
	}
	if out := m.render(); strings.Contains(out, mcpIndicator) {
		t.Error("indicator should stand aside while the input line is in use")
	}
}

func TestHelpExplainsMCPSetting(t *testing.T) {
	m, s := testModel(t)
	m = press(t, m, "?")

	out := m.render()
	if !strings.Contains(out, "ike mcp enable") {
		t.Errorf("help does not say how to turn MCP access on:\n%s", out)
	}

	if _, err := s.SetMCPEnabled(true); err != nil {
		t.Fatal(err)
	}
	m.refreshFromStore(t)
	out = m.render()
	if !strings.Contains(out, "ike mcp disable") {
		t.Errorf("help does not say how to turn MCP access off:\n%s", out)
	}
}

func TestRefreshPicksUpExternalChanges(t *testing.T) {
	m, s := testModel(t)
	// Simulate another frontend (CLI/MCP) writing while the TUI is open.
	s.Add("from elsewhere", task.Do)
	next, _ := m.Update(tickMsg{})
	m = next.(Model)
	if len(m.data.List(task.Do)) != 1 {
		t.Error("tick did not reload externally-added task")
	}
}

func TestRenderSmoke(t *testing.T) {
	m, s := testModel(t)
	s.Add("visible task", task.Do)
	m.refreshFromStore(t)
	out := m.render()

	for _, want := range []string{"Do It First", "Schedule It", "Delegate It",
		"Consider Eliminating It", "visible task", "Urgent", "Not urgent"} {
		if !strings.Contains(out, want) {
			t.Errorf("render missing %q", want)
		}
	}

	m.width, m.height = 20, 5
	if out := m.render(); !strings.Contains(out, "too small") {
		t.Errorf("small terminal should say too small, got:\n%s", out)
	}
}

// titlesOf is a compact way to show task order in failure messages.
func titlesOf(ts []task.Task) []string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = t.Title
	}
	return out
}

// refreshFromStore reloads model data outside of Update, for test setup.
func (m *Model) refreshFromStore(t *testing.T) {
	t.Helper()
	d, err := m.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	m.refresh(d)
}

// The refresh tick used to discard its errors, so if the data file became
// unreadable mid-session the TUI kept rendering the last good matrix with no
// sign anything was wrong. The next mutation would surface it, but until then
// the screen quietly disagreed with the file.
func TestRefreshTickSurfacesAReadError(t *testing.T) {
	m, s := testModel(t)
	if _, _, err := s.Add("real task", task.Do); err != nil {
		t.Fatal(err)
	}
	m.refreshFromStore(t)

	// Corrupt the file behind the TUI's back, the way a bad sync or a hand edit
	// would, and make sure the mtime moves so the tick actually re-reads.
	if err := os.WriteFile(s.Path(), []byte("{ not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	m.lastMtime = 0

	next, _ := m.Update(tickMsg{})
	m = next.(Model)

	if m.loadErr == "" {
		t.Fatal("a failed reload left no error on the model")
	}
	view := m.render()
	if !strings.Contains(view, "cannot re-read the data file") {
		t.Errorf("the failure is not visible in the view:\n%s", view)
	}

	// Recovering clears it again, so a transient problem does not stick. The
	// file has to be repaired directly: Mutate deliberately refuses to write
	// over a file it cannot parse, so s.Add would fail here too.
	valid := `{"version":2,"next_id":2,"tasks":[{"id":1,"title":"real task","quadrant":1}],"archive":[]}`
	if err := os.WriteFile(s.Path(), []byte(valid), 0o600); err != nil {
		t.Fatal(err)
	}
	m.lastMtime = 0
	next, _ = m.Update(tickMsg{})
	m = next.(Model)

	if m.loadErr != "" {
		t.Errorf("loadErr = %q, want it cleared after a good read", m.loadErr)
	}
	if got := m.render(); strings.Contains(got, "cannot re-read") {
		t.Errorf("stale error still shown after recovery:\n%s", got)
	}
}

// corrupt makes the next store operation fail, so the tests below can check
// what the TUI does with a mutation that did not happen.
func corrupt(t *testing.T, s *store.Store) {
	t.Helper()
	if err := os.WriteFile(s.Path(), []byte("{ not json"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// A mutation that fails must not report success. These three used to set their
// status message unconditionally, overwriting the error that mutate had just
// written into it — so a failed complete said "done: …" while the task stayed
// on screen. apply's bool return is what makes the success message conditional.
func TestFailedMutationsDoNotReportSuccess(t *testing.T) {
	cases := []struct {
		name    string
		keys    []string
		wrongly string
	}{
		{"complete", []string{"x"}, "done:"},
		{"delete", []string{"d", "d"}, "deleted:"},
		{"move", []string{"m", "2"}, "moved to"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, s := testModel(t)
			if _, _, err := s.Add("ship v2", task.Do); err != nil {
				t.Fatal(err)
			}
			m.refreshFromStore(t)
			corrupt(t, s)

			m = press(t, m, c.keys...)

			if strings.Contains(m.status, c.wrongly) {
				t.Errorf("failed %s reported success: %q", c.name, m.status)
			}
			if m.status == "" {
				t.Errorf("failed %s left no error in the status line", c.name)
			}
			// The matrix on screen still shows the task, because nothing changed.
			if got := len(m.data.List(task.Do)); got != 1 {
				t.Errorf("task count = %d, want 1 — a failed mutation must not alter the view", got)
			}
		})
	}
}

// Entering input mode sets every piece of input state together. Setting them
// one field at a time let the quadrant-rename placeholder survive into the next
// task edit, where it read "quadrant name (empty resets to the default)".
func TestInputPlaceholderDoesNotLeakBetweenModes(t *testing.T) {
	m, s := testModel(t)
	if _, _, err := s.Add("ship v2", task.Do); err != nil {
		t.Fatal(err)
	}
	m.refreshFromStore(t)

	m = press(t, m, "t")   // rename the quadrant
	m = press(t, m, "esc") // cancel
	m = press(t, m, "e")   // now edit a task title

	if strings.Contains(m.input.Placeholder, "quadrant") {
		t.Errorf("quadrant placeholder leaked into the task edit: %q", m.input.Placeholder)
	}
	if m.labelTarget != 0 {
		t.Errorf("labelTarget = %d, want 0 while editing a task", m.labelTarget)
	}
}

// Canceling an edit clears the task being edited, so the next add cannot be
// mistaken for a rename of it.
func TestCancelClearsEditingID(t *testing.T) {
	m, s := testModel(t)
	if _, _, err := s.Add("ship v2", task.Do); err != nil {
		t.Fatal(err)
	}
	m.refreshFromStore(t)

	m = press(t, m, "e")
	m = press(t, m, "esc")
	if m.editingID != 0 {
		t.Errorf("editingID = %d, want 0 after canceling", m.editingID)
	}
}
