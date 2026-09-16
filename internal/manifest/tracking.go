// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package manifest

import (
	"context"
	"fmt"

	"github.com/FPGArtktic/lazysubmodules/internal/git"
)

// SetTracking stores the tracking configuration of a submodule.
//
// It writes lsm-mode and lsm-ref. In branch mode it also writes the native
// branch variable, so that "git submodule update --remote" keeps following
// the same branch without LazySubmodules; in the other modes it removes the
// native branch variable. All other variables are
// kept. The submodule must already exist; SetTracking never creates a
// section, and it may be used to repair an invalid lsm-mode.
//
// Context: root is the top level of the superproject; ref must have been
// validated for the mode by the caller. The writes are separate git
// invocations and are not atomic.
// Return: nil, an error wrapping ErrInvalidMode when mode is not a known
// tracking mode, an error wrapping ErrNotFound when Load would not list the
// submodule, an error wrapping git.ErrNotRegularFile when the file is not a
// regular file (nothing is written in these cases), or an error wrapping
// *git.Error.
func SetTracking(ctx context.Context, g *git.Runner, root, name string, mode Mode,
	ref string,
) error {
	if _, err := ParseMode(string(mode)); err != nil {
		return fmt.Errorf("submodule %q: %w", name, err)
	}
	if err := checkExists(ctx, g, root, name); err != nil {
		return err
	}
	if err := write(ctx, g, root, name, mode, ref); err != nil {
		return fmt.Errorf("%s: submodule %q: %w", File, name, err)
	}
	return nil
}

// write performs the updates of SetTracking. The native branch is written
// first and removed last, so that an interrupted update never leaves branch
// mode without the native key.
func write(ctx context.Context, g *git.Runner, root, name string, mode Mode, ref string) error {
	if mode == ModeBranch {
		if err := g.ConfigSet(ctx, root, File, key(name, KeyBranch), ref); err != nil {
			return err
		}
	}
	if err := g.ConfigSet(ctx, root, File, key(name, KeyMode), string(mode)); err != nil {
		return err
	}
	if err := g.ConfigSet(ctx, root, File, key(name, KeyRef), ref); err != nil {
		return err
	}
	if mode == ModeBranch {
		return nil
	}
	return g.ConfigUnset(ctx, root, File, key(name, KeyBranch))
}

// checkExists returns an error wrapping ErrNotFound unless Load would list
// the submodule. The lsm-mode values are not validated, so that an invalid
// value can be overwritten.
func checkExists(ctx context.Context, g *git.Runner, root, name string) error {
	recs, err := read(ctx, g, root)
	if err != nil {
		return err
	}
	for _, rec := range recs {
		if rec.sub.Name == name && rec.usable() {
			return nil
		}
	}
	return fmt.Errorf("%w in %s: %q", ErrNotFound, File, name)
}

// key returns the configuration key of a submodule variable. Git splits the
// key at the first and the last dot, so the name may contain dots.
func key(name, variable string) string {
	return section + "." + name + "." + variable
}
