// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package tui

import (
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Fixed text of the frame.
const (
	sgrReset    = "\x1b[m"
	panelTable  = "Submodules"
	panelDetail = "Preview"
)

// View renders the screen.
//
// Context: called by the Bubble Tea program after every update.
// Return: the view in the alternate screen buffer; its content has exactly
// one line per terminal row, each exactly as wide as the terminal.
func (m Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	return v
}

// render returns the content of the screen.
func (m Model) render() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	if m.width < minScreenWidth || m.height < minScreenHeight {
		lines := make([]string, m.height)
		for i := range lines {
			lines[i] = strings.Repeat(" ", m.width)
		}
		lines[0] = fit("terminal too small", m.width)
		return strings.Join(lines, "\n")
	}
	l := m.geometry()
	body := m.frame(l)
	if box := m.overlayBox(); box != nil {
		body = overlayOn(body, box, m.width)
	}
	body = append(body, m.statusBar(), m.hintBar())
	return strings.Join(body, "\n")
}

// frame draws the panels with their borders.
func (m Model) frame(l frameLayout) []string {
	s, g := m.styles, m.styles.frame
	rows := max(l.body-2, 0)
	left := m.tablePanel(l.left, rows)
	var right []string
	top := s.border.Render(g.topLeft) + m.panelTitle(panelTable, l.left)
	bottom := g.bottomLeft + strings.Repeat(g.horizontal, l.left)
	if l.right > 0 {
		right = m.previewPanel(l.right, rows)
		top += s.border.Render(g.teeDown) + m.panelTitle(panelDetail, l.right)
		bottom += g.teeUp + strings.Repeat(g.horizontal, l.right)
	}
	lines := make([]string, 0, l.body)
	lines = append(lines, top+s.border.Render(g.topRight))
	bar := s.border.Render(g.vertical)
	for i := range rows {
		line := bar + fitCell(lineAt(left, i), l.left) + bar
		if l.right > 0 {
			line += fitCell(lineAt(right, i), l.right) + bar
		}
		lines = append(lines, line)
	}
	return append(lines, s.border.Render(bottom+g.bottomRight))
}

// fitCell fits a styled line into a panel and ends every style it
// opened, so that no style reaches the border.
func fitCell(s string, width int) string {
	s = fit(s, width)
	if strings.Contains(s, "\x1b") && !strings.HasSuffix(strings.TrimRight(s, " "), sgrReset) {
		s += sgrReset
	}
	return s
}

// lineAt returns line i, or "" past the end.
func lineAt(lines []string, i int) string {
	if i < len(lines) {
		return lines[i]
	}
	return ""
}

// panelTitle draws the top edge of a panel of the given inner width with
// its title.
func (m Model) panelTitle(title string, width int) string {
	t := ansi.Truncate(" "+title+" ", width, "")
	fill := max(width-ansi.StringWidth(t), 0)
	return m.styles.title.Render(t) +
		m.styles.border.Render(strings.Repeat(m.styles.frame.horizontal, fill))
}

// tablePanel returns the lines of the submodule panel.
func (m Model) tablePanel(width, rows int) []string {
	switch {
	case !m.loaded && m.loads > 0:
		return []string{" " + m.spinner.View() + " loading submodules…"}
	case !m.loaded:
		return wrapIndented("could not read the submodules; press r to try again", width)
	case len(m.statuses) == 0:
		return wrapIndented("no submodules in .gitmodules", width)
	}
	lines := strings.Split(m.table.View(), "\n")
	return lines[:min(len(lines), rows)]
}

// wrapIndented wraps plain text to a panel with one space of indentation.
func wrapIndented(text string, width int) []string {
	lines := wrap(text, max(width-1, 1))
	for i, l := range lines {
		lines[i] = " " + l
	}
	return lines
}

// statusBar renders the status line: the running operation with a
// spinner, or the last message.
func (m Model) statusBar() string {
	s := m.styles
	var parts []string
	switch {
	case m.busy != "":
		parts = append(parts, m.spinner.View()+" "+m.busy+"…")
	case m.loads > 0 && m.loaded:
		parts = append(parts, m.spinner.View()+" loading…")
	}
	switch {
	case m.message.text == "":
	case m.message.kind == messageError:
		parts = append(parts, s.failure.Render("error:")+" "+m.message.text)
	case m.message.kind == messageSuccess:
		parts = append(parts, s.success.Render(m.message.text))
	default:
		parts = append(parts, s.info.Render(m.message.text))
	}
	return fit(" "+strings.Join(parts, "  "), m.width)
}

// hintBar renders the key hints of the current view.
func (m Model) hintBar() string {
	hints, extra := m.hints()
	width := m.width - 1
	fits := func(b []key.Binding) bool {
		return ansi.StringWidth(m.help.ShortHelpView(b)) <= width
	}
	// The last two hints (help and quit, or close) stay; hints before them
	// are dropped from the end on a narrow screen, and extra hints are
	// added before them while there is room.
	tail := len(hints) - 2
	for len(hints) > 2 && !fits(hints) {
		tail--
		hints = slices.Delete(slices.Clone(hints), tail, tail+1)
	}
	for _, e := range extra {
		wider := slices.Insert(slices.Clone(hints), tail, e)
		if !fits(wider) {
			break
		}
		hints, tail = wider, tail+1
	}
	// The help bubble can render wider than the width it was given, so the
	// line is clipped instead of relying on it.
	line := lipgloss.NewStyle().MaxWidth(width).Render(m.help.ShortHelpView(hints))
	return fit(" "+line, m.width)
}

// hints returns the key hints for the current view; extra hints are shown
// when there is room for them. Every list has at least two entries.
func (m Model) hints() (hints, extra []key.Binding) {
	k := m.keys
	switch m.overlay.kind {
	case overlayConfirm:
		if m.overlay.loading {
			return []key.Binding{k.cancel, abortKey()}, nil
		}
		return []key.Binding{k.confirm, k.cancel}, nil
	case overlayPicker:
		if m.overlay.filtering {
			return []key.Binding{arrowKeys(), k.choose, clearFilterKey()}, nil
		}
		return []key.Binding{k.up, k.down, k.filter, k.choose, k.closeView}, nil
	case overlayPattern:
		return []key.Binding{k.choose, cancelInputKey()}, nil
	case overlayViewer:
		return []key.Binding{k.up, k.down, k.pageUp, k.pageDown, k.closeView}, nil
	}
	return k.mainHints()
}

// arrowKeys describes the arrows, which move the picker cursor while the
// filter is edited; the letters are text then.
func arrowKeys() key.Binding {
	return key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑/↓", "move"))
}

// abortKey describes ctrl+c while a confirmation loads.
func abortKey() key.Binding {
	return key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "quit"))
}

// clearFilterKey describes esc while the picker filter is edited.
func clearFilterKey() key.Binding {
	return key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "clear filter"))
}

// cancelInputKey describes esc in the pattern dialog, where q is text.
func cancelInputKey() key.Binding {
	return key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel"))
}

// box draws a dialog with a title around lines. With height > 0, the box
// has exactly that height, borders included; otherwise it fits the lines.
func (m Model) box(title string, lines []string, width, height int) []string {
	s, g := m.styles, m.styles.frame
	inner := max(width-2, 1)
	if height > 0 {
		lines = lines[:min(len(lines), max(height-2, 0))]
	}
	out := make([]string, 0, len(lines)+2)
	out = append(out, s.border.Render(g.topLeft)+m.panelTitle(title, inner)+
		s.border.Render(g.topRight))
	bar := s.border.Render(g.vertical)
	for _, l := range lines {
		out = append(out, bar+" "+fitCell(l, inner-2)+" "+bar)
	}
	for height > 0 && len(out) < height-1 {
		out = append(out, bar+strings.Repeat(" ", inner)+bar)
	}
	return append(out, s.border.Render(g.bottomLeft+strings.Repeat(g.horizontal, inner)+
		g.bottomRight))
}

// overlayOn draws a box centered over the body lines, the frame. On the
// rows between the top and the bottom border of the frame, the cell next
// to each side of the box is blanked, so that no text of the panels
// touches it, unless that cell is the outer border of the frame.
func overlayOn(body, box []string, width int) []string {
	if len(box) == 0 {
		return body
	}
	boxWidth := ansi.StringWidth(box[0])
	x := max((width-boxWidth)/2, 0)
	y := max((len(body)-len(box))/2, 0)
	gapLeft, gapRight := 0, 0
	if x >= 2 {
		gapLeft = 1
	}
	if x+boxWidth <= width-2 {
		gapRight = 1
	}
	out := append([]string(nil), body...)
	for i, b := range box {
		if y+i >= len(out) {
			break
		}
		row := out[y+i]
		gl, gr := gapLeft, gapRight
		if y+i == 0 || y+i == len(out)-1 {
			gl, gr = 0, 0
		}
		left := fit(ansi.Truncate(row, x-gl, ""), x-gl)
		right := ansi.TruncateLeft(row, x+boxWidth+gr, "")
		out[y+i] = fit(left+sgrReset+strings.Repeat(" ", gl)+b+strings.Repeat(" ", gr)+right,
			width)
	}
	return out
}
