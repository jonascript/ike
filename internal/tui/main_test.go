package tui

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMain points XDG_STATE_HOME at a scratch directory for the whole package.
//
// New records the file it opened in the recent-files list, so without this every
// test run would write scratch paths into the developer's real
// ~/.local/state/ike/recent.json — and their file picker would fill up with
// temp directories that no longer exist. Set here rather than in each helper
// because it has to cover any test that calls New, including ones added later.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "ike-tui-state")
	if err != nil {
		panic(err)
	}
	os.Setenv("XDG_STATE_HOME", filepath.Join(dir, "state"))
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
