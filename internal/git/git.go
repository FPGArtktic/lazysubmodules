// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

// Package git runs the git command line client.
//
// The package is a thin wrapper without business logic. Each helper maps to
// one git invocation, parses its output and translates well-known "not
// found" exit statuses into sentinel errors. Git is located with
// exec.LookPath and never run through a shell; arguments are always passed
// as a slice.
//
// Every invocation runs with LC_ALL=C, so that output parsing does not
// depend on the locale, with GIT_OPTIONAL_LOCKS=0, and with
// GIT_NO_LAZY_FETCH=1, so that a partial clone does not fetch missing objects
// on demand. Repository-local variables such as GIT_DIR or GIT_INDEX_FILE are
// removed from the inherited environment, so that a surrounding git hook
// cannot redirect a command to another repository. Configuration passed
// through GIT_CONFIG_PARAMETERS and GIT_CONFIG_COUNT is kept.
//
// Only Fetch, SubmoduleAdd, and Checkout and SubmoduleInit with Online use
// the network. Every other helper except Commit also runs with an empty
// GIT_ALLOW_PROTOCOL, which permits no transport at all and so covers git
// releases that ignore GIT_NO_LAZY_FETCH. Commit keeps the configured
// transports, since the hooks it runs may use them; it only disables lazy
// fetching. Hooks see the environment of the command that runs them.
//
// Only features available in git 2.39 are used. Callers must validate
// user-controlled values (refs, patterns, paths) before passing them: empty
// values, a leading "-" and control characters must be rejected.
package git

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strings"
	"syscall"
	"time"
)

// waitDelay bounds how long a canceled git process may take to exit after
// SIGTERM before it is killed.
const waitDelay = 10 * time.Second

// Runner runs git commands. It is immutable after New and safe for
// concurrent use.
type Runner struct {
	bin string
	env []string
}

// Option configures a Runner in New.
type Option func(*Runner)

// WithEnv adds KEY=VALUE pairs to the environment of every invocation.
//
// The pairs are appended after the inherited environment and the variables
// set by the package, so they take precedence. Tests use it to isolate git
// from the user configuration.
//
// Context: only as an argument of New.
// Return: the option.
func WithEnv(kv ...string) Option {
	extra := slices.Clone(kv)
	return func(r *Runner) {
		r.env = append(r.env, extra...)
	}
}

// New locates the git binary and returns a Runner.
//
// The environment of the current process is captured once, here.
//
// Context: any.
// Return: the Runner, or an error wrapping ErrGitNotFound.
func New(opts ...Option) (*Runner, error) {
	return newRunner(exec.LookPath, opts...)
}

// newRunner implements New with a replaceable lookup function.
func newRunner(lookPath func(string) (string, error), opts ...Option) (*Runner, error) {
	bin, err := lookPath("git")
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrGitNotFound, err)
	}
	r := &Runner{bin: bin}
	for _, opt := range opts {
		opt(r)
	}
	r.env = buildEnv(os.Environ(), r.env)
	return r, nil
}

// IsLocalEnvVar reports whether an environment variable ties git to a
// particular repository, such as GIT_DIR or GIT_INDEX_FILE.
//
// The list covers the variables printed by "git rev-parse --local-env-vars"
// of git 2.39 and later, except GIT_CONFIG_PARAMETERS and GIT_CONFIG_COUNT,
// which carry command line configuration. The Runner removes these
// variables from the environment it inherits; a program that runs other
// commands in a repository, as "git submodule foreach" does, should remove
// them as well.
//
// Context: any.
// Return: true for a repository-local variable name (without "=value").
func IsLocalEnvVar(name string) bool {
	switch name {
	case "GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_CONFIG", "GIT_OBJECT_DIRECTORY",
		"GIT_DIR", "GIT_WORK_TREE", "GIT_IMPLICIT_WORK_TREE", "GIT_GRAFT_FILE",
		"GIT_INDEX_FILE", "GIT_NO_REPLACE_OBJECTS", "GIT_REPLACE_REF_BASE",
		"GIT_PREFIX", "GIT_INTERNAL_SUPER_PREFIX", "GIT_SHALLOW_FILE", "GIT_COMMON_DIR":
		return true
	}
	return false
}

// buildEnv returns base without repository-local variables and without the
// variables forced by the package, followed by the forced variables and
// extra.
func buildEnv(base, extra []string) []string {
	forced := []string{"LC_ALL=C", "GIT_OPTIONAL_LOCKS=0", noLazyFetch}
	env := make([]string, 0, len(base)+len(forced)+len(extra))
	for _, kv := range base {
		name, _, _ := strings.Cut(kv, "=")
		switch name {
		case "LC_ALL", "GIT_OPTIONAL_LOCKS", "GIT_NO_LAZY_FETCH":
			continue
		}
		if !IsLocalEnvVar(name) {
			env = append(env, kv)
		}
	}
	env = append(env, forced...)
	return append(env, extra...)
}

// Cmd describes one git invocation.
type Cmd struct {
	// Dir is the working directory; empty means the current directory.
	Dir string
	// Args are the git arguments without the binary.
	Args []string
	// Env holds KEY=VALUE pairs for this invocation only. They take
	// precedence over the environment of the Runner.
	Env []string
	// Stdin is the standard input; nil means the null device.
	Stdin io.Reader
	// Stdout receives the output when non-nil; it is captured otherwise.
	Stdout io.Writer
	// Stderr receives diagnostics when non-nil; they are captured otherwise.
	Stderr io.Writer
}

// Exec runs git as described by c.
//
// Lazy fetching is disabled unless c.Env enables it; other transports are
// available as the configuration and the environment permit. When the
// context ends, git and every process it started receive SIGTERM, so that
// they can remove their lock files, and git is killed if it does not exit in
// time.
//
// Context: any; the invocation is bound to ctx.
// Return: the captured standard output (empty when c.Stdout is set, and
// possibly partial on failure), and *Error when git could not be started or
// exited with a non-zero status.
func (r *Runner) Exec(ctx context.Context, c Cmd) (string, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, r.bin, c.Args...)
	cmd.Dir = c.Dir
	cmd.Env = r.env
	if len(c.Env) > 0 {
		// A new slice: r.env is shared by concurrent invocations.
		cmd.Env = slices.Concat(r.env, c.Env)
	}
	cmd.Stdin = c.Stdin
	cmd.Stdout = c.Stdout
	if cmd.Stdout == nil {
		cmd.Stdout = &stdout
	}
	cmd.Stderr = c.Stderr
	if cmd.Stderr == nil {
		cmd.Stderr = &stderr
	}
	cmd.Cancel = func() error {
		return SignalTree(cmd.Process, syscall.SIGTERM)
	}
	cmd.WaitDelay = waitDelay
	if err := cmd.Run(); err != nil {
		return stdout.String(), newError(slices.Clone(c.Args), c.Dir, err, ctx.Err(),
			stderr.String())
	}
	return stdout.String(), nil
}

// Run runs git with args in dir and captures its output.
//
// The environment is the one of Exec with an empty Cmd.Env.
//
// Context: any; the invocation is bound to ctx.
// Return: the raw standard output, and *Error on failure.
func (r *Runner) Run(ctx context.Context, dir string, args ...string) (string, error) {
	return r.Exec(ctx, Cmd{Dir: dir, Args: args})
}

// Bin returns the path of the git binary used by the Runner.
//
// Context: any.
// Return: the path found by exec.LookPath.
func (r *Runner) Bin() string {
	return r.bin
}

// trimNewline removes the line terminator of single-line git output.
func trimNewline(out string) string {
	return strings.TrimSuffix(out, "\n")
}

// splitLines splits newline-terminated output into lines.
func splitLines(out string) []string {
	out = trimNewline(out)
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

// splitNUL splits NUL-terminated output into records.
func splitNUL(out string) []string {
	out = strings.TrimSuffix(out, "\x00")
	if out == "" {
		return nil
	}
	return strings.Split(out, "\x00")
}
