package tui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/joncrockett/ike/internal/store"
	"github.com/joncrockett/ike/internal/task"
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
	if got := m.tasksIn(task.Schedule); len(got) != 1 {
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
		t.Errorf("cancelled add still wrote %d tasks", len(tasks))
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

func TestRefreshPicksUpExternalChanges(t *testing.T) {
	m, s := testModel(t)
	// Simulate another frontend (CLI/MCP) writing while the TUI is open.
	s.Add("from elsewhere", task.Do)
	next, _ := m.Update(tickMsg{})
	m = next.(Model)
	if len(m.tasksIn(task.Do)) != 1 {
		t.Error("tick did not reload externally-added task")
	}
}

func TestRenderSmoke(t *testing.T) {
	m, s := testModel(t)
	s.Add("visible task", task.Do)
	m.refreshFromStore(t)
	out := m.render()

	for _, want := range []string{"Do", "Schedule", "Delegate", "Eliminate", "visible task",
		"Urgent", "Not urgent", "IMPORTANT"} {
		if !strings.Contains(out, want) {
			t.Errorf("render missing %q", want)
		}
	}

	m.width, m.height = 20, 5
	if out := m.render(); !strings.Contains(out, "too small") {
		t.Errorf("small terminal should say too small, got:\n%s", out)
	}
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
