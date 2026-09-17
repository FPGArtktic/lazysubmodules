// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package tui

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"github.com/FPGArtktic/lazysubmodules/internal/core"
	"github.com/FPGArtktic/lazysubmodules/internal/lock"
	"github.com/FPGArtktic/lazysubmodules/internal/manifest"
)

func TestRepoIsBackend(t *testing.T) {
	t.Parallel()
	// The assignment compiles only while *core.Repo implements Backend.
	var _ Backend = (*core.Repo)(nil)
}

// selectedName returns the name of the selected submodule.
func selectedName(m Model) string {
	st, ok := m.selected()
	if !ok {
		return ""
	}
	return st.Submodule.Name
}

func TestNavigation(t *testing.T) {
	t.Parallel()
	f := newFake()
	h := start(t, f)
	h.wantScreen([]string{"Submodules", "Preview", "NAME", "MODE", "REF", "LOCK", "STATE",
		"kernel", "tag-pattern", "v6.6.*", "a1b2c3d", "behind", "crypto lib", "3f2a1b0",
		"unmanaged", "uninitialized", "6 submodules, 4 not ok", "Update adds (1)",
		"Linux v6.6.9", "u update", "q quit"})
	for _, step := range []struct {
		keys []string
		want string
	}{
		{[]string{"j"}, "u-boot"},
		{[]string{"down"}, "fpga-ip"},
		{[]string{"k"}, "u-boot"},
		{[]string{"up", "up"}, "kernel"},
		{[]string{"end"}, "theme"},
		{[]string{"down"}, "theme"},
		{[]string{"home"}, "kernel"},
		{[]string{"G", "k", "k"}, "crypto lib"},
	} {
		h.press(step.keys...)
		h.waitIdle()
		var got, previewed string
		h.probe(func(m Model) { got, previewed = selectedName(m), m.preview.name })
		if got != step.want || previewed != step.want {
			t.Errorf("after %q: selected %q, preview of %q, want %q", step.keys, got,
				previewed, step.want)
		}
	}
	h.wantScreen([]string{"crypto lib ok", "Lock", "3f2a1b000000"})
	h.press("g", "j")
	h.waitScreen("board: add a new board", "Update adds (1)")
	h.finish()
	if n := f.countCalls("Preview u-boot"); n < 1 {
		t.Errorf("u-boot preview loaded %d times", n)
	}
	for _, c := range f.callLog() {
		if strings.HasPrefix(c, "Update") || strings.HasPrefix(c, "Set") ||
			strings.HasPrefix(c, "Fetch") {
			t.Errorf("navigation called %s", c)
		}
	}
}

func TestPreviewDebounce(t *testing.T) {
	t.Parallel()
	f := newFake()
	h := start(t, f)
	before := f.countCalls("Preview")
	// Keys sent together are handled before the preview is due.
	h.press("j", "j", "j", "j", "k")
	h.waitIdle()
	h.finish()
	if got := f.countCalls("Preview") - before; got != 1 {
		t.Errorf("%d previews loaded while scrolling, want 1: %q", got, f.callLog())
	}
	if got := f.countCalls("Preview crypto lib"); got != 1 {
		t.Errorf("crypto lib previewed %d times, want 1", got)
	}
}

func TestUpdateConfirmation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		key, title string
		commit     bool
		result     string
	}{
		{"u", "Update", false, "kernel: updated to v6.6.9 (e4f5a6b), staged"},
		{"U", "Update and commit", true, "kernel: updated to v6.6.9 (e4f5a6b), committed c0ffee0"},
	} {
		t.Run(tc.key, func(t *testing.T) {
			t.Parallel()
			f := newFake()
			h := start(t, f)
			h.press(tc.key)
			h.waitScreen(tc.title, "Update kernel: v6.6.8 (a1b2c3d) -> v6.6.9 (e4f5a6b)",
				"y confirm")
			for _, k := range []string{"n", "esc", "q"} {
				h.press(k)
				h.waitScreen("canceled")
				h.until("closed dialog", func(m Model) bool { return m.overlay.kind == overlayNone })
				h.press(tc.key)
				h.waitScreen("y confirm")
			}
			if n := f.countCalls("Update"); n != 0 {
				t.Fatalf("canceled update called Update %d times", n)
			}
			h.press("x", "enter")
			h.press("y")
			h.waitScreen(tc.result)
			h.waitIdle()
			h.wantScreen([]string{"kernel", "e4f5a6b", "ok"})
			h.finish()
			want := []core.UpdateOptions{{Names: []string{"kernel"}, Commit: tc.commit}}
			if len(f.updates) != 1 || !slices.Equal(f.updates[0].Names, want[0].Names) ||
				f.updates[0].Commit != tc.commit || f.updates[0].Fetch || f.updates[0].DryRun {
				t.Errorf("updates = %+v, want %+v", f.updates, want)
			}
		})
	}
}

func TestBusyGuard(t *testing.T) {
	t.Parallel()
	f := newFake()
	f.gate, f.started = make(chan struct{}), make(chan string, 4)
	h := start(t, f)
	h.press("u")
	h.confirmWhenReady()
	if got := <-f.started; got != "update" {
		t.Fatalf("started %s", got)
	}
	h.waitScreen("updating kernel…")
	for _, k := range []string{"u", "U", "f", "b", "t", "p"} {
		h.press(k)
		h.waitScreen("busy: updating kernel; wait until it is done")
		h.probe(func(m Model) {
			if m.overlay.kind != overlayNone {
				t.Errorf("%s opened a dialog while busy", k)
			}
		})
	}
	// Reading keeps working while the operation runs.
	h.press("j", "d")
	h.waitScreen("Gitlink diff of u-boot", "The checked-out commit is the one")
	h.press("esc")
	close(f.gate)
	h.waitScreen("kernel: updated to v6.6.9")
	h.waitIdle()
	h.press("f")
	h.waitScreen("u-boot: fetched")
	h.finish()
	if got := f.countCalls("Update"); got != 1 {
		t.Errorf("Update called %d times", got)
	}
	if len(f.fetches) != 1 || !slices.Equal(f.fetches[0], []string{"u-boot"}) {
		t.Errorf("fetches = %q", f.fetches)
	}
}

func TestErrorsInStatusBar(t *testing.T) {
	t.Parallel()
	f := newFake()
	f.updateErr = fmt.Errorf("kernel: %w\nsecond line", core.ErrDirty)
	f.fetchErr = fmt.Errorf("git fetch: exit status 128: \x1b[31mdenied")
	h := start(t, f)
	h.press("u")
	h.confirmWhenReady()
	h.waitScreen("error: kernel: refused: submodule has uncommitted changes; second line")
	h.press("f")
	h.waitScreen("error: git fetch: exit status 128: �[31mdenied")
	f.set(func(f *fakeBackend) {
		f.statusErr = errFake
		f.fetchErr = fmt.Errorf("theme: %w", core.ErrUninitialized)
	})
	h.press("r")
	h.waitScreen("error: fake failure")
	// The table keeps the last status and the program keeps running.
	h.press("G", "f")
	h.waitScreen("theme: refused: submodule is not initialized (press f to fetch)")
	// The reload after the fetch fails too; its error must arrive before the
	// next message, or it would replace that message.
	h.waitIdle()
	h.press("k", "u")
	h.waitScreen("legacy is not managed; press b, t or p to track it")
	h.wantScreen([]string{"kernel", "legacy"})
	m := h.finish()
	if m.message.kind != messageInfo || !m.loaded {
		t.Errorf("final message %+v, loaded %t", m.message, m.loaded)
	}
}

func TestInitialLoadError(t *testing.T) {
	t.Parallel()
	f := newFake()
	f.statusErr = fmt.Errorf("read .gitmodules: %w", errFake)
	h := start(t, f)
	h.waitScreen("error: read .gitmodules: fake failure", "could not read the submodules")
	h.press("u", "enter", "d")
	h.waitScreen("no submodule selected")
	f.set(func(f *fakeBackend) { f.statusErr = nil })
	h.press("r")
	h.waitScreen("kernel", "behind")
	h.finish()
}

func TestHelpOverlay(t *testing.T) {
	t.Parallel()
	h := start(t, newFake())
	h.press("?")
	h.waitScreen("Keys", "Navigation", "Tracking", "update+commit", "diff", "quit now",
		"Only f (fetch) uses the network.")
	h.press("q")
	h.until("help closed", func(m Model) bool { return m.overlay.kind == overlayNone })
	h.press("?")
	h.until("help open", func(m Model) bool { return m.overlay.view == viewHelp })
	h.press("?")
	h.until("help closed", func(m Model) bool { return m.overlay.kind == overlayNone })
	h.press("q")
	h.final()
}

func TestQuitClosesOverlaysFirst(t *testing.T) {
	t.Parallel()
	f := newFake()
	h := start(t, f)
	for _, open := range [][]string{{"enter"}, {"?"}, {"d"}, {"v"}, {"b"}, {"t"}, {"u"}} {
		h.press(open...)
		h.until(fmt.Sprintf("dialog of %q", open), func(m Model) bool {
			return m.overlay.kind != overlayNone
		})
		h.waitIdle()
		h.press("q")
		h.until(fmt.Sprintf("dialog of %q closed", open), func(m Model) bool {
			return m.overlay.kind == overlayNone
		})
	}
	// In the pattern dialog, q is text; esc closes it.
	h.press("p", "q")
	h.waitDialog("pattern: v6.6.*q")
	h.press("esc")
	h.until("pattern closed", func(m Model) bool { return m.overlay.kind == overlayNone })
	h.press("q")
	h.final()
	if got := f.countCalls("Update") + f.countCalls("Set"); got != 0 {
		t.Errorf("%d modifying calls", got)
	}
}

func TestCtrlCQuitsFromDialogs(t *testing.T) {
	t.Parallel()
	for _, open := range []string{"enter", "b", "p", "u"} {
		t.Run(open, func(t *testing.T) {
			t.Parallel()
			h := start(t, newFake())
			h.press(open)
			h.until("dialog", func(m Model) bool { return m.overlay.kind != overlayNone })
			m := h.finish()
			if m.overlay.kind == overlayNone {
				t.Error("ctrl+c closed the dialog instead of quitting")
			}
		})
	}
}

func TestQuitWhileBusy(t *testing.T) {
	t.Parallel()
	f := newFake()
	f.gate, f.started = make(chan struct{}), make(chan string, 1)
	defer close(f.gate)
	h := start(t, f)
	h.press("u")
	h.confirmWhenReady()
	<-f.started
	h.press("q")
	h.waitScreen("An operation is running: updating kernel.")
	h.press("n")
	h.until("dialog closed", func(m Model) bool { return m.overlay.kind == overlayNone })
	h.probe(func(m Model) {
		if m.busy == "" {
			t.Error("declining to quit ended the operation")
		}
	})
	h.press("q", "y")
	m := h.final()
	if m.busy == "" {
		t.Error("the final model is not busy")
	}
}

func TestBranchPicker(t *testing.T) {
	t.Parallel()
	f := newFake()
	h := start(t, f)
	h.press("j", "b")
	h.waitScreen("Track u-boot by branch", "3 of 3; / filters", "> main (current)", "stable")
	h.press("j", "j", "j", "k")
	h.waitScreen("> next")
	h.press("enter")
	h.waitScreen("Change tracking", "Track u-boot by branch next?",
		"Only .gitmodules changes; press u afterwards to update.")
	h.press("y")
	h.waitScreen("u-boot: now tracks branch next; press u to update")
	h.waitIdle()
	h.finish()
	if !slices.Equal(f.sets, []string{"u-boot branch next"}) {
		t.Errorf("sets = %q", f.sets)
	}
	if n := f.countCalls("Update"); n != 0 {
		t.Errorf("changing the tracking updated %d times", n)
	}
}

func TestTagPickerFilter(t *testing.T) {
	t.Parallel()
	f := newFake()
	h := start(t, f)
	h.press("t")
	h.waitDialog("Track kernel by tag", "5 of 5", "> v6.7-rc1")
	h.press("/", "6.6.1")
	box := h.waitDialog("filter: 6.6.1", "> v6.6.10", "v6.6.1")
	// Letters are text in the filter, so only the arrows move.
	h.wantScreen([]string{"↑/↓ move", "enter choose", "esc clear filter"}, "↑/k", "↓/j")
	if strings.Contains(box, "v6.6.9") || strings.Contains(box, "v6.7-rc1") {
		t.Errorf("filtered picker:\n%s", box)
	}
	h.press("down", "enter")
	h.waitScreen("Track kernel by tag v6.6.1?")
	h.press("n")
	h.until("closed", func(m Model) bool { return m.overlay.kind == overlayNone })
	// esc clears the filter, and a filter without matches chooses nothing.
	h.press("t")
	h.waitDialog("5 of 5")
	h.press("/", "zzz")
	h.waitDialog("no match")
	h.press("enter", "esc")
	h.waitDialog("5 of 5")
	h.press("G")
	h.waitDialog("> v6.6.1")
	h.press("enter", "y")
	h.waitScreen("kernel: now tracks tag v6.6.1; press u to update")
	h.waitIdle()
	h.finish()
	if !slices.Equal(f.sets, []string{"kernel tag v6.6.1"}) {
		t.Errorf("sets = %q", f.sets)
	}
	if n := f.countCalls(`Tags kernel ""`); n != 2 {
		t.Errorf("tag list loaded %d times, want 2", n)
	}
}

func TestPickerWithoutRefs(t *testing.T) {
	t.Parallel()
	h := start(t, newFake())
	h.selectName("fpga-ip")
	h.press("b")
	h.waitScreen("fpga-ip has no remote branches; press f to fetch")
	h.probe(func(m Model) {
		if m.overlay.kind != overlayNone {
			t.Error("the picker stayed open")
		}
	})
	h.press("t")
	h.waitScreen("fpga-ip has no tags; press f to fetch")
	h.finish()
}

// backspaces returns n backspace key names.
func backspaces(n int) []string {
	return slices.Repeat([]string{"backspace"}, n)
}

func TestPatternDialog(t *testing.T) {
	t.Parallel()
	f := newFake()
	h := start(t, f)
	h.press("p")
	h.waitScreen("Track kernel by tag pattern", "pattern: v6.6.*", "4 local tags match",
		"v6.6.10", "v6.6.1", "Pre-release tags are skipped")
	h.press(backspaces(6)...)
	h.waitScreen("Type a glob such as v1.* or v6.6.*.")
	h.press("enter")
	h.waitScreen("type a tag pattern such as v1.*")
	h.press("v6.6.1*")
	h.waitScreen("pattern: v6.6.1*", "2 local tags match")
	h.press(backspaces(7)...)
	h.press("v 1")
	h.waitScreen("invalid pattern", `invalid tag pattern "v 1"`)
	h.press("enter")
	h.waitScreen(`error: kernel: invalid tag pattern "v 1": invalid argument`)
	h.probe(func(m Model) {
		if m.overlay.kind != overlayPattern {
			t.Errorf("an invalid pattern left the dialog: %v", m.overlay.kind)
		}
	})
	h.press(backspaces(3)...)
	h.press("v6.*")
	h.waitScreen("5 local tags match (1 pre-release)", "v6.7-rc1 pre-release")
	h.press("enter")
	h.waitScreen("Track kernel by tag pattern v6.*? It matches 5 local tags.")
	h.press("y")
	h.waitScreen("kernel: now tracks tag-pattern v6.*; press u to update")
	h.waitIdle()
	h.finish()
	if !slices.Equal(f.sets, []string{"kernel tag-pattern v6.*"}) {
		t.Errorf("sets = %q", f.sets)
	}
	for _, want := range []string{`Tags kernel "v6.6.*"`, `Tags kernel "v6.6.1*"`,
		`Tags kernel "v 1"`, `Tags kernel "v6.*"`} {
		if f.countCalls(want) != 1 {
			t.Errorf("%s called %d times, want once: %q", want, f.countCalls(want), f.callLog())
		}
	}
}

func TestPatternWithoutConfiguredPattern(t *testing.T) {
	t.Parallel()
	f := newFake()
	h := start(t, f)
	h.selectName("u-boot")
	h.press("p")
	h.waitScreen("Track u-boot by tag pattern", "pattern: v1.*")
	h.press("v2026*", "enter", "y")
	h.waitScreen("u-boot: now tracks tag-pattern v2026*")
	h.waitIdle()
	h.finish()
	if f.countCalls(`Tags u-boot ""`) != 0 {
		t.Errorf("an empty pattern was checked: %q", f.callLog())
	}
}

func TestSetError(t *testing.T) {
	t.Parallel()
	f := newFake()
	f.setErr = fmt.Errorf("u-boot: invalid branch %q: %w", "x", core.ErrInvalidArgument)
	h := start(t, f)
	h.press("j", "b")
	h.waitDialog("> main")
	h.press("enter", "y")
	h.waitScreen(`error: u-boot: invalid branch "x": invalid argument`)
	h.finish()
}

func TestWindowResize(t *testing.T) {
	t.Parallel()
	h := start(t, newFake())
	h.wantScreen([]string{"Preview", "LOCK", "MODE"})
	for _, size := range []struct {
		width, height int
		shows, hides  []string
	}{
		{79, 24, []string{"Submodules", "LOCK", "tag-pattern"}, []string{"Preview"}},
		{80, 24, []string{"Preview", "LOCK"}, nil},
		{44, 12, []string{"STATE", "MODE"}, []string{"LOCK", "Preview"}},
		{30, 10, []string{"STATE"}, []string{"MODE", "LOCK"}},
		{19, 10, []string{"terminal too small"}, []string{"Submodules"}},
		{120, 30, []string{"Preview", "LOCK", "Update adds (1)", "Linux v6.6.8"}, nil},
	} {
		h.resize(size.width, size.height)
		var lines []string
		h.probe(func(m Model) { lines = strings.Split(m.View().Content, "\n") })
		checkScreenSize(t, lines, size.width, size.height)
		h.wantScreen(size.shows, size.hides...)
	}
	h.finish()
}

func TestSmallScreenKeepsSelectionVisible(t *testing.T) {
	t.Parallel()
	h := start(t, newFake())
	h.press("G")
	h.waitIdle()
	h.resize(60, 7)
	h.waitScreen("theme")
	h.resize(60, 30)
	h.press("g")
	h.waitIdle()
	h.resize(60, 7)
	h.waitScreen("kernel")
	h.finish()
}

func TestDetailsVerifyDiff(t *testing.T) {
	t.Parallel()
	f := newFake()
	h := start(t, f)
	h.press("enter")
	h.waitScreen("Details of kernel", "https://git.example.org/kernel",
		"Tracking   tag-pattern v6.6.*", "Branch key none",
		"HEAD       a1b2c3d4e5f60000000000000000000000000000", "HEAD is the locked commit",
		"Linux v6.6.7", "v6.6.6")
	h.press("esc")
	h.press("v")
	h.waitScreen("Verify kernel", "ok   lock-entry", "kernel: verification passed")
	h.press("q")
	h.selectName("fpga-ip")
	h.press("v")
	h.waitScreen("FAIL tag", "tag v2.3.1 now points to 5c6d7e8f9a0b",
		"error: fpga-ip: verification failed: tag")
	// The preview of kernel is due after g; it must be loaded before the
	// reload below, whose calls the test checks.
	h.press("q", "g")
	h.waitIdle()
	h.press("d")
	h.waitScreen("Gitlink diff of kernel", "Submodule kernel a1b2c3d..e4f5a6b:", "> Linux v6.6.9")
	h.press("q")
	h.until("closed", func(m Model) bool { return m.overlay.kind == overlayNone })
	f.set(func(f *fakeBackend) { f.previewErr = errFake })
	h.press("r")
	h.waitScreen("error: fake failure")
	h.finish()
	if !slices.Equal(f.callLog()[len(f.callLog())-2:], []string{`Status []`, "Preview kernel"}) {
		t.Errorf("calls end with %q", f.callLog()[len(f.callLog())-2:])
	}
}

func TestLongDialogsScroll(t *testing.T) {
	t.Parallel()
	f := newFake()
	var diff strings.Builder
	diff.WriteString("Submodule kernel a..b:\n")
	for i := range 100 {
		fmt.Fprintf(&diff, "  > commit %03d\n", i)
	}
	f.diffs["kernel"] = diff.String()
	h := start(t, f)
	h.press("d")
	h.waitScreen("commit 000")
	h.wantScreen(nil, "commit 099")
	h.press("end")
	h.waitScreen("commit 099")
	h.press("home", "pgdown", "j")
	h.waitScreen("commit 0")
	h.press("q")
	names := make([]string, 0, 60)
	for i := range 60 {
		names = append(names, fmt.Sprintf("v1.%d.0", i))
	}
	f.set(func(f *fakeBackend) { f.tags["kernel"] = names })
	h.press("t")
	h.waitDialog("60 of 60")
	h.press("G")
	h.waitDialog("> v1.59.0")
	h.press("pgup")
	box := h.waitDialog("> v1.")
	if strings.Contains(box, "> v1.59.0") || strings.Contains(box, "> v1.0.0") {
		t.Errorf("page up did not move by a page:\n%s", box)
	}
	h.press("home")
	h.waitDialog("> v1.0.0")
	h.finish()
}

// checkScreenSize checks that a screen has the terminal size exactly.
func checkScreenSize(t *testing.T, lines []string, width, height int) {
	t.Helper()
	if len(lines) != height {
		t.Errorf("%dx%d: %d lines", width, height, len(lines))
	}
	for i, l := range lines {
		if w := ansi.StringWidth(l); w != width {
			t.Errorf("%dx%d: line %d is %d wide: %q", width, height, i, w, l)
		}
	}
}

func TestManagedGuardAndModes(t *testing.T) {
	t.Parallel()
	f := newFake()
	h := start(t, f)
	h.selectName("legacy")
	for _, k := range []string{"u", "U", "f", "v"} {
		h.press(k)
		h.waitScreen("legacy is not managed; press b, t or p to track it")
		h.press("r")
		h.waitIdle()
	}
	h.press("d")
	h.waitScreen("Gitlink diff of legacy")
	h.press("q", "b")
	h.waitScreen("legacy has no remote branches")
	h.finish()
	for _, c := range f.callLog() {
		if strings.Contains(c, "legacy") && !strings.HasPrefix(c, "Preview") &&
			!strings.HasPrefix(c, "GitlinkDiff") && !strings.HasPrefix(c, "Branches") {
			t.Errorf("unmanaged submodule got %s", c)
		}
	}
}

func TestUpdatePlan(t *testing.T) {
	t.Parallel()
	f := newFake()
	f.planErr = fmt.Errorf("kernel: %w: tag v9 does not exist", core.ErrMissingRef)
	h := start(t, f)
	// A refusal of the dry run is shown without a question; it is too long
	// for the status bar.
	h.press("u")
	h.waitDialog("Error", "kernel: refused: ref not found in local refs: tag v9 does not exist")
	h.probe(func(m Model) {
		want := "kernel: refused: ref not found in local refs: tag v9 does not exist " +
			"(press f to fetch, or change the ref)"
		if m.message.text != want || m.overlay.view != viewError {
			t.Errorf("a refused update: message %q, overlay %+v", m.message.text, m.overlay)
		}
	})
	h.press("esc")
	f.set(func(f *fakeBackend) { f.planErr = nil })
	// Nothing to update: no question either.
	h.selectName("crypto lib")
	h.press("u")
	h.waitScreen("crypto lib: already up to date")
	h.press("U")
	h.waitScreen("crypto lib: already up to date; nothing to commit")
	// The update is staged, only the commit remains.
	f.set(func(f *fakeBackend) {
		f.committed = map[string]lock.Entry{"crypto lib": {Name: "crypto lib",
			Mode: manifest.ModeCommit, Ref: sha("9e9e"), Commit: sha("9e9e")}}
	})
	h.press("U")
	h.waitDialog("Update crypto lib: 9e9e000 -> 3f2a1b0", "The update is staged already.",
		"Commit the result in the superproject (git commit -s)?")
	h.confirmWhenReady()
	h.waitScreen("crypto lib: updated to 3f2a1b0, committed c0ffee0")
	h.waitIdle()
	// Initializing records nothing new, so nothing is staged.
	h.selectName("theme")
	h.press("u")
	h.waitDialog("Update theme: v1.0.0 (5e5e000), initialize",
		"The index records this already; nothing is staged.", "Continue?")
	h.confirmWhenReady()
	h.waitScreen("theme: initialized at v1.0.0 (5e5e000)")
	h.wantScreen(nil, "staged")
	h.waitIdle()
	h.finish()
	if got := f.countCalls("Update"); got != 2 {
		t.Errorf("%d updates: %q", got, f.callLog())
	}
	for _, want := range []string{`Plan ["kernel"] commit=false fetch=false`,
		`Plan ["crypto lib"] commit=true fetch=false`,
		`Plan ["theme"] commit=false fetch=false`} {
		if !slices.Contains(f.callLog(), want) {
			t.Errorf("no %s in %q", want, f.callLog())
		}
	}
}

func TestPatternWithoutTags(t *testing.T) {
	t.Parallel()
	f := newFake()
	f.tagsErr = fmt.Errorf("theme: %w", core.ErrUninitialized)
	h := start(t, f)
	h.selectName("theme")
	h.press("p", "v1.*")
	h.waitDialog("pattern: v1.*", "tags unavailable",
		"theme: refused: submodule is not initialized (press f to")
	// Enter still asks: the tracking can change without tags, as with set.
	h.press("enter")
	h.waitDialog("Track theme by tag pattern v1.*? Its local tags could not be", "listed.")
	h.confirmWhenReady()
	h.waitScreen("theme: now tracks tag-pattern v1.*; press u to update")
	h.waitIdle()
	h.finish()
	if !slices.Equal(f.sets, []string{"theme tag-pattern v1.*"}) {
		t.Errorf("sets = %q", f.sets)
	}
}

func TestViewerFollowsResize(t *testing.T) {
	t.Parallel()
	f := newFake()
	f.updateErr = fmt.Errorf("kernel: %w: %s", core.ErrUnrelatedStaged,
		strings.Repeat("a-rather-long-path-name ", 8))
	h := start(t, f)
	h.press("u")
	h.confirmWhenReady()
	h.waitDialog("Error", "a-rather-long-path-name")
	h.resize(60, 20)
	h.probe(func(m Model) { checkViewerText(t, m) })
	h.press("esc", "?")
	h.waitDialog("Keys", "update+commit")
	h.resize(100, 30)
	h.probe(func(m Model) { checkViewerText(t, m) })
	h.resize(60, 20)
	h.probe(func(m Model) { checkViewerText(t, m) })
	// The key names stay whole at any width.
	h.waitDialog("update+commit")
	h.finish()
}

// checkViewerText checks that the text of the viewer was laid out for its
// current width: no line is wider than the viewer.
func checkViewerText(t *testing.T, m Model) {
	t.Helper()
	width, _ := m.viewerSize()
	for l := range strings.SplitSeq(m.viewer.GetContent(), "\n") {
		if w := ansi.StringWidth(l); w > width {
			t.Errorf("viewer line %q is %d wide, the viewer %d", l, w, width)
		}
	}
}

func TestDialogEdges(t *testing.T) {
	t.Parallel()
	f := newFake()
	f.previews["kernel"] = core.Preview{Log: []string{strings.Repeat("x", 200)}}
	h := startWith(t, f, harnessOptions{width: 80, height: 24, noColor: true,
		profile: colorprofile.ASCII})
	h.waitScreen("xxxx…")
	for _, keys := range [][]string{{"enter"}, {"?"}, {"U"}} {
		h.press(keys...)
		h.until("dialog", func(m Model) bool {
			return m.overlay.kind != overlayNone && !m.overlay.loading
		})
		h.probe(func(m Model) {
			box := m.overlayBox()
			lines := strings.Split(ansi.Strip(m.View().Content), "\n")
			boxWidth := ansi.StringWidth(box[0])
			x := (80 - boxWidth) / 2
			y := (m.geometry().body - len(box)) / 2
			for i := range box {
				if y+i == 0 || y+i == m.geometry().body-1 {
					continue // the top and bottom border of the frame
				}
				row := []rune(lines[y+i])
				if row[x-1] != ' ' || row[x+boxWidth] != ' ' {
					t.Errorf("%q: row %d touches the dialog: %q", keys, y+i, string(row))
				}
			}
		})
		h.press("esc")
		h.until("closed", func(m Model) bool { return m.overlay.kind == overlayNone })
	}
	h.finish()
}
