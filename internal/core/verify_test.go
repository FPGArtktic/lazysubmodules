// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package core_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/FPGArtktic/lazysubmodules/internal/core"
	"github.com/FPGArtktic/lazysubmodules/internal/git/gittest"
	"github.com/FPGArtktic/lazysubmodules/internal/manifest"
)

// branchChecks lists the checks of a fully verified branch or commit entry.
func branchChecks() []string {
	return []string{core.CheckLockEntry, core.CheckLockConfig, core.CheckLockCommit,
		core.CheckGitlink, core.CheckInitialized, core.CheckHead}
}

// tagChecks lists the checks of a fully verified tag or tag-pattern entry.
func tagChecks() []string {
	return append(branchChecks(), core.CheckTag)
}

// verifyOne verifies one submodule.
func (f *fixture) verifyOne(name string, opts core.VerifyOptions) (core.VerifyResult, error) {
	f.t.Helper()
	res, err := f.repo().Verify(f.t.Context(), []string{name}, opts)
	if len(res) != 1 {
		f.t.Fatalf("Verify(%s) = %v, %v; want one result", name, res, err)
	}
	return res[0], err
}

// checkNames lists the names of the checks of a result.
func checkNames(res core.VerifyResult) []string {
	names := make([]string, 0, len(res.Checks))
	for _, c := range res.Checks {
		names = append(names, c.Name)
	}
	return names
}

// wantChecks checks the names of the checks that ran and the names of those
// that failed; a failed check must mention detail.
func wantChecks(t *testing.T, res core.VerifyResult, names, failed []string, detail string) {
	t.Helper()
	if got := checkNames(res); !slices.Equal(got, names) {
		t.Errorf("%s: checks %q, want %q", res.Submodule.Name, got, names)
	}
	var gotFailed []string
	for _, c := range res.Checks {
		if c.Detail == "" {
			t.Errorf("%s: check %s has no detail", res.Submodule.Name, c.Name)
		}
		if !c.OK {
			gotFailed = append(gotFailed, c.Name)
			if !strings.Contains(c.Detail, detail) {
				t.Errorf("%s: check %s: detail %q, want %q", res.Submodule.Name, c.Name,
					c.Detail, detail)
			}
		}
	}
	if !slices.Equal(gotFailed, failed) {
		t.Errorf("%s: failed checks %q, want %q (%+v)", res.Submodule.Name, gotFailed, failed,
			res.Checks)
	}
	if res.OK() != (len(failed) == 0) {
		t.Errorf("%s: OK() = %v with failed checks %q", res.Submodule.Name, res.OK(), failed)
	}
}

func TestVerifyPasses(t *testing.T) {
	t.Parallel()
	for _, format := range formats() {
		t.Run(format, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t, format)
			v100, v101 := f.commits[gittest.TagV100], f.commits[gittest.TagV101]
			f.track("branch", manifest.ModeBranch, "main", "main", f.commits[gittest.TagV200RC])
			f.track("tag", manifest.ModeTag, gittest.TagV100, gittest.TagV100, v100)
			f.track("pattern", manifest.ModeTagPattern, "v1.*", gittest.TagV101, v101)
			f.track("commit", manifest.ModeCommit, v100[:7], v100, v100)
			f.add("unmanaged", "", "")
			res, err := f.repo().Verify(t.Context(), nil, core.VerifyOptions{})
			if err != nil || len(res) != 4 {
				t.Fatalf("Verify = %+v, %v", res, err)
			}
			want := [][]string{branchChecks(), tagChecks(), tagChecks(), branchChecks()}
			for i := range want {
				wantChecks(t, res[i], want[i], nil, "")
			}
			if res[0].Submodule.Name != "branch" || res[3].Submodule.Name != "commit" {
				t.Errorf("results out of order: %+v", res)
			}
		})
	}
}

func TestVerifyStagedButNotCommitted(t *testing.T) {
	t.Parallel()
	f := newFixture(t, gittest.SHA1)
	v100, v101 := f.commits[gittest.TagV100], f.commits[gittest.TagV101]
	f.track("lib", manifest.ModeTag, gittest.TagV100, gittest.TagV100, v100)
	// An update that is staged but not committed.
	f.configure("lib", manifest.ModeTag, gittest.TagV101)
	f.checkout("lib", v101)
	f.lock("lib", manifest.ModeTag, gittest.TagV101, v101)
	gittest.Git(t, f.super.Dir, "add", "--all")
	res, err := f.verifyOne("lib", core.VerifyOptions{})
	wantErr(t, "Verify", err, core.ErrVerify)
	if err == nil || err.Error() != "verification failed: lib" {
		t.Errorf("Verify = %v", err)
	}
	wantChecks(t, res, tagChecks(), []string{core.CheckGitlink},
		"HEAD of the superproject records "+v100[:12]+", locked "+v101[:12])

	// A new submodule that is staged but not committed.
	gittest.Git(t, f.super.Dir, "submodule", "add", "--quiet", "--name", "new", "--",
		f.up.Bare, "new")
	f.configure("new", manifest.ModeTag, gittest.TagV101)
	f.checkout("new", v101)
	f.lock("new", manifest.ModeTag, gittest.TagV101, v101)
	gittest.Git(t, f.super.Dir, "add", "--all")
	res, err = f.verifyOne("new", core.VerifyOptions{})
	wantErr(t, "Verify(new)", err, core.ErrVerify)
	wantChecks(t, res, tagChecks(), []string{core.CheckGitlink},
		"HEAD of the superproject records no gitlink at new")
}

func TestVerifyUnbornSuperproject(t *testing.T) {
	t.Parallel()
	f := newFixture(t, gittest.SHA1)
	dir := gittest.InitRepo(t, gittest.SHA1)
	gittest.Git(t, dir, "submodule", "add", "--quiet", "--name", "lib", "--", f.up.Bare, "lib")
	f.super = &gittest.Super{Dir: dir}
	f.configure("lib", manifest.ModeBranch, "main")
	f.lock("lib", manifest.ModeBranch, "main", f.commits[gittest.TagV200RC])
	res, err := f.verifyOne("lib", core.VerifyOptions{})
	wantErr(t, "Verify", err, core.ErrVerify)
	wantChecks(t, res, branchChecks(), []string{core.CheckGitlink}, "records no gitlink at lib")
	st := f.status("lib")
	wantState(t, st, core.StateOK, "up to date")
}

func TestVerifyMovedTag(t *testing.T) {
	t.Parallel()
	for _, mode := range []manifest.Mode{manifest.ModeTag, manifest.ModeTagPattern} {
		t.Run(string(mode), func(t *testing.T) {
			t.Parallel()
			f := newFixture(t, gittest.SHA256)
			f.track("lib", mode, gittest.TagV101, gittest.TagV101, f.commits[gittest.TagV101])
			res, err := f.verifyOne("lib", core.VerifyOptions{})
			if err != nil {
				t.Fatalf("Verify before the move = %v (%+v)", err, res)
			}
			f.up.MoveTag(t, gittest.TagV101, f.commits[gittest.TagV200RC])
			f.fetchLib()
			res, err = f.verifyOne("lib", core.VerifyOptions{})
			wantErr(t, "Verify", err, core.ErrVerify)
			wantChecks(t, res, tagChecks(), []string{core.CheckTag}, "moved tag")

			gittest.Git(t, f.dir("lib"), "tag", "--delete", gittest.TagV101)
			res, _ = f.verifyOne("lib", core.VerifyOptions{})
			failed := []string{core.CheckTag}
			if mode == manifest.ModeTagPattern {
				failed = []string{core.CheckLockConfig, core.CheckTag}
			}
			wantChecks(t, res, tagChecks(), failed, "does not exist")
		})
	}
}

func TestVerifyLockConfig(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name   string
		mode   manifest.Mode
		ref    string // configured
		lock   manifest.Mode
		locked string // locked ref; the commit is always that of v1.0.1
		detail string // "" when the check passes
	}{
		{"mode", manifest.ModeBranch, gittest.BranchStable, manifest.ModeTag, gittest.TagV101,
			"lock records mode tag, configuration has branch"},
		{"tag", manifest.ModeTag, gittest.TagV100, manifest.ModeTag, gittest.TagV101,
			"lock records tag v1.0.1, configuration has v1.0.0"},
		{"branch", manifest.ModeBranch, "main", manifest.ModeBranch, gittest.BranchStable,
			"lock records branch stable, configuration has main"},
		{"pattern", manifest.ModeTagPattern, "v1.0.0*", manifest.ModeTagPattern,
			gittest.TagV101, "locked tag v1.0.1 does not exist or does not match v1.0.0*"},
		{"pattern matches", manifest.ModeTagPattern, "v1.0.[1-9]", manifest.ModeTagPattern,
			gittest.TagV101, ""},
		{"commit", manifest.ModeCommit, "<v100>", manifest.ModeCommit, "<v101>",
			"configuration has <v100>"},
		{"commit abbreviated", manifest.ModeCommit, "<v101-short>", manifest.ModeCommit,
			"<v101>", ""},
		{"commit ref", manifest.ModeCommit, "<v101>", manifest.ModeCommit, "<v100>",
			"lock records ref"},
		{"invalid ref", manifest.ModeTag, "v1.0.1.lock", manifest.ModeTag, "v1.0.1.lock",
			`configured invalid tag "v1.0.1.lock"`},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t, gittest.SHA1)
			expand := func(s string) string {
				return strings.NewReplacer(
					"<v100>", f.commits[gittest.TagV100],
					"<v101>", f.commits[gittest.TagV101],
					"<v101-short>", f.commits[gittest.TagV101][:7],
				).Replace(s)
			}
			f.add("lib", c.mode, expand(c.ref))
			f.pin("lib", c.lock, expand(c.locked), f.commits[gittest.TagV101])
			res, err := f.verifyOne("lib", core.VerifyOptions{})
			names := branchChecks()
			if c.lock == manifest.ModeTag || c.lock == manifest.ModeTagPattern {
				names = tagChecks()
			}
			if c.name == "invalid ref" {
				// The tag v1.0.1.lock does not exist.
				wantChecks(t, res, names, []string{core.CheckLockConfig, core.CheckTag}, "")
				if !strings.Contains(res.Checks[1].Detail, c.detail) {
					t.Errorf("detail %q", res.Checks[1].Detail)
				}
				return
			}
			if c.detail == "" {
				if err != nil {
					t.Errorf("Verify = %v (%+v)", err, res)
				}
				wantChecks(t, res, names, nil, "")
				return
			}
			wantErr(t, "Verify", err, core.ErrVerify)
			wantChecks(t, res, names, []string{core.CheckLockConfig}, expand(c.detail))
		})
	}
}

func TestVerifyUninitialized(t *testing.T) {
	t.Parallel()
	f := newFixture(t, gittest.SHA1)
	v101 := f.commits[gittest.TagV101]
	f.track("tag", manifest.ModeTag, gittest.TagV101, gittest.TagV101, v101)
	f.track("pattern", manifest.ModeTagPattern, "v1.*", gittest.TagV101, v101)
	f.super.Deinit(t, "tag")
	f.super.Deinit(t, "pattern")
	uninitialized := []string{core.CheckLockEntry, core.CheckLockConfig, core.CheckLockCommit,
		core.CheckGitlink, core.CheckInitialized}
	res, err := f.repo().Verify(t.Context(), nil, core.VerifyOptions{Signatures: true})
	wantErr(t, "Verify", err, core.ErrVerify)
	if err == nil || err.Error() != "verification failed: tag, pattern" {
		t.Errorf("Verify = %v", err)
	}
	for _, r := range res {
		// The tags are listed in the repository in .git/modules.
		wantChecks(t, r, uninitialized, []string{core.CheckInitialized}, "not checked out")
	}

	f.removeModule("pattern")
	one, _ := f.verifyOne("pattern", core.VerifyOptions{})
	wantChecks(t, one, uninitialized, []string{core.CheckLockConfig, core.CheckInitialized}, "")
	if detail := one.Checks[1].Detail; !strings.Contains(detail, "cannot list tags") {
		t.Errorf("lock-config detail %q", detail)
	}
}

func TestVerifyMissingLockEntry(t *testing.T) {
	t.Parallel()
	f := newFixture(t, gittest.SHA1)
	f.add("lib", manifest.ModeTag, gittest.TagV101)
	f.add("other", manifest.ModeTag, gittest.TagV101)
	f.pin("other", manifest.ModeTag, gittest.TagV101, f.commits[gittest.TagV101])
	res, err := f.verifyOne("lib", core.VerifyOptions{Signatures: true})
	wantErr(t, "Verify", err, core.ErrVerify)
	wantChecks(t, res, []string{core.CheckLockEntry, core.CheckInitialized},
		[]string{core.CheckLockEntry}, "no entry in .lsm.lock")

	f.super.Deinit(t, "lib")
	res, _ = f.verifyOne("lib", core.VerifyOptions{})
	wantChecks(t, res, []string{core.CheckLockEntry, core.CheckInitialized},
		[]string{core.CheckLockEntry, core.CheckInitialized}, "")
}

func TestVerifyLockCommitLength(t *testing.T) {
	t.Parallel()
	f := newFixture(t, gittest.SHA256)
	f.add("lib", manifest.ModeTag, gittest.TagV101)
	// A SHA-1 name in a SHA-256 superproject.
	f.pin("lib", manifest.ModeTag, gittest.TagV101, f.commits[gittest.TagV101][:40])
	res, err := f.verifyOne("lib", core.VerifyOptions{})
	wantErr(t, "Verify", err, core.ErrVerify)
	wantChecks(t, res, []string{core.CheckLockEntry, core.CheckLockConfig,
		core.CheckLockCommit, core.CheckInitialized}, []string{core.CheckLockCommit},
		"64 hexadecimal digits")
}

func TestVerifySelection(t *testing.T) {
	t.Parallel()
	f := newFixture(t, gittest.SHA1)
	f.add("plain", "", "")
	r := f.repo()
	ctx := t.Context()
	res, err := r.Verify(ctx, nil, core.VerifyOptions{})
	if err != nil || len(res) != 0 {
		t.Errorf("Verify(unmanaged only) = %v, %v; want nothing", res, err)
	}
	res, err = r.Verify(ctx, []string{"plain", "plain"}, core.VerifyOptions{})
	wantErr(t, "Verify(plain)", err, core.ErrUnmanaged, core.ErrRefused)
	if res != nil || err == nil ||
		err.Error() != "plain: refused: submodule is not managed by lazysubmodules" {
		t.Errorf("Verify(plain) = %v, %v", res, err)
	}
	res, err = r.Verify(ctx, []string{"plain", "missing"}, core.VerifyOptions{})
	wantErr(t, "Verify(missing)", err, core.ErrNotFound)
	if res != nil || errors.Is(err, core.ErrRefused) {
		t.Errorf("Verify(missing) = %v, %v; want only ErrNotFound", res, err)
	}
}

func TestVerifySignatures(t *testing.T) {
	t.Parallel()
	key, ok := gittest.SSHSigningKey(t)
	if !ok {
		t.Skip("ssh-keygen not installed")
	}
	other, _ := gittest.SSHSigningKey(t)
	f := newFixture(t, gittest.SHA1, gittest.SSHSigningConfig(key)...)
	v100, v101 := f.commits[gittest.TagV100], f.commits[gittest.TagV101]
	f.up.SignedTag(t, "v3.0.0", v101, "signed release", key)
	f.up.SignedTag(t, "v3.0.1", v101, "signed by an untrusted key", other)
	gittest.Git(t, f.up.Work, append(gittest.ConfigArgs(gittest.SSHSigningConfig(key)...),
		"commit", "--quiet", "--allow-empty", "--gpg-sign", "--message=signed")...)
	gittest.Git(t, f.up.Work, "push", "--quiet", "origin", "HEAD:refs/heads/main")
	signed := gittest.Git(t, f.up.Work, "rev-parse", "HEAD")

	f.track("signed-tag", manifest.ModeTagPattern, "v3.0.0", "v3.0.0", v101)
	f.track("untrusted-tag", manifest.ModeTag, "v3.0.1", "v3.0.1", v101)
	f.track("unsigned-tag", manifest.ModeTag, gittest.TagV100, gittest.TagV100, v100)
	f.track("lightweight-tag", manifest.ModeTag, gittest.TagV101, gittest.TagV101, v101)
	f.track("signed-commit", manifest.ModeCommit, signed, signed, signed)
	f.track("signed-branch", manifest.ModeBranch, "main", "main", signed)
	f.track("unsigned-commit", manifest.ModeCommit, v100, v100, v100)
	r := f.repo()

	res, err := r.Verify(t.Context(), nil, core.VerifyOptions{})
	if err != nil || len(res) != 7 {
		t.Fatalf("Verify without signatures = %+v, %v", res, err)
	}
	for _, v := range res {
		if slices.Contains(checkNames(v), core.CheckSignature) {
			t.Errorf("%s: signature checked without the option", v.Submodule.Name)
		}
	}

	res, err = r.Verify(t.Context(), nil, core.VerifyOptions{Signatures: true})
	wantErr(t, "Verify", err, core.ErrVerify)
	want := "verification failed: untrusted-tag, unsigned-tag, lightweight-tag, unsigned-commit"
	if err == nil || err.Error() != want {
		t.Errorf("Verify = %v, want %s", err, want)
	}
	withSignature := func(names []string) []string {
		return append(slices.Clone(names), core.CheckSignature)
	}
	fail := []string{core.CheckSignature}
	wantChecks(t, res[0], withSignature(tagChecks()), nil, "")
	wantChecks(t, res[1], withSignature(tagChecks()), fail, "tag v3.0.1: ")
	wantChecks(t, res[2], withSignature(tagChecks()), fail, "tag v1.0.0: ")
	wantChecks(t, res[3], withSignature(tagChecks()), fail, "tag v1.0.1: ")
	wantChecks(t, res[4], withSignature(branchChecks()), nil, "")
	wantChecks(t, res[5], withSignature(branchChecks()), nil, "")
	wantChecks(t, res[6], withSignature(branchChecks()), fail, "commit "+v100[:12]+": ")
	if detail := res[2].Checks[7].Detail; !strings.Contains(detail, "no signature") {
		t.Errorf("unsigned tag detail %q", detail)
	}
}
