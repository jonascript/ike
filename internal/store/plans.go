package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jonascript/ike/internal/task"
)

// Plans are stored one file per task, beside the data file:
//
//	<datafile>.plans/<space>/<id>.md
//
// Beside it, rather than in it, for the reason Task's comment gives: Snapshot
// copies the whole task list into as many as forty snapshots, so a few KB of
// markdown per task would be amplified across the entire history — the same
// blow-up Snapshot.ArchiveEntry already exists to have fixed once. Only the
// PlanAt stamp lives on the task.
//
// Beside the *data file* rather than under XDG_STATE_HOME because a plan is
// user data, not cached state: it must follow --file and IKE_DATA_FILE, so two
// matrices cannot share one set of plans, and it should be picked up by whatever
// backs up the data directory. The layout follows tasks.json.lock and
// tasks.json.bak, which are already siblings of the file they belong to.
//
// The path carries the space name because tasks are numbered per space, so
// task 3 in `work` and task 3 in `personal` are different tasks.

// planDirSuffix names the sidecar directory holding plan bodies.
const planDirSuffix = ".plans"

// planDir is the directory holding plans for one space.
func (s *Store) planDir(space string) string {
	return filepath.Join(s.path+planDirSuffix, space)
}

// planPath is the file holding one task's plan.
func (s *Store) planPath(space string, id int) string {
	return filepath.Join(s.planDir(space), strconv.Itoa(id)+".md")
}

// Plan returns the plan attached to a task, or "" if there is none.
//
// A missing file is not an error. The stamp on the task and the file on disk
// are written together under the lock, but a data file restored from a backup —
// or copied to a machine without the sidecar directory — can carry a stamp with
// no body, and refusing to read the matrix over that would be a poor trade.
func (s *Store) Plan(id int) (string, error) {
	d, err := s.Load()
	if err != nil {
		return "", err
	}
	if _, err := findTask(&d, id); err != nil {
		return "", err
	}
	return s.readPlan(d.Space, id)
}

func (s *Store) readPlan(space string, id int) (string, error) {
	b, err := os.ReadFile(s.planPath(space, id))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", s.redact(err)
	}
	// Sanitized on the way out as well as validated on the way in, the same
	// double guarantee DisplayTitle gives a title: this file is plain text on
	// disk that a hand edit, an older build, or a synced copy can have put
	// anything into, and it is bound for a terminal.
	return task.SanitizeBlock(string(b)), nil
}

// SetPlan attaches a plan to a task, replacing any existing one. A blank body
// clears it, the way a blank quadrant label resets to the default.
//
// The body is written inside the same Mutate callback that stamps PlanAt, so
// both are covered by one flock and the stamp cannot end up describing a file
// that was never written.
func (s *Store) SetPlan(id int, body string) (task.Task, Data, error) {
	if err := task.ValidatePlan(body); err != nil {
		return task.Task{}, Data{}, err
	}
	if body == "" {
		return s.ClearPlan(id)
	}

	var out task.Task
	d, err := s.mutateSpace(func(space string, d *Data) error {
		i, err := findTask(d, id)
		if err != nil {
			return err
		}
		// Written before pushUndo so a failing write records no history and
		// leaves the matrix untouched — the same reason pushUndo comes after
		// validation everywhere else in ops.go.
		if err := s.writePlan(space, id, body); err != nil {
			return err
		}
		pushUndo(d, fmt.Sprintf("plan %q", d.Tasks[i].Title))

		now := time.Now().UTC()
		d.Tasks[i].PlanAt = &now
		out = d.Tasks[i]
		return nil
	})
	return out, d, err
}

// ClearPlan removes a task's plan.
func (s *Store) ClearPlan(id int) (task.Task, Data, error) {
	var out task.Task
	d, err := s.mutateSpace(func(space string, d *Data) error {
		i, err := findTask(d, id)
		if err != nil {
			return err
		}
		if d.Tasks[i].PlanAt == nil {
			// No stamp, nothing to undo. Returning early rather than recording
			// a no-op snapshot keeps `ike undo` describing real changes.
			out = d.Tasks[i]
			return nil
		}
		pushUndo(d, fmt.Sprintf("clear the plan for %q", d.Tasks[i].Title))
		if err := s.removePlan(space, id); err != nil {
			return err
		}
		d.Tasks[i].PlanAt = nil
		out = d.Tasks[i]
		return nil
	})
	return out, d, err
}

// SetDir sets the working directory a delegated run executes in. A blank dir
// clears it.
//
// source names where the path came from, and is passed in rather than fixed
// here for the reason CheckPath's doc gives: the message should point at the
// thing the user typed, and that is `--dir` from the CLI but a prompt in the
// TUI.
func (s *Store) SetDir(id int, source, dir string) (task.Task, Data, error) {
	if dir != "" {
		p, err := CheckDir(source, dir)
		if err != nil {
			return task.Task{}, Data{}, err
		}
		dir = p
	}
	var out task.Task
	d, err := s.Mutate(func(d *Data) error {
		i, err := findTask(d, id)
		if err != nil {
			return err
		}
		if d.Tasks[i].Dir == dir {
			out = d.Tasks[i]
			return nil
		}
		if dir == "" {
			pushUndo(d, fmt.Sprintf("clear the directory for %q", d.Tasks[i].Title))
		} else {
			pushUndo(d, fmt.Sprintf("set the directory for %q", d.Tasks[i].Title))
		}
		d.Tasks[i].Dir = dir
		out = d.Tasks[i]
		return nil
	})
	return out, d, err
}

// writePlan replaces one task's plan file.
func (s *Store) writePlan(space string, id int, body string) error {
	dir := s.planDir(space)
	if err := os.MkdirAll(dir, dataDirMode); err != nil {
		return s.redact(err)
	}
	// Through the same atomic write the data file uses, so an interrupted write
	// leaves the previous plan rather than a truncated one. No .bak here: unlike
	// the matrix, a plan is one task's worth of text and is cheap to redraft.
	if err := writeBytesAtomic(s.planPath(space, id), ".plan-*.md", []byte(body)); err != nil {
		return s.redact(err)
	}
	return nil
}

func (s *Store) removePlan(space string, id int) error {
	err := os.Remove(s.planPath(space, id))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return s.redact(err)
	}
	return nil
}

// PrunePlans deletes plan files that belong to no active task, and reports how
// many it removed.
//
// Deleting a task deliberately does *not* delete its plan: Delete is undoable,
// so removing the body would make undo silently lossy — the task would come
// back with a PlanAt stamp and nothing behind it. Because NextID is monotonic
// and IDs are never reused, an orphan can never be picked up by a later task,
// so leaving it is safe and costs a few KB. This is the explicit sweep for
// anyone who wants the space back.
//
// Archived tasks keep their plans, since Restore brings them back active.
func (s *Store) PrunePlans() (int, error) {
	f, err := s.loadFile()
	if err != nil {
		return 0, err
	}

	removed := 0
	for space, d := range f.Spaces {
		live := make(map[int]bool, len(d.Tasks)+len(d.Archive))
		for _, t := range d.Tasks {
			live[t.ID] = true
		}
		for _, t := range d.Archive {
			live[t.ID] = true
		}

		entries, err := os.ReadDir(s.planDir(space))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return removed, s.redact(err)
		}
		for _, e := range entries {
			id, ok := planFileID(e.Name())
			if !ok || live[id] {
				continue
			}
			if err := os.Remove(filepath.Join(s.planDir(space), e.Name())); err != nil {
				return removed, s.redact(err)
			}
			removed++
		}
	}
	return removed, nil
}

// planFileID parses "<id>.md" back into a task ID. Anything else in the
// directory is left alone — a sweep that deleted files it did not recognize
// would be a poor thing to point at a directory inside someone's data folder.
func planFileID(name string) (int, bool) {
	base, ok := strings.CutSuffix(name, ".md")
	if !ok {
		return 0, false
	}
	id, err := strconv.Atoi(base)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}
