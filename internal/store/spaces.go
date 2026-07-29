package store

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

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
