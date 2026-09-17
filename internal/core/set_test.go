// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package core_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FPGArtktic/lazysubmodules/internal/core"
	"github.com/FPGArtktic/lazysubmodules/internal/git/gittest"
	"github.com/FPGArtktic/lazysubmodules/internal/manifest"
)

// set runs Set on the fixture superproject.
func (f *fixture) set(name string, mode manifest.Mode, ref string) (manifest.Submodule, error) {
	f.t.Helper()
	return f.repo().Set(f.t.Context(), name, mode, ref)
}

// wantTracking checks the tracking variables of a submodule in .gitmodules.
func wantTracking(t *testing.T, f *fixture, name string, mode manifest.Mode, ref, branch string) {
	t.Helper()
	got := []string{
		gitmodulesKey(t, f.super.Dir, name, manifest.KeyMode),
		gitmodulesKey(t, f.super.Dir, name, manifest.KeyRef),
		gitmodulesKey(t, f.super.Dir, name, manifest.KeyBranch),
	}
	if got[0] != string(mode) || got[1] != ref || got[2] != branch {
		t.Errorf("%s: mode, ref, branch = %q; want %s, %s, %q", name, got, mode, ref, branch)
	}
}

func TestSetModes(t *testing.T) {
	t.Parallel()
	for _, format := range formats() {
		t.Run(format, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t, format)
			f.add("lib", "", "")
			f.add("other", manifest.ModeTag, gittest.TagV100)
			wantState(t, f.status("lib"), core.StateUnmanaged, "")
			head := headOf(t, f.dir("lib"))
			before := gittest.Git(t, f.super.Dir, "ls-files", "--stage")
			v100 := f.commits[gittest.TagV100]
			for _, c := range []struct {
				mode   manifest.Mode
				ref    string
				stored string
				branch string
			}{
				{manifest.ModeBranch, gittest.BranchStable, gittest.BranchStable,
					gittest.BranchStable},
				{manifest.ModeTag, gittest.TagV100, gittest.TagV100, ""},
				{manifest.ModeBranch, "feature/x", "feature/x", "feature/x"},
				{manifest.ModeTagPattern, "v[0-9].*", "v[0-9].*", ""},
				{manifest.ModeCommit, v100, v100, ""},
				{manifest.ModeCommit, v100[:7], v100, ""},
				{manifest.ModeCommit, v100[:len(v100)-1], v100, ""},
			} {
				sub, err := f.set("lib", c.mode, c.ref)
				want := manifest.Submodule{Name: "lib", Path: "lib", URL: f.up.Bare,
					Mode: c.mode, Ref: c.stored, Branch: c.branch}
				if err != nil || sub != want {
					t.Errorf("Set(%s, %s) = %+v, %v; want %+v", c.mode, c.ref, sub, err, want)
				}
				wantTracking(t, f, "lib", c.mode, c.stored, c.branch)
			}
			// Only .gitmodules changed, and it is not staged.
			wantTracking(t, f, "other", manifest.ModeTag, gittest.TagV100, "")
			wantStaged(t, f.super.Dir)
			if got := gittest.Git(t, f.super.Dir, "ls-files", "--stage"); got != before {
				t.Errorf("index changed:\n%s", got)
			}
			status := gittest.Git(t, f.super.Dir, "status", "--porcelain",
				"--untracked-files=all", "--ignore-submodules=none")
			if status != "M .gitmodules" {
				t.Errorf("status %q", status)
			}
			if got := headOf(t, f.dir("lib")); got != head {
				t.Errorf("submodule moved to %s", got)
			}
			if _, err := os.Lstat(filepath.Join(f.super.Dir, ".lsm.lock")); err == nil {
				t.Errorf("Set wrote a lock file")
			}
			// The submodule is managed now; an update applies the setting.
			wantState(t, f.status("lib"), core.StateBehind, "no lock entry")
		})
	}
}

func TestSetAbbreviatedCommit(t *testing.T) {
	t.Parallel()
	f := newFixture(t, gittest.SHA1)
	v100, v101 := f.commits[gittest.TagV100], f.commits[gittest.TagV101]
	f.add("lib", manifest.ModeTag, gittest.TagV100)
	f.add("gone", manifest.ModeTag, gittest.TagV100)
	f.super.Deinit(t, "lib")

	// The repository in the git directory serves for the expansion.
	sub, err := f.set("lib", manifest.ModeCommit, v101[:8])
	if err != nil || sub.Ref != v101 {
		t.Errorf("Set(deinitialized) = %+v, %v", sub, err)
	}

	f.super.Deinit(t, "gone")
	f.removeModule("gone")
	_, err = f.set("gone", manifest.ModeCommit, v100[:8])
	wantErr(t, "Set(no repository)", err, core.ErrInvalidArgument)
	if want := "gone: invalid commit \"" + v100[:8] + "\": cannot be expanded without the " +
		"submodule repository; give the full commit name"; err == nil || err.Error() != want {
		t.Errorf("Set(no repository) = %v\nwant %s", err, want)
	}
	if sub, err := f.set("gone", manifest.ModeCommit, v100); err != nil || sub.Ref != v100 {
		t.Errorf("Set(full commit, no repository) = %+v, %v", sub, err)
	}

	// A tag named like the abbreviation shadows the commit.
	gittest.Git(t, f.super.Dir, "submodule", "update", "--init", "--quiet", "--", "lib")
	gittest.Git(t, f.dir("lib"), "tag", v100[:7], v101)
	for ref, reason := range map[string]string{
		"1234567": `lib: invalid commit "1234567": commit 1234567 does not exist`,
		v100[:7]: `lib: invalid commit "` + v100[:7] + `": commit ` + v100[:7] +
			" is ambiguous: a ref of that name points to " + v101[:12],
	} {
		_, err := f.set("lib", manifest.ModeCommit, ref)
		wantErr(t, "Set("+ref+")", err, core.ErrInvalidArgument)
		if err == nil || !strings.HasPrefix(err.Error(), reason) {
			t.Errorf("Set(%s) = %v, want %q", ref, err, reason)
		}
	}
	wantTracking(t, f, "lib", manifest.ModeCommit, v101, "")
}

func TestSetInvalid(t *testing.T) {
	t.Parallel()
	for _, format := range formats() {
		t.Run(format, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t, format)
			f.add("lib", manifest.ModeBranch, "main")
			before := treeState(t, f.super.Dir)
			long := strings.Repeat("a", len(f.commits[gittest.TagV100])+1)
			for _, c := range []struct {
				mode manifest.Mode
				ref  string
			}{
				{manifest.ModeBranch, ""},
				{manifest.ModeBranch, "-main"},
				{manifest.ModeBranch, "--upload-pack=x"},
				{manifest.ModeBranch, "a..b"},
				{manifest.ModeBranch, "a b"},
				{manifest.ModeBranch, "main\n"},
				{manifest.ModeBranch, "ma\x1bin"},
				{manifest.ModeBranch, "@{-1}"},
				{manifest.ModeTag, "v1*"},
				{manifest.ModeTag, "v1\x00"},
				{manifest.ModeTag, "-v1"},
				{manifest.ModeTagPattern, "v1:*"},
				{manifest.ModeTagPattern, "-*"},
				{manifest.ModeCommit, "HEAD"},
				{manifest.ModeCommit, "ABCDEF0"},
				{manifest.ModeCommit, "abcdef"},
				{manifest.ModeCommit, long},
				{manifest.ModeCommit, "-abcdef0"},
				{manifest.Mode("tags"), "v1"},
				{manifest.Mode(""), "v1"},
			} {
				sub, err := f.set("lib", c.mode, c.ref)
				wantErr(t, "Set("+string(c.mode)+", "+c.ref+")", err, core.ErrInvalidArgument)
				if sub != (manifest.Submodule{}) || err == nil ||
					!strings.HasPrefix(err.Error(), "lib: invalid ") {
					t.Errorf("Set(%s, %q) = %+v, %v", c.mode, c.ref, sub, err)
				}
			}
			wantSameTree(t, "invalid Set", before, treeState(t, f.super.Dir))
		})
	}
}

func TestSetNames(t *testing.T) {
	t.Parallel()
	f := newFixture(t, gittest.SHA1)
	f.add("lib", manifest.ModeTag, gittest.TagV100)
	gittest.Git(t, f.super.Dir, "submodule", "add", "--quiet", "--name", "fpga.core",
		"--", f.up.Bare, "ip/fpga-core")
	for _, name := range []string{"nope", "", "LIB", "lib/", "ip/fpga-core"} {
		_, err := f.set(name, manifest.ModeTag, gittest.TagV101)
		wantErr(t, "Set("+name+")", err, core.ErrNotFound)
	}
	sub, err := f.set("fpga.core", manifest.ModeTag, gittest.TagV101)
	if err != nil || sub.Path != "ip/fpga-core" || sub.Name != "fpga.core" {
		t.Errorf("Set(fpga.core) = %+v, %v", sub, err)
	}
	wantTracking(t, f, "fpga.core", manifest.ModeTag, gittest.TagV101, "")
	wantTracking(t, f, "lib", manifest.ModeTag, gittest.TagV100, "")
}

func TestSetRepairsInvalidMode(t *testing.T) {
	t.Parallel()
	f := newFixture(t, gittest.SHA1)
	v101 := f.commits[gittest.TagV101]
	f.add("lib", manifest.ModeTag, gittest.TagV100)
	f.add("other", manifest.ModeTag, gittest.TagV100)
	f.super.SetKey(t, "lib", manifest.KeyMode, "tags")
	f.super.SetKey(t, "other", manifest.KeyMode, "branches")
	_, err := f.repo().Status(t.Context(), nil)
	wantErr(t, "Status(invalid mode)", err, manifest.ErrInvalidMode)

	// The abbreviated commit cannot be expanded yet.
	_, err = f.set("lib", manifest.ModeCommit, v101[:8])
	wantErr(t, "Set(abbreviated)", err, core.ErrInvalidArgument)
	_, err = f.set("nope", manifest.ModeTag, gittest.TagV101)
	wantErr(t, "Set(unknown)", err, core.ErrNotFound)

	// While other is invalid, only the given values are known.
	sub, err := f.set("lib", manifest.ModeBranch, "main")
	want := manifest.Submodule{Name: "lib", Mode: manifest.ModeBranch, Ref: "main", Branch: "main"}
	if err != nil || sub != want {
		t.Errorf("Set(lib) = %+v, %v; want %+v", sub, err, want)
	}
	sub, err = f.set("other", manifest.ModeCommit, v101)
	want = manifest.Submodule{Name: "other", Path: "other", URL: f.up.Bare,
		Mode: manifest.ModeCommit, Ref: v101}
	if err != nil || sub != want {
		t.Errorf("Set(other) = %+v, %v; want %+v", sub, err, want)
	}
	wantTracking(t, f, "lib", manifest.ModeBranch, "main", "main")
	st, err := f.repo().Status(t.Context(), nil)
	if err != nil || len(st) != 2 {
		t.Errorf("Status after repair = %+v, %v", st, err)
	}
}
