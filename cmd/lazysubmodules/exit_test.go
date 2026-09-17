// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/FPGArtktic/lazysubmodules/internal/core"
	"github.com/FPGArtktic/lazysubmodules/internal/git"
	"github.com/FPGArtktic/lazysubmodules/internal/lock"
	"github.com/FPGArtktic/lazysubmodules/internal/manifest"
)

func TestExitCode(t *testing.T) {
	t.Parallel()
	exitErr := exec.CommandContext(t.Context(), "sh", "-c", "exit 3").Run()
	if _, ok := errors.AsType[*exec.ExitError](exitErr); !ok {
		t.Fatalf("sh: %v", exitErr)
	}
	gitErr := &git.Error{Args: []string{"fetch", "origin"}, ExitCode: 128,
		Err: errors.New("exit status 128")}
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"nil", nil, exitOK},
		{"plain", errors.New("boom"), exitError},
		{"unknown name", fmt.Errorf("kernel: %w", core.ErrNotFound), exitError},
		{"unknown names", errors.Join(fmt.Errorf("a: %w", core.ErrNotFound),
			fmt.Errorf("b: %w", core.ErrNotFound)), exitError},
		{"invalid mode", fmt.Errorf("kernel: %w", manifest.ErrInvalidMode), exitError},
		{"invalid lock", fmt.Errorf("load: %w", lock.ErrInvalidEntry), exitError},
		{"symbolic link", fmt.Errorf(".lsm.lock: %w", git.ErrNotRegularFile), exitError},
		{"no git", git.ErrGitNotFound, exitError},
		{"command failed", fmt.Errorf("kernel: sh: %w", exitErr), exitError},
		{"canceled", context.Canceled, exitError},
		{"invalid argument", fmt.Errorf("kernel: %w", core.ErrInvalidArgument), exitUsage},
		{"refused", core.ErrRefused, exitRefused},
		{"refusals", errors.Join(fmt.Errorf("app: %w", core.ErrDirty),
			fmt.Errorf("broken: %w", core.ErrMissingRef)), exitRefused},
		{"verify", fmt.Errorf("%w: fresh, broken", core.ErrVerify), exitVerify},
		{"git", gitErr, exitGit},
		{"wrapped git", fmt.Errorf("fetch kernel: %w", gitErr), exitGit},
		{"git and rollback", errors.Join(gitErr, errors.New("restore kernel")), exitGit},
		{"interrupted git", &git.Error{ExitCode: -1, Err: context.Canceled}, exitGit},
		{"refusal and git", errors.Join(fmt.Errorf("app: %w", core.ErrDirty), gitErr),
			exitRefused},
	}
	for _, tt := range tests {
		if got := exitCode(tt.err); got != tt.want {
			t.Errorf("%s: exitCode(%v) = %d, want %d", tt.name, tt.err, got, tt.want)
		}
	}
	for _, err := range []error{core.ErrUnmanaged, core.ErrDirty, core.ErrMissingRef,
		core.ErrUninitialized, core.ErrUnrelatedStaged, core.ErrUnmergedIndex,
		core.ErrSymlinkPath, core.ErrNotSubmodule, core.ErrNoURL, core.ErrNotRepository,
		core.ErrPathExists} {
		if got := exitCode(fmt.Errorf("x: %w", err)); got != exitRefused {
			t.Errorf("exitCode(%v) = %d, want %d", err, got, exitRefused)
		}
	}
}

func TestFail(t *testing.T) {
	t.Parallel()
	var stderr bytes.Buffer
	e := &env{stderr: &stderr}
	err := errors.Join(fmt.Errorf("app: %w", core.ErrDirty),
		errors.New("tag\x1b]0;owned\x07\tgone"))
	if code := e.fail(err); code != exitRefused {
		t.Errorf("fail = %d", code)
	}
	want := "lazysubmodules: app: refused: submodule has uncommitted changes\n" +
		"lazysubmodules: tag�]0;owned�\tgone\n"
	if stderr.String() != want {
		t.Errorf("stderr %q, want %q", stderr.String(), want)
	}
}

func TestColorOutput(t *testing.T) {
	t.Parallel()
	tests := []struct {
		environ  []string
		terminal bool
		want     bool
	}{
		{[]string{"TERM=xterm-256color"}, true, true},
		{nil, true, true},
		{[]string{"TERM=xterm-256color"}, false, false},
		{[]string{"TERM=dumb"}, true, false},
		{[]string{"TERM=xterm", "NO_COLOR=1"}, true, false},
		{[]string{"TERM=xterm", "NO_COLOR=yes please"}, true, false},
		{[]string{"TERM=xterm", "NO_COLOR="}, true, true},
		{[]string{"NO_COLOR=1", "NO_COLOR="}, true, true},
		{[]string{"TERM=dumb", "TERM=xterm"}, true, true},
		{[]string{"XNO_COLOR=1", "NO_COLORX=1", "TERM=linux"}, true, true},
	}
	for _, tt := range tests {
		e := &env{environ: tt.environ, stdout: &bytes.Buffer{},
			terminal: func(any) bool { return tt.terminal }}
		if got := e.colorOutput(); got != tt.want {
			t.Errorf("environ %q, terminal %v: colorOutput = %v", tt.environ, tt.terminal, got)
		}
	}
}

func TestIsTerminal(t *testing.T) {
	t.Parallel()
	file, err := os.Create(filepath.Join(t.TempDir(), "out"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	null, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = null.Close() })
	for _, stream := range []any{nil, &bytes.Buffer{}, file, null} {
		if isTerminal(stream) {
			t.Errorf("isTerminal(%T) = true", stream)
		}
	}
	pty, tty := openPTY(t)
	for _, stream := range []any{pty, tty} {
		if !isTerminal(stream) {
			t.Errorf("isTerminal(%s) = false", stream.(*os.File).Name())
		}
	}
	e := &env{}
	if e.isTerminal(&bytes.Buffer{}) || !e.isTerminal(tty) {
		t.Error("env.isTerminal does not default to isTerminal")
	}
}
