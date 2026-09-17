// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package core

import (
	"context"
	"errors"

	"github.com/FPGArtktic/lazysubmodules/internal/git"
	"github.com/FPGArtktic/lazysubmodules/internal/lock"
	"github.com/FPGArtktic/lazysubmodules/internal/manifest"
)

// headRev names the commit that an update with Commit replaces.
const headRev = "HEAD"

// snapshot is a copy of the tracking configuration of the superproject:
// .gitmodules and the lock file as the index or a commit records them.
type snapshot struct {
	subs []manifest.Submodule
	lock *lock.Lock
}

// tracking is what a snapshot records for one submodule.
type tracking struct {
	// sub is the configuration from .gitmodules, or nil when there is none.
	sub *manifest.Submodule
	// entry is the lock entry, or nil when there is none.
	entry *lock.Entry
}

// loadSnapshot reads the tracking configuration that the superproject
// records in rev, or in the index when rev is empty.
//
// A HEAD without commits records nothing, and neither does a copy of
// .gitmodules or the lock file that is not a regular file or, in the index,
// has merge conflicts: an update stages the working tree copy in its place,
// which replaces the link or resolves the conflict. An invalid entry of a
// copy records nothing for its submodule, rather than failing: the working
// tree copy was read without error, so it may hold the repair of the entry
// (Set replaces an invalid lsm-mode, for example), which an update then
// stages.
func (r *Repo) loadSnapshot(ctx context.Context, rev string) (snapshot, error) {
	subs, err := manifest.LoadRev(ctx, r.git, r.root, rev)
	switch {
	case errors.Is(err, git.ErrRefNotFound):
		return snapshot{lock: &lock.Lock{}}, nil
	case errors.Is(err, manifest.ErrInvalidMode):
		// LoadRev returns the valid entries along with the error.
		err = nil
	case unreadableCopy(err):
		subs, err = nil, nil
	}
	if err != nil {
		return snapshot{}, err
	}
	lk, err := lock.LoadRev(ctx, r.git, r.root, rev)
	switch {
	case errors.Is(err, lock.ErrInvalidEntry):
		err = nil
	case unreadableCopy(err):
		lk, err = &lock.Lock{}, nil
	}
	if err != nil {
		return snapshot{}, err
	}
	return snapshot{subs: subs, lock: lk}, nil
}

// unreadableCopy reports whether err means that the copy of a configuration
// file in the index or a commit cannot be read as one: it is not a regular
// file, or the index has merge conflicts for it.
func unreadableCopy(err error) bool {
	return errors.Is(err, git.ErrNotRegularFile) || errors.Is(err, git.ErrUnmerged)
}

// tracking returns what the snapshot records for the named submodule.
func (s snapshot) tracking(name string) tracking {
	t := tracking{entry: lockEntry(s.lock, name)}
	if sub, ok := manifest.Find(s.subs, name); ok {
		t.sub = &sub
	}
	return t
}

// sameEntry reports whether the lock entry e exists and equals want.
func sameEntry(e *lock.Entry, want lock.Entry) bool {
	return e != nil && *e == want
}
