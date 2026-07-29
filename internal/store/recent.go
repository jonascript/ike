package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
)

// recentLimit caps the remembered file list. It is a convenience for picking
// between a handful of matrices, not a history.
const recentLimit = 10

// RecentFiles is the list of data files the TUI has opened, most recent first.
//
// It lives beside the data rather than inside it: a data file cannot list the
// others without every copy of it disagreeing, and this is machine-local
// convenience rather than part of anyone's matrix. It holds paths only — never
// task text — so it is not a second place personal content can leak from,
// though it is still written 0600 in a 0700 directory since a list of paths
// describes someone's filesystem.
type RecentFiles struct {
	Paths []string `json:"paths"`
}

// recentPath is the state file's location: XDG_STATE_HOME, else the documented
// default. It is deliberately not next to the data file, which --file can move
// anywhere.
func recentPath() (string, error) {
	dir := os.Getenv("XDG_STATE_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(dir, "ike", "recent.json"), nil
}

// LoadRecent reads the remembered file list.
//
// Every failure is a nil list rather than an error: this is a convenience, and
// a missing or unreadable state file must never be the reason the TUI cannot
// start.
func LoadRecent() RecentFiles {
	p, err := recentPath()
	if err != nil {
		return RecentFiles{}
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return RecentFiles{}
	}
	var r RecentFiles
	if err := json.Unmarshal(b, &r); err != nil {
		return RecentFiles{}
	}
	// Drop files that have since been moved or deleted, so the picker does not
	// offer a path that cannot open.
	kept := make([]string, 0, len(r.Paths))
	for _, path := range r.Paths {
		if _, err := os.Stat(path); err == nil {
			kept = append(kept, path)
		}
	}
	r.Paths = kept
	return r
}

// RememberRecent moves path to the front of the remembered list.
//
// Best effort by design: it returns no error, because failing to record a
// convenience must never interrupt opening a file. Only the TUI calls it —
// having every `ike list` write a second file would be a lot of writes for a
// list nothing but the picker reads.
func RememberRecent(path string) {
	p, err := recentPath()
	if err != nil {
		return
	}
	r := LoadRecent()
	r.Paths = slices.DeleteFunc(r.Paths, func(have string) bool { return have == path })
	r.Paths = append([]string{path}, r.Paths...)
	if len(r.Paths) > recentLimit {
		r.Paths = r.Paths[:recentLimit]
	}
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(p), dataDirMode); err != nil {
		return
	}
	// Written in place rather than through writeFileAtomic: there is no lock to
	// take and nothing here is worth recovering, so a torn write costs the list
	// and LoadRecent already treats an unparseable one as empty.
	_ = os.WriteFile(p, b, dataFileMode)
	_ = os.Chmod(p, dataFileMode)
}
