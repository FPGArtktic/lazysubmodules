// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package git

import (
	"context"
	"io"
)

// Fetch downloads branches and tags from a remote.
//
// Tags moved on the remote replace the local ones ("--force") and branches
// deleted on the remote are pruned. Nested submodules are not fetched,
// whatever fetch.recurseSubmodules and submodule.recurse say. The
// invocation is Online.
//
// Context: dir must be inside a repository; credentials are handled by the
// configured helpers.
// Return: nil, or *Error. When progress is non-nil, git diagnostics are
// streamed to it and Error.Stderr is empty.
func (r *Runner) Fetch(ctx context.Context, dir, remote string, progress io.Writer) error {
	_, err := r.Exec(ctx, Cmd{
		Dir: dir,
		Args: []string{"fetch", "--tags", "--force", "--prune", "--no-recurse-submodules",
			"--end-of-options", remote},
		Env:    Online.env(),
		Stderr: progress,
	})
	return err
}

// SubmoduleInit registers and checks out a submodule at the commit recorded in
// the superproject.
//
// Online, a missing submodule repository is cloned, and missing commits and
// objects are fetched. Offline, only the existing submodule repository in the
// git directory of the superproject is used, and a missing repository,
// commit or object of a partial clone is an error. The configured update
// strategy (submodule.<name>.update) is overridden by a checkout, and nested
// submodules are left alone, whatever submodule.recurse says.
//
// Context: root is the top level of the superproject; p is relative to it.
// Return: nil, or *Error. When progress is non-nil, the output of git is
// streamed to it.
func (r *Runner) SubmoduleInit(ctx context.Context, root, p string, network Network,
	progress io.Writer) error {
	c := Cmd{
		Dir: root,
		Args: []string{literalPathspecs, "-c", "submodule.recurse=false",
			"submodule", "update", "--init", "--checkout"},
		Env: network.env(),
	}
	if network == Offline {
		// "--no-fetch" does not prevent the initial clone; the missing
		// transports do.
		c.Args = append(c.Args, "--no-fetch")
	}
	c.Args = append(c.Args, "--", p)
	return r.stream(ctx, c, progress)
}

// SubmoduleAdd clones a repository and registers it as a submodule.
//
// The submodule name equals p. A non-empty branch is recorded as the native
// "branch" key and checked out. The invocation is Online.
//
// Context: root is the top level of the superproject; p is relative to it.
// Return: nil, or *Error. When progress is non-nil, the output of git is
// streamed to it.
func (r *Runner) SubmoduleAdd(ctx context.Context, root, url, p, branch string,
	progress io.Writer) error {
	args := []string{literalPathspecs, "submodule", "add"}
	if branch != "" {
		args = append(args, "-b", branch)
	}
	c := Cmd{Dir: root, Args: append(args, "--", url, p), Env: Online.env()}
	return r.stream(ctx, c, progress)
}

// stream runs c and sends the output and diagnostics of git to progress when
// it is non-nil.
func (r *Runner) stream(ctx context.Context, c Cmd, progress io.Writer) error {
	if progress != nil {
		c.Stdout = progress
		c.Stderr = progress
	}
	_, err := r.Exec(ctx, c)
	return err
}
