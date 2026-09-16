// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

// Package lock reads and writes .lsm.lock, the lock file of a
// superproject.
//
// For each managed submodule, the lock records the tracking mode, the ref
// that was resolved and the full commit it resolved to:
//
//	[submodule "kernel"]
//		mode = tag-pattern
//		ref = v6.6.8
//		commit = a1b2c3d4e5f6a7b8c9d0a1b2c3d4e5f6a7b8c9d0
//
// Comparing the locked commit with the commit a tag resolves to later
// reveals tags that were moved on the remote. The lock is committed to the
// superproject together with the gitlink change. It is read and written
// exclusively with "git config -f .lsm.lock", so parsing and quoting are
// left to git. Load ignores unknown sections and variables, and Write keeps
// them.
package lock

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/FPGArtktic/lazysubmodules/internal/git"
	"github.com/FPGArtktic/lazysubmodules/internal/manifest"
)

// File is the lock file path relative to the top level of the superproject.
const File = ".lsm.lock"

// Variable names inside a submodule section.
const (
	keyMode   = "mode"
	keyRef    = "ref"
	keyCommit = "commit"
)

// section is the name of the configuration section describing submodules.
const section = "submodule"

// Entry is the locked state of one submodule.
type Entry struct {
	// Name is the submodule name, as in .gitmodules.
	Name string
	// Mode is the tracking mode the ref was resolved with.
	Mode manifest.Mode
	// Ref is the resolved ref: the tag or branch name, or the full commit
	// SHA in commit mode.
	Ref string
	// Commit is the full SHA the ref resolved to: 40 hexadecimal digits, or
	// 64 in SHA-256 repositories.
	Commit string
}

// Lock is the content of a lock file.
//
// A Lock is an immutable snapshot and safe for concurrent use: Write and
// Remove change the file, not a Lock loaded before. The zero value is an
// empty lock.
type Lock struct {
	entries []Entry
	index   map[string]int
}

// Load reads the lock file of a superproject.
//
// Variables are matched as git matches them: the section and variable names
// case-insensitively, the submodule name exactly. The submodule name may
// contain dots. When a variable occurs more than once, the last value wins.
// Every entry must have a known mode, a ref that is safe to pass to git (not
// empty, no leading "-", no white space or control characters) and a
// commit accepted by ValidCommit; the length is not compared with the object
// format of the repository.
//
// Context: root is the top level of the superproject; a missing file yields
// an empty lock.
// Return: the lock, an error wrapping ErrInvalidEntry naming the first
// invalid entry, an error wrapping git.ErrNotRegularFile when the lock file
// is not a regular file (for example a symbolic link committed to the
// superproject), or an error wrapping *git.Error when git cannot read the
// file.
func Load(ctx context.Context, g *git.Runner, root string) (*Lock, error) {
	vars, err := g.ConfigList(ctx, root, File)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", File, err)
	}
	l := &Lock{index: make(map[string]int)}
	for _, v := range vars {
		name, variable, ok := splitKey(v.Key)
		if !ok {
			continue
		}
		i, seen := l.index[name]
		if !seen {
			i = len(l.entries)
			l.index[name] = i
			l.entries = append(l.entries, Entry{Name: name})
		}
		l.entries[i].set(variable, v.Value)
	}
	for _, e := range l.entries {
		if err := e.validate(); err != nil {
			return nil, fmt.Errorf("%s: %w", File, err)
		}
	}
	return l, nil
}

// Get looks up the entry of a submodule.
//
// Context: any; the name is compared exactly, as git compares subsection
// names.
// Return: the entry and true, or the zero Entry and false.
func (l *Lock) Get(name string) (Entry, bool) {
	i, ok := l.index[name]
	if !ok {
		return Entry{}, false
	}
	return l.entries[i], true
}

// Entries lists all entries of the lock.
//
// Context: any.
// Return: a new slice with the entries in the order of their first
// appearance in the file; nil for an empty lock.
func (l *Lock) Entries() []Entry {
	return slices.Clone(l.entries)
}

// set records one variable; unknown variables are ignored.
func (e *Entry) set(variable, value string) {
	switch variable {
	case keyMode:
		e.Mode = manifest.Mode(value)
	case keyRef:
		e.Ref = value
	case keyCommit:
		e.Commit = value
	}
}

// splitKey splits "submodule.<name>.<variable>" at the first and the last
// dot, so that the name may contain dots. Git prints the section and the
// variable in lowercase; they are compared case-insensitively regardless.
func splitKey(key string) (name, variable string, ok bool) {
	sect, rest, found := strings.Cut(key, ".")
	if !found || !strings.EqualFold(sect, section) {
		return "", "", false
	}
	i := strings.LastIndexByte(rest, '.')
	if i < 0 {
		return "", "", false
	}
	return rest[:i], strings.ToLower(rest[i+1:]), true
}

// key returns the configuration key of a submodule variable. Git splits the
// key at the first and the last dot, so the name may contain dots.
func key(name, variable string) string {
	return section + "." + name + "." + variable
}
