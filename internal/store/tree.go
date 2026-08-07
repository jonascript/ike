package store

import (
	"bytes"
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
		// A current that names an *unreadable* space is not dangling, and is
		// deliberately left alone: repairing it away would have the next bare
		// `ike list` quietly show some other space instead of saying, loudly,
		// that the one the user works in needs attention.
		if _, _, isCorrupt := f.corruptNamed(f.Current); !isCorrupt {
			f.Current = slices.Min(slices.Collect(maps.Keys(f.Spaces)))
		}
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
			f.markCorrupt(derived, fname, err)
			continue
		}
		var sf spaceFile
		if err := json.Unmarshal(b, &sf); err != nil {
			f.markCorrupt(derived, fname, err)
			continue
		}
		if sf.Version < 1 || sf.Version > currentVersion {
			f.markCorrupt(derived, fname, fmt.Errorf("unsupported version %d (expected %d)", sf.Version, currentVersion))
			continue
		}
		if sf.Name == "" {
			f.markCorrupt(derived, fname, errors.New("space file has no name"))
			continue
		}
		// The embedded name is canonical; the filename is derived from it and
		// repaired on the next write if they disagree. Two files claiming one
		// name would otherwise be one space with two futures: the first
		// filename wins, the other is surfaced rather than silently shadowed,
		// and — being corrupt — its file can never be written or deleted.
		if prior, dup := f.fileFor[sf.Name]; dup {
			f.markCorrupt(derived, fname, fmt.Errorf("%s and %s both claim the space %q; %s wins", prior, fname, sf.Name, prior))
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

// corruptSpace is one unreadable space: which file it is stuck in, and why.
type corruptSpace struct {
	file string // filename within the spaces directory
	err  error
}

func (f *File) markCorrupt(name, file string, err error) {
	if f.corrupt == nil {
		f.corrupt = map[string]corruptSpace{}
	}
	f.corrupt[name] = corruptSpace{file: file, err: err}
}

// corruptNamed looks name up among the unreadable spaces, resolving the way
// resolve does: empty means current, and a case-insensitive match counts.
func (f *File) corruptNamed(name string) (string, corruptSpace, bool) {
	if name == "" {
		name = f.Current
	}
	if cs, ok := f.corrupt[name]; ok {
		return name, cs, true
	}
	for have, cs := range f.corrupt {
		if strings.EqualFold(have, name) {
			return have, cs, true
		}
	}
	return "", corruptSpace{}, false
}

// readDocFlags reads only the document file's manifest-level fields. The
// consent readers use it so that `ike mcp status` and `ike agent status` keep
// answering whatever state the space files are in. Every version carries these
// fields at the top level, so no version branch is needed; a standalone space
// file simply has neither flag, which is the "consent never travels" guarantee
// again.
func readDocFlags(path string) (manifest, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return manifest{}, nil
	}
	if err != nil {
		return manifest{}, err
	}
	var m struct {
		manifest
		Name string `json:"name"`
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return manifest{}, fmt.Errorf("parsing %s: %w", path, err)
	}
	if m.Name != "" {
		// A space file, not a document. Its shape has no consent fields, but a
		// crafted one could carry them anyway; a standalone open never has
		// consent, so neither may the flags read from one.
		return manifest{Version: m.Version}, nil
	}
	return m.manifest, nil
}

// preV5BackupSuffix names the one-time copy of a pre-split monolith, taken
// before the first version-5 write replaces it. The rolling .bak is clobbered
// by the very next manifest write, so without this the pre-migration state
// would survive exactly one mutation.
const preV5BackupSuffix = ".pre-v5.bak"

// writeTree persists the document f to disk: each space to its own file, the
// manifest last among updates, deletions after that. It is the commit step of
// mutateFile and nothing else calls it, so the lock, the fresh re-read, and
// the gate still have exactly one implementation.
//
// Writes are confined to what changed: a space whose marshaling matches the
// bytes it was read from is left alone. Fault tolerance is the point — a bug
// in one space's mutation can no longer rewrite the others — and it is also
// what keeps the ordering rule cheap. That rule: creations and updates first,
// then the manifest, then deletions. Every crash window then leaves a state
// readTree already repairs — at worst an extra space file the manifest does
// not point to, or a dangling Current.
//
// A space readTree marked corrupt is in neither f.Spaces nor f.rawSpace, so
// this function cannot write to, over, or instead of its file: the
// reads-never-overwrite-what-they-cannot-parse rule, extended per file.
func writeTree(path string, f *File) error {
	if f.standalone {
		return writeStandalone(path, f)
	}
	dir := spacesDir(path)

	// Migrating from a monolith. The monolith stays authoritative until the
	// manifest write below replaces it: a crash anywhere before that leaves a
	// valid pre-v5 file the next reader upgrades again, and an older binary
	// running mid-window still sees a document it understands.
	migrating := f.onDiskVersion >= 1 && f.onDiskVersion < currentVersion
	if migrating {
		if err := writeFileOnce(path+preV5BackupSuffix, f.rawDoc); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(dir, dataDirMode); err != nil {
		return err
	}
	// MkdirAll leaves an existing directory's mode alone; the listing of
	// space names is as personal as the spaces, so tighten it the way a
	// skipped write tightens a file.
	_ = os.Chmod(dir, dataDirMode)
	if migrating {
		// Any space file already present is debris of a migration that crashed
		// before its commit point. Cleared rather than trusted, or a space
		// deleted since that crash would resurrect beside the real ones.
		entries, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if _, ok := decodeSpaceFilename(e.Name()); ok && !strings.HasPrefix(e.Name(), ".") {
				if err := os.Remove(filepath.Join(dir, e.Name())); err != nil {
					return err
				}
			}
		}
	}

	// Creations and updates, in sorted order so a partial failure is
	// reproducible.
	for _, name := range slices.Sorted(maps.Keys(f.Spaces)) {
		b, err := marshalJSONFile(spaceFile{Version: currentVersion, Name: name, Data: *f.Spaces[name]})
		if err != nil {
			return err
		}
		canonical := encodeSpaceFilename(name)
		target := filepath.Join(dir, canonical)
		dirty := !bytes.Equal(b, f.rawSpace[name])
		if dirty {
			if err := writeBackup(target); err != nil {
				return err
			}
			if err := writeBytesAtomic(target, ".space-*.json", b); err != nil {
				return err
			}
		} else {
			// The monolith relied on every write replacing the inode to
			// tighten a file left world-readable by an older build. A skipped
			// write must keep that promise by hand.
			_ = os.Chmod(target, dataFileMode)
		}
		// The filename is derived from the canonical embedded name; a file
		// read from anywhere else — a hand copy, a filesystem that normalized
		// the encoding — is moved home. Best effort: a repair must never be
		// the reason a mutation fails, and a straggler is surfaced by the
		// duplicate-name handling on the next read rather than lost.
		if prior := f.fileFor[name]; prior != "" && prior != canonical {
			priorPath := filepath.Join(dir, prior)
			switch {
			case sameFile(priorPath, target):
				// A case-insensitive filesystem: one file, wrongly-cased
				// entry. Renaming it in place fixes the case.
				_ = os.Rename(priorPath, target)
			case dirty:
				// target was written fresh above; the old file is superseded.
				_ = os.Rename(priorPath, priorPath+".bak")
			default:
				_ = os.Rename(priorPath, target)
			}
		}
	}

	// The manifest, only if it changed. During a migration it always has —
	// the monolith's bytes are not a manifest — and this write is the commit
	// point that retires the monolith.
	mb, err := marshalJSONFile(manifest{
		Version:      currentVersion,
		Current:      f.Current,
		MCPEnabled:   f.MCPEnabled,
		AgentEnabled: f.AgentEnabled,
	})
	if err != nil {
		return err
	}
	if !bytes.Equal(mb, f.rawDoc) {
		if err := writeBackup(path); err != nil {
			return err
		}
		if err := writeBytesAtomic(path, ".tasks-*.json", mb); err != nil {
			return err
		}
	} else {
		_ = os.Chmod(path, dataFileMode)
	}

	// Deletions last, so a crash strands an extra file rather than losing one.
	// Deleting is renaming to .bak: removal and backup in one atomic step,
	// which keeps ".bak is the recovery path for a removal" true now that the
	// document file no longer holds spaces at all.
	for _, name := range slices.Sorted(maps.Keys(f.rawSpace)) {
		if _, live := f.Spaces[name]; live {
			continue
		}
		fname := f.fileFor[name]
		if fname == "" {
			continue
		}
		full := filepath.Join(dir, fname)
		// A rename that changed only the name's case shares one file between
		// the old entry and the new on a case-insensitive filesystem — the
		// update above already wrote the new content into it, and "deleting"
		// the old entry here would delete the file it just wrote. Fix the
		// entry's case instead.
		caseOnly := false
		for live := range f.Spaces {
			target := filepath.Join(dir, encodeSpaceFilename(live))
			if strings.EqualFold(fname, encodeSpaceFilename(live)) && sameFile(full, target) {
				_ = os.Rename(full, target)
				caseOnly = true
				break
			}
		}
		if caseOnly {
			continue
		}
		if err := os.Rename(full, full+".bak"); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	// Files condemned by RemoveSpace on an unreadable space — the one write
	// ever aimed at a corrupt file, and it too is a rename to .bak.
	for _, fname := range f.removeCorrupt {
		full := filepath.Join(dir, fname)
		if err := os.Rename(full, full+".bak"); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

// writeStandalone writes the one space of a Store opened directly on a space
// file back to that file. The lifecycle operations refuse on a standalone
// document, so exactly one space can be here.
func writeStandalone(path string, f *File) error {
	if len(f.Spaces) != 1 {
		return fmt.Errorf("standalone %s must hold exactly one space, has %d", path, len(f.Spaces))
	}
	for name, d := range f.Spaces {
		b, err := marshalJSONFile(spaceFile{Version: currentVersion, Name: name, Data: *d})
		if err != nil {
			return err
		}
		if bytes.Equal(b, f.rawSpace[name]) {
			return nil
		}
		if err := writeBackup(path); err != nil {
			return err
		}
		return writeBytesAtomic(path, ".space-*.json", b)
	}
	return nil
}

// writeFileOnce creates path with b unless it already exists. O_EXCL rather
// than a stat-then-write, so two racing migrations cannot both think they
// wrote first.
func writeFileOnce(path string, b []byte) error {
	out, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, dataFileMode)
	if errors.Is(err, os.ErrExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := out.Write(b); err != nil {
		out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// sameFile reports whether two paths name one file, which on a
// case-insensitive filesystem two differently-cased names do.
func sameFile(a, b string) bool {
	fa, err := os.Stat(a)
	if err != nil {
		return false
	}
	fb, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(fa, fb)
}
