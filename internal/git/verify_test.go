// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package git_test

import (
	"errors"
	"testing"

	"github.com/FPGArtktic/lazysubmodules/internal/git"
	"github.com/FPGArtktic/lazysubmodules/internal/git/gittest"
)

// signingKey returns an SSH signing key or skips the test.
func signingKey(t *testing.T) string {
	t.Helper()
	key, ok := gittest.SSHSigningKey(t)
	if !ok {
		t.Skip("ssh-keygen not installed")
	}
	return key
}

// wantBadSignature checks that err wraps both ErrBadSignature and *git.Error.
func wantBadSignature(t *testing.T, what string, err error) {
	t.Helper()
	if !errors.Is(err, git.ErrBadSignature) {
		t.Errorf("%s = %v, want ErrBadSignature", what, err)
	}
	if _, ok := errors.AsType[*git.Error](err); !ok {
		t.Errorf("%s = %v, want a wrapped *git.Error", what, err)
	}
}

func TestVerifyTag(t *testing.T) {
	t.Parallel()
	key := signingKey(t)
	other := signingKey(t)
	for _, format := range formats() {
		t.Run(format, func(t *testing.T) {
			t.Parallel()
			up := gittest.NewUpstream(t, format)
			head := up.Commit(t, "release")
			up.SignedTag(t, "v1.0.0", head, "signed release", key)
			up.SignedTag(t, "v1.0.1", head, "signed by an untrusted key", other)
			up.AnnotatedTag(t, "v1.0.2", head, "unsigned release")
			up.Tag(t, "v1.0.3", head)

			r := gittest.Runner(t, gittest.SSHSigningConfig(key)...)
			ctx := t.Context()
			if err := r.VerifyTag(ctx, up.Work, "v1.0.0"); err != nil {
				t.Errorf("VerifyTag(signed) = %v", err)
			}
			for _, tag := range []string{"v1.0.1", "v1.0.2", "v1.0.3", "missing"} {
				wantBadSignature(t, "VerifyTag("+tag+")", r.VerifyTag(ctx, up.Work, tag))
			}

			// Without an allowed signers file nothing can be trusted.
			plain := gittest.Runner(t, "gpg.format=ssh")
			wantBadSignature(t, "VerifyTag(no trust)", plain.VerifyTag(ctx, up.Work, "v1.0.0"))
		})
	}
}

func TestVerifyTagNotShadowed(t *testing.T) {
	t.Parallel()
	key := signingKey(t)
	up := gittest.NewUpstream(t, gittest.SHA1)
	head := up.Commit(t, "release")
	// "stash" alone resolves to refs/stash, a commit, before refs/tags/stash.
	up.SignedTag(t, "stash", head, "signed release", key)
	gittest.Git(t, up.Work, "update-ref", "refs/stash", head)
	r := gittest.Runner(t, gittest.SSHSigningConfig(key)...)
	if err := r.VerifyTag(t.Context(), up.Work, "stash"); err != nil {
		t.Errorf("VerifyTag(stash) = %v", err)
	}
}

func TestVerifyTagAfterFetch(t *testing.T) {
	t.Parallel()
	key := signingKey(t)
	f := newFixture(t, gittest.SHA1)
	f.up.SignedTag(t, "v4.0.0", f.commits[gittest.TagV101], "signed", key)
	r := gittest.Runner(t, gittest.SSHSigningConfig(key)...)
	ctx := t.Context()
	if err := r.Fetch(ctx, f.sub, "origin", nil); err != nil {
		t.Fatal(err)
	}
	if err := r.VerifyTag(ctx, f.sub, "v4.0.0"); err != nil {
		t.Errorf("VerifyTag = %v", err)
	}
	// A moved tag is no longer signed.
	f.up.MoveTag(t, "v4.0.0", f.commits[gittest.TagV200RC])
	if err := r.Fetch(ctx, f.sub, "origin", nil); err != nil {
		t.Fatal(err)
	}
	wantBadSignature(t, "VerifyTag(moved)", r.VerifyTag(ctx, f.sub, "v4.0.0"))
}

func TestVerifyCommit(t *testing.T) {
	t.Parallel()
	key := signingKey(t)
	for _, format := range formats() {
		t.Run(format, func(t *testing.T) {
			t.Parallel()
			up := gittest.NewUpstream(t, format)
			unsigned := up.Commit(t, "unsigned")
			args := append(gittest.ConfigArgs(gittest.SSHSigningConfig(key)...),
				"commit", "--quiet", "--allow-empty", "--gpg-sign", "--message=signed")
			gittest.Git(t, up.Work, args...)
			signed := gittest.Git(t, up.Work, "rev-parse", "HEAD")

			r := gittest.Runner(t, gittest.SSHSigningConfig(key)...)
			ctx := t.Context()
			if err := r.VerifyCommit(ctx, up.Work, signed); err != nil {
				t.Errorf("VerifyCommit(signed) = %v", err)
			}
			wantBadSignature(t, "VerifyCommit(unsigned)", r.VerifyCommit(ctx, up.Work, unsigned))
			tree := gittest.Git(t, up.Work, "rev-parse", "HEAD^{tree}")
			wantBadSignature(t, "VerifyCommit(tree)", r.VerifyCommit(ctx, up.Work, tree))
		})
	}
}

func TestVerifyOutsideRepository(t *testing.T) {
	t.Parallel()
	r := gittest.Runner(t)
	ctx := t.Context()
	dir := t.TempDir()
	for name, err := range map[string]error{
		"VerifyTag":    r.VerifyTag(ctx, dir, "v1"),
		"VerifyCommit": r.VerifyCommit(ctx, dir, "HEAD"),
	} {
		gitErr, ok := errors.AsType[*git.Error](err)
		if !ok || gitErr.ExitCode != 128 || errors.Is(err, git.ErrBadSignature) {
			t.Errorf("%s outside a repository = %v, want a plain *git.Error", name, err)
		}
	}
}
