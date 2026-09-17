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
	"github.com/FPGArtktic/lazysubmodules/internal/git/gittest"
	"github.com/FPGArtktic/lazysubmodules/internal/lock"
	"github.com/FPGArtktic/lazysubmodules/internal/manifest"
)

// resolveCase is one resolution of the submodule "lib".
type resolveCase struct {
	mode    manifest.Mode
	ref     string
	opts    core.ResolveOptions
	locked  *lock.Entry
	wantRef string // "" expects ErrMissingRef
	want    string // expected commit; the commit of wantRef by default
}

// check resolves c and compares the result.
func (c resolveCase) check(t *testing.T, f *fixture, r *core.Repo) {
	t.Helper()
	sub := f.lib()
	sub.Mode, sub.Ref = c.mode, c.ref
	got, err := r.Resolve(t.Context(), sub, c.locked, c.opts)
	what := "Resolve(" + string(c.mode) + " " + c.ref + ")"
	if c.wantRef == "" {
		wantErr(t, what, err, core.ErrMissingRef, core.ErrRefused)
		if errors.Is(err, core.ErrInvalidArgument) {
			t.Errorf("%s = %v, must not wrap ErrInvalidArgument", what, err)
		}
		if err != nil && !strings.HasPrefix(err.Error(), "lib: refused: ") {
			t.Errorf("%s: message %q lacks the submodule name", what, err)
		}
		return
	}
	want := core.Resolution{Mode: c.mode, Ref: c.wantRef, Commit: c.want}
	if want.Commit == "" {
		want.Commit = f.commits[c.wantRef]
	}
	if err != nil || got != want {
		t.Errorf("%s = %+v, %v; want %+v", what, got, err, want)
	}
}

func TestResolveModes(t *testing.T) {
	t.Parallel()
	for _, format := range formats() {
		t.Run(format, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t, format)
			f.add("lib", manifest.ModeTag, gittest.TagV100)
			r := f.repo()
			tip := f.commits[gittest.TagV200RC]
			v100 := f.commits[gittest.TagV100]
			incl := core.ResolveOptions{IncludePrerelease: true}
			tree := gittest.Git(t, f.dir("lib"), "rev-parse", "HEAD^{tree}")
			tagObject := gittest.Git(t, f.dir("lib"), "rev-parse", "refs/tags/"+gittest.TagV100)
			if tagObject == v100 {
				t.Fatalf("%s is not an annotated tag", gittest.TagV100)
			}
			cases := []resolveCase{
				// Branches resolve through refs/remotes/origin.
				{mode: manifest.ModeBranch, ref: "main", wantRef: "main", want: tip},
				{mode: manifest.ModeBranch, ref: gittest.BranchStable,
					wantRef: gittest.BranchStable, want: f.commits[gittest.TagV101]},
				{mode: manifest.ModeBranch, ref: "missing"},
				{mode: manifest.ModeBranch, ref: "origin/main"},
				// Lightweight and annotated tags resolve to their commit.
				{mode: manifest.ModeTag, ref: gittest.TagV101, wantRef: gittest.TagV101},
				{mode: manifest.ModeTag, ref: gittest.TagV100, wantRef: gittest.TagV100},
				{mode: manifest.ModeTag, ref: gittest.TagV200RC, wantRef: gittest.TagV200RC},
				{mode: manifest.ModeTag, ref: gittest.TagRC1, wantRef: gittest.TagRC1},
				{mode: manifest.ModeTag, ref: "v9.9.9"},
				{mode: manifest.ModeTag, ref: "main"},
				// Patterns select the highest version; pre-releases only
				// on request, and v1.0.0-rc.1 sorts below v1.0.0.
				{mode: manifest.ModeTagPattern, ref: "v1.*", wantRef: gittest.TagV101},
				{mode: manifest.ModeTagPattern, ref: "v*", wantRef: gittest.TagV101},
				{mode: manifest.ModeTagPattern, ref: "*", wantRef: gittest.TagV101},
				{mode: manifest.ModeTagPattern, ref: "v*", opts: incl, wantRef: gittest.TagV200RC},
				{mode: manifest.ModeTagPattern, ref: "v1.0.0*", wantRef: gittest.TagV100},
				{mode: manifest.ModeTagPattern, ref: "v1.0.0*", opts: incl, wantRef: gittest.TagV100},
				{mode: manifest.ModeTagPattern, ref: "v1.0.[0-9]", wantRef: gittest.TagV101},
				{mode: manifest.ModeTagPattern, ref: "v1.0.0-*", opts: incl, wantRef: gittest.TagRC1},
				{mode: manifest.ModeTagPattern, ref: "v1.0.0-*"},
				{mode: manifest.ModeTagPattern, ref: "v2.*"},
				{mode: manifest.ModeTagPattern, ref: "v2.*", opts: incl, wantRef: gittest.TagV200RC},
				{mode: manifest.ModeTagPattern, ref: "v3.*", opts: incl},
				// Commits may be abbreviated; the lock gets the full name.
				{mode: manifest.ModeCommit, ref: v100, wantRef: v100, want: v100},
				{mode: manifest.ModeCommit, ref: v100[:7], wantRef: v100, want: v100},
				{mode: manifest.ModeCommit, ref: v100[:12], wantRef: v100, want: v100},
				{mode: manifest.ModeCommit, ref: strings.Repeat("0", len(v100))},
				{mode: manifest.ModeCommit, ref: "0000000"},
				{mode: manifest.ModeCommit, ref: tree},
				// A tag object is no commit, even though it peels to one.
				{mode: manifest.ModeCommit, ref: tagObject},
				{mode: manifest.ModeCommit, ref: tagObject[:10]},
				// Invalid configured refs are missing refs, not usage errors.
				{mode: manifest.ModeTag, ref: "-v1"},
				{mode: manifest.ModeTag, ref: "v1 0"},
				{mode: manifest.ModeTag, ref: ""},
				{mode: manifest.ModeTag, ref: "v1..0"},
				{mode: manifest.ModeTag, ref: "v1\x1b[0m"},
				{mode: manifest.ModeBranch, ref: "@{-1}"},
				{mode: manifest.ModeTagPattern, ref: "v1.*~"},
				{mode: manifest.ModeTagPattern, ref: "-*"},
				{mode: manifest.ModeCommit, ref: strings.ToUpper(v100)},
				{mode: manifest.ModeCommit, ref: v100[:6]},
				{mode: manifest.ModeCommit, ref: v100 + "0"},
				{mode: manifest.ModeCommit, ref: "main"},
				{mode: manifest.ModeCommit, ref: "HEAD~1"},
				{mode: manifest.Mode("bogus"), ref: "main"},
			}
			for _, c := range cases {
				c.check(t, f, r)
			}
		})
	}
}

func TestResolveVersionSort(t *testing.T) {
	t.Parallel()
	f := newFixture(t, gittest.SHA1)
	head := f.commits[gittest.TagV101]
	for _, tag := range []string{"v1.9.0", "v1.10.0-rc.1", "v1.10.0-rc.2"} {
		f.up.Tag(t, tag, f.commits[gittest.TagV100])
	}
	f.up.AnnotatedTag(t, "v1.10.0", head, "release")
	f.add("lib", manifest.ModeTagPattern, "v1.*")
	r := f.repo()
	incl := core.ResolveOptions{IncludePrerelease: true}
	resolveCase{mode: manifest.ModeTagPattern, ref: "v1.*", wantRef: "v1.10.0", want: head}.
		check(t, f, r)
	resolveCase{mode: manifest.ModeTagPattern, ref: "v1.*", opts: incl, wantRef: "v1.10.0",
		want: head}.check(t, f, r)

	gittest.Git(t, f.dir("lib"), "tag", "--delete", "v1.10.0")
	resolveCase{mode: manifest.ModeTagPattern, ref: "v1.*", wantRef: "v1.9.0",
		want: f.commits[gittest.TagV100]}.check(t, f, r)
	resolveCase{mode: manifest.ModeTagPattern, ref: "v1.*", opts: incl, wantRef: "v1.10.0-rc.2",
		want: f.commits[gittest.TagV100]}.check(t, f, r)
}

func TestResolveNeverDowngrades(t *testing.T) {
	t.Parallel()
	f := newFixture(t, gittest.SHA1)
	stable, pre := f.commits[gittest.TagV100], f.commits[gittest.TagV101]
	f.up.Tag(t, "v2.0.3", stable)
	f.up.Tag(t, "v2.1.0-rc.1", pre)
	f.up.Tag(t, "v2.1.0-rc.2", f.commits[gittest.TagV200RC])
	f.add("lib", manifest.ModeTagPattern, "v2.*")
	r := f.repo()
	lockedAt := func(mode manifest.Mode, ref string) *lock.Entry {
		return &lock.Entry{Name: "lib", Mode: mode, Ref: ref, Commit: pre}
	}
	rc1 := lockedAt(manifest.ModeTagPattern, "v2.1.0-rc.1")
	v2 := func(locked *lock.Entry, wantRef, want string) resolveCase {
		return resolveCase{mode: manifest.ModeTagPattern, ref: "v2.*", locked: locked,
			wantRef: wantRef, want: want}
	}
	// Without a lock, the highest stable tag wins.
	v2(nil, "v2.0.3", stable).check(t, f, r)
	// The locked pre-release stays selected, but a newer pre-release is
	// not taken without the option.
	v2(rc1, "v2.1.0-rc.1", pre).check(t, f, r)
	// Only a tag-pattern lock entry counts.
	v2(lockedAt(manifest.ModeTag, "v2.1.0-rc.1"), "v2.0.3", stable).check(t, f, r)
	// A locked stable tag does not admit pre-releases.
	v2(lockedAt(manifest.ModeTagPattern, "v2.0.3"), "v2.0.3", stable).check(t, f, r)
	// The locked tag must still match the configured pattern.
	resolveCase{mode: manifest.ModeTagPattern, ref: "v2.0.*", locked: rc1, wantRef: "v2.0.3",
		want: stable}.check(t, f, r)
	// A locked tag that disappeared is no candidate.
	v2(lockedAt(manifest.ModeTagPattern, "v2.1.0-rc.0"), "v2.0.3", stable).check(t, f, r)

	// A release above the locked pre-release is selected.
	f.up.Tag(t, "v2.1.0", f.commits[gittest.TagV200RC])
	f.fetchLib()
	v2(rc1, "v2.1.0", f.commits[gittest.TagV200RC]).check(t, f, r)

	// Once the locked tag is deleted, the resolution falls back.
	gittest.Git(t, f.dir("lib"), "tag", "--delete", "v2.1.0", "v2.1.0-rc.1")
	v2(rc1, "v2.0.3", stable).check(t, f, r)
}

func TestResolveSkipsTagsOfTrees(t *testing.T) {
	t.Parallel()
	f := newFixture(t, gittest.SHA1)
	f.up.Tag(t, "v1.2.0", f.commits[gittest.TagV101]+"^{tree}")
	f.add("lib", manifest.ModeTagPattern, "v1.*")
	r := f.repo()
	resolveCase{mode: manifest.ModeTagPattern, ref: "v1.*", wantRef: gittest.TagV101}.
		check(t, f, r)
	resolveCase{mode: manifest.ModeTag, ref: "v1.2.0"}.check(t, f, r)
	resolveCase{mode: manifest.ModeTagPattern, ref: "v1.2.*"}.check(t, f, r)
}

func TestResolveAbbreviatedCommitShadowedByRef(t *testing.T) {
	t.Parallel()
	f := newFixture(t, gittest.SHA1)
	f.add("lib", manifest.ModeCommit, f.commits[gittest.TagV100])
	r := f.repo()
	short := f.commits[gittest.TagV100][:7]
	// Git prefers a ref named like the abbreviation.
	gittest.Git(t, f.dir("lib"), "tag", short, f.commits[gittest.TagRC1])
	sub := f.lib()
	sub.Ref = short
	_, err := r.Resolve(t.Context(), sub, nil, core.ResolveOptions{})
	wantErr(t, "Resolve(shadowed)", err, core.ErrMissingRef)
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Errorf("Resolve(shadowed) = %v, want an ambiguity message", err)
	}
	// The full name is never shadowed.
	full := f.commits[gittest.TagV100]
	gittest.Git(t, f.dir("lib"), "tag", full, f.commits[gittest.TagRC1])
	resolveCase{mode: manifest.ModeCommit, ref: full, wantRef: full, want: full}.check(t, f, r)
	// A ref that points to the commit itself is harmless.
	gittest.Git(t, f.dir("lib"), "tag", "--force", short, full)
	resolveCase{mode: manifest.ModeCommit, ref: short, wantRef: full, want: full}.check(t, f, r)
}

func TestResolveUninitialized(t *testing.T) {
	t.Parallel()
	for _, format := range formats() {
		t.Run(format, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t, format)
			f.add("lib", manifest.ModeTag, gittest.TagV100)
			f.super.Deinit(t, "lib")
			r := f.repo()
			// The refs are read from the repository in .git/modules.
			resolveCase{mode: manifest.ModeTag, ref: gittest.TagV100,
				wantRef: gittest.TagV100}.check(t, f, r)
			resolveCase{mode: manifest.ModeTagPattern, ref: "v*",
				wantRef: gittest.TagV101}.check(t, f, r)
			resolveCase{mode: manifest.ModeBranch, ref: "main", wantRef: "main",
				want: f.commits[gittest.TagV200RC]}.check(t, f, r)
			resolveCase{mode: manifest.ModeTag, ref: "v9"}.check(t, f, r)

			f.removeModule("lib")
			_, err := r.Resolve(t.Context(), f.lib(), nil, core.ResolveOptions{})
			wantErr(t, "Resolve(no repository)", err, core.ErrUninitialized, core.ErrRefused)
		})
	}
}

func TestResolveIgnoresSuperprojectRefs(t *testing.T) {
	t.Parallel()
	f := newFixture(t, gittest.SHA1)
	f.add("lib", manifest.ModeTag, gittest.TagV100)
	f.super.Deinit(t, "lib")
	f.removeModule("lib")
	// An empty directory in place of the module repository makes git fall
	// back to the superproject, which has a tag of the same name.
	if err := os.MkdirAll(filepath.Join(f.super.Dir, ".git", "modules", "lib"), 0o755); err != nil {
		t.Fatal(err)
	}
	gittest.Git(t, f.super.Dir, "tag", gittest.TagV100)
	r := f.repo()
	_, err := r.Resolve(t.Context(), f.lib(), nil, core.ResolveOptions{})
	wantErr(t, "Resolve(empty module directory)", err, core.ErrUninitialized)
}

func TestResolveRemovedWorktree(t *testing.T) {
	t.Parallel()
	f := newFixture(t, gittest.SHA1)
	f.add("lib", manifest.ModeTag, gittest.TagV100)
	// The module repository still names the removed working tree, so git
	// refuses to run in it.
	if err := os.RemoveAll(f.dir("lib")); err != nil {
		t.Fatal(err)
	}
	_, err := f.repo().Resolve(t.Context(), f.lib(), nil, core.ResolveOptions{})
	wantErr(t, "Resolve(removed worktree)", err, core.ErrUninitialized)
}

func TestResolveUnmanaged(t *testing.T) {
	t.Parallel()
	f := newFixture(t, gittest.SHA1)
	f.add("lib", "", "")
	_, err := f.repo().Resolve(t.Context(), f.lib(), nil, core.ResolveOptions{})
	wantErr(t, "Resolve(unmanaged)", err, core.ErrUnmanaged, core.ErrRefused)
}

func TestIsPrerelease(t *testing.T) {
	t.Parallel()
	for tag, want := range map[string]bool{
		"v1.0.0":        false,
		"1.0.0":         false,
		"v1.0.0-rc.1":   true,
		"1.0.0-beta":    true,
		"v6.6-rc3":      true,
		"v6.6":          false,
		"release-2.1":   false,
		"release-2.1-1": true,
		"foo-bar":       false,
		"":              false,
		"2024-01-01":    true,
		"v2024.01.01":   false,
		"v1.0.0+build":  false,
		"v1.0.0_rc1":    false,
		"stable-v2-rc1": true,
		"v1-":           true,
	} {
		if got := core.IsPrerelease(tag); got != want {
			t.Errorf("IsPrerelease(%q) = %v, want %v", tag, got, want)
		}
	}
}
