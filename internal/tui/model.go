// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package tui

import (
	"context"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/table"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"github.com/FPGArtktic/lazysubmodules/internal/core"
)

// debounce is how long the preview and the tag count wait for further
// keys before they are loaded.
const debounce = 120 * time.Millisecond

// Model is the Bubble Tea model of the interface. Create it with New.
type Model struct {
	ctx     context.Context
	backend Backend
	jobs    *jobs
	keys    keyMap
	styles  styles

	width, height int

	table   table.Model
	spinner spinner.Model
	help    help.Model
	input   textinput.Model
	viewer  viewport.Model

	// seq numbers requests; a result is used only while its number is
	// the latest of its kind.
	seq int

	statuses  []core.Status
	loaded    bool
	statusSeq int
	// loads counts the visible reads in progress (status, refs, verify,
	// diff); busy names the running modifying operation, if any.
	loads    int
	busy     string
	spinning bool

	preview previewState
	overlay overlayState
	message statusLine
}

// previewState is the preview of one submodule.
type previewState struct {
	name    string
	seq     int
	loaded  bool
	data    core.Preview
	err     error
	pending bool
}

// statusLine is the message in the status bar.
type statusLine struct {
	text string
	kind messageKind
}

// messageKind selects the style of a status bar message.
type messageKind int

const (
	messageInfo messageKind = iota
	messageSuccess
	messageError
)

// newModel creates the model with or without colors.
func newModel(ctx context.Context, b Backend, noColor bool) Model {
	m := Model{
		ctx:     ctx,
		backend: b,
		jobs:    &jobs{},
		keys:    newKeyMap(),
		styles:  newStyles(noColor),
		spinner: spinner.New(spinner.WithSpinner(spinner.MiniDot)),
		help:    help.New(),
		viewer:  viewport.New(),
	}
	// Columns first: rows without columns make the table panic.
	m.table = table.New(
		table.WithColumns(columns(widths{})),
		table.WithFocused(true),
		table.WithKeyMap(tableKeys(m.keys)),
	)
	m.table.SetStyles(m.styles.table())
	m.help.ShortSeparator = "  "
	m.help.Styles = help.Styles{
		ShortKey: m.styles.key, ShortDesc: m.styles.desc, ShortSeparator: m.styles.desc,
		FullKey: m.styles.key, FullDesc: m.styles.desc, FullSeparator: m.styles.desc,
		Ellipsis: m.styles.desc,
	}
	m.input = newInput(m.styles)
	m.viewer.SoftWrap = true
	m.viewer.KeyMap = viewerKeys(m.keys)
	// Init cannot change the model, so the first load is registered here.
	m.statusSeq = m.nextSeq()
	m.loads = 1
	m.spinning = true
	return m
}

// newInput creates the text input of the pattern dialog. Its cursor does
// not blink, which would otherwise schedule a tick twice a second.
func newInput(s styles) textinput.Model {
	in := textinput.New()
	in.Prompt = "pattern: "
	in.Placeholder = "v1.*"
	in.CharLimit = 200
	in.KeyMap.Paste.Unbind()
	st := textinput.DefaultStyles(true)
	st.Cursor.Blink = false
	if s.noColor {
		st.Focused.Placeholder = s.dim
		st.Focused.Prompt = s.key
	}
	in.SetStyles(st)
	return in
}

// Init starts loading the status of the submodules.
//
// Context: called once by the Bubble Tea program.
// Return: the commands that load the status and start the spinner.
func (m Model) Init() tea.Cmd {
	return tea.Batch(m.statusCmd(m.statusSeq), m.spinner.Tick)
}

// Update applies a message to the model.
//
// Context: called by the Bubble Tea program for every message, one at a
// time.
// Return: the new model and the commands to run.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		return m, nil
	case tea.KeyPressMsg:
		return m.onKey(msg)
	case spinner.TickMsg:
		return m.onTick(msg)
	case tea.PasteMsg:
		if m.overlay.kind == overlayPattern || m.overlay.filtering {
			return m.updateInput(msg)
		}
		return m, nil
	}
	return m.onResult(msg)
}

// onTick advances the spinner while something is in progress and stops
// its ticks otherwise.
func (m Model) onTick(msg spinner.TickMsg) (Model, tea.Cmd) {
	if !m.working() {
		m.spinning = false
		return m, nil
	}
	var cmd tea.Cmd
	m.spinner, cmd = m.spinner.Update(msg)
	return m, cmd
}

// working reports whether the spinner should turn.
func (m Model) working() bool {
	return m.busy != "" || m.loads > 0 || m.preview.pending
}

// spin starts the spinner unless it is turning already.
func (m *Model) spin() tea.Cmd {
	if m.spinning {
		return nil
	}
	m.spinning = true
	return m.spinner.Tick
}

// nextSeq returns a new request number.
func (m *Model) nextSeq() int {
	m.seq++
	return m.seq
}

// call wraps a Backend call in a command that runs with the program
// context. A call is skipped once the program has ended.
func (m Model) call(fn func(ctx context.Context) tea.Msg) tea.Cmd {
	ctx, j := m.ctx, m.jobs
	return func() tea.Msg {
		if !j.begin() {
			return nil
		}
		defer j.end()
		return fn(ctx)
	}
}

// setMessage replaces the status bar message.
func (m *Model) setMessage(kind messageKind, text string) {
	m.message = statusLine{text: text, kind: kind}
}

// setError shows an error in the status bar.
func (m *Model) setError(err error) {
	m.setMessage(messageError, errorText(err))
}

// selected returns the status of the selected submodule.
func (m Model) selected() (core.Status, bool) {
	i := m.table.Cursor()
	if i < 0 || i >= len(m.statuses) {
		return core.Status{}, false
	}
	return m.statuses[i], true
}

// find returns the status of a submodule by name.
func (m Model) find(name string) (core.Status, bool) {
	for _, st := range m.statuses {
		if st.Submodule.Name == name {
			return st, true
		}
	}
	return core.Status{}, false
}
