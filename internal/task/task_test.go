package task

import (
	"strings"
	"testing"
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
