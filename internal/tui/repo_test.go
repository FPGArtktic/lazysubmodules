// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package tui

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"

	"github.com/FPGArtktic/lazysubmodules/internal/core"
	"github.com/FPGArtktic/lazysubmodules/internal/git/gittest"
)

// states returns "name state" for every row of the table.
func (h *harness) states() []string {
	h.t.Helper()
	var rows []string
	h.probe(func(m Model) {
		for _, st := range m.statuses {
			rows = append(rows, st.Submodule.Name+" "+string(st.State))
		}
	})
	return rows
}

// wantState checks the state of one submodule.
func (h *harness) wantState(name string, want core.State) {
	h.t.Helper()
	row := name + " " + string(want)
	if rows := h.states(); !slices.Contains(rows, row) {
		h.t.Errorf("no row %q in %q", row, rows)
	}
}

// startRepo runs the interface on a real superproject.
func startRepo(t *testing.T, c *gittest.ComplexSuper) *harness {
	t.Helper()
	repo, err := core.Open(t.Context(), c.Runner(t), c.Dir)
	if err != nil {
		t.Fatal(err)
	}
	return startWith(t, repo, harnessOptions{width: 120, height: 40, noColor: true,
		profile: colorprofile.ASCII})
}

// confirm answers the confirmation dialog once it shows prompt.
func (h *harness) confirm(prompt string) {
	h.t.Helper()
	h.waitDialog(prompt, "y confirm")
	h.press("y")
}

func TestComplexSuperproject(t *testing.T) {
	t.Parallel()
	c := gittest.NewComplexSuper(t, gittest.SHA1)
	h := startRepo(t, c)
	want := []string{"kernel behind", "u-boot behind", "fpga.core ok", "crypto lib ok",
		"legacy unmanaged", "tools ok", "theme uninitialized", "fresh uninitialized",
		"app dirty", "sdk ok", "mirror-lib ok", "broken missing-ref", "signed ok", "quirky ok"}
	if got := h.states(); !slices.Equal(got, want) {
		t.Fatalf("rows:\n%q\nwant\n%q", got, want)
	}
	short := func(commit string) string { return commit[:7] }
	h.wantScreen([]string{"14 submodules, 6 not ok", "crypto lib", "commit      " +
		short(c.CryptoLib.Ref), "Update adds (1)", "Linux v6.6.10",
		"Lock   v6.6.9 " + c.Kernel.Lock.Commit[:12], "Tags", "v6.7-rc1"}, "libs/crypto lib")

	t.Run("pickers", func(t *testing.T) {
		h := h.with(t)
		h.press("t")
		h.waitDialog("Track kernel by tag", "12 of 12", "> v6.7-rc1")
		h.press("esc", "p")
		h.waitDialog("pattern: v6.6.*", "11 local tags match (1 pre-release)",
			"v6.6.11-rc1 pre-release")
		h.press("esc")
		h.selectName("u-boot")
		h.press("b")
		h.waitDialog("2 of 2", "> main (current)", "next")
		h.press("esc")
	})

	t.Run("update and commit", func(t *testing.T) {
		h := h.with(t)
		h.selectName("u-boot")
		h.press("U")
		target := short(c.UBoot.Branches["main"])
		h.confirm("Update u-boot: main (" + short(c.UBoot.Lock.Commit) + ") -> main (" +
			target + ")")
		h.waitScreen("u-boot: updated to main (" + target + "), committed ")
		h.waitIdle()
		h.wantState("u-boot", core.StateOK)
		if got := c.Git(t, c.Dir, "log", "-1", "--format=%s"); got !=
			"manifest: update u-boot to main" {
			t.Errorf("commit subject %q", got)
		}
	})

	t.Run("update", func(t *testing.T) {
		h := h.with(t)
		h.selectName("kernel")
		h.press("u")
		tag := short(c.Kernel.Tags["v6.6.10"])
		h.confirm("Update kernel: v6.6.9 (" + short(c.Kernel.Lock.Commit) + ") -> v6.6.10 (" +
			tag + ")")
		h.waitScreen("kernel: updated to v6.6.10 (" + tag + "), staged")
		h.waitIdle()
		h.wantState("kernel", core.StateOK)
		if got := c.Git(t, c.Dir, "diff", "--cached", "--name-only"); got != ".lsm.lock\nkernel" {
			t.Errorf("staged %q", got)
		}
		h.press("d")
		h.waitDialog("Gitlink diff of kernel", "Submodule kernel ", "> Linux v6.6.10")
		h.press("q", "v")
		h.waitDialog("Verify kernel", "FAIL gitlink", "ok   head")
		h.waitScreen("error: kernel: verification failed: gitlink")
		h.press("q")
	})

	t.Run("change tracking", func(t *testing.T) {
		h := h.with(t)
		h.selectName("u-boot")
		h.press("b")
		h.waitDialog("> main (current)")
		h.press("j", "enter")
		h.confirm("Track u-boot by branch next?")
		h.waitScreen("u-boot: now tracks branch next; press u to update")
		h.waitIdle()
		for key, want := range map[string]string{"lsm-ref": "next", "branch": "next"} {
			got := c.Git(t, c.Dir, "config", "-f", ".gitmodules", "submodule.u-boot."+key)
			if got != want {
				t.Errorf("%s = %q, want %q", key, got, want)
			}
		}
		h.wantState("u-boot", core.StateBehind)
		// The staged kernel update cannot be committed alone any more; the
		// dry run refuses before the question. The message is too long for
		// the status bar, so it opens in a dialog as well.
		h.selectName("kernel")
		h.press("U")
		h.waitDialog("Error", "refused: the commit would include unrelated changes: ",
			".gitmodules (unstaged changes outside the selected")
		h.waitScreen("error: refused: the commit would include unrelated changes")
		h.press("q")
		h.until("no dialog", func(m Model) bool { return m.overlay.kind == overlayNone })
		if got := c.Git(t, c.Dir, "log", "-1", "--format=%s"); got !=
			"manifest: update u-boot to main" {
			t.Errorf("a refused update committed %q", got)
		}
	})

	t.Run("refusals", func(t *testing.T) {
		h := h.with(t)
		// The dry run refuses before the question.
		for name, text := range map[string]string{
			"app":    "error: app: refused: submodule has uncommitted changes",
			"broken": "error: broken: refused: ref not found in local refs: tag v9.9.9",
			"legacy": "legacy is not managed; press b, t or p to track it",
			"fresh":  "error: fresh: refused: submodule is not initialized (press f to fetch)",
		} {
			h.selectName(name)
			h.press("u")
			h.waitScreen(text)
			h.waitIdle()
			h.probe(func(m Model) {
				if m.overlay.kind == overlayConfirm {
					t.Errorf("%s: a refused update asks", name)
				}
			})
			// A message too long for the status bar opens in a dialog.
			h.press("esc")
			h.until("no dialog", func(m Model) bool { return m.overlay.kind == overlayNone })
		}
		h.selectName("crypto lib")
		h.press("u")
		h.waitScreen("crypto lib: already up to date")
	})

	t.Run("initialize and fetch", func(t *testing.T) {
		h := h.with(t)
		h.selectName("theme")
		h.press("u")
		theme := "v1.0.0 (" + short(c.Theme.Lock.Commit) + ")"
		h.confirm("Update theme: " + theme + ", initialize")
		h.waitScreen("theme: initialized at " + theme)
		h.waitIdle()
		h.wantScreen(nil, "staged")
		h.wantState("theme", core.StateOK)
		if got := c.Git(t, c.Dir, "diff", "--cached", "--name-only", "--", "docs/theme"); got != "" {
			t.Errorf("staged %q", got)
		}
		h.selectName("fresh")
		h.press("f")
		h.waitScreen("fresh: cloned and fetched")
		h.waitIdle()
		h.wantState("fresh", core.StateOK)
	})
	h.finish()
}

func TestHostileSuperproject(t *testing.T) {
	t.Parallel()
	s := gittest.NewHostileSuper(t, gittest.SHA1)
	repo, err := core.Open(t.Context(), gittest.Runner(t), s.Dir)
	if err != nil {
		t.Fatal(err)
	}
	h := startWith(t, repo, harnessOptions{width: 100, height: 30, noColor: false,
		profile: colorprofile.TrueColor})
	// The lock file has entries that no command accepts.
	h.waitScreen(`error: .lsm.lock: invalid lock entry "dash-ref"`,
		"could not read the submodules")
	h.probe(func(m Model) { checkSafe(t, m.View().Content) })
	for _, name := range []string{"dash-ref", "ctrl-ref"} {
		gittest.Git(t, s.Dir, "config", "-f", gittest.LockFile, "--remove-section",
			"submodule."+name)
	}
	h.press("r")
	h.waitIdle()
	subs, err := repo.Submodules(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var rows int
	h.probe(func(m Model) { rows = len(m.statuses) })
	if rows != len(subs) || rows < 10 {
		t.Fatalf("%d rows, want %d; screen:\n%s", rows, len(subs), h.screen())
	}
	for range rows {
		h.probe(func(m Model) { checkSafe(t, m.View().Content) })
		h.press("enter")
		h.waitIdle()
		h.probe(func(m Model) { checkSafe(t, m.View().Content) })
		h.press("esc", "d")
		h.waitIdle()
		h.probe(func(m Model) { checkSafe(t, m.View().Content) })
		h.press("esc", "down")
		h.waitIdle()
	}
	m := h.finish()
	if strings.Contains(m.message.text, "\x1b") {
		t.Errorf("message %q", m.message.text)
	}
}
