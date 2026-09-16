// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package lock_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/FPGArtktic/lazysubmodules/internal/git"
	"github.com/FPGArtktic/lazysubmodules/internal/git/gittest"
	"github.com/FPGArtktic/lazysubmodules/internal/lock"
	"github.com/FPGArtktic/lazysubmodules/internal/manifest"
)

// write calls Write and fails the test on error.
func write(t *testing.T, g *git.Runner, dir string, e lock.Entry) {
	t.Helper()
	if err := lock.Write(t.Context(), g, dir, e); err != nil {
		t.Fatalf("Write(%+v) = %v", e, err)
	}
}

// remove calls Remove and fails the test on error.
func remove(t *testing.T, g *git.Runner, dir, name string) {
	t.Helper()
	if err := lock.Remove(t.Context(), g, dir, name); err != nil {
		t.Fatalf("Remove(%q) = %v", name, err)
	}
}

// names returns the names of the entries of l.
func names(l *lock.Lock) []string {
	var out []string
	for _, e := range l.Entries() {
		out = append(out, e.Name)
	}
	return out
}

func TestWriteLayout(t *testing.T) {
	t.Parallel()
	dir := newRepo(t, "")
	write(t, gittest.Runner(t), dir, lock.Entry{
		Name:   "kernel",
		Mode:   manifest.ModeTagPattern,
		Ref:    "v6.6.8",
		Commit: sha1A,
	})
	// The lock file example of README.md.
	want := "[submodule \"kernel\"]\n" +
		"\tmode = tag-pattern\n" +
		"\tref = v6.6.8\n" +
		"\tcommit = " + sha1A + "\n"
	if got := readLock(t, dir); got != want {
		t.Errorf("lock file =\n%s\nwant\n%s", got, want)
	}
}

func TestWriteLoadRoundTrip(t *testing.T) {
	t.Parallel()
	for _, format := range []string{gittest.SHA1, gittest.SHA256} {
		t.Run(format, func(t *testing.T) {
			t.Parallel()
			g := gittest.Runner(t)
			dir := gittest.InitRepo(t, format)
			gittest.Git(t, dir, "commit", "--quiet", "--allow-empty", "-m", "initial")
			head := gittest.Git(t, dir, "rev-parse", "HEAD")
			if !lock.ValidCommit(head) {
				t.Fatalf("ValidCommit(%q) = false for a %s commit", head, format)
			}
			want := []lock.Entry{
				{Name: "u-boot", Mode: manifest.ModeBranch, Ref: "release/2.x", Commit: head},
				{Name: "kernel", Mode: manifest.ModeTag, Ref: "v6.6.8", Commit: head},
				{Name: "fpga.ip", Mode: manifest.ModeTagPattern, Ref: "v2.1.0-rc.1", Commit: head},
				{Name: "With Space", Mode: manifest.ModeCommit, Ref: head, Commit: head},
				{Name: `quo"te\`, Mode: manifest.ModeTag, Ref: "ünïcode/v1", Commit: head},
			}
			for _, e := range want {
				write(t, g, dir, e)
			}
			l := load(t, g, dir)
			if got := l.Entries(); !slices.Equal(got, want) {
				t.Errorf("Entries =\n%+v\nwant\n%+v", got, want)
			}
			for _, e := range want {
				if got, ok := l.Get(e.Name); !ok || got != e {
					t.Errorf("Get(%q) = %+v, %t; want %+v, true", e.Name, got, ok, e)
				}
			}
		})
	}
}

func TestWriteKeepsFileOrder(t *testing.T) {
	t.Parallel()
	g := gittest.Runner(t)
	dir := newRepo(t, "")
	order := []string{"zeta", "alpha", "mid"}
	for _, name := range order {
		write(t, g, dir, lock.Entry{Name: name, Mode: manifest.ModeTag, Ref: "v1", Commit: sha1A})
	}
	if got := names(load(t, g, dir)); !slices.Equal(got, order) {
		t.Errorf("entries = %q, want %q", got, order)
	}
	write(t, g, dir, lock.Entry{Name: "alpha", Mode: manifest.ModeTag, Ref: "v2", Commit: sha1B})
	l := load(t, g, dir)
	if got := names(l); !slices.Equal(got, order) {
		t.Errorf("entries after overwrite = %q, want %q", got, order)
	}
	if got, _ := l.Get("alpha"); got.Ref != "v2" || got.Commit != sha1B {
		t.Errorf("overwritten entry = %+v", got)
	}
}

func TestWriteOverwrite(t *testing.T) {
	t.Parallel()
	g := gittest.Runner(t)
	dir := newRepo(t, `[submodule "first"]
	mode = tag
	ref = v1
	commit = `+sha1A+`
[Submodule "kernel"]
	Mode = commit
	ref = `+sha1B+`
	COMMIT = `+sha1B+`
	commit = `+sha1B+`
	future = kept
[submodule "last"]
	mode = branch
	ref = main
	commit = `+sha1A+`
[submodule "kernel"]
	ref = `+sha1B+`
`)
	want := lock.Entry{Name: "kernel", Mode: manifest.ModeTagPattern, Ref: "v6.6.8", Commit: sha1A}
	write(t, g, dir, want)

	l := load(t, g, dir)
	wantEntries := []lock.Entry{
		{Name: "first", Mode: manifest.ModeTag, Ref: "v1", Commit: sha1A},
		want,
		{Name: "last", Mode: manifest.ModeBranch, Ref: "main", Commit: sha1A},
	}
	if got := l.Entries(); !slices.Equal(got, wantEntries) {
		t.Errorf("Entries =\n%+v\nwant\n%+v", got, wantEntries)
	}
	for _, variable := range []string{"mode", "ref", "commit"} {
		values := gittest.Git(t, dir, "config", "-f", lock.File, "--get-all",
			"submodule.kernel."+variable)
		if strings.Contains(values, "\n") {
			t.Errorf("%s has several values after Write:\n%s", variable, values)
		}
	}
	future := gittest.Git(t, dir, "config", "-f", lock.File, "submodule.kernel.future")
	if future != "kept" {
		t.Errorf("unknown variable = %q, want kept", future)
	}
}

func TestWriteInvalidEntry(t *testing.T) {
	t.Parallel()
	valid := lock.Entry{Name: "kernel", Mode: manifest.ModeTag, Ref: "v1", Commit: sha1A}
	tests := map[string]func(e *lock.Entry){
		"empty name":      func(e *lock.Entry) { e.Name = "" },
		"newline in name": func(e *lock.Entry) { e.Name = "ker\nnel" },
		"NUL in name":     func(e *lock.Entry) { e.Name = "ker\x00nel" },
		"unknown mode":    func(e *lock.Entry) { e.Mode = "tags" },
		"empty mode":      func(e *lock.Entry) { e.Mode = "" },
		"empty ref":       func(e *lock.Entry) { e.Ref = "" },
		"option ref":      func(e *lock.Entry) { e.Ref = "-v1" },
		"ref with space":  func(e *lock.Entry) { e.Ref = "v 1" },
		"ref with LF":     func(e *lock.Entry) { e.Ref = "v1\n" },
		"ref with DEL":    func(e *lock.Entry) { e.Ref = "v1\x7f" },
		"empty commit":    func(e *lock.Entry) { e.Commit = "" },
		"short commit":    func(e *lock.Entry) { e.Commit = sha1A[:12] },
		"uppercase":       func(e *lock.Entry) { e.Commit = strings.ToUpper(sha1A) },
		"65 digits":       func(e *lock.Entry) { e.Commit = sha256A + "0" },
	}
	for name, change := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			e := valid
			change(&e)
			checkWriteRejected(t, "", e)
			checkWriteRejected(t, sample, e)
		})
	}
}

// checkWriteRejected asserts that Write rejects e without touching a lock
// file with the given content (empty: no lock file).
func checkWriteRejected(t *testing.T, content string, e lock.Entry) {
	t.Helper()
	dir := newRepo(t, content)
	err := lock.Write(t.Context(), gittest.Runner(t), dir, e)
	if !errors.Is(err, lock.ErrInvalidEntry) {
		t.Fatalf("Write(%+v) = %v, want %v", e, err, lock.ErrInvalidEntry)
	}
	if want := "invalid lock entry " + strconv.Quote(e.Name); !strings.Contains(err.Error(), want) {
		t.Errorf("error %q does not contain %q", err, want)
	}
	switch {
	case content == "" && lockExists(t, dir):
		t.Errorf("Write created %s", lock.File)
	case content != "" && readLock(t, dir) != content:
		t.Errorf("Write changed %s", lock.File)
	}
}

// lockLockFile creates the lock file of git for the lock file in dir, which
// makes every change of the lock file fail.
func lockLockFile(t *testing.T, dir string) {
	t.Helper()
	gittest.WriteFile(t, filepath.Join(dir, lock.File+".lock"), "")
}

func TestWriteGitError(t *testing.T) {
	t.Parallel()
	dir := newRepo(t, sample)
	lockLockFile(t, dir)
	e := lock.Entry{Name: "kernel", Mode: manifest.ModeTag, Ref: "v1", Commit: sha1A}
	err := lock.Write(t.Context(), gittest.Runner(t), dir, e)
	if _, ok := errors.AsType[*git.Error](err); !ok {
		t.Fatalf("Write = %v, want *git.Error", err)
	}
	if want := "write " + lock.File + ": submodule \"kernel\": "; !strings.HasPrefix(
		err.Error(), want) {
		t.Errorf("error %q does not start with %q", err, want)
	}
	if readLock(t, dir) != sample {
		t.Errorf("Write changed the locked file")
	}
}

func TestRemoveGitError(t *testing.T) {
	t.Parallel()
	dir := newRepo(t, sample)
	lockLockFile(t, dir)
	err := lock.Remove(t.Context(), gittest.Runner(t), dir, "vendor.lib.v2")
	if _, ok := errors.AsType[*git.Error](err); !ok {
		t.Fatalf("Remove = %v, want *git.Error", err)
	}
	if want := "remove from " + lock.File + ": submodule \"vendor.lib.v2\": "; !strings.HasPrefix(
		err.Error(), want) {
		t.Errorf("error %q does not start with %q", err, want)
	}
	if readLock(t, dir) != sample {
		t.Errorf("Remove changed the locked file")
	}

	// Reading the file fails first for a file git cannot parse.
	dir = newRepo(t, "[submodule \"kernel\"\n")
	err = lock.Remove(t.Context(), gittest.Runner(t), dir, "kernel")
	if gitErr, ok := errors.AsType[*git.Error](err); !ok || !slices.Contains(gitErr.Args, "--list") {
		t.Errorf("Remove(invalid file) = %v, want the *git.Error of the listing", err)
	}
}

func TestLockFileNotRegular(t *testing.T) {
	t.Parallel()
	g := gittest.Runner(t)
	outside := t.TempDir()
	target := filepath.Join(outside, "target")
	gittest.WriteFile(t, target, sample)
	e := lock.Entry{Name: "$(touch pwned)", Mode: manifest.ModeTag, Ref: "v1", Commit: sha1A}
	for name, setup := range map[string]func(string) error{
		"symlink":   func(p string) error { return os.Symlink(target, p) },
		"dangling":  func(p string) error { return os.Symlink(filepath.Join(outside, "new"), p) },
		"directory": func(p string) error { return os.Mkdir(p, 0o755) },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir := newRepo(t, "")
			if err := setup(filepath.Join(dir, lock.File)); err != nil {
				t.Fatal(err)
			}
			_, loadErr := lock.Load(t.Context(), g, dir)
			for op, err := range map[string]error{
				"Load":   loadErr,
				"Write":  lock.Write(t.Context(), g, dir, e),
				"Remove": lock.Remove(t.Context(), g, dir, "kernel"),
			} {
				if !errors.Is(err, git.ErrNotRegularFile) {
					t.Errorf("%s = %v, want %v", op, err, git.ErrNotRegularFile)
				}
			}
		})
	}
	t.Cleanup(func() {
		if got, err := os.ReadFile(target); err != nil || string(got) != sample {
			t.Errorf("symlink target changed: %q, %v", got, err)
		}
		if _, err := os.Lstat(filepath.Join(outside, "new")); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("dangling symlink target created: %v", err)
		}
	})
}

func TestRemove(t *testing.T) {
	t.Parallel()
	g := gittest.Runner(t)
	const (
		first = "[submodule \"first\"]\n\tmode = tag\n\tref = v1\n\tcommit = " + sha1A + "\n"
		last  = "[submodule \"vendor.lib\"]\n\tmode = tag\n\tref = v2\n\tcommit = " + sha1B + "\n"
	)
	dir := newRepo(t, first+
		"[submodule \"vendor.lib.v2\"]\n\tmode = tag\n\tref = v3\n\tcommit = "+sha1A+"\n"+
		last)
	remove(t, g, dir, "vendor.lib.v2")
	if got, want := readLock(t, dir), first+last; got != want {
		t.Errorf("lock file =\n%s\nwant\n%s", got, want)
	}
	remove(t, g, dir, "first")
	remove(t, g, dir, "vendor.lib")
	if l := load(t, g, dir); len(l.Entries()) != 0 {
		t.Errorf("entries after removing all = %+v", l.Entries())
	}
	e := lock.Entry{Name: "first", Mode: manifest.ModeTag, Ref: "v1", Commit: sha1A}
	write(t, g, dir, e)
	if got := readLock(t, dir); got != first {
		t.Errorf("lock file after Write =\n%s\nwant\n%s", got, first)
	}
}

func TestRemoveMissing(t *testing.T) {
	t.Parallel()
	g := gittest.Runner(t)
	dir := newRepo(t, sample)
	for _, name := range []string{"missing", "KERNEL", "vendor.lib", "lib.v2", "core"} {
		remove(t, g, dir, name)
		if readLock(t, dir) != sample {
			t.Fatalf("Remove(%q) changed the lock file:\n%s", name, readLock(t, dir))
		}
	}
	empty := newRepo(t, "")
	remove(t, g, empty, "kernel")
	if lockExists(t, empty) {
		t.Errorf("Remove created %s", lock.File)
	}
	remove(t, g, filepath.Join(t.TempDir(), "missing"), "kernel")
}

func TestRemoveEverySection(t *testing.T) {
	t.Parallel()
	g := gittest.Runner(t)
	dir := newRepo(t, sample+`[SUBMODULE "kernel"]
	future = removed
[submodule.kernel]
	mode = tag
[submodule "kernel"]
`)
	remove(t, g, dir, "kernel")
	l := load(t, g, dir)
	want := sampleEntries()[1:]
	if got := l.Entries(); !slices.Equal(got, want) {
		t.Errorf("Entries =\n%+v\nwant\n%+v", got, want)
	}
	lines := gittest.Git(t, dir, "config", "-f", lock.File, "--list")
	if strings.Contains(lines, "submodule.kernel.") {
		t.Errorf("variables of kernel are left:\n%s", lines)
	}
}

func TestRemoveInvalidName(t *testing.T) {
	t.Parallel()
	dir := newRepo(t, sample)
	for _, name := range []string{"", "ker\nnel", "ker\x00nel"} {
		err := lock.Remove(t.Context(), gittest.Runner(t), dir, name)
		if !errors.Is(err, lock.ErrInvalidEntry) {
			t.Errorf("Remove(%q) = %v, want %v", name, err, lock.ErrInvalidEntry)
		}
	}
	if readLock(t, dir) != sample {
		t.Errorf("Remove changed the lock file")
	}
}
