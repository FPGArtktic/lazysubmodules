// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package main

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/FPGArtktic/lazysubmodules/internal/git/gittest"
)

func TestTUIWithoutTerminal(t *testing.T) {
	t.Parallel()
	const msg = "lazysubmodules: tui requires a terminal\n"
	outside(t).want(t, []string{"tui"}, exitUsage, "", msg)

	tests := []struct {
		environ        []string
		stdin, stdout  bool
		want           string
		reachesProgram bool
	}{
		{[]string{"TERM=xterm"}, false, true, msg, false},
		{[]string{"TERM=xterm"}, true, false, msg, false},
		{[]string{"TERM=dumb"}, false, false, msg, false},
		{[]string{"TERM=dumb"}, true, true,
			"lazysubmodules: tui requires a terminal with cursor movement (TERM is dumb)\n", false},
		{nil, true, true, "lazysubmodules: tui requires a terminal (TERM is not set)\n", false},
		{[]string{"TERM="}, true, true,
			"lazysubmodules: tui requires a terminal (TERM is not set)\n", false},
		{[]string{"TERM=xterm", "NO_COLOR=1"}, true, true, "", true},
	}
	for _, tt := range tests {
		inv := outside(t)
		stdin := strings.NewReader("")
		var stdout, stderr bytes.Buffer
		e := &env{args: []string{"tui"}, dir: inv.dir, gitEnv: inv.gitEnv, environ: tt.environ,
			stdin: stdin, stdout: &stdout, stderr: &stderr,
			terminal: func(stream any) bool {
				return stream == stdin && tt.stdin || stream == &stdout && tt.stdout
			},
		}
		code := run(t.Context(), e)
		if tt.reachesProgram {
			// Outside of a repository, opening it fails after the checks.
			if code != exitGit || !strings.Contains(stderr.String(), "not a git repository") {
				t.Errorf("%+v: exit status %d, stderr %q", tt, code, stderr.String())
			}
			continue
		}
		if code != exitUsage || stdout.String() != "" || stderr.String() != tt.want {
			t.Errorf("%+v: exit status %d, stdout %q, stderr %q", tt, code, stdout.String(),
				stderr.String())
		}
	}
}

// syncBuffer is a bytes.Buffer safe for concurrent use.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

// Write implements io.Writer.
func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

// TestTUIQuit starts the interface in a repository, with keys from a pipe,
// and quits it with "q".
func TestTUIQuit(t *testing.T) {
	t.Parallel()
	s := gittest.NewSuper(t, gittest.SHA1)
	in, keys := io.Pipe()
	t.Cleanup(func() { _ = keys.Close() })
	go func() {
		// Every key waits for the interface to read it; the pipe is closed
		// when the test ends.
		for {
			if _, err := keys.Write([]byte("q")); err != nil {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
	}()
	var stdout syncBuffer
	var stderr bytes.Buffer
	e := &env{args: []string{"tui"}, dir: s.Dir, gitEnv: gittest.Env(t),
		environ: testEnviron(), stdin: in, stdout: &stdout, stderr: &stderr,
		terminal: func(any) bool { return true },
	}
	done := make(chan int, 1)
	go func() { done <- run(t.Context(), e) }()
	select {
	case code := <-done:
		if code != exitOK || stderr.String() != "" {
			t.Errorf("exit status %d, stderr %q", code, stderr.String())
		}
		stdout.mu.Lock()
		defer stdout.mu.Unlock()
		// The program switched to the alternate screen and back.
		out := stdout.buf.String()
		if !strings.Contains(out, "\x1b[?1049h") || !strings.Contains(out, "\x1b[?1049l") {
			t.Errorf("output %q", out)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("the interface did not quit")
	}
}

// TestBinaryTUI checks the interface of the built command on a complex
// superproject, on a terminal and without one.
func TestBinaryTUI(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("builds the command")
	}
	bin := buildBinary(t)
	c := gittest.NewComplexSuper(t, gittest.SHA1)
	b := newBinary(t, bin, c)
	b.want([]string{"tui"}, exitUsage, "", "lazysubmodules: tui requires a terminal\n")
	b.tuiPrompts()
	b.tuiColors()
	b.tuiSignals()
	b.tuiIgnoredHangup()
	b.tuiInterruptedUpdate(c)
}

// tuiOnTerminal runs the interface on a pseudo-terminal, typing keys once
// the table is shown until it ends, and returns the exit status and the
// output with the line breaks of the terminal turned back into "\n".
func (b *binary) tuiOnTerminal(extra []string, keys string) (int, string) {
	b.t.Helper()
	p := startOnPTY(b.t, b.command(extra, "tui"))
	// The table appears once the status is loaded.
	p.waitOutput("Submodules")
	p.waitOutput("quirky")
	code := p.wait(keys)
	return code, terminalText(p.Output())
}

// tuiColors checks the colors of the interface and its refusal of a
// terminal without cursor movement.
func (b *binary) tuiColors() {
	t := b.t
	code, out := b.tuiOnTerminal(nil, "q")
	if code != exitOK || !strings.Contains(out, "\x1b[?1049h") || !hasColor(out) {
		t.Errorf("tui: exit status %d, output\n%q", code, out)
	}
	// Any value of NO_COLOR disables colors, not only those that the
	// terminal libraries recognize.
	code, out = b.tuiOnTerminal([]string{"NO_COLOR=yes"}, "q")
	if code != exitOK || !strings.Contains(out, "\x1b[?1049h") || hasColor(out) {
		t.Errorf("tui with NO_COLOR: exit status %d, output\n%q", code, out)
	}
	code, out = b.onTerminal([]string{"TERM=dumb"}, "tui")
	if code != exitUsage || out !=
		"lazysubmodules: tui requires a terminal with cursor movement (TERM is dumb)\n" {
		t.Errorf("tui with TERM=dumb: exit status %d, output\n%q", code, out)
	}
}

// tuiSignals sends SIGTERM, SIGINT and SIGHUP to the interface, while the
// status loads and once the table is shown: it ends at once, restores the
// terminal and names the signal. Bubble Tea handles no signals itself, or
// it could block its own shutdown for good; each case runs twice since that
// depended on timing.
func (b *binary) tuiSignals() {
	t := b.t
	signals := map[syscall.Signal]string{syscall.SIGTERM: "terminated",
		syscall.SIGINT: "interrupt", syscall.SIGHUP: "hangup"}
	for sig, name := range signals {
		for i := range 4 {
			p := startOnPTY(t, b.command(nil, "tui"))
			p.waitOutput("\x1b[?1049h")
			if i%2 == 1 {
				p.waitOutput("quirky")
			}
			if err := p.cmd.Process.Signal(sig); err != nil {
				t.Fatal(err)
			}
			code, ok := p.waitWithin(10*time.Second, "")
			out := p.Output()
			_, after, restored := strings.Cut(out, "\x1b[?1049l")
			want := "lazysubmodules: terminal interface: " + name + " signal received\r\n"
			if !ok || code != exitError || !restored || !strings.HasSuffix(after, want) {
				t.Errorf("%s (run %d): ended %t, exit status %d, output\n%q", name, i, ok,
					code, out)
			}
		}
	}
}

// tuiIgnoredHangup starts the interface with SIGHUP ignored, as nohup
// does: a hangup does not end it, and q still does.
func (b *binary) tuiIgnoredHangup() {
	t := b.t
	cmd := b.command(nil, "tui")
	cmd.Args = []string{"sh", "-c", `trap '' HUP; exec "$0" tui`, cmd.Path}
	cmd.Path = "/bin/sh"
	p := startOnPTY(t, cmd)
	p.waitOutput("quirky")
	if err := p.cmd.Process.Signal(syscall.SIGHUP); err != nil {
		t.Fatal(err)
	}
	if code, ended := p.waitWithin(time.Second, ""); ended {
		t.Fatalf("an ignored hangup ended the interface: exit status %d, output\n%q", code,
			p.Output())
	}
	if code := p.wait("q"); code != exitOK {
		t.Errorf("q after an ignored hangup: exit status %d, output\n%q", code, p.Output())
	}
}

// tuiInterruptedUpdate interrupts an update of kernel with ctrl+c while its
// post-checkout hook runs: the submodule is put back, and the error of the
// update is printed once the screen is closed.
func (b *binary) tuiInterruptedUpdate(c *gittest.ComplexSuper) {
	t := b.t
	marker := filepath.Join(t.TempDir(), "hook-started")
	target := c.Kernel.Tags["v6.6.10"]
	hooks := filepath.Join(c.Kernel.GitDir, "hooks")
	script := "#!/bin/sh\nif test \"$2\" = " + target + "; then\n\t: >'" + marker +
		"'\n\texec sleep 30\nfi\n"
	if err := os.MkdirAll(hooks, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hooks, "post-checkout"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	p := startOnPTY(t, b.command(nil, "tui"))
	p.waitOutput("quirky")
	// u opens the question, y answers it once it is shown.
	deadline := time.Now().Add(20 * time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the update did not start; output:\n%q", p.Output())
		}
		if _, err := p.pty.WriteString("uy"); err != nil {
			t.Fatal(err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if _, err := p.pty.WriteString("\x03"); err != nil {
		t.Fatal(err)
	}
	code, ok := p.waitWithin(20*time.Second, "")
	_, after, _ := strings.Cut(p.Output(), "\x1b[?1049l")
	if !ok || code != exitGit || !strings.Contains(after,
		"lazysubmodules: updating kernel: kernel: git -c advice.detachedHead=false checkout ") ||
		!strings.Contains(after, "context canceled") || strings.Contains(after, "roll back") {
		t.Errorf("interrupted update: ended %t, exit status %d, output after the screen\n%q",
			ok, code, after)
	}
	if head := c.Git(t, c.Kernel.In(c.Dir), "rev-parse", "HEAD"); head != c.Kernel.Head {
		t.Errorf("kernel was not put back: HEAD %s, want %s", head, c.Kernel.Head)
	}
	if staged := c.Git(t, c.Dir, "diff", "--cached", "--name-only"); staged != "" {
		t.Errorf("staged after the interrupted update: %q", staged)
	}
}

// hasColor reports whether text contains an SGR sequence that sets a
// foreground or background color.
func hasColor(text string) bool {
	re := regexp.MustCompile("\x1b\\[([0-9;:]*)m")
	for _, m := range re.FindAllStringSubmatch(text, -1) {
		for p := range strings.FieldsFuncSeq(m[1], func(c rune) bool {
			return c == ';' || c == ':'
		}) {
			n, err := strconv.Atoi(p)
			if err == nil && (n >= 30 && n <= 38 || n >= 40 && n <= 48 ||
				n >= 90 && n <= 97 || n >= 100 && n <= 107) {
				return true
			}
		}
	}
	return false
}

func TestHasColor(t *testing.T) {
	t.Parallel()
	for text, want := range map[string]bool{
		"plain":                    false,
		"\x1b[1mbold\x1b[0m":       false,
		"\x1b[7;1;39;49m\x1b[m":    false,
		"\x1b[>4;2m\x1b[?1049h":    false,
		"\x1b[36;1mtitle":          true,
		"\x1b[1;90mdim":            true,
		"\x1b[38;5;196mred":        true,
		"\x1b[48:2::1:2:3mbg":      true,
		"\x1b[0;1;4;102mhighlight": true,
	} {
		if got := hasColor(text); got != want {
			t.Errorf("hasColor(%q) = %v", text, got)
		}
	}
}

func TestTUIUsageErrors(t *testing.T) {
	t.Parallel()
	outside(t).want(t, []string{"tui", "kernel"}, exitUsage, "",
		"lazysubmodules: tui: unexpected argument \"kernel\"\n"+usageHint)
	outside(t).want(t, []string{"tui", "--porcelain"}, exitUsage, "",
		"lazysubmodules: tui: flag provided but not defined: -porcelain\n"+usageHint)
}

// tuiPrompts checks that git runs without terminal prompts in the
// interface, even when the environment allows them, and without a
// controlling terminal, which programs such as ssh would open to prompt: a
// git wrapper records GIT_TERMINAL_PROMPT of every invocation and whether
// it can open the terminal.
func (b *binary) tuiPrompts() {
	t := b.t
	realGit, err := exec.LookPath("git")
	if err != nil || strings.ContainsAny(realGit, "'\n") {
		t.Fatalf("git at %q: %v", realGit, err)
	}
	dir := t.TempDir()
	// A failed redirection ends the shell running a special built-in such as
	// ":", hence the subshell.
	script := "#!/bin/sh\ntty=no\nif (: </dev/tty) 2>/dev/null; then tty=yes; fi\n" +
		"printf '%s %s\\n' \"${GIT_TERMINAL_PROMPT-unset}\" \"$tty\" >>\"$LSM_PROMPT_LOG\"\n" +
		"exec '" + realGit + "' \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	logs := map[string]string{}
	for _, args := range [][]string{{"status"}, {"tui"}} {
		log := filepath.Join(dir, args[0]+".log")
		extra := []string{"PATH=" + dir + ":" + os.Getenv("PATH"), "GIT_TERMINAL_PROMPT=1",
			"LSM_PROMPT_LOG=" + log}
		var code int
		var out string
		if args[0] == "tui" {
			code, out = b.tuiOnTerminal(extra, "q")
		} else {
			code, out = b.onTerminal(extra, args...)
		}
		if code != exitOK {
			t.Fatalf("%q: exit status %d, output\n%q", args, code, out)
		}
		data, err := os.ReadFile(log)
		if err != nil {
			t.Fatal(err)
		}
		logs[args[0]] = string(data)
	}
	for cmd, value := range map[string]string{"status": "1 yes", "tui": "0 no"} {
		lines := strings.Split(strings.TrimSuffix(logs[cmd], "\n"), "\n")
		if len(lines) < 2 || slices.ContainsFunc(lines, func(l string) bool { return l != value }) {
			t.Errorf("%s: GIT_TERMINAL_PROMPT of the git invocations:\n%s", cmd, logs[cmd])
		}
	}
}
