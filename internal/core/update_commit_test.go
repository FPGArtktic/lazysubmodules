// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package core_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FPGArtktic/lazysubmodules/internal/core"
	"github.com/FPGArtktic/lazysubmodules/internal/git"
	"github.com/FPGArtktic/lazysubmodules/internal/git/gittest"
	"github.com/FPGArtktic/lazysubmodules/internal/lock"
	"github.com/FPGArtktic/lazysubmodules/internal/manifest"
)

func TestUpdateCommit(t *testing.T) {
	t.Parallel()
	for _, format := range formats() {
		t.Run(format, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t, format)
			v100, v101 := f.commits[gittest.TagV100], f.commits[gittest.TagV101]
			f.track("lib", manifest.ModeTagPattern, "v1.*", gittest.TagV100, v100)
			old := headOf(t, f.super.Dir)

			// Unrelated staged changes are refused.
			gittest.WriteFile(t, filepath.Join(f.super.Dir, "README"), "staged\n")
			gittest.Git(t, f.super.Dir, "add", "README")
			before := treeState(t, f.super.Dir)
			_, err := f.update(core.UpdateOptions{Commit: true})
			wantErr(t, "Update(unrelated)", err, core.ErrUnrelatedStaged, core.ErrRefused)
			if err == nil || !strings.HasSuffix(err.Error(), ": README") {
				t.Errorf("Update(unrelated) = %v", err)
			}
			wantSameTree(t, "refused commit", before, treeState(t, f.super.Dir))
			gittest.Git(t, f.super.Dir, "reset", "--quiet", "--", "README")

			// Staged changes of the lock file and .gitmodules are allowed.
			f.super.SetKey(t, "lib", "update", "checkout")
			gittest.Git(t, f.super.Dir, "add", manifest.File)
			res := f.mustUpdate(core.UpdateOptions{Commit: true})
			if res.Commit == "" || res.Commit != headOf(t, f.super.Dir) {
				t.Fatalf("Update(commit) = %+v", res)
			}
			if parent := gittest.Git(t, f.super.Dir, "rev-parse", "HEAD^"); parent != old {
				t.Errorf("parent %s, want %s: not exactly one commit", parent, old)
			}
			want := "manifest: update lib to v1.0.1\n\n" +
				"Tracking mode: tag-pattern v1.*\n" +
				"Old: " + v100[:12] + " (v1.0.0)\n" +
				"New: " + v101[:12] + " (v1.0.1)\n"
			if got := core.CommitMessage(res.Changes); got != want {
				t.Errorf("CommitMessage =\n%s\nwant\n%s", got, want)
			}
			msg := headMessage(t, f.super.Dir)
			if msg != want+"\n"+signOff {
				t.Errorf("commit message =\n%s\nwant\n%s", msg, want+"\n"+signOff)
			}
			lintMessage(t, msg)
			// The commit holds the update; README stays modified.
			if got := gitlinkAt(t, f.super.Dir, "HEAD", "lib"); got != v101 {
				t.Errorf("committed gitlink %s, want %s", got, v101)
			}
			files := gittest.Git(t, f.super.Dir, "diff", "--name-only", "HEAD^", "HEAD")
			if files != ".gitmodules\n.lsm.lock\nlib" {
				t.Errorf("committed files %q", files)
			}
			status := gittest.Git(t, f.super.Dir, "status", "--porcelain")
			if status != "M README" {
				t.Errorf("status after commit %q", status)
			}
			if vr, err := f.repo().Verify(t.Context(), nil, core.VerifyOptions{}); err != nil ||
				!vr[0].OK() {
				t.Errorf("Verify after commit = %+v, %v", vr, err)
			}

			// Nothing changed: no commit.
			committed := res.Commit
			res = f.mustUpdate(core.UpdateOptions{Commit: true})
			if res.Commit != "" || headOf(t, f.super.Dir) != committed {
				t.Errorf("second Update(commit) = %+v", res)
			}
		})
	}
}

func TestUpdateCommitSeveral(t *testing.T) {
	t.Parallel()
	f := newFixture(t, gittest.SHA1)
	v100, v101 := f.commits[gittest.TagV100], f.commits[gittest.TagV101]
	tip := f.commits[gittest.TagV200RC]
	f.track("u-boot", manifest.ModeBranch, "main", "main", v101)
	f.super.SetKey(t, "u-boot", manifest.KeyBranch, "main")
	f.track("same", manifest.ModeTag, gittest.TagV100, gittest.TagV100, v100)
	f.track("kernel", manifest.ModeTagPattern, "v1.*", gittest.TagV100, v100)
	f.add("new", manifest.ModeTag, gittest.TagV101)
	old := headOf(t, f.super.Dir)

	res := f.mustUpdate(core.UpdateOptions{Commit: true})
	want := "manifest: update 3 submodules\n\n" +
		"Submodule \"u-boot\":\n" +
		"  Tracking mode: branch main\n" +
		"  Old: " + v101[:12] + " (main)\n" +
		"  New: " + tip[:12] + " (main)\n\n" +
		"Submodule \"kernel\":\n" +
		"  Tracking mode: tag-pattern v1.*\n" +
		"  Old: " + v100[:12] + " (v1.0.0)\n" +
		"  New: " + v101[:12] + " (v1.0.1)\n\n" +
		"Submodule \"new\":\n" +
		"  Tracking mode: tag v1.0.1\n" +
		"  Old: " + tip[:12] + " (unlocked)\n" +
		"  New: " + v101[:12] + " (v1.0.1)\n\n" +
		signOff
	msg := headMessage(t, f.super.Dir)
	if msg != want {
		t.Errorf("commit message =\n%s\nwant\n%s", msg, want)
	}
	lintMessage(t, msg)
	if res.Commit != headOf(t, f.super.Dir) ||
		gittest.Git(t, f.super.Dir, "rev-parse", "HEAD^") != old {
		t.Errorf("Update(commit) = %+v: not exactly one commit", res)
	}
	files := gittest.Git(t, f.super.Dir, "diff", "--name-only", "HEAD^", "HEAD")
	if files != ".lsm.lock\nkernel\nnew\nu-boot" {
		t.Errorf("committed files %q", files)
	}
	if status := gittest.Git(t, f.super.Dir, "status", "--porcelain"); status != "" {
		t.Errorf("status after commit %q", status)
	}
}

func TestUpdateCommitUnbornSuperproject(t *testing.T) {
	t.Parallel()
	up, commits := gittest.NewTaggedUpstream(t, gittest.SHA256)
	dir := gittest.InitRepo(t, gittest.SHA256)
	gittest.Git(t, dir, "submodule", "add", "--quiet", "--", up.Bare, "lib")
	super := &gittest.Super{Dir: dir}
	super.SetKey(t, "lib", manifest.KeyMode, string(manifest.ModeTag))
	super.SetKey(t, "lib", manifest.KeyRef, gittest.TagV100)
	f := &fixture{t: t, up: up, commits: commits, super: super, g: gittest.Runner(t)}

	res := f.mustUpdate(core.UpdateOptions{Commit: true})
	if res.Commit == "" || gittest.Git(t, dir, "rev-list", "--count", "HEAD") != "1" {
		t.Fatalf("Update(commit) = %+v, want a root commit", res)
	}
	files := gittest.Git(t, dir, "ls-tree", "--name-only", "HEAD")
	if files != ".gitmodules\n.lsm.lock\nlib" {
		t.Errorf("committed files %q", files)
	}
	if got := gitlinkAt(t, dir, "HEAD", "lib"); got != commits[gittest.TagV100] {
		t.Errorf("committed gitlink %s", got)
	}
	lintMessage(t, headMessage(t, dir))
	// The root commit replaces no gitlink.
	if !strings.Contains(headMessage(t, dir), "\nOld: none\nNew: "+
		commits[gittest.TagV100][:12]+" (v1.0.0)\n") {
		t.Errorf("commit message:\n%s", headMessage(t, dir))
	}
	if vr, err := f.repo().Verify(t.Context(), nil, core.VerifyOptions{}); err != nil ||
		!vr[0].OK() {
		t.Errorf("Verify after commit = %+v, %v", vr, err)
	}
}

func TestUpdateCommitFails(t *testing.T) {
	t.Parallel()
	f := newFixture(t, gittest.SHA1)
	f.track("lib", manifest.ModeTagPattern, "v1.*", gittest.TagV100, f.commits[gittest.TagV100])
	hook := filepath.Join(f.super.Dir, ".git", "hooks", "commit-msg")
	gittest.WriteFile(t, hook, "#!/bin/sh\necho 'rejected by hook' >&2\nexit 1\n")
	if err := os.Chmod(hook, 0o755); err != nil {
		t.Fatal(err)
	}
	old := headOf(t, f.super.Dir)
	res, err := f.update(core.UpdateOptions{Commit: true})
	if gitErr, ok := errors.AsType[*git.Error](err); !ok ||
		!strings.Contains(gitErr.Stderr, "rejected by hook") {
		t.Errorf("Update(commit) = %v, want the *git.Error of the hook", err)
	}
	if res.Commit != "" || len(res.Changes) != 1 || !res.Changes[0].Changed() ||
		headOf(t, f.super.Dir) != old {
		t.Errorf("Update(commit) = %+v", res)
	}
	// The update stays staged.
	wantStaged(t, f.super.Dir, lock.File, "lib")
}

func TestUpdateCommitNothingStaged(t *testing.T) {
	t.Parallel()
	f := newFixture(t, gittest.SHA1)
	v100, v101 := f.commits[gittest.TagV100], f.commits[gittest.TagV101]
	f.track("lib", manifest.ModeTag, gittest.TagV101, gittest.TagV101, v101)
	f.checkout("lib", v100)
	old := headOf(t, f.super.Dir)
	// The lock and the committed gitlink already agree with the target:
	// the checkout changes nothing that could be committed.
	res := f.mustUpdate(core.UpdateOptions{Commit: true})
	if c := oneChange(t, res); !c.Changed() || c.OldHead != v100 || res.Commit != "" {
		t.Errorf("Update(commit) = %+v", res)
	}
	if headOf(t, f.super.Dir) != old || headOf(t, f.dir("lib")) != v101 {
		t.Errorf("unexpected commit or checkout")
	}
	wantStaged(t, f.super.Dir)
}
