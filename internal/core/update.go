// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package core

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/FPGArtktic/lazysubmodules/internal/git"
	"github.com/FPGArtktic/lazysubmodules/internal/lock"
	"github.com/FPGArtktic/lazysubmodules/internal/manifest"
)

// maxListedPaths bounds the number of paths named in a refusal.
const maxListedPaths = 3

// UpdateOptions controls Update.
type UpdateOptions struct {
	// Names selects managed submodules; without names, every managed
	// submodule is updated.
	Names []string
	// Fetch fetches the refs of the selected submodules from origin before
	// they are resolved, and lets Update clone a submodule that is not
	// initialized and has no repository yet. Only this option makes Update
	// use the network.
	Fetch bool
	// DryRun computes the changes without modifying anything: nothing is
	// fetched, initialized, cloned, checked out, written or staged.
	DryRun bool
	// Commit creates one commit of the update with "git commit -s".
	Commit bool
	// IncludePrerelease lets tag patterns select pre-release tags.
	IncludePrerelease bool
	// Progress receives the output of git while it fetches, clones or
	// initializes submodules; when nil, the output is captured.
	Progress io.Writer
}

// Change describes the update of one submodule.
type Change struct {
	// Submodule is the configuration from .gitmodules before the update.
	Submodule manifest.Submodule
	// Old is the lock entry before the update, as the lock file in the
	// working tree records it, or nil.
	Old *lock.Entry
	// OldHead is the commit checked out before the update; empty when the
	// submodule was not initialized or its HEAD was unborn.
	OldHead string
	// OldGitlink is the commit that the superproject recorded for the
	// submodule before the update: in the index, or, with
	// UpdateOptions.Commit, in HEAD, which the commit replaces. It is empty
	// when there is none, such as in a superproject without commits.
	OldGitlink string
	// New is the resolution the submodule is updated to. In a dry run it
	// is the zero Resolution when the target cannot be known without
	// fetching or initializing first: for a submodule that must be cloned
	// or whose repository cannot be read before its working tree directory
	// exists, and, with Fetch, for a ref that is not available locally.
	New Resolution
	// Init reports that the submodule was, or would be, initialized.
	Init bool
	// Clone reports that the initialization needs a clone, which only
	// happens with UpdateOptions.Fetch.
	Clone bool
}

// Changed reports whether the update modifies anything for the submodule.
//
// Context: any.
// Return: true when the submodule is initialized; when its checked-out
// commit, the commit recorded by the superproject (OldGitlink) or its lock
// entry differs from New; or when the native branch key in .gitmodules
// does not match the tracking mode.
func (c Change) Changed() bool {
	if c.Init || c.Old == nil || c.OldHead != c.New.Commit || c.OldGitlink != c.New.Commit {
		return true
	}
	return c.Old.Mode != c.New.Mode || c.Old.Ref != c.New.Ref ||
		c.Old.Commit != c.New.Commit || staleBranchKey(c.Submodule)
}

// entry returns the lock entry that records New.
func (c Change) entry() lock.Entry {
	return lock.Entry{
		Name:   c.Submodule.Name,
		Mode:   c.New.Mode,
		Ref:    c.New.Ref,
		Commit: c.New.Commit,
	}
}

// staleBranchKey reports whether the native branch key of a submodule
// disagrees with its tracking mode: branch mode needs the tracked branch,
// so that "git submodule update --remote" follows it, and the other modes
// need no branch key.
func staleBranchKey(sub manifest.Submodule) bool {
	if sub.Mode == manifest.ModeBranch {
		return sub.Branch != sub.Ref
	}
	return sub.Branch != ""
}

// UpdateResult is the outcome of Update.
type UpdateResult struct {
	// Changes lists every selected submodule in .gitmodules order,
	// including those that are up to date.
	Changes []Change
	// Commit is the commit created for UpdateOptions.Commit, or empty.
	Commit string
}

// Update moves managed submodules to the commits their tracking
// configuration selects.
//
// The target of each selected submodule is resolved as Resolve does it,
// with the current lock entry, so a tag pattern never goes back from its
// locked tag while that tag exists and matches. The submodule is checked
// out at the target with a detached HEAD, its lock entry is written, and
// the native branch key in .gitmodules is made to follow the tracking
// mode. Then .gitmodules, the lock file and the gitlinks of the changed
// submodules are staged, even when .gitignore matches them; a gitlink that
// the index records at another commit than the target is staged again, so
// an update whose staging was undone or failed can be repeated. The two
// files are staged as a whole, including changes that other submodules
// have in them. With opts.Commit, one commit is created with the message of
// CommitMessage; a submodule whose gitlink in HEAD differs from the target
// counts as changed, so an update staged before is committed too. When no
// submodule changed, nothing is staged or committed.
//
// A submodule that is not initialized is initialized first: offline when
// its repository exists in the git directory of the superproject, or else
// by a clone, which needs opts.Fetch. With opts.Fetch, the refs of every
// selected submodule are fetched from origin before they are resolved.
//
// Update refuses when a selected submodule has uncommitted changes, has no
// gitlink in the index, has a path through a symbolic link, is not
// initialized and cannot be initialized offline (no repository, or one
// that lacks the commit recorded in the superproject), or has an invalid
// or missing ref, and, with opts.Commit, when the index holds staged
// changes of other paths than .gitmodules, the lock file and the selected
// submodules. These checks run for every selected submodule before
// anything is modified, and all refusals are reported together. Only the
// removed working tree directory of a submodule whose repository names it
// is created before, empty, as git leaves it when it deinitializes a
// submodule, so that the repository can be read offline; this happens
// neither in a dry run nor with opts.Fetch, and a refusal or failure removes
// the directory again while it is empty. A ref that can only be resolved
// after preparation (after fetching, or in a repository that names another
// working tree) is checked after that preparation; such a refusal leaves
// the fetched refs and the initialized submodules in place, but moves no
// submodule that was initialized before and changes neither .gitmodules,
// the lock file nor the index. When a later step fails, the submodules
// moved before are checked out at their previous commit again, and the
// lock entries and native branch keys that were written get their previous
// values back; initialized submodules stay initialized.
//
// Context: without opts.Fetch, only local refs are consulted and nothing
// uses the network.
// Return: the changes and the new commit; an empty result when there is no
// managed submodule and no name is given; an error wrapping ErrNotFound or
// ErrUnmanaged for a name that cannot be updated; an error joining the
// refusals, each wrapping ErrRefused (ErrDirty, ErrNotSubmodule,
// ErrUninitialized, ErrSymlinkPath, ErrMissingRef or ErrUnrelatedStaged);
// an error from reading or writing .gitmodules or the lock file; or
// *git.Error. When the commit fails, the result lists the staged changes
// along with the error.
func (r *Repo) Update(ctx context.Context, opts UpdateOptions) (UpdateResult, error) {
	subs, lk, err := r.load(ctx, opts.Names, selectManaged)
	if err != nil || len(subs) == 0 {
		return UpdateResult{}, err
	}
	u := &updater{repo: r, opts: opts}
	if err := u.run(ctx, subs, lk); err != nil {
		u.removeCreated()
		return UpdateResult{}, err
	}
	res := UpdateResult{Changes: u.changes()}
	if opts.Commit && !opts.DryRun {
		res.Commit, err = u.commit(ctx, res.Changes)
	}
	return res, err
}

// updater carries the state of one Update.
type updater struct {
	repo  *Repo
	opts  UpdateOptions
	steps []*step
	// mu guards created, which lists the working tree directories that plan
	// created, in the order of creation.
	mu      sync.Mutex
	created []createdDir
}

// createdDir is a working tree directory that did not exist before Update,
// with the topmost of the directories created for it.
type createdDir struct {
	dir string
	top string
}

// run plans the update and, unless it is a dry run, prepares and applies
// it.
func (u *updater) run(ctx context.Context, subs []manifest.Submodule, lk *lock.Lock) error {
	if err := u.plan(ctx, subs, lk); err != nil || u.opts.DryRun {
		return err
	}
	if err := u.prepare(ctx); err != nil {
		return err
	}
	return u.apply(ctx)
}

// step is the update of one submodule.
type step struct {
	change Change
	loc    location
	// resolved reports whether change.New is known.
	resolved bool
	// head is the commit checked out before apply; for a submodule that is
	// initialized, the commit checked out by the initialization.
	head string
	// refusal is the reason why the submodule cannot be updated.
	refusal error
}

// pending reports whether apply must write or stage anything for the step:
// the change modifies something, or the index records the gitlink at
// another commit than the target.
func (s *step) pending() bool {
	return s.change.Changed() || s.loc.gitlink != s.change.New.Commit
}

// changes returns the changes of all steps.
func (u *updater) changes() []Change {
	out := make([]Change, len(u.steps))
	for i, s := range u.steps {
		out[i] = s.change
	}
	return out
}

// resolveOptions returns the resolution options of the update.
func (u *updater) resolveOptions() ResolveOptions {
	return ResolveOptions{IncludePrerelease: u.opts.IncludePrerelease}
}

// plan inspects every selected submodule and joins all refusals.
func (u *updater) plan(ctx context.Context, subs []manifest.Submodule, lk *lock.Lock) error {
	u.steps = make([]*step, len(subs))
	err := forEach(ctx, len(subs), maxParallel, func(ctx context.Context, i int) error {
		s := &step{change: Change{Submodule: subs[i], Old: lockEntry(lk, subs[i].Name)}}
		if err := u.inspect(ctx, s); err != nil {
			return wrapName(subs[i].Name, err)
		}
		u.steps[i] = s
		return nil
	})
	if err != nil {
		return err
	}
	refusals := make([]error, 0, len(subs)+1)
	if u.opts.Commit {
		unrelated, err := u.unrelatedStaged(ctx)
		if err != nil {
			return err
		}
		if len(unrelated) > 0 {
			refusals = append(refusals,
				fmt.Errorf("%w: %s", ErrUnrelatedStaged, listPaths(unrelated)))
		}
	}
	for _, s := range u.steps {
		refusals = append(refusals, s.refusal)
	}
	return errors.Join(refusals...)
}

// inspect locates a submodule, records a refusal when it cannot be
// updated, and resolves its target when the refs can be read before any
// preparation.
func (u *updater) inspect(ctx context.Context, s *step) error {
	r, sub := u.repo, s.change.Submodule
	loc, err := r.locate(ctx, sub)
	if err != nil {
		return err
	}
	s.loc = loc
	if err := u.checkState(ctx, s); err != nil || s.refusal != nil {
		return err
	}
	if err := u.recordGitlink(ctx, s); err != nil {
		return err
	}
	if err := r.checkConfiguredRef(ctx, sub); err != nil {
		return s.refuse(err)
	}
	if u.opts.Fetch && !u.opts.DryRun {
		// The refs change when they are fetched.
		return nil
	}
	dir, ok := s.loc.refDir()
	if !ok {
		// A clone, or a repository that names a missing working tree.
		return nil
	}
	res, err := r.resolveIn(ctx, dir, sub, s.change.Old, u.resolveOptions())
	if _, missing := errors.AsType[*refError](err); missing && u.opts.Fetch {
		// Only a dry run gets here: a fetch could bring the ref.
		return nil
	}
	if err != nil {
		return s.refuse(err)
	}
	s.change.New, s.resolved = res, true
	return nil
}

// checkState records a refusal for a submodule that has uncommitted
// changes, a path that is not a submodule, or no repository to initialize
// from without Fetch. It reads HEAD of a populated submodule.
func (u *updater) checkState(ctx context.Context, s *step) error {
	r, loc, name := u.repo, s.loc, s.change.Submodule.Name
	if s.refusal = loc.refusal(name); s.refusal != nil {
		return nil
	}
	if !loc.populated {
		return u.checkInit(ctx, s)
	}
	dirty, err := r.git.IsDirty(ctx, loc.worktree)
	if err != nil || dirty {
		s.refusal = refusalIf(dirty, name, ErrDirty)
		return err
	}
	s.change.OldHead, err = r.head(ctx, loc.worktree)
	s.head = s.change.OldHead
	return err
}

// checkInit plans the initialization of a submodule that is not populated.
// Without Fetch, the submodule repository must exist and hold the commit
// that the index records, since the initialization checks that commit out
// and may not fetch it. Git cannot run in a repository that names a working
// tree directory that was removed; outside a dry run, that directory is
// created again first. A repository that git still cannot run in is
// checked by the initialization itself.
func (u *updater) checkInit(ctx context.Context, s *step) error {
	name := s.change.Submodule.Name
	hasRepo, err := isDir(s.loc.gitDir)
	if err != nil {
		return err
	}
	s.change.Init, s.change.Clone = true, !hasRepo
	switch {
	case u.opts.Fetch:
		return nil
	case !hasRepo:
		s.refusal = wrapName(name, ErrUninitialized)
		return nil
	case !s.loc.hasGitDir && !u.opts.DryRun:
		if err := u.recreateWorktree(ctx, s); err != nil {
			return err
		}
	}
	if !s.loc.hasGitDir {
		return nil
	}
	_, err = u.repo.git.ResolveCommit(ctx, s.loc.gitDir, s.loc.gitlink)
	if errors.Is(err, git.ErrRefNotFound) {
		s.refusal = fmt.Errorf("%s: %w: its repository lacks the recorded commit %s",
			displayName(name), ErrUninitialized, abbrev(s.loc.gitlink))
		return nil
	}
	return err
}

// recreateWorktree creates the missing working tree directory of a
// submodule, empty, as git leaves it when it deinitializes a submodule, and
// checks whether git can run in the submodule repository now. The
// initialization populates the directory; removeCreated removes it when the
// update fails before.
func (u *updater) recreateWorktree(ctx context.Context, s *step) error {
	created, err := u.mkdirs(s)
	if err != nil || !created {
		return err
	}
	s.loc.hasGitDir, err = u.repo.isGitDir(ctx, s.loc.gitDir)
	return err
}

// mkdirs creates the working tree directory of a step and its missing
// parents, and records them in u.created. It reports false when the
// directory exists. Steps create their directories one at a time, so that
// u.created lists them in the order of creation.
func (u *updater) mkdirs(s *step) (bool, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	dir := s.loc.worktree
	found, err := exists(dir)
	if err != nil || found {
		return false, err
	}
	c := createdDir{dir: dir, top: firstMissing(u.repo.root, s.change.Submodule.Path)}
	u.created = append(u.created, c)
	// As git creates working trees: subject to the umask.
	if err := os.MkdirAll(dir, 0o777); err != nil {
		return false, fmt.Errorf("create working tree: %w", err)
	}
	return true, nil
}

// removeCreated removes the directories that plan created, the last
// created first, as far as they are still empty.
func (u *updater) removeCreated() {
	u.mu.Lock()
	defer u.mu.Unlock()
	for _, c := range slices.Backward(u.created) {
		removeEmptyDirs(c)
	}
	u.created = nil
}

// removeEmptyDirs removes a created directory and its parents up to the
// topmost created one, stopping at the first that is not empty or cannot be
// removed; such a directory stays like a deinitialized submodule.
func removeEmptyDirs(c createdDir) {
	for dir := c.dir; ; dir = filepath.Dir(dir) {
		if os.Remove(dir) != nil || dir == c.top {
			return
		}
	}
}

// recordGitlink sets OldGitlink: the gitlink in the index, or with Commit
// the one in HEAD, which is empty when HEAD is unborn or has none.
func (u *updater) recordGitlink(ctx context.Context, s *step) error {
	s.change.OldGitlink = s.loc.gitlink
	if !u.opts.Commit {
		return nil
	}
	r := u.repo
	gitlink, err := r.git.TreeGitlink(ctx, r.root, "HEAD", s.change.Submodule.Path)
	if errors.Is(err, git.ErrRefNotFound) {
		gitlink, err = "", nil
	}
	s.change.OldGitlink = gitlink
	return err
}

// refusalIf returns sentinel for the named submodule when cond holds.
func refusalIf(cond bool, name string, sentinel error) error {
	if !cond {
		return nil
	}
	return wrapName(name, sentinel)
}

// refuse records a *refError as the refusal of the step; it returns every
// other error.
func (s *step) refuse(err error) error {
	if _, ok := errors.AsType[*refError](err); ok {
		s.refusal = err
		return nil
	}
	return err
}

// unrelatedStaged lists the staged paths that a commit of the update must
// not include.
func (u *updater) unrelatedStaged(ctx context.Context) ([]string, error) {
	r := u.repo
	staged, err := r.git.StagedPaths(ctx, r.root)
	if err != nil {
		return nil, err
	}
	allowed := map[string]bool{manifest.File: true, lock.File: true}
	for _, s := range u.steps {
		allowed[s.change.Submodule.Path] = true
	}
	var unrelated []string
	for _, p := range staged {
		if !allowed[p] {
			unrelated = append(unrelated, p)
		}
	}
	return unrelated, nil
}

// listPaths formats paths for a message, naming the first few.
func listPaths(paths []string) string {
	names := make([]string, 0, maxListedPaths+1)
	for _, p := range paths[:min(len(paths), maxListedPaths)] {
		names = append(names, displayName(p))
	}
	if len(paths) > maxListedPaths {
		names = append(names, fmt.Sprintf("and %d more", len(paths)-maxListedPaths))
	}
	return strings.Join(names, ", ")
}

// prepare initializes and fetches the submodules as requested, then
// resolves the targets that were not resolved by plan. Missing refs are
// joined refusals.
func (u *updater) prepare(ctx context.Context) error {
	r := u.repo
	for _, s := range u.steps {
		sub := s.change.Submodule
		err := r.sync(ctx, sub, s.loc.worktree, s.change.Init, u.opts.Fetch, u.opts.Progress)
		if err != nil {
			return wrapName(sub.Name, err)
		}
	}
	refusals := make([]error, len(u.steps))
	err := forEach(ctx, len(u.steps), maxParallel, func(ctx context.Context, i int) error {
		s := u.steps[i]
		if err := u.resolveLate(ctx, s); err != nil {
			return wrapName(s.change.Submodule.Name, err)
		}
		refusals[i] = s.refusal
		return nil
	})
	if err != nil {
		return err
	}
	return errors.Join(refusals...)
}

// resolveLate reads the HEAD of an initialized submodule and resolves a
// target that plan left open, now that the working tree is populated.
func (u *updater) resolveLate(ctx context.Context, s *step) error {
	r, dir := u.repo, s.loc.worktree
	if s.change.Init {
		head, err := r.head(ctx, dir)
		if err != nil {
			return err
		}
		s.head = head
	}
	if s.resolved {
		return nil
	}
	res, err := r.resolveIn(ctx, dir, s.change.Submodule, s.change.Old, u.resolveOptions())
	if err != nil {
		return s.refuse(err)
	}
	s.change.New, s.resolved = res, true
	return nil
}

// apply checks out the targets, writes the lock entries and the native
// branch keys of the pending steps, and stages the result. When a step
// fails, what apply changed before is put back.
func (u *updater) apply(ctx context.Context) error {
	if !slices.ContainsFunc(u.steps, (*step).pending) {
		return nil
	}
	r := u.repo
	undo := &applyUndo{u: u}
	var err error
	if undo.hadLock, err = exists(filepath.Join(r.root, lock.File)); err != nil {
		return err
	}
	if err := u.checkout(ctx, undo); err != nil {
		return errors.Join(err, undo.run(ctx))
	}
	paths := []string{manifest.File, lock.File}
	for _, s := range u.steps {
		if !s.pending() {
			continue
		}
		if err := u.write(ctx, s, undo); err != nil {
			return errors.Join(wrapName(s.change.Submodule.Name, err), undo.run(ctx))
		}
		paths = append(paths, s.change.Submodule.Path)
	}
	// Force: a superproject may ignore "*.lock".
	if err := r.git.Add(ctx, r.root, true, paths...); err != nil {
		return errors.Join(err, undo.run(ctx))
	}
	return nil
}

// checkout moves every submodule whose HEAD is not the target and records
// the moved ones in undo.
func (u *updater) checkout(ctx context.Context, undo *applyUndo) error {
	for _, s := range u.steps {
		if s.head == s.change.New.Commit {
			continue
		}
		if err := u.repo.git.Checkout(ctx, s.loc.worktree, s.change.New.Commit, git.Online); err != nil {
			return wrapName(s.change.Submodule.Name, err)
		}
		undo.moved = append(undo.moved, s)
	}
	return nil
}

// write stores the lock entry and the native branch key of a step when
// they differ, and records in undo what it is about to write.
func (u *updater) write(ctx context.Context, s *step, undo *applyUndo) error {
	r, c := u.repo, s.change
	if entry := c.entry(); c.Old == nil || *c.Old != entry {
		undo.locked = append(undo.locked, s)
		if err := lock.Write(ctx, r.git, r.root, entry); err != nil {
			return err
		}
	}
	sub := c.Submodule
	if !staleBranchKey(sub) {
		return nil
	}
	undo.branched = append(undo.branched, s)
	return manifest.SetTracking(ctx, r.git, r.root, sub.Name, sub.Mode, sub.Ref)
}

// applyUndo records what apply changed, so that a failed apply can put it
// back.
type applyUndo struct {
	u *updater
	// hadLock reports whether the lock file existed before apply.
	hadLock bool
	// moved lists the steps whose submodule was checked out, locked those
	// whose lock entry was written and branched those whose native branch
	// key was written, each in the order of the writes.
	moved    []*step
	locked   []*step
	branched []*step
}

// run puts back the native branch keys, the lock entries and the checked-out
// commits, in this order. It continues after failures and joins their
// errors; it runs even when ctx was canceled.
func (a *applyUndo) run(ctx context.Context) error {
	ctx = context.WithoutCancel(ctx)
	var errs []error
	for _, s := range a.branched {
		errs = append(errs, a.restoreBranchKey(ctx, s.change.Submodule))
	}
	for _, s := range a.locked {
		errs = append(errs, a.restoreLockEntry(ctx, s.change))
	}
	if len(a.locked) > 0 && !a.hadLock {
		errs = append(errs, removeEmpty(filepath.Join(a.u.repo.root, lock.File)))
	}
	errs = append(errs, a.u.restore(ctx, a.moved))
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("roll back update: %w", err)
	}
	return nil
}

// restoreBranchKey puts back the native branch key of a submodule as
// .gitmodules had it before; an empty value is restored as a missing key.
func (a *applyUndo) restoreBranchKey(ctx context.Context, sub manifest.Submodule) error {
	r := a.u.repo
	key := configSection + "." + sub.Name + "." + manifest.KeyBranch
	var err error
	if sub.Branch == "" {
		err = r.git.ConfigUnset(ctx, r.root, manifest.File, key)
	} else {
		err = r.git.ConfigSet(ctx, r.root, manifest.File, key, sub.Branch)
	}
	return withName(sub.Name, err)
}

// restoreLockEntry puts back the previous lock entry of a change, or removes
// the entry when there was none.
func (a *applyUndo) restoreLockEntry(ctx context.Context, c Change) error {
	r := a.u.repo
	if c.Old != nil {
		return withName(c.Submodule.Name, lock.Write(ctx, r.git, r.root, *c.Old))
	}
	return withName(c.Submodule.Name, lock.Remove(ctx, r.git, r.root, c.Submodule.Name))
}

// restore checks out the previous commit of moved submodules. A submodule
// whose HEAD was unborn stays where it is.
func (u *updater) restore(ctx context.Context, moved []*step) error {
	var errs []error
	for _, s := range moved {
		if s.head == "" {
			continue
		}
		if err := u.repo.git.Checkout(ctx, s.loc.worktree, s.head, git.Online); err != nil {
			errs = append(errs, wrapName(s.change.Submodule.Name,
				fmt.Errorf("restore %s: %w", abbrev(s.head), err)))
		}
	}
	return errors.Join(errs...)
}

// commit records the staged update. Nothing is committed when no
// submodule changed or nothing is staged.
func (u *updater) commit(ctx context.Context, changes []Change) (string, error) {
	r := u.repo
	msg := CommitMessage(changes)
	if msg == "" {
		return "", nil
	}
	staged, err := r.git.HasStaged(ctx, r.root)
	if err != nil || !staged {
		return "", err
	}
	if err := r.git.Commit(ctx, r.root, msg); err != nil {
		return "", err
	}
	return r.git.Head(ctx, r.root)
}
