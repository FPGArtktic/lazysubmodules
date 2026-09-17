// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package gittest

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// TrackedFile is the file changed by every Upstream commit; it therefore
// exists in every checkout of an upstream.
const TrackedFile = "file.txt"

// Tags created by NewTaggedUpstream.
const (
	TagRC1    = "v1.0.0-rc.1" // lightweight pre-release
	TagV100   = "v1.0.0"      // annotated
	TagV101   = "v1.0.1"      // lightweight
	TagV200RC = "v2.0.0-rc.1" // annotated pre-release
	// BranchStable points to the commit of TagV101.
	BranchStable = "stable"
)

// Upstream is a remote repository for submodules.
type Upstream struct {
	// Bare is the bare repository used as the remote URL.
	Bare string
	// Work is a clone of Bare, on branch main, used to author history.
	Work string

	commits int
}

// NewUpstream creates a bare repository with one commit on branch main.
//
// Context: test helper; format is SHA1 or SHA256.
// Return: the upstream.
func NewUpstream(t testing.TB, format string) *Upstream {
	t.Helper()
	root := t.TempDir()
	u := &Upstream{
		Bare: filepath.Join(root, "upstream.git"),
		Work: filepath.Join(root, "work"),
	}
	Git(t, root, "init", "--quiet", "--bare", "--object-format="+format, u.Bare)
	disableAutoMaintenance(t, u.Bare)
	Git(t, root, "init", "--quiet", "--object-format="+format, u.Work)
	Git(t, u.Work, "remote", "add", "origin", u.Bare)
	u.Commit(t, "initial commit")
	return u
}

// disableAutoMaintenance turns off automatic gc and maintenance in the
// repository at dir.
//
// The receiving side of a push to a local repository does not see the
// configuration that Env passes through the environment. Without this, every
// push starts a detached "git gc --auto" or "git maintenance run --auto" in
// the bare repository. Such a process outlives its parent, so in a container
// whose init process does not reap orphans each push leaves a zombie behind
// until the process limit is exhausted.
//
// Context: test helper; dir must be a git repository.
func disableAutoMaintenance(t testing.TB, dir string) {
	t.Helper()
	Git(t, dir, "config", "receive.autogc", "false")
	Git(t, dir, "config", "gc.auto", "0")
	Git(t, dir, "config", "maintenance.auto", "false")
}

// NewTaggedUpstream creates an upstream with release history.
//
// History on main, oldest first: the initial commit; TagRC1; TagV100;
// TagV101 (also BranchStable); TagV200RC (the tip of main).
//
// Context: test helper; format is SHA1 or SHA256.
// Return: the upstream and the commit SHA of every tag.
func NewTaggedUpstream(t testing.TB, format string) (*Upstream, map[string]string) {
	t.Helper()
	u := NewUpstream(t, format)
	commits := make(map[string]string, 4)

	commits[TagRC1] = u.Commit(t, "add feature one")
	u.Tag(t, TagRC1, commits[TagRC1])
	commits[TagV100] = u.Commit(t, "release one")
	u.AnnotatedTag(t, TagV100, commits[TagV100], "release "+TagV100)
	commits[TagV101] = u.Commit(t, "fix feature one")
	u.Tag(t, TagV101, commits[TagV101])
	u.Branch(t, BranchStable, commits[TagV101])
	commits[TagV200RC] = u.Commit(t, "add feature two")
	u.AnnotatedTag(t, TagV200RC, commits[TagV200RC], "pre-release "+TagV200RC)
	return u, commits
}

// Commit creates a commit on main with unique content and pushes it.
//
// Context: test helper.
// Return: the commit SHA.
func (u *Upstream) Commit(t testing.TB, msg string) string {
	t.Helper()
	u.writeNext(t, msg)
	sha := commitAll(t, u.Work, msg)
	Git(t, u.Work, "push", "--quiet", "origin", "HEAD:refs/heads/main")
	return sha
}

// writeNext writes the unique content of the next commit to TrackedFile in
// Work.
func (u *Upstream) writeNext(t testing.TB, msg string) {
	t.Helper()
	u.commits++
	WriteFile(t, filepath.Join(u.Work, TrackedFile),
		"commit "+strconv.Itoa(u.commits)+": "+msg+"\n")
}

// Branch creates or moves a branch (not main) to rev and force-pushes it.
//
// Context: test helper.
// Return: nothing; fails the test on error.
func (u *Upstream) Branch(t testing.TB, name, rev string) {
	t.Helper()
	Git(t, u.Work, "branch", "--force", name, rev)
	u.push(t, "refs/heads/"+name)
}

// Tag creates a lightweight tag and pushes it.
//
// Context: test helper.
// Return: nothing; fails the test on error.
func (u *Upstream) Tag(t testing.TB, name, rev string) {
	t.Helper()
	Git(t, u.Work, "tag", name, rev)
	u.push(t, "refs/tags/"+name)
}

// AnnotatedTag creates an annotated tag and pushes it.
//
// Context: test helper.
// Return: nothing; fails the test on error.
func (u *Upstream) AnnotatedTag(t testing.TB, name, rev, msg string) {
	t.Helper()
	Git(t, u.Work, "tag", "--annotate", "--message="+msg, name, rev)
	u.push(t, "refs/tags/"+name)
}

// SignedTag creates a tag signed with an SSHSigningKey and pushes it.
//
// Context: test helper.
// Return: nothing; fails the test on error.
func (u *Upstream) SignedTag(t testing.TB, name, rev, msg, keyPath string) {
	t.Helper()
	args := append(ConfigArgs(SSHSigningConfig(keyPath)...),
		"tag", "--sign", "--message="+msg, name, rev)
	Git(t, u.Work, args...)
	u.push(t, "refs/tags/"+name)
}

// MoveTag force-moves an existing tag to rev and force-pushes it, as a
// maintainer rewriting a release would. An annotated tag stays annotated.
//
// Context: test helper.
// Return: nothing; fails the test on error.
func (u *Upstream) MoveTag(t testing.TB, name, rev string) {
	t.Helper()
	args := []string{"tag", "--force"}
	if Git(t, u.Work, "cat-file", "-t", "refs/tags/"+name) == "tag" {
		args = append(args, "--annotate", "--message=moved "+name)
	}
	Git(t, u.Work, append(args, name, rev)...)
	u.push(t, "refs/tags/"+name)
}

// AddSubmodule adds inner as a submodule of the upstream, commits and pushes,
// so that the upstream becomes a repository with a nested submodule.
//
// Context: test helper; inner must use the same object format.
// Return: the new commit SHA.
func (u *Upstream) AddSubmodule(t testing.TB, name string, inner *Upstream) string {
	t.Helper()
	Git(t, u.Work, "submodule", "add", "--name", name, "--", inner.Bare, name)
	sha := commitAll(t, u.Work, "add submodule "+name)
	Git(t, u.Work, "push", "--quiet", "origin", "HEAD:refs/heads/main")
	return sha
}

// push force-pushes one ref of the work repository to the bare repository.
func (u *Upstream) push(t testing.TB, ref string) {
	t.Helper()
	Git(t, u.Work, "push", "--quiet", "--force", "origin", ref+":"+ref)
}

// Super is a superproject.
type Super struct {
	// Dir is the top level of the working tree.
	Dir string
}

// NewSuper creates a superproject with one commit on branch main.
//
// Context: test helper; format is SHA1 or SHA256.
// Return: the superproject.
func NewSuper(t testing.TB, format string) *Super {
	t.Helper()
	s := &Super{Dir: InitRepo(t, format)}
	WriteFile(t, filepath.Join(s.Dir, "README"), "superproject\n")
	commitAll(t, s.Dir, "initial commit")
	return s
}

// AddSubmodule clones up as submodule name at path name and commits it.
//
// Context: test helper; up must use the same object format.
// Return: the submodule path relative to Dir.
func (s *Super) AddSubmodule(t testing.TB, name string, up *Upstream) string {
	t.Helper()
	Git(t, s.Dir, "submodule", "add", "--name", name, "--", up.Bare, name)
	commitAll(t, s.Dir, "add submodule "+name)
	return name
}

// SetKey sets submodule.<name>.<key> in .gitmodules without committing.
//
// Context: test helper.
// Return: nothing; fails the test on error.
func (s *Super) SetKey(t testing.TB, name, key, value string) {
	t.Helper()
	Git(t, s.Dir, "config", "-f", ".gitmodules", "submodule."+name+"."+key, value)
}

// Commit stages all changes and commits them.
//
// Context: test helper; there must be something to commit.
// Return: nothing; fails the test on error.
func (s *Super) Commit(t testing.TB, msg string) {
	t.Helper()
	commitAll(t, s.Dir, msg)
}

// Deinit unregisters a submodule and empties its working tree, keeping its
// repository in the git directory of the superproject.
//
// Context: test helper.
// Return: nothing; fails the test on error.
func (s *Super) Deinit(t testing.TB, path string) {
	t.Helper()
	Git(t, s.Dir, "submodule", "deinit", "--force", "--", path)
}

// MakeDirty modifies TrackedFile in a populated submodule without committing.
//
// Context: test helper.
// Return: nothing; fails the test on error.
func (s *Super) MakeDirty(t testing.TB, path string) {
	t.Helper()
	makeDirty(t, filepath.Join(s.Dir, path))
}

// makeDirty modifies TrackedFile in the working tree dir.
func makeDirty(t testing.TB, dir string) {
	t.Helper()
	file := filepath.Join(dir, TrackedFile)
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("gittest: %v", err)
	}
	WriteFile(t, file, string(data)+"uncommitted change\n")
}
