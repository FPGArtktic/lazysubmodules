// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package core_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FPGArtktic/lazysubmodules/internal/core"
	"github.com/FPGArtktic/lazysubmodules/internal/git"
	"github.com/FPGArtktic/lazysubmodules/internal/git/gittest"
	"github.com/FPGArtktic/lazysubmodules/internal/lock"
	"github.com/FPGArtktic/lazysubmodules/internal/manifest"
)

// formats lists the object formats the repository tests run with.
func formats() []string {
	return []string{gittest.SHA1, gittest.SHA256}
}

// fixture is a superproject whose submodules are clones of one tagged
// upstream (see gittest.NewTaggedUpstream).
type fixture struct {
	t       *testing.T
	up      *gittest.Upstream
	commits map[string]string
	super   *gittest.Super
	g       *git.Runner
}

// newFixture creates the upstream and the superproject. The config entries
// are passed to the Runner used by the Repo.
func newFixture(t *testing.T, format string, config ...string) *fixture {
	t.Helper()
	up, commits := gittest.NewTaggedUpstream(t, format)
	return &fixture{
		t:       t,
		up:      up,
		commits: commits,
		super:   gittest.NewSuper(t, format),
		g:       gittest.Runner(t, config...),
	}
}

// add adds a submodule cloned from the upstream and commits it. An empty
// mode leaves the submodule unmanaged. The clone is on branch main, at the
// commit of gittest.TagV200RC.
func (f *fixture) add(name string, mode manifest.Mode, ref string) {
	f.t.Helper()
	f.super.AddSubmodule(f.t, name, f.up)
	if mode != "" {
		f.configure(name, mode, ref)
		f.super.Commit(f.t, "track "+name)
	}
}

// configure writes the tracking configuration without committing.
func (f *fixture) configure(name string, mode manifest.Mode, ref string) {
	f.t.Helper()
	f.super.SetKey(f.t, name, manifest.KeyMode, string(mode))
	f.super.SetKey(f.t, name, manifest.KeyRef, ref)
}

// dir returns the working tree of a submodule.
func (f *fixture) dir(name string) string {
	return filepath.Join(f.super.Dir, name)
}

// checkout detaches the submodule HEAD at rev.
func (f *fixture) checkout(name, rev string) {
	f.t.Helper()
	gittest.Git(f.t, f.dir(name), "checkout", "--quiet", "--detach", rev)
}

// lock writes a lock entry without committing.
func (f *fixture) lock(name string, mode manifest.Mode, ref, commit string) {
	f.t.Helper()
	e := lock.Entry{Name: name, Mode: mode, Ref: ref, Commit: commit}
	if err := lock.Write(f.t.Context(), f.g, f.super.Dir, e); err != nil {
		f.t.Fatal(err)
	}
}

// pin does what an update does: it checks out commit, locks it with mode
// and ref and commits the superproject.
func (f *fixture) pin(name string, mode manifest.Mode, ref, commit string) {
	f.t.Helper()
	f.checkout(name, commit)
	f.lock(name, mode, ref, commit)
	f.super.Commit(f.t, "pin "+name)
}

// track adds a managed submodule and pins it to the given lock state.
func (f *fixture) track(name string, mode manifest.Mode, ref, lockRef, commit string) {
	f.t.Helper()
	f.add(name, mode, ref)
	f.pin(name, mode, lockRef, commit)
}

// repo opens the superproject.
func (f *fixture) repo() *core.Repo {
	f.t.Helper()
	r, err := core.Open(f.t.Context(), f.g, f.super.Dir)
	if err != nil {
		f.t.Fatal(err)
	}
	return r
}

// lib returns the configuration of the submodule "lib".
func (f *fixture) lib() manifest.Submodule {
	f.t.Helper()
	subs, err := f.repo().Submodules(f.t.Context())
	if err != nil {
		f.t.Fatal(err)
	}
	sub, ok := manifest.Find(subs, "lib")
	if !ok {
		f.t.Fatal("submodule lib not found")
	}
	return sub
}

// status returns the status of one submodule.
func (f *fixture) status(name string) core.Status {
	f.t.Helper()
	st, err := f.repo().Status(f.t.Context(), []string{name})
	if err != nil {
		f.t.Fatalf("Status(%s): %v", name, err)
	}
	if len(st) != 1 {
		f.t.Fatalf("Status(%s) returned %d results", name, len(st))
	}
	return st[0]
}

// fetchLib fetches the upstream into the submodule "lib" as
// "lazysubmodules fetch" does, so that moved tags replace the local ones.
func (f *fixture) fetchLib() {
	f.t.Helper()
	if err := f.g.Fetch(f.t.Context(), f.dir("lib"), "origin", nil); err != nil {
		f.t.Fatal(err)
	}
}

// removeModule deletes the repository of a deinitialized submodule.
func (f *fixture) removeModule(name string) {
	f.t.Helper()
	if err := os.RemoveAll(filepath.Join(f.super.Dir, ".git", "modules", name)); err != nil {
		f.t.Fatal(err)
	}
}

// wantState checks the state of a status and that its reason contains
// reason.
func wantState(t *testing.T, st core.Status, state core.State, reason string) {
	t.Helper()
	if st.State != state || !strings.Contains(st.Reason, reason) {
		t.Errorf("%s: state %s (%s), want %s (%s)",
			st.Submodule.Name, st.State, st.Reason, state, reason)
	}
}

// wantErr checks that err wraps every target.
func wantErr(t *testing.T, what string, err error, targets ...error) {
	t.Helper()
	if err == nil {
		t.Errorf("%s: nil error, want %v", what, targets)
		return
	}
	for _, target := range targets {
		if !errors.Is(err, target) {
			t.Errorf("%s: %v, want an error wrapping %v", what, err, target)
		}
	}
}
