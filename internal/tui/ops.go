// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package tui

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/FPGArtktic/lazysubmodules/internal/core"
	"github.com/FPGArtktic/lazysubmodules/internal/manifest"
)

// statusMsg carries the result of Backend.Status.
type statusMsg struct {
	seq      int
	statuses []core.Status
	err      error
}

// opMsg carries the outcome of a modifying operation; id numbers it in
// the jobs.
type opMsg struct {
	id   int
	text string
	err  error
}

// planMsg carries the dry run of an update that its confirmation shows.
// For an update with a commit, staged is the dry run without the commit.
type planMsg struct {
	seq    int
	plan   core.UpdateResult
	staged core.UpdateResult
	err    error
}

// verifyMsg carries the result of Backend.Verify.
type verifyMsg struct {
	seq     int
	name    string
	results []core.VerifyResult
	err     error
}

// diffMsg carries the result of Backend.GitlinkDiff.
type diffMsg struct {
	seq  int
	name string
	text string
	err  error
}

// onResult dispatches the result of a command.
func (m Model) onResult(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case statusMsg:
		return m.onStatus(msg)
	case opMsg:
		return m.onOp(msg)
	case planMsg:
		return m.onPlan(msg), nil
	case previewDueMsg:
		return m.onPreviewDue(msg)
	case previewMsg:
		return m.onPreview(msg), nil
	case refsMsg:
		return m.onRefs(msg), nil
	case checkDueMsg:
		return m.onCheckDue(msg)
	case matchesMsg:
		return m.onMatches(msg), nil
	case verifyMsg:
		return m.onVerify(msg)
	case diffMsg:
		return m.onDiff(msg)
	}
	return m, nil
}

// mainKey handles a key in the table view.
func (m Model) mainKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k := m.keys
	switch {
	case key.Matches(msg, k.quit):
		return m.quit()
	case key.Matches(msg, k.help):
		m.openViewer(viewHelp, "", "Keys", m.helpText(), 0)
		return m, nil
	case key.Matches(msg, k.reload):
		m.setMessage(messageInfo, "")
		return m.reload()
	}
	actions := []key.Binding{k.details, k.update, k.commit, k.branch, k.tag, k.pattern,
		k.fetch, k.verify, k.diff}
	if !key.Matches(msg, actions...) {
		return m.moveCursor(msg)
	}
	st, ok := m.selected()
	if !ok {
		m.setMessage(messageInfo, "no submodule selected")
		return m, nil
	}
	return m.act(msg, st)
}

// act runs the action of a key on the selected submodule.
func (m Model) act(msg tea.KeyPressMsg, st core.Status) (tea.Model, tea.Cmd) {
	k := m.keys
	modifying := []key.Binding{k.update, k.commit, k.branch, k.tag, k.pattern, k.fetch}
	if m.busy != "" && key.Matches(msg, modifying...) {
		m.setMessage(messageInfo, "busy: "+m.busy+"; wait until it is done")
		return m, nil
	}
	managedOnly := []key.Binding{k.update, k.commit, k.fetch, k.verify}
	if !st.Submodule.Managed() && key.Matches(msg, managedOnly...) {
		m.setMessage(messageInfo, clean(st.Submodule.Name)+
			" is not managed; press b, t or p to track it")
		return m, nil
	}
	switch {
	case key.Matches(msg, k.details):
		return m.openDetails(st)
	case key.Matches(msg, k.update, k.commit):
		return m.askUpdate(st, key.Matches(msg, k.commit))
	case key.Matches(msg, k.branch):
		return m.openPicker(st, manifest.ModeBranch)
	case key.Matches(msg, k.tag):
		return m.openPicker(st, manifest.ModeTag)
	case key.Matches(msg, k.pattern):
		return m.openPattern(st)
	case key.Matches(msg, k.fetch):
		return m.start(m.fetchOp(st.Submodule.Name))
	case key.Matches(msg, k.verify):
		return m.verify(st.Submodule.Name)
	}
	return m.openDiff(st.Submodule.Name)
}

// moveCursor passes a key to the table and loads the preview of a newly
// selected row.
func (m Model) moveCursor(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	before := m.table.Cursor()
	m.table, _ = m.table.Update(msg)
	after := m.table.Cursor()
	if after == before {
		return m, nil
	}
	m.table.SetRows(m.rows(after))
	// A temporary: the call changes m, and the order in which the results
	// are evaluated is not specified.
	cmd := m.schedulePreview()
	return m, cmd
}

// quit ends the program, after a confirmation while an operation runs.
func (m Model) quit() (tea.Model, tea.Cmd) {
	if m.busy == "" {
		return m, tea.Quit
	}
	m.ask("", "Quit", []string{
		"An operation is running: " + m.busy + ".",
		"Quitting interrupts it; changes made so far are put back where possible. Quit?",
	}, operation{})
	m.overlay.quit = true
	return m, nil
}

// reload loads the status of all submodules again. The preview is loaded
// again with the status, so a preview that is still due is dropped.
func (m Model) reload() (Model, tea.Cmd) {
	m.statusSeq = m.nextSeq()
	m.loads++
	if m.preview.pending {
		m.preview.seq = m.nextSeq()
	}
	load := m.statusCmd(m.statusSeq)
	spin := m.spin()
	return m, tea.Batch(load, spin)
}

// statusCmd calls Backend.Status for all submodules.
func (m Model) statusCmd(seq int) tea.Cmd {
	b := m.backend
	return m.call(func(ctx context.Context) tea.Msg {
		st, err := b.Status(ctx, nil)
		return statusMsg{seq: seq, statuses: st, err: err}
	})
}

// onStatus shows a new status, keeping the selected submodule selected.
func (m Model) onStatus(msg statusMsg) (tea.Model, tea.Cmd) {
	m.loads--
	if msg.seq != m.statusSeq {
		return m, nil
	}
	if msg.err != nil {
		// The error of the operation before the reload stays visible.
		if m.message.kind == messageError && m.message.text != "" {
			m.message.text += "; " + errorText(msg.err)
		} else {
			m.setError(msg.err)
		}
		if !m.preview.pending {
			return m, nil
		}
		// The reload dropped the preview that was due.
		cmd := m.refreshPreview()
		return m, cmd
	}
	selected, hadSelection := m.selected()
	cursor := m.table.Cursor()
	first := !m.loaded
	m.statuses, m.loaded = msg.statuses, true
	m.table.SetRows(m.rows(-1))
	m.layout()
	if hadSelection {
		for i, st := range m.statuses {
			if st.Submodule.Name == selected.Submodule.Name {
				cursor = i
			}
		}
	}
	m.selectRow(cursor)
	if first && m.message.text == "" {
		m.setMessage(messageInfo, countText(m.statuses))
	}
	m.refreshDetails()
	cmd := m.refreshPreview()
	return m, cmd
}

// countText summarizes the states of the submodules.
func countText(statuses []core.Status) string {
	attention := 0
	for _, st := range statuses {
		switch st.State {
		case core.StateOK, core.StateUnmanaged:
		default:
			attention++
		}
	}
	text := fmt.Sprintf("%d submodules", len(statuses))
	if len(statuses) == 1 {
		text = "1 submodule"
	}
	if attention > 0 {
		text += fmt.Sprintf(", %d not ok", attention)
	}
	return text
}

// start runs a modifying operation unless another one is running.
func (m Model) start(op operation) (Model, tea.Cmd) {
	if op.run == nil {
		return m, nil
	}
	if m.busy != "" {
		m.setMessage(messageInfo, "busy: "+m.busy+"; wait until it is done")
		return m, nil
	}
	m.busy = op.label
	m.setMessage(messageInfo, "")
	run, label, j := op.run, op.label, m.jobs
	work := m.call(func(ctx context.Context) tea.Msg {
		msg := run(ctx)
		// The program may end before it shows the outcome; Run reports it
		// then.
		msg.id = j.finished(outcome{label: label, text: msg.text, err: msg.err})
		return msg
	})
	spin := m.spin()
	return m, tea.Batch(work, spin)
}

// onOp reports the outcome of an operation and reloads the status.
func (m Model) onOp(msg opMsg) (tea.Model, tea.Cmd) {
	m.jobs.shown(msg.id)
	m.busy = ""
	if msg.err != nil {
		m.setError(msg.err)
		m.showLongError(msg.err)
	} else {
		m.setMessage(messageSuccess, msg.text)
	}
	return m.reload()
}

// showLongError opens an error that does not fit into the status bar in
// the viewer, unless another dialog is open. The status bar also shows the
// reload that follows an operation.
func (m *Model) showLongError(err error) {
	const shown = " ⠋ loading…  error: "
	if m.overlay.kind != overlayNone || ansi.StringWidth(shown+m.message.text) <= m.width {
		return
	}
	text := errorText(err)
	m.openViewer(viewError, "", "Error", m.errorLines(text), 0)
	m.overlay.text = text
}

// errorLines wraps an error text to the viewer.
func (m Model) errorLines(text string) string {
	width, _ := m.viewerSize()
	var lines []string
	for _, l := range cleanLines(text) {
		lines = append(lines, wrap(l, width)...)
	}
	return strings.Join(lines, "\n")
}

// askUpdate asks whether to update a submodule, and whether to commit. The
// question shows the dry run of the update; a refusal or an update without
// changes is reported at once instead.
func (m Model) askUpdate(st core.Status, commit bool) (Model, tea.Cmd) {
	name := st.Submodule.Name
	title := "Update"
	if commit {
		title = "Update and commit"
	}
	m.ask(name, title, nil, m.updateOp(name, commit))
	seq := m.nextSeq()
	m.overlay.seq, m.overlay.loading, m.overlay.commit = seq, true, commit
	m.loads++
	b := m.backend
	plan := m.call(func(ctx context.Context) tea.Msg {
		return planUpdate(ctx, b, seq, name, commit)
	})
	spin := m.spin()
	return m, tea.Batch(plan, spin)
}

// planUpdate runs the dry runs of an update.
func planUpdate(ctx context.Context, b Backend, seq int, name string, commit bool) planMsg {
	opts := core.UpdateOptions{Names: []string{name}, DryRun: true}
	msg := planMsg{seq: seq}
	if commit {
		msg.staged, msg.err = b.Update(ctx, opts)
		if msg.err != nil {
			return msg
		}
		opts.Commit = true
	}
	msg.plan, msg.err = b.Update(ctx, opts)
	return msg
}

// onPlan completes the confirmation of an update with its dry run.
func (m Model) onPlan(msg planMsg) Model {
	m.loads--
	o := m.overlay
	if o.kind != overlayConfirm || o.seq != msg.seq || !o.loading {
		return m
	}
	if msg.err != nil {
		m.closeOverlay()
		m.setError(msg.err)
		m.showLongError(msg.err)
		return m
	}
	c, ok := firstChange(msg.plan)
	if !ok {
		m.closeOverlay()
		text := clean(o.name) + ": already up to date"
		if o.commit {
			text += "; nothing to commit"
		}
		m.setMessage(messageInfo, text)
		return m
	}
	_, unstaged := firstChange(msg.staged)
	m.overlay.loading = false
	m.overlay.prompt = updatePrompt(o.name, c, o.commit, o.commit && !unstaged)
	return m
}

// firstChange returns the first change of an update that modifies
// anything, including an index that the update stages again (see
// core.Change.Changed).
func firstChange(res core.UpdateResult) (core.Change, bool) {
	i := slices.IndexFunc(res.Changes, core.Change.Changed)
	if i < 0 {
		return core.Change{}, false
	}
	return res.Changes[i], true
}

// updatePrompt returns the question of an update confirmation; staged
// reports that the working tree and the index have the update already, so
// that only the commit remains.
func updatePrompt(name string, c core.Change, commit, staged bool) []string {
	lines := []string{"Update " + clean(name) + ": " + changeText(c)}
	switch {
	case !c.RecordChanged():
		return append(lines, unrecordedText(name, c, commit), "Continue?")
	case staged:
		lines = append(lines, "The update is staged already.")
	}
	action := "Stage the result in the superproject?"
	if commit {
		action = "Commit the result in the superproject (git commit -s)?"
	}
	return append(lines, action)
}

// unrecordedText describes an update that changes nothing the superproject
// records, which is HEAD when commit is set and the index otherwise: what
// it rewrites to match that record, and that nothing is staged or
// committed.
func unrecordedText(name string, c core.Change, commit bool) string {
	record, what := "The index", "staged"
	if commit {
		record, what = "HEAD", "committed"
	}
	var done []string
	switch files := c.RestoredFiles(); len(files) {
	case 0:
	case 1:
		done = append(done, "the working tree copy of "+files[0]+" is rewritten to match it")
	default:
		done = append(done, "the working tree copies of "+strings.Join(files, " and ")+
			" are rewritten to match it")
	}
	if c.RestoresIndex() {
		done = append(done, "the change staged for "+clean(name)+" is discarded")
	}
	done = append(done, "nothing is "+what)
	if len(done) > 1 {
		done[len(done)-1] = "and " + done[len(done)-1]
	}
	return record + " records this already; " + strings.Join(done, ", ") + "."
}

// changeText describes a planned update, as "update --dry-run" does: what
// the superproject records now, the target, and what else happens to the
// submodule. "record again" means that the superproject records the same
// commit anew, "restore <files>" that the working tree copies of these
// files are rewritten to what the superproject records already, and
// "discard the staged change" that the index gets back what HEAD records
// for the submodule.
func changeText(c core.Change) string {
	from, to := oldSide(c), newSide(c)
	if c.Old != nil && c.Old.Commit == c.OldGitlink && c.New.Commit != "" &&
		c.Old.Mode != c.New.Mode {
		from, to = string(c.Old.Mode)+" "+from, string(c.New.Mode)+" "+to
	}
	text := to
	if from != to {
		text = from + " -> " + to
	}
	switch {
	case c.Clone:
		text += ", clone"
	case c.Init:
		text += ", initialize"
	case c.OldHead != "" && c.OldHead != c.OldGitlink && c.OldHead != c.New.Commit &&
		c.New.Commit != "":
		text += ", HEAD is " + shortCommit(c.OldHead)
	case from == to && c.RecordChanged():
		text += ", record again"
	}
	if files := c.RestoredFiles(); len(files) > 0 {
		text += ", restore " + strings.Join(files, " and ")
	}
	if c.RestoresIndex() {
		text += ", discard the staged change"
	}
	return text
}

// oldSide describes what the superproject records before an update: the
// locked ref and the gitlink, "<commit> (unlocked)" without a lock entry,
// or "none" without a gitlink.
func oldSide(c core.Change) string {
	switch {
	case c.OldGitlink == "":
		return "none"
	case c.Old == nil:
		return shortCommit(c.OldGitlink) + " (unlocked)"
	case c.Old.Commit == c.OldGitlink:
		return resolutionText(core.Resolution{Mode: c.Old.Mode, Ref: c.Old.Ref,
			Commit: c.Old.Commit})
	}
	return shortCommit(c.OldGitlink)
}

// newSide describes the target of an update. Without fetching, a target is
// unknown only when the repository of the submodule can be read after its
// initialization alone.
func newSide(c core.Change) string {
	if c.New.Commit == "" {
		return "unknown until initialized"
	}
	return resolutionText(c.New)
}

// restoresOnly reports whether a change that modifies anything only
// rewrites copies of .gitmodules, .lsm.lock or the gitlink to what the
// superproject records (see core.Change.RestoredFiles and
// core.Change.RestoresIndex): it records nothing new, and it neither
// initializes the submodule nor checks out another commit.
func restoresOnly(c core.Change) bool {
	return !c.Init && c.OldHead == c.New.Commit && !c.RecordChanged()
}

// updateOp updates one submodule without fetching.
func (m Model) updateOp(name string, commit bool) operation {
	b := m.backend
	return operation{label: "updating " + clean(name), run: func(ctx context.Context) opMsg {
		res, err := b.Update(ctx, core.UpdateOptions{Names: []string{name}, Commit: commit})
		if err != nil {
			return opMsg{err: err}
		}
		return opMsg{text: updateText(name, res, commit)}
	}}
}

// updateText describes the outcome of an update: what happened to the
// submodule, whether a staged change was discarded, and whether the result
// was staged or committed. Only a change of what the superproject records
// is staged or committed.
func updateText(name string, res core.UpdateResult, commit bool) string {
	c, ok := firstChange(res)
	if !ok {
		return clean(name) + ": already up to date"
	}
	target := resolutionText(c.New)
	var text string
	switch {
	case c.Clone:
		text = "cloned at " + target
	case c.Init:
		text = "initialized at " + target
	case restoresOnly(c) && len(c.RestoredFiles()) > 0:
		text = strings.Join(c.RestoredFiles(), " and ") + " restored to " + target
	case restoresOnly(c):
		text = "index restored to " + target
	case !c.RecordChanged():
		text = "checked out " + target
	case oldSide(c) == newSide(c):
		text = target + " recorded again"
	default:
		text = "updated to " + target
	}
	if c.RestoresIndex() {
		text += ", staged change discarded"
	}
	switch {
	case res.Commit != "":
		text += ", committed " + shortCommit(res.Commit)
	case commit:
		text += ", nothing to commit"
	case c.RecordChanged():
		text += ", staged"
	}
	return clean(name) + ": " + text
}

// resolutionText describes a resolved ref.
func resolutionText(r core.Resolution) string {
	if r.Mode == manifest.ModeCommit || r.Ref == "" {
		return shortCommit(r.Commit)
	}
	return clean(r.Ref) + " (" + shortCommit(r.Commit) + ")"
}

// setOp changes the tracking configuration of a submodule.
func (m Model) setOp(name string, mode manifest.Mode, ref string) operation {
	b := m.backend
	return operation{label: "changing the tracking of " + clean(name),
		run: func(ctx context.Context) opMsg {
			sub, err := b.Set(ctx, name, mode, ref)
			if err != nil {
				return opMsg{err: err}
			}
			return opMsg{text: fmt.Sprintf("%s: now tracks %s %s; press u to update",
				clean(name), sub.Mode, clean(sub.Ref))}
		}}
}

// fetchOp fetches one submodule; this is the only network access.
func (m Model) fetchOp(name string) operation {
	b := m.backend
	return operation{label: "fetching " + clean(name), run: func(ctx context.Context) opMsg {
		res, err := b.Fetch(ctx, []string{name}, nil)
		if err != nil {
			return opMsg{err: err}
		}
		text := clean(name) + ": fetched"
		if len(res) > 0 && res[0].Cloned {
			text = clean(name) + ": cloned and fetched"
		} else if len(res) > 0 && res[0].Init {
			text = clean(name) + ": initialized and fetched"
		}
		return opMsg{text: text}
	}}
}

// verify checks one submodule.
func (m Model) verify(name string) (tea.Model, tea.Cmd) {
	seq := m.nextSeq()
	m.openViewer(viewVerify, name, "Verify "+clean(name), "", seq)
	m.loads++
	b := m.backend
	check := m.call(func(ctx context.Context) tea.Msg {
		res, err := b.Verify(ctx, []string{name}, core.VerifyOptions{})
		return verifyMsg{seq: seq, name: name, results: res, err: err}
	})
	spin := m.spin()
	return m, tea.Batch(check, spin)
}

// onVerify shows the checks and a summary.
func (m Model) onVerify(msg verifyMsg) (tea.Model, tea.Cmd) {
	m.loads--
	name := clean(msg.name)
	failed := failedChecks(msg.results)
	switch {
	case msg.err != nil && !errors.Is(msg.err, core.ErrVerify):
		m.setError(msg.err)
	case len(failed) > 0:
		m.setMessage(messageError, name+": verification failed: "+strings.Join(failed, ", "))
	default:
		m.setMessage(messageSuccess, name+": verification passed")
	}
	o := m.overlay
	if o.kind != overlayViewer || o.view != viewVerify || o.seq != msg.seq {
		return m, nil
	}
	if msg.err != nil && !errors.Is(msg.err, core.ErrVerify) {
		m.closeOverlay()
		return m, nil
	}
	m.overlay.loading = false
	m.viewer.SetContent(m.checksText(msg.results))
	return m, nil
}

// failedChecks lists the names of the failed checks.
func failedChecks(results []core.VerifyResult) []string {
	var failed []string
	for _, r := range results {
		for _, c := range r.Checks {
			if !c.OK {
				failed = append(failed, c.Name)
			}
		}
	}
	return failed
}

// checksText lists the checks of a verification.
func (m Model) checksText(results []core.VerifyResult) string {
	var lines []string
	for _, r := range results {
		for _, c := range r.Checks {
			mark := m.styles.success.Render("ok  ")
			if !c.OK {
				mark = m.styles.failure.Render("FAIL")
			}
			lines = append(lines, fmt.Sprintf("%s %-12s %s", mark, c.Name, clean(c.Detail)))
		}
	}
	if len(lines) == 0 {
		return "no checks ran"
	}
	return strings.Join(lines, "\n")
}

// openDiff shows the gitlink diff of a submodule.
func (m Model) openDiff(name string) (tea.Model, tea.Cmd) {
	seq := m.nextSeq()
	m.openViewer(viewDiff, name, "Gitlink diff of "+clean(name), "", seq)
	m.loads++
	b := m.backend
	load := m.call(func(ctx context.Context) tea.Msg {
		text, err := b.GitlinkDiff(ctx, name)
		return diffMsg{seq: seq, name: name, text: text, err: err}
	})
	spin := m.spin()
	return m, tea.Batch(load, spin)
}

// onDiff shows a gitlink diff.
func (m Model) onDiff(msg diffMsg) (tea.Model, tea.Cmd) {
	m.loads--
	o := m.overlay
	if o.kind != overlayViewer || o.view != viewDiff || o.seq != msg.seq {
		return m, nil
	}
	if msg.err != nil {
		m.closeOverlay()
		m.setError(msg.err)
		return m, nil
	}
	text := strings.Join(cleanLines(msg.text), "\n")
	if strings.TrimSpace(msg.text) == "" {
		text = "The checked-out commit is the one the superproject HEAD records."
	}
	m.overlay.loading = false
	m.viewer.SetContent(text)
	return m, nil
}

// openDetails shows everything known about a submodule.
func (m Model) openDetails(st core.Status) (tea.Model, tea.Cmd) {
	m.openViewer(viewDetails, st.Submodule.Name, "Details of "+clean(st.Submodule.Name),
		m.detailsText(st), 0)
	if m.preview.name == st.Submodule.Name && (m.preview.loaded || m.preview.pending) {
		return m, nil
	}
	cmd := m.refreshPreview()
	return m, cmd
}
