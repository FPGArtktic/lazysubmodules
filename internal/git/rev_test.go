// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package git_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/FPGArtktic/lazysubmodules/internal/git"
	"github.com/FPGArtktic/lazysubmodules/internal/git/gittest"
)

// formats lists the object formats every repository test runs with.
func formats() []string {
	return []string{gittest.SHA1, gittest.SHA256}
}

// hexLen returns the length of a full object name in format.
func hexLen(format string) int {
	if format == gittest.SHA256 {
		return 64
	}
	return 40
}

// fixture is a superproject with one submodule cloned from a tagged upstream.
type fixture struct {
	up      *gittest.Upstream
	commits map[string]string
	super   *gittest.Super
	path    string // submodule path relative to super.Dir
	sub     string // absolute submodule working tree
}

func newFixture(t *testing.T, format string) *fixture {
	t.Helper()
	up, commits := gittest.NewTaggedUpstream(t, format)
	super := gittest.NewSuper(t, format)
	path := super.AddSubmodule(t, "lib", up)
	return &fixture{
		up:      up,
		commits: commits,
		super:   super,
		path:    path,
		sub:     filepath.Join(super.Dir, path),
	}
}

// realDir resolves symbolic links in dir.
func realDir(t *testing.T, dir string) string {
	t.Helper()
	res, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestTopLevel(t *testing.T) {
	t.Parallel()
	f := newFixture(t, gittest.SHA1)
	r := gittest.Runner(t)
	sub := filepath.Join(f.super.Dir, "nested", "dir")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	for dir, want := range map[string]string{
		f.super.Dir: f.super.Dir,
		sub:         f.super.Dir,
		f.sub:       f.sub,
	} {
		got, err := r.TopLevel(t.Context(), dir)
		if err != nil {
			t.Fatal(err)
		}
		if got != realDir(t, want) {
			t.Errorf("TopLevel(%s) = %q, want %q", dir, got, want)
		}
	}
	_, err := r.TopLevel(t.Context(), t.TempDir())
	if _, ok := errors.AsType[*git.Error](err); !ok {
		t.Errorf("TopLevel outside a repository: error = %v, want *git.Error", err)
	}
}

func TestGitPath(t *testing.T) {
	t.Parallel()
	f := newFixture(t, gittest.SHA1)
	r := gittest.Runner(t)
	ctx := t.Context()
	want := filepath.Join(f.super.Dir, ".git", "modules", "lib")

	got, err := r.GitPath(ctx, f.super.Dir, "modules/lib")
	if err != nil || got != want {
		t.Errorf("GitPath(root) = %q, %v; want %q", got, err, want)
	}
	subdir := filepath.Join(f.super.Dir, "a", "b")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err = r.GitPath(ctx, subdir, "modules/lib")
	if err != nil || got != want {
		t.Errorf("GitPath(subdir) = %q, %v; want %q", got, err, want)
	}
	got, err = r.GitPath(ctx, f.sub, "HEAD")
	if err != nil || realDir(t, got) != realDir(t, filepath.Join(want, "HEAD")) {
		t.Errorf("GitPath(submodule) = %q, %v; want %q", got, err, filepath.Join(want, "HEAD"))
	}
	if !info(t, want).IsDir() {
		t.Errorf("%s is not a directory", want)
	}
}

func TestSubmoduleGitDir(t *testing.T) {
	t.Parallel()
	f := newFixture(t, gittest.SHA1)
	r := gittest.Runner(t)
	ctx := t.Context()
	modules := filepath.Join(f.super.Dir, ".git", "modules")
	for name, want := range map[string]string{
		"lib":   filepath.Join(modules, "lib"),
		"a/b":   filepath.Join(modules, "a", "b"),
		"x.y.z": filepath.Join(modules, "x.y.z"),
	} {
		got, err := r.SubmoduleGitDir(ctx, f.super.Dir, name)
		if err != nil || got != want {
			t.Errorf("SubmoduleGitDir(%s) = %q, %v; want %q", name, got, err, want)
		}
	}
	// Without the extension, git ignores submodule.<name>.gitdir.
	gittest.Git(t, f.super.Dir, "config", "submodule.lib.gitdir", "elsewhere")
	got, err := r.SubmoduleGitDir(ctx, f.super.Dir, "lib")
	if want := filepath.Join(modules, "lib"); err != nil || got != want {
		t.Errorf("SubmoduleGitDir(lib) = %q, %v; want %q", got, err, want)
	}
}

func TestSubmoduleGitDirPathConfig(t *testing.T) {
	t.Parallel()
	r := gittest.Runner(t)
	ctx := t.Context()
	dir := t.TempDir()
	gittest.Git(t, dir, "-c", "init.defaultSubmodulePathConfig=true", "init", "--quiet", dir)
	if gittest.Git(t, dir, "config", "--get", "--default=", "extensions.submodulePathConfig") == "" {
		t.Skip("git does not support extensions.submodulePathConfig")
	}
	up := gittest.NewUpstream(t, gittest.SHA1)
	gittest.Git(t, dir, "submodule", "add", "--name", "lib/x", "--", up.Bare, "lib3")
	stored := gittest.Git(t, dir, "config", "--get", "submodule.lib/x.gitdir")
	want := filepath.Join(dir, filepath.FromSlash(stored))
	got, err := r.SubmoduleGitDir(ctx, dir, "lib/x")
	if err != nil || got != want || !info(t, got).IsDir() {
		t.Errorf("SubmoduleGitDir(lib/x) = %q, %v; want %q (%s)", got, err, want, stored)
	}
	gitDir := gittest.Git(t, filepath.Join(dir, "lib3"), "rev-parse", "--absolute-git-dir")
	if realDir(t, gitDir) != realDir(t, got) {
		t.Errorf("git directory of lib3 = %q, want %q", gitDir, got)
	}

	absolute := filepath.Join(t.TempDir(), "abs")
	gittest.Git(t, dir, "config", "submodule.abs.gitdir", absolute)
	if got, err := r.SubmoduleGitDir(ctx, dir, "abs"); err != nil || got != absolute {
		t.Errorf("SubmoduleGitDir(abs) = %q, %v; want %q", got, err, absolute)
	}
	// Git sets the variable when it initializes the submodule.
	unset := filepath.Join(dir, ".git", "modules", "unset")
	if got, err := r.SubmoduleGitDir(ctx, dir, "unset"); err != nil || got != unset {
		t.Errorf("SubmoduleGitDir(unset) = %q, %v; want %q", got, err, unset)
	}
	gittest.Git(t, dir, "config", "extensions.submodulePathConfig", "maybe")
	if _, err := r.SubmoduleGitDir(ctx, dir, "lib/x"); err == nil {
		t.Errorf("SubmoduleGitDir with an invalid extension value = nil, want an error")
	}
}

func TestRepositoryHelpersOutsideRepository(t *testing.T) {
	t.Parallel()
	r := gittest.Runner(t)
	ctx := t.Context()
	dir := t.TempDir()
	_, gitPathErr := r.GitPath(ctx, dir, "HEAD")
	_, gitDirErr := r.SubmoduleGitDir(ctx, dir, "lib")
	_, formatErr := r.ObjectFormat(ctx, dir)
	_, tagsErr := r.ListTags(ctx, dir, "v*")
	_, branchesErr := r.ListRemoteBranches(ctx, dir, "origin")
	_, indexErr := r.IndexGitlink(ctx, dir, "lib")
	_, treeErr := r.TreeGitlink(ctx, dir, "HEAD", "lib")
	for name, err := range map[string]error{
		"GitPath":            gitPathErr,
		"SubmoduleGitDir":    gitDirErr,
		"ObjectFormat":       formatErr,
		"ListTags":           tagsErr,
		"ListRemoteBranches": branchesErr,
		"IndexGitlink":       indexErr,
		"TreeGitlink":        treeErr,
	} {
		gitErr, ok := errors.AsType[*git.Error](err)
		if !ok || gitErr.ExitCode <= 0 || errors.Is(err, git.ErrRefNotFound) {
			t.Errorf("%s outside a repository = %v, want a plain *git.Error", name, err)
		}
	}
}

// info returns the file information of an existing path.
func info(t *testing.T, path string) os.FileInfo {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return fi
}

func TestObjectFormat(t *testing.T) {
	t.Parallel()
	r := gittest.Runner(t)
	for _, format := range formats() {
		got, err := r.ObjectFormat(t.Context(), gittest.InitRepo(t, format))
		if err != nil || got != format {
			t.Errorf("ObjectFormat = %q, %v; want %q", got, err, format)
		}
	}
}

func TestResolveCommit(t *testing.T) {
	t.Parallel()
	for _, format := range formats() {
		t.Run(format, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t, format)
			r := gittest.Runner(t)
			ctx := t.Context()
			tests := map[string]string{
				"refs/tags/" + gittest.TagRC1:    f.commits[gittest.TagRC1],
				"refs/tags/" + gittest.TagV100:   f.commits[gittest.TagV100],
				"refs/tags/" + gittest.TagV200RC: f.commits[gittest.TagV200RC],
				"refs/remotes/origin/stable":     f.commits[gittest.TagV101],
				f.commits[gittest.TagV100]:       f.commits[gittest.TagV100],
				f.commits[gittest.TagV100][:10]:  f.commits[gittest.TagV100],
			}
			for rev, want := range tests {
				got, err := r.ResolveCommit(ctx, f.sub, rev)
				if err != nil || got != want {
					t.Errorf("ResolveCommit(%s) = %q, %v; want %q", rev, got, err, want)
				}
				if len(got) != hexLen(format) {
					t.Errorf("ResolveCommit(%s) length = %d", rev, len(got))
				}
			}
			// The annotated tag itself is a different object.
			tagObject := gittest.Git(t, f.sub, "rev-parse", "refs/tags/"+gittest.TagV100)
			if tagObject == f.commits[gittest.TagV100] {
				t.Errorf("tag %s is not annotated", gittest.TagV100)
			}
		})
	}
}

func TestResolveCommitNotFound(t *testing.T) {
	t.Parallel()
	f := newFixture(t, gittest.SHA1)
	r := gittest.Runner(t)
	missing := []string{
		"refs/tags/v9.9.9",
		"refs/tags/with space",
		"refs/remotes/origin/nope",
		"0123456789abcdef0123456789abcdef01234567",
		gittest.Git(t, f.sub, "rev-parse", "HEAD^{tree}"),
	}
	for _, rev := range missing {
		got, err := r.ResolveCommit(t.Context(), f.sub, rev)
		if got != "" || !errors.Is(err, git.ErrRefNotFound) {
			t.Errorf("ResolveCommit(%q) = %q, %v; want ErrRefNotFound", rev, got, err)
		}
	}
	_, err := r.ResolveCommit(t.Context(), t.TempDir(), "HEAD")
	if errors.Is(err, git.ErrRefNotFound) {
		t.Errorf("ResolveCommit outside a repository: %v, want a git error", err)
	}
}

func TestHead(t *testing.T) {
	t.Parallel()
	for _, format := range formats() {
		t.Run(format, func(t *testing.T) {
			t.Parallel()
			r := gittest.Runner(t)
			ctx := t.Context()
			unborn := gittest.InitRepo(t, format)
			if got, err := r.Head(ctx, unborn); got != "" || !errors.Is(err, git.ErrRefNotFound) {
				t.Errorf("Head(unborn) = %q, %v; want ErrRefNotFound", got, err)
			}
			f := newFixture(t, format)
			got, err := r.Head(ctx, f.sub)
			if err != nil || got != f.commits[gittest.TagV200RC] {
				t.Errorf("Head(submodule) = %q, %v; want %q", got, err, f.commits[gittest.TagV200RC])
			}
		})
	}
}

func TestListTags(t *testing.T) {
	t.Parallel()
	f := newFixture(t, gittest.SHA1)
	r := gittest.Runner(t)
	// A user setting must not change the order.
	gittest.Git(t, f.sub, "config", "tag.sort", "refname")
	gittest.Git(t, f.sub, "config", "column.ui", "always")
	tests := []struct {
		pattern string
		want    []string
	}{
		{"", []string{gittest.TagV200RC, gittest.TagV101, gittest.TagV100, gittest.TagRC1}},
		{"v1.*", []string{gittest.TagV101, gittest.TagV100, gittest.TagRC1}},
		{"v1.0.0*", []string{gittest.TagV100, gittest.TagRC1}},
		{"v2.*", []string{gittest.TagV200RC}},
		{"v1.0.0", []string{gittest.TagV100}},
		{"nomatch*", nil},
	}
	for _, tt := range tests {
		got, err := r.ListTags(t.Context(), f.sub, tt.pattern)
		if err != nil || !slices.Equal(got, tt.want) {
			t.Errorf("ListTags(%q) = %q, %v; want %q", tt.pattern, got, err, tt.want)
		}
	}
}

func TestListTagsEmptyRepository(t *testing.T) {
	t.Parallel()
	r := gittest.Runner(t)
	got, err := r.ListTags(t.Context(), gittest.InitRepo(t, gittest.SHA1), "")
	if err != nil || got != nil {
		t.Errorf("ListTags = %q, %v; want nil, nil", got, err)
	}
}

func TestListRemoteBranches(t *testing.T) {
	t.Parallel()
	f := newFixture(t, gittest.SHA1)
	r := gittest.Runner(t)
	ctx := t.Context()
	f.up.Branch(t, "feature/x", f.commits[gittest.TagRC1])
	gittest.Git(t, f.sub, "remote", "add", "origin2", f.up.Bare)
	gittest.Git(t, f.sub, "fetch", "--quiet", "origin")
	gittest.Git(t, f.sub, "fetch", "--quiet", "origin2")
	gittest.Git(t, f.sub, "remote", "set-head", "origin", "main")

	got, err := r.ListRemoteBranches(ctx, f.sub, "origin")
	want := []string{"feature/x", "main", gittest.BranchStable}
	if err != nil || !slices.Equal(got, want) {
		t.Errorf("ListRemoteBranches(origin) = %q, %v; want %q", got, err, want)
	}
	got, err = r.ListRemoteBranches(ctx, f.sub, "nope")
	if err != nil || got != nil {
		t.Errorf("ListRemoteBranches(nope) = %q, %v; want nil, nil", got, err)
	}
}

func TestCheckBranchName(t *testing.T) {
	t.Parallel()
	r := gittest.Runner(t)
	for _, name := range []string{"main", "feature/x", "release-1.0", "v1.0.0-rc.1"} {
		if err := r.CheckBranchName(t.Context(), name); err != nil {
			t.Errorf("CheckBranchName(%q) = %v", name, err)
		}
	}
	invalid := []string{"", "a..b", "-x", "a b", "x~1", "a^", "a:b", "tail/", "x.lock", "@{-1}", "-"}
	for _, name := range invalid {
		if err := r.CheckBranchName(t.Context(), name); !errors.Is(err, git.ErrInvalidRefName) {
			t.Errorf("CheckBranchName(%q) = %v, want ErrInvalidRefName", name, err)
		}
	}
}

func TestCheckTagName(t *testing.T) {
	t.Parallel()
	r := gittest.Runner(t)
	for _, name := range []string{"v1.0.0", "v1.0.0-rc.1", "release/2026", "x"} {
		if err := r.CheckTagName(t.Context(), name); err != nil {
			t.Errorf("CheckTagName(%q) = %v", name, err)
		}
	}
	invalid := []string{"", "v1..0", "a b", "x~1", "a^", "a:b", "v1*", "v1?", "v[1]", "x.lock", "a@{b"}
	for _, name := range invalid {
		if err := r.CheckTagName(t.Context(), name); !errors.Is(err, git.ErrInvalidRefName) {
			t.Errorf("CheckTagName(%q) = %v, want ErrInvalidRefName", name, err)
		}
	}
}
