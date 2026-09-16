// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

// Package gittest creates isolated git repositories for tests.
//
// Every git invocation runs without the user and system configuration, with a
// fixed identity and fixed dates, so that commit SHAs are stable for a given
// object format and sequence of operations. Remotes are local bare
// repositories; nothing uses the network. The environment is passed to git
// explicitly, never through the process environment, so the helpers are safe
// in parallel tests.
//
// The package must only be imported from _test.go files.
package gittest

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/FPGArtktic/lazysubmodules/internal/git"
)

// Identity and date used for every commit and tag.
const (
	Name  = "LazySubmodules Test"
	Email = "test@example.org"
	Date  = "@1767225600 +0000" // 2026-01-01T00:00:00Z
)

// Object formats accepted by the constructors.
const (
	SHA1   = "sha1"
	SHA256 = "sha256"
)

// Env returns the environment that isolates git from the host.
//
// The global and system configuration are disabled, HOME and
// XDG_CONFIG_HOME point to a new temporary directory, author and committer
// are fixed, terminal prompts, editors and trace output are disabled, and the
// configuration protocol.file.allow=always, init.defaultBranch=main,
// advice.detachedHead=false, gc.auto=0 and maintenance.auto=false is set
// through GIT_CONFIG_COUNT, followed by the optional config entries
// ("key=value"). Disabling automatic maintenance keeps background processes
// away from temporary directories that are about to be removed.
//
// GIT_ALLOW_PROTOCOL=file permits only local repositories, whatever the host
// environment says, so no test can reach the network by accident. A test that
// needs another transport (served locally) appends its own
// GIT_ALLOW_PROTOCOL; the last value of a variable wins.
//
// Context: test helper; fails the test on a malformed config entry.
// Return: KEY=VALUE pairs for git.WithEnv.
func Env(t testing.TB, config ...string) []string {
	t.Helper()
	home := t.TempDir()
	env := []string{
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_PARAMETERS=",
		"GIT_ATTR_NOSYSTEM=1",
		"HOME=" + home,
		"XDG_CONFIG_HOME=" + filepath.Join(home, ".config"),
		"GIT_AUTHOR_NAME=" + Name,
		"GIT_AUTHOR_EMAIL=" + Email,
		"GIT_AUTHOR_DATE=" + Date,
		"GIT_COMMITTER_NAME=" + Name,
		"GIT_COMMITTER_EMAIL=" + Email,
		"GIT_COMMITTER_DATE=" + Date,
		"GIT_TERMINAL_PROMPT=0",
		"GIT_EDITOR=:",
		"SSH_AUTH_SOCK=",
		"GIT_ALLOW_PROTOCOL=file",
		"GIT_TRACE=0",
		"GIT_TRACE_PACKET=0",
		"GIT_TRACE_PERFORMANCE=0",
		"GIT_TRACE_SETUP=0",
		"GIT_TRACE2=0",
		"GIT_TRACE2_EVENT=0",
		"GIT_TRACE2_PERF=0",
	}
	entries := append([]string{
		"protocol.file.allow=always",
		"init.defaultBranch=main",
		"advice.detachedHead=false",
		"gc.auto=0",
		"maintenance.auto=false",
	}, config...)
	env = append(env, "GIT_CONFIG_COUNT="+strconv.Itoa(len(entries)))
	for i, entry := range entries {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			t.Fatalf("gittest: config entry %q is not key=value", entry)
		}
		env = append(env,
			"GIT_CONFIG_KEY_"+strconv.Itoa(i)+"="+key,
			"GIT_CONFIG_VALUE_"+strconv.Itoa(i)+"="+value)
	}
	return env
}

// Runner returns a git.Runner using Env.
//
// Context: test helper; fails the test when git is missing.
// Return: the runner.
func Runner(t testing.TB, config ...string) *git.Runner {
	t.Helper()
	r, err := git.New(git.WithEnv(Env(t, config...)...))
	if err != nil {
		t.Fatalf("gittest: %v", err)
	}
	return r
}

// Git runs git with Env in dir.
//
// Context: test helper; fails the test when git fails.
// Return: the standard output without surrounding white space.
func Git(t testing.TB, dir string, args ...string) string {
	t.Helper()
	out, err := Runner(t).Run(t.Context(), dir, args...)
	if err != nil {
		if gitErr, ok := errors.AsType[*git.Error](err); ok {
			t.Fatalf("gittest: %v\n%s", err, gitErr.Stderr)
		}
		t.Fatalf("gittest: %v", err)
	}
	return strings.TrimSpace(out)
}

// ConfigArgs turns config entries ("key=value") into "-c" arguments.
//
// Context: any.
// Return: arguments to put before the git subcommand.
func ConfigArgs(config ...string) []string {
	args := make([]string, 0, 2*len(config))
	for _, entry := range config {
		args = append(args, "-c", entry)
	}
	return args
}

// WriteFile writes content to path, creating parent directories.
//
// Context: test helper.
// Return: nothing; fails the test on error.
func WriteFile(t testing.TB, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("gittest: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("gittest: %v", err)
	}
}

// InitRepo creates an empty repository (unborn branch main).
//
// Context: test helper; format is SHA1 or SHA256.
// Return: the working tree directory.
func InitRepo(t testing.TB, format string) string {
	t.Helper()
	dir := t.TempDir()
	Git(t, dir, "init", "--quiet", "--object-format="+format, dir)
	return dir
}

// commitAll stages all changes in dir and commits them.
func commitAll(t testing.TB, dir, msg string) string {
	t.Helper()
	Git(t, dir, "add", "--all")
	Git(t, dir, "commit", "--quiet", "-m", msg)
	return Git(t, dir, "rev-parse", "HEAD")
}

// SSHSigningKey creates an ed25519 SSH key for signing tests.
//
// Next to the private key, the public key is written to keyPath+".pub" and an
// allowed signers file trusting it for Email to keyPath+".allowed_signers".
//
// Context: test helper; fails the test when ssh-keygen fails.
// Return: the private key path, and ok=false when ssh-keygen is not
// installed (the caller should skip).
func SSHSigningKey(t testing.TB) (keyPath string, ok bool) {
	t.Helper()
	return sshSigningKey(t, t.TempDir())
}

// SSHSigningConfig returns config entries that make git sign with the key
// from SSHSigningKey and trust only that key when verifying.
//
// Context: any.
// Return: entries for Env, Runner or ConfigArgs.
func SSHSigningConfig(keyPath string) []string {
	return []string{
		"gpg.format=ssh",
		"user.signingKey=" + keyPath,
		"gpg.ssh.allowedSignersFile=" + keyPath + ".allowed_signers",
	}
}
