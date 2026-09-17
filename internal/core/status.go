// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package core

import (
	"context"
	"errors"
	"fmt"

	"github.com/FPGArtktic/lazysubmodules/internal/git"
	"github.com/FPGArtktic/lazysubmodules/internal/lock"
	"github.com/FPGArtktic/lazysubmodules/internal/manifest"
)

// State summarizes a submodule for status.
type State string

// States in the order of precedence: the first one that applies is
// reported.
const (
	// StateUnmanaged: the submodule has no lsm-mode key.
	StateUnmanaged State = "unmanaged"
	// StateUninitialized: the submodule is not checked out.
	StateUninitialized State = "uninitialized"
	// StateDirty: the submodule working tree has uncommitted changes.
	StateDirty State = "dirty"
	// StateMissingRef: the configured ref is invalid or not found locally.
	StateMissingRef State = "missing-ref"
	// StateDrift: HEAD differs from the locked commit, or the locked tag
	// now resolves to another commit or no longer exists.
	StateDrift State = "drift"
	// StateBehind: an update would select another ref or commit than the
	// lock records, or there is no lock entry yet.
	StateBehind State = "behind"
	// StateOK: HEAD is the locked commit and nothing newer is available
	// locally.
	StateOK State = "ok"
)

// Status is the state of one submodule.
type Status struct {
	// Submodule is the configuration from .gitmodules.
	Submodule manifest.Submodule
	// Lock is the lock entry, or nil when there is none.
	Lock *lock.Entry
	// Head is the commit checked out in the submodule; empty when the
	// submodule is not checked out or its HEAD is unborn.
	Head string
	// Target is what an update without options would select, also for a
	// submodule that is not checked out when its repository exists; nil
	// when it cannot be resolved or the submodule is unmanaged.
	Target *Resolution
	// State is the summary.
	State State
	// Reason explains the state in one line.
	Reason string
}

// Status reports the state of submodules.
//
// Without names, every submodule in .gitmodules is reported, including
// unmanaged ones; names select submodules, unmanaged ones included. The
// first of these rules that applies to a submodule gives its state:
//
//  1. unmanaged: no lsm-mode key;
//  2. uninitialized: the working tree is not checked out;
//  3. dirty: the working tree has uncommitted changes;
//  4. missing-ref: the configured ref is invalid or does not resolve;
//  5. drift: a lock entry exists and HEAD differs from its commit, or, when
//     it locks a tag, the tag resolves to another commit or is gone;
//  6. behind: there is no lock entry, or the resolved mode, ref or commit
//     differs from it;
//  7. ok.
//
// A detached HEAD is normal. Submodules nested in a submodule are not
// managed: they are not listed, and only modified files inside them make
// the submodule dirty. Up to eight submodules are inspected at the same
// time. Only local refs are consulted.
//
// Context: the refs of the submodules must have been fetched before.
// Return: one Status per selected submodule, in .gitmodules order; or an
// error wrapping ErrNotFound for an unknown name, an error from reading
// .gitmodules or the lock file, or the first *git.Error of an invocation
// that failed for another reason than a missing ref.
func (r *Repo) Status(ctx context.Context, names []string) ([]Status, error) {
	subs, lk, err := r.load(ctx, names, selectAll)
	if err != nil {
		return nil, err
	}
	out := make([]Status, len(subs))
	err = forEach(ctx, len(subs), maxParallel, func(ctx context.Context, i int) error {
		st, err := r.status(ctx, subs[i], lockEntry(lk, subs[i].Name))
		if err != nil {
			return wrapName(subs[i].Name, err)
		}
		out[i] = st
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// setState records the state and its reason. Characters of refs or tag
// names that are not printable are replaced in the reason.
func (st *Status) setState(state State, reason string) {
	st.State, st.Reason = state, printable(reason)
}

// status inspects one submodule.
func (r *Repo) status(ctx context.Context, sub manifest.Submodule, locked *lock.Entry) (
	Status, error,
) {
	st := Status{Submodule: sub, Lock: locked}
	loc, err := r.locate(ctx, sub)
	if err != nil {
		return st, err
	}
	if loc.populated {
		if st.Head, err = r.head(ctx, loc.worktree); err != nil {
			return st, err
		}
	}
	if !sub.Managed() {
		st.setState(StateUnmanaged, "no "+manifest.KeyMode+" key")
		return st, nil
	}
	target, missing, err := r.target(ctx, sub, loc, locked)
	if err != nil {
		return st, err
	}
	st.Target = target
	if !loc.populated {
		st.setState(StateUninitialized, uninitializedReason(loc))
		return st, nil
	}
	dirty, err := r.git.IsDirty(ctx, loc.worktree)
	switch {
	case err != nil:
		return st, err
	case dirty:
		st.setState(StateDirty, "working tree has uncommitted changes")
		return st, nil
	case missing != nil:
		st.setState(StateMissingRef, missing.detail)
		return st, nil
	}
	return r.compare(ctx, loc.worktree, st)
}

// compare applies the rules that compare HEAD, the lock entry and the
// target of a checked-out, clean submodule whose ref resolved.
func (r *Repo) compare(ctx context.Context, dir string, st Status) (Status, error) {
	reason, err := r.drift(ctx, dir, st.Head, st.Lock)
	if err != nil {
		return st, err
	}
	if reason != "" {
		st.setState(StateDrift, reason)
		return st, nil
	}
	if reason = behind(st.Lock, st.Target); reason != "" {
		st.setState(StateBehind, reason)
		return st, nil
	}
	st.setState(StateOK, "up to date")
	return st, nil
}

// head returns the commit checked out in dir, or "" for an unborn HEAD.
func (r *Repo) head(ctx context.Context, dir string) (string, error) {
	head, err := r.git.Head(ctx, dir)
	if errors.Is(err, git.ErrRefNotFound) {
		return "", nil
	}
	return head, err
}

// target resolves the configured ref of a managed submodule with default
// options. When the ref is invalid or does not resolve, it returns a
// *refError instead of a resolution; when no repository of the submodule
// can be read, it returns neither.
func (r *Repo) target(ctx context.Context, sub manifest.Submodule, loc location,
	locked *lock.Entry,
) (*Resolution, *refError, error) {
	err := r.checkConfiguredRef(ctx, sub)
	if missing, ok := errors.AsType[*refError](err); ok {
		return nil, missing, nil
	}
	if err != nil {
		return nil, nil, err
	}
	dir, ok := loc.refDir()
	if !ok {
		return nil, nil, nil
	}
	res, err := r.resolveIn(ctx, dir, sub, locked, ResolveOptions{})
	if missing, ok := errors.AsType[*refError](err); ok {
		return nil, missing, nil
	}
	if err != nil {
		return nil, nil, err
	}
	return &res, nil, nil
}

// drift explains how the checked-out commit or the locked tag in dir
// departed from the lock entry; it returns "" when they agree or there is
// no entry.
func (r *Repo) drift(ctx context.Context, dir, head string, locked *lock.Entry) (
	string, error,
) {
	switch {
	case locked == nil:
		return "", nil
	case head != locked.Commit:
		return fmt.Sprintf("HEAD %s differs from locked commit %s",
			displayCommit(head), abbrev(locked.Commit)), nil
	case !isTagMode(locked.Mode):
		return "", nil
	}
	commit, err := r.git.ResolveCommit(ctx, dir, tagsPrefix+locked.Ref)
	switch {
	case errors.Is(err, git.ErrRefNotFound):
		return fmt.Sprintf("locked tag %s no longer exists", locked.Ref), nil
	case err != nil:
		return "", err
	case commit != locked.Commit:
		return fmt.Sprintf("locked tag %s now points to %s, not %s",
			locked.Ref, abbrev(commit), abbrev(locked.Commit)), nil
	}
	return "", nil
}

// behind explains how the target differs from the lock entry; it returns
// "" when they agree. HEAD needs no comparison: the drift rule already
// requires it to be the locked commit.
func behind(locked *lock.Entry, target *Resolution) string {
	switch {
	case locked == nil:
		return "no lock entry"
	case locked.Mode != target.Mode || locked.Ref != target.Ref:
		return fmt.Sprintf("update would select %s %s instead of %s %s",
			target.Mode, target.Ref, locked.Mode, locked.Ref)
	case locked.Commit != target.Commit:
		return fmt.Sprintf("%s %s now resolves to %s, locked %s",
			target.Mode, target.Ref, abbrev(target.Commit), abbrev(locked.Commit))
	}
	return ""
}

// isTagMode reports whether the ref of a mode is a tag.
func isTagMode(mode manifest.Mode) bool {
	return mode == manifest.ModeTag || mode == manifest.ModeTagPattern
}

// displayCommit abbreviates a commit for messages; an empty commit stands
// for an unborn HEAD.
func displayCommit(commit string) string {
	if commit == "" {
		return "(unborn)"
	}
	return abbrev(commit)
}
