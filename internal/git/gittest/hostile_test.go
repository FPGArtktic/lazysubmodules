// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package gittest_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/FPGArtktic/lazysubmodules/internal/git/gittest"
)

func TestHostileSuper(t *testing.T) {
	t.Parallel()
	for _, format := range formats() {
		t.Run(format, func(t *testing.T) {
			t.Parallel()
			h := gittest.NewHostileSuper(t, format)
			g := gittest.Runner(t)
			subs := h.Submodules()
			gitDir := filepath.Join(h.Dir, ".git")
			samePath(t, "outside", h.Outside, filepath.Join(h.Dir, "..", "outside"))
			checkFiles(t, g, h.Dir, subs)
			checkGitlinks(t, g, h.Dir, "HEAD", subs)
			checkCheckouts(t, g, gitDir, "", subs)
			checkHostileValues(t, h)
			checkLures(t, h)
			wantClean(t, h.Dir)

			// Git itself refuses the suspicious name and URL.
			for _, s := range []*gittest.Submodule{h.DotDot, h.DashURL} {
				out, err := tryGit(t, g, h.Dir, "submodule", "update", "--init", "--", s.Path)
				if err == nil {
					t.Errorf("%s: git initialized it: %s", s.Name, out)
				}
			}
			wantNoPwned(t, h)

			t.Run("symbolic links", func(t *testing.T) { checkLinks(t, h) })
		})
	}
}

// checkHostileValues verifies the values of the hostile entries.
func checkHostileValues(t *testing.T, h *gittest.HostileSuper) {
	t.Helper()
	names := []string{
		"good", "../../../outside/modules/dotdot", "escape", "absolute", "via-link", "self",
		"dotgit", "dash-ref", "dash-branch", "dash-pattern", "dash-commit", "dash-url",
		"ctrl-ref", "newline-ref", `we"ird\name`,
	}
	var got []string
	for _, s := range h.Submodules() {
		got = append(got, s.Name)
		dash := strings.HasPrefix(s.Name, "dash-") && s != h.DashURL
		if dash != strings.HasPrefix(s.Ref, "--output="+h.Outside+"/pwned-"+s.Name) {
			t.Errorf("%s: ref %q", s.Name, s.Ref)
		}
	}
	if !slices.Equal(got, names) {
		t.Errorf("names %q, want %q", got, names)
	}
	lib := filepath.Join(h.Outside, "lib")
	checks := map[string]bool{
		"escape path":      h.Escape.Path == "../outside/lib" && h.Escape.Dir() == lib,
		"absolute path":    h.Absolute.Path == lib && h.Absolute.Dir() == lib,
		"absolute branch":  h.Absolute.Branch == "main" && h.Absolute.Lock == nil,
		"self path":        h.Self.Path == "." && h.Self.Dir() == h.Dir,
		"dotgit path":      h.DotGit.Dir() == h.Good.GitDir,
		"dash-branch key":  h.DashBranch.Branch == h.DashBranch.Ref,
		"dash-ref lock":    h.DashRef.Lock != nil && h.DashRef.Lock.Ref == h.DashRef.Ref,
		"dash-url":         h.DashURL.URL == "--upload-pack=touch "+h.Outside+"/pwned-dash-url",
		"control ref":      strings.Contains(h.CtrlRef.Ref, "\x1b]0;") && h.CtrlRef.Lock != nil,
		"control lock":     h.CtrlRef.Lock != nil && h.CtrlRef.Lock.Ref == h.CtrlRef.Ref,
		"newline ref":      h.NewlineRef.Ref == "v1.0.0\nv1.0.1" && h.NewlineRef.Lock == nil,
		"quoted checkout":  h.Quoted.Head != "" && h.Quoted.Lock == nil,
		"dotdot not clone": h.DotDot.Head == "" && h.DotDot.GitDir == "" && h.DotDot.Lock != nil,
		"escape lock":      h.Escape.Lock != nil && h.Escape.Gitlink == "",
	}
	for what, ok := range checks {
		if !ok {
			t.Errorf("%s: unexpected value", what)
		}
	}
}

// checkLures verifies what the hostile entries point to.
func checkLures(t *testing.T, h *gittest.HostileSuper) {
	t.Helper()
	g := gittest.Runner(t)
	lib := filepath.Join(h.Outside, "lib")
	samePath(t, "outside repository", mustGit(t, g, lib, "rev-parse", "--show-toplevel"), lib)
	samePath(t, "path through the link", h.ViaLink.Dir(), lib)
	dotdot := filepath.Join(h.Outside, "modules", "dotdot")
	if got := mustGit(t, g, dotdot, "rev-parse", "--is-bare-repository"); got != "true" {
		t.Errorf("%s is not a bare repository: %s", dotdot, got)
	}
	module := mustGit(t, g, h.Dir, "rev-parse", "--git-path", "modules/"+h.DotDot.Name)
	samePath(t, "repository of the dotdot name", filepath.Join(h.Dir, module), dotdot)

	if got := mustGit(t, g, h.Dir, "ls-files", "--stage", "--", "linked"); !strings.HasPrefix(
		got, "120000 ") {
		t.Errorf("linked is not a committed symbolic link: %q", got)
	}
	root := mustGit(t, g, h.Dir, "rev-list", "--max-parents=0", "HEAD")
	if got := mustGit(t, g, h.Dir, "rev-parse", gittest.TagV100+"^{commit}"); got != root {
		t.Errorf("superproject tag at %s, want %s", got, root)
	}
	for file, copied := range map[string]string{
		gittest.LockFile:       gittest.OutsideLock,
		gittest.GitmodulesFile: gittest.OutsideGitmodules,
	} {
		want, err := os.ReadFile(filepath.Join(h.Dir, file))
		if err != nil {
			t.Fatal(err)
		}
		if got, err := os.ReadFile(filepath.Join(h.Outside, copied)); err != nil ||
			string(got) != string(want) {
			t.Errorf("copy of %s: %q, %v", file, got, err)
		}
	}
}

// wantNoPwned reports files that a hostile value created.
func wantNoPwned(t *testing.T, h *gittest.HostileSuper) {
	t.Helper()
	if found, err := filepath.Glob(filepath.Join(h.Outside, "pwned*")); err != nil ||
		len(found) != 0 {
		t.Errorf("created %q, %v", found, err)
	}
}

// checkLinks verifies LinkLock and LinkGitmodules; it changes the fixture.
func checkLinks(t *testing.T, h *gittest.HostileSuper) {
	t.Helper()
	g := gittest.Runner(t)
	subs := h.Submodules()
	h.LinkLock(t)
	wantLink(t, filepath.Join(h.Dir, gittest.LockFile), "../outside/"+gittest.OutsideLock)
	if got := mustGit(t, g, h.Dir, "ls-files", "--stage", "--", gittest.LockFile); !strings.
		HasPrefix(got, "120000 ") {
		t.Errorf("committed lock file: %q", got)
	}
	if got := mustGit(t, g, h.Dir, "cat-file", "-p", "HEAD:"+gittest.LockFile); got !=
		"../outside/"+gittest.OutsideLock {
		t.Errorf("committed link target %q", got)
	}
	// Git follows the link when it reads the file.
	checkEntries(t, "linked lock file", configEntries(t, g, h.Dir, "-f", gittest.LockFile),
		wantLock(subs))
	wantClean(t, h.Dir)

	h.LinkGitmodules(t)
	wantLink(t, filepath.Join(h.Dir, gittest.GitmodulesFile),
		"../outside/"+gittest.OutsideGitmodules)
	checkEntries(t, "linked .gitmodules", configEntries(t, g, h.Dir, "-f",
		gittest.GitmodulesFile), wantGitmodules(subs))
	checkEntries(t, "committed .gitmodules", configEntries(t, g, h.Dir,
		"--blob=HEAD:"+gittest.GitmodulesFile), wantGitmodules(subs))
	status, err := g.Run(t.Context(), h.Dir, "status", "--porcelain")
	if err != nil || status != " T .gitmodules\n" {
		t.Errorf("status %q, %v", status, err)
	}
	wantNoPwned(t, h)
}

// wantLink reports when p is not a symbolic link to target.
func wantLink(t *testing.T, p, target string) {
	t.Helper()
	if got, err := os.Readlink(p); err != nil || got != target {
		t.Errorf("%s links to %q (%v), want %q", p, got, err, target)
	}
}
