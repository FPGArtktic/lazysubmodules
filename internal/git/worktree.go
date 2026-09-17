// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package git

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
)

const (
	// gitlinkMode is the index and tree mode of a submodule entry.
	gitlinkMode = "160000"
	// exitFatal is the exit status of git for fatal errors, such as running
	// outside a repository.
	exitFatal = 128
	// literalPathspecs makes git treat every pathspec literally, so that
	// paths containing glob characters select only themselves.
	literalPathspecs = "--literal-pathspecs"
)

// IsDirty reports whether the working tree at dir has uncommitted changes.
//
// Untracked files are ignored, also inside nested submodules. Modified
// tracked files inside a nested submodule count, and so does a staged change
// of its gitlink. A nested submodule that is merely checked out at another
// commit than the one recorded does not count: checking out a commit in dir
// leaves nested submodules alone, so this state follows every such checkout
// and loses nothing.
//
// Context: dir must be a working tree.
// Return: true when tracked content differs from HEAD, or *Error.
func (r *Runner) IsDirty(ctx context.Context, dir string) (bool, error) {
	out, err := r.runOffline(ctx, dir, "status", "--porcelain=v2", "--untracked-files=no",
		"--ignore-submodules=none")
	if err != nil {
		return false, err
	}
	for _, line := range splitLines(out) {
		if !movedGitlink(line) {
			return true, nil
		}
	}
	return false, nil
}

// movedGitlink reports whether a "git status --porcelain=v2" line describes
// a submodule that differs from the index only in its checked-out commit or
// in untracked content. Such a line reads "1 .M S<c>.<u> ...": the change is
// not staged, and the submodule state has no "M" for modified tracked
// content. Paths are quoted by git, so a line never contains a newline.
func movedGitlink(line string) bool {
	fields := strings.SplitN(line, " ", 4)
	if len(fields) < 4 || fields[0] != "1" || fields[1] != ".M" {
		return false
	}
	sub := fields[2]
	return len(sub) == 4 && sub[0] == 'S' && sub[2] == '.'
}

// IsWorktreeRoot reports whether dir is the top level of a usable working
// tree, such as a populated submodule.
//
// Context: any; dir may be missing.
// Return: false when dir, its ".git" entry or a valid repository behind it is
// missing, true when git reports dir as the top level, or an error, for
// example *Error when git refuses the repository because of its ownership
// (safe.directory).
func (r *Runner) IsWorktreeRoot(ctx context.Context, dir string) (bool, error) {
	_, err := os.Lstat(filepath.Join(dir, ".git"))
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("worktree %s: %w", dir, err)
	}
	top, err := r.TopLevel(ctx, dir)
	if fatalWith(err, "not a git repository", "must be run in a work tree") {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return SamePath(top, dir)
}

// IsGitDir reports whether dir is itself a git directory in which git
// commands can run, such as a bare repository or the repository of a
// submodule inside the git directory of its superproject.
//
// Git looks for the repository from dir as usual. A directory inside a git
// directory that is not a repository itself therefore does not count, and
// neither does a repository whose configured working tree (core.worktree)
// is missing, since git fails to enter it.
//
// Context: any; dir may be missing.
// Return: false when dir is missing or not a directory, when git finds no
// repository or another one from dir, or when git cannot enter the working
// tree; true when git finds dir itself; or an error, for example *Error
// when git refuses the repository because of its ownership (safe.directory).
func (r *Runner) IsGitDir(ctx context.Context, dir string) (bool, error) {
	fi, err := os.Stat(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("git dir %s: %w", dir, err)
	case !fi.IsDir():
		return false, nil
	}
	out, err := r.runOffline(ctx, dir, "rev-parse", "--absolute-git-dir")
	if fatalWith(err, "not a git repository", "cannot chdir to ") {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return SamePath(trimNewline(out), dir)
}

// fatalWith reports whether err is a fatal error of git whose diagnostics
// contain one of msgs. The messages are stable because git runs with
// LC_ALL=C.
func fatalWith(err error, msgs ...string) bool {
	gitErr, ok := errors.AsType[*Error](err)
	return ok && gitErr.ExitCode == exitFatal && slices.ContainsFunc(msgs, func(msg string) bool {
		return strings.Contains(gitErr.Stderr, msg)
	})
}

// SamePath reports whether a and b name the same file or directory after
// resolving symbolic links.
//
// Context: any; relative paths are relative to the current directory.
// Return: true when both resolve to the same absolute path, or an error when
// a path cannot be resolved, for example because it does not exist.
func SamePath(a, b string) (bool, error) {
	ra, err := realPath(a)
	if err != nil {
		return false, err
	}
	rb, err := realPath(b)
	if err != nil {
		return false, err
	}
	return ra == rb, nil
}

// realPath returns the absolute path of p without symbolic links.
func realPath(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err == nil {
		abs, err = filepath.EvalSymlinks(abs)
	}
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", p, err)
	}
	return abs, nil
}

// Checkout detaches HEAD at a commit and updates the working tree.
//
// In a partial clone, the objects of the commit may be missing; Online
// fetches them from the promisor remote, Offline fails instead. Git 2.39
// does not accept "--end-of-options" here, so a commit that could be taken
// for an option (such as "-f", which would discard local changes) is
// refused before git runs.
//
// Context: dir must be a working tree; commit must exist locally.
// Return: nil, an error wrapping ErrInvalidRefName for an empty commit or one
// starting with "-", or *Error (for example when local changes would be
// lost, or when objects are missing Offline).
func (r *Runner) Checkout(ctx context.Context, dir, commit string, network Network) error {
	if commit == "" || strings.HasPrefix(commit, "-") {
		return fmt.Errorf("checkout: %w: %q", ErrInvalidRefName, commit)
	}
	_, err := r.Exec(ctx, Cmd{
		Dir: dir,
		Args: []string{
			"-c", "advice.detachedHead=false",
			"checkout", "--quiet", "--detach", commit, "--",
		},
		Env: network.env(),
	})
	return err
}

// Add stages paths.
//
// Context: paths are relative to dir and taken literally; force also stages
// paths that match an ignore rule, such as a new lock file in a repository
// that ignores "*.lock".
// Return: nil (also for no paths), or *Error. Without force, an ignored path
// is an error, and the other paths may have been staged.
func (r *Runner) Add(ctx context.Context, dir string, force bool, paths ...string) error {
	if len(paths) == 0 {
		return nil
	}
	args := []string{literalPathspecs, "add"}
	if force {
		args = append(args, "--force")
	}
	args = append(append(args, "--"), paths...)
	_, err := r.runOffline(ctx, dir, args...)
	return err
}

// Remove removes paths from the index and the working tree.
//
// Removing a submodule also removes its section from ".gitmodules".
//
// Context: paths are relative to dir and taken literally; force skips the
// up-to-date check.
// Return: nil (also for no paths), or *Error.
func (r *Runner) Remove(ctx context.Context, dir string, force bool, paths ...string) error {
	if len(paths) == 0 {
		return nil
	}
	args := []string{literalPathspecs, "rm", "--quiet"}
	if force {
		args = append(args, "-f")
	}
	args = append(append(args, "--"), paths...)
	_, err := r.runOffline(ctx, dir, args...)
	return err
}

// StagedPaths lists the paths whose index state differs from HEAD.
//
// On an unborn branch every path in the index is listed. Submodule changes
// are listed regardless of any "ignore" configuration.
//
// Context: dir must be a working tree.
// Return: paths relative to the top level, or *Error.
func (r *Runner) StagedPaths(ctx context.Context, dir string) ([]string, error) {
	out, err := r.runOffline(ctx, dir, "diff", "--cached", "--name-only", "--no-renames",
		"--no-relative", "--ignore-submodules=none", "-z")
	if err != nil {
		return nil, err
	}
	return splitNUL(out), nil
}

// HasStaged reports whether the index differs from HEAD.
//
// Context: dir must be a working tree.
// Return: true when StagedPaths is not empty, or *Error.
func (r *Runner) HasStaged(ctx context.Context, dir string) (bool, error) {
	paths, err := r.StagedPaths(ctx, dir)
	return len(paths) > 0, err
}

// Commit records the index as a new commit with a Signed-off-by trailer.
//
// The message is passed on standard input. Hooks and signing run as
// configured. Lazy fetching is disabled, but the transports stay available
// to the hooks.
//
// Context: dir must be a working tree with a configured identity.
// Return: nil, or *Error.
func (r *Runner) Commit(ctx context.Context, dir, message string) error {
	_, err := r.Exec(ctx, Cmd{
		Dir:   dir,
		Args:  []string{"commit", "--quiet", "-s", "-F", "-"},
		Stdin: strings.NewReader(message),
	})
	return err
}

// IndexGitlink returns the commit recorded in the index for a submodule path.
//
// Context: dir must be the top level of a working tree; p is relative to it.
// Return: the commit SHA, an error wrapping ErrRefNotFound when the index has
// no gitlink at p, or *Error.
func (r *Runner) IndexGitlink(ctx context.Context, dir, p string) (string, error) {
	out, err := r.runOffline(ctx, dir, literalPathspecs, "ls-files", "--stage", "-z", "--", p)
	if err != nil {
		return "", err
	}
	want := cleanPath(p)
	for _, rec := range splitNUL(out) {
		// "<mode> <object> <stage>\t<path>"
		meta, name, _ := strings.Cut(rec, "\t")
		fields := strings.Fields(meta)
		if name == want && len(fields) == 3 && fields[0] == gitlinkMode {
			return fields[1], nil
		}
	}
	return "", fmt.Errorf("gitlink %s in index: %w", p, ErrRefNotFound)
}

// TreeGitlink returns the commit recorded for a submodule path in the tree of
// a revision.
//
// Context: dir must be the top level of a working tree; p is relative to it.
// Return: the commit SHA, an error wrapping ErrRefNotFound when rev does not
// exist (for example an unborn HEAD) or its tree has no gitlink at p, an
// error wrapping ErrInvalidRefName when rev starts with "-", or *Error, for
// example when rev names no tree or the tree is missing from a partial
// clone.
func (r *Runner) TreeGitlink(ctx context.Context, dir, rev, p string) (string, error) {
	if strings.HasPrefix(rev, "-") {
		return "", fmt.Errorf("tree: %w: %q", ErrInvalidRefName, rev)
	}
	// Only the object named by rev is verified: peeling it to a tree would
	// report a tree missing from a partial clone as not found, while ls-tree
	// reports it as an error.
	object, err := r.verifyRev(ctx, dir, rev, rev)
	if err != nil {
		return "", err
	}
	out, err := r.runOffline(ctx, dir, literalPathspecs, "ls-tree", "-z", object, "--", p)
	if err != nil {
		return "", err
	}
	want := cleanPath(p)
	for _, rec := range splitNUL(out) {
		// "<mode> <type> <object>\t<path>"
		meta, name, _ := strings.Cut(rec, "\t")
		fields := strings.Fields(meta)
		if name == want && len(fields) == 3 && fields[0] == gitlinkMode {
			return fields[2], nil
		}
	}
	return "", fmt.Errorf("gitlink %s in %s: %w", p, rev, ErrRefNotFound)
}

// cleanPath normalizes a slash-separated path as git prints it.
func cleanPath(p string) string {
	return path.Clean(filepath.ToSlash(p))
}

// Log lists commits in one-line format, newest first.
//
// Context: dir must be inside a repository; revs are revisions or ranges.
// Return: lines "<abbreviated sha> <subject>" (at most limit when limit > 0),
// or *Error.
func (r *Runner) Log(ctx context.Context, dir string, limit int, revs ...string) ([]string, error) {
	args := []string{"log", "--oneline", "--no-decorate", "--no-color", "--no-show-signature"}
	if limit > 0 {
		args = append(args, "--max-count="+strconv.Itoa(limit))
	}
	args = append(args, "--end-of-options")
	args = append(append(args, revs...), "--")
	out, err := r.runOffline(ctx, dir, args...)
	if err != nil {
		return nil, err
	}
	return splitLines(out), nil
}

// SubmoduleDiff shows how the gitlink of a submodule path changed.
//
// The working tree state is compared with HEAD, listing the commits added or
// removed. On an unborn branch the index is compared with the empty tree.
//
// Context: dir must be the top level of a working tree; p is relative to it.
// Return: the diff text (empty without changes), or *Error.
func (r *Runner) SubmoduleDiff(ctx context.Context, dir, p string) (string, error) {
	args := []string{
		literalPathspecs, "diff", "--no-color", "--no-ext-diff",
		"--ignore-submodules=none", "--submodule=log",
	}
	_, err := r.Head(ctx, dir)
	switch {
	case errors.Is(err, ErrRefNotFound):
		args = append(args, "--cached")
	case err != nil:
		return "", err
	default:
		args = append(args, "HEAD")
	}
	return r.runOffline(ctx, dir, append(args, "--", p)...)
}
