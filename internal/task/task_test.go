package task

import "testing"

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
		desc  string
	}{
		{Do, "Do", "Urgent + Important"},
		{Schedule, "Schedule", "Not Urgent"},
		{Delegate, "Delegate", "Not Urgent"},
		{Eliminate, "Eliminate", "Not Important"},
		{0, "?", ""},
	}
	for _, c := range cases {
		if got := c.q.Label(); got != c.label {
			t.Errorf("Quadrant(%d).Label() = %q, want %q", c.q, got, c.label)
		}
		if got := c.q.Desc(); got != c.desc {
			t.Errorf("Quadrant(%d).Desc() = %q, want %q", c.q, got, c.desc)
		}
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
