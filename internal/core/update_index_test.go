// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package core_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FPGArtktic/lazysubmodules/internal/core"
	"github.com/FPGArtktic/lazysubmodules/internal/git/gittest"
	"github.com/FPGArtktic/lazysubmodules/internal/lock"
	"github.com/FPGArtktic/lazysubmodules/internal/manifest"
)

// mustSet runs Set and fails the test on an error.
func (f *fixture) mustSet(name string, mode manifest.Mode, ref string) {
	f.t.Helper()
	if _, err := f.set(name, mode, ref); err != nil {
		f.t.Fatal(err)
	}
}

// wantUpToDate checks that an update changes nothing, not even a file.
func wantUpToDate(t *testing.T, f *fixture, opts core.UpdateOptions) {
	t.Helper()
	before := treeState(t, f.super.Dir)
	head := headOf(t, f.super.Dir)
	res := f.mustUpdate(opts)
	for _, c := range res.Changes {
		if c.Changed() {
			t.Errorf("Update(%+v): %+v", opts, c)
		}
	}
	if res.Commit != "" || headOf(t, f.super.Dir) != head {
		t.Errorf("Update(%+v) committed %s", opts, res.Commit)
	}
	wantSameTree(t, "update without changes", before, treeState(t, f.super.Dir))
}

// wantCommitted checks that an update with Commit, and with Fetch when
// fetch is set, creates one commit with the given files and message, after
// which the superproject is clean.
func wantCommitted(t *testing.T, f *fixture, fetch bool, files, msg string) {
	t.Helper()
	head := headOf(t, f.super.Dir)
	res := f.mustUpdate(core.UpdateOptions{Commit: true, Fetch: fetch})
	if res.Commit == "" || gittest.Git(t, f.super.Dir, "rev-parse", "HEAD^") != head {
		t.Fatalf("Update(commit) = %+v: not exactly one commit", res)
	}
	if got := gittest.Git(t, f.super.Dir, "diff", "--name-only", "HEAD^", "HEAD"); got != files {
		t.Errorf("committed files %q, want %q", got, files)
	}
	if got := headMessage(t, f.super.Dir); got != msg+"\n"+signOff {
		t.Errorf("commit message:\n%s\nwant\n%s", got, msg+"\n"+signOff)
	}
	lintMessage(t, headMessage(t, f.super.Dir))
	if status := gittest.Git(t, f.super.Dir, "status", "--porcelain"); status != "" {
		t.Errorf("status after commit %q", status)
	}
	wantUpToDate(t, f, core.UpdateOptions{Commit: true})
}

func TestUpdateRestagesTracking(t *testing.T) {
	t.Parallel()

	t.Run("lock entry", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t, gittest.SHA1)
		v101 := f.commits[gittest.TagV101]
		f.track("lib", manifest.ModeTagPattern, "v1.*", gittest.TagV101, v101)
		committed := lock.Entry{Name: "lib", Mode: manifest.ModeTagPattern,
			Ref: gittest.TagV101, Commit: v101}
		// Only the mode changes: the commit stays.
		f.mustSet("lib", manifest.ModeTag, gittest.TagV101)
		f.mustUpdate(core.UpdateOptions{})
		wantStaged(t, f.super.Dir, manifest.File, lock.File)
		gittest.Git(t, f.super.Dir, "reset", "--quiet")

		c := oneChange(t, f.mustUpdate(core.UpdateOptions{}))
		if !c.Changed() || c.Old == nil || *c.Old != committed ||
			c.OldHead != v101 || c.OldGitlink != v101 || c.New.Mode != manifest.ModeTag {
			t.Errorf("update of the unstaged lock entry: %+v", c)
		}
		wantStaged(t, f.super.Dir, manifest.File, lock.File)
		indexMatchesWorktree(t, f.super.Dir, manifest.File, lock.File)
		wantUpToDate(t, f, core.UpdateOptions{})

		gittest.Git(t, f.super.Dir, "reset", "--quiet")
		wantCommitted(t, f, false, ".gitmodules\n.lsm.lock", "manifest: update lib to v1.0.1\n\n"+
			"Tracking mode: tag v1.0.1\n"+
			"Old: "+v101[:12]+" (v1.0.1)\n"+
			"New: "+v101[:12]+" (v1.0.1)\n")
	})

	t.Run("configured ref", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t, gittest.SHA1)
		v101 := f.commits[gittest.TagV101]
		f.track("lib", manifest.ModeTagPattern, "v1.*", gittest.TagV101, v101)
		// The pattern changes, but it selects the locked tag again.
		f.mustSet("lib", manifest.ModeTagPattern, "v1.0.*")

		c := oneChange(t, f.mustUpdate(core.UpdateOptions{}))
		if !c.Changed() || c.New.Commit != v101 || c.New.Ref != gittest.TagV101 {
			t.Errorf("update of the unstaged pattern: %+v", c)
		}
		wantStaged(t, f.super.Dir, manifest.File)
		wantUpToDate(t, f, core.UpdateOptions{})

		gittest.Git(t, f.super.Dir, "reset", "--quiet")
		wantCommitted(t, f, false, ".gitmodules", "manifest: update lib to v1.0.1\n\n"+
			"Tracking mode: tag-pattern v1.0.*\n"+
			"Old: "+v101[:12]+" (v1.0.1)\n"+
			"New: "+v101[:12]+" (v1.0.1)\n")
	})

	t.Run("branch key", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t, gittest.SHA1)
		tip := f.commits[gittest.TagV200RC]
		// HEAD lacks the native branch key; the working tree has it.
		f.track("lib", manifest.ModeBranch, "main", "main", tip)
		f.super.SetKey(t, "lib", manifest.KeyBranch, "main")

		c := oneChange(t, f.mustUpdate(core.UpdateOptions{}))
		if !c.Changed() || c.New.Commit != tip {
			t.Errorf("update of the unstaged branch key: %+v", c)
		}
		wantStaged(t, f.super.Dir, manifest.File)
		indexMatchesWorktree(t, f.super.Dir, manifest.File)
		wantUpToDate(t, f, core.UpdateOptions{})

		wantCommitted(t, f, false, ".gitmodules", "manifest: update lib to main\n\n"+
			"Tracking mode: branch main\n"+
			"Old: "+tip[:12]+" (main)\n"+
			"New: "+tip[:12]+" (main)\n")
	})

	t.Run("working tree lock", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t, gittest.SHA1)
		v100, v101 := f.commits[gittest.TagV100], f.commits[gittest.TagV101]
		f.track("lib", manifest.ModeTagPattern, "v1.*", gittest.TagV100, v100)
		lockFile := filepath.Join(f.super.Dir, lock.File)
		content := readFile(t, lockFile)
		f.mustUpdate(core.UpdateOptions{})
		// The staged update stays, but the lock file goes back.
		gittest.WriteFile(t, lockFile, content)

		// A failed update puts back the lock file, not the index entry.
		indexLock := filepath.Join(f.super.Dir, ".git", "index.lock")
		gittest.WriteFile(t, indexLock, "")
		if _, err := f.update(core.UpdateOptions{}); err == nil {
			t.Error("Update with a locked index succeeded")
		}
		if got := readFile(t, lockFile); got != content {
			t.Errorf("lock file after the rollback:\n%s\nwant\n%s", got, content)
		}
		if err := os.Remove(indexLock); err != nil {
			t.Fatal(err)
		}

		c := oneChange(t, f.mustUpdate(core.UpdateOptions{}))
		want := lock.Entry{Name: "lib", Mode: manifest.ModeTagPattern, Ref: gittest.TagV101,
			Commit: v101}
		if !c.Changed() || c.Old == nil || *c.Old != want || c.OldHead != v101 {
			t.Errorf("update of the lock file: %+v", c)
		}
		if got := lockOf(t, f, "lib"); got == nil || *got != want {
			t.Errorf("lock entry %+v, want %+v", got, want)
		}
		wantStaged(t, f.super.Dir, lock.File, "lib")
		indexMatchesWorktree(t, f.super.Dir, lock.File)
		wantUpToDate(t, f, core.UpdateOptions{})
	})

	t.Run("index only", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t, gittest.SHA1)
		v100, v101 := f.commits[gittest.TagV100], f.commits[gittest.TagV101]
		f.track("lib", manifest.ModeTag, gittest.TagV100, gittest.TagV100, v100)
		lockFile := filepath.Join(f.super.Dir, lock.File)
		content := readFile(t, lockFile)
		f.lock("lib", manifest.ModeTag, gittest.TagV100, v101)
		gittest.Git(t, f.super.Dir, "add", lock.File)
		gittest.WriteFile(t, lockFile, content)

		// HEAD is up to date, so nothing is committed, but the index is
		// repaired.
		res := f.mustUpdate(core.UpdateOptions{Commit: true})
		if c := oneChange(t, res); c.Changed() || res.Commit != "" {
			t.Errorf("Update(commit) = %+v", res)
		}
		wantStaged(t, f.super.Dir)
		indexMatchesWorktree(t, f.super.Dir, lock.File)
	})

	for _, fetch := range []bool{false, true} {
		t.Run(fmt.Sprintf("staged pre-release/fetch=%v", fetch), func(t *testing.T) {
			t.Parallel()
			f := newFixture(t, gittest.SHA1)
			v101, rc := f.commits[gittest.TagV101], f.commits[gittest.TagV200RC]
			f.track("lib", manifest.ModeTagPattern, "v*", gittest.TagV101, v101)
			c := oneChange(t, f.mustUpdate(core.UpdateOptions{IncludePrerelease: true}))
			if c.New.Commit != rc {
				t.Fatalf("Update(pre-release) = %+v", c)
			}
			// The lock file of the working tree keeps the tag, although HEAD
			// records another one; with Fetch, the target is resolved after
			// the fetch.
			wantCommitted(t, f, fetch, ".lsm.lock\nlib",
				"manifest: update lib to v2.0.0-rc.1\n\n"+
					"Tracking mode: tag-pattern v*\n"+
					"Old: "+v101[:12]+" (v1.0.1)\n"+
					"New: "+rc[:12]+" (v2.0.0-rc.1)\n")
		})
	}

	t.Run("invalid copies", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t, gittest.SHA1)
		v100 := f.commits[gittest.TagV100]
		f.track("lib", manifest.ModeTag, gittest.TagV100, gittest.TagV100, v100)
		// HEAD and the index record an invalid mode and lock entry, which
		// the working tree repairs.
		f.super.SetKey(t, "lib", manifest.KeyMode, "bogus")
		gittest.Git(t, f.super.Dir, "config", "-f", lock.File, "submodule.lib.commit", "bogus")
		f.super.Commit(t, "break tracking")
		f.mustSet("lib", manifest.ModeTag, gittest.TagV100)
		f.lock("lib", manifest.ModeTag, gittest.TagV100, v100)

		c := oneChange(t, f.mustUpdate(core.UpdateOptions{DryRun: true}))
		if !c.Changed() || c.Old != nil || c.New.Commit != v100 {
			t.Errorf("update of invalid copies: %+v", c)
		}
		wantCommitted(t, f, false, ".gitmodules\n.lsm.lock", "manifest: update lib to v1.0.0\n\n"+
			"Tracking mode: tag v1.0.0\n"+
			"Old: "+v100[:12]+" (unlocked)\n"+
			"New: "+v100[:12]+" (v1.0.0)\n")
		if vr, err := f.repo().Verify(t.Context(), nil, core.VerifyOptions{}); err != nil {
			t.Errorf("Verify = %+v, %v", vr, err)
		}
	})
}

func TestUpdateLinkedLockInIndex(t *testing.T) {
	t.Parallel()
	for _, commit := range []bool{false, true} {
		t.Run(fmt.Sprintf("commit=%t", commit), func(t *testing.T) {
			t.Parallel()
			f := newFixture(t, gittest.SHA1)
			v100 := f.commits[gittest.TagV100]
			f.track("lib", manifest.ModeTag, gittest.TagV100, gittest.TagV100, v100)
			d := f.super.Dir
			// A commit replaced the lock file by a link, and the working tree
			// has the regular file back.
			file := filepath.Join(d, lock.File)
			content := readFile(t, file)
			if err := os.Remove(file); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("elsewhere", file); err != nil {
				t.Fatal(err)
			}
			f.super.Commit(t, "link the lock file")
			if err := os.Remove(file); err != nil {
				t.Fatal(err)
			}
			gittest.WriteFile(t, file, content)

			// Neither the index nor HEAD records a lock entry; the update
			// stages the regular file in place of the link.
			c := oneChange(t, f.mustUpdate(core.UpdateOptions{Commit: commit}))
			if !c.Changed() || c.Old != nil || c.New.Commit != v100 {
				t.Errorf("Update = %+v", c)
			}
			rev := ""
			if commit {
				rev = "HEAD"
				if got := headMessage(t, d); !strings.Contains(got,
					"Old: "+v100[:12]+" (unlocked)\n") {
					t.Errorf("commit message:\n%s", got)
				}
			}
			entry := gittest.Git(t, d, "ls-files", "--stage", "--", lock.File)
			if commit {
				entry = gittest.Git(t, d, "ls-tree", rev, "--", lock.File)
			}
			if !strings.HasPrefix(entry, "100644 ") {
				t.Errorf("lock file in %q: %s", rev, entry)
			}
			wantUpToDate(t, f, core.UpdateOptions{Commit: commit})
		})
	}
}
