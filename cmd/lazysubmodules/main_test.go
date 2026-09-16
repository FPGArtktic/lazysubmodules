// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package main

import (
	"bytes"
	"errors"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"slices"
	"strings"
	"testing"
)

const wantUsage = `Usage: lazysubmodules <command> [arguments]

Manage Git submodules that track branches or tags.

Commands:
  help     show help for lazysubmodules or a command
  version  print version, commit and build date

Run 'lazysubmodules <command> -h' for help on a command.
`

const wantVersionHelp = `Usage: lazysubmodules version

Print the version, the source commit and the build date.
`

// runArgs runs the command line and returns the exit status and output.
func runArgs(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = run(t.Context(), &env{args: args, stdout: &out, stderr: &errOut})
	return code, out.String(), errOut.String()
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
	}{
		{nil, nil, "", false},
		{[]string{"a", "--fetch", "b"}, []string{"a", "b"}, "", true},
		{[]string{"--ref", "v1", "a"}, []string{"a"}, "v1", false},
		{[]string{"a", "-ref=v2", "--fetch=false"}, []string{"a"}, "v2", false},
		{[]string{"--ref", "--fetch"}, nil, "--fetch", false},
		{[]string{"-", "--", "--fetch", "-x"}, []string{"-", "--fetch", "-x"}, "", false},
		{[]string{"--fetch", "--", "--"}, []string{"--"}, "", true},
	}
	for _, tt := range tests {
		fs := newFlagSet("test")
		ref := fs.String("ref", "", "")
		fetch := fs.Bool("fetch", false, "")
		positional, err := splitArgs(fs, tt.args)
		if err != nil || !slices.Equal(positional, tt.positional) ||
			*ref != tt.ref || *fetch != tt.fetch {
			t.Errorf("splitArgs(%q) = %q, %v; ref %q, fetch %v", tt.args, positional, err,
				*ref, *fetch)
		}
	}

	for _, args := range [][]string{{"--ref"}, {"--nope"}, {"---ref=x"}} {
		fs := newFlagSet("test")
		fs.String("ref", "", "")
		if _, err := splitArgs(fs, args); err == nil || errors.Is(err, flag.ErrHelp) {
			t.Errorf("splitArgs(%q) error = %v, want a parse error", args, err)
		}
	}
}

func TestCommandUsageWithFlags(t *testing.T) {
	t.Parallel()
	fs := newFlagSet("x")
	fs.Bool("fetch", false, "fetch first")
	var out bytes.Buffer
	c := command{name: "x", synopsis: "[<name>...]", help: "Do x."}
	writeCommandUsage(&out, c, fs)
	want := "Usage: lazysubmodules x [<name>...]\n\nDo x.\n\nOptions:\n" +
		"  -fetch\n    \tfetch first\n"
	if out.String() != want {
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

// TestBinary builds the command with the release -ldflags and checks the
// injected version and the exit statuses of the real process.
func TestBinary(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("builds the command")
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go command not available")
	}
	bin := filepath.Join(t.TempDir(), "lazysubmodules")
	ldflags := "-X main.version=v1.2.3-rc.1 -X main.commit=0123456789ab " +
		"-X main.date=2026-09-16T12:00:00Z"
	// Without VCS stamping, the build does not depend on the state of the
	// surrounding repository (GIT_DIR from a hook, dubious ownership).
	build := exec.CommandContext(t.Context(), goBin, "build", "-trimpath", "-buildvcs=false",
		"-ldflags", ldflags, "-o", bin, ".")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}

	out, err := exec.CommandContext(t.Context(), bin, "version").Output()
	want := "lazysubmodules v1.2.3-rc.1\ncommit: 0123456789ab\ndate: 2026-09-16T12:00:00Z\n"
	if err != nil || string(out) != want {
		t.Errorf("version = %q, %v; want %q", out, err, want)
	}

	for args, wantCode := range map[string]int{"help": 0, "": 2, "bogus": 2} {
		cmd := exec.CommandContext(t.Context(), bin, strings.Fields(args)...)
		err := cmd.Run()
		if code := cmd.ProcessState.ExitCode(); code != wantCode {
			t.Errorf("%q: exit status %d (%v), want %d", args, code, err, wantCode)
		}
	}
}
