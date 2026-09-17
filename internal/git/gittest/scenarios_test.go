// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package gittest_test

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/FPGArtktic/lazysubmodules/internal/git/gittest"
)

func TestNestedSuper(t *testing.T) {
	t.Parallel()
	for _, format := range formats() {
		t.Run(format, func(t *testing.T) {
			t.Parallel()
			n := gittest.NewNestedSuper(t, format)
			g := gittest.Runner(t)
			subs := n.Submodules()
			checkFiles(t, g, n.Dir, subs)
			checkGitlinks(t, g, n.Dir, "HEAD", subs)
			checkCheckouts(t, g, n.GitDir, "", subs)

			samePath(t, "outer repository", mustGit(t, g, n.Dir, "rev-parse",
				"--show-superproject-working-tree"), n.Outer)
			samePath(t, "git dir", n.GitDir, filepath.Join(n.Outer, ".git", "modules", "super"))
			if fi, err := os.Lstat(filepath.Join(n.Dir, ".git")); err != nil ||
				!fi.Mode().IsRegular() {
				t.Errorf(".git of the superproject: %v, %v; want a file", fi, err)
			}
			tip := mustGit(t, g, n.SuperUpstream.Bare, "rev-parse", "main")
			outer := gitlinks(t, g, n.Outer, "HEAD")
			if len(outer) != 1 || outer[n.SuperPath] != tip ||
				mustGit(t, g, n.Dir, "rev-parse", "HEAD") != tip {
				t.Errorf("outer gitlinks %v, superproject upstream at %s", outer, tip)
			}
			for _, s := range subs[:2] {
				if !strings.HasPrefix(s.GitDir, n.GitDir+"/modules/") || s.Head == "" {
					t.Errorf("%s: repository %q, head %q", s.Name, s.GitDir, s.Head)
				}
			}
			later := n.Later
			if later.GitDir != "" || later.Head != "" || later.Branch != gittest.BranchStable ||
				later.Lock.Commit != later.Branches[gittest.BranchStable] {
				t.Errorf("later: %+v", later)
			}
			if n.Lib.Lock.Ref != gittest.TagV100 || n.Lib.Tags[gittest.TagV101] == "" {
				t.Errorf("lib: %+v", n.Lib)
			}
			for _, dir := range []string{n.Outer, n.Dir} {
				wantClean(t, dir)
			}
		})
	}
}

func TestUnbornSuper(t *testing.T) {
	t.Parallel()
	for _, format := range formats() {
		t.Run(format, func(t *testing.T) {
			t.Parallel()
			u := gittest.NewUnbornSuper(t, format)
			g := gittest.Runner(t)
			subs := []*gittest.Submodule{u.Lib}
			wantNoRef(t, g, u.Dir, "HEAD")
			want := wantGitmodules(subs)
			checkEntries(t, "working tree", configEntries(t, g, u.Dir, "-f", ".gitmodules"), want)
			checkEntries(t, "index", configEntries(t, g, u.Dir, "--blob=:.gitmodules"), want)
			checkGitlinks(t, g, u.Dir, "", subs)
			checkCheckouts(t, g, filepath.Join(u.Dir, ".git"), "refs/heads/main", subs)
			if _, err := os.Lstat(filepath.Join(u.Dir, gittest.LockFile)); !errors.Is(err,
				fs.ErrNotExist) {
				t.Errorf("lock file: %v", err)
			}
			if u.Lib.Lock != nil || u.Lib.Head != u.Lib.Tags[gittest.TagV200RC] ||
				u.Lib.Mode != gittest.ModeTagPattern {
				t.Errorf("lib: %+v", u.Lib)
			}
			status, err := gittest.Runner(t).Run(t.Context(), u.Dir, "status", "--porcelain")
			if want := "A  .gitmodules\nA  lib\n"; err != nil || status != want {
				t.Errorf("status %q, %v; want %q", status, err, want)
			}
		})
	}
}

func TestManySuper(t *testing.T) {
	t.Parallel()
	for _, n := range []int{100, 11} {
		t.Run(strconv.Itoa(n), func(t *testing.T) {
			t.Parallel()
			m := gittest.NewManySuper(t, n)
			g := gittest.Runner(t)
			subs := m.Submodules()
			if len(subs) != n {
				t.Fatalf("%d submodules, want %d", len(subs), n)
			}
			checkFiles(t, g, m.Dir, subs)
			checkGitlinks(t, g, m.Dir, "HEAD", subs)
			checkManyOrder(t, subs)

			// One line per submodule, checked out at the recorded commit.
			status := strings.Split(mustGit(t, g, m.Dir, "submodule", "status"), "\n")
			paths := make(map[string]bool, n)
			for _, line := range status {
				fields := strings.Fields(line)
				paths[fields[1]] = true
				s := subs[slices.IndexFunc(subs, func(s *gittest.Submodule) bool {
					return s.Path == fields[1]
				})]
				if fields[0] != s.Gitlink || s.Head != s.Gitlink ||
					!strings.HasPrefix(s.GitDir, filepath.Join(m.Dir, ".git", "modules")) {
					t.Errorf("submodule status %q, %+v", line, s)
				}
			}
			if len(status) != n || len(paths) != n {
				t.Errorf("submodule status:\n%s", strings.Join(status, "\n"))
			}
			wantClean(t, m.Dir)
		})
	}
}

// checkManyOrder verifies the names and modes of NewManySuper.
func checkManyOrder(t *testing.T, subs []*gittest.Submodule) {
	t.Helper()
	n := len(subs)
	width := len(strconv.Itoa(n - 1))
	modes := []string{gittest.ModeBranch, gittest.ModeTag, gittest.ModeTagPattern,
		gittest.ModeCommit, ""}
	names := make([]string, n)
	for i, s := range subs {
		names[i] = s.Name
		want := fmt.Sprintf("sub-%0*d", width, n-1-i)
		if s.Name != want || s.Path != "mods/"+want || s.Mode != modes[i%5] ||
			(s.Lock == nil) != (s.Mode == "") {
			t.Errorf("submodule %d: %+v, want name %s, mode %q", i, s, want, modes[i%5])
		}
	}
	if slices.IsSorted(names) {
		t.Errorf("names are sorted: %q", names)
	}
	if subs[2].Lock.Commit != subs[2].Tags[gittest.TagV100] || subs[2].Tags[gittest.TagV101] ==
		"" || subs[3].Ref != subs[3].Tags[gittest.TagV101] {
		t.Errorf("tag-pattern %+v, commit %+v", subs[2], subs[3])
	}
}

// wantClean reports changes in the working tree dir.
func wantClean(t *testing.T, dir string) {
	t.Helper()
	status, err := gittest.Runner(t).Run(t.Context(), dir, "status", "--porcelain",
		"--ignore-submodules=none")
	if err != nil || status != "" {
		t.Errorf("status of %s: %q, %v", dir, status, err)
	}
}
