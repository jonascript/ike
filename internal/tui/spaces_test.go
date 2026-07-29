package tui

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/jonascript/ike/internal/store"
	"github.com/jonascript/ike/internal/task"
)

// spacesModel returns a model over a file that already has a second space, with
// one task in each.
func spacesModel(t *testing.T) (Model, *store.Store) {
	t.Helper()
	s := store.OpenAt(filepath.Join(t.TempDir(), "tasks.json"))
	if _, _, err := s.Add("home task", task.Do); err != nil {
		t.Fatal(err)
	}
	if _, err := s.NewSpace("work"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.InSpace("work").Add("work task", task.Do); err != nil {
		t.Fatal(err)
	}
	m, err := New(s)
	if err != nil {
		t.Fatal(err)
	}
	m.width, m.height = 80, 24
	return m, s
}

func activeTitles(t *testing.T, s *store.Store, space string) []string {
	t.Helper()
	d, err := s.InSpace(space).Load()
	if err != nil {
		t.Fatal(err)
	}
	return titlesOf(d.List(0))
}

func TestSpaceHeaderNamesTheSpace(t *testing.T) {
	m, _ := spacesModel(t)
	out := m.render()
	if !strings.Contains(out, "space default") {
		t.Errorf("render lacks the space header:\n%s", out)
	}
	if !strings.Contains(out, "1 of 2") {
		t.Errorf("render lacks the space counter:\n%s", out)
	}
}

func TestSpacePickerListsSpaces(t *testing.T) {
	m, _ := spacesModel(t)
	m = press(t, m, "s")
	if m.mode != modeSpaces {
		t.Fatalf("mode = %v, want the picker", m.mode)
	}
	out := m.render()
	for _, want := range []string{"Spaces — 2", "default", "work", "1 active"} {
		if !strings.Contains(out, want) {
			t.Errorf("picker lacks %q:\n%s", want, out)
		}
	}
	// esc goes back to the matrix.
	if m := press(t, m, "esc"); m.mode != modeNormal {
		t.Errorf("mode after esc = %v, want normal", m.mode)
	}
}

// The picker must move the store as well as the screen. If it only switched what
// is rendered, the next keypress would mutate the space the user just left.
func TestPickerSwitchMovesTheStoreAndPersistsCurrent(t *testing.T) {
	m, s := spacesModel(t)
	m = press(t, m, "s", "j", "enter")

	if m.data.Space != "work" {
		t.Fatalf("space = %q, want work", m.data.Space)
	}
	if m.mode != modeNormal {
		t.Errorf("mode = %v, want back on the matrix", m.mode)
	}
	// Persisted, so the CLI and a new TUI agree.
	d, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if d.Space != "work" {
		t.Errorf("file current = %q, want work", d.Space)
	}
	// And the next mutation lands there, not in the space we came from.
	m = press(t, m, "x")
	if got := activeTitles(t, s, "work"); len(got) != 0 {
		t.Errorf("work tasks = %v, want the completion to have landed here", got)
	}
	if got := activeTitles(t, s, "default"); len(got) != 1 {
		t.Errorf("default tasks = %v, want it untouched", got)
	}
}

func TestCycleKeysSwitchAndWrap(t *testing.T) {
	m, _ := spacesModel(t)
	if m = press(t, m, "]"); m.data.Space != "work" {
		t.Fatalf("after ] space = %q, want work", m.data.Space)
	}
	// Wraps back around rather than stopping at the end.
	if m = press(t, m, "]"); m.data.Space != "default" {
		t.Errorf("after wrapping ] space = %q, want default", m.data.Space)
	}
	if m = press(t, m, "["); m.data.Space != "work" {
		t.Errorf("after [ space = %q, want work", m.data.Space)
	}
}

func TestCycleWithOneSpaceSaysSo(t *testing.T) {
	m, _ := testModel(t)
	m = press(t, m, "]")
	if !strings.Contains(m.status, "only one space") {
		t.Errorf("status = %q, want an explanation", m.status)
	}
}

// Switching space has to reset everything that indexed into the old one.
func TestSwitchingResetsCursors(t *testing.T) {
	m, s := spacesModel(t)
	// Give default several tasks and put the cursor down the list.
	for _, title := range []string{"b", "c", "d"} {
		if _, _, err := s.Add(title, task.Do); err != nil {
			t.Fatal(err)
		}
	}
	m.refreshFromStore(t)
	m = press(t, m, "j", "j")
	if m.cursor[task.Do] == 0 {
		t.Fatal("cursor should have moved before the switch")
	}
	m = press(t, m, "]")
	if m.cursor[task.Do] != 0 {
		t.Errorf("cursor = %d, want it reset for the new space", m.cursor[task.Do])
	}
	if m.focus != task.Do {
		t.Errorf("focus = %v, want it reset", m.focus)
	}
	if m.archCursor != 0 {
		t.Errorf("archCursor = %d, want it reset", m.archCursor)
	}
}

func TestPickerCreatesASpace(t *testing.T) {
	m, s := spacesModel(t)
	m = press(t, m, "s", "n")
	if m.mode != modeInput || m.purpose != inputNewSpace {
		t.Fatalf("mode = %v purpose = %v, want the new-space prompt", m.mode, m.purpose)
	}
	if !strings.Contains(m.render(), "new space:") {
		t.Errorf("prompt not shown:\n%s", m.render())
	}
	m.input.SetValue("errands")
	m = press(t, m, "enter")

	// The prompt came from the picker, so it returns there.
	if m.mode != modeSpaces {
		t.Errorf("mode = %v, want back in the picker", m.mode)
	}
	names, err := s.ListSpaces()
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 3 {
		t.Errorf("spaces = %+v, want the new one", names)
	}
	// Creating does not switch, matching the CLI.
	if m.data.Space != "default" {
		t.Errorf("space = %q, want creating not to switch", m.data.Space)
	}
}

func TestPickerRenamesASpace(t *testing.T) {
	m, s := spacesModel(t)
	m = press(t, m, "s", "j", "r")
	if m.purpose != inputRenameSpace || m.spaceTarget != "work" {
		t.Fatalf("purpose = %v target = %q, want the rename prompt for work", m.purpose, m.spaceTarget)
	}
	m.input.SetValue("job")
	m = press(t, m, "enter")

	names, err := s.ListSpaces()
	if err != nil {
		t.Fatal(err)
	}
	got := []string{names[0].Name, names[1].Name}
	if got[0] != "default" || got[1] != "job" {
		t.Errorf("spaces = %v, want default and job", got)
	}
	// The task came with it.
	if titles := activeTitles(t, s, "job"); len(titles) != 1 || titles[0] != "work task" {
		t.Errorf("job tasks = %v, want the work task carried over", titles)
	}
}

func TestPickerCancelReturnsToThePicker(t *testing.T) {
	m, _ := spacesModel(t)
	m = press(t, m, "s", "n")
	m = typeText(t, m, "scratch")
	m = press(t, m, "esc")
	if m.mode != modeSpaces {
		t.Errorf("mode = %v, want back in the picker", m.mode)
	}
	if m.purpose != inputNone || m.spaceTarget != "" {
		t.Errorf("input state left set: purpose %v target %q", m.purpose, m.spaceTarget)
	}
}

// Deleting a space cannot be undone, so it takes two presses and says what goes.
func TestPickerDeleteNeedsConfirmationAndWarnsAboutContents(t *testing.T) {
	m, s := spacesModel(t)
	m = press(t, m, "s", "j", "d")

	if !strings.Contains(m.status, "press d again") {
		t.Errorf("status = %q, want a confirmation prompt", m.status)
	}
	if !strings.Contains(m.status, "cannot be undone") {
		t.Errorf("status = %q, want it to say the delete is permanent", m.status)
	}
	if !strings.Contains(m.status, "1 active") {
		t.Errorf("status = %q, want it to name what would be lost", m.status)
	}
	if names, _ := s.ListSpaces(); len(names) != 2 {
		t.Fatal("nothing should be deleted on the first press")
	}

	m = press(t, m, "d")
	names, err := s.ListSpaces()
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0].Name != "default" {
		t.Errorf("spaces = %+v, want only default", names)
	}
	if !strings.Contains(m.status, "deleted space") {
		t.Errorf("status = %q, want the deletion reported", m.status)
	}
}

func TestPickerDeleteCancelledByAnotherKey(t *testing.T) {
	m, s := spacesModel(t)
	m = press(t, m, "s", "j", "d", "k", "d")
	// The second d armed a different row rather than deleting the first.
	if names, _ := s.ListSpaces(); len(names) != 2 {
		t.Errorf("spaces = %+v, want nothing deleted", names)
	}
	if !strings.Contains(m.status, "press d again") {
		t.Errorf("status = %q, want a fresh confirmation", m.status)
	}
}

func TestPickerRefusesToDeleteTheLastSpace(t *testing.T) {
	m, s := spacesModel(t)
	// Remove work first, leaving only default.
	if _, err := s.RemoveSpace("work", true); err != nil {
		t.Fatal(err)
	}
	m.refreshFromStore(t)
	m = press(t, m, "s", "d", "d")
	if !strings.Contains(m.status, "only space") {
		t.Errorf("status = %q, want a refusal", m.status)
	}
	if names, _ := s.ListSpaces(); len(names) != 1 {
		t.Errorf("spaces = %+v, want the space kept", names)
	}
}

// The pin is what stops a keypress being redirected. Another frontend switching
// the current space must not change where this keypress lands.
func TestExternalSpaceUseDoesNotRedirectAKeypress(t *testing.T) {
	m, s := spacesModel(t)
	if m.data.Space != "default" {
		t.Fatalf("space = %q, want to start on default", m.data.Space)
	}

	// Another frontend moves the current space, with no tick in between.
	if _, err := store.OpenAt(s.Path()).UseSpace("work"); err != nil {
		t.Fatal(err)
	}
	m = press(t, m, "x") // complete the task on screen

	if got := activeTitles(t, s, "default"); len(got) != 0 {
		t.Errorf("default tasks = %v, want the completion to have landed on screen", got)
	}
	if got := activeTitles(t, s, "work"); len(got) != 1 {
		t.Errorf("work tasks = %v, want it untouched by a keypress aimed at default", got)
	}
}

// It should still end up where the user went, but at the poll, where the switch
// and the re-render happen together.
func TestTickFollowsAnExternalSpaceSwitch(t *testing.T) {
	m, s := spacesModel(t)
	if _, err := store.OpenAt(s.Path()).UseSpace("work"); err != nil {
		t.Fatal(err)
	}

	next, _ := m.Update(tickMsg{})
	m = next.(Model)

	if m.data.Space != "work" {
		t.Fatalf("space = %q, want the tick to have followed the switch", m.data.Space)
	}
	if !strings.Contains(m.render(), "space work") {
		t.Errorf("render still shows the old space:\n%s", m.render())
	}
	// And now keypresses land in the space on screen.
	m = press(t, m, "x")
	if got := activeTitles(t, s, "work"); len(got) != 0 {
		t.Errorf("work tasks = %v, want the completion here", got)
	}
	if got := activeTitles(t, s, "default"); len(got) != 1 {
		t.Errorf("default tasks = %v, want it untouched", got)
	}
}

// A prompt holds state belonging to the space being left — an editingID is an ID
// in the old matrix — so following a switch has to wait.
func TestTickDoesNotFollowWhileTyping(t *testing.T) {
	m, s := spacesModel(t)
	m = press(t, m, "a")
	m = typeText(t, m, "half written")
	if _, err := store.OpenAt(s.Path()).UseSpace("work"); err != nil {
		t.Fatal(err)
	}

	next, _ := m.Update(tickMsg{})
	m = next.(Model)
	if m.data.Space != "default" {
		t.Errorf("space = %q, want the switch deferred while typing", m.data.Space)
	}
	if m.input.Value() != "half written" {
		t.Errorf("input = %q, want the half-typed task kept", m.input.Value())
	}
}

func TestArchiveViewNamesItsSpace(t *testing.T) {
	m, s := spacesModel(t)
	d, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Complete(d.Tasks[0].ID); err != nil {
		t.Fatal(err)
	}
	m.refreshFromStore(t)
	m = press(t, m, "v")
	if !strings.Contains(m.render(), "in default") {
		t.Errorf("archive view does not name its space:\n%s", m.render())
	}
}

func TestHelpMentionsSpaces(t *testing.T) {
	m, _ := spacesModel(t)
	m = press(t, m, "?")
	out := m.render()
	if !strings.Contains(out, "s spaces") {
		t.Errorf("help lacks the space keys:\n%s", out)
	}
	if !strings.Contains(out, "not undoable") {
		t.Errorf("help does not warn that space changes are permanent:\n%s", out)
	}
}

// The grid lost a row to the space header, so the smallest supported terminal
// must still show a task rather than only a "… more" line.
func TestRenderAtMinimumHeightStillShowsATask(t *testing.T) {
	m, _ := spacesModel(t)
	// Wide enough that the title is not truncated, so this tests the height
	// arithmetic alone.
	m.width, m.height = 80, minHeight
	out := m.render()
	if strings.Contains(out, "too small") {
		t.Fatalf("the minimum height should render:\n%s", out)
	}
	if !strings.Contains(out, "home task") {
		t.Errorf("no task row at the minimum height:\n%s", out)
	}
	// And the narrowest supported width still renders something coherent.
	m.width = minWidth
	if out := m.render(); strings.Contains(out, "too small") {
		t.Errorf("the minimum size should render:\n%s", out)
	}
}

// A space prompt must not leave state behind for the next prompt to inherit.
func TestSpacePromptStateDoesNotLeak(t *testing.T) {
	m, _ := spacesModel(t)
	m = press(t, m, "s", "j", "r") // rename prompt, prefilled with "work"
	m = press(t, m, "esc", "esc")  // back to the picker, then the matrix
	m = press(t, m, "a")           // add a task
	if m.purpose != inputAddTask {
		t.Errorf("purpose = %v, want inputAddTask", m.purpose)
	}
	if m.spaceTarget != "" {
		t.Errorf("spaceTarget = %q, want it cleared", m.spaceTarget)
	}
	if got := m.input.Value(); got != "" {
		t.Errorf("input = %q, want the space name not to have leaked in", got)
	}
	if !strings.Contains(m.render(), "add to 1") {
		t.Errorf("prompt is not the add prompt:\n%s", m.render())
	}
}
