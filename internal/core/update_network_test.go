// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package core_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/FPGArtktic/lazysubmodules/internal/core"
	"github.com/FPGArtktic/lazysubmodules/internal/git"
	"github.com/FPGArtktic/lazysubmodules/internal/git/gittest"
	"github.com/FPGArtktic/lazysubmodules/internal/manifest"
)

// lazyRunner returns a Runner that fetches the objects a partial clone
// lacks on demand, as git does by default, to set up partial clones.
func lazyRunner(t *testing.T) *git.Runner {
	t.Helper()
	r, err := git.New(git.WithEnv(append(gittest.Env(t), "GIT_NO_LAZY_FETCH=0")...))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// missingObjects lists the objects that the repository at dir lacks,
// without fetching them. The options are passed to git before the command.
func missingObjects(t *testing.T, dir string, opts ...string) []string {
	t.Helper()
	out := gittest.Git(t, dir, append(opts, "rev-list", "--objects", "--all",
		"--missing=print")...)
	var missing []string
	for line := range strings.Lines(out) {
		if object, ok := strings.CutPrefix(strings.TrimSpace(line), "?"); ok {
			missing = append(missing, object)
		}
	}
	return missing
}

// partialClone replaces the repository of the submodule lib with a partial
// clone without blobs; only the blobs of the checked-out commit are
// fetched. The runners of the tests allow the file transport, so only the
// network mode of the invocations keeps an update from fetching the others.
func partialClone(t *testing.T, f *fixture) {
	t.Helper()
	gittest.Git(t, f.up.Bare, "config", "uploadpack.allowFilter", "true")
	f.super.Deinit(t, "lib")
	f.removeModule("lib")
	// Git ignores the filter when it clones a local path, but not a file URL.
	_, err := lazyRunner(t).Run(t.Context(), f.super.Dir,
		"-c", "url.file://"+f.up.Bare+".insteadOf="+f.up.Bare,
		"submodule", "update", "--quiet", "--init", "--filter=blob:none", "--", "lib")
	if err != nil {
		t.Fatal(err)
	}
	if len(missingObjects(t, f.dir("lib"))) == 0 {
		t.Fatal("the submodule repository is not a partial clone")
	}
}

func TestUpdatePartialCloneStaysOffline(t *testing.T) {
	t.Parallel()
	for _, format := range formats() {
		t.Run(format, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t, format)
			tip, v101 := f.commits[gittest.TagV200RC], f.commits[gittest.TagV101]
			f.add("lib", manifest.ModeTagPattern, "v1.*")
			partialClone(t, f)
			missing := missingObjects(t, f.dir("lib"))
			before := treeState(t, f.super.Dir)

			// The target lacks its blobs; without Fetch, the checkout fails
			// instead of fetching them, and the update is rolled back.
			for _, opts := range []core.UpdateOptions{{}, {Commit: true}} {
				_, err := f.update(opts)
				if _, ok := errors.AsType[*git.Error](err); !ok || errors.Is(err, core.ErrRefused) ||
					!strings.HasPrefix(err.Error(), "lib: git ") {
					t.Errorf("Update(%+v) = %v, want the failed checkout", opts, err)
				}
				if got := missingObjects(t, f.dir("lib")); !slices.Equal(got, missing) {
					t.Errorf("Update(%+v) fetched objects: missing %q, want %q", opts, got, missing)
				}
				wantSameTree(t, "offline update", before, treeState(t, f.super.Dir))
			}
			if got := headOf(t, f.dir("lib")); got != tip {
				t.Errorf("HEAD %s, want %s", got, tip)
			}

			// A dry run needs no objects.
			res, err := f.update(core.UpdateOptions{DryRun: true})
			if err != nil || oneChange(t, res).New.Commit != v101 {
				t.Errorf("Update(dry run) = %+v, %v", res, err)
			}

			// With Fetch, the checkout may fetch what it lacks.
			c := oneChange(t, f.mustUpdate(core.UpdateOptions{Fetch: true}))
			if c.New.Commit != v101 || headOf(t, f.dir("lib")) != v101 {
				t.Errorf("Update(fetch) = %+v", c)
			}
			if got := missingObjects(t, f.dir("lib")); len(got) >= len(missing) {
				t.Errorf("Update(fetch) fetched nothing: missing %q", got)
			}
		})
	}
}

func TestUpdateOfflineInitPartialClone(t *testing.T) {
	t.Parallel()
	for _, removed := range []bool{false, true} {
		t.Run(map[bool]string{false: "deinitialized", true: "removed"}[removed], func(t *testing.T) {
			t.Parallel()
			f := newFixture(t, gittest.SHA1)
			f.add("lib", manifest.ModeTagPattern, "v1.*")
			partialClone(t, f)
			v101 := f.commits[gittest.TagV101]
			// The superproject records v1.0.1, whose blobs the submodule
			// repository lacks.
			gittest.Git(t, f.super.Dir, "update-index", "--cacheinfo", "160000,"+v101+",lib")
			gittest.Git(t, f.super.Dir, "commit", "--quiet", "-m", "move lib")
			if removed {
				// The repository names the removed working tree.
				if err := os.RemoveAll(f.dir("lib")); err != nil {
					t.Fatal(err)
				}
			} else {
				f.super.Deinit(t, "lib")
			}
			module := filepath.Join(f.super.Dir, ".git", "modules", "lib")
			// Git cannot run in a repository whose working tree is missing.
			inModule := []string{"--git-dir=" + module, "--work-tree=" + module}
			missing := missingObjects(t, module, inModule...)
			before := treeState(t, f.super.Dir)

			// The initialization would fail only after registering the
			// submodule, so it is refused before.
			want := "lib: refused: submodule is not initialized (use --fetch): " +
				"its repository lacks objects of the recorded commit " + v101[:12]
			for _, opts := range []core.UpdateOptions{{}, {Commit: true}, {DryRun: true}} {
				res, err := f.update(opts)
				what := fmt.Sprintf("Update(%+v)", opts)
				if removed && opts.DryRun {
					// A dry run cannot read the repository.
					if err != nil || !oneChange(t, res).Init || res.Changes[0].New.Commit != "" {
						t.Errorf("%s = %+v, %v", what, res, err)
					}
					continue
				}
				wantErr(t, what, err, core.ErrRefused, core.ErrUninitialized)
				if err == nil || err.Error() != want || len(res.Changes) != 0 {
					t.Errorf("%s = %+v, %v\nwant %s", what, res, err, want)
				}
			}
			wantSameTree(t, "refused update", before, treeState(t, f.super.Dir))
			if got := missingObjects(t, module, inModule...); !slices.Equal(got, missing) {
				t.Errorf("objects were fetched: missing %q, want %q", got, missing)
			}
			wantState(t, f.status("lib"), core.StateUninitialized, "not checked out")

			// With Fetch, the initialization fetches what the commit lacks.
			c := oneChange(t, f.mustUpdate(core.UpdateOptions{Fetch: true}))
			wantSteps(t, c, true, false)
			if c.New.Commit != v101 || headOf(t, f.dir("lib")) != v101 {
				t.Errorf("Update(fetch) = %+v", c)
			}
			wantState(t, f.status("lib"), core.StateOK, "up to date")
		})
	}
}
