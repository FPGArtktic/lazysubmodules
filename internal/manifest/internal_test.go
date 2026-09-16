// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package manifest

import "testing"

func TestSplitKey(t *testing.T) {
	t.Parallel()
	tests := []struct {
		key, name, variable string
		ok                  bool
	}{
		{"submodule.kernel.path", "kernel", "path", true},
		{"submodule.a.b.c.lsm-ref", "a.b.c", "lsm-ref", true},
		{"Submodule.Kernel.LSM-Mode", "Kernel", "lsm-mode", true},
		{"SUBMODULE.x.URL", "x", "url", true},
		{"submodule..path", "", "path", true},
		{"submodule.path", "", "", false},
		{"submodule", "", "", false},
		{"submodules.x.path", "", "", false},
		{"core.x.path", "", "", false},
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
		"kernel":       true,
		"a.b.c":        true,
		"lib/one":      true,
		"...":          true,
		"..a":          true,
		"a..":          true,
		"with space":   true,
		"":             false,
		"..":           false,
		"../x":         false,
		"x/..":         false,
		"a/../b":       false,
		"a\\..\\b":     false,
		"/..":          false,
		"a//..//b":     false,
		"x/../../etc/": false,
	}
	for name, want := range tests {
		if got := validName(name); got != want {
			t.Errorf("validName(%q) = %t, want %t", name, got, want)
		}
	}
}

func TestValidPath(t *testing.T) {
	t.Parallel()
	tests := map[string]bool{
		"kernel":         true,
		"third_party/fw": true,
		"..hidden":       true,
		".config":        true,
		"a b/c":          true,
		"":               false,
		".":              false,
		"..":             false,
		"../x":           false,
		"a/../../x":      false,
		"a/../b":         false,
		"./a":            false,
		"a/":             false,
		"a//b":           false,
		"/abs":           false,
	}
	for p, want := range tests {
		if got := validPath(p); got != want {
			t.Errorf("validPath(%q) = %t, want %t", p, got, want)
		}
	}
}
