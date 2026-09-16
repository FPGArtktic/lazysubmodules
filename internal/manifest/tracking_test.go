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

// trackingSample has a submodule with a native branch ("target"), one without
// ("fresh") and an unmanaged sibling that must never change.
const trackingSample = `[submodule "target"]
	path = target
	url = https://git.example.org/target.git
	branch = old
	update = checkout
	lsm-mode = tag
	lsm-ref = v0.1.0
[submodule "fresh"]
	path = fresh
	url = https://git.example.org/fresh.git
	shallow = true
[submodule "sibling"]
	path = sibling
	url = https://git.example.org/sibling.git
	branch = develop
`

// configList runs "git config -f .gitmodules --list" in dir, as a user
// would, and returns its lines.
func configList(t *testing.T, dir string) []string {
	t.Helper()
	return strings.Split(gittest.Git(t, dir, "config", "-f", manifest.File, "--list"), "\n")
}

// setTracking calls SetTracking and fails the test on error.
func setTracking(t *testing.T, g *git.Runner, dir, name string, mode manifest.Mode, ref string) {
	t.Helper()
	if err := manifest.SetTracking(t.Context(), g, dir, name, mode, ref); err != nil {
		t.Fatalf("SetTracking(%q, %q, %q) = %v", name, mode, ref, err)
	}
}

func TestSetTracking(t *testing.T) {
	t.Parallel()
	refs := map[manifest.Mode]string{
		manifest.ModeBranch:     "release/2.x",
		manifest.ModeTag:        "v2.3.1",
		manifest.ModeTagPattern: "v2.*",
		manifest.ModeCommit:     "0123456789abcdef0123456789abcdef01234567",
	}
	sibling := manifest.Submodule{
		Name:   "sibling",
		Path:   "sibling",
		URL:    "https://git.example.org/sibling.git",
		Branch: "develop",
	}
	for mode, ref := range refs {
		for _, name := range []string{"target", "fresh"} {
			t.Run(string(mode)+"/"+name, func(t *testing.T) {
				t.Parallel()
				g := gittest.Runner(t)
				dir := newRepo(t, trackingSample)
				setTracking(t, g, dir, name, mode, ref)

				subs := load(t, g, dir)
				got, ok := manifest.Find(subs, name)
				wantBranch := ""
				if mode == manifest.ModeBranch {
					wantBranch = ref
				}
				if !ok || got.Mode != mode || got.Ref != ref || got.Branch != wantBranch {
					t.Errorf("after SetTracking: %+v, %t; want mode %q, ref %q, branch %q",
						got, ok, mode, ref, wantBranch)
				}
				if n, _ := manifest.Find(subs, "sibling"); n != sibling {
					t.Errorf("sibling changed: %+v", n)
				}

				lines := configList(t, dir)
				prefix := "submodule." + name + "."
				for _, want := range []string{
					prefix + "lsm-mode=" + string(mode),
					prefix + "lsm-ref=" + ref,
				} {
					if !slices.Contains(lines, want) {
						t.Errorf("config --list lacks %q:\n%s", want, strings.Join(lines, "\n"))
					}
				}
				hasBranch := slices.ContainsFunc(lines, func(l string) bool {
					return strings.HasPrefix(l, prefix+"branch=")
				})
				if hasBranch != (mode == manifest.ModeBranch) {
					t.Errorf("native branch key present = %t in mode %s:\n%s",
						hasBranch, mode, strings.Join(lines, "\n"))
				}
				if name == "target" && !slices.Contains(lines, "submodule.target.update=checkout") {
					t.Errorf("unknown key of target was lost:\n%s", strings.Join(lines, "\n"))
				}
			})
		}
	}
}

func TestSetTrackingLayout(t *testing.T) {
	t.Parallel()
	g := gittest.Runner(t)
	dir := newRepo(t, `[submodule "kernel"]
	path = kernel
	url = https://git.example.org/linux.git
	branch = main
[submodule "u-boot"]
	path = u-boot
	url = https://git.example.org/u-boot.git
`)
	setTracking(t, g, dir, "kernel", manifest.ModeTagPattern, "v6.6.*")
	setTracking(t, g, dir, "u-boot", manifest.ModeBranch, "main")

	// The .gitmodules example of README.md.
	const want = `[submodule "kernel"]
	path = kernel
	url = https://git.example.org/linux.git
	lsm-mode = tag-pattern
	lsm-ref = v6.6.*
[submodule "u-boot"]
	path = u-boot
	url = https://git.example.org/u-boot.git
	branch = main
	lsm-mode = branch
	lsm-ref = main
`
	if got := readManifest(t, dir); got != want {
		t.Errorf(".gitmodules =\n%s\nwant\n%s", got, want)
	}
}

func TestSetTrackingModeSwitches(t *testing.T) {
	t.Parallel()
	g := gittest.Runner(t)
	dir := newRepo(t, trackingSample)
	steps := []struct {
		mode manifest.Mode
		ref  string
	}{
		{manifest.ModeBranch, "main"},
		{manifest.ModeBranch, "next"},
		{manifest.ModeTag, "v1.0.0"},
		{manifest.ModeTagPattern, "v1.*"},
		{manifest.ModeBranch, "main"},
		{manifest.ModeCommit, "0123456789ab"},
		{manifest.ModeCommit, "0123456789ab"},
		{manifest.ModeTag, "v1.0.1"},
	}
	for _, step := range steps {
		setTracking(t, g, dir, "fresh", step.mode, step.ref)
		want := manifest.Submodule{
			Name: "fresh",
			Path: "fresh",
			URL:  "https://git.example.org/fresh.git",
			Mode: step.mode,
			Ref:  step.ref,
		}
		if step.mode == manifest.ModeBranch {
			want.Branch = step.ref
		}
		if got, _ := manifest.Find(load(t, g, dir), "fresh"); got != want {
			t.Errorf("after %s %s: %+v, want %+v", step.mode, step.ref, got, want)
		}
	}
	// Every variable is written in place: no duplicates accumulate.
	const want = `[submodule "fresh"]
	path = fresh
	url = https://git.example.org/fresh.git
	shallow = true
	lsm-mode = tag
	lsm-ref = v1.0.1
`
	if got := readManifest(t, dir); !strings.Contains(got, want) {
		t.Errorf(".gitmodules =\n%s\nwant it to contain\n%s", got, want)
	}
}

func TestSetTrackingNamesAreExact(t *testing.T) {
	t.Parallel()
	g := gittest.Runner(t)
	dir := newRepo(t, `[submodule "kernel"]
	path = kernel
[submodule "Kernel"]
	path = kernel-mixed
[submodule "a.b.c"]
	path = abc
[submodule "a.b"]
	path = ab
[submodule "with \"quote\" and space"]
	path = quoted
`)
	setTracking(t, g, dir, "Kernel", manifest.ModeTag, "v1")
	setTracking(t, g, dir, "a.b.c", manifest.ModeBranch, "main")
	setTracking(t, g, dir, `with "quote" and space`, manifest.ModeCommit, "abcdef0")

	want := []manifest.Submodule{
		{Name: "kernel", Path: "kernel"},
		{Name: "Kernel", Path: "kernel-mixed", Mode: manifest.ModeTag, Ref: "v1"},
		{Name: "a.b.c", Path: "abc", Branch: "main", Mode: manifest.ModeBranch, Ref: "main"},
		{Name: "a.b", Path: "ab"},
		{Name: `with "quote" and space`, Path: "quoted", Mode: manifest.ModeCommit, Ref: "abcdef0"},
	}
	if got := load(t, g, dir); !slices.Equal(got, want) {
		t.Errorf("Load =\n%+v\nwant\n%+v\nfile:\n%s", got, want, readManifest(t, dir))
	}
	lines := configList(t, dir)
	if key := `submodule.with "quote" and space.lsm-mode=commit`; !slices.Contains(lines, key) {
		t.Errorf("config --list lacks %q:\n%s", key, strings.Join(lines, "\n"))
	}
}

func TestSetTrackingNotFound(t *testing.T) {
	t.Parallel()
	g := gittest.Runner(t)

	missing := gittest.InitRepo(t, gittest.SHA1)
	err := manifest.SetTracking(t.Context(), g, missing, "kernel", manifest.ModeTag, "v1")
	if !errors.Is(err, manifest.ErrNotFound) {
		t.Errorf("SetTracking(missing file) = %v, want ErrNotFound", err)
	}
	if _, err := os.Stat(filepath.Join(missing, manifest.File)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("SetTracking created %s: %v", manifest.File, err)
	}

	const content = `[submodule "kernel"]
	path = kernel
[submodule "no-path"]
	url = https://git.example.org/no-path.git
[submodule "../evil"]
	path = evil
[submodule "outside"]
	path = ../outside
`
	dir := newRepo(t, content)
	names := []string{"KERNEL", "kern", "kernel.path", "no-path", "../evil", "outside", ""}
	for _, name := range names {
		err := manifest.SetTracking(t.Context(), g, dir, name, manifest.ModeBranch, "main")
		if !errors.Is(err, manifest.ErrNotFound) {
			t.Errorf("SetTracking(%q) = %v, want ErrNotFound", name, err)
		}
	}
	if got := readManifest(t, dir); got != content {
		t.Errorf("file changed:\n%s", got)
	}
	err = manifest.SetTracking(t.Context(), g, dir, "nope", manifest.ModeTag, "v1")
	if want := `no such submodule in .gitmodules: "nope"`; err == nil || err.Error() != want {
		t.Errorf("SetTracking error = %v, want %q", err, want)
	}
}

func TestSetTrackingInvalidMode(t *testing.T) {
	t.Parallel()
	g := gittest.Runner(t)
	dir := newRepo(t, trackingSample)
	for _, mode := range []manifest.Mode{"", "bogus", "Branch"} {
		err := manifest.SetTracking(t.Context(), g, dir, "target", mode, "main")
		if !errors.Is(err, manifest.ErrInvalidMode) {
			t.Errorf("SetTracking(mode %q) = %v, want ErrInvalidMode", mode, err)
		}
	}
	if got := readManifest(t, dir); got != trackingSample {
		t.Errorf("file changed:\n%s", got)
	}
}

func TestSetTrackingRepairsInvalidMode(t *testing.T) {
	t.Parallel()
	g := gittest.Runner(t)
	dir := newRepo(t, "[submodule \"x\"]\n\tpath = x\n\tbranch = main\n\tlsm-mode = bogus\n")
	if _, err := manifest.Load(t.Context(), g, dir); !errors.Is(err, manifest.ErrInvalidMode) {
		t.Fatalf("Load = %v, want ErrInvalidMode", err)
	}
	setTracking(t, g, dir, "x", manifest.ModeTag, "v1.0.0")
	want := []manifest.Submodule{{Name: "x", Path: "x", Mode: manifest.ModeTag, Ref: "v1.0.0"}}
	if got := load(t, g, dir); !slices.Equal(got, want) {
		t.Errorf("Load = %+v, want %+v", got, want)
	}
}

func TestSetTrackingGitError(t *testing.T) {
	t.Parallel()
	g := gittest.Runner(t)
	for _, mode := range []manifest.Mode{manifest.ModeBranch, manifest.ModeTag} {
		dir := newRepo(t, trackingSample)
		// A leftover lock file makes every write of git config fail.
		gittest.WriteFile(t, filepath.Join(dir, manifest.File+".lock"), "")
		err := manifest.SetTracking(t.Context(), g, dir, "target", mode, "v1")
		gitErr, ok := errors.AsType[*git.Error](err)
		if !ok {
			t.Errorf("SetTracking(%s) = %v, want *git.Error", mode, err)
			continue
		}
		if !strings.HasPrefix(err.Error(), `.gitmodules: submodule "target": git config`) {
			t.Errorf("SetTracking(%s) error = %q", mode, err)
		}
		if gitErr.ExitCode <= 0 {
			t.Errorf("SetTracking(%s) exit code = %d", mode, gitErr.ExitCode)
		}
		if got := readManifest(t, dir); got != trackingSample {
			t.Errorf("file changed:\n%s", got)
		}
	}

	dir := newRepo(t, "[submodule \"x\"\n")
	err := manifest.SetTracking(t.Context(), g, dir, "x", manifest.ModeTag, "v1")
	if _, ok := errors.AsType[*git.Error](err); !ok {
		t.Errorf("SetTracking(invalid file) = %v, want *git.Error", err)
	}
}

func TestSetTrackingFailsAfterFirstWrite(t *testing.T) {
	t.Parallel()
	g := gittest.Runner(t)
	dir := newRepo(t, trackingSample)
	// No process argument can hold a NUL byte, so only the lsm-ref write
	// fails. Callers validate refs; this shows that the writes are separate.
	err := manifest.SetTracking(t.Context(), g, dir, "fresh", manifest.ModeCommit, "a\x00b")
	if _, ok := errors.AsType[*git.Error](err); !ok ||
		!strings.HasPrefix(err.Error(), `.gitmodules: submodule "fresh": git config`) {
		t.Fatalf("SetTracking = %v, want *git.Error", err)
	}
	got, _ := manifest.Find(load(t, g, dir), "fresh")
	if got.Mode != manifest.ModeCommit || got.Ref != "" {
		t.Errorf("fresh after the failed write = %+v, want only lsm-mode written", got)
	}
}

func TestManifestNotRegularFile(t *testing.T) {
	t.Parallel()
	g := gittest.Runner(t)
	target := filepath.Join(t.TempDir(), "target")
	gittest.WriteFile(t, target, trackingSample)
	dir := gittest.InitRepo(t, gittest.SHA1)
	if err := os.Symlink(target, filepath.Join(dir, manifest.File)); err != nil {
		t.Fatal(err)
	}
	_, loadErr := manifest.Load(t.Context(), g, dir)
	setErr := manifest.SetTracking(t.Context(), g, dir, "target", manifest.ModeTag, "v1")
	for op, err := range map[string]error{"Load": loadErr, "SetTracking": setErr} {
		if !errors.Is(err, git.ErrNotRegularFile) {
			t.Errorf("%s = %v, want %v", op, err, git.ErrNotRegularFile)
		}
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != trackingSample {
		t.Errorf("symlink target changed: %q, %v", got, err)
	}
}
