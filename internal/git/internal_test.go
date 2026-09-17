// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestBuildEnvScrubsRepositoryVariables(t *testing.T) {
	t.Parallel()
	base := []string{
		"PATH=/usr/bin",
		"GIT_DIR=/elsewhere/.git",
		"GIT_WORK_TREE=/elsewhere",
		"GIT_INDEX_FILE=/elsewhere/.git/index",
		"GIT_OBJECT_DIRECTORY=/elsewhere/.git/objects",
		"GIT_ALTERNATE_OBJECT_DIRECTORIES=/x",
		"GIT_COMMON_DIR=/elsewhere/.git",
		"GIT_PREFIX=sub/",
		"GIT_INTERNAL_SUPER_PREFIX=outer/",
		"GIT_CONFIG=/elsewhere/config",
		"GIT_IMPLICIT_WORK_TREE=0",
		"GIT_GRAFT_FILE=/g",
		"GIT_NO_REPLACE_OBJECTS=1",
		"GIT_REPLACE_REF_BASE=refs/x/",
		"GIT_SHALLOW_FILE=/s",
		"GIT_CONFIG_PARAMETERS='core.x'='1'",
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=core.y",
		"GIT_CONFIG_VALUE_0=2",
		"GIT_TERMINAL_PROMPT=0",
		"LC_ALL=de_DE.UTF-8",
		"LANG=de_DE.UTF-8",
		"GIT_OPTIONAL_LOCKS=1",
		"GIT_DIR_NOT_LOCAL=kept",
	}
	got := buildEnv(base, []string{"HOME=/h", "LC_ALL=POSIX"})
	want := []string{
		"PATH=/usr/bin",
		"GIT_CONFIG_PARAMETERS='core.x'='1'",
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=core.y",
		"GIT_CONFIG_VALUE_0=2",
		"GIT_TERMINAL_PROMPT=0",
		"LANG=de_DE.UTF-8",
		"GIT_DIR_NOT_LOCAL=kept",
		"LC_ALL=C",
		"GIT_OPTIONAL_LOCKS=0",
		"HOME=/h",
		"LC_ALL=POSIX",
	}
	if !slices.Equal(got, want) {
		t.Errorf("buildEnv:\n got %q\nwant %q", got, want)
	}
}

func TestBuildEnvEmpty(t *testing.T) {
	t.Parallel()
	got := buildEnv(nil, nil)
	want := []string{"LC_ALL=C", "GIT_OPTIONAL_LOCKS=0"}
	if !slices.Equal(got, want) {
		t.Errorf("buildEnv(nil, nil) = %q, want %q", got, want)
	}
}

func TestNewRunnerGitNotFound(t *testing.T) {
	t.Parallel()
	lookPath := func(string) (string, error) {
		return "", &exec.Error{Name: "git", Err: exec.ErrNotFound}
	}
	r, err := newRunner(lookPath)
	if r != nil || !errors.Is(err, ErrGitNotFound) || !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("newRunner = %v, %v; want nil, ErrGitNotFound wrapping exec.ErrNotFound", r, err)
	}
}

func TestNewRunnerOptions(t *testing.T) {
	t.Parallel()
	lookPath := func(string) (string, error) { return "/opt/git", nil }
	extra := []string{"A=1"}
	r, err := newRunner(lookPath, WithEnv(extra...), WithEnv("B=2"))
	if err != nil {
		t.Fatal(err)
	}
	extra[0] = "A=changed"
	if r.Bin() != "/opt/git" {
		t.Errorf("Bin() = %q", r.Bin())
	}
	n := len(r.env)
	wantTail := []string{"LC_ALL=C", "GIT_OPTIONAL_LOCKS=0", "A=1", "B=2"}
	if n < 4 || !slices.Equal(r.env[n-4:], wantTail) {
		t.Errorf("env tail = %q, want %q", r.env[max(0, n-4):], wantTail)
	}
}

func TestErrorMessage(t *testing.T) {
	t.Parallel()
	exitErr := &exec.ExitError{}
	tests := []struct {
		name string
		err  *Error
		want string
	}{
		{
			name: "exit status with stderr",
			err: &Error{
				Args:     []string{"rev-parse", "--verify", "a b", ""},
				ExitCode: 128,
				Stderr:   "fatal: one\n\nfatal: two\nhint: three\nhint: four",
				Err:      exitErr,
			},
			want: `git rev-parse --verify "a b" "": exit status 128: ` +
				`fatal: one; fatal: two; hint: three`,
		},
		{
			name: "not started",
			err:  &Error{Args: []string{"status"}, ExitCode: -1, Err: exec.ErrNotFound},
			want: "git status: " + exec.ErrNotFound.Error(),
		},
		{
			name: "quoted control character",
			err:  &Error{Args: []string{"tag", "x\ty"}, ExitCode: 1},
			want: `git tag "x\ty": exit status 1`,
		},
		{
			name: "redacted credentials",
			err: &Error{
				Args:     []string{"submodule", "add", "--", "https://u:tok@h/x", "scp@h:x"},
				ExitCode: 128,
				Stderr:   "fatal: clone of '" + redacted + "@h/x' failed",
			},
			want: "git submodule add -- https://" + redacted + "@h/x scp@h:x: " +
				"exit status 128: fatal: clone of '" + redacted + "@h/x' failed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.err.Error(); got != tt.want {
				t.Errorf("Error() = %q, want %q", got, tt.want)
			}
			if !errors.Is(tt.err.Unwrap(), tt.err.Err) {
				t.Errorf("Unwrap() = %v, want %v", tt.err.Unwrap(), tt.err.Err)
			}
		})
	}
}

func TestNewErrorWrapsContext(t *testing.T) {
	t.Parallel()
	runErr := errors.New("signal: terminated")
	e := newError([]string{"fetch"}, "/d", runErr, context.Canceled, " fatal: x \n")
	if e.ExitCode != -1 || e.Stderr != "fatal: x" || e.Dir != "/d" {
		t.Errorf("newError = %+v", e)
	}
	if !errors.Is(e, context.Canceled) || !errors.Is(e, runErr) {
		t.Errorf("newError does not wrap both errors: %v", e.Err)
	}
}

func TestNewErrorContextErrorOnce(t *testing.T) {
	t.Parallel()
	e := newError([]string{"version"}, "", context.Canceled, context.Canceled, "")
	if !errors.Is(e, context.Canceled) || e.Err.Error() != context.Canceled.Error() {
		t.Errorf("Err = %v, want the context error once", e.Err)
	}
	if got, want := e.Error(), "git version: context canceled"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestNewErrorRedactsStderr(t *testing.T) {
	t.Parallel()
	e := newError([]string{"fetch"}, "", errors.New("exit status 128"), nil,
		"fatal: unable to access 'https://user:secret@example.org/x.git/'\n")
	want := "fatal: unable to access 'https://" + redacted + "@example.org/x.git/'"
	if e.Stderr != want {
		t.Errorf("Stderr = %q, want %q", e.Stderr, want)
	}
}

func TestRedactURLs(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"":                                  "",
		"plain text":                        "plain text",
		"https://example.org/x.git":         "https://example.org/x.git",
		"https://user@example.org/x.git":    "https://" + redacted + "@example.org/x.git",
		"https://u:p@ss@example.org":        "https://" + redacted + "@example.org",
		"ssh://git@host:22/x?a=b@c#d@e":     "ssh://" + redacted + "@host:22/x?a=b@c#d@e",
		"git@host:x.git":                    "git@host:x.git",
		"a https://t@h/x and http://h/@y z": "a https://" + redacted + "@h/x and http://h/@y z",
		"'file://u:p@h' \"http://t@h\"":     "'file://" + redacted + "@h' \"http://" + redacted + "@h\"",
		"x:// @y":                           "x:// @y",
		"trailing://":                       "trailing://",
	}
	for in, want := range tests {
		if got := redactURLs(in); got != want {
			t.Errorf("redactURLs(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMovedGitlink(t *testing.T) {
	t.Parallel()
	const tail = " 160000 160000 160000 0123 0123 lib"
	tests := map[string]bool{
		"1 .M SC.." + tail:                  true,
		"1 .M SC.U" + tail:                  true,
		"1 .M S..U" + tail:                  true,
		"1 .M S.M." + tail:                  false,
		"1 .M SCMU" + tail:                  false,
		"1 MM SC.." + tail:                  false,
		"1 M. SC.." + tail:                  false,
		"1 .D S..." + tail:                  false,
		"1 .M N..." + tail:                  false,
		"1 .T SC.." + tail:                  false,
		"2 .M SC.. 160000 160000 160000 x":  false,
		"u UU SC.. 160000 160000 160000 x":  false,
		"? untracked":                       false,
		"# branch.oid 0123":                 false,
		"1 .M SC..":                         false,
		"1 .M SC. 160000 160000 160000 x y": false,
		"":                                  false,
	}
	for line, want := range tests {
		if got := movedGitlink(line); got != want {
			t.Errorf("movedGitlink(%q) = %t, want %t", line, got, want)
		}
	}
}

func TestFatalWith(t *testing.T) {
	t.Parallel()
	msgs := []string{"not a git repository", "must be run in a work tree"}
	tests := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{errors.New("fatal: not a git repository"), false},
		{&Error{ExitCode: 128, Stderr: "fatal: not a git repository: /x"}, true},
		{&Error{ExitCode: 128, Stderr: "fatal: this operation must be run in a work tree"}, true},
		{&Error{ExitCode: 128, Stderr: "fatal: detected dubious ownership in repository"}, false},
		{&Error{ExitCode: 1, Stderr: "fatal: not a git repository: /x"}, false},
		{fmt.Errorf("wrapped: %w", &Error{ExitCode: 128, Stderr: "not a git repository"}), true},
	}
	for _, tt := range tests {
		if got := fatalWith(tt.err, msgs...); got != tt.want {
			t.Errorf("fatalWith(%v) = %t, want %t", tt.err, got, tt.want)
		}
	}
	if fatalWith(&Error{ExitCode: 128, Stderr: "fatal: x"}) {
		t.Errorf("fatalWith without messages = true")
	}
}

func TestReadStat(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tests := map[string]struct {
		content string
		state   byte
		ppid    int
		ok      bool
	}{
		"plain":      {"42 (git) S 7 42 42 0 -1", 'S', 7, true},
		"odd comm":   {"43 (a) b (c) T 8 43", 'T', 8, true},
		"no comm":    {"44 git S 9", 0, 0, false},
		"truncated":  {"45 (git) S", 0, 0, false},
		"not a pid":  {"46 (git) S x 1", 0, 0, false},
		"long state": {"47 (git) SS 1", 0, 0, false},
	}
	for name, tt := range tests {
		file := filepath.Join(dir, name)
		if err := os.WriteFile(file, []byte(tt.content), 0o644); err != nil {
			t.Fatal(err)
		}
		state, ppid, ok := readStat(file)
		if state != tt.state || ppid != tt.ppid || ok != tt.ok {
			t.Errorf("%s: readStat = %q, %d, %t; want %q, %d, %t", name, state, ppid, ok,
				tt.state, tt.ppid, tt.ok)
		}
	}
	if _, _, ok := readStat(filepath.Join(dir, "missing")); ok {
		t.Errorf("readStat(missing) = ok")
	}
}

func TestBlockedSignals(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tests := map[string]struct {
		content string
		mask    uint64
		ok      bool
	}{
		"blocked":   {"Name:\tgit\nSigPnd:\t0000000000000000\nSigBlk:\t0000000000004000\n", 0x4000, true},
		"none":      {"SigBlk:\t0000000000000000\n", 0, true},
		"last line": {"Name:\tsh\nSigBlk:\tfffffffe7ffbfeff", 0xfffffffe7ffbfeff, true},
		"missing":   {"Name:\tsh\nShdPnd:\t0000000000004000\n", 0, false},
		"invalid":   {"SigBlk:\tnot hex\n", 0, false},
	}
	for name, tt := range tests {
		file := filepath.Join(dir, name)
		if err := os.WriteFile(file, []byte(tt.content), 0o644); err != nil {
			t.Fatal(err)
		}
		if mask, ok := blockedSignals(file); mask != tt.mask || ok != tt.ok {
			t.Errorf("%s: blockedSignals = %#x, %t; want %#x, %t", name, mask, ok, tt.mask, tt.ok)
		}
	}
	if _, ok := blockedSignals(filepath.Join(dir, "absent")); ok {
		t.Errorf("blockedSignals(absent) = ok")
	}
}

func TestRunningState(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		state                     byte
		interruptible, uninterrup bool
	}{
		{'R', true, true},
		{'S', true, true},
		{'D', false, true},
		{'T', false, false},
		{'t', false, false},
		{'Z', false, false},
		{'X', false, false},
		{'I', false, false},
		{0, false, false},
	} {
		if got := runningState(tt.state, false); got != tt.interruptible {
			t.Errorf("runningState(%q, false) = %t", tt.state, got)
		}
		if got := runningState(tt.state, true); got != tt.uninterrup {
			t.Errorf("runningState(%q, true) = %t", tt.state, got)
		}
	}
}

func TestProcessState(t *testing.T) {
	t.Parallel()
	if _, err := os.Stat(filepath.Join(procDir, "self", "status")); err != nil {
		t.Skipf("no proc file system: %v", err)
	}
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pid := cmd.Process.Pid
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	if blocks(pid, syscall.SIGTERM) || blocks(pid, 0) || blocks(os.Getpid(), syscall.SIGKILL) {
		t.Errorf("a signal is blocked")
	}
	deadline := time.Now().Add(time.Minute)
	for state(pid) != 'S' && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if s := state(pid); s != 'S' || !running(pid, true) {
		t.Errorf("sleeping process: state %q, running %t", s, running(pid, true))
	}
	if err := cmd.Process.Signal(syscall.SIGSTOP); err != nil {
		t.Fatal(err)
	}
	waitStopped([]int{pid}, deadline)
	if s := state(pid); s != 'T' || running(pid, true) {
		t.Errorf("stopped process: state %q, running %t", s, running(pid, true))
	}
	if err := cmd.Process.Signal(syscall.SIGCONT); err != nil {
		t.Fatal(err)
	}
	// The deadline ends the wait for a process that does not stop.
	start := time.Now()
	waitStopped([]int{pid}, start.Add(20*time.Millisecond))
	if elapsed := time.Since(start); elapsed < 20*time.Millisecond || elapsed > 10*time.Second {
		t.Errorf("waitStopped(running) returned after %v", elapsed)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err == nil {
		t.Fatal("sleep was not killed")
	}
	if s := state(pid); s != 0 || running(pid, true) {
		t.Errorf("waited process: state %q, running %t", s, running(pid, true))
	}
}

func TestDescendants(t *testing.T) {
	t.Parallel()
	if _, err := os.Stat(filepath.Join(procDir, "self", "stat")); err != nil {
		t.Skipf("no proc file system: %v", err)
	}
	// sh starts sleep in the background and waits: sh -> sleep. The context
	// must outlive the test: when it ends first, exec kills sh alone, and
	// the cleanup no longer finds the orphaned sleep.
	ctx := context.WithoutCancel(t.Context())
	cmd := exec.CommandContext(ctx, "sh", "-c", "sleep 30 & wait")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = SignalTree(cmd.Process, syscall.SIGKILL)
		_ = cmd.Wait()
	})
	var found []int
	for range 100 {
		if found = descendants(cmd.Process.Pid); len(found) > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(found) != 1 {
		t.Fatalf("descendants = %v, want the sleep process", found)
	}
	if got := descendants(found[0]); len(got) != 0 {
		t.Errorf("descendants(sleep) = %v, want none", got)
	}
	if got := descendants(-1); len(got) != 0 {
		t.Errorf("descendants(-1) = %v, want none", got)
	}
}

func TestExitCode(t *testing.T) {
	t.Parallel()
	if got := exitCode(nil); got != -1 {
		t.Errorf("exitCode(nil) = %d", got)
	}
	if got := exitCode(errors.New("x")); got != -1 {
		t.Errorf("exitCode(plain) = %d", got)
	}
	wrapped := errors.Join(errors.New("ctx"), &Error{ExitCode: 5})
	if got := exitCode(wrapped); got != 5 {
		t.Errorf("exitCode(wrapped) = %d", got)
	}
}

func TestParseConfigList(t *testing.T) {
	t.Parallel()
	out := "submodule.a.b.path\na.b\x00submodule.a.b.flag\x00" +
		"submodule.K.url\nhttps://x/y\nz\x00x.empty\n\x00"
	got := parseConfigList(out)
	want := []ConfigEntry{
		{Key: "submodule.a.b.path", Value: "a.b"},
		{Key: "submodule.a.b.flag", Value: ""},
		{Key: "submodule.K.url", Value: "https://x/y\nz"},
		{Key: "x.empty", Value: ""},
	}
	if !slices.Equal(got, want) {
		t.Errorf("parseConfigList = %q, want %q", got, want)
	}
	if got := parseConfigList(""); got != nil {
		t.Errorf("parseConfigList(\"\") = %q, want nil", got)
	}
}

func TestInSection(t *testing.T) {
	t.Parallel()
	tests := []struct {
		key, section string
		want         bool
	}{
		{"submodule.kernel.path", "submodule.kernel", true},
		{"submodule.kernel.path", "SubModule.kernel", true},
		{"submodule.kernel.path", "submodule.Kernel", false},
		{"submodule.a.b.path", "submodule.a.b", true},
		{"submodule.a.b.path", "submodule.a", false},
		{"core.bare", "core", true},
		{"core.x.bare", "core", false},
		{"core.bare", "core.bare", false},
		{"nodot", "nodot", false},
	}
	for _, tt := range tests {
		if got := inSection(tt.key, tt.section); got != tt.want {
			t.Errorf("inSection(%q, %q) = %v, want %v", tt.key, tt.section, got, tt.want)
		}
	}
}

func TestSplitHelpers(t *testing.T) {
	t.Parallel()
	if got := splitLines("a\nb c\n"); !slices.Equal(got, []string{"a", "b c"}) {
		t.Errorf("splitLines = %q", got)
	}
	if got := splitLines("\n"); got != nil {
		t.Errorf("splitLines(newline) = %q", got)
	}
	if got := splitNUL("a b\x00c\nd\x00"); !slices.Equal(got, []string{"a b", "c\nd"}) {
		t.Errorf("splitNUL = %q", got)
	}
	if got := firstLines("  \n", 3); got != "" {
		t.Errorf("firstLines(blank) = %q", got)
	}
	if got := cleanPath("a/b/"); got != "a/b" {
		t.Errorf("cleanPath = %q", got)
	}
	if !strings.HasPrefix(quoteArg("\x01"), `"`) {
		t.Errorf("quoteArg does not quote control characters")
	}
}
