// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package tui

import (
	"context"
	"errors"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/FPGArtktic/lazysubmodules/internal/core"
	"github.com/FPGArtktic/lazysubmodules/internal/manifest"
)

// overlayKind selects the dialog shown over the panels.
type overlayKind int

const (
	overlayNone overlayKind = iota
	overlayConfirm
	overlayPicker
	overlayPattern
	overlayViewer
)

// viewerKind selects the content of the text viewer.
type viewerKind int

const (
	viewDetails viewerKind = iota
	viewVerify
	viewDiff
	viewHelp
	viewError
)

// overlayState is the open dialog. Only the fields of its kind are used.
type overlayState struct {
	kind  overlayKind
	name  string
	title string

	// Confirmation: the question, and what "y" does. An update
	// confirmation loads its question; commit tells whether it commits.
	prompt []string
	op     operation
	quit   bool
	commit bool

	// Picker: the refs of a mode, the current ref, the cursor and the
	// filter.
	mode      manifest.Mode
	current   string
	refs      []string
	cursor    int
	offset    int
	filtering bool

	// Loading state of the picker, the tag count and the viewer.
	seq     int
	loading bool

	// Pattern: the tags matching the pattern that was checked.
	matches  []string
	checked  string
	checkErr error

	// Viewer: its content, and for an error the text to wrap.
	view viewerKind
	text string
}

// operation is a modifying Backend call.
type operation struct {
	label string
	run   func(ctx context.Context) opMsg
}

// closeOverlay closes the open dialog.
func (m *Model) closeOverlay() {
	if m.overlay.kind == overlayPicker || m.overlay.kind == overlayPattern {
		m.input.Blur()
		m.input.Reset()
	}
	m.overlay = overlayState{}
}

// onKey routes a key to the open dialog or to the table.
func (m Model) onKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if key.Matches(msg, m.keys.abort) {
		return m, tea.Quit
	}
	switch m.overlay.kind {
	case overlayConfirm:
		return m.confirmKey(msg)
	case overlayPicker:
		return m.pickerKey(msg)
	case overlayPattern:
		return m.patternKey(msg)
	case overlayViewer:
		return m.viewerKey(msg)
	}
	return m.mainKey(msg)
}

// ask opens a confirmation for an operation.
func (m *Model) ask(name, title string, prompt []string, op operation) {
	m.closeOverlay()
	m.overlay = overlayState{kind: overlayConfirm, name: name, title: title, prompt: prompt,
		op: op}
}

// confirmKey handles a key in the confirmation dialog. The question must
// be shown before it can be confirmed.
func (m Model) confirmKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.confirm) && !m.overlay.loading:
		o := m.overlay
		m.closeOverlay()
		if o.quit {
			return m, tea.Quit
		}
		return m.start(o.op)
	case key.Matches(msg, m.keys.cancel):
		m.closeOverlay()
		m.setMessage(messageInfo, "canceled")
	}
	return m, nil
}

// viewerKey handles a key in the text viewer.
func (m Model) viewerKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.closeView),
		m.overlay.view == viewHelp && key.Matches(msg, m.keys.help):
		m.closeOverlay()
		return m, nil
	case key.Matches(msg, m.keys.top):
		m.viewer.GotoTop()
		return m, nil
	case key.Matches(msg, m.keys.bottom):
		m.viewer.GotoBottom()
		return m, nil
	}
	var cmd tea.Cmd
	m.viewer, cmd = m.viewer.Update(msg)
	return m, cmd
}

// openViewer opens the text viewer.
func (m *Model) openViewer(view viewerKind, name, title, text string, seq int) {
	m.closeOverlay()
	m.overlay = overlayState{kind: overlayViewer, view: view, name: name, title: title,
		seq: seq, loading: seq != 0}
	m.viewer.SetContent(text)
	m.viewer.GotoTop()
}

// refreshViewer lays out the text of the open viewer again for a new
// size. The diff and the checks are wrapped by the viewer itself.
func (m *Model) refreshViewer() {
	o := m.overlay
	if o.kind != overlayViewer {
		return
	}
	switch o.view {
	case viewHelp:
		m.setViewerText(m.helpText())
	case viewError:
		m.setViewerText(m.errorLines(o.text))
	case viewDetails:
		m.refreshDetails()
	}
}

// setViewerText replaces the text of the viewer, keeping its position.
func (m *Model) setViewerText(text string) {
	offset := m.viewer.YOffset()
	m.viewer.SetContent(text)
	m.viewer.SetYOffset(offset)
}

// refreshDetails updates an open details dialog after its data changed.
func (m *Model) refreshDetails() {
	o := m.overlay
	if o.kind != overlayViewer || o.view != viewDetails {
		return
	}
	st, ok := m.find(o.name)
	if !ok {
		return
	}
	m.setViewerText(m.detailsText(st))
}

// overlayBox renders the open dialog, or returns nil.
func (m Model) overlayBox() []string {
	o := m.overlay
	switch o.kind {
	case overlayConfirm:
		if o.loading {
			return m.box(o.title, []string{m.spinner.View() + " checking…", "",
				m.styles.key.Render("n") + " cancel"}, m.dialogWidth(), 0)
		}
		lines := make([]string, 0, len(o.prompt)+2)
		for _, p := range o.prompt {
			lines = append(lines, wrap(p, m.dialogWidth()-4)...)
		}
		lines = append(lines, "", m.styles.key.Render("y")+" confirm   "+
			m.styles.key.Render("n")+" cancel")
		return m.box(o.title, lines, m.dialogWidth(), 0)
	case overlayPicker:
		return m.pickerBox()
	case overlayPattern:
		return m.patternBox()
	case overlayViewer:
		body := m.geometry().body
		text := strings.Split(m.viewer.View(), "\n")
		if o.loading {
			text = []string{m.spinner.View() + " loading…"}
		}
		return m.box(o.title, text, m.width-4, body)
	}
	return nil
}

// helpText returns the key reference, in two columns when the viewer is
// wide enough.
func (m Model) helpText() string {
	groups := m.keys.helpGroups()
	blocks := make([]string, 0, len(groups))
	for _, g := range groups {
		lines := []string{m.styles.section.Render(g.title)}
		for _, b := range g.bindings {
			h := b.Help()
			lines = append(lines, "  "+m.styles.key.Render(pad(h.Key, 7))+" "+h.Desc)
		}
		blocks = append(blocks, strings.Join(lines, "\n"))
	}
	const columnWidth = 34
	width, _ := m.viewerSize()
	var body string
	if width >= 2*columnWidth {
		col := lipgloss.NewStyle().Width(columnWidth)
		left := lipgloss.JoinVertical(lipgloss.Left, blocks[0], "", blocks[2])
		right := lipgloss.JoinVertical(lipgloss.Left, blocks[1], "", blocks[3])
		body = lipgloss.JoinHorizontal(lipgloss.Top, col.Render(left), col.Render(right))
	} else {
		body = strings.Join(blocks, "\n\n")
	}
	notes := []string{
		"In dialogs, y confirms and n or esc cancels. q closes a dialog, except in " +
			"text fields (the pattern dialog and the filter of a list), where it is text; " +
			"esc leaves them.",
		"While an operation runs, q asks before quitting; ctrl+c quits at once and " +
			"interrupts the operation.",
		"Only f (fetch) uses the network.",
	}
	var lines []string
	for _, n := range notes {
		lines = append(lines, wrap(n, width)...)
	}
	return body + "\n\n" + strings.Join(lines, "\n")
}

// pad pads s with spaces to n cells.
func pad(s string, n int) string {
	return fit(s, max(n, ansi.StringWidth(s)))
}

// Hints for a submodule that must be fetched first.
const (
	cliFetchHint = "(use --fetch)"
	tuiFetchHint = "(press f to fetch)"
)

// errorText formats an error for the status bar, on one line, with a hint
// for refusals the interface can help with.
func errorText(err error) string {
	text := strings.Join(cleanLines(err.Error()), "; ")
	switch {
	case errors.Is(err, core.ErrUninitialized):
		// The message of the core package names the option of the command
		// line; the interface fetches with a key.
		if strings.Contains(text, cliFetchHint) {
			return strings.Replace(text, cliFetchHint, tuiFetchHint, 1)
		}
		text += " " + tuiFetchHint
	case errors.Is(err, core.ErrMissingRef):
		text += " (press f to fetch, or change the ref)"
	case errors.Is(err, core.ErrUnmanaged):
		text += " (press b, t or p to track it)"
	}
	return text
}
