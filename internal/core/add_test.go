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

// newAddFixture returns a fixture whose superproject has no submodule yet.
func newAddFixture(t *testing.T, format string) *fixture {
	t.Helper()
	return newFixture(t, format)
}

// addSub runs Add with the fixture upstream.
func (f *fixture) addSub(p string, mode manifest.Mode, ref string, pre bool) (
	core.Change, error,
) {
	f.t.Helper()
	return f.repo().Add(f.t.Context(), core.AddOptions{
		URL: f.up.Bare, Path: p, Mode: mode, Ref: ref, IncludePrerelease: pre,
	})
}

// superState describes a superproject for the rollback tests: every file
// except the index, whose content is compared through ls-files, and the
// object database, where "git add" leaves unreachable objects.
type superState struct {
	tree  map[string]string
	index string
}

// captureSuper returns the state of a superproject.
func captureSuper(t *testing.T, dir string) superState {
	t.Helper()
	return superState{
		tree:  treeState(t, dir, ".git/index", ".git/objects"),
		index: gittest.Git(t, dir, "ls-files", "--stage"),
	}
}

// wantSameSuper checks that nothing of a superproject changed.
func wantSameSuper(t *testing.T, what, dir string, before superState) {
	t.Helper()
	after := captureSuper(t, dir)
	wantSameTree(t, what, before.tree, after.tree)
	if after.index != before.index {
		t.Errorf("%s changed the index:\n%s\nwant\n%s", what, after.index, before.index)
	}
}

func TestAddModes(t *testing.T) {
	t.Parallel()
	for _, format := range formats() {
		for _, c := range []struct {
			name   string
			mode   manifest.Mode
			ref    func(f *fixture) string
			pre    bool
			tag    string
			stored func(f *fixture) string
			branch string
		}{
			{"branch", manifest.ModeBranch, fixed(gittest.BranchStable), false, gittest.TagV101,
				fixed(gittest.BranchStable), gittest.BranchStable},
			{"tag", manifest.ModeTag, fixed(gittest.TagV100), false, gittest.TagV100,
				fixed(gittest.TagV100), ""},
			{"tag pattern", manifest.ModeTagPattern, fixed("v*"), false, gittest.TagV101,
				fixed("v*"), ""},
			{"pre-release", manifest.ModeTagPattern, fixed("v*"), true, gittest.TagV200RC,
				fixed("v*"), ""},
			{"commit", manifest.ModeCommit, abbreviated(gittest.TagV100), false,
				gittest.TagV100, full(gittest.TagV100), ""},
		} {
			t.Run(format+"/"+c.name, func(t *testing.T) {
				t.Parallel()
				f := newAddFixture(t, format)
				f.add("lib", manifest.ModeTag, gittest.TagV101)
				const p = "libs/new"
				target := core.Resolution{Mode: c.mode, Ref: c.tag, Commit: f.commits[c.tag]}
				switch c.mode {
				case manifest.ModeBranch:
					target.Ref = c.ref(f)
				case manifest.ModeCommit:
					target.Ref = target.Commit
				}

				change, err := f.addSub("./libs//new/", c.mode, c.ref(f), c.pre)
				if err != nil {
					t.Fatal(err)
				}
				wantSub := manifest.Submodule{Name: p, Path: p, URL: f.up.Bare, Mode: c.mode,
					Ref: c.stored(f), Branch: c.branch}
				if change.Submodule != wantSub || change.New != target ||
					change.Old != nil || change.OldHead != "" || !change.Changed() {
					t.Errorf("change %+v\nwant submodule %+v, target %+v", change, wantSub, target)
				}
				wantSteps(t, change, true, true)
				wantTracking(t, f, p, c.mode, c.stored(f), c.branch)
				wantLock := lock.Entry{Name: p, Mode: c.mode, Ref: target.Ref, Commit: target.Commit}
				if got := lockOf(t, f, p); got == nil || *got != wantLock {
					t.Errorf("lock entry %+v, want %+v", got, wantLock)
				}
				dir := filepath.Join(f.super.Dir, "libs", "new")
				if got := gittest.Git(t, dir, "rev-parse", "--abbrev-ref", "HEAD"); got != "HEAD" {
					t.Errorf("HEAD is attached to %s", got)
				}
				if got := headOf(t, dir); got != target.Commit {
					t.Errorf("HEAD %s, want %s", got, target.Commit)
				}
				// "git submodule add -b" creates the local branch.
				_, err = f.g.Run(t.Context(), dir, "rev-parse", "--verify", "--quiet",
					"refs/heads/"+gittest.BranchStable)
				if (err == nil) != (c.mode == manifest.ModeBranch) {
					t.Errorf("local branch %s: %v", gittest.BranchStable, err)
				}
				wantStaged(t, f.super.Dir, manifest.File, lock.File, p)
				indexMatchesWorktree(t, f.super.Dir, manifest.File, lock.File)
				if got := gitlinkAt(t, f.super.Dir, "", p); got != target.Commit {
					t.Errorf("gitlink %s, want %s", got, target.Commit)
				}
				if st := f.status(p); st.State != core.StateOK {
					t.Errorf("status %s (%s)", st.State, st.Reason)
				}
				// An update with Commit commits the added submodule, whose
				// gitlink HEAD does not record yet, with the update of lib.
				res := f.mustUpdate(core.UpdateOptions{IncludePrerelease: c.pre, Commit: true})
				if len(res.Changes) != 2 || !res.Changes[1].Changed() ||
					res.Changes[1].OldGitlink != "" || res.Changes[1].New != target {
					t.Errorf("update after add: %+v", res)
				}
				msg := headMessage(t, f.super.Dir)
				lintMessage(t, msg)
				if !strings.HasPrefix(msg, "manifest: update 2 submodules\n") ||
					!strings.Contains(msg, "\nSubmodule \"libs/new\":\n") ||
					!strings.Contains(msg, "\n  Old: none\n") {
					t.Errorf("commit message:\n%s", msg)
				}
				if got := gitlinkAt(t, f.super.Dir, "HEAD", p); got != target.Commit {
					t.Errorf("committed gitlink %s, want %s", got, target.Commit)
				}
				wantStaged(t, f.super.Dir)
			})
		}
	}
}

func TestAddStaleLockEntry(t *testing.T) {
	t.Parallel()
	f := newAddFixture(t, gittest.SHA1)
	v100 := f.commits[gittest.TagV100]
	target := core.Resolution{Mode: manifest.ModeTag, Ref: gittest.TagV100, Commit: v100}
	// The index records an entry for the path, and the working tree
	// another one.
	stale := lock.Entry{Name: "new", Mode: manifest.ModeTag, Ref: "v0",
		Commit: f.commits[gittest.TagRC1]}
	f.lock(stale.Name, stale.Mode, stale.Ref, stale.Commit)
	gittest.Git(t, f.super.Dir, "add", lock.File)
	f.lock("new", manifest.ModeBranch, "main", f.commits[gittest.TagV200RC])

	change, err := f.addSub("new", manifest.ModeTag, gittest.TagV100, false)
	if err != nil || change.Old == nil || *change.Old != stale || change.New != target ||
		!change.Changed() {
		t.Errorf("Add = %+v, %v; want the old entry %+v", change, err, stale)
	}
	want := lock.Entry{Name: "new", Mode: manifest.ModeTag, Ref: gittest.TagV100, Commit: v100}
	if got := lockOf(t, f, "new"); got == nil || *got != want {
		t.Errorf("lock entry %+v, want %+v", got, want)
	}
	indexMatchesWorktree(t, f.super.Dir, lock.File)
}

func TestAddRefused(t *testing.T) {
	t.Parallel()
	f := newAddFixture(t, gittest.SHA1)
	f.add("lib", manifest.ModeTag, gittest.TagV101)
	gittest.Git(t, f.super.Dir, "submodule", "add", "--quiet", "--name", "named", "--",
		f.up.Bare, "elsewhere")
	gittest.Git(t, f.super.Dir, "submodule", "add", "--quiet", "--name", "deep", "--",
		f.up.Bare, "deep/a")
	f.super.Commit(t, "more submodules")
	if err := os.Symlink(t.TempDir(), filepath.Join(f.super.Dir, "link")); err != nil {
		t.Fatal(err)
	}
	gittest.Git(t, f.super.Dir, "update-index", "--add", "--cacheinfo",
		"160000,"+f.commits[gittest.TagV100]+",ghost")
	// A submodule whose directory does not exist.
	f.super.SetKey(t, "phantom", manifest.KeyPath, "outer/inner")
	f.super.SetKey(t, "phantom", manifest.KeyURL, f.up.Bare)
	before := captureSuper(t, f.super.Dir)
	tag := manifest.ModeTag
	for _, c := range []struct {
		url, path string
		mode      manifest.Mode
		ref       string
		want      error
	}{
		{"", "x", tag, "v1", core.ErrInvalidArgument},
		{"-uhttps://example.org", "x", tag, "v1", core.ErrInvalidArgument},
		{"https://example.org/\n", "x", tag, "v1", core.ErrInvalidArgument},
		{f.up.Bare, "", tag, "v1", core.ErrInvalidArgument},
		{f.up.Bare, ".", tag, "v1", core.ErrInvalidArgument},
		{f.up.Bare, "../x", tag, "v1", core.ErrInvalidArgument},
		{f.up.Bare, "/abs", tag, "v1", core.ErrInvalidArgument},
		{f.up.Bare, ".git/x", tag, "v1", core.ErrInvalidArgument},
		{f.up.Bare, "x/.GIT", tag, "v1", core.ErrInvalidArgument},
		{f.up.Bare, "-x", tag, "v1", core.ErrInvalidArgument},
		{f.up.Bare, "x\x1b[31m", tag, "v1", core.ErrInvalidArgument},
		{f.up.Bare, "x", manifest.Mode("tags"), "v1", core.ErrInvalidArgument},
		{f.up.Bare, "x", manifest.ModeBranch, "a..b", core.ErrInvalidArgument},
		{f.up.Bare, "x", manifest.ModeCommit, "HEAD", core.ErrInvalidArgument},
		{f.up.Bare, "x", tag, "-v1", core.ErrInvalidArgument},
		{f.up.Bare, "lib/inner", tag, "v1", core.ErrInvalidArgument},
		{f.up.Bare, "link/x", tag, "v1", core.ErrInvalidArgument},
		{f.up.Bare, "README/x", tag, "v1", core.ErrInvalidArgument},
		{f.up.Bare, "README", tag, "v1", core.ErrPathExists},
		{f.up.Bare, "link", tag, "v1", core.ErrPathExists},
		{f.up.Bare, "lib", tag, "v1", core.ErrPathExists},
		{f.up.Bare, "lib/", tag, "v1", core.ErrPathExists},
		{f.up.Bare, "named", tag, "v1", core.ErrPathExists},
		{f.up.Bare, "elsewhere", tag, "v1", core.ErrPathExists},
		{f.up.Bare, "deep", tag, "v1", core.ErrPathExists},
		{f.up.Bare, "ghost", tag, "v1", core.ErrPathExists},
		{f.up.Bare, "outer", tag, "v1", core.ErrPathExists},
		{f.up.Bare, "outer/inner/x", tag, "v1", core.ErrInvalidArgument},
	} {
		change, err := f.repo().Add(t.Context(), core.AddOptions{
			URL: c.url, Path: c.path, Mode: c.mode, Ref: c.ref,
		})
		wantErr(t, "Add("+c.path+")", err, c.want)
		if change != (core.Change{}) {
			t.Errorf("Add(%s) returned %+v", c.path, change)
		}
	}
	wantSameSuper(t, "refused add", f.super.Dir, before)

	// A broken lock file is reported before anything is cloned.
	gittest.WriteFile(t, filepath.Join(f.super.Dir, lock.File), "[submodule \"x\"]\n\tmode = x\n")
	before = captureSuper(t, f.super.Dir)
	_, err := f.addSub("new", tag, gittest.TagV100, false)
	wantErr(t, "Add(broken lock)", err, lock.ErrInvalidEntry)
	wantSameSuper(t, "add with a broken lock", f.super.Dir, before)
}

func TestAddRollback(t *testing.T) {
	t.Parallel()
	tag := manifest.ModeTag
	for _, c := range []struct {
		name  string
		setup func(f *fixture)
		path  string
		mode  manifest.Mode
		ref   string
		want  error
	}{
		{name: "first submodule", path: "libs/new", mode: tag, ref: "v9",
			want: core.ErrMissingRef},
		{name: "second submodule", path: "new", mode: tag, ref: "v9",
			setup: func(f *fixture) { f.add("lib", tag, gittest.TagV101) },
			want:  core.ErrMissingRef},
		{name: "missing branch", path: "a/b/c", mode: manifest.ModeBranch, ref: "nope"},
		{name: "missing commit", path: "new", mode: manifest.ModeCommit, ref: "1234567",
			want: core.ErrMissingRef},
		{name: "only pre-releases", path: "new", mode: manifest.ModeTagPattern, ref: "v2.*",
			want: core.ErrMissingRef},
		{name: "stale lock entry", path: "new", mode: tag, ref: "v9",
			setup: func(f *fixture) {
				f.lock("new", tag, gittest.TagV100, f.commits[gittest.TagV100])
			},
			want: core.ErrMissingRef},
		{name: "lock file locked", path: "new", mode: tag, ref: gittest.TagV100,
			setup: func(f *fixture) {
				gittest.WriteFile(f.t, filepath.Join(f.super.Dir, ".lsm.lock.lock"), "")
			}},
		{name: "lock file locked with other entries", path: "new", mode: tag,
			ref: gittest.TagV100,
			setup: func(f *fixture) {
				f.add("lib", tag, gittest.TagV101)
				f.pin("lib", tag, gittest.TagV101, f.commits[gittest.TagV101])
				gittest.WriteFile(f.t, filepath.Join(f.super.Dir, ".lsm.lock.lock"), "")
			}},
		{name: "staging fails", path: "libs/new", mode: tag, ref: gittest.TagV100,
			setup: sparseLock},
		{name: "staging fails with a stale lock entry", path: "new", mode: tag,
			ref: gittest.TagV100,
			setup: func(f *fixture) {
				f.add("lib", tag, gittest.TagV101)
				f.lock("new", tag, "v0", f.commits[gittest.TagRC1])
				sparseLock(f)
			}},
		{name: "existing module repository", path: "new", mode: tag, ref: gittest.TagV100,
			setup: func(f *fixture) {
				f.super.AddSubmodule(f.t, "new", f.up)
				gittest.Git(f.t, f.super.Dir, "rm", "--quiet", "-f", "--", "new")
				f.super.Commit(f.t, "remove new")
				gittest.Git(f.t, f.super.Dir, "config", "--remove-section", "submodule.new")
			}},
		{name: "configured submodule", path: "new", mode: tag, ref: "v9",
			setup: func(f *fixture) {
				gittest.Git(f.t, f.super.Dir, "config", "submodule.new.fetchRecurseSubmodules",
					"false")
			},
			want: core.ErrMissingRef},
	} {
		for _, format := range formats() {
			t.Run(format+"/"+c.name, func(t *testing.T) {
				t.Parallel()
				f := newAddFixture(t, format)
				if c.setup != nil {
					c.setup(f)
				}
				before := captureSuper(t, f.super.Dir)
				change, err := f.addSub(c.path, c.mode, c.ref, false)
				if c.want != nil {
					wantErr(t, "Add", err, c.want)
				} else if _, ok := errors.AsType[*git.Error](err); !ok {
					t.Errorf("Add = %v, want a *git.Error", err)
				}
				if err != nil && strings.Contains(err.Error(), "roll back") {
					t.Errorf("rollback failed: %v", err)
				}
				if change != (core.Change{}) {
					t.Errorf("Add returned %+v", change)
				}
				wantSameSuper(t, "failed add", f.super.Dir, before)
			})
		}
	}
}

// sparseLock excludes the lock file from a sparse checkout, so that git
// refuses to stage it; staging other files and removing the submodule still
// work.
func sparseLock(f *fixture) {
	f.t.Helper()
	gittest.WriteFile(f.t, filepath.Join(f.super.Dir, ".git", "info", "sparse-checkout"),
		"/*\n!/"+lock.File+"\n")
	gittest.Git(f.t, f.super.Dir, "config", "core.sparseCheckout", "true")
}

func TestAddRollbackPathConfig(t *testing.T) {
	t.Parallel()
	up, commits := gittest.NewTaggedUpstream(t, gittest.SHA1)
	dir := t.TempDir()
	gittest.Git(t, dir, "-c", "init.defaultSubmodulePathConfig=true", "init", "--quiet", dir)
	if gittest.Git(t, dir, "config", "--get", "--default=",
		"extensions.submodulePathConfig") == "" {
		t.Skip("git does not support extensions.submodulePathConfig")
	}
	gittest.WriteFile(t, filepath.Join(dir, "README"), "superproject\n")
	gittest.Git(t, dir, "add", "README")
	gittest.Git(t, dir, "commit", "--quiet", "-m", "initial commit")
	f := &fixture{t: t, up: up, commits: commits, super: &gittest.Super{Dir: dir},
		g: gittest.Runner(t)}
	before := captureSuper(t, dir)
	for _, p := range []string{"libs/new", "new"} {
		_, err := f.addSub(p, manifest.ModeTag, "v9", false)
		wantErr(t, "Add("+p+")", err, core.ErrMissingRef)
		wantSameSuper(t, "failed add", dir, before)
	}
	change, err := f.addSub("libs/new", manifest.ModeTag, gittest.TagV100, false)
	if err != nil || change.New.Commit != commits[gittest.TagV100] {
		t.Errorf("Add = %+v, %v", change, err)
	}
}
