package store

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
)

// Moving spaces between files: export writes one out as a standalone data file,
// import copies them in. Together they are how a matrix reaches another machine
// — export, copy the one file, import — which is why they live beside the space
// operations rather than inside them.

// ExportSpace writes one space to path as a standalone space file — the same
// shape the space's own file in the .spaces directory holds, so an export is
// the portability goal made literal: copying the file and exporting it produce
// the same bytes. It opens with `ike --file` and imports with `ike space
// import`.
//
// Both consent flags — MCP access and agent delegation — are deliberately left
// off in the exported file, whatever they are here. Consent is a decision about
// a file on a machine, and an export exists to be copied elsewhere: carrying
// "agents may read this", still less "ike may start an agent", along to a
// machine whose owner never said so would be the wrong default in the one
// direction that matters. Since version 5 the space-file shape has no field for
// either flag, so this holds by construction rather than by decision.
//
// Plan bodies are *not* exported. They live beside the data file rather than in
// it (see plans.go), so an export carries the tasks and their PlanAt stamps but
// not the markdown. That is a known gap rather than a decision — a task landing
// on the other machine will report a plan it cannot show.
//
// It refuses to overwrite unless force, and writes through the same atomic path
// as any other write, so an interrupted export cannot leave a half file that
// still looks importable.
func (s *Store) ExportSpace(name, path string, force bool) (SpaceInfo, error) {
	p, err := CheckPath("export path", path)
	if err != nil {
		return SpaceInfo{}, err
	}
	_, canonical, d, err := s.loadResolved(name)
	if err != nil {
		return SpaceInfo{}, err
	}
	if !force {
		if _, err := os.Stat(p); err == nil {
			return SpaceInfo{}, fmt.Errorf("%s already exists; pass --force to replace it", p)
		} else if !errors.Is(err, os.ErrNotExist) {
			return SpaceInfo{}, err
		}
	}
	b, err := marshalJSONFile(spaceFile{Version: currentVersion, Name: canonical, Data: *d})
	if err != nil {
		return SpaceInfo{}, err
	}
	if err := writeBackup(p); err != nil {
		return SpaceInfo{}, err
	}
	if err := writeBytesAtomic(p, ".space-*.json", b); err != nil {
		return SpaceInfo{}, err
	}
	return SpaceInfo{
		Name:     canonical,
		Active:   len(d.Tasks),
		Archived: len(d.Archive),
	}, nil
}

// ImportSpaces copies spaces out of another ike data file into this one.
//
// By default it takes that file's current space; all takes every space in it.
// A name already in use is an error rather than a merge — combining two
// matrices would have to reconcile two ID sequences and two histories, and
// silently interleaving someone's work is worse than asking them to pick a
// name. as renames a single imported space on the way in.
//
// Task IDs need no renumbering: next_id belongs to the space and travels with
// it. Neither consent flag is ever carried in, for the same reason export never
// writes them out.
func (s *Store) ImportSpaces(path, as string, all bool) ([]SpaceInfo, error) {
	p, err := CheckPath("import path", path)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(p); err != nil {
		return nil, err
	}
	src, err := readTree(p)
	if err != nil {
		return nil, err
	}
	as = strings.TrimSpace(as)
	if all && as != "" {
		return nil, errors.New("--as renames a single space; it cannot be combined with --all")
	}

	// Which spaces to take, in a stable order so the report reads the same way
	// twice and a partial failure is reproducible.
	take := []string{src.Current}
	if all {
		take = slices.Sorted(maps.Keys(src.Spaces))
	}

	var imported []SpaceInfo
	_, err = s.mutateFile(func(f *File) error {
		if err := f.checkLifecycle("import into this file"); err != nil {
			return err
		}
		imported = nil
		for _, from := range take {
			d, ok := src.Spaces[from]
			if !ok {
				return fmt.Errorf("%s has no space named %q", p, from)
			}
			to := from
			if as != "" {
				to = as
			}
			if err := f.checkNewName(to); err != nil {
				return err
			}
			copied := *d
			f.Spaces[to] = &copied
			imported = append(imported, SpaceInfo{
				Name:     to,
				Active:   len(copied.Tasks),
				Archived: len(copied.Archive),
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return imported, nil
}
