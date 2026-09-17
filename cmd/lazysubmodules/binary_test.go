// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/FPGArtktic/lazysubmodules/internal/git/gittest"
)

// buildBinary builds the command with the release -ldflags.
func buildBinary(t *testing.T) string {
	t.Helper()
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
	return bin
}

// TestBinary builds the command once and checks the real process: the
// injected version, exit statuses, and commands on a complex superproject,
// also on a terminal.
func TestBinary(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("builds the command")
	}
	bin := buildBinary(t)

	t.Run("version", func(t *testing.T) {
		t.Parallel()
		out, err := exec.CommandContext(t.Context(), bin, "version").Output()
		want := "lazysubmodules v1.2.3-rc.1\ncommit: 0123456789ab\ndate: 2026-09-16T12:00:00Z\n"
		if err != nil || string(out) != want {
			t.Errorf("version = %q, %v; want %q", out, err, want)
		}
	})

	t.Run("exit status", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		for args, wantCode := range map[string]int{
			"help": exitOK, "": exitUsage, "bogus": exitUsage, "status --porcelain=v2": exitUsage,
			"status": exitGit,
		} {
			cmd := exec.CommandContext(t.Context(), bin, strings.Fields(args)...)
			cmd.Dir = dir
			cmd.Env = binaryEnv(t, append(gittest.Env(t),
				"GIT_CEILING_DIRECTORIES="+filepath.Dir(dir))...)
			err := cmd.Run()
			if code := cmd.ProcessState.ExitCode(); code != wantCode {
				t.Errorf("%q: exit status %d (%v), want %d", args, code, err, wantCode)
			}
		}
	})

	t.Run("complex", func(t *testing.T) {
		t.Parallel()
		c := gittest.NewComplexSuper(t, gittest.SHA1)
		b := newBinary(t, bin, c)
		b.commands(c)
		b.terminal()
	})
}

// newBinary returns a binary that runs in the complex superproject c.
func newBinary(t *testing.T, bin string, c *gittest.ComplexSuper) *binary {
	t.Helper()
	return &binary{t: t, path: bin, dir: c.Dir, env: binaryEnv(t, gittest.Env(t, c.Config...)...)}
}

// binaryEnv returns the environment of the built command: the search path,
// a terminal type with colors, and extra.
func binaryEnv(t *testing.T, extra ...string) []string {
	t.Helper()
	return append([]string{"PATH=" + os.Getenv("PATH"), "TERM=xterm-256color"}, extra...)
}

// binary runs the built command in a repository.
type binary struct {
	t    *testing.T
	path string
	dir  string
	env  []string
}

// command returns the command line with extra environment variables.
func (b *binary) command(extra []string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(b.t.Context(), b.path, args...)
	cmd.Dir = b.dir
	cmd.Env = slices.Concat(b.env, extra)
	return cmd
}

// run runs the command line with pipes as standard streams.
func (b *binary) run(args ...string) result {
	b.t.Helper()
	cmd := b.command(nil, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdin, cmd.Stdout, cmd.Stderr = strings.NewReader(""), &stdout, &stderr
	err := cmd.Run()
	if _, ok := errors.AsType[*exec.ExitError](err); err != nil && !ok {
		b.t.Fatal(err)
	}
	return result{code: cmd.ProcessState.ExitCode(), stdout: stdout.String(),
		stderr: stderr.String()}
}

// want checks the exit status and output of a command line.
func (b *binary) want(args []string, code int, stdout, stderr string) {
	b.t.Helper()
	if got := b.run(args...); got != (result{code, stdout, stderr}) {
		b.t.Errorf("%q:\n%v\nwant exit status %d\nstdout:\n%s\nstderr:\n%s", args, got, code,
			stdout, stderr)
	}
}

// commands checks commands whose output goes to pipes.
func (b *binary) commands(c *gittest.ComplexSuper) {
	t := b.t
	res := b.run("status", "--porcelain=v1")
	if res.code != exitOK || res.stderr != "" {
		t.Errorf("status:\n%v", res)
	}
	wantGolden(t, "status-complex-sha1", res.stdout)
	b.want([]string{"update"}, exitRefused, "", complexRefusal)
	b.want([]string{"foreach", "sh", "-c", `test "$name" != u-boot`}, exitError, "",
		"lazysubmodules: u-boot: sh: exit status 1\n")

	f := c.FPGACore
	b.want([]string{"verify", f.Name}, exitOK, "fpga.core: ok (7 checks)\n", "")
	if res := b.run("fetch", f.Name); res.code != exitOK || res.stdout != "fpga.core: fetched\n" {
		t.Errorf("fetch:\n%v", res)
	}
	b.want([]string{"verify", f.Name}, exitVerify,
		"fpga.core: failed (1 of 7 checks)\n"+
			"  tag: tag v2.3.1 points to "+f.Tags[f.Ref][:12]+", locked "+f.Lock.Commit[:12]+
			" (moved tag)\n",
		"lazysubmodules: verification failed: fpga.core\n")
}

// onTerminal runs a command line on a pseudo-terminal until it ends and
// returns the exit status and the output with the line breaks of the
// terminal turned back into "\n".
func (b *binary) onTerminal(extra []string, args ...string) (int, string) {
	b.t.Helper()
	p := startOnPTY(b.t, b.command(extra, args...))
	code := p.wait("")
	return code, terminalText(p.Output())
}

// terminalText turns the line breaks of a terminal back into "\n".
func terminalText(out string) string {
	return strings.ReplaceAll(out, "\r\n", "\n")
}

// terminal checks the commands that behave differently on a terminal.
func (b *binary) terminal() {
	t := b.t
	code, out := b.onTerminal(nil, "status")
	if code != exitOK || !strings.Contains(out, "  \x1b[33mbehind\x1b[0m\n") {
		t.Errorf("status on a terminal: exit status %d, output\n%q", code, out)
	}
	for _, extra := range [][]string{{"NO_COLOR=1"}, {"TERM=dumb"}} {
		code, out := b.onTerminal(extra, "status")
		if code != exitOK || strings.Contains(out, "\x1b") || !strings.Contains(out, "behind") {
			t.Errorf("status on a terminal with %q: exit status %d, output\n%q", extra, code, out)
		}
	}
}
