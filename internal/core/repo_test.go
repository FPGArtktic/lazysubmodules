// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package core_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/FPGArtktic/lazysubmodules/internal/core"
	"github.com/FPGArtktic/lazysubmodules/internal/git"
	"github.com/FPGArtktic/lazysubmodules/internal/git/gittest"
	"github.com/FPGArtktic/lazysubmodules/internal/manifest"
)

// realDir resolves symbolic links in dir.
func realDir(t *testing.T, dir string) string {
	t.Helper()
	res, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestOpen(t *testing.T) {
	t.Parallel()
	for _, format := range formats() {
		t.Run(format, func(t *testing.T) {
			t.Parallel()
			super := gittest.NewSuper(t, format)
			sub := filepath.Join(super.Dir, "a", "b")
			if err := os.MkdirAll(sub, 0o755); err != nil {
				t.Fatal(err)
			}
			for _, dir := range []string{super.Dir, sub} {
				r, err := core.Open(t.Context(), gittest.Runner(t), dir)
				if err != nil {
					t.Fatalf("Open(%s) = %v", dir, err)
				}
				if realDir(t, r.Root()) != realDir(t, super.Dir) {
					t.Errorf("Open(%s).Root() = %s, want %s", dir, r.Root(), super.Dir)
				}
			}
		})
	}
}

func TestOpenErrors(t *testing.T) {
	t.Parallel()
	g := gittest.Runner(t)
	up := gittest.NewUpstream(t, gittest.SHA1)
	for name, dir := range map[string]string{
		"outside a repository": t.TempDir(),
		"bare repository":      up.Bare,
		"missing directory":    filepath.Join(t.TempDir(), "missing"),
	} {
		r, err := core.Open(t.Context(), g, dir)
		if _, ok := errors.AsType[*git.Error](err); !ok || r != nil {
			t.Errorf("Open(%s) = %v, %v; want *git.Error", name, r, err)
		}
	}
}

func TestSubmodules(t *testing.T) {
	t.Parallel()
	f := newFixture(t, gittest.SHA1)
	f.add("lib", manifest.ModeTagPattern, "v1.*")
	f.add("plain", "", "")
	subs, err := f.repo().Submodules(t.Context())
	want := []manifest.Submodule{
		{Name: "lib", Path: "lib", URL: f.up.Bare, Mode: manifest.ModeTagPattern, Ref: "v1.*"},
		{Name: "plain", Path: "plain", URL: f.up.Bare},
	}
	if err != nil || len(subs) != len(want) || subs[0] != want[0] || subs[1] != want[1] {
		t.Errorf("Submodules = %+v, %v; want %+v", subs, err, want)
	}
}
