package cli

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jonascript/ike/internal/store"
	"github.com/jonascript/ike/internal/task"
)

// captureStdout runs fn with os.Stdout redirected and returns what it wrote.
// The print helpers write to os.Stdout directly, which is what `ike list`
// actually does, so this tests the real output path rather than a stand-in.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.String()
	}()

	fn()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	out := <-done
	if err := r.Close(); err != nil {
		t.Fatal(err)
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
		{" 4", 0, true},
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
		// The message has to quote the offending input to be useful.
		if err != nil && !strings.Contains(err.Error(), c.in) {
			t.Errorf("parseID(%q) error %q should name the bad input", c.in, err)
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
	got := captureStdout(t, func() { printGrouped(tasks, nil) })

	want := "1 · Do It First\n" +
		"    1  ship it\n" +
		"2 · Schedule It\n" +
		"    7  plan the thing\n" +
		"3 · Delegate It\n" +
		"    2  hand off\n" +
		"4 · Consider Eliminating It\n" +
		"    3  drop this\n"
	if got != want {
		t.Errorf("printGrouped output:\n%q\nwant:\n%q", got, want)
	}
}

// Quadrants with no tasks are skipped, and a renamed quadrant shows its custom
// name rather than the built-in default.
func TestPrintGroupedSkipsEmptyAndUsesCustomLabels(t *testing.T) {
	tasks := []task.Task{{ID: 5, Title: "only one", Quadrant: task.Delegate}}
	labels := store.Labels{task.Delegate: "Pass Along"}

	got := captureStdout(t, func() { printGrouped(tasks, labels) })

	want := "3 · Pass Along\n    5  only one\n"
	if got != want {
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

	got := captureStdout(t, func() { printGrouped(tasks, labels) })

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
	got := captureStdout(t, func() {
		if err := printJSON(tasks); err != nil {
			t.Error(err)
		}
	})

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

// quadrantLabels must never be the reason a command fails: a broken data file
// should surface from the operation itself, not from resolving a heading.
func TestQuadrantLabelsFallsBackOnReadError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.json")
	if err := os.WriteFile(path, []byte("{ not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	if got := quadrantLabels(store.OpenAt(path)); got != nil {
		t.Errorf("quadrantLabels on an unreadable file = %v, want nil", got)
	}
	// nil still resolves to the built-in defaults.
	var nilLabels store.Labels
	if got, want := nilLabels.Of(task.Do), task.Do.Label(); got != want {
		t.Errorf("nil Labels.Of(Do) = %q, want %q", got, want)
	}
}
