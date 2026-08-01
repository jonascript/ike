package task

import (
	"strings"
	"testing"
	"time"
)

func TestQuadrantValid(t *testing.T) {
	cases := []struct {
		q    Quadrant
		want bool
	}{
		{0, false},
		{Do, true},
		{Schedule, true},
		{Delegate, true},
		{Eliminate, true},
		{5, false},
		{-1, false},
	}
	for _, c := range cases {
		if got := c.q.Valid(); got != c.want {
			t.Errorf("Quadrant(%d).Valid() = %v, want %v", c.q, got, c.want)
		}
	}
}

func TestQuadrantLabels(t *testing.T) {
	cases := []struct {
		q     Quadrant
		label string
	}{
		{Do, "Do It First"},
		{Schedule, "Schedule It"},
		{Delegate, "Delegate It"},
		{Eliminate, "Consider Eliminating It"},
		{0, "?"},
	}
	for _, c := range cases {
		if got := c.q.Label(); got != c.label {
			t.Errorf("Quadrant(%d).Label() = %q, want %q", c.q, got, c.label)
		}
	}
}

func TestValidateLabel(t *testing.T) {
	if err := ValidateLabel(""); err != nil {
		t.Errorf("blank label should be allowed (it means reset): %v", err)
	}
	if err := ValidateLabel("Do It Right Now"); err != nil {
		t.Errorf("ordinary label rejected: %v", err)
	}
	if err := ValidateLabel(strings.Repeat("x", MaxLabelLen)); err != nil {
		t.Errorf("label at the limit rejected: %v", err)
	}
	if err := ValidateLabel(strings.Repeat("x", MaxLabelLen+1)); err == nil {
		t.Error("over-long label should be rejected")
	}
	// Measured in runes, not bytes, so accented names get their full length.
	if err := ValidateLabel(strings.Repeat("é", MaxLabelLen)); err != nil {
		t.Errorf("multi-byte label at the limit rejected: %v", err)
	}
	if err := ValidateLabel("two\nlines"); err == nil {
		t.Error("label with a line break should be rejected")
	}
	// A label is rendered into the quadrant header, so an escape sequence here
	// would repaint the matrix.
	if err := ValidateLabel("\x1b[31mred"); err == nil {
		t.Error("label with an ANSI escape should be rejected")
	}
}

func TestValidate(t *testing.T) {
	cases := []struct {
		name    string
		title   string
		q       Quadrant
		wantErr bool
	}{
		{"ok", "buy milk", Do, false},
		{"empty title", "", Do, true},
		{"whitespace title", "   ", Schedule, true},
		{"bad quadrant low", "x", 0, true},
		{"bad quadrant high", "x", 5, true},
		{"at length limit", strings.Repeat("x", MaxTitleLen), Do, false},
		{"over length limit", strings.Repeat("x", MaxTitleLen+1), Do, true},
		// Runes, not bytes, so accented titles get their full length.
		{"multi-byte at limit", strings.Repeat("é", MaxTitleLen), Do, false},
		// Control characters would let a stored title repaint the terminal, so
		// `ike list` could show something other than what is stored. The CSI
		// and carriage-return pair below is the line-overwrite trick.
		{"ansi escape", "real\x1b[2K\rFAKE", Do, true},
		{"bare escape", "\x1b", Do, true},
		{"newline", "two\nlines", Do, true},
		{"carriage return", "over\rwrite", Do, true},
		{"tab", "a\tb", Do, true},
		{"nul", "a\x00b", Do, true},
		{"del", "a\x7fb", Do, true},
		// C1 controls: 0x9b is an alternate CSI on some terminals.
		{"c1 csi", "a\u009bb", Do, true},
		// Ordinary non-ASCII text must still be accepted.
		{"emoji", "ship 🚀", Do, false},
		{"accents", "café résumé", Do, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := Validate(c.title, c.q)
			if (err != nil) != c.wantErr {
				t.Errorf("Validate(%q, %d) error = %v, wantErr %v", c.title, c.q, err, c.wantErr)
			}
		})
	}
}

func TestSanitizeDisplay(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain text untouched", "buy milk", "buy milk"},
		{"unicode untouched", "café 🚀", "café 🚀"},
		{"escape replaced", "\x1b[31mred", "�[31mred"},
		{"carriage return replaced", "over\rwrite", "over�write"},
		{"newline replaced", "two\nlines", "two�lines"},
		{"tab replaced", "a\tb", "a�b"},
		{"nul replaced", "a\x00b", "a�b"},
		{"del replaced", "a\x7fb", "a�b"},
		{"c1 replaced", "a\u009bb", "a�b"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := SanitizeDisplay(c.in); got != c.want {
				t.Errorf("SanitizeDisplay(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestSanitizeBlock(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain text untouched", "buy milk", "buy milk"},
		{"unicode untouched", "café 🚀", "café 🚀"},
		{"newline kept", "two\nlines", "two\nlines"},
		{"tab kept", "a\tb", "a\tb"},
		{"crlf keeps the newline, drops the return", "a\r\nb", "a�\nb"},
		{"escape replaced", "\x1b[31mred", "�[31mred"},
		{"osc 52 clipboard write replaced", "\x1b]52;c;aGk=\x07", "�]52;c;aGk=�"},
		{"nul replaced", "a\x00b", "a�b"},
		{"del replaced", "a\x7fb", "a�b"},
		{"c1 replaced", "ab", "a�b"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := SanitizeBlock(c.in); got != c.want {
				t.Errorf("SanitizeBlock(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// The two sanitizers must not be interchangeable. SanitizeDisplay replaces
// every rune below 0x20, newline included, so using it on a plan would collapse
// the whole thing onto one line of replacement characters — and using
// SanitizeBlock on a title would let a newline forge an extra listing row.
func TestSanitizersDifferOnNewlines(t *testing.T) {
	const plan = "## Goal\n\n- step one\n- step two\n"

	if got := SanitizeBlock(plan); got != plan {
		t.Errorf("SanitizeBlock mangled a plan: %q", got)
	}
	if got := SanitizeDisplay(plan); strings.Contains(got, "\n") {
		t.Error("SanitizeDisplay is supposed to strip newlines; " +
			"if that changed, SanitizeBlock has lost its reason to exist")
	}
}

func TestValidatePlan(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		wantErr bool
	}{
		{"blank is a clear, not an error", "", false},
		{"markdown with newlines and tabs", "## Goal\n\n- a\n\tb\n", false},
		{"at the limit", strings.Repeat("x", MaxPlanLen), false},
		{"over the limit", strings.Repeat("x", MaxPlanLen+1), true},
		{"counts runes, not bytes", strings.Repeat("é", MaxPlanLen), false},
		{"escape rejected", "plan\x1b[2Kwith escape", true},
		{"carriage return rejected", "plan\rwith return", true},
		{"nul rejected", "plan\x00", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := ValidatePlan(c.body); (err != nil) != c.wantErr {
				t.Errorf("ValidatePlan() error = %v, wantErr %v", err, c.wantErr)
			}
		})
	}
}

func TestHasPlan(t *testing.T) {
	when := time.Date(2026, 3, 4, 12, 0, 0, 0, time.UTC)
	if (Task{ID: 1}).HasPlan() {
		t.Error("a task with no PlanAt has no plan")
	}
	if !(Task{ID: 1, PlanAt: &when}).HasPlan() {
		t.Error("a task with PlanAt has a plan")
	}
}

// Validate rejects control characters on the way in, so a title only carries
// them if it predates that check or was written by something other than ike.
// DisplayTitle is the matching guarantee that such a title still cannot
// repaint a terminal.
func TestDisplayTitleNeutralizesUntrustedFile(t *testing.T) {
	const raw = "real\x1b[2K\rFAKE INJECTED"
	tk := Task{ID: 1, Title: raw, Quadrant: Do}

	if tk.Title != raw {
		t.Error("the stored title must be left alone")
	}
	got := tk.DisplayTitle()
	if strings.ContainsRune(got, 0x1b) || strings.ContainsRune(got, '\r') {
		t.Errorf("DisplayTitle() = %q, still contains control characters", got)
	}
	// The text stays legible; only the control bytes are replaced.
	if !strings.Contains(got, "real") || !strings.Contains(got, "FAKE INJECTED") {
		t.Errorf("DisplayTitle() = %q, dropped visible text", got)
	}
}

func TestArchiveDate(t *testing.T) {
	when := time.Date(2026, 3, 4, 12, 0, 0, 0, time.UTC)
	got := Task{ID: 1, DoneAt: &when}.ArchiveDate()
	if want := when.Local().Format("2006-01-02"); got != want {
		t.Errorf("ArchiveDate() = %q, want %q", got, want)
	}
	// A task with no completion stamp — reachable from a hand-edited or
	// pre-archive file — renders a blank date rather than a zero-time one.
	if got := (Task{ID: 1}).ArchiveDate(); got != "" {
		t.Errorf("ArchiveDate() with no stamp = %q, want empty", got)
	}
}

// ArchiveRow is the single definition of an archive line, rendered by both
// `ike archive` and the TUI's archive view.
func TestArchiveRow(t *testing.T) {
	when := time.Date(2026, 3, 4, 12, 0, 0, 0, time.UTC)
	tk := Task{ID: 7, Title: "ship it", DoneAt: &when}

	got := tk.ArchiveRow(tk.DisplayTitle())
	want := "    7  " + tk.ArchiveDate() + "  ship it"
	if got != want {
		t.Errorf("ArchiveRow() = %q, want %q", got, want)
	}
	// The title is supplied, so the TUI can pass a width-truncated one.
	if got := tk.ArchiveRow("ship…"); !strings.HasSuffix(got, "ship…") {
		t.Errorf("ArchiveRow() should render the title it is given: %q", got)
	}
}

func TestLessAndSortOrder(t *testing.T) {
	ts := []Task{
		{ID: 4, Quadrant: Schedule, Rank: 1024},
		{ID: 1, Quadrant: Do, Rank: 2048},
		{ID: 2, Quadrant: Do, Rank: 1024},
		{ID: 3, Quadrant: Do}, // unranked: sorts by ID, after ranked peers
	}
	SortOrder(ts)

	var got []int
	for _, t := range ts {
		got = append(got, t.ID)
	}
	// Quadrant first, then rank, then ID. Rank 0 sorts ahead of any real rank,
	// which is why normalizeRanks backfills it on every read.
	want := []int{3, 2, 1, 4}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("SortOrder = %v, want %v", got, want)
		}
	}
	if Less(ts[0], ts[0]) {
		t.Error("Less must be irreflexive")
	}
}
