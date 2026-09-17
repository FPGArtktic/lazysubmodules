// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package tui

import (
	"strings"
	"unicode"

	"charm.land/bubbles/v2/table"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/FPGArtktic/lazysubmodules/internal/core"
)

// ANSI colors of the interface; terminals map them to their palette, and
// the renderer drops them for terminals without colors.
const (
	colorRed     = "1"
	colorGreen   = "2"
	colorYellow  = "3"
	colorMagenta = "5"
	colorCyan    = "6"
	colorGray    = "8"
)

// styles holds every style of the interface. Without colors, only bold
// text and reverse video remain, so that the selection stays visible.
type styles struct {
	noColor bool

	border, title, section, dim, key, desc lipgloss.Style
	selected, info, success, failure       lipgloss.Style
	states                                 map[core.State]lipgloss.Style

	frame frameGlyphs
}

// frameGlyphs holds the characters of the frame and the dialog borders.
type frameGlyphs struct {
	horizontal, vertical                       string
	topLeft, topRight, bottomLeft, bottomRight string
	teeDown, teeUp                             string
}

// boxHorizontal is the horizontal line of the box drawing characters.
const boxHorizontal = "─"

// newFrameGlyphs returns the box drawing characters when they take one cell
// each, and ASCII characters otherwise. Terminals set up for East Asian
// text show box drawing characters two cells wide, which the width
// functions follow when RUNEWIDTH_EASTASIAN is true; the layout needs one
// cell per border character.
func newFrameGlyphs(boxWidth int) frameGlyphs {
	if boxWidth != 1 {
		return frameGlyphs{horizontal: "-", vertical: "|", topLeft: "+", topRight: "+",
			bottomLeft: "+", bottomRight: "+", teeDown: "+", teeUp: "+"}
	}
	return frameGlyphs{horizontal: boxHorizontal, vertical: "│", topLeft: "┌", topRight: "┐",
		bottomLeft: "└", bottomRight: "┘", teeDown: "┬", teeUp: "┴"}
}

// newStyles builds the styles, with or without colors.
func newStyles(noColor bool) styles {
	plain := lipgloss.NewStyle()
	s := styles{
		noColor:  noColor,
		border:   plain,
		title:    plain.Bold(true),
		section:  plain.Bold(true),
		dim:      plain,
		key:      plain.Bold(true),
		desc:     plain,
		selected: plain.Reverse(true),
		info:     plain,
		success:  plain,
		failure:  plain.Bold(true),
		states:   map[core.State]lipgloss.Style{},
		frame:    newFrameGlyphs(ansi.StringWidth(boxHorizontal)),
	}
	if noColor {
		return s
	}
	color := func(c string) lipgloss.Style { return plain.Foreground(lipgloss.Color(c)) }
	s.border = color(colorGray)
	s.title = color(colorCyan).Bold(true)
	s.section = color(colorCyan).Bold(true)
	s.dim = color(colorGray)
	s.key = color(colorCyan).Bold(true)
	s.success = color(colorGreen)
	s.failure = color(colorRed).Bold(true)
	s.states = map[core.State]lipgloss.Style{
		core.StateOK:            color(colorGreen),
		core.StateBehind:        color(colorYellow),
		core.StateDrift:         color(colorRed),
		core.StateMissingRef:    color(colorRed),
		core.StateDirty:         color(colorMagenta),
		core.StateUninitialized: color(colorGray),
		core.StateUnmanaged:     color(colorGray),
	}
	return s
}

// state renders a state name in its color.
func (s styles) state(st core.State) string {
	if style, ok := s.states[st]; ok {
		return style.Render(string(st))
	}
	return string(st)
}

// table returns the styles of the submodule table. Cells carry no colors
// of their own, so that the selection style covers the whole row; the
// cells of other rows are colored before they are handed to the table.
func (s styles) table() table.Styles {
	return table.Styles{
		Header:   s.title.Padding(0, 0, 0, 1),
		Cell:     lipgloss.NewStyle().Padding(0, 0, 0, 1),
		Selected: s.selected,
	}
}

// clean makes text from git or from the configuration safe to show: control
// characters and other characters that are not printable are replaced, and
// the text is kept on one line.
func clean(s string) string {
	return strings.Map(func(c rune) rune {
		if c == '\t' {
			return ' '
		}
		if unicode.IsPrint(c) {
			return c
		}
		return unicode.ReplacementChar
	}, s)
}

// cleanLines splits text into lines and cleans each of them.
func cleanLines(s string) []string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range lines {
		lines[i] = clean(l)
	}
	return lines
}

// fit truncates or pads a styled line to exactly width cells.
func fit(s string, width int) string {
	if width <= 0 {
		return ""
	}
	s = ansi.Truncate(s, width, "…")
	if w := ansi.StringWidth(s); w < width {
		s += strings.Repeat(" ", width-w)
	}
	return s
}

// wrap breaks plain text into lines of at most width cells.
func wrap(s string, width int) []string {
	if width <= 0 {
		return nil
	}
	return strings.Split(ansi.Wrap(s, width, " "), "\n")
}

// shortCommit abbreviates a commit name for the table.
func shortCommit(commit string) string {
	const n = 7
	if len(commit) > n {
		return commit[:n]
	}
	return commit
}

// midCommit abbreviates a commit name for the preview.
func midCommit(commit string) string {
	const n = 12
	if len(commit) > n {
		return commit[:n]
	}
	return commit
}
