package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os/exec"
	"strings"
)

// An interactive session is the other half of this package: the same agent, on
// the same task, in the same directory, but talking to you rather than
// streaming into a transcript.
//
// It is deliberately not a Run. A Run owns a pipe and parses NDJSON; a session
// owns the terminal, and ike's job is to get out of its way — build the command,
// hand it over, and pick up whatever it left behind. So this returns an
// *exec.Cmd rather than starting anything: the CLI runs it directly and the TUI
// gives it to tea.ExecProcess, which needs the command itself to release and
// restore the terminal around it.
//
// The conversation is pinned to the task by a session ID ike chooses up front,
// which is what lets you leave and come back to it days later instead of
// briefing a fresh agent every time.

// Session describes one interactive visit to a task.
type Session struct {
	// Mode picks the opening brief: ModePlan to talk a plan through, ModeExecute
	// to supervise the work.
	Mode Mode
	// Title, Quadrant, Plan and Dir are as they are on a Spec.
	Title    string
	Quadrant string
	Plan     string
	Dir      string
	// SessionID is the conversation to resume. Empty starts a new one, and the
	// caller is expected to have generated the ID with NewSessionID and stored
	// it, so the next visit can resume rather than starting over again.
	SessionID string
	// Resume says whether SessionID names a conversation that already exists.
	// A new conversation passes the same ID to --session-id instead.
	Resume bool
	// DraftPath is where the agent is asked to leave a plan you agree on, for
	// the caller to adopt afterwards. Empty leaves that out of the brief.
	DraftPath string
	// PermissionMode overrides the default. It matters far less here than for a
	// headless run: you are present, so the agent can simply ask.
	PermissionMode string
	// Model optionally overrides the model.
	Model string
}

// NewSessionID mints a conversation ID.
//
// A version 4 UUID, which is the shape --session-id requires. Hand-rolled from
// crypto/rand rather than adding a dependency for sixteen bytes and a bit of
// formatting.
func NewSessionID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generating a session id: %w", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 1
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32], nil
}

// InteractiveCommand builds the command for a live session.
//
// Stdin, stdout and stderr are left unset on purpose. The caller supplies the
// real terminal — directly in the CLI, or through tea.ExecProcess in the TUI,
// which only fills in streams that are still nil. Setting them here would take
// that choice away and, in the TUI's case, break the handover.
func InteractiveCommand(ctx context.Context, s Session) (*exec.Cmd, error) {
	bin, err := lookBinary()
	if err != nil {
		return nil, err
	}
	if s.Dir == "" {
		return nil, errNoDir
	}
	if s.SessionID == "" {
		return nil, errNoSession
	}
	if err := ValidatePermissionMode(s.PermissionMode); err != nil {
		return nil, err
	}

	cmd := exec.CommandContext(ctx, bin, sessionArgs(s)...)
	cmd.Dir = s.Dir
	// No process group here, unlike a headless run. This process *is* the
	// foreground job: it must receive the ctrl+c you type at it, which is
	// exactly what putting it in its own group would prevent.
	return cmd, nil
}

// sessionArgs builds the command line for a session.
func sessionArgs(s Session) []string {
	var out []string
	if s.Resume {
		// A nudge rather than a brief. Everything about the task is already in
		// the conversation, and re-sending it would restate what the agent
		// just spent a session learning — but --resume does need *a* prompt:
		// without one it fails with "Provide a prompt to continue the
		// conversation" and the session never opens.
		out = append(out, "--resume", s.SessionID, resumePrompt)
	} else {
		// --session-id, not a flag that reports one back. Choosing the ID means
		// ike can store it before the session has even started, so a session
		// that ends badly is still resumable.
		out = append(out, "--session-id", s.SessionID, sessionPrompt(s))
	}
	switch {
	case s.Mode == ModePlan:
		// Plan mode here for the same reason as a headless planning run: talking
		// a plan through should not be able to change the directory it is about.
		out = append(out, "--permission-mode", "plan")
	case s.PermissionMode != "":
		out = append(out, "--permission-mode", s.PermissionMode)
	}
	if s.Model != "" {
		out = append(out, "--model", s.Model)
	}
	return out
}

// resumePrompt reopens a conversation. Short on purpose: the history holds the
// task, the plan, and wherever the last session got to, so the only useful
// thing to say is that we are back.
const resumePrompt = "We're picking this task back up. Briefly remind me where we got to, " +
	"then wait for me."

// sessionPrompt is the opening brief for a new conversation.
func sessionPrompt(s Session) string {
	var b strings.Builder
	if s.Mode == ModePlan {
		b.WriteString("Let's work out how to do one task from my Eisenhower matrix. " +
			"Talk it through with me — ask about anything ambiguous rather than guessing.\n\n")
	} else {
		b.WriteString("Let's work on one task from my Eisenhower matrix. I'm here, so ask me " +
			"rather than guessing when something is ambiguous.\n\n")
	}
	b.WriteString(describe(s.spec()))

	if s.Plan != "" {
		b.WriteString("\nThis plan is already attached to the task:\n\n")
		b.WriteString(fence(s.Plan))
	}

	if s.DraftPath != "" {
		// Named explicitly, and the instruction stays in the conversation's
		// history — which is why the path has to be stable across sessions.
		fmt.Fprintf(&b, "\nWhen we have agreed on a plan, write it as markdown to:\n\n  %s\n\n"+
			"ike picks it up from there and attaches it to the task, so that file is the "+
			"plan — not a copy of it. Don't write it until we've agreed, and don't write "+
			"anything else there.\n", s.DraftPath)
	}
	if s.Mode == ModePlan {
		b.WriteString("\nStart by looking around the working directory and telling me what " +
			"you think the task involves.\n")
	}
	return b.String()
}

// spec adapts a Session to the shape describe expects, so a task is presented
// the same way whether it is being streamed or talked about.
func (s Session) spec() Spec {
	return Spec{Title: s.Title, Quadrant: s.Quadrant, Dir: s.Dir}
}
