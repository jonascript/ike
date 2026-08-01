package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The tests drive a fake `claude` rather than the real one: a run costs money,
// needs credentials, and would make the suite depend on a model's wording. The
// fake is this test binary re-executed with a marker in its environment — the
// standard trick — so there is no shell script to keep executable and no
// second language in the repo.
//
// What it replays is a real transcript. testdata/toolrun.jsonl was captured
// from an actual `claude -p --output-format stream-json --verbose` run that
// read a file, ran a command, and wrote a file, so the parser is checked
// against the format as it really arrives rather than as this package imagines
// it.

const (
	helperEnv     = "IKE_TEST_HELPER"
	helperFixture = "IKE_TEST_FIXTURE"
	helperExit    = "IKE_TEST_EXIT"
	helperStderr  = "IKE_TEST_STDERR"
	helperSleep   = "IKE_TEST_SLEEP"
)

// TestMain lets this binary stand in for the claude CLI when the marker is set.
func TestMain(m *testing.M) {
	if os.Getenv(helperEnv) == "" {
		os.Exit(m.Run())
	}

	if s := os.Getenv(helperSleep); s != "" {
		// Long enough that the test cancels first, and bounded so a failing
		// test cannot wedge the suite.
		time.Sleep(time.Minute)
	}
	if f := os.Getenv(helperFixture); f != "" {
		b, err := os.ReadFile(f)
		if err != nil {
			panic(err)
		}
		os.Stdout.Write(b)
	}
	if e := os.Getenv(helperStderr); e != "" {
		os.Stderr.WriteString(e)
	}
	if os.Getenv(helperExit) == "1" {
		os.Exit(1)
	}
	os.Exit(0)
}

// fakeAgent points Start at this test binary, configured by env.
func fakeAgent(t *testing.T, env map[string]string) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(binEnv, exe)
	t.Setenv(helperEnv, "1")
	for k, v := range env {
		t.Setenv(k, v)
	}
}

// collect drains a run to completion.
func collect(t *testing.T, r *Run) []Event {
	t.Helper()
	var out []Event
	done := make(chan struct{})
	go func() {
		defer close(done)
		for e := range r.Events() {
			out = append(out, e)
		}
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("the run did not finish")
	}
	return out
}

func fixture(t *testing.T, name string) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func startRun(t *testing.T, s Spec) *Run {
	t.Helper()
	if s.Dir == "" {
		s.Dir = t.TempDir()
	}
	if s.Title == "" {
		s.Title = "a task"
	}
	r, err := Start(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Cancel)
	return r
}

// The end-to-end shape of a real run: it starts, does some work with tools, and
// finishes with a result.
func TestRunStreamsARealTranscript(t *testing.T) {
	fakeAgent(t, map[string]string{helperFixture: fixture(t, "toolrun.jsonl")})
	events := collect(t, startRun(t, Spec{Mode: ModeExecute}))

	if len(events) == 0 {
		t.Fatal("no events")
	}
	if events[0].Kind != KindStarted {
		t.Errorf("first event kind = %v, want KindStarted", events[0].Kind)
	}
	if events[0].SessionID == "" {
		t.Error("the start event should carry the session ID")
	}
	if events[0].Model == "" {
		t.Error("the start event should carry the model")
	}

	last := events[len(events)-1]
	if last.Kind != KindResult {
		t.Fatalf("last event kind = %v, want KindResult", last.Kind)
	}
	if last.IsError {
		t.Error("the fixture is a successful run")
	}
	if last.Text == "" {
		t.Error("the result should carry the agent's closing summary")
	}
	if last.CostUSD <= 0 {
		t.Error("the result should carry the run's cost")
	}

	// The tools the fixture run actually used, in order.
	var tools []string
	for _, e := range events {
		if e.Kind == KindTool {
			tools = append(tools, e.Tool)
		}
	}
	want := []string{"Read", "Bash", "Write"}
	if strings.Join(tools, ",") != strings.Join(want, ",") {
		t.Errorf("tools = %v, want %v", tools, want)
	}

	var text int
	for _, e := range events {
		if e.Kind == KindText {
			text++
		}
	}
	if text == 0 {
		t.Error("the fixture contains text blocks; none were surfaced")
	}

	// The fixture's thinking blocks carry a signature and no text — reasoning
	// withheld, which is the common case in a real run. They must not become
	// blank transcript lines.
	for _, e := range events {
		if e.Kind == KindThinking && strings.TrimSpace(e.Text) == "" {
			t.Error("an empty thinking block became a transcript line")
		}
	}
}

// A thinking block that does carry text should surface, and one that does not
// should be dropped. Both shapes occur in the same run.
func TestThinkingBlocks(t *testing.T) {
	lines := strings.Join([]string{
		`{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"","signature":"abc"}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"weighing two options"}]}}`,
		`{"type":"result","subtype":"success","result":"done"}`,
	}, "\n") + "\n"

	p := filepath.Join(t.TempDir(), "stream.jsonl")
	if err := os.WriteFile(p, []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}
	fakeAgent(t, map[string]string{helperFixture: p})
	events := collect(t, startRun(t, Spec{Mode: ModeExecute}))

	var thoughts []string
	for _, e := range events {
		if e.Kind == KindThinking {
			thoughts = append(thoughts, e.Text)
		}
	}
	if len(thoughts) != 1 || thoughts[0] != "weighing two options" {
		t.Errorf("thinking events = %q, want just the one with text", thoughts)
	}
}

// The stream carries event types this package has never heard of, and the CLI
// adds more between releases. Skipping them must not end the run.
func TestUnknownEventTypesAreSkipped(t *testing.T) {
	lines := strings.Join([]string{
		`{"type":"system","subtype":"init","session_id":"abc","model":"claude-opus-5"}`,
		`{"type":"rate_limit_event","rate_limit_info":{"status":"allowed"}}`,
		`{"type":"system","subtype":"thinking_tokens","tokens":123}`,
		`{"type":"some_future_event","payload":{"nested":[1,2,3]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","content":"huge"}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"hello"}]}}`,
		`{"type":"result","subtype":"success","is_error":false,"result":"done","total_cost_usd":0.01}`,
	}, "\n") + "\n"

	p := filepath.Join(t.TempDir(), "stream.jsonl")
	if err := os.WriteFile(p, []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}
	fakeAgent(t, map[string]string{helperFixture: p})
	events := collect(t, startRun(t, Spec{Mode: ModeExecute}))

	var kinds []Kind
	for _, e := range events {
		kinds = append(kinds, e.Kind)
	}
	want := []Kind{KindStarted, KindText, KindResult}
	if len(kinds) != len(want) {
		t.Fatalf("kinds = %v, want %v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("kinds = %v, want %v", kinds, want)
		}
	}
}

// A stray non-JSON write — from a wrapper script, a plugin, a shell profile —
// must not end a run that is otherwise fine.
func TestGarbageLinesDoNotEndTheRun(t *testing.T) {
	lines := strings.Join([]string{
		`Warning: something entirely unstructured`,
		`{"type":"system","subtype":"init","session_id":"abc"}`,
		`{"type":"assistant","message":{"content":[{"type":"text",`, // truncated JSON
		``,
		`   `,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"still here"}]}}`,
		`{"type":"result","subtype":"success","result":"done"}`,
	}, "\n") + "\n"

	p := filepath.Join(t.TempDir(), "stream.jsonl")
	if err := os.WriteFile(p, []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}
	fakeAgent(t, map[string]string{helperFixture: p})
	events := collect(t, startRun(t, Spec{Mode: ModeExecute}))

	var sawText, sawResult bool
	for _, e := range events {
		if e.Kind == KindText && e.Text == "still here" {
			sawText = true
		}
		if e.Kind == KindResult {
			sawResult = true
		}
		if e.Kind == KindError {
			t.Errorf("a garbage line produced an error event: %q", e.Text)
		}
	}
	if !sawText || !sawResult {
		t.Error("the run did not survive the garbage lines")
	}
}

// Agent output is the most untrusted text ike renders. An escape sequence in it
// must not reach the screen.
func TestAgentTextIsSanitized(t *testing.T) {
	// A tool name and a result carrying an escape and a carriage return.
	lines := `{"type":"assistant","message":{"content":[{"type":"text","text":"real\u001b[2K\rFAKE\n\nsecond line"}]}}` + "\n" +
		`{"type":"result","subtype":"success","result":"done\u001b]0;pwned\u0007"}` + "\n"

	p := filepath.Join(t.TempDir(), "stream.jsonl")
	if err := os.WriteFile(p, []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}
	fakeAgent(t, map[string]string{helperFixture: p})

	for _, e := range collect(t, startRun(t, Spec{Mode: ModeExecute})) {
		if strings.ContainsRune(e.Text, 0x1b) || strings.ContainsRune(e.Text, '\r') {
			t.Errorf("%v event text still carries control characters: %q", e.Kind, e.Text)
		}
		// Line structure survives — that is the whole difference between
		// SanitizeBlock and SanitizeDisplay.
		if e.Kind == KindText && !strings.Contains(e.Text, "\n\nsecond line") {
			t.Errorf("sanitizing flattened the text: %q", e.Text)
		}
	}
}

// A process that dies without a result event has to explain itself, and stderr
// is where the CLI puts the reason.
func TestFailureWithoutAResultReportsStderr(t *testing.T) {
	fakeAgent(t, map[string]string{
		helperExit:   "1",
		helperStderr: "error: unknown option '--nonsense'",
	})
	events := collect(t, startRun(t, Spec{Mode: ModeExecute}))

	if len(events) != 1 || events[0].Kind != KindError {
		t.Fatalf("events = %+v, want one KindError", events)
	}
	if !strings.Contains(events[0].Text, "--nonsense") {
		t.Errorf("the error should carry stderr, got %q", events[0].Text)
	}
}

// A non-zero exit *after* the agent reported its own result is not a second
// failure to report — the result already said how it went.
func TestResultWinsOverExitCode(t *testing.T) {
	p := filepath.Join(t.TempDir(), "stream.jsonl")
	line := `{"type":"result","subtype":"error_during_execution","is_error":true,"result":"I could not finish"}` + "\n"
	if err := os.WriteFile(p, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	fakeAgent(t, map[string]string{helperFixture: p, helperExit: "1"})
	events := collect(t, startRun(t, Spec{Mode: ModeExecute}))

	if len(events) != 1 {
		t.Fatalf("events = %+v, want just the result", events)
	}
	if events[0].Kind != KindResult || !events[0].IsError {
		t.Errorf("event = %+v, want a failed KindResult", events[0])
	}
}

// A result subtype other than "success" is a failure even when is_error is
// absent — the CLI uses subtypes like error_during_execution and
// error_max_turns.
func TestNonSuccessSubtypeIsAFailure(t *testing.T) {
	p := filepath.Join(t.TempDir(), "stream.jsonl")
	line := `{"type":"result","subtype":"error_max_turns","result":"ran out of turns"}` + "\n"
	if err := os.WriteFile(p, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	fakeAgent(t, map[string]string{helperFixture: p})
	events := collect(t, startRun(t, Spec{Mode: ModeExecute}))

	if len(events) != 1 || !events[0].IsError {
		t.Errorf("events = %+v, want a failed result", events)
	}
}

func TestCancelStopsTheRun(t *testing.T) {
	fakeAgent(t, map[string]string{helperSleep: "1"})
	r := startRun(t, Spec{Mode: ModeExecute})

	go func() {
		time.Sleep(50 * time.Millisecond)
		r.Cancel()
	}()

	// The channel closing is the contract: a canceled run ends its stream.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range r.Events() {
		}
	}()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("Cancel did not stop the run")
	}

	// Calling it again, and after the run has ended, must be safe.
	r.Cancel()
	r.Cancel()
}

func TestCancellingTheContextStopsTheRun(t *testing.T) {
	fakeAgent(t, map[string]string{helperSleep: "1"})
	ctx, cancel := context.WithCancel(context.Background())
	r, err := Start(ctx, Spec{Mode: ModeExecute, Title: "a task", Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	collect(t, r)
}

func TestStartRequiresADirectory(t *testing.T) {
	fakeAgent(t, nil)
	if _, err := Start(context.Background(), Spec{Mode: ModeExecute, Title: "a task"}); err == nil {
		t.Error("a run with no working directory should be refused")
	}
}

// The run happens where the task says it does.
func TestRunUsesTheSpecifiedDirectory(t *testing.T) {
	dir := t.TempDir()
	fakeAgent(t, nil)
	r := startRun(t, Spec{Mode: ModeExecute, Dir: dir})
	collect(t, r)

	if r.cmd.Dir != dir {
		t.Errorf("cmd.Dir = %q, want %q", r.cmd.Dir, dir)
	}
}

// Someone who has never installed the CLI should be told what to install, not
// handed exec.LookPath's wording.
func TestMissingBinaryExplainsItself(t *testing.T) {
	t.Setenv(binEnv, "")
	t.Setenv("PATH", t.TempDir())

	_, err := Start(context.Background(), Spec{Mode: ModeExecute, Title: "a task", Dir: t.TempDir()})
	if err == nil {
		t.Fatal("expected an error with no claude on PATH")
	}
	if !strings.Contains(err.Error(), "claude.com/claude-code") || !strings.Contains(err.Error(), binEnv) {
		t.Errorf("error = %q; it should say how to install it and how to override it", err)
	}
}

func TestArgs(t *testing.T) {
	joined := func(s Spec) string { return strings.Join(args(s), " ") }

	plan := joined(Spec{Mode: ModePlan, Title: "t", Dir: "/tmp"})
	// --verbose is not optional: without it, --output-format stream-json emits
	// nothing useful.
	for _, want := range []string{"-p", "--output-format stream-json", "--verbose", "--permission-mode plan"} {
		if !strings.Contains(joined(Spec{Mode: ModePlan}), want) {
			t.Errorf("plan args missing %q: %s", want, plan)
		}
	}

	run := joined(Spec{Mode: ModeExecute})
	if !strings.Contains(run, "--permission-mode "+DefaultPermissionMode) {
		t.Errorf("execute args = %s, want the default permission mode", run)
	}
	if strings.Contains(run, "--model") {
		t.Error("no model should be passed unless one was asked for")
	}

	custom := joined(Spec{Mode: ModeExecute, PermissionMode: "bypassPermissions", Model: "opus"})
	if !strings.Contains(custom, "--permission-mode bypassPermissions") || !strings.Contains(custom, "--model opus") {
		t.Errorf("overrides not applied: %s", custom)
	}
}

// A planning run must not be able to change the directory it is exploring.
func TestPlanModeIsReadOnly(t *testing.T) {
	a := args(Spec{Mode: ModePlan, PermissionMode: "bypassPermissions"})
	joined := strings.Join(a, " ")
	if !strings.Contains(joined, "--permission-mode plan") {
		t.Errorf("plan args = %s, want --permission-mode plan", joined)
	}
	if strings.Contains(joined, "bypassPermissions") {
		t.Error("a PermissionMode override must not apply to a planning run; " +
			"drafting a plan is supposed to be read-only")
	}
}

func TestLimitedWriter(t *testing.T) {
	var b strings.Builder
	w := &limitedWriter{w: &b, n: 10}

	n, err := w.Write([]byte("12345"))
	if n != 5 || err != nil {
		t.Fatalf("Write = %d, %v", n, err)
	}
	// Over the limit: the writer must still report the full length, or the
	// child sees a short write and stops.
	n, err = w.Write([]byte("678901234567890"))
	if n != 15 || err != nil {
		t.Fatalf("Write = %d, %v; a short write would stall the child", n, err)
	}
	if b.String() != "1234567890" {
		t.Errorf("kept %q, want the first 10 bytes", b.String())
	}
}

// Guards against exec.Cmd being replaced with something that shares ike's
// stdin. The TUI owns the terminal, and a child reading from it would eat the
// user's keystrokes.
func TestChildDoesNotInheritStdin(t *testing.T) {
	fakeAgent(t, nil)
	r := startRun(t, Spec{Mode: ModeExecute})
	collect(t, r)

	if r.cmd.Stdin != nil {
		t.Error("the child must not share ike's stdin")
	}
}
