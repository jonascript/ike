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
	if len([]rune(label)) > MaxLabelLen {
		return fmt.Errorf("quadrant label is %d characters; the maximum is %d",
			len([]rune(label)), MaxLabelLen)
	}
	if strings.ContainsAny(label, "\n\r\t") {
		return fmt.Errorf("quadrant label cannot contain line breaks or tabs")
	}
	return nil
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
	if !q.Valid() {
		return fmt.Errorf("quadrant must be 1-4, got %d", q)
	}
	return nil
}
