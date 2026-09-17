// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package core_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode"

	"github.com/FPGArtktic/lazysubmodules/internal/core"
	"github.com/FPGArtktic/lazysubmodules/internal/git"
	"github.com/FPGArtktic/lazysubmodules/internal/git/gittest"
	"github.com/FPGArtktic/lazysubmodules/internal/lock"
	"github.com/FPGArtktic/lazysubmodules/internal/manifest"
)

// hostileScenario runs every command against the hostile superproject.
// Its steps run in order and build on each other.
type hostileScenario struct {
	h    *gittest.HostileSuper
	g    *git.Runner
	repo *core.Repo
	// outside is the state of the directory that nothing may modify.
	outside map[string]string
}

func TestScenarioHostileSuper(t *testing.T) {
	t.Parallel()
	for _, format := range formats() {
		t.Run(format, func(t *testing.T) {
			t.Parallel()
			h := gittest.NewHostileSuper(t, format)
			s := &hostileScenario{h: h, g: gittest.Runner(t), outside: treeState(t, h.Outside)}
			s.repo = openRepo(t, s.g, h.Dir)
			for _, step := range []struct {
				name string
				run  func(t *testing.T)
			}{
				{"invalid lock entries", s.invalidLock},
				{"invalid entries", s.invalidEntries},
				{"skipped entries", s.skippedEntries},
				{"commit", s.commit},
				{"symbolic links", s.symlinks},
			} {
				ok := t.Run(step.name, step.run)
				s.wantOutsideUntouched(t)
				if !ok {
					t.FailNow()
				}
			}
		})
	}
}

// wantOutsideUntouched checks that nothing was written next to the
// superproject.
func (s *hostileScenario) wantOutsideUntouched(t *testing.T) {
	t.Helper()
	wantSameTree(t, "the commands", s.outside, treeState(t, s.h.Outside))
	entries, err := os.ReadDir(filepath.Dir(s.h.Dir))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if !slices.Equal(names, []string{"outside", "super"}) {
		t.Errorf("next to the superproject: %q", names)
	}
}

// Refusals of the hostile entries that no command may modify.
const (
	viaLinkRefusal = "via-link: refused: submodule path contains a symbolic link"
	dotGitRefusal  = "dotgit: refused: the index records no submodule at its path"
	dashURLRefusal = "dash-url: refused: .gitmodules records no usable url"
)

// fetchRefusals is the refusal of a fetch of every submodule.
const fetchRefusals = viaLinkRefusal + "\n" + dotGitRefusal + "\n" + dashURLRefusal

// updateRefusals returns the refusal of an update of every submodule.
func (s *hostileScenario) updateRefusals() string {
	h := s.h
	bad := func(name, what, ref, reason string) string {
		return fmt.Sprintf("%s: refused: bad ref in .gitmodules: invalid %s %q: %s",
			name, what, ref, reason)
	}
	const dash, ctrl = `starts with "-"`, "contains white space or control characters"
	return strings.Join([]string{
		viaLinkRefusal, dotGitRefusal,
		bad("dash-ref", "tag", h.DashRef.Ref, dash),
		bad("dash-branch", "branch", h.DashBranch.Ref, dash),
		bad("dash-pattern", "tag pattern", h.DashPattern.Ref, dash),
		bad("dash-commit", "commit", h.DashCommit.Ref, dash),
		dashURLRefusal,
		bad("ctrl-ref", "tag", h.CtrlRef.Ref, ctrl),
		bad("newline-ref", "tag", h.NewlineRef.Ref, ctrl),
	}, "\n")
}

// runForeach runs Foreach with a command that prints the name and the path
// of each submodule.
func (s *hostileScenario) runForeach(t *testing.T) (string, string, error) {
	t.Helper()
	var stdout, stderr strings.Builder
	err := s.repo.Foreach(t.Context(), core.ForeachOptions{
		Args:   []string{"sh", "-c", `printf '%s|%s\n' "$name" "$sm_path"`},
		Stdout: &stdout, Stderr: &stderr,
	})
	return stdout.String(), stderr.String(), err
}

// wantForeach checks that Foreach runs in the checked-out submodules only.
func (s *hostileScenario) wantForeach(t *testing.T) {
	t.Helper()
	stdout, stderr, err := s.runForeach(t)
	want := "good|good\ndash-ref|dash-ref\ndash-branch|dash-branch\n" +
		"dash-pattern|dash-pattern\ndash-commit|dash-commit\nctrl-ref|ctrl-ref\n" +
		"newline-ref|newline-ref\n" + s.h.Quoted.Name + "|weird\n"
	notes := "skipping via-link: submodule path contains a symbolic link\n" +
		"skipping dotgit: the index records no submodule at the path\n" +
		"skipping dash-url: submodule is not checked out\n"
	if err != nil || stdout != want || stderr != notes {
		t.Errorf("Foreach = %v\nstdout:\n%s\nwant:\n%s\nstderr:\n%s", err, stdout, want, stderr)
	}
}

// wantPrintable checks that a message holds no control characters other
// than newlines.
func wantPrintable(t *testing.T, what, msg string) {
	t.Helper()
	if strings.ContainsFunc(msg, func(r rune) bool { return r != '\n' && unicode.IsControl(r) }) {
		t.Errorf("%s: %q contains control characters", what, msg)
	}
}

// invalidLock runs the commands while the lock file has entries with
// hostile refs: the commands that read the lock file refuse to work, the
// others refuse the hostile entries of .gitmodules.
func (s *hostileScenario) invalidLock(t *testing.T) {
	h, r := s.h, s.repo
	before := treeState(t, h.Dir)
	wantInvalid := func(what string, err error) {
		t.Helper()
		wantErr(t, what, err, lock.ErrInvalidEntry)
		if err != nil {
			wantPrintable(t, what, err.Error())
		}
	}
	_, err := r.Status(t.Context(), nil)
	wantInvalid("Status", err)
	for _, opts := range []core.UpdateOptions{
		{}, {Fetch: true}, {DryRun: true}, {Commit: true}, {Names: []string{"good"}},
	} {
		_, err := r.Update(t.Context(), opts)
		wantInvalid(fmt.Sprintf("Update(%+v)", opts), err)
	}
	_, err = r.Verify(t.Context(), nil, core.VerifyOptions{})
	wantInvalid("Verify", err)

	res, err := r.Fetch(t.Context(), nil, nil)
	wantErr(t, "Fetch", err, core.ErrSymlinkPath, core.ErrNotSubmodule, core.ErrNoURL)
	if err == nil || err.Error() != fetchRefusals || res != nil {
		t.Errorf("Fetch = %+v, %v", res, err)
	}
	res, err = r.Fetch(t.Context(), []string{h.DashURL.Name}, nil)
	wantErr(t, "Fetch(dash-url)", err, core.ErrNoURL)
	if res != nil {
		t.Errorf("Fetch(dash-url) = %+v", res)
	}
	s.wantForeach(t)
	wantSameTree(t, "commands with an invalid lock file", before, treeState(t, h.Dir))
}

// hostileRows returns the status of the hostile superproject without the
// invalid lock entries.
func (s *hostileScenario) hostileRows() []statusRow {
	h := s.h
	invalid := func(what, ref, reason string) string {
		return fmt.Sprintf("invalid %s %q: %s", what, ref, reason)
	}
	const dash, ctrl = `starts with "-"`, "contains white space or control characters"
	return []statusRow{
		{"good", core.StateOK, "up to date"},
		{"via-link", core.StateUninitialized, "submodule path contains a symbolic link"},
		{"dotgit", core.StateUninitialized, "the index records no submodule at the path"},
		{"dash-ref", core.StateMissingRef, invalid("tag", h.DashRef.Ref, dash)},
		{"dash-branch", core.StateMissingRef, invalid("branch", h.DashBranch.Ref, dash)},
		{"dash-pattern", core.StateMissingRef, invalid("tag pattern", h.DashPattern.Ref, dash)},
		{"dash-commit", core.StateMissingRef, invalid("commit", h.DashCommit.Ref, dash)},
		{"dash-url", core.StateUninitialized, "submodule is not checked out"},
		{"ctrl-ref", core.StateMissingRef, invalid("tag", h.CtrlRef.Ref, ctrl)},
		{"newline-ref", core.StateMissingRef, invalid("tag", h.NewlineRef.Ref, ctrl)},
		{h.Quoted.Name, core.StateBehind, "no lock entry"},
	}
}

// invalidEntries removes the lock entries with hostile refs and runs the
// commands, which report or refuse the hostile entries of .gitmodules.
func (s *hostileScenario) invalidEntries(t *testing.T) {
	h, r := s.h, s.repo
	for _, name := range []string{h.DashRef.Name, h.CtrlRef.Name} {
		runGit(t, s.g, h.Dir, "config", "-f", lock.File, "--remove-section", "submodule."+name)
	}
	before := treeState(t, h.Dir)
	st, err := r.Status(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	wantStatusRows(t, st, s.hostileRows())
	for _, sub := range st {
		if sub.Target != nil && sub.Submodule.Name != "good" && sub.Submodule.Name != h.Quoted.Name {
			t.Errorf("%q: target %+v", sub.Submodule.Name, sub.Target)
		}
	}
	for _, opts := range []core.UpdateOptions{
		{}, {Fetch: true}, {DryRun: true}, {DryRun: true, Fetch: true},
		{Fetch: true, Commit: true, IncludePrerelease: true},
	} {
		what := fmt.Sprintf("Update(%+v)", opts)
		res, err := r.Update(t.Context(), opts)
		wantRefusal(t, what, res, err, s.updateRefusals(), core.ErrSymlinkPath,
			core.ErrNotSubmodule, core.ErrMissingRef, core.ErrNoURL)
		if err != nil {
			wantPrintable(t, what, err.Error())
		}
	}
	res, err := r.Update(t.Context(), core.UpdateOptions{Names: []string{"dash-url"},
		Fetch: true})
	wantRefusal(t, "Update(dash-url)", res, err, dashURLRefusal, core.ErrNoURL)

	vr, err := r.Verify(t.Context(), nil, core.VerifyOptions{})
	want := "verification failed: via-link, dotgit, dash-ref, dash-branch, dash-pattern, " +
		"dash-commit, dash-url, ctrl-ref, newline-ref, " + strconv.Quote(h.Quoted.Name)
	if !errors.Is(err, core.ErrVerify) || err.Error() != want || len(vr) != len(st) {
		t.Errorf("Verify = %d results, %v\nwant %s", len(vr), err, want)
	}
	for _, v := range vr {
		for _, c := range v.Checks {
			wantPrintable(t, "check "+c.Name, c.Detail)
		}
	}
	s.wantForeach(t)
	wantSameTree(t, "commands on invalid entries", before, treeState(t, h.Dir))
}

// skippedEntries checks that no command accepts the names of the entries
// that git ignores: a name leaving the git directory and paths outside
// the working tree.
func (s *hostileScenario) skippedEntries(t *testing.T) {
	h, r := s.h, s.repo
	before := treeState(t, h.Dir)
	subs, err := r.Submodules(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var listed []string
	for _, sub := range subs {
		listed = append(listed, sub.Name)
	}
	if want := fixtureNames(slices.DeleteFunc(h.Submodules(), func(sub *gittest.Submodule) bool {
		return slices.Contains([]*gittest.Submodule{h.DotDot, h.Escape, h.Absolute, h.Self}, sub)
	})); !slices.Equal(listed, want) {
		t.Errorf("Submodules = %q, want %q", listed, want)
	}
	for _, sub := range []*gittest.Submodule{h.DotDot, h.Escape, h.Absolute, h.Self} {
		names := []string{sub.Name}
		_, err := r.Status(t.Context(), names)
		wantErr(t, "Status("+sub.Name+")", err, core.ErrNotFound)
		_, err = r.Update(t.Context(), core.UpdateOptions{Names: names, Fetch: true})
		wantErr(t, "Update("+sub.Name+")", err, core.ErrNotFound)
		_, err = r.Verify(t.Context(), names, core.VerifyOptions{})
		wantErr(t, "Verify("+sub.Name+")", err, core.ErrNotFound)
		_, err = r.Fetch(t.Context(), names, nil)
		wantErr(t, "Fetch("+sub.Name+")", err, core.ErrNotFound)
		if _, err := r.Set(t.Context(), sub.Name, manifest.ModeTag, gittest.TagV100); !errors.Is(
			err, core.ErrNotFound) {
			t.Errorf("Set(%s) = %v", sub.Name, err)
		}
	}
	wantSameTree(t, "commands naming skipped entries", before, treeState(t, h.Dir))
}

// commit updates the submodule with a quoted name, which the lock file
// change must not be committed with by accident.
func (s *hostileScenario) commit(t *testing.T) {
	h, r := s.h, s.repo
	q := h.Quoted
	opts := core.UpdateOptions{Names: []string{q.Name}, Commit: true}
	before := treeState(t, h.Dir)
	res, err := r.Update(t.Context(), opts)
	wantRefusal(t, "Update(commit)", res, err,
		unrelatedRefusal+outsideChange(lock.File, "unstaged"), core.ErrUnrelatedStaged)
	wantSameTree(t, "refused commit", before, treeState(t, h.Dir))

	runGit(t, s.g, h.Dir, "commit", "--quiet", "--message=drop hostile lock entries", "--",
		lock.File)
	res = mustRepoUpdate(t, r, opts)
	v100 := q.Tags[gittest.TagV100]
	wantChanges(t, res.Changes, []changeView{fixtureView(q, core.Resolution{
		Mode: manifest.ModeTag, Ref: gittest.TagV100, Commit: v100,
	}, true)})
	want := `manifest: update "we\"ird\\name" to v1.0.0` + "\n\n" +
		"Tracking mode: tag v1.0.0\n" +
		"Old: " + v100[:12] + " (unlocked)\n" +
		"New: " + v100[:12] + " (v1.0.0)\n\n" + signOff
	if got := headMessage(t, h.Dir); res.Commit == "" || got != want {
		t.Errorf("commit %q, message:\n%s\nwant\n%s", res.Commit, got, want)
	}
	lintMessage(t, headMessage(t, h.Dir))
	if got := runGit(t, s.g, h.Dir, "diff", "--name-only", "HEAD^", "HEAD"); got != lock.File {
		t.Errorf("committed files %q", got)
	}
	vr, err := r.Verify(t.Context(), []string{q.Name}, core.VerifyOptions{})
	if err != nil || len(vr) != 1 {
		t.Fatalf("Verify = %+v, %v", vr, err)
	}
	wantChecks(t, vr[0], tagChecks(), nil, "")
}

// symlinks replaces .gitmodules and the lock file with symbolic links to
// files outside the superproject; no command follows them.
func (s *hostileScenario) symlinks(t *testing.T) {
	h, r := s.h, s.repo
	wantLink := func(what string, err error) {
		t.Helper()
		wantErr(t, what, err, git.ErrNotRegularFile)
	}
	commands := func(fetch bool) {
		t.Helper()
		_, err := r.Status(t.Context(), nil)
		wantLink("Status", err)
		for _, opts := range []core.UpdateOptions{{}, {Commit: true}, {DryRun: true}} {
			_, err := r.Update(t.Context(), opts)
			wantLink(fmt.Sprintf("Update(%+v)", opts), err)
		}
		_, err = r.Verify(t.Context(), nil, core.VerifyOptions{})
		wantLink("Verify", err)
		// Add refuses before it clones anything.
		_, err = r.Add(t.Context(), core.AddOptions{
			URL: h.Good.Upstream.Bare, Path: "added", Mode: manifest.ModeTag,
			Ref: gittest.TagV100,
		})
		wantLink("Add", err)
		if fetch {
			_, err = r.Fetch(t.Context(), nil, nil)
			wantLink("Fetch", err)
			_, _, err = s.runForeach(t)
			wantLink("Foreach", err)
			_, err = r.Set(t.Context(), "good", manifest.ModeTag, gittest.TagV101)
			wantLink("Set", err)
		}
	}

	h.LinkGitmodules(t)
	before := treeState(t, h.Dir)
	_, err := r.Submodules(t.Context())
	wantLink("Submodules", err)
	commands(true)
	wantSameTree(t, "commands with a linked .gitmodules", before, treeState(t, h.Dir))
	runGit(t, s.g, h.Dir, "checkout", "--", manifest.File)

	h.LinkLock(t)
	before = treeState(t, h.Dir)
	commands(false)
	// Fetch and Foreach do not read the lock file.
	_, err = r.Fetch(t.Context(), nil, nil)
	wantErr(t, "Fetch", err, core.ErrSymlinkPath)
	s.wantForeach(t)
	wantSameTree(t, "commands with a linked lock file", before, treeState(t, h.Dir))
}
