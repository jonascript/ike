package tui

import (
	"fmt"
	"image/color"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/joncrockett/ike/internal/task"
)

const (
	minWidth  = 40
	minHeight = 10
)

// quadrantColor returns the accent color for a quadrant, adapted to the
// terminal background.
func (m Model) quadrantColor(q task.Quadrant) color.Color {
	ld := lipgloss.LightDark(m.isDark)
	switch q {
	case task.Do:
		return ld(lipgloss.Color("#c0392b"), lipgloss.Color("#e06c75"))
	case task.Schedule:
		return ld(lipgloss.Color("#1a5fb4"), lipgloss.Color("#61afef"))
	case task.Delegate:
		return ld(lipgloss.Color("#a05a00"), lipgloss.Color("#e5c07b"))
	}
	return ld(lipgloss.Color("#777777"), lipgloss.Color("#5c6370"))
}

func (m Model) dimColor() color.Color {
	return lipgloss.LightDark(m.isDark)(lipgloss.Color("#999999"), lipgloss.Color("#5c6370"))
}

func (m Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	return v
}

func (m Model) render() string {
	if m.width < minWidth || m.height < minHeight {
		return fmt.Sprintf("terminal too small (%dx%d, need %dx%d)", m.width, m.height, minWidth, minHeight)
	}
	if m.mode == modeArchive {
		return m.renderArchive()
	}

	footer := m.renderFooter()
	footerH := lipgloss.Height(footer)
	gridH := m.height - footerH

	cellW := m.width / 2
	cellH := gridH / 2

	q1 := m.renderQuadrant(task.Do, cellW, cellH)
	q2 := m.renderQuadrant(task.Schedule, cellW, cellH)
	q3 := m.renderQuadrant(task.Delegate, cellW, cellH)
	q4 := m.renderQuadrant(task.Eliminate, cellW, cellH)

	grid := lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.JoinHorizontal(lipgloss.Top, q1, q2),
		lipgloss.JoinHorizontal(lipgloss.Top, q3, q4),
	)
	return lipgloss.JoinVertical(lipgloss.Left, grid, footer)
}

func (m Model) renderQuadrant(q task.Quadrant, w, h int) string {
	focused := q == m.focus && m.mode != modeArchive
	accent := m.quadrantColor(q)

	border := lipgloss.RoundedBorder()
	borderColor := m.dimColor()
	if focused {
		border = lipgloss.ThickBorder()
		borderColor = accent
	}
	box := lipgloss.NewStyle().
		Border(border).
		BorderForeground(borderColor).
		Padding(0, 1).
		Width(w - 2).  // border adds 2 columns
		Height(h - 2). // border adds 2 rows
		MaxWidth(w)

	innerW := w - 4 // border (2) + padding (2)
	header := ansi.Truncate(fmt.Sprintf("%d · %s — %s", q, q.Label(), q.Desc()), innerW, "…")
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(accent)

	lines := []string{headerStyle.Render(header)}
	lines = append(lines, m.renderTaskLines(q, innerW, h-3, focused)...) // h-3: border rows + header line
	return box.Render(strings.Join(lines, "\n"))
}

func (m Model) renderTaskLines(q task.Quadrant, w, visible int, focused bool) []string {
	tasks := m.tasksIn(q)
	if len(tasks) == 0 {
		return []string{lipgloss.NewStyle().Foreground(m.dimColor()).Italic(true).Render("empty")}
	}

	// Scroll window that keeps the cursor visible.
	offset := 0
	if focused && m.cursor[q] >= visible {
		offset = m.cursor[q] - visible + 1
	}

	selStyle := lipgloss.NewStyle().Bold(true)
	idStyle := lipgloss.NewStyle().Foreground(m.dimColor())

	var lines []string
	for i := offset; i < len(tasks) && i-offset < visible; i++ {
		t := tasks[i]
		marker := "  "
		title := ansi.Truncate(t.Title, max(w-8, 4), "…")
		line := fmt.Sprintf("%s%s %s", marker, idStyle.Render(fmt.Sprintf("%3d", t.ID)), title)
		if focused && i == m.cursor[q] {
			line = selStyle.Render(fmt.Sprintf("▸ %3d %s", t.ID, title))
		}
		lines = append(lines, line)
	}
	if offset+visible < len(tasks) {
		lines = append(lines, idStyle.Render(fmt.Sprintf("  … %d more", len(tasks)-offset-visible)))
	}
	return lines
}

func (m Model) renderFooter() string {
	dim := lipgloss.NewStyle().Foreground(m.dimColor())

	var status string
	switch m.mode {
	case modeInput:
		verb := fmt.Sprintf("add to %d · %s", m.focus, m.focus.Label())
		if m.editingID != 0 {
			verb = fmt.Sprintf("edit %d", m.editingID)
		}
		status = fmt.Sprintf("%s: %s", verb, m.input.View())
	default:
		status = m.status
	}

	help := "a add · e edit · x done · m move · d delete · v archive · 1-4/tab focus · ? help · q quit"
	if m.showHelp {
		help = strings.Join([]string{
			"1-4 focus quadrant · tab/shift+tab cycle · j/k or ↑/↓ select task",
			"a add to focused quadrant · e edit title · x/enter complete · m then 1-4 move",
			"d twice delete permanently · v archive view · ? close help · q quit",
		}, "\n")
	}
	return strings.Join([]string{
		ansi.Truncate(status, m.width, "…"),
		dim.Render(help),
	}, "\n")
}

func (m Model) renderArchive() string {
	dim := lipgloss.NewStyle().Foreground(m.dimColor())
	title := lipgloss.NewStyle().Bold(true).Render(fmt.Sprintf("Archive — %d completed", len(m.data.Archive)))

	arch := make([]task.Task, len(m.data.Archive))
	copy(arch, m.data.Archive)
	// Newest completion first, matching `ike archive`.
	for i, j := 0, len(arch)-1; i < j; i, j = i+1, j-1 {
		arch[i], arch[j] = arch[j], arch[i]
	}

	visible := m.height - 3 // title + blank + footer
	offset := 0
	if m.archCursor >= visible {
		offset = m.archCursor - visible + 1
	}

	lines := []string{title, ""}
	if len(arch) == 0 {
		lines = append(lines, dim.Italic(true).Render("nothing completed yet"))
	}
	for i := offset; i < len(arch) && i-offset < visible; i++ {
		t := arch[i]
		when := ""
		if t.DoneAt != nil {
			when = t.DoneAt.Local().Format("2006-01-02")
		}
		line := fmt.Sprintf("  %3d  %s  %s", t.ID, when, ansi.Truncate(t.Title, max(m.width-20, 4), "…"))
		if i == m.archCursor {
			line = lipgloss.NewStyle().Bold(true).Render("▸" + line[1:])
		}
		lines = append(lines, line)
	}
	lines = append(lines, dim.Render("j/k scroll · v/esc/q back"))
	return strings.Join(lines, "\n")
}
