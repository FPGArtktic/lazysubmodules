// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package core_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/FPGArtktic/lazysubmodules/internal/core"
	"github.com/FPGArtktic/lazysubmodules/internal/git"
	"github.com/FPGArtktic/lazysubmodules/internal/git/gittest"
	"github.com/FPGArtktic/lazysubmodules/internal/lock"
	"github.com/FPGArtktic/lazysubmodules/internal/manifest"
)

// complexRefusal is the refusal of an update of every submodule of the
// complex superproject as it is built.
const complexRefusal = "fresh: refused: submodule is not initialized (use --fetch)\n" +
	"app: refused: submodule has uncommitted changes\n" +
	"broken: refused: ref not found in local refs: " +
	"tag v9.9.9 does not exist or does not point to a commit"

// complexStatusRows returns the status of the complex superproject as it
// is built.
func complexStatusRows(c *gittest.ComplexSuper) []statusRow {
	const upToDate = "up to date"
	return []statusRow{
		{"kernel", core.StateBehind,
			"update would select tag-pattern v6.6.10 instead of tag-pattern v6.6.9"},
		{"u-boot", core.StateBehind, fmt.Sprintf("branch main now resolves to %s, locked %s",
			c.UBoot.Branches["main"][:12], c.UBoot.Lock.Commit[:12])},
		{"fpga.core", core.StateOK, upToDate},
		{"crypto lib", core.StateOK, upToDate},
		{"legacy", core.StateUnmanaged, "no lsm-mode key"},
		{"tools", core.StateOK, upToDate},
		{"theme", core.StateUninitialized, "submodule is not checked out"},
		{"fresh", core.StateUninitialized, "submodule is not checked out"},
		{"app", core.StateDirty, "working tree has uncommitted changes"},
		{"sdk", core.StateOK, upToDate},
		{"mirror-lib", core.StateOK, upToDate},
		{"broken", core.StateMissingRef, "tag v9.9.9 does not exist or does not point to a commit"},
		{"signed", core.StateOK, upToDate},
		{"quirky", core.StateOK, upToDate},
	}
}

// complexTargets returns the targets that Status reports for the complex
// superproject as it is built: the locked commit, except for the submodules
// that are behind and those whose target cannot be resolved.
func complexTargets(c *gittest.ComplexSuper) map[string]*core.Resolution {
	targets := make(map[string]*core.Resolution)
	for _, s := range c.Submodules() {
		if s.Lock != nil {
			targets[s.Name] = lockedTarget(s)
		}
	}
	targets[c.Kernel.Name] = kernelTarget(c, "v6.6.10")
	targets[c.UBoot.Name] = uBootTarget(c)
	for _, s := range []*gittest.Submodule{c.Fresh, c.Broken} {
		delete(targets, s.Name)
	}
	return targets
}

// kernelTarget returns the resolution of a kernel tag.
func kernelTarget(c *gittest.ComplexSuper, tag string) *core.Resolution {
	return &core.Resolution{Mode: manifest.ModeTagPattern, Ref: tag, Commit: c.Kernel.Tags[tag]}
}

// uBootTarget returns the resolution of the advanced u-boot branch.
func uBootTarget(c *gittest.ComplexSuper) *core.Resolution {
	return &core.Resolution{Mode: manifest.ModeBranch, Ref: "main",
		Commit: c.UBoot.Branches["main"]}
}

// wantComplexStatus checks the status of the complex superproject as it is
// built.
func wantComplexStatus(t *testing.T, r *core.Repo, c *gittest.ComplexSuper) {
	t.Helper()
	st, err := r.Status(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	wantStatusRows(t, st, complexStatusRows(c))
	wantFixtureStatus(t, st, c.Submodules(), complexTargets(c))
}

// complexScenario walks through the life of the complex superproject with
// one Repo. Its steps run in order and build on each other.
type complexScenario struct {
	c    *gittest.ComplexSuper
	g    *git.Runner
	repo *core.Repo
	// trace receives the trace of every git command that g runs.
	trace string
	// selected are the submodules updated by name.
	selected []string
	// untouched records the working trees and repositories of the
	// unmanaged submodule and of the nested submodule of tools.
	untouched map[string]map[string]string
}

func TestScenarioComplex(t *testing.T) {
	t.Parallel()
	for _, format := range formats() {
		t.Run(format, func(t *testing.T) {
			t.Parallel()
			c := gittest.NewComplexSuper(t, format)
			s := &complexScenario{c: c,
				selected: []string{"theme", "sdk", "kernel", "u-boot", "kernel"}}
			s.g, s.trace = tracedRunner(t, c.Config...)
			s.repo = openRepo(t, s.g, c.Dir)
			s.untouched = s.snapshotUntouched(t)
			for _, step := range []struct {
				name string
				run  func(t *testing.T)
			}{
				{"status", func(t *testing.T) { wantComplexStatus(t, s.repo, c) }},
				{"dry run", s.dryRun},
				{"refusal", s.refusal},
				{"unmanaged", s.unmanaged},
				{"update", s.update},
				{"commit", s.commit},
				{"verify", s.verify},
				{"moved tag", s.movedTag},
				{"new release", s.newRelease},
				{"clone", s.clone},
				{"mirror", s.mirror},
				{"untouched", s.checkUntouched},
				{"transports", s.checkTransports},
			} {
				if !t.Run(step.name, step.run) {
					t.FailNow()
				}
			}
		})
	}
}

// snapshotUntouched records what no step may modify: the unmanaged
// submodule and the nested submodule of tools, working trees and
// repositories.
func (s *complexScenario) snapshotUntouched(t *testing.T) map[string]map[string]string {
	t.Helper()
	c := s.c
	inner := c.ToolsInner.In(c.Tools.Dir())
	return map[string]map[string]string{
		c.Legacy.Dir():      treeState(t, c.Legacy.Dir()),
		c.Legacy.GitDir:     treeState(t, c.Legacy.GitDir),
		inner:               treeState(t, inner),
		c.ToolsInner.GitDir: treeState(t, c.ToolsInner.GitDir),
	}
}

// checkUntouched compares the snapshot of snapshotUntouched with the
// current state.
func (s *complexScenario) checkUntouched(t *testing.T) {
	c := s.c
	for dir, before := range s.untouched {
		wantSameTree(t, "the scenario ("+dir+")", before, treeState(t, dir))
	}
	st, err := s.repo.Status(t.Context(), []string{c.Legacy.Name, c.Tools.Name})
	if err != nil {
		t.Fatal(err)
	}
	wantStatusRows(t, st, []statusRow{
		{"legacy", core.StateUnmanaged, "no lsm-mode key"},
		{"tools", core.StateOK, "up to date"},
	})
	if st[0].Head != c.Legacy.Gitlink || gitlinkAt(t, c.Dir, "HEAD", c.Legacy.Path) !=
		c.Legacy.Gitlink {
		t.Errorf("legacy moved: head %s", st[0].Head)
	}
	got := runGit(t, s.g, c.Dir, "config", "-f", manifest.File, "--get-regexp",
		`^submodule\.legacy\.`)
	want := "submodule.legacy.path vendor/legacy\nsubmodule.legacy.url " + c.Legacy.URL
	if got != want {
		t.Errorf("legacy configuration %q, want %q", got, want)
	}
	lk, err := lock.Load(t.Context(), s.g, c.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := lk.Get(c.Legacy.Name); ok {
		t.Error("the lock file has an entry for legacy")
	}
}

// checkTransports checks that every transfer of the scenario was served
// from a local upstream repository.
func (s *complexScenario) checkTransports(t *testing.T) {
	var bares []string
	for _, sub := range s.c.Submodules() {
		bares = append(bares, sub.Upstream.Bare)
	}
	data, _ := readTrace(t, s.trace, 0)
	served := transports(t, data, bares...)
	for _, sub := range []*gittest.Submodule{s.c.FPGACore, s.c.SDK, s.c.Fresh} {
		if !served[sub.Upstream.Bare] {
			t.Errorf("%s was not fetched from its upstream", sub.Name)
		}
	}
}

// dryRun checks that no dry run modifies anything and that each plans what
// the update would do.
func (s *complexScenario) dryRun(t *testing.T) {
	c, r := s.c, s.repo
	before := treeState(t, c.Dir)
	for _, opts := range []core.UpdateOptions{{DryRun: true}, {DryRun: true, Commit: true}} {
		res, err := r.Update(t.Context(), opts)
		wantRefusal(t, fmt.Sprintf("Update(%+v)", opts), res, err, complexRefusal,
			core.ErrUninitialized, core.ErrDirty, core.ErrMissingRef)
	}
	// With a fetch, a clone is planned and a missing ref might be fetched.
	res, err := r.Update(t.Context(), core.UpdateOptions{DryRun: true, Fetch: true})
	wantRefusal(t, "Update(dry run, fetch)", res, err,
		"app: refused: submodule has uncommitted changes", core.ErrDirty)

	eligible := []string{"kernel", "u-boot", "fpga.core", "crypto lib", "tools", "theme", "sdk",
		"mirror-lib", "signed", "quirky"}
	res = mustRepoUpdate(t, r, core.UpdateOptions{DryRun: true, Names: eligible})
	theme := unchangedView(c.Theme)
	theme.Init, theme.Changed = true, true
	wantChanges(t, res.Changes, []changeView{
		fixtureView(c.Kernel, *kernelTarget(c, "v6.6.10"), true),
		fixtureView(c.UBoot, *uBootTarget(c), true),
		unchangedView(c.FPGACore), unchangedView(c.CryptoLib), unchangedView(c.Tools), theme,
		unchangedView(c.SDK), unchangedView(c.MirrorLib), unchangedView(c.Signed),
		unchangedView(c.Quirky),
	})

	res = mustRepoUpdate(t, r, core.UpdateOptions{DryRun: true, Fetch: true, Commit: true,
		Names: []string{"broken", "fresh"}})
	fresh := fixtureView(c.Fresh, core.Resolution{}, true)
	fresh.Init, fresh.Clone = true, true
	wantChanges(t, res.Changes, []changeView{fresh, fixtureView(c.Broken, core.Resolution{}, true)})

	// Only the option selects the pre-release; the tag pattern of sdk has no
	// higher tag than its locked pre-release.
	res = mustRepoUpdate(t, r, core.UpdateOptions{DryRun: true, IncludePrerelease: true,
		Names: []string{"sdk", "kernel"}})
	wantChanges(t, res.Changes, []changeView{
		fixtureView(c.Kernel, *kernelTarget(c, "v6.6.11-rc1"), true), unchangedView(c.SDK),
	})
	wantSameTree(t, "dry runs", before, treeState(t, c.Dir))
}

// refusal checks that an update is refused as a whole, before anything is
// modified.
func (s *complexScenario) refusal(t *testing.T) {
	c, r := s.c, s.repo
	before := treeState(t, c.Dir)
	for _, opts := range []core.UpdateOptions{{}, {Commit: true}, {IncludePrerelease: true}} {
		res, err := r.Update(t.Context(), opts)
		wantRefusal(t, fmt.Sprintf("Update(%+v)", opts), res, err, complexRefusal,
			core.ErrUninitialized, core.ErrDirty, core.ErrMissingRef)
		if errors.Is(err, core.ErrUnrelatedStaged) {
			t.Errorf("Update(%+v): %v", opts, err)
		}
	}
	// Nothing is fetched or cloned for a refused update either.
	res, err := r.Update(t.Context(), core.UpdateOptions{Fetch: true})
	wantRefusal(t, "Update(fetch)", res, err, "app: refused: submodule has uncommitted changes",
		core.ErrDirty)
	res, err = r.Update(t.Context(), core.UpdateOptions{Names: []string{"broken", "kernel", "app"}})
	wantRefusal(t, "Update(broken, kernel, app)", res, err,
		"app: refused: submodule has uncommitted changes\n"+
			"broken: refused: ref not found in local refs: "+
			"tag v9.9.9 does not exist or does not point to a commit",
		core.ErrDirty, core.ErrMissingRef)
	wantSameTree(t, "refused updates", before, treeState(t, c.Dir))
}

// unmanaged checks that no command accepts the unmanaged submodule by
// name.
func (s *complexScenario) unmanaged(t *testing.T) {
	c, r := s.c, s.repo
	before := treeState(t, c.Dir)
	const refused = "legacy: refused: submodule is not managed by lazysubmodules"
	for _, names := range [][]string{{"legacy"}, {"kernel", "legacy"}} {
		res, err := r.Update(t.Context(), core.UpdateOptions{Names: names, Fetch: true})
		wantRefusal(t, fmt.Sprintf("Update(%q)", names), res, err, refused, core.ErrUnmanaged)
		vr, err := r.Verify(t.Context(), names, core.VerifyOptions{})
		wantErr(t, fmt.Sprintf("Verify(%q)", names), err, core.ErrUnmanaged)
		fr, ferr := r.Fetch(t.Context(), names, nil)
		wantErr(t, fmt.Sprintf("Fetch(%q)", names), ferr, core.ErrUnmanaged)
		if vr != nil || fr != nil {
			t.Errorf("results for %q: %+v, %+v", names, vr, fr)
		}
	}
	// Unknown names are reported before unmanaged ones.
	_, err := r.Update(t.Context(), core.UpdateOptions{Names: []string{"legacy", "inner"}})
	wantErr(t, "Update(legacy, inner)", err, core.ErrNotFound)
	if err == nil || errors.Is(err, core.ErrUnmanaged) || err.Error() != "inner: no such submodule" {
		t.Errorf("Update(legacy, inner) = %v", err)
	}
	if _, err := r.Resolve(t.Context(), configOf(c.Legacy), nil, core.ResolveOptions{}); !errors.Is(
		err, core.ErrUnmanaged) {
		t.Errorf("Resolve(legacy) = %v", err)
	}
	st, err := r.Status(t.Context(), []string{"legacy"})
	if err != nil {
		t.Fatal(err)
	}
	wantStatusRows(t, st, []statusRow{{"legacy", core.StateUnmanaged, "no lsm-mode key"}})
	wantSameTree(t, "commands naming legacy", before, treeState(t, c.Dir))
}

// updatedLock returns the lock entries after the update of kernel and
// u-boot.
func (s *complexScenario) updatedLock() []lock.Entry {
	c := s.c
	k, u := kernelTarget(c, "v6.6.10"), uBootTarget(c)
	return fixtureLock(c.Submodules(),
		lock.Entry{Name: "kernel", Mode: k.Mode, Ref: k.Ref, Commit: k.Commit},
		lock.Entry{Name: "u-boot", Mode: u.Mode, Ref: u.Ref, Commit: u.Commit})
}

// update updates submodules selected by name, after the dirty one was
// cleaned.
func (s *complexScenario) update(t *testing.T) {
	c, r := s.c, s.repo
	runGit(t, s.g, c.App.Dir(), "checkout", "--", gittest.TrackedFile)
	_, mark := readTrace(t, s.trace, 0)
	res := mustRepoUpdate(t, r, core.UpdateOptions{Names: s.selected})
	// Theme was initialized offline.
	wantNoTransport(t, s.trace, mark)
	theme := unchangedView(c.Theme)
	theme.Init, theme.Changed = true, true
	wantChanges(t, res.Changes, []changeView{
		fixtureView(c.Kernel, *kernelTarget(c, "v6.6.10"), true),
		fixtureView(c.UBoot, *uBootTarget(c), true), theme, unchangedView(c.SDK),
	})
	if res.Commit != "" {
		t.Errorf("committed %s", res.Commit)
	}
	// The gitlink of theme is unchanged; .gitmodules needs no change.
	wantStaged(t, c.Dir, lock.File, c.UBoot.Path, c.Kernel.Path)
	indexMatchesWorktree(t, c.Dir, lock.File)
	wantLockFile(t, s.g, c.Dir, s.updatedLock())
	for dir, want := range map[string]string{
		c.Kernel.Dir(): c.Kernel.Tags["v6.6.10"],
		c.UBoot.Dir():  c.UBoot.Branches["main"],
		c.Theme.Dir():  c.Theme.Gitlink,
		c.SDK.Dir():    c.SDK.Head,
	} {
		if got := headOf(t, dir); got != want {
			t.Errorf("HEAD of %s is %s, want %s", dir, got, want)
		}
	}
	rows := complexStatusRows(c)
	for i := range rows {
		switch rows[i].name {
		case "kernel", "u-boot", "theme", "app":
			rows[i].state, rows[i].reason = core.StateOK, "up to date"
		}
	}
	st, err := r.Status(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	wantStatusRows(t, st, rows)

	// Updating again changes nothing.
	before := treeState(t, c.Dir)
	res = mustRepoUpdate(t, r, core.UpdateOptions{Names: s.selected})
	for _, ch := range res.Changes {
		if ch.Changed() || ch.Init {
			t.Errorf("second update: %+v", viewOf(ch))
		}
	}
	wantSameTree(t, "second update", before, treeState(t, c.Dir))
}

// commit commits the staged update, which an unrelated staged file
// prevents first.
func (s *complexScenario) commit(t *testing.T) {
	c, r := s.c, s.repo
	readme := gittest.SubdirPath + "/README"
	gittest.WriteFile(t, filepath.Join(c.Subdir, "README"), "unrelated change\n")
	runGit(t, s.g, c.Dir, "add", "--", readme)
	before := treeState(t, c.Dir)
	opts := core.UpdateOptions{Names: s.selected, Commit: true}
	res, err := r.Update(t.Context(), opts)
	wantRefusal(t, "Update(commit)", res, err,
		unrelatedRefusal+readme, core.ErrUnrelatedStaged)
	wantSameTree(t, "refused commit", before, treeState(t, c.Dir))

	// The change stays in the working tree, but not in the commit.
	runGit(t, s.g, c.Dir, "reset", "--quiet", "--", readme)
	res = mustRepoUpdate(t, r, opts)
	head := headOf(t, c.Dir)
	if res.Commit != head || runGit(t, s.g, c.Dir, "rev-parse", "HEAD^") != c.History[4] {
		t.Fatalf("commit %s, HEAD %s: not one commit on top of the fixture", res.Commit, head)
	}
	k, u := c.Kernel, c.UBoot
	kernel := fixtureView(k, *kernelTarget(c, "v6.6.10"), true)
	kernel.OldHead = k.Tags["v6.6.10"]
	uBoot := fixtureView(u, *uBootTarget(c), true)
	uBoot.OldHead = u.Branches["main"]
	theme := unchangedView(c.Theme)
	theme.OldHead = c.Theme.Gitlink
	wantChanges(t, res.Changes, []changeView{kernel, uBoot, theme, unchangedView(c.SDK)})
	want := "manifest: update 2 submodules\n\n" +
		"Submodule \"kernel\":\n" +
		"  Tracking mode: tag-pattern v6.6.*\n" +
		"  Old: " + k.Lock.Commit[:12] + " (v6.6.9)\n" +
		"  New: " + k.Tags["v6.6.10"][:12] + " (v6.6.10)\n\n" +
		"Submodule \"u-boot\":\n" +
		"  Tracking mode: branch main\n" +
		"  Old: " + u.Lock.Commit[:12] + " (main)\n" +
		"  New: " + u.Branches["main"][:12] + " (main)\n\n" + signOff
	if got := headMessage(t, c.Dir); got != want ||
		got != core.CommitMessage(res.Changes)+"\n"+signOff {
		t.Errorf("commit message:\n%s\nwant\n%s", got, want)
	}
	lintMessage(t, headMessage(t, c.Dir))
	if got := runGit(t, s.g, c.Dir, "diff", "--name-only", "HEAD^", "HEAD"); got !=
		".lsm.lock\nbootloader/u-boot\nkernel" {
		t.Errorf("committed files %q", got)
	}
	wantStaged(t, c.Dir)
	if got := runGit(t, s.g, c.Dir, "diff", "--name-only", "--", readme); got != readme {
		t.Errorf("unrelated change %q is gone", got)
	}
	if gitlinkAt(t, c.Dir, "HEAD", k.Path) != k.Tags["v6.6.10"] ||
		gitlinkAt(t, c.Dir, "HEAD", u.Path) != u.Branches["main"] {
		t.Error("the commit does not record the new gitlinks")
	}

	// Nothing is left to commit.
	before = treeState(t, c.Dir)
	res = mustRepoUpdate(t, r, opts)
	if res.Commit != "" || slices.ContainsFunc(res.Changes, core.Change.Changed) {
		t.Errorf("second commit: %+v", res)
	}
	wantSameTree(t, "second commit", before, treeState(t, c.Dir))
}

// verify checks the committed tree.
func (s *complexScenario) verify(t *testing.T) {
	c, r := s.c, s.repo
	res, err := r.Verify(t.Context(), nil, core.VerifyOptions{})
	wantErr(t, "Verify", err, core.ErrVerify)
	if err == nil || err.Error() != "verification failed: fresh, broken" {
		t.Errorf("Verify = %v", err)
	}
	var managed []string
	for _, sub := range c.Submodules() {
		if sub.Mode != "" {
			managed = append(managed, sub.Name)
		}
	}
	got := make([]string, 0, len(res))
	for _, v := range res {
		got = append(got, v.Submodule.Name)
		switch v.Submodule.Name {
		case "fresh":
			wantChecks(t, v, []string{core.CheckLockEntry, core.CheckLockConfig,
				core.CheckLockCommit, core.CheckGitlink, core.CheckInitialized},
				[]string{core.CheckLockConfig, core.CheckInitialized}, "")
		case "broken":
			wantChecks(t, v, tagChecks(), []string{core.CheckLockConfig},
				"lock records tag v1.0.0, configuration has v9.9.9")
		case "u-boot", "crypto lib", "tools", "mirror-lib":
			wantChecks(t, v, branchChecks(), nil, "")
		default:
			wantChecks(t, v, tagChecks(), nil, "")
		}
	}
	if !slices.Equal(got, managed) {
		t.Errorf("verified %q, want %q", got, managed)
	}
	good := slices.DeleteFunc(managed, func(name string) bool {
		return name == "fresh" || name == "broken"
	})
	if _, err := r.Verify(t.Context(), good, core.VerifyOptions{}); err != nil {
		t.Errorf("Verify(%q) = %v", good, err)
	}
}

// movedTag fetches the tag that was moved on the remote.
func (s *complexScenario) movedTag(t *testing.T) {
	c, r := s.c, s.repo
	f := c.FPGACore
	moved, locked := f.Tags[f.Ref], f.Lock.Commit
	results, err := r.Fetch(t.Context(), []string{f.Name}, nil)
	if err != nil || len(results) != 1 || results[0].Init || results[0].Cloned ||
		results[0].Submodule != configOf(f) {
		t.Fatalf("Fetch = %+v, %v", results, err)
	}
	st, err := r.Status(t.Context(), []string{f.Name})
	if err != nil {
		t.Fatal(err)
	}
	wantStatusRows(t, st, []statusRow{{f.Name, core.StateDrift, fmt.Sprintf(
		"locked tag v2.3.1 now points to %s, not %s", moved[:12], locked[:12])}})
	vr, err := r.Verify(t.Context(), []string{f.Name}, core.VerifyOptions{})
	wantErr(t, "Verify", err, core.ErrVerify)
	if err == nil || err.Error() != "verification failed: fpga.core" || len(vr) != 1 {
		t.Fatalf("Verify = %+v, %v", vr, err)
	}
	wantChecks(t, vr[0], tagChecks(), []string{core.CheckTag}, fmt.Sprintf(
		"tag v2.3.1 points to %s, locked %s (moved tag)", moved[:12], locked[:12]))

	res := mustRepoUpdate(t, r, core.UpdateOptions{DryRun: true, Names: []string{f.Name}})
	target := core.Resolution{Mode: manifest.ModeTag, Ref: f.Ref, Commit: moved}
	wantChanges(t, res.Changes, []changeView{fixtureView(f, target, true)})
}

// newRelease publishes a release that ends the pre-release of sdk.
func (s *complexScenario) newRelease(t *testing.T) {
	c, r := s.c, s.repo
	sdk := c.SDK
	release := sdk.Upstream.Commit(t, "sdk v3.0.0")
	sdk.Upstream.AnnotatedTag(t, "v3.0.0", release, "release v3.0.0")
	st, err := r.Status(t.Context(), []string{sdk.Name})
	if err != nil {
		t.Fatal(err)
	}
	wantStatusRows(t, st, []statusRow{{sdk.Name, core.StateOK, "up to date"}})
	if _, err := r.Fetch(t.Context(), []string{sdk.Name}, nil); err != nil {
		t.Fatal(err)
	}
	st, err = r.Status(t.Context(), []string{sdk.Name})
	if err != nil {
		t.Fatal(err)
	}
	wantStatusRows(t, st, []statusRow{{sdk.Name, core.StateBehind,
		"update would select tag-pattern v3.0.0 instead of tag-pattern v3.0.0-rc.2"}})
	res := mustRepoUpdate(t, r, core.UpdateOptions{Names: []string{sdk.Name}})
	target := core.Resolution{Mode: manifest.ModeTagPattern, Ref: "v3.0.0", Commit: release}
	wantChanges(t, res.Changes, []changeView{fixtureView(sdk, target, true)})
	if got := headOf(t, sdk.Dir()); got != release {
		t.Errorf("sdk at %s, want %s", got, release)
	}
	wantStaged(t, c.Dir, lock.File, sdk.Path)
}

// clone clones the submodule that was never cloned.
func (s *complexScenario) clone(t *testing.T) {
	c, r := s.c, s.repo
	fresh := c.Fresh
	res, err := r.Update(t.Context(), core.UpdateOptions{Names: []string{fresh.Name}})
	wantRefusal(t, "Update(fresh)", res, err,
		"fresh: refused: submodule is not initialized (use --fetch)", core.ErrUninitialized)
	res = mustRepoUpdate(t, r, core.UpdateOptions{Names: []string{fresh.Name}, Fetch: true})
	target := core.Resolution{Mode: manifest.ModeTagPattern, Ref: "v1.1.0",
		Commit: fresh.Tags["v1.1.0"]}
	want := fixtureView(fresh, target, true)
	want.Init, want.Clone = true, true
	wantChanges(t, res.Changes, []changeView{want})
	if got := headOf(t, fresh.Dir()); got != target.Commit {
		t.Errorf("fresh at %s, want %s", got, target.Commit)
	}
	if ok, err := s.g.IsGitDir(t.Context(), filepath.Join(c.GitDir, "modules", fresh.Name)); !ok ||
		err != nil {
		t.Errorf("no repository of fresh in the git directory: %v", err)
	}
	st, err := r.Status(t.Context(), []string{fresh.Name})
	if err != nil {
		t.Fatal(err)
	}
	wantStatusRows(t, st, []statusRow{{fresh.Name, core.StateOK, "up to date"}})
	// The gitlink and the lock entry were right already.
	wantStaged(t, c.Dir, lock.File, c.SDK.Path)
}

// mirror clones and fetches the submodule whose URL only the mirror
// configuration makes usable, and proves that no other transport is used.
func (s *complexScenario) mirror(t *testing.T) {
	c, r, g := s.c, s.repo, s.g
	m := c.MirrorLib
	runGit(t, g, c.Dir, "submodule", "deinit", "--force", "--quiet", "--", m.Path)
	if err := os.RemoveAll(m.GitDir); err != nil {
		t.Fatal(err)
	}
	st, err := r.Status(t.Context(), []string{m.Name})
	if err != nil {
		t.Fatal(err)
	}
	wantStatusRows(t, st, []statusRow{{m.Name, core.StateUninitialized,
		"submodule is not checked out"}})

	// Without the mirror configuration, the test environment refuses the
	// https transport before connecting.
	plain := openRepo(t, gittest.Runner(t), c.Dir)
	_, err = plain.Update(t.Context(), core.UpdateOptions{Names: []string{m.Name}, Fetch: true})
	wantTransportRefused(t, "Update without the mirror", err)
	_, err = plain.Fetch(t.Context(), []string{m.Name}, nil)
	wantTransportRefused(t, "Fetch without the mirror", err)

	_, mark := readTrace(t, s.trace, 0)
	res := mustRepoUpdate(t, r, core.UpdateOptions{Names: []string{m.Name}, Fetch: true})
	want := unchangedView(m)
	want.OldHead, want.Init, want.Clone, want.Changed = "", true, true, true
	wantChanges(t, res.Changes, []changeView{want})
	mark = wantLocalTransport(t, s.trace, mark, m.Upstream.Bare)
	if got := runGit(t, g, m.Dir(), "config", "--get", "remote.origin.url"); got !=
		gittest.MirrorURL {
		t.Errorf("origin of the clone is %s", got)
	}

	results, err := r.Fetch(t.Context(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var fetched []string
	for _, res := range results {
		fetched = append(fetched, res.Submodule.Name)
		if res.Init || res.Cloned {
			t.Errorf("Fetch initialized %s", res.Submodule.Name)
		}
	}
	var bares []string
	for _, sub := range c.Submodules() {
		if sub != c.Legacy {
			bares = append(bares, sub.Upstream.Bare)
		}
	}
	if want := slices.DeleteFunc(fixtureNames(c.Submodules()), func(name string) bool {
		return name == c.Legacy.Name
	}); !slices.Equal(fetched, want) {
		t.Errorf("fetched %q, want %q", fetched, want)
	}
	wantLocalTransport(t, s.trace, mark, bares...)
	st, err = r.Status(t.Context(), []string{m.Name})
	if err != nil {
		t.Fatal(err)
	}
	wantStatusRows(t, st, []statusRow{{m.Name, core.StateOK, "up to date"}})
}
