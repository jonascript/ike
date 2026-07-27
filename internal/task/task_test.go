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
