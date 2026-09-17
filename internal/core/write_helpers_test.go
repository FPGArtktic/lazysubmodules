// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package core_test

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/FPGArtktic/lazysubmodules/internal/core"
	"github.com/FPGArtktic/lazysubmodules/internal/git/gittest"
	"github.com/FPGArtktic/lazysubmodules/internal/lock"
	"github.com/FPGArtktic/lazysubmodules/internal/manifest"
)

// signOff is the trailer that "git commit -s" adds with the test identity.
const signOff = "Signed-off-by: " + gittest.Name + " <" + gittest.Email + ">"

// treeState describes every file below dir, the git directory included:
// the relative path maps to the type and a hash of the content. Paths in
// skip ("/"-separated) are left out.
func treeState(t *testing.T, dir string, skip ...string) map[string]string {
	t.Helper()
	state := make(map[string]string)
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		switch {
		case slices.Contains(skip, rel) && d.IsDir():
			return filepath.SkipDir
		case slices.Contains(skip, rel):
			return nil
		case d.Type()&fs.ModeSymlink != 0:
			target, err := os.Readlink(p)
			state[rel] = "link " + target
			return err
		case d.IsDir():
			state[rel] = "dir"
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		data, err := os.ReadFile(p)
		state[rel] = fmt.Sprintf("%v %x", info.Mode(), sha256.Sum256(data))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return state
}

// wantSameTree reports the paths that differ between two treeState
// results.
func wantSameTree(t *testing.T, what string, before, after map[string]string) {
	t.Helper()
	var diffs []string
	for p, v := range before {
		if w, ok := after[p]; !ok {
			diffs = append(diffs, "removed "+p)
		} else if v != w {
			diffs = append(diffs, "changed "+p)
		}
	}
	for p := range after {
		if _, ok := before[p]; !ok {
			diffs = append(diffs, "added "+p)
		}
	}
	slices.Sort(diffs)
	if len(diffs) > 0 {
		t.Errorf("%s modified the repository:\n%s", what, strings.Join(diffs, "\n"))
	}
}

// staged lists the staged paths of the superproject, sorted.
func staged(t *testing.T, dir string) []string {
	t.Helper()
	out := gittest.Git(t, dir, "diff", "--cached", "--name-only", "--no-renames")
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

// wantStaged checks the staged paths of the superproject.
func wantStaged(t *testing.T, dir string, want ...string) {
	t.Helper()
	if got := staged(t, dir); !slices.Equal(got, want) {
		t.Errorf("staged paths %q, want %q", got, want)
	}
}

// indexMatchesWorktree checks that the index holds the working tree
// content of the given files.
func indexMatchesWorktree(t *testing.T, dir string, files ...string) {
	t.Helper()
	for _, file := range files {
		data, err := os.ReadFile(filepath.Join(dir, file))
		if err != nil {
			t.Fatal(err)
		}
		if got := gittest.Git(t, dir, "show", ":"+file); got != strings.TrimSpace(string(data)) {
			t.Errorf("index version of %s:\n%s\nwant\n%s", file, got, data)
		}
	}
}

// gitlinkAt returns the commit recorded for path p in rev, which is ""
// for the index.
func gitlinkAt(t *testing.T, dir, rev, p string) string {
	t.Helper()
	var out string
	if rev == "" {
		out = gittest.Git(t, dir, "ls-files", "--stage", "--", p)
	} else {
		out = gittest.Git(t, dir, "ls-tree", rev, "--", p)
	}
	// "<mode> <sha> <stage>\t<path>" or "<mode> commit <sha>\t<path>"
	fields := strings.Fields(out)
	if len(fields) < 3 || fields[0] != "160000" {
		t.Fatalf("no gitlink at %s in %q: %q", p, rev, out)
	}
	if rev == "" {
		return fields[1]
	}
	return fields[2]
}

// lockOf returns the lock entry of a submodule, or nil.
func lockOf(t *testing.T, f *fixture, name string) *lock.Entry {
	t.Helper()
	lk, err := lock.Load(t.Context(), f.g, f.super.Dir)
	if err != nil {
		t.Fatal(err)
	}
	e, ok := lk.Get(name)
	if !ok {
		return nil
	}
	return &e
}

// headOf returns the commit checked out in dir.
func headOf(t *testing.T, dir string) string {
	t.Helper()
	return gittest.Git(t, dir, "rev-parse", "HEAD")
}

// gitmodulesKey returns a variable of .gitmodules, or "" when it is unset.
func gitmodulesKey(t *testing.T, dir, name, key string) string {
	t.Helper()
	return gittest.Git(t, dir, "config", "-f", manifest.File, "--default=", "--get",
		"submodule."+name+"."+key)
}

// update runs Update on the fixture superproject.
func (f *fixture) update(opts core.UpdateOptions) (core.UpdateResult, error) {
	f.t.Helper()
	return f.repo().Update(f.t.Context(), opts)
}

// mustUpdate runs Update and fails the test on an error.
func (f *fixture) mustUpdate(opts core.UpdateOptions) core.UpdateResult {
	f.t.Helper()
	res, err := f.update(opts)
	if err != nil {
		f.t.Fatalf("Update(%+v): %v", opts, err)
	}
	return res
}

// titleRegexp is the subject rule of the commit message rules.
var titleRegexp = regexp.MustCompile(
	`^(cli|core|git|manifest|lock|porcelain|tui|build|scripts|ci|release|docs): .+`)

// lintMessage checks a commit message, as git records it, against the
// rules of the repository gitlint configuration and the gitlint defaults
// that a generated message could break.
func lintMessage(t *testing.T, msg string) {
	t.Helper()
	lines := strings.Split(strings.TrimSuffix(msg, "\n"), "\n")
	title := lines[0]
	switch {
	case !titleRegexp.MatchString(title):
		t.Errorf("title %q does not match the subsystem rule", title)
	case utf8.RuneCountInString(title) > 75:
		t.Errorf("title %q is longer than 75 characters", title)
	case strings.ContainsAny(title[len(title)-1:], "?:!.,; \t"):
		t.Errorf("title %q ends with punctuation or white space", title)
	case regexp.MustCompile(`(?i)\bwip\b`).MatchString(title):
		t.Errorf("title %q contains WIP", title)
	}
	if len(lines) < 3 || lines[1] != "" || strings.TrimSpace(strings.Join(lines[2:], "")) == "" {
		t.Fatalf("message has no body after a blank line:\n%s", msg)
	}
	for _, line := range lines[2:] {
		if utf8.RuneCountInString(line) > 75 || strings.ContainsRune(line, '\t') ||
			strings.TrimRight(line, " ") != line {
			t.Errorf("body line %q breaks the length, tab or white space rule", line)
		}
	}
	if !slices.Contains(lines, signOff) {
		t.Errorf("message has no %q line:\n%s", signOff, msg)
	}
}

// headMessage returns the message of the HEAD commit as git records it.
func headMessage(t *testing.T, dir string) string {
	t.Helper()
	out, err := gittest.Runner(t).Run(t.Context(), dir, "log", "-1", "--format=%B")
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimRight(out, "\n")
}
