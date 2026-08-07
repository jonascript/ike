package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jonascript/ike/internal/task"
)

// exportFixture returns a store with a work space holding one active task, one
// archived task, a renamed quadrant, and undo history.
func exportFixture(t *testing.T) *Store {
	t.Helper()
	s := spacesStore(t, "work")
	work := s.InSpace("work")
	if _, _, err := work.Add("keep me", task.Do); err != nil {
		t.Fatal(err)
	}
	done, _, err := work.Add("finished", task.Schedule)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := work.Complete(done.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := work.SetQuadrantLabel(task.Do, "Firefighting"); err != nil {
		t.Fatal(err)
	}
	return s
}

// An export is a normal data file, and everything inside the space travels.
func TestExportProducesAnOpenableFile(t *testing.T) {
	s := exportFixture(t)
	out := filepath.Join(t.TempDir(), "work.json")

	info, err := s.ExportSpace("work", out, false)
	if err != nil {
		t.Fatal(err)
	}
	if info.Active != 1 || info.Archived != 1 {
		t.Errorf("exported = %+v, want 1 active and 1 archived", info)
	}

	// It opens standalone, on the exported space, with everything intact.
	d, err := OpenAt(out).Load()
	if err != nil {
		t.Fatalf("the exported file should open: %v", err)
	}
	if d.Space != "work" {
		t.Errorf("space = %q, want work", d.Space)
	}
	if len(d.AllSpaces) != 1 {
		t.Errorf("spaces = %+v, want only the exported one", d.AllSpaces)
	}
	if len(d.Tasks) != 1 || d.Tasks[0].Title != "keep me" {
		t.Errorf("tasks = %v", titlesOf(d.Tasks))
	}
	if len(d.Archive) != 1 || d.Archive[0].DoneAt == nil {
		t.Errorf("archive = %v, want the completion stamp preserved", titlesOf(d.Archive))
	}
	if got := d.Labels.Of(task.Do); got != "Firefighting" {
		t.Errorf("label = %q, want the rename", got)
	}
	if len(d.Undo) == 0 {
		t.Error("undo history should travel with the space")
	}
	if v := onDiskVersion(t, out); v != currentVersion {
		t.Errorf("exported version = %d, want %d", v, currentVersion)
	}
}

// Consent does not travel: an export exists to be copied to another machine,
// whose owner has not agreed to anything.
func TestExportNeverCarriesMCPAccess(t *testing.T) {
	s := exportFixture(t)
	if _, err := s.SetMCPEnabled(true); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "work.json")
	if _, err := s.ExportSpace("work", out, false); err != nil {
		t.Fatal(err)
	}

	if enabled, err := OpenAt(out).MCPEnabled(); err != nil || enabled {
		t.Errorf("exported MCPEnabled = %v (err %v), want it off", enabled, err)
	}
	if _, ok := rawFile(t, out)["mcp_enabled"]; ok {
		t.Error("the exported file should not carry mcp_enabled at all")
	}
	// And a gated store cannot export at all while access is off.
	if _, err := s.ForMCP().ExportSpace("work", filepath.Join(t.TempDir(), "x.json"), false); err != nil {
		t.Errorf("a gated export with access on should work: %v", err)
	}
}

func TestExportRefusesToOverwriteWithoutForce(t *testing.T) {
	s := exportFixture(t)
	out := filepath.Join(t.TempDir(), "work.json")
	if err := os.WriteFile(out, []byte("do not clobber"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := s.ExportSpace("work", out, false); err == nil {
		t.Fatal("export over an existing file should fail")
	}
	if b, _ := os.ReadFile(out); string(b) != "do not clobber" {
		t.Error("the existing file was modified")
	}
	if _, err := s.ExportSpace("work", out, true); err != nil {
		t.Fatalf("forced export: %v", err)
	}
	if _, err := OpenAt(out).Load(); err != nil {
		t.Errorf("forced export should be readable: %v", err)
	}
}

func TestExportRejectsBadPathsAndUnknownSpaces(t *testing.T) {
	s := exportFixture(t)
	dir := t.TempDir()

	if _, err := s.ExportSpace("work", "relative.json", false); err == nil {
		t.Error("a relative export path should fail")
	}
	if _, err := s.ExportSpace("work", "~/work.json", false); err == nil {
		t.Error("an unexpanded ~ should fail")
	}
	if _, err := s.ExportSpace("work", filepath.Join(dir, "nope", "w.json"), false); err == nil {
		t.Error("a missing parent directory should fail")
	}
	if _, err := s.ExportSpace("mistyped", filepath.Join(dir, "w.json"), false); err == nil {
		t.Error("an unknown space should fail")
	}
}

// The round trip is the point: export, copy the file, import somewhere else.
func TestExportImportRoundTrip(t *testing.T) {
	src := exportFixture(t)
	out := filepath.Join(t.TempDir(), "work.json")
	if _, err := src.ExportSpace("work", out, false); err != nil {
		t.Fatal(err)
	}

	dst := testStore(t)
	imported, err := dst.ImportSpaces(out, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(imported) != 1 || imported[0].Name != "work" {
		t.Fatalf("imported = %+v, want the work space", imported)
	}

	d, err := dst.InSpace("work").Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Tasks) != 1 || d.Tasks[0].Title != "keep me" {
		t.Errorf("tasks = %v", titlesOf(d.Tasks))
	}
	if len(d.Archive) != 1 {
		t.Errorf("archive = %v, want it imported too", titlesOf(d.Archive))
	}
	if got := d.Labels.Of(task.Do); got != "Firefighting" {
		t.Errorf("label = %q, want the rename imported", got)
	}
	if len(d.Undo) == 0 {
		t.Error("history should survive the round trip")
	}
	// IDs come along; next_id belongs to the space, so nothing needs renumbering.
	if d.NextID <= len(d.Tasks) {
		t.Errorf("next_id = %d, want the source's counter", d.NextID)
	}
	// Importing does not switch.
	if cur, err := dst.Load(); err != nil || cur.Space != defaultSpace {
		t.Errorf("current = %q, want importing not to switch", cur.Space)
	}
}

func TestImportRejectsANameAlreadyInUse(t *testing.T) {
	src := exportFixture(t)
	out := filepath.Join(t.TempDir(), "work.json")
	if _, err := src.ExportSpace("work", out, false); err != nil {
		t.Fatal(err)
	}

	dst := spacesStore(t, "work")
	if _, _, err := dst.InSpace("work").Add("mine", task.Do); err != nil {
		t.Fatal(err)
	}
	_, err := dst.ImportSpaces(out, "", false)
	if err == nil {
		t.Fatal("importing onto an existing name should fail rather than merge")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error = %v, want it to say the name is taken", err)
	}
	// The existing space is untouched.
	if d, _ := dst.InSpace("work").Load(); len(d.Tasks) != 1 || d.Tasks[0].Title != "mine" {
		t.Errorf("tasks = %v, want the local space intact", titlesOf(d.Tasks))
	}

	// --as gives it somewhere to go.
	imported, err := dst.ImportSpaces(out, "from-laptop", false)
	if err != nil {
		t.Fatalf("import --as: %v", err)
	}
	if len(imported) != 1 || imported[0].Name != "from-laptop" {
		t.Errorf("imported = %+v, want the renamed space", imported)
	}
}

func TestImportAllTakesEverySpace(t *testing.T) {
	src := spacesStore(t, "work", "errands")
	if _, _, err := src.InSpace("work").Add("a", task.Do); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "all.json")
	// Export cannot write more than one space, so copy the whole source tree —
	// manifest plus spaces directory — which is what "another machine's data
	// file" looks like since the split.
	b, err := os.ReadFile(src.Path())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(out, b, 0o600); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(spacesDir(src.Path()))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(spacesDir(out), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		sb, err := os.ReadFile(filepath.Join(spacesDir(src.Path()), e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(spacesDir(out), e.Name()), sb, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	// Rename the local space out of the way, so importing "default" has a name
	// free to land on.
	dst := OpenAt(filepath.Join(t.TempDir(), "tasks.json"))
	if err := dst.RenameSpace(defaultSpace, "mine"); err != nil {
		t.Fatal(err)
	}
	imported, err := dst.ImportSpaces(out, "", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(imported) != 3 {
		t.Fatalf("imported = %+v, want all three spaces", imported)
	}
	// Sorted, so the report and any retry read the same way.
	if imported[0].Name != defaultSpace || imported[1].Name != "errands" || imported[2].Name != "work" {
		t.Errorf("order = %+v, want sorted by name", imported)
	}
	if names := spaceNames(t, dst); len(names) != 4 {
		t.Errorf("spaces = %v, want the local one plus three", names)
	}
}

func TestImportAllRejectsAs(t *testing.T) {
	src := exportFixture(t)
	out := filepath.Join(t.TempDir(), "work.json")
	if _, err := src.ExportSpace("work", out, false); err != nil {
		t.Fatal(err)
	}
	if _, err := testStore(t).ImportSpaces(out, "renamed", true); err == nil {
		t.Error("--as with --all should be refused: it cannot rename several spaces")
	}
}

func TestImportRejectsBadPaths(t *testing.T) {
	dst := testStore(t)
	if _, err := dst.ImportSpaces("relative.json", "", false); err == nil {
		t.Error("a relative import path should fail")
	}
	if _, err := dst.ImportSpaces(filepath.Join(t.TempDir(), "missing.json"), "", false); err == nil {
		t.Error("a missing import file should fail")
	}
	// A file that is not an ike data file at all.
	bad := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(bad, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := dst.ImportSpaces(bad, "", false); err == nil {
		t.Error("an unparseable import file should fail")
	}
}

// An old single-matrix file is importable, which is how you bring one in from a
// machine running an older ike.
func TestImportUpgradesALegacyFile(t *testing.T) {
	legacy := filepath.Join(t.TempDir(), "old.json")
	body := `{
	  "version": 3,
	  "next_id": 4,
	  "tasks": [{"id": 1, "title": "from the old build", "quadrant": 1, "rank": 1024}],
	  "archive": []
	}`
	if err := os.WriteFile(legacy, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	dst := spacesStore(t)
	imported, err := dst.ImportSpaces(legacy, "laptop", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(imported) != 1 || imported[0].Name != "laptop" {
		t.Fatalf("imported = %+v", imported)
	}
	d, err := dst.InSpace("laptop").Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Tasks) != 1 || d.Tasks[0].Title != "from the old build" {
		t.Errorf("tasks = %v", titlesOf(d.Tasks))
	}
}

func TestOpenPathValidatesLikeTheEnvironmentVariable(t *testing.T) {
	dir := t.TempDir()
	if _, err := OpenPath("--file", filepath.Join(dir, "tasks.json")); err != nil {
		t.Errorf("a good path should open: %v", err)
	}
	for _, p := range []string{"relative.json", "~/tasks.json", filepath.Join(dir, "nope", "t.json")} {
		if _, err := OpenPath("--file", p); err == nil {
			t.Errorf("OpenPath(%q) should fail", p)
		} else if !strings.Contains(err.Error(), "--file") {
			t.Errorf("error %v should name the source it came from", err)
		}
	}
}

func TestRecentFilesRoundTrip(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))

	if got := LoadRecent(); len(got.Paths) != 0 {
		t.Errorf("a fresh list = %v, want empty", got.Paths)
	}

	// Only files that exist are remembered, since the picker offers them.
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.json"), filepath.Join(dir, "b.json")
	for _, p := range []string{a, b} {
		if err := os.WriteFile(p, []byte(`{"version":4,"current":"default","spaces":{"default":{"next_id":1}}}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	RememberRecent(a)
	RememberRecent(b)
	if got := LoadRecent().Paths; len(got) != 2 || got[0] != b || got[1] != a {
		t.Errorf("paths = %v, want most recent first", got)
	}
	// Re-opening moves a file to the front rather than duplicating it.
	RememberRecent(a)
	if got := LoadRecent().Paths; len(got) != 2 || got[0] != a {
		t.Errorf("paths = %v, want a moved to the front with no duplicate", got)
	}
	// A file that has since gone is dropped.
	if err := os.Remove(b); err != nil {
		t.Fatal(err)
	}
	if got := LoadRecent().Paths; len(got) != 1 || got[0] != a {
		t.Errorf("paths = %v, want the missing file dropped", got)
	}
}

// The list is a convenience, so nothing about it may break opening a file.
func TestRecentFilesToleratesAnUnusableStateFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, "ike"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ike", "recent.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := LoadRecent(); len(got.Paths) != 0 {
		t.Errorf("a corrupt list = %v, want it treated as empty", got.Paths)
	}
	RememberRecent(filepath.Join(dir, "whatever.json")) // must not panic
}

func TestRecentFileIsPrivate(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", filepath.Join(dir, "state"))
	target := filepath.Join(dir, "t.json")
	if err := os.WriteFile(target, []byte(`{"version":4,"current":"default","spaces":{"default":{"next_id":1}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	RememberRecent(target)

	fi, err := os.Stat(filepath.Join(dir, "state", "ike", "recent.json"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != dataFileMode {
		t.Errorf("mode = %v, want %v — it describes the filesystem", fi.Mode().Perm(), dataFileMode)
	}
}
