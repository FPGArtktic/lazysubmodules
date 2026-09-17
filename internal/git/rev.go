// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package git

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

// refCheckDir is the working directory of ref name checks. It keeps
// "check-ref-format --branch" away from any repository, so that the result
// does not depend on the current directory.
const refCheckDir = "/"

// TopLevel returns the top-level directory of the working tree containing dir.
//
// Context: dir must be inside a working tree.
// Return: the absolute path printed by "git rev-parse --show-toplevel", or
// *Error.
func (r *Runner) TopLevel(ctx context.Context, dir string) (string, error) {
	out, err := r.runOffline(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	return trimNewline(out), nil
}

// GitPath resolves a path inside the git directory of the repository at dir.
//
// For example, "HEAD" resolves to the file holding the checked-out branch.
// Use SubmoduleGitDir, not "modules/<name>", to locate the repository of a
// submodule.
//
// Context: dir must be inside a repository.
// Return: the absolute path (which may not exist), or an error.
func (r *Runner) GitPath(ctx context.Context, dir, p string) (string, error) {
	out, err := r.runOffline(ctx, dir, "rev-parse", "--git-path", p)
	if err != nil {
		return "", err
	}
	return absPath(dir, trimNewline(out))
}

// SubmoduleGitDir returns the git directory that holds the repository of a
// submodule, whether the submodule is populated or not.
//
// By default this is "modules/<name>" inside the git directory of the
// superproject. With the repository extension submodulePathConfig (git 2.52
// and later), git takes the directory from submodule.<name>.gitdir instead,
// relative to the top level, and encodes names such as "lib/x" there. Git
// sets that variable when it initializes a submodule; the default applies
// while it is not set.
//
// Context: root is the top level of the superproject.
// Return: the absolute path (which may not exist), or an error.
func (r *Runner) SubmoduleGitDir(ctx context.Context, root, name string) (string, error) {
	enabled, err := r.localConfigBool(ctx, root, "extensions.submodulePathConfig")
	if err != nil {
		return "", err
	}
	if enabled {
		dir, found, err := r.configGet(ctx, root, "submodule."+name+".gitdir")
		if err != nil {
			return "", err
		}
		if found {
			return absPath(root, dir)
		}
	}
	return r.GitPath(ctx, root, "modules/"+name)
}

// localConfigBool reads a boolean variable from the configuration file of
// the repository at dir, where git looks for repository extensions. A
// missing variable is false.
func (r *Runner) localConfigBool(ctx context.Context, dir, key string) (bool, error) {
	out, err := r.runOffline(ctx, dir, "config", "--local", "--bool", "--get", key)
	if exitCode(err) == 1 {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return trimNewline(out) == "true", nil
}

// configGet reads the last value of a variable from all configuration files
// of the repository at dir, as git does. Found is false when the variable is
// not set.
func (r *Runner) configGet(ctx context.Context, dir, key string) (string, bool, error) {
	out, err := r.runOffline(ctx, dir, "config", "--get", key)
	if exitCode(err) == 1 {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return trimNewline(out), true, nil
}

// absPath makes p absolute, relative to dir unless it is absolute already.
func absPath(dir, p string) (string, error) {
	if !filepath.IsAbs(p) {
		p = filepath.Join(dir, p)
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", fmt.Errorf("path %s: %w", p, err)
	}
	return abs, nil
}

// ObjectFormat returns the hash algorithm of the repository at dir.
//
// Context: dir must be inside a repository.
// Return: "sha1" or "sha256", or *Error.
func (r *Runner) ObjectFormat(ctx context.Context, dir string) (string, error) {
	out, err := r.runOffline(ctx, dir, "rev-parse", "--show-object-format")
	if err != nil {
		return "", err
	}
	return trimNewline(out), nil
}

// ResolveCommit returns the commit a revision points to.
//
// Annotated tags are dereferenced to the tagged commit. Only local objects
// and refs are consulted.
//
// Context: dir must be inside a repository.
// Return: the full commit SHA, an error wrapping ErrRefNotFound when rev does
// not name a commit, or *Error.
func (r *Runner) ResolveCommit(ctx context.Context, dir, rev string) (string, error) {
	return r.verifyRev(ctx, dir, rev, rev+"^{commit}")
}

// MissingObjects lists the objects of a commit that the repository at dir
// lacks, such as the blobs that a partial clone has not fetched yet.
//
// The tree of the commit and every tree and blob below it are checked; the
// history of the commit is not, and neither are the commits that the tree
// records for submodules. Missing objects are not fetched, not even from
// the promisor remote of a partial clone.
//
// Context: dir must be inside a repository.
// Return: the names of the missing objects, none when the commit can be
// checked out without fetching anything; an error wrapping ErrRefNotFound
// when commit does not name a commit that the repository has; an error
// wrapping ErrInvalidRefName when commit starts with "-"; or *Error.
func (r *Runner) MissingObjects(ctx context.Context, dir, commit string) ([]string, error) {
	if strings.HasPrefix(commit, "-") {
		return nil, fmt.Errorf("objects: %w: %q", ErrInvalidRefName, commit)
	}
	// Depending on its version, rev-list lists a missing commit or fails.
	full, err := r.ResolveCommit(ctx, dir, commit)
	if err != nil {
		return nil, err
	}
	out, err := r.runOffline(ctx, dir, "rev-list", "--objects", "--no-object-names",
		"--no-walk", "--missing=print", "--end-of-options", full)
	if err != nil {
		return nil, err
	}
	var missing []string
	for _, line := range splitLines(out) {
		if object, ok := strings.CutPrefix(line, "?"); ok {
			missing = append(missing, object)
		}
	}
	return missing, nil
}

// Head returns the commit checked out in the repository at dir.
//
// Context: dir must be inside a repository; a detached HEAD is fine.
// Return: the full commit SHA, an error wrapping ErrRefNotFound when HEAD is
// unborn, or *Error.
func (r *Runner) Head(ctx context.Context, dir string) (string, error) {
	return r.verifyRev(ctx, dir, "HEAD", "HEAD")
}

// verifyRev resolves spec with "rev-parse --verify"; name is used in the
// not-found error.
func (r *Runner) verifyRev(ctx context.Context, dir, name, spec string) (string, error) {
	out, err := r.runOffline(ctx, dir, "rev-parse", "--verify", "--quiet", spec)
	if exitCode(err) == 1 {
		return "", fmt.Errorf("%s: %w", name, ErrRefNotFound)
	}
	if err != nil {
		return "", err
	}
	return trimNewline(out), nil
}

// ListTags lists local tags matching a glob, highest version first.
//
// Tags are sorted with "--sort=-v:refname" and versionsort.suffix=-, so that
// a pre-release such as v1.0.0-rc.1 is placed after v1.0.0.
//
// Context: dir must be inside a repository.
// Return: tag names (empty pattern lists all tags), or *Error.
func (r *Runner) ListTags(ctx context.Context, dir, pattern string) ([]string, error) {
	args := []string{
		"-c", "versionsort.suffix=-",
		"tag", "--list", "--no-column", "--sort=-v:refname",
	}
	if pattern != "" {
		args = append(args, "--end-of-options", pattern)
	}
	out, err := r.runOffline(ctx, dir, args...)
	if err != nil {
		return nil, err
	}
	return splitLines(out), nil
}

// ListRemoteBranches lists the remote-tracking branches of a remote.
//
// Context: dir must be inside a repository.
// Return: branch names without the "refs/remotes/<remote>/" prefix and
// without the symbolic HEAD, sorted by name, or *Error.
func (r *Runner) ListRemoteBranches(ctx context.Context, dir, remote string) ([]string, error) {
	prefix := "refs/remotes/" + remote + "/"
	out, err := r.runOffline(ctx, dir, "for-each-ref", "--format=%(refname)", prefix)
	if err != nil {
		return nil, err
	}
	var branches []string
	for _, ref := range splitLines(out) {
		name, ok := strings.CutPrefix(ref, prefix)
		if !ok || name == "HEAD" {
			continue
		}
		branches = append(branches, name)
	}
	return branches, nil
}

// CheckBranchName checks that name is a valid branch name.
//
// Shorthands such as "@{-1}" are rejected, as they do not name a branch
// literally.
//
// Context: any; no repository is needed.
// Return: nil, an error wrapping ErrInvalidRefName, or *Error when git could
// not run.
func (r *Runner) CheckBranchName(ctx context.Context, name string) error {
	out, err := r.runOffline(ctx, refCheckDir, "check-ref-format", "--branch", name)
	if exitCode(err) > 0 || (err == nil && trimNewline(out) != name) {
		return fmt.Errorf("%w: %q", ErrInvalidRefName, name)
	}
	return err
}

// CheckTagName checks that name is a valid tag name.
//
// Context: any; no repository is needed.
// Return: nil, an error wrapping ErrInvalidRefName, or *Error when git could
// not run.
func (r *Runner) CheckTagName(ctx context.Context, name string) error {
	_, err := r.runOffline(ctx, refCheckDir, "check-ref-format", "refs/tags/"+name)
	if exitCode(err) > 0 {
		return fmt.Errorf("%w: %q", ErrInvalidRefName, name)
	}
	return err
}
