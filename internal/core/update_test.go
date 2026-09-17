// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package core_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/FPGArtktic/lazysubmodules/internal/core"
	"github.com/FPGArtktic/lazysubmodules/internal/git"
	"github.com/FPGArtktic/lazysubmodules/internal/git/gittest"
	"github.com/FPGArtktic/lazysubmodules/internal/lock"
	"github.com/FPGArtktic/lazysubmodules/internal/manifest"
)

// oneChange checks that a result has a single change and returns it.
func oneChange(t *testing.T, res core.UpdateResult) core.Change {
	t.Helper()
	if len(res.Changes) != 1 {
		t.Fatalf("Update returned %d changes, want 1", len(res.Changes))
	}
	return res.Changes[0]
}

// wantSteps checks the Init and Clone flags of a change.
func wantSteps(t *testing.T, c core.Change, init, clone bool) {
	t.Helper()
	if c.Init != init || c.Clone != clone {
		t.Errorf("%s: Init %v, Clone %v; want %v, %v",
			c.Submodule.Name, c.Init, c.Clone, init, clone)
	}
}

func TestUpdateModes(t *testing.T) {
	t.Parallel()
	for _, format := range formats() {
		for _, c := range []struct {
			name    string
			mode    manifest.Mode
			ref     func(f *fixture) string
			lockRef func(f *fixture) string
			tag     string // the tag of the target commit
			staged  []string
		}{
			{"branch at HEAD", manifest.ModeBranch, fixed("main"), fixed("main"),
				gittest.TagV200RC, []string{manifest.File, lock.File}},
			{"branch", manifest.ModeBranch, fixed(gittest.BranchStable),
				fixed(gittest.BranchStable), gittest.TagV101,
				[]string{manifest.File, lock.File, "lib"}},
			{"annotated tag", manifest.ModeTag, fixed(gittest.TagV100), fixed(gittest.TagV100),
				gittest.TagV100, []string{lock.File, "lib"}},
			{"tag pattern", manifest.ModeTagPattern, fixed("v1.*"), fixed(gittest.TagV101),
				gittest.TagV101, []string{lock.File, "lib"}},
			{"commit", manifest.ModeCommit, abbreviated(gittest.TagV100), full(gittest.TagV100),
				gittest.TagV100, []string{lock.File, "lib"}},
		} {
			t.Run(format+"/"+c.name, func(t *testing.T) {
				t.Parallel()
				f := newFixture(t, format)
				f.add("lib", c.mode, c.ref(f))
				tip := f.commits[gittest.TagV200RC]
				target := core.Resolution{Mode: c.mode, Ref: c.lockRef(f), Commit: f.commits[c.tag]}

				change := oneChange(t, f.mustUpdate(core.UpdateOptions{}))
				if change.Old != nil || change.OldHead != tip || change.New != target ||
					!change.Changed() {
					t.Errorf("change %+v, want old head %s and target %+v", change, tip, target)
				}
				wantSteps(t, change, false, false)
				if got := headOf(t, f.dir("lib")); got != target.Commit {
					t.Errorf("HEAD %s, want %s", got, target.Commit)
				}
				want := lock.Entry{Name: "lib", Mode: c.mode, Ref: target.Ref, Commit: target.Commit}
				if got := lockOf(t, f, "lib"); got == nil || *got != want {
					t.Errorf("lock entry %+v, want %+v", got, want)
				}
				wantBranch := ""
				if c.mode == manifest.ModeBranch {
					wantBranch = c.ref(f)
				}
				if got := gitmodulesKey(t, f.super.Dir, "lib", "branch"); got != wantBranch {
					t.Errorf("native branch %q, want %q", got, wantBranch)
				}
				wantStaged(t, f.super.Dir, c.staged...)
				indexMatchesWorktree(t, f.super.Dir, manifest.File, lock.File)
				if got := gitlinkAt(t, f.super.Dir, "", "lib"); got != target.Commit {
					t.Errorf("gitlink %s, want %s", got, target.Commit)
				}
				wantState(t, f.status("lib"), core.StateOK, "")

				// A second update changes nothing.
				before := treeState(t, f.super.Dir)
				res := f.mustUpdate(core.UpdateOptions{})
				if change := oneChange(t, res); change.Changed() || res.Commit != "" ||
					change.OldHead != target.Commit || change.OldGitlink != target.Commit ||
					change.New != target {
					t.Errorf("second update: %+v", res)
				}
				wantSameTree(t, "second update", before, treeState(t, f.super.Dir))
				if slices.Contains(c.staged, "lib") {
					wantCommitStaged(t, f, c.ref(f), tip, target)
				}
			})
		}
	}
}

// wantCommitStaged checks that an update with Commit commits the update of
// "lib" that an update without Commit staged before, and that a further
// update changes nothing.
func wantCommitStaged(t *testing.T, f *fixture, ref, oldGitlink string, target core.Resolution) {
	t.Helper()
	res := f.mustUpdate(core.UpdateOptions{Commit: true})
	change := oneChange(t, res)
	if !change.Changed() || change.OldGitlink != oldGitlink || change.Old != nil ||
		change.New != target || res.Commit == "" || res.Commit != headOf(t, f.super.Dir) {
		t.Fatalf("update with commit: %+v", res)
	}
	shown, note := target.Ref, " ("+target.Ref+")"
	if target.Mode == manifest.ModeCommit {
		shown, note = target.Commit[:12], ""
	}
	// The lock file in HEAD has no entry yet.
	want := "manifest: update lib to " + shown + "\n\n" +
		"Tracking mode: " + string(target.Mode) + " " + ref + "\n" +
		"Old: " + oldGitlink[:12] + " (unlocked)\n" +
		"New: " + target.Commit[:12] + note + "\n\n" + signOff
	if msg := headMessage(t, f.super.Dir); msg != want {
		t.Errorf("commit message:\n%s\nwant\n%s", msg, want)
	}
	if got := gitlinkAt(t, f.super.Dir, "HEAD", "lib"); got != target.Commit {
		t.Errorf("committed gitlink %s, want %s", got, target.Commit)
	}
	wantStaged(t, f.super.Dir)
	if vr, err := f.repo().Verify(t.Context(), nil, core.VerifyOptions{}); err != nil {
		t.Errorf("Verify after commit = %+v, %v", vr, err)
	}
	before := treeState(t, f.super.Dir)
	res = f.mustUpdate(core.UpdateOptions{Commit: true})
	if change := oneChange(t, res); change.Changed() || res.Commit != "" {
		t.Errorf("update after commit: %+v", res)
	}
	wantSameTree(t, "update after commit", before, treeState(t, f.super.Dir))
}

// fixed returns a constant ref.
func fixed(ref string) func(*fixture) string {
	return func(*fixture) string { return ref }
}

// full returns the commit of a tag.
func full(tag string) func(*fixture) string {
	return func(f *fixture) string { return f.commits[tag] }
}

// abbreviated returns the commit of a tag, abbreviated to 10 digits.
func abbreviated(tag string) func(*fixture) string {
	return func(f *fixture) string { return f.commits[tag][:10] }
}

func TestUpdateStagesOnlyItsFiles(t *testing.T) {
	t.Parallel()
	f := newFixture(t, gittest.SHA1)
	f.add("lib", manifest.ModeTag, gittest.TagV101)
	f.add("other", manifest.ModeTag, gittest.TagV100)
	// A superproject that ignores lock files still gets the lock staged.
	gittest.WriteFile(t, filepath.Join(f.super.Dir, ".gitignore"), "*.lock\n")
	f.super.Commit(t, "ignore lock files")
	gittest.WriteFile(t, filepath.Join(f.super.Dir, "README"), "changed\n")
	gittest.WriteFile(t, filepath.Join(f.super.Dir, "untracked"), "new\n")
	gittest.WriteFile(t, filepath.Join(f.dir("other"), "untracked"), "new\n")
	gittest.Git(t, f.dir("other"), "checkout", "--quiet", "--detach",
		f.commits[gittest.TagV100])
	f.lock("other", manifest.ModeTag, gittest.TagV100, f.commits[gittest.TagV100])

	res := f.mustUpdate(core.UpdateOptions{Names: []string{"lib"}})
	if c := oneChange(t, res); c.Submodule.Name != "lib" || !c.Changed() {
		t.Errorf("change %+v", c)
	}
	wantStaged(t, f.super.Dir, lock.File, "lib")
	// Everything else keeps its state; the lock file is staged as a whole.
	status := gittest.Git(t, f.super.Dir, "status", "--porcelain", "--untracked-files=all",
		"--ignore-submodules=none")
	want := "A  .lsm.lock\n M README\nM  lib\n M other\n?? untracked"
	if status != want {
		t.Errorf("status:\n%s\nwant\n%s", status, want)
	}
	if got := gitlinkAt(t, f.super.Dir, "", "other"); got != f.commits[gittest.TagV200RC] {
		t.Errorf("gitlink of other %s was changed", got)
	}
}

func TestUpdateDryRun(t *testing.T) {
	t.Parallel()
	for _, format := range formats() {
		t.Run(format, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t, format)
			v100, v101 := f.commits[gittest.TagV100], f.commits[gittest.TagV101]
			pattern := manifest.ModeTagPattern
			f.track("behind", pattern, "v1.*", gittest.TagV100, v100)
			f.track("ok", manifest.ModeTag, gittest.TagV101, gittest.TagV101, v101)
			f.track("deinit", pattern, "v1.*", gittest.TagV100, v100)
			f.super.Deinit(t, "deinit")
			f.track("fresh", manifest.ModeTag, gittest.TagV100, gittest.TagV100, v100)
			f.super.Deinit(t, "fresh")
			f.removeModule("fresh")
			f.add("later", manifest.ModeTag, "v9")
			gittest.WriteFile(t, filepath.Join(f.super.Dir, "README"), "staged\n")
			gittest.Git(t, f.super.Dir, "add", "README")
			f.up.Commit(t, "newer")
			f.up.Tag(t, "v9", f.commits[gittest.TagV101])
			before := treeState(t, f.super.Dir)

			// Without Fetch, the update would be refused.
			res, err := f.update(core.UpdateOptions{DryRun: true, Commit: true})
			wantErr(t, "Update(dry run)", err, core.ErrUninitialized, core.ErrMissingRef,
				core.ErrUnrelatedStaged)
			if len(res.Changes) != 0 {
				t.Errorf("refused dry run returned %+v", res)
			}
			wantSameTree(t, "refused dry run", before, treeState(t, f.super.Dir))

			// With Fetch, nothing is fetched and unknown targets stay empty.
			res, err = f.update(core.UpdateOptions{DryRun: true, Fetch: true,
				Names: []string{"later", "fresh", "deinit", "ok", "behind"}})
			if err != nil || res.Commit != "" {
				t.Fatalf("Update(dry run) = %+v, %v", res, err)
			}
			wantSameTree(t, "dry run", before, treeState(t, f.super.Dir))
			want := []struct {
				name        string
				changed     bool
				init, clone bool
				head        string
				target      string
			}{
				{"behind", true, false, false, v100, v101},
				{"ok", false, false, false, v101, v101},
				{"deinit", true, true, false, "", v101},
				{"fresh", true, true, true, "", ""},
				{"later", true, false, false, f.commits[gittest.TagV200RC], ""},
			}
			if len(res.Changes) != len(want) {
				t.Fatalf("dry run returned %d changes", len(res.Changes))
			}
			for i, w := range want {
				c := res.Changes[i]
				if c.Submodule.Name != w.name || c.Changed() != w.changed ||
					c.OldHead != w.head || c.New.Commit != w.target {
					t.Errorf("change %d: %+v, want %+v", i, c, w)
				}
				wantSteps(t, c, w.init, w.clone)
			}
			if c := res.Changes[0]; c.Old == nil || c.Old.Ref != gittest.TagV100 ||
				c.New.Ref != gittest.TagV101 {
				t.Errorf("behind: old %+v, new %+v", c.Old, c.New)
			}
		})
	}
}

func TestUpdateRefusesAll(t *testing.T) {
	t.Parallel()
	f := newFixture(t, gittest.SHA1)
	v100, v101 := f.commits[gittest.TagV100], f.commits[gittest.TagV101]
	f.track("dirty", manifest.ModeTag, gittest.TagV101, gittest.TagV101, v101)
	f.track("clean", manifest.ModeTagPattern, "v1.*", gittest.TagV100, v100)
	f.super.MakeDirty(t, "dirty")
	before := treeState(t, f.super.Dir)

	res, err := f.update(core.UpdateOptions{})
	wantErr(t, "Update(dirty)", err, core.ErrDirty, core.ErrRefused)
	if errors.Is(err, core.ErrMissingRef) || len(res.Changes) != 0 || err == nil ||
		err.Error() != "dirty: refused: submodule has uncommitted changes" {
		t.Errorf("Update(dirty) = %+v, %v", res, err)
	}
	wantSameTree(t, "refused update", before, treeState(t, f.super.Dir))

	// Every refusal is reported, the unrelated staged changes first.
	f.add("missing", manifest.ModeTag, "v9")
	f.add("invalid", manifest.ModeBranch, "a..b")
	f.track("gone", manifest.ModeTag, gittest.TagV101, gittest.TagV101, v101)
	f.super.Deinit(t, "gone")
	f.removeModule("gone")
	for _, file := range []string{"a", "b", "c", "d"} {
		gittest.WriteFile(t, filepath.Join(f.super.Dir, file), file+"\n")
	}
	gittest.Git(t, f.super.Dir, "add", "a", "b", "c", "d")
	before = treeState(t, f.super.Dir)
	_, err = f.update(core.UpdateOptions{Commit: true, Fetch: true})
	wantErr(t, "Update(all refusals)", err, core.ErrDirty, core.ErrMissingRef,
		core.ErrUnrelatedStaged)
	want := []string{
		"refused: the commit would include unrelated changes: a, b, c, and 1 more",
		"dirty: refused: submodule has uncommitted changes",
		`invalid: refused: bad ref in .gitmodules: invalid branch "a..b": ` +
			"not a valid branch name",
	}
	if err == nil || err.Error() != strings.Join(want, "\n") {
		t.Errorf("Update(all refusals) = %v\nwant\n%s", err, strings.Join(want, "\n"))
	}
	wantSameTree(t, "refused update", before, treeState(t, f.super.Dir))

	// Without Fetch, a missing ref and a missing repository are refused
	// before anything happens.
	_, err = f.update(core.UpdateOptions{Names: []string{"missing", "gone", "clean"}})
	want = []string{
		"missing: refused: ref not found in local refs: " +
			"tag v9 does not exist or does not point to a commit",
		"gone: refused: submodule is not initialized (use --fetch)",
	}
	wantErr(t, "Update(missing)", err, core.ErrMissingRef, core.ErrUninitialized)
	if err == nil || err.Error() != strings.Join(want, "\n") {
		t.Errorf("Update(missing) = %v\nwant\n%s", err, strings.Join(want, "\n"))
	}
	wantSameTree(t, "refused update", before, treeState(t, f.super.Dir))
}

func TestUpdateMissingRefAfterFetch(t *testing.T) {
	t.Parallel()
	f := newFixture(t, gittest.SHA1)
	v100 := f.commits[gittest.TagV100]
	f.track("lib", manifest.ModeTagPattern, "v1.*", gittest.TagV100, v100)
	f.add("later", manifest.ModeTag, "v9")
	_, err := f.update(core.UpdateOptions{Fetch: true})
	wantErr(t, "Update(missing after fetch)", err, core.ErrMissingRef)
	// The refs were fetched, but no submodule was moved and no file written.
	if got := headOf(t, f.dir("lib")); got != v100 {
		t.Errorf("lib was moved to %s", got)
	}
	wantStaged(t, f.super.Dir)
	if got := lockOf(t, f, "later"); got != nil {
		t.Errorf("lock entry %+v was written", got)
	}

	// The tag appears upstream; the next update with Fetch finds it.
	f.up.Tag(t, "v9", f.commits[gittest.TagV100])
	res := f.mustUpdate(core.UpdateOptions{Fetch: true})
	if len(res.Changes) != 2 || res.Changes[1].New.Commit != v100 ||
		res.Changes[0].New.Commit != f.commits[gittest.TagV101] {
		t.Errorf("Update(fetch) = %+v", res)
	}
}

func TestUpdateSymlinkedPath(t *testing.T) {
	t.Parallel()
	f := newFixture(t, gittest.SHA1)
	target := filepath.Join(t.TempDir(), "repo")
	gittest.Git(t, f.super.Dir, "clone", "--quiet", f.up.Bare, target)
	if err := os.Symlink(target, filepath.Join(f.super.Dir, "link")); err != nil {
		t.Fatal(err)
	}
	f.super.SetKey(t, "link", manifest.KeyPath, "link")
	f.super.SetKey(t, "link", manifest.KeyURL, f.up.Bare)
	f.configure("link", manifest.ModeTag, gittest.TagV100)
	before := treeState(t, target)
	for _, fetch := range []bool{false, true} {
		_, err := f.update(core.UpdateOptions{Fetch: fetch})
		wantErr(t, "Update(symlink)", err, core.ErrSymlinkPath, core.ErrRefused)
		_, err = f.repo().Fetch(t.Context(), nil, nil)
		wantErr(t, "Fetch(symlink)", err, core.ErrSymlinkPath)
	}
	wantSameTree(t, "update through a symbolic link", before, treeState(t, target))
}

func TestUpdateUninitialized(t *testing.T) {
	t.Parallel()
	for _, format := range formats() {
		t.Run(format, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t, format)
			v100, v101 := f.commits[gittest.TagV100], f.commits[gittest.TagV101]
			f.track("deinit", manifest.ModeTagPattern, "v1.*", gittest.TagV100, v100)
			f.track("removed", manifest.ModeTag, gittest.TagV101, gittest.TagV101, v101)
			f.super.Deinit(t, "deinit")
			if err := os.RemoveAll(f.dir("removed")); err != nil {
				t.Fatal(err)
			}
			// Offline: the upstream is gone.
			if err := os.Rename(f.up.Bare, f.up.Bare+".moved"); err != nil {
				t.Fatal(err)
			}

			res := f.mustUpdate(core.UpdateOptions{})
			if len(res.Changes) != 2 {
				t.Fatalf("Update = %+v", res)
			}
			for i, target := range []string{v101, v101} {
				c := res.Changes[i]
				wantSteps(t, c, true, false)
				if c.OldHead != "" || c.New.Commit != target || !c.Changed() {
					t.Errorf("change %+v", c)
				}
				if got := headOf(t, f.dir(c.Submodule.Name)); got != target {
					t.Errorf("%s: HEAD %s, want %s", c.Submodule.Name, got, target)
				}
				wantState(t, f.status(c.Submodule.Name), core.StateOK, "")
			}
			wantStaged(t, f.super.Dir, lock.File, "deinit")
		})
	}
}

func TestUpdateClone(t *testing.T) {
	t.Parallel()
	f := newFixture(t, gittest.SHA1)
	v100 := f.commits[gittest.TagV100]
	f.track("lib", manifest.ModeTagPattern, "v1.*", gittest.TagV100, v100)
	clone := filepath.Join(t.TempDir(), "clone")
	gittest.Git(t, f.super.Dir, "clone", "--quiet", f.super.Dir, clone)
	r, err := core.Open(t.Context(), f.g, clone)
	if err != nil {
		t.Fatal(err)
	}
	before := treeState(t, clone)
	_, err = r.Update(t.Context(), core.UpdateOptions{})
	wantErr(t, "Update(no repository)", err, core.ErrUninitialized)
	wantSameTree(t, "refused update", before, treeState(t, clone))

	var progress strings.Builder
	res, err := r.Update(t.Context(), core.UpdateOptions{Fetch: true, Progress: &progress})
	if err != nil {
		t.Fatal(err)
	}
	c := oneChange(t, res)
	wantSteps(t, c, true, true)
	if c.New.Commit != f.commits[gittest.TagV101] || c.Old == nil || c.Old.Commit != v100 {
		t.Errorf("change %+v", c)
	}
	if got := headOf(t, filepath.Join(clone, "lib")); got != c.New.Commit {
		t.Errorf("HEAD %s, want %s", got, c.New.Commit)
	}
	if !strings.Contains(progress.String(), "Cloning into") {
		t.Errorf("progress %q", progress.String())
	}
	wantStaged(t, clone, lock.File, "lib")
}

func TestUpdateFetchMovedTag(t *testing.T) {
	t.Parallel()
	for _, format := range formats() {
		t.Run(format, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t, format)
			v101, tip := f.commits[gittest.TagV101], f.commits[gittest.TagV200RC]
			f.track("lib", manifest.ModeTag, gittest.TagV101, gittest.TagV101, v101)
			f.track("dev", manifest.ModeBranch, "main", "main", tip)
			f.up.MoveTag(t, gittest.TagV101, tip)
			newer := f.up.Commit(t, "newer")

			// Without Fetch, the local refs are used.
			res := f.mustUpdate(core.UpdateOptions{})
			for _, c := range res.Changes {
				if c.Changed() && c.Submodule.Name == "lib" {
					t.Errorf("update without fetch changed %+v", c)
				}
			}
			res = f.mustUpdate(core.UpdateOptions{Fetch: true})
			if len(res.Changes) != 2 || res.Changes[0].New.Commit != tip ||
				res.Changes[1].New.Commit != newer {
				t.Fatalf("Update(fetch) = %+v", res)
			}
			if got := lockOf(t, f, "lib"); got == nil || got.Commit != tip {
				t.Errorf("lock entry %+v", got)
			}
			if got := headOf(t, f.dir("lib")); got != tip {
				t.Errorf("HEAD %s, want %s", got, tip)
			}
			wantState(t, f.status("lib"), core.StateOK, "")
			wantState(t, f.status("dev"), core.StateOK, "")
		})
	}
}

func TestUpdateNativeBranchKey(t *testing.T) {
	t.Parallel()
	f := newFixture(t, gittest.SHA1)
	v101 := f.commits[gittest.TagV101]
	f.track("tagged", manifest.ModeTag, gittest.TagV101, gittest.TagV101, v101)
	f.super.SetKey(t, "tagged", manifest.KeyBranch, "main")
	f.track("follow", manifest.ModeBranch, gittest.BranchStable, gittest.BranchStable, v101)
	f.track("kept", manifest.ModeBranch, gittest.BranchStable, gittest.BranchStable, v101)
	f.super.SetKey(t, "kept", manifest.KeyBranch, gittest.BranchStable)
	f.super.Commit(t, "branch keys")
	lockBefore := lockOf(t, f, "tagged")

	res := f.mustUpdate(core.UpdateOptions{})
	var changed []string
	for _, c := range res.Changes {
		if c.Changed() {
			changed = append(changed, c.Submodule.Name)
		}
	}
	if !slices.Equal(changed, []string{"tagged", "follow"}) {
		t.Errorf("changed %q", changed)
	}
	for name, want := range map[string]string{
		"tagged": "", "follow": gittest.BranchStable, "kept": gittest.BranchStable,
	} {
		if got := gitmodulesKey(t, f.super.Dir, name, manifest.KeyBranch); got != want {
			t.Errorf("%s: native branch %q, want %q", name, got, want)
		}
	}
	wantStaged(t, f.super.Dir, manifest.File)
	if got := lockOf(t, f, "tagged"); *got != *lockBefore {
		t.Errorf("lock entry %+v changed", got)
	}
	msg := core.CommitMessage(res.Changes)
	if !strings.HasPrefix(msg, "manifest: update 2 submodules\n") {
		t.Errorf("CommitMessage =\n%s", msg)
	}
	if res := f.mustUpdate(core.UpdateOptions{}); slices.ContainsFunc(res.Changes,
		core.Change.Changed) {
		t.Errorf("second update changed %+v", res.Changes)
	}
}

func TestUpdateNeverDowngrades(t *testing.T) {
	t.Parallel()
	f := newFixture(t, gittest.SHA1)
	rc := f.commits[gittest.TagV200RC]
	f.add("sdk", manifest.ModeTagPattern, "v*")
	target := func(opts core.UpdateOptions) core.Change {
		t.Helper()
		return oneChange(t, f.mustUpdate(opts))
	}
	if c := target(core.UpdateOptions{IncludePrerelease: true}); c.New.Ref != gittest.TagV200RC ||
		c.New.Commit != rc {
		t.Fatalf("Update(prerelease) = %+v", c)
	}
	if c := target(core.UpdateOptions{Fetch: true}); c.Changed() {
		t.Errorf("Update left the locked pre-release: %+v", c)
	}
	wantState(t, f.status("sdk"), core.StateOK, "")

	// A newer pre-release needs the option; a release is taken.
	f.up.Tag(t, "v2.0.0-rc.2", f.up.Commit(t, "second candidate"))
	if c := target(core.UpdateOptions{Fetch: true}); c.Changed() {
		t.Errorf("Update took a newer pre-release: %+v", c)
	}
	release := f.up.Commit(t, "release")
	f.up.AnnotatedTag(t, "v2.0.0", release, "release")
	if c := target(core.UpdateOptions{Fetch: true}); c.New.Ref != "v2.0.0" ||
		c.New.Commit != release || c.Old.Ref != gittest.TagV200RC {
		t.Errorf("Update(release) = %+v", c)
	}
	if got := lockOf(t, f, "sdk"); got.Ref != "v2.0.0" || got.Commit != release {
		t.Errorf("lock entry %+v", got)
	}
}

func TestUpdateRestoresAfterFailedCheckout(t *testing.T) {
	t.Parallel()
	f := newFixture(t, gittest.SHA1)
	v100, v101 := f.commits[gittest.TagV100], f.commits[gittest.TagV101]
	f.track("a", manifest.ModeTagPattern, "v1.*", gittest.TagV100, v100)
	f.track("i", manifest.ModeTagPattern, "v1.*", gittest.TagV100, v100)
	f.track("b", manifest.ModeTag, gittest.TagV101, gittest.TagV101, v101)
	f.super.Deinit(t, "i")
	// The next release adds a file that exists, untracked, in b.
	gittest.WriteFile(t, filepath.Join(f.up.Work, "extra.txt"), "tracked\n")
	f.up.Tag(t, "v3.0.0", f.up.Commit(t, "add extra"))
	if err := f.g.Fetch(t.Context(), f.dir("b"), "origin", nil); err != nil {
		t.Fatal(err)
	}
	f.configure("b", manifest.ModeTag, "v3.0.0")
	gittest.WriteFile(t, filepath.Join(f.dir("b"), "extra.txt"), "untracked\n")
	wantState(t, f.status("b"), core.StateBehind, "")
	lockFile, err := os.ReadFile(filepath.Join(f.super.Dir, lock.File))
	if err != nil {
		t.Fatal(err)
	}

	_, err = f.update(core.UpdateOptions{})
	gitErr, ok := errors.AsType[*git.Error](err)
	if !ok || !strings.HasPrefix(err.Error(), "b: git ") ||
		!strings.Contains(gitErr.Stderr, "untracked working tree files") {
		t.Fatalf("Update = %v, want the checkout failure of b", err)
	}
	// The initialized submodule stays initialized, at its gitlink.
	for _, name := range []string{"a", "i"} {
		if got := headOf(t, f.dir(name)); got != v100 {
			t.Errorf("%s was not restored: HEAD %s, want %s", name, got, v100)
		}
	}
	if got := headOf(t, f.dir("b")); got != v101 {
		t.Errorf("b moved to %s", got)
	}
	if got, err := os.ReadFile(filepath.Join(f.super.Dir, lock.File)); err != nil ||
		string(got) != string(lockFile) {
		t.Errorf("lock file changed:\n%s", got)
	}
	wantStaged(t, f.super.Dir)
}

func TestUpdateUpToDateStagesNothing(t *testing.T) {
	t.Parallel()
	f := newFixture(t, gittest.SHA1)
	v101 := f.commits[gittest.TagV101]
	f.track("lib", manifest.ModeTag, gittest.TagV101, gittest.TagV101, v101)
	f.super.SetKey(t, "lib", "update", "checkout")
	f.lock("stale", manifest.ModeTag, gittest.TagV100, f.commits[gittest.TagV100])
	before := treeState(t, f.super.Dir)
	res := f.mustUpdate(core.UpdateOptions{})
	if c := oneChange(t, res); c.Changed() || res.Commit != "" {
		t.Errorf("Update = %+v", res)
	}
	wantSameTree(t, "update without changes", before, treeState(t, f.super.Dir))
	wantStaged(t, f.super.Dir)

	// A commit would include the entry of a submodule that is not selected.
	_, err := f.update(core.UpdateOptions{Commit: true})
	wantErr(t, "Update(commit)", err, core.ErrUnrelatedStaged)
	wantSameTree(t, "refused update", before, treeState(t, f.super.Dir))
	gittest.Git(t, f.super.Dir, "commit", "--quiet", "-m", "stale entry", "--", lock.File)
	head := headOf(t, f.super.Dir)
	before = treeState(t, f.super.Dir)
	res = f.mustUpdate(core.UpdateOptions{Commit: true})
	if c := oneChange(t, res); c.Changed() || res.Commit != "" || headOf(t, f.super.Dir) != head {
		t.Errorf("Update(commit) = %+v", res)
	}
	wantSameTree(t, "update with commit without changes", before, treeState(t, f.super.Dir))
	wantStaged(t, f.super.Dir)
}

func TestUpdateSelection(t *testing.T) {
	t.Parallel()
	f := newFixture(t, gittest.SHA1)
	f.add("plain", "", "")
	before := treeState(t, f.super.Dir)
	for _, opts := range []core.UpdateOptions{{}, {Commit: true, Fetch: true}} {
		res, err := f.update(opts)
		if err != nil || len(res.Changes) != 0 || res.Commit != "" {
			t.Errorf("Update(%+v) without managed submodules = %+v, %v", opts, res, err)
		}
	}
	_, err := f.update(core.UpdateOptions{Names: []string{"plain"}})
	wantErr(t, "Update(unmanaged)", err, core.ErrUnmanaged, core.ErrRefused)
	_, err = f.update(core.UpdateOptions{Names: []string{"nope", "plain"}})
	wantErr(t, "Update(unknown)", err, core.ErrNotFound)
	wantSameTree(t, "update of unmanaged submodules", before, treeState(t, f.super.Dir))

	f.add("lib", manifest.ModeTag, gittest.TagV100)
	f.add("other", manifest.ModeTag, gittest.TagV100)
	res := f.mustUpdate(core.UpdateOptions{Names: []string{"other", "lib", "other"}})
	if len(res.Changes) != 2 || res.Changes[0].Submodule.Name != "lib" ||
		res.Changes[1].Submodule.Name != "other" {
		t.Errorf("Update(names) = %+v", res)
	}

	empty := gittest.NewSuper(t, gittest.SHA1)
	r, err := core.Open(t.Context(), f.g, empty.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if res, err := r.Update(t.Context(), core.UpdateOptions{Commit: true}); err != nil ||
		len(res.Changes) != 0 {
		t.Errorf("Update without .gitmodules = %+v, %v", res, err)
	}
}

func TestUpdateNestedSubmodule(t *testing.T) {
	t.Parallel()
	inner := gittest.NewUpstream(t, gittest.SHA1)
	outer := gittest.NewUpstream(t, gittest.SHA1)
	first := outer.AddSubmodule(t, "inner", inner)
	outer.Tag(t, "v1.0.0", first)
	super := gittest.NewSuper(t, gittest.SHA1)
	f := &fixture{t: t, up: outer, super: super, g: gittest.Runner(t)}
	f.track("outer", manifest.ModeTagPattern, "v1.*", "v1.0.0", first)
	nested := filepath.Join(f.dir("outer"), "inner")
	gittest.Git(t, f.dir("outer"), "submodule", "update", "--init", "--quiet")
	newer := inner.Commit(t, "newer inner")
	gittest.Git(t, nested, "fetch", "--quiet", "origin")
	gittest.Git(t, nested, "checkout", "--quiet", "--detach", newer)
	second := outer.Commit(t, "second")
	outer.Tag(t, "v1.1.0", second)

	c := oneChange(t, f.mustUpdate(core.UpdateOptions{Fetch: true}))
	if c.New.Ref != "v1.1.0" || c.New.Commit != second {
		t.Errorf("Update = %+v", c)
	}
	// The nested submodule is left alone.
	if got := headOf(t, nested); got != newer {
		t.Errorf("nested submodule moved to %s", got)
	}
	wantState(t, f.status("outer"), core.StateOK, "")
	wantStaged(t, f.super.Dir, lock.File, "outer")
}
