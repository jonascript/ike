package tui

import (
	"context"
	"fmt"
	"os"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/jonascript/ike/internal/agent"
	"github.com/jonascript/ike/internal/store"
	"github.com/jonascript/ike/internal/task"
)

// A run streams into the TUI through the ordinary Bubble Tea channel pattern: a
// command blocks on one event, wraps it as a message, and is re-issued from
// Update. Nothing here touches the model from a goroutine.
//
// The run keeps going while you look at something else. `esc` leaves the run
// view without stopping the agent, because a run takes minutes and being
// trapped watching it would make delegation useless for anything but the task
// in front of you. The matrix marks the running task; `D` on it comes back.

// agentEventMsg carries one event from a live run.
type agentEventMsg struct {
	seq   int
	event agent.Event
}

// agentDoneMsg says the run's event stream has closed.
type agentDoneMsg struct{ seq int }

// agentStartedMsg hands the model a run that Start has just produced. Start
// blocks on exec, so it happens in a command rather than in Update.
type agentStartedMsg struct {
	seq  int
	run  *agent.Run
	spec agent.Spec
	task task.Task
	err  error
}

// planSavedMsg carries the outcome of storing a drafted plan.
type planSavedMsg struct {
	data store.Data
	task task.Task
	err  error
}

// run is the state of the one run the TUI allows at a time.
//
// One at a time on purpose: several would need per-task run state, a way to
// choose between them, and a rule for what the matrix shows when two are going
// at once. A person watching a delegated task is watching one thing.
type run struct {
	// seq identifies this run, so events from a canceled one that are already
	// in flight are ignored rather than appearing in its successor's
	// transcript.
	seq     int
	handle  *agent.Run
	taskID  int
	title   string
	mode    agent.Mode
	lines   []string
	cursor  int
	follow  bool // whether the view is pinned to the newest line
	done    bool
	failed  bool
	plan    string // the drafted plan, for a ModePlan run
	cost    float64
	model   string
	effort  string // the level this run settled on, and why, for the header
	why     string
	started bool
}

// startAgent launches a run for the selected task.
func (m *Model) startAgent(mode agent.Mode) tea.Cmd {
	t, ok := m.selected()
	if !ok {
		return nil
	}
	if m.run != nil && !m.run.done {
		m.status = "a run is already going; press D to watch it, or ctrl+c there to stop it"
		return nil
	}
	if mode == agent.ModeExecute {
		// The gate, read fresh rather than from m.data: consent belongs to the
		// file, and `ike agent disable` in another terminal must take effect
		// here without waiting for a poll.
		enabled, err := m.store.AgentEnabled()
		if err != nil {
			m.status = err.Error()
			return nil
		}
		if !enabled {
			m.status = "delegation is off · run `ike agent enable` to allow it"
			return nil
		}
	}

	t, ok = m.ensureDir(t)
	if !ok {
		return nil
	}
	dir := t.Dir

	plan, err := m.store.Plan(t.ID)
	if err != nil {
		m.status = err.Error()
		return nil
	}

	m.runSeq++
	seq := m.runSeq
	// The TUI has no --effort of its own, so this is always the recommendation.
	// Resolved here rather than read back off the spec so the header can say why
	// as well as what: the reason is not part of a command line.
	level, why := agent.ResolveEffort(mode, "", plan != "")
	m.run = &run{
		seq:    seq,
		taskID: t.ID,
		title:  t.DisplayTitle(),
		mode:   mode,
		effort: level,
		why:    why,
		follow: true,
	}
	m.mode = modeRun
	m.status = ""

	spec := agent.Spec{
		Mode:     mode,
		Title:    t.Title,
		Quadrant: m.data.Labels.Of(t.Quadrant),
		Plan:     plan,
		Dir:      dir,
	}
	captured := t
	return func() tea.Msg {
		r, err := agent.Start(context.Background(), spec)
		return agentStartedMsg{seq: seq, run: r, spec: spec, task: captured, err: err}
	}
}

// sessionEndedMsg says an interactive session has exited and the TUI has the
// terminal back.
type sessionEndedMsg struct {
	taskID int
	err    error
}

// draftAdoptedMsg carries a plan an interactive session left behind.
type draftAdoptedMsg struct {
	data store.Data
	task task.Task
	got  bool
	err  error
}

// startSession hands the terminal to an interactive agent on the selected task.
//
// tea.ExecProcess is what makes this work: it releases the terminal, runs the
// command with it, and restores the TUI when the command exits — so `c` feels
// like stepping out of ike and back in, with the matrix exactly as you left it.
// The command's streams are left nil deliberately, because that is the signal
// for ExecProcess to wire the real terminal to them.
func (m *Model) startSession(mode agent.Mode) tea.Cmd {
	t, ok := m.selected()
	if !ok {
		return nil
	}
	if m.run != nil && !m.run.done {
		// The terminal is about to belong to something else, and a streaming
		// run drawing into it would corrupt both.
		m.status = "a run is going; stop it with ctrl+c in the run view first"
		return nil
	}
	if mode == agent.ModeExecute {
		enabled, err := m.store.AgentEnabled()
		if err != nil {
			m.status = err.Error()
			return nil
		}
		if !enabled {
			m.status = "delegation is off · run `ike agent enable` to allow it"
			return nil
		}
	}

	t, ok = m.ensureDir(t)
	if !ok {
		return nil
	}
	plan, err := m.store.Plan(t.ID)
	if err != nil {
		m.status = err.Error()
		return nil
	}

	resume := t.HasSession()
	if !resume {
		sid, err := agent.NewSessionID()
		if err != nil {
			m.status = err.Error()
			return nil
		}
		// Stored before the agent starts, so a session that ends badly is
		// still reachable next time.
		updated, d, err := m.store.SetSession(t.ID, sid)
		if !m.apply(d, err) {
			return nil
		}
		t = updated
	}

	draft, err := m.store.PlanDraftPath(t.ID)
	if err != nil {
		m.status = err.Error()
		return nil
	}

	c, err := agent.InteractiveCommand(context.Background(), agent.Session{
		Mode:      mode,
		Title:     t.Title,
		Quadrant:  m.data.Labels.Of(t.Quadrant),
		Plan:      plan,
		Dir:       t.Dir,
		SessionID: t.SessionID,
		Resume:    resume,
		DraftPath: draft,
	})
	if err != nil {
		m.status = err.Error()
		return nil
	}

	m.mode = modeNormal
	m.status = ""
	id := t.ID
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return sessionEndedMsg{taskID: id, err: err}
	})
}

// ensureDir resolves and stores a task's working directory if it has none.
func (m *Model) ensureDir(t task.Task) (task.Task, bool) {
	if t.Dir != "" {
		return t, true
	}
	cwd, err := os.Getwd()
	if err != nil {
		m.status = err.Error()
		return t, false
	}
	// Remembered, so the task stays attached to the project rather than to
	// wherever this particular ike was started.
	updated, d, err := m.store.SetDir(t.ID, "the current directory", cwd)
	if !m.apply(d, err) {
		return t, false
	}
	return updated, true
}

// waitForEvent reads one event and re-issues itself, which is how a channel is
// consumed in Bubble Tea without the model being touched off the update loop.
func waitForEvent(seq int, r *agent.Run) tea.Cmd {
	return func() tea.Msg {
		e, ok := <-r.Events()
		if !ok {
			return agentDoneMsg{seq: seq}
		}
		return agentEventMsg{seq: seq, event: e}
	}
}

// handleAgentMsg folds a run message into the model.
func (m Model) handleAgentMsg(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case agentStartedMsg:
		if m.run == nil || m.run.seq != msg.seq {
			// A run canceled before it started. Reap it rather than leaving a
			// process nobody is reading.
			if msg.run != nil {
				msg.run.Cancel()
			}
			return m, nil, true
		}
		if msg.err != nil {
			m.run.done, m.run.failed = true, true
			m.run.lines = append(m.run.lines, wrapNote(msg.err.Error()))
			m.pinCursor()
			return m, nil, true
		}
		m.run.handle = msg.run
		m.run.started = true
		return m, waitForEvent(msg.seq, msg.run), true

	case agentEventMsg:
		if m.run == nil || m.run.seq != msg.seq {
			return m, nil, true
		}
		m.absorb(msg.event)
		m.pinCursor()
		return m, waitForEvent(msg.seq, m.run.handle), true

	case agentDoneMsg:
		if m.run == nil || m.run.seq != msg.seq {
			return m, nil, true
		}
		m.run.done = true
		if m.run.mode == agent.ModePlan && !m.run.failed {
			return m, m.savePlanCmd(), true
		}
		m.pinCursor()
		return m, nil, true

	case sessionEndedMsg:
		// Back from the conversation. The store may have moved a long way while
		// the terminal was elsewhere — the agent can have run ike itself — so
		// re-read rather than trusting what was on screen before.
		if d, err := m.store.Load(); err == nil {
			m.followCurrentSpace(&d)
			m.refresh(d)
		}
		if msg.err != nil {
			// Quitting out of an agent is the ordinary ending and is not
			// reliably distinguishable from a real failure by exit status, so
			// this is reported without being treated as one.
			m.status = "session ended: " + task.SanitizeDisplay(msg.err.Error())
		}
		id := msg.taskID
		s := m.store
		return m, func() tea.Msg {
			t, d, got, err := s.AdoptPlanDraft(id)
			return draftAdoptedMsg{data: d, task: t, got: got, err: err}
		}, true

	case draftAdoptedMsg:
		if msg.err != nil {
			m.status = msg.err.Error()
			return m, nil, true
		}
		if !msg.got {
			return m, nil, true
		}
		m.refresh(msg.data)
		m.status = "attached the plan you agreed on to " + msg.task.DisplayTitle()
		return m, nil, true

	case planSavedMsg:
		if msg.err != nil {
			m.status = msg.err.Error()
			if m.run != nil {
				m.run.lines = append(m.run.lines, wrapNote(msg.err.Error()))
				m.pinCursor()
			}
			return m, nil, true
		}
		m.refresh(msg.data)
		if m.run != nil {
			m.run.lines = append(m.run.lines, "", "  · plan attached to task "+fmt.Sprint(msg.task.ID))
			m.pinCursor()
		}
		m.status = "plan attached to " + msg.task.DisplayTitle()
		return m, nil, true
	}
	return m, nil, false
}

// savePlanCmd stores the plan a planning run produced.
func (m *Model) savePlanCmd() tea.Cmd {
	body := strings.TrimSpace(m.run.plan)
	id := m.run.taskID
	s := m.store
	if body == "" {
		m.run.failed = true
		m.run.lines = append(m.run.lines, wrapNote("the agent finished without producing a plan"))
		m.pinCursor()
		return nil
	}
	return func() tea.Msg {
		t, d, err := s.SetPlan(id, body)
		return planSavedMsg{data: d, task: t, err: err}
	}
}

// absorb turns one event into transcript lines.
//
// Every string here has already been through task.SanitizeBlock, in
// internal/agent, at the single point everything leaves that package — so no
// call site has to remember to, and the CLI and the TUI cannot disagree about
// whether it happened.
func (m *Model) absorb(e agent.Event) {
	r := m.run
	switch e.Kind {
	case agent.KindStarted:
		r.model = e.Model
		if e.Model != "" {
			r.lines = append(r.lines, "  · "+e.Model)
		}
	case agent.KindThinking:
		r.lines = append(r.lines, "  · "+firstLine(e.Text))
	case agent.KindTool:
		r.lines = append(r.lines, "  → "+e.Tool)
	case agent.KindText:
		r.lines = append(r.lines, splitLines(e.Text)...)
	case agent.KindResult:
		r.cost = e.CostUSD
		r.failed = e.IsError
		if r.mode == agent.ModePlan {
			// The plan is the run's result rather than everything it said, so
			// it is kept whole here instead of being reassembled from the
			// transcript.
			r.plan = e.Text
		}
		if e.IsError {
			r.lines = append(r.lines, "", wrapNote("the run did not succeed: "+firstLine(e.Text)))
		}
	case agent.KindError:
		r.failed = true
		r.lines = append(r.lines, "", wrapNote(e.Text))
	}
}

// pinCursor keeps the view on the newest line while it is following.
func (m *Model) pinCursor() {
	if m.run != nil && m.run.follow {
		m.run.cursor = max(len(m.run.lines)-1, 0)
	}
}

// handleRunKey handles keys in the run view.
func (m Model) handleRunKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	r := m.run
	switch {
	case key.Matches(msg, keys.Cancel), key.Matches(msg, keys.Agent):
		// Detach. The run keeps going and keeps accumulating, because a run
		// takes minutes and there is nothing to watch for most of them.
		m.mode = modeNormal
		if r != nil && !r.done {
			m.status = fmt.Sprintf("still running: %s · D to watch it again", r.title)
		}
		return m, nil

	case key.Matches(msg, keys.Quit):
		// ctrl+c and q both reach here. In the run view they mean "stop this",
		// not "quit ike" — a run is the one thing on screen that a stray q
		// should not throw away silently.
		if r != nil && !r.done {
			r.stop()
			m.status = "run canceled"
			return m, nil
		}
		m.mode = modeNormal
		return m, nil
	}

	if r != nil && moveCursor(&r.cursor, len(r.lines), msg) {
		// Scrolling back detaches from the tail; returning to the last line
		// re-follows, so a glance at earlier output does not permanently stop
		// the view from advancing.
		r.follow = r.cursor >= len(r.lines)-1
		return m, nil
	}
	return m, nil
}

// stop cancels a run, if it has actually started. A run whose Start has not
// returned yet is caught by the seq check in handleAgentMsg.
func (r *run) stop() {
	r.done = true
	if r.handle != nil {
		r.handle.Cancel()
	}
}

// runView renders the transcript through the shared full-screen list, so the
// scroll-window arithmetic has one implementation — the reason listView exists.
// A cursor pinned to the last row is exactly "follow the tail", and moveCursor
// gives scrollback for nothing.
func (m Model) renderRun() string {
	r := m.run
	if r == nil {
		return ""
	}
	verb := "delegating"
	if r.mode == agent.ModePlan {
		verb = "planning"
	}

	state := "running…"
	switch {
	case r.failed:
		state = "failed"
	case r.done && r.cost > 0:
		state = fmt.Sprintf("done ($%.2f)", r.cost)
	case r.done:
		state = "done"
	}

	// renderList derives the chrome it has to pay for from this header, so
	// adding the effort line here does not cost the transcript a row off the
	// bottom of the terminal.
	dim := lipgloss.NewStyle().Foreground(m.dimColor())
	header := []string{
		dim.Render(fmt.Sprintf("%s · %s", state, r.title)),
		dim.Render(effortNote(r.effort, r.why)),
		"",
	}

	// Every row starts with a two-space gutter, which is what renderList
	// replaces with the cursor mark.
	rows := make([]string, len(r.lines))
	for i, l := range r.lines {
		rows[i] = "  " + l
	}

	hint := "esc detach · ctrl+c stop · j/k scroll"
	if r.done {
		hint = "esc/q back · j/k scroll"
	}
	return m.renderList(listView{
		title:  strings.ToUpper(verb[:1]) + verb[1:] + " task " + fmt.Sprint(r.taskID),
		header: header,
		rows:   rows,
		empty:  "waiting for the agent…",
		cursor: r.cursor,
		hint:   hint,
	})
}

// renderPlan shows the plan attached to the selected task.
func (m Model) renderPlan() string {
	rows := make([]string, len(m.planLines))
	for i, l := range m.planLines {
		rows[i] = "  " + l
	}
	return m.renderList(listView{
		title:  "Plan for task " + fmt.Sprint(m.planTask),
		rows:   rows,
		empty:  "no plan attached",
		cursor: m.planCursor,
		hint:   "P draft with an agent · c talk it through · D delegate · j/k scroll · esc/q back",
	})
}

// handlePlanKey handles keys in the plan view.
func (m Model) handlePlanKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Cancel), key.Matches(msg, keys.Quit), key.Matches(msg, keys.Plan):
		m.mode = modeNormal
		return m, nil

	case key.Matches(msg, keys.DraftPlan):
		m.mode = modeNormal
		return m, m.startAgent(agent.ModePlan)

	case key.Matches(msg, keys.Chat):
		m.mode = modeNormal
		return m, m.startSession(agent.ModePlan)

	case key.Matches(msg, keys.Agent):
		m.mode = modeNormal
		return m, m.startAgent(agent.ModeExecute)
	}
	moveCursor(&m.planCursor, len(m.planLines), msg)
	return m, nil
}

// openPlan reads the selected task's plan into the plan view.
func (m *Model) openPlan() {
	t, ok := m.selected()
	if !ok {
		return
	}
	body, err := m.store.Plan(t.ID)
	if err != nil {
		m.status = err.Error()
		return
	}
	m.planTask = t.ID
	m.planCursor = 0
	m.planLines = nil
	if body != "" {
		m.planLines = splitLines(body)
	}
	m.mode = modePlan
	m.status = ""
}

// splitLines breaks a block into transcript rows, dropping a trailing empty
// line so a body ending in a newline does not add a blank row.
func splitLines(s string) []string {
	return strings.Split(strings.TrimRight(s, "\n"), "\n")
}

// firstLine keeps a long block to one row. Thinking and failure summaries are
// shown as a sign of what is happening rather than as something to read in full.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	const maxRunes = 120
	if len([]rune(s)) > maxRunes {
		s = string([]rune(s)[:maxRunes]) + "…"
	}
	return s
}

func wrapNote(s string) string { return "  ! " + firstLine(s) }

// effortNote says what the run settled on and why, matching the line the CLI
// prints so the two frontends describe a run the same way.
func effortNote(level, why string) string {
	if why == "" {
		return "effort " + level
	}
	return "effort " + level + " — " + why
}
