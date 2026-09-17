// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package gittest_test

import (
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/FPGArtktic/lazysubmodules/internal/git"
	"github.com/FPGArtktic/lazysubmodules/internal/git/gittest"
)

// formats lists the object formats of the scenario fixtures.
func formats() []string {
	return []string{gittest.SHA1, gittest.SHA256}
}

// hexLen returns the length of a full object name.
func hexLen(format string) int {
	if format == gittest.SHA256 {
		return 64
	}
	return 40
}

// tryGit runs git with g in dir and returns the trimmed output and the
// error, without failing the test.
func tryGit(t *testing.T, g *git.Runner, dir string, args ...string) (string, error) {
	t.Helper()
	out, err := g.Run(t.Context(), dir, args...)
	return strings.TrimSpace(out), err
}

// mustGit runs git with g in dir and fails the test when git fails.
func mustGit(t *testing.T, g *git.Runner, dir string, args ...string) string {
	t.Helper()
	out, err := tryGit(t, g, dir, args...)
	if err != nil {
		t.Fatalf("%v", err)
	}
	return out
}

// configEntries lists a configuration file with git; args select it, such
// as "-f", ".gitmodules".
func configEntries(t *testing.T, g *git.Runner, dir string, args ...string) []git.ConfigEntry {
	t.Helper()
	out, err := g.Run(t.Context(), dir, append([]string{"config", "--null", "--list"},
		args...)...)
	if err != nil {
		t.Fatalf("%v", err)
	}
	var entries []git.ConfigEntry
	for rec := range strings.SplitSeq(strings.TrimSuffix(out, "\x00"), "\x00") {
		if rec != "" {
			key, value, _ := strings.Cut(rec, "\n")
			entries = append(entries, git.ConfigEntry{Key: key, Value: value})
		}
	}
	return entries
}

// wantGitmodules returns the .gitmodules entries the fixtures write for
// subs.
func wantGitmodules(subs []*gittest.Submodule) []git.ConfigEntry {
	var want []git.ConfigEntry
	for _, s := range subs {
		add := func(key, value string) {
			want = append(want, git.ConfigEntry{Key: "submodule." + s.Name + "." + key,
				Value: value})
		}
		add("path", s.Path)
		add("url", s.URL)
		if s.Branch != "" {
			add("branch", s.Branch)
		}
		for _, e := range s.Extra {
			add(e.Key, e.Value)
		}
		if s.Mode != "" {
			add("lsm-mode", s.Mode)
			add("lsm-ref", s.Ref)
		}
	}
	return want
}

// wantLock returns the lock file entries of subs.
func wantLock(subs []*gittest.Submodule) []git.ConfigEntry {
	var want []git.ConfigEntry
	for _, s := range subs {
		if s.Lock == nil {
			continue
		}
		prefix := "submodule." + s.Name + "."
		want = append(want,
			git.ConfigEntry{Key: prefix + "mode", Value: s.Lock.Mode},
			git.ConfigEntry{Key: prefix + "ref", Value: s.Lock.Ref},
			git.ConfigEntry{Key: prefix + "commit", Value: s.Lock.Commit})
	}
	return want
}

// checkEntries compares configuration entries in order.
func checkEntries(t *testing.T, what string, got, want []git.ConfigEntry) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Errorf("%s:\n got %q\nwant %q", what, got, want)
	}
}

// checkFiles compares .gitmodules and the lock file of the superproject dir,
// in the working tree and in HEAD, with subs.
func checkFiles(t *testing.T, g *git.Runner, dir string, subs []*gittest.Submodule) {
	t.Helper()
	for file, want := range map[string][]git.ConfigEntry{
		gittest.GitmodulesFile: wantGitmodules(subs),
		gittest.LockFile:       wantLock(subs),
	} {
		checkEntries(t, file, configEntries(t, g, dir, "-f", file), want)
		checkEntries(t, "HEAD:"+file, configEntries(t, g, dir, "--blob=HEAD:"+file), want)
	}
}

// gitlinks returns the gitlinks of a tree ("" for the index) by path.
func gitlinks(t *testing.T, g *git.Runner, dir, rev string) map[string]string {
	t.Helper()
	args := []string{"ls-tree", "-r", "-z", "--full-tree", rev}
	if rev == "" {
		args = []string{"ls-files", "--stage", "-z"}
	}
	links := map[string]string{}
	out, err := g.Run(t.Context(), dir, args...)
	if err != nil {
		t.Fatalf("%v", err)
	}
	for rec := range strings.SplitSeq(out, "\x00") {
		// "<mode> <type> <object>\t<path>" or "<mode> <object> <stage>\t<path>"
		meta, p, _ := strings.Cut(rec, "\t")
		fields := strings.Fields(meta)
		if len(fields) == 3 && fields[0] == "160000" {
			links[p] = fields[1]
			if rev != "" {
				links[p] = fields[2]
			}
		}
	}
	return links
}

// checkGitlinks compares the gitlinks of rev ("" for the index) with subs.
func checkGitlinks(t *testing.T, g *git.Runner, dir, rev string, subs []*gittest.Submodule) {
	t.Helper()
	want := map[string]string{}
	for _, s := range subs {
		if s.Gitlink != "" {
			want[s.Path] = s.Gitlink
		}
	}
	if got := gitlinks(t, g, dir, rev); !maps.Equal(got, want) {
		t.Errorf("gitlinks of %q:\n got %v\nwant %v", rev, got, want)
	}
}

// checkCheckouts compares the working trees and repositories of subs with
// their descriptions. gitDir is the git directory of the superproject.
// Checked-out submodules must have a detached HEAD, or be on branch, when
// it is not empty.
func checkCheckouts(t *testing.T, g *git.Runner, gitDir, branch string,
	subs []*gittest.Submodule) {
	t.Helper()
	for _, s := range subs {
		dir := s.Dir()
		switch {
		case s.Head != "":
			if got := mustGit(t, g, dir, "rev-parse", "HEAD"); got != s.Head {
				t.Errorf("%s: HEAD %s, want %s", s.Name, got, s.Head)
			}
			if ref, _ := tryGit(t, g, dir, "symbolic-ref", "--quiet", "HEAD"); ref != branch {
				t.Errorf("%s: HEAD on %q, want %q", s.Name, ref, branch)
			}
			samePath(t, s.Name+" git dir", mustGit(t, g, dir, "rev-parse", "--absolute-git-dir"),
				s.GitDir)
		case s.Gitlink != "":
			if entries, err := os.ReadDir(dir); err != nil || len(entries) != 0 {
				t.Errorf("%s: directory %v, %v; want it empty", s.Name, entries, err)
			}
		}
		if s.GitDir != "" {
			samePath(t, s.Name+" repository", mustGit(t, g, s.GitDir, "rev-parse",
				"--absolute-git-dir"), s.GitDir)
			continue
		}
		// A name that is not a local path may name any directory.
		module := filepath.Join(gitDir, "modules", s.Name)
		_, err := os.Lstat(module)
		if s.Gitlink != "" && filepath.IsLocal(s.Name) && !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s: repository %s exists (%v)", s.Name, module, err)
		}
	}
}

// samePath reports when two paths do not name the same directory.
func samePath(t *testing.T, what, got, want string) {
	t.Helper()
	if same, err := git.SamePath(got, want); err != nil || !same {
		t.Errorf("%s: %s, want %s (%v)", what, got, want, err)
	}
}

// tagList lists the tags matching pattern ("" for all) in dir, highest
// version first.
func tagList(t *testing.T, g *git.Runner, dir, pattern string) []string {
	t.Helper()
	args := []string{"-c", "versionsort.suffix=-", "tag", "--list", "--sort=-v:refname"}
	if pattern != "" {
		args = append(args, pattern)
	}
	return strings.Fields(mustGit(t, g, dir, args...))
}

// objectType returns the type of the object a ref names.
func objectType(t *testing.T, g *git.Runner, dir, ref string) string {
	t.Helper()
	return mustGit(t, g, dir, "cat-file", "-t", ref)
}

// wantNoRef reports when rev exists in dir.
func wantNoRef(t *testing.T, g *git.Runner, dir, rev string) {
	t.Helper()
	if out, err := tryGit(t, g, dir, "rev-parse", "--verify", "--quiet", rev); err == nil {
		t.Errorf("%s exists in %s: %s", rev, dir, out)
	}
}
