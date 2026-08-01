// Package task defines the core domain types for ike: tasks and the
// Eisenhower quadrants they live in. It has no dependencies beyond stdlib.
package task

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Quadrant identifies one of the four Eisenhower matrix quadrants.
type Quadrant int

const (
	Do        Quadrant = 1 // urgent + important
	Schedule  Quadrant = 2 // important, not urgent
	Delegate  Quadrant = 3 // urgent, not important
	Eliminate Quadrant = 4 // neither
)

// Valid reports whether q is one of the four defined quadrants.
func (q Quadrant) Valid() bool {
	return q >= Do && q <= Eliminate
}

// MaxLabelLen bounds a custom quadrant label, so one long name cannot push
// the TUI's quadrant headers out of their cells.
const MaxLabelLen = 40

// MaxSpaceNameLen bounds a space name. It is shorter than a quadrant label
// because the name is a listing column, a TUI header, and something you type
// after `-s` — all of which want it short.
const MaxSpaceNameLen = 32

// MaxTitleLen bounds a task title. The TUI's input widget caps what you can
// type, but nothing constrained titles arriving from the CLI or from an MCP
// client, and every undo snapshot clones the whole task list — so one huge
// title is amplified across the entire history.
const MaxTitleLen = 500

// MaxPlanLen bounds an attached plan. It is far larger than a title because a
// plan is prose, and it costs nothing in the history: plan bodies are stored
// beside the data file rather than on the Task, precisely so they stay out of
// the snapshots the title comment above is worried about.
//
// The bound still matters at the other end. A plan is written by an agent, and
// an agent that loops can produce a great deal of text; this is what stops one
// run filling the disk.
const MaxPlanLen = 20000

// isControl reports whether r is a C0 or C1 control character.
func isControl(r rune) bool {
	return r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f)
}

// isBlockControl reports whether r is a control character that has no place in
// multi-line text. Tab and newline are the two that do: they are the structure
// of a plan or a transcript rather than an attack on the display.
func isBlockControl(r rune) bool {
	return isControl(r) && r != '\n' && r != '\t'
}

// firstControlChar returns the first C0 or C1 control character in s, and
// whether it found one.
//
// Every user- or agent-supplied string is checked, because ike renders them
// straight into a terminal. An ESC (0x1b) in a stored title can repaint the
// line, so `ike list` would display something other than what is stored —
// which matters most when the title came from an agent, since that list is
// how a human audits what the agent did. Other sequences reach further: OSC 52
// writes the clipboard, OSC 0 rewrites the window title. A bare newline is
// enough to fabricate a plausible extra row, or to break the TUI's box layout.
// The --json output is already safe (encoding/json escapes these), but the
// human-readable views are the ones people trust.
func firstControlChar(s string) (rune, bool) {
	for _, r := range s {
		if isControl(r) {
			return r, true
		}
	}
	return 0, false
}

// firstBlockControlChar is firstControlChar for text that is allowed to span
// lines — a plan, or a line of agent output.
func firstBlockControlChar(s string) (rune, bool) {
	for _, r := range s {
		if isBlockControl(r) {
			return r, true
		}
	}
	return 0, false
}

// Label returns the quadrant's default action name. Users can override these
// per data file; see Data.QuadrantLabels in the store package, which falls
// back to this.
func (q Quadrant) Label() string {
	switch q {
	case Do:
		return "Do It First"
	case Schedule:
		return "Schedule It"
	case Delegate:
		return "Delegate It"
	case Eliminate:
		return "Consider Eliminating It"
	}
	return "?"
}

// ValidateLabel checks a user-supplied quadrant label. A blank label is not an
// error here — callers treat it as "reset to the default".
func ValidateLabel(label string) error {
	if n := len([]rune(label)); n > MaxLabelLen {
		return fmt.Errorf("quadrant label is %d characters; the maximum is %d",
			n, MaxLabelLen)
	}
	if r, bad := firstControlChar(label); bad {
		return fmt.Errorf("quadrant label cannot contain control characters (found %#U)", r)
	}
	return nil
}

// ValidateSpaceName checks a user-supplied space name.
//
// Unlike ValidateLabel, a blank name is an error rather than a reset: a space
// name is a key, not a display override, and an empty key is unreachable. The
// caller is expected to have trimmed already — leading and trailing space is
// rejected rather than silently accepted, or " work" and "work" would be two
// different matrices that look identical in every listing.
func ValidateSpaceName(name string) error {
	if name == "" {
		return errors.New("space name cannot be empty")
	}
	if strings.TrimSpace(name) != name {
		return fmt.Errorf("space name %q has leading or trailing whitespace", name)
	}
	if n := len([]rune(name)); n > MaxSpaceNameLen {
		return fmt.Errorf("space name is %d characters; the maximum is %d",
			n, MaxSpaceNameLen)
	}
	if r, bad := firstControlChar(name); bad {
		return fmt.Errorf("space name cannot contain control characters (found %#U)", r)
	}
	return nil
}

// SanitizeDisplay replaces control characters with U+FFFD so a string is safe
// to print into a terminal.
//
// Validate rejects these on the way in, which covers everything ike itself
// writes. This is the matching guarantee on the way out, for text that never
// passed through Validate: a file written by an older build, a hand edit, or a
// copy arriving through a synced folder. Display integrity is the whole point
// of the check, so it is enforced at both ends rather than trusting the file.
func SanitizeDisplay(s string) string {
	return strings.Map(func(r rune) rune {
		if isControl(r) {
			return '�'
		}
		return r
	}, s)
}

// SanitizeBlock is SanitizeDisplay for text that is meant to span lines: an
// attached plan, or a line of agent output on its way to the screen. It keeps
// newlines and tabs and replaces every other control character.
//
// It exists because SanitizeDisplay cannot be used here. That function replaces
// every rune below 0x20, which includes '\n' — running a plan through it turns
// each line break into U+FFFD and renders the whole plan as one long line. The
// two are separate functions rather than one with a flag so that neither call
// site can pick the wrong behavior silently: a single-line field that used
// SanitizeBlock by mistake would let a newline forge an extra row, which is the
// exact failure firstControlChar's comment describes.
//
// Agent output is the most untrusted text ike renders — it is bytes chosen by a
// model, printed into a terminal — so every transcript line goes through here
// before it is drawn. Note that width-aware truncation is not a substitute:
// ansi.Truncate measures escape sequences without removing them.
func SanitizeBlock(s string) string {
	return strings.Map(func(r rune) rune {
		if isBlockControl(r) {
			return '�'
		}
		return r
	}, s)
}

// Task is a single to-do item. Completed tasks keep their ID and gain a
// DoneAt timestamp when moved to the archive.
//
// Dir and PlanAt support delegation: a task can carry a working directory and
// a plan, and either can be handed to an agent. Note what is *not* here — the
// plan body. Snapshot copies Tasks wholesale into as many as forty snapshots,
// so a few KB of markdown per task would be amplified across the whole history,
// which is the blow-up Snapshot.ArchiveEntry exists to have already fixed once.
// PlanAt is the marker frontends render from; the body lives beside the data
// file and is read on demand. See internal/store/plans.go.
type Task struct {
	ID        int        `json:"id"`
	Title     string     `json:"title"`
	Quadrant  Quadrant   `json:"quadrant"`
	Rank      float64    `json:"rank,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	DoneAt    *time.Time `json:"done_at,omitempty"`

	// Dir is the working directory a delegated run executes in, remembered
	// after the first run so a task stays attached to the project it is about.
	Dir string `json:"dir,omitempty"`
	// PlanAt is when the attached plan was last written, and nil when there is
	// no plan. It says a plan exists without making a frontend stat a file to
	// find out, so the matrix can mark planned tasks from the Data it holds.
	PlanAt *time.Time `json:"plan_at,omitempty"`
}

// HasPlan reports whether a plan is attached.
func (t Task) HasPlan() bool { return t.PlanAt != nil }

// DisplayTitle is the title as it is safe to print into a terminal. Use it for
// every human-facing render; --json output keeps the raw title, since
// encoding/json escapes control characters itself.
func (t Task) DisplayTitle() string { return SanitizeDisplay(t.Title) }

// ArchiveDate is the local completion date, or "" for a task carrying no
// completion stamp — reachable from a hand-edited or pre-archive data file.
func (t Task) ArchiveDate() string {
	if t.DoneAt == nil {
		return ""
	}
	return t.DoneAt.Local().Format(time.DateOnly)
}

// ArchiveRow formats one line of an archive listing. `ike archive` and the
// TUI's archive view both render through it, so the two cannot drift apart;
// title is passed in because only the TUI truncates it to the pane width.
func (t Task) ArchiveRow(title string) string {
	return fmt.Sprintf("  %3d  %s  %s", t.ID, t.ArchiveDate(), title)
}

// Less reports whether a sorts before b in display order: by quadrant, then
// by rank within the quadrant, then by ID. Rank is assigned by the store; a
// zero rank means "not ranked yet" and falls back to ID order.
func Less(a, b Task) bool {
	if a.Quadrant != b.Quadrant {
		return a.Quadrant < b.Quadrant
	}
	if a.Rank != b.Rank {
		return a.Rank < b.Rank
	}
	return a.ID < b.ID
}

// SortOrder sorts tasks in place into display order.
func SortOrder(ts []Task) {
	sort.SliceStable(ts, func(i, j int) bool { return Less(ts[i], ts[j]) })
}

// Validate checks that a task's user-supplied fields are acceptable.
func Validate(title string, q Quadrant) error {
	if strings.TrimSpace(title) == "" {
		return fmt.Errorf("task title cannot be empty")
	}
	if n := len([]rune(title)); n > MaxTitleLen {
		return fmt.Errorf("task title is %d characters; the maximum is %d", n, MaxTitleLen)
	}
	if r, bad := firstControlChar(title); bad {
		return fmt.Errorf("task title cannot contain control characters (found %#U)", r)
	}
	if !q.Valid() {
		return fmt.Errorf("quadrant must be 1-4, got %d", q)
	}
	return nil
}

// ValidatePlan checks a plan body. Like ValidateLabel, a blank plan is not an
// error here — callers treat it as "clear the attached plan".
//
// It allows newlines and tabs where Validate does not, because a plan is a
// block of markdown rather than a line in a listing.
func ValidatePlan(body string) error {
	if n := len([]rune(body)); n > MaxPlanLen {
		return fmt.Errorf("plan is %d characters; the maximum is %d", n, MaxPlanLen)
	}
	if r, bad := firstBlockControlChar(body); bad {
		return fmt.Errorf("plan cannot contain control characters (found %#U)", r)
	}
	return nil
}
