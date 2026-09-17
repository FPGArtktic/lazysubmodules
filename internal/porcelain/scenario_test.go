// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package porcelain_test

import (
	"bytes"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/FPGArtktic/lazysubmodules/internal/core"
	"github.com/FPGArtktic/lazysubmodules/internal/git/gittest"
	"github.com/FPGArtktic/lazysubmodules/internal/lock"
	"github.com/FPGArtktic/lazysubmodules/internal/porcelain"
)

// complexStates returns the state of every submodule of the complex
// superproject as it is built.
func complexStates() map[string]core.State {
	return map[string]core.State{
		"kernel":     core.StateBehind,
		"u-boot":     core.StateBehind,
		"fpga.core":  core.StateOK,
		"crypto lib": core.StateOK,
		"legacy":     core.StateUnmanaged,
		"tools":      core.StateOK,
		"theme":      core.StateUninitialized,
		"fresh":      core.StateUninitialized,
		"app":        core.StateDirty,
		"sdk":        core.StateOK,
		"mirror-lib": core.StateOK,
		"broken":     core.StateMissingRef,
		"signed":     core.StateOK,
		"quirky":     core.StateOK,
	}
}

// complexOutputs is the porcelain output of a complex superproject.
type complexOutputs struct {
	// built is the output for the superproject as it is built.
	built string
	// drift is the output after the moved tag of fpga.core was fetched.
	drift string
}

// TestGoldenComplex writes the status of the complex superproject, compares
// the SHA-1 output with testdata/complex-sha1.golden and checks that the
// SHA-256 output has the same content with 64-digit commits. Both outputs
// are also compared with the values the fixture recorded, before and after
// fetching the moved tag of fpga.core, which makes it drift.
func TestGoldenComplex(t *testing.T) {
	t.Parallel()
	var sha1, sha256 complexOutputs
	ok := t.Run("build", func(t *testing.T) {
		t.Run(gittest.SHA1, func(t *testing.T) {
			t.Parallel()
			sha1 = complexOutput(t, gittest.SHA1)
		})
		t.Run(gittest.SHA256, func(t *testing.T) {
			t.Parallel()
			sha256 = complexOutput(t, gittest.SHA256)
		})
	})
	if !ok {
		return
	}
	wantGolden(t, "complex-sha1", []byte(sha1.built))
	wantSameShape(t, "built", sha1.built, sha256.built)
	wantSameShape(t, "drift", sha1.drift, sha256.drift)
}

// complexOutput builds a complex superproject, checks its porcelain output
// before and after fpga.core fetched its moved tag, and returns both.
func complexOutput(t *testing.T, format string) complexOutputs {
	t.Helper()
	c := gittest.NewComplexSuper(t, format)
	r, err := core.Open(t.Context(), c.Runner(t), c.Dir)
	if err != nil {
		t.Fatal(err)
	}
	states := complexStates()
	var out complexOutputs
	out.built = statusOutput(t, r)
	wantFixtureOutput(t, out.built, c, states)
	if _, err := r.Fetch(t.Context(), []string{c.FPGACore.Name}, nil); err != nil {
		t.Fatal(err)
	}
	states[c.FPGACore.Name] = core.StateDrift
	out.drift = statusOutput(t, r)
	wantFixtureOutput(t, out.drift, c, states)
	return out
}

// statusOutput returns the porcelain output of the status of every
// submodule.
func statusOutput(t *testing.T, r *core.Repo) string {
	t.Helper()
	st, err := r.Status(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	if err := porcelain.WriteStatusV1(&b, st); err != nil {
		t.Fatal(err)
	}
	wantFormat(t, b.String(), len(st))
	return b.String()
}

// wantFixtureOutput compares output with the lines built from the values
// that the fixture recorded and the given states. None of the values of
// the complex superproject needs quoting.
func wantFixtureOutput(t *testing.T, output string, c *gittest.ComplexSuper,
	states map[string]core.State,
) {
	t.Helper()
	var b strings.Builder
	b.WriteString(porcelain.HeaderV1 + "\n")
	hexLen := map[string]int{gittest.SHA1: 40, gittest.SHA256: 64}[c.Format]
	for _, s := range c.Submodules() {
		fields := []string{s.Name, s.Path, s.Mode, s.Ref, "", "", s.Head, string(states[s.Name])}
		if s.Lock != nil {
			fields[4], fields[5] = s.Lock.Ref, s.Lock.Commit
			if len(s.Lock.Commit) != hexLen {
				t.Errorf("%s: locked commit %q is not a %s name", s.Name, s.Lock.Commit, c.Format)
			}
		}
		b.WriteString(strings.Join(fields, "\t") + "\n")
	}
	if want := b.String(); output != want {
		t.Errorf("%s: output differs from the fixture:\n%s", c.Format, lineDiff(want, output))
	}
}

// wantSameShape checks that the SHA-256 output equals the SHA-1 output
// once every commit name is replaced by a placeholder numbered in order of
// appearance, and that the outputs use commit names of the right length.
func wantSameShape(t *testing.T, what, sha1, sha256 string) {
	t.Helper()
	shape1, n1 := shape(t, sha1, 40)
	shape256, n256 := shape(t, sha256, 64)
	if n1 == 0 || n1 != n256 {
		t.Errorf("%s: %d SHA-1 and %d SHA-256 commit fields", what, n1, n256)
	}
	if shape1 != shape256 {
		t.Errorf("%s: SHA-256 output differs from SHA-1 output:\n%s",
			what, lineDiff(shape1, shape256))
	}
}

// shape replaces each distinct commit name of output by a placeholder and
// returns the result with the number of fields replaced. Commit names must
// have hexLen digits.
func shape(t *testing.T, output string, hexLen int) (string, int) {
	t.Helper()
	ids := make(map[string]string)
	count := 0
	lines := strings.Split(output, "\n")
	for i, line := range lines {
		fields := strings.Split(line, "\t")
		for j, f := range fields {
			if !lock.ValidCommit(f) {
				continue
			}
			if len(f) != hexLen {
				t.Errorf("line %d, field %d: commit %s has %d digits, want %d",
					i+1, j+1, f, len(f), hexLen)
			}
			if _, ok := ids[f]; !ok {
				ids[f] = fmt.Sprintf("<commit %d>", len(ids)+1)
			}
			fields[j] = ids[f]
			count++
		}
		lines[i] = strings.Join(fields, "\t")
	}
	return strings.Join(lines, "\n"), count
}

// TestWriteStatusV1Hostile writes the status of a superproject with hostile
// .gitmodules values and checks that every field decodes to the value in
// the status, and that the hostile values are quoted.
func TestWriteStatusV1Hostile(t *testing.T) {
	t.Parallel()
	h := gittest.NewHostileSuper(t, gittest.SHA1)
	// The lock file cannot be read while it has entries with these refs.
	for _, s := range []*gittest.Submodule{h.DashRef, h.CtrlRef} {
		gittest.Git(t, h.Dir, "config", "-f", gittest.LockFile, "--remove-section",
			"submodule."+s.Name)
	}
	r, err := core.Open(t.Context(), gittest.Runner(t), h.Dir)
	if err != nil {
		t.Fatal(err)
	}
	st, err := r.Status(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	if err := porcelain.WriteStatusV1(&b, st); err != nil {
		t.Fatal(err)
	}
	wantFormat(t, b.String(), len(st))
	lines := strings.Split(strings.TrimSuffix(b.String(), "\n"), "\n")[1:]
	for i, s := range st {
		if i >= len(lines) {
			break
		}
		if got, want := decodeFields(t, lines[i]), statusFields(s); !slices.Equal(got, want) {
			t.Errorf("line %d decodes to %q, want %q", i+1, got, want)
		}
	}
	// CtrlRef lost its lock entry above; NewlineRef and Quoted have none.
	for _, want := range []string{
		`"we\"ird\\name"` + "\tweird\ttag\tv1.0.0\t\t\t" + h.Quoted.Head + "\tbehind",
		"ctrl-ref\tctrl-ref\ttag\t" + `"v1.0.0\033]0;pwned\a"` + "\t\t\t" + h.CtrlRef.Head +
			"\tmissing-ref",
		"newline-ref\tnewline-ref\ttag\t" + `"v1.0.0\nv1.0.1"` + "\t\t\t" +
			h.NewlineRef.Head + "\tmissing-ref",
	} {
		if !slices.Contains(lines, want) {
			t.Errorf("no line %q in\n%s", want, b.String())
		}
	}
}

// statusFields returns the fields a status is expected to have.
func statusFields(s core.Status) []string {
	f := []string{s.Submodule.Name, s.Submodule.Path, "", "", "", "", s.Head, string(s.State)}
	if s.Submodule.Managed() {
		f[2], f[3] = string(s.Submodule.Mode), s.Submodule.Ref
		if s.Lock != nil {
			f[4], f[5] = s.Lock.Ref, s.Lock.Commit
		}
	}
	return f
}
