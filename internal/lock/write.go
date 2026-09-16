// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package lock

import (
	"context"
	"fmt"
	"slices"

	"github.com/FPGArtktic/lazysubmodules/internal/git"
)

// Write stores the entry of a submodule in the lock file of a superproject.
//
// The mode, ref and commit variables of the submodule are replaced; other
// variables and entries are kept. A missing file or section is created, with
// the variables in that order, the layout documented in README.md.
//
// Context: root is the top level of the superproject. The variables are
// written by separate git invocations, so the update is not atomic.
// Return: nil, an error wrapping ErrInvalidEntry when Load would reject the
// entry, an error wrapping git.ErrNotRegularFile when the lock file is not a
// regular file (nothing is written in both cases), or an error wrapping
// *git.Error.
func Write(ctx context.Context, g *git.Runner, root string, e Entry) error {
	if err := e.validate(); err != nil {
		return fmt.Errorf("write %s: %w", File, err)
	}
	vars := []struct{ name, value string }{
		{keyMode, string(e.Mode)},
		{keyRef, e.Ref},
		{keyCommit, e.Commit},
	}
	for _, v := range vars {
		if err := g.ConfigSet(ctx, root, File, key(e.Name, v.name), v.value); err != nil {
			return fmt.Errorf("write %s: submodule %q: %w", File, e.Name, err)
		}
	}
	return nil
}

// Remove deletes the entry of a submodule, with all its variables, from the
// lock file of a superproject.
//
// "git config --remove-section" matches the section name as spelled in the
// file, while git reads a "[Submodule ...]" header as the same section.
// Remove therefore unsets every variable of the entry, which matches the
// section name case-insensitively, and then removes the remaining header.
//
// Context: root is the top level of the superproject.
// Return: nil, also when the entry or the file does not exist (the file is
// then left untouched), an error wrapping ErrInvalidEntry when name is empty
// or contains a newline or NUL, an error wrapping git.ErrNotRegularFile when
// the lock file is not a regular file, or an error wrapping *git.Error.
func Remove(ctx context.Context, g *git.Runner, root, name string) error {
	if !validName(name) {
		return fmt.Errorf("remove from %s: %w %q: invalid submodule name",
			File, ErrInvalidEntry, name)
	}
	if err := remove(ctx, g, root, name); err != nil {
		return fmt.Errorf("remove from %s: submodule %q: %w", File, name, err)
	}
	return nil
}

// remove implements Remove for a valid name.
func remove(ctx context.Context, g *git.Runner, root, name string) error {
	vars, err := g.ConfigList(ctx, root, File)
	if err != nil {
		return err
	}
	var names []string
	for _, v := range vars {
		n, variable, ok := splitKey(v.Key)
		if ok && n == name && !slices.Contains(names, variable) {
			names = append(names, variable)
		}
	}
	if len(names) == 0 {
		return nil
	}
	for _, variable := range names {
		if err := g.ConfigUnset(ctx, root, File, key(name, variable)); err != nil {
			return err
		}
	}
	return g.ConfigRemoveSection(ctx, root, File, section+"."+name)
}
