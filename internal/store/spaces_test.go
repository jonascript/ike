package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jonascript/ike/internal/task"
)

// spacesStore returns a store whose file already has the named spaces, plus
// the "default" one every file starts with.
func spacesStore(t *testing.T, names ...string) *Store {
	t.Helper()
	s := testStore(t)
	for _, n := range names {
		if _, err := s.NewSpace(n); err != nil {
			t.Fatalf("NewSpace(%q): %v", n, err)
		}
	}
	return s
}

func spaceNames(t *testing.T, s *Store) []string {
	t.Helper()
	infos, err := s.ListSpaces()
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, len(infos))
	for i, in := range infos {
		out[i] = in.Name
	}
	return out
}

// The whole point of spaces: nothing an operation does in one can be observed
// from another. Every piece of per-space state is checked, because each was a
// separate decision to put inside the space rather than on the document.
func TestSpacesAreFullyIndependent(t *testing.T) {
	s := spacesStore(t, "work")
	home, work := s.InSpace("default"), s.InSpace("work")

	if _, _, err := home.Add("home task", task.Do); err != nil {
		t.Fatal(err)
	}
	if _, _, err := work.Add("work task", task.Schedule); err != nil {
		t.Fatal(err)
	}
	if _, _, err := work.SetQuadrantLabel(task.Do, "Firefighting"); err != nil {
		t.Fatal(err)
	}
	done, _, err := work.Add("finish me", task.Do)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := work.Complete(done.ID); err != nil {
		t.Fatal(err)
	}

	hd, err := home.Load()
	if err != nil {
		t.Fatal(err)
	}
	wd, err := work.Load()
	if err != nil {
		t.Fatal(err)
	}

	// Tasks.
	if len(hd.Tasks) != 1 || hd.Tasks[0].Title != "home task" {
		t.Errorf("default tasks = %v, want only the home task", titlesOf(hd.Tasks))
	}
	if len(wd.Tasks) != 1 || wd.Tasks[0].Title != "work task" {
		t.Errorf("work tasks = %v, want only the work task", titlesOf(wd.Tasks))
	}
	// Archive.
	if len(hd.Archive) != 0 {
		t.Errorf("default archive = %v, want empty", titlesOf(hd.Archive))
	}
	if len(wd.Archive) != 1 {
		t.Errorf("work archive = %v, want the completed task", titlesOf(wd.Archive))
	}
	// Labels.
	if got := hd.Labels.Of(task.Do); got != task.Do.Label() {
		t.Errorf("default label = %q, want the untouched default", got)
	}
	if got := wd.Labels.Of(task.Do); got != "Firefighting" {
		t.Errorf("work label = %q, want the rename", got)
	}
	// Undo history: undoing in one space must not reach into the other.
	if _, _, err := home.Undo(); err != nil {
		t.Fatal(err)
	}
	wd2, err := work.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(wd2.Tasks) != 1 || len(wd2.Archive) != 1 {
		t.Errorf("work changed after an undo in default: %v / %v",
			titlesOf(wd2.Tasks), titlesOf(wd2.Archive))
	}
}

// IDs are per space, so the numbers you type are the ones you see in front of
// you rather than a global sequence with gaps.
func TestNextIDIsPerSpace(t *testing.T) {
	s := spacesStore(t, "work")
	first, _, err := s.InSpace("default").Add("a", task.Do)
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := s.InSpace("work").Add("b", task.Do)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != 1 || second.ID != 1 {
		t.Errorf("ids = %d and %d, want both spaces to start at 1", first.ID, second.ID)
	}
}

// An unpinned store follows `ike space use`; a pinned one does not. This is the
// difference that keeps an open TUI writing to the matrix it is showing.
func TestPinnedAndUnpinnedStores(t *testing.T) {
	s := spacesStore(t, "work")
	unpinned := s
	pinned := s.InSpace("default")

	if _, err := s.UseSpace("work"); err != nil {
		t.Fatal(err)
	}
	if d, err := unpinned.Load(); err != nil || d.Space != "work" {
		t.Errorf("unpinned space = %q (err %v), want it to follow current", d.Space, err)
	}
	if d, err := pinned.Load(); err != nil || d.Space != "default" {
		t.Errorf("pinned space = %q (err %v), want it to stay put", d.Space, err)
	}

	// And a write through the pinned store lands where it is pinned.
	if _, _, err := pinned.Add("stays home", task.Do); err != nil {
		t.Fatal(err)
	}
	wd, err := s.InSpace("work").Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(wd.Tasks) != 0 {
		t.Errorf("work tasks = %v, want the write to have landed in default", titlesOf(wd.Tasks))
	}
}

// A typo must not create a matrix and quietly swallow the tasks meant for a
// real one — and the file must be left exactly as it was.
func TestMutateNeverCreatesASpace(t *testing.T) {
	s := testStore(t)
	if _, _, err := s.Add("real", task.Do); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(s.Path())
	if err != nil {
		t.Fatal(err)
	}

	if _, _, err := s.InSpace("mistyped").Add("typo", task.Do); err == nil {
		t.Fatal("adding to a nonexistent space should error")
	}
	if _, err := s.InSpace("mistyped").Load(); err == nil {
		t.Error("loading a nonexistent space should error")
	}

	after, err := os.ReadFile(s.Path())
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Error("a failed mutation rewrote the file")
	}
	if got := spaceNames(t, s); len(got) != 1 {
		t.Errorf("spaces = %v, want the typo not to have created one", got)
	}
}

func TestNewSpaceDoesNotSwitch(t *testing.T) {
	s := testStore(t)
	d, err := s.NewSpace("work")
	if err != nil {
		t.Fatal(err)
	}
	if d.Space != defaultSpace {
		t.Errorf("returned space = %q, want to still be on %q", d.Space, defaultSpace)
	}
	if len(d.AllSpaces) != 2 {
		t.Errorf("AllSpaces = %v, want the new space listed", d.AllSpaces)
	}
	cur, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cur.Space != defaultSpace {
		t.Errorf("current = %q, want creating a space not to switch to it", cur.Space)
	}
}

func TestSpaceNameValidation(t *testing.T) {
	s := spacesStore(t, "work")
	cases := []struct {
		name string
		want string
	}{
		{"", "empty"},
		{"   ", "empty"},
		{"work", "already exists"},
		{"WORK", "already exists"},
		{"bad\x1bname", "control characters"},
		{"tab\there", "control characters"},
		{strings.Repeat("x", task.MaxSpaceNameLen+1), "maximum"},
	}
	for _, c := range cases {
		_, err := s.NewSpace(c.name)
		if err == nil {
			t.Errorf("NewSpace(%q) should fail", c.name)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("NewSpace(%q) = %v, want it to mention %q", c.name, err, c.want)
		}
	}
}

func TestUseSpaceRejectsUnknown(t *testing.T) {
	s := testStore(t)
	if _, err := s.UseSpace("nope"); err == nil {
		t.Error("switching to a nonexistent space should error")
	}
	if got := spaceNames(t, s); len(got) != 1 {
		t.Errorf("spaces = %v, want no space created by the attempt", got)
	}
}

// A rename is a re-key, so everything inside the space has to come with it —
// including the history, which is what would silently vanish if the data were
// copied rather than moved.
func TestRenameSpacePreservesContentsAndHistory(t *testing.T) {
	s := spacesStore(t, "work")
	work := s.InSpace("work")
	if _, _, err := work.Add("keep me", task.Do); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UseSpace("work"); err != nil {
		t.Fatal(err)
	}
	if err := s.RenameSpace("work", "job"); err != nil {
		t.Fatal(err)
	}

	if got := spaceNames(t, s); !slicesEqual(got, []string{"default", "job"}) {
		t.Errorf("spaces = %v, want default and job", got)
	}
	d, err := s.InSpace("job").Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Tasks) != 1 || d.Tasks[0].Title != "keep me" {
		t.Errorf("tasks = %v, want them carried across the rename", titlesOf(d.Tasks))
	}
	if !d.AllSpaces[1].Current {
		t.Error("current should have followed the rename")
	}
	// The undo stack came along, so the pre-rename change is still revertible.
	if _, _, err := s.InSpace("job").Undo(); err != nil {
		t.Errorf("undo after a rename: %v", err)
	}
}

func TestRenameSpaceRejectsCollisionAndKeepsTheOriginal(t *testing.T) {
	s := spacesStore(t, "work")
	if err := s.RenameSpace("work", "default"); err == nil {
		t.Error("renaming onto an existing name should error")
	}
	if got := spaceNames(t, s); !slicesEqual(got, []string{"default", "work"}) {
		t.Errorf("spaces = %v, want the failed rename to have changed nothing", got)
	}
}

// Renaming a space to a different casing of its own name is a legitimate
// rename, not a collision with itself.
func TestRenameSpaceCanChangeOnlyCasing(t *testing.T) {
	s := spacesStore(t, "work")
	if err := s.RenameSpace("work", "Work"); err != nil {
		t.Fatalf("recasing a space name: %v", err)
	}
	if got := spaceNames(t, s); !slicesEqual(got, []string{"Work", "default"}) {
		t.Errorf("spaces = %v, want the recased name", got)
	}
}

func TestRemoveSpaceRefusesNonEmptyWithoutForce(t *testing.T) {
	s := spacesStore(t, "work")
	if _, _, err := s.InSpace("work").Add("a task", task.Do); err != nil {
		t.Fatal(err)
	}
	_, err := s.RemoveSpace("work", false)
	if err == nil {
		t.Fatal("removing a non-empty space without force should error")
	}
	if !strings.Contains(err.Error(), "1 active task") {
		t.Errorf("error = %v, want it to say what would be lost", err)
	}
	if got := spaceNames(t, s); len(got) != 2 {
		t.Errorf("spaces = %v, want the space kept", got)
	}

	info, err := s.RemoveSpace("work", true)
	if err != nil {
		t.Fatalf("forced removal: %v", err)
	}
	if info.Active != 1 {
		t.Errorf("removed = %+v, want it to report the one active task", info)
	}
	if got := spaceNames(t, s); !slicesEqual(got, []string{"default"}) {
		t.Errorf("spaces = %v, want only default left", got)
	}
}

// An archived task still counts as contents worth warning about; it is the
// half people forget they have.
func TestRemoveSpaceCountsTheArchive(t *testing.T) {
	s := spacesStore(t, "work")
	work := s.InSpace("work")
	tk, _, err := work.Add("done", task.Do)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := work.Complete(tk.ID); err != nil {
		t.Fatal(err)
	}
	_, err = s.RemoveSpace("work", false)
	if err == nil || !strings.Contains(err.Error(), "1 archived task") {
		t.Errorf("error = %v, want it to mention the archived task", err)
	}
}

func TestRemoveSpaceRefusesTheLastSpace(t *testing.T) {
	s := testStore(t)
	if _, err := s.RemoveSpace(defaultSpace, true); err == nil {
		t.Error("removing the only space should error even with force")
	}
	if got := spaceNames(t, s); len(got) != 1 {
		t.Errorf("spaces = %v, want the space kept", got)
	}
}

func TestRemoveCurrentSpaceMovesCurrentToFirstSurvivor(t *testing.T) {
	s := spacesStore(t, "work", "alpha")
	if _, err := s.UseSpace("work"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RemoveSpace("work", false); err != nil {
		t.Fatal(err)
	}
	d, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if d.Space != "alpha" {
		t.Errorf("current = %q, want the alphabetically first survivor", d.Space)
	}
}

// Space lifecycle changes are document-level and have no per-space stack to
// record onto, so they must leave every space's history exactly as it was.
func TestSpaceLifecycleIsNotUndoable(t *testing.T) {
	s := testStore(t)
	if _, _, err := s.Add("only change", task.Do); err != nil {
		t.Fatal(err)
	}
	if _, err := s.NewSpace("work"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UseSpace("work"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UseSpace(defaultSpace); err != nil {
		t.Fatal(err)
	}

	label, _, err := s.Undo()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(label, "only change") {
		t.Errorf("undid %q, want the task add — space changes must not record history", label)
	}
	if _, _, err := s.Undo(); err == nil {
		t.Error("there should be nothing left to undo")
	}
}

func TestListSpacesCountsAndOrder(t *testing.T) {
	s := spacesStore(t, "work", "alpha")
	tk, _, err := s.InSpace("work").Add("a", task.Do)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.InSpace("work").Complete(tk.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.InSpace("work").Add("b", task.Do); err != nil {
		t.Fatal(err)
	}

	infos, err := s.ListSpaces()
	if err != nil {
		t.Fatal(err)
	}
	if !slicesEqual(spaceNames(t, s), []string{"alpha", defaultSpace, "work"}) {
		t.Errorf("order = %v, want sorted by name", spaceNames(t, s))
	}
	var work SpaceInfo
	for _, in := range infos {
		if in.Name == "work" {
			work = in
		}
		if in.Current != (in.Name == defaultSpace) {
			t.Errorf("%q current = %v, want only %q marked", in.Name, in.Current, defaultSpace)
		}
	}
	if work.Active != 1 || work.Archived != 1 {
		t.Errorf("work counts = %d active / %d archived, want 1 and 1", work.Active, work.Archived)
	}
}

// One file, one lock: writes to different spaces still serialize, and none is
// lost. This is what would break if the space operations grew a second write
// path instead of going through mutateFile.
func TestConcurrentWritersAcrossSpaces(t *testing.T) {
	// See TestConcurrentWriters: the point is that nothing is lost, not that the
	// writes beat the default timeout on a slow machine.
	defer withLockTimeout(30 * time.Second)()

	s := spacesStore(t, "work")
	const perSpace = 15

	var wg sync.WaitGroup
	for _, space := range []string{defaultSpace, "work"} {
		for i := range perSpace {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if _, _, err := s.InSpace(space).Add(space+string(rune('a'+i)), task.Do); err != nil {
					t.Errorf("add to %s: %v", space, err)
				}
			}()
		}
	}
	wg.Wait()

	for _, space := range []string{defaultSpace, "work"} {
		d, err := s.InSpace(space).Load()
		if err != nil {
			t.Fatal(err)
		}
		if len(d.Tasks) != perSpace {
			t.Errorf("%s has %d tasks, want %d — a write was lost", space, len(d.Tasks), perSpace)
		}
		seen := map[int]bool{}
		for _, tk := range d.Tasks {
			if seen[tk.ID] {
				t.Errorf("%s reused id %d", space, tk.ID)
			}
			seen[tk.ID] = true
		}
	}
}

// A space whose name was hand-edited to contain an escape sequence must not be
// able to repaint the terminal, and the raw name must still be the lookup key.
func TestSpaceNamesAreSanitizedForDisplayButNotForLookup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.json")
	body := `{
	  "version": 4,
	  "current": "default",
	  "spaces": {
	    "default": {"next_id": 1, "tasks": []},
	    "ev\u001bil": {"next_id": 1, "tasks": []}
	  }
	}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	s := OpenAt(path)

	// The raw name still resolves — sanitizing is a rendering concern, and a
	// sanitized name fed back as a key would address nothing.
	d, err := s.InSpace("ev\x1bil").Load()
	if err != nil {
		t.Fatalf("the raw name should still resolve: %v", err)
	}
	if d.Space != "ev\x1bil" {
		t.Errorf("resolved space = %q, want the raw key", d.Space)
	}
	// Frontends render through SanitizeDisplay, which neutralizes it.
	if got := task.SanitizeDisplay(d.Space); strings.ContainsRune(got, 0x1b) {
		t.Errorf("sanitized name %q still contains an escape", got)
	}
}

// The derived fields describe the document, so they must be present on
// everything a mutation hands back — that is what frontends render from.
func TestMutationsReturnTheDerivedFields(t *testing.T) {
	s := spacesStore(t, "work")
	_, d, err := s.Add("x", task.Do)
	if err != nil {
		t.Fatal(err)
	}
	if d.Space != defaultSpace {
		t.Errorf("Space = %q, want %q", d.Space, defaultSpace)
	}
	if len(d.AllSpaces) != 2 {
		t.Errorf("AllSpaces = %v, want both spaces", d.AllSpaces)
	}
	// And they are not written to disk.
	body := rawSpace(t, s.Path(), defaultSpace)
	for _, k := range []string{"space", "spaces", "all_spaces"} {
		if _, ok := body[k]; ok {
			t.Errorf("space body persists derived key %q", k)
		}
	}
}

// `ike space list --json` is a scripting surface, so its shape is pinned.
func TestSpaceInfoJSONShape(t *testing.T) {
	s := testStore(t)
	infos, err := s.ListSpaces()
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(infos)
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"name":"default","active":0,"archived":0,"current":true}]`
	if string(b) != want {
		t.Errorf("json = %s, want %s", b, want)
	}
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
