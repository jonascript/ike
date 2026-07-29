package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSpaceListMarksCurrent(t *testing.T) {
	p := scratch(t)
	mustRunCLI(t, p, "add", "a task")
	mustRunCLI(t, p, "space", "new", "work")

	out := mustRunCLI(t, p, "space", "list")
	for _, want := range []string{"* default", "1 active", "work"} {
		if !strings.Contains(out, want) {
			t.Errorf("space list = %q, want it to contain %q", out, want)
		}
	}
	// The bare command lists too.
	if bare := mustRunCLI(t, p, "space"); bare != out {
		t.Errorf("bare `ike space` = %q, want the same as `space list` %q", bare, out)
	}
}

func TestSpaceListJSON(t *testing.T) {
	p := scratch(t)
	mustRunCLI(t, p, "space", "new", "work")

	var got []struct {
		Name     string `json:"name"`
		Active   int    `json:"active"`
		Archived int    `json:"archived"`
		Current  bool   `json:"current"`
	}
	if err := json.Unmarshal([]byte(mustRunCLI(t, p, "space", "list", "--json")), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("json = %+v, want two spaces", got)
	}
	if got[0].Name != "default" || !got[0].Current {
		t.Errorf("first = %+v, want default marked current", got[0])
	}
	if got[1].Name != "work" || got[1].Current {
		t.Errorf("second = %+v, want work not current", got[1])
	}
}

func TestSpaceNewUseRenameRm(t *testing.T) {
	p := scratch(t)

	if out := mustRunCLI(t, p, "space", "new", "work"); !strings.Contains(out, "created space work") {
		t.Errorf("new = %q", out)
	}
	// Creating does not switch.
	if out := mustRunCLI(t, p, "space", "list"); !strings.Contains(out, "* default") {
		t.Errorf("after new, current = %q, want still default", out)
	}
	if out := mustRunCLI(t, p, "space", "use", "work"); !strings.Contains(out, "now on space work") {
		t.Errorf("use = %q", out)
	}
	if out := mustRunCLI(t, p, "space", "rename", "work", "job"); !strings.Contains(out, "renamed work to job") {
		t.Errorf("rename = %q", out)
	}
	if out := mustRunCLI(t, p, "space", "list"); !strings.Contains(out, "* job") {
		t.Errorf("after rename, current = %q, want job", out)
	}
	// Removing the current space says where you ended up.
	out := mustRunCLI(t, p, "space", "rm", "job")
	if !strings.Contains(out, "deleted space job") || !strings.Contains(out, "now on space default") {
		t.Errorf("rm = %q, want the deletion and the new current space", out)
	}
}

// Deleting a space is not undoable, so the refusal has to say what would go.
func TestSpaceRmRefusesNonEmptyWithoutForce(t *testing.T) {
	p := scratch(t)
	mustRunCLI(t, p, "space", "new", "work")
	mustRunCLI(t, p, "-s", "work", "add", "keep me")

	_, err := runCLI(t, p, "space", "rm", "work")
	if err == nil {
		t.Fatal("rm of a non-empty space should fail without --force")
	}
	if !strings.Contains(err.Error(), "1 active task") {
		t.Errorf("error = %v, want it to say what would be lost", err)
	}
	if out := mustRunCLI(t, p, "space", "list"); !strings.Contains(out, "work") {
		t.Error("the space should still be there")
	}

	out := mustRunCLI(t, p, "space", "rm", "work", "--force")
	if !strings.Contains(out, "1 active") {
		t.Errorf("forced rm = %q, want it to report what it destroyed", out)
	}
}

func TestSpaceRmRefusesTheLastSpace(t *testing.T) {
	p := scratch(t)
	if _, err := runCLI(t, p, "space", "rm", "default", "--force"); err == nil {
		t.Error("removing the only space should fail")
	}
}

// The flag routes a single command to another space without switching.
func TestSpaceFlagIsOneOffAndDoesNotChangeCurrent(t *testing.T) {
	p := scratch(t)
	mustRunCLI(t, p, "space", "new", "work")
	mustRunCLI(t, p, "add", "home thing")
	mustRunCLI(t, p, "-s", "work", "add", "work thing")

	if out := mustRunCLI(t, p, "list"); !strings.Contains(out, "home thing") || strings.Contains(out, "work thing") {
		t.Errorf("list = %q, want only the current space's task", out)
	}
	if out := mustRunCLI(t, p, "list", "-s", "work"); !strings.Contains(out, "work thing") || strings.Contains(out, "home thing") {
		t.Errorf("list -s work = %q, want only the work task", out)
	}
	if out := mustRunCLI(t, p, "space", "list"); !strings.Contains(out, "* default") {
		t.Errorf("current = %q, want the flag not to have switched it", out)
	}
}

// Without the flag the space is implied and saying it would be noise; with it,
// the write went somewhere other than where a bare `ike list` looks.
func TestSpaceFlagNamesTheSpaceInOutput(t *testing.T) {
	p := scratch(t)
	mustRunCLI(t, p, "space", "new", "work")

	if out := mustRunCLI(t, p, "add", "quiet"); strings.Contains(out, "in default") {
		t.Errorf("add = %q, want no space suffix without the flag", out)
	}
	out := mustRunCLI(t, p, "-s", "work", "add", "loud")
	if !strings.Contains(out, "in work") {
		t.Errorf("add -s work = %q, want it to name the space", out)
	}
	// Same for the other confirmations.
	if out := mustRunCLI(t, p, "-s", "work", "done", "1"); !strings.Contains(out, "in work") {
		t.Errorf("done -s work = %q", out)
	}
	if out := mustRunCLI(t, p, "-s", "work", "undo"); !strings.Contains(out, "in work") {
		t.Errorf("undo -s work = %q", out)
	}
}

func TestSpaceFlagWithUnknownNameFailsAndWritesNothing(t *testing.T) {
	p := scratch(t)
	mustRunCLI(t, p, "add", "real")
	before, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}

	for _, args := range [][]string{
		{"-s", "mistyped", "add", "typo"},
		{"-s", "mistyped", "list"},
		{"-s", "mistyped", "done", "1"},
	} {
		if _, err := runCLI(t, p, args...); err == nil {
			t.Errorf("ike %s should fail against a nonexistent space", strings.Join(args, " "))
		}
	}

	after, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Error("a command against a nonexistent space rewrote the file")
	}
}

// The space commands name their target as an argument, so combining them with
// the flag is asking twice.
func TestSpaceSubcommandsRejectTheSpaceFlag(t *testing.T) {
	p := scratch(t)
	mustRunCLI(t, p, "space", "new", "work")

	for _, args := range [][]string{
		{"-s", "work", "space", "use", "default"},
		{"-s", "work", "space", "new", "other"},
		{"-s", "work", "space", "rename", "work", "job"},
		{"-s", "work", "space", "rm", "work"},
	} {
		out, err := runCLI(t, p, args...)
		if err == nil {
			t.Errorf("ike %s should be refused (out %q)", strings.Join(args, " "), out)
			continue
		}
		if !strings.Contains(err.Error(), "drop --space") {
			t.Errorf("ike %s = %v, want it to explain the conflict", strings.Join(args, " "), err)
		}
	}
	// Listing is a read of the whole document, so the flag is merely redundant.
	if _, err := runCLI(t, p, "-s", "work", "space", "list"); err != nil {
		t.Errorf("space list with the flag should be allowed: %v", err)
	}
}

// Each tree gets its own flag variable, so a value cannot survive into the next
// invocation the way a package-level one did.
func TestSpaceFlagDoesNotLeakBetweenRuns(t *testing.T) {
	p := scratch(t)
	mustRunCLI(t, p, "space", "new", "work")
	mustRunCLI(t, p, "-s", "work", "add", "work thing")

	if out := mustRunCLI(t, p, "add", "home thing"); strings.Contains(out, "in work") {
		t.Errorf("add = %q, want the previous run's --space not to apply", out)
	}
	if out := mustRunCLI(t, p, "list"); !strings.Contains(out, "home thing") {
		t.Errorf("list = %q, want the task in the current space", out)
	}
}

// Undo is per space, so it must not reach into whichever space changed last.
func TestUndoIsScopedToTheSpace(t *testing.T) {
	p := scratch(t)
	mustRunCLI(t, p, "space", "new", "work")
	mustRunCLI(t, p, "add", "home thing")
	mustRunCLI(t, p, "-s", "work", "add", "work thing")

	mustRunCLI(t, p, "undo") // the current space's own last change
	if out := mustRunCLI(t, p, "list"); strings.Contains(out, "home thing") {
		t.Errorf("list = %q, want the home task undone", out)
	}
	if out := mustRunCLI(t, p, "list", "-s", "work"); !strings.Contains(out, "work thing") {
		t.Errorf("work list = %q, want it untouched by an undo in default", out)
	}
}

func TestMCPStatusNamesTheSpaces(t *testing.T) {
	p := scratch(t)
	mustRunCLI(t, p, "space", "new", "work")
	out := mustRunCLI(t, p, "mcp", "status")
	if !strings.Contains(out, "default (current)") || !strings.Contains(out, "work") {
		t.Errorf("mcp status = %q, want it to name the spaces the setting covers", out)
	}
}

// Consent is a property of the file, so it must not depend on which space is
// current or reachable.
func TestMCPEnableWorksWithASpaceFlag(t *testing.T) {
	p := scratch(t)
	mustRunCLI(t, p, "space", "new", "work")
	if _, err := runCLI(t, p, "-s", "work", "mcp", "enable"); err != nil {
		t.Fatalf("mcp enable with a space flag: %v", err)
	}
	if out := mustRunCLI(t, p, "-s", "work", "mcp", "status"); !strings.Contains(out, "MCP access: on") {
		t.Errorf("status = %q, want the setting to apply to the file", out)
	}
	if out := mustRunCLI(t, p, "mcp", "status"); !strings.Contains(out, "MCP access: on") {
		t.Errorf("status from the current space = %q, want the same answer", out)
	}
}

func TestFileFlagSelectsAnotherDataFile(t *testing.T) {
	a, b := scratch(t), scratch(t)
	mustRunCLI(t, a, "add", "in file a")
	mustRunCLI(t, b, "add", "in file b")

	// --file wins over whatever the opener resolved to.
	out := mustRunCLI(t, a, "--file", b, "list")
	if !strings.Contains(out, "in file b") || strings.Contains(out, "in file a") {
		t.Errorf("list --file = %q, want only the other file's task", out)
	}
	// And it composes with --space.
	mustRunCLI(t, b, "space", "new", "work")
	mustRunCLI(t, a, "--file", b, "-s", "work", "add", "in b work")
	if out := mustRunCLI(t, b, "list", "-s", "work"); !strings.Contains(out, "in b work") {
		t.Errorf("write through --file did not land: %q", out)
	}
}

func TestFileFlagRejectsBadPaths(t *testing.T) {
	p := scratch(t)
	for _, bad := range []string{"relative.json", "~/tasks.json", filepath.Join(t.TempDir(), "nope", "t.json")} {
		if _, err := runCLI(t, p, "--file", bad, "list"); err == nil {
			t.Errorf("--file %q should fail", bad)
		}
	}
}

func TestExportImportThroughTheCLI(t *testing.T) {
	src := scratch(t)
	mustRunCLI(t, src, "space", "new", "work")
	mustRunCLI(t, src, "-s", "work", "add", "carry me")
	out := filepath.Join(t.TempDir(), "work.json")

	if got := mustRunCLI(t, src, "space", "export", "work", out); !strings.Contains(got, "exported space work") {
		t.Errorf("export = %q", got)
	}
	// The export opens on its own.
	if got := mustRunCLI(t, src, "--file", out, "list"); !strings.Contains(got, "carry me") {
		t.Errorf("the exported file should open with --file: %q", got)
	}
	// Refuses to clobber, then imports elsewhere.
	if _, err := runCLI(t, src, "space", "export", "work", out); err == nil {
		t.Error("a second export to the same path should need --force")
	}

	dst := scratch(t)
	if got := mustRunCLI(t, dst, "space", "import", out); !strings.Contains(got, "imported space work") {
		t.Errorf("import = %q", got)
	}
	if got := mustRunCLI(t, dst, "list", "-s", "work"); !strings.Contains(got, "carry me") {
		t.Errorf("imported list = %q", got)
	}
	// A second import collides, and --as gives it a home.
	if _, err := runCLI(t, dst, "space", "import", out); err == nil {
		t.Error("importing onto an existing name should fail")
	}
	if got := mustRunCLI(t, dst, "space", "import", out, "--as", "laptop"); !strings.Contains(got, "laptop") {
		t.Errorf("import --as = %q", got)
	}
}

func TestExportDoesNotCarryMCPAccess(t *testing.T) {
	src := scratch(t)
	mustRunCLI(t, src, "mcp", "enable")
	out := filepath.Join(t.TempDir(), "exported.json")
	mustRunCLI(t, src, "space", "export", "default", out)

	if got := mustRunCLI(t, src, "--file", out, "mcp", "status"); !strings.Contains(got, "MCP access: off") {
		t.Errorf("exported file status = %q, want access off", got)
	}
}

// The space commands are exempt from --space, not from --file: reading or
// importing into another document is the whole point of the flag. Wiring them to
// the unflagged opener made `ike -f other.json space list` describe the wrong
// file, which looked right until the two files had different spaces.
func TestSpaceCommandsHonorTheFileFlag(t *testing.T) {
	a, b := scratch(t), scratch(t)
	mustRunCLI(t, a, "space", "new", "only-in-a")
	mustRunCLI(t, b, "space", "new", "only-in-b")

	out := mustRunCLI(t, a, "--file", b, "space", "list")
	if !strings.Contains(out, "only-in-b") || strings.Contains(out, "only-in-a") {
		t.Errorf("space list --file = %q, want the other file's spaces", out)
	}
	// And a write through the flag lands in that file.
	mustRunCLI(t, a, "--file", b, "space", "new", "made-through-flag")
	if got := mustRunCLI(t, b, "space", "list"); !strings.Contains(got, "made-through-flag") {
		t.Errorf("b = %q, want the space created through --file", got)
	}
	if got := mustRunCLI(t, a, "space", "list"); strings.Contains(got, "made-through-flag") {
		t.Errorf("a = %q, want it untouched", got)
	}
	// An exported single-space file lists only that space.
	out2 := filepath.Join(t.TempDir(), "one.json")
	mustRunCLI(t, a, "space", "export", "only-in-a", out2)
	listing := mustRunCLI(t, a, "--file", out2, "space", "list")
	if !strings.Contains(listing, "only-in-a") || strings.Contains(listing, "default") {
		t.Errorf("exported listing = %q, want only the exported space", listing)
	}
}
