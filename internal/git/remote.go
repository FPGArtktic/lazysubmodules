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
// deleted on the remote are pruned. This is the only helper, together with
// SubmoduleAdd and SubmoduleInit without noFetch, that uses the network.
//
// Context: dir must be inside a repository; credentials are handled by the
// configured helpers.
// Return: nil, or *Error. When progress is non-nil, git diagnostics are
// streamed to it and Error.Stderr is empty.
func (r *Runner) Fetch(ctx context.Context, dir, remote string, progress io.Writer) error {
	_, err := r.Exec(ctx, Cmd{
		Dir:    dir,
		Args:   []string{"fetch", "--tags", "--force", "--prune", "--end-of-options", remote},
		Stderr: progress,
	})
	return err
}

// SubmoduleInit registers and checks out a submodule at the commit recorded in
// the superproject.
//
// Without noFetch, a missing submodule repository is cloned and missing
// commits are fetched. With noFetch, git may not use any transport: only the
// existing submodule repository in the git directory of the superproject is
// used, and a missing repository or commit is an error. The configured update
// strategy (submodule.<name>.update) is overridden by a checkout.
//
// Context: root is the top level of the superproject; p is relative to it.
// Return: nil, or *Error. When progress is non-nil, the output of git is
// streamed to it.
func (r *Runner) SubmoduleInit(ctx context.Context, root, p string, noFetch bool,
	progress io.Writer) error {
	c := Cmd{
		Dir:  root,
		Args: []string{literalPathspecs, "submodule", "update", "--init", "--checkout"},
	}
	if noFetch {
		// "--no-fetch" does not prevent the initial clone. An empty list of
		// allowed protocols does, and it overrides protocol.<name>.allow.
		c.Args = append(c.Args, "--no-fetch")
		c.Env = []string{"GIT_ALLOW_PROTOCOL="}
	}
	c.Args = append(c.Args, "--", p)
	return r.stream(ctx, c, progress)
}

// SubmoduleAdd clones a repository and registers it as a submodule.
//
// The submodule name equals p. A non-empty branch is recorded as the native
// "branch" key and checked out.
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
	return r.stream(ctx, Cmd{Dir: root, Args: append(args, "--", url, p)}, progress)
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
