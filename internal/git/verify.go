// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package git

import (
	"context"
	"fmt"
)

// VerifyTag checks the signature of an annotated tag.
//
// GPG, SSH and X.509 signatures are verified as configured in git (for
// example gpg.format and gpg.ssh.allowedSignersFile).
//
// Context: dir must be inside a repository; tag is a tag name without the
// "refs/tags/" prefix.
// Return: nil for a good signature, an error wrapping ErrBadSignature and
// *Error when the tag is missing, unsigned or not trusted, or *Error when git
// could not run.
func (r *Runner) VerifyTag(ctx context.Context, dir, tag string) error {
	_, err := r.Run(ctx, dir, "verify-tag", "--end-of-options", "refs/tags/"+tag)
	return signatureError("tag "+tag, err)
}

// VerifyCommit checks the signature of a commit.
//
// Context: dir must be inside a repository.
// Return: nil for a good signature, an error wrapping ErrBadSignature and
// *Error when the commit is missing, unsigned or not trusted, or *Error when
// git could not run.
func (r *Runner) VerifyCommit(ctx context.Context, dir, commit string) error {
	_, err := r.Run(ctx, dir, "verify-commit", "--end-of-options", commit)
	return signatureError("commit "+commit, err)
}

// signatureError maps a failed verification of what to ErrBadSignature.
// Git exits with status 1 for missing, unsigned or untrusted objects and with
// other statuses for fatal errors.
func signatureError(what string, err error) error {
	if exitCode(err) == 1 {
		return fmt.Errorf("%s: %w: %w", what, ErrBadSignature, err)
	}
	return err
}
