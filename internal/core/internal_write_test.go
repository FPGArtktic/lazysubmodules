// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package core

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/FPGArtktic/lazysubmodules/internal/git"
	"github.com/FPGArtktic/lazysubmodules/internal/git/gittest"
	"github.com/FPGArtktic/lazysubmodules/internal/lock"
	"github.com/FPGArtktic/lazysubmodules/internal/manifest"
)

func TestRestoreLock(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	g := gittest.Runner(t)
	commit := strings.Repeat("ab", 20)
	entry := func(name, ref string) lock.Entry {
		return lock.Entry{Name: name, Mode: manifest.ModeTag, Ref: ref, Commit: commit}
	}
	old := entry("new", "v1")
	for _, c := range []struct {
		name    string
		written bool
		old     *lock.Entry
		others  bool
		want    []lock.Entry // nil: no lock file
	}{
		{"not written", false, nil, false, []lock.Entry{entry("new", "v2")}},
		{"stale entry", true, &old, true, []lock.Entry{entry("other", "v1"), old}},
		{"new file", true, nil, false, nil},
		{"existing file", true, nil, true, []lock.Entry{entry("other", "v1")}},
	} {
		dir := gittest.NewSuper(t, gittest.SHA1).Dir
		r, err := Open(ctx, g, dir)
		if err != nil {
			t.Fatal(err)
		}
		if c.others {
			if err := lock.Write(ctx, g, dir, entry("other", "v1")); err != nil {
				t.Fatal(err)
			}
		}
		u, err := r.newAddUndo(ctx, "new")
		if err != nil {
			t.Fatal(err)
		}
		u.oldEntry = c.old
		if err := lock.Write(ctx, g, dir, entry("new", "v2")); err != nil {
			t.Fatal(err)
		}
		u.lockWritten = c.written
		if err := u.restoreLock(ctx); err != nil {
			t.Errorf("%s: restoreLock = %v", c.name, err)
		}
		lk, err := lock.Load(ctx, g, dir)
		if got := lk.Entries(); err != nil || len(got) != len(c.want) {
			t.Errorf("%s: entries %+v, %v; want %+v", c.name, got, err, c.want)
		} else {
			for i := range got {
				if got[i] != c.want[i] {
					t.Errorf("%s: entry %+v, want %+v", c.name, got[i], c.want[i])
				}
			}
		}
		_, err = os.Lstat(filepath.Join(dir, lock.File))
		if (err == nil) != (c.want != nil) {
			t.Errorf("%s: lock file: %v", c.name, err)
		}
	}
}

func TestAddUndoFailures(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	dir := gittest.NewSuper(t, gittest.SHA1).Dir
	r, err := Open(ctx, gittest.Runner(t), dir)
	if err != nil {
		t.Fatal(err)
	}
	u, err := r.newAddUndo(ctx, "new")
	if err != nil {
		t.Fatal(err)
	}
	// A lock file that cannot be written makes the rollback fail, but the
	// other steps still run.
	gittest.WriteFile(t, filepath.Join(dir, "new", "file"), "x\n")
	u.lockWritten = true
	u.oldEntry = &lock.Entry{Name: "new", Mode: manifest.ModeTag, Ref: "v1",
		Commit: strings.Repeat("ab", 20)}
	gittest.WriteFile(t, filepath.Join(dir, lock.File+".lock"), "")
	err = u.run(ctx)
	if _, ok := errors.AsType[*git.Error](err); !ok ||
		!strings.HasPrefix(err.Error(), "roll back new: ") {
		t.Errorf("run = %v, want a rollback failure", err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "new")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("created directory remains: %v", err)
	}

	// Without a git directory, nothing can be looked up.
	u = &addUndo{repo: &Repo{git: r.git, root: t.TempDir()}, path: "new"}
	if _, err := u.repo.newAddUndo(ctx, "new"); err == nil {
		t.Errorf("newAddUndo outside a repository succeeded")
	}
	for name, step := range map[string]func() error{
		"removeGitlink":  func() error { return u.removeGitlink(ctx) },
		"removeGitDir":   func() error { return u.removeGitDir(ctx) },
		"removeManifest": func() error { return u.removeManifest(ctx) },
	} {
		if name == "removeManifest" {
			gittest.WriteFile(t, filepath.Join(u.repo.root, manifest.File), "")
		}
		if err := step(); err == nil {
			t.Errorf("%s outside a repository succeeded", name)
		}
	}
}

func TestSyncRequiresCheckout(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	super := gittest.NewSuper(t, gittest.SHA1)
	// Git initializes nothing for a directory that the superproject tracks,
	// and reports success.
	gittest.WriteFile(t, filepath.Join(super.Dir, "docs", "a.txt"), "docs\n")
	super.SetKey(t, "docs", manifest.KeyPath, "docs")
	super.SetKey(t, "docs", manifest.KeyURL, super.Dir)
	super.Commit(t, "docs")
	r, err := Open(ctx, gittest.Runner(t), super.Dir)
	if err != nil {
		t.Fatal(err)
	}
	sub := manifest.Submodule{Name: "docs", Path: "docs"}
	for _, online := range []bool{false, true} {
		err := r.sync(ctx, sub, filepath.Join(super.Dir, "docs"), true, online, nil)
		if err == nil || err.Error() != "git did not check out the submodule" {
			t.Errorf("sync(online %v) = %v", online, err)
		}
	}
}

func TestFileHelpers(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	file := filepath.Join(dir, "file")
	empty := filepath.Join(dir, "empty")
	sub := filepath.Join(dir, "a", "b")
	gittest.WriteFile(t, file, "x")
	gittest.WriteFile(t, empty, "")
	gittest.WriteFile(t, filepath.Join(sub, "c"), "x")
	if err := os.Symlink(empty, filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}

	for p, want := range map[string]string{
		"a/b/x":   filepath.Join(dir, "a", "b", "x"),
		"a/x/y":   filepath.Join(dir, "a", "x"),
		"new":     filepath.Join(dir, "new"),
		"x/y/z/w": filepath.Join(dir, "x"),
	} {
		if got := firstMissing(dir, p); got != want {
			t.Errorf("firstMissing(%s) = %s, want %s", p, got, want)
		}
	}
	for p, want := range map[string]bool{
		file: false, empty: true, sub: false, filepath.Join(dir, "link"): false,
		filepath.Join(dir, "missing"): false,
	} {
		if got, err := isEmptyFile(p); err != nil || got != want {
			t.Errorf("isEmptyFile(%s) = %v, %v; want %v", p, got, err, want)
		}
		if got, err := exists(p); err != nil || got != (p != filepath.Join(dir, "missing")) {
			t.Errorf("exists(%s) = %v, %v", p, got, err)
		}
	}
	if got, err := exists(filepath.Join(file, "x")); err != nil || got {
		t.Errorf("exists(below a file) = %v, %v", got, err)
	}
	for _, p := range []string{file, sub, filepath.Join(dir, "link")} {
		if err := removeEmpty(p); err != nil {
			t.Errorf("removeEmpty(%s) = %v", p, err)
		}
		if _, err := os.Lstat(p); err != nil {
			t.Errorf("removeEmpty removed %s", p)
		}
	}
	if err := removeEmpty(empty); err != nil {
		t.Errorf("removeEmpty(empty) = %v", err)
	}
	if _, err := os.Lstat(empty); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("removeEmpty kept the empty file: %v", err)
	}

	// Empty parents are removed up to, but not including, the top.
	deep := filepath.Join(dir, "top", "p", "q", "r")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	gittest.WriteFile(t, filepath.Join(dir, "top", "keep"), "x")
	removeEmptyParents(filepath.Join(deep, "gone"), filepath.Join(dir, "top", "p"))
	if _, err := os.Lstat(filepath.Join(dir, "top", "p", "q")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("empty parents remain: %v", err)
	}
	removeEmptyParents(filepath.Join(dir, "top", "p", "x"), dir)
	if _, err := os.Lstat(filepath.Join(dir, "top", "keep")); err != nil {
		t.Errorf("a directory that is not empty was removed: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "top", "p")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("empty parent remains: %v", err)
	}
}

func TestCreatedDirs(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	file := filepath.Join(root, "file")
	gittest.WriteFile(t, file, "x")
	u := &updater{repo: &Repo{root: root}}
	step := func(p string) *step {
		return &step{
			change: Change{Submodule: manifest.Submodule{Name: p, Path: p}},
			loc:    location{worktree: filepath.Join(root, filepath.FromSlash(p))},
		}
	}
	for _, c := range []struct {
		path    string
		created bool
	}{
		{"a/b/one", true}, {"a/b/two", true}, {"a", false}, {"c", true},
	} {
		if created, err := u.mkdirs(step(c.path)); err != nil || created != c.created {
			t.Errorf("mkdirs(%s) = %v, %v; want %v", c.path, created, err, c.created)
		}
	}
	created, err := u.mkdirs(step("file/x"))
	if err == nil || created || !strings.HasPrefix(err.Error(), "create working tree: ") {
		t.Errorf("mkdirs(below a file) = %v, %v", created, err)
	}
	want := []createdDir{
		{filepath.Join(root, "a", "b", "one"), filepath.Join(root, "a")},
		{filepath.Join(root, "a", "b", "two"), filepath.Join(root, "a", "b", "two")},
		{filepath.Join(root, "c"), filepath.Join(root, "c")},
		{filepath.Join(file, "x"), filepath.Join(file, "x")},
	}
	if !slices.Equal(u.created, want) {
		t.Errorf("created %q, want %q", u.created, want)
	}

	// A populated directory stays; the others are removed, the parent that
	// both a/b directories share included.
	gittest.WriteFile(t, filepath.Join(root, "c", ".git"), "gitdir: x\n")
	u.removeCreated()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if !slices.Equal(names, []string{"c", "file"}) || u.created != nil {
		t.Errorf("after removeCreated: %q, created %q", names, u.created)
	}
}

func TestSmallHelpers(t *testing.T) {
	t.Parallel()
	for key, want := range map[string]string{
		"submodule.lib.url":       "lib",
		"submodule.a.b/c.active":  "a.b/c",
		"submodule.active":        "",
		"remote.origin.url":       "",
		"submodule..url":          "",
		"submodulex.lib.url":      "",
		"submodule.lib with.path": "lib with",
	} {
		if got := configName(key); got != want {
			t.Errorf("configName(%q) = %q, want %q", key, got, want)
		}
	}
	for paths, want := range map[string]string{
		"a":         "a",
		"a,b,c":     "a, b, c",
		"a,b,c,d":   "a, b, c, and 1 more",
		"a,b,c,d,e": "a, b, c, and 2 more",
		"x\ny,z":    `"x\ny", z`,
	} {
		if got := listPaths(strings.Split(paths, ",")); got != want {
			t.Errorf("listPaths(%q) = %q, want %q", paths, got, want)
		}
	}
	for s, want := range map[string]bool{
		"manifest: update lib to v1":                  true,
		"manifest: update lib to v1.":                 false,
		"manifest: update lib to v1!":                 false,
		"manifest: update lib ":                       false,
		"manifest: update a\tb":                       false,
		"manifest: update WIP":                        false,
		"manifest: update wip_x":                      true,
		"manifest: update x-Wip-y":                    false,
		"manifest: update wipe":                       true,
		"manifest: update " + strings.Repeat("ż", 58): true,
		"manifest: update " + strings.Repeat("ż", 59): false,
	} {
		if got := validSubject(s); got != want {
			t.Errorf("validSubject(%q) = %v, want %v", s, got, want)
		}
	}
	r := &Repo{}
	if got := r.displayPath("/top/lib", "lib"); got != "lib" {
		t.Errorf("displayPath without a directory = %q", got)
	}
}
