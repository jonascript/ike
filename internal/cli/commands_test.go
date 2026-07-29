package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jonascript/ike/internal/store"
	"github.com/jonascript/ike/internal/task"
)

// scratch returns a data file path in a fresh temp directory. Nothing here ever
// touches the real matrix.
func scratch(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "tasks.json")
}

// runCLI builds the command tree over path and runs it, returning what the
// command printed. Each call constructs a fresh tree, so flag values cannot
// leak between invocations the way package-level flag variables did.
func runCLI(t *testing.T, path string, args ...string) (string, error) {
	t.Helper()
	root := NewRootCmd(func() (*store.Store, error) { return store.OpenAt(path), nil })
	var out, errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

// mustRunCLI runs a command that is expected to succeed.
func mustRunCLI(t *testing.T, path string, args ...string) string {
	t.Helper()
	out, err := runCLI(t, path, args...)
	if err != nil {
		t.Fatalf("ike %s: %v", strings.Join(args, " "), err)
	}
	return out
}

func TestParseID(t *testing.T) {
	cases := []struct {
		in      string
		want    int
		wantErr bool
	}{
		{"1", 1, false},
		{"42", 42, false},
		{"0", 0, false},
		{"-3", -3, false},
		{"", 0, true},
		{"abc", 0, true},
		{"1.5", 0, true},
		{"3x", 0, true},
	}
	for _, c := range cases {
		got, err := parseID(c.in)
		if (err != nil) != c.wantErr {
			t.Errorf("parseID(%q) error = %v, wantErr %v", c.in, err, c.wantErr)
			continue
		}
		if err == nil && got != c.want {
			t.Errorf("parseID(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestPrintGrouped(t *testing.T) {
	tasks := []task.Task{
		{ID: 1, Title: "ship it", Quadrant: task.Do},
		{ID: 7, Title: "plan the thing", Quadrant: task.Schedule},
		{ID: 2, Title: "hand off", Quadrant: task.Delegate},
		{ID: 3, Title: "drop this", Quadrant: task.Eliminate},
	}
	var buf bytes.Buffer
	printGrouped(&buf, tasks, nil)

	want := "1 · Do It First\n" +
		"    1  ship it\n" +
		"2 · Schedule It\n" +
		"    7  plan the thing\n" +
		"3 · Delegate It\n" +
		"    2  hand off\n" +
		"4 · Consider Eliminating It\n" +
		"    3  drop this\n"
	if got := buf.String(); got != want {
		t.Errorf("printGrouped output:\n%q\nwant:\n%q", got, want)
	}
}

// Quadrants with no tasks are skipped, and a renamed quadrant shows its custom
// name rather than the built-in default.
func TestPrintGroupedSkipsEmptyAndUsesCustomLabels(t *testing.T) {
	tasks := []task.Task{{ID: 5, Title: "only one", Quadrant: task.Delegate}}
	labels := store.Labels{task.Delegate: "Pass Along"}

	var buf bytes.Buffer
	printGrouped(&buf, tasks, labels)

	want := "3 · Pass Along\n    5  only one\n"
	if got := buf.String(); got != want {
		t.Errorf("printGrouped output:\n%q\nwant:\n%q", got, want)
	}
}

// The security regression test for `ike list`. Validate rejects control
// characters on the way in, so a title only carries them if it reached the file
// some other way — an older build, a hand edit, or a synced copy. Printing them
// raw let a stored title repaint the line, so the list could show something
// other than what is stored. That matters most for titles an agent wrote, since
// this list is how a human audits them.
func TestPrintGroupedNeutralizesEscapeSequences(t *testing.T) {
	const overwrite = "real task\x1b[2K\rFAKE INJECTED LINE"
	tasks := []task.Task{{ID: 1, Title: overwrite, Quadrant: task.Do}}
	labels := store.Labels{task.Do: "head\x1b[31mer"}

	var buf bytes.Buffer
	printGrouped(&buf, tasks, labels)
	got := buf.String()

	if strings.ContainsRune(got, 0x1b) {
		t.Errorf("output still contains an escape: %q", got)
	}
	if strings.ContainsRune(got, '\r') {
		t.Errorf("output still contains a carriage return: %q", got)
	}
	// Exactly two lines: a header and one task. A surviving \r or \n would let
	// one task occupy more, or overwrite its neighbor.
	if lines := strings.Count(got, "\n"); lines != 2 {
		t.Errorf("expected 2 lines, got %d: %q", lines, got)
	}
	// The text is still legible; only the control bytes are replaced.
	if !strings.Contains(got, "real task") || !strings.Contains(got, "FAKE INJECTED LINE") {
		t.Errorf("visible text was dropped: %q", got)
	}
}

// --json is a machine interface, so it keeps the raw title; encoding/json
// escapes control characters itself. This pins that the sanitizing above did
// not leak into the JSON path.
func TestPrintJSONKeepsRawTitleEscaped(t *testing.T) {
	tasks := []task.Task{{
		ID:        1,
		Title:     "real\x1b[2K\rFAKE",
		Quadrant:  task.Do,
		CreatedAt: time.Unix(0, 0).UTC(),
	}}
	var buf bytes.Buffer
	if err := printJSON(&buf, tasks); err != nil {
		t.Fatal(err)
	}
	got := buf.String()

	if strings.ContainsRune(got, 0x1b) {
		t.Errorf("JSON output should escape the control byte, not emit it: %q", got)
	}
	if !strings.Contains(got, `\u001b`) {
		t.Errorf("JSON output should carry the escaped original: %q", got)
	}
	if strings.Contains(got, "�") {
		t.Errorf("JSON output must not be sanitized, only escaped: %q", got)
	}
}

func TestAddAndList(t *testing.T) {
	p := scratch(t)

	out := mustRunCLI(t, p, "add", "ship v2", "-q", "1")
	if !strings.Contains(out, "added 1 [Do It First] ship v2") {
		t.Errorf("add output = %q", out)
	}

	out = mustRunCLI(t, p, "list")
	if !strings.Contains(out, "1 · Do It First") || !strings.Contains(out, "ship v2") {
		t.Errorf("list output = %q", out)
	}
}

// The quadrant flag defaults to 2, and a fresh tree per invocation means the
// value from an earlier `-q 1` cannot bleed into a later call.
func TestAddQuadrantFlagDoesNotLeakBetweenRuns(t *testing.T) {
	p := scratch(t)
	mustRunCLI(t, p, "add", "urgent thing", "-q", "1")
	mustRunCLI(t, p, "add", "later thing")

	out := mustRunCLI(t, p, "list", "-q", "2")
	if !strings.Contains(out, "later thing") {
		t.Errorf("second add should have defaulted to quadrant 2: %q", out)
	}
	if strings.Contains(out, "urgent thing") {
		t.Errorf("quadrant 2 listing should not contain the quadrant 1 task: %q", out)
	}
}

// The label a command prints comes from the data its own mutation produced.
func TestAddPrintsCustomQuadrantLabel(t *testing.T) {
	p := scratch(t)
	mustRunCLI(t, p, "label", "1", "Firefighting")

	out := mustRunCLI(t, p, "add", "page duty", "-q", "1")
	if !strings.Contains(out, "[Firefighting]") {
		t.Errorf("add should print the custom label: %q", out)
	}
}

func TestDoneMovesToArchive(t *testing.T) {
	p := scratch(t)
	mustRunCLI(t, p, "add", "ship v2", "-q", "1")
	mustRunCLI(t, p, "done", "1")

	if out := mustRunCLI(t, p, "list"); !strings.Contains(out, "no active tasks") {
		t.Errorf("task should have left the active list: %q", out)
	}
	if out := mustRunCLI(t, p, "archive"); !strings.Contains(out, "ship v2") {
		t.Errorf("task should be in the archive: %q", out)
	}
}

func TestUndoRedoRoundTrip(t *testing.T) {
	p := scratch(t)
	mustRunCLI(t, p, "add", "ship v2", "-q", "1")

	if out := mustRunCLI(t, p, "undo"); !strings.Contains(out, `undid add "ship v2"`) {
		t.Errorf("undo output = %q", out)
	}
	if out := mustRunCLI(t, p, "list"); !strings.Contains(out, "no active tasks") {
		t.Errorf("undo should have removed the task: %q", out)
	}
	if out := mustRunCLI(t, p, "redo"); !strings.Contains(out, `redid add "ship v2"`) {
		t.Errorf("redo output = %q", out)
	}
	if out := mustRunCLI(t, p, "list"); !strings.Contains(out, "ship v2") {
		t.Errorf("redo should have restored the task: %q", out)
	}
}

// An exhausted history stack is an error, not silent success.
func TestUndoWithNothingToUndo(t *testing.T) {
	if _, err := runCLI(t, scratch(t), "undo"); err == nil {
		t.Error("undo on an empty history should fail")
	}
}

func TestLabelArgumentValidation(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"reset with no quadrant", []string{"label", "--reset"}, "--reset needs a quadrant"},
		{"name and reset together", []string{"label", "1", "Nope", "--reset"}, "not both"},
		{"quadrant with no name", []string{"label", "1"}, "give a name to set"},
		{"unparseable quadrant", []string{"label", "x", "Name"}, "invalid quadrant"},
		{"out of range quadrant", []string{"label", "9", "Name"}, "quadrant must be 1-4"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := runCLI(t, scratch(t), c.args...)
			if err == nil {
				t.Fatalf("ike %s should have failed", strings.Join(c.args, " "))
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error = %q, want it to mention %q", err, c.want)
			}
		})
	}
}

func TestLabelSetAndReset(t *testing.T) {
	p := scratch(t)

	if out := mustRunCLI(t, p, "label", "3", "Pass Along"); !strings.Contains(out, "quadrant 3 is now Pass Along") {
		t.Errorf("label set output = %q", out)
	}
	out := mustRunCLI(t, p, "label")
	if !strings.Contains(out, "3 · Pass Along") || !strings.Contains(out, "(default: Delegate It)") {
		t.Errorf("label listing should mark the custom name: %q", out)
	}

	mustRunCLI(t, p, "label", "3", "--reset")
	if out := mustRunCLI(t, p, "label"); strings.Contains(out, "Pass Along") {
		t.Errorf("reset should have restored the default: %q", out)
	}
}

func TestReorderRejectsUnknownDirection(t *testing.T) {
	p := scratch(t)
	mustRunCLI(t, p, "add", "ship v2", "-q", "1")

	_, err := runCLI(t, p, "reorder", "1", "sideways")
	if err == nil || !strings.Contains(err.Error(), "invalid direction") {
		t.Errorf("error = %v, want an invalid-direction message", err)
	}
}

// A data file that cannot be parsed must fail the command outright. Nothing
// papers over it: the quadrant labels a command prints now come from the same
// read as the operation, so there is no second, error-swallowing lookup.
func TestUnreadableDataFileFailsTheCommand(t *testing.T) {
	p := scratch(t)
	if err := os.WriteFile(p, []byte("{ not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"list"}, {"add", "x"}, {"label"}, {"archive"}} {
		if _, err := runCLI(t, p, args...); err == nil {
			t.Errorf("ike %s on an unparseable file should fail", strings.Join(args, " "))
		}
	}
}

// `ike mcp enable|disable|status` is how a user grants and revokes agent access
// to their matrix. The store-level gate is covered in internal/store; this pins
// the commands that drive it.
func TestMCPEnableDisableStatus(t *testing.T) {
	p := scratch(t)

	out := mustRunCLI(t, p, "mcp", "status")
	if !strings.Contains(out, "MCP access: off") {
		t.Errorf("access must start off: %q", out)
	}
	if !strings.Contains(out, p) {
		t.Errorf("status should name the data file it applies to: %q", out)
	}

	if out := mustRunCLI(t, p, "mcp", "enable"); !strings.Contains(out, "MCP access is now on") {
		t.Errorf("enable output = %q", out)
	}
	if on, err := store.OpenAt(p).MCPEnabled(); err != nil || !on {
		t.Errorf("enable should have persisted: on=%v err=%v", on, err)
	}

	// Re-running reports no change rather than claiming a fresh one.
	if out := mustRunCLI(t, p, "mcp", "enable"); !strings.Contains(out, "was already on") {
		t.Errorf("second enable output = %q", out)
	}
	if out := mustRunCLI(t, p, "mcp", "status"); !strings.Contains(out, "MCP access: on") {
		t.Errorf("status after enable = %q", out)
	}

	if out := mustRunCLI(t, p, "mcp", "disable"); !strings.Contains(out, "MCP access is now off") {
		t.Errorf("disable output = %q", out)
	}
	if on, err := store.OpenAt(p).MCPEnabled(); err != nil || on {
		t.Errorf("disable should have persisted: on=%v err=%v", on, err)
	}
	if out := mustRunCLI(t, p, "mcp", "status"); !strings.Contains(out, "MCP access: off") {
		t.Errorf("status after disable = %q", out)
	}
}

// The server must refuse to start while access is off, and say how to turn it
// on — the reader is looking at an MCP client's error log.
func TestMCPServeRefusesWhileDisabled(t *testing.T) {
	_, err := runCLI(t, scratch(t), "mcp")
	if err == nil {
		t.Fatal("ike mcp should refuse to serve while access is off")
	}
	if !strings.Contains(err.Error(), "ike mcp enable") {
		t.Errorf("error should name the command that grants access: %q", err)
	}
}

func TestMvRmRestore(t *testing.T) {
	p := scratch(t)
	mustRunCLI(t, p, "add", "ship v2", "-q", "1")
	mustRunCLI(t, p, "add", "drop this", "-q", "1")

	if out := mustRunCLI(t, p, "mv", "1", "3"); !strings.Contains(out, "moved 1 to 3 · Delegate It") {
		t.Errorf("mv output = %q", out)
	}
	if out := mustRunCLI(t, p, "list", "-q", "3"); !strings.Contains(out, "ship v2") {
		t.Errorf("task should have moved to quadrant 3: %q", out)
	}

	if out := mustRunCLI(t, p, "rm", "2"); !strings.Contains(out, "deleted 2  drop this") {
		t.Errorf("rm output = %q", out)
	}
	// rm skips the archive entirely, unlike done.
	if out := mustRunCLI(t, p, "archive"); !strings.Contains(out, "archive is empty") {
		t.Errorf("rm must not archive: %q", out)
	}

	mustRunCLI(t, p, "done", "1")
	if out := mustRunCLI(t, p, "restore", "1"); !strings.Contains(out, "restored 1 to 3 · Delegate It") {
		t.Errorf("restore output = %q", out)
	}
	if out := mustRunCLI(t, p, "list"); !strings.Contains(out, "ship v2") {
		t.Errorf("restored task should be active again: %q", out)
	}
}

func TestMvRejectsBadArguments(t *testing.T) {
	p := scratch(t)
	mustRunCLI(t, p, "add", "ship v2", "-q", "1")

	for _, c := range []struct{ args, want []string }{
		{[]string{"mv", "x", "2"}, []string{"invalid task id"}},
		{[]string{"mv", "1", "x"}, []string{"invalid quadrant"}},
		{[]string{"mv", "1", "9"}, []string{"quadrant must be 1-4"}},
		{[]string{"mv", "99", "2"}, []string{"no active task with id 99"}},
	} {
		_, err := runCLI(t, p, c.args...)
		if err == nil {
			t.Errorf("ike %s should have failed", strings.Join(c.args, " "))
			continue
		}
		if !strings.Contains(err.Error(), c.want[0]) {
			t.Errorf("ike %s error = %q, want it to mention %q",
				strings.Join(c.args, " "), err, c.want[0])
		}
	}
}

func TestReorderDirections(t *testing.T) {
	p := scratch(t)
	for _, title := range []string{"first", "second", "third"} {
		mustRunCLI(t, p, "add", title, "-q", "1")
	}

	order := func() string {
		t.Helper()
		return mustRunCLI(t, p, "list", "-q", "1")
	}
	indexOf := func(out, title string) int { return strings.Index(out, title) }

	mustRunCLI(t, p, "reorder", "3", "top")
	if out := order(); indexOf(out, "third") > indexOf(out, "first") {
		t.Errorf("`top` should have moved third to the front: %q", out)
	}
	mustRunCLI(t, p, "reorder", "3", "bottom")
	if out := order(); indexOf(out, "third") < indexOf(out, "first") {
		t.Errorf("`bottom` should have moved third to the end: %q", out)
	}
	mustRunCLI(t, p, "reorder", "3", "up")
	if out := order(); indexOf(out, "third") > indexOf(out, "second") {
		t.Errorf("`up` should have moved third above second: %q", out)
	}
	if out := mustRunCLI(t, p, "reorder", "3", "down"); !strings.Contains(out, "down in 1 · Do It First") {
		t.Errorf("reorder output = %q", out)
	}
}

func TestArchiveJSONAndEmpty(t *testing.T) {
	p := scratch(t)
	if out := mustRunCLI(t, p, "archive", "--json"); !strings.Contains(out, "[]") {
		t.Errorf("empty archive as JSON = %q", out)
	}
	mustRunCLI(t, p, "add", "ship v2", "-q", "1")
	mustRunCLI(t, p, "done", "1")

	out := mustRunCLI(t, p, "archive", "--json")
	if !strings.Contains(out, `"title": "ship v2"`) || !strings.Contains(out, `"done_at"`) {
		t.Errorf("archive JSON = %q", out)
	}
}
