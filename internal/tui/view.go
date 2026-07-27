package tui

import (
	"fmt"
	"image/color"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jonascript/ike/internal/store"
	"github.com/jonascript/ike/internal/task"
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

	const gutterW = 2               // vertical axis labels: letter + space
	gridH := m.height - footerH - 1 // 1 row for the top axis labels

	cellW := (m.width - gutterW) / 2
	cellH := gridH / 2

	axis := lipgloss.NewStyle().Bold(true).Foreground(m.dimColor())
	header := strings.Repeat(" ", gutterW) +
		axis.Render(center("Urgent", cellW)) +
		axis.Render(center("Not urgent", cellW))

	q1 := m.renderQuadrant(task.Do, cellW, cellH)
	q2 := m.renderQuadrant(task.Schedule, cellW, cellH)
	q3 := m.renderQuadrant(task.Delegate, cellW, cellH)
	q4 := m.renderQuadrant(task.Eliminate, cellW, cellH)

	topGutter := axis.Render(verticalLabel("IMPORTANT", cellH, gutterW))
	botGutter := axis.Render(verticalLabel("NOT IMPORTANT", cellH, gutterW))

	grid := lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.JoinHorizontal(lipgloss.Top, topGutter, q1, q2),
		lipgloss.JoinHorizontal(lipgloss.Top, botGutter, q3, q4),
	)
	return lipgloss.JoinVertical(lipgloss.Left, header, grid, footer)
}

// center pads s to width w, centered.
func center(s string, w int) string {
	if len(s) >= w {
		return s[:w]
	}
	left := (w - len(s)) / 2
	return strings.Repeat(" ", left) + s + strings.Repeat(" ", w-len(s)-left)
}

// verticalLabel renders text one letter per line, vertically centered in
// height rows and padded to width columns. If the text is too tall it falls
// back to a short form, then to blank.
func verticalLabel(text string, height, width int) string {
	if len(text) > height {
		short := map[string]string{"IMPORTANT": "IMP", "NOT IMPORTANT": "NOT IMP"}[text]
		if len(short) > height {
			short = ""
		}
		text = short
	}
	top := (height - len(text)) / 2
	lines := make([]string, height)
	for i := range lines {
		ch := " "
		if i >= top && i-top < len(text) {
			ch = string(text[i-top])
		}
		lines[i] = ch + strings.Repeat(" ", width-1)
	}
	return strings.Join(lines, "\n")
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
	// Width/Height are outer dimensions in lipgloss v2 (border included).
	box := lipgloss.NewStyle().
		Border(border).
		BorderForeground(borderColor).
		Padding(0, 1).
		Width(w).
		Height(h)

	innerW := w - 4 // border (2) + padding (2)
	header := ansi.Truncate(fmt.Sprintf("%d · %s", q, m.data.Labels.Of(q)), innerW, "…")
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
		verb := fmt.Sprintf("add to %d · %s", m.focus, m.data.Labels.Of(m.focus))
		switch {
		case m.labelTarget != 0:
			verb = fmt.Sprintf("rename quadrant %d", m.labelTarget)
		case m.editingID != 0:
			verb = fmt.Sprintf("edit %d", m.editingID)
		}
		status = fmt.Sprintf("%s: %s", verb, m.input.View())
	default:
		status = m.status
	}

	// Redo is only offered while there is something to redo — the next real
	// change discards the stack, from any frontend.
	undoHelp := "u undo"
	if store.RedoLabel(m.data) != "" {
		undoHelp = "u undo · U redo"
	}
	help := "a add · e edit · x done · m move · J/K reorder · d delete · " +
		undoHelp + " · v archive · ? help · q quit"
	if m.showHelp {
		help = strings.Join([]string{
			"1-4 focus quadrant · tab/shift+tab cycle · j/k or ↑/↓ select task",
			"a add to focused quadrant · e edit title · x/enter complete · m then 1-4 move quadrant",
			"t rename the focused quadrant (empty input restores its default name)",
			"J/K or shift+↑/↓ reorder within quadrant · u undo last change (any frontend)",
			"U or ctrl+r redo, until the next change discards it",
			"d twice delete permanently · v archive view (r there restores) · ? close help · q quit",
			m.mcpHelpLine(),
		}, "\n")
	}
	helpLines := strings.Split(help, "\n")
	for i, l := range helpLines {
		helpLines[i] = dim.Render(ansi.Truncate(l, m.width, "…"))
	}
	statusLine := ansi.Truncate(status, m.width, "…")
	if m.mode != modeInput {
		// Not while typing: the status row holds the text input then, and the
		// marker would trail the cursor.
		statusLine = m.withMCPIndicator(statusLine)
	}
	return strings.Join(append([]string{statusLine}, helpLines...), "\n")
}

// mcpHelpLine states the current MCP access setting and the command that
// changes it. It names the marker so the footer symbol is self-explaining.
func (m Model) mcpHelpLine() string {
	if m.data.MCPEnabled {
		return mcpIndicator + " AI agents can manage this matrix over MCP · turn off with: ike mcp disable"
	}
	return "AI agent access (MCP) is off · turn on with: ike mcp enable"
}

// mcpIndicator is the ambient "an agent can reach this matrix" marker. It is
// shown only while access is on, so its absence is the quiet default.
const mcpIndicator = "◆ mcp"

// withMCPIndicator right-aligns the indicator on a footer line, if access is
// on and the line has room for it. Too narrow, and the status text wins.
func (m Model) withMCPIndicator(line string) string {
	if !m.data.MCPEnabled {
		return line
	}
	used := ansi.StringWidth(line)
	if used+1+ansi.StringWidth(mcpIndicator) > m.width {
		return line
	}
	gap := m.width - used - ansi.StringWidth(mcpIndicator)
	marker := lipgloss.NewStyle().Foreground(m.quadrantColor(task.Delegate)).Render(mcpIndicator)
	return line + strings.Repeat(" ", gap) + marker
}

func (m Model) renderArchive() string {
	dim := lipgloss.NewStyle().Foreground(m.dimColor())
	title := lipgloss.NewStyle().Bold(true).Render(fmt.Sprintf("Archive — %d completed", len(m.data.Archive)))

	// Newest completion first, matching `ike archive`.
	arch := m.archiveList()

	visible := m.height - 4 // title + blank + status + footer
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
	lines = append(lines,
		ansi.Truncate(m.status, m.width, "…"),
		dim.Render("r restore to its quadrant · j/k scroll · v/esc/q back"),
	)
	return strings.Join(lines, "\n")
}
