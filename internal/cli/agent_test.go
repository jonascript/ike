package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/jonascript/ike/internal/agent"
	"github.com/jonascript/ike/internal/store"
)

// These tests never run the real claude CLI. As in internal/agent, the fake is
// this test binary re-executed with a marker in its environment, replaying a
// canned stream — so there is no network, no cost, and no dependence on how a
// model happens to word a plan.

const (
	helperEnv     = "IKE_CLI_TEST_HELPER"
	helperFixture = "IKE_CLI_TEST_FIXTURE"
	helperExit    = "IKE_CLI_TEST_EXIT"
	helperArgs    = "IKE_CLI_TEST_ARGS"
	helperCwd     = "IKE_CLI_TEST_CWD"
	helperDraft   = "IKE_CLI_TEST_DRAFT"
)

func TestMain(m *testing.M) {
	if os.Getenv(helperEnv) == "" {
		os.Exit(m.Run())
	}
	// Record how ike invoked us, so a test can assert on the command line and
	// the working directory without reaching into internal/agent.
	if p := os.Getenv(helperArgs); p != "" {
		_ = os.WriteFile(p, []byte(strings.Join(os.Args[1:], "\x00")), 0o600)
	}
	if p := os.Getenv(helperCwd); p != "" {
		cwd, _ := os.Getwd()
		_ = os.WriteFile(p, []byte(cwd), 0o600)
	}
	// Stand in for an interactive session that agreed on a plan: write it to
	// the draft path ike named in the opening brief, the way a real agent
	// would, so the handoff is exercised end to end. Finding the path by
	// reading it back out of the prompt also checks that the brief really names
	// somewhere ike looks.
	if body := os.Getenv(helperDraft); body != "" {
		re := regexp.MustCompile(`\S+\.draft\.md`)
		for _, a := range os.Args[1:] {
			if path := re.FindString(a); path != "" {
				_ = os.WriteFile(path, []byte(body), 0o600)
			}
		}
	}
	if f := os.Getenv(helperFixture); f != "" {
		b, err := os.ReadFile(f)
		if err != nil {
			panic(err)
		}
		os.Stdout.Write(b)
	}
	if os.Getenv(helperExit) == "1" {
		os.Exit(1)
	}
	os.Exit(0)
}

// stream writes a canned agent transcript and points ike at the fake.
func fakeStream(t *testing.T, events ...string) {
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
	t.Setenv(helperEnv, "1")
	t.Setenv(helperFixture, p)
}

// planStream is a successful planning run whose result is the plan.
func planStream(plan string) []string {
	return []string{
		`{"type":"system","subtype":"init","session_id":"abc","model":"claude-opus-5"}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"Exploring."}]}}`,
		`{"type":"result","subtype":"success","is_error":false,"result":` + quote(plan) + `,"total_cost_usd":0.02}`,
	}
}

func quote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	return `"` + s + `"`
}

// withCwd runs the body in dir, since `ike delegate` with no --dir uses the
// process's own directory.
func withCwd(t *testing.T, dir string) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(old) })
}

func TestAgentGateDefaultsOffAndToggles(t *testing.T) {
	p := scratch(t)

	out := mustRunCLI(t, p, "agent", "status")
	if !strings.Contains(out, "delegation: off") {
		t.Errorf("status = %q, want delegation off by default", out)
	}

	out = mustRunCLI(t, p, "agent", "enable")
	if !strings.Contains(out, "delegation is now on") {
		t.Errorf("enable = %q", out)
	}
	// Enabling twice reports no change rather than claiming one.
	out = mustRunCLI(t, p, "agent", "enable")
	if !strings.Contains(out, "was already on") {
		t.Errorf("second enable = %q", out)
	}
	if out := mustRunCLI(t, p, "agent", "status"); !strings.Contains(out, "delegation: on") {
		t.Errorf("status = %q", out)
	}
	if out := mustRunCLI(t, p, "agent", "disable"); !strings.Contains(out, "delegation is now off") {
		t.Errorf("disable = %q", out)
	}
}

// The two gates are separate decisions and neither implies the other.
func TestAgentGateIsSeparateFromMCP(t *testing.T) {
	p := scratch(t)
	mustRunCLI(t, p, "mcp", "enable")

	if out := mustRunCLI(t, p, "agent", "status"); !strings.Contains(out, "delegation: off") {
		t.Errorf("`ike mcp enable` also turned delegation on: %q", out)
	}
	mustRunCLI(t, p, "agent", "enable")
	if out := mustRunCLI(t, p, "mcp", "status"); !strings.Contains(out, "MCP access: on") {
		t.Errorf("`ike agent enable` disturbed the MCP gate: %q", out)
	}
}

// Refusing has to say how to allow it — that is the whole job of the message.
func TestDelegateRefusedWhenGateIsOff(t *testing.T) {
	p := scratch(t)
	mustRunCLI(t, p, "add", "ship v2", "-q", "1")
	fakeStream(t, planStream("x")...)

	_, err := runCLI(t, p, "delegate", "1")
	if err == nil {
		t.Fatal("delegate should be refused with the gate off")
	}
	if !strings.Contains(err.Error(), "ike agent enable") {
		t.Errorf("error = %q, it must name the command to run", err)
	}
}

// Drafting a plan is read-only, so it deliberately does not need the gate.
func TestPlanDoesNotNeedTheGate(t *testing.T) {
	p := scratch(t)
	dir := t.TempDir()
	mustRunCLI(t, p, "add", "ship v2", "-q", "1")
	fakeStream(t, planStream("## Goal\n\nShip it.")...)

	out := mustRunCLI(t, p, "plan", "1", "--dir", dir)
	if !strings.Contains(out, "attached a plan") {
		t.Errorf("plan = %q", out)
	}
	if got := mustRunCLI(t, p, "plan", "1", "--show"); !strings.Contains(got, "Ship it.") {
		t.Errorf("--show = %q", got)
	}
}

func TestPlanRoundTripsThroughTheCLI(t *testing.T) {
	p := scratch(t)
	dir := t.TempDir()
	mustRunCLI(t, p, "add", "ship v2", "-q", "1")
	fakeStream(t, planStream("## Goal\n\nShip it.\n\n- step one")...)
	mustRunCLI(t, p, "plan", "1", "--dir", dir)

	// The plan survives as written, newlines and all — the point of
	// SanitizeBlock over SanitizeDisplay.
	out := mustRunCLI(t, p, "plan", "1", "--show")
	if !strings.Contains(out, "## Goal\n\nShip it.\n\n- step one") {
		t.Errorf("--show = %q, want the plan with its line structure", out)
	}

	// Clearing removes it.
	mustRunCLI(t, p, "plan", "1", "--clear")
	if _, err := runCLI(t, p, "plan", "1", "--show"); err == nil {
		t.Error("--show should fail once the plan is cleared")
	}
}

func TestPlanFromFile(t *testing.T) {
	p := scratch(t)
	mustRunCLI(t, p, "add", "ship v2", "-q", "1")

	src := filepath.Join(t.TempDir(), "plan.md")
	if err := os.WriteFile(src, []byte("## Written by hand\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A plan you wrote yourself needs no agent at all.
	mustRunCLI(t, p, "plan", "1", "--from-file", src)
	if out := mustRunCLI(t, p, "plan", "1", "--show"); !strings.Contains(out, "Written by hand") {
		t.Errorf("--show = %q", out)
	}
}

func TestPlanShowWithNoPlanExplainsItself(t *testing.T) {
	p := scratch(t)
	mustRunCLI(t, p, "add", "ship v2", "-q", "1")

	_, err := runCLI(t, p, "plan", "1", "--show")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "ike plan 1") {
		t.Errorf("error = %q, it should say how to draft one", err)
	}
}

// The first run remembers where it happened, so the same task run later from
// anywhere goes back to the same project.
func TestDirectoryIsRememberedFromTheFirstRun(t *testing.T) {
	p := scratch(t)
	dir := t.TempDir()
	mustRunCLI(t, p, "add", "ship v2", "-q", "1")
	fakeStream(t, planStream("a plan")...)

	mustRunCLI(t, p, "plan", "1", "--dir", dir)

	s := store.OpenAt(p)
	d, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if d.Tasks[0].Dir != dir {
		t.Errorf("Dir = %q, want %q", d.Tasks[0].Dir, dir)
	}

	// A later run needs no --dir, and lands in the same place.
	cwdFile := filepath.Join(t.TempDir(), "cwd")
	t.Setenv(helperCwd, cwdFile)
	mustRunCLI(t, p, "agent", "enable")
	mustRunCLI(t, p, "delegate", "1")

	got, err := os.ReadFile(cwdFile)
	if err != nil {
		t.Fatal(err)
	}
	// macOS temp dirs are symlinked through /private, so compare resolved.
	want, _ := filepath.EvalSymlinks(dir)
	gotResolved, _ := filepath.EvalSymlinks(string(got))
	if gotResolved != want {
		t.Errorf("the run happened in %q, want %q", gotResolved, want)
	}
}

// With no --dir and nothing stored, the current directory is used and kept —
// what makes `cd ~/dev/thing && ike delegate 3` do the obvious thing.
func TestDirectoryFallsBackToTheCurrentDirectory(t *testing.T) {
	p := scratch(t)
	dir := t.TempDir()
	mustRunCLI(t, p, "add", "ship v2", "-q", "1")
	fakeStream(t, planStream("a plan")...)
	withCwd(t, dir)

	mustRunCLI(t, p, "plan", "1")

	d, err := store.OpenAt(p).Load()
	if err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.EvalSymlinks(dir)
	got, _ := filepath.EvalSymlinks(d.Tasks[0].Dir)
	if got != want {
		t.Errorf("Dir = %q, want the current directory %q", got, want)
	}
}

func TestDelegateRejectsABadDirectory(t *testing.T) {
	p := scratch(t)
	mustRunCLI(t, p, "add", "ship v2", "-q", "1")
	mustRunCLI(t, p, "agent", "enable")
	fakeStream(t, planStream("a plan")...)

	if _, err := runCLI(t, p, "delegate", "1", "--dir", "relative/path"); err == nil {
		t.Error("a relative --dir should be refused")
	}
	if _, err := runCLI(t, p, "delegate", "1", "--dir", "~/dev"); err == nil {
		t.Error("an unexpanded ~ should be refused")
	}
	if _, err := runCLI(t, p, "delegate", "1", "--dir", filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Error("a directory that does not exist should be refused")
	}
}

// A planning run must be read-only whatever else is asked for, and an execute
// run must carry the default permission mode.
func TestRunFlagsMatchTheMode(t *testing.T) {
	p := scratch(t)
	dir := t.TempDir()
	mustRunCLI(t, p, "add", "ship v2", "-q", "1")
	mustRunCLI(t, p, "agent", "enable")
	fakeStream(t, planStream("a plan")...)

	argsFile := filepath.Join(t.TempDir(), "args")
	t.Setenv(helperArgs, argsFile)

	mustRunCLI(t, p, "plan", "1", "--dir", dir)
	if got := readArgs(t, argsFile); !strings.Contains(got, "--permission-mode plan") {
		t.Errorf("planning args = %q, want plan mode", got)
	}

	mustRunCLI(t, p, "delegate", "1")
	got := readArgs(t, argsFile)
	if !strings.Contains(got, "--permission-mode "+agent.DefaultPermissionMode) {
		t.Errorf("delegate args = %q, want the default permission mode", got)
	}
	// The attached plan is handed to the run, or delegation would ignore the
	// thing it exists to act on.
	if !strings.Contains(got, "a plan") {
		t.Errorf("delegate args = %q, want the attached plan in the prompt", got)
	}

	mustRunCLI(t, p, "delegate", "1", "--permission-mode", "manual")
	if got := readArgs(t, argsFile); !strings.Contains(got, "--permission-mode manual") {
		t.Errorf("delegate args = %q, want the override", got)
	}
}

// A mistyped mode must fail before a process starts, or it surfaces as a failed
// run rather than as the mistyped flag it is.
func TestBadPermissionModeIsRefusedBeforeRunning(t *testing.T) {
	p := scratch(t)
	dir := t.TempDir()
	mustRunCLI(t, p, "add", "ship v2", "-q", "1")
	mustRunCLI(t, p, "agent", "enable")

	argsFile := filepath.Join(t.TempDir(), "args")
	fakeStream(t, planStream("a plan")...)
	t.Setenv(helperArgs, argsFile)

	_, err := runCLI(t, p, "delegate", "1", "--dir", dir, "--permission-mode", "acceptedits")
	if err == nil {
		t.Fatal("a misspelled permission mode should be refused")
	}
	if !strings.Contains(err.Error(), "acceptEdits") {
		t.Errorf("error = %q, it should list the valid modes", err)
	}
	if _, statErr := os.Stat(argsFile); statErr == nil {
		t.Error("the agent was started despite the bad flag")
	}

	// plan would silently make a delegated run read-only.
	_, err = runCLI(t, p, "delegate", "1", "--dir", dir, "--permission-mode", "plan")
	if err == nil || !strings.Contains(err.Error(), "ike plan") {
		t.Errorf("error = %v, want it to point at `ike plan`", err)
	}
}

func readArgs(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(string(b), "\x00", " ")
}

// A failed run has to fail the command, or a script around ike would read a
// broken delegation as a successful one.
func TestFailedRunFailsTheCommand(t *testing.T) {
	p := scratch(t)
	dir := t.TempDir()
	mustRunCLI(t, p, "add", "ship v2", "-q", "1")
	mustRunCLI(t, p, "agent", "enable")
	fakeStream(t,
		`{"type":"system","subtype":"init","session_id":"abc"}`,
		`{"type":"result","subtype":"error_during_execution","is_error":true,"result":"I could not do it"}`,
	)

	_, err := runCLI(t, p, "delegate", "1", "--dir", dir)
	if err == nil {
		t.Fatal("a failed run should fail the command")
	}
	if !strings.Contains(err.Error(), "I could not do it") {
		t.Errorf("error = %q, want the agent's own reason", err)
	}
}

// A planning run that produces nothing must not attach an empty plan.
func TestPlanRefusesAnEmptyResult(t *testing.T) {
	p := scratch(t)
	dir := t.TempDir()
	mustRunCLI(t, p, "add", "ship v2", "-q", "1")
	fakeStream(t,
		`{"type":"system","subtype":"init","session_id":"abc"}`,
		`{"type":"result","subtype":"success","is_error":false,"result":"   "}`,
	)

	if _, err := runCLI(t, p, "plan", "1", "--dir", dir); err == nil {
		t.Error("an empty plan should not be attached")
	}
	if _, err := runCLI(t, p, "plan", "1", "--show"); err == nil {
		t.Error("nothing should have been attached")
	}
}

// The transcript is agent-chosen text going to a terminal. It must not be able
// to repaint the screen the run is being audited with.
func TestTranscriptIsSanitized(t *testing.T) {
	p := scratch(t)
	dir := t.TempDir()
	mustRunCLI(t, p, "add", "ship v2", "-q", "1")
	mustRunCLI(t, p, "agent", "enable")
	fakeStream(t,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"real\u001b[2K\rFAKE"}]}}`,
		`{"type":"result","subtype":"success","is_error":false,"result":"done"}`,
	)

	out := mustRunCLI(t, p, "delegate", "1", "--dir", dir)
	if strings.ContainsRune(out, 0x1b) || strings.ContainsRune(out, '\r') {
		t.Errorf("transcript = %q, still carries control characters", out)
	}
}

func TestPlanPrune(t *testing.T) {
	p := scratch(t)
	mustRunCLI(t, p, "add", "ship v2", "-q", "1")
	mustRunCLI(t, p, "plan", "1", "--from-file", writeTemp(t, "a plan"))

	// Deleting leaves the plan, so undo is not lossy.
	mustRunCLI(t, p, "rm", "1")
	if out := mustRunCLI(t, p, "plan", "--prune"); !strings.Contains(out, "1 orphaned plan") {
		t.Errorf("prune = %q", out)
	}
	if out := mustRunCLI(t, p, "plan", "--prune"); !strings.Contains(out, "0 orphaned plans") {
		t.Errorf("second prune = %q", out)
	}
}

// --prune sweeps the whole file, so pairing it with a task id is a mistake
// worth naming rather than quietly ignoring one of the two.
func TestPrunePlusIDIsRefused(t *testing.T) {
	p := scratch(t)
	mustRunCLI(t, p, "add", "ship v2", "-q", "1")
	if _, err := runCLI(t, p, "plan", "1", "--prune"); err == nil {
		t.Error("--prune with a task id should be refused")
	}
}

func TestPlanWithNoArgsExplainsItself(t *testing.T) {
	p := scratch(t)
	_, err := runCLI(t, p, "plan")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "--prune") {
		t.Errorf("error = %q, it should name both ways to call it", err)
	}
}

func writeTemp(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "plan.md")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// The conversation is a property of the task: the first visit pins a session,
// and every later one resumes it rather than briefing a new agent.
func TestInteractiveSessionIsPinnedThenResumed(t *testing.T) {
	p := scratch(t)
	dir := t.TempDir()
	mustRunCLI(t, p, "add", "ship v2", "-q", "1")
	fakeStream(t)
	argsFile := filepath.Join(t.TempDir(), "args")
	t.Setenv(helperArgs, argsFile)

	mustRunCLI(t, p, "plan", "1", "-i", "--dir", dir)

	d, err := store.OpenAt(p).Load()
	if err != nil {
		t.Fatal(err)
	}
	sid := d.Tasks[0].SessionID
	if sid == "" {
		t.Fatal("the first visit should pin a session; without one it is unresumable")
	}
	first := readArgs(t, argsFile)
	if !strings.Contains(first, "--session-id "+sid) {
		t.Errorf("first visit args = %q, want the pinned id", first)
	}
	if !strings.Contains(first, "ship v2") {
		t.Error("the first visit should carry the opening brief")
	}

	// Second visit: same conversation, no repeated brief.
	mustRunCLI(t, p, "plan", "1", "-i")
	again := readArgs(t, argsFile)
	if !strings.Contains(again, "--resume "+sid) {
		t.Errorf("second visit args = %q, want it to resume", again)
	}
	if strings.Contains(again, "ship v2") {
		t.Errorf("a resumed session should not repeat the brief: %q", again)
	}

	// The stored ID does not drift.
	d, _ = store.OpenAt(p).Load()
	if d.Tasks[0].SessionID != sid {
		t.Error("resuming changed the task's session")
	}

	// --new-session starts over deliberately.
	mustRunCLI(t, p, "plan", "1", "-i", "--new-session")
	d, _ = store.OpenAt(p).Load()
	if d.Tasks[0].SessionID == sid {
		t.Error("--new-session should have started a different conversation")
	}
}

// A plan agreed in conversation is attached when you come back, which is what
// makes the session part of ike rather than just a shell-out.
func TestInteractiveSessionAdoptsTheAgreedPlan(t *testing.T) {
	p := scratch(t)
	dir := t.TempDir()
	mustRunCLI(t, p, "add", "ship v2", "-q", "1")
	fakeStream(t)
	t.Setenv(helperDraft, "## Agreed\n\nDo the thing.\n")

	out := mustRunCLI(t, p, "plan", "1", "-i", "--dir", dir)
	if !strings.Contains(out, "attached the plan you agreed on") {
		t.Errorf("out = %q, want the adopted plan reported", out)
	}
	if got := mustRunCLI(t, p, "plan", "1", "--show"); !strings.Contains(got, "Do the thing.") {
		t.Errorf("--show = %q", got)
	}

	// Consumed, so a later session that agrees on nothing does not re-adopt it.
	t.Setenv(helperDraft, "")
	mustRunCLI(t, p, "plan", "1", "--clear")
	mustRunCLI(t, p, "plan", "1", "-i")
	if _, err := runCLI(t, p, "plan", "1", "--show"); err == nil {
		t.Error("a stale draft was adopted a second time")
	}
}

// Supervising still needs permission — being present does not remove the
// decision to let ike start an agent that edits files.
func TestInteractiveDelegateStillNeedsTheGate(t *testing.T) {
	p := scratch(t)
	dir := t.TempDir()
	mustRunCLI(t, p, "add", "ship v2", "-q", "1")
	fakeStream(t)

	if _, err := runCLI(t, p, "delegate", "1", "-i", "--dir", dir); err == nil {
		t.Fatal("an interactive delegate should be refused with the gate off")
	}
	mustRunCLI(t, p, "agent", "enable")
	mustRunCLI(t, p, "delegate", "1", "-i", "--dir", dir)
}

// --plan-first chains the two runs, so one command drafts and then acts.
// ike chooses an effort level from what the run has to work with, and the run
// has to actually use it: a header that says one thing while the agent runs at
// another is worse than no header.
func TestEffortIsRecommendedThenOverridable(t *testing.T) {
	p := scratch(t)
	dir := t.TempDir()
	mustRunCLI(t, p, "add", "ship v2", "-q", "1")
	mustRunCLI(t, p, "agent", "enable")
	fakeStream(t, planStream("## The drafted plan")...)

	argsFile := filepath.Join(t.TempDir(), "args")
	t.Setenv(helperArgs, argsFile)

	// Planning is the reasoning, so it does not step down.
	out := mustRunCLI(t, p, "plan", "1", "--dir", dir)
	if !strings.Contains(out, "effort high — drafting a plan") {
		t.Errorf("plan header = %q, want the level and the reason", out)
	}
	if got := readArgs(t, argsFile); !strings.Contains(got, "--effort high") {
		t.Errorf("planning args = %q, want the recommendation on the command line", got)
	}

	// That run attached a plan, so delegating now is follow-through.
	out = mustRunCLI(t, p, "delegate", "1")
	if !strings.Contains(out, "effort medium — following an attached plan") {
		t.Errorf("delegate header = %q, want the step down and its reason", out)
	}
	if got := readArgs(t, argsFile); !strings.Contains(got, "--effort medium") {
		t.Errorf("delegate args = %q, want the recommendation on the command line", got)
	}

	// With the plan cleared there is nothing to follow, so the run has to work
	// the approach out as well as do it.
	mustRunCLI(t, p, "plan", "1", "--clear")
	out = mustRunCLI(t, p, "delegate", "1")
	if !strings.Contains(out, "effort high — no plan to follow") {
		t.Errorf("delegate header = %q, want the reason for staying high", out)
	}

	// An explicit level wins, and is reported without a reason — there is
	// nothing to explain about a flag somebody typed.
	out = mustRunCLI(t, p, "delegate", "1", "--effort", "low")
	if !strings.Contains(out, "effort low") || strings.Contains(out, "effort low —") {
		t.Errorf("delegate header = %q, want the bare level for an explicit flag", out)
	}
	if got := readArgs(t, argsFile); !strings.Contains(got, "--effort low") {
		t.Errorf("delegate args = %q, want the override", got)
	}
}

// A mistyped level must fail before a process starts, for the same reason a
// mistyped permission mode does.
func TestBadEffortIsRefusedBeforeRunning(t *testing.T) {
	p := scratch(t)
	dir := t.TempDir()
	mustRunCLI(t, p, "add", "ship v2", "-q", "1")
	mustRunCLI(t, p, "agent", "enable")

	argsFile := filepath.Join(t.TempDir(), "args")
	fakeStream(t, planStream("a plan")...)
	t.Setenv(helperArgs, argsFile)

	for _, args := range [][]string{
		{"delegate", "1", "--dir", dir, "--effort", "mid"},
		{"plan", "1", "--dir", dir, "--effort", "mid"},
	} {
		_, err := runCLI(t, p, args...)
		if err == nil {
			t.Fatalf("%v: a misspelled effort level should be refused", args)
		}
		if !strings.Contains(err.Error(), "medium") {
			t.Errorf("%v: error = %q, it should list the valid levels", args, err)
		}
	}
	if _, statErr := os.Stat(argsFile); statErr == nil {
		t.Error("the agent was started despite the bad flag")
	}
}

func TestPlanFirstChainsBothRuns(t *testing.T) {
	p := scratch(t)
	dir := t.TempDir()
	mustRunCLI(t, p, "add", "ship v2", "-q", "1")
	mustRunCLI(t, p, "agent", "enable")
	fakeStream(t, planStream("## The drafted plan")...)

	out := mustRunCLI(t, p, "delegate", "1", "--dir", dir, "--plan-first")
	if !strings.Contains(out, "attached a plan") {
		t.Errorf("out = %q, want the planning run to have attached a plan", out)
	}
	// And the plan is stored, so it survives for a later run too.
	if got := mustRunCLI(t, p, "plan", "1", "--show"); !strings.Contains(got, "The drafted plan") {
		t.Errorf("--show = %q", got)
	}
}
