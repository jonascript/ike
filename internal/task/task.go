// Package task defines the core domain types for ike: tasks and the
// Eisenhower quadrants they live in. It has no dependencies beyond stdlib.
package task

import (
	"fmt"
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

// Label returns the short action name for the quadrant.
func (q Quadrant) Label() string {
	switch q {
	case Do:
		return "Do"
	case Schedule:
		return "Schedule"
	case Delegate:
		return "Delegate"
	case Eliminate:
		return "Eliminate"
	}
	return "?"
}

// Desc returns the quadrant's descriptive nickname; the urgency/importance
// axes are conveyed by the matrix layout itself.
func (q Quadrant) Desc() string {
	switch q {
	case Do:
		return "Emergencies"
	case Schedule:
		return "Planning"
	case Delegate:
		return "Interruptions"
	case Eliminate:
		return "Time-wasters"
	}
	return ""
}

// Task is a single to-do item. Completed tasks keep their ID and gain a
// DoneAt timestamp when moved to the archive.
type Task struct {
	ID        int        `json:"id"`
	Title     string     `json:"title"`
	Quadrant  Quadrant   `json:"quadrant"`
	CreatedAt time.Time  `json:"created_at"`
	DoneAt    *time.Time `json:"done_at,omitempty"`
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
