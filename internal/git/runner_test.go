// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package git_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/FPGArtktic/lazysubmodules/internal/git"
	"github.com/FPGArtktic/lazysubmodules/internal/git/gittest"
)

func TestNew(t *testing.T) {
	t.Parallel()
	r, err := git.New()
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(r.Bin()) || filepath.Base(r.Bin()) != "git" {
		t.Errorf("Bin() = %q, want an absolute path to git", r.Bin())
	}
}

func TestRunEnvironment(t *testing.T) {
	t.Parallel()
	// A shell alias prints the environment git itself received.
	r := gittest.Runner(t, "alias.printenv=!env")
	out, err := r.Run(t.Context(), t.TempDir(), "printenv")
	if err != nil {
		t.Fatal(err)
	}
	env := strings.Split(out, "\n")
	for _, want := range []string{
		"LC_ALL=C",
		"GIT_OPTIONAL_LOCKS=0",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=" + gittest.Name,
	} {
		if !slices.Contains(env, want) {
			t.Errorf("environment lacks %q", want)
		}
	}
}

func TestRunPassesArgumentsIntact(t *testing.T) {
	t.Parallel()
	r := gittest.Runner(t)
	out, err := r.Run(t.Context(), "", "rev-parse", "--sq-quote", "a b", "it's", "", "$HOME;x")
	if err != nil {
		t.Fatal(err)
	}
	want := ` 'a b' 'it'\''s' '' '$HOME;x'` + "\n"
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestRunError(t *testing.T) {
	t.Parallel()
	r := gittest.Runner(t)
	dir := gittest.InitRepo(t, gittest.SHA1)
	out, err := r.Run(t.Context(), dir, "rev-parse", "--verify", "no such ref")
	if out != "" {
		t.Errorf("output = %q", out)
	}
	gitErr, ok := errors.AsType[*git.Error](err)
	if !ok {
		t.Fatalf("error %v is not *git.Error", err)
	}
	if gitErr.ExitCode != 128 || gitErr.Dir != dir ||
		!slices.Equal(gitErr.Args, []string{"rev-parse", "--verify", "no such ref"}) ||
		gitErr.Stderr != "fatal: Needed a single revision" {
		t.Errorf("error = %+v", gitErr)
	}
	want := `git rev-parse --verify "no such ref": exit status 128: ` +
		"fatal: Needed a single revision"
	if err.Error() != want {
		t.Errorf("Error() = %q, want %q", err.Error(), want)
	}
}

func TestExecMissingDirectory(t *testing.T) {
	t.Parallel()
	r := gittest.Runner(t)
	dir := filepath.Join(t.TempDir(), "missing")
	_, err := r.Run(t.Context(), dir, "status")
	gitErr, ok := errors.AsType[*git.Error](err)
	if !ok || gitErr.ExitCode != -1 || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("error = %#v, want *git.Error with exit code -1 wrapping ErrNotExist", err)
	}
}

func TestExecStreams(t *testing.T) {
	t.Parallel()
	r := gittest.Runner(t)
	ctx := t.Context()

	var stdout bytes.Buffer
	out, err := r.Exec(ctx, git.Cmd{Args: []string{"version"}, Stdout: &stdout})
	if err != nil || out != "" || !strings.HasPrefix(stdout.String(), "git version ") {
		t.Errorf("streamed stdout: out=%q err=%v buffer=%q", out, err, stdout.String())
	}

	var stderr bytes.Buffer
	dir := gittest.InitRepo(t, gittest.SHA1)
	_, err = r.Exec(ctx, git.Cmd{
		Dir:    dir,
		Args:   []string{"rev-parse", "--verify", "nope"},
		Stderr: &stderr,
	})
	gitErr, ok := errors.AsType[*git.Error](err)
	if !ok || gitErr.Stderr != "" || !strings.Contains(stderr.String(), "fatal:") {
		t.Errorf("streamed stderr: err=%v buffer=%q", err, stderr.String())
	}
}

func TestExecStdin(t *testing.T) {
	t.Parallel()
	r := gittest.Runner(t)
	out, err := r.Exec(t.Context(), git.Cmd{
		Args:  []string{"hash-object", "--stdin"},
		Stdin: strings.NewReader("x\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := "587be6b4c3f93f93c489c0111bba5596147a26cb\n"; out != want {
		t.Errorf("hash-object = %q, want %q", out, want)
	}
}

func TestExecDefaultStdinIsEmpty(t *testing.T) {
	t.Parallel()
	r := gittest.Runner(t)
	out, err := r.Run(t.Context(), "", "hash-object", "--stdin")
	if err != nil {
		t.Fatal(err)
	}
	if want := "e69de29bb2d1d6434b8b29ae775ad8c2e48c5391\n"; out != want {
		t.Errorf("hash-object of null stdin = %q, want %q", out, want)
	}
}

func TestExecContextCanceledBeforeStart(t *testing.T) {
	t.Parallel()
	r := gittest.Runner(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := r.Run(ctx, "", "version")
	gitErr, ok := errors.AsType[*git.Error](err)
	if !ok || gitErr.ExitCode != -1 || !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want *git.Error wrapping context.Canceled", err)
	}
	if want := "git version: context canceled"; err.Error() != want {
		t.Errorf("Error() = %q, want %q", err.Error(), want)
	}
}

func TestExecEnv(t *testing.T) {
	t.Parallel()
	r := gittest.Runner(t, "alias.printenv=!env")
	ctx := t.Context()
	errs := make(chan error, 8)
	for i := range 8 {
		go func() {
			value := strconv.Itoa(i + 1)
			out, err := r.Exec(ctx, git.Cmd{
				Args: []string{"printenv"},
				Env:  []string{"LSM_TEST_VALUE=" + value, "GIT_TERMINAL_PROMPT=" + value},
			})
			env := strings.Split(out, "\n")
			switch {
			case err != nil:
			case !slices.Contains(env, "LSM_TEST_VALUE="+value):
				err = fmt.Errorf("invocation %d: LSM_TEST_VALUE missing", i)
			case slices.Contains(env, "GIT_TERMINAL_PROMPT=0"):
				err = fmt.Errorf("invocation %d: runner environment not overridden", i)
			}
			errs <- err
		}()
	}
	for range 8 {
		if err := <-errs; err != nil {
			t.Error(err)
		}
	}
	out, err := r.Run(ctx, "", "printenv")
	if err != nil || strings.Contains(out, "LSM_TEST_VALUE=") ||
		!strings.Contains(out, "\nGIT_TERMINAL_PROMPT=0\n") {
		t.Errorf("environment leaked into a later invocation: %v\n%s", err, out)
	}
}

// waitForFile waits until path exists.
func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		_, err := os.Stat(path)
		if err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s did not appear: %v", path, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestExecCancelSendsSIGTERM(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mark := filepath.Join(dir, "mark")
	ready := filepath.Join(dir, "ready")
	// The alias, a child of git, records SIGTERM and gives up after 10 s.
	alias := `!trap 'echo term >"$LSM_MARK"; exit 1' TERM; : >"$LSM_READY"; ` +
		`i=0; while [ $i -lt 200 ]; do sleep 0.05; i=$((i + 1)); done`
	r := gittest.Runner(t, "alias.waitterm="+alias)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := r.Exec(ctx, git.Cmd{
			Dir:  dir,
			Args: []string{"waitterm"},
			Env:  []string{"LSM_MARK=" + mark, "LSM_READY=" + ready},
		})
		done <- err
	}()
	waitForFile(t, ready)
	start := time.Now()
	cancel()
	err := <-done
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("cancellation took %v", elapsed)
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want context.Canceled", err)
	}
	if _, err := os.Stat(mark); err != nil {
		t.Errorf("the child of git did not receive SIGTERM: %v", err)
	}
}

func TestExecCancelWhileGitStartsProcesses(t *testing.T) {
	t.Parallel()
	// "git submodule update" starts a chain of processes, down to the remote
	// helper "hang", a script that runs sleep. A cancellation that lands
	// while the chain is being built must end every process of it, and
	// promptly, since the sleep holds the output pipe of git.
	f := newFixture(t, gittest.SHA1)
	bin := t.TempDir()
	helper := filepath.Join(bin, "git-remote-hang")
	gittest.WriteFile(t, helper, "#!/bin/sh\nsleep 3600\n")
	if err := os.Chmod(helper, 0o755); err != nil {
		t.Fatal(err)
	}
	gittest.Git(t, f.super.Dir, "config", "-f", ".gitmodules", "submodule.lib.url", "hang::x")
	gittest.Git(t, f.super.Dir, "submodule", "sync", "--quiet")
	f.super.Deinit(t, f.path)
	module := filepath.Join(f.super.Dir, ".git", "modules", "lib")
	for delay := range 11 {
		marker := newMarker(t)
		r, err := git.New(git.WithEnv(append(gittest.Env(t), "GIT_ALLOW_PROTOCOL=file:hang",
			"PATH="+bin+string(filepath.ListSeparator)+os.Getenv("PATH"),
			markerVar+"="+marker)...))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.RemoveAll(module); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)
		var progress strings.Builder
		go func() {
			done <- r.SubmoduleInit(ctx, f.super.Dir, f.path, false, &progress)
		}()
		time.Sleep(time.Duration(2*delay) * time.Millisecond)
		start := time.Now()
		cancel()
		err = <-done
		if elapsed := time.Since(start); elapsed > 5*time.Second {
			t.Errorf("delay %d ms: cancellation took %v", 2*delay, elapsed)
		}
		if !errors.Is(err, context.Canceled) {
			t.Errorf("delay %d ms: SubmoduleInit = %v, want context.Canceled", 2*delay, err)
		}
		wantNoProcess(t, marker)
	}
}

func TestExecContextCanceledWhileRunning(t *testing.T) {
	t.Parallel()
	r := gittest.Runner(t)
	// git blocks reading the pipe, whose write end stays open.
	stdin, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = stdin.Close()
		_ = w.Close()
	})
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err = r.Exec(ctx, git.Cmd{Args: []string{"hash-object", "--stdin"}, Stdin: stdin})
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("cancellation took %v", elapsed)
	}
	gitErr, ok := errors.AsType[*git.Error](err)
	if !ok || gitErr.ExitCode != -1 || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want *git.Error wrapping context.DeadlineExceeded", err)
	}
}

func TestRunnerConcurrentUse(t *testing.T) {
	t.Parallel()
	r := gittest.Runner(t)
	dir := gittest.NewSuper(t, gittest.SHA1).Dir
	want := gittest.Git(t, dir, "rev-parse", "HEAD")
	errs := make(chan error, 8)
	for range 8 {
		go func() {
			head, err := r.Head(t.Context(), dir)
			if err == nil && head != want {
				err = errors.New("unexpected HEAD " + head)
			}
			errs <- err
		}()
	}
	for range 8 {
		if err := <-errs; err != nil {
			t.Error(err)
		}
	}
}
