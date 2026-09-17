// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package core

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/FPGArtktic/lazysubmodules/internal/manifest"
)

// commandWaitDelay bounds how long a canceled command may take to exit
// after SIGTERM before it is killed.
const commandWaitDelay = 10 * time.Second

// ForeachOptions describes the command that Foreach runs.
type ForeachOptions struct {
	// Args is the command and its arguments. The command is looked up in
	// PATH unless it contains a "/"; a relative path is relative to the
	// submodule.
	Args []string
	// Stdin, Stdout and Stderr are the standard streams of the commands;
	// nil means the null device. Stderr also receives the notes about
	// skipped submodules.
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

// Foreach runs a command in every managed submodule that is checked out.
//
// The submodules are visited in .gitmodules order, and the command runs
// with the submodule working tree as its current directory. It is executed
// directly, without a shell. Submodules that are not checked out are
// skipped with a note on opts.Stderr. The first command that fails ends
// Foreach.
//
// The command inherits the environment of the process, without the
// variables that tie git to a repository (such as GIT_DIR), and gets the
// variables of "git submodule foreach" plus two more:
//
//   - name: the submodule name;
//   - sm_path: the path recorded in .gitmodules;
//   - displaypath: the path relative to the directory given to Open;
//   - sha1: the commit checked out in the submodule (git uses the commit
//     recorded in the superproject instead); empty for an unborn HEAD;
//   - toplevel: the absolute path of the superproject;
//   - LSM_MODE and LSM_REF: the tracking mode and the configured ref.
//
// Context: the commands may do anything, including network access; a
// canceled context stops the running command with SIGTERM.
// Return: nil when every command succeeded; an error wrapping
// ErrInvalidArgument when opts.Args is empty; an error naming the
// submodule and wrapping the failure of the command, such as
// *exec.ExitError; an error from reading .gitmodules; or *git.Error.
func (r *Repo) Foreach(ctx context.Context, opts ForeachOptions) error {
	if len(opts.Args) == 0 || opts.Args[0] == "" {
		return &invalidError{what: "command", value: "", reason: "empty command"}
	}
	subs, err := r.Submodules(ctx)
	if err != nil {
		return err
	}
	subs, err = selectSubmodules(subs, nil, selectManaged)
	if err != nil {
		return err
	}
	env := slices.DeleteFunc(os.Environ(), func(kv string) bool {
		name, _, _ := strings.Cut(kv, "=")
		return isRepoEnv(name)
	})
	for _, sub := range subs {
		if err := r.runIn(ctx, sub, env, opts); err != nil {
			return wrapName(sub.Name, err)
		}
	}
	return nil
}

// runIn runs the command of opts in one submodule, or writes a note when
// the submodule is not checked out.
func (r *Repo) runIn(ctx context.Context, sub manifest.Submodule, env []string,
	opts ForeachOptions,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	loc, err := r.locate(ctx, sub)
	if err != nil {
		return err
	}
	if !loc.populated {
		if opts.Stderr != nil {
			fmt.Fprintf(opts.Stderr, "skipping %s: %s\n",
				displayName(sub.Name), uninitializedReason(loc))
		}
		return nil
	}
	head, err := r.head(ctx, loc.worktree)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, opts.Args[0], opts.Args[1:]...)
	cmd.Dir = loc.worktree
	cmd.Env = append(slices.Clip(env),
		"name="+sub.Name,
		"sm_path="+sub.Path,
		"displaypath="+r.displayPath(loc.worktree, sub.Path),
		"sha1="+head,
		"toplevel="+r.root,
		"LSM_MODE="+string(sub.Mode),
		"LSM_REF="+sub.Ref,
	)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = opts.Stdin, opts.Stdout, opts.Stderr
	cmd.Cancel = func() error {
		return cmd.Process.Signal(syscall.SIGTERM)
	}
	cmd.WaitDelay = commandWaitDelay
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", opts.Args[0], err)
	}
	return nil
}

// displayPath returns the path of a submodule working tree relative to the
// directory the Repo was opened from, "/"-separated; smPath when there is
// no such relative path.
func (r *Repo) displayPath(worktree, smPath string) string {
	rel, err := filepath.Rel(r.cwd, worktree)
	if err != nil {
		return smPath
	}
	return filepath.ToSlash(rel)
}

// isRepoEnv reports whether an environment variable ties git to a
// particular repository, as listed by "git rev-parse --local-env-vars",
// apart from the variables that carry command line configuration. "git
// submodule foreach" removes them as well.
func isRepoEnv(name string) bool {
	switch name {
	case "GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_CONFIG", "GIT_OBJECT_DIRECTORY",
		"GIT_DIR", "GIT_WORK_TREE", "GIT_IMPLICIT_WORK_TREE", "GIT_GRAFT_FILE",
		"GIT_INDEX_FILE", "GIT_NO_REPLACE_OBJECTS", "GIT_REPLACE_REF_BASE",
		"GIT_PREFIX", "GIT_INTERNAL_SUPER_PREFIX", "GIT_SHALLOW_FILE", "GIT_COMMON_DIR":
		return true
	}
	return false
}
