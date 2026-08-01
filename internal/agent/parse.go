package agent

import (
	"encoding/json"
	"strings"

	"github.com/jonascript/ike/internal/task"
)

// Kind classifies an Event. The set is deliberately small: it is what a
// transcript needs to be readable, not a mirror of the CLI's wire format.
type Kind int

const (
	// KindStarted is the run beginning, carrying the session ID and model.
	KindStarted Kind = iota
	// KindText is a block of prose from the agent.
	KindText
	// KindThinking is a block of the agent's reasoning.
	KindThinking
	// KindTool is the agent invoking a tool.
	KindTool
	// KindResult is the run finishing, successfully or not.
	KindResult
	// KindError is ike's own failure to run or read the agent, never the
	// agent's own output.
	KindError
)

// Event is one thing worth showing the user about a run.
//
// Text is already sanitized for a terminal: every field on the way out of this
// package has been through task.SanitizeBlock, because this is model-chosen
// text bound for a screen and this package is the choke point it all passes
// through. Doing it here rather than at each frontend means the CLI and the TUI
// cannot disagree about whether it happened.
type Event struct {
	Kind Kind
	// Text is the prose of a KindText, KindThinking or KindResult event, or the
	// message of a KindError.
	Text string
	// Tool is the tool name on a KindTool event.
	Tool string
	// SessionID identifies the run, and is set on KindStarted. It is what
	// `claude --resume` takes, so it is worth showing even though ike does not
	// resume runs itself yet.
	SessionID string
	// Model is the model the run is using, set on KindStarted.
	Model string
	// IsError marks a KindResult that failed.
	IsError bool
	// CostUSD is the run's cost, set on KindResult.
	CostUSD float64
}

// wire is the subset of the CLI's stream-json envelope ike reads.
//
// Only these fields are declared. The format carries a great deal more —
// per-message token accounting, cache statistics, rate-limit state, plugin and
// skill inventories — and decoding into a struct that names just what is used
// means a new field upstream is ignored rather than breaking the parse.
type wire struct {
	Type      string `json:"type"`
	Subtype   string `json:"subtype"`
	SessionID string `json:"session_id"`
	Model     string `json:"model"`

	// assistant / user events
	Message struct {
		Content []struct {
			Type     string `json:"type"`
			Text     string `json:"text"`
			Thinking string `json:"thinking"`
			Name     string `json:"name"`
		} `json:"content"`
	} `json:"message"`

	// result events
	IsError bool    `json:"is_error"`
	Result  string  `json:"result"`
	CostUSD float64 `json:"total_cost_usd"`
}

// parseLine turns one line of the agent's stdout into zero or more events.
//
// Zero is the common case for lines ike does not care about, and that is the
// important behaviour: an unrecognised type is skipped, never an error. The
// stream carries event types this package has never heard of — `rate_limit_event`
// and `system/thinking_tokens` both appear in an ordinary run — and the CLI adds
// more between releases. A parser that rejected the unfamiliar would break on a
// `claude` upgrade, which is not something ike gets to control.
//
// A line that is not JSON at all is skipped for the same reason. stdout is
// documented as NDJSON and in practice is (the "no stdin data received"
// warning goes to stderr), but a stray write from a wrapper script or a plugin
// must not end a run that is otherwise fine.
func parseLine(line string) []Event {
	line = strings.TrimSpace(line)
	if line == "" || !strings.HasPrefix(line, "{") {
		return nil
	}
	var w wire
	if err := json.Unmarshal([]byte(line), &w); err != nil {
		return nil
	}

	switch w.Type {
	case "system":
		if w.Subtype != "init" {
			return nil // thinking_tokens and whatever else arrives later
		}
		return []Event{{
			Kind:      KindStarted,
			SessionID: clean(w.SessionID),
			Model:     clean(w.Model),
		}}

	case "assistant":
		// One message can hold several blocks — a thought, then prose, then a
		// tool call — and each is its own line in the transcript.
		var out []Event
		for _, b := range w.Message.Content {
			switch b.Type {
			case "text":
				if t := clean(b.Text); t != "" {
					out = append(out, Event{Kind: KindText, Text: t})
				}
			case "thinking":
				// Often empty. A thinking block frequently arrives carrying
				// only a signature, with the reasoning itself withheld — an
				// ordinary run is full of them. Emitting those would put blank
				// lines through the transcript, so the emptiness check is doing
				// real work rather than guarding a case that cannot happen.
				if t := clean(b.Thinking); t != "" {
					out = append(out, Event{Kind: KindThinking, Text: t})
				}
			case "tool_use":
				out = append(out, Event{Kind: KindTool, Tool: clean(b.Name)})
			}
		}
		return out

	case "result":
		return []Event{{
			Kind:    KindResult,
			Text:    clean(w.Result),
			IsError: w.IsError || w.Subtype != "success",
			CostUSD: w.CostUSD,
		}}
	}

	// user events (tool results) are deliberately dropped: a transcript showing
	// every file the agent read is mostly noise, and a tool result can be
	// megabytes. The tool_use line already says what happened.
	return nil
}

// clean is task.SanitizeBlock, applied at the one point every field leaves this
// package. Agent output is the most untrusted text ike renders — bytes chosen
// by a model, printed into a terminal — and an escape sequence in it could
// otherwise repaint the screen the user is auditing the run with.
func clean(s string) string { return task.SanitizeBlock(s) }
