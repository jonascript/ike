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
	minWidth = 40
	// minHeight fits one task row: 2 footer rows, the space header, the axis
	// labels, and two 4-row cells — a border pair, a quadrant heading, and the
	// row itself. Below this renderTaskLines has nothing to draw in and the
	// matrix shows headings over empty boxes.
	minHeight = 12
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

// tooSmallMessage explains that the terminal needs to be bigger, in a form that
// fits the terminal it is complaining about. The single-line version is 37
// characters, so at 30 columns it was cut to "terminal too small (30x8, need" —
// losing the required size, which is the only actionable part of it.
func tooSmallMessage(w, h int) string {
	need := fmt.Sprintf("need %dx%d", minWidth, minHeight)

	if long := fmt.Sprintf("terminal too small (%dx%d, %s)", w, h, need); len(long) <= w {
		return long
	}
	// Two lines, because at this width one cannot carry both the current size
	// and the required one. Needs a second row to put them on.
	if short := fmt.Sprintf("too small: %dx%d", w, h); h >= 2 && len(short) <= w && len(need) <= w {
		return short + "\n" + need
	}
	if len(need) <= w {
		return need
	}
	// Below ten columns nothing informative fits, so keep the numbers that say
	// how far there is to go.
	return fmt.Sprintf("%dx%d", minWidth, minHeight)
}

func (m Model) render() string {
	if m.width < minWidth || m.height < minHeight {
		return tooSmallMessage(m.width, m.height)
	}
	if m.mode == modeArchive {
		return m.renderArchive()
	}
	if m.mode == modeSpaces {
		return m.renderSpaces()
	}
	if m.mode == modeFiles {
		return m.renderFiles()
	}
	if m.mode == modeRun {
		return m.renderRun()
	}
	if m.mode == modePlan {
		return m.renderPlan()
	}

	footer := m.renderFooter()
	footerH := lipgloss.Height(footer)

	const gutterW = 2               // vertical axis labels: letter + space
	gridH := m.height - footerH - 2 // space header + top axis labels

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
	return lipgloss.JoinVertical(lipgloss.Left, m.renderSpaceHeader(), header, grid, footer)
}

// renderSpaceHeader names the space on screen. Without it there is nothing on
// the matrix that says which one you are looking at, and the four quadrants of
// two different spaces look alike.
func (m Model) renderSpaceHeader() string {
	// Styled as one run rather than bolding the name alone, so the rendered text
	// stays contiguous: everything that reads this screen, tests included, can
	// then match on "space <name>".
	line := lipgloss.NewStyle().Bold(true).Render("space " + task.SanitizeDisplay(m.data.Space))
	if n := len(m.data.AllSpaces); n > 1 {
		at := 1
		for i, sp := range m.data.AllSpaces {
			if sp.Name == m.data.Space {
				at = i + 1
			}
		}
		counter := lipgloss.NewStyle().Foreground(m.dimColor()).
			Render(fmt.Sprintf("%d of %d · s to switch", at, n))
		if used := ansi.StringWidth(line) + 1 + ansi.StringWidth(counter); used <= m.width {
			line += strings.Repeat(" ", m.width-used+1) + counter
		}
	}
	return ansi.Truncate(line, m.width, "…")
}

// renderSpaces is the space picker: a full-screen list, in the shape of the
// archive view, that can also create, rename, and delete what it lists.
func (m Model) renderSpaces() string {
	spaces := m.data.AllSpaces
	width := 0
	for _, sp := range spaces {
		width = max(width, ansi.StringWidth(task.SanitizeDisplay(sp.Name)))
	}
	rows := make([]string, len(spaces))
	for i, sp := range spaces {
		// The current space is where every other frontend acts, so it is marked
		// even when the cursor is elsewhere in the list.
		current := " "
		if sp.Current {
			current = "•"
		}
		rows[i] = fmt.Sprintf("  %s %-*s  %d active, %d archived",
			current, width, task.SanitizeDisplay(sp.Name), sp.Active, sp.Archived)
	}
	return m.renderList(listView{
		title:  fmt.Sprintf("Spaces — %d", len(spaces)),
		rows:   rows,
		cursor: m.spaceCursor,
		hint:   "enter switch · n new · r rename · d twice delete · j/k select · s/esc/q back",
	})
}

// renderFiles is the data file picker: the files opened before, and a way to
// type a path that is not among them.
func (m Model) renderFiles() string {
	rows := make([]string, len(m.recent))
	for i, path := range m.recent {
		// The file in use is marked even when the cursor is elsewhere.
		gutter := "    "
		if path == m.store.Path() {
			gutter = "  • "
		}
		rows[i] = gutter + path
	}
	return m.renderList(listView{
		title:  "Data files",
		header: []string{lipgloss.NewStyle().Foreground(m.dimColor()).Render("current: " + m.store.Path()), ""},
		rows:   rows,
		empty:  "no other files opened yet — press o to type a path",
		cursor: m.fileCursor,
		hint:   "enter open · o type a path · j/k select · f/esc/q back",
	})
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
	tasks := m.data.List(q)
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
		// The delegation mark sits after the title so it cannot push the ID
		// column around, and is one cell wide so a row's width is unchanged.
		mark := m.taskMark(t)
		title := ansi.Truncate(t.DisplayTitle(), max(w-8-len([]rune(mark)), 4), "…") + mark
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

// Delegation marks. A task can be planned, and one task at a time can have a
// run going; the running mark wins, since it is the more immediate fact.
const (
	planMark = " ✎"
	runMark  = " ⣾"
)

// taskMark is the delegation marker for a row, or "" for a task with neither.
//
// Rendered from PlanAt on the task rather than by looking for the plan file,
// which is the whole reason that stamp is persisted: the matrix redraws on
// every keypress and a stat per row per frame would be a poor trade for a
// symbol.
func (m Model) taskMark(t task.Task) string {
	if m.run != nil && !m.run.done && m.run.taskID == t.ID {
		return runMark
	}
	if t.HasPlan() {
		return planMark
	}
	return ""
}

func (m Model) renderFooter() string {
	dim := lipgloss.NewStyle().Foreground(m.dimColor())

	var status string
	switch m.mode {
	case modeInput:
		verb := fmt.Sprintf("add to %d · %s", m.focus, m.data.Labels.Of(m.focus))
		switch m.purpose {
		case inputRenameQuadrant:
			verb = fmt.Sprintf("rename quadrant %d", m.labelTarget)
		case inputEditTask:
			verb = fmt.Sprintf("edit %d", m.editingID)
		case inputNewSpace:
			verb = "new space"
		case inputRenameSpace:
			verb = fmt.Sprintf("rename space %s", task.SanitizeDisplay(m.spaceTarget))
		case inputOpenFile:
			verb = "open data file"
		}
		status = fmt.Sprintf("%s: %s", verb, m.input.View())
	default:
		status = m.status
	}
	// A failed background reload outranks any transient status message: the
	// matrix on screen may no longer match the file.
	if m.loadErr != "" && m.mode != modeInput {
		status = "cannot re-read the data file: " + task.SanitizeDisplay(m.loadErr)
	}

	// Redo is only offered while there is something to redo — the next real
	// change discards the stack, from any frontend.
	undoHelp := "u undo"
	if store.RedoLabel(m.data) != "" {
		undoHelp = "u undo · U redo"
	}
	help := "a add · e edit · x done · m move · J/K reorder · d delete · " +
		undoHelp + " · v archive · s spaces · p plan · D delegate · ? help · q quit"
	if m.showHelp {
		help = strings.Join([]string{
			"1-4 focus quadrant · tab/shift+tab cycle · j/k or ↑/↓ select task",
			"a add to focused quadrant · e edit title · x/enter complete · m then 1-4 move quadrant",
			"t rename the focused quadrant (empty input restores its default name)",
			"J/K or shift+↑/↓ reorder within quadrant · u undo last change (any frontend)",
			"U or ctrl+r redo, until the next change discards it",
			"d twice delete permanently · v archive view (r there restores) · ? close help · q quit",
			"s spaces: switch matrix, or n/r/dd to add, rename, delete · ]/[ next/prev space",
			"each space keeps its own tasks, headings, and history",
			"space changes are not undoable, unlike changes to tasks",
			"f data files: switch to another matrix file, or o to type a path",
			"p show the attached plan (" + strings.TrimSpace(planMark) + " marks a task that has one) · P draft one with an agent",
			"D delegate: run an agent on the task, or reattach to a run already going",
			"in a run: esc detaches and it keeps going · ctrl+c stops it · quitting ike stops it",
			m.agentHelpLine(),
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

// agentHelpLine states whether ike may run an agent, and the command that
// changes it — the delegation counterpart of mcpHelpLine.
func (m Model) agentHelpLine() string {
	if m.data.AgentAllowed {
		return "delegation is on: D runs an agent on a task · turn off with: ike agent disable"
	}
	return "delegation is off · turn on with: ike agent enable (p and P still work; planning is read-only)"
}

// mcpHelpLine states the current MCP access setting and the command that
// changes it. It names the marker so the footer symbol is self-explaining.
func (m Model) mcpHelpLine() string {
	if m.data.MCPAllowed {
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
	if !m.data.MCPAllowed {
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
	// Newest completion first, matching `ike archive`.
	arch := m.data.ListArchive()
	rows := make([]string, len(arch))
	for i, t := range arch {
		rows[i] = t.ArchiveRow(ansi.Truncate(t.DisplayTitle(), max(m.width-20, 4), "…"))
	}
	return m.renderList(listView{
		title: fmt.Sprintf("Archive — %d completed in %s",
			len(arch), task.SanitizeDisplay(m.data.Space)),
		rows:   rows,
		empty:  "nothing completed yet",
		cursor: m.archCursor,
		hint:   "r restore to its quadrant · j/k scroll · v/esc/q back",
	})
}
