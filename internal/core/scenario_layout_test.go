// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package core_test

import (
	"fmt"
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

// samePath reports whether two existing paths name the same directory.
func samePath(t *testing.T, a, b string) bool {
	t.Helper()
	same, err := git.SamePath(a, b)
	if err != nil {
		t.Fatal(err)
	}
	return same
}

// wantRoot checks the top level of a Repo.
func wantRoot(t *testing.T, r *core.Repo, want string) {
	t.Helper()
	if !samePath(t, r.Root(), want) {
		t.Errorf("Root() = %s, want %s", r.Root(), want)
	}
}

func TestScenarioLinkedWorktree(t *testing.T) {
	t.Parallel()
	for _, format := range formats() {
		t.Run(format, func(t *testing.T) {
			t.Parallel()
			c := gittest.NewLinkedWorktreeSuper(t, format)
			g := c.Runner(t)
			// The main worktree shares its object database, its configuration
			// and keeps the directory of the linked worktree and its branch.
			skip := []string{".git/objects", ".git/worktrees", ".git/refs/heads/linked",
				".git/logs/refs/heads/linked", ".git/config"}
			main := treeState(t, c.Main, skip...)
			config := filepath.Join(c.Main, ".git", "config")
			mainConfig := runGit(t, g, c.Main, "config", "-f", config, "--list")
			r := openRepo(t, g, c.Theme.Dir())
			wantRoot(t, r, c.Dir)
			wantComplexStatus(t, r, c)
			res, err := r.Update(t.Context(), core.UpdateOptions{})
			wantRefusal(t, "Update", res, err, complexRefusal,
				core.ErrUninitialized, core.ErrDirty, core.ErrMissingRef)

			// The theme repository of the linked worktree is its own.
			names := []string{"kernel", "u-boot", "theme"}
			res = mustRepoUpdate(t, r, core.UpdateOptions{Names: names, Commit: true})
			theme := unchangedView(c.Theme)
			theme.Init, theme.Changed = true, true
			wantChanges(t, res.Changes, []changeView{
				fixtureView(c.Kernel, *kernelTarget(c, "v6.6.10"), true),
				fixtureView(c.UBoot, *uBootTarget(c), true), theme,
			})
			if res.Commit == "" ||
				runGit(t, g, c.Dir, "rev-parse", "refs/heads/linked") != res.Commit ||
				runGit(t, g, c.Dir, "rev-parse", "refs/heads/linked^") != c.History[4] {
				t.Errorf("Update(commit) = %+v: not one commit on branch linked", res)
			}
			if got := runGit(t, g, c.Theme.Dir(), "rev-parse", "--absolute-git-dir"); !samePath(t,
				got, c.Theme.GitDir) || headOf(t, c.Theme.Dir()) != c.Theme.Gitlink {
				t.Errorf("theme uses the repository %s", got)
			}
			if _, err := r.Verify(t.Context(), names, core.VerifyOptions{}); err != nil {
				t.Errorf("Verify(%q) = %v", names, err)
			}
			if _, err := r.Fetch(t.Context(), []string{c.FPGACore.Name}, nil); err != nil {
				t.Fatal(err)
			}
			st, err := r.Status(t.Context(), []string{c.FPGACore.Name})
			if err != nil || len(st) != 1 || st[0].State != core.StateDrift {
				t.Errorf("Status(fpga.core) = %+v, %v", st, err)
			}

			// The main worktree is left alone, except that git registers the
			// initialized submodule in the shared configuration.
			wantSameTree(t, "commands in the linked worktree", main, treeState(t, c.Main, skip...))
			want := mainConfig + "\nsubmodule.theme.active=true\nsubmodule.theme.url=" + c.Theme.URL
			if got := runGit(t, g, c.Main, "config", "-f", config, "--list"); got != want {
				t.Errorf("shared configuration:\n%s\nwant\n%s", got, want)
			}
			wantComplexStatus(t, openRepo(t, g, c.Main), c)
		})
	}
}

func TestScenarioSubdirInvocation(t *testing.T) {
	t.Parallel()
	c, invocations := gittest.NewSubdirInvocation(t, gittest.SHA1)
	g := c.Runner(t)
	before := treeState(t, c.Dir)
	for _, inv := range invocations {
		t.Run(inv.Name, func(t *testing.T) {
			r := openRepo(t, g, inv.Dir)
			wantRoot(t, r, inv.Root)
			st, err := r.Status(t.Context(), nil)
			if err != nil || !slices.Equal(statusNames(st), inv.Names) {
				t.Fatalf("Status = %q, %v; want %q", statusNames(st), err, inv.Names)
			}
			if samePath(t, inv.Root, c.Dir) {
				wantStatusRows(t, st, complexStatusRows(c))
				return
			}
			wantOnlyUnmanaged(t, r, st, inv.Names)
		})
	}
	wantSameTree(t, "commands in the start directories", before, treeState(t, c.Dir))

	t.Run("update from a subdirectory", func(t *testing.T) {
		r := openRepo(t, g, c.Subdir)
		res := mustRepoUpdate(t, r, core.UpdateOptions{Names: []string{c.Kernel.Name}})
		wantChanges(t, res.Changes, []changeView{
			fixtureView(c.Kernel, *kernelTarget(c, "v6.6.10"), true),
		})
		wantStaged(t, c.Dir, lock.File, c.Kernel.Path)
	})
	t.Run("clone into the start directory", func(t *testing.T) {
		r := openRepo(t, g, c.Fresh.Dir())
		wantRoot(t, r, c.Dir)
		res := mustRepoUpdate(t, r, core.UpdateOptions{Names: []string{c.Fresh.Name},
			Fetch: true})
		if c := oneChange(t, res); !c.Clone || c.New.Ref != "v1.1.0" {
			t.Errorf("Update(fresh) = %+v", viewOf(c))
		}
		var stdout, stderr strings.Builder
		err := r.Foreach(t.Context(), core.ForeachOptions{
			Args:   []string{"sh", "-c", `echo "$name:$displaypath"`},
			Stdout: &stdout, Stderr: &stderr,
		})
		var want strings.Builder
		for _, s := range c.Submodules() {
			switch {
			case s == c.Fresh:
				want.WriteString("fresh:.\n")
			case s.Mode != "" && s.Head != "":
				fmt.Fprintf(&want, "%s:../../%s\n", s.Name, s.Path)
			}
		}
		if err != nil || stdout.String() != want.String() ||
			stderr.String() != "skipping theme: submodule is not checked out\n" {
			t.Errorf("Foreach = %v\nstdout:\n%s\nwant:\n%s\nstderr:\n%s", err, stdout.String(),
				want.String(), stderr.String())
		}
	})
}

// wantOnlyUnmanaged checks a Repo whose submodules, listed in st and named
// by names, are all unmanaged: every command leaves them alone.
func wantOnlyUnmanaged(t *testing.T, r *core.Repo, st []core.Status, names []string) {
	t.Helper()
	for _, s := range st {
		if s.State != core.StateUnmanaged || s.Head == "" {
			t.Errorf("%s: %+v", s.Submodule.Name, s)
		}
	}
	res, err := r.Update(t.Context(), core.UpdateOptions{Fetch: true, Commit: true})
	if err != nil || len(res.Changes) != 0 || res.Commit != "" {
		t.Errorf("Update = %+v, %v", res, err)
	}
	vr, err := r.Verify(t.Context(), nil, core.VerifyOptions{Signatures: true})
	fr, ferr := r.Fetch(t.Context(), nil, nil)
	if err != nil || ferr != nil || len(vr) != 0 || len(fr) != 0 {
		t.Errorf("Verify = %+v, %v; Fetch = %+v, %v", vr, err, fr, ferr)
	}
	var out strings.Builder
	err = r.Foreach(t.Context(), core.ForeachOptions{Args: []string{"pwd"}, Stdout: &out,
		Stderr: &out})
	if err != nil || out.Len() != 0 {
		t.Errorf("Foreach = %v, output %q", err, out.String())
	}
	_, err = r.Update(t.Context(), core.UpdateOptions{Names: []string{"kernel"}})
	wantErr(t, "Update(kernel)", err, core.ErrNotFound)
	if len(names) > 0 {
		_, err = r.Update(t.Context(), core.UpdateOptions{Names: names})
		wantErr(t, fmt.Sprintf("Update(%q)", names), err, core.ErrUnmanaged)
	}
}

func TestScenarioNestedSuper(t *testing.T) {
	t.Parallel()
	for _, format := range formats() {
		t.Run(format, func(t *testing.T) {
			t.Parallel()
			n := gittest.NewNestedSuper(t, format)
			g := gittest.Runner(t)
			skip := []string{n.SuperPath, ".git/modules/super"}
			outer := treeState(t, n.Outer, skip...)
			for dir, root := range map[string]string{
				n.Dir:                                n.Dir,
				filepath.Join(n.Dir, "libs"):         n.Dir,
				n.Later.Dir():                        n.Dir,
				n.Lib.Dir():                          n.Lib.Dir(),
				n.Outer:                              n.Outer,
				filepath.Join(n.Outer, "components"): n.Outer,
			} {
				wantRoot(t, openRepo(t, g, dir), root)
			}
			r := openRepo(t, g, n.Dir)
			st, err := r.Status(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			wantStatusRows(t, st, []statusRow{
				{"lib", core.StateBehind,
					"update would select tag-pattern v1.0.1 instead of tag-pattern v1.0.0"},
				{"pinned", core.StateOK, "up to date"},
				{"later", core.StateUninitialized, "submodule is not checked out"},
			})
			v101 := n.Lib.Tags[gittest.TagV101]
			libTarget := core.Resolution{Mode: manifest.ModeTagPattern, Ref: gittest.TagV101,
				Commit: v101}
			wantFixtureStatus(t, st, n.Submodules(), map[string]*core.Resolution{
				"lib": &libTarget, "pinned": lockedTarget(n.Pinned),
			})
			res, err := r.Update(t.Context(), core.UpdateOptions{Commit: true})
			wantRefusal(t, "Update", res, err,
				"later: refused: submodule is not initialized (use --fetch)", core.ErrUninitialized)

			res = mustRepoUpdate(t, r, core.UpdateOptions{Fetch: true, Commit: true})
			later := unchangedView(n.Later)
			later.Init, later.Clone, later.Changed = true, true, true
			wantChanges(t, res.Changes, []changeView{
				fixtureView(n.Lib, libTarget, true), unchangedView(n.Pinned), later,
			})
			wantNestedCommit(t, g, n, res.Commit)
			if ok, err := g.IsGitDir(t.Context(), filepath.Join(n.GitDir, "modules", "later")); !ok ||
				err != nil {
				t.Errorf("no repository of later in %s: %v", n.GitDir, err)
			}
			if _, err := r.Verify(t.Context(), nil, core.VerifyOptions{}); err != nil {
				t.Errorf("Verify = %v", err)
			}

			// The outer repository only sees its submodule at a new commit.
			wantSameTree(t, "commands in the nested superproject", outer,
				treeState(t, n.Outer, skip...))
			if got := runGit(t, g, n.Outer, "status", "--porcelain"); got != "M "+n.SuperPath {
				t.Errorf("outer status %q", got)
			}
			ro := openRepo(t, g, n.Outer)
			st, err = ro.Status(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			wantStatusRows(t, st, []statusRow{{"super", core.StateUnmanaged, "no lsm-mode key"}})
			wantOnlyUnmanaged(t, ro, st, []string{"super"})
		})
	}
}

// wantNestedCommit checks the commit of the update of the nested
// superproject.
func wantNestedCommit(t *testing.T, g *git.Runner, n *gittest.NestedSuper, commit string) {
	t.Helper()
	if commit == "" || headOf(t, n.Dir) != commit {
		t.Fatalf("commit %q, HEAD %s", commit, headOf(t, n.Dir))
	}
	v100, v101 := n.Lib.Tags[gittest.TagV100], n.Lib.Tags[gittest.TagV101]
	later := n.Later.Gitlink[:12]
	want := "manifest: update 2 submodules\n\n" +
		"Submodule \"lib\":\n" +
		"  Tracking mode: tag-pattern v1.*\n" +
		"  Old: " + v100[:12] + " (v1.0.0)\n" +
		"  New: " + v101[:12] + " (v1.0.1)\n\n" +
		"Submodule \"later\":\n" +
		"  Tracking mode: branch stable\n" +
		"  Old: " + later + " (stable)\n" +
		"  New: " + later + " (stable)\n\n" + signOff
	if got := headMessage(t, n.Dir); got != want {
		t.Errorf("commit message:\n%s\nwant\n%s", got, want)
	}
	lintMessage(t, headMessage(t, n.Dir))
	if got := runGit(t, g, n.Dir, "diff", "--name-only", "HEAD^", "HEAD"); got !=
		".lsm.lock\nlib" {
		t.Errorf("committed files %q", got)
	}
	st, err := openRepo(t, g, n.Dir).Status(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	wantStatusRows(t, st, []statusRow{
		{"lib", core.StateOK, "up to date"},
		{"pinned", core.StateOK, "up to date"},
		{"later", core.StateOK, "up to date"},
	})
}

func TestScenarioUnbornSuper(t *testing.T) {
	t.Parallel()
	for _, format := range formats() {
		for name, staged := range map[string]bool{"direct commit": false, "staged first": true} {
			t.Run(format+"/"+name, func(t *testing.T) {
				t.Parallel()
				testUnbornSuper(t, format, staged)
			})
		}
	}
}

// testUnbornSuper updates the unborn superproject with a commit, when
// staged is set after an update without one.
func testUnbornSuper(t *testing.T, format string, staged bool) {
	u := gittest.NewUnbornSuper(t, format)
	g := gittest.Runner(t)
	lib := u.Lib
	r := openRepo(t, g, u.Dir)
	target := core.Resolution{Mode: manifest.ModeTagPattern, Ref: gittest.TagV101,
		Commit: lib.Tags[gittest.TagV101]}
	st, err := r.Status(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	wantStatusRows(t, st, []statusRow{{"lib", core.StateBehind, "no lock entry"}})
	wantFixtureStatus(t, st, []*gittest.Submodule{lib}, map[string]*core.Resolution{
		"lib": &target,
	})
	vr, err := r.Verify(t.Context(), nil, core.VerifyOptions{})
	wantErr(t, "Verify", err, core.ErrVerify)
	if len(vr) != 1 {
		t.Fatalf("Verify = %+v, %v", vr, err)
	}
	wantChecks(t, vr[0], []string{core.CheckLockEntry, core.CheckInitialized},
		[]string{core.CheckLockEntry}, "no entry in .lsm.lock")

	before := treeState(t, u.Dir)
	want := fixtureView(lib, target, true)
	res := mustRepoUpdate(t, r, core.UpdateOptions{DryRun: true})
	wantChanges(t, res.Changes, []changeView{want})
	// A commit replaces nothing.
	want.OldGitlink = ""
	res = mustRepoUpdate(t, r, core.UpdateOptions{DryRun: true, Commit: true})
	wantChanges(t, res.Changes, []changeView{want})
	wantSameTree(t, "dry runs", before, treeState(t, u.Dir))

	if staged {
		res = mustRepoUpdate(t, r, core.UpdateOptions{})
		stagedView := want
		stagedView.OldGitlink = lib.Gitlink
		wantChanges(t, res.Changes, []changeView{stagedView})
		wantStaged(t, u.Dir, manifest.File, lock.File, lib.Path)
		vr, err = r.Verify(t.Context(), nil, core.VerifyOptions{})
		wantErr(t, "Verify(staged)", err, core.ErrVerify)
		if len(vr) != 1 {
			t.Fatalf("Verify(staged) = %+v, %v", vr, err)
		}
		wantChecks(t, vr[0], tagChecks(), []string{core.CheckGitlink},
			"HEAD of the superproject records no gitlink at lib")
		want.OldHead = target.Commit
	}

	res = mustRepoUpdate(t, r, core.UpdateOptions{Commit: true})
	wantChanges(t, res.Changes, []changeView{want})
	if res.Commit == "" || runGit(t, g, u.Dir, "rev-list", "--parents", "HEAD") != res.Commit {
		t.Fatalf("Update(commit) = %+v: not a root commit", res)
	}
	msg := "manifest: update lib to v1.0.1\n\n" +
		"Tracking mode: tag-pattern v1.*\n" +
		"Old: none\n" +
		"New: " + target.Commit[:12] + " (v1.0.1)\n\n" + signOff
	if got := headMessage(t, u.Dir); got != msg {
		t.Errorf("commit message:\n%s\nwant\n%s", got, msg)
	}
	if got := runGit(t, g, u.Dir, "ls-tree", "--name-only", "HEAD"); got !=
		".gitmodules\n.lsm.lock\nlib" {
		t.Errorf("committed files %q", got)
	}
	wantStaged(t, u.Dir)
	if _, err := r.Verify(t.Context(), nil, core.VerifyOptions{}); err != nil {
		t.Errorf("Verify after the commit = %v", err)
	}
	st, err = r.Status(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	wantStatusRows(t, st, []statusRow{{"lib", core.StateOK, "up to date"}})
}
