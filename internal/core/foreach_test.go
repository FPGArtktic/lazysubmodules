// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package core_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/FPGArtktic/lazysubmodules/internal/core"
	"github.com/FPGArtktic/lazysubmodules/internal/git/gittest"
	"github.com/FPGArtktic/lazysubmodules/internal/manifest"
)

// foreach runs Foreach with args in the Repo opened at dir and returns
// the output streams.
func foreach(t *testing.T, f *fixture, dir string, args ...string) (string, string, error) {
	t.Helper()
	r, err := core.Open(t.Context(), f.g, dir)
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr strings.Builder
	err = r.Foreach(t.Context(), core.ForeachOptions{Args: args, Stdout: &stdout, Stderr: &stderr})
	return stdout.String(), stderr.String(), err
}

// envScript prints the variables of Foreach, one line per submodule.
const envScript = `printf '%s|%s|%s|%s|%s|%s|%s|%s\n' "$name" "$sm_path" "$displaypath" ` +
	`"$sha1" "$toplevel" "$LSM_MODE" "$LSM_REF" "$(pwd -P)"`

func TestForeachEnvironment(t *testing.T) {
	t.Parallel()
	f := newFixture(t, gittest.SHA1)
	v100 := f.commits[gittest.TagV100]
	f.track("c", manifest.ModeTagPattern, "v1.*", gittest.TagV100, v100)
	f.add("b", "", "")
	gittest.Git(t, f.super.Dir, "submodule", "add", "--quiet", "--name", "fpga.core", "--",
		f.up.Bare, "ip/fpga core")
	f.configure("fpga.core", manifest.ModeBranch, "main")
	f.super.Deinit(t, "b")
	top := realDir(t, f.super.Dir)
	tip := f.commits[gittest.TagV200RC]

	stdout, stderr, err := foreach(t, f, f.super.Dir, "sh", "-c", envScript)
	want := "c|c|c|" + v100 + "|" + top + "|tag-pattern|v1.*|" + top + "/c\n" +
		"fpga.core|ip/fpga core|ip/fpga core|" + tip + "|" + top + "|branch|main|" +
		top + "/ip/fpga core\n"
	if err != nil || stdout != want || stderr != "" {
		t.Errorf("Foreach = %v\nstdout:\n%s\nwant:\n%s\nstderr:\n%s", err, stdout, want, stderr)
	}

	// Display paths are relative to the directory the Repo was opened from.
	sub := filepath.Join(f.super.Dir, "ip", "x")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(f.super.Dir, link); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{sub, filepath.Join(link, "ip", "x")} {
		stdout, _, err = foreach(t, f, dir, "sh", "-c", `printf '%s\n' "$displaypath"`)
		if err != nil || stdout != "../../c\n../fpga core\n" {
			t.Errorf("Foreach from %s = %q, %v", dir, stdout, err)
		}
	}
}

func TestForeachNoShell(t *testing.T) {
	t.Parallel()
	f := newFixture(t, gittest.SHA1)
	f.add("a", manifest.ModeTag, gittest.TagV100)
	f.add("b", manifest.ModeTag, gittest.TagV100)
	stdout, _, err := foreach(t, f, f.super.Dir, "printf", `%s|`, "$name", "a;b", "*", "`x`")
	if want := "$name|a;b|*|`x`|$name|a;b|*|`x`|"; err != nil || stdout != want {
		t.Errorf("Foreach = %q, %v; want %q", stdout, err, want)
	}

	// A relative command is found in the submodule.
	for _, name := range []string{"a", "b"} {
		script := filepath.Join(f.dir(name), "tools", "run.sh")
		gittest.WriteFile(t, script, "#!/bin/sh\necho \"$name:$1:$(basename \"$(pwd)\")\"\n")
		if err := os.Chmod(script, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	stdout, _, err = foreach(t, f, f.super.Dir, "tools/run.sh", "x y")
	if want := "a:x y:a\nb:x y:b\n"; err != nil || stdout != want {
		t.Errorf("Foreach(relative) = %q, %v; want %q", stdout, err, want)
	}
	_, _, err = foreach(t, f, f.super.Dir, "run.sh")
	if !errors.Is(err, exec.ErrNotFound) || !strings.HasPrefix(err.Error(), "a: run.sh: ") {
		t.Errorf("Foreach(not in PATH) = %v", err)
	}

	// Standard input goes to the commands.
	r := f.repo()
	var stdout2 strings.Builder
	err = r.Foreach(t.Context(), core.ForeachOptions{Args: []string{"cat"},
		Stdin: strings.NewReader("input\n"), Stdout: &stdout2})
	if err != nil || stdout2.String() != "input\n" {
		t.Errorf("Foreach(cat) = %q, %v", stdout2.String(), err)
	}
}

func TestForeachStops(t *testing.T) {
	t.Parallel()
	f := newFixture(t, gittest.SHA1)
	for _, name := range []string{"a", "b", "c"} {
		f.add(name, manifest.ModeTag, gittest.TagV100)
	}
	stdout, _, err := foreach(t, f, f.super.Dir, "sh", "-c", `echo "$name"; test "$name" != b`)
	exitErr, ok := errors.AsType[*exec.ExitError](err)
	if !ok || exitErr.ExitCode() != 1 || !strings.HasPrefix(err.Error(), "b: sh: exit status 1") {
		t.Errorf("Foreach = %v, want exit status 1 of b", err)
	}
	if stdout != "a\nb\n" {
		t.Errorf("stdout %q, want only a and b", stdout)
	}

	r := f.repo()
	for _, args := range [][]string{nil, {}, {""}} {
		err := r.Foreach(t.Context(), core.ForeachOptions{Args: args})
		wantErr(t, "Foreach(no command)", err, core.ErrInvalidArgument)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err = r.Foreach(ctx, core.ForeachOptions{Args: []string{"true"}})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Foreach(canceled) = %v", err)
	}
}

func TestForeachCancelSendsSigterm(t *testing.T) {
	t.Parallel()
	f := newFixture(t, gittest.SHA1)
	f.add("a", manifest.ModeTag, gittest.TagV100)
	r := f.repo()
	out, err := os.Create(filepath.Join(t.TempDir(), "out"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := out.Close(); err != nil {
			t.Error(err)
		}
	})
	// The command reports the signal; its short-lived children leave no
	// process behind that could hold the output open.
	script := `trap 'echo got-term; exit 3' TERM; echo ready; while :; do sleep 0.05; done`
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- r.Foreach(ctx, core.ForeachOptions{Args: []string{"sh", "-c", script},
			Stdout: out})
	}()
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(readFile(t, out.Name()), "ready") {
		if time.Now().After(deadline) {
			t.Fatal("the command did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	start := time.Now()
	cancel()
	err = <-done
	exitErr, ok := errors.AsType[*exec.ExitError](err)
	if !ok || exitErr.ExitCode() != 3 || !strings.HasPrefix(err.Error(), "a: sh: ") {
		t.Errorf("Foreach = %v, want exit status 3 from the trap", err)
	}
	if got := readFile(t, out.Name()); got != "ready\ngot-term\n" {
		t.Errorf("output %q, want the SIGTERM trap", got)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("Foreach returned %v after the cancellation", d)
	}
}

func TestForeachSkipsUninitialized(t *testing.T) {
	t.Parallel()
	f := newFixture(t, gittest.SHA1)
	f.add("a", manifest.ModeTag, gittest.TagV100)
	f.add("b", manifest.ModeTag, gittest.TagV100)
	f.add("c", manifest.ModeTag, gittest.TagV100)
	f.super.Deinit(t, "a")
	f.super.Deinit(t, "c")
	f.removeModule("c")
	stdout, stderr, err := foreach(t, f, f.super.Dir, "sh", "-c", `echo "$name"`)
	want := "skipping a: submodule is not checked out\n" +
		"skipping c: submodule is not checked out\n"
	if err != nil || stdout != "b\n" || stderr != want {
		t.Errorf("Foreach = %v\nstdout %q\nstderr %q", err, stdout, stderr)
	}
	// Without a stream for notes, submodules are skipped silently.
	if err := f.repo().Foreach(t.Context(), core.ForeachOptions{Args: []string{"true"}}); err != nil {
		t.Errorf("Foreach without streams = %v", err)
	}

	empty := gittest.NewSuper(t, gittest.SHA1)
	stdout, _, err = foreach(t, f, empty.Dir, "echo", "x")
	if err != nil || stdout != "" {
		t.Errorf("Foreach without submodules = %q, %v", stdout, err)
	}
}

// foreachHelperDir names the variable that makes TestForeachRepositoryEnv
// run Foreach in a child process with a prepared environment.
const foreachHelperDir = "LSM_TEST_FOREACH_DIR"

func TestForeachRepositoryEnv(t *testing.T) {
	t.Parallel()
	if dir := os.Getenv(foreachHelperDir); dir != "" {
		r, err := core.Open(t.Context(), gittest.Runner(t), dir)
		if err == nil {
			err = r.Foreach(t.Context(), core.ForeachOptions{
				Args:   []string{"sh", "-c", `echo "[${GIT_DIR-unset}|${GIT_INDEX_FILE-unset}|$LSM_KEEP]"`},
				Stdout: os.Stdout,
			})
		}
		if err != nil {
			t.Fatal(err)
		}
		return
	}
	f := newFixture(t, gittest.SHA1)
	f.add("lib", manifest.ModeTag, gittest.TagV100)
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestForeachRepositoryEnv$")
	cmd.Env = append(os.Environ(), foreachHelperDir+"="+f.super.Dir,
		"GIT_DIR="+t.TempDir(), "GIT_INDEX_FILE=/nonexistent", "LSM_KEEP=kept")
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "[unset|unset|kept]\n") {
		t.Errorf("child process: %v\n%s", err, out)
	}
}
