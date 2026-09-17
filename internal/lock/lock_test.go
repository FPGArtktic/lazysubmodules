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

// commitAll stages every change in dir and commits it.
func commitAll(t *testing.T, dir, msg string) {
	t.Helper()
	gittest.Git(t, dir, "add", "--all")
	gittest.Git(t, dir, "commit", "--quiet", "--message="+msg)
}

func TestLoadRev(t *testing.T) {
	t.Parallel()
	for _, format := range []string{gittest.SHA1, gittest.SHA256} {
		t.Run(format, func(t *testing.T) {
			t.Parallel()
			g := gittest.Runner(t)
			ctx := t.Context()
			dir := gittest.InitRepo(t, format)
			gittest.WriteFile(t, filepath.Join(dir, "README"), "x\n")
			commitAll(t, dir, "initial commit")
			gittest.WriteFile(t, filepath.Join(dir, lock.File), sample)
			commitAll(t, dir, "add lock")
			staged := lock.Entry{Name: "kernel", Mode: manifest.ModeTag, Ref: "v6.7", Commit: sha1B}
			if err := lock.Write(ctx, g, dir, staged); err != nil {
				t.Fatal(err)
			}
			gittest.Git(t, dir, "add", lock.File)
			worktree := lock.Entry{Name: "kernel", Mode: manifest.ModeTag, Ref: "v6.8", Commit: sha256A}
			if err := lock.Write(ctx, g, dir, worktree); err != nil {
				t.Fatal(err)
			}

			want := map[string][]lock.Entry{
				"HEAD":   sampleEntries(),
				"":       sampleEntries(),
				"HEAD~1": nil,
			}
			want[""][0] = staged
			for rev, entries := range want {
				l, err := lock.LoadRev(ctx, g, dir, rev)
				if err != nil {
					t.Errorf("LoadRev(%q) = %v", rev, err)
					continue
				}
				if got := l.Entries(); !slices.Equal(got, entries) {
					t.Errorf("LoadRev(%q).Entries =\n%+v\nwant\n%+v", rev, got, entries)
				}
			}
			if got, _ := load(t, g, dir).Get("kernel"); got != worktree {
				t.Errorf("Load: kernel = %+v, want %+v", got, worktree)
			}
		})
	}
}

func TestLoadRevUnborn(t *testing.T) {
	t.Parallel()
	g := gittest.Runner(t)
	ctx := t.Context()
	dir := newRepo(t, sample)
	l, err := lock.LoadRev(ctx, g, dir, "")
	if err != nil || l == nil || l.Entries() != nil {
		t.Errorf("LoadRev(empty index) = %+v, %v; want an empty lock", l, err)
	}
	gittest.Git(t, dir, "add", lock.File)
	l, err = lock.LoadRev(ctx, g, dir, "HEAD")
	if l != nil || !errors.Is(err, git.ErrRefNotFound) {
		t.Errorf("LoadRev(unborn HEAD) = %+v, %v; want ErrRefNotFound", l, err)
	}
	l, err = lock.LoadRev(ctx, g, dir, "")
	if err != nil || !slices.Equal(l.Entries(), sampleEntries()) {
		t.Errorf("LoadRev(index) = %+v, %v", l, err)
	}
}

func TestLoadRevErrors(t *testing.T) {
	t.Parallel()
	g := gittest.Runner(t)
	ctx := t.Context()
	commit := sha1A
	dir := newRepo(t, "[submodule \"bad\"]\n\tmode = tag\n\tref = v1\n"+
		"[submodule \"good\"]\n\tmode = tag\n\tref = v1\n\tcommit = "+commit+"\n"+
		"[submodule \"worse\"]\n\tmode = other\n\tref = v1\n\tcommit = "+commit+"\n")
	commitAll(t, dir, "add lock")
	// The recorded files keep the valid entries, the working tree copy none.
	valid := []lock.Entry{{Name: "good", Mode: manifest.ModeTag, Ref: "v1", Commit: commit}}
	for rev, prefix := range map[string]string{
		"HEAD": `HEAD:.lsm.lock: invalid lock entry "bad": `,
		"":     `:.lsm.lock: invalid lock entry "bad": `,
	} {
		l, err := lock.LoadRev(ctx, g, dir, rev)
		if l == nil || !slices.Equal(l.Entries(), valid) || !errors.Is(err, lock.ErrInvalidEntry) ||
			!strings.HasPrefix(err.Error(), prefix) {
			t.Errorf("LoadRev(%q) = %+v, %v; want %+v and ErrInvalidEntry starting with %q",
				rev, l, err, valid, prefix)
			continue
		}
		if _, ok := l.Get("bad"); ok {
			t.Errorf("LoadRev(%q) has the invalid entry", rev)
		}
		if e, ok := l.Get("good"); !ok || e != valid[0] {
			t.Errorf("LoadRev(%q).Get(good) = %+v, %t", rev, e, ok)
		}
	}
	if l, err := lock.Load(ctx, g, dir); l != nil || !errors.Is(err, lock.ErrInvalidEntry) {
		t.Errorf("Load = %+v, %v; want ErrInvalidEntry", l, err)
	}
	tests := map[string]error{
		"nope":   git.ErrRefNotFound,
		"HEAD~1": git.ErrRefNotFound,
		"-x":     git.ErrInvalidRefName,
	}
	for rev, want := range tests {
		l, err := lock.LoadRev(ctx, g, dir, rev)
		if l != nil || !errors.Is(err, want) ||
			!strings.HasPrefix(err.Error(), "read "+rev+":"+lock.File+": ") {
			t.Errorf("LoadRev(%q) = %+v, %v; want %v", rev, l, err, want)
		}
	}

	// A lock file committed as a symbolic link.
	other := gittest.InitRepo(t, gittest.SHA1)
	gittest.WriteFile(t, filepath.Join(other, "target"), sample)
	if err := os.Symlink("target", filepath.Join(other, lock.File)); err != nil {
		t.Fatal(err)
	}
	commitAll(t, other, "add a link")
	for _, rev := range []string{"HEAD", ""} {
		l, err := lock.LoadRev(ctx, g, other, rev)
		if l != nil || !errors.Is(err, git.ErrNotRegularFile) {
			t.Errorf("LoadRev(%q, link) = %+v, %v; want ErrNotRegularFile", rev, l, err)
		}
	}
	// The file is missing from HEAD.
	gittest.Git(t, other, "rm", "--quiet", lock.File)
	commitAll(t, other, "remove the link")
	l, err := lock.LoadRev(ctx, g, other, "HEAD")
	if err != nil || l == nil || l.Entries() != nil {
		t.Errorf("LoadRev(no lock file) = %+v, %v; want an empty lock", l, err)
	}
}
