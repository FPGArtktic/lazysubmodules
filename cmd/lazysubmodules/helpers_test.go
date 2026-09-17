// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/FPGArtktic/lazysubmodules/internal/git/gittest"
)

// TestMain registers the -update flag, which rewrites the golden files
// under testdata/, without a package variable.
func TestMain(m *testing.M) {
	flag.Bool("update", false, "rewrite the golden files under testdata/")
	flag.Parse()
	os.Exit(m.Run())
}

// updateGolden reports whether -update was given.
func updateGolden() bool {
	f := flag.Lookup("update")
	return f != nil && f.Value.String() == "true"
}

// wantGolden compares got with testdata/<name>.golden, or rewrites the
// file with -update.
func wantGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if updateGolden() {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("output differs from %s (rewrite with -update):\n%s", path,
			lineDiff(string(want), got))
	}
}

// lineDiff lists the lines that differ between want and got.
func lineDiff(want, got string) string {
	w, g := strings.Split(want, "\n"), strings.Split(got, "\n")
	var b strings.Builder
	for i := range max(len(w), len(g)) {
		var wl, gl string
		if i < len(w) {
			wl = w[i]
		}
		if i < len(g) {
			gl = g[i]
		}
		if wl != gl {
			fmt.Fprintf(&b, "line %d:\n  want %q\n  got  %q\n", i+1, wl, gl)
		}
	}
	return b.String()
}

// testEnviron is the process environment seen by the commands under test:
// a terminal type with colors and nothing that disables them.
func testEnviron() []string {
	return []string{"TERM=xterm-256color"}
}

// result is the outcome of one command line.
type result struct {
	code   int
	stdout string
	stderr string
}

// String formats the result for failure messages.
func (r result) String() string {
	return fmt.Sprintf("exit status %d\nstdout:\n%s\nstderr:\n%s", r.code, r.stdout, r.stderr)
}

// invocation describes how run is called.
type invocation struct {
	// dir is the working directory; it must be a test repository or a
	// directory outside every repository.
	dir string
	// gitEnv isolates git from the host.
	gitEnv []string
	// environ replaces testEnviron when non-nil.
	environ []string
	// stdin is the standard input; nil means an empty one.
	stdin io.Reader
	// terminal makes every stream look like a terminal.
	terminal bool
}

// outside returns an invocation in an empty directory outside every
// repository, so that a command which gets past its argument checks cannot
// reach the repository of the test itself.
func outside(t *testing.T) invocation {
	t.Helper()
	dir := t.TempDir()
	env := append(gittest.Env(t), "GIT_CEILING_DIRECTORIES="+filepath.Dir(dir))
	return invocation{dir: dir, gitEnv: env}
}

// run runs a command line in-process.
func (inv invocation) run(t *testing.T, args ...string) result {
	t.Helper()
	var stdout, stderr bytes.Buffer
	e := &env{
		args:    args,
		dir:     inv.dir,
		environ: inv.environ,
		gitEnv:  inv.gitEnv,
		stdin:   inv.stdin,
		stdout:  &stdout,
		stderr:  &stderr,
	}
	if e.environ == nil {
		e.environ = testEnviron()
	}
	if e.stdin == nil {
		e.stdin = strings.NewReader("")
	}
	if inv.terminal {
		e.terminal = func(any) bool { return true }
	}
	code := run(t.Context(), e)
	return result{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

// want checks the exit status and the complete output of a command line.
func (inv invocation) want(t *testing.T, args []string, code int, stdout, stderr string) {
	t.Helper()
	got := inv.run(t, args...)
	if got.code != code || got.stdout != stdout || got.stderr != stderr {
		t.Errorf("%q:\n%v\nwant exit status %d\nstdout:\n%s\nstderr:\n%s", args, got, code,
			stdout, stderr)
	}
}

// wantCode checks the exit status of a command line and returns the result.
func (inv invocation) wantCode(t *testing.T, code int, args ...string) result {
	t.Helper()
	got := inv.run(t, args...)
	if got.code != code {
		t.Errorf("%q:\n%v\nwant exit status %d", args, got, code)
	}
	return got
}

// wantPrintable fails when text contains a character that is not
// printable, other than line breaks and tabs.
func wantPrintable(t *testing.T, what, text string) {
	t.Helper()
	for i := 0; i < len(text); {
		c, size := utf8.DecodeRuneInString(text[i:])
		// A byte that is not UTF-8 decodes to the printable U+FFFD, but a
		// terminal may take it for a control character, such as 0x9b for
		// CSI.
		if c == utf8.RuneError && size == 1 || c != '\n' && c != '\t' && !unicode.IsPrint(c) {
			t.Errorf("%s: character %U (byte %#x) at byte %d:\n%q", what, c, text[i], i, text)
			return
		}
		i += size
	}
}

// usageHint is the second line of every usage error.
const usageHint = "Run 'lazysubmodules help' for usage.\n"
