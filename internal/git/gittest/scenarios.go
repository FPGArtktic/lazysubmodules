// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package gittest

import (
	"fmt"
	"path/filepath"
	"strconv"
	"testing"
)

// taggedSubs holds a tagged upstream shared by the submodules of a
// fixture.
type taggedSubs struct {
	up                *Upstream
	tags, branches    map[string]string
	v100, v101, v200r string
}

// newTaggedSubs creates a NewTaggedUpstream and reads its refs.
func (b *builder) newTaggedSubs() *taggedSubs {
	b.t.Helper()
	up, commits := NewTaggedUpstream(b.t, b.format)
	ts := &taggedSubs{up: up, v100: commits[TagV100],
		v101: commits[TagV101], v200r: commits[TagV200RC]}
	ts.tags, ts.branches = b.remoteRefs(up)
	return ts
}

// sub describes a submodule of the tagged upstream with a gitlink at commit,
// managed and locked at commit unless mode is empty.
func (ts *taggedSubs) sub(name, p, mode, ref, lockRef, commit string) *Submodule {
	s := &Submodule{Name: name, Path: p, URL: ts.up.Bare, Gitlink: commit, Upstream: ts.up,
		Tags: ts.tags, Branches: ts.branches}
	if mode != "" {
		s.Mode, s.Ref = mode, ref
		s.Lock = &LockEntry{Mode: mode, Ref: lockRef, Commit: commit}
	}
	if mode == ModeBranch {
		s.Branch = ref
	}
	return s
}

// commitSubmodules writes .gitmodules and the lock file for subs, records
// their gitlinks in the index of dir and commits.
func (b *builder) commitSubmodules(dir string, subs []*Submodule, msg string) {
	b.t.Helper()
	b.writeGitmodules(dir, subs, true)
	b.writeLock(dir, subs)
	b.setGitlinks(dir, subs)
	b.git(dir, "add", "--force", GitmodulesFile, LockFile)
	b.commit(dir, msg)
}

// NestedSuper is the superproject of NewNestedSuper.
type NestedSuper struct {
	// Super is the superproject; Dir is below Outer.
	*Super
	// Outer is the top level of the repository that has the superproject
	// as its submodule "super" at SuperPath.
	Outer string
	// SuperPath is the path of the superproject in Outer.
	SuperPath string
	// SuperUpstream is the repository that Outer records for the
	// superproject; its main branch is the commit Outer records.
	SuperUpstream *Upstream
	// GitDir is the absolute git directory of the superproject, inside the
	// git directory of Outer.
	GitDir string
	// Lib ("lib"): tag-pattern "v1.*", locked at v1.0.0 and checked out.
	Lib *Submodule
	// Pinned ("pinned" at "libs/pinned"): tag "v1.0.1", locked and checked
	// out.
	Pinned *Submodule
	// Later ("later" at "libs/later"): branch "stable" with the native
	// branch key, locked at stable, never cloned.
	Later *Submodule
}

// NewNestedSuper creates a superproject that is itself a checked-out
// submodule of an outer repository. Its submodules are clones of one
// NewTaggedUpstream; the fields describe them. Their repositories live in
// the git directory of the superproject, which is inside the one of Outer.
// Building it takes about twice as long as NewTaggedUpstream.
//
// Context: test helper; format is SHA1 or SHA256.
// Return: the superproject.
func NewNestedSuper(t testing.TB, format string) *NestedSuper {
	t.Helper()
	b := newBuilder(t, format)
	ts := b.newTaggedSubs()
	n := &NestedSuper{
		SuperPath:     "components/super",
		SuperUpstream: NewUpstream(t, format),
		Lib:           ts.sub("lib", "lib", ModeTagPattern, "v1.*", TagV100, ts.v100),
		Pinned:        ts.sub("pinned", "libs/pinned", ModeTag, TagV101, TagV101, ts.v101),
		Later: ts.sub("later", "libs/later", ModeBranch, BranchStable, BranchStable,
			ts.v101),
	}
	n.Later.state = absent
	subs := n.Submodules()
	work := n.SuperUpstream.Work
	b.commitSubmodules(work, subs, "add submodules")
	b.publish(n.SuperUpstream)

	n.Outer = InitRepo(t, format)
	WriteFile(t, filepath.Join(n.Outer, "README"), "outer repository\n")
	b.git(n.Outer, "add", "README")
	b.commit(n.Outer, "initial commit")
	b.git(n.Outer, "submodule", "add", "--quiet", "--name", "super", "--",
		n.SuperUpstream.Bare, n.SuperPath)
	b.commit(n.Outer, "add superproject")

	n.Super = &Super{Dir: filepath.Join(n.Outer, filepath.FromSlash(n.SuperPath))}
	b.clone(n.Dir, subs)
	n.GitDir = b.git(n.Dir, "rev-parse", "--absolute-git-dir")
	finish(n.Dir, n.GitDir, subs)
	return n
}

// Submodules returns the submodules of the superproject in .gitmodules
// order.
//
// Context: any.
// Return: Lib, Pinned and Later.
func (n *NestedSuper) Submodules() []*Submodule {
	return []*Submodule{n.Lib, n.Pinned, n.Later}
}

// UnbornSuper is the superproject of NewUnbornSuper.
type UnbornSuper struct {
	*Super
	// Lib ("lib"): tag-pattern "v1.*" without a lock entry. Its gitlink
	// (in the index) and checkout are the tip of main, TagV200RC; as "git
	// submodule add" leaves it, HEAD is on branch main.
	Lib *Submodule
}

// NewUnbornSuper creates a superproject without any commit: the
// submodule Lib, a clone of a NewTaggedUpstream, has been added with "git
// submodule add" and its tracking keys written, and .gitmodules and the
// gitlink are staged. There is no lock file. Building it takes a little
// longer than NewTaggedUpstream.
//
// Context: test helper; format is SHA1 or SHA256.
// Return: the superproject.
func NewUnbornSuper(t testing.TB, format string) *UnbornSuper {
	t.Helper()
	b := newBuilder(t, format)
	ts := b.newTaggedSubs()
	u := &UnbornSuper{
		Super: &Super{Dir: InitRepo(t, format)},
		Lib:   ts.sub("lib", "lib", ModeTagPattern, "v1.*", "", ts.v200r),
	}
	u.Lib.Lock = nil
	b.git(u.Dir, "submodule", "add", "--quiet", "--name", u.Lib.Name, "--", u.Lib.URL,
		u.Lib.Path)
	b.git(u.Dir, "config", "-f", GitmodulesFile, "submodule.lib.lsm-mode", u.Lib.Mode)
	b.git(u.Dir, "config", "-f", GitmodulesFile, "submodule.lib.lsm-ref", u.Lib.Ref)
	b.git(u.Dir, "add", GitmodulesFile)
	finish(u.Dir, b.git(u.Dir, "rev-parse", "--absolute-git-dir"), []*Submodule{u.Lib})
	return u
}

// ManySuper is the superproject of NewManySuper.
type ManySuper struct {
	*Super
	subs []*Submodule
}

// Submodules returns the submodules of the superproject in .gitmodules
// order.
//
// Context: any; the slice is shared and must not be modified.
// Return: the n submodules.
func (m *ManySuper) Submodules() []*Submodule {
	return m.subs
}

// NewManySuper creates a SHA-1 superproject with n small submodules, all
// clones of one NewTaggedUpstream, in one commit with the lock file.
//
// Submodule i (counting from 0 in .gitmodules order) is named
// "sub-<n-1-i>", zero-padded to a common width, so the file order is the
// reverse of the sorted order, and lives at "mods/<name>". Its settings
// depend on i%5: 0 branch BranchStable, 1 tag TagV100, 2 tag-pattern "v1.*"
// locked at TagV100 while TagV101 exists, 3 commit mode with the full SHA
// of TagV101, 4 unmanaged at the tip of main. Every submodule is checked out
// at its gitlink, which is the locked commit. The clones run eight at a time;
// n = 100 takes several seconds (8.5 s on a busy four-core host).
//
// Context: test helper; n must be positive.
// Return: the superproject.
func NewManySuper(t testing.TB, n int) *ManySuper {
	t.Helper()
	if n < 1 {
		t.Fatalf("gittest: NewManySuper(%d): n must be positive", n)
	}
	b := newBuilder(t, SHA1)
	ts := b.newTaggedSubs()
	m := &ManySuper{Super: NewSuper(t, SHA1), subs: make([]*Submodule, n)}
	width := len(strconv.Itoa(n - 1))
	for i := range m.subs {
		name := fmt.Sprintf("sub-%0*d", width, n-1-i)
		p := "mods/" + name
		var s *Submodule
		switch i % 5 {
		case 0:
			s = ts.sub(name, p, ModeBranch, BranchStable, BranchStable, ts.v101)
		case 1:
			s = ts.sub(name, p, ModeTag, TagV100, TagV100, ts.v100)
		case 2:
			s = ts.sub(name, p, ModeTagPattern, "v1.*", TagV100, ts.v100)
		case 3:
			s = ts.sub(name, p, ModeCommit, ts.v101, ts.v101, ts.v101)
		default:
			s = ts.sub(name, p, "", "", "", ts.v200r)
		}
		m.subs[i] = s
	}
	b.commitSubmodules(m.Dir, m.subs, "add "+strconv.Itoa(n)+" submodules")
	b.clone(m.Dir, m.subs)
	finish(m.Dir, b.git(m.Dir, "rev-parse", "--absolute-git-dir"), m.subs)
	return m
}
