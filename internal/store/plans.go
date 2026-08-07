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

// PlanDraftPath is where an interactive agent session is told to leave the plan
// it agreed with you, for AdoptPlanDraft to pick up.
//
// It sits beside the plan itself and is stable per task, which is what makes it
// usable across sessions: the instruction naming it is in the conversation's
// history, so a session resumed next week still writes to somewhere ike looks.
// A temp path would have gone stale the moment the first session ended.
//
// A draft rather than the plan file directly, because the store is the only
// thing that writes that: routing through SetPlan keeps the atomic write, the
// validation, the PlanAt stamp, and the undo entry, none of which an agent
// writing the file itself would produce.
// The directory is created here, not just named. A task with no plan yet has no
// plan directory either, so handing an agent a path inside one that does not
// exist would make the write fail — and the plan agreed in the conversation
// would be lost at the last step, which is the worst possible moment.
func (s *Store) PlanDraftPath(id int) (string, error) {
	d, err := s.Load()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(s.planDir(d.Space), dataDirMode); err != nil {
		return "", s.redact(err)
	}
	return s.planDraftPath(d.Space, id), nil
}

func (s *Store) planDraftPath(space string, id int) string {
	return filepath.Join(s.planDir(space), strconv.Itoa(id)+".draft.md")
}

// AdoptPlanDraft attaches whatever an agent left at the draft path, and reports
// whether there was anything to attach.
//
// Called after an interactive session ends. A missing or blank draft is the
// ordinary case — most conversations do not end in a plan — so it is not an
// error.
func (s *Store) AdoptPlanDraft(id int) (task.Task, Data, bool, error) {
	d, err := s.Load()
	if err != nil {
		return task.Task{}, Data{}, false, err
	}
	draft := s.planDraftPath(d.Space, id)

	b, err := os.ReadFile(draft)
	if errors.Is(err, os.ErrNotExist) {
		return task.Task{}, d, false, nil
	}
	if err != nil {
		return task.Task{}, Data{}, false, s.redact(err)
	}
	body := strings.TrimSpace(task.SanitizeBlock(string(b)))
	if body == "" {
		_ = os.Remove(draft)
		return task.Task{}, d, false, nil
	}

	t, out, err := s.SetPlan(id, body)
	if err != nil {
		// Deliberately left in place. The draft is the only copy of work the
		// agent just did, and removing it because ike could not store it would
		// destroy it — a plan too long to validate is still recoverable by hand
		// from the path AdoptPlanDraft was about to read.
		return task.Task{}, Data{}, false, err
	}
	_ = os.Remove(draft)
	return t, out, true, nil
}

// SetSession pins an interactive conversation to a task, so later visits resume
// it rather than starting over. A blank id forgets the conversation.
func (s *Store) SetSession(id int, sessionID string) (task.Task, Data, error) {
	var out task.Task
	d, err := s.Mutate(func(d *Data) error {
		i, err := findTask(d, id)
		if err != nil {
			return err
		}
		if d.Tasks[i].SessionID == sessionID {
			out = d.Tasks[i]
			return nil
		}
		// No pushUndo. A session ID is a pointer to a conversation that exists
		// outside ike, and undoing back to a previous one would resume a
		// conversation the user has moved on from — closer to reopening revoked
		// MCP access than to reversing an edit.
		d.Tasks[i].SessionID = sessionID
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
//
// It also sweeps the plan directories of spaces that no longer exist —
// removing or renaming a space historically left its plans behind with
// nothing pointing at them. A directory belonging to an *unreadable* space is
// left strictly alone: its space still exists, just in a file that needs
// recovery, and the plans may be the only part of it still readable.
func (s *Store) PrunePlans() (int, error) {
	f, err := s.loadFile()
	if err != nil {
		return 0, err
	}

	removed := 0
	if entries, err := os.ReadDir(s.path + planDirSuffix); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			name := e.Name()
			if _, _, err := f.resolve(name); err == nil {
				continue // a live space's plans
			}
			if _, _, ok := f.corruptNamed(name); ok {
				continue // an unreadable space still owns its plans
			}
			dir := filepath.Join(s.path+planDirSuffix, name)
			n, err := countPlanFiles(dir)
			if err != nil {
				return removed, s.redact(err)
			}
			if err := os.RemoveAll(dir); err != nil {
				return removed, s.redact(err)
			}
			removed += n
		}
	}
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

// countPlanFiles counts the plan files in one space's plan directory, so a
// sweep of the whole directory can report how many plans it removed rather
// than how many directories.
func countPlanFiles(dir string) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, e := range entries {
		if _, ok := planFileID(e.Name()); ok {
			n++
		}
	}
	return n, nil
}

// planFileID parses "<id>.md" or "<id>.draft.md" back into a task ID. Anything
// else in the directory is left alone — a sweep that deleted files it did not
// recognize would be a poor thing to point at a directory inside someone's data
// folder.
//
// Drafts are included so a conversation abandoned half way does not leave a
// file that nothing ever cleans up.
func planFileID(name string) (int, bool) {
	base, ok := strings.CutSuffix(name, ".md")
	if !ok {
		return 0, false
	}
	base = strings.TrimSuffix(base, ".draft")
	id, err := strconv.Atoi(base)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}
