// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package tui

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/FPGArtktic/lazysubmodules/internal/core"
	"github.com/FPGArtktic/lazysubmodules/internal/manifest"
)

// Prompts of the text input.
const (
	promptFilter  = "filter: "
	promptPattern = "pattern: "
)

// matchSamples is the number of matching tags the pattern dialog lists.
const matchSamples = 5

// setHint reminds that changing the tracking configuration does not
// update the submodule.
const setHint = "Only .gitmodules changes; press u afterwards to update."

// refsMsg carries the branches or tags for the picker.
type refsMsg struct {
	seq  int
	refs []string
	err  error
}

// checkDueMsg asks for the tags matching the pattern after typing paused.
type checkDueMsg struct {
	seq int
}

// matchesMsg carries the tags matching a pattern.
type matchesMsg struct {
	seq     int
	pattern string
	tags    []string
	err     error
}

// openPicker opens the list of branches (branch mode) or tags (tag mode)
// of a submodule and starts loading it.
func (m Model) openPicker(st core.Status, mode manifest.Mode) (Model, tea.Cmd) {
	name := st.Submodule.Name
	current := ""
	if st.Submodule.Mode == mode {
		current = st.Submodule.Ref
	}
	m.closeOverlay()
	seq := m.nextSeq()
	m.overlay = overlayState{kind: overlayPicker, name: name, mode: mode, current: current,
		title: fmt.Sprintf("Track %s by %s", clean(name), mode), seq: seq, loading: true}
	m.loads++
	b := m.backend
	load := m.call(func(ctx context.Context) tea.Msg {
		var refs []string
		var err error
		if mode == manifest.ModeBranch {
			refs, err = b.Branches(ctx, name)
		} else {
			refs, err = b.Tags(ctx, name, "")
		}
		return refsMsg{seq: seq, refs: refs, err: err}
	})
	spin := m.spin()
	return m, tea.Batch(load, spin)
}

// onRefs fills the picker.
func (m Model) onRefs(msg refsMsg) Model {
	m.loads--
	o := &m.overlay
	if o.kind != overlayPicker || msg.seq != o.seq {
		return m
	}
	o.loading = false
	switch {
	case msg.err != nil:
		m.closeOverlay()
		m.setError(msg.err)
		return m
	case len(msg.refs) == 0:
		what := "remote branches"
		if o.mode != manifest.ModeBranch {
			what = "tags"
		}
		m.setMessage(messageInfo, fmt.Sprintf("%s has no %s; press f to fetch", clean(o.name), what))
		m.closeOverlay()
		return m
	}
	o.refs = msg.refs
	o.cursor = max(slices.Index(msg.refs, o.current), 0)
	m.clampPicker()
	return m
}

// visibleRefs returns the refs that match the filter.
func (m Model) visibleRefs() []string {
	filter := strings.ToLower(m.input.Value())
	if filter == "" {
		return m.overlay.refs
	}
	var out []string
	for _, r := range m.overlay.refs {
		if strings.Contains(strings.ToLower(r), filter) {
			out = append(out, r)
		}
	}
	return out
}

// pickerCapacity returns the number of refs the picker shows at once.
func (m Model) pickerCapacity() int {
	return max(m.geometry().body-3, 1)
}

// clampPicker keeps the picker cursor on a visible ref and scrolls to it.
func (m *Model) clampPicker() {
	o := &m.overlay
	n := len(m.visibleRefs())
	capacity := m.pickerCapacity()
	o.cursor = min(max(o.cursor, 0), max(n-1, 0))
	if o.cursor < o.offset {
		o.offset = o.cursor
	}
	if o.cursor >= o.offset+capacity {
		o.offset = o.cursor - capacity + 1
	}
	o.offset = min(max(o.offset, 0), max(n-capacity, 0))
}

// pickerKey handles a key in the picker.
func (m Model) pickerKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	o := &m.overlay
	k := m.keys
	switch {
	case o.loading:
		if key.Matches(msg, k.closeView) {
			m.closeOverlay()
		}
		return m, nil
	case o.filtering:
		return m.filterKey(msg)
	case key.Matches(msg, k.closeView):
		m.closeOverlay()
		return m, nil
	case key.Matches(msg, k.filter):
		o.filtering = true
		m.input.Prompt = promptFilter
		m.sizeInput()
		// A temporary: Focus changes m.input.
		cmd := m.input.Focus()
		return m, cmd
	case key.Matches(msg, k.choose):
		return m.choose()
	}
	capacity := m.pickerCapacity()
	switch {
	case key.Matches(msg, k.up):
		o.cursor--
	case key.Matches(msg, k.down):
		o.cursor++
	case key.Matches(msg, k.pageUp):
		o.cursor -= capacity
	case key.Matches(msg, k.pageDown):
		o.cursor += capacity
	case key.Matches(msg, k.top):
		o.cursor = 0
	case key.Matches(msg, k.bottom):
		o.cursor = len(m.visibleRefs()) - 1
	}
	m.clampPicker()
	return m, nil
}

// filterKey handles a key while the picker filter is edited: arrows move,
// enter chooses, esc clears the filter, and other keys edit it.
func (m Model) filterKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	o := &m.overlay
	switch msg.String() {
	case "esc":
		o.filtering = false
		m.input.Blur()
		m.input.Reset()
	case "enter":
		return m.choose()
	case "up":
		o.cursor--
	case "down":
		o.cursor++
	default:
		return m.updateInput(msg)
	}
	m.clampPicker()
	return m, nil
}

// choose asks to track the ref under the picker cursor.
func (m Model) choose() (tea.Model, tea.Cmd) {
	refs := m.visibleRefs()
	o := m.overlay
	if len(refs) == 0 {
		return m, nil
	}
	ref := refs[o.cursor]
	prompt := []string{fmt.Sprintf("Track %s by %s %s?", clean(o.name), o.mode, clean(ref)), setHint}
	m.ask(o.name, "Change tracking", prompt, m.setOp(o.name, o.mode, ref))
	return m, nil
}

// pickerBox renders the picker.
func (m Model) pickerBox() []string {
	o := m.overlay
	width := m.dialogWidth()
	if o.loading {
		return m.box(o.title, []string{m.spinner.View() + " loading…"}, width, 0)
	}
	refs := m.visibleRefs()
	header := fmt.Sprintf("%d of %d; / filters", len(refs), len(o.refs))
	if o.filtering || m.input.Value() != "" {
		header = m.input.View()
	}
	lines := []string{header}
	if len(refs) == 0 {
		lines = append(lines, m.styles.dim.Render("no match"))
	}
	end := min(len(refs), o.offset+m.pickerCapacity())
	for i := o.offset; i < end; i++ {
		text := "  " + clean(refs[i])
		if refs[i] == o.current {
			text += " (current)"
		}
		if i == o.cursor {
			text = m.styles.selected.Render(fit("> "+text[2:], width-4))
		}
		lines = append(lines, text)
	}
	return m.box(o.title, lines, width, 0)
}

// openPattern opens the tag pattern dialog, prefilled with the configured
// pattern.
func (m Model) openPattern(st core.Status) (Model, tea.Cmd) {
	name := st.Submodule.Name
	m.closeOverlay()
	m.overlay = overlayState{kind: overlayPattern, name: name,
		title: "Track " + clean(name) + " by tag pattern"}
	m.input.Prompt = promptPattern
	m.sizeInput()
	if st.Submodule.Mode == manifest.ModeTagPattern {
		m.input.SetValue(st.Submodule.Ref)
		m.input.CursorEnd()
	}
	focus := m.input.Focus()
	check := m.scheduleCheck()
	return m, tea.Batch(focus, check)
}

// sizeInput fits the text input into the dialog.
func (m *Model) sizeInput() {
	m.input.SetWidth(max(m.dialogWidth()-4-len(m.input.Prompt)-1, 1))
}

// updateInput passes a message to the text input and reacts to a changed
// value.
func (m Model) updateInput(msg tea.Msg) (Model, tea.Cmd) {
	before := m.input.Value()
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	if m.input.Value() == before {
		return m, cmd
	}
	if m.overlay.kind == overlayPattern {
		check := m.scheduleCheck()
		return m, tea.Batch(cmd, check)
	}
	m.overlay.cursor, m.overlay.offset = 0, 0
	m.clampPicker()
	return m, cmd
}

// scheduleCheck counts the tags matching the pattern once typing paused.
func (m *Model) scheduleCheck() tea.Cmd {
	o := &m.overlay
	o.seq = m.nextSeq()
	o.matches, o.checked, o.checkErr = nil, "", nil
	o.loading = m.input.Value() != ""
	if !o.loading {
		return nil
	}
	seq := o.seq
	return tea.Tick(debounce, func(time.Time) tea.Msg { return checkDueMsg{seq: seq} })
}

// onCheckDue lists the tags matching the pattern that is still typed.
func (m Model) onCheckDue(msg checkDueMsg) (Model, tea.Cmd) {
	o := m.overlay
	if o.kind != overlayPattern || msg.seq != o.seq {
		return m, nil
	}
	b, name, pattern, seq := m.backend, o.name, m.input.Value(), o.seq
	return m, m.call(func(ctx context.Context) tea.Msg {
		tags, err := b.Tags(ctx, name, pattern)
		return matchesMsg{seq: seq, pattern: pattern, tags: tags, err: err}
	})
}

// onMatches stores the tags matching the pattern.
func (m Model) onMatches(msg matchesMsg) Model {
	o := &m.overlay
	if o.kind != overlayPattern || msg.seq != o.seq {
		return m
	}
	o.loading = false
	o.checked, o.matches, o.checkErr = msg.pattern, msg.tags, msg.err
	return m
}

// patternKey handles a key in the pattern dialog. Every printable key,
// q included, is text there.
func (m Model) patternKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.closeOverlay()
		return m, nil
	case "enter":
		return m.submitPattern()
	}
	return m.updateInput(msg)
}

// submitPattern asks to track the typed pattern. Only an invalid pattern
// is refused: tags that cannot be listed, such as those of a submodule that
// was never cloned, do not stop the tracking from changing, as they do not
// for the set command.
func (m Model) submitPattern() (tea.Model, tea.Cmd) {
	o := m.overlay
	value := m.input.Value()
	checked := o.checked == value
	switch {
	case value == "":
		m.setMessage(messageInfo, "type a tag pattern such as v1.*")
		return m, nil
	case checked && invalidPattern(o.checkErr):
		m.setError(o.checkErr)
		return m, nil
	}
	question := fmt.Sprintf("Track %s by tag pattern %s?", clean(o.name), clean(value))
	switch {
	case checked && o.checkErr != nil:
		question += " Its local tags could not be listed."
	case checked:
		question += fmt.Sprintf(" It matches %d local tags.", len(o.matches))
	}
	m.ask(o.name, "Change tracking", []string{question, setHint},
		m.setOp(o.name, manifest.ModeTagPattern, value))
	return m, nil
}

// patternBox renders the pattern dialog.
func (m Model) patternBox() []string {
	o := m.overlay
	width := m.dialogWidth()
	value := m.input.Value()
	lines := []string{m.input.View(), ""}
	switch {
	case value == "":
		lines = append(lines, "Type a glob such as v1.* or v6.6.*.")
	case o.loading || o.checked != value:
		lines = append(lines, "checking…")
	case invalidPattern(o.checkErr):
		lines = append(lines, m.styles.failure.Render("invalid pattern"))
		lines = append(lines, wrap(errorText(o.checkErr), width-4)...)
	case o.checkErr != nil:
		lines = append(lines, m.styles.failure.Render("tags unavailable"))
		lines = append(lines, wrap(errorText(o.checkErr), width-4)...)
	default:
		lines = append(lines, m.matchLines(o.matches)...)
	}
	lines = append(lines, "", "Pre-release tags are skipped by an update,",
		"unless the lock records one already.")
	return m.box(o.title, lines, width, 0)
}

// invalidPattern reports whether the tags matching a pattern could not be
// listed because the pattern is invalid.
func invalidPattern(err error) bool {
	return errors.Is(err, core.ErrInvalidArgument)
}

// matchLines summarizes the tags matching a pattern.
func (m Model) matchLines(tags []string) []string {
	pre := 0
	for _, t := range tags {
		if core.IsPrerelease(t) {
			pre++
		}
	}
	summary := fmt.Sprintf("%d local tags match", len(tags))
	if len(tags) == 1 {
		summary = "1 local tag matches"
	}
	if pre > 0 {
		summary += fmt.Sprintf(" (%d pre-release)", pre)
	}
	lines := []string{m.styles.success.Render(summary)}
	for _, t := range tags[:min(len(tags), matchSamples)] {
		line := "  " + clean(t)
		if core.IsPrerelease(t) {
			line += m.styles.dim.Render(" pre-release")
		}
		lines = append(lines, line)
	}
	if len(tags) > matchSamples {
		lines = append(lines, fmt.Sprintf("  … %d more", len(tags)-matchSamples))
	}
	return lines
}
