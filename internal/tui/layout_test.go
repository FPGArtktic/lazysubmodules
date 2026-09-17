// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package tui

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/FPGArtktic/lazysubmodules/internal/core"
	"github.com/FPGArtktic/lazysubmodules/internal/lock"
	"github.com/FPGArtktic/lazysubmodules/internal/manifest"
)

// step applies messages to a model without running their commands.
func step(t *testing.T, m Model, msgs ...tea.Msg) Model {
	t.Helper()
	for _, msg := range msgs {
		next, _ := m.Update(msg)
		var ok bool
		if m, ok = next.(Model); !ok {
			t.Fatalf("Update returned %T", next)
		}
	}
	return m
}

// loadedModel returns a model that shows statuses.
func loadedModel(t *testing.T, statuses []core.Status, noColor bool) Model {
	t.Helper()
	m := newModel(t.Context(), newFake(), noColor)
	return step(t, m, statusMsg{seq: m.statusSeq, statuses: statuses})
}

// keyMsg returns the message of a key name or a single character.
func keyMsg(k string) tea.KeyPressMsg {
	if msg, ok := specialKey(k); ok {
		return msg
	}
	r := []rune(k)[0]
	return tea.KeyPressMsg{Code: r, Text: k}
}

// hostileStatuses returns statuses with names and refs that must not reach
// the terminal as they are.
func hostileStatuses() []core.Status {
	sts := sampleStatuses()
	sts[0].Submodule.Name = "evil\x1b]0;title\x07name-that-is-rather-long-for-a-table"
	sts[0].Submodule.Ref = "v1\x1b[2J*"
	sts[1].Reason = "reason\x1b[31m with\ttab\nand newline"
	sts[2].Submodule.Name = "日本語のサブモジュール"
	return sts
}

func TestScreenSizeAlwaysExact(t *testing.T) {
	t.Parallel()
	dialogs := [][]string{nil, {"?"}, {"u"}, {"enter"}, {"p"}, {"t"}}
	// Sizes around the limits of the layout: the smallest screen, the
	// dropped columns and the preview.
	widthList := []int{1, 19, 20, 21, 44, 79, 80, 81, 139}
	heights := []int{1, 5, 6, 7, 24, 41}
	for _, noColor := range []bool{true, false} {
		for _, keys := range dialogs {
			t.Run(fmt.Sprintf("%q/nocolor=%t", keys, noColor), func(t *testing.T) {
				t.Parallel()
				m := loadedModel(t, hostileStatuses(), noColor)
				for _, k := range keys {
					m = step(t, m, keyMsg(k))
				}
				if len(keys) > 0 && keys[0] == "t" {
					m = step(t, m, refsMsg{seq: m.overlay.seq, refs: []string{"v1", "v2\x1b[2J"}})
				}
				if len(keys) > 0 && keys[0] == "u" {
					m = step(t, m, planMsg{seq: m.overlay.seq, plan: hostilePlan()})
				}
				for _, width := range widthList {
					for _, height := range heights {
						m = step(t, m, tea.WindowSizeMsg{Width: width, Height: height})
						checkScreen(t, m.View().Content, width, height)
					}
				}
			})
		}
	}
}

// hostilePlan returns the dry run of an update of the first hostile
// submodule.
func hostilePlan() core.UpdateResult {
	st := hostileStatuses()[0]
	return core.UpdateResult{Changes: []core.Change{{Submodule: st.Submodule, Old: st.Lock,
		OldGitlink: st.Lock.Commit, OldHead: st.Head,
		New: core.Resolution{Mode: st.Submodule.Mode, Ref: "v2\x1b]0;x\x07", Commit: sha("ff")}}}}
}

func TestASCIIFrame(t *testing.T) {
	t.Parallel()
	if g := newFrameGlyphs(2); g.horizontal != "-" || g.vertical != "|" || g.teeDown != "+" {
		t.Errorf("glyphs for wide box drawing characters: %+v", g)
	}
	if g := newFrameGlyphs(1); g.horizontal != "─" || g.topLeft != "┌" {
		t.Errorf("glyphs for narrow box drawing characters: %+v", g)
	}
	m := loadedModel(t, sampleStatuses(), true)
	m.styles.frame = newFrameGlyphs(2)
	for _, keys := range [][]string{nil, {"?"}} {
		for _, k := range keys {
			m = step(t, m, keyMsg(k))
		}
		for _, size := range [][2]int{{80, 24}, {120, 30}, {40, 10}} {
			m = step(t, m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			content := m.View().Content
			checkScreen(t, content, size[0], size[1])
			if strings.ContainsAny(content, "─│┌┐└┘┬┴") {
				t.Errorf("box drawing characters in the ASCII frame:\n%s", content)
			}
			if keys == nil && !strings.HasPrefix(ansi.Strip(content), "+ Submodules -") {
				t.Errorf("frame:\n%s", ansi.Strip(content))
			}
		}
	}
}

func TestReloadDropsDuePreview(t *testing.T) {
	t.Parallel()
	for _, statusErr := range []error{nil, errFake} {
		m := loadedModel(t, sampleStatuses(), true)
		m = step(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
		m = step(t, m, previewMsg{seq: m.preview.seq, name: "kernel"}, keyMsg("j"))
		due := m.preview.seq
		m = step(t, m, keyMsg("r"))
		if _, cmd := m.Update(previewDueMsg{seq: due}); cmd != nil {
			t.Errorf("a preview due before the reload was loaded")
		}
		next, cmd := m.Update(statusMsg{seq: m.statusSeq, statuses: sampleStatuses(),
			err: statusErr})
		m = next.(Model)
		if cmd == nil || !m.preview.pending || m.preview.name != "u-boot" {
			t.Errorf("status error %v: the preview is not loaded again: %+v", statusErr,
				m.preview)
		}
	}
}

func TestConfirmWaitsForPlan(t *testing.T) {
	t.Parallel()
	m := loadedModel(t, sampleStatuses(), true)
	// The first question is canceled before its plan arrives.
	m = step(t, m, tea.WindowSizeMsg{Width: 100, Height: 30}, keyMsg("u"))
	stale := m.overlay.seq
	m = step(t, m, keyMsg("esc"), keyMsg("u"), keyMsg("y"))
	if m.busy != "" || m.overlay.kind != overlayConfirm || !m.overlay.loading {
		t.Fatalf("y before the question: busy %q, overlay %+v", m.busy, m.overlay)
	}
	if hints, _ := m.hints(); slices.ContainsFunc(hints, func(b key.Binding) bool {
		return b.Help().Desc == "confirm"
	}) {
		t.Error("the hints offer y while the question loads")
	}
	seq := m.overlay.seq
	st := sampleStatuses()[0]
	plan := core.UpdateResult{Changes: []core.Change{{Submodule: st.Submodule, Old: st.Lock,
		OldGitlink: st.Lock.Commit, OldHead: st.Head, New: *st.Target}}}
	// The plan of the earlier question is ignored.
	m = step(t, m, planMsg{seq: stale, plan: plan})
	if !m.overlay.loading || m.overlay.seq != seq || m.loads != 1 {
		t.Fatalf("after a stale plan: overlay %+v, loads %d", m.overlay, m.loads)
	}
	m = step(t, m, planMsg{seq: seq, plan: plan}, keyMsg("y"))
	if m.busy != "updating kernel" || m.overlay.kind != overlayNone || m.loads != 0 {
		t.Errorf("after y: busy %q, overlay %+v, loads %d", m.busy, m.overlay, m.loads)
	}
}

// checkScreen checks the size of a screen and that it is safe to print.
func checkScreen(t *testing.T, content string, width, height int) {
	t.Helper()
	lines := strings.Split(content, "\n")
	if len(lines) != height {
		t.Fatalf("%dx%d: %d lines", width, height, len(lines))
	}
	for i, l := range lines {
		if w := ansi.StringWidth(l); w != width {
			t.Fatalf("%dx%d: line %d is %d wide:\n%s", width, height, i, w, ansi.Strip(content))
		}
	}
	checkSafe(t, content)
}

// checkSafe fails when content has control characters other than line
// breaks and SGR sequences.
func checkSafe(t *testing.T, content string) {
	t.Helper()
	stripped := ansi.Strip(content)
	for _, c := range stripped {
		if c != '\n' && (c < 0x20 || c == 0x7f || (c >= 0x80 && c < 0xa0)) {
			t.Fatalf("control character %q in screen:\n%q", c, stripped)
		}
	}
	for _, seq := range []string{"\x1b]", "\x1b[2J", "\x07"} {
		if strings.Contains(content, seq) {
			t.Fatalf("screen contains %q", seq)
		}
	}
}

func TestFitWidths(t *testing.T) {
	t.Parallel()
	natural := widths{name: 10, mode: 11, ref: 7, lock: 7, state: 13}
	for _, tc := range []struct {
		avail int
		want  widths
	}{
		{natural.total(), natural},
		{100, natural},
		// The name and ref columns share the space in proportion.
		{natural.total() - 4, widths{name: 7, mode: 11, ref: 6, lock: 7, state: 13}},
		// Too little room for them: the lock column goes first.
		{40, widths{name: 7, mode: 11, ref: 5, lock: 0, state: 13}},
		// Then the mode column.
		{30, widths{name: 8, mode: 0, ref: 6, lock: 0, state: 13}},
		// Narrower still, the cells are cut when the rows are drawn.
		{10, widths{name: 1, mode: 0, ref: 1, lock: 0, state: 13}},
	} {
		if got := fitWidths(natural, tc.avail); got != tc.want {
			t.Errorf("fitWidths(%d) = %+v, want %+v", tc.avail, got, tc.want)
		}
	}
}

func TestCells(t *testing.T) {
	t.Parallel()
	sts := sampleStatuses()
	for i, want := range [][5]string{
		{"kernel", "tag-pattern", "v6.6.*", "a1b2c3d", "behind"},
		{"u-boot", "branch", "main", "d4e5f6a", "behind"},
		{"fpga-ip", "tag", "v2.3.1", "0718ab2", "drift"},
		{"crypto lib", "commit", "3f2a1b0", "3f2a1b0", "ok"},
		{"legacy", "-", "-", "-", "unmanaged"},
		{"theme", "tag", "v1.0.0", "5e5e000", "uninitialized"},
	} {
		if got := cells(sts[i]); got != want {
			t.Errorf("cells(%s) = %q, want %q", sts[i].Submodule.Name, got, want)
		}
	}
	hostile := cells(hostileStatuses()[0])
	if strings.ContainsFunc(hostile[0]+hostile[2], func(c rune) bool { return c < 0x20 }) {
		t.Errorf("hostile cells %q", hostile)
	}
}

func TestUpdateText(t *testing.T) {
	t.Parallel()
	tag := core.Resolution{Mode: manifest.ModeTag, Ref: "v1", Commit: sha("abcdef12")}
	commit := core.Resolution{Mode: manifest.ModeCommit, Ref: sha("1234"), Commit: sha("1234")}
	sub := manifest.Submodule{Name: "lib", Mode: manifest.ModeTag, Ref: "v1"}
	old := &lock.Entry{Name: "lib", Mode: manifest.ModeTag, Ref: "v0", Commit: sha("1")}
	same := &lock.Entry{Name: "lib", Mode: manifest.ModeTag, Ref: "v1", Commit: tag.Commit}
	branched := sub
	branched.Branch = "main"
	result := func(c core.Change, commit string) core.UpdateResult {
		return core.UpdateResult{Changes: []core.Change{c}, Commit: commit}
	}
	for _, tc := range []struct {
		res    core.UpdateResult
		commit bool
		want   string
	}{
		{core.UpdateResult{}, false, "lib: already up to date"},
		{result(core.Change{Submodule: sub, Old: same, OldGitlink: tag.Commit,
			OldHead: tag.Commit, New: tag}, ""), false, "lib: already up to date"},
		{result(core.Change{Submodule: sub, Old: old, OldGitlink: old.Commit,
			OldHead: old.Commit, New: tag}, ""), false, "lib: updated to v1 (abcdef1), staged"},
		{result(core.Change{Submodule: sub, Old: old, OldGitlink: old.Commit,
			OldHead: old.Commit, New: tag}, sha("fe")), true,
			"lib: updated to v1 (abcdef1), committed fe00000"},
		// Initializing, or checking out the recorded commit, stages nothing.
		{result(core.Change{Submodule: sub, Old: same, OldGitlink: tag.Commit, New: tag,
			Init: true}, ""), false, "lib: initialized at v1 (abcdef1)"},
		{result(core.Change{Submodule: sub, Old: same, OldGitlink: tag.Commit, New: tag,
			Init: true}, ""), true, "lib: initialized at v1 (abcdef1), nothing to commit"},
		{result(core.Change{Submodule: sub, New: tag, Init: true}, sha("fe")), true,
			"lib: initialized at v1 (abcdef1), committed fe00000"},
		{result(core.Change{Submodule: sub, Old: same, OldGitlink: tag.Commit,
			OldHead: sha("2"), New: tag}, ""), false, "lib: checked out v1 (abcdef1)"},
		{result(core.Change{Submodule: sub, Old: same, OldGitlink: tag.Commit,
			OldHead: sha("2"), New: tag}, ""), true,
			"lib: checked out v1 (abcdef1), nothing to commit"},
		{result(core.Change{Submodule: branched, Old: same, OldGitlink: tag.Commit,
			OldHead: tag.Commit, New: tag}, ""), false, "lib: v1 (abcdef1) recorded again, staged"},
		{result(core.Change{Submodule: sub, New: tag, Init: true, Clone: true}, ""), false,
			"lib: cloned at v1 (abcdef1), staged"},
		{result(core.Change{Submodule: sub, New: commit, OldHead: sha("1")}, ""), false,
			"lib: updated to 1234000, staged"},
	} {
		if got := updateText("lib", tc.res, tc.commit); got != tc.want {
			t.Errorf("updateText(%+v, %t) = %q, want %q", tc.res, tc.commit, got, tc.want)
		}
	}
}

func TestUpdatePrompt(t *testing.T) {
	t.Parallel()
	tag := core.Resolution{Mode: manifest.ModeTag, Ref: "v1", Commit: sha("abcdef12")}
	sub := manifest.Submodule{Name: "lib", Mode: manifest.ModeTag, Ref: "v1"}
	old := &lock.Entry{Name: "lib", Mode: manifest.ModeTag, Ref: "v0", Commit: sha("1")}
	same := &lock.Entry{Name: "lib", Mode: manifest.ModeTag, Ref: "v1", Commit: tag.Commit}
	pattern := &lock.Entry{Name: "lib", Mode: manifest.ModeTagPattern, Ref: "v1",
		Commit: tag.Commit}
	const stage, commit = "Stage the result in the superproject?",
		"Commit the result in the superproject (git commit -s)?"
	for _, tc := range []struct {
		change         core.Change
		commit, staged bool
		want           []string
	}{
		{core.Change{Submodule: sub, Old: old, OldGitlink: old.Commit, OldHead: old.Commit,
			New: tag}, false, false,
			[]string{"Update lib: v0 (1000000) -> v1 (abcdef1)", stage}},
		{core.Change{Submodule: sub, Old: old, OldGitlink: old.Commit, OldHead: sha("2"),
			New: tag}, false, false,
			[]string{"Update lib: v0 (1000000) -> v1 (abcdef1), HEAD is 2000000", stage}},
		{core.Change{Submodule: sub, Old: pattern, OldGitlink: tag.Commit,
			OldHead: tag.Commit, New: tag}, true, false,
			[]string{"Update lib: tag-pattern v1 (abcdef1) -> tag v1 (abcdef1)", commit}},
		{core.Change{Submodule: sub, OldGitlink: sha("3"), OldHead: sha("3"), New: tag},
			false, false, []string{"Update lib: 3000000 (unlocked) -> v1 (abcdef1)", stage}},
		{core.Change{Submodule: sub, Old: old, New: tag, Init: true}, false, false,
			[]string{"Update lib: none -> v1 (abcdef1), initialize", stage}},
		{core.Change{Submodule: sub, Old: same, OldGitlink: tag.Commit, Init: true},
			false, false,
			[]string{"Update lib: v1 (abcdef1) -> unknown until initialized, initialize", stage}},
		{core.Change{Submodule: sub, Old: same, OldGitlink: tag.Commit, New: tag, Init: true},
			true, false, []string{"Update lib: v1 (abcdef1), initialize",
				"HEAD records this already; nothing is committed.", "Continue?"}},
		{core.Change{Submodule: sub, Old: same, OldGitlink: tag.Commit, OldHead: sha("2"),
			New: tag}, false, false, []string{"Update lib: v1 (abcdef1), HEAD is 2000000",
			"The index records this already; nothing is staged.", "Continue?"}},
		{core.Change{Submodule: sub, Old: old, OldGitlink: old.Commit, OldHead: tag.Commit,
			New: tag}, true, true, []string{"Update lib: v0 (1000000) -> v1 (abcdef1)",
			"The update is staged already.", commit}},
		{core.Change{Submodule: manifest.Submodule{Name: "lib", Mode: manifest.ModeTag,
			Ref: "v1", Branch: "main"}, Old: same, OldGitlink: tag.Commit, OldHead: tag.Commit,
			New: tag}, false, false, []string{"Update lib: v1 (abcdef1), record again", stage}},
	} {
		got := updatePrompt("lib", tc.change, tc.commit, tc.staged)
		if !slices.Equal(got, tc.want) {
			t.Errorf("updatePrompt(%+v, %t, %t) =\n%q\nwant\n%q", tc.change, tc.commit,
				tc.staged, got, tc.want)
		}
	}
}

func TestStaleResultsIgnored(t *testing.T) {
	t.Parallel()
	m := loadedModel(t, sampleStatuses(), true)
	m = step(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	current := m.preview.seq
	m = step(t, m,
		previewMsg{seq: current - 1, name: "kernel", err: errFake},
		statusMsg{seq: m.statusSeq - 1, err: errFake},
		refsMsg{seq: 999, err: errFake},
		matchesMsg{seq: 999, pattern: "x"},
		diffMsg{seq: 999, name: "kernel", err: errFake},
	)
	if m.message.kind == messageError {
		t.Errorf("a stale result was shown: %+v", m.message)
	}
	if !m.preview.pending || m.preview.seq != current || m.preview.loaded {
		t.Errorf("the current preview was replaced: %+v", m.preview)
	}
	// A verification reports its result even when its dialog is gone.
	m = step(t, m, keyMsg("v"), keyMsg("esc"), keyMsg("d"))
	diffSeq := m.overlay.seq
	m = step(t, m, verifyMsg{seq: diffSeq - 1, name: "kernel", err: errFake})
	if m.message.text != "fake failure" || m.overlay.view != viewDiff || !m.overlay.loading {
		t.Errorf("after a late verification: %+v, %+v", m.message, m.overlay)
	}
}

func TestSpinnerStopsWhenIdle(t *testing.T) {
	t.Parallel()
	m := loadedModel(t, sampleStatuses(), true)
	m = step(t, m, previewMsg{seq: m.preview.seq, name: "kernel"})
	next, cmd := m.Update(m.spinner.Tick())
	if cmd != nil {
		t.Error("an idle spinner scheduled another tick")
	}
	if next.(Model).spinning {
		t.Error("the spinner still counts as turning")
	}
}
