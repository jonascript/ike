package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// The version-5 layout splits the document across files: a small manifest at
// the data path holding the document-level fields, and one file per space in
// the .spaces sidecar directory. The two shapes below are what those files
// hold. File remains the in-memory document — these exist only at the disk
// boundary, so nothing above readTree/writeTree changes shape.

// manifest is the on-disk form of tasks.json at version 5: exactly the
// document-level fields, and deliberately no "spaces" key. A version-4 binary
// reading it therefore fails the version check outright rather than decoding
// an empty matrix it would overwrite — the same refusal the v3 and v4 bumps
// were chosen for.
type manifest struct {
	Version      int    `json:"version"`
	Current      string `json:"current"`
	MCPEnabled   bool   `json:"mcp_enabled,omitempty"`
	AgentEnabled bool   `json:"agent_enabled,omitempty"`
}

// spaceFile is the on-disk form of one space. The embedded name is canonical
// (the filename is derived from it and repaired on write), and the shape is
// self-describing so the same file works as the export format: consent flags
// have no field to travel in, which turns export's "never carry consent"
// guarantee from a decision into a property of the type.
type spaceFile struct {
	Version int    `json:"version"`
	Name    string `json:"name"`
	Data
}

// marshalJSONFile renders v the way every ike file is written: indented, with
// a trailing newline.
func marshalJSONFile(v any) ([]byte, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// spacesDir is the sidecar directory holding path's space files.
func spacesDir(path string) string { return path + spacesDirSuffix }

// readTree reads the whole document from disk: the file at path, plus — at
// version 5 — one file per space beside it. It replaces the old readFile and
// keeps its contract: reads never write, older versions are upgraded in
// memory and persisted by the next write, and a version newer than this build
// writes is refused outright.
func readTree(path string) (File, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		// No document file. A populated spaces directory beside the missing
		// path is still a document — losing the small manifest must not hide
		// every space — so it is reconstructed the way a dangling Current is
		// repaired: current becomes the alphabetically first space, and both
		// consent flags are off, consent being the one thing a repair must
		// never invent.
		if f, ok, derr := readOrphanSpaces(path); derr != nil || ok {
			return f, derr
		}
		return emptyFile(), nil
	}
	if err != nil {
		return File{}, err
	}
	var f File
	if err := json.Unmarshal(b, &f); err != nil {
		return File{}, fmt.Errorf("parsing %s: %w", path, err)
	}
	if f.Version < 1 || f.Version > currentVersion {
		return File{}, fmt.Errorf("%s has unsupported version %d (expected %d)", path, f.Version, currentVersion)
	}
	f.onDiskVersion = f.Version
	f.rawDoc = b

	// The discriminator between the shapes below is the version, never a
	// missing key. Every shape carries `version` at the top level, so it has
	// already decoded; going by absent keys instead would quietly accept a
	// truncated or hand-edited file as an empty matrix, and the next write
	// would erase the lot.
	switch {
	case f.Version < 4:
		// A single-matrix file: the body re-reads as one space.
		var d Data
		if err := json.Unmarshal(b, &d); err != nil {
			return File{}, fmt.Errorf("parsing %s: %w", path, err)
		}
		// History from before version 3 is dropped rather than reinterpreted.
		// Those snapshots stored a whole archive and no ArchiveEntry, so undoing
		// a restore recorded by an older build would silently lose that entry's
		// completion stamp. Losing undo history on a one-time upgrade is a far
		// better outcome than quietly losing an archived task, and the tasks
		// themselves are untouched either way.
		if f.Version < 3 {
			d.Undo, d.Redo = nil, nil
		}
		f.Spaces = map[string]*Data{defaultSpace: &d}
		f.Current = defaultSpace
	case f.Version == 4:
		// The whole document in one file; the envelope has already decoded
		// into f.Spaces.
	default:
		// Version 5: either the manifest of a split document, or a single
		// space file handed to --file. A space file is the only v5 shape with
		// a name, so that is the discriminator — the manifest deliberately
		// has no such key.
		var sf spaceFile
		if err := json.Unmarshal(b, &sf); err != nil {
			return File{}, fmt.Errorf("parsing %s: %w", path, err)
		}
		if sf.Name != "" {
			d := sf.Data
			f.Current = sf.Name
			f.Spaces = map[string]*Data{sf.Name: &d}
			f.rawSpace = map[string][]byte{sf.Name: b}
			f.standalone = true
			// A space file has no field for either consent flag, but the
			// envelope decode above shares File's json tags with the v4 shape,
			// so a crafted file could smuggle them in. Consent never travels.
			f.MCPEnabled, f.AgentEnabled = false, false
			break
		}
		if err := readSpaces(path, &f); err != nil {
			return File{}, err
		}
	}
	// A version-5 manifest with no space files gets the same refusal as a v4
	// envelope with no spaces: a truncated or half-copied tree must be
	// refused, not accepted as an empty matrix the next write makes permanent.
	// Spaces that exist but cannot be parsed count as present here — they are
	// exactly what a degraded open exists to keep answering about.
	if len(f.Spaces) == 0 && len(f.corrupt) == 0 {
		return File{}, fmt.Errorf("%s has no spaces", path)
	}
	f.Version = currentVersion

	for name, d := range f.Spaces {
		if d == nil {
			return File{}, fmt.Errorf("%s has an empty space %q", path, name)
		}
		if d.NextID < 1 {
			d.NextID = 1
		}
		// Before normalizeRanks, so a task rescued from an invalid quadrant gets
		// a rank in the quadrant it lands in.
		clampQuadrants(d)
		// Every space, not just the one about to be read. A write persists them
		// all, so a space left un-normalized would have its pre-rank ordering
		// rewritten by whichever mutation happened to touch a different space.
		normalizeRanks(d)
	}
	// A current that names nothing — a hand edit, or a space removed by a build
	// that did not follow it — would otherwise break every command until it was
	// fixed by hand. Repair it the way an out-of-range NextID is repaired. An
	// explicitly requested space that is missing still fails: "the file is
	// inconsistent" and "you asked for something that is not there" are
	// different situations and deserve different answers. When every space is
	// unreadable there is nothing to repair toward, and Current is left alone
	// so the listing still says which space was current.
	if _, ok := f.Spaces[f.Current]; !ok && len(f.Spaces) > 0 {
		f.Current = slices.Min(slices.Collect(maps.Keys(f.Spaces)))
	}
	return f, nil
}

// readSpaces reads every space file in path's spaces directory into f.
//
// A file that cannot be read or parsed marks its space corrupt instead of
// failing the document — one bad space costing every other space is exactly
// what the split layout exists to prevent. A corrupt space lands in f.corrupt
// and nowhere else, so nothing downstream can write to, over, or instead of
// its file.
func readSpaces(path string, f *File) error {
	dir := spacesDir(path)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	f.Spaces = map[string]*Data{}
	f.rawSpace = map[string][]byte{}
	f.fileFor = map[string]string{}
	// ReadDir returns entries sorted by filename, which is what makes "the
	// lexicographically first file wins" below deterministic.
	for _, e := range entries {
		fname := e.Name()
		// Dotfiles are the atomic-write temp files; encodeSpaceFilename
		// escapes a leading dot, so no real space is ever skipped by this.
		if e.IsDir() || strings.HasPrefix(fname, ".") {
			continue
		}
		derived, ok := decodeSpaceFilename(fname)
		if !ok {
			continue // .bak siblings and other strays are not space files
		}
		b, err := os.ReadFile(filepath.Join(dir, fname))
		if err != nil {
			f.markCorrupt(derived, err)
			continue
		}
		var sf spaceFile
		if err := json.Unmarshal(b, &sf); err != nil {
			f.markCorrupt(derived, err)
			continue
		}
		if sf.Version < 1 || sf.Version > currentVersion {
			f.markCorrupt(derived, fmt.Errorf("unsupported version %d (expected %d)", sf.Version, currentVersion))
			continue
		}
		if sf.Name == "" {
			f.markCorrupt(derived, errors.New("space file has no name"))
			continue
		}
		// The embedded name is canonical; the filename is derived from it and
		// repaired on the next write if they disagree. Two files claiming one
		// name would otherwise be one space with two futures: the first
		// filename wins, the other is surfaced rather than silently shadowed,
		// and — being corrupt — its file can never be written or deleted.
		if prior, dup := f.fileFor[sf.Name]; dup {
			f.markCorrupt(derived, fmt.Errorf("%s and %s both claim the space %q; %s wins", prior, fname, sf.Name, prior))
			continue
		}
		d := sf.Data
		f.Spaces[sf.Name] = &d
		f.rawSpace[sf.Name] = b
		f.fileFor[sf.Name] = fname
	}
	return nil
}

// readOrphanSpaces reconstructs a document from a spaces directory whose
// manifest is missing. It reports ok=false when there is nothing there —
// the ordinary fresh-start case.
func readOrphanSpaces(path string) (File, bool, error) {
	f := File{Version: currentVersion}
	if err := readSpaces(path, &f); err != nil {
		return File{}, false, err
	}
	if len(f.Spaces) == 0 && len(f.corrupt) == 0 {
		return File{}, false, nil
	}
	if len(f.Spaces) > 0 {
		f.Current = slices.Min(slices.Collect(maps.Keys(f.Spaces)))
	}
	for _, d := range f.Spaces {
		if d.NextID < 1 {
			d.NextID = 1
		}
		clampQuadrants(d)
		normalizeRanks(d)
	}
	return f, true, nil
}

func (f *File) markCorrupt(name string, err error) {
	if f.corrupt == nil {
		f.corrupt = map[string]error{}
	}
	f.corrupt[name] = err
}
