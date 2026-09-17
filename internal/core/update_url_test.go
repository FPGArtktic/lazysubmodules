// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package core_test

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/FPGArtktic/lazysubmodules/internal/core"
	"github.com/FPGArtktic/lazysubmodules/internal/git/gittest"
	"github.com/FPGArtktic/lazysubmodules/internal/manifest"
)

func TestUpdateRefusesMissingURL(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name string
		// url is the url value written to .gitmodules; empty removes it.
		url    string
		remove bool
	}{
		{name: "no url, deinitialized"},
		{name: "no url, repository removed", remove: true},
		{name: "option url, deinitialized", url: "--upload-pack=touch "},
		{name: "option url, repository removed", url: "--upload-pack=touch ", remove: true},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t, gittest.SHA1)
			v100 := f.commits[gittest.TagV100]
			f.track("lib", manifest.ModeTag, gittest.TagV100, gittest.TagV100, v100)
			f.track("other", manifest.ModeTag, gittest.TagV100, gittest.TagV100, v100)
			f.super.Deinit(t, "lib")
			if c.remove {
				f.removeModule("lib")
			}
			// Git ignores the url of both entries: a checked-out submodule
			// is still fetched from its own origin.
			pwned := filepath.Join(t.TempDir(), "pwned")
			for _, name := range []string{"lib", "other"} {
				if c.url == "" {
					gittest.Git(t, f.super.Dir, "config", "-f", manifest.File, "--unset",
						"submodule."+name+"."+manifest.KeyURL)
				} else {
					f.super.SetKey(t, name, manifest.KeyURL, c.url+pwned)
				}
			}
			before := treeState(t, f.super.Dir)

			for _, opts := range []core.UpdateOptions{
				{}, {Fetch: true}, {DryRun: true}, {DryRun: true, Fetch: true},
				{Fetch: true, Commit: true}, {Names: []string{"other", "lib"}, Fetch: true},
			} {
				res, err := f.update(opts)
				what := fmt.Sprintf("Update(%+v)", opts)
				wantErr(t, what, err, core.ErrNoURL, core.ErrRefused)
				want := "lib: refused: .gitmodules records no usable url"
				if err == nil || err.Error() != want || len(res.Changes) != 0 {
					t.Errorf("%s = %+v, %v; want %s", what, res, err, want)
				}
			}
			results, err := f.repo().Fetch(t.Context(), nil, nil)
			wantErr(t, "Fetch", err, core.ErrNoURL)
			if results != nil {
				t.Errorf("Fetch = %+v", results)
			}
			// Git would have registered the submodule in .git/config.
			wantSameTree(t, "refused update", before, treeState(t, f.super.Dir))
			if got := readFile(t, pwned); got != "<missing>" {
				t.Errorf("the url ran a command: %q", got)
			}

			// A checked-out submodule needs no url.
			c := oneChange(t, f.mustUpdate(core.UpdateOptions{
				Names: []string{"other"}, Fetch: true,
			}))
			if c.Changed() || c.New.Commit != v100 {
				t.Errorf("Update(other) = %+v", c)
			}
			if _, err := f.repo().Fetch(t.Context(), []string{"other"}, nil); err != nil {
				t.Errorf("Fetch(other) = %v", err)
			}
			wantState(t, f.status("lib"), core.StateUninitialized, "not checked out")
		})
	}
}
