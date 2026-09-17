// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package core_test

import (
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/FPGArtktic/lazysubmodules/internal/core"
	"github.com/FPGArtktic/lazysubmodules/internal/git/gittest"
	"github.com/FPGArtktic/lazysubmodules/internal/lock"
	"github.com/FPGArtktic/lazysubmodules/internal/manifest"
)

// maxGitProcesses is the number of submodules that core inspects at the
// same time, each with one git process at a time.
const maxGitProcesses = 8

func TestScenarioManySuper(t *testing.T) {
	t.Parallel()
	const n = 100
	m := gittest.NewManySuper(t, n)
	g := gittest.Runner(t)
	subs := m.Submodules()
	// Every fifth submodule, starting with the third, is behind; every
	// fifth, starting with the fifth, is unmanaged.
	var (
		rows    []statusRow
		plan    []changeView
		updated []statusRow
		moved   []string
	)
	targets := make(map[string]*core.Resolution)
	for i, s := range subs {
		row := statusRow{s.Name, core.StateOK, "up to date"}
		if s.Mode != "" {
			targets[s.Name] = lockedTarget(s)
		}
		switch i % 5 {
		case 2:
			target := core.Resolution{Mode: manifest.ModeTagPattern, Ref: gittest.TagV101,
				Commit: s.Tags[gittest.TagV101]}
			targets[s.Name] = &target
			plan = append(plan, fixtureView(s, target, true))
			moved = append(moved, s.Path)
			updated = append(updated, row)
			row.state, row.reason = core.StateBehind,
				"update would select tag-pattern v1.0.1 instead of tag-pattern v1.0.0"
		case 4:
			row.state, row.reason = core.StateUnmanaged, "no lsm-mode key"
			updated = append(updated, row)
		default:
			plan = append(plan, unchangedView(s))
			updated = append(updated, row)
		}
		rows = append(rows, row)
	}
	if names := fixtureNames(subs); slices.IsSorted(names) {
		t.Fatalf("the fixture lists the submodules in sorted order: %q", names[:3])
	}

	r := openRepo(t, g, m.Dir)
	sampler := startSampler(t, m.Dir)
	timed := func(what string, fn func()) {
		t.Helper()
		start := time.Now()
		fn()
		t.Logf("%s of %d submodules: %v", what, n, time.Since(start))
	}
	for i := range 3 {
		timed("Status "+strconv.Itoa(i+1), func() {
			st, err := r.Status(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			wantStatusRows(t, st, rows)
			wantFixtureStatus(t, st, subs, targets)
		})
	}
	timed("Verify", func() {
		res, err := r.Verify(t.Context(), nil, core.VerifyOptions{})
		if err != nil || len(res) != len(plan) {
			t.Errorf("Verify = %d results, %v", len(res), err)
		}
	})
	timed("Update dry run", func() {
		res := mustRepoUpdate(t, r, core.UpdateOptions{DryRun: true})
		wantChanges(t, res.Changes, plan)
	})
	timed("Update", func() {
		res := mustRepoUpdate(t, r, core.UpdateOptions{})
		if got := len(slices.DeleteFunc(res.Changes, func(c core.Change) bool {
			return !c.Changed()
		})); got != len(moved) {
			t.Errorf("%d changed submodules, want %d", got, len(moved))
		}
	})
	most, samples := sampler.finish()
	t.Logf("at most %d git processes at the same time in %d samples", most, samples)
	if most > maxGitProcesses {
		t.Errorf("%d git processes ran at the same time, want at most %d", most,
			maxGitProcesses)
	}

	slices.Sort(moved)
	wantStaged(t, m.Dir, append([]string{lock.File}, moved...)...)
	st, err := r.Status(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	wantStatusRows(t, st, updated)
	res := mustRepoUpdate(t, r, core.UpdateOptions{Commit: true})
	msg := headMessage(t, m.Dir)
	if res.Commit == "" || !strings.HasPrefix(msg, "manifest: update 20 submodules\n\n") {
		t.Errorf("commit %q:\n%s", res.Commit, msg)
	}
	lintMessage(t, msg)
	if _, err := r.Verify(t.Context(), nil, core.VerifyOptions{}); err != nil {
		t.Errorf("Verify after the commit = %v", err)
	}
}
