// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/FPGArtktic/lazysubmodules/internal/git"
	"github.com/FPGArtktic/lazysubmodules/internal/git/gittest"
	"github.com/FPGArtktic/lazysubmodules/internal/manifest"
)

func TestCheckRef(t *testing.T) {
	t.Parallel()
	sha1 := &Repo{git: gittest.Runner(t), hexLen: sha1HexLen}
	sha256 := &Repo{git: sha1.git, hexLen: sha256HexLen}
	hex40, hex64 := strings.Repeat("a1", 20), strings.Repeat("b2", 32)
	for _, c := range []struct {
		repo   *Repo
		mode   manifest.Mode
		ref    string
		reason string // "" when valid
	}{
		{sha1, manifest.ModeBranch, "main", ""},
		{sha1, manifest.ModeBranch, "feature/x-1", ""},
		{sha1, manifest.ModeBranch, "", "empty value"},
		{sha1, manifest.ModeBranch, "-main", `starts with "-"`},
		{sha1, manifest.ModeBranch, "a b", "white space"},
		{sha1, manifest.ModeBranch, "a\tb", "white space"},
		{sha1, manifest.ModeBranch, "a\x7fb", "control characters"},
		{sha1, manifest.ModeTag, "v1\u00a0x", "white space"},
		{sha1, manifest.ModeTag, "v1\u009b31m", "control characters"},
		{sha1, manifest.ModeTag, "v1\u200b", "control characters"},
		{sha1, manifest.ModeTag, "wydanie-\u017c", ""},
		{sha1, manifest.ModeBranch, "a..b", "not a valid branch name"},
		{sha1, manifest.ModeBranch, "@{-1}", "not a valid branch name"},
		{sha1, manifest.ModeBranch, "a:b", "not a valid branch name"},
		{sha1, manifest.ModeTag, "v1.0.0", ""},
		{sha1, manifest.ModeTag, "v1.0.0-rc.1", ""},
		{sha1, manifest.ModeTag, "v1*", "not a valid tag name"},
		{sha1, manifest.ModeTag, "v1.lock", "not a valid tag name"},
		{sha1, manifest.ModeTag, "v1^{}", "not a valid tag name"},
		{sha1, manifest.ModeTagPattern, "v1.*", ""},
		{sha1, manifest.ModeTagPattern, "v[0-9].?", ""},
		{sha1, manifest.ModeTagPattern, "*", ""},
		{sha1, manifest.ModeTagPattern, "v1.*.lock", "not a valid tag pattern name"},
		{sha1, manifest.ModeTagPattern, "v1:*", "not a valid tag pattern name"},
		{sha1, manifest.ModeTagPattern, "v1 *", "white space"},
		{sha1, manifest.ModeTagPattern, "-*", `starts with "-"`},
		{sha1, manifest.ModeCommit, "abcdef0", ""},
		{sha1, manifest.ModeCommit, hex40, ""},
		{sha1, manifest.ModeCommit, hex64, "7 to 40 hexadecimal digits"},
		{sha1, manifest.ModeCommit, "abcdef", "7 to 40"},
		{sha1, manifest.ModeCommit, "ABCDEF0", "lowercase hexadecimal"},
		{sha1, manifest.ModeCommit, "abcdefg", "lowercase hexadecimal"},
		{sha1, manifest.ModeCommit, "HEAD~10", "lowercase hexadecimal"},
		{sha1, manifest.ModeCommit, "-abcdef0", `starts with "-"`},
		{sha256, manifest.ModeCommit, hex40, ""},
		{sha256, manifest.ModeCommit, hex64, ""},
		{sha256, manifest.ModeCommit, hex64 + "0", "7 to 64"},
		{sha1, manifest.Mode(""), "main", "unknown tracking mode"},
		{sha1, manifest.Mode("tags"), "main", "unknown tracking mode"},
	} {
		err := c.repo.checkRef(t.Context(), c.mode, c.ref)
		if c.reason == "" {
			if err != nil {
				t.Errorf("checkRef(%s, %q) = %v", c.mode, c.ref, err)
			}
			continue
		}
		invalid, ok := errors.AsType[*invalidError](err)
		if !ok || !errors.Is(err, ErrInvalidArgument) || !strings.Contains(err.Error(), c.reason) {
			t.Errorf("checkRef(%s, %q) = %v, want an invalid argument (%s)",
				c.mode, c.ref, err, c.reason)
			continue
		}
		if !strings.HasPrefix(invalid.Error(), "invalid ") {
			t.Errorf("checkRef(%s, %q): message %q", c.mode, c.ref, invalid)
		}
	}
}

func TestCheckRefGitFailure(t *testing.T) {
	t.Parallel()
	r := &Repo{git: gittest.Runner(t), hexLen: sha1HexLen}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := r.checkRef(ctx, manifest.ModeBranch, "main")
	if _, ok := errors.AsType[*git.Error](err); !ok || errors.Is(err, ErrInvalidArgument) {
		t.Errorf("checkRef(canceled) = %v, want a plain *git.Error", err)
	}
	err = r.checkConfiguredRef(ctx, manifest.Submodule{Name: "lib",
		Mode: manifest.ModeTag, Ref: "v1"})
	if _, ok := errors.AsType[*git.Error](err); !ok || errors.Is(err, ErrMissingRef) ||
		!strings.HasPrefix(err.Error(), "git ") {
		t.Errorf("checkConfiguredRef(canceled) = %v, want a plain *git.Error", err)
	}
	if named := withName("lib", err); !strings.HasPrefix(named.Error(), "lib: git ") {
		t.Errorf("withName(lib, %v) = %v", err, named)
	}
	missing := &refError{name: "lib", detail: "gone"}
	if got := withName("lib", missing); got.Error() != missing.Error() {
		t.Errorf("withName(lib, *refError) = %v, want %v", got, missing)
	}
	if got := withName("lib", nil); got != nil {
		t.Errorf("withName(lib, nil) = %v", got)
	}
}

func TestCleanPath(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"lib":                 "lib",
		"a/b/c":               "a/b/c",
		"./a//b/":             "a/b",
		"a/../b":              "b",
		"a b":                 "a b",
		"lib.git":             "lib.git",
		"":                    "",
		".":                   "",
		"./":                  "",
		"a/..":                "",
		"../lib":              "",
		"a/../../lib":         "",
		"/abs":                "",
		"-lib":                "",
		"./-lib":              "",
		"a\nb":                "",
		"a\x00b":              "",
		"a\x7fb":              "",
		"dir/\x1b[31m":        "",
		"a\u009bb":            "",
		".git":                "",
		"a/.git/b":            "",
		"a/.GIT":              "",
		"./.Git/../x":         "x",
		".gitx/.git.d":        ".gitx/.git.d",
		"\u017c\u00f3\u0142w": "\u017c\u00f3\u0142w",
	} {
		got, err := cleanPath(in)
		if want == "" {
			if !errors.Is(err, ErrInvalidArgument) || got != "" {
				t.Errorf("cleanPath(%q) = %q, %v; want an invalid argument", in, got, err)
			}
			continue
		}
		if err != nil || got != want {
			t.Errorf("cleanPath(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
}

func TestCheckURL(t *testing.T) {
	t.Parallel()
	for url, ok := range map[string]bool{
		"https://git.example.org/linux.git": true,
		"git@example.org:linux.git":         true,
		"../sibling.git":                    true,
		"/srv/git/repo with space.git":      true,
		"":                                  false,
		"-uhttps://example.org":             false,
		"--upload-pack=touch /tmp/x":        false,
		"https://example.org/\nx":           false,
		"https://example.org/\x00":          false,
		"https://example.org/\u0085":        false,
	} {
		err := checkURL(url)
		if (err == nil) != ok || (err != nil && !errors.Is(err, ErrInvalidArgument)) {
			t.Errorf("checkURL(%q) = %v, want valid=%v", url, err, ok)
		}
	}
}

func TestSelectSubmodules(t *testing.T) {
	t.Parallel()
	subs := []manifest.Submodule{
		{Name: "c", Path: "c", Mode: manifest.ModeTag, Ref: "v1"},
		{Name: "b", Path: "b"},
		{Name: "a", Path: "a", Mode: manifest.ModeBranch, Ref: "main"},
		{Name: "x.y", Path: "d"},
	}
	names := func(s []manifest.Submodule) []string {
		out := []string{}
		for _, sub := range s {
			out = append(out, sub.Name)
		}
		return out
	}
	for _, c := range []struct {
		names []string
		sel   selection
		want  []string
		err   error
		msg   string
	}{
		{nil, selectAll, []string{"c", "b", "a", "x.y"}, nil, ""},
		{nil, selectManaged, []string{"c", "a"}, nil, ""},
		{[]string{"a", "c", "a"}, selectManaged, []string{"c", "a"}, nil, ""},
		{[]string{"x.y", "b"}, selectAll, []string{"b", "x.y"}, nil, ""},
		{[]string{"b", "a", "b"}, selectManaged, nil, ErrUnmanaged,
			"b: refused: submodule is not managed by lazysubmodules"},
		{[]string{"x.y", "b"}, selectManaged, nil, ErrUnmanaged, "b: refused: submodule is " +
			"not managed by lazysubmodules\nx.y: refused: submodule is not managed by " +
			"lazysubmodules"},
		{[]string{"A"}, selectAll, nil, ErrNotFound, "A: no such submodule"},
		{[]string{"b", "", "z", ""}, selectManaged, nil, ErrNotFound,
			"\"\": no such submodule\nz: no such submodule"},
	} {
		got, err := selectSubmodules(subs, c.names, c.sel)
		switch {
		case c.err == nil && (err != nil || !slices.Equal(names(got), c.want)):
			t.Errorf("select(%q, %d) = %q, %v; want %q", c.names, c.sel, names(got), err, c.want)
		case c.err != nil && (got != nil || !errors.Is(err, c.err) || err.Error() != c.msg):
			t.Errorf("select(%q, %d) = %q, %q; want %q", c.names, c.sel, names(got), err, c.msg)
		}
	}
	if got, err := selectSubmodules(nil, nil, selectAll); err != nil || got != nil {
		t.Errorf("select(no submodules) = %v, %v", got, err)
	}
}

func TestMessageHelpers(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"lib":                        "lib",
		"a b/c.d":                    "a b/c.d",
		"":                           `""`,
		"a\x1b[m":                    `"a\x1b[m"`,
		`a"b`:                        `"a\"b"`,
		`a\b`:                        `"a\\b"`,
		"a\u200bb":                   `"a\u200bb"`,
		"za\u017c\u00f3\u0142\u0107": "za\u017c\u00f3\u0142\u0107",
	} {
		if got := displayName(in); got != want {
			t.Errorf("displayName(%q) = %s, want %s", in, got, want)
		}
	}
	if got := printable("ok\x1b[31m\tred\u00e9"); got != "ok\ufffd[31m\ufffdred\u00e9" {
		t.Errorf("printable = %q", got)
	}
	for in, want := range map[string]string{
		"":                  "",
		"abc":               "abc",
		"0123456789ab":      "0123456789ab",
		"0123456789abcdef0": "0123456789ab",
	} {
		if got := abbrev(in); got != want {
			t.Errorf("abbrev(%q) = %q, want %q", in, got, want)
		}
	}
	if got := displayCommit(""); got != "(unborn)" {
		t.Errorf("displayCommit(\"\") = %q", got)
	}
	err := &refError{name: "a\x1b", detail: "tag v1 does not exist"}
	if !errors.Is(err, ErrMissingRef) || !errors.Is(err, ErrRefused) ||
		err.Error() != `"a\x1b": refused: ref not found in local refs: tag v1 does not exist` {
		t.Errorf("refError = %q", err)
	}
	err = &refError{name: "lib", detail: "invalid tag \"v\x1b\": not valid", invalid: true}
	if !errors.Is(err, ErrMissingRef) ||
		err.Error() != "lib: refused: bad ref in .gitmodules: invalid tag \"v\ufffd\": not valid" {
		t.Errorf("refError(invalid) = %q", err)
	}
}

func TestSignatureDetail(t *testing.T) {
	t.Parallel()
	gitErr := &git.Error{Args: []string{"verify-tag"}, ExitCode: 1,
		Stderr: "\n  error: no signature found\x1b[0m\nsecond line"}
	for err, want := range map[error]string{
		fmt.Errorf("tag v1: %w: %w", git.ErrBadSignature, gitErr): "error: no signature " +
			"found\x1b[0m",
		fmt.Errorf("x: %w", &git.Error{ExitCode: 1}): "no valid signature",
		git.ErrBadSignature:                          "no valid signature",
	} {
		if got := signatureDetail(err); got != want {
			t.Errorf("signatureDetail(%v) = %q, want %q", err, got, want)
		}
	}
	// Details and reasons are sanitized when they are recorded.
	v := &verifier{}
	v.add(CheckSignature, false, "tag v1: "+signatureDetail(gitErr))
	if got := v.checks[0].Detail; got != "tag v1: error: no signature found\ufffd[0m" {
		t.Errorf("recorded detail %q", got)
	}
	var st Status
	st.setState(StateMissingRef, "no tag matching v\u009b* names a commit")
	if st.Reason != "no tag matching v\ufffd* names a commit" {
		t.Errorf("recorded reason %q", st.Reason)
	}
}

func TestForEach(t *testing.T) {
	t.Parallel()
	const n, limit = 50, 4
	var running, peak atomic.Int32
	done := make([]bool, n)
	err := forEach(t.Context(), n, limit, func(_ context.Context, i int) error {
		cur := running.Add(1)
		defer running.Add(-1)
		for {
			old := peak.Load()
			if cur <= old || peak.CompareAndSwap(old, cur) {
				break
			}
		}
		time.Sleep(time.Millisecond)
		done[i] = true
		return nil
	})
	if err != nil || slices.Contains(done, false) {
		t.Errorf("forEach = %v, done %v", err, done)
	}
	if p := peak.Load(); p > limit || p < 2 {
		t.Errorf("peak concurrency %d, want 2..%d", p, limit)
	}
	if err := forEach(t.Context(), 0, limit, nil); err != nil {
		t.Errorf("forEach(0) = %v", err)
	}
	if err := forEach(t.Context(), 3, 0, func(context.Context, int) error { return nil }); err != nil {
		t.Errorf("forEach(limit 0) = %v", err)
	}
}

func TestForEachFirstErrorCancels(t *testing.T) {
	t.Parallel()
	errFirst := errors.New("first")
	var calls atomic.Int32
	err := forEach(t.Context(), 100, 2, func(ctx context.Context, i int) error {
		calls.Add(1)
		if i == 1 {
			return errFirst
		}
		<-ctx.Done()
		return fmt.Errorf("item %d: %w", i, ctx.Err())
	})
	if !errors.Is(err, errFirst) {
		t.Errorf("forEach = %v, want the first error", err)
	}
	if c := calls.Load(); c > 3 {
		t.Errorf("%d calls started after the error", c)
	}
}

func TestForEachParentCanceled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	var calls atomic.Int32
	err := forEach(ctx, 10, 1, func(context.Context, int) error {
		if calls.Add(1) == 2 {
			cancel()
		}
		return nil
	})
	if !errors.Is(err, context.Canceled) || calls.Load() > 3 {
		t.Errorf("forEach = %v after %d calls, want context.Canceled", err, calls.Load())
	}
}

func TestHasSymlink(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for _, dir := range []string{"a/b", "real"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	gittest.WriteFile(t, filepath.Join(root, "file"), "x")
	for link, dest := range map[string]string{"a/link": "../real", "top": "real"} {
		if err := os.Symlink(dest, filepath.Join(root, link)); err != nil {
			t.Fatal(err)
		}
	}
	for p, want := range map[string]bool{
		"a/b":          false,
		"a/b/missing":  false,
		"missing/x":    false,
		"file/x":       false,
		"a/link":       true,
		"a/link/x":     true,
		"top":          true,
		"top/missing":  true,
		"a/b/../link":  true,
		"real/../file": false,
	} {
		got, err := hasSymlink(root, p)
		if err != nil || got != want {
			t.Errorf("hasSymlink(%s) = %v, %v; want %v", p, got, err, want)
		}
	}
}

func TestLocateErrors(t *testing.T) {
	t.Parallel()
	super := gittest.NewSuper(t, gittest.SHA1)
	r := &Repo{git: gittest.Runner(t), root: super.Dir, hexLen: sha1HexLen}
	sub := manifest.Submodule{Name: "lib", Path: "dir/lib", Mode: manifest.ModeTag, Ref: "v1"}
	modules := filepath.Join(super.Dir, ".git", "modules")
	gittest.WriteFile(t, filepath.Join(modules, "lib"), "x")

	// Without a gitlink in the index, the path is not inspected.
	loc, err := r.locate(t.Context(), sub)
	if err != nil || loc.tracked() || loc.gitDir != "" || loc.populated || loc.symlink {
		t.Errorf("locate(no gitlink) = %+v, %v", loc, err)
	}
	if err := loc.refusal("lib"); !errors.Is(err, ErrNotSubmodule) {
		t.Errorf("refusal(no gitlink) = %v", err)
	}
	head := gittest.Git(t, super.Dir, "rev-parse", "HEAD")
	gittest.Git(t, super.Dir, "update-index", "--add", "--cacheinfo", "160000,"+head+",dir/lib")

	// A regular file in place of the module repository is no repository.
	loc, err = r.locate(t.Context(), sub)
	if err != nil || loc.gitlink != head || loc.populated || loc.repo != git.RepoInvalid ||
		loc.symlink {
		t.Errorf("locate(file) = %+v, %v", loc, err)
	}
	if err := loc.refusal("lib"); err != nil {
		t.Errorf("refusal(gitlink) = %v", err)
	}
	if dir, ok := loc.refDir(); ok || dir != "" {
		t.Errorf("refDir = %q, %v", dir, ok)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := r.locate(ctx, sub); !errors.Is(err, context.Canceled) {
		t.Errorf("locate(canceled) = %v, want context.Canceled", err)
	}

	if os.Geteuid() == 0 {
		t.Skip("permissions do not apply to root")
	}
	for _, dir := range []string{filepath.Join(super.Dir, "dir"), modules} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(dir, 0); err != nil {
			t.Fatal(err)
		}
		_, err := r.locate(t.Context(), sub)
		if err := os.Chmod(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if !errors.Is(err, os.ErrPermission) {
			t.Errorf("locate(%s not accessible) = %v, want a permission error", dir, err)
		}
	}
}

func TestLocateRepository(t *testing.T) {
	t.Parallel()
	up := gittest.NewUpstream(t, gittest.SHA1)
	up.Commit(t, "one")
	super := gittest.NewSuper(t, gittest.SHA1)
	super.AddSubmodule(t, "lib", up)
	super.Deinit(t, "lib")
	sub := manifest.Submodule{Name: "lib", Path: "lib", URL: up.Bare, Mode: manifest.ModeTag,
		Ref: "v1"}
	module := filepath.Join(super.Dir, ".git", "modules", "lib")
	plain := &Repo{git: gittest.Runner(t), root: super.Dir, hexLen: sha1HexLen}
	// Before git 2.45, this setting refuses the repositories inside a ".git"
	// directory, which "git submodule" still initializes.
	explicit := &Repo{git: gittest.Runner(t, "safe.bareRepository=explicit"), root: super.Dir,
		hexLen: sha1HexLen}
	ctx := t.Context()
	locate := func(r *Repo) location {
		t.Helper()
		loc, err := r.locate(ctx, sub)
		if err != nil || loc.populated || loc.gitDir != module {
			t.Fatalf("locate = %+v, %v", loc, err)
		}
		return loc
	}

	loc := locate(plain)
	if dir, ok := loc.refDir(); loc.repo != git.RepoUsable || !ok || dir != module {
		t.Errorf("deinitialized: repo %v, refDir %q, %t", loc.repo, dir, ok)
	}
	if err := loc.initRefusal(sub); err != nil {
		t.Errorf("deinitialized: initRefusal = %v", err)
	}
	loc = locate(explicit)
	if dir, ok := loc.refDir(); ok != (loc.repo == git.RepoUsable) ||
		(ok && dir != module) || loc.repo == git.RepoInvalid {
		t.Errorf("safe.bareRepository: repo %v, refDir %q, %t", loc.repo, dir, ok)
	}

	if err := os.RemoveAll(module); err != nil {
		t.Fatal(err)
	}
	loc = locate(plain)
	if _, ok := loc.refDir(); loc.repo != git.RepoMissing || ok ||
		loc.initRefusal(sub) != nil {
		t.Errorf("removed: repo %v, refDir %t, initRefusal %v", loc.repo, ok, loc.initRefusal(sub))
	}

	// An empty directory in place of the repository.
	if err := os.Mkdir(module, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, r := range []*Repo{plain, explicit} {
		loc = locate(r)
		err := loc.initRefusal(sub)
		want := "lib: refused: the submodule repository directory is not a repository: " +
			module + " (remove it)"
		if _, ok := loc.refDir(); loc.repo != git.RepoInvalid || ok ||
			!errors.Is(err, ErrNotRepository) || err.Error() != want {
			t.Errorf("empty: repo %v, refDir %t, initRefusal %v", loc.repo, ok, err)
		}
		if got := uninitializedReason(loc); !strings.Contains(got, "not a repository") {
			t.Errorf("empty: reason %q", got)
		}
	}

	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := plain.locate(canceled, sub); !errors.Is(err, context.Canceled) {
		t.Errorf("locate(canceled) = %v, want context.Canceled", err)
	}
}
