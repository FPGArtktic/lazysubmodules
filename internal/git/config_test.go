// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package git_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/FPGArtktic/lazysubmodules/internal/git"
	"github.com/FPGArtktic/lazysubmodules/internal/git/gittest"
)

const sampleConfig = `[submodule "kernel"]
	path = kernel
	url = https://git.example.org/linux.git
	lsm-mode = tag-pattern
	lsm-ref = v6.6.*
[submodule "a.b"]
	PATH = dotted
	flag
[submodule "Kernel"]
	path = other
	x = "  spaced\tvalue  "
[submodule "empty"]
`

// readFile returns the content of path.
func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// exists reports whether path exists.
func exists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Stat(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	return err == nil
}

func TestConfigList(t *testing.T) {
	t.Parallel()
	r := gittest.Runner(t)
	dir := t.TempDir()
	gittest.WriteFile(t, filepath.Join(dir, ".gitmodules"), sampleConfig)
	want := sampleEntries()
	got, err := r.ConfigList(t.Context(), dir, ".gitmodules")
	if err != nil || !slices.Equal(got, want) {
		t.Errorf("ConfigList(relative) =\n%q, %v\nwant\n%q", got, err, want)
	}
	got, err = r.ConfigList(t.Context(), "", filepath.Join(dir, ".gitmodules"))
	if err != nil || !slices.Equal(got, want) {
		t.Errorf("ConfigList(absolute) = %q, %v", got, err)
	}
}

func TestConfigListMissingOrEmptyFile(t *testing.T) {
	t.Parallel()
	r := gittest.Runner(t)
	dir := t.TempDir()
	got, err := r.ConfigList(t.Context(), dir, ".lsm.lock")
	if got != nil || err != nil {
		t.Errorf("ConfigList(missing) = %q, %v; want nil, nil", got, err)
	}
	got, err = r.ConfigList(t.Context(), filepath.Join(dir, "missing-dir"), ".lsm.lock")
	if got != nil || err != nil {
		t.Errorf("ConfigList(missing dir) = %q, %v; want nil, nil", got, err)
	}
	gittest.WriteFile(t, filepath.Join(dir, "empty"), "")
	got, err = r.ConfigList(t.Context(), dir, "empty")
	if got != nil || err != nil {
		t.Errorf("ConfigList(empty) = %q, %v; want nil, nil", got, err)
	}
}

func TestConfigListInvalidFile(t *testing.T) {
	t.Parallel()
	r := gittest.Runner(t)
	dir := t.TempDir()
	gittest.WriteFile(t, filepath.Join(dir, "bad"), "[unterminated\n")
	_, err := r.ConfigList(t.Context(), dir, "bad")
	if _, ok := errors.AsType[*git.Error](err); !ok {
		t.Errorf("ConfigList(bad) error = %v, want *git.Error", err)
	}
}

func TestConfigSet(t *testing.T) {
	t.Parallel()
	r := gittest.Runner(t)
	ctx := t.Context()
	dir := t.TempDir()
	values := map[string]string{
		"submodule.a.b.lsm-ref":  "-leading dash",
		"submodule.x.lsm-ref":    "  spaces\tand \"quotes\" \\ ; # ",
		"submodule.Case.lsm-ref": "v1.*",
	}
	for key, value := range values {
		if err := r.ConfigSet(ctx, dir, ".lsm.lock", key, value); err != nil {
			t.Fatalf("ConfigSet(%s) = %v", key, err)
		}
	}
	entries, err := r.ConfigList(ctx, dir, ".lsm.lock")
	if err != nil || len(entries) != len(values) {
		t.Fatalf("ConfigList = %q, %v", entries, err)
	}
	for _, e := range entries {
		if values[e.Key] != e.Value {
			t.Errorf("%s = %q, want %q", e.Key, e.Value, values[e.Key])
		}
	}
}

func TestConfigSetReplacesAllValues(t *testing.T) {
	t.Parallel()
	r := gittest.Runner(t)
	dir := t.TempDir()
	gittest.WriteFile(t, filepath.Join(dir, "c"), "[a]\n\tb = 1\n\tb = 2\n")
	if err := r.ConfigSet(t.Context(), dir, "c", "a.b", "3"); err != nil {
		t.Fatal(err)
	}
	got, err := r.ConfigList(t.Context(), dir, "c")
	want := []git.ConfigEntry{{Key: "a.b", Value: "3"}}
	if err != nil || !slices.Equal(got, want) {
		t.Errorf("ConfigList = %q, %v; want %q", got, err, want)
	}
}

func TestConfigSetInvalidKey(t *testing.T) {
	t.Parallel()
	r := gittest.Runner(t)
	err := r.ConfigSet(t.Context(), t.TempDir(), "c", "nosection", "v")
	if _, ok := errors.AsType[*git.Error](err); !ok {
		t.Errorf("ConfigSet(invalid key) = %v, want *git.Error", err)
	}
}

func TestConfigUnset(t *testing.T) {
	t.Parallel()
	r := gittest.Runner(t)
	ctx := t.Context()
	dir := t.TempDir()

	if err := r.ConfigUnset(ctx, dir, "c", "a.b"); err != nil {
		t.Errorf("ConfigUnset(missing file) = %v", err)
	}
	if exists(t, filepath.Join(dir, "c")) {
		t.Errorf("ConfigUnset created the file")
	}

	gittest.WriteFile(t, filepath.Join(dir, "c"), "[a]\n\tb = 1\n\tb = 2\n\tc = 3\n")
	if err := r.ConfigUnset(ctx, dir, "c", "a.missing"); err != nil {
		t.Errorf("ConfigUnset(missing key) = %v", err)
	}
	if err := r.ConfigUnset(ctx, dir, "c", "a.b"); err != nil {
		t.Errorf("ConfigUnset(a.b) = %v", err)
	}
	got, err := r.ConfigList(ctx, dir, "c")
	want := []git.ConfigEntry{{Key: "a.c", Value: "3"}}
	if err != nil || !slices.Equal(got, want) {
		t.Errorf("ConfigList = %q, %v; want %q", got, err, want)
	}
	if err := r.ConfigUnset(ctx, dir, "c", "invalid"); err == nil {
		t.Errorf("ConfigUnset(invalid key) = nil, want error")
	}
}

func TestConfigRemoveSection(t *testing.T) {
	t.Parallel()
	r := gittest.Runner(t)
	ctx := t.Context()
	dir := t.TempDir()
	file := filepath.Join(dir, ".gitmodules")

	if err := r.ConfigRemoveSection(ctx, dir, ".gitmodules", "submodule.kernel"); err != nil {
		t.Errorf("ConfigRemoveSection(missing file) = %v", err)
	}
	if exists(t, file) {
		t.Errorf("ConfigRemoveSection created the file")
	}

	gittest.WriteFile(t, file, sampleConfig)
	steps := []string{
		"submodule.missing",
		"submodule.KERNEL", // subsection names are case-sensitive
		"submodule.kernel",
		"submodule.a.b",
		"submodule.empty", // no variables, only the header
	}
	for _, section := range steps {
		if err := r.ConfigRemoveSection(ctx, dir, ".gitmodules", section); err != nil {
			t.Errorf("ConfigRemoveSection(%s) = %v", section, err)
		}
	}
	want := "[submodule \"Kernel\"]\n\tpath = other\n\tx = \"  spaced\\tvalue  \"\n"
	if got := readFile(t, file); got != want {
		t.Errorf("file after removals = %q, want %q", got, want)
	}
}

func TestConfigRemoveSectionError(t *testing.T) {
	t.Parallel()
	r := gittest.Runner(t)
	ctx := t.Context()
	dir := t.TempDir()
	file := filepath.Join(dir, ".gitmodules")
	gittest.WriteFile(t, file, sampleConfig)
	// A stale lock file makes every change fail.
	gittest.WriteFile(t, file+".lock", "")

	err := r.ConfigRemoveSection(ctx, dir, ".gitmodules", "submodule.kernel")
	if _, ok := errors.AsType[*git.Error](err); !ok {
		t.Errorf("ConfigRemoveSection(locked) = %v, want *git.Error", err)
	}
	// The failure does not matter when the section is missing anyway.
	if err := r.ConfigRemoveSection(ctx, dir, ".gitmodules", "submodule.missing"); err != nil {
		t.Errorf("ConfigRemoveSection(locked, missing section) = %v", err)
	}
	if got := readFile(t, file); got != sampleConfig {
		t.Errorf("file changed by failed removals: %q", got)
	}

	// For a file git cannot parse, listing the variables fails as well.
	gittest.WriteFile(t, file, "[submodule \"x\"\n")
	err = r.ConfigRemoveSection(ctx, dir, ".gitmodules", "submodule.x")
	_, ok := errors.AsType[*git.Error](err)
	if msg := fmt.Sprint(err); !ok || !strings.Contains(msg, "--remove-section") ||
		!strings.Contains(msg, "--list") {
		t.Errorf("ConfigRemoveSection(invalid) = %v, want both git errors", err)
	}
}

func TestConfigSetError(t *testing.T) {
	t.Parallel()
	r := gittest.Runner(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "c")
	gittest.WriteFile(t, file, "[a]\n\tb = 1\n")
	gittest.WriteFile(t, file+".lock", "")
	for name, err := range map[string]error{
		"ConfigSet":   r.ConfigSet(t.Context(), dir, "c", "a.b", "2"),
		"ConfigUnset": r.ConfigUnset(t.Context(), dir, "c", "a.b"),
	} {
		if _, ok := errors.AsType[*git.Error](err); !ok {
			t.Errorf("%s(locked) = %v, want *git.Error", name, err)
		}
	}
	if got := readFile(t, file); got != "[a]\n\tb = 1\n" {
		t.Errorf("file changed: %q", got)
	}
}

func TestConfigRefusesNonRegularFiles(t *testing.T) {
	t.Parallel()
	r := gittest.Runner(t)
	ctx := t.Context()
	dir := t.TempDir()
	outside := t.TempDir()
	target := filepath.Join(outside, "target")
	gittest.WriteFile(t, target, sampleConfig)
	links := map[string]string{
		"link":     target,
		"relative": "../" + filepath.Base(outside) + "/target",
		"dangling": filepath.Join(outside, "created"),
	}
	for name, dest := range links {
		if err := os.Symlink(dest, filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "directory"), 0o755); err != nil {
		t.Fatal(err)
	}

	for _, file := range []string{"link", "relative", "dangling", "directory"} {
		_, listErr := r.ConfigList(ctx, dir, file)
		for name, err := range map[string]error{
			"ConfigList":          listErr,
			"ConfigSet":           r.ConfigSet(ctx, dir, file, "submodule.x.path", "x"),
			"ConfigUnset":         r.ConfigUnset(ctx, dir, file, "submodule.kernel.path"),
			"ConfigRemoveSection": r.ConfigRemoveSection(ctx, dir, file, "submodule.kernel"),
		} {
			if !errors.Is(err, git.ErrNotRegularFile) {
				t.Errorf("%s(%s) = %v, want ErrNotRegularFile", name, file, err)
			}
		}
		_, err := r.ConfigList(ctx, "", filepath.Join(dir, file))
		if !errors.Is(err, git.ErrNotRegularFile) {
			t.Errorf("ConfigList(absolute %s) = %v, want ErrNotRegularFile", file, err)
		}
	}
	if got := readFile(t, target); got != sampleConfig {
		t.Errorf("link target changed: %q", got)
	}
	if exists(t, filepath.Join(outside, "created")) {
		t.Errorf("dangling link target created")
	}
}

// sampleEntries returns what ConfigList returns for sampleConfig.
func sampleEntries() []git.ConfigEntry {
	return []git.ConfigEntry{
		{Key: "submodule.kernel.path", Value: "kernel"},
		{Key: "submodule.kernel.url", Value: "https://git.example.org/linux.git"},
		{Key: "submodule.kernel.lsm-mode", Value: "tag-pattern"},
		{Key: "submodule.kernel.lsm-ref", Value: "v6.6.*"},
		{Key: "submodule.a.b.path", Value: "dotted"},
		{Key: "submodule.a.b.flag", Value: ""},
		{Key: "submodule.Kernel.path", Value: "other"},
		{Key: "submodule.Kernel.x", Value: "  spaced\tvalue  "},
	}
}

func TestConfigListRev(t *testing.T) {
	t.Parallel()
	for _, format := range formats() {
		t.Run(format, func(t *testing.T) {
			t.Parallel()
			r := gittest.Runner(t)
			ctx := t.Context()
			dir := gittest.NewSuper(t, format).Dir
			for _, file := range []string{".lsm.lock", "conf/sub/c"} {
				gittest.WriteFile(t, filepath.Join(dir, file), sampleConfig)
			}
			gittest.Git(t, dir, "add", "--all")
			gittest.Git(t, dir, "commit", "--quiet", "-m", "add configuration")
			gittest.WriteFile(t, filepath.Join(dir, ".lsm.lock"), "[a]\n\tb = staged\n")
			gittest.Git(t, dir, "add", ".lsm.lock")
			gittest.WriteFile(t, filepath.Join(dir, ".lsm.lock"), "[a]\n\tb = worktree\n")

			tree := gittest.Git(t, dir, "rev-parse", "HEAD^{tree}")
			tests := []struct {
				rev, file string
				want      []git.ConfigEntry
			}{
				{"HEAD", ".lsm.lock", sampleEntries()},
				{"main", ".lsm.lock", sampleEntries()},
				{tree, ".lsm.lock", sampleEntries()},
				{"", ".lsm.lock", []git.ConfigEntry{{Key: "a.b", Value: "staged"}}},
				{"HEAD~1", ".lsm.lock", nil}, // before the file was added
				{"HEAD", "conf/sub/c", sampleEntries()},
				{"HEAD", "./conf/sub/c", sampleEntries()},
				{"", "conf/sub/c", sampleEntries()},
				{"", "./conf/sub/c", sampleEntries()},
			}
			for _, rev := range []string{"HEAD", ""} {
				for _, file := range []string{"missing", "conf/missing", ".lsm.lock/x"} {
					tests = append(tests, struct {
						rev, file string
						want      []git.ConfigEntry
					}{rev, file, nil})
				}
			}
			for _, tt := range tests {
				got, err := r.ConfigListRev(ctx, dir, tt.rev, tt.file)
				if err != nil || !slices.Equal(got, tt.want) {
					t.Errorf("ConfigListRev(%q, %q) = %q, %v; want %q",
						tt.rev, tt.file, got, err, tt.want)
				}
			}
			got, err := r.ConfigList(ctx, dir, ".lsm.lock")
			if want := []git.ConfigEntry{{Key: "a.b", Value: "worktree"}}; err != nil ||
				!slices.Equal(got, want) {
				t.Errorf("ConfigList = %q, %v; want %q", got, err, want)
			}
		})
	}
}

func TestConfigListRevUnborn(t *testing.T) {
	t.Parallel()
	for _, format := range formats() {
		t.Run(format, func(t *testing.T) {
			t.Parallel()
			r := gittest.Runner(t)
			ctx := t.Context()
			dir := gittest.InitRepo(t, format)
			got, err := r.ConfigListRev(ctx, dir, "", ".gitmodules")
			if got != nil || err != nil {
				t.Errorf("ConfigListRev(empty index) = %q, %v; want nil, nil", got, err)
			}
			gittest.WriteFile(t, filepath.Join(dir, ".gitmodules"), sampleConfig)
			gittest.Git(t, dir, "add", ".gitmodules")
			got, err = r.ConfigListRev(ctx, dir, "HEAD", ".gitmodules")
			if got != nil || !errors.Is(err, git.ErrRefNotFound) {
				t.Errorf("ConfigListRev(unborn HEAD) = %q, %v; want ErrRefNotFound", got, err)
			}
			got, err = r.ConfigListRev(ctx, dir, "", ".gitmodules")
			if err != nil || !slices.Equal(got, sampleEntries()) {
				t.Errorf("ConfigListRev(index) = %q, %v; want %q", got, err, sampleEntries())
			}
		})
	}
}

func TestConfigListRevErrors(t *testing.T) {
	t.Parallel()
	f := newFixture(t, gittest.SHA1)
	r := gittest.Runner(t)
	ctx := t.Context()
	dir := f.super.Dir
	if err := os.Symlink(".gitmodules", filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	gittest.WriteFile(t, filepath.Join(dir, "dir", "c"), sampleConfig)
	gittest.WriteFile(t, filepath.Join(dir, "bad"), "[unterminated\n")
	gittest.WriteFile(t, filepath.Join(dir, "exec"), sampleConfig)
	if err := os.Chmod(filepath.Join(dir, "exec"), 0o755); err != nil {
		t.Fatal(err)
	}
	gittest.Git(t, dir, "add", "--all")
	gittest.Git(t, dir, "commit", "--quiet", "-m", "add files")
	blob := gittest.Git(t, dir, "rev-parse", "HEAD:bad")

	for _, rev := range []string{"HEAD", ""} {
		got, err := r.ConfigListRev(ctx, dir, rev, "exec")
		if err != nil || !slices.Equal(got, sampleEntries()) {
			t.Errorf("ConfigListRev(%q, exec) = %q, %v; want the entries", rev, got, err)
		}
		// A symbolic link, a submodule and a directory.
		for _, file := range []string{"link", f.path, f.path + "/", "dir", "dir/"} {
			got, err := r.ConfigListRev(ctx, dir, rev, file)
			if got != nil || !errors.Is(err, git.ErrNotRegularFile) {
				t.Errorf("ConfigListRev(%q, %s) = %q, %v; want ErrNotRegularFile",
					rev, file, got, err)
			}
		}
		got, err = r.ConfigListRev(ctx, dir, rev, "bad")
		if _, ok := errors.AsType[*git.Error](err); !ok || got != nil {
			t.Errorf("ConfigListRev(%q, bad) = %q, %v; want *git.Error", rev, got, err)
		}
	}
	for _, rev := range []string{"-x", "--output=x", "-"} {
		got, err := r.ConfigListRev(ctx, dir, rev, "exec")
		if got != nil || !errors.Is(err, git.ErrInvalidRefName) {
			t.Errorf("ConfigListRev(%q) = %q, %v; want ErrInvalidRefName", rev, got, err)
		}
	}
	for _, rev := range []string{"nope", "HEAD~9"} {
		got, err := r.ConfigListRev(ctx, dir, rev, "exec")
		if got != nil || !errors.Is(err, git.ErrRefNotFound) {
			t.Errorf("ConfigListRev(%q) = %q, %v; want ErrRefNotFound", rev, got, err)
		}
	}
	got, err := r.ConfigListRev(ctx, dir, blob, "exec")
	if _, ok := errors.AsType[*git.Error](err); !ok || got != nil {
		t.Errorf("ConfigListRev(blob) = %q, %v; want *git.Error", got, err)
	}
	for _, rev := range []string{"HEAD", ""} {
		got, err := r.ConfigListRev(ctx, t.TempDir(), rev, "exec")
		if _, ok := errors.AsType[*git.Error](err); !ok || got != nil {
			t.Errorf("ConfigListRev(%q) outside a repository = %q, %v; want *git.Error",
				rev, got, err)
		}
	}
}

func TestConfigListRevUnmerged(t *testing.T) {
	t.Parallel()
	r := gittest.Runner(t)
	ctx := t.Context()
	dir := gittest.NewSuper(t, gittest.SHA1).Dir
	commit := func(value string) {
		gittest.WriteFile(t, filepath.Join(dir, "c"), "[a]\n\tb = "+value+"\n")
		gittest.Git(t, dir, "commit", "--quiet", "--all", "--message="+value)
	}
	gittest.WriteFile(t, filepath.Join(dir, "c"), "")
	gittest.Git(t, dir, "add", "c")
	commit("base")
	gittest.Git(t, dir, "checkout", "--quiet", "-b", "other")
	commit("theirs")
	gittest.Git(t, dir, "checkout", "--quiet", "main")
	commit("ours")
	_, err := r.Run(ctx, dir, "merge", "--quiet", "other")
	if _, ok := errors.AsType[*git.Error](err); !ok {
		t.Fatalf("merge = %v, want a conflict", err)
	}

	got, err := r.ConfigListRev(ctx, dir, "", "c")
	if got != nil || !errors.Is(err, git.ErrUnmerged) {
		t.Errorf("ConfigListRev(unmerged) = %q, %v; want ErrUnmerged", got, err)
	}
	for rev, value := range map[string]string{"HEAD": "ours", "MERGE_HEAD": "theirs"} {
		got, err := r.ConfigListRev(ctx, dir, rev, "c")
		if want := []git.ConfigEntry{{Key: "a.b", Value: value}}; err != nil ||
			!slices.Equal(got, want) {
			t.Errorf("ConfigListRev(%s) = %q, %v; want %q", rev, got, err, want)
		}
	}
}
