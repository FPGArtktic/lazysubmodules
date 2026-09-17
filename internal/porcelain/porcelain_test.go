// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package porcelain_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/FPGArtktic/lazysubmodules/internal/core"
	"github.com/FPGArtktic/lazysubmodules/internal/lock"
	"github.com/FPGArtktic/lazysubmodules/internal/manifest"
	"github.com/FPGArtktic/lazysubmodules/internal/porcelain"
)

// Commit names used by the synthetic statuses.
const (
	commitA   = "a1b2c3d4e5f6a7b8c9d0a1b2c3d4e5f6a7b8c9d0"
	commitB   = "d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3"
	commitC   = "0718ab2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a"
	commit256 = "5a6b7c8d9e0f1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6b"
)

// status returns a Status of a managed submodule. A lock entry is added
// when lockRef or lockCommit is not empty; its mode is the configured one.
func status(name, path string, mode manifest.Mode, ref, lockRef, lockCommit, head string,
	state core.State,
) core.Status {
	st := core.Status{
		Submodule: manifest.Submodule{Name: name, Path: path, Mode: mode, Ref: ref},
		Head:      head,
		State:     state,
	}
	if lockRef != "" || lockCommit != "" {
		st.Lock = &lock.Entry{Name: name, Mode: mode, Ref: lockRef, Commit: lockCommit}
	}
	return st
}

// stateStatuses returns one status for every state, as a superproject
// could report them.
func stateStatuses() []core.Status {
	legacy := status("legacy", "vendor/legacy", "", "", "", "", commitC, core.StateUnmanaged)
	return []core.Status{
		status("kernel", "kernel", manifest.ModeTagPattern, "v6.6.*", "v6.6.9", commitA, commitA,
			core.StateOK),
		status("u-boot", "bootloader/u-boot", manifest.ModeBranch, "main", "main", commitB,
			commitB, core.StateBehind),
		status("fpga.core", "ip/fpga-core", manifest.ModeTag, "v2.3.1", "v2.3.1", commitC,
			commitA, core.StateDrift),
		status("app", "apps/app", manifest.ModeTag, "v1.2.0", "v1.2.0", commitB, commitB,
			core.StateDirty),
		status("theme", "docs/theme", manifest.ModeTag, "v1.0.0", "v1.0.0", commitA, "",
			core.StateUninitialized),
		status("broken", "libs/broken", manifest.ModeTag, "v9.9.9", "v1.0.0", commitC, commitC,
			core.StateMissingRef),
		legacy,
		status("crypto lib", "libs/crypto lib", manifest.ModeCommit, commitB, commitB, commitB,
			commitB, core.StateOK),
		status(`we"ird\name`, "odd\tpath", manifest.ModeTag, "v1.0.0\x1b]0;pwned\a",
			"v1.0.0\nv1.0.1", commit256, commit256, core.StateOK),
	}
}

// TestGoldenStates writes a status of every state and a line that needs
// quoting, and compares the result with testdata/states.golden.
func TestGoldenStates(t *testing.T) {
	t.Parallel()
	st := stateStatuses()
	var b bytes.Buffer
	if err := porcelain.WriteStatusV1(&b, st); err != nil {
		t.Fatal(err)
	}
	wantFormat(t, b.String(), len(st))
	wantGolden(t, "states", b.Bytes())
}

// TestWriteStatusV1Line checks the line written for single statuses: every
// state, the empty fields, the quoting of each quotable field, and fields
// that the format leaves out.
func TestWriteStatusV1Line(t *testing.T) {
	t.Parallel()
	stale := status("legacy", "vendor/legacy", "", "v1.0.0", "v1.0.0", commitA, commitB,
		core.StateUnmanaged)
	target := status("kernel", "kernel", manifest.ModeTagPattern, "v6.6.*", "v6.6.9", commitA,
		commitA, core.StateBehind)
	target.Target = &core.Resolution{Mode: manifest.ModeTagPattern, Ref: "v6.6.10", Commit: commitB}
	target.Reason = "update would select\ttag-pattern v6.6.10"
	tests := []struct {
		name string
		st   core.Status
		want string
	}{
		{"ok", status("kernel", "kernel", manifest.ModeTagPattern, "v6.6.*", "v6.6.9", commitA,
			commitA, core.StateOK),
			"kernel\tkernel\ttag-pattern\tv6.6.*\tv6.6.9\t" + commitA + "\t" + commitA + "\tok"},
		{"behind", status("u-boot", "bootloader/u-boot", manifest.ModeBranch, "main", "main",
			commitA, commitA, core.StateBehind),
			"u-boot\tbootloader/u-boot\tbranch\tmain\tmain\t" + commitA + "\t" + commitA +
				"\tbehind"},
		{"drift", status("fpga.core", "ip/fpga-core", manifest.ModeTag, "v2.3.1", "v2.3.1",
			commitA, commitB, core.StateDrift),
			"fpga.core\tip/fpga-core\ttag\tv2.3.1\tv2.3.1\t" + commitA + "\t" + commitB +
				"\tdrift"},
		{"dirty", status("app", "apps/app", manifest.ModeTag, "v1.2.0", "v1.2.0", commitC,
			commitC, core.StateDirty),
			"app\tapps/app\ttag\tv1.2.0\tv1.2.0\t" + commitC + "\t" + commitC + "\tdirty"},
		{"uninitialized", status("theme", "docs/theme", manifest.ModeTag, "v1.0.0", "v1.0.0",
			commitA, "", core.StateUninitialized),
			"theme\tdocs/theme\ttag\tv1.0.0\tv1.0.0\t" + commitA + "\t\tuninitialized"},
		{"missing-ref", status("broken", "libs/broken", manifest.ModeTag, "v9.9.9", "v1.0.0",
			commitA, commitA, core.StateMissingRef),
			"broken\tlibs/broken\ttag\tv9.9.9\tv1.0.0\t" + commitA + "\t" + commitA +
				"\tmissing-ref"},
		{"unmanaged", status("legacy", "vendor/legacy", "", "", "", "", commitB,
			core.StateUnmanaged),
			"legacy\tvendor/legacy\t\t\t\t\t" + commitB + "\tunmanaged"},
		{"unmanaged with stray ref and lock entry", stale,
			"legacy\tvendor/legacy\t\t\t\t\t" + commitB + "\tunmanaged"},
		{"unmanaged not checked out", status("legacy", "vendor/legacy", "", "", "", "", "",
			core.StateUnmanaged),
			"legacy\tvendor/legacy\t\t\t\t\t\tunmanaged"},
		{"no lock entry", status("new", "libs/new", manifest.ModeBranch, "main", "", "",
			commitA, core.StateBehind),
			"new\tlibs/new\tbranch\tmain\t\t\t" + commitA + "\tbehind"},
		{"unborn HEAD", status("empty", "libs/empty", manifest.ModeTag, "v1", "v1", commitA, "",
			core.StateDrift),
			"empty\tlibs/empty\ttag\tv1\tv1\t" + commitA + "\t\tdrift"},
		{"commit mode", status("crypto lib", "libs/crypto lib", manifest.ModeCommit, commitA,
			commitA, commitA, commitA, core.StateOK),
			"crypto lib\tlibs/crypto lib\tcommit\t" + commitA + "\t" + commitA + "\t" +
				commitA + "\t" + commitA + "\tok"},
		{"sha256", status("sdk", "sdk", manifest.ModeTagPattern, "v*", "v3.0.0-rc.2",
			commit256, commit256, core.StateOK),
			"sdk\tsdk\ttag-pattern\tv*\tv3.0.0-rc.2\t" + commit256 + "\t" + commit256 + "\tok"},
		{"target and reason are not written", target,
			"kernel\tkernel\ttag-pattern\tv6.6.*\tv6.6.9\t" + commitA + "\t" + commitA +
				"\tbehind"},
		{"quoted name", status(`we"ird\name`, "weird", manifest.ModeTag, "v1.0.0", "", "",
			commitA, core.StateBehind),
			`"we\"ird\\name"` + "\tweird\ttag\tv1.0.0\t\t\t" + commitA + "\tbehind"},
		{"quoted path", status("tab", "a\tb", manifest.ModeTag, "v1", "", "", "",
			core.StateUninitialized),
			"tab\t" + `"a\tb"` + "\ttag\tv1\t\t\t\tuninitialized"},
		{"quoted ref", status("ctrl-ref", "ctrl-ref", manifest.ModeTag, "v1.0.0\x1b]0;pwned\a",
			"", "", commitA, core.StateMissingRef),
			"ctrl-ref\tctrl-ref\ttag\t" + `"v1.0.0\033]0;pwned\a"` + "\t\t\t" + commitA +
				"\tmissing-ref"},
		{"quoted lock ref", status("newline-ref", "newline-ref", manifest.ModeTag, "v1.0.0",
			"v1.0.0\nv1.0.1", commitA, commitA, core.StateDrift),
			"newline-ref\tnewline-ref\ttag\tv1.0.0\t" + `"v1.0.0\nv1.0.1"` + "\t" + commitA +
				"\t" + commitA + "\tdrift"},
		{"leading quote", status(`"name"`, `"path`, manifest.ModeBranch, `"ref`, "", "", "",
			core.StateUninitialized),
			`"\"name\""` + "\t" + `"\"path"` + "\tbranch\t" + `"\"ref"` +
				"\t\t\t\tuninitialized"},
		{"spaces and non-ASCII unquoted", status("zażółć gęślą", " lead/trail ",
			manifest.ModeTag, "wersja-ł", "", "", "", core.StateUninitialized),
			"zażółć gęślą\t lead/trail \ttag\twersja-ł\t\t\t\tuninitialized"},
		{"C1 control and invalid UTF-8", status("csi\u009b", "bad\xffpath", manifest.ModeTag,
			"v1", "", "", "", core.StateUninitialized),
			`"csi\302\233"` + "\t" + `"bad\377path"` + "\ttag\tv1\t\t\t\tuninitialized"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var b bytes.Buffer
			if err := porcelain.WriteStatusV1(&b, []core.Status{tt.st}); err != nil {
				t.Fatal(err)
			}
			want := porcelain.HeaderV1 + "\n" + tt.want + "\n"
			if got := b.String(); got != want {
				t.Errorf("got\n%s\nwant\n%s", lineDiff(want, got), want)
			}
			wantFormat(t, b.String(), 1)
		})
	}
}

// TestWriteStatusV1Empty checks that no statuses give the header alone.
// TestWriteStatusV1LineBreaks checks that one submodule gives one line for
// functions that break lines at more than LF, such as str.splitlines of
// Python, whatever its name and path contain.
func TestWriteStatusV1LineBreaks(t *testing.T) {
	t.Parallel()
	breaks := "\n\r\v\f\x1c\x1d\x1e\u0085\u2028\u2029"
	for _, sep := range breaks {
		field := "a" + string(sep) + "b"
		var out bytes.Buffer
		st := status(field, field, manifest.ModeTag, field, field, commitA, commitA,
			core.StateOK)
		if err := porcelain.WriteStatusV1(&out, []core.Status{st}); err != nil {
			t.Fatal(err)
		}
		lines := strings.FieldsFunc(out.String(), func(c rune) bool {
			return strings.ContainsRune(breaks, c)
		})
		if len(lines) != 2 || lines[0] != porcelain.HeaderV1 {
			t.Errorf("separator %U: %d lines: %q", sep, len(lines), out.String())
		}
	}
}

func TestWriteStatusV1Empty(t *testing.T) {
	t.Parallel()
	for _, st := range [][]core.Status{nil, {}} {
		var b bytes.Buffer
		if err := porcelain.WriteStatusV1(&b, st); err != nil {
			t.Fatal(err)
		}
		if got, want := b.String(), "# lsm-porcelain v1\n"; got != want {
			t.Errorf("WriteStatusV1(%#v) = %q, want %q", st, got, want)
		}
		wantFormat(t, b.String(), 0)
	}
}

// TestWriteStatusV1UnknownState checks that a state outside the format is
// refused before anything is written, and that the error names the
// submodule without control characters.
func TestWriteStatusV1UnknownState(t *testing.T) {
	t.Parallel()
	tests := []struct {
		state core.State
		name  string
		want  string
	}{
		{"", "kernel", `kernel: state not defined by the porcelain format: ""`},
		{"stale", "kernel", `kernel: state not defined by the porcelain format: "stale"`},
		{"OK", "kernel", `kernel: state not defined by the porcelain format: "OK"`},
		{"ok\t", "kernel", `kernel: state not defined by the porcelain format: "ok\t"`},
		{"bogus", "a\nb", `"a\nb": state not defined by the porcelain format: "bogus"`},
	}
	for _, tt := range tests {
		st := stateStatuses()
		st = append(st, status(tt.name, "path", manifest.ModeTag, "v1", "", "", "", tt.state))
		st = append(st, stateStatuses()...)
		var b bytes.Buffer
		err := porcelain.WriteStatusV1(&b, st)
		if !errors.Is(err, porcelain.ErrUnknownState) {
			t.Errorf("state %q: error %v, want %v", tt.state, err, porcelain.ErrUnknownState)
			continue
		}
		if err.Error() != tt.want {
			t.Errorf("state %q: error %q, want %q", tt.state, err, tt.want)
		}
		if b.Len() != 0 {
			t.Errorf("state %q: wrote %q", tt.state, b.String())
		}
	}
}

// failingWriter accepts limit bytes and then fails with err.
type failingWriter struct {
	limit   int
	err     error
	calls   int
	written strings.Builder
}

// Write implements io.Writer.
func (w *failingWriter) Write(p []byte) (int, error) {
	w.calls++
	n := min(len(p), w.limit-w.written.Len())
	w.written.Write(p[:n])
	if n < len(p) {
		return n, w.err
	}
	return n, nil
}

// TestWriteStatusV1WriteError checks that the error of the writer is
// returned, wrapped, and that the output is written with a single call.
func TestWriteStatusV1WriteError(t *testing.T) {
	t.Parallel()
	var full bytes.Buffer
	if err := porcelain.WriteStatusV1(&full, stateStatuses()); err != nil {
		t.Fatal(err)
	}
	for _, limit := range []int{0, 1, len(porcelain.HeaderV1) + 1, full.Len() - 1, full.Len()} {
		w := &failingWriter{limit: limit, err: errors.New("disk full")}
		err := porcelain.WriteStatusV1(w, stateStatuses())
		switch {
		case limit == full.Len() && err != nil:
			t.Errorf("limit %d: %v", limit, err)
		case limit < full.Len() && !errors.Is(err, w.err):
			t.Errorf("limit %d: error %v, want %v", limit, err, w.err)
		case err != nil && err.Error() != "write porcelain status: disk full":
			t.Errorf("limit %d: error %q", limit, err)
		}
		if got, want := w.written.String(), full.String()[:limit]; got != want {
			t.Errorf("limit %d: wrote %q, want %q", limit, got, want)
		}
		if w.calls != 1 {
			t.Errorf("limit %d: %d calls of Write, want 1", limit, w.calls)
		}
	}
}
