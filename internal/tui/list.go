package tui

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// listView is a full-screen scrolling list. The archive, the space picker, and
// the file picker are all this shape, differing only in what a row says and
// which keys act on it.
//
// They were three separate render functions with the same scroll arithmetic
// copied into each, and the copies had drifted: the file picker reserved five
// rows of chrome while drawing six, so a long list rendered one line past the
// bottom of the terminal. Deriving the chrome from the header here rather than
// counting it by hand at each call site is what stops that recurring.
type listView struct {
	title  string   // heading, rendered bold
	header []string // extra lines between the title and the rows
	rows   []string // one per item, each starting with a two-space gutter
	empty  string   // shown in place of the rows when there are none
	cursor int
	hint   string // key hints, on the last line
}

// cursorMark is the selected-row indicator. It replaces the first column of a
// row's gutter, so a row's width does not change when it is selected.
const cursorMark = "▸"

// render draws the list into the terminal, scrolled to keep the cursor visible.
func (m Model) renderList(v listView) string {
	dim := lipgloss.NewStyle().Foreground(m.dimColor())
	bold := lipgloss.NewStyle().Bold(true)

	// Every line that is not a row: the title, the blank after it, whatever the
	// caller put in the header, then the status line and the hint.
	chrome := 2 + len(v.header) + 2
	visible := max(m.height-chrome, 1)

	offset := 0
	if v.cursor >= visible {
		offset = v.cursor - visible + 1
	}

	lines := append([]string{bold.Render(v.title), ""}, v.header...)
	if len(v.rows) == 0 {
		lines = append(lines, dim.Italic(true).Render(v.empty))
	}
	for i := offset; i < len(v.rows) && i-offset < visible; i++ {
		row := v.rows[i]
		if i == v.cursor {
			row = bold.Render(mark(row))
		}
		lines = append(lines, ansi.Truncate(row, m.width, "…"))
	}
	return strings.Join(append(lines,
		ansi.Truncate(m.status, m.width, "…"),
		dim.Render(v.hint),
	), "\n")
}

// mark puts the cursor indicator in a row's gutter.
func mark(row string) string {
	if row == "" {
		return cursorMark
	}
	return cursorMark + row[1:]
}

// moveCursor applies the keys every list shares — up, down, and nothing else —
// reporting whether it consumed the message so the caller can handle its own.
func moveCursor(cursor *int, n int, msg tea.KeyPressMsg) bool {
	switch {
	case key.Matches(msg, keys.Down):
		if *cursor < n-1 {
			*cursor++
		}
	case key.Matches(msg, keys.Up):
		if *cursor > 0 {
			*cursor--
		}
	default:
		return false
	}
	return true
}

// clampCursor bounds a list cursor to n items, for a list that changed under it.
func clampCursor(cursor *int, n int) {
	if *cursor >= n {
		*cursor = max(n-1, 0)
	}
	if *cursor < 0 {
		*cursor = 0
	}
}
