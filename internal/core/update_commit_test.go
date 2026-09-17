// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package core_test

import (
	"errors"
	"fmt"
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

// unrelatedRefusal is the start of the refusal of an update with Commit
// that would commit unrelated changes.
const unrelatedRefusal = "refused: the commit would include unrelated changes: "

// outsideChange names a file in the refusal of an update with Commit whose copies
// differ outside the sections of the selected submodules; kind tells which
// copies differ: "staged", "unstaged" or both.
func outsideChange(file, kind string) string {
	return file + " (" + kind + " changes outside the selected submodules)"
}

func TestUpdateCommitRefusesOtherSections(t *testing.T) {
	t.Parallel()
	unstaged := outsideChange(manifest.File, "unstaged")
	for _, c := range []struct {
		name string
		// setup changes the copies of the files; it runs after both
		// submodules were committed at v1.0.0.
		setup func(t *testing.T, f *fixture)
		// refusal lists what the refusal names.
		refusal string
		// all reports whether an update of every submodule is allowed.
		all bool
	}{
		{"tracking keys", func(_ *testing.T, f *fixture) {
			f.mustSet("other", manifest.ModeBranch, "main")
		}, unstaged, true},
		{"staged tracking keys", func(t *testing.T, f *fixture) {
			f.mustSet("other", manifest.ModeBranch, "main")
			gittest.Git(t, f.super.Dir, "add", manifest.File)
		}, outsideChange(manifest.File, "staged"), true},
		{"staged only", func(t *testing.T, f *fixture) {
			file := filepath.Join(f.super.Dir, manifest.File)
			content := readFile(t, file)
			f.mustSet("other", manifest.ModeBranch, "main")
			gittest.Git(t, f.super.Dir, "add", manifest.File)
			gittest.WriteFile(t, file, content)
		}, outsideChange(manifest.File, "staged and unstaged"), true},
		{"lock entry", func(_ *testing.T, f *fixture) {
			f.lock("other", manifest.ModeTag, gittest.TagV100, f.commits[gittest.TagV101])
		}, outsideChange(lock.File, "unstaged"), true},
		{"staged lock entry", func(t *testing.T, f *fixture) {
			f.lock("other", manifest.ModeTag, gittest.TagV100, f.commits[gittest.TagV101])
			gittest.Git(t, f.super.Dir, "add", lock.File)
		}, outsideChange(lock.File, "staged"), true},
		{"other section", func(t *testing.T, f *fixture) {
			gittest.Git(t, f.super.Dir, "config", "-f", manifest.File, "lsm.note", "x")
		}, unstaged, false},
		{"entry without submodule", func(t *testing.T, f *fixture) {
			f.lock("gone", manifest.ModeTag, gittest.TagV100, f.commits[gittest.TagV100])
			f.super.SetKey(t, "other", "update", "checkout")
		}, unstaged + ", " + outsideChange(lock.File, "unstaged"), false},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t, gittest.SHA1)
			v100 := f.commits[gittest.TagV100]
			f.track("lib", manifest.ModeTagPattern, "v1.*", gittest.TagV100, v100)
			f.track("other", manifest.ModeTag, gittest.TagV100, gittest.TagV100, v100)
			c.setup(t, f)
			head := headOf(t, f.super.Dir)
			before := treeState(t, f.super.Dir)
			lib := []string{"lib"}
			for _, opts := range []core.UpdateOptions{
				{Names: lib, Commit: true},
				{Names: lib, Commit: true, DryRun: true},
				{Names: lib, Commit: true, Fetch: true},
			} {
				res, err := f.update(opts)
				what := fmt.Sprintf("Update(%+v)", opts)
				wantErr(t, what, err, core.ErrUnrelatedStaged, core.ErrRefused)
				want := unrelatedRefusal + c.refusal
				if err == nil || err.Error() != want || len(res.Changes) != 0 {
					t.Errorf("%s = %+v, %v\nwant %s", what, res, err, want)
				}
			}
			wantSameTree(t, "refused update", before, treeState(t, f.super.Dir))
			// Without Commit, the files are staged as a whole.
			if _, err := f.update(core.UpdateOptions{Names: lib, DryRun: true}); err != nil {
				t.Errorf("Update(dry run) = %v", err)
			}

			res, err := f.update(core.UpdateOptions{Commit: true})
			if !c.all {
				wantErr(t, "Update(all)", err, core.ErrUnrelatedStaged)
				wantSameTree(t, "refused update", before, treeState(t, f.super.Dir))
				return
			}
			if err != nil || res.Commit == "" ||
				gittest.Git(t, f.super.Dir, "rev-parse", "HEAD^") != head {
				t.Fatalf("Update(all) = %+v, %v", res, err)
			}
			lintMessage(t, headMessage(t, f.super.Dir))
			// The commit records every copy as the working tree has it.
			if status := gittest.Git(t, f.super.Dir, "status", "--porcelain"); status != "" {
				t.Errorf("status after commit %q", status)
			}
			if vr, err := f.repo().Verify(t.Context(), nil, core.VerifyOptions{}); err != nil {
				t.Errorf("Verify = %+v, %v", vr, err)
			}
		})
	}
}

func TestUpdateCommitRefusesOtherSectionsUnborn(t *testing.T) {
	t.Parallel()
	up, commits := gittest.NewTaggedUpstream(t, gittest.SHA1)
	dir := gittest.InitRepo(t, gittest.SHA1)
	super := &gittest.Super{Dir: dir}
	for _, name := range []string{"lib", "other"} {
		gittest.Git(t, dir, "submodule", "add", "--quiet", "--", up.Bare, name)
		super.SetKey(t, name, manifest.KeyMode, string(manifest.ModeTag))
		super.SetKey(t, name, manifest.KeyRef, gittest.TagV100)
	}
	f := &fixture{t: t, up: up, commits: commits, super: super, g: gittest.Runner(t)}
	before := treeState(t, dir)
	// Without a commit, HEAD records neither section.
	_, err := f.update(core.UpdateOptions{Names: []string{"lib"}, Commit: true})
	want := unrelatedRefusal + outsideChange(manifest.File, "staged and unstaged") + ", other"
	if !errors.Is(err, core.ErrUnrelatedStaged) || err.Error() != want {
		t.Errorf("Update(lib) = %v, want %s", err, want)
	}
	wantSameTree(t, "refused update", before, treeState(t, dir))
	res := f.mustUpdate(core.UpdateOptions{Commit: true})
	if res.Commit == "" || !strings.HasPrefix(headMessage(t, dir), "manifest: update 2 submodules\n") {
		t.Errorf("Update(all) = %+v\n%s", res, headMessage(t, dir))
	}
}

func TestUpdateCommitInvalidEntryInHead(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name string
		// damage makes the entry of lib invalid, repair makes it valid again.
		damage, repair func(t *testing.T, f *fixture)
		// file is the file that the update commits, and old the lock ref of
		// the commit message.
		file, old string
	}{
		{
			"lsm-mode",
			func(t *testing.T, f *fixture) { f.super.SetKey(t, "lib", manifest.KeyMode, "bogus") },
			func(_ *testing.T, f *fixture) { f.mustSet("lib", manifest.ModeTag, gittest.TagV100) },
			manifest.File, gittest.TagV100,
		},
		{
			"lock entry",
			func(t *testing.T, f *fixture) {
				gittest.Git(t, f.super.Dir, "config", "-f", lock.File, "submodule.lib.commit", "bogus")
			},
			func(_ *testing.T, f *fixture) {
				f.lock("lib", manifest.ModeTag, gittest.TagV100, f.commits[gittest.TagV100])
			},
			lock.File, "unlocked",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t, gittest.SHA1)
			v100 := f.commits[gittest.TagV100]
			for _, name := range []string{"lib", "other"} {
				f.track(name, manifest.ModeTag, gittest.TagV100, gittest.TagV100, v100)
			}
			c.damage(t, f)
			f.super.Commit(t, "damage lib")
			c.repair(t, f)

			// Only the entry of lib is not recorded; other is up to date.
			res, err := f.update(core.UpdateOptions{Commit: true, DryRun: true})
			if err != nil || len(res.Changes) != 2 || !res.Changes[0].Changed() ||
				res.Changes[1].Changed() {
				t.Fatalf("Update(dry run) = %+v, %v", res, err)
			}
			wantCommitted(t, f, false, c.file, "manifest: update lib to v1.0.0\n\n"+
				"Tracking mode: tag v1.0.0\n"+
				"Old: "+v100[:12]+" ("+c.old+")\n"+
				"New: "+v100[:12]+" (v1.0.0)\n")
		})
	}
}
