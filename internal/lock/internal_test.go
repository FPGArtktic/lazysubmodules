// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package lock

import "testing"

func TestSplitKey(t *testing.T) {
	t.Parallel()
	tests := []struct {
		key, name, variable string
		ok                  bool
	}{
		{"submodule.kernel.commit", "kernel", "commit", true},
		{"submodule.a.b.c.ref", "a.b.c", "ref", true},
		{"Submodule.Kernel.MODE", "Kernel", "mode", true},
		{"submodule..mode", "", "mode", true},
		{"submodule.mode", "", "", false},
		{"submodule", "", "", false},
		{"submodules.x.mode", "", "", false},
		{"core.x.mode", "", "", false},
		{"", "", "", false},
	}
	for _, tt := range tests {
		name, variable, ok := splitKey(tt.key)
		if name != tt.name || variable != tt.variable || ok != tt.ok {
			t.Errorf("splitKey(%q) = %q, %q, %t; want %q, %q, %t",
				tt.key, name, variable, ok, tt.name, tt.variable, tt.ok)
		}
	}
}

func TestValidName(t *testing.T) {
	t.Parallel()
	tests := map[string]bool{
		"kernel":     true,
		"a.b.c":      true,
		"lib/one":    true,
		"with space": true,
		`quo"te\`:    true,
		"..":         true,
		"":           false,
		"a\nb":       false,
		"a\x00b":     false,
		"\n":         false,
	}
	for name, want := range tests {
		if got := validName(name); got != want {
			t.Errorf("validName(%q) = %t, want %t", name, got, want)
		}
	}
}

func TestValidRef(t *testing.T) {
	t.Parallel()
	tests := map[string]bool{
		"main":        true,
		"release/2.x": true,
		"v6.6.8":      true,
		"v1.0.0-rc.1": true,
		"a-b":         true,
		"ünïcode":     true,
		"":            false,
		"-":           false,
		"--orphan":    false,
		" v1":         false,
		"v 1":         false,
		"v1\t":        false,
		"v1\n":        false,
		"v1\r":        false,
		"v\x00":       false,
		"v\x1b[31m":   false,
		"v\x7f":       false,
	}
	for ref, want := range tests {
		if got := validRef(ref); got != want {
			t.Errorf("validRef(%q) = %t, want %t", ref, got, want)
		}
	}
}
