// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package lock_test

import (
	"strings"
	"testing"

	"github.com/FPGArtktic/lazysubmodules/internal/lock"
)

func TestValidCommit(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{"sha1", sha1A, true},
		{"sha1 digits only", strings.Repeat("0", 40), true},
		{"sha1 letters only", strings.Repeat("f", 40), true},
		{"sha256", sha256A, true},
		{"sha256 letters only", strings.Repeat("a", 64), true},
		{"empty", "", false},
		{"abbreviated", sha1A[:7], false},
		{"39 digits", sha1A[:39], false},
		{"41 digits", sha1A + "a", false},
		{"63 digits", sha256A[:63], false},
		{"65 digits", sha256A + "a", false},
		{"80 digits", sha1A + sha1A, false},
		{"uppercase", strings.ToUpper(sha1A), false},
		{"one uppercase digit", "A" + sha1A[1:], false},
		{"uppercase sha256", strings.ToUpper(sha256A), false},
		{"letter after f", "g" + sha1A[1:], false},
		{"prefix", "0x" + sha1A[2:], false},
		{"leading space", " " + sha1A[1:], false},
		{"trailing newline", sha1A[1:] + "\n", false},
		{"option", "-" + sha1A[1:], false},
		{"revision syntax", sha1A[:38] + "^0", false},
		{"multi-byte", "é" + sha1A[2:], false},
		{"NUL", "\x00" + sha1A[1:], false},
	}
	for _, tt := range tests {
		if got := lock.ValidCommit(tt.in); got != tt.want {
			t.Errorf("%s: ValidCommit(%q) = %t, want %t", tt.name, tt.in, got, tt.want)
		}
	}
}
