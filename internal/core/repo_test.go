// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package core_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/FPGArtktic/lazysubmodules/internal/core"
	"github.com/FPGArtktic/lazysubmodules/internal/git"
	"github.com/FPGArtktic/lazysubmodules/internal/git/gittest"
	"github.com/FPGArtktic/lazysubmodules/internal/lock"
	"github.com/FPGArtktic/lazysubmodules/internal/manifest"
)

// realDir resolves symbolic links in dir.
func realDir(t *testing.T, dir string) string {
	t.Helper()
	res, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestOpen(t *testing.T) {
	t.Parallel()
	for _, format := range formats() {
		t.Run(format, func(t *testing.T) {
			t.Parallel()
			super := gittest.NewSuper(t, format)
			sub := filepath.Join(super.Dir, "a", "b")
			if err := os.MkdirAll(sub, 0o755); err != nil {
				t.Fatal(err)
			}
			for _, dir := range []string{super.Dir, sub} {
				r, err := core.Open(t.Context(), gittest.Runner(t), dir)
				if err != nil {
					t.Fatalf("Open(%s) = %v", dir, err)
				}
				if realDir(t, r.Root()) != realDir(t, super.Dir) {
					t.Errorf("Open(%s).Root() = %s, want %s", dir, r.Root(), super.Dir)
				}
			}
		})
	}
}

func TestOpenInsideSubmodule(t *testing.T) {
	t.Parallel()
	f := newFixture(t, gittest.SHA1)
	f.track("lib", manifest.ModeTag, gittest.TagV100, gittest.TagV100, f.commits[gittest.TagV100])
	// As for git commands, a checked-out submodule is a repository of its own,
	// here one without submodules.
	r, err := core.Open(t.Context(), f.g, f.dir("lib"))
	if err != nil {
		t.Fatal(err)
	}
	if realDir(t, r.Root()) != realDir(t, f.dir("lib")) {
		t.Errorf("Root() = %s, want the submodule", r.Root())
	}
	subs, err := r.Submodules(t.Context())
	if err != nil || len(subs) != 0 {
		t.Errorf("Submodules = %+v, %v", subs, err)
	}
	st, err := r.Status(t.Context(), nil)
	if err != nil || len(st) != 0 {
		t.Errorf("Status = %+v, %v", st, err)
	}
	res, err := r.Update(t.Context(), core.UpdateOptions{Fetch: true, Commit: true})
	if err != nil || len(res.Changes) != 0 || res.Commit != "" {
		t.Errorf("Update = %+v, %v", res, err)
	}
}

func TestOpenLinkedWorktree(t *testing.T) {
	t.Parallel()
	f := newFixture(t, gittest.SHA1)
	v100, v101 := f.commits[gittest.TagV100], f.commits[gittest.TagV101]
	f.track("lib", manifest.ModeTagPattern, "v1.*", gittest.TagV100, v100)
	wt := filepath.Join(t.TempDir(), "wt")
	gittest.Git(t, f.super.Dir, "worktree", "add", "--quiet", "--detach", wt)
	// The object database is shared: staging in the worktree adds objects.
	skip := []string{".git/worktrees", ".git/objects"}
	main := treeState(t, f.super.Dir, skip...)

	// The submodule directory is empty in the new worktree; Open finds the
	// top level of the worktree from there.
	r, err := core.Open(t.Context(), f.g, filepath.Join(wt, "lib"))
	if err != nil {
		t.Fatal(err)
	}
	if realDir(t, r.Root()) != realDir(t, wt) {
		t.Errorf("Root() = %s, want %s", r.Root(), wt)
	}
	// Each worktree has its own submodule repositories.
	st, err := r.Status(t.Context(), nil)
	if err != nil || len(st) != 1 {
		t.Fatalf("Status = %+v, %v", st, err)
	}
	wantState(t, st[0], core.StateUninitialized, "not checked out")
	if st[0].Lock == nil || st[0].Lock.Commit != v100 || st[0].Target != nil {
		t.Errorf("lock %+v, target %+v", st[0].Lock, st[0].Target)
	}
	_, err = r.Update(t.Context(), core.UpdateOptions{})
	wantErr(t, "Update(offline)", err, core.ErrUninitialized)

	c := oneChange(t, mustUpdateRepo(t, r, core.UpdateOptions{Fetch: true}))
	wantSteps(t, c, true, true)
	if c.New.Commit != v101 || headOf(t, filepath.Join(wt, "lib")) != v101 {
		t.Errorf("Update(fetch) = %+v", c)
	}
	wantStaged(t, wt, lock.File, "lib")
	st, err = r.Status(t.Context(), nil)
	if err != nil || len(st) != 1 || st[0].State != core.StateOK {
		t.Errorf("Status after update = %+v, %v", st, err)
	}
	// The main worktree is left alone.
	wantSameTree(t, "update in a linked worktree", main, treeState(t, f.super.Dir, skip...))
	wantState(t, f.status("lib"), core.StateBehind, "")
}

func TestOpenNestedSuperproject(t *testing.T) {
	t.Parallel()
	f := newFixture(t, gittest.SHA1)
	v100, v101 := f.commits[gittest.TagV100], f.commits[gittest.TagV101]
	f.track("lib", manifest.ModeTagPattern, "v1.*", gittest.TagV100, v100)
	// A clone of the superproject inside the working tree of another
	// repository, which does not track it.
	outer := gittest.NewSuper(t, gittest.SHA1)
	inner := filepath.Join(outer.Dir, "vendor", "super")
	gittest.Git(t, outer.Dir, "clone", "--quiet", f.super.Dir, inner)
	before := treeState(t, outer.Dir, "vendor")

	r, err := core.Open(t.Context(), f.g, filepath.Join(inner, "lib"))
	if err != nil {
		t.Fatal(err)
	}
	if realDir(t, r.Root()) != realDir(t, inner) {
		t.Errorf("Root() = %s, want %s", r.Root(), inner)
	}
	c := oneChange(t, mustUpdateRepo(t, r, core.UpdateOptions{Fetch: true, Commit: true}))
	wantSteps(t, c, true, true)
	if c.New.Commit != v101 || headOf(t, filepath.Join(inner, "lib")) != v101 {
		t.Errorf("Update = %+v", c)
	}
	if got := gitlinkAt(t, inner, "HEAD", "lib"); got != v101 {
		t.Errorf("committed gitlink %s, want %s", got, v101)
	}
	st, err := r.Status(t.Context(), nil)
	if err != nil || len(st) != 1 || st[0].State != core.StateOK {
		t.Errorf("Status = %+v, %v", st, err)
	}
	// The outer repository is left alone.
	wantSameTree(t, "update in a nested superproject", before,
		treeState(t, outer.Dir, "vendor"))
	if got := gittest.Git(t, outer.Dir, "status", "--porcelain"); got != "?? vendor/" {
		t.Errorf("outer status %q", got)
	}
}

// mustUpdateRepo runs Update on r and fails the test on an error.
func mustUpdateRepo(t *testing.T, r *core.Repo, opts core.UpdateOptions) core.UpdateResult {
	t.Helper()
	res, err := r.Update(t.Context(), opts)
	if err != nil {
		t.Fatalf("Update(%+v): %v", opts, err)
	}
	return res
}

func TestOpenErrors(t *testing.T) {
	t.Parallel()
	g := gittest.Runner(t)
	up := gittest.NewUpstream(t, gittest.SHA1)
	for name, dir := range map[string]string{
		"outside a repository": t.TempDir(),
		"bare repository":      up.Bare,
		"missing directory":    filepath.Join(t.TempDir(), "missing"),
	} {
		r, err := core.Open(t.Context(), g, dir)
		if _, ok := errors.AsType[*git.Error](err); !ok || r != nil {
			t.Errorf("Open(%s) = %v, %v; want *git.Error", name, r, err)
		}
	}
}

func TestSubmodules(t *testing.T) {
	t.Parallel()
	f := newFixture(t, gittest.SHA1)
	f.add("lib", manifest.ModeTagPattern, "v1.*")
	f.add("plain", "", "")
	subs, err := f.repo().Submodules(t.Context())
	want := []manifest.Submodule{
		{Name: "lib", Path: "lib", URL: f.up.Bare, Mode: manifest.ModeTagPattern, Ref: "v1.*"},
		{Name: "plain", Path: "plain", URL: f.up.Bare},
	}
	if err != nil || len(subs) != len(want) || subs[0] != want[0] || subs[1] != want[1] {
		t.Errorf("Submodules = %+v, %v; want %+v", subs, err, want)
	}
}
