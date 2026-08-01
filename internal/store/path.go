package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DataFile returns the path of the tasks database, honoring (in order):
// IKE_DATA_FILE, $XDG_DATA_HOME/ike/tasks.json, ~/.local/share/ike/tasks.json.
func DataFile() (string, error) {
	if p := os.Getenv("IKE_DATA_FILE"); p != "" {
		return checkOverride(p)
	}
	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dataHome = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(dataHome, "ike", "tasks.json"), nil
}

// checkOverride validates an IKE_DATA_FILE value.
func checkOverride(p string) (string, error) { return CheckPath("IKE_DATA_FILE", p) }

// CheckPath validates a user-supplied data file path. source names where the
// path came from, so the message points at the thing the user typed.
//
// The path is set by the user, so this is not a privilege boundary — it is a
// footgun guard. A relative path resolves against whatever working directory
// the process inherited, which for an MCP server launched by a client is not a
// directory the user chose or can predict, so the same setting would find
// different matrices depending on how ike was started. An unexpanded
// "~/tasks.json" from a config file (where no shell expands it) would create a
// directory literally named "~". And because writes create missing parents, a
// typo used to silently build a whole directory tree instead of failing.
//
// --file goes through the same rules as the environment variable rather than
// its own laxer copy: the two name the same thing, and it would be strange for
// a path to be acceptable one way and rejected the other.
func CheckPath(source, p string) (string, error) {
	if strings.HasPrefix(p, "~") {
		return "", fmt.Errorf("%s=%q starts with ~, which only a shell expands; "+
			"write the path out in full", source, p)
	}
	if !filepath.IsAbs(p) {
		return "", fmt.Errorf("%s=%q must be an absolute path, so ike finds the "+
			"same matrix whatever directory it runs from", source, p)
	}
	dir := filepath.Dir(p)
	fi, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("%s=%q: directory %s does not exist "+
				"(create it first, so a typo cannot invent one)", source, p, dir)
		}
		return "", fmt.Errorf("%s=%q: %w", source, p, err)
	}
	if !fi.IsDir() {
		return "", fmt.Errorf("%s=%q: %s is not a directory", source, p, dir)
	}
	return p, nil
}

// CheckDir validates a user-supplied working directory — the one a delegated
// run executes in. It is CheckPath's rules applied one level down: where
// CheckPath wants the *parent* of a file to exist, this wants the path itself
// to be a directory that is already there.
//
// The reasoning is CheckPath's, and matters more here. A relative directory
// resolves against whatever working directory the frontend inherited, which for
// a task run from the TUI is wherever the terminal happened to be — so the same
// stored value would send an agent somewhere different each time. And this
// directory is not merely read: it is where a subprocess will edit files, so
// "create it if it is missing" would let a typo point a run at a fresh empty
// directory instead of failing.
func CheckDir(source, p string) (string, error) {
	if strings.HasPrefix(p, "~") {
		return "", fmt.Errorf("%s=%q starts with ~, which only a shell expands; "+
			"write the path out in full", source, p)
	}
	if !filepath.IsAbs(p) {
		return "", fmt.Errorf("%s=%q must be an absolute path, so a task stays "+
			"attached to the same directory whatever directory ike runs from", source, p)
	}
	fi, err := os.Stat(p)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("%s=%q does not exist "+
				"(create it first, so a typo cannot invent one)", source, p)
		}
		return "", fmt.Errorf("%s=%q: %w", source, p, err)
	}
	if !fi.IsDir() {
		return "", fmt.Errorf("%s=%q is not a directory", source, p)
	}
	return p, nil
}
