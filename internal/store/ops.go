package store

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/jonascript/ike/internal/task"
)

const (
	// rankGap is the spacing between adjacent tasks' ranks. Reorder rewrites
	// a whole quadrant's ranks as multiples of it, so the values never drift.
	rankGap = 1024

	// undoDepth is how many mutations each history stack keeps, oldest dropped
	// first. Snapshots are whole task lists, so this bounds the file size.
	undoDepth = 20
)

// Deltas for Reorder that always reach the ends of a quadrant. Reorder clamps,
// so any value past the end is equivalent; these are sized to stay well clear
// of integer overflow.
const (
	ToTop    = -1 << 30
	ToBottom = 1 << 30
)

// Errors returned when a history stack is exhausted.
var (
	ErrNothingToUndo = errors.New("nothing to undo")
	ErrNothingToRedo = errors.New("nothing to redo")
)

// Every mutating operation returns the post-mutation Data alongside its own
// result. Mutate already produces it, so handing it back costs nothing and
// spares callers a second read: the quadrant labels a frontend needs to render
// the outcome, and the full state the TUI re-renders from, are both in there.
// Reading them again would also be a read of a *later* file state, which may
// no longer be the one the operation produced.

// Add creates a new task at the bottom of the given quadrant and returns it.
func (s *Store) Add(title string, q task.Quadrant) (task.Task, Data, error) {
	title = strings.TrimSpace(title)
	if err := task.Validate(title, q); err != nil {
		return task.Task{}, Data{}, err
	}
	var created task.Task
	d, err := s.Mutate(func(d *Data) error {
		pushUndo(d, fmt.Sprintf("add %q", title))
		created = task.Task{
			ID:        d.NextID,
			Title:     title,
			Quadrant:  q,
			Rank:      maxRank(d, q) + rankGap,
			CreatedAt: time.Now().UTC(),
		}
		d.NextID++
		d.Tasks = append(d.Tasks, created)
		return nil
	})
	return created, d, err
}

// Complete moves a task to the archive, stamping DoneAt.
func (s *Store) Complete(id int) (task.Task, Data, error) {
	var done task.Task
	d, err := s.Mutate(func(d *Data) error {
		i, err := findTask(d, id)
		if err != nil {
			return err
		}
		pushUndo(d, fmt.Sprintf("complete %q", d.Tasks[i].Title))
		done = d.Tasks[i]
		now := time.Now().UTC()
		done.DoneAt = &now
		d.Tasks = append(d.Tasks[:i], d.Tasks[i+1:]...)
		d.Archive = append(d.Archive, done)
		return nil
	})
	return done, d, err
}

// Restore moves an archived task back into the active matrix, clearing its
// completion stamp and placing it at the bottom of the quadrant it was
// completed in.
func (s *Store) Restore(id int) (task.Task, Data, error) {
	var restored task.Task
	d, err := s.Mutate(func(d *Data) error {
		i, err := findArchived(d, id)
		if err != nil {
			return err
		}
		restored = d.Archive[i]
		pushUndo(d, fmt.Sprintf("restore %q", restored.Title))
		restored.DoneAt = nil
		restored.Rank = maxRank(d, restored.Quadrant) + rankGap
		d.Archive = append(d.Archive[:i], d.Archive[i+1:]...)
		d.Tasks = append(d.Tasks, restored)
		return nil
	})
	return restored, d, err
}

// Move changes a task's quadrant, placing it at the bottom of the destination.
func (s *Store) Move(id int, q task.Quadrant) (task.Task, Data, error) {
	if !q.Valid() {
		return task.Task{}, Data{}, fmt.Errorf("quadrant must be 1-4, got %d", q)
	}
	var moved task.Task
	d, err := s.Mutate(func(d *Data) error {
		i, err := findTask(d, id)
		if err != nil {
			return err
		}
		if d.Tasks[i].Quadrant == q {
			moved = d.Tasks[i]
			return nil
		}
		pushUndo(d, fmt.Sprintf("move %q to %s", d.Tasks[i].Title, d.Labels.Of(q)))
		d.Tasks[i].Quadrant = q
		d.Tasks[i].Rank = maxRank(d, q) + rankGap
		moved = d.Tasks[i]
		return nil
	})
	return moved, d, err
}

// Reorder moves a task within its quadrant by delta positions — negative is
// toward the top, positive toward the bottom — clamped at either end. It
// rewrites the whole quadrant's ranks, so repeated moves never lose precision.
func (s *Store) Reorder(id int, delta int) (task.Task, Data, error) {
	var moved task.Task
	d, err := s.Mutate(func(d *Data) error {
		i, err := findTask(d, id)
		if err != nil {
			return err
		}
		moved = d.Tasks[i]

		// Indices of the task's quadrant siblings, in display order.
		order := quadrantOrder(d, d.Tasks[i].Quadrant)
		from := slices.Index(order, i)
		to := min(max(from+delta, 0), len(order)-1)
		if to == from {
			return nil
		}
		pushUndo(d, fmt.Sprintf("reorder %q", d.Tasks[i].Title))

		order = slices.Insert(slices.Delete(order, from, from+1), to, i)
		for pos, idx := range order {
			d.Tasks[idx].Rank = float64(pos+1) * rankGap
		}
		moved = d.Tasks[i]
		return nil
	})
	return moved, d, err
}

// SetQuadrantLabel renames a quadrant's heading. A blank label clears the
// override, restoring the built-in default. It returns the resulting display
// name.
func (s *Store) SetQuadrantLabel(q task.Quadrant, label string) (string, Data, error) {
	if !q.Valid() {
		return "", Data{}, fmt.Errorf("quadrant must be 1-4, got %d", q)
	}
	label = strings.TrimSpace(label)
	if err := task.ValidateLabel(label); err != nil {
		return "", Data{}, err
	}
	var result string
	d, err := s.Mutate(func(d *Data) error {
		was := d.Labels.Of(q)
		if label == was {
			result = was
			return nil
		}
		if label == "" {
			pushUndo(d, fmt.Sprintf("reset the name of quadrant %d to %q", q, q.Label()))
			delete(d.Labels, q)
		} else {
			pushUndo(d, fmt.Sprintf("rename quadrant %d to %q", q, label))
			if d.Labels == nil {
				d.Labels = Labels{}
			}
			d.Labels[q] = label
		}
		result = d.Labels.Of(q)
		return nil
	})
	return result, d, err
}

// QuadrantLabels returns the display names of all four quadrants, custom where
// set and default otherwise.
func (s *Store) QuadrantLabels() (Labels, error) {
	d, err := s.Load()
	if err != nil {
		return nil, err
	}
	return d.QuadrantLabels(), nil
}

// QuadrantLabels returns the display names of all four quadrants, custom where
// set and default otherwise. It is the pure form of Store.QuadrantLabels, for
// callers that already hold the data — every mutation hands theirs back.
func (d Data) QuadrantLabels() Labels {
	out := make(Labels, 4)
	for q := task.Do; q <= task.Eliminate; q++ {
		out[q] = d.Labels.Of(q)
	}
	return out
}

// SetMCPEnabled turns agent access to this matrix on or off, and reports
// whether the setting changed.
//
// This is a permission, not an edit: it is deliberately kept off the undo
// stack, so no sequence of undo or redo can re-open access the owner closed.
// It also does not return Data the way the task operations do — nothing
// renders a result from it, and handing an MCP-gated caller a full copy of the
// matrix from the call that flips the gate would be a poor shape.
func (s *Store) SetMCPEnabled(on bool) (changed bool, err error) {
	_, err = s.Mutate(func(d *Data) error {
		changed = d.MCPEnabled != on
		d.MCPEnabled = on
		return nil
	})
	return changed, err
}

// MCPEnabled reports whether the MCP server may serve this matrix.
func (s *Store) MCPEnabled() (bool, error) {
	d, err := s.Load()
	if err != nil {
		return false, err
	}
	return d.MCPEnabled, nil
}

// Rename changes a task's title.
func (s *Store) Rename(id int, title string) (task.Task, Data, error) {
	title = strings.TrimSpace(title)
	var renamed task.Task
	d, err := s.Mutate(func(d *Data) error {
		i, err := findTask(d, id)
		if err != nil {
			return err
		}
		if err := task.Validate(title, d.Tasks[i].Quadrant); err != nil {
			return err
		}
		pushUndo(d, fmt.Sprintf("rename %q", d.Tasks[i].Title))
		d.Tasks[i].Title = title
		renamed = d.Tasks[i]
		return nil
	})
	return renamed, d, err
}

// Delete removes an active task permanently (it does not go to the archive).
// It is still undoable until it falls off the undo stack.
func (s *Store) Delete(id int) (task.Task, Data, error) {
	var deleted task.Task
	d, err := s.Mutate(func(d *Data) error {
		i, err := findTask(d, id)
		if err != nil {
			return err
		}
		pushUndo(d, fmt.Sprintf("delete %q", d.Tasks[i].Title))
		deleted = d.Tasks[i]
		d.Tasks = append(d.Tasks[:i], d.Tasks[i+1:]...)
		return nil
	})
	return deleted, d, err
}

// Undo reverts the most recent recorded mutation and returns its label. It
// restores both task lists but deliberately leaves next_id alone: IDs stay
// monotonic and are never reused, so undoing an add does not free its ID.
// The undone state moves to the redo stack, so Redo puts it back.
func (s *Store) Undo() (string, Data, error) {
	var label string
	d, err := s.Mutate(func(d *Data) error {
		if len(d.Undo) == 0 {
			return ErrNothingToUndo
		}
		snap := d.Undo[len(d.Undo)-1]
		d.Undo = d.Undo[:len(d.Undo)-1]
		// The state we are stepping away from is what Redo would restore.
		d.Redo = trimHistory(append(d.Redo, snapshotOf(d, snap.Label)))
		d.Tasks, d.Archive, d.Labels = snap.Tasks, snap.Archive, snap.Labels
		label = snap.Label
		return nil
	})
	return label, d, err
}

// Redo re-applies the most recently undone change and returns its label. It
// is available only until the next real mutation, which discards the redo
// stack. A redo is itself undoable.
func (s *Store) Redo() (string, Data, error) {
	var label string
	d, err := s.Mutate(func(d *Data) error {
		if len(d.Redo) == 0 {
			return ErrNothingToRedo
		}
		snap := d.Redo[len(d.Redo)-1]
		d.Redo = d.Redo[:len(d.Redo)-1]
		// recordUndo, not pushUndo: this must not clear the rest of the redo
		// stack, or redoing several steps would strand the ones above it.
		recordUndo(d, snap.Label)
		d.Tasks, d.Archive, d.Labels = snap.Tasks, snap.Archive, snap.Labels
		label = snap.Label
		return nil
	})
	return label, d, err
}

// UndoLabel reports what the next Undo would revert, or "" if the stack is
// empty. Frontends use it to label the undo affordance.
func UndoLabel(d Data) string {
	if len(d.Undo) == 0 {
		return ""
	}
	return d.Undo[len(d.Undo)-1].Label
}

// RedoLabel reports what the next Redo would re-apply, or "" if there is none.
func RedoLabel(d Data) string {
	if len(d.Redo) == 0 {
		return ""
	}
	return d.Redo[len(d.Redo)-1].Label
}

// pushUndo records the pre-mutation state for a genuinely new change. Call it
// inside a Mutate callback, after validation and immediately before applying
// the change, so that failed and no-op mutations leave the history alone.
//
// It also discards any redo history: the redone future belongs to a branch
// that this change diverges from, and replaying it would clobber this change.
// Because every frontend's mutations funnel through here, that holds for
// writes from the CLI and MCP server too, not just the TUI.
func pushUndo(d *Data, label string) {
	recordUndo(d, label)
	d.Redo = nil
}

// recordUndo pushes the current state onto the undo stack, leaving the redo
// stack untouched.
func recordUndo(d *Data, label string) {
	d.Undo = trimHistory(append(d.Undo, snapshotOf(d, label)))
}

func snapshotOf(d *Data, label string) Snapshot {
	return Snapshot{
		Label:   label,
		Tasks:   slices.Clone(d.Tasks),
		Archive: slices.Clone(d.Archive),
		Labels:  maps.Clone(d.Labels),
	}
}

// trimHistory caps a history stack, dropping the oldest entries.
func trimHistory(stack []Snapshot) []Snapshot {
	if len(stack) > undoDepth {
		return stack[len(stack)-undoDepth:]
	}
	return stack
}

// quadrantOrder returns the indices into d.Tasks of q's tasks, in display order.
func quadrantOrder(d *Data, q task.Quadrant) []int {
	var order []int
	for i := range d.Tasks {
		if d.Tasks[i].Quadrant == q {
			order = append(order, i)
		}
	}
	sort.SliceStable(order, func(a, b int) bool {
		return task.Less(d.Tasks[order[a]], d.Tasks[order[b]])
	})
	return order
}

// maxRank returns the highest rank currently used in quadrant q, or 0.
func maxRank(d *Data, q task.Quadrant) float64 {
	var m float64
	for _, t := range d.Tasks {
		if t.Quadrant == q && t.Rank > m {
			m = t.Rank
		}
	}
	return m
}

// normalizeRanks assigns a rank to every active task that lacks one, placing
// it after the already-ranked tasks of its quadrant in ID order. It runs on
// every read so that data written before ranks existed still sorts stably.
func normalizeRanks(d *Data) {
	for q := task.Do; q <= task.Eliminate; q++ {
		var missing []int
		next := maxRank(d, q)
		for i := range d.Tasks {
			if d.Tasks[i].Quadrant == q && d.Tasks[i].Rank == 0 {
				missing = append(missing, i)
			}
		}
		sort.SliceStable(missing, func(a, b int) bool {
			return d.Tasks[missing[a]].ID < d.Tasks[missing[b]].ID
		})
		for _, i := range missing {
			next += rankGap
			d.Tasks[i].Rank = next
		}
	}
}

// List returns active tasks, optionally filtered to one quadrant (q == 0
// means all), ordered by quadrant then by position within it.
func (s *Store) List(q task.Quadrant) ([]task.Task, error) {
	d, err := s.Load()
	if err != nil {
		return nil, err
	}
	return d.List(q), nil
}

// List returns d's active tasks, optionally filtered to one quadrant (q == 0
// means all), in display order. It is the single definition of that list, used
// by Store.List and rendered directly by the TUI from the data it already has.
func (d Data) List(q task.Quadrant) []task.Task {
	tasks := make([]task.Task, 0, len(d.Tasks))
	for _, t := range d.Tasks {
		if q == 0 || t.Quadrant == q {
			tasks = append(tasks, t)
		}
	}
	task.SortOrder(tasks)
	return tasks
}

// ListArchive returns archived tasks, newest completion first.
func (s *Store) ListArchive() ([]task.Task, error) {
	d, err := s.Load()
	if err != nil {
		return nil, err
	}
	return d.ListArchive(), nil
}

// ListArchive returns d's archived tasks, newest completion first. It is the
// single definition of archive order: `ike archive` and the TUI's archive view
// both render this, so they cannot drift apart. Tasks with no completion stamp
// — reachable from a hand-edited or pre-archive data file — sort by ID, which
// is why this is not just a reversal of the stored slice.
func (d Data) ListArchive() []task.Task {
	// Not slices.Clone: it preserves nil, and `ike archive --json` must emit []
	// rather than null for an empty archive.
	arch := make([]task.Task, len(d.Archive))
	copy(arch, d.Archive)
	sort.SliceStable(arch, func(i, j int) bool {
		ti, tj := arch[i].DoneAt, arch[j].DoneAt
		if ti != nil && tj != nil && !ti.Equal(*tj) {
			return ti.After(*tj)
		}
		return arch[i].ID > arch[j].ID
	})
	return arch
}

func findTask(d *Data, id int) (int, error) {
	for i, t := range d.Tasks {
		if t.ID == id {
			return i, nil
		}
	}
	return 0, fmt.Errorf("no active task with id %d", id)
}

func findArchived(d *Data, id int) (int, error) {
	for i, t := range d.Archive {
		if t.ID == id {
			return i, nil
		}
	}
	return 0, fmt.Errorf("no archived task with id %d", id)
}
