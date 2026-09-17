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
	l, err := parse(File, vars)
	if err != nil {
		return nil, err
	}
	return l, nil
}

// LoadRev reads the lock file recorded in a revision of a superproject, or
// in its index.
//
// The recorded file is parsed and validated as Load does it with the working
// tree copy. Objects missing from a partial clone are not fetched.
//
// Context: root is the top level of the superproject; rev names a commit,
// such as "HEAD", or is empty for the index. A revision or index without the
// file yields an empty lock.
// Return: the lock; an error wrapping ErrInvalidEntry naming the first
// invalid entry, along with a lock of the other entries; an error wrapping
// git.ErrRefNotFound when rev does not exist (for example an unborn HEAD);
// an error wrapping git.ErrInvalidRefName when rev starts with "-"; an
// error wrapping git.ErrUnmerged when the index holds conflict stages for
// the file; or an error as Load returns it, where git.ErrNotRegularFile
// means that the recorded file is not a regular file.
func LoadRev(ctx context.Context, g *git.Runner, root, rev string) (*Lock, error) {
	src := rev + ":" + File
	vars, err := g.ConfigListRev(ctx, root, rev, File)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", src, err)
	}
	return parse(src, vars)
}

// parse builds a lock from the variables of a lock file. It returns the
// valid entries, and an error wrapping ErrInvalidEntry that names the first
// invalid one; src names the file in errors.
func parse(src string, vars []git.ConfigEntry) (*Lock, error) {
	var entries []Entry
	index := make(map[string]int)
	for _, v := range vars {
		name, variable, ok := splitKey(v.Key)
		if !ok {
			continue
		}
		i, seen := index[name]
		if !seen {
			i = len(entries)
			index[name] = i
			entries = append(entries, Entry{Name: name})
		}
		entries[i].set(variable, v.Value)
	}
	l := &Lock{index: make(map[string]int, len(entries))}
	var invalid error
	for _, e := range entries {
		if err := e.validate(); err != nil {
			if invalid == nil {
				invalid = fmt.Errorf("%s: %w", src, err)
			}
			continue
		}
		l.index[e.Name] = len(l.entries)
		l.entries = append(l.entries, e)
	}
	return l, invalid
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
