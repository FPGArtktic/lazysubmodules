// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package main

import (
	"strings"
	"testing"

	"github.com/FPGArtktic/lazysubmodules/internal/git/gittest"
)

// TestUpdateCommitLeavesOutInitialized checks that the commit of an update,
// and the subject that a dry run shows, leave out a submodule that the
// update only initializes, since the superproject records it already.
func TestUpdateCommitLeavesOutInitialized(t *testing.T) {
	t.Parallel()
	c := gittest.NewComplexSuper(t, gittest.SHA1)
	inv := invocation{dir: c.Dir, gitEnv: gittest.Env(t, c.Config...)}
	k, theme := c.Kernel, c.Theme
	git := func(args ...string) string {
		t.Helper()
		return c.Git(t, c.Dir, args...)
	}
	kernelLine := "kernel: v6.6.9 (" + short7(k.Lock.Commit) + ") -> v6.6.10 (" +
		short7(k.Tags["v6.6.10"]) + ")\n"
	themeLine := "theme: v1.0.0 (" + short7(theme.Gitlink) + "), "
	const subject = "manifest: update kernel to v6.6.10"

	inv.want(t, []string{"update", "--dry-run", "--commit", "theme", "kernel"}, exitOK,
		"would update "+kernelLine+"would update "+themeLine+"initialize\n"+
			"would commit \""+subject+"\"\n", "")
	inv.want(t, []string{"update", "--dry-run", "--commit", "theme"}, exitOK,
		"would update "+themeLine+"initialize\nnothing to commit\n", "")

	head := git("rev-parse", "HEAD")
	res := inv.wantCode(t, exitOK, "update", "--commit", "theme", "kernel")
	commit := git("rev-parse", "HEAD")
	if res.stdout != kernelLine+themeLine+"initialized\ncommitted "+commit+"\n" ||
		!strings.Contains(res.stderr, theme.Path) {
		t.Errorf("update --commit:\n%v", res)
	}
	want := subject + "\n\n" +
		"Tracking mode: tag-pattern v6.6.*\n" +
		"Old: " + k.Lock.Commit[:12] + " (v6.6.9)\n" +
		"New: " + k.Tags["v6.6.10"][:12] + " (v6.6.10)\n\n" + signOff
	if got := git("log", "-1", "--format=%B") + "\n"; got != want {
		t.Errorf("commit message:\n%s\nwant\n%s", got, want)
	}
	if parent := git("rev-parse", "HEAD^"); parent != head {
		t.Errorf("HEAD^ is %s, want %s: not exactly one commit", parent, head)
	}
	if got := git("diff", "--name-only", "HEAD^", "HEAD"); got != ".lsm.lock\nkernel" {
		t.Errorf("committed files %q", got)
	}
	if got := git("status", "--porcelain", "--", theme.Path, k.Path); got != "" {
		t.Errorf("status after the commit %q", got)
	}

	// An update that only initializes commits nothing.
	git("submodule", "deinit", "--force", "--quiet", "--", theme.Path)
	res = inv.wantCode(t, exitOK, "update", "--commit", "theme")
	if res.stdout != themeLine+"initialized\nnothing to commit\n" ||
		!strings.Contains(res.stderr, theme.Path) {
		t.Errorf("update --commit theme:\n%v", res)
	}
	if got := git("rev-parse", "HEAD"); got != commit {
		t.Errorf("HEAD moved to %s", got)
	}
	if got := git("diff", "--cached", "--name-only"); got != "" {
		t.Errorf("staged %q", got)
	}
	if got := c.Git(t, theme.Dir(), "rev-parse", "HEAD"); got != theme.Gitlink {
		t.Errorf("theme at %s, want %s", got, theme.Gitlink)
	}
}
