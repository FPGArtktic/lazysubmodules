// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

// Package manifest reads and writes the tracking configuration of submodules
// in the .gitmodules file of a superproject.
//
// The configuration lives next to the native keys, under the "lsm-" prefix:
//
//	[submodule "kernel"]
//		path = kernel
//		url = https://git.example.org/linux.git
//		lsm-mode = tag-pattern
//		lsm-ref = v6.6.*
//
// The file is read and written exclusively with "git config -f .gitmodules",
// so parsing, quoting and the preservation of unknown keys are left to git.
// Submodules without lsm-mode are unmanaged; they are modified only when
// SetTracking is called for them explicitly.
package manifest

import (
	"context"
	"fmt"
	"path"
	"path/filepath"
	"strings"

	"github.com/FPGArtktic/lazysubmodules/internal/git"
)

// File is the manifest path relative to the top level of the superproject.
const File = ".gitmodules"

// Variable names inside a submodule section.
const (
	// KeyMode holds the tracking mode.
	KeyMode = "lsm-mode"
	// KeyRef holds the tracked branch, tag, tag pattern or commit.
	KeyRef = "lsm-ref"
	// KeyBranch is the native branch used by "git submodule update --remote".
	KeyBranch = "branch"
	// KeyPath is the native path of the submodule in the working tree.
	KeyPath = "path"
	// KeyURL is the native URL of the submodule repository.
	KeyURL = "url"
)

// section is the name of the configuration section describing submodules.
const section = "submodule"

// Submodule is one submodule described in .gitmodules.
type Submodule struct {
	// Name is the subsection name, with its case preserved.
	Name string
	// Path is the path as stored, relative to the top level, "/"-separated.
	Path string
	// URL is the native url value.
	URL string
	// Branch is the native branch value.
	Branch string
	// Mode is the lsm-mode value; empty when the submodule is unmanaged.
	Mode Mode
	// Ref is the lsm-ref value.
	Ref string
}

// Managed reports whether LazySubmodules tracks the submodule.
//
// Context: any.
// Return: true when lsm-mode is set.
func (s Submodule) Managed() bool {
	return s.Mode != ""
}

// Load reads all submodules from the .gitmodules file of a superproject.
//
// Variables are matched as git matches them: the section and variable names
// case-insensitively, the submodule name exactly. The submodule name may
// contain dots. When a variable occurs more than once, the last value wins.
// Unknown variables and other sections are ignored.
//
// Entries that git itself does not treat as a submodule are skipped: an
// entry without a path, an entry whose name is empty or has a ".." path
// component, and an entry whose path is not a clean relative path inside the
// working tree (git never records a gitlink at such a path). As in git, path
// and url values starting with "-" are ignored.
//
// Context: root is the top level of the superproject; a missing file yields
// no submodules.
// Return: the submodules in the order of their first appearance in the file,
// an error wrapping ErrInvalidMode naming the first submodule with an
// invalid lsm-mode, an error wrapping git.ErrNotRegularFile when the file is
// not a regular file (such as a symbolic link), or an error wrapping
// *git.Error when git cannot read the file.
func Load(ctx context.Context, g *git.Runner, root string) ([]Submodule, error) {
	recs, err := read(ctx, g, root)
	if err != nil {
		return nil, err
	}
	var subs []Submodule
	for _, rec := range recs {
		if !rec.usable() {
			continue
		}
		sub := rec.sub
		if rec.hasMode {
			sub.Mode, err = ParseMode(rec.mode)
			if err != nil {
				return nil, fmt.Errorf("%s: submodule %q: %s: %w", File, sub.Name, KeyMode, err)
			}
		}
		subs = append(subs, sub)
	}
	return subs, nil
}

// Find looks up a submodule by name.
//
// Context: any; the name is compared exactly, as git compares subsection
// names.
// Return: the submodule and true, or the zero Submodule and false.
func Find(subs []Submodule, name string) (Submodule, bool) {
	for _, sub := range subs {
		if sub.Name == name {
			return sub, true
		}
	}
	return Submodule{}, false
}

// record collects the variables of one submodule before validation.
type record struct {
	sub     Submodule
	mode    string
	hasMode bool
}

// read lists the variables of the .gitmodules file below root and groups
// them by submodule, in the order of first appearance.
func read(ctx context.Context, g *git.Runner, root string) ([]record, error) {
	entries, err := g.ConfigList(ctx, root, File)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", File, err)
	}
	var recs []record
	index := make(map[string]int)
	for _, e := range entries {
		name, variable, ok := splitKey(e.Key)
		if !ok {
			continue
		}
		i, seen := index[name]
		if !seen {
			i = len(recs)
			index[name] = i
			recs = append(recs, record{sub: Submodule{Name: name}})
		}
		recs[i].set(variable, e.Value)
	}
	return recs, nil
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

// set records one variable. Like git, it ignores path and url values that
// would be taken for a command line option.
func (r *record) set(variable, value string) {
	switch variable {
	case KeyPath:
		if !strings.HasPrefix(value, "-") {
			r.sub.Path = value
		}
	case KeyURL:
		if !strings.HasPrefix(value, "-") {
			r.sub.URL = value
		}
	case KeyBranch:
		r.sub.Branch = value
	case KeyMode:
		r.mode, r.hasMode = value, true
	case KeyRef:
		r.sub.Ref = value
	}
}

// usable reports whether git treats the record as a submodule.
func (r *record) usable() bool {
	return validName(r.sub.Name) && validPath(r.sub.Path)
}

// validName mirrors check_submodule_name() of git: the name is not empty and
// no component separated by "/" or "\" is "..". The name becomes a path
// below the git directory of the superproject.
func validName(name string) bool {
	if name == "" {
		return false
	}
	isSep := func(r rune) bool { return r == '/' || r == '\\' }
	for comp := range strings.FieldsFuncSeq(name, isSep) {
		if comp == ".." {
			return false
		}
	}
	return true
}

// validPath reports whether p is a non-empty, clean, relative
// "/"-separated path below the top level, other than the top level itself.
func validPath(p string) bool {
	return p != "." && path.Clean(p) == p && filepath.IsLocal(filepath.FromSlash(p))
}
