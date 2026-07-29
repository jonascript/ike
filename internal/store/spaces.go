package store

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/jonascript/ike/internal/task"
)

// NotEmptyError reports that a space still holds tasks, and how many.
//
// It carries the counts rather than a finished sentence so the wording lives
// with the rest of the frontend's wording. The store had grown two helpers to
// phrase one error while the CLI phrased the same numbers differently a package
// away, which is one vocabulary too many for one concept.
type NotEmptyError struct {
	Space SpaceInfo
}

func (e *NotEmptyError) Error() string {
	return fmt.Sprintf("%q is not empty (%d active, %d archived)",
		e.Space.Name, e.Space.Active, e.Space.Archived)
}

// SpaceInfo describes one space for a picker or listing. It carries counts
// rather than tasks: a Data hands one of these back for every space in the file
// on every read, and copying per-task state N times is exactly the blow-up
// Snapshot.ArchiveEntry exists to undo.
type SpaceInfo struct {
	Name     string `json:"name"`
	Active   int    `json:"active"`
	Archived int    `json:"archived"`
	Current  bool   `json:"current"`
}

// resolve returns the space an operation should act on, and its canonical name:
// the one requested, or whichever the file records as current when the request
// is empty.
//
// It never creates. A name that is not in the file is an error, so a typo
// cannot conjure an empty matrix and quietly swallow the tasks that were meant
// for a real one — and an MCP client, which has no tool for making a space,
// cannot make one through a misspelled space argument either.
func (f *File) resolve(name string) (string, *Data, error) {
	if name == "" {
		name = f.Current
	}
	if d, ok := f.Spaces[name]; ok {
		return name, d, nil
	}
	// An exact match wins; a case-insensitive one is unambiguous because names
	// differing only by case are rejected when a space is created or renamed.
	for have, d := range f.Spaces {
		if strings.EqualFold(have, name) {
			return have, d, nil
		}
	}
	return "", nil, fmt.Errorf("no space named %q", name)
}

// dataFor returns one space's data with the document-level fields frontends
// render from filled in. Those are derived on every read rather than stored,
// because they describe the file rather than the matrix — and because a
// frontend must render the outcome of an operation from what that operation
// returned, not from a second read that would observe a later file state.
func (f *File) dataFor(name string, d *Data) Data {
	out := *d
	out.Space = name
	out.AllSpaces = f.spaceInfos()
	out.MCPAllowed = f.MCPEnabled
	return out
}

// spaceInfos describes every space in the file, sorted by name. Sorted rather
// than in map order so a picker, `ike space list`, and the TUI's next/previous
// keys all agree on what "the space after this one" means.
func (f *File) spaceInfos() []SpaceInfo {
	out := make([]SpaceInfo, 0, len(f.Spaces))
	for _, name := range slices.Sorted(maps.Keys(f.Spaces)) {
		d := f.Spaces[name]
		out = append(out, SpaceInfo{
			Name:     name,
			Active:   len(d.Tasks),
			Archived: len(d.Archive),
			Current:  name == f.Current,
		})
	}
	return out
}

// checkNewName validates a name for a space that does not exist yet.
//
// The collision check is case-insensitive even though lookup is not. Two spaces
// differing only by case are indistinguishable at a glance in every listing,
// and `-s Work` silently missing `work` is a worse outcome than being told the
// name is taken. Rejecting them here is also what makes resolve's
// case-insensitive fallback unambiguous.
func (f *File) checkNewName(name string) error {
	if err := task.ValidateSpaceName(name); err != nil {
		return err
	}
	for have := range f.Spaces {
		if strings.EqualFold(have, name) {
			return fmt.Errorf("a space named %q already exists", have)
		}
	}
	return nil
}

// The space operations below are deliberately **not undoable**. History is
// per-space, so a document-level change has no stack to record onto, and a
// stack that could resurrect a removed space would have to hold the whole
// matrix. `tasks.json.bak` remains the recovery path for a removal, which is
// why RemoveSpace makes the caller say the name and confirm the loss.

// ListSpaces describes every space in the file, sorted by name.
func (s *Store) ListSpaces() ([]SpaceInfo, error) {
	// loadFile, not loadResolved: this describes the document, and must answer
	// even when the space this Store is pinned to is not in it.
	f, err := s.loadFile()
	if err != nil {
		return nil, err
	}
	return f.spaceInfos(), nil
}

// NewSpace adds an empty space. It does not switch to it: creating a space from
// a script should not change what the next bare `ike list` shows, and the two
// steps read clearly enough separately.
//
// It returns the current space's data, unchanged apart from now listing the new
// space, so a caller re-renders from the same value every other operation hands
// back.
func (s *Store) NewSpace(name string) (Data, error) {
	name = strings.TrimSpace(name)
	var out Data
	_, err := s.mutateFile(func(f *File) error {
		if err := f.checkNewName(name); err != nil {
			return err
		}
		f.Spaces[name] = &Data{NextID: 1}
		current, d, err := f.resolve(s.space)
		if err != nil {
			return err
		}
		out = f.dataFor(current, d)
		return nil
	})
	return out, err
}

// UseSpace makes name the current space and returns its data, so the caller
// renders the space it just switched to without a second read.
func (s *Store) UseSpace(name string) (Data, error) {
	name = strings.TrimSpace(name)
	var out Data
	_, err := s.mutateFile(func(f *File) error {
		canonical, d, err := f.resolve(name)
		if err != nil {
			return err
		}
		f.Current = canonical
		out = f.dataFor(canonical, d)
		return nil
	})
	return out, err
}

// RenameSpace renames a space, following Current if it was the one renamed.
// Nothing inside a space refers to it by name, so its history survives intact.
func (s *Store) RenameSpace(from, to string) error {
	from, to = strings.TrimSpace(from), strings.TrimSpace(to)
	_, err := s.mutateFile(func(f *File) error {
		canonical, d, err := f.resolve(from)
		if err != nil {
			return err
		}
		if canonical == to {
			return nil
		}
		// Checked after resolving, so renaming a space to a different casing of
		// its own name is not blocked by the space itself.
		delete(f.Spaces, canonical)
		if err := f.checkNewName(to); err != nil {
			f.Spaces[canonical] = d
			return err
		}
		f.Spaces[to] = d
		if f.Current == canonical {
			f.Current = to
		}
		return nil
	})
	return err
}

// RemoveSpace deletes a space and everything in it. It reports what was
// destroyed, so a caller can say so afterwards.
//
// Unlike deleting a task this cannot be undone, so it refuses a space holding
// anything unless force says otherwise, and it refuses the last space outright
// — a file with no spaces is not a state any other code path is prepared for.
// Removing the current space moves Current to the alphabetically first
// survivor rather than leaving it dangling.
func (s *Store) RemoveSpace(name string, force bool) (SpaceInfo, error) {
	name = strings.TrimSpace(name)
	var removed SpaceInfo
	_, err := s.mutateFile(func(f *File) error {
		canonical, d, err := f.resolve(name)
		if err != nil {
			return err
		}
		if len(f.Spaces) == 1 {
			return fmt.Errorf("%q is the only space; there has to be one", canonical)
		}
		removed = SpaceInfo{
			Name:     canonical,
			Active:   len(d.Tasks),
			Archived: len(d.Archive),
			Current:  canonical == f.Current,
		}
		if !force && (removed.Active > 0 || removed.Archived > 0) {
			return &NotEmptyError{Space: removed}
		}
		delete(f.Spaces, canonical)
		if f.Current == canonical {
			f.Current = slices.Min(slices.Collect(maps.Keys(f.Spaces)))
		}
		return nil
	})
	return removed, err
}
