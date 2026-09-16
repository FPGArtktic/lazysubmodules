// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package lock_test

import (
	"context"
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

// Well-formed commits of both object formats.
const (
	sha1A   = "a1b2c3d4e5f6a7b8c9d0a1b2c3d4e5f6a7b8c9d0"
	sha1B   = "0123456789abcdef0123456789abcdef01234567"
	sha256A = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
)

// sample covers dotted and mixed-case names, mixed-case section and
// variable names, unknown variables and sections, split sections, repeated
// variables and both commit lengths.
const sample = `# LazySubmodules test lock
[core]
	commit = ignored
[submodule "kernel"]
	mode = tag
	ref = v6.6.7
	commit = 0000000000000000000000000000000000000000
	future = ignored
[Submodule "Kernel"]
	MODE = commit
	Ref = ` + sha1B + `
	COMMIT = ` + sha1B + `
[submodule "vendor.lib.v2"]
	mode = branch
	ref = release/2.x
	commit = ` + sha256A + `
[submodule]
	mode = ignored
[submodule "kernel"]
	mode = tag-pattern
	ref = v6.6.8
	commit = ` + sha1A + `
`

// sampleEntries returns what Load returns for sample.
func sampleEntries() []lock.Entry {
	return []lock.Entry{
		{Name: "kernel", Mode: manifest.ModeTagPattern, Ref: "v6.6.8", Commit: sha1A},
		{Name: "Kernel", Mode: manifest.ModeCommit, Ref: sha1B, Commit: sha1B},
		{Name: "vendor.lib.v2", Mode: manifest.ModeBranch, Ref: "release/2.x", Commit: sha256A},
	}
}

// newRepo creates a repository whose lock file has the given content; an
// empty content means no lock file.
func newRepo(t *testing.T, content string) string {
	t.Helper()
	dir := gittest.InitRepo(t, gittest.SHA1)
	if content != "" {
		gittest.WriteFile(t, filepath.Join(dir, lock.File), content)
	}
	return dir
}

// readLock returns the content of the lock file in dir.
func readLock(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, lock.File))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// lockExists reports whether dir has a lock file.
func lockExists(t *testing.T, dir string) bool {
	t.Helper()
	_, err := os.Stat(filepath.Join(dir, lock.File))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	return err == nil
}

// load calls Load and fails the test on error.
func load(t *testing.T, g *git.Runner, dir string) *lock.Lock {
	t.Helper()
	l, err := lock.Load(t.Context(), g, dir)
	if err != nil {
		t.Fatalf("Load = %v", err)
	}
	if l == nil {
		t.Fatal("Load returned a nil lock without an error")
	}
	return l
}

func TestLoadMissingFile(t *testing.T) {
	t.Parallel()
	g := gittest.Runner(t)
	for name, dir := range map[string]string{
		"repository":        newRepo(t, ""),
		"missing directory": filepath.Join(t.TempDir(), "missing"),
	} {
		l := load(t, g, dir)
		if got := l.Entries(); len(got) != 0 {
			t.Errorf("%s: Entries = %+v, want none", name, got)
		}
		if e, ok := l.Get("kernel"); ok || e != (lock.Entry{}) {
			t.Errorf("%s: Get = %+v, %t; want zero, false", name, e, ok)
		}
		if lockExists(t, dir) {
			t.Errorf("%s: Load created %s", name, lock.File)
		}
	}
}

func TestLoadWithoutEntries(t *testing.T) {
	t.Parallel()
	g := gittest.Runner(t)
	for _, content := range []string{
		"\n",
		"# only a comment\n",
		"[core]\n\tbare = false\n[submodule]\n\tmode = tag\n",
		"[submodule \"empty\"]\n",
	} {
		l := load(t, g, newRepo(t, content))
		if got := l.Entries(); len(got) != 0 {
			t.Errorf("Load(%q).Entries = %+v, want none", content, got)
		}
	}
}

func TestLoad(t *testing.T) {
	t.Parallel()
	l := load(t, gittest.Runner(t), newRepo(t, sample))
	want := sampleEntries()
	if got := l.Entries(); !slices.Equal(got, want) {
		t.Errorf("Entries =\n%+v\nwant\n%+v", got, want)
	}
	for _, e := range want {
		if got, ok := l.Get(e.Name); !ok || got != e {
			t.Errorf("Get(%q) = %+v, %t; want %+v, true", e.Name, got, ok, e)
		}
	}
	for _, name := range []string{"KERNEL", "vendor", "lib.v2", "", "core"} {
		if got, ok := l.Get(name); ok {
			t.Errorf("Get(%q) = %+v, true; want false", name, got)
		}
	}
}

func TestEntriesReturnsCopy(t *testing.T) {
	t.Parallel()
	l := load(t, gittest.Runner(t), newRepo(t, sample))
	entries := l.Entries()
	entries[0].Commit = sha1B
	entries[1] = lock.Entry{}
	if got := l.Entries(); !slices.Equal(got, sampleEntries()) {
		t.Errorf("Entries changed through a returned slice: %+v", got)
	}
	if got, _ := l.Get("kernel"); got.Commit != sha1A {
		t.Errorf("Get changed through a returned slice: %+v", got)
	}
}

func TestZeroLock(t *testing.T) {
	t.Parallel()
	var l lock.Lock
	if got := l.Entries(); got != nil {
		t.Errorf("Entries = %+v, want nil", got)
	}
	if got, ok := l.Get("kernel"); ok {
		t.Errorf("Get = %+v, true; want false", got)
	}
}

func TestLoadInvalidEntry(t *testing.T) {
	t.Parallel()
	const (
		valid = "[submodule \"good\"]\n\tmode = tag\n\tref = v1\n\tcommit = " + sha1A + "\n"
		name  = "bad.Entry"
	)
	tests := []struct {
		name    string
		content string
		mode    bool // the error also wraps manifest.ErrInvalidMode
	}{
		{"unknown mode", "mode = Tag\n\tref = v1\n\tcommit = " + sha1A, true},
		{"missing mode", "ref = v1\n\tcommit = " + sha1A, true},
		{"empty mode", "mode =\n\tref = v1\n\tcommit = " + sha1A, true},
		{"missing ref", "mode = tag\n\tcommit = " + sha1A, false},
		{"option ref", "mode = branch\n\tref = --orphan\n\tcommit = " + sha1A, false},
		{"spaced ref", "mode = tag\n\tref = \"v1 \"\n\tcommit = " + sha1A, false},
		{"control ref", "mode = tag\n\tref = \"v\\t1\"\n\tcommit = " + sha1A, false},
		{"missing commit", "mode = tag\n\tref = v1", false},
		{"short commit", "mode = tag\n\tref = v1\n\tcommit = a1b2c3d", false},
		{"39 digits", "mode = tag\n\tref = v1\n\tcommit = " + sha1A[1:], false},
		{"41 digits", "mode = tag\n\tref = v1\n\tcommit = " + sha1A + "0", false},
		{"63 digits", "mode = tag\n\tref = v1\n\tcommit = " + sha256A[1:], false},
		{"uppercase", "mode = tag\n\tref = v1\n\tcommit = " + strings.ToUpper(sha1A), false},
		{"not hex", "mode = tag\n\tref = v1\n\tcommit = " + strings.Repeat("g", 40), false},
		{"unknown only", "future = 1", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			content := valid + "[submodule \"" + name + "\"]\n\t" + tt.content + "\n" + valid
			checkInvalid(t, content, name, tt.mode)
		})
	}
	t.Run("empty name", func(t *testing.T) {
		t.Parallel()
		checkInvalid(t, strings.Replace(valid, "good", "", 1), "", false)
	})
}

// checkInvalid asserts that Load rejects content because of the named entry.
func checkInvalid(t *testing.T, content, entry string, mode bool) {
	t.Helper()
	l, err := lock.Load(t.Context(), gittest.Runner(t), newRepo(t, content))
	if !errors.Is(err, lock.ErrInvalidEntry) || l != nil {
		t.Fatalf("Load = %v, %v; want nil, %v", l, err, lock.ErrInvalidEntry)
	}
	if want := lock.File + ": invalid lock entry \"" + entry + "\": "; !strings.HasPrefix(
		err.Error(), want) {
		t.Errorf("error %q does not start with %q", err, want)
	}
	if got := errors.Is(err, manifest.ErrInvalidMode); got != mode {
		t.Errorf("errors.Is(%v, manifest.ErrInvalidMode) = %t, want %t", err, got, mode)
	}
}

func TestLoadGitError(t *testing.T) {
	t.Parallel()
	dir := newRepo(t, "[submodule \"kernel\"\n\tmode = tag\n")
	l, err := lock.Load(t.Context(), gittest.Runner(t), dir)
	if gitErr, ok := errors.AsType[*git.Error](err); !ok || l != nil {
		t.Fatalf("Load = %v, %v; want *git.Error", l, err)
	} else if gitErr.ExitCode <= 0 {
		t.Errorf("exit code = %d, want > 0", gitErr.ExitCode)
	}
	if errors.Is(err, lock.ErrInvalidEntry) {
		t.Errorf("syntax error reported as %v", lock.ErrInvalidEntry)
	}
	if !strings.HasPrefix(err.Error(), "read "+lock.File+": ") {
		t.Errorf("error %q does not name %s", err, lock.File)
	}
}

func TestLoadCanceled(t *testing.T) {
	t.Parallel()
	dir := newRepo(t, sample)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := lock.Load(ctx, gittest.Runner(t), dir); !errors.Is(err, context.Canceled) {
		t.Errorf("Load(canceled) = %v, want %v", err, context.Canceled)
	}
}
