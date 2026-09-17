// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/FPGArtktic/lazysubmodules/internal/manifest"
)

// TestSynopsis checks the first help line of every command against the
// command reference in README.md.
func TestSynopsis(t *testing.T) {
	t.Parallel()
	want := []string{
		"lazysubmodules add <url> <path> " +
			"(--branch B | --tag T | --tag-pattern P | --commit SHA)",
		"lazysubmodules set <name> (--branch B | --tag T | --tag-pattern P | --commit SHA)",
		"lazysubmodules update [<name>...] [--fetch] [--dry-run] [--commit] " +
			"[--include-prerelease]",
		"lazysubmodules status [<name>...] [--porcelain=v1]",
		"lazysubmodules fetch [<name>...]",
		"lazysubmodules verify [<name>...] [--signatures]",
		"lazysubmodules foreach -- <command> [args...]",
		"lazysubmodules version",
		"lazysubmodules help [<command>]",
	}
	var got []string
	for _, c := range commands() {
		res := outside(t).wantCode(t, exitOK, "help", c.name)
		line, _, _ := strings.Cut(res.stdout, "\n")
		got = append(got, strings.TrimPrefix(line, "Usage: "))
	}
	if !slices.Equal(got, want) {
		t.Errorf("synopses:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// TestCommandHelp compares the help of every command, as "<command> -h"
// prints it, with testdata/help.golden.
func TestCommandHelp(t *testing.T) {
	t.Parallel()
	var all strings.Builder
	for _, c := range commands() {
		res := outside(t).wantCode(t, exitOK, c.name, "-h")
		if res.stderr != "" {
			t.Errorf("%s -h: stderr %q", c.name, res.stderr)
		}
		for _, args := range [][]string{{c.name, "--help"}, {"help", c.name}} {
			if other := outside(t).run(t, args...); other != res {
				t.Errorf("%q:\n%v\nwant\n%v", args, other, res)
			}
		}
		for line := range strings.Lines(res.stdout) {
			// The synopsis is quoted from the command reference.
			if len(line) > 81 && !strings.HasPrefix(line, "Usage: ") {
				t.Errorf("%s -h: line longer than 80 columns: %q", c.name, line)
			}
		}
		all.WriteString("$ lazysubmodules " + c.name + " -h\n" + res.stdout + "\n")
	}
	wantGolden(t, "help", all.String())
}

func TestCommandUsageErrors(t *testing.T) {
	t.Parallel()
	const trackingRequired = "one of --branch, --tag, --tag-pattern or --commit is required"
	const trackingOnce = "only one of --branch, --tag, --tag-pattern or --commit may be given"
	tests := []struct {
		args []string
		msg  string
	}{
		{[]string{"add"}, "add: missing <url> and <path>"},
		{[]string{"add", "u", "--tag", "v1"}, "add: missing <path>"},
		{[]string{"add", "u", "p", "q", "--tag=v1"}, `add: unexpected argument "q"`},
		{[]string{"add", "u", "p"}, "add: " + trackingRequired},
		{[]string{"add", "u", "p", "--include-prerelease"}, "add: " + trackingRequired},
		{[]string{"add", "--tag", "a", "u", "p", "--branch", "b"}, "add: " + trackingOnce},
		{[]string{"add", "u", "p", "--tag", "a", "--tag=b"}, "add: " + trackingOnce},
		{[]string{"add", "u", "p", "--tag"}, "add: flag needs an argument: -tag"},
		{[]string{"add", "u", "p", "--commit=abc", "--include-prerelease=maybe"},
			`add: invalid boolean value "maybe" for -include-prerelease: parse error`},
		{[]string{"set"}, "set: missing <name>"},
		{[]string{"set", "a", "b", "--tag", "v1"}, `set: unexpected argument "b"`},
		{[]string{"set", "kernel"}, "set: " + trackingRequired},
		{[]string{"set", "kernel", "--tag-pattern", "v*", "--commit", "abc"},
			"set: " + trackingOnce},
		{[]string{"set", "--", "kernel", "--tag", "v1"}, `set: unexpected argument "--tag"`},
		{[]string{"set", "--tag", "v1", "--", "-x", "\x1b[2J"},
			`set: unexpected argument "\x1b[2J"`},
		{[]string{"set", "kernel", "--include-prerelease", "--tag", "v1"},
			"set: flag provided but not defined: -include-prerelease"},
		{[]string{"update", "--bogus"}, "update: flag provided but not defined: -bogus"},
		{[]string{"update", "kernel", "--fetch=sometimes"},
			`update: invalid boolean value "sometimes" for -fetch: parse error`},
		{[]string{"status", "--porcelain=v2"}, `status: invalid value "v2" for flag ` +
			`-porcelain: unsupported format "v2" (only v1 is supported)`},
		{[]string{"status", "--porcelain="}, `status: invalid value "" for flag ` +
			`-porcelain: unsupported format "" (only v1 is supported)`},
		{[]string{"status", "--porcelain=true"}, `status: invalid value "true" for flag ` +
			`-porcelain: unsupported format "true" (only v1 is supported)`},
		{[]string{"status", "--short"}, "status: flag provided but not defined: -short"},
		{[]string{"fetch", "--tags"}, "fetch: flag provided but not defined: -tags"},
		{[]string{"verify", "--signature"}, "verify: flag provided but not defined: -signature"},
		{[]string{"foreach"}, "foreach: missing command"},
		{[]string{"foreach", "--"}, "foreach: missing command"},
		{[]string{"foreach", "--", ""}, "foreach: missing command"},
		{[]string{"foreach", "--quiet", "--", "true"},
			"foreach: flag provided but not defined: -quiet"},
	}
	for _, tt := range tests {
		outside(t).want(t, tt.args, exitUsage, "", "lazysubmodules: "+tt.msg+"\n"+usageHint)
	}
}

// TestArgumentsReachRepository checks that valid command lines get past
// argument checking: outside of a repository, git fails.
func TestArgumentsReachRepository(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		{"status"},
		{"status", "kernel", "--porcelain"},
		{"update", "kernel", "--fetch", "--dry-run", "--commit", "--include-prerelease"},
		{"fetch", "a", "b"},
		{"verify", "--signatures", "a"},
		{"set", "kernel", "--commit", "abc1234"},
		{"add", "--tag-pattern=v*", "https://git.example.invalid/a.git", "a",
			"--include-prerelease"},
		{"foreach", "git", "status", "--short"},
		{"foreach", "--", "-weird-command"},
	} {
		res := outside(t).wantCode(t, exitGit, args...)
		if res.stdout != "" || !strings.HasPrefix(res.stderr, "lazysubmodules: ") ||
			!strings.Contains(res.stderr, "git rev-parse") ||
			!strings.Contains(res.stderr, "not a git repository") {
			t.Errorf("%q:\n%v", args, res)
		}
	}
}

func TestTrackingFlags(t *testing.T) {
	t.Parallel()
	tests := []struct {
		args []string
		mode manifest.Mode
		ref  string
	}{
		{[]string{"--branch", "main"}, manifest.ModeBranch, "main"},
		{[]string{"--tag=v1.0.0"}, manifest.ModeTag, "v1.0.0"},
		{[]string{"-tag-pattern", "v6.6.*"}, manifest.ModeTagPattern, "v6.6.*"},
		{[]string{"--commit", "0123abc"}, manifest.ModeCommit, "0123abc"},
		{[]string{"--tag="}, manifest.ModeTag, ""},
	}
	for _, tt := range tests {
		fs := newFlagSet("test")
		track := addTrackingFlags(fs)
		if _, err := splitArgs(fs, tt.args); err != nil {
			t.Fatalf("%q: %v", tt.args, err)
		}
		mode, ref, err := track.mode()
		if err != nil || mode != tt.mode || ref != tt.ref {
			t.Errorf("%q: mode %q, ref %q, %v", tt.args, mode, ref, err)
		}
		if got := fs.Lookup(string(tt.mode)).Value.String(); got != tt.ref {
			t.Errorf("%q: flag value %q", tt.args, got)
		}
	}
}
