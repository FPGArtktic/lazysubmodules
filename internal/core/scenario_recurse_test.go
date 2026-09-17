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

// TestScenarioSubmoduleRecurse updates tools of the complex superproject
// with submodule.recurse=true, as many users configure it: the nested
// submodule inner is never moved, and a checkout of inner that neither
// side records does not stop the update.
func TestScenarioSubmoduleRecurse(t *testing.T) {
	t.Parallel()
	c := gittest.NewComplexSuper(t, gittest.SHA1)
	g := gittest.Runner(t, append(c.Config, "submodule.recurse=true",
		"fetch.recurseSubmodules=true")...)
	repo := openRepo(t, g, c.Dir)
	ctx := t.Context()
	tools, inner := c.Tools, c.ToolsInner
	innerDir := inner.In(tools.Dir())
	older, newer := inner.Gitlink, inner.Head

	// Branch rec of tools records the newer commit of inner.
	work := tools.Upstream.Work
	c.Git(t, filepath.Join(work, "inner"), "checkout", "--quiet", "--detach", newer)
	c.Git(t, work, "commit", "--quiet", "-am", "tools: record newer inner")
	c.Git(t, work, "push", "--quiet", "origin", "HEAD:refs/heads/rec")
	if _, err := repo.Fetch(ctx, []string{tools.Name}, nil); err != nil {
		t.Fatal(err)
	}
	update := func(branch string) {
		t.Helper()
		if _, err := repo.Set(ctx, tools.Name, manifest.ModeBranch, branch); err != nil {
			t.Fatal(err)
		}
		res, err := repo.Update(ctx, core.UpdateOptions{Names: []string{tools.Name}})
		if err != nil || len(res.Changes) != 1 || !res.Changes[0].Changed() {
			t.Fatalf("update to %s: %+v, %v", branch, res, err)
		}
		if got := headOf(t, tools.Dir()); got != res.Changes[0].New.Commit {
			t.Errorf("tools at %s after the update to %s", got, branch)
		}
	}

	// inner at a commit of its own, which neither side records.
	gittest.WriteFile(t, filepath.Join(innerDir, gittest.TrackedFile), "local\n")
	c.Git(t, innerDir, "commit", "--quiet", "-am", "local change")
	local := headOf(t, innerDir)
	update("rec")
	if got := headOf(t, innerDir); got != local {
		t.Errorf("inner moved from its own commit to %s", got)
	}

	// inner at the commit that tools records.
	c.Git(t, innerDir, "checkout", "--quiet", "--detach", newer)
	update(tools.Ref)
	if got := headOf(t, innerDir); got != newer {
		t.Errorf("inner moved from %s to %s, recorded %s", newer, got, older)
	}
	if st := statusOf(t, repo, tools.Name); st.State != core.StateOK {
		t.Errorf("tools: state %s (%s)", st.State, st.Reason)
	}
}

// statusOf returns the status of one submodule.
func statusOf(t *testing.T, repo *core.Repo, name string) core.Status {
	t.Helper()
	st, err := repo.Status(t.Context(), []string{name})
	if err != nil || len(st) != 1 {
		t.Fatalf("Status(%s) = %v, %v", name, st, err)
	}
	return st[0]
}
