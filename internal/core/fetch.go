// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package core

import (
	"context"
	"errors"
	"io"

	"github.com/FPGArtktic/lazysubmodules/internal/manifest"
)

// remote is the remote that submodules are fetched from; git names it so
// when it clones a submodule.
const remote = "origin"

// FetchResult reports what Fetch did for one submodule.
type FetchResult struct {
	// Submodule is the configuration from .gitmodules.
	Submodule manifest.Submodule
	// Init reports that the submodule was initialized.
	Init bool
	// Cloned reports that the repository of the submodule was cloned.
	Cloned bool
}

// Fetch downloads the branches and tags of managed submodules from origin.
//
// A submodule that is not initialized is initialized first, which clones
// its repository when the git directory of the superproject has none. Then
// "git fetch --tags --force --prune origin" runs in the submodule: tags
// moved on the remote replace the local ones, which lets Status and Verify
// detect them, and deleted remote branches are pruned. The submodules are
// processed one after the other, in .gitmodules order, and the first
// failure ends the fetch. A submodule whose path leads through a symbolic
// link, or whose path the index does not record as a submodule, is refused
// before anything is fetched.
//
// Context: uses the network; credentials come from the git configuration.
// Progress receives the output of git; when nil, the output is captured.
// Return: one FetchResult per submodule fetched; an error wrapping
// ErrNotFound or ErrUnmanaged for a name that cannot be fetched, an error
// joining the ErrSymlinkPath and ErrNotSubmodule refusals, or an error from
// reading
// .gitmodules; on a *git.Error, the results of the submodules fetched
// before it.
func (r *Repo) Fetch(ctx context.Context, names []string, progress io.Writer) (
	[]FetchResult, error,
) {
	subs, err := r.Submodules(ctx)
	if err != nil {
		return nil, err
	}
	subs, err = selectSubmodules(subs, names, selectManaged)
	if err != nil {
		return nil, err
	}
	locs, err := r.locateAll(ctx, subs)
	if err != nil {
		return nil, err
	}
	refusals := make([]error, len(subs))
	for i, loc := range locs {
		refusals[i] = loc.refusal(subs[i].Name)
	}
	if err := errors.Join(refusals...); err != nil {
		return nil, err
	}
	results := make([]FetchResult, 0, len(subs))
	for i, sub := range subs {
		res := FetchResult{Submodule: sub, Init: !locs[i].populated}
		if res.Init {
			exists, err := isDir(locs[i].gitDir)
			if err != nil {
				return results, wrapName(sub.Name, err)
			}
			res.Cloned = !exists
		}
		if err := r.sync(ctx, sub, locs[i].worktree, res.Init, true, progress); err != nil {
			return results, wrapName(sub.Name, err)
		}
		results = append(results, res)
	}
	return results, nil
}

// locateAll locates submodules, up to maxParallel at the same time.
func (r *Repo) locateAll(ctx context.Context, subs []manifest.Submodule) ([]location, error) {
	locs := make([]location, len(subs))
	err := forEach(ctx, len(subs), maxParallel, func(ctx context.Context, i int) error {
		loc, err := r.locate(ctx, subs[i])
		if err != nil {
			return wrapName(subs[i].Name, err)
		}
		locs[i] = loc
		return nil
	})
	return locs, err
}

// sync initializes a submodule when init is set and fetches its refs when
// online is set. Without online, the initialization may not use any
// transport, so it only succeeds from an existing submodule repository.
// Git reports success without checking anything out for a path that it does
// not take for a submodule; nothing runs in such a directory, which would
// belong to the superproject.
func (r *Repo) sync(ctx context.Context, sub manifest.Submodule, worktree string,
	init, online bool, progress io.Writer,
) error {
	if init {
		if err := r.git.SubmoduleInit(ctx, r.root, sub.Path, !online, progress); err != nil {
			return err
		}
		populated, err := r.git.IsWorktreeRoot(ctx, worktree)
		if err != nil {
			return err
		}
		if !populated {
			return errors.New("git did not check out the submodule")
		}
	}
	if !online {
		return nil
	}
	return r.git.Fetch(ctx, worktree, remote, progress)
}
