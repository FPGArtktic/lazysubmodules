// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package tui

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"

	"github.com/FPGArtktic/lazysubmodules/internal/core"
	"github.com/FPGArtktic/lazysubmodules/internal/git/gittest"
	"github.com/FPGArtktic/lazysubmodules/internal/lock"
	"github.com/FPGArtktic/lazysubmodules/internal/manifest"
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

// recordedSuper returns a superproject whose HEAD and index record the
// submodule lib tracking gittest.TagV101: its gitlink, its lock entry and its
// tracking keys. lib is checked out at that tag. It also returns the commits
// of the upstream tags.
func recordedSuper(t *testing.T) (string, map[string]string) {
	t.Helper()
	up, commits := gittest.NewTaggedUpstream(t, gittest.SHA1)
	s := gittest.NewSuper(t, gittest.SHA1)
	s.AddSubmodule(t, "lib", up)
	gittest.Git(t, filepath.Join(s.Dir, "lib"), "checkout", "--quiet", "--detach",
		commits[gittest.TagV101])
	s.SetKey(t, "lib", manifest.KeyMode, string(manifest.ModeTag))
	s.SetKey(t, "lib", manifest.KeyRef, gittest.TagV101)
	writeLock(t, s.Dir, lock.Entry{Name: "lib", Mode: manifest.ModeTag, Ref: gittest.TagV101,
		Commit: commits[gittest.TagV101]})
	s.Commit(t, "track lib")
	return s.Dir, commits
}

// writeLock writes a lock entry to the working tree of a superproject.
func writeLock(t *testing.T, dir string, e lock.Entry) {
	t.Helper()
	if err := lock.Write(t.Context(), gittest.Runner(t), dir, e); err != nil {
		t.Fatal(err)
	}
}

// wantPrompt waits for the question of the open confirmation and checks it.
func (h *harness) wantPrompt(want []string) {
	h.t.Helper()
	h.until("confirmation ready", func(m Model) bool {
		return m.overlay.kind == overlayConfirm && !m.overlay.loading
	})
	h.probe(func(m Model) {
		if !slices.Equal(m.overlay.prompt, want) {
			h.t.Errorf("prompt:\n%q\nwant\n%q", m.overlay.prompt, want)
		}
	})
}

// wantMessage waits for the outcome of an operation in the status bar and
// checks it.
func (h *harness) wantMessage(want string) {
	h.t.Helper()
	h.waitIdle()
	h.probe(func(m Model) {
		if m.message.text != want {
			h.t.Errorf("message %q, want %q", m.message.text, want)
		}
	})
}

// wantClean checks that a superproject is clean at commit head.
func wantClean(t *testing.T, dir, head, what string) {
	t.Helper()
	if got := gittest.Git(t, dir, "status", "--porcelain"); got != "" {
		t.Errorf("%s: status %q", what, got)
	}
	if got := gittest.Git(t, dir, "rev-parse", "HEAD"); got != head {
		t.Errorf("%s: HEAD moved to %s", what, got)
	}
}

// TestUpdateRestoresWorkingTree checks an update that only rewrites the
// working tree copy of .lsm.lock or .gitmodules to what the superproject
// records: the question and the outcome name the files, and claim neither
// a checkout nor a staged or committed result, and nothing is staged or
// committed.
func TestUpdateRestoresWorkingTree(t *testing.T) {
	t.Parallel()
	lockV100 := func(t *testing.T, dir string, commits map[string]string) {
		t.Helper()
		writeLock(t, dir, lock.Entry{Name: "lib", Mode: manifest.ModeTag,
			Ref: gittest.TagV100, Commit: commits[gittest.TagV100]})
	}
	branchKey := func(t *testing.T, dir string, _ map[string]string) {
		t.Helper()
		gittest.Git(t, dir, "config", "-f", manifest.File,
			"submodule.lib."+manifest.KeyBranch, "main")
	}
	for _, c := range []struct {
		name  string
		spoil func(t *testing.T, dir string, commits map[string]string)
		// files and copies name the rewritten files for the notes and the
		// question.
		files, copies string
	}{
		{"lock entry", lockV100, ".lsm.lock", "copy of .lsm.lock is"},
		{"branch key", branchKey, ".gitmodules", "copy of .gitmodules is"},
		{"both", func(t *testing.T, dir string, commits map[string]string) {
			t.Helper()
			lockV100(t, dir, commits)
			branchKey(t, dir, commits)
		}, ".gitmodules and .lsm.lock", "copies of .gitmodules and .lsm.lock are"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			dir, commits := recordedSuper(t)
			head := gittest.Git(t, dir, "rev-parse", "HEAD")
			repo, err := core.Open(t.Context(), gittest.Runner(t), dir)
			if err != nil {
				t.Fatal(err)
			}
			target := gittest.TagV101 + " (" + commits[gittest.TagV101][:7] + ")"
			c.spoil(t, dir, commits)
			res, err := repo.Update(t.Context(), core.UpdateOptions{Names: []string{"lib"},
				DryRun: true})
			if err != nil || len(res.Changes) != 1 || !res.Changes[0].Changed() ||
				res.Changes[0].RecordChanged() {
				t.Fatalf("dry run = %+v, %v: want a change that records nothing", res, err)
			}
			h := start(t, repo)
			for _, commit := range []bool{false, true} {
				key, record, what, outcome := "u", "The index", "staged", ""
				if commit {
					key, record, what, outcome = "U", "HEAD", "committed", ", nothing to commit"
					c.spoil(t, dir, commits)
				}
				h.press(key)
				h.wantPrompt([]string{
					"Update lib: " + target + ", restore " + c.files,
					record + " records this already; the working tree " + c.copies +
						" rewritten to match it, and nothing is " + what + ".",
					"Continue?",
				})
				h.press("y")
				want := "lib: " + c.files + " restored to " + target + outcome
				h.wantMessage(want)
				h.wantScreen([]string{want}, "staged", "checked out")
				wantClean(t, dir, head, fmt.Sprintf("commit=%t", commit))
			}
			h.finish()
		})
	}
}

// TestUpdateCommitDiscardsStagedChange checks U when the index records
// another gitlink, lock entry or tracking keys for the submodule than HEAD,
// which records the target: the question says that the staged change is
// discarded, the update runs, and the index matches HEAD afterwards.
func TestUpdateCommitDiscardsStagedChange(t *testing.T) {
	t.Parallel()
	stageLock := func(t *testing.T, dir string, commits map[string]string) {
		t.Helper()
		writeLock(t, dir, lock.Entry{Name: "lib", Mode: manifest.ModeTag,
			Ref: gittest.TagV100, Commit: commits[gittest.TagV100]})
		gittest.Git(t, dir, "add", lock.File)
	}
	for _, c := range []struct {
		name  string
		spoil func(t *testing.T, dir string, commits map[string]string)
		// status is "git status --porcelain" after spoil; notes and done
		// describe the update in the question and the outcome.
		status, notes, question, done string
	}{
		{"staged lock", stageLock, "M  .lsm.lock", ", restore .lsm.lock",
			"the working tree copy of .lsm.lock is rewritten to match it, ", ".lsm.lock"},
		{"staged branch key", func(t *testing.T, dir string, _ map[string]string) {
			t.Helper()
			gittest.Git(t, dir, "config", "-f", manifest.File,
				"submodule.lib."+manifest.KeyBranch, "main")
			gittest.Git(t, dir, "add", manifest.File)
		}, "M  .gitmodules", ", restore .gitmodules",
			"the working tree copy of .gitmodules is rewritten to match it, ", ".gitmodules"},
		// Only the index differs: before, U reported "already up to date".
		{"index lock", func(t *testing.T, dir string, commits map[string]string) {
			t.Helper()
			p := filepath.Join(dir, lock.File)
			content := gittest.Git(t, dir, "show", "HEAD:"+lock.File) + "\n"
			stageLock(t, dir, commits)
			gittest.WriteFile(t, p, content)
		}, "MM .lsm.lock", "", "", "index"},
		{"index gitlink", func(t *testing.T, dir string, commits map[string]string) {
			t.Helper()
			gittest.Git(t, dir, "update-index", "--cacheinfo",
				"160000,"+commits[gittest.TagV100]+",lib")
		}, "MM lib", "", "", "index"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			dir, commits := recordedSuper(t)
			head := gittest.Git(t, dir, "rev-parse", "HEAD")
			repo, err := core.Open(t.Context(), gittest.Runner(t), dir)
			if err != nil {
				t.Fatal(err)
			}
			target := gittest.TagV101 + " (" + commits[gittest.TagV101][:7] + ")"
			c.spoil(t, dir, commits)
			if got := gittest.Git(t, dir, "status", "--porcelain"); got != c.status {
				t.Fatalf("status %q, want %q", got, c.status)
			}
			h := start(t, repo)
			h.press("U")
			h.wantPrompt([]string{
				"Update lib: " + target + c.notes + ", discard the staged change",
				"HEAD records this already; " + c.question +
					"the change staged for lib is discarded, and nothing is committed.",
				"Continue?",
			})
			h.press("y")
			h.wantMessage("lib: " + c.done + " restored to " + target +
				", staged change discarded, nothing to commit")
			wantClean(t, dir, head, "U")
			h.press("U")
			h.wantMessage("lib: already up to date; nothing to commit")
			h.finish()
		})
	}
}
