// Package tui implements ike's full-screen Eisenhower matrix interface.
package tui

import (
	"fmt"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/jonascript/ike/internal/store"
	"github.com/jonascript/ike/internal/task"
)

type mode int

const (
	modeNormal mode = iota
	modeInput
	modeMove
	modeArchive
	modeSpaces
)

// inputPurpose says what the shared text input is currently collecting.
//
// This used to be inferred from whether editingID and labelTarget were zero, in
// a priority-ordered switch duplicated in three places. That works for three
// purposes and stops working at five: the prompt, the commit, and the success
// message would each have to agree on the same implicit ordering, and a new
// purpose that happened to leave both fields zero would silently be read as
// "add a task".
type inputPurpose int

const (
	inputNone inputPurpose = iota
	inputAddTask
	inputEditTask
	inputRenameQuadrant
	inputNewSpace
	inputRenameSpace
)

// refreshInterval is how often the TUI checks the data file for changes
// made by other frontends (CLI, MCP server).
const refreshInterval = 2 * time.Second

type tickMsg time.Time

// Model is the Bubble Tea model for the matrix UI.
type Model struct {
	store *store.Store
	data  store.Data

	focus  task.Quadrant
	cursor map[task.Quadrant]int

	mode        mode
	inputBack   mode // mode to return to when input mode ends
	input       textinput.Model
	purpose     inputPurpose  // what the input is collecting
	editingID   int           // task being edited, while purpose is inputEditTask
	labelTarget task.Quadrant // quadrant being renamed, while purpose is inputRenameQuadrant
	spaceTarget string        // space being renamed, while purpose is inputRenameSpace

	pendingDelete int    // task ID awaiting a second `d` press
	pendingSpace  string // space name awaiting a second `d` press in the picker
	archCursor    int
	spaceCursor   int
	showHelp      bool

	status  string
	loadErr string

	width, height int
	isDark        bool // terminal background; true until told otherwise

	lastMtime int64
}

// New builds a Model backed by s.
//
// The store is pinned to whichever space it resolved to, so the TUI keeps acting
// on the matrix it is displaying. Left unpinned it would re-resolve the file's
// current space on every keypress, and an `ike space use` from another terminal
// would silently redirect the next `x` into a matrix that is not on screen. The
// poll still follows such a switch — but at a point where it can re-render at
// the same time.
func New(s *store.Store) (Model, error) {
	data, err := s.Load()
	if err != nil {
		return Model{}, err
	}
	s = s.InSpace(data.Space)
	mtime, _ := s.ModTime()

	ti := textinput.New()
	ti.CharLimit = 200

	return Model{
		store:     s,
		data:      data,
		focus:     task.Do,
		cursor:    map[task.Quadrant]int{},
		input:     ti,
		isDark:    true,
		lastMtime: mtime,
	}, nil
}

// Run starts the TUI program.
func Run(s *store.Store) error {
	m, err := New(s)
	if err != nil {
		return err
	}
	_, err = tea.NewProgram(m).Run()
	return err
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(tea.RequestBackgroundColor, tick())
}

func tick() tea.Cmd {
	return tea.Tick(refreshInterval, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// selected returns the task under the cursor, if any.
func (m Model) selected() (task.Task, bool) {
	tasks := m.data.List(m.focus)
	i := m.cursor[m.focus]
	if i < 0 || i >= len(tasks) {
		return task.Task{}, false
	}
	return tasks[i], true
}

func (m *Model) clampCursors() {
	for q := task.Do; q <= task.Eliminate; q++ {
		n := len(m.data.List(q))
		if m.cursor[q] >= n {
			m.cursor[q] = n - 1
		}
		if m.cursor[q] < 0 {
			m.cursor[q] = 0
		}
	}
	// A count, not an index: ListArchive is a permutation of Archive, so the
	// lengths agree. Anything that *indexes* must go through ListArchive.
	if n := len(m.data.Archive); m.archCursor >= n {
		m.archCursor = max(n-1, 0)
	}
}

// refresh replaces the model's data and re-clamps cursors.
func (m *Model) refresh(d store.Data) {
	m.data = d
	if mt, err := m.store.ModTime(); err == nil {
		m.lastMtime = mt
	}
	m.clampCursors()
}

// apply takes the two results of a store mutation and re-renders from them,
// reporting whether it succeeded.
//
// Callers use the bool to decide whether to set a success message. Signaling
// failure through m.status instead was the reason a failed complete, delete, or
// move used to overwrite its own error with "done: …" and leave the row on
// screen: there was nothing to check but the status text itself.
func (m *Model) apply(d store.Data, err error) bool {
	if err != nil {
		m.status = err.Error()
		return false
	}
	m.refresh(d)
	return true
}

// cursorTo puts the cursor on the task with the given ID, so the selection
// follows a task that moved.
func (m *Model) cursorTo(id int) {
	for q := task.Do; q <= task.Eliminate; q++ {
		for i, t := range m.data.List(q) {
			if t.ID == id {
				m.cursor[q] = i
				return
			}
		}
	}
}

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

// history runs an undo or redo step, reporting what moved in the status line.
// Either stack being empty arrives as an ordinary error and shows as one.
func (m *Model) history(verb string, step func() (string, store.Data, error)) {
	label, d, err := step()
	if m.apply(d, err) {
		m.status = fmt.Sprintf("%s %s", verb, label)
	}
}

// reorder moves the selected task within its quadrant, keeping the cursor on it.
func (m *Model) reorder(delta int) {
	t, ok := m.selected()
	if !ok {
		return
	}
	if _, d, err := m.store.Reorder(t.ID, delta); m.apply(d, err) {
		m.status = ""
		m.cursorTo(t.ID)
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case tea.BackgroundColorMsg:
		m.isDark = msg.IsDark()
		return m, nil

	case tickMsg:
		// The poll picks up CLI and MCP writes. A failure here must be visible:
		// silently keeping the last good data left the TUI showing a matrix that
		// no longer matched the file, with no indication anything was wrong.
		if mt, err := m.store.ModTime(); err != nil {
			m.loadErr = err.Error()
		} else if mt != m.lastMtime {
			d, err := m.store.Load()
			if err != nil {
				m.loadErr = err.Error()
			} else {
				m.loadErr = ""
				m.followCurrentSpace(&d)
				m.refresh(d)
			}
		}
		return m, tick()

	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch m.mode {
	case modeInput:
		return m.handleInputKey(msg)
	case modeMove:
		return m.handleMoveKey(msg)
	case modeArchive:
		return m.handleArchiveKey(msg)
	case modeSpaces:
		return m.handleSpacesKey(msg)
	}
	return m.handleNormalKey(msg)
}

// enterInput switches to input mode with every piece of input state set
// together. Setting them one at a time across the call sites left the previous
// session's placeholder visible in the next one.
//
// back is the mode to return to on confirm or cancel: a prompt raised from the
// space picker belongs back in the picker, not on the matrix.
func (m *Model) enterInput(p inputPurpose, back mode, value, placeholder string) tea.Cmd {
	m.mode = modeInput
	m.purpose = p
	m.inputBack = back
	m.input.SetValue(value)
	m.input.Placeholder = placeholder
	m.input.CursorEnd()
	m.status = ""
	return m.input.Focus()
}

// exitInput leaves input mode and clears the input state, so nothing from this
// session can leak into the next one.
func (m *Model) exitInput() {
	m.mode = m.inputBack
	m.purpose = inputNone
	m.editingID = 0
	m.labelTarget = 0
	m.spaceTarget = ""
	m.input.Blur()
}

func (m Model) handleNormalKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// Any key other than a second `d` clears a pending delete.
	pending := m.pendingDelete
	m.pendingDelete = 0
	if pending != 0 {
		m.status = ""
	}

	switch {
	case key.Matches(msg, keys.Quit):
		return m, tea.Quit

	case key.Matches(msg, keys.Quadrant):
		m.focus = task.Quadrant(msg.Key().Code - '0')

	case key.Matches(msg, keys.NextQuad):
		m.focus = m.focus%4 + 1

	case key.Matches(msg, keys.PrevQuad):
		m.focus = (m.focus+2)%4 + 1

	case key.Matches(msg, keys.Down):
		if m.cursor[m.focus] < len(m.data.List(m.focus))-1 {
			m.cursor[m.focus]++
		}

	case key.Matches(msg, keys.Up):
		if m.cursor[m.focus] > 0 {
			m.cursor[m.focus]--
		}

	case key.Matches(msg, keys.MoveUp):
		m.reorder(-1)

	case key.Matches(msg, keys.MoveDown):
		m.reorder(1)

	case key.Matches(msg, keys.Add):
		return m, m.enterInput(inputAddTask, modeNormal, "", "task title")

	case key.Matches(msg, keys.Edit):
		if t, ok := m.selected(); ok {
			m.editingID = t.ID
			return m, m.enterInput(inputEditTask, modeNormal, t.Title, "task title")
		}

	case key.Matches(msg, keys.Title):
		m.labelTarget = m.focus
		return m, m.enterInput(inputRenameQuadrant, modeNormal, m.data.Labels.Of(m.focus),
			"quadrant name (empty resets to the default)")

	case key.Matches(msg, keys.Done):
		if t, ok := m.selected(); ok {
			if _, d, err := m.store.Complete(t.ID); m.apply(d, err) {
				m.status = fmt.Sprintf("done: %s", t.DisplayTitle())
			}
		}

	case key.Matches(msg, keys.Move):
		if _, ok := m.selected(); ok {
			m.mode = modeMove
			m.status = "move to quadrant [1-4], any other key cancels"
		}

	case key.Matches(msg, keys.Delete):
		if t, ok := m.selected(); ok {
			if pending != t.ID {
				m.pendingDelete = t.ID
				m.status = fmt.Sprintf("press d again to delete %q", t.DisplayTitle())
			} else if _, d, err := m.store.Delete(t.ID); m.apply(d, err) {
				m.status = fmt.Sprintf("deleted: %s", t.DisplayTitle())
			}
		}

	case key.Matches(msg, keys.Undo):
		m.history("undid", m.store.Undo)

	case key.Matches(msg, keys.Redo):
		m.history("redid", m.store.Redo)

	case key.Matches(msg, keys.Archive):
		m.mode = modeArchive
		m.archCursor = 0

	case key.Matches(msg, keys.Spaces):
		m.mode = modeSpaces
		m.status = ""
		m.cursorToSpace(m.data.Space)

	case key.Matches(msg, keys.NextSpace):
		m.cycleSpace(1)

	case key.Matches(msg, keys.PrevSpace):
		m.cycleSpace(-1)

	case key.Matches(msg, keys.Help):
		m.showHelp = !m.showHelp
	}
	return m, nil
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

	switch {
	case key.Matches(msg, keys.Quit), key.Matches(msg, keys.Cancel), key.Matches(msg, keys.Spaces):
		m.mode = modeNormal
		m.status = ""

	case key.Matches(msg, keys.Confirm):
		if sp, ok := m.selectedSpace(); ok {
			m.mode = modeNormal
			m.useSpace(sp.Name)
		}

	case key.Matches(msg, keys.Down):
		if m.spaceCursor < len(m.data.AllSpaces)-1 {
			m.spaceCursor++
		}

	case key.Matches(msg, keys.Up):
		if m.spaceCursor > 0 {
			m.spaceCursor--
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

func (m Model) handleInputKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Cancel):
		m.exitInput()
		return m, nil

	case key.Matches(msg, keys.Confirm):
		value := m.input.Value()
		purpose, editing := m.purpose, m.editingID
		target, spaceName := m.labelTarget, m.spaceTarget

		var d store.Data
		var err error
		switch purpose {
		case inputRenameQuadrant:
			_, d, err = m.store.SetQuadrantLabel(target, value)
		case inputEditTask:
			_, d, err = m.store.Rename(editing, value)
		case inputNewSpace:
			d, err = m.store.NewSpace(value)
		case inputRenameSpace:
			// RenameSpace returns no Data — nothing renders from it — so the
			// state to re-render from comes from a plain read afterwards.
			if err = m.store.RenameSpace(spaceName, value); err == nil {
				d, err = m.store.Load()
			}
		default:
			_, d, err = m.store.Add(value, m.focus)
		}

		if m.apply(d, err) {
			m.status = ""
			switch purpose {
			case inputRenameQuadrant:
				m.status = fmt.Sprintf("quadrant %d is now %q", target, m.data.Labels.Of(target))
			case inputNewSpace:
				m.status = fmt.Sprintf("created space %q", task.SanitizeDisplay(value))
				m.cursorToSpace(value)
			case inputRenameSpace:
				m.status = fmt.Sprintf("renamed space to %q", task.SanitizeDisplay(value))
				m.cursorToSpace(value)
			case inputAddTask:
				// Move the cursor to the task just added (last in its quadrant).
				m.cursor[m.focus] = max(len(m.data.List(m.focus))-1, 0)
			}
		}
		m.exitInput()
		return m, nil
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m Model) handleMoveKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	m.mode = modeNormal
	m.status = ""
	if key.Matches(msg, keys.Quadrant) {
		dest := task.Quadrant(msg.Key().Code - '0')
		if t, ok := m.selected(); ok && dest != t.Quadrant {
			if _, d, err := m.store.Move(t.ID, dest); m.apply(d, err) {
				m.status = fmt.Sprintf("moved to %d · %s", dest, m.data.Labels.Of(dest))
			}
		}
	}
	return m, nil
}

func (m Model) handleArchiveKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Quit), key.Matches(msg, keys.Cancel), key.Matches(msg, keys.Archive):
		m.mode = modeNormal
		m.status = ""

	case key.Matches(msg, keys.Restore):
		arch := m.data.ListArchive()
		if m.archCursor < len(arch) {
			t := arch[m.archCursor]
			if _, d, err := m.store.Restore(t.ID); m.apply(d, err) {
				m.status = fmt.Sprintf("restored %q to %d · %s",
					t.DisplayTitle(), t.Quadrant, m.data.Labels.Of(t.Quadrant))
			}
		}

	case key.Matches(msg, keys.Down):
		if m.archCursor < len(m.data.Archive)-1 {
			m.archCursor++
		}
	case key.Matches(msg, keys.Up):
		if m.archCursor > 0 {
			m.archCursor--
		}
	}
	return m, nil
}
