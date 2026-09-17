// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

// Package core holds the business logic of LazySubmodules.
//
// A Repo gives access to the submodules of a superproject. It resolves the
// configured branch, tag, tag pattern or commit of each managed submodule to
// a commit, reports the state of every submodule and verifies that the lock
// file, the committed gitlinks and the checked-out commits agree. The
// command line and the terminal interfaces contain no business logic; they
// call this package.
//
// Resolution is local: only refs that were fetched before are consulted,
// and no function of this package uses the network unless its
// documentation says so.
//
// Errors that describe an unsafe state wrap ErrRefused, invalid input from
// the caller wraps ErrInvalidArgument, failed verification wraps ErrVerify,
// and failed git invocations are reported as *git.Error.
package core

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"github.com/FPGArtktic/lazysubmodules/internal/git"
	"github.com/FPGArtktic/lazysubmodules/internal/lock"
	"github.com/FPGArtktic/lazysubmodules/internal/manifest"
)

// Lengths of a full object name in hexadecimal digits.
const (
	sha1HexLen   = 40
	sha256HexLen = 64
)

// abbrevLen is the length of the abbreviated commit names in messages.
const abbrevLen = 12

// Repo is a superproject. It is immutable and safe for concurrent use.
type Repo struct {
	git  *git.Runner
	root string
	// cwd is the absolute directory the Repo was opened from, without
	// symbolic links when they can be resolved. Display paths are relative
	// to it.
	cwd    string
	hexLen int
}

// Open finds the superproject containing dir.
//
// The top level of the working tree that contains dir becomes the root of
// the Repo; inside a checked-out submodule, that is the submodule. The
// object format of the repository determines the length of commit names.
// Paths shown to the commands run by Foreach are relative to dir.
//
// Context: dir must be inside a working tree; the Runner is used for every
// later git invocation.
// Return: the Repo, *git.Error when dir is not inside a working tree, or an
// error for an unsupported object format.
func Open(ctx context.Context, g *git.Runner, dir string) (*Repo, error) {
	root, err := g.TopLevel(ctx, dir)
	if err != nil {
		return nil, fmt.Errorf("open repository: %w", err)
	}
	format, err := g.ObjectFormat(ctx, root)
	if err != nil {
		return nil, fmt.Errorf("open repository: %w", err)
	}
	cwd, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("open repository: %w", err)
	}
	// Git reports the top level without symbolic links. When dir cannot be
	// resolved, display paths merely become longer.
	if resolved, err := filepath.EvalSymlinks(cwd); err == nil {
		cwd = resolved
	}
	r := &Repo{git: g, root: root, cwd: cwd}
	switch format {
	case "sha1":
		r.hexLen = sha1HexLen
	case "sha256":
		r.hexLen = sha256HexLen
	default:
		return nil, fmt.Errorf("open repository %s: unsupported object format %q", root, format)
	}
	return r, nil
}

// Root returns the top level of the superproject.
//
// Context: any.
// Return: the absolute path printed by git.
func (r *Repo) Root() string {
	return r.root
}

// Submodules lists the submodules described in .gitmodules.
//
// Context: reads the working tree copy of .gitmodules.
// Return: the submodules in file order (none when the file is missing), or
// an error from manifest.Load, for example for an invalid lsm-mode value.
func (r *Repo) Submodules(ctx context.Context) ([]manifest.Submodule, error) {
	return manifest.Load(ctx, r.git, r.root)
}

// selection tells which submodules a command works on.
type selection int

const (
	// selectAll lists every submodule and accepts the names of unmanaged
	// ones. Status uses it.
	selectAll selection = iota
	// selectManaged lists the managed submodules and refuses the names of
	// unmanaged ones. Every command except status uses it.
	selectManaged
)

// load reads .gitmodules and the lock file and selects the submodules a
// command works on.
func (r *Repo) load(ctx context.Context, names []string, sel selection) (
	[]manifest.Submodule, *lock.Lock, error,
) {
	subs, err := r.Submodules(ctx)
	if err != nil {
		return nil, nil, err
	}
	subs, err = selectSubmodules(subs, names, sel)
	if err != nil {
		return nil, nil, err
	}
	lk, err := lock.Load(ctx, r.git, r.root)
	if err != nil {
		return nil, nil, err
	}
	return subs, lk, nil
}

// selectSubmodules applies the selection rule to the submodules of
// .gitmodules. Without names, all submodules are selected (only the managed
// ones with selectManaged). Named submodules are selected once each, in
// file order. Unknown names are reported first, joined, each wrapping
// ErrNotFound; then, with selectManaged, the names of unmanaged submodules,
// each wrapping ErrUnmanaged.
func selectSubmodules(subs []manifest.Submodule, names []string, sel selection) (
	[]manifest.Submodule, error,
) {
	wanted := make(map[string]bool, len(names))
	var unknown []error
	for _, name := range names {
		if _, ok := manifest.Find(subs, name); !ok && !wanted[name] {
			unknown = append(unknown, wrapName(name, ErrNotFound))
		}
		wanted[name] = true
	}
	if len(unknown) > 0 {
		return nil, errors.Join(unknown...)
	}
	var selected []manifest.Submodule
	var unmanaged []error
	for _, sub := range subs {
		switch {
		case len(names) > 0 && !wanted[sub.Name]:
			continue
		case sel == selectManaged && !sub.Managed():
			if len(names) > 0 {
				unmanaged = append(unmanaged, wrapName(sub.Name, ErrUnmanaged))
			}
			continue
		}
		selected = append(selected, sub)
	}
	if len(unmanaged) > 0 {
		return nil, errors.Join(unmanaged...)
	}
	return selected, nil
}

// lockEntry returns a copy of the lock entry of a submodule, or nil.
func lockEntry(lk *lock.Lock, name string) *lock.Entry {
	e, ok := lk.Get(name)
	if !ok {
		return nil
	}
	return &e
}

// wrapName prefixes err with the name of the submodule it concerns.
func wrapName(name string, err error) error {
	return fmt.Errorf("%s: %w", displayName(name), err)
}

// displayName returns a submodule name for messages. Names that are empty
// or contain quotes, backslashes or characters that are not printable are
// quoted, so that a crafted .gitmodules cannot inject terminal control
// sequences.
func displayName(name string) string {
	quote := name == "" || strings.ContainsFunc(name, func(c rune) bool {
		return c == '"' || c == '\\' || !unicode.IsPrint(c)
	})
	if quote {
		return strconv.Quote(name)
	}
	return name
}

// printable replaces the characters of s that are not printable, such as
// terminal control sequences in the output of a signature tool.
func printable(s string) string {
	return strings.Map(func(c rune) rune {
		if unicode.IsPrint(c) {
			return c
		}
		return unicode.ReplacementChar
	}, s)
}

// abbrev shortens a full commit name for messages.
func abbrev(commit string) string {
	if len(commit) > abbrevLen {
		return commit[:abbrevLen]
	}
	return commit
}
