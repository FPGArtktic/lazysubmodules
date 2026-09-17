// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FPGArtktic/lazysubmodules/internal/git/gittest"
	"github.com/FPGArtktic/lazysubmodules/internal/lock"
	"github.com/FPGArtktic/lazysubmodules/internal/manifest"
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

// restoreSuper returns a superproject whose HEAD, index and working tree
// record the submodule lib tracking gittest.TagV101, with lib checked out
// there, a function that writes a lock entry for lib to the working tree,
// and the commits of the upstream tags.
func restoreSuper(t *testing.T) (*gittest.Super, func(ref, commit string), map[string]string) {
	t.Helper()
	up, commits := gittest.NewTaggedUpstream(t, gittest.SHA1)
	s := gittest.NewSuper(t, gittest.SHA1)
	s.AddSubmodule(t, "lib", up)
	v101 := commits[gittest.TagV101]
	gittest.Git(t, filepath.Join(s.Dir, "lib"), "checkout", "--quiet", "--detach", v101)
	s.SetKey(t, "lib", manifest.KeyMode, string(manifest.ModeTag))
	s.SetKey(t, "lib", manifest.KeyRef, gittest.TagV101)
	writeLock := func(ref, commit string) {
		t.Helper()
		e := lock.Entry{Name: "lib", Mode: manifest.ModeTag, Ref: ref, Commit: commit}
		if err := lock.Write(t.Context(), gittest.Runner(t), s.Dir, e); err != nil {
			t.Fatal(err)
		}
	}
	writeLock(gittest.TagV101, v101)
	s.Commit(t, "track lib")
	return s, writeLock, commits
}

// wantUnchanged checks that the superproject is clean at commit head.
func wantUnchanged(t *testing.T, dir, head, what string) {
	t.Helper()
	if got := gittest.Git(t, dir, "status", "--porcelain"); got != "" {
		t.Errorf("%s: status %q", what, got)
	}
	if got := gittest.Git(t, dir, "rev-parse", "HEAD"); got != head {
		t.Errorf("%s: HEAD moved to %s", what, got)
	}
}

// TestUpdateRestoresWorkingTree checks the output of an update that only
// rewrites the working tree copy of .lsm.lock or .gitmodules to what the
// superproject records, and that it stages and commits nothing.
func TestUpdateRestoresWorkingTree(t *testing.T) {
	t.Parallel()
	s, writeLock, commits := restoreSuper(t)
	v100, v101 := commits[gittest.TagV100], commits[gittest.TagV101]
	head := gittest.Git(t, s.Dir, "rev-parse", "HEAD")
	spoilers := []struct {
		spoil func()
		files string
	}{
		{func() { writeLock(gittest.TagV100, v100) }, ".lsm.lock"},
		{func() { s.SetKey(t, "lib", manifest.KeyBranch, "main") }, ".gitmodules"},
		{func() {
			writeLock(gittest.TagV100, v100)
			s.SetKey(t, "lib", manifest.KeyBranch, "main")
		}, ".gitmodules and .lsm.lock"},
	}
	inv := invocation{dir: s.Dir, gitEnv: gittest.Env(t)}
	line := "lib: " + gittest.TagV101 + " (" + short7(v101) + "), "
	for i, sp := range spoilers {
		sp.spoil()
		inv.want(t, []string{"update", "--dry-run"}, exitOK,
			"would update "+line+"restore "+sp.files+"\n", "")
		inv.want(t, []string{"update", "--dry-run", "--commit"}, exitOK,
			"would update "+line+"restore "+sp.files+"\nnothing to commit\n", "")
		inv.want(t, []string{"update"}, exitOK, line+"restored "+sp.files+"\n", "")
		wantUnchanged(t, s.Dir, head, fmt.Sprintf("spoiler %d, update", i))
		sp.spoil()
		inv.want(t, []string{"update", "--commit"}, exitOK,
			line+"restored "+sp.files+"\nnothing to commit\n", "")
		wantUnchanged(t, s.Dir, head, fmt.Sprintf("spoiler %d, update --commit", i))
	}
}

// TestUpdateCommitDiscardsStagedChange checks that update --commit says so
// when it stages what HEAD records for a submodule in place of a staged
// change, whether or not the working tree copy differs as well.
func TestUpdateCommitDiscardsStagedChange(t *testing.T) {
	t.Parallel()
	s, writeLock, commits := restoreSuper(t)
	v100, v101 := commits[gittest.TagV100], commits[gittest.TagV101]
	head := gittest.Git(t, s.Dir, "rev-parse", "HEAD")
	git := func(args ...string) {
		t.Helper()
		gittest.Git(t, s.Dir, args...)
	}
	// indexOnly stages the lock entry of v1.0.0 and puts back the working
	// tree copy.
	indexOnly := func() {
		content, err := os.ReadFile(filepath.Join(s.Dir, lock.File))
		if err != nil {
			t.Fatal(err)
		}
		writeLock(gittest.TagV100, v100)
		git("add", lock.File)
		gittest.WriteFile(t, filepath.Join(s.Dir, lock.File), string(content))
	}
	cases := []struct {
		name   string
		spoil  func()
		status string
		notes  string
	}{
		{"staged lock", func() {
			writeLock(gittest.TagV100, v100)
			git("add", lock.File)
		}, "M  .lsm.lock", "restore .lsm.lock, discard the staged change"},
		{"staged branch key", func() {
			s.SetKey(t, "lib", manifest.KeyBranch, "main")
			git("add", manifest.File)
		}, "M  .gitmodules", "restore .gitmodules, discard the staged change"},
		{"index lock", indexOnly, "MM .lsm.lock", "discard the staged change"},
		{"index gitlink", func() {
			git("update-index", "--cacheinfo", "160000,"+v100+",lib")
		}, "MM lib", "discard the staged change"},
	}
	inv := invocation{dir: s.Dir, gitEnv: gittest.Env(t)}
	line := "lib: " + gittest.TagV101 + " (" + short7(v101) + "), "
	for _, c := range cases {
		c.spoil()
		if got := gittest.Git(t, s.Dir, "status", "--porcelain"); got != c.status {
			t.Fatalf("%s: status %q, want %q", c.name, got, c.status)
		}
		done := strings.NewReplacer("restore", "restored", "discard", "discarded").
			Replace(c.notes)
		inv.want(t, []string{"update", "--dry-run", "--commit"}, exitOK,
			"would update "+line+c.notes+"\nnothing to commit\n", "")
		inv.want(t, []string{"update", "--commit"}, exitOK,
			line+done+"\nnothing to commit\n", "")
		wantUnchanged(t, s.Dir, head, c.name)
		inv.want(t, []string{"update", "--commit"}, exitOK,
			"lib: up to date\nnothing to commit\n", "")
	}
}
