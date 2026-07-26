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

	mode      mode
	input     textinput.Model
	editingID int // 0 while adding, task ID while editing

	pendingDelete int // task ID awaiting second `d` press
	archCursor    int
	showHelp      bool

	status  string
	loadErr string

	width, height int
	isDark        bool // terminal background; true until told otherwise

	lastMtime int64
}

// New builds a Model backed by s.
func New(s *store.Store) (Model, error) {
	data, err := s.Load()
	if err != nil {
		return Model{}, err
	}
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

// tasksIn returns the active tasks of quadrant q in stable ID order.
func (m Model) tasksIn(q task.Quadrant) []task.Task {
	var out []task.Task
	for _, t := range m.data.Tasks {
		if t.Quadrant == q {
			out = append(out, t)
		}
	}
	return out
}

// selected returns the task under the cursor, if any.
func (m Model) selected() (task.Task, bool) {
	tasks := m.tasksIn(m.focus)
	i := m.cursor[m.focus]
	if i < 0 || i >= len(tasks) {
		return task.Task{}, false
	}
	return tasks[i], true
}

func (m *Model) clampCursors() {
	for q := task.Do; q <= task.Eliminate; q++ {
		n := len(m.tasksIn(q))
		if m.cursor[q] >= n {
			m.cursor[q] = n - 1
		}
		if m.cursor[q] < 0 {
			m.cursor[q] = 0
		}
	}
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

// mutate runs fn against the store and refreshes, surfacing errors in status.
func (m *Model) mutate(fn func() (store.Data, error)) {
	d, err := fn()
	if err != nil {
		m.status = err.Error()
		return
	}
	m.refresh(d)
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
		if mt, err := m.store.ModTime(); err == nil && mt != m.lastMtime {
			if d, err := m.store.Load(); err == nil {
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
	}
	return m.handleNormalKey(msg)
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
		if m.cursor[m.focus] < len(m.tasksIn(m.focus))-1 {
			m.cursor[m.focus]++
		}

	case key.Matches(msg, keys.Up):
		if m.cursor[m.focus] > 0 {
			m.cursor[m.focus]--
		}

	case key.Matches(msg, keys.Add):
		m.mode = modeInput
		m.editingID = 0
		m.input.SetValue("")
		m.input.Placeholder = "task title"
		m.status = ""
		return m, m.input.Focus()

	case key.Matches(msg, keys.Edit):
		if t, ok := m.selected(); ok {
			m.mode = modeInput
			m.editingID = t.ID
			m.input.SetValue(t.Title)
			m.input.CursorEnd()
			m.status = ""
			return m, m.input.Focus()
		}

	case key.Matches(msg, keys.Done):
		if t, ok := m.selected(); ok {
			m.mutate(func() (store.Data, error) {
				_, err := m.store.Complete(t.ID)
				if err != nil {
					return store.Data{}, err
				}
				return m.store.Load()
			})
			m.status = fmt.Sprintf("done: %s", t.Title)
		}

	case key.Matches(msg, keys.Move):
		if _, ok := m.selected(); ok {
			m.mode = modeMove
			m.status = "move to quadrant [1-4], any other key cancels"
		}

	case key.Matches(msg, keys.Delete):
		if t, ok := m.selected(); ok {
			if pending == t.ID {
				m.mutate(func() (store.Data, error) {
					_, err := m.store.Delete(t.ID)
					if err != nil {
						return store.Data{}, err
					}
					return m.store.Load()
				})
				m.status = fmt.Sprintf("deleted: %s", t.Title)
			} else {
				m.pendingDelete = t.ID
				m.status = fmt.Sprintf("press d again to delete %q", t.Title)
			}
		}

	case key.Matches(msg, keys.Archive):
		m.mode = modeArchive
		m.archCursor = 0

	case key.Matches(msg, keys.Help):
		m.showHelp = !m.showHelp
	}
	return m, nil
}

func (m Model) handleInputKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Cancel):
		m.mode = modeNormal
		m.input.Blur()
		return m, nil

	case key.Matches(msg, keys.Confirm):
		title := m.input.Value()
		editing := m.editingID
		m.mutate(func() (store.Data, error) {
			var err error
			if editing == 0 {
				_, err = m.store.Add(title, m.focus)
			} else {
				_, err = m.store.Rename(editing, title)
			}
			if err != nil {
				return store.Data{}, err
			}
			return m.store.Load()
		})
		if m.status == "" && editing == 0 {
			// Move the cursor to the task just added (last in its quadrant).
			m.cursor[m.focus] = max(len(m.tasksIn(m.focus))-1, 0)
		}
		m.mode = modeNormal
		m.input.Blur()
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
			m.mutate(func() (store.Data, error) {
				_, err := m.store.Move(t.ID, dest)
				if err != nil {
					return store.Data{}, err
				}
				return m.store.Load()
			})
			m.status = fmt.Sprintf("moved to %d · %s", dest, dest.Label())
		}
	}
	return m, nil
}

func (m Model) handleArchiveKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Quit), key.Matches(msg, keys.Cancel), key.Matches(msg, keys.Archive):
		m.mode = modeNormal
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
