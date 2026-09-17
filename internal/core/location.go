// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package core

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/FPGArtktic/lazysubmodules/internal/git"
	"github.com/FPGArtktic/lazysubmodules/internal/manifest"
)

// exitFatal is the exit status of git for fatal errors.
const exitFatal = 128

// location describes where the repository of a submodule can be found.
type location struct {
	// worktree is the absolute path of the submodule working tree.
	worktree string
	// gitlink is the commit that the index of the superproject records at
	// the submodule path; empty when the index records no submodule there.
	// Git ignores a .gitmodules entry without a gitlink, and so does this
	// package: such a path is never populated, and nothing is read from or
	// written to it.
	gitlink string
	// populated reports whether worktree is the top level of a checked-out
	// repository.
	populated bool
	// symlink reports that a component of the submodule path is a symbolic
	// link. Git never checks out a submodule through one, and following it
	// would let a crafted superproject point at an unrelated repository, so
	// such a path is never populated and must not be initialized.
	symlink bool
	// gitDir is the absolute path of the submodule repository in the git
	// directory of the superproject. It is only looked up for a submodule
	// that the index records and that is not populated.
	gitDir string
	// hasGitDir reports whether gitDir is a repository that git commands
	// can run in. It is false when the directory is missing or empty, and
	// also when the repository still names a working tree that was removed.
	hasGitDir bool
}

// tracked reports whether the index of the superproject records a
// submodule at the path.
func (l location) tracked() bool {
	return l.gitlink != ""
}

// refusal returns the refusal of a command that would modify the
// submodule at the location: its path leads through a symbolic link, or the
// index records no submodule there. It returns nil otherwise.
func (l location) refusal(name string) error {
	switch {
	case l.symlink:
		return wrapName(name, ErrSymlinkPath)
	case !l.tracked():
		return wrapName(name, ErrNotSubmodule)
	}
	return nil
}

// refDir returns the directory in which the refs of the submodule can be
// read: the working tree when it is populated, else the submodule
// repository in the git directory of the superproject.
func (l location) refDir() (string, bool) {
	switch {
	case l.populated:
		return l.worktree, true
	case l.hasGitDir:
		return l.gitDir, true
	}
	return "", false
}

// locate finds the working tree and, when it is not populated, the
// repository of a submodule. A path through a symbolic link, or one that
// the index does not record as a submodule, is not inspected further.
func (r *Repo) locate(ctx context.Context, sub manifest.Submodule) (location, error) {
	loc := location{worktree: filepath.Join(r.root, filepath.FromSlash(sub.Path))}
	link, err := hasSymlink(r.root, sub.Path)
	if err != nil || link {
		loc.symlink = link
		return loc, err
	}
	gitlink, err := r.git.IndexGitlink(ctx, r.root, sub.Path)
	switch {
	case errors.Is(err, git.ErrRefNotFound):
		return loc, nil
	case err != nil:
		return loc, err
	}
	loc.gitlink = gitlink
	loc.populated, err = r.git.IsWorktreeRoot(ctx, loc.worktree)
	if err != nil || loc.populated {
		return loc, err
	}
	loc.gitDir, err = r.git.SubmoduleGitDir(ctx, r.root, sub.Name)
	if err != nil {
		return loc, err
	}
	loc.hasGitDir, err = r.isGitDir(ctx, loc.gitDir)
	return loc, err
}

// isGitDir reports whether dir is itself a git directory that git commands
// can run in. A directory inside the git directory of the superproject that
// is not a repository would make git fall back to the superproject, so the
// git directory that git finds must be dir itself.
func (r *Repo) isGitDir(ctx context.Context, dir string) (bool, error) {
	ok, err := isDir(dir)
	if err != nil || !ok {
		return false, err
	}
	head, err := r.git.GitPath(ctx, dir, "HEAD")
	if gitErr, ok := errors.AsType[*git.Error](err); ok && gitErr.ExitCode == exitFatal {
		// For example "cannot chdir" to a working tree that was removed.
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return sameDir(filepath.Dir(head), dir)
}

// isDir reports whether dir is a directory, following symbolic links. A
// missing directory is not an error.
func isDir(dir string) (bool, error) {
	fi, err := os.Stat(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("submodule repository: %w", err)
	}
	return fi.IsDir(), nil
}

// sameDir reports whether two existing directories are the same after
// resolving symbolic links.
func sameDir(a, b string) (bool, error) {
	ra, err := filepath.EvalSymlinks(a)
	if err != nil {
		return false, fmt.Errorf("resolve %s: %w", a, err)
	}
	rb, err := filepath.EvalSymlinks(b)
	if err != nil {
		return false, fmt.Errorf("resolve %s: %w", b, err)
	}
	return ra == rb, nil
}

// hasSymlink reports whether a component of the "/"-separated path p below
// root is a symbolic link. Missing components end the check.
func hasSymlink(root, p string) (bool, error) {
	cur := root
	for comp := range strings.SplitSeq(p, "/") {
		cur = filepath.Join(cur, comp)
		fi, err := os.Lstat(cur)
		switch {
		case errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR):
			return false, nil
		case err != nil:
			return false, fmt.Errorf("submodule path: %w", err)
		case fi.Mode()&fs.ModeSymlink != 0:
			return true, nil
		}
	}
	return false, nil
}

// uninitializedReason explains why a submodule is not populated.
func uninitializedReason(loc location) string {
	switch {
	case loc.symlink:
		return "submodule path contains a symbolic link"
	case !loc.tracked():
		return "the index records no submodule at the path"
	}
	return "submodule is not checked out"
}
