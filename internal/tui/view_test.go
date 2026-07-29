package tui

import (
	"strings"
	"testing"
)

// The message about a too-small terminal has to survive being printed in that
// terminal. The long form is 37 characters, so at 30 columns it used to be cut
// to "terminal too small (30x8, need", dropping the required size — the only
// part the reader can act on.
func TestTooSmallMessageFitsTheTerminal(t *testing.T) {
	for _, c := range []struct {
		w, h int
	}{
		{120, 40}, {40, 12}, {39, 11}, {37, 10}, {36, 9}, {30, 8}, {20, 6},
		{16, 4}, {15, 3}, {14, 2}, {12, 2}, {10, 1}, {9, 1}, {5, 1}, {1, 1},
		{0, 0},
	} {
		msg := tooSmallMessage(c.w, c.h)
		if msg == "" {
			t.Errorf("%dx%d: empty message", c.w, c.h)
			continue
		}
		lines := strings.Split(msg, "\n")
		for i, line := range lines {
			// Measured in runes: the message is ASCII today, but a width check
			// on bytes would be wrong the moment it is not.
			if n := len([]rune(line)); n > c.w && c.w >= 5 {
				t.Errorf("%dx%d: line %d is %d wide and will be cut: %q",
					c.w, c.h, i, n, line)
			}
		}
		if len(lines) > c.h && c.h >= 2 {
			t.Errorf("%dx%d: message needs %d rows but only %d are available: %q",
				c.w, c.h, len(lines), c.h, msg)
		}
	}
}

func TestTooSmallMessageStaysUseful(t *testing.T) {
	// Wide enough for the full sentence.
	if got := tooSmallMessage(60, 5); got != "terminal too small (60x5, need 40x12)" {
		t.Errorf("at 60 columns = %q, want the full sentence", got)
	}
	// The width from the original report. Both the actual and required sizes
	// have to survive, across two lines.
	got := tooSmallMessage(30, 8)
	if !strings.Contains(got, "30x8") {
		t.Errorf("at 30 columns = %q, dropped the current size", got)
	}
	if !strings.Contains(got, "40x12") {
		t.Errorf("at 30 columns = %q, dropped the required size", got)
	}
	// Very narrow: the requirement is what matters, so it is what survives.
	if got := tooSmallMessage(12, 3); !strings.Contains(got, "40x12") {
		t.Errorf("at 12 columns = %q, should still say what is needed", got)
	}
	// A single row cannot hold two lines.
	if got := tooSmallMessage(20, 1); strings.Contains(got, "\n") {
		t.Errorf("at height 1 = %q, must be one line", got)
	}
}

// The render path must actually use it, at the boundary in both dimensions.
func TestRenderTooSmallAtTheBoundary(t *testing.T) {
	m, _ := testModel(t)
	for _, c := range []struct {
		w, h  int
		small bool
	}{
		{minWidth, minHeight, false},
		{minWidth - 1, minHeight, true},
		{minWidth, minHeight - 1, true},
		{30, 8, true},
	} {
		m.width, m.height = c.w, c.h
		out := m.render()
		isSmall := strings.Contains(out, "too small") || strings.Contains(out, "need ")
		if isSmall != c.small {
			t.Errorf("%dx%d: too-small message present = %v, want %v (got %q)",
				c.w, c.h, isSmall, c.small, out)
		}
		if c.small {
			for _, line := range strings.Split(out, "\n") {
				if len([]rune(line)) > c.w {
					t.Errorf("%dx%d: rendered line exceeds the width: %q", c.w, c.h, line)
				}
			}
		}
	}
}
