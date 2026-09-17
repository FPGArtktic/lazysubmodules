// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package core

import (
	"context"
	"errors"

	"github.com/FPGArtktic/lazysubmodules/internal/manifest"
)

// Set changes the tracking configuration of a submodule in .gitmodules.
//
// The ref is validated for the mode first. In commit mode, an abbreviated
// commit is expanded to the full commit name with the refs of the
// submodule, which must be available locally; a full commit name is stored
// as given. In branch mode the native branch key is written too, and the
// other modes remove it. Set accepts an unmanaged submodule, which becomes
// managed, and a submodule with an invalid lsm-mode, which it repairs.
// Nothing else changes: the submodule, the lock file and the index are
// left alone until Update applies the configuration.
//
// Context: offline; refs are read from the submodule working tree or from
// its repository in the git directory of the superproject. While another
// submodule in .gitmodules has an invalid lsm-mode, an abbreviated commit
// cannot be expanded.
// Return: the configuration after the change (only Name, Mode, Ref and
// Branch are filled while .gitmodules still has an invalid lsm-mode); an
// error naming the submodule and wrapping ErrInvalidArgument for an invalid
// mode or ref or for an abbreviated commit that cannot be expanded; an
// error wrapping ErrNotFound for an unknown name; an error from reading or
// writing .gitmodules; or *git.Error.
func (r *Repo) Set(ctx context.Context, name string, mode manifest.Mode, ref string) (
	manifest.Submodule, error,
) {
	if err := r.checkRef(ctx, mode, ref); err != nil {
		return manifest.Submodule{}, wrapName(name, err)
	}
	subs, err := r.Submodules(ctx)
	repair := errors.Is(err, manifest.ErrInvalidMode)
	if err != nil && !repair {
		return manifest.Submodule{}, err
	}
	sub, found := manifest.Find(subs, name)
	if !found && !repair {
		return manifest.Submodule{}, wrapName(name, ErrNotFound)
	}
	if mode == manifest.ModeCommit && len(ref) < r.hexLen {
		ref, err = r.expandCommit(ctx, sub, found, ref)
		if err != nil {
			return manifest.Submodule{}, wrapName(name, err)
		}
	}
	err = manifest.SetTracking(ctx, r.git, r.root, name, mode, ref)
	if errors.Is(err, manifest.ErrNotFound) {
		return manifest.Submodule{}, wrapName(name, ErrNotFound)
	}
	if err != nil {
		return manifest.Submodule{}, err
	}
	if repair {
		subs, err = r.Submodules(ctx)
		if err == nil {
			sub, _ = manifest.Find(subs, name)
		}
	}
	sub.Name, sub.Mode, sub.Ref, sub.Branch = name, mode, ref, ""
	if mode == manifest.ModeBranch {
		sub.Branch = ref
	}
	return sub, nil
}

// expandCommit returns the full name of an abbreviated commit, read from
// the refs of a submodule that was found in .gitmodules. Its errors do not
// name the submodule.
func (r *Repo) expandCommit(ctx context.Context, sub manifest.Submodule, found bool,
	ref string,
) (string, error) {
	invalid := func(reason string) error {
		return &invalidError{what: refKind(manifest.ModeCommit), value: ref, reason: reason}
	}
	if !found {
		return "", invalid("cannot be expanded while " + manifest.File +
			" is invalid; give the full commit name")
	}
	loc, err := r.locate(ctx, sub)
	if err != nil {
		return "", err
	}
	dir, ok := loc.refDir()
	if !ok {
		return "", invalid("cannot be expanded without the submodule repository; " +
			"give the full commit name")
	}
	res, err := r.resolveCommit(ctx, dir, manifest.Submodule{
		Name: sub.Name, Mode: manifest.ModeCommit, Ref: ref,
	})
	if missing, ok := errors.AsType[*refError](err); ok {
		return "", invalid(missing.detail)
	}
	if err != nil {
		return "", err
	}
	return res.Commit, nil
}
