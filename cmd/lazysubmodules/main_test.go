// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package main

import (
	"bytes"
	"errors"
	"flag"
	"regexp"
	"runtime/debug"
	"slices"
	"testing"
)

const wantUsage = `Usage: lazysubmodules <command> [arguments]

Manage Git submodules that track branches or tags.

Commands:
  add      clone a new submodule that tracks a ref (network)
  set      change what a submodule tracks
  update   move submodules to the refs they track and stage the result
  status   show the state of submodules
  fetch    download branches and tags of submodules (network)
  verify   check that lock file, gitlinks and checkouts agree
  foreach  run a command in every managed submodule
  tui      start the interactive terminal interface
  version  print version, commit and build date
  help     show help for lazysubmodules or a command

Run 'lazysubmodules <command> -h' for help on a command.
`

const wantVersionHelp = `Usage: lazysubmodules version

Print the version, the source commit and the build date.
`

// runArgs runs the command line outside of every repository and returns
// the exit status and output.
func runArgs(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	res := outside(t).run(t, args...)
	return res.code, res.stdout, res.stderr
}

func TestVersion(t *testing.T) {
	t.Parallel()
	re := regexp.MustCompile(`\Alazysubmodules \S+\ncommit: \S+\ndate: \S+\n\z`)
	for _, args := range [][]string{{"version"}, {"--version"}, {"version", "--"}} {
		code, stdout, stderr := runArgs(t, args...)
		if code != exitOK || !re.MatchString(stdout) || stderr != "" {
			t.Errorf("%q: code %d, stdout %q, stderr %q", args, code, stdout, stderr)
		}
	}
}

func TestHelp(t *testing.T) {
	t.Parallel()
	tests := []struct {
		args []string
		want string
	}{
		{[]string{"help"}, wantUsage},
		{[]string{"-h"}, wantUsage},
		{[]string{"--help"}, wantUsage},
		{[]string{"help", "version"}, wantVersionHelp},
		{[]string{"version", "-h"}, wantVersionHelp},
		{[]string{"version", "--help"}, wantVersionHelp},
		{[]string{"--version", "-help"}, wantVersionHelp},
		{[]string{"help", "--", "version"}, wantVersionHelp},
		{[]string{"help", "help"}, "Usage: lazysubmodules help [<command>]\n\n" +
			"Show the list of commands, or the help of one command.\n"},
	}
	for _, tt := range tests {
		code, stdout, stderr := runArgs(t, tt.args...)
		if code != exitOK || stdout != tt.want || stderr != "" {
			t.Errorf("%q: code %d, stdout %q, stderr %q", tt.args, code, stdout, stderr)
		}
	}
}

func TestUsageErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		args []string
		want string // stderr
	}{
		{nil, wantUsage},
		{[]string{"bogus"}, "lazysubmodules: unknown command \"bogus\"\n" +
			"Run 'lazysubmodules help' for usage.\n"},
		{[]string{"--bogus"}, "lazysubmodules: unknown option \"--bogus\"\n" +
			"Run 'lazysubmodules help' for usage.\n"},
		{[]string{""}, "lazysubmodules: unknown command \"\"\n" +
			"Run 'lazysubmodules help' for usage.\n"},
		{[]string{"version", "extra"}, "lazysubmodules: version: unexpected argument \"extra\"\n" +
			"Run 'lazysubmodules help' for usage.\n"},
		{[]string{"version", "--", "-h"}, "lazysubmodules: version: unexpected argument \"-h\"\n" +
			"Run 'lazysubmodules help' for usage.\n"},
		{[]string{"version", "--bogus"}, "lazysubmodules: version: " +
			"flag provided but not defined: -bogus\nRun 'lazysubmodules help' for usage.\n"},
		{[]string{"help", "bogus"}, "lazysubmodules: help: unknown command \"bogus\"\n" +
			"Run 'lazysubmodules help' for usage.\n"},
		{[]string{"help", "a", "b"}, "lazysubmodules: help: too many arguments\n" +
			"Run 'lazysubmodules help' for usage.\n"},
	}
	for _, tt := range tests {
		code, stdout, stderr := runArgs(t, tt.args...)
		if code != exitUsage || stdout != "" || stderr != tt.want {
			t.Errorf("%q: code %d, stdout %q, stderr %q", tt.args, code, stdout, stderr)
		}
	}
}

// failingWriter fails every write.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("disk full")
}

func TestVersionWriteError(t *testing.T) {
	t.Parallel()
	var stderr bytes.Buffer
	code := run(t.Context(), &env{
		args:   []string{"version"},
		stdout: failingWriter{},
		stderr: &stderr,
	})
	if code != exitError || stderr.String() != "lazysubmodules: disk full\n" {
		t.Errorf("code %d, stderr %q", code, stderr.String())
	}
}

func TestSplitArgs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		args       []string
		positional []string
		ref        string
		fetch      bool
		format     porcelainVersion
	}{
		{nil, nil, "", false, ""},
		{[]string{"a", "--fetch", "b"}, []string{"a", "b"}, "", true, ""},
		{[]string{"--ref", "v1", "a"}, []string{"a"}, "v1", false, ""},
		{[]string{"a", "-ref=v2", "--fetch=false"}, []string{"a"}, "v2", false, ""},
		{[]string{"--ref", "--fetch"}, nil, "--fetch", false, ""},
		{[]string{"-", "--", "--fetch", "-x"}, []string{"-", "--fetch", "-x"}, "", false, ""},
		{[]string{"--fetch", "--", "--"}, []string{"--"}, "", true, ""},
		// An optional value never takes the next argument.
		{[]string{"--porcelain", "v1"}, []string{"v1"}, "", false, porcelainV1},
		{[]string{"a", "-porcelain=v1", "--porcelain"}, []string{"a"}, "", false, porcelainV1},
		{[]string{"--", "--porcelain"}, []string{"--porcelain"}, "", false, ""},
	}
	for _, tt := range tests {
		fs := newFlagSet("test")
		ref := fs.String("ref", "", "")
		fetch := fs.Bool("fetch", false, "")
		var format porcelainVersion
		fs.Var(&format, "porcelain", "")
		positional, err := splitArgs(fs, tt.args)
		if err != nil || !slices.Equal(positional, tt.positional) ||
			*ref != tt.ref || *fetch != tt.fetch || format != tt.format {
			t.Errorf("splitArgs(%q) = %q, %v; ref %q, fetch %v, porcelain %q", tt.args,
				positional, err, *ref, *fetch, format)
		}
	}

	for _, args := range [][]string{
		{"--ref"}, {"--nope"}, {"---ref=x"}, {"---ref", "x"}, {"--porcelain=v2"},
		{"--porcelain=true"}, {"--porcelain="}, {"---porcelain"},
	} {
		fs := newFlagSet("test")
		fs.String("ref", "", "")
		var format porcelainVersion
		fs.Var(&format, "porcelain", "")
		if _, err := splitArgs(fs, args); err == nil || errors.Is(err, flag.ErrHelp) {
			t.Errorf("splitArgs(%q) error = %v, want a parse error", args, err)
		}
	}
}

func TestCommandUsageWithFlags(t *testing.T) {
	t.Parallel()
	fs := newFlagSet("x")
	fs.Bool("fetch", false, "fetch first")
	fs.String("ref", "", "use the `revision`")
	var format porcelainVersion
	fs.Var(&format, "porcelain", "print `v1`")
	var out bytes.Buffer
	c := command{name: "x", synopsis: "[<name>...]", help: "Do x."}
	writeCommandUsage(&out, c, fs)
	want := "Usage: lazysubmodules x [<name>...]\n\nDo x.\n\nOptions:\n" +
		"  --fetch           fetch first\n" +
		"  --porcelain[=v1]  print v1\n" +
		"  --ref <revision>  use the revision\n"
	if out.String() != want {
		t.Errorf("usage = %q, want %q", out.String(), want)
	}
	out.Reset()
	writeCommandUsage(&out, command{name: "y", help: "Do y."}, newFlagSet("y"))
	if want := "Usage: lazysubmodules y\n\nDo y.\n"; out.String() != want {
		t.Errorf("usage = %q, want %q", out.String(), want)
	}
}

func TestResolveBuildInfo(t *testing.T) {
	t.Parallel()
	placeholder := buildInfo{version: "dev", commit: "none", date: "unknown"}
	linked := buildInfo{version: "v0.1.0", commit: "abc", date: "2026-09-16T00:00:00Z"}
	vcs := &debug.BuildInfo{
		Main: debug.Module{Path: "github.com/FPGArtktic/lazysubmodules", Version: "(devel)"},
		Settings: []debug.BuildSetting{
			{Key: "vcs", Value: "git"},
			{Key: "vcs.revision", Value: "0123abcd"},
			{Key: "vcs.time", Value: "2026-09-01T10:00:00Z"},
			{Key: "vcs.modified", Value: "true"},
		},
	}
	tagged := &debug.BuildInfo{Main: debug.Module{Version: "v0.2.0"}}
	tests := []struct {
		name   string
		linked buildInfo
		bi     *debug.BuildInfo
		ok     bool
		want   buildInfo
	}{
		{"linked", linked, vcs, true, linked},
		{"no build info", placeholder, nil, false, placeholder},
		{"nil build info", placeholder, nil, true, placeholder},
		{"vcs", placeholder, vcs, true, buildInfo{"dev", "0123abcd", "2026-09-01T10:00:00Z"}},
		{"module version", placeholder, tagged, true, buildInfo{"v0.2.0", "none", "unknown"}},
		{
			"partially linked",
			buildInfo{version: "dev", commit: "fff", date: "unknown"},
			vcs, true,
			buildInfo{"dev", "fff", "2026-09-01T10:00:00Z"},
		},
	}
	for _, tt := range tests {
		if got := resolveBuildInfo(tt.linked, tt.bi, tt.ok); got != tt.want {
			t.Errorf("%s: resolveBuildInfo = %+v, want %+v", tt.name, got, tt.want)
		}
	}
}
