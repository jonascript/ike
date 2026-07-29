// Package task defines the core domain types for ike: tasks and the
// Eisenhower quadrants they live in. It has no dependencies beyond stdlib.
package task

import (
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

// MaxTitleLen bounds a task title. The TUI's input widget caps what you can
// type, but nothing constrained titles arriving from the CLI or from an MCP
// client, and every undo snapshot clones the whole task list — so one huge
// title is amplified across the entire history.
const MaxTitleLen = 500

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
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
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
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			return '�'
		}
		return r
	}, s)
}

// Task is a single to-do item. Completed tasks keep their ID and gain a
// DoneAt timestamp when moved to the archive.
type Task struct {
	ID        int        `json:"id"`
	Title     string     `json:"title"`
	Quadrant  Quadrant   `json:"quadrant"`
	Rank      float64    `json:"rank,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	DoneAt    *time.Time `json:"done_at,omitempty"`
}

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
