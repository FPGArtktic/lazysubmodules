// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/FPGArtktic/lazysubmodules/internal/core"
	"github.com/FPGArtktic/lazysubmodules/internal/lock"
	"github.com/FPGArtktic/lazysubmodules/internal/manifest"
)

// previewDueMsg asks for the preview after the selection rested.
type previewDueMsg struct {
	seq int
}

// previewMsg carries the result of Backend.Preview.
type previewMsg struct {
	seq  int
	name string
	data core.Preview
	err  error
}

// schedulePreview loads the preview of the selection once the selection
// has not changed for a moment, so that scrolling does not start a git
// command for every row passed.
func (m *Model) schedulePreview() tea.Cmd {
	st, ok := m.selected()
	if !ok {
		return nil
	}
	seq := m.nextSeq()
	m.preview = previewState{name: st.Submodule.Name, seq: seq, pending: true}
	tick := tea.Tick(debounce, func(time.Time) tea.Msg { return previewDueMsg{seq: seq} })
	return tea.Batch(tick, m.spin())
}

// refreshPreview loads the preview of the selection now. The shown
// preview of the same submodule stays until the new one arrives.
func (m *Model) refreshPreview() tea.Cmd {
	st, ok := m.selected()
	if !ok {
		m.preview = previewState{}
		return nil
	}
	name := st.Submodule.Name
	if m.preview.name != name {
		m.preview = previewState{name: name}
	}
	m.preview.seq = m.nextSeq()
	m.preview.pending = true
	return tea.Batch(m.previewCmd(name, m.preview.seq), m.spin())
}

// previewCmd calls Backend.Preview.
func (m Model) previewCmd(name string, seq int) tea.Cmd {
	b := m.backend
	return m.call(func(ctx context.Context) tea.Msg {
		data, err := b.Preview(ctx, name)
		return previewMsg{seq: seq, name: name, data: data, err: err}
	})
}

// onPreviewDue starts loading a preview that is still wanted.
func (m Model) onPreviewDue(msg previewDueMsg) (Model, tea.Cmd) {
	if msg.seq != m.preview.seq {
		return m, nil
	}
	return m, m.previewCmd(m.preview.name, msg.seq)
}

// onPreview stores a preview that is still wanted.
func (m Model) onPreview(msg previewMsg) Model {
	if msg.seq != m.preview.seq {
		return m
	}
	m.preview.pending = false
	m.preview.loaded = true
	m.preview.data, m.preview.err = msg.data, msg.err
	if msg.err != nil {
		m.setError(msg.err)
	}
	m.refreshDetails()
	return m
}

// previewPanel returns the lines of the preview panel.
func (m Model) previewPanel(width, rows int) []string {
	st, ok := m.selected()
	if !ok {
		return nil
	}
	lines := m.summary(st, width-1, false)
	lines = append(lines, m.history(st, false)...)
	lines = lines[:min(len(lines), rows)]
	for i, l := range lines {
		lines[i] = " " + l
	}
	return lines
}

// summary describes the state, HEAD, lock entry and target of a
// submodule. In full, commits are not abbreviated and the configuration is
// listed as well.
func (m Model) summary(st core.Status, width int, full bool) []string {
	s := m.styles
	sub := st.Submodule
	commit, label := midCommit, 6
	switch {
	case full:
		commit, label = func(c string) string { return c }, 10
	case width < wideSummary:
		commit = shortCommit
	}
	field := func(name, value string) string {
		return fmt.Sprintf("%-*s %s", label, name, value)
	}
	head := "not checked out"
	if st.Head != "" {
		head = commit(st.Head)
	}
	lines := []string{s.title.Render(clean(sub.Name)) + " " + s.state(st.State)}
	for _, l := range wrap(clean(st.Reason), max(width, 1)) {
		lines = append(lines, s.dim.Render(l))
	}
	if full {
		lines = append(lines, "", field("Path", clean(sub.Path)), field("URL", clean(sub.URL)),
			field("Tracking", tracking(sub)), field("Branch key", orNone(clean(sub.Branch))))
	}
	return append(lines,
		field("HEAD", head),
		field("Lock", lockText(st.Lock, commit)),
		field("Target", targetText(st, commit)))
}

// wideSummary is the narrowest preview whose commits are abbreviated to
// 12 digits rather than 7.
const wideSummary = 40

// orNone returns s, or "none" when it is empty.
func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}

// tracking describes the tracking configuration of a submodule.
func tracking(sub manifest.Submodule) string {
	if !sub.Managed() {
		return "none (unmanaged)"
	}
	return string(sub.Mode) + " " + clean(sub.Ref)
}

// lockText describes a lock entry.
func lockText(e *lock.Entry, commit func(string) string) string {
	if e == nil {
		return "none"
	}
	if e.Mode == manifest.ModeCommit {
		return commit(e.Commit)
	}
	return clean(e.Ref) + " " + commit(e.Commit)
}

// targetText describes what an update would select.
func targetText(st core.Status, commit func(string) string) string {
	switch {
	case st.Target != nil && st.Target.Mode == manifest.ModeCommit:
		return commit(st.Target.Commit)
	case st.Target != nil:
		return clean(st.Target.Ref) + " " + commit(st.Target.Commit)
	case st.Submodule.Managed():
		return "unknown"
	}
	return "none"
}

// history returns the sections of the preview: the commits an update
// would add, how HEAD differs from the lock, recent commits and tags.
// Without all, the sections are shortened to fit a panel.
func (m Model) history(st core.Status, all bool) []string {
	p := m.preview
	switch {
	case p.name != st.Submodule.Name || !p.loaded:
		return []string{"", m.spinner.View() + " loading…"}
	case p.err != nil:
		return append([]string{"", m.styles.failure.Render("error:")},
			cleanLines(errorText(p.err))...)
	}
	limit := func(lines []string, n int) []string {
		if all || len(lines) <= n {
			return lines
		}
		return append(lines[:n:n], fmt.Sprintf("… %d more", len(lines)-n))
	}
	var out []string
	if len(p.data.Pending) > 0 {
		out = m.section(out, fmt.Sprintf("Update adds (%d)", len(p.data.Pending)),
			limit(p.data.Pending, 5))
	}
	if lines := lockDiffLines(st, p.data); lines != nil {
		out = m.section(out, "Lock vs HEAD", limit(lines, 5))
	}
	if len(p.data.Log) > 0 {
		out = m.section(out, "Log", limit(p.data.Log, 8))
	}
	if len(p.data.Tags) > 0 {
		out = m.section(out, "Tags", limit(p.data.Tags, 10))
	}
	return out
}

// section appends a titled, indented list.
func (m Model) section(out []string, title string, lines []string) []string {
	out = append(out, "", m.styles.section.Render(title))
	for _, l := range lines {
		out = append(out, "  "+clean(l))
	}
	return out
}

// lockDiffLines describes how HEAD differs from the lock entry, or returns
// nil when there is nothing to compare.
func lockDiffLines(st core.Status, p core.Preview) []string {
	switch {
	case st.Lock == nil || st.Head == "":
		return nil
	case st.Lock.Commit == st.Head:
		return []string{"HEAD is the locked commit"}
	case len(p.LockDiff) == 0:
		return []string{"the locked commit is not available locally"}
	}
	return p.LockDiff
}

// detailsText returns the text of the details dialog.
func (m Model) detailsText(st core.Status) string {
	width, _ := m.viewerSize()
	lines := m.summary(st, width, true)
	lines = append(lines, m.history(st, true)...)
	return strings.Join(lines, "\n")
}
