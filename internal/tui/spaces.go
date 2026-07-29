package tui

import (
	"fmt"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/jonascript/ike/internal/store"
	"github.com/jonascript/ike/internal/task"
)

// This file holds the space and file pickers: the state that follows which
// matrix is on screen, and the two modes that switch between them. It sits
// beside model.go rather than inside it because the pickers are a self-contained
// concern — model.go is the matrix, Update, and the shared machinery — and
// because every other package that grew spaces support grew a spaces.go too.

// cursorToSpace puts the picker's cursor on a space by name, so the selection
// follows a space that was just created or renamed.
func (m *Model) cursorToSpace(name string) {
	for i, sp := range m.data.AllSpaces {
		if sp.Name == name {
			m.spaceCursor = i
			return
		}
	}
}

// selectedSpace returns the space under the picker's cursor, if any.
func (m Model) selectedSpace() (store.SpaceInfo, bool) {
	if m.spaceCursor < 0 || m.spaceCursor >= len(m.data.AllSpaces) {
		return store.SpaceInfo{}, false
	}
	return m.data.AllSpaces[m.spaceCursor], true
}

// switchTo adopts d as the current space, re-pinning the store to it and
// resetting everything that indexed into the space being left.
//
// Re-pinning is the load-bearing half. The store must follow the space on
// screen, or the next keypress resolves the file's current space instead and
// completes a task in a matrix the user cannot see. Every cursor is reset
// because they index a different task list now, and lastMtime is re-seeded so
// the next poll compares against the file as it stands rather than reloading
// spuriously.
func (m *Model) switchTo(d store.Data) {
	m.store = m.store.InSpace(d.Space)
	m.focus = task.Do
	m.cursor = map[task.Quadrant]int{}
	m.archCursor = 0
	m.pendingDelete = 0
	m.pendingSpace = ""
	m.status = ""
	m.loadErr = ""
	m.refresh(d)
	m.cursorToSpace(d.Space)
}

// followCurrentSpace moves the TUI onto the file's current space if another
// frontend changed it, rewriting d in place to that space's data.
//
// This is the other half of pinning. The pin stops a keypress being redirected
// mid-session, but the TUI should still end up where `ike space use` put the
// user rather than sitting on a space they have moved off. Doing it here, at the
// poll, means the switch and the re-render happen together — so the screen and
// the mutation target never disagree.
//
// It does nothing while a prompt or a move is open: both hold state belonging to
// the space being left, an editingID being an ID in the old matrix.
func (m *Model) followCurrentSpace(d *store.Data) {
	if m.mode == modeInput || m.mode == modeMove {
		return
	}
	current := ""
	for _, sp := range d.AllSpaces {
		if sp.Current {
			current = sp.Name
		}
	}
	if current == "" || current == d.Space {
		return
	}
	moved, err := m.store.InSpace(current).Load()
	if err != nil {
		m.loadErr = err.Error()
		return
	}
	m.switchTo(moved)
	m.status = fmt.Sprintf("space %q", task.SanitizeDisplay(moved.Space))
	*d = moved
}

// useSpace switches to a named space, persisting the choice so the CLI and any
// new TUI agree on where the user is.
func (m *Model) useSpace(name string) {
	d, err := m.store.UseSpace(name)
	if err != nil {
		m.status = err.Error()
		return
	}
	m.switchTo(d)
	m.status = fmt.Sprintf("space %q", task.SanitizeDisplay(d.Space))
}

// cycleSpace moves to the next or previous space in the sorted list, wrapping.
func (m *Model) cycleSpace(delta int) {
	all := m.data.AllSpaces
	if len(all) < 2 {
		m.status = "only one space; press s to make another"
		return
	}
	at := 0
	for i, sp := range all {
		if sp.Name == m.data.Space {
			at = i
			break
		}
	}
	next := ((at+delta)%len(all) + len(all)) % len(all)
	m.useSpace(all[next].Name)
}

// handleSpacesKey drives the space picker: a full-screen list, like the archive
// view, but one that can also create, rename, and delete what it lists.
func (m Model) handleSpacesKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// Any key but a second `d` clears a pending delete, matching the matrix.
	pending := m.pendingSpace
	m.pendingSpace = ""
	if pending != "" {
		m.status = ""
	}

	if moveCursor(&m.spaceCursor, len(m.data.AllSpaces), msg) {
		return m, nil
	}
	switch {
	case key.Matches(msg, keys.Quit), key.Matches(msg, keys.Cancel), key.Matches(msg, keys.Spaces):
		m.mode = modeNormal
		m.status = ""

	case key.Matches(msg, keys.Confirm):
		if sp, ok := m.selectedSpace(); ok {
			m.mode = modeNormal
			m.useSpace(sp.Name)
		}

	case key.Matches(msg, keys.NewSpace):
		return m, m.enterInput(inputNewSpace, modeSpaces, "", "new space name")

	case key.Matches(msg, keys.RenameSpace):
		if sp, ok := m.selectedSpace(); ok {
			m.spaceTarget = sp.Name
			return m, m.enterInput(inputRenameSpace, modeSpaces, sp.Name, "space name")
		}

	case key.Matches(msg, keys.Delete):
		if sp, ok := m.selectedSpace(); ok {
			m.deleteSpace(sp, pending)
		}
	}
	return m, nil
}

// handleFilesKey drives the file picker: the recently opened data files, plus a
// prompt for typing a path that is not in the list.
func (m Model) handleFilesKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if moveCursor(&m.fileCursor, len(m.recent), msg) {
		return m, nil
	}
	switch {
	case key.Matches(msg, keys.Quit), key.Matches(msg, keys.Cancel), key.Matches(msg, keys.Files):
		m.mode = modeNormal
		m.status = ""

	case key.Matches(msg, keys.Confirm):
		if m.fileCursor >= 0 && m.fileCursor < len(m.recent) {
			m.mode = modeNormal
			m.openFile(m.recent[m.fileCursor])
		}

	case key.Matches(msg, keys.OpenFile):
		return m, m.enterInput(inputOpenFile, modeFiles, "", "absolute path to a data file")
	}
	return m, nil
}

// openFile switches the whole TUI to another data file.
//
// A failure leaves the model exactly where it was, showing why: pointing at a
// path that turns out to be unreadable should not cost you the session you were
// already in. The path goes through the same validation as --file, so the rules
// do not depend on which way you opened it.
func (m *Model) openFile(path string) {
	s, err := store.OpenPath("path", path)
	if err != nil {
		m.status = err.Error()
		return
	}
	d, err := s.Load()
	if err != nil {
		m.status = err.Error()
		return
	}
	m.store = s.InSpace(d.Space)
	store.RememberRecent(m.store.Path())
	m.recent = store.LoadRecent().Paths
	m.fileCursor = 0
	m.switchTo(d)
	m.status = fmt.Sprintf("opened %s", path)
}

// deleteSpace removes the selected space on a second `d`, having first said
// exactly what that would destroy.
//
// Deleting a space is the one thing in ike with no way back — history is per
// space, so there is no stack left to undo it from — which is why the warning
// names the counts rather than just asking to confirm.
func (m *Model) deleteSpace(sp store.SpaceInfo, pending string) {
	if pending != sp.Name {
		m.pendingSpace = sp.Name
		held := ""
		if sp.Active > 0 || sp.Archived > 0 {
			held = fmt.Sprintf(" (%d active, %d archived)", sp.Active, sp.Archived)
		}
		// Kept short enough to survive truncation at 80 columns: the counts and
		// the words "cannot be undone" are the whole point of the prompt, so
		// losing the tail of it to an ellipsis would defeat the warning.
		m.status = fmt.Sprintf("press d again to delete %q%s — cannot be undone",
			task.SanitizeDisplay(sp.Name), held)
		return
	}
	// Force: the confirmation above already said what would go, so a second
	// refusal here would just be a dead end with no way to proceed.
	if _, err := m.store.RemoveSpace(sp.Name, true); err != nil {
		m.status = err.Error()
		return
	}
	d, err := m.store.InSpace("").Load()
	if err != nil {
		m.status = err.Error()
		return
	}
	m.switchTo(d)
	m.status = fmt.Sprintf("deleted space %q", task.SanitizeDisplay(sp.Name))
}
