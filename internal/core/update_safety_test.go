// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package core_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FPGArtktic/lazysubmodules/internal/core"
	"github.com/FPGArtktic/lazysubmodules/internal/git"
	"github.com/FPGArtktic/lazysubmodules/internal/git/gittest"
	"github.com/FPGArtktic/lazysubmodules/internal/lock"
	"github.com/FPGArtktic/lazysubmodules/internal/manifest"
)

// readFile returns the content of a file, or "<missing>".
func readFile(t *testing.T, p string) string {
	t.Helper()
	data, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return "<missing>"
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// hasObject reports whether the repository in gitDir has an object.
func hasObject(t *testing.T, g *git.Runner, gitDir, object string) bool {
	t.Helper()
	// The explicit working tree keeps git from entering a removed one.
	_, err := g.Run(t.Context(), t.TempDir(), "--git-dir="+gitDir, "--work-tree="+gitDir,
		"cat-file", "-e", object)
	return err == nil
}

// newHostileFixture returns a fixture whose .gitmodules has two managed
// entries without a gitlink: "docs", a directory of the superproject with
// an ignored secret, and "evil", a repository nested in the unmanaged
// submodule "outer". The superproject has an origin remote whose main
// branch is behind HEAD and whose tag v1 differs from the local one.
func newHostileFixture(t *testing.T) *fixture {
	t.Helper()
	f := newFixture(t, gittest.SHA1)
	s := f.super
	gittest.WriteFile(t, filepath.Join(s.Dir, "docs", "a.txt"), "docs\n")
	gittest.WriteFile(t, filepath.Join(s.Dir, ".gitignore"), "*.env\n")
	s.Commit(t, "add docs")
	bare := filepath.Join(t.TempDir(), "super.git")
	gittest.Git(t, s.Dir, "clone", "--quiet", "--bare", s.Dir, bare)
	gittest.Git(t, s.Dir, "remote", "add", "origin", bare)
	gittest.Git(t, s.Dir, "fetch", "--quiet", "origin")
	gittest.Git(t, bare, "tag", "v1", "main~1")
	gittest.Git(t, s.Dir, "tag", "v1", "HEAD")

	outer := gittest.NewUpstream(t, gittest.SHA1)
	outer.AddSubmodule(t, "inner", f.up)
	s.AddSubmodule(t, "outer", outer)
	gittest.Git(t, f.dir("outer"), "submodule", "update", "--init", "--quiet")
	// A slice, not a map: the entries must appear in .gitmodules in this
	// order, which the expected error and foreach output depend on.
	for _, e := range []struct{ name, path string }{
		{"docs", "docs"}, {"evil", "outer/inner"},
	} {
		s.SetKey(t, e.name, manifest.KeyPath, e.path)
		s.SetKey(t, e.name, manifest.KeyURL, f.up.Bare)
	}
	f.configure("docs", manifest.ModeBranch, "main")
	f.configure("evil", manifest.ModeTag, gittest.TagV100)
	s.Commit(t, "hostile entries")
	gittest.WriteFile(t, filepath.Join(s.Dir, "docs", "secret.env"), "TOKEN=secret\n")
	return f
}

func TestUpdateRefusesPathWithoutGitlink(t *testing.T) {
	t.Parallel()
	f := newHostileFixture(t)
	head := headOf(t, f.super.Dir)
	before := treeState(t, f.super.Dir)
	const refusal = "refused: the index records no submodule at its path"
	for _, opts := range []core.UpdateOptions{
		{}, {Fetch: true}, {Fetch: true, Commit: true}, {DryRun: true, Fetch: true},
	} {
		res, err := f.update(opts)
		what := fmt.Sprintf("Update(%+v)", opts)
		wantErr(t, what, err, core.ErrNotSubmodule, core.ErrRefused)
		if want := "docs: " + refusal + "\nevil: " + refusal; err == nil ||
			err.Error() != want || len(res.Changes) != 0 {
			t.Errorf("%s = %+v, %v\nwant %s", what, res, err, want)
		}
	}
	for _, names := range [][]string{nil, {"docs"}} {
		results, err := f.repo().Fetch(t.Context(), names, nil)
		wantErr(t, fmt.Sprintf("Fetch(%q)", names), err, core.ErrNotSubmodule)
		if results != nil {
			t.Errorf("Fetch(%q) = %+v", names, results)
		}
	}
	// Nothing ran in the superproject or in the nested repository: no
	// fetch, no checkout, no lock file, nothing staged or committed.
	wantSameTree(t, "refused update", before, treeState(t, f.super.Dir))
	if got := gittest.Git(t, f.super.Dir, "symbolic-ref", "HEAD"); got != "refs/heads/main" ||
		headOf(t, f.super.Dir) != head {
		t.Errorf("superproject HEAD moved to %s (%s)", headOf(t, f.super.Dir), got)
	}

	// Read-only commands treat the entries as not checked out.
	r := f.repo()
	st, err := r.Status(t.Context(), []string{"docs", "evil"})
	if err != nil || len(st) != 2 {
		t.Fatalf("Status = %+v, %v", st, err)
	}
	for _, s := range st {
		wantState(t, s, core.StateUninitialized, "the index records no submodule at the path")
		if s.Head != "" || s.Target != nil {
			t.Errorf("%s: head %q, target %+v", s.Submodule.Name, s.Head, s.Target)
		}
		_, err := r.Resolve(t.Context(), s.Submodule, nil, core.ResolveOptions{})
		wantErr(t, "Resolve("+s.Submodule.Name+")", err, core.ErrNotSubmodule)
	}
	vr, err := r.Verify(t.Context(), []string{"evil"}, core.VerifyOptions{})
	wantErr(t, "Verify(evil)", err, core.ErrVerify)
	if len(vr) != 1 {
		t.Fatalf("Verify(evil) = %+v, %v", vr, err)
	}
	wantChecks(t, vr[0], []string{core.CheckLockEntry, core.CheckInitialized},
		[]string{core.CheckLockEntry, core.CheckInitialized}, "")
	if detail := checkDetail(t, vr[0], core.CheckInitialized); !strings.Contains(detail,
		"records no submodule") {
		t.Errorf("initialized detail %q", detail)
	}
	stdout, stderr, err := foreach(t, f, f.super.Dir, "sh", "-c", `echo "$name"`)
	wantNotes := "skipping docs: the index records no submodule at the path\n" +
		"skipping evil: the index records no submodule at the path\n"
	if err != nil || stdout != "" || stderr != wantNotes {
		t.Errorf("Foreach = %v\nstdout %q\nstderr %q", err, stdout, stderr)
	}
	wantSameTree(t, "read-only commands", before, treeState(t, f.super.Dir))
}

func TestUpdateStagedUpdate(t *testing.T) {
	t.Parallel()
	setup := func(t *testing.T) (*fixture, string, string) {
		t.Helper()
		f := newFixture(t, gittest.SHA1)
		v100, v101 := f.commits[gittest.TagV100], f.commits[gittest.TagV101]
		f.track("lib", manifest.ModeTagPattern, "v1.*", gittest.TagV100, v100)
		return f, v100, v101
	}
	// committed is the lock entry that HEAD records.
	committed := func(f *fixture) lock.Entry {
		return lock.Entry{Name: "lib", Mode: manifest.ModeTagPattern, Ref: gittest.TagV100,
			Commit: f.commits[gittest.TagV100]}
	}

	t.Run("stage then commit", func(t *testing.T) {
		t.Parallel()
		f, v100, v101 := setup(t)
		old := headOf(t, f.super.Dir)
		if c := oneChange(t, f.mustUpdate(core.UpdateOptions{})); c.OldGitlink != v100 ||
			c.Old == nil || *c.Old != committed(f) {
			t.Errorf("staging update: %+v", c)
		}
		wantStaged(t, f.super.Dir, lock.File, "lib")
		// The commit replaces HEAD, so the old values are those of HEAD.
		res := f.mustUpdate(core.UpdateOptions{Commit: true})
		c := oneChange(t, res)
		if !c.Changed() || c.OldGitlink != v100 || c.OldHead != v101 || c.Old == nil ||
			*c.Old != committed(f) || res.Commit == "" || res.Commit != headOf(t, f.super.Dir) {
			t.Fatalf("Update(commit) = %+v", res)
		}
		if parent := gittest.Git(t, f.super.Dir, "rev-parse", "HEAD^"); parent != old {
			t.Errorf("parent %s, want %s", parent, old)
		}
		want := "manifest: update lib to v1.0.1\n\n" +
			"Tracking mode: tag-pattern v1.*\n" +
			"Old: " + v100[:12] + " (v1.0.0)\n" +
			"New: " + v101[:12] + " (v1.0.1)\n\n" + signOff
		if msg := headMessage(t, f.super.Dir); msg != want {
			t.Errorf("commit message:\n%s\nwant\n%s", msg, want)
		}
		if got := gitlinkAt(t, f.super.Dir, "HEAD", "lib"); got != v101 {
			t.Errorf("committed gitlink %s, want %s", got, v101)
		}
		wantStaged(t, f.super.Dir)
		if vr, err := f.repo().Verify(t.Context(), nil, core.VerifyOptions{}); err != nil {
			t.Errorf("Verify = %+v, %v", vr, err)
		}
	})

	t.Run("unstaged update", func(t *testing.T) {
		t.Parallel()
		f, v100, v101 := setup(t)
		f.mustUpdate(core.UpdateOptions{})
		gittest.Git(t, f.super.Dir, "reset", "--quiet")
		wantStaged(t, f.super.Dir)
		c := oneChange(t, f.mustUpdate(core.UpdateOptions{}))
		if !c.Changed() || c.OldGitlink != v100 || c.OldHead != v101 || c.Old == nil ||
			*c.Old != committed(f) {
			t.Errorf("update after reset: %+v", c)
		}
		wantStaged(t, f.super.Dir, lock.File, "lib")
		indexMatchesWorktree(t, f.super.Dir, lock.File)
		if got := gitlinkAt(t, f.super.Dir, "", "lib"); got != v101 {
			t.Errorf("staged gitlink %s, want %s", got, v101)
		}
		if c := oneChange(t, f.mustUpdate(core.UpdateOptions{})); c.Changed() {
			t.Errorf("update after staging again: %+v", c)
		}
	})

	t.Run("failed staging", func(t *testing.T) {
		t.Parallel()
		f, v100, v101 := setup(t)
		lockFile := filepath.Join(f.super.Dir, lock.File)
		lockBefore := readFile(t, lockFile)
		indexLock := filepath.Join(f.super.Dir, ".git", "index.lock")
		gittest.WriteFile(t, indexLock, "")
		_, err := f.update(core.UpdateOptions{Commit: true})
		if _, ok := errors.AsType[*git.Error](err); !ok || errors.Is(err, core.ErrRefused) ||
			!strings.Contains(err.Error(), "index.lock") ||
			strings.Contains(err.Error(), "roll back") {
			t.Errorf("Update(index locked) = %v, want the failure of git add", err)
		}
		// The update was rolled back.
		if got := headOf(t, f.dir("lib")); got != v100 {
			t.Errorf("lib was not restored: HEAD %s", got)
		}
		if got := readFile(t, lockFile); got != lockBefore {
			t.Errorf("lock file changed:\n%s", got)
		}
		if err := os.Remove(indexLock); err != nil {
			t.Fatal(err)
		}
		wantStaged(t, f.super.Dir)
		res := f.mustUpdate(core.UpdateOptions{Commit: true})
		if res.Commit == "" || gitlinkAt(t, f.super.Dir, "HEAD", "lib") != v101 {
			t.Errorf("retry = %+v", res)
		}
	})
}

func TestUpdateCommitRestagesStaleGitlink(t *testing.T) {
	t.Parallel()
	f := newFixture(t, gittest.SHA1)
	v100, v101 := f.commits[gittest.TagV100], f.commits[gittest.TagV101]
	f.track("lib", manifest.ModeTag, gittest.TagV101, gittest.TagV101, v101)
	f.track("lib2", manifest.ModeTagPattern, "v1.*", gittest.TagV100, v100)
	// lib is up to date, but the index holds an older gitlink.
	f.checkout("lib", v100)
	gittest.Git(t, f.super.Dir, "add", "lib")
	f.checkout("lib", v101)

	res := f.mustUpdate(core.UpdateOptions{Commit: true})
	if len(res.Changes) != 2 || res.Changes[0].Changed() || !res.Changes[1].Changed() ||
		res.Commit == "" {
		t.Fatalf("Update(commit) = %+v", res)
	}
	if msg := headMessage(t, f.super.Dir); !strings.HasPrefix(msg,
		"manifest: update lib2 to v1.0.1\n") {
		t.Errorf("commit message:\n%s", msg)
	}
	files := gittest.Git(t, f.super.Dir, "diff", "--name-only", "HEAD^", "HEAD")
	if files != ".lsm.lock\nlib2" {
		t.Errorf("committed files %q", files)
	}
	for _, name := range []string{"lib", "lib2"} {
		if got := gitlinkAt(t, f.super.Dir, "HEAD", name); got != v101 {
			t.Errorf("%s: committed gitlink %s, want %s", name, got, v101)
		}
	}
	wantStaged(t, f.super.Dir)
	if vr, err := f.repo().Verify(t.Context(), nil, core.VerifyOptions{}); err != nil {
		t.Errorf("Verify = %+v, %v", vr, err)
	}
}

func TestUpdateRollback(t *testing.T) {
	t.Parallel()
	t.Run("new lock file", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t, gittest.SHA1)
		tip := f.commits[gittest.TagV200RC]
		f.add("new", manifest.ModeTag, gittest.TagV100)
		// The native branch key is missing, so the update writes it.
		f.add("br", manifest.ModeBranch, gittest.BranchStable)
		gitmodules := readFile(t, filepath.Join(f.super.Dir, manifest.File))
		gittest.WriteFile(t, filepath.Join(f.super.Dir, ".git", "index.lock"), "")

		_, err := f.update(core.UpdateOptions{})
		if _, ok := errors.AsType[*git.Error](err); !ok || strings.Contains(err.Error(), "roll back") {
			t.Errorf("Update = %v, want the failure of git add", err)
		}
		for _, name := range []string{"new", "br"} {
			if got := headOf(t, f.dir(name)); got != tip {
				t.Errorf("%s was not restored: HEAD %s", name, got)
			}
		}
		if got := readFile(t, filepath.Join(f.super.Dir, lock.File)); got != "<missing>" {
			t.Errorf("lock file was left:\n%s", got)
		}
		if got := readFile(t, filepath.Join(f.super.Dir, manifest.File)); got != gitmodules {
			t.Errorf(".gitmodules changed:\n%s\nwant\n%s", got, gitmodules)
		}
	})

	t.Run("existing lock file", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t, gittest.SHA1)
		v100, v101 := f.commits[gittest.TagV100], f.commits[gittest.TagV101]
		tip := f.commits[gittest.TagV200RC]
		f.track("a", manifest.ModeTagPattern, "v1.*", gittest.TagV100, v100)
		f.add("c", manifest.ModeTag, gittest.TagV101)
		// A tag has no native branch key, so the update removes it.
		f.track("t", manifest.ModeTag, gittest.TagV101, gittest.TagV101, v101)
		f.super.SetKey(t, "t", manifest.KeyBranch, "main")
		f.super.Commit(t, "stale branch key")
		files := map[string]string{}
		for _, file := range []string{manifest.File, lock.File} {
			files[file] = readFile(t, filepath.Join(f.super.Dir, file))
		}
		gittest.WriteFile(t, filepath.Join(f.super.Dir, ".git", "index.lock"), "")

		_, err := f.update(core.UpdateOptions{})
		if _, ok := errors.AsType[*git.Error](err); !ok || strings.Contains(err.Error(), "roll back") {
			t.Errorf("Update = %v, want the failure of git add", err)
		}
		for name, want := range map[string]string{"a": v100, "c": tip, "t": v101} {
			if got := headOf(t, f.dir(name)); got != want {
				t.Errorf("%s: HEAD %s, want %s", name, got, want)
			}
		}
		for file, want := range files {
			if got := readFile(t, filepath.Join(f.super.Dir, file)); got != want {
				t.Errorf("%s changed:\n%s\nwant\n%s", file, got, want)
			}
		}
	})

	t.Run("lock file locked", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t, gittest.SHA1)
		v100 := f.commits[gittest.TagV100]
		f.track("a", manifest.ModeTagPattern, "v1.*", gittest.TagV100, v100)
		gittest.WriteFile(t, filepath.Join(f.super.Dir, lock.File+".lock"), "")

		// The lock entry cannot be put back either, but the checkout is.
		_, err := f.update(core.UpdateOptions{})
		if _, ok := errors.AsType[*git.Error](err); !ok ||
			!strings.HasPrefix(err.Error(), "a: write .lsm.lock: ") ||
			!strings.Contains(err.Error(), "\nroll back update: a: write .lsm.lock: ") {
			t.Errorf("Update = %v, want the failed write and rollback", err)
		}
		if got := headOf(t, f.dir("a")); got != v100 {
			t.Errorf("a was not restored: HEAD %s", got)
		}
		wantStaged(t, f.super.Dir)
	})
}

func TestUpdateRestoreSkipsUnbornHead(t *testing.T) {
	t.Parallel()
	f := newFixture(t, gittest.SHA1)
	v101 := f.commits[gittest.TagV101]
	f.track("a", manifest.ModeTag, gittest.TagV101, gittest.TagV101, v101)
	f.track("b", manifest.ModeTag, gittest.TagV101, gittest.TagV101, v101)
	gittest.Git(t, f.dir("a"), "checkout", "--quiet", "--orphan", "empty")
	gittest.Git(t, f.dir("a"), "rm", "-r", "--quiet", "-f", ".")
	// The next release adds a file that exists, untracked, in b.
	gittest.WriteFile(t, filepath.Join(f.up.Work, "extra.txt"), "tracked\n")
	f.up.Tag(t, "v3.0.0", f.up.Commit(t, "add extra"))
	if err := f.g.Fetch(t.Context(), f.dir("b"), "origin", nil); err != nil {
		t.Fatal(err)
	}
	f.configure("b", manifest.ModeTag, "v3.0.0")
	gittest.WriteFile(t, filepath.Join(f.dir("b"), "extra.txt"), "untracked\n")

	_, err := f.update(core.UpdateOptions{})
	if _, ok := errors.AsType[*git.Error](err); !ok || !strings.HasPrefix(err.Error(), "b: git ") ||
		strings.Contains(err.Error(), "restore") {
		t.Errorf("Update = %v, want only the checkout failure of b", err)
	}
	// a had no commit to go back to; it stays at its target.
	if got := headOf(t, f.dir("a")); got != v101 {
		t.Errorf("a: HEAD %s, want %s", got, v101)
	}
	if got := headOf(t, f.dir("b")); got != v101 {
		t.Errorf("b moved to %s", got)
	}
}

func TestUpdateOfflineInitNeedsRecordedCommit(t *testing.T) {
	t.Parallel()
	f := newFixture(t, gittest.SHA1)
	v100 := f.commits[gittest.TagV100]
	names := []string{"lib", "gone", "moved"}
	for _, name := range names {
		f.track(name, manifest.ModeTag, gittest.TagV100, gittest.TagV100, v100)
	}
	// A pull of the superproject moved the gitlinks to a commit that the
	// submodule repositories do not have yet.
	newer := f.up.Commit(t, "newer")
	for _, name := range names {
		gittest.Git(t, f.super.Dir, "update-index", "--cacheinfo", "160000,"+newer+","+name)
	}
	gittest.Git(t, f.super.Dir, "commit", "--quiet", "-m", "move gitlinks")
	// lib is deinitialized; the working trees of gone and moved are removed,
	// and the repository of moved names another working tree.
	f.super.Deinit(t, "lib")
	modules := filepath.Join(f.super.Dir, ".git", "modules")
	for _, name := range names[1:] {
		if err := os.RemoveAll(f.dir(name)); err != nil {
			t.Fatal(err)
		}
	}
	gittest.Git(t, f.super.Dir, "config", "-f", filepath.Join(modules, "moved", "config"),
		"core.worktree", "../../../elsewhere")
	before := treeState(t, f.super.Dir)

	// moved is not refused, but the directory created for it is removed.
	for _, opts := range []core.UpdateOptions{
		{Names: names}, {Names: names, DryRun: true, Commit: true},
	} {
		res, err := f.update(opts)
		wantErr(t, fmt.Sprintf("Update(%+v)", opts), err, core.ErrUninitialized, core.ErrRefused)
		lacks := ": refused: submodule is not initialized (use --fetch): " +
			"its repository lacks the recorded commit " + newer[:12]
		want := "lib" + lacks + "\ngone" + lacks
		if opts.DryRun {
			// A dry run creates no directory, so gone cannot be checked.
			want = "lib" + lacks
		}
		if err == nil || err.Error() != want || len(res.Changes) != 0 {
			t.Errorf("Update(%+v) = %+v, %v\nwant %s", opts, res, err, want)
		}
	}
	wantSameTree(t, "refused update", before, treeState(t, f.super.Dir))
	for _, name := range names[:2] {
		wantState(t, f.status(name), core.StateUninitialized, "not checked out")
	}

	// A repository that git cannot run in is not checked before; its
	// initialization fails, and nothing is fetched.
	_, err := f.update(core.UpdateOptions{Names: []string{"moved"}})
	if _, ok := errors.AsType[*git.Error](err); !ok || errors.Is(err, core.ErrRefused) {
		t.Errorf("Update(moved) = %v, want the failed initialization", err)
	}
	if hasObject(t, f.g, filepath.Join(modules, "moved"), newer) {
		t.Errorf("Update(moved) without Fetch fetched %s", newer)
	}

	// With Fetch, the initialization fetches the recorded commit.
	res := f.mustUpdate(core.UpdateOptions{Names: names[:2], Fetch: true})
	for i, c := range res.Changes {
		wantSteps(t, c, true, false)
		if c.New.Commit != v100 || headOf(t, f.dir(names[i])) != v100 {
			t.Errorf("Update(fetch) = %+v", c)
		}
		if !hasObject(t, f.g, filepath.Join(modules, names[i]), newer) {
			t.Errorf("%s: the recorded commit was not fetched", names[i])
		}
	}
}

func TestUpdateRemovedWorktree(t *testing.T) {
	t.Parallel()
	f := newFixture(t, gittest.SHA1)
	v101 := f.commits[gittest.TagV101]
	// Two submodules below a common directory, and one with a missing ref.
	deep := []string{"one", "two"}
	for _, name := range deep {
		gittest.Git(t, f.super.Dir, "submodule", "add", "--quiet", "--name", name, "--",
			f.up.Bare, "deep/"+name)
		f.configure(name, manifest.ModeTagPattern, "v1.*")
	}
	f.super.Commit(t, "add deep submodules")
	f.track("missing", manifest.ModeTag, gittest.TagV101, gittest.TagV101, v101)
	f.configure("missing", manifest.ModeTag, "v9")
	for _, dir := range []string{"deep", "missing"} {
		if err := os.RemoveAll(f.dir(dir)); err != nil {
			t.Fatal(err)
		}
	}
	before := treeState(t, f.super.Dir)

	// A dry run creates nothing, so the targets are unknown; the existing
	// repositories need no clone.
	for _, fetch := range []bool{false, true} {
		res, err := f.update(core.UpdateOptions{DryRun: true, Fetch: fetch, Names: deep})
		if err != nil || len(res.Changes) != 2 {
			t.Fatalf("Update(dry run, fetch %t) = %+v, %v", fetch, res, err)
		}
		for _, c := range res.Changes {
			wantSteps(t, c, true, false)
			if c.New != (core.Resolution{}) || !c.Changed() {
				t.Errorf("dry run, fetch %t: %+v", fetch, c)
			}
		}
	}
	wantSameTree(t, "dry run", before, treeState(t, f.super.Dir))

	// The missing ref is found before any submodule is initialized, and the
	// directories created to read the repositories are removed again.
	_, err := f.update(core.UpdateOptions{})
	want := "missing: refused: ref not found in local refs: " +
		"tag v9 does not exist or does not point to a commit"
	if !errors.Is(err, core.ErrMissingRef) || err.Error() != want {
		t.Errorf("Update = %v, want %s", err, want)
	}
	wantSameTree(t, "refused update", before, treeState(t, f.super.Dir))
	wantState(t, f.status("one"), core.StateUninitialized, "not checked out")

	res := f.mustUpdate(core.UpdateOptions{Names: deep})
	for _, c := range res.Changes {
		wantSteps(t, c, true, false)
		dir := filepath.Join(f.super.Dir, "deep", c.Submodule.Name)
		if c.New.Commit != v101 || c.New.Ref != gittest.TagV101 || headOf(t, dir) != v101 {
			t.Errorf("Update = %+v", c)
		}
	}
	wantStaged(t, f.super.Dir, manifest.File, lock.File, "deep/one", "deep/two")
}

func TestUpdateDryRunCommitNeverCommits(t *testing.T) {
	t.Parallel()
	f := newFixture(t, gittest.SHA1)
	v100 := f.commits[gittest.TagV100]
	f.track("lib", manifest.ModeTagPattern, "v1.*", gittest.TagV100, v100)
	// A staged change that a commit of the update would include.
	f.super.SetKey(t, "lib", "update", "checkout")
	gittest.Git(t, f.super.Dir, "add", manifest.File)
	head := headOf(t, f.super.Dir)
	before := treeState(t, f.super.Dir)

	res, err := f.update(core.UpdateOptions{DryRun: true, Commit: true})
	if err != nil || res.Commit != "" || !oneChange(t, res).Changed() ||
		headOf(t, f.super.Dir) != head {
		t.Errorf("Update(dry run, commit) = %+v, %v", res, err)
	}
	wantSameTree(t, "dry run with commit", before, treeState(t, f.super.Dir))
}

func TestUpdateSymlinkedFiles(t *testing.T) {
	t.Parallel()
	for _, file := range []string{manifest.File, lock.File} {
		t.Run(file, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t, gittest.SHA1)
			v100 := f.commits[gittest.TagV100]
			f.track("lib", manifest.ModeTagPattern, "v1.*", gittest.TagV100, v100)
			// A committed link could make a write change a file elsewhere.
			outside := filepath.Join(t.TempDir(), "outside")
			local := filepath.Join(f.super.Dir, file)
			gittest.WriteFile(t, outside, readFile(t, local))
			if err := os.Remove(local); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, local); err != nil {
				t.Fatal(err)
			}
			content := readFile(t, outside)
			before := treeState(t, f.super.Dir)
			r := f.repo()
			_, err := r.Status(t.Context(), nil)
			wantErr(t, "Status", err, git.ErrNotRegularFile)
			for _, opts := range []core.UpdateOptions{{}, {Fetch: true, Commit: true}} {
				_, err := r.Update(t.Context(), opts)
				wantErr(t, fmt.Sprintf("Update(%+v)", opts), err, git.ErrNotRegularFile)
			}
			if file == manifest.File {
				// Set does not read the lock file.
				_, err = r.Set(t.Context(), "lib", manifest.ModeBranch, "main")
				wantErr(t, "Set", err, git.ErrNotRegularFile)
			}
			// Add refuses before it clones anything.
			_, err = f.addSub("new", manifest.ModeTag, gittest.TagV100, false)
			wantErr(t, "Add", err, git.ErrNotRegularFile)
			wantSameTree(t, "commands on a linked "+file, before, treeState(t, f.super.Dir))
			if got := readFile(t, outside); got != content {
				t.Errorf("the linked file changed:\n%s", got)
			}
		})
	}
}

func TestUpdateNotRepository(t *testing.T) {
	t.Parallel()
	for name, create := range map[string]func(t *testing.T, p string){
		"empty directory": func(t *testing.T, p string) {
			if err := os.Mkdir(p, 0o755); err != nil {
				t.Fatal(err)
			}
		},
		"file": func(t *testing.T, p string) {
			gittest.WriteFile(t, p, "gitdir: ../../../elsewhere\n")
		},
		"broken link": func(t *testing.T, p string) {
			if err := os.Symlink("elsewhere", p); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t, gittest.SHA1)
			v100 := f.commits[gittest.TagV100]
			f.track("lib", manifest.ModeTag, gittest.TagV100, gittest.TagV100, v100)
			f.track("other", manifest.ModeTag, gittest.TagV100, gittest.TagV100, v100)
			f.super.Deinit(t, "lib")
			f.removeModule("lib")
			module := filepath.Join(f.super.Dir, ".git", "modules", "lib")
			create(t, module)
			before := treeState(t, f.super.Dir)

			// Git would neither use nor replace what is there, and it would
			// fail only after registering the submodule.
			want := "lib: refused: the submodule repository directory is not a repository: " +
				module + " (remove it)"
			r := f.repo()
			for _, opts := range []core.UpdateOptions{
				{}, {Fetch: true}, {DryRun: true}, {DryRun: true, Fetch: true},
				{Fetch: true, Commit: true},
			} {
				res, err := r.Update(t.Context(), opts)
				wantRefusal(t, fmt.Sprintf("Update(%+v)", opts), res, err, want,
					core.ErrNotRepository)
			}
			res, err := r.Fetch(t.Context(), nil, nil)
			wantErr(t, "Fetch", err, core.ErrNotRepository, core.ErrRefused)
			if err == nil || err.Error() != want || len(res) != 0 {
				t.Errorf("Fetch = %+v, %v\nwant %s", res, err, want)
			}
			wantSameTree(t, "refused commands", before, treeState(t, f.super.Dir))
			if status := gittest.Git(t, f.super.Dir, "status", "--porcelain"); status != "" {
				t.Errorf("git status = %q", status)
			}
			wantState(t, f.status("lib"), core.StateUninitialized, "is not a repository")

			// Once the directory is removed, the submodule can be cloned.
			if err := os.Remove(module); err != nil {
				t.Fatal(err)
			}
			res2 := f.mustUpdate(core.UpdateOptions{Fetch: true, Names: []string{"lib"}})
			wantSteps(t, oneChange(t, res2), true, true)
			wantState(t, f.status("lib"), core.StateOK, "up to date")
		})
	}
}
