// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package core_test

import (
	"context"
	"errors"
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

// fetchNames formats fetch results as "<name>[+init][+clone]".
func fetchNames(results []core.FetchResult) []string {
	out := make([]string, 0, len(results))
	for _, res := range results {
		name := res.Submodule.Name
		if res.Init {
			name += "+init"
		}
		if res.Cloned {
			name += "+clone"
		}
		out = append(out, name)
	}
	return out
}

func TestFetch(t *testing.T) {
	t.Parallel()
	for _, format := range formats() {
		t.Run(format, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t, format)
			v101, tip := f.commits[gittest.TagV101], f.commits[gittest.TagV200RC]
			tag := manifest.ModeTag
			f.track("moved", tag, gittest.TagV101, gittest.TagV101, v101)
			f.track("deinit", tag, gittest.TagV101, gittest.TagV101, v101)
			f.add("plain", "", "")
			f.track("fresh", tag, gittest.TagV101, gittest.TagV101, v101)
			f.super.Deinit(t, "deinit")
			f.super.Deinit(t, "fresh")
			f.removeModule("fresh")
			f.up.MoveTag(t, gittest.TagV101, tip)
			newer := f.up.Commit(t, "newer")
			gittest.Git(t, f.up.Work, "push", "--quiet", "origin", "--delete",
				gittest.BranchStable)
			plainHead := headOf(t, f.dir("plain"))
			wantState(t, f.status("moved"), core.StateOK, "")

			var progress strings.Builder
			results, err := f.repo().Fetch(t.Context(), nil, &progress)
			want := []string{"moved", "deinit+init", "fresh+init+clone"}
			if got := fetchNames(results); err != nil || !slices.Equal(got, want) {
				t.Fatalf("Fetch = %q, %v; want %q", got, err, want)
			}
			if !strings.Contains(progress.String(), "Cloning into") {
				t.Errorf("progress %q", progress.String())
			}
			for _, name := range []string{"moved", "deinit", "fresh"} {
				dir := f.dir(name)
				if got := gittest.Git(t, dir, "rev-parse", "v1.0.1^{commit}"); got != tip {
					t.Errorf("%s: tag v1.0.1 at %s, want the moved tag %s", name, got, tip)
				}
				if got := gittest.Git(t, dir, "rev-parse", "origin/main"); got != newer {
					t.Errorf("%s: origin/main at %s, want %s", name, got, newer)
				}
				branches := gittest.Git(t, dir, "for-each-ref", "refs/remotes/origin/")
				if strings.Contains(branches, gittest.BranchStable) {
					t.Errorf("%s: deleted branch not pruned:\n%s", name, branches)
				}
			}
			// Fetch moves nothing; initialized submodules are at their gitlink.
			wantState(t, f.status("moved"), core.StateDrift, "now points to")
			if got := headOf(t, f.dir("fresh")); got != v101 {
				t.Errorf("fresh: HEAD %s, want %s", got, v101)
			}
			if got := headOf(t, f.dir("plain")); got != plainHead {
				t.Errorf("unmanaged submodule moved to %s", got)
			}
			if _, err := os.Stat(filepath.Join(f.super.Dir, ".git", "modules", "plain",
				"FETCH_HEAD")); err == nil {
				t.Errorf("unmanaged submodule was fetched")
			}
			wantStaged(t, f.super.Dir)
			vr, err := f.repo().Verify(t.Context(), []string{"moved"}, core.VerifyOptions{})
			wantErr(t, "Verify(moved)", err, core.ErrVerify)
			if len(vr) != 1 || vr[0].OK() {
				t.Errorf("Verify(moved) = %+v", vr)
			}
		})
	}
}

func TestFetchSelection(t *testing.T) {
	t.Parallel()
	f := newFixture(t, gittest.SHA1)
	f.add("plain", "", "")
	f.add("b", manifest.ModeTag, gittest.TagV100)
	f.add("a", manifest.ModeTag, gittest.TagV100)
	r := f.repo()
	results, err := r.Fetch(t.Context(), []string{"a", "b", "a"}, nil)
	if got := fetchNames(results); err != nil || !slices.Equal(got, []string{"b", "a"}) {
		t.Errorf("Fetch(a, b) = %q, %v", got, err)
	}
	before := treeState(t, f.super.Dir)
	results, err = r.Fetch(t.Context(), []string{"plain"}, nil)
	wantErr(t, "Fetch(unmanaged)", err, core.ErrUnmanaged)
	if results != nil {
		t.Errorf("Fetch(unmanaged) = %+v", results)
	}
	_, err = r.Fetch(t.Context(), []string{"a", "nope"}, nil)
	wantErr(t, "Fetch(unknown)", err, core.ErrNotFound)
	wantSameTree(t, "refused fetch", before, treeState(t, f.super.Dir))

	gittest.WriteFile(t, filepath.Join(f.super.Dir, manifest.File), "[submodule \"x\"\n")
	_, err = r.Fetch(t.Context(), nil, nil)
	if _, ok := errors.AsType[*git.Error](err); !ok {
		t.Errorf("Fetch(broken .gitmodules) = %v", err)
	}
}

func TestFetchFailure(t *testing.T) {
	t.Parallel()
	f := newFixture(t, gittest.SHA1)
	f.add("a", manifest.ModeTag, gittest.TagV100)
	f.add("b", manifest.ModeTag, gittest.TagV100)
	f.add("c", manifest.ModeTag, gittest.TagV100)
	gittest.Git(t, f.dir("b"), "remote", "set-url", "origin", filepath.Join(t.TempDir(), "gone"))
	results, err := f.repo().Fetch(t.Context(), nil, nil)
	gitErr, ok := errors.AsType[*git.Error](err)
	if !ok || !strings.HasPrefix(err.Error(), "b: git fetch") || gitErr.ExitCode <= 0 {
		t.Errorf("Fetch = %v, want the failure of b", err)
	}
	if got := fetchNames(results); !slices.Equal(got, []string{"a"}) {
		t.Errorf("Fetch results %q, want only a", got)
	}
	modules := filepath.Join(f.super.Dir, ".git", "modules")
	if _, err := os.Stat(filepath.Join(modules, "a", "FETCH_HEAD")); err != nil {
		t.Errorf("a was not fetched: %v", err)
	}
	if _, err := os.Stat(filepath.Join(modules, "c", "FETCH_HEAD")); err == nil {
		t.Errorf("c was fetched after the failure")
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := f.repo().Fetch(ctx, []string{"a"}, nil); !errors.Is(err, context.Canceled) {
		t.Errorf("Fetch(canceled) = %v", err)
	}
}
