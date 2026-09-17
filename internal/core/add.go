// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package core

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"github.com/FPGArtktic/lazysubmodules/internal/git"
	"github.com/FPGArtktic/lazysubmodules/internal/lock"
	"github.com/FPGArtktic/lazysubmodules/internal/manifest"
)

// configSection is the section of the submodule variables in the git
// configuration of the superproject.
const configSection = "submodule"

// AddOptions describes a new submodule for Add.
type AddOptions struct {
	// URL is the repository to clone.
	URL string
	// Path is where the submodule is checked out, relative to the top
	// level of the superproject; it also becomes the submodule name.
	Path string
	// Mode is the tracking mode.
	Mode manifest.Mode
	// Ref is the branch, tag, tag pattern or commit to track.
	Ref string
	// IncludePrerelease lets a tag pattern select a pre-release tag.
	IncludePrerelease bool
	// Progress receives the output of git while it clones; when nil, the
	// output is captured.
	Progress io.Writer
}

// Add clones a repository as a new managed submodule.
//
// The repository is added with "git submodule add", which names the
// submodule after its path; in branch mode, the branch is passed with
// "-b". Then the tracking configuration is written, the ref is resolved as
// Update resolves it, the commit is checked out with a detached HEAD, the
// lock entry is written, and .gitmodules, the lock file and the gitlink are
// staged. In commit mode, the full commit name is stored. Nothing is
// committed.
//
// The path must be a clean relative path inside the superproject that does
// not exist, has no ".git" component, does not lead through a symbolic link
// or into a submodule, and is not the path or name of another submodule.
// When a step fails once git has started to add the submodule, everything
// the add created is removed: the gitlink and the .gitmodules entry, the
// working tree and the directories created for it, and the submodule
// repository unless it existed before. The lock entry and the submodule
// variables in the git configuration of the superproject get their
// previous values back. A .gitmodules or lock file that the add created is
// removed when it is empty again. The object database may keep unreachable
// objects, and git itself stages all of .gitmodules when it adds a
// submodule, so unstaged changes of that file end up staged.
//
// Context: uses the network to clone; credentials come from the git
// configuration.
// Return: the change, whose Submodule is the new configuration, whose Old
// is the lock entry that the index recorded for the path before (normally
// none), and whose Init and Clone are set; an error wrapping
// ErrInvalidArgument for an invalid URL, path, mode or ref; an error
// wrapping ErrPathExists for a path that exists, belongs to or lies inside
// another submodule or leads through a file; an error wrapping
// ErrSymlinkPath for a path that leads through a symbolic link; an error
// wrapping ErrMissingRef when the ref does not resolve in the clone; an
// error from reading the working tree copy of .gitmodules or the lock
// file, before anything is cloned, such as one wrapping
// git.ErrNotRegularFile for a copy that is not a regular file,
// manifest.ErrInvalidMode or lock.ErrInvalidEntry; *git.Error; or an error
// from inspecting, creating or removing files. After a rollback, the error
// of the failed step is joined with the errors of the rollback, if any.
func (r *Repo) Add(ctx context.Context, opts AddOptions) (Change, error) {
	p, err := r.checkAdd(ctx, opts)
	if err != nil {
		return Change{}, err
	}
	undo, err := r.newAddUndo(ctx, p)
	if err != nil {
		return Change{}, err
	}
	change, err := r.add(ctx, opts, undo)
	if err != nil {
		return Change{}, errors.Join(withName(p, err), undo.run(context.WithoutCancel(ctx)))
	}
	return change, nil
}

// checkAdd validates the options of Add and returns the cleaned path.
func (r *Repo) checkAdd(ctx context.Context, opts AddOptions) (string, error) {
	if err := checkURL(opts.URL); err != nil {
		return "", err
	}
	p, err := cleanPath(opts.Path)
	if err != nil {
		return "", err
	}
	if err := r.checkRef(ctx, opts.Mode, opts.Ref); err != nil {
		return "", err
	}
	subs, err := r.Submodules(ctx)
	if err != nil {
		return "", err
	}
	for _, sub := range subs {
		switch {
		case sub.Name == p || sub.Path == p || strings.HasPrefix(sub.Path, p+"/"):
			return "", fmt.Errorf("%s: %w: used by submodule %s",
				displayName(p), ErrPathExists, displayName(sub.Name))
		case strings.HasPrefix(p, sub.Path+"/"):
			return "", fmt.Errorf("%s: %w: inside submodule %s",
				displayName(p), ErrPathExists, displayName(sub.Name))
		}
	}
	return p, r.checkNewPath(ctx, p)
}

// checkNewPath checks that nothing exists at the path of a new submodule,
// neither in the working tree nor as a gitlink in the index, and that the
// existing directories leading to it are not symbolic links. Each of these
// is an unsafe state of the working tree, a refusal, not an invalid path.
func (r *Repo) checkNewPath(ctx context.Context, p string) error {
	_, err := os.Lstat(filepath.Join(r.root, filepath.FromSlash(p)))
	switch {
	case err == nil:
		return fmt.Errorf("%s: %w", displayName(p), ErrPathExists)
	case errors.Is(err, syscall.ENOTDIR):
		return fmt.Errorf("%s: %w: it leads through a file", displayName(p), ErrPathExists)
	case !errors.Is(err, fs.ErrNotExist):
		return fmt.Errorf("check path: %w", err)
	}
	link, err := hasSymlink(r.root, p)
	if err != nil {
		return err
	}
	if link {
		return fmt.Errorf("%s: %w", displayName(p), ErrSymlinkPath)
	}
	_, err = r.git.IndexGitlink(ctx, r.root, p)
	switch {
	case err == nil:
		return fmt.Errorf("%s: %w: the index has a gitlink there", displayName(p), ErrPathExists)
	case errors.Is(err, git.ErrRefNotFound):
		return nil
	}
	return err
}

// add performs the steps of Add after validation.
func (r *Repo) add(ctx context.Context, opts AddOptions, undo *addUndo) (Change, error) {
	p := undo.path
	// The old lock entry is the one of the index, as for an update.
	index, err := r.loadSnapshot(ctx, "")
	if err != nil {
		return Change{}, err
	}
	branch := ""
	if opts.Mode == manifest.ModeBranch {
		branch = opts.Ref
	}
	if err := r.git.SubmoduleAdd(ctx, r.root, opts.URL, p, branch, opts.Progress); err != nil {
		return Change{}, err
	}
	sub, err := r.addedSubmodule(ctx, p)
	if err != nil {
		return Change{}, err
	}
	sub.Mode, sub.Ref, sub.Branch = opts.Mode, opts.Ref, branch
	dir := filepath.Join(r.root, filepath.FromSlash(p))
	res, err := r.resolveIn(ctx, dir, sub, nil,
		ResolveOptions{IncludePrerelease: opts.IncludePrerelease})
	if err != nil {
		return Change{}, err
	}
	if opts.Mode == manifest.ModeCommit {
		sub.Ref = res.Ref
	}
	if err := manifest.SetTracking(ctx, r.git, r.root, sub.Name, sub.Mode, sub.Ref); err != nil {
		return Change{}, err
	}
	if err := r.git.Checkout(ctx, dir, res.Commit, git.Online); err != nil {
		return Change{}, err
	}
	change := Change{Submodule: sub, Old: index.tracking(p).entry, New: res, Init: true,
		Clone: true}
	undo.lockWritten = true
	if err := lock.Write(ctx, r.git, r.root, change.entry()); err != nil {
		return Change{}, err
	}
	// Force: a superproject may ignore "*.lock".
	if err := r.git.Add(ctx, r.root, true, manifest.File, lock.File, p); err != nil {
		return Change{}, err
	}
	return change, nil
}

// addedSubmodule returns the submodule that git registered at path p.
func (r *Repo) addedSubmodule(ctx context.Context, p string) (manifest.Submodule, error) {
	subs, err := r.Submodules(ctx)
	if err != nil {
		return manifest.Submodule{}, err
	}
	i := slices.IndexFunc(subs, func(s manifest.Submodule) bool { return s.Path == p })
	if i < 0 || subs[i].Name != p {
		return manifest.Submodule{}, fmt.Errorf("git did not register submodule %s in %s",
			displayName(p), manifest.File)
	}
	return subs[i], nil
}

// addUndo records the state before Add and removes what the add created.
type addUndo struct {
	repo *Repo
	// path is the path and name of the new submodule.
	path string
	// created is the topmost directory that the add creates.
	created string
	// gitDir is the submodule repository before the add, and gitDirExisted
	// whether it existed. gitDirCreated is the topmost directory that a
	// clone to gitDir creates.
	gitDir        string
	gitDirExisted bool
	gitDirCreated string
	// configFile is the configuration file of the superproject, in its git
	// directory, and config holds the variables of the submodule in it.
	configFile string
	config     map[string]string
	// hadManifest and hadLock report whether .gitmodules and the lock file
	// existed.
	hadManifest bool
	hadLock     bool
	// oldEntry is a stale lock entry of the same name, or nil, and
	// lockWritten whether the add may have written the lock entry.
	oldEntry    *lock.Entry
	lockWritten bool
}

// newAddUndo records the state before a submodule is added at p. It also
// fails for a lock file that cannot be read, before anything is cloned.
func (r *Repo) newAddUndo(ctx context.Context, p string) (*addUndo, error) {
	u := &addUndo{repo: r, path: p}
	lk, err := lock.Load(ctx, r.git, r.root)
	if err != nil {
		return nil, err
	}
	u.oldEntry = lockEntry(lk, p)
	u.created = firstMissing(r.root, p)
	if err := u.recordGitDir(ctx); err != nil {
		return nil, err
	}
	if u.hadManifest, err = exists(filepath.Join(r.root, manifest.File)); err != nil {
		return nil, err
	}
	u.hadLock, err = exists(filepath.Join(r.root, lock.File))
	return u, err
}

// recordGitDir records the state of the git directory of the superproject:
// the submodule repository and the submodule variables.
func (u *addUndo) recordGitDir(ctx context.Context) error {
	r := u.repo
	var err error
	if u.configFile, err = r.git.GitPath(ctx, r.root, "config"); err != nil {
		return err
	}
	if u.config, err = u.submoduleConfig(ctx); err != nil {
		return err
	}
	if u.gitDir, err = r.git.SubmoduleGitDir(ctx, r.root, u.path); err != nil {
		return err
	}
	if u.gitDirExisted, err = exists(u.gitDir); err != nil {
		return err
	}
	u.gitDirCreated = u.gitDir
	rel, err := filepath.Rel(u.commonDir(), u.gitDir)
	if err == nil && filepath.IsLocal(rel) {
		u.gitDirCreated = firstMissing(u.commonDir(), filepath.ToSlash(rel))
	}
	return nil
}

// commonDir returns the git directory that holds the configuration of the
// superproject.
func (u *addUndo) commonDir() string {
	return filepath.Dir(u.configFile)
}

// run removes what the add created. It continues after failures and joins
// their errors.
func (u *addUndo) run(ctx context.Context) error {
	err := errors.Join(
		u.restoreLock(ctx),
		u.removeGitlink(ctx),
		u.removeCreated(),
		// Before removeConfig, which may hold the location of the repository.
		u.removeGitDir(ctx),
		u.removeConfig(ctx),
		u.removeManifest(ctx),
	)
	if err != nil {
		return fmt.Errorf("roll back %s: %w", displayName(u.path), err)
	}
	return nil
}

// restoreLock puts back the lock entry that existed before, or removes the
// new one.
func (u *addUndo) restoreLock(ctx context.Context) error {
	r := u.repo
	switch {
	case !u.lockWritten:
		return nil
	case u.oldEntry != nil:
		return lock.Write(ctx, r.git, r.root, *u.oldEntry)
	}
	if err := lock.Remove(ctx, r.git, r.root, u.path); err != nil {
		return err
	}
	if u.hadLock {
		return nil
	}
	return removeEmpty(filepath.Join(r.root, lock.File))
}

// removeGitlink removes the gitlink from the index, and with it the
// working tree and the .gitmodules entry of the submodule.
func (u *addUndo) removeGitlink(ctx context.Context) error {
	r := u.repo
	_, err := r.git.IndexGitlink(ctx, r.root, u.path)
	if errors.Is(err, git.ErrRefNotFound) {
		// Git registers the submodule in .gitmodules only after staging
		// the gitlink, so there is nothing else to remove.
		return nil
	}
	if err != nil {
		return err
	}
	// "git rm" refuses to remove a submodule while .gitmodules has unstaged
	// changes, such as the tracking configuration.
	if err := r.git.Add(ctx, r.root, true, manifest.File); err != nil {
		return err
	}
	return r.git.Remove(ctx, r.root, true, u.path)
}

// removeCreated removes the working tree and the directories created for
// it. They did not exist before the add.
func (u *addUndo) removeCreated() error {
	return removeAll(u.created)
}

// removeAll removes p with everything below it.
func removeAll(p string) error {
	if err := os.RemoveAll(p); err != nil {
		return fmt.Errorf("remove %s: %w", p, err)
	}
	return nil
}

// removeGitDir removes the submodule repository, and the directories
// created for it, unless it existed before. Git may have chosen another
// location than the one recorded before the add (with the repository
// extension submodulePathConfig); that repository is new, and so are its
// parent directories once they are empty.
func (u *addUndo) removeGitDir(ctx context.Context) error {
	r := u.repo
	dir, err := r.git.SubmoduleGitDir(ctx, r.root, u.path)
	switch {
	case err != nil:
		return err
	case dir == u.gitDir && u.gitDirExisted:
		return nil
	case dir == u.gitDir:
		return removeAll(u.gitDirCreated)
	}
	if err := removeAll(dir); err != nil {
		return err
	}
	removeEmptyParents(dir, u.commonDir())
	return nil
}

// removeEmptyParents removes the parent directories of p below the
// directory top while they are empty.
func removeEmptyParents(p, top string) {
	below := top + string(filepath.Separator)
	for dir := filepath.Dir(p); strings.HasPrefix(dir, below); dir = filepath.Dir(dir) {
		if os.Remove(dir) != nil {
			// Not empty, or not removable: it stays, and so do its parents.
			return
		}
	}
}

// submoduleConfig returns the variables of the submodule in the
// configuration of the superproject; the last value of each wins.
func (u *addUndo) submoduleConfig(ctx context.Context) (map[string]string, error) {
	r := u.repo
	vars, err := r.git.ConfigList(ctx, r.root, u.configFile)
	if err != nil {
		return nil, err
	}
	config := make(map[string]string)
	for _, v := range vars {
		if configName(v.Key) == u.path {
			config[v.Key] = v.Value
		}
	}
	return config, nil
}

// removeConfig restores the variables of the submodule that "git submodule
// add" set in the configuration of the superproject. Git removes a section
// header once the last variable below it is unset.
func (u *addUndo) removeConfig(ctx context.Context) error {
	r := u.repo
	current, err := u.submoduleConfig(ctx)
	if err != nil {
		return err
	}
	for key, value := range current {
		old, ok := u.config[key]
		switch {
		case !ok:
			err = r.git.ConfigUnset(ctx, r.root, u.configFile, key)
		case old != value:
			err = r.git.ConfigSet(ctx, r.root, u.configFile, key, old)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// removeManifest removes a .gitmodules file that the add created, once
// removing the submodule left it empty.
func (u *addUndo) removeManifest(ctx context.Context) error {
	r := u.repo
	file := filepath.Join(r.root, manifest.File)
	if u.hadManifest {
		return nil
	}
	if empty, err := isEmptyFile(file); err != nil || !empty {
		return err
	}
	staged, err := r.git.StagedPaths(ctx, r.root)
	if err != nil {
		return err
	}
	if slices.Contains(staged, manifest.File) {
		return r.git.Remove(ctx, r.root, true, manifest.File)
	}
	return removeEmpty(file)
}

// configName returns the submodule name of a "submodule.<name>.<variable>"
// configuration key, or "" for other keys. Git prints the section name in
// lowercase.
func configName(key string) string {
	rest, ok := strings.CutPrefix(key, configSection+".")
	i := strings.LastIndexByte(rest, '.')
	if !ok || i < 0 {
		return ""
	}
	return rest[:i]
}

// firstMissing returns the topmost missing directory on the way from root
// to the "/"-separated path p, which is missing itself. The existing
// directories on the way must have been checked.
func firstMissing(root, p string) string {
	cur := root
	for comp := range strings.SplitSeq(p, "/") {
		cur = filepath.Join(cur, comp)
		if _, err := os.Lstat(cur); err != nil {
			return cur
		}
	}
	return cur
}

// exists reports whether a file or directory exists at p.
func exists(p string) (bool, error) {
	_, err := os.Lstat(p)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR):
		return false, nil
	}
	return false, fmt.Errorf("check %s: %w", p, err)
}

// isEmptyFile reports whether p is an empty regular file. A missing file is
// not empty.
func isEmptyFile(p string) (bool, error) {
	fi, err := os.Lstat(p)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("check %s: %w", p, err)
	}
	return fi.Mode().IsRegular() && fi.Size() == 0, nil
}

// removeEmpty removes p when it is an empty regular file.
func removeEmpty(p string) error {
	empty, err := isEmptyFile(p)
	if err != nil || !empty {
		return err
	}
	if err := os.Remove(p); err != nil {
		return fmt.Errorf("remove %s: %w", p, err)
	}
	return nil
}
