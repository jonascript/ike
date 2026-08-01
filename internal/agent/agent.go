// Package agent runs the Claude Code CLI as a subprocess and streams what it
// does back as events.
//
// It is the only package in ike that starts a process. Everything else — which
// task is being delegated, whether the user has consented, what to do with the
// result — belongs to the caller, in the same way internal/mcpserver is a pure
// transport with the business logic behind it in the store.
//
// The CLI is used rather than an SDK or the API directly because it brings the
// user's own setup with it: their authentication, their settings, their
// per-project CLAUDE.md. A task delegated from ike should behave the way the
// same request typed into `claude` would.
package agent

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
)

// Mode is what a run is for.
type Mode int

const (
	// ModePlan drafts a plan. It runs with --permission-mode plan, so it can
	// read the working directory but not change it.
	ModePlan Mode = iota
	// ModeExecute carries the task out.
	ModeExecute
)

// DefaultPermissionMode is what an execute run uses unless told otherwise.
//
// acceptEdits lets the agent read and write files without stopping, but still
// holds back Bash and the rest, which surface as denials in the transcript
// instead. A headless run cannot answer a permission prompt, so the choice is
// really between "edits go through" and "nothing that needs permission goes
// through"; this is the setting that makes a delegated task useful without
// handing over the shell. bypassPermissions is available for anyone who wants
// it, deliberately by explicit request only.
const DefaultPermissionMode = "acceptEdits"

// binEnv overrides the binary ike runs. It exists for tests, and for anyone
// whose claude is not on PATH or who wants to wrap it.
const binEnv = "IKE_AGENT_CMD"

// Spec describes one run.
type Spec struct {
	Mode Mode
	// Title is the task being delegated.
	Title string
	// Quadrant is the task's display label, which tells the agent how the user
	// classified the work.
	Quadrant string
	// Plan is the attached plan: revised by a ModePlan run, followed by a
	// ModeExecute one. It may be empty.
	Plan string
	// Dir is the working directory. It must be absolute and exist; the store's
	// CheckDir is what guarantees that.
	Dir string
	// PermissionMode overrides DefaultPermissionMode on an execute run.
	PermissionMode string
	// Model optionally overrides the model, e.g. "opus" or "sonnet".
	Model string
}

// Run is a live agent process.
type Run struct {
	cmd    *exec.Cmd
	events chan Event
	cancel context.CancelFunc

	// stderr is kept so a process that dies without a result event can say why.
	stderr *strings.Builder

	once sync.Once
}

// Events returns the run's event stream. It is closed when the run ends, which
// is the signal that no more events are coming.
func (r *Run) Events() <-chan Event { return r.events }

// Cancel stops the run. It is safe to call more than once, and on a run that
// has already finished.
func (r *Run) Cancel() { r.once.Do(func() { r.kill() }) }

// Start launches the agent and begins streaming.
//
// The returned Run is live: read Events until it closes. Start itself only
// fails on problems visible before the process is running — no binary, a bad
// spec — so anything after that arrives as a KindError event rather than a
// second error return.
func Start(ctx context.Context, s Spec) (*Run, error) {
	bin, err := lookBinary()
	if err != nil {
		return nil, err
	}
	if s.Dir == "" {
		return nil, errors.New("a delegated run needs a working directory")
	}

	ctx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(ctx, bin, args(s)...)
	cmd.Dir = s.Dir
	// Left nil on purpose, which gives the child /dev/null. The CLI waits for
	// stdin when it is a terminal — an ordinary run prints "no stdin data
	// received in 3s" and pauses for exactly that long — and a child sharing
	// ike's stdin would be reading the keys meant for the TUI.
	cmd.Stdin = nil
	// Its own process group, so Cancel can signal the whole tree. The agent
	// spawns subprocesses of its own, and killing only the parent would leave
	// them running against the user's files after the run looked stopped.
	setProcessGroup(cmd)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	var stderr strings.Builder
	cmd.Stderr = &limitedWriter{w: &stderr, n: maxStderr}

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("starting %s: %w", bin, err)
	}

	r := &Run{
		cmd:    cmd,
		events: make(chan Event, eventBuffer),
		cancel: cancel,
		stderr: &stderr,
	}
	go r.stream(stdout)
	return r, nil
}

const (
	// eventBuffer keeps the reader ahead of a frontend that is mid-render, so
	// the agent is never blocked by the speed of the screen.
	eventBuffer = 64
	// maxStderr bounds what is kept from stderr. It is only ever shown when a
	// run dies without a result, and a runaway process could otherwise fill
	// memory with warnings.
	maxStderr = 8 << 10
	// maxLine bounds one line of stdout. The default scanner limit is 64K,
	// which a single tool_result event exceeds easily.
	maxLine = 4 << 20
)

// stream reads the process's stdout to completion, then reaps it.
func (r *Run) stream(stdout io.ReadCloser) {
	defer close(r.events)
	defer r.cancel()

	sawResult := false
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 64<<10), maxLine)
	for sc.Scan() {
		for _, e := range parseLine(sc.Text()) {
			if e.Kind == KindResult {
				sawResult = true
			}
			r.events <- e
		}
	}
	// A scan error is worth reporting, but not if it is just the pipe closing
	// because the run was cancelled.
	if err := sc.Err(); err != nil && !sawResult {
		r.events <- Event{Kind: KindError, Text: clean("reading the agent's output: " + err.Error())}
	}

	err := r.cmd.Wait()
	if sawResult {
		// The agent said how it went, which is the more useful answer. A
		// non-zero exit after a result event just repeats it.
		return
	}
	if err == nil {
		return
	}
	r.events <- Event{Kind: KindError, Text: clean(exitMessage(err, r.stderr.String()))}
}

// exitMessage explains a process that ended without saying how it went.
func exitMessage(err error, stderr string) string {
	msg := "the agent exited without finishing: " + err.Error()
	// stderr is where the CLI puts its own diagnostics — a bad flag, an auth
	// problem, a directory it will not trust — so it usually holds the actual
	// reason. It is shown only here, because in a normal run it is noise.
	if s := strings.TrimSpace(stderr); s != "" {
		msg += "\n" + s
	}
	return msg
}

// kill stops the agent and everything it started.
//
// Best effort throughout: the process may already have exited, and cancel()
// closes the context either way, so there is nothing useful to report here.
func (r *Run) kill() {
	r.cancel()
	if r.cmd.Process == nil {
		return
	}
	killProcessGroup(r.cmd)
}

// args builds the command line.
//
// Every flag here is load-bearing:
//   - -p is what makes the run non-interactive.
//   - --output-format stream-json is the machine-readable stream.
//   - --verbose is required *with* it; without --verbose the stream carries
//     nothing useful.
//   - --permission-mode plan is what makes a planning run read-only.
//
// The prompt is passed as an argument rather than on stdin so that stdin stays
// closed; see the note in Start.
func args(s Spec) []string {
	out := []string{
		"-p", prompt(s),
		"--output-format", "stream-json",
		"--verbose",
	}
	switch s.Mode {
	case ModePlan:
		out = append(out, "--permission-mode", "plan")
	case ModeExecute:
		mode := s.PermissionMode
		if mode == "" {
			mode = DefaultPermissionMode
		}
		out = append(out, "--permission-mode", mode)
	}
	if s.Model != "" {
		out = append(out, "--model", s.Model)
	}
	return out
}

// lookBinary finds the claude CLI.
func lookBinary() (string, error) {
	if override := os.Getenv(binEnv); override != "" {
		return override, nil
	}
	bin, err := exec.LookPath("claude")
	if err != nil {
		// The bare LookPath error is "executable file not found in $PATH",
		// which does not tell someone who has never installed it what to do.
		return "", fmt.Errorf("ike delegates by running the Claude Code CLI, and `claude` "+
			"is not on your PATH.\nInstall it from https://claude.com/claude-code, or set %s "+
			"to its full path.", binEnv)
	}
	return bin, nil
}

// limitedWriter keeps the first n bytes written to it and discards the rest.
type limitedWriter struct {
	w *strings.Builder
	n int
}

func (l *limitedWriter) Write(p []byte) (int, error) {
	full := len(p)
	if room := l.n - l.w.Len(); room > 0 {
		if len(p) > room {
			p = p[:room]
		}
		l.w.Write(p)
	}
	// The full length, captured before the truncation above: reporting the
	// number of bytes actually kept would look like a short write, and the
	// child would treat its own stderr as broken.
	return full, nil
}
