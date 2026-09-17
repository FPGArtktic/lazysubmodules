// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package core

import (
	"slices"
	"strings"
	"testing"

	"github.com/FPGArtktic/lazysubmodules/internal/lock"
	"github.com/FPGArtktic/lazysubmodules/internal/manifest"
)

// branchSub returns the working tree configuration of a submodule that
// tracks branch main, with the given native branch key.
func branchSub(nativeBranch string) manifest.Submodule {
	return manifest.Submodule{Name: "b", Path: "b", Mode: manifest.ModeBranch, Ref: "main",
		Branch: nativeBranch}
}

func TestChangeRecordChanged(t *testing.T) {
	t.Parallel()
	commit := strings.Repeat("a1", 20)
	target := Resolution{Mode: manifest.ModeBranch, Ref: "main", Commit: commit}
	entry := lock.Entry{Name: "b", Mode: manifest.ModeBranch, Ref: "main", Commit: commit}
	change := func(nativeBranch string, keys keyState, staleLock bool) Change {
		old := entry
		return Change{Submodule: branchSub(nativeBranch), Old: &old, OldHead: commit,
			OldGitlink: commit, New: target, keys: keys, staleLock: staleLock}
	}
	with := func(c Change, edit func(*Change)) Change {
		edit(&c)
		return c
	}
	staleIndex := func(c *Change) { c.staleIndex = true }
	both := []string{manifest.File, lock.File}
	for _, c := range []struct {
		name              string
		change            Change
		changed, recorded bool
		files             []string
		index             bool
	}{
		{"up to date", change("main", keysAgree, false), false, false, nil, false},
		{"working tree lock", change("main", keysAgree, true), true, false,
			[]string{lock.File}, false},
		{"working tree branch key", change("", keysAgree, false), true, false,
			[]string{manifest.File}, false},
		{"working tree copies", change("", keysAgree, true), true, false, both, false},
		{"index", with(change("main", keysAgree, false), staleIndex), true, false, nil, true},
		{"index and working tree copies", with(change("", keysAgree, true), staleIndex),
			true, false, both, true},
		{"recorded keys", change("main", keysDiffer, false), true, true, nil, false},
		{"recorded keys and index", with(change("", keysDiffer, true), staleIndex),
			true, true, nil, false},
		{"keys of the submodule", change("", keysOfSubmodule, false), true, true, nil, false},
		{"up to date without comparison", change("main", keysOfSubmodule, false),
			false, false, nil, false},
		{"lock entry", with(change("main", keysAgree, false), func(c *Change) {
			c.Old.Ref = "develop"
		}), true, true, nil, false},
		{"gitlink", with(change("main", keysAgree, false), func(c *Change) {
			c.OldGitlink = strings.Repeat("b2", 20)
		}), true, true, nil, false},
		{"checkout", with(change("main", keysAgree, false), func(c *Change) {
			c.OldHead = strings.Repeat("b2", 20)
		}), true, false, nil, false},
	} {
		if got := c.change.Changed(); got != c.changed {
			t.Errorf("%s: Changed() = %v, want %v", c.name, got, c.changed)
		}
		if got := c.change.RecordChanged(); got != c.recorded {
			t.Errorf("%s: RecordChanged() = %v, want %v", c.name, got, c.recorded)
		}
		if got := c.change.RestoredFiles(); !slices.Equal(got, c.files) {
			t.Errorf("%s: RestoredFiles() = %q, want %q", c.name, got, c.files)
		}
		if got := c.change.RestoresIndex(); got != c.index {
			t.Errorf("%s: RestoresIndex() = %v, want %v", c.name, got, c.index)
		}
	}
}

func TestStepSetTarget(t *testing.T) {
	t.Parallel()
	commit := strings.Repeat("a1", 20)
	target := Resolution{Mode: manifest.ModeBranch, Ref: "main", Commit: commit}
	entry := lock.Entry{Name: "b", Mode: manifest.ModeBranch, Ref: "main", Commit: commit}
	other := entry
	other.Commit = strings.Repeat("b2", 20)
	recorded := func(sub manifest.Submodule) *manifest.Submodule { return &sub }
	pattern := branchSub("")
	pattern.Mode, pattern.Ref = manifest.ModeTagPattern, "v*"
	agreeing := tracking{recorded(branchSub("main")), &entry}
	for _, c := range []struct {
		name string
		// worktree is the native branch key of the working tree.
		worktree string
		written  *lock.Entry
		recorded tracking
		// index and gitlink are what the index records; an index without a
		// lock entry stands for agreeing.
		index      tracking
		gitlink    string
		staleLock  bool
		staleIndex bool
		keys       keyState
	}{
		{"agree", "main", &entry, agreeing, tracking{}, commit, false, false, keysAgree},
		{"working tree copies", "", &other, agreeing, tracking{}, commit,
			true, false, keysAgree},
		{"no working tree lock", "main", nil, agreeing, tracking{}, commit,
			true, false, keysAgree},
		{"no recorded keys", "main", &entry, tracking{nil, &entry}, tracking{}, commit,
			false, false, keysDiffer},
		{"recorded branch key", "main", &entry, tracking{recorded(branchSub("")), &entry},
			tracking{}, commit, false, false, keysDiffer},
		{"recorded mode", "main", &entry, tracking{recorded(pattern), &entry}, tracking{},
			commit, false, false, keysDiffer},
		// The lock entry is compared through Change.Old.
		{"recorded lock", "main", &entry, tracking{recorded(branchSub("main")), &other},
			tracking{}, commit, false, false, keysAgree},
		{"index gitlink", "main", &entry, agreeing, tracking{}, other.Commit,
			false, true, keysAgree},
		{"index lock", "main", &entry, agreeing,
			tracking{recorded(branchSub("main")), &other}, commit, false, true, keysAgree},
		{"index branch key", "main", &entry, agreeing,
			tracking{recorded(branchSub("")), &entry}, commit, false, true, keysAgree},
		{"index mode", "main", &entry, agreeing, tracking{recorded(pattern), &entry}, commit,
			false, true, keysAgree},
		{"no index keys", "main", &entry, agreeing, tracking{nil, &entry}, commit,
			false, true, keysAgree},
	} {
		index := c.index
		if index.entry == nil {
			index = agreeing
		}
		s := &step{written: c.written, recorded: c.recorded, index: index,
			loc: location{gitlink: c.gitlink}}
		s.change = Change{Submodule: branchSub(c.worktree), Old: c.recorded.entry}
		s.setTarget(target)
		if !s.resolved || s.change.New != target || s.change.staleLock != c.staleLock ||
			s.change.staleIndex != c.staleIndex || s.change.keys != c.keys {
			t.Errorf("%s: resolved %v, New %+v, staleLock %v, staleIndex %v, keys %d; "+
				"want %v, %v, %d", c.name, s.resolved, s.change.New, s.change.staleLock,
				s.change.staleIndex, s.change.keys, c.staleLock, c.staleIndex, c.keys)
		}
	}
}
