// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package tui

import (
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/table"
	"charm.land/bubbles/v2/viewport"
)

// keyMap holds the key bindings of the interface.
type keyMap struct {
	up, down, pageUp, pageDown, top, bottom key.Binding

	details, update, commit, branch, tag, pattern  key.Binding
	fetch, verify, diff, reload, help, quit, abort key.Binding

	confirm, cancel, closeView, filter, choose key.Binding
}

// newKeyMap returns the key bindings. Bubble Tea reports a shifted letter
// by its text, so "U" is bound rather than "shift+u".
func newKeyMap() keyMap {
	return keyMap{
		up:       key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		down:     key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		pageUp:   key.NewBinding(key.WithKeys("pgup"), key.WithHelp("pgup", "page up")),
		pageDown: key.NewBinding(key.WithKeys("pgdown"), key.WithHelp("pgdn", "page down")),
		top:      key.NewBinding(key.WithKeys("home", "g"), key.WithHelp("g", "first")),
		bottom:   key.NewBinding(key.WithKeys("end", "G"), key.WithHelp("G", "last")),

		details: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "details")),
		update:  key.NewBinding(key.WithKeys("u"), key.WithHelp("u", "update")),
		commit:  key.NewBinding(key.WithKeys("U"), key.WithHelp("U", "update+commit")),
		branch:  key.NewBinding(key.WithKeys("b"), key.WithHelp("b", "branch")),
		tag:     key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "tag")),
		pattern: key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "pattern")),
		fetch:   key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "fetch")),
		verify:  key.NewBinding(key.WithKeys("v"), key.WithHelp("v", "verify")),
		diff:    key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "diff")),
		reload:  key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "reload")),
		help:    key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		quit:    key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
		abort:   key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "quit now")),

		confirm:   key.NewBinding(key.WithKeys("y", "Y"), key.WithHelp("y", "confirm")),
		cancel:    key.NewBinding(key.WithKeys("n", "N", "esc", "q"), key.WithHelp("n/esc", "cancel")),
		closeView: key.NewBinding(key.WithKeys("esc", "q"), key.WithHelp("esc/q", "close")),
		filter:    key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
		choose:    key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "choose")),
	}
}

// mainHints lists the hints of the key bar for the table, most important
// first; the extra hints are shown before the last two when the bar is wide
// enough.
func (k keyMap) mainHints() (hints, extra []key.Binding) {
	hints = []key.Binding{k.update, k.branch, k.tag, k.pattern, k.fetch, k.verify, k.help, k.quit}
	extra = []key.Binding{k.commit, k.diff, k.reload, k.details}
	return hints, extra
}

// helpGroups lists every binding of the table view for the help overlay.
func (k keyMap) helpGroups() []helpGroup {
	return []helpGroup{
		{"Navigation", []key.Binding{k.up, k.down, k.pageUp, k.pageDown, k.top, k.bottom}},
		{"Submodule", []key.Binding{k.details, k.update, k.commit, k.fetch, k.verify, k.diff}},
		{"Tracking", []key.Binding{k.branch, k.tag, k.pattern}},
		{"General", []key.Binding{k.reload, k.help, k.quit, k.abort}},
	}
}

// helpGroup is a titled column of the help overlay.
type helpGroup struct {
	title    string
	bindings []key.Binding
}

// tableKeys restricts the table bindings to navigation. The default key
// map of the table also binds u, d, b, f and space, which are actions
// here.
func tableKeys(k keyMap) table.KeyMap {
	km := table.DefaultKeyMap()
	km.LineUp = k.up
	km.LineDown = k.down
	km.PageUp = k.pageUp
	km.PageDown = k.pageDown
	km.GotoTop = k.top
	km.GotoBottom = k.bottom
	km.HalfPageUp.Unbind()
	km.HalfPageDown.Unbind()
	return km
}

// viewerKeys returns the scroll bindings of the text viewer. Letters that
// the viewport binds by default are dropped, except j and k, so that no
// key does something unexpected there.
func viewerKeys(k keyMap) viewport.KeyMap {
	km := viewport.DefaultKeyMap()
	km.Up = k.up
	km.Down = k.down
	km.PageUp = k.pageUp
	km.PageDown = key.NewBinding(key.WithKeys("pgdown", "space"))
	km.HalfPageUp = key.NewBinding(key.WithKeys("ctrl+u"))
	km.HalfPageDown = key.NewBinding(key.WithKeys("ctrl+d"))
	km.Left.Unbind()
	km.Right.Unbind()
	return km
}
