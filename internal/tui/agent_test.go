package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/jonascript/ike/internal/agent"
	"github.com/jonascript/ike/internal/store"
	"github.com/jonascript/ike/internal/task"
)

// These tests never start a process. Everything below the key handling is
// internal/agent's job and is tested there; what matters here is that events
// fold into the model correctly and that the two views behave — which is
// exactly what feeding synthetic messages through Update checks.

// send pushes a non-key message through Update, the way a tea.Cmd's result
// would arrive.
func send(t *testing.T, m Model, msg tea.Msg) Model {
	t.Helper()
	next, _ := m.Update(msg)
	return next.(Model)
}

// running puts the model into a live run on task id, without a process.
func running(m Model, id int, mode agent.Mode) Model {
	m.runSeq++
	m.run = &run{seq: m.runSeq, taskID: id, title: "a task", mode: mode, follow: true}
	m.mode = modeRun
	return m
}

func event(seq int, e agent.Event) agentEventMsg { return agentEventMsg{seq: seq, event: e} }

// Delegating without permission must refuse, and say how to grant it.
func TestDelegateRefusedWhenGateIsOff(t *testing.T) {
	m, s := testModel(t)
	s.Add("ship v2", task.Do)
	m.refreshFromStore(t)

	m = press(t, m, "D")

	if m.mode != modeNormal {
		t.Errorf("mode = %v, want to have stayed on the matrix", m.mode)
	}
	if m.run != nil {
		t.Error("a run was started with the gate off")
	}
	if !strings.Contains(m.status, "ike agent enable") {
		t.Errorf("status = %q, it must name the command to run", m.status)
	}
}

// Drafting a plan is read-only, so it is deliberately not gated. It should get
// as far as trying to start — which fails here only because there is no binary.
func TestDraftingAPlanIsNotGated(t *testing.T) {
	m, s := testModel(t)
	s.Add("ship v2", task.Do)
	m.refreshFromStore(t)
	t.Setenv("IKE_AGENT_CMD", "")
	t.Setenv("PATH", t.TempDir())

	m = press(t, m, "P")

	if m.run == nil {
		t.Fatal("P should have begun a planning run without needing the gate")
	}
	if m.mode != modeRun {
		t.Errorf("mode = %v, want modeRun", m.mode)
	}
}

func TestTranscriptAccumulates(t *testing.T) {
	m, s := testModel(t)
	s.Add("ship v2", task.Do)
	m.refreshFromStore(t)
	m = running(m, 1, agent.ModeExecute)
	seq := m.run.seq

	m = send(t, m, event(seq, agent.Event{Kind: agent.KindStarted, Model: "claude-opus-5"}))
	m = send(t, m, event(seq, agent.Event{Kind: agent.KindText, Text: "Working on it."}))
	m = send(t, m, event(seq, agent.Event{Kind: agent.KindTool, Tool: "Read"}))
	m = send(t, m, event(seq, agent.Event{Kind: agent.KindResult, Text: "done", CostUSD: 0.05}))
	m = send(t, m, agentDoneMsg{seq: seq})

	out := m.render()
	for _, want := range []string{"claude-opus-5", "Working on it.", "Read", "$0.05"} {
		if !strings.Contains(out, want) {
			t.Errorf("the run view does not show %q:\n%s", want, out)
		}
	}
	if !m.run.done {
		t.Error("the run should be marked done")
	}
}

// A text block spanning several lines becomes several rows, or a long answer
// would be one unreadable line clipped at the terminal width.
func TestMultiLineTextBecomesSeveralRows(t *testing.T) {
	m, _ := testModel(t)
	m = running(m, 1, agent.ModeExecute)
	before := len(m.run.lines)

	m = send(t, m, event(m.run.seq, agent.Event{Kind: agent.KindText, Text: "one\ntwo\nthree"}))

	if got := len(m.run.lines) - before; got != 3 {
		t.Errorf("added %d rows, want 3", got)
	}
}

// esc leaves the run view without stopping the agent — a run takes minutes and
// being trapped watching it would make delegation useless.
func TestEscapeDetachesWithoutStopping(t *testing.T) {
	m, s := testModel(t)
	s.Add("ship v2", task.Do)
	m.refreshFromStore(t)
	m = running(m, 1, agent.ModeExecute)
	seq := m.run.seq

	m = press(t, m, "esc")

	if m.mode != modeNormal {
		t.Errorf("mode = %v, want to be back on the matrix", m.mode)
	}
	if m.run == nil || m.run.done {
		t.Fatal("esc must not stop the run")
	}
	if !strings.Contains(m.status, "still running") {
		t.Errorf("status = %q, it should say the run is still going", m.status)
	}

	// The matrix marks the running task.
	if !strings.Contains(m.render(), strings.TrimSpace(runMark)) {
		t.Error("the matrix should mark the task that is still running")
	}

	// Events keep arriving while detached, and the transcript keeps growing.
	m = send(t, m, event(seq, agent.Event{Kind: agent.KindText, Text: "still working"}))
	if len(m.run.lines) == 0 {
		t.Error("a detached run should keep accumulating")
	}

	// D reattaches rather than starting a second run.
	m = press(t, m, "D")
	if m.mode != modeRun {
		t.Errorf("mode = %v, want D to reattach", m.mode)
	}
	if m.run.seq != seq {
		t.Error("D started a new run instead of reattaching")
	}
	if !strings.Contains(m.render(), "still working") {
		t.Error("reattaching should show what arrived while detached")
	}
}

// ctrl+c in the run view stops the run rather than quitting ike — a run is the
// one thing on screen a stray keystroke should not throw ike away over.
func TestCtrlCStopsTheRunRatherThanQuitting(t *testing.T) {
	m, _ := testModel(t)
	m = running(m, 1, agent.ModeExecute)

	next, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	m = next.(Model)

	if cmd != nil {
		t.Error("ctrl+c in a run must not quit ike")
	}
	if !m.run.done {
		t.Error("ctrl+c should stop the run")
	}
	if m.status != "run canceled" {
		t.Errorf("status = %q", m.status)
	}
}

// Events already in flight when a run is canceled must not appear in its
// successor's transcript.
func TestLateEventsFromAnOldRunAreIgnored(t *testing.T) {
	m, s := testModel(t)
	s.Add("ship v2", task.Do)
	m.refreshFromStore(t)
	m = running(m, 1, agent.ModeExecute)
	old := m.run.seq

	// A second run replaces the first.
	m = running(m, 1, agent.ModeExecute)
	fresh := m.run.seq
	if old == fresh {
		t.Fatal("the two runs should have different sequence numbers")
	}

	m = send(t, m, event(old, agent.Event{Kind: agent.KindText, Text: "from the old run"}))
	if len(m.run.lines) != 0 {
		t.Errorf("the old run's event leaked into the new transcript: %v", m.run.lines)
	}

	m = send(t, m, agentDoneMsg{seq: old})
	if m.run.done {
		t.Error("the old run's completion ended the new one")
	}
}

// A planning run stores its result, which is the plan.
func TestPlanningRunAttachesThePlan(t *testing.T) {
	m, s := testModel(t)
	s.Add("ship v2", task.Do)
	m.refreshFromStore(t)
	m = running(m, 1, agent.ModePlan)
	seq := m.run.seq

	const plan = "## Goal\n\nShip it.\n\n- step one"
	m = send(t, m, event(seq, agent.Event{Kind: agent.KindResult, Text: plan}))
	next, cmd := m.Update(agentDoneMsg{seq: seq})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("finishing a planning run should save the plan")
	}
	m = send(t, m, cmd())

	body, err := s.Plan(1)
	if err != nil {
		t.Fatal(err)
	}
	if body != plan {
		t.Errorf("stored plan = %q, want %q", body, plan)
	}
	// The run view stays up when the run finishes — the transcript and the
	// "plan attached" note are the point of it. esc goes back to the matrix,
	// which now marks the task as planned.
	if !strings.Contains(m.render(), "plan attached") {
		t.Error("the run view should note that the plan was attached")
	}
	m = press(t, m, "esc")
	if !strings.Contains(m.render(), strings.TrimSpace(planMark)) {
		t.Error("a planned task should be marked in the matrix")
	}
}

// A planning run that produces nothing must not attach an empty plan.
func TestPlanningRunWithNoResultAttachesNothing(t *testing.T) {
	m, s := testModel(t)
	s.Add("ship v2", task.Do)
	m.refreshFromStore(t)
	m = running(m, 1, agent.ModePlan)
	seq := m.run.seq

	m = send(t, m, event(seq, agent.Event{Kind: agent.KindResult, Text: "   "}))
	m = send(t, m, agentDoneMsg{seq: seq})

	if body, _ := s.Plan(1); body != "" {
		t.Errorf("an empty plan was attached: %q", body)
	}
	if !m.run.failed {
		t.Error("the run should be marked failed")
	}
}

// A failed run is not silently indistinguishable from a successful one.
func TestFailedRunIsMarked(t *testing.T) {
	m, _ := testModel(t)
	m = running(m, 1, agent.ModeExecute)
	seq := m.run.seq

	m = send(t, m, event(seq, agent.Event{Kind: agent.KindResult, Text: "could not do it", IsError: true}))
	m = send(t, m, agentDoneMsg{seq: seq})

	if !m.run.failed {
		t.Error("the run should be marked failed")
	}
	if out := m.render(); !strings.Contains(out, "failed") {
		t.Errorf("the run view should say it failed:\n%s", out)
	}
}

func TestPlanView(t *testing.T) {
	m, s := testModel(t)
	s.Add("ship v2", task.Do)
	if _, _, err := s.SetPlan(1, "## Goal\n\nShip it."); err != nil {
		t.Fatal(err)
	}
	m.refreshFromStore(t)

	m = press(t, m, "p")
	if m.mode != modePlan {
		t.Fatalf("mode = %v, want modePlan", m.mode)
	}
	out := m.render()
	if !strings.Contains(out, "## Goal") || !strings.Contains(out, "Ship it.") {
		t.Errorf("the plan view does not show the plan:\n%s", out)
	}

	m = press(t, m, "esc")
	if m.mode != modeNormal {
		t.Errorf("mode = %v, want to be back on the matrix", m.mode)
	}
}

func TestPlanViewWithNoPlan(t *testing.T) {
	m, s := testModel(t)
	s.Add("ship v2", task.Do)
	m.refreshFromStore(t)

	m = press(t, m, "p")
	if out := m.render(); !strings.Contains(out, "no plan attached") {
		t.Errorf("the plan view should say there is none:\n%s", out)
	}
}

// Scrolling back stops the view chasing the tail, and returning to the last
// line resumes it — so a glance at earlier output is not permanent.
func TestScrollbackDetachesAndReattachesTheTail(t *testing.T) {
	m, _ := testModel(t)
	m = running(m, 1, agent.ModeExecute)
	seq := m.run.seq
	for i := range 10 {
		m = send(t, m, event(seq, agent.Event{Kind: agent.KindText, Text: string(rune('a' + i))}))
	}
	if !m.run.follow || m.run.cursor != len(m.run.lines)-1 {
		t.Fatalf("the view should start pinned to the newest line, cursor=%d", m.run.cursor)
	}

	m = press(t, m, "k")
	if m.run.follow {
		t.Error("scrolling back should stop the view following the tail")
	}
	was := m.run.cursor
	m = send(t, m, event(seq, agent.Event{Kind: agent.KindText, Text: "new line"}))
	if m.run.cursor != was {
		t.Error("a new event moved the cursor while scrolled back")
	}

	m = press(t, m, "j", "j")
	if !m.run.follow {
		t.Error("returning to the last line should resume following")
	}
}

// The run dies with the process, so quitting stops it deliberately rather than
// orphaning an agent with nothing reading it.
func TestQuittingStopsALiveRun(t *testing.T) {
	m, _ := testModel(t)
	m = running(m, 1, agent.ModeExecute)
	m.mode = modeNormal

	next, cmd := m.Update(keyFor("q"))
	m = next.(Model)

	if cmd == nil {
		t.Error("q on the matrix should still quit")
	}
	if !m.run.done {
		t.Error("quitting should stop a live run")
	}
}

// The TUI has no --effort of its own, so what it shows is always ike's own
// choice — which makes the header the only place a person can see it.
func TestRunViewShowsTheChosenEffort(t *testing.T) {
	m, s := testModel(t)
	s.Add("ship v2", task.Do)
	if _, err := s.SetAgentEnabled(true); err != nil {
		t.Fatal(err)
	}
	// No binary, so the run fails to start — but startAgent settles the effort
	// before it gets that far, which is all this is about.
	t.Setenv("IKE_AGENT_CMD", "")
	t.Setenv("PATH", t.TempDir())
	m.refreshFromStore(t)

	m = press(t, m, "D")
	if m.run == nil {
		t.Fatal("D should have begun a run")
	}
	if m.run.effort != "high" || m.run.why != "no plan to follow" {
		t.Errorf("effort = %q (%q), want high with the no-plan reason", m.run.effort, m.run.why)
	}
	if out := m.render(); !strings.Contains(out, "effort high — no plan to follow") {
		t.Errorf("run view = %q, want the effort line in the header", out)
	}

	// Attach a plan and the next run is follow-through instead.
	if _, _, err := s.SetPlan(1, "## step one"); err != nil {
		t.Fatal(err)
	}
	m.run = nil
	m.mode = modeNormal
	m.refreshFromStore(t)

	m = press(t, m, "D")
	if m.run == nil {
		t.Fatal("D should have begun a second run")
	}
	if m.run.effort != "medium" || m.run.why != "following an attached plan" {
		t.Errorf("effort = %q (%q), want the step down for an attached plan", m.run.effort, m.run.why)
	}
	if out := m.render(); !strings.Contains(out, "effort medium — following an attached plan") {
		t.Errorf("run view = %q, want the effort line in the header", out)
	}
}

// A second run while one is going is refused rather than silently replacing it.
func TestASecondRunIsRefused(t *testing.T) {
	m, s := testModel(t)
	s.Add("ship v2", task.Do)
	s.Add("another", task.Do)
	m.refreshFromStore(t)
	if _, err := s.SetAgentEnabled(true); err != nil {
		t.Fatal(err)
	}
	m.refreshFromStore(t)

	m = running(m, 1, agent.ModeExecute)
	seq := m.run.seq
	m.mode = modeNormal
	m.cursor[task.Do] = 1 // a different task

	m = press(t, m, "P")
	if m.run.seq != seq {
		t.Error("a second run replaced the live one")
	}
	if !strings.Contains(m.status, "already going") {
		t.Errorf("status = %q, it should say a run is already going", m.status)
	}
}

// The ambient footer line tells you which state the gate is in, and how to
// change it — the delegation counterpart of the MCP line.
func TestAgentHelpLineReflectsTheGate(t *testing.T) {
	m, s := testModel(t)
	s.Add("ship v2", task.Do)
	m.refreshFromStore(t)
	m.showHelp = true

	if out := m.render(); !strings.Contains(out, "ike agent enable") {
		t.Errorf("help should say how to turn delegation on:\n%s", out)
	}
	if _, err := s.SetAgentEnabled(true); err != nil {
		t.Fatal(err)
	}
	m.refreshFromStore(t)
	if out := m.render(); !strings.Contains(out, "ike agent disable") {
		t.Errorf("help should say how to turn delegation off:\n%s", out)
	}
}

// fakeAgentEnv names the file this binary replays when standing in for the
// claude CLI. See TestMain.
const fakeAgentEnv = "IKE_TUI_FAKE_STREAM"

// fakeAgentCLI points agent.Start at this test binary, replaying events.
func fakeAgentCLI(t *testing.T, events ...string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "stream.jsonl")
	if err := os.WriteFile(p, []byte(strings.Join(events, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("IKE_AGENT_CMD", exe)
	t.Setenv(fakeAgentEnv, p)
}

// pump runs the Bubble Tea command loop by hand: execute a command, feed its
// message back through Update, repeat until nothing is left.
//
// The tests above inject messages directly, which checks that events fold into
// the model correctly but not that the commands actually produce them. This
// closes that gap — it exercises startAgent's exec, waitForEvent re-issuing
// itself for each event, and savePlanCmd, against a real subprocess.
func pump(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	for i := 0; cmd != nil; i++ {
		if i > 200 {
			t.Fatal("the command loop did not settle")
		}
		msg := cmd()
		if msg == nil {
			return m
		}
		var next tea.Model
		next, cmd = m.Update(msg)
		m = next.(Model)
	}
	return m
}

// A planning run, end to end through the real command plumbing: exec the
// binary, stream its events, attach the plan it produced.
func TestPlanningRunEndToEnd(t *testing.T) {
	m, s := testModel(t)
	s.Add("ship v2", task.Do)
	m.refreshFromStore(t)
	dir := t.TempDir()
	if _, _, err := s.SetDir(1, "test", dir); err != nil {
		t.Fatal(err)
	}
	m.refreshFromStore(t)

	fakeAgentCLI(t,
		`{"type":"system","subtype":"init","session_id":"abc","model":"claude-opus-5"}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"Exploring."}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Read"}]}}`,
		`{"type":"result","subtype":"success","is_error":false,"result":"## Goal\n\nShip it.","total_cost_usd":0.03}`,
	)

	cmd := m.startAgent(agent.ModePlan)
	if cmd == nil {
		t.Fatal("startAgent returned no command")
	}
	m = pump(t, m, cmd)

	out := m.render()
	for _, want := range []string{"claude-opus-5", "Exploring.", "Read", "$0.03", "plan attached"} {
		if !strings.Contains(out, want) {
			t.Errorf("the run view does not show %q:\n%s", want, out)
		}
	}
	if body, _ := s.Plan(1); body != "## Goal\n\nShip it." {
		t.Errorf("stored plan = %q", body)
	}
}

// A delegated run, end to end, including the gate and the attached plan
// reaching the agent's command line.
func TestDelegateRunEndToEnd(t *testing.T) {
	m, s := testModel(t)
	s.Add("ship v2", task.Do)
	dir := t.TempDir()
	if _, _, err := s.SetDir(1, "test", dir); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.SetPlan(1, "## The plan"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetAgentEnabled(true); err != nil {
		t.Fatal(err)
	}
	m.refreshFromStore(t)

	fakeAgentCLI(t,
		`{"type":"system","subtype":"init","session_id":"abc","model":"claude-opus-5"}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Write"}]}}`,
		`{"type":"result","subtype":"success","is_error":false,"result":"I created the file.","total_cost_usd":0.10}`,
	)

	m = pump(t, m, m.startAgent(agent.ModeExecute))

	if m.run == nil || !m.run.done {
		t.Fatal("the run did not finish")
	}
	if m.run.failed {
		t.Error("a successful run was marked failed")
	}
	out := m.render()
	if !strings.Contains(out, "Write") {
		t.Errorf("the run view does not show the tool use:\n%s", out)
	}
	if !strings.Contains(out, "$0.10") {
		t.Errorf("the run view does not show the cost, so the result event never landed:\n%s", out)
	}
	// Delegating does not complete the task — reading what it did and deciding
	// is the point.
	d, _ := s.Load()
	if len(d.Tasks) != 1 || len(d.Archive) != 0 {
		t.Error("a delegated run should not complete the task by itself")
	}
}

// `c` hands the terminal over, and the conversation is pinned to the task so
// pressing it again resumes rather than briefing a new agent.
func TestChatPinsThenResumesTheConversation(t *testing.T) {
	m, s := testModel(t)
	s.Add("ship v2", task.Do)
	dir := t.TempDir()
	if _, _, err := s.SetDir(1, "test", dir); err != nil {
		t.Fatal(err)
	}
	m.refreshFromStore(t)
	fakeAgentCLI(t)

	cmd := m.startSession(agent.ModePlan)
	if cmd == nil {
		t.Fatal("c should have started a session")
	}
	// Back on the matrix, because the terminal is about to belong to claude —
	// there is no ike view to be in while it does.
	if m.mode != modeNormal {
		t.Errorf("mode = %v, want modeNormal during a handover", m.mode)
	}

	d, _ := s.Load()
	sid := d.Tasks[0].SessionID
	if sid == "" {
		t.Fatal("the session should be stored before the agent starts, or it is unresumable")
	}
	// A task with a conversation is marked, so you can see there is one to
	// pick up.
	m.refreshFromStore(t)
	if !strings.Contains(m.render(), strings.TrimSpace(chatMark)) {
		t.Error("a task with a conversation should be marked in the matrix")
	}

	// Pressing it again resumes the same one.
	m2 := m
	if cmd := m2.startSession(agent.ModePlan); cmd == nil {
		t.Fatal("c should work a second time")
	}
	d, _ = s.Load()
	if d.Tasks[0].SessionID != sid {
		t.Error("a second visit started a different conversation")
	}
}

// Supervising still needs permission; being at the terminal does not remove the
// decision to let ike start an agent that edits files.
func TestSupervisingStillNeedsTheGate(t *testing.T) {
	m, s := testModel(t)
	s.Add("ship v2", task.Do)
	if _, _, err := s.SetDir(1, "test", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	m.refreshFromStore(t)
	fakeAgentCLI(t)

	if cmd := m.startSession(agent.ModeExecute); cmd != nil {
		t.Error("C should be refused with the gate off")
	}
	if !strings.Contains(m.status, "ike agent enable") {
		t.Errorf("status = %q", m.status)
	}

	// Talking a plan through is read-only, so it is not gated.
	if cmd := m.startSession(agent.ModePlan); cmd == nil {
		t.Error("c should work with the gate off — planning is read-only")
	}
}

// A streaming run owns the screen, and a session is about to own the terminal.
// Letting both happen would corrupt the display.
func TestSessionIsRefusedWhileARunIsGoing(t *testing.T) {
	m, s := testModel(t)
	s.Add("ship v2", task.Do)
	m.refreshFromStore(t)
	m = running(m, 1, agent.ModeExecute)
	m.mode = modeNormal

	if cmd := m.startSession(agent.ModePlan); cmd != nil {
		t.Error("a session should be refused while a run is streaming")
	}
	if !strings.Contains(m.status, "run is going") {
		t.Errorf("status = %q", m.status)
	}
}

// Coming back from a session adopts whatever plan was agreed, and re-reads the
// store — the agent may have changed it while the terminal was elsewhere.
func TestReturningFromASessionAdoptsThePlan(t *testing.T) {
	m, s := testModel(t)
	s.Add("ship v2", task.Do)
	m.refreshFromStore(t)

	draft, err := s.PlanDraftPath(1)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(draft, []byte("## Agreed\n\nDo the thing.\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	next, cmd := m.Update(sessionEndedMsg{taskID: 1})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("returning should look for a plan the session left")
	}
	m = send(t, m, cmd())

	if body, _ := s.Plan(1); body != "## Agreed\n\nDo the thing." {
		t.Errorf("stored plan = %q", body)
	}
	if !strings.Contains(m.status, "attached the plan") {
		t.Errorf("status = %q", m.status)
	}
	if !strings.Contains(m.render(), strings.TrimSpace(planMark)) {
		t.Error("the matrix should mark the task as planned")
	}
}

// Most conversations do not end in a plan, and that is not an error.
func TestReturningWithNoPlanIsQuiet(t *testing.T) {
	m, s := testModel(t)
	s.Add("ship v2", task.Do)
	m.refreshFromStore(t)

	next, cmd := m.Update(sessionEndedMsg{taskID: 1})
	m = next.(Model)
	m = send(t, m, cmd())

	if strings.Contains(m.status, "attached") {
		t.Errorf("status = %q, nothing was agreed", m.status)
	}
	if body, _ := s.Plan(1); body != "" {
		t.Errorf("a plan appeared from nowhere: %q", body)
	}
}

// The gate is read fresh when a run starts, not from the polled Data, so
// revoking it in another terminal takes effect without waiting for a tick.
func TestGateIsReadFreshWhenStartingARun(t *testing.T) {
	m, s := testModel(t)
	s.Add("ship v2", task.Do)
	if _, err := s.SetAgentEnabled(true); err != nil {
		t.Fatal(err)
	}
	m.refreshFromStore(t)

	// Revoked behind the model's back — no refresh, so m.data still says on.
	if _, err := store.OpenAt(s.Path()).SetAgentEnabled(false); err != nil {
		t.Fatal(err)
	}
	if !m.data.AgentAllowed {
		t.Fatal("the stale Data should still report delegation on, or this proves nothing")
	}

	m = press(t, m, "D")
	if m.run != nil {
		t.Error("the run started against a gate that had just been revoked")
	}
	if !strings.Contains(m.status, "ike agent enable") {
		t.Errorf("status = %q", m.status)
	}
}
