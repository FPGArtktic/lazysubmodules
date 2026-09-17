// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package git_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/FPGArtktic/lazysubmodules/internal/git"
	"github.com/FPGArtktic/lazysubmodules/internal/git/gittest"
)

func TestIsDirty(t *testing.T) {
	t.Parallel()
	f := newFixture(t, gittest.SHA1)
	r := gittest.Runner(t)
	ctx := t.Context()
	check := func(dir string, want bool) {
		t.Helper()
		got, err := r.IsDirty(ctx, dir)
		if err != nil || got != want {
			t.Errorf("IsDirty(%s) = %v, %v; want %v", dir, got, err, want)
		}
	}
	check(f.super.Dir, false)
	check(f.sub, false)

	gittest.WriteFile(t, filepath.Join(f.sub, "untracked"), "x\n")
	check(f.sub, false)

	f.super.MakeDirty(t, f.path)
	check(f.sub, true)
	check(f.super.Dir, true) // modified submodule content counts

	_, err := r.IsDirty(ctx, t.TempDir())
	if _, ok := errors.AsType[*git.Error](err); !ok {
		t.Errorf("IsDirty outside a repository: %v, want *git.Error", err)
	}
}

func TestIsDirtyNestedSubmodule(t *testing.T) {
	t.Parallel()
	inner := gittest.NewUpstream(t, gittest.SHA1)
	outer := gittest.NewUpstream(t, gittest.SHA1)
	outer.AddSubmodule(t, "inner", inner)
	// The next outer commit records a newer inner commit.
	inner.Commit(t, "newer inner")
	gittest.Git(t, filepath.Join(outer.Work, "inner"), "pull", "--quiet", "origin", "main")
	outer.Commit(t, "bump inner") // commits the gitlink change as well
	super := gittest.NewSuper(t, gittest.SHA1)
	outerDir := filepath.Join(super.Dir, super.AddSubmodule(t, "outer", outer))
	gittest.Git(t, outerDir, "submodule", "update", "--init", "--quiet")
	innerDir := filepath.Join(outerDir, "inner")
	r := gittest.Runner(t)
	ctx := t.Context()
	check := func(what string, want bool) {
		t.Helper()
		if got, err := r.IsDirty(ctx, outerDir); err != nil || got != want {
			t.Errorf("%s: IsDirty = %v, %v; want %v", what, got, err, want)
		}
	}
	check("initialized", false)

	// Checking out an older outer commit leaves inner at the newer commit.
	if err := r.Checkout(ctx, outerDir, "HEAD~1", git.Offline); err != nil {
		t.Fatal(err)
	}
	check("nested submodule at another commit", false)
	gittest.WriteFile(t, filepath.Join(innerDir, "untracked"), "x\n")
	check("untracked file in nested submodule", false)

	gittest.WriteFile(t, filepath.Join(innerDir, gittest.TrackedFile), "changed\n")
	check("modified file in nested submodule", true)
	gittest.Git(t, innerDir, "checkout", "--quiet", "--", gittest.TrackedFile)
	check("restored file in nested submodule", false)

	gittest.Git(t, outerDir, "add", "inner")
	check("staged gitlink", true)
	gittest.Git(t, outerDir, "reset", "--quiet")
	check("unstaged gitlink", false)

	if err := os.RemoveAll(innerDir); err != nil {
		t.Fatal(err)
	}
	check("deleted nested submodule", true)
}

func TestIsDirtyIgnoresSubmoduleConfig(t *testing.T) {
	t.Parallel()
	f := newFixture(t, gittest.SHA1)
	r := gittest.Runner(t)
	f.super.SetKey(t, "lib", "ignore", "all")
	f.super.Commit(t, "ignore lib")
	gittest.Git(t, f.super.Dir, "config", "diff.ignoreSubmodules", "all")
	f.super.MakeDirty(t, f.path)
	got, err := r.IsDirty(t.Context(), f.super.Dir)
	if err != nil || !got {
		t.Errorf("IsDirty = %v, %v; want true", got, err)
	}
}

func TestIsWorktreeRoot(t *testing.T) {
	t.Parallel()
	f := newFixture(t, gittest.SHA1)
	r := gittest.Runner(t)
	ctx := t.Context()
	tmp := t.TempDir()

	subdir := filepath.Join(f.super.Dir, "dir")
	emptyGit := filepath.Join(tmp, "empty-git")
	badGitFile := filepath.Join(tmp, "bad-git-file")
	regular := filepath.Join(tmp, "regular")
	link := filepath.Join(tmp, "link")
	for _, dir := range []string{subdir, filepath.Join(emptyGit, ".git"), badGitFile} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	gittest.WriteFile(t, filepath.Join(badGitFile, ".git"), "gitdir: /nonexistent\n")
	gittest.WriteFile(t, regular, "x\n")
	if err := os.Symlink(f.super.Dir, link); err != nil {
		t.Fatal(err)
	}

	tests := map[string]bool{
		f.super.Dir:                    true,
		f.sub:                          true,
		link:                           true,
		subdir:                         false,
		filepath.Join(tmp, "missing"):  false,
		emptyGit:                       false,
		badGitFile:                     false,
		regular:                        false,
		filepath.Join(regular, "file"): false,
	}
	for dir, want := range tests {
		got, err := r.IsWorktreeRoot(ctx, dir)
		if err != nil || got != want {
			t.Errorf("IsWorktreeRoot(%s) = %v, %v; want %v", dir, got, err, want)
		}
	}

	f.super.Deinit(t, f.path)
	got, err := r.IsWorktreeRoot(ctx, f.sub)
	if err != nil || got {
		t.Errorf("IsWorktreeRoot(deinitialized) = %v, %v; want false", got, err)
	}
}

func TestIsWorktreeRootDubiousOwnership(t *testing.T) {
	t.Parallel()
	f := newFixture(t, gittest.SHA1)
	// Git refuses every repository as if another user owned it.
	r, err := git.New(git.WithEnv(append(gittest.Env(t),
		"GIT_TEST_ASSUME_DIFFERENT_OWNER=1")...))
	if err != nil {
		t.Fatal(err)
	}
	root, err := r.IsWorktreeRoot(t.Context(), f.sub)
	gitErr, ok := errors.AsType[*git.Error](err)
	if root || !ok || !strings.Contains(gitErr.Stderr, "dubious ownership") {
		t.Errorf("IsWorktreeRoot = %v, %v; want *git.Error about the ownership", root, err)
	}
}

func TestIsWorktreeRootErrors(t *testing.T) {
	t.Parallel()
	f := newFixture(t, gittest.SHA1)
	r := gittest.Runner(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	root, err := r.IsWorktreeRoot(ctx, f.sub)
	if root || !errors.Is(err, context.Canceled) {
		t.Errorf("IsWorktreeRoot(canceled) = %v, %v; want context.Canceled", root, err)
	}

	if os.Geteuid() == 0 {
		t.Skip("permissions do not apply to root")
	}
	locked := filepath.Join(t.TempDir(), "locked")
	if err := os.Mkdir(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })
	root, err = r.IsWorktreeRoot(t.Context(), filepath.Join(locked, "sub"))
	if root || !errors.Is(err, os.ErrPermission) {
		t.Errorf("IsWorktreeRoot(unreadable) = %v, %v; want a permission error", root, err)
	}
}

func TestIsGitDir(t *testing.T) {
	t.Parallel()
	f := newFixture(t, gittest.SHA1)
	r := gittest.Runner(t)
	ctx := t.Context()
	tmp := t.TempDir()
	gitDir := filepath.Join(f.super.Dir, ".git")
	modules := filepath.Join(gitDir, "modules", "lib")
	notRepo := filepath.Join(gitDir, "modules", "other")
	empty := filepath.Join(tmp, "empty")
	file := filepath.Join(tmp, "file")
	link := filepath.Join(tmp, "link")
	for _, dir := range []string{notRepo, empty} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	gittest.WriteFile(t, file, "x\n")
	if err := os.Symlink(f.up.Bare, link); err != nil {
		t.Fatal(err)
	}
	check := func(what string, tests map[string]bool) {
		t.Helper()
		for dir, want := range tests {
			got, err := r.IsGitDir(ctx, dir)
			if err != nil || got != want {
				t.Errorf("%s: IsGitDir(%s) = %v, %v; want %v", what, dir, got, err, want)
			}
		}
	}
	check("populated", map[string]bool{
		gitDir:                         true,
		modules:                        true,
		f.up.Bare:                      true,
		link:                           true,
		f.super.Dir:                    false,
		f.sub:                          false,
		notRepo:                        false, // git finds the superproject
		empty:                          false,
		file:                           false,
		filepath.Join(tmp, "missing"):  false,
		filepath.Join(file, "missing"): false,
	})

	// Deinitializing unsets the working tree of the repository.
	f.super.Deinit(t, f.path)
	if err := os.Remove(f.sub); err != nil {
		t.Fatal(err)
	}
	check("deinitialized", map[string]bool{modules: true})
	// Git cannot enter the working tree that the repository names.
	if err := r.SubmoduleInit(ctx, f.super.Dir, f.path, git.Offline, nil); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(f.sub); err != nil {
		t.Fatal(err)
	}
	check("working tree removed", map[string]bool{modules: false})
}

func TestIsGitDirErrors(t *testing.T) {
	t.Parallel()
	f := newFixture(t, gittest.SHA1)
	gitDir := filepath.Join(f.super.Dir, ".git")
	// Git refuses every repository as if another user owned it.
	r, err := git.New(git.WithEnv(append(gittest.Env(t),
		"GIT_TEST_ASSUME_DIFFERENT_OWNER=1")...))
	if err != nil {
		t.Fatal(err)
	}
	ok, err := r.IsGitDir(t.Context(), gitDir)
	gitErr, isGitErr := errors.AsType[*git.Error](err)
	if ok || !isGitErr || !strings.Contains(gitErr.Stderr, "dubious ownership") {
		t.Errorf("IsGitDir = %v, %v; want *git.Error about the ownership", ok, err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	ok, err = gittest.Runner(t).IsGitDir(ctx, gitDir)
	if ok || !errors.Is(err, context.Canceled) {
		t.Errorf("IsGitDir(canceled) = %v, %v; want context.Canceled", ok, err)
	}

	if os.Geteuid() == 0 {
		t.Skip("permissions do not apply to root")
	}
	locked := filepath.Join(t.TempDir(), "locked")
	if err := os.Mkdir(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })
	ok, err = gittest.Runner(t).IsGitDir(t.Context(), filepath.Join(locked, "repo"))
	if ok || !errors.Is(err, os.ErrPermission) {
		t.Errorf("IsGitDir(unreadable) = %v, %v; want a permission error", ok, err)
	}
}

func TestInspectGitDir(t *testing.T) {
	t.Parallel()
	f := newFixture(t, gittest.SHA1)
	ctx := t.Context()
	tmp := t.TempDir()
	gitDir := filepath.Join(f.super.Dir, ".git")
	modules := filepath.Join(gitDir, "modules", "lib")
	emptyModule := filepath.Join(gitDir, "modules", "other")
	empty := filepath.Join(tmp, "empty")
	file := filepath.Join(tmp, "file")
	link := filepath.Join(tmp, "link")
	dangling := filepath.Join(tmp, "dangling")
	for _, dir := range []string{emptyModule, empty} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	gittest.WriteFile(t, file, "gitdir: "+modules+"\n")
	for target, name := range map[string]string{f.up.Bare: link, "missing": dangling} {
		if err := os.Symlink(target, name); err != nil {
			t.Fatal(err)
		}
	}
	check := func(what string, r *git.Runner, tests map[string]git.RepoState) {
		t.Helper()
		for dir, want := range tests {
			got, err := r.InspectGitDir(ctx, dir)
			if err != nil || got != want {
				t.Errorf("%s: InspectGitDir(%s) = %v, %v; want %v", what, dir, got, err, want)
			}
		}
	}
	r := gittest.Runner(t)
	check("plain", r, map[string]git.RepoState{
		gitDir:                         git.RepoUsable,
		modules:                        git.RepoUsable,
		f.up.Bare:                      git.RepoUsable,
		link:                           git.RepoUsable,
		f.super.Dir:                    git.RepoInvalid,
		f.sub:                          git.RepoInvalid,
		emptyModule:                    git.RepoInvalid,
		empty:                          git.RepoInvalid,
		file:                           git.RepoInvalid,
		dangling:                       git.RepoInvalid,
		filepath.Join(tmp, "missing"):  git.RepoMissing,
		filepath.Join(file, "missing"): git.RepoMissing,
	})
	// Git refuses a bare repository that is not inside a git directory; git
	// before 2.45 refuses the one of the superproject as well, which it
	// finds from an empty directory inside it.
	check("bare repositories refused",
		gittest.Runner(t, "safe.bareRepository=explicit"), map[string]git.RepoState{
			f.up.Bare:   git.RepoUnusable,
			emptyModule: git.RepoInvalid,
			empty:       git.RepoInvalid,
		})
	owner, err := git.New(git.WithEnv(append(gittest.Env(t),
		"GIT_TEST_ASSUME_DIFFERENT_OWNER=1")...))
	if err != nil {
		t.Fatal(err)
	}
	check("owned by another user", owner, map[string]git.RepoState{
		gitDir:      git.RepoUnusable,
		modules:     git.RepoUnusable,
		emptyModule: git.RepoInvalid,
	})

	// Git cannot enter the working tree that the repository names.
	f.super.Deinit(t, f.path)
	if err := r.SubmoduleInit(ctx, f.super.Dir, f.path, git.Offline, nil); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(f.sub); err != nil {
		t.Fatal(err)
	}
	check("working tree removed", r, map[string]git.RepoState{modules: git.RepoUnusable})
}

func TestInspectGitDirErrors(t *testing.T) {
	t.Parallel()
	r := gittest.Runner(t)
	// A repository format that git does not know.
	future := filepath.Join(t.TempDir(), "future.git")
	gittest.Git(t, filepath.Dir(future), "init", "--quiet", "--bare", future)
	gittest.Git(t, future, "config", "core.repositoryFormatVersion", "1")
	gittest.Git(t, future, "config", "extensions.lsmFuture", "true")
	state, err := r.InspectGitDir(t.Context(), future)
	if _, ok := errors.AsType[*git.Error](err); !ok {
		t.Errorf("InspectGitDir(unknown extension) = %v, %v; want *git.Error", state, err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	state, err = r.InspectGitDir(ctx, t.TempDir())
	if !errors.Is(err, context.Canceled) {
		t.Errorf("InspectGitDir(canceled) = %v, %v; want context.Canceled", state, err)
	}

	if os.Geteuid() == 0 {
		t.Skip("permissions do not apply to root")
	}
	locked := filepath.Join(t.TempDir(), "locked")
	if err := os.Mkdir(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })
	state, err = r.InspectGitDir(t.Context(), filepath.Join(locked, "repo"))
	if !errors.Is(err, os.ErrPermission) {
		t.Errorf("InspectGitDir(unreadable) = %v, %v; want a permission error", state, err)
	}
}

func TestSamePath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	file := filepath.Join(dir, "file")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	gittest.WriteFile(t, file, "x\n")
	links := map[string]string{"sub-link": sub, "file-link": "file", "chain": "sub-link"}
	for name, dest := range links {
		if err := os.Symlink(dest, filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	tests := []struct {
		a, b string
		want bool
	}{
		{sub, sub, true},
		{sub, filepath.Join(dir, "sub-link"), true},
		{filepath.Join(dir, "chain"), sub + "/", true},
		{filepath.Join(sub, ".."), dir, true},
		{file, filepath.Join(dir, "file-link"), true},
		{sub, dir, false},
		{sub, file, false},
	}
	for _, tt := range tests {
		if got, err := git.SamePath(tt.a, tt.b); err != nil || got != tt.want {
			t.Errorf("SamePath(%s, %s) = %v, %v; want %v", tt.a, tt.b, got, err, tt.want)
		}
	}
	missing := filepath.Join(dir, "missing")
	for _, pair := range [][2]string{{sub, missing}, {missing, sub}} {
		got, err := git.SamePath(pair[0], pair[1])
		if got || !errors.Is(err, os.ErrNotExist) {
			t.Errorf("SamePath(%s, %s) = %v, %v; want ErrNotExist", pair[0], pair[1], got, err)
		}
	}
}

func TestCheckout(t *testing.T) {
	t.Parallel()
	for _, format := range formats() {
		t.Run(format, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t, format)
			r := gittest.Runner(t)
			ctx := t.Context()
			target := f.commits[gittest.TagV100]
			if err := r.Checkout(ctx, f.sub, target, git.Offline); err != nil {
				t.Fatal(err)
			}
			if head, err := r.Head(ctx, f.sub); err != nil || head != target {
				t.Errorf("Head = %q, %v; want %q", head, err, target)
			}
			branch := gittest.Git(t, f.sub, "rev-parse", "--abbrev-ref", "HEAD")
			if branch != "HEAD" {
				t.Errorf("HEAD is not detached: %q", branch)
			}
			err := r.Checkout(ctx, f.sub, strings.Repeat("1", hexLen(format)), git.Offline)
			if _, ok := errors.AsType[*git.Error](err); !ok {
				t.Errorf("Checkout(missing) = %v, want *git.Error", err)
			}
		})
	}
}

func TestCheckoutRefusesToLoseChanges(t *testing.T) {
	t.Parallel()
	f := newFixture(t, gittest.SHA1)
	r := gittest.Runner(t)
	f.super.MakeDirty(t, f.path)
	err := r.Checkout(t.Context(), f.sub, f.commits[gittest.TagRC1], git.Offline)
	if _, ok := errors.AsType[*git.Error](err); !ok {
		t.Errorf("Checkout(dirty) = %v, want *git.Error", err)
	}
}

func TestCheckoutRejectsOptions(t *testing.T) {
	t.Parallel()
	f := newFixture(t, gittest.SHA1)
	r := gittest.Runner(t)
	ctx := t.Context()
	f.super.MakeDirty(t, f.path)
	for _, network := range []git.Network{git.Offline, git.Online} {
		for _, commit := range []string{"-f", "--force", "-", ""} {
			err := r.Checkout(ctx, f.sub, commit, network)
			if !errors.Is(err, git.ErrInvalidRefName) {
				t.Errorf("Checkout(%q, %t) = %v, want ErrInvalidRefName", commit, network, err)
			}
		}
	}
	if dirty, err := r.IsDirty(ctx, f.sub); err != nil || !dirty {
		t.Errorf("IsDirty after rejected checkouts = %v, %v; want true", dirty, err)
	}
}

func TestAddAndRemove(t *testing.T) {
	t.Parallel()
	r := gittest.Runner(t)
	ctx := t.Context()
	dir := gittest.NewSuper(t, gittest.SHA1).Dir
	for _, name := range []string{"a*b", "axb", "a b"} {
		gittest.WriteFile(t, filepath.Join(dir, name), name+"\n")
	}
	if err := r.Add(ctx, dir, false); err != nil {
		t.Errorf("Add() = %v", err)
	}
	if err := r.Add(ctx, dir, false, "a*b", "a b"); err != nil {
		t.Fatal(err)
	}
	staged, err := r.StagedPaths(ctx, dir)
	if want := []string{"a b", "a*b"}; err != nil || !slices.Equal(staged, want) {
		t.Errorf("StagedPaths = %q, %v; want %q", staged, err, want)
	}
	for _, force := range []bool{false, true} {
		err = r.Add(ctx, dir, force, "missing")
		if _, ok := errors.AsType[*git.Error](err); !ok {
			t.Errorf("Add(missing, force %t) = %v, want *git.Error", force, err)
		}
	}

	if err := r.Remove(ctx, dir, false); err != nil {
		t.Errorf("Remove() = %v", err)
	}
	// Staged but uncommitted content needs force.
	err = r.Remove(ctx, dir, false, "a*b")
	if _, ok := errors.AsType[*git.Error](err); !ok {
		t.Errorf("Remove(no force) = %v, want *git.Error", err)
	}
	if err := r.Remove(ctx, dir, true, "a*b"); err != nil {
		t.Fatal(err)
	}
	staged, err = r.StagedPaths(ctx, dir)
	if want := []string{"a b"}; err != nil || !slices.Equal(staged, want) {
		t.Errorf("StagedPaths = %q, %v; want %q", staged, err, want)
	}
	if exists(t, filepath.Join(dir, "a*b")) || !exists(t, filepath.Join(dir, "axb")) {
		t.Errorf("Remove touched the wrong files")
	}
}

func TestAddIgnoredPath(t *testing.T) {
	t.Parallel()
	r := gittest.Runner(t)
	ctx := t.Context()
	dir := gittest.NewSuper(t, gittest.SHA1).Dir
	gittest.WriteFile(t, filepath.Join(dir, ".gitignore"), "*.lock\n")
	gittest.WriteFile(t, filepath.Join(dir, ".lsm.lock"), "[submodule \"x\"]\n")
	gittest.WriteFile(t, filepath.Join(dir, "tracked"), "x\n")

	err := r.Add(ctx, dir, false, "tracked", ".lsm.lock")
	gitErr, ok := errors.AsType[*git.Error](err)
	if !ok || !strings.Contains(gitErr.Stderr, "ignored") {
		t.Errorf("Add(ignored) = %v, want *git.Error naming the ignore rule", err)
	}
	if err := r.Add(ctx, dir, true, "tracked", ".lsm.lock"); err != nil {
		t.Fatalf("Add(ignored, force) = %v", err)
	}
	staged, err := r.StagedPaths(ctx, dir)
	if want := []string{".lsm.lock", "tracked"}; err != nil || !slices.Equal(staged, want) {
		t.Errorf("StagedPaths = %q, %v; want %q", staged, err, want)
	}
}

func TestRemoveSubmodule(t *testing.T) {
	t.Parallel()
	f := newFixture(t, gittest.SHA1)
	r := gittest.Runner(t)
	ctx := t.Context()
	if err := r.Remove(ctx, f.super.Dir, true, f.path); err != nil {
		t.Fatal(err)
	}
	if exists(t, f.sub) {
		t.Errorf("submodule working tree still exists")
	}
	entries, err := r.ConfigList(ctx, f.super.Dir, ".gitmodules")
	if err != nil || len(entries) != 0 {
		t.Errorf(".gitmodules entries = %q, %v; want none", entries, err)
	}
}

func TestStagedPaths(t *testing.T) {
	t.Parallel()
	f := newFixture(t, gittest.SHA1)
	r := gittest.Runner(t)
	ctx := t.Context()

	paths, err := r.StagedPaths(ctx, f.super.Dir)
	if err != nil || paths != nil {
		t.Errorf("StagedPaths(clean) = %q, %v; want nil", paths, err)
	}
	has, err := r.HasStaged(ctx, f.super.Dir)
	if err != nil || has {
		t.Errorf("HasStaged(clean) = %v, %v; want false", has, err)
	}

	// A staged gitlink change is listed even when submodule changes are ignored.
	gittest.Git(t, f.super.Dir, "config", "diff.ignoreSubmodules", "all")
	gittest.Git(t, f.sub, "checkout", "--quiet", f.commits[gittest.TagV100])
	gittest.WriteFile(t, filepath.Join(f.super.Dir, "dir", "new file\n"), "x\n")
	gittest.Git(t, f.super.Dir, "add", "--all")
	gittest.Git(t, f.super.Dir, "mv", "README", "README.md")

	nested := filepath.Join(f.super.Dir, "dir")
	paths, err = r.StagedPaths(ctx, nested)
	want := []string{"README", "README.md", "dir/new file\n", f.path}
	if err != nil || !slices.Equal(paths, want) {
		t.Errorf("StagedPaths = %q, %v; want %q", paths, err, want)
	}
	has, err = r.HasStaged(ctx, f.super.Dir)
	if err != nil || !has {
		t.Errorf("HasStaged = %v, %v; want true", has, err)
	}
}

func TestStagedPathsUnborn(t *testing.T) {
	t.Parallel()
	for _, format := range formats() {
		t.Run(format, func(t *testing.T) {
			t.Parallel()
			r := gittest.Runner(t)
			ctx := t.Context()
			dir := gittest.InitRepo(t, format)
			paths, err := r.StagedPaths(ctx, dir)
			if err != nil || paths != nil {
				t.Errorf("StagedPaths(empty unborn) = %q, %v; want nil", paths, err)
			}
			gittest.WriteFile(t, filepath.Join(dir, "f"), "x\n")
			gittest.Git(t, dir, "add", "f")
			paths, err = r.StagedPaths(ctx, dir)
			if err != nil || !slices.Equal(paths, []string{"f"}) {
				t.Errorf("StagedPaths(unborn) = %q, %v; want [f]", paths, err)
			}
			has, err := r.HasStaged(ctx, dir)
			if err != nil || !has {
				t.Errorf("HasStaged(unborn) = %v, %v; want true", has, err)
			}
		})
	}
}

func TestStagedPathsOutsideRepository(t *testing.T) {
	t.Parallel()
	r := gittest.Runner(t)
	_, err := r.HasStaged(t.Context(), t.TempDir())
	if _, ok := errors.AsType[*git.Error](err); !ok {
		t.Errorf("HasStaged outside a repository = %v, want *git.Error", err)
	}
}

func TestCommit(t *testing.T) {
	t.Parallel()
	for _, format := range formats() {
		t.Run(format, func(t *testing.T) {
			t.Parallel()
			r := gittest.Runner(t)
			ctx := t.Context()
			dir := gittest.InitRepo(t, format)
			gittest.WriteFile(t, filepath.Join(dir, "f"), "x\n")
			gittest.Git(t, dir, "add", "f")

			msg := "manifest: update lib to v1.0.0\n\n" +
				"Tracking mode: tag v1.0.0\nOld: none\n"
			if err := r.Commit(ctx, dir, msg); err != nil {
				t.Fatal(err)
			}
			got := gittest.Git(t, dir, "log", "-1", "--format=%B")
			want := strings.TrimSpace(msg) + "\n\n" +
				"Signed-off-by: " + gittest.Name + " <" + gittest.Email + ">"
			if got != want {
				t.Errorf("message = %q, want %q", got, want)
			}
			if head, err := r.Head(ctx, dir); err != nil || len(head) != hexLen(format) {
				t.Errorf("Head after commit = %q, %v", head, err)
			}

			err := r.Commit(ctx, dir, "core: nothing to commit\n")
			if _, ok := errors.AsType[*git.Error](err); !ok {
				t.Errorf("Commit(clean) = %v, want *git.Error", err)
			}
		})
	}
}

func TestCommitSignOffAfterSubmoduleList(t *testing.T) {
	t.Parallel()
	r := gittest.Runner(t)
	ctx := t.Context()
	dir := gittest.NewSuper(t, gittest.SHA1).Dir
	// The layout of a commit updating several submodules (DESIGN 4.5). Its
	// last paragraph must not look like a trailer block, whatever the name,
	// so that git separates the sign-off with a blank line.
	for _, name := range []string{"u-boot", "kernel", ":x", "a: b", "Signed-off-by"} {
		msg := "manifest: update 2 submodules\n\n" +
			"Submodule \"first\":\n  Tracking mode: branch main\n" +
			"  Old: 0123456789ab (main)\n  New: 123456789abc (main)\n\n" +
			"Submodule \"" + name + "\":\n  Tracking mode: tag v1.0.0\n" +
			"  Old: none\n  New: 23456789abcd (v1.0.0)\n"
		gittest.WriteFile(t, filepath.Join(dir, "f"), name+"\n")
		gittest.Git(t, dir, "add", "f")
		if err := r.Commit(ctx, dir, msg); err != nil {
			t.Fatal(err)
		}
		got := gittest.Git(t, dir, "log", "-1", "--format=%B")
		want := msg + "\nSigned-off-by: " + gittest.Name + " <" + gittest.Email + ">"
		if got != want {
			t.Errorf("name %q: message =\n%s\nwant\n%s", name, got, want)
		}
	}
}

func TestCommitRunsHooks(t *testing.T) {
	t.Parallel()
	r := gittest.Runner(t)
	dir := gittest.NewSuper(t, gittest.SHA1).Dir
	hook := filepath.Join(dir, ".git", "hooks", "commit-msg")
	gittest.WriteFile(t, hook, "#!/bin/sh\necho 'rejected by hook' >&2\nexit 1\n")
	if err := os.Chmod(hook, 0o755); err != nil {
		t.Fatal(err)
	}
	gittest.WriteFile(t, filepath.Join(dir, "f"), "x\n")
	gittest.Git(t, dir, "add", "f")
	err := r.Commit(t.Context(), dir, "core: hooked\n")
	gitErr, ok := errors.AsType[*git.Error](err)
	if !ok || !strings.Contains(gitErr.Stderr, "rejected by hook") {
		t.Errorf("Commit with failing hook = %v, want *git.Error from the hook", err)
	}
}

func TestIndexGitlink(t *testing.T) {
	t.Parallel()
	for _, format := range formats() {
		t.Run(format, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t, format)
			r := gittest.Runner(t)
			ctx := t.Context()
			tip := f.commits[gittest.TagV200RC]
			for _, p := range []string{f.path, f.path + "/", "./" + f.path} {
				got, err := r.IndexGitlink(ctx, f.super.Dir, p)
				if err != nil || got != tip {
					t.Errorf("IndexGitlink(%q) = %q, %v; want %q", p, got, err, tip)
				}
			}

			old := f.commits[gittest.TagRC1]
			gittest.Git(t, f.sub, "checkout", "--quiet", old)
			gittest.Git(t, f.super.Dir, "add", f.path)
			if got, err := r.IndexGitlink(ctx, f.super.Dir, f.path); err != nil || got != old {
				t.Errorf("IndexGitlink after add = %q, %v; want %q", got, err, old)
			}

			for _, p := range []string{"README", "missing", "li"} {
				got, err := r.IndexGitlink(ctx, f.super.Dir, p)
				if got != "" || !errors.Is(err, git.ErrRefNotFound) {
					t.Errorf("IndexGitlink(%q) = %q, %v; want ErrRefNotFound", p, got, err)
				}
			}
		})
	}
}

func TestIndexGitlinkLiteralPath(t *testing.T) {
	t.Parallel()
	up := gittest.NewUpstream(t, gittest.SHA1)
	super := gittest.NewSuper(t, gittest.SHA1)
	r := gittest.Runner(t)
	path := super.AddSubmodule(t, "lib*", up)
	gittest.WriteFile(t, filepath.Join(super.Dir, "libx"), "x\n")
	want := gittest.Git(t, up.Work, "rev-parse", "HEAD")
	if got, err := r.IndexGitlink(t.Context(), super.Dir, path); err != nil || got != want {
		t.Errorf("IndexGitlink(%q) = %q, %v; want %q", path, got, err, want)
	}
	if got, err := r.TreeGitlink(t.Context(), super.Dir, "HEAD", path); err != nil || got != want {
		t.Errorf("TreeGitlink(%q) = %q, %v; want %q", path, got, err, want)
	}
}

func TestTreeGitlink(t *testing.T) {
	t.Parallel()
	for _, format := range formats() {
		t.Run(format, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t, format)
			r := gittest.Runner(t)
			ctx := t.Context()
			tip := f.commits[gittest.TagV200RC]
			added := gittest.Git(t, f.super.Dir, "rev-parse", "HEAD")

			// A staged change does not affect the tree of HEAD.
			gittest.Git(t, f.sub, "checkout", "--quiet", f.commits[gittest.TagRC1])
			gittest.Git(t, f.super.Dir, "add", f.path)
			for _, rev := range []string{"HEAD", added, "main"} {
				for _, p := range []string{f.path, f.path + "/", "./" + f.path} {
					got, err := r.TreeGitlink(ctx, f.super.Dir, rev, p)
					if err != nil || got != tip {
						t.Errorf("TreeGitlink(%s, %q) = %q, %v; want %q", rev, p, got, err, tip)
					}
				}
			}

			notFound := [][2]string{
				{"HEAD~1", f.path}, // before the submodule was added
				{"HEAD", "README"}, // not a gitlink
				{"HEAD", "missing"},
				{"HEAD", "li"},
				{"nope", f.path},
			}
			for _, tt := range notFound {
				got, err := r.TreeGitlink(ctx, f.super.Dir, tt[0], tt[1])
				if got != "" || !errors.Is(err, git.ErrRefNotFound) {
					t.Errorf("TreeGitlink(%s, %s) = %q, %v; want ErrRefNotFound",
						tt[0], tt[1], got, err)
				}
			}
			for _, rev := range []string{"-", "--git-dir", "-HEAD"} {
				got, err := r.TreeGitlink(ctx, f.super.Dir, rev, f.path)
				if got != "" || !errors.Is(err, git.ErrInvalidRefName) {
					t.Errorf("TreeGitlink(%s) = %q, %v; want ErrInvalidRefName", rev, got, err)
				}
			}
			// A revision that names a blob has no tree.
			blob := gittest.Git(t, f.super.Dir, "rev-parse", "HEAD:README")
			got, err := r.TreeGitlink(ctx, f.super.Dir, blob, f.path)
			if _, ok := errors.AsType[*git.Error](err); !ok || got != "" {
				t.Errorf("TreeGitlink(blob) = %q, %v; want *git.Error", got, err)
			}
		})
	}
}

func TestTreeGitlinkUnborn(t *testing.T) {
	t.Parallel()
	for _, format := range formats() {
		t.Run(format, func(t *testing.T) {
			t.Parallel()
			up := gittest.NewUpstream(t, format)
			r := gittest.Runner(t)
			dir := gittest.InitRepo(t, format)
			gittest.Git(t, dir, "submodule", "add", "--", up.Bare, "lib")
			got, err := r.TreeGitlink(t.Context(), dir, "HEAD", "lib")
			if got != "" || !errors.Is(err, git.ErrRefNotFound) {
				t.Errorf("TreeGitlink(unborn) = %q, %v; want ErrRefNotFound", got, err)
			}
			want := gittest.Git(t, up.Work, "rev-parse", "HEAD")
			if got, err := r.IndexGitlink(t.Context(), dir, "lib"); err != nil || got != want {
				t.Errorf("IndexGitlink(unborn) = %q, %v; want %q", got, err, want)
			}
		})
	}
}

func TestLog(t *testing.T) {
	t.Parallel()
	f := newFixture(t, gittest.SHA1)
	r := gittest.Runner(t)
	ctx := t.Context()
	gittest.Git(t, f.sub, "config", "log.showSignature", "true")
	gittest.Git(t, f.sub, "config", "log.decorate", "full")
	gittest.Git(t, f.sub, "config", "color.ui", "always")

	subjects := func(lines []string) []string {
		out := make([]string, 0, len(lines))
		for _, line := range lines {
			sha, subject, _ := strings.Cut(line, " ")
			if len(sha) < 7 {
				t.Errorf("line %q does not start with an abbreviated SHA", line)
			}
			out = append(out, subject)
		}
		return out
	}
	tests := []struct {
		limit int
		revs  []string
		want  []string
	}{
		{2, []string{"HEAD"}, []string{"add feature two", "fix feature one"}},
		{0, nil, []string{
			"add feature two", "fix feature one", "release one",
			"add feature one", "initial commit",
		}},
		{0, []string{gittest.TagV100 + ".." + gittest.TagV101}, []string{"fix feature one"}},
		{10, []string{gittest.TagRC1, "^" + gittest.TagRC1}, nil},
		{
			0,
			[]string{gittest.TagV101 + "..." + gittest.TagRC1},
			[]string{"fix feature one", "release one"},
		},
	}
	for _, tt := range tests {
		lines, err := r.Log(ctx, f.sub, tt.limit, tt.revs...)
		if err != nil || !slices.Equal(subjects(lines), tt.want) {
			t.Errorf("Log(%d, %q) = %q, %v; want subjects %q", tt.limit, tt.revs, lines, err, tt.want)
		}
	}
	lines, err := r.Log(ctx, f.sub, 1, "HEAD")
	if err != nil || !strings.HasPrefix(f.commits[gittest.TagV200RC], strings.Fields(lines[0])[0]) {
		t.Errorf("Log(1) = %q, %v", lines, err)
	}
	_, err = r.Log(ctx, f.sub, 1, "nope")
	if _, ok := errors.AsType[*git.Error](err); !ok {
		t.Errorf("Log(nope) = %v, want *git.Error", err)
	}
}

func TestLogSignedCommit(t *testing.T) {
	t.Parallel()
	key := signingKey(t)
	f := newFixture(t, gittest.SHA1)
	signing := gittest.SSHSigningConfig(key)
	args := append(gittest.ConfigArgs(signing...),
		"commit", "--quiet", "--allow-empty", "--gpg-sign", "--message=signed")
	gittest.Git(t, f.sub, args...)
	gittest.Git(t, f.sub, "config", "log.showSignature", "true")
	r := gittest.Runner(t, signing...)
	lines, err := r.Log(t.Context(), f.sub, 0, "HEAD~2..HEAD")
	if err != nil || len(lines) != 2 || !strings.HasSuffix(lines[0], " signed") ||
		!strings.HasSuffix(lines[1], " add feature two") {
		t.Errorf("Log = %q, %v; want two commits without signature lines", lines, err)
	}
}

func TestSubmoduleDiff(t *testing.T) {
	t.Parallel()
	for _, format := range formats() {
		t.Run(format, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t, format)
			r := gittest.Runner(t)
			ctx := t.Context()
			out, err := r.SubmoduleDiff(ctx, f.super.Dir, f.path)
			if err != nil || out != "" {
				t.Errorf("SubmoduleDiff(unchanged) = %q, %v; want empty", out, err)
			}

			// Unstaged and staged changes are both shown.
			gittest.Git(t, f.super.Dir, "config", "diff.ignoreSubmodules", "all")
			old := f.commits[gittest.TagV100]
			tip := f.commits[gittest.TagV200RC]
			gittest.Git(t, f.sub, "checkout", "--quiet", old)
			want := "Submodule lib " + tip[:7] + ".." + old[:7] + " (rewind):\n" +
				"  < add feature two\n  < fix feature one\n"
			for range 2 {
				out, err = r.SubmoduleDiff(ctx, f.super.Dir, f.path)
				if err != nil || out != want {
					t.Errorf("SubmoduleDiff = %q, %v; want %q", out, err, want)
				}
				gittest.Git(t, f.super.Dir, "add", f.path)
			}
		})
	}
}

func TestSubmoduleDiffUnborn(t *testing.T) {
	t.Parallel()
	up := gittest.NewUpstream(t, gittest.SHA1)
	r := gittest.Runner(t)
	dir := gittest.InitRepo(t, gittest.SHA1)
	gittest.Git(t, dir, "submodule", "add", "--", up.Bare, "lib")
	head := gittest.Git(t, up.Work, "rev-parse", "HEAD")
	out, err := r.SubmoduleDiff(t.Context(), dir, "lib")
	want := "Submodule lib 0000000..." + head[:7] + " (new submodule)\n"
	if err != nil || out != want {
		t.Errorf("SubmoduleDiff(unborn) = %q, %v; want %q", out, err, want)
	}
	_, err = r.SubmoduleDiff(t.Context(), t.TempDir(), "lib")
	if _, ok := errors.AsType[*git.Error](err); !ok {
		t.Errorf("SubmoduleDiff outside a repository = %v, want *git.Error", err)
	}
}
