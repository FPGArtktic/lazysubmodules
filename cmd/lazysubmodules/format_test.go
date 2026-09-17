// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/FPGArtktic/lazysubmodules/internal/core"
	"github.com/FPGArtktic/lazysubmodules/internal/lock"
	"github.com/FPGArtktic/lazysubmodules/internal/manifest"
)

// Commits of the formatting tests.
const (
	commitA = "a1b2c3d4e5f6a7b8c9d0a1b2c3d4e5f6a7b8c9d0"
	commitB = "e4f5a6b7c8d9e0f1a2b3c4d5e6f7a8b9c0d1e2f3"
	commitC = "cccccccccccccccccccccccccccccccccccccccc"
)

// tagSub returns a submodule tracking a tag or tag pattern.
func tagSub(name string, mode manifest.Mode, ref string) manifest.Submodule {
	return manifest.Submodule{Name: name, Path: name, Mode: mode, Ref: ref}
}

// entry returns a lock entry.
func entry(name string, mode manifest.Mode, ref, commit string) *lock.Entry {
	return &lock.Entry{Name: name, Mode: mode, Ref: ref, Commit: commit}
}

func TestChangeLine(t *testing.T) {
	t.Parallel()
	tp, tag, commitMode := manifest.ModeTagPattern, manifest.ModeTag, manifest.ModeCommit
	kernel := tagSub("kernel", tp, "v6.6.*")
	theme := tagSub("theme", tag, "v1.0.0")
	branch := manifest.Submodule{Name: "lib", Path: "lib", Mode: manifest.ModeBranch,
		Ref: "main", Branch: "main"}
	tests := []struct {
		name      string
		change    core.Change
		done, dry string
	}{
		{
			"up to date",
			core.Change{Submodule: theme, Old: entry("theme", tag, "v1.0.0", commitA),
				OldHead: commitA, OldGitlink: commitA,
				New: core.Resolution{Mode: tag, Ref: "v1.0.0", Commit: commitA}},
			"theme: up to date", "theme: up to date",
		},
		{
			"new tag",
			core.Change{Submodule: kernel, Old: entry("kernel", tp, "v6.6.8", commitA),
				OldHead: commitA, OldGitlink: commitA,
				New: core.Resolution{Mode: tp, Ref: "v6.6.9", Commit: commitB}},
			"kernel: v6.6.8 (a1b2c3d) -> v6.6.9 (e4f5a6b)",
			"would update kernel: v6.6.8 (a1b2c3d) -> v6.6.9 (e4f5a6b)",
		},
		{
			"offline initialization",
			core.Change{Submodule: theme, Old: entry("theme", tag, "v1.0.0", commitA),
				OldGitlink: commitA, Init: true,
				New: core.Resolution{Mode: tag, Ref: "v1.0.0", Commit: commitA}},
			"theme: v1.0.0 (a1b2c3d), initialized",
			"would update theme: v1.0.0 (a1b2c3d), initialize",
		},
		{
			"clone with unknown target",
			core.Change{Submodule: kernel, Old: entry("kernel", tp, "v6.6.8", commitA),
				OldGitlink: commitA, Init: true, Clone: true},
			"kernel: v6.6.8 (a1b2c3d) -> unknown until fetched, cloned",
			"would update kernel: v6.6.8 (a1b2c3d) -> unknown until fetched, clone",
		},
		{
			"clone to a newer tag",
			core.Change{Submodule: kernel, Old: entry("kernel", tp, "v6.6.8", commitA),
				OldGitlink: commitA, Init: true, Clone: true,
				New: core.Resolution{Mode: tp, Ref: "v6.6.9", Commit: commitB}},
			"kernel: v6.6.8 (a1b2c3d) -> v6.6.9 (e4f5a6b), cloned",
			"would update kernel: v6.6.8 (a1b2c3d) -> v6.6.9 (e4f5a6b), clone",
		},
		{
			"moved HEAD",
			core.Change{Submodule: theme, Old: entry("theme", tag, "v1.0.0", commitA),
				OldHead: commitC, OldGitlink: commitA,
				New: core.Resolution{Mode: tag, Ref: "v1.0.0", Commit: commitA}},
			"theme: v1.0.0 (a1b2c3d), HEAD was ccccccc",
			"would update theme: v1.0.0 (a1b2c3d), HEAD is ccccccc",
		},
		{
			"moved HEAD and new target",
			core.Change{Submodule: theme, Old: entry("theme", tag, "v1.0.0", commitA),
				OldHead: commitC, OldGitlink: commitA,
				New: core.Resolution{Mode: tag, Ref: "v1.0.0", Commit: commitB}},
			"theme: v1.0.0 (a1b2c3d) -> v1.0.0 (e4f5a6b), HEAD was ccccccc",
			"would update theme: v1.0.0 (a1b2c3d) -> v1.0.0 (e4f5a6b), HEAD is ccccccc",
		},
		{
			"no lock entry",
			core.Change{Submodule: branch, OldHead: commitA, OldGitlink: commitA,
				New: core.Resolution{Mode: manifest.ModeBranch, Ref: "main", Commit: commitB}},
			"lib: a1b2c3d (unlocked) -> main (e4f5a6b)",
			"would update lib: a1b2c3d (unlocked) -> main (e4f5a6b)",
		},
		{
			"no gitlink",
			core.Change{Submodule: branch, Init: true,
				New: core.Resolution{Mode: manifest.ModeBranch, Ref: "main", Commit: commitB}},
			"lib: none -> main (e4f5a6b), initialized",
			"would update lib: none -> main (e4f5a6b), initialize",
		},
		{
			"lock entry of another commit",
			core.Change{Submodule: theme, Old: entry("theme", tag, "v1.0.0", commitA),
				OldHead: commitC, OldGitlink: commitC,
				New: core.Resolution{Mode: tag, Ref: "v1.0.0", Commit: commitA}},
			"theme: ccccccc -> v1.0.0 (a1b2c3d)",
			"would update theme: ccccccc -> v1.0.0 (a1b2c3d)",
		},
		{
			"commit mode",
			core.Change{Submodule: tagSub("crypto lib", commitMode, commitB),
				Old:     entry("crypto lib", commitMode, commitA, commitA),
				OldHead: commitA, OldGitlink: commitA,
				New: core.Resolution{Mode: commitMode, Ref: commitB, Commit: commitB}},
			"crypto lib: a1b2c3d -> e4f5a6b",
			"would update crypto lib: a1b2c3d -> e4f5a6b",
		},
		{
			"new mode",
			core.Change{Submodule: theme, Old: entry("theme", tp, "v1.0.0", commitA),
				OldHead: commitA, OldGitlink: commitA,
				New: core.Resolution{Mode: tag, Ref: "v1.0.0", Commit: commitA}},
			"theme: tag-pattern v1.0.0 (a1b2c3d) -> tag v1.0.0 (a1b2c3d)",
			"would update theme: tag-pattern v1.0.0 (a1b2c3d) -> tag v1.0.0 (a1b2c3d)",
		},
		{
			"new mode and commit",
			core.Change{Submodule: theme, Old: entry("theme", commitMode, commitA, commitA),
				OldHead: commitA, OldGitlink: commitA,
				New: core.Resolution{Mode: tag, Ref: "v1.0.0", Commit: commitB}},
			"theme: commit a1b2c3d -> tag v1.0.0 (e4f5a6b)",
			"would update theme: commit a1b2c3d -> tag v1.0.0 (e4f5a6b)",
		},
		{
			"native branch key only",
			core.Change{Submodule: manifest.Submodule{Name: "lib", Path: "lib",
				Mode: manifest.ModeBranch, Ref: "main"},
				Old:     entry("lib", manifest.ModeBranch, "main", commitA),
				OldHead: commitA, OldGitlink: commitA,
				New: core.Resolution{Mode: manifest.ModeBranch, Ref: "main", Commit: commitA}},
			"lib: main (a1b2c3d), recorded again",
			"would update lib: main (a1b2c3d), record again",
		},
		{
			"hostile values",
			core.Change{Submodule: tagSub("we\"ird\x1b[2J", tag, "v1"),
				Old: entry("we\"ird\x1b[2J", tag, "v0\x07", commitA), OldHead: commitA,
				OldGitlink: commitA,
				New:        core.Resolution{Mode: tag, Ref: "v1\x1b[2J", Commit: commitB}},
			`"we\"ird\x1b[2J": v0` + "\ufffd (a1b2c3d) -> v1\ufffd[2J (e4f5a6b)",
			`would update "we\"ird\x1b[2J": v0` + "\ufffd (a1b2c3d) -> v1\ufffd[2J (e4f5a6b)",
		},
	}
	for _, tt := range tests {
		if got := changeLine(tt.change, false); got != tt.done {
			t.Errorf("%s:\n got %q\nwant %q", tt.name, got, tt.done)
		}
		if got := changeLine(tt.change, true); got != tt.dry {
			t.Errorf("%s, dry run:\n got %q\nwant %q", tt.name, got, tt.dry)
		}
	}
}

func TestWriteUpdate(t *testing.T) {
	t.Parallel()
	tp := manifest.ModeTagPattern
	upToDate := core.Change{Submodule: tagSub("sdk", tp, "v*"),
		Old: entry("sdk", tp, "v1", commitA), OldHead: commitA, OldGitlink: commitA,
		New: core.Resolution{Mode: tp, Ref: "v1", Commit: commitA}}
	changed := core.Change{Submodule: tagSub("kernel", tp, "v6.6.*"),
		Old: entry("kernel", tp, "v6.6.8", commitA), OldHead: commitA, OldGitlink: commitA,
		New: core.Resolution{Mode: tp, Ref: "v6.6.9", Commit: commitB}}
	unknown := core.Change{Submodule: tagSub("fresh", tp, "v1.*"), Init: true, Clone: true}
	v100 := core.Resolution{Mode: manifest.ModeTag, Ref: "v1.0.0", Commit: commitA}
	initOnly := core.Change{Submodule: tagSub("theme", manifest.ModeTag, "v1.0.0"),
		Old: entry("theme", manifest.ModeTag, "v1.0.0", commitA), OldGitlink: commitA,
		Init: true, New: v100}
	checkoutOnly := initOnly
	checkoutOnly.Init, checkoutOnly.OldHead = false, commitB
	failure := errors.New("refused")
	const (
		upToDateLine = "sdk: up to date\n"
		changedLine  = "kernel: v6.6.8 (a1b2c3d) -> v6.6.9 (e4f5a6b)\n"
		dryLine      = "would update kernel: v6.6.8 (a1b2c3d) -> v6.6.9 (e4f5a6b)\n"
	)
	tests := []struct {
		name string
		opts core.UpdateOptions
		res  core.UpdateResult
		err  error
		want string
	}{
		{"nothing managed", core.UpdateOptions{}, core.UpdateResult{}, nil,
			"no managed submodules\n"},
		{"refused", core.UpdateOptions{Commit: true}, core.UpdateResult{}, failure, ""},
		{"staged", core.UpdateOptions{},
			core.UpdateResult{Changes: []core.Change{upToDate, changed}}, nil,
			upToDateLine + changedLine},
		{"committed", core.UpdateOptions{Commit: true},
			core.UpdateResult{Changes: []core.Change{changed, upToDate}, Commit: commitC}, nil,
			changedLine + upToDateLine + "committed " + commitC + "\n"},
		{"commit failed", core.UpdateOptions{Commit: true},
			core.UpdateResult{Changes: []core.Change{changed}}, failure, changedLine},
		{"nothing to commit", core.UpdateOptions{Commit: true},
			core.UpdateResult{Changes: []core.Change{upToDate}}, nil,
			upToDateLine + "nothing to commit\n"},
		{"dry run", core.UpdateOptions{DryRun: true},
			core.UpdateResult{Changes: []core.Change{changed}}, nil, dryLine},
		{"dry run of a commit", core.UpdateOptions{DryRun: true, Commit: true},
			core.UpdateResult{Changes: []core.Change{upToDate, changed}}, nil,
			upToDateLine + dryLine + `would commit "manifest: update kernel to v6.6.9"` + "\n"},
		{"dry run of a commit after a clone", core.UpdateOptions{DryRun: true, Commit: true},
			core.UpdateResult{Changes: []core.Change{changed, unknown}}, nil,
			dryLine + "would update fresh: none -> unknown until fetched, clone\n" +
				"would commit the result\n"},
		{"dry run of an empty commit", core.UpdateOptions{DryRun: true, Commit: true},
			core.UpdateResult{Changes: []core.Change{upToDate}}, nil,
			upToDateLine + "nothing to commit\n"},
		// Initializing and checking out change nothing that a commit records.
		{"dry run of an initialization", core.UpdateOptions{DryRun: true, Commit: true},
			core.UpdateResult{Changes: []core.Change{initOnly, checkoutOnly}}, nil,
			"would update theme: v1.0.0 (a1b2c3d), initialize\n" +
				"would update theme: v1.0.0 (a1b2c3d), HEAD is e4f5a6b\nnothing to commit\n"},
		{"initialization", core.UpdateOptions{Commit: true},
			core.UpdateResult{Changes: []core.Change{initOnly}}, nil,
			"theme: v1.0.0 (a1b2c3d), initialized\nnothing to commit\n"},
	}
	for _, tt := range tests {
		var out bytes.Buffer
		writeUpdate(&out, tt.opts, tt.res, tt.err)
		if out.String() != tt.want {
			t.Errorf("%s:\n got %q\nwant %q", tt.name, out.String(), tt.want)
		}
	}
}

// tableStatuses covers every state and the special cells of the table.
func tableStatuses() []core.Status {
	tp, tag := manifest.ModeTagPattern, manifest.ModeTag
	commitSub := tagSub("crypto lib", manifest.ModeCommit, commitB)
	commitSub.Path = "libs/crypto lib"
	return []core.Status{
		{Submodule: tagSub("kernel", tp, "v6.6.*"), Lock: entry("kernel", tp, "v6.6.9", commitA),
			Head: commitA, State: core.StateBehind},
		{Submodule: tagSub("ядро", tag, "v1"), Lock: entry("ядро", tag, "v1", commitA),
			Head: commitA, State: core.StateOK},
		{Submodule: commitSub, Lock: entry("crypto lib", manifest.ModeCommit, commitB, commitB),
			Head: commitB, State: core.StateOK},
		{Submodule: manifest.Submodule{Name: "legacy", Path: "vendor/legacy", Ref: "v9"},
			Lock: entry("legacy", tag, "v1", commitA), Head: commitC,
			State: core.StateUnmanaged},
		{Submodule: tagSub("theme", tag, "v1"), Lock: entry("theme", tag, "v1", commitA),
			State: core.StateUninitialized},
		{Submodule: tagSub("app", tag, "v1"), Head: commitC, State: core.StateDirty},
		{Submodule: tagSub("fpga", tag, "v2"), Lock: entry("fpga", tag, "v2", commitA),
			Head: commitA, State: core.StateDrift},
		{Submodule: tagSub("broken", tag, "v9"), Lock: entry("broken", tag, "v1", commitA),
			Head: commitA, State: core.StateMissingRef},
		{Submodule: manifest.Submodule{Name: "evil\x1b]0;x\x07", Path: "a\tb", Mode: tag,
			Ref: "v1\n"}, Lock: entry("evil", tag, "v\"1", commitA), State: core.StateMissingRef},
	}
}

func TestWriteStatusTable(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	if err := writeStatusTable(&out, tableStatuses(), false); err != nil {
		t.Fatal(err)
	}
	wantGolden(t, "table", out.String())
	wantPrintable(t, "table", out.String())
	plain := out.String()

	out.Reset()
	if err := writeStatusTable(&out, tableStatuses(), true); err != nil {
		t.Fatal(err)
	}
	colors := []string{"\x1b[1m", "\x1b[33m", "\x1b[32m", "\x1b[32m", "\x1b[90m", "\x1b[90m",
		"\x1b[35m", "\x1b[31m", "\x1b[31m", "\x1b[31m"}
	var want strings.Builder
	for i, line := range strings.SplitAfter(strings.TrimSuffix(plain, "\n"), "\n") {
		line = strings.TrimSuffix(line, "\n")
		cut := strings.LastIndex(line, "  ") + 2
		want.WriteString(line[:cut] + colors[i] + line[cut:] + "\x1b[0m\n")
	}
	if out.String() != want.String() {
		t.Errorf("colored table:\n%s", lineDiff(want.String(), out.String()))
	}

	out.Reset()
	if err := writeStatusTable(&out, nil, true); err != nil || out.String() != "no submodules\n" {
		t.Errorf("empty table %q, %v", out.String(), err)
	}
	if err := writeStatusTable(failingWriter{}, tableStatuses(), false); err == nil {
		t.Error("write error not reported")
	}
}

func TestWriteVerifyResult(t *testing.T) {
	t.Parallel()
	sub := tagSub("fpga", manifest.ModeTag, "v2")
	tests := []struct {
		checks []core.Check
		want   string
	}{
		{
			[]core.Check{{Name: core.CheckLockEntry, OK: true}, {Name: core.CheckHead, OK: true}},
			"fpga: ok (2 checks)\n",
		},
		{
			[]core.Check{
				{Name: core.CheckLockEntry, OK: true, Detail: "present"},
				{Name: core.CheckGitlink, Detail: "HEAD records a1b2c3d4e5f6"},
				{Name: core.CheckTag, Detail: "moved\x1b[2J tag"},
			},
			"fpga: failed (2 of 3 checks)\n  gitlink: HEAD records a1b2c3d4e5f6\n" +
				"  tag: moved\ufffd[2J tag\n",
		},
		{nil, "fpga: failed (no check ran)\n"},
	}
	for _, tt := range tests {
		var out bytes.Buffer
		writeVerifyResult(&out, core.VerifyResult{Submodule: sub, Checks: tt.checks})
		if out.String() != tt.want {
			t.Errorf("checks %+v:\n got %q\nwant %q", tt.checks, out.String(), tt.want)
		}
	}
}

func TestFetchResultAndTrackedRef(t *testing.T) {
	t.Parallel()
	for r, want := range map[core.FetchResult]string{
		{}:                         "fetched",
		{Init: true}:               "initialized and fetched",
		{Init: true, Cloned: true}: "cloned and fetched",
	} {
		if got := fetchResult(r); got != want {
			t.Errorf("fetchResult(%+v) = %q, want %q", r, got, want)
		}
	}
	commitMode := manifest.ModeCommit
	expanded := manifest.Submodule{Name: "x", Mode: commitMode, Ref: commitA}
	for _, tt := range []struct {
		sub   manifest.Submodule
		mode  manifest.Mode
		given string
		want  string
	}{
		{expanded, commitMode, "a1b2c3d", commitA},
		{manifest.Submodule{Name: "x"}, manifest.ModeTag, "v1\x07", "v1\ufffd"},
		{expanded, manifest.ModeTag, "v1", "v1"},
	} {
		if got := trackedRef(tt.sub, tt.mode, tt.given); got != tt.want {
			t.Errorf("trackedRef(%+v, %s, %q) = %q, want %q", tt.sub, tt.mode, tt.given, got,
				tt.want)
		}
	}
}
