// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package core_test

import (
	"path/filepath"
	"testing"

	"github.com/FPGArtktic/lazysubmodules/internal/core"
	"github.com/FPGArtktic/lazysubmodules/internal/git/gittest"
	"github.com/FPGArtktic/lazysubmodules/internal/manifest"
)

// TestMirror uses a URL that git rewrites with url.<base>.insteadOf to the
// local upstream; only the file protocol is allowed, so nothing can reach
// the host of the original URL.
func TestMirror(t *testing.T) {
	t.Parallel()
	const url = "https://git.example.invalid/lib.git"
	up, commits := gittest.NewTaggedUpstream(t, gittest.SHA1)
	f := &fixture{t: t, up: up, commits: commits, super: gittest.NewSuper(t, gittest.SHA1),
		g: gittest.Runner(t, "url."+up.Bare+".insteadOf="+url)}
	v100, v101 := commits[gittest.TagV100], commits[gittest.TagV101]

	change, err := f.repo().Add(t.Context(), core.AddOptions{URL: url, Path: "lib",
		Mode: manifest.ModeTagPattern, Ref: "v1.0.*"})
	if err != nil || change.New.Commit != v101 {
		t.Fatalf("Add = %+v, %v", change, err)
	}
	if got := gitmodulesKey(t, f.super.Dir, "lib", manifest.KeyURL); got != url {
		t.Errorf("recorded URL %q, want %q", got, url)
	}
	f.super.Commit(t, "add lib")

	// A fresh clone of the superproject gets the submodule through the
	// mirror, and so do later fetches.
	clone := filepath.Join(t.TempDir(), "clone")
	gittest.Git(t, f.super.Dir, "clone", "--quiet", f.super.Dir, clone)
	r, err := core.Open(t.Context(), f.g, clone)
	if err != nil {
		t.Fatal(err)
	}
	res, err := r.Update(t.Context(), core.UpdateOptions{Fetch: true})
	if err != nil || len(res.Changes) != 1 || !res.Changes[0].Clone ||
		res.Changes[0].New.Commit != v101 {
		t.Fatalf("Update(fetch) = %+v, %v", res, err)
	}
	up.MoveTag(t, gittest.TagV101, v100)
	results, err := r.Fetch(t.Context(), nil, nil)
	if err != nil || len(results) != 1 || results[0].Init {
		t.Errorf("Fetch = %+v, %v", results, err)
	}
	if got := gittest.Git(t, filepath.Join(clone, "lib"), "rev-parse",
		gittest.TagV101+"^{commit}"); got != v100 {
		t.Errorf("moved tag at %s, want %s", got, v100)
	}
	if got := gittest.Git(t, filepath.Join(clone, "lib"), "remote", "get-url",
		"origin"); got != url {
		t.Errorf("origin %q, want %q", got, url)
	}
}
