// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package porcelain_test

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/FPGArtktic/lazysubmodules/internal/core"
	"github.com/FPGArtktic/lazysubmodules/internal/porcelain"
)

// updateFlag names the flag that rewrites the golden files in testdata
// instead of comparing with them:
//
//	go test ./internal/porcelain -run Golden -update
//
// Review the diff of testdata afterwards: the v1 format is a stable
// interface, and a golden file changes only with the fixture it describes.
const updateFlag = "update"

// TestMain registers updateFlag. The flag is looked up by name, which
// keeps the package free of global variables.
func TestMain(m *testing.M) {
	flag.Bool(updateFlag, false, "rewrite the golden files in testdata")
	os.Exit(m.Run())
}

// updating reports whether the golden files are rewritten.
func updating() bool {
	f := flag.Lookup(updateFlag)
	return f != nil && f.Value.String() == "true"
}

// goldenPath returns the path of a golden file.
func goldenPath(name string) string {
	return filepath.Join("testdata", name+".golden")
}

// wantGolden compares output with a golden file, or rewrites the file with
// -update.
func wantGolden(t *testing.T, name string, output []byte) {
	t.Helper()
	path := goldenPath(name)
	if updating() {
		if err := os.WriteFile(path, output, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want := readGolden(t, name)
	if !bytes.Equal(output, want) {
		t.Errorf("output differs from %s (run with -%s to rewrite it):\n%s",
			path, updateFlag, lineDiff(string(want), string(output)))
	}
}

// readGolden returns the content of a golden file.
func readGolden(t *testing.T, name string) []byte {
	t.Helper()
	want, err := os.ReadFile(goldenPath(name))
	if err != nil {
		t.Fatalf("%v (run with -%s to create it)", err, updateFlag)
	}
	return want
}

// lineDiff lists the lines that differ between two outputs, quoted so that
// TABs and control characters are visible.
func lineDiff(want, got string) string {
	w, g := strings.Split(want, "\n"), strings.Split(got, "\n")
	var b strings.Builder
	for i := range max(len(w), len(g)) {
		wl, gl := lineAt(w, i), lineAt(g, i)
		if wl != gl {
			fmt.Fprintf(&b, "line %d:\n  want %s\n  got  %s\n", i+1, wl, gl)
		}
	}
	return b.String()
}

// lineAt returns line i quoted, or "(none)" past the end.
func lineAt(lines []string, i int) string {
	if i >= len(lines) {
		return "(none)"
	}
	return strconv.Quote(lines[i])
}

// wantFormat checks the properties that every v1 output has, whatever the
// statuses: the header, LF line endings, eight fields per line, a known
// state in the last field, valid UTF-8 and no control characters other
// than the separators.
func wantFormat(t *testing.T, output string, lines int) {
	t.Helper()
	if !utf8.ValidString(output) {
		t.Errorf("output is not valid UTF-8: %s", strconv.Quote(output))
	}
	if strings.ContainsFunc(output, func(r rune) bool {
		return r != '\t' && r != '\n' && unicode.IsControl(r)
	}) {
		t.Errorf("output contains control characters: %s", strconv.Quote(output))
	}
	body, ok := strings.CutPrefix(output, porcelain.HeaderV1+"\n")
	if !ok {
		t.Fatalf("output does not start with the header: %s", strconv.Quote(output))
	}
	if body == "" {
		if lines != 0 {
			t.Errorf("no lines, want %d", lines)
		}
		return
	}
	if !strings.HasSuffix(body, "\n") {
		t.Errorf("output does not end with LF: %s", strconv.Quote(output))
	}
	records := strings.Split(strings.TrimSuffix(body, "\n"), "\n")
	if len(records) != lines {
		t.Errorf("%d lines, want %d", len(records), lines)
	}
	for _, line := range records {
		fields := strings.Split(line, "\t")
		if len(fields) != 8 || !knownState(core.State(fields[7])) {
			t.Errorf("malformed line %s", strconv.Quote(line))
		}
	}
}

// decodeFields splits a v1 line into its fields and removes the quoting
// the way a script written in Go would do it.
func decodeFields(t *testing.T, line string) []string {
	t.Helper()
	fields := strings.Split(line, "\t")
	for i, f := range fields {
		if !strings.HasPrefix(f, `"`) {
			continue
		}
		u, err := strconv.Unquote(f)
		if err != nil {
			t.Fatalf("field %d of %s: %v", i+1, strconv.Quote(line), err)
		}
		fields[i] = u
	}
	return fields
}

// knownState reports whether state is one of the seven v1 states.
func knownState(state core.State) bool {
	switch state {
	case core.StateOK, core.StateBehind, core.StateDrift, core.StateDirty,
		core.StateUninitialized, core.StateMissingRef, core.StateUnmanaged:
		return true
	}
	return false
}
