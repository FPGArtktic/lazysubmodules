// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package gittest

import (
	"os"
	"path/filepath"
	"testing"
)

// Names of the files in HostileSuper.Outside that the symbolic links of
// LinkLock and LinkGitmodules point to.
const (
	OutsideLock       = "lsm.lock"
	OutsideGitmodules = "gitmodules"
)

// HostileSuper is the superproject of NewHostileSuper. Its submodules are
// clones of one NewTaggedUpstream.
type HostileSuper struct {
	*Super
	// Outside is a directory next to Dir that the hostile entries point to;
	// nothing may change below it. It holds the repositories "lib" (a
	// clone with a working tree) and "modules/dotdot" (a bare clone), and
	// copies of the lock file (OutsideLock) and .gitmodules
	// (OutsideGitmodules) as committed.
	Outside string

	// Good ("good"): a well-formed submodule, tag v1.0.0, locked and
	// checked out.
	Good *Submodule
	// DotDot (named "../../../outside/modules/dotdot", at "dotdot"): a name
	// that makes the repository directory .git/modules/<name> the bare
	// repository Outside/modules/dotdot. Tag v1.0.0, gitlink and lock
	// entry, never cloned.
	DotDot *Submodule
	// Escape ("escape"): path "../outside/lib", the repository Outside/lib.
	// Tag v1.0.0 with a lock entry, no gitlink.
	Escape *Submodule
	// Absolute ("absolute"): the absolute path of Outside/lib. Branch
	// "main" with the native branch key, no gitlink.
	Absolute *Submodule
	// ViaLink ("via-link"): path "linked/lib", where "linked" is a
	// committed symbolic link to "../outside", so the path names
	// Outside/lib. Tag v1.0.0, no gitlink.
	ViaLink *Submodule
	// Self ("self"): path ".", the superproject itself, which has a tag
	// v1.0.0 of its own. No gitlink.
	Self *Submodule
	// DotGit ("dotgit"): path ".git/modules/good", the repository of Good.
	// Tag v1.0.0, no gitlink.
	DotGit *Submodule
	// DashRef ("dash-ref"), DashBranch ("dash-branch"), DashPattern
	// ("dash-pattern") and DashCommit ("dash-commit"): refs in tag, branch
	// (also as the native branch key), tag-pattern and commit mode that git
	// would take for the option "--output=<Outside>/pwned-<name>". All are
	// checked out at v1.0.0; DashRef has a lock entry with its ref.
	DashRef, DashBranch, DashPattern, DashCommit *Submodule
	// DashURL ("dash-url"): URL "--upload-pack=touch <Outside>/pwned-dash-url",
	// tag v1.0.0, gitlink, never cloned.
	DashURL *Submodule
	// CtrlRef ("ctrl-ref"): tag ref "v1.0.0" followed by a terminal title
	// escape sequence (ESC ... BEL), also in its lock entry. Checked out.
	CtrlRef *Submodule
	// NewlineRef ("newline-ref"): tag ref "v1.0.0\nv1.0.1". Checked out.
	NewlineRef *Submodule
	// Quoted (`we"ird\name` at "weird"): a name with a double quote and a
	// backslash. Tag v1.0.0, checked out, no lock entry.
	Quoted *Submodule
}

// NewHostileSuper creates a superproject whose .gitmodules and lock file
// hold hostile values; the fields describe them. Git itself ignores the
// suspicious name of DotDot and the URL of DashURL, and it cannot record a
// gitlink for the paths outside the working tree. Unless a field says
// otherwise, submodules are checked out at their gitlinks.
//
// The superproject is Dir = <root>/super and Outside is <root>/outside, so
// that the relative paths above reach it. LinkLock and LinkGitmodules turn
// the two files into symbolic links. Building the fixture takes about twice
// as long as NewTaggedUpstream.
//
// Context: test helper; format is SHA1 or SHA256.
// Return: the superproject.
func NewHostileSuper(t testing.TB, format string) *HostileSuper {
	t.Helper()
	b := newBuilder(t, format)
	ts := b.newTaggedSubs()
	root := t.TempDir()
	h := &HostileSuper{
		Super:   &Super{Dir: filepath.Join(root, "super")},
		Outside: filepath.Join(root, "outside"),
	}
	b.git(root, "clone", "--quiet", ts.up.Bare, filepath.Join(h.Outside, "lib"))
	b.git(root, "clone", "--quiet", "--bare", ts.up.Bare,
		filepath.Join(h.Outside, "modules", "dotdot"))

	b.initRepo(h.Dir)
	WriteFile(t, filepath.Join(h.Dir, "README"), "hostile superproject\n")
	if err := os.Symlink(filepath.Join("..", "outside"), filepath.Join(h.Dir, "linked")); err != nil {
		t.Fatalf("gittest: %v", err)
	}
	b.git(h.Dir, "add", "--all")
	b.commit(h.Dir, "initial commit")
	b.git(h.Dir, "tag", TagV100)

	h.describe(ts)
	subs := h.Submodules()
	b.commitSubmodules(h.Dir, subs, "add submodules")
	b.clone(h.Dir, subs)
	finish(h.Dir, b.git(h.Dir, "rev-parse", "--absolute-git-dir"), subs)
	for _, file := range [][2]string{{LockFile, OutsideLock}, {GitmodulesFile, OutsideGitmodules}} {
		data, err := os.ReadFile(filepath.Join(h.Dir, file[0]))
		if err != nil {
			t.Fatalf("gittest: %v", err)
		}
		WriteFile(t, filepath.Join(h.Outside, file[1]), string(data))
	}
	return h
}

// describe fills in the submodule fields.
func (h *HostileSuper) describe(ts *taggedSubs) {
	v100 := ts.v100
	pwned := func(name string) string {
		return "--output=" + filepath.Join(h.Outside, "pwned-"+name)
	}
	lib := filepath.Join(h.Outside, "lib")
	h.Good = ts.sub("good", "good", ModeTag, TagV100, TagV100, v100)
	h.DotDot = ts.sub("../../../outside/modules/dotdot", "dotdot", ModeTag, TagV100, TagV100,
		v100)
	h.Escape = ts.sub("escape", "../outside/lib", ModeTag, TagV100, TagV100, "")
	h.Absolute = ts.sub("absolute", filepath.ToSlash(lib), ModeBranch, "main", "", "")
	h.ViaLink = ts.sub("via-link", "linked/lib", ModeTag, TagV100, "", "")
	h.Self = ts.sub("self", ".", ModeTag, TagV100, "", "")
	h.DotGit = ts.sub("dotgit", ".git/modules/good", ModeTag, TagV100, "", "")
	h.DashRef = ts.sub("dash-ref", "dash-ref", ModeTag, pwned("dash-ref"), pwned("dash-ref"),
		v100)
	h.DashBranch = ts.sub("dash-branch", "dash-branch", ModeBranch, pwned("dash-branch"), "",
		v100)
	h.DashPattern = ts.sub("dash-pattern", "dash-pattern", ModeTagPattern,
		pwned("dash-pattern"), "", v100)
	h.DashCommit = ts.sub("dash-commit", "dash-commit", ModeCommit, pwned("dash-commit"), "",
		v100)
	h.DashURL = ts.sub("dash-url", "dash-url", ModeTag, TagV100, "", v100)
	h.DashURL.URL = "--upload-pack=touch " + filepath.Join(h.Outside, "pwned-dash-url")
	ctrl := TagV100 + "\x1b]0;pwned\a"
	h.CtrlRef = ts.sub("ctrl-ref", "ctrl-ref", ModeTag, ctrl, ctrl, v100)
	h.NewlineRef = ts.sub("newline-ref", "newline-ref", ModeTag, TagV100+"\n"+TagV101, "",
		v100)
	h.Quoted = ts.sub(`we"ird\name`, "weird", ModeTag, TagV100, "", v100)

	// An empty lock ref above means no lock entry, an empty commit no
	// gitlink; lock entries always record v1.0.0.
	for _, s := range h.Submodules() {
		if s.Lock != nil && s.Lock.Ref == "" {
			s.Lock = nil
		}
		if s.Lock != nil {
			s.Lock.Commit = v100
		}
		if s.Gitlink == "" {
			s.state = absent
		}
	}
	h.DotDot.state, h.DashURL.state = absent, absent
}

// Submodules returns the submodules of the superproject in .gitmodules
// order.
//
// Context: any.
// Return: the submodule fields in the order they are declared.
func (h *HostileSuper) Submodules() []*Submodule {
	return []*Submodule{
		h.Good, h.DotDot, h.Escape, h.Absolute, h.ViaLink, h.Self, h.DotGit,
		h.DashRef, h.DashBranch, h.DashPattern, h.DashCommit, h.DashURL,
		h.CtrlRef, h.NewlineRef, h.Quoted,
	}
}

// LinkLock replaces the lock file by a symbolic link to OutsideLock in
// Outside, which has the same content, and commits the link, as a cloned
// superproject may contain it.
//
// Context: test helper.
// Return: nothing; fails the test on error.
func (h *HostileSuper) LinkLock(t testing.TB) {
	t.Helper()
	h.link(t, LockFile, OutsideLock)
	Git(t, h.Dir, "add", LockFile)
	Git(t, h.Dir, "commit", "--quiet", "--message=link the lock file")
}

// LinkGitmodules replaces .gitmodules in the working tree by a symbolic
// link to OutsideGitmodules in Outside, which has the same content. The
// change is not staged: the committed .gitmodules stays a regular file.
//
// Context: test helper.
// Return: nothing; fails the test on error.
func (h *HostileSuper) LinkGitmodules(t testing.TB) {
	t.Helper()
	h.link(t, GitmodulesFile, OutsideGitmodules)
}

// link replaces the file name in Dir by a relative symbolic link to the
// file target in Outside.
func (h *HostileSuper) link(t testing.TB, name, target string) {
	t.Helper()
	p := filepath.Join(h.Dir, name)
	if err := os.Remove(p); err != nil {
		t.Fatalf("gittest: %v", err)
	}
	if err := os.Symlink(filepath.Join("..", "outside", target), p); err != nil {
		t.Fatalf("gittest: %v", err)
	}
}
