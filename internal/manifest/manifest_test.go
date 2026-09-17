// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package manifest_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/FPGArtktic/lazysubmodules/internal/git"
	"github.com/FPGArtktic/lazysubmodules/internal/git/gittest"
	"github.com/FPGArtktic/lazysubmodules/internal/manifest"
)

// sample covers dotted and mixed-case names, unknown keys and sections,
// unmanaged entries, split sections and repeated variables.
const sample = `# LazySubmodules test manifest
[core]
	bare = false
[submodule "kernel"]
	path = kernel
	url = https://git.example.org/linux.git
	lsm-mode = tag-pattern
	lsm-ref = v6.5.*
	lsm-ref = v6.6.*
	update = checkout
	lsm-future = ignored
[submodule "Kernel"]
	PATH = kernel-mixed
	Url = ../Kernel.git
	LSM-Mode = commit
	Lsm-Ref = 0123456789abcdef0123456789abcdef01234567
[Submodule "late"]
	url = https://git.example.org/late.git
[submodule "u-boot"]
	path = u-boot
	url = https://git.example.org/u-boot.git
	branch = main
	lsm-mode = branch
	lsm-ref = main
[submodule "vendor.lib.v2"]
	path = third_party/lib
	url = https://git.example.org/lib.git
	lsm-mode = tag
	lsm-ref = v2.3.1
[submodule "plain"]
	path = plain
	url = https://git.example.org/plain.git
	branch = develop
	shallow = true
	ignore = dirty
[submodule "no-path"]
	url = https://git.example.org/no-path.git
	lsm-mode = bogus
[submodule]
	path = no-subsection
[submodule "with space"]
	path = dir with space
	url = "https://git.example.org/space.git"
[submodule "late"]
	path = late
[submodule "kernel"]
	branch = stale
`

// sampleSubmodules returns what Load returns for sample.
func sampleSubmodules() []manifest.Submodule {
	return []manifest.Submodule{
		{
			Name:   "kernel",
			Path:   "kernel",
			URL:    "https://git.example.org/linux.git",
			Branch: "stale",
			Mode:   manifest.ModeTagPattern,
			Ref:    "v6.6.*",
		},
		{
			Name: "Kernel",
			Path: "kernel-mixed",
			URL:  "../Kernel.git",
			Mode: manifest.ModeCommit,
			Ref:  "0123456789abcdef0123456789abcdef01234567",
		},
		{Name: "late", Path: "late", URL: "https://git.example.org/late.git"},
		{
			Name:   "u-boot",
			Path:   "u-boot",
			URL:    "https://git.example.org/u-boot.git",
			Branch: "main",
			Mode:   manifest.ModeBranch,
			Ref:    "main",
		},
		{
			Name: "vendor.lib.v2",
			Path: "third_party/lib",
			URL:  "https://git.example.org/lib.git",
			Mode: manifest.ModeTag,
			Ref:  "v2.3.1",
		},
		{
			Name:   "plain",
			Path:   "plain",
			URL:    "https://git.example.org/plain.git",
			Branch: "develop",
		},
		{Name: "with space", Path: "dir with space", URL: "https://git.example.org/space.git"},
	}
}

// newRepo creates a repository whose .gitmodules has the given content.
func newRepo(t *testing.T, content string) string {
	t.Helper()
	dir := gittest.InitRepo(t, gittest.SHA1)
	gittest.WriteFile(t, filepath.Join(dir, manifest.File), content)
	return dir
}

// readManifest returns the content of .gitmodules in dir.
func readManifest(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, manifest.File))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// load calls Load and fails the test on error.
func load(t *testing.T, g *git.Runner, dir string) []manifest.Submodule {
	t.Helper()
	subs, err := manifest.Load(t.Context(), g, dir)
	if err != nil {
		t.Fatalf("Load = %v", err)
	}
	return subs
}

func TestLoad(t *testing.T) {
	t.Parallel()
	dir := newRepo(t, sample)
	got := load(t, gittest.Runner(t), dir)
	if want := sampleSubmodules(); !slices.Equal(got, want) {
		t.Errorf("Load =\n%+v\nwant\n%+v", got, want)
	}
	managed := 0
	for _, sub := range got {
		if sub.Managed() {
			managed++
		}
	}
	if managed != 4 {
		t.Errorf("managed submodules = %d, want 4", managed)
	}
}

func TestLoadSkipsEntriesIgnoredByGit(t *testing.T) {
	t.Parallel()
	const content = `[submodule ""]
	path = empty-name
[submodule ".."]
	path = dotdot
[submodule "../escape"]
	path = escape
[submodule "a/../b"]
	path = inner-dotdot
[submodule "a\\..\\b"]
	path = backslash-dotdot
[submodule "empty-path"]
	path =
	lsm-mode = bogus
[submodule "implicit-path"]
	path
[submodule "absolute"]
	path = /etc
[submodule "parent"]
	path = ../outside
[submodule "nested-parent"]
	path = a/../../outside
[submodule "top"]
	path = .
[submodule "unclean"]
	path = a//b
[submodule "dot-component"]
	path = ./a
[submodule "trailing-slash"]
	path = a/
[submodule "option-path"]
	path = --output=x
[submodule "..ok../a..b"]
	path = sub/dots..in..name
	url = https://git.example.org/good.git
	url = -u/bad
	path = -ignored
[submodule "option-url"]
	path = option-url
	url = --upload-pack=touch
`
	dir := newRepo(t, content)
	want := []manifest.Submodule{
		{
			Name: "..ok../a..b",
			Path: "sub/dots..in..name",
			URL:  "https://git.example.org/good.git",
		},
		{Name: "option-url", Path: "option-url"},
	}
	got := load(t, gittest.Runner(t), dir)
	if !slices.Equal(got, want) {
		t.Errorf("Load =\n%+v\nwant\n%+v", got, want)
	}
}

func TestLoadMissingOrEmptyFile(t *testing.T) {
	t.Parallel()
	g := gittest.Runner(t)
	dir := gittest.InitRepo(t, gittest.SHA1)
	if got, err := manifest.Load(t.Context(), g, dir); got != nil || err != nil {
		t.Errorf("Load(missing) = %+v, %v; want nil, nil", got, err)
	}
	gittest.WriteFile(t, filepath.Join(dir, manifest.File), "")
	if got, err := manifest.Load(t.Context(), g, dir); got != nil || err != nil {
		t.Errorf("Load(empty) = %+v, %v; want nil, nil", got, err)
	}
	gittest.WriteFile(t, filepath.Join(dir, manifest.File), "[submodule \"x\"]\n\turl = u\n")
	if got, err := manifest.Load(t.Context(), g, dir); got != nil || err != nil {
		t.Errorf("Load(no path) = %+v, %v; want nil, nil", got, err)
	}
}

func TestLoadInvalidMode(t *testing.T) {
	t.Parallel()
	g := gittest.Runner(t)
	values := map[string]string{
		"unknown":   "lsm-mode = bogus",
		"case":      "lsm-mode = Branch",
		"empty":     "lsm-mode =",
		"implicit":  "lsm-mode",
		"last-wins": "lsm-mode = tag\n\tlsm-mode = tags",
	}
	for label, line := range values {
		content := "[submodule \"ok\"]\n\tpath = ok\n\tlsm-mode = tag\n" +
			"[submodule \"bad.one\"]\n\tpath = bad\n\t" + line + "\n"
		dir := newRepo(t, content)
		got, err := manifest.Load(t.Context(), g, dir)
		if got != nil || !errors.Is(err, manifest.ErrInvalidMode) {
			t.Errorf("%s: Load = %+v, %v; want ErrInvalidMode", label, got, err)
			continue
		}
		if !strings.HasPrefix(err.Error(), `.gitmodules: submodule "bad.one": lsm-mode: `) {
			t.Errorf("%s: error %q does not name the submodule", label, err)
		}
	}
}

func TestLoadInvalidFile(t *testing.T) {
	t.Parallel()
	dir := newRepo(t, "[submodule \"x\"\n\tpath = x\n")
	got, err := manifest.Load(t.Context(), gittest.Runner(t), dir)
	if _, ok := errors.AsType[*git.Error](err); got != nil || !ok {
		t.Errorf("Load = %+v, %v; want *git.Error", got, err)
	}
}

func TestLoadAddedSubmodule(t *testing.T) {
	t.Parallel()
	g := gittest.Runner(t)
	up := gittest.NewUpstream(t, gittest.SHA1)
	super := gittest.NewSuper(t, gittest.SHA1)
	super.AddSubmodule(t, "lib", up)
	super.AddSubmodule(t, "Other.Lib", up)

	want := []manifest.Submodule{
		{Name: "lib", Path: "lib", URL: up.Bare},
		{Name: "Other.Lib", Path: "Other.Lib", URL: up.Bare},
	}
	if got := load(t, g, super.Dir); !slices.Equal(got, want) {
		t.Errorf("Load =\n%+v\nwant\n%+v", got, want)
	}

	super.SetKey(t, "Other.Lib", manifest.KeyMode, "tag-pattern")
	super.SetKey(t, "Other.Lib", manifest.KeyRef, "v1.*")
	want[1].Mode, want[1].Ref = manifest.ModeTagPattern, "v1.*"
	if got := load(t, g, super.Dir); !slices.Equal(got, want) {
		t.Errorf("Load after SetKey =\n%+v\nwant\n%+v", got, want)
	}
}

func TestFind(t *testing.T) {
	t.Parallel()
	subs := sampleSubmodules()
	for _, want := range subs {
		got, ok := manifest.Find(subs, want.Name)
		if !ok || got != want {
			t.Errorf("Find(%q) = %+v, %t; want %+v, true", want.Name, got, ok, want)
		}
	}
	for _, name := range []string{"", "KERNEL", "vendor", "lib.v2", "u-boot "} {
		if got, ok := manifest.Find(subs, name); ok || got != (manifest.Submodule{}) {
			t.Errorf("Find(%q) = %+v, %t; want zero, false", name, got, ok)
		}
	}
	if got, ok := manifest.Find(nil, "kernel"); ok || got != (manifest.Submodule{}) {
		t.Errorf("Find(nil) = %+v, %t; want zero, false", got, ok)
	}
}

// commitAll stages every change in dir and commits it.
func commitAll(t *testing.T, dir, msg string) {
	t.Helper()
	gittest.Git(t, dir, "add", "--all")
	gittest.Git(t, dir, "commit", "--quiet", "--message="+msg)
}

// setRef replaces lsm-ref of the kernel entry in the .gitmodules file in dir.
func setRef(t *testing.T, dir, ref string) {
	t.Helper()
	gittest.Git(t, dir, "config", "--file", manifest.File, "--replace-all",
		"submodule.kernel."+manifest.KeyRef, ref)
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
			gittest.WriteFile(t, filepath.Join(dir, manifest.File), sample)
			commitAll(t, dir, "add manifest")
			setRef(t, dir, "v6.7.*")
			gittest.Git(t, dir, "add", manifest.File)
			setRef(t, dir, "v6.8.*")

			want := map[string][]manifest.Submodule{
				"HEAD":   sampleSubmodules(),
				"":       sampleSubmodules(),
				"HEAD~1": nil,
			}
			want[""][0].Ref = "v6.7.*"
			for rev, subs := range want {
				got, err := manifest.LoadRev(ctx, g, dir, rev)
				if err != nil || !slices.Equal(got, subs) {
					t.Errorf("LoadRev(%q) =\n%+v, %v\nwant\n%+v", rev, got, err, subs)
				}
			}
			if got := load(t, g, dir); got[0].Ref != "v6.8.*" {
				t.Errorf("Load = %+v, want the working tree copy", got[0])
			}
		})
	}
}

func TestLoadRevUnborn(t *testing.T) {
	t.Parallel()
	g := gittest.Runner(t)
	ctx := t.Context()
	dir := gittest.InitRepo(t, gittest.SHA1)
	got, err := manifest.LoadRev(ctx, g, dir, "")
	if got != nil || err != nil {
		t.Errorf("LoadRev(empty index) = %+v, %v; want nil, nil", got, err)
	}
	gittest.WriteFile(t, filepath.Join(dir, manifest.File), sample)
	gittest.Git(t, dir, "add", manifest.File)
	got, err = manifest.LoadRev(ctx, g, dir, "HEAD")
	if got != nil || !errors.Is(err, git.ErrRefNotFound) {
		t.Errorf("LoadRev(unborn HEAD) = %+v, %v; want ErrRefNotFound", got, err)
	}
	if got, err := manifest.LoadRev(ctx, g, dir, ""); err != nil ||
		!slices.Equal(got, sampleSubmodules()) {
		t.Errorf("LoadRev(index) = %+v, %v", got, err)
	}
}

func TestLoadRevErrors(t *testing.T) {
	t.Parallel()
	g := gittest.Runner(t)
	ctx := t.Context()
	dir := newRepo(t, "[submodule \"bad.one\"]\n\tpath = bad\n\tlsm-mode = bogus\n"+
		"[submodule \"good\"]\n\tpath = good\n\tlsm-mode = tag\n\tlsm-ref = v1\n"+
		"[submodule \"worse\"]\n\tpath = worse\n\tlsm-mode = other\n"+
		"[submodule \"plain\"]\n\tpath = plain\n")
	commitAll(t, dir, "add manifest")
	// The recorded files keep the entries with a valid mode, the working
	// tree copy none.
	valid := []manifest.Submodule{
		{Name: "good", Path: "good", Mode: manifest.ModeTag, Ref: "v1"},
		{Name: "plain", Path: "plain"},
	}
	for rev, prefix := range map[string]string{
		"HEAD": `HEAD:.gitmodules: submodule "bad.one": lsm-mode: `,
		"":     `:.gitmodules: submodule "bad.one": lsm-mode: `,
	} {
		got, err := manifest.LoadRev(ctx, g, dir, rev)
		if !slices.Equal(got, valid) || !errors.Is(err, manifest.ErrInvalidMode) ||
			!strings.HasPrefix(err.Error(), prefix) {
			t.Errorf("LoadRev(%q) = %+v, %v; want %+v and ErrInvalidMode starting with %q",
				rev, got, err, valid, prefix)
		}
	}
	if got, err := manifest.Load(ctx, g, dir); got != nil || !errors.Is(err, manifest.ErrInvalidMode) {
		t.Errorf("Load = %+v, %v; want ErrInvalidMode", got, err)
	}
	tests := map[string]error{
		"nope":   git.ErrRefNotFound,
		"HEAD~1": git.ErrRefNotFound,
		"-x":     git.ErrInvalidRefName,
	}
	for rev, want := range tests {
		got, err := manifest.LoadRev(ctx, g, dir, rev)
		if got != nil || !errors.Is(err, want) ||
			!strings.HasPrefix(err.Error(), "read "+rev+":"+manifest.File+": ") {
			t.Errorf("LoadRev(%q) = %+v, %v; want %v", rev, got, err, want)
		}
	}

	// Git refuses a symbolic link named .gitmodules, but not a directory.
	other := gittest.InitRepo(t, gittest.SHA1)
	gittest.WriteFile(t, filepath.Join(other, manifest.File, "x"), sample)
	commitAll(t, other, "add a directory")
	for _, rev := range []string{"HEAD", ""} {
		got, err := manifest.LoadRev(ctx, g, other, rev)
		if got != nil || !errors.Is(err, git.ErrNotRegularFile) {
			t.Errorf("LoadRev(%q, directory) = %+v, %v; want ErrNotRegularFile", rev, got, err)
		}
	}
	gittest.WriteFile(t, filepath.Join(other, "bad"), "[unterminated\n")
	gittest.Git(t, other, "rm", "-r", "--quiet", "--cached", manifest.File)
	gittest.Git(t, other, "update-index", "--add", "--cacheinfo",
		"100644,"+gittest.Git(t, other, "hash-object", "-w", "bad")+","+manifest.File)
	got, err := manifest.LoadRev(ctx, g, other, "")
	if _, ok := errors.AsType[*git.Error](err); !ok || got != nil {
		t.Errorf("LoadRev(invalid file) = %+v, %v; want *git.Error", got, err)
	}
}
