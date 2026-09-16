// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package manifest_test

import (
	"errors"
	"strconv"
	"testing"

	"github.com/FPGArtktic/lazysubmodules/internal/manifest"
)

func TestParseMode(t *testing.T) {
	t.Parallel()
	valid := map[string]manifest.Mode{
		"branch":      manifest.ModeBranch,
		"tag":         manifest.ModeTag,
		"tag-pattern": manifest.ModeTagPattern,
		"commit":      manifest.ModeCommit,
	}
	for s, want := range valid {
		got, err := manifest.ParseMode(s)
		if got != want || err != nil {
			t.Errorf("ParseMode(%q) = %q, %v; want %q, nil", s, got, err, want)
		}
		if got.String() != s {
			t.Errorf("Mode(%q).String() = %q", s, got.String())
		}
	}

	invalid := []string{"", "Branch", "TAG", "tags", " tag", "tag ", "tag_pattern", "sha", "none"}
	for _, s := range invalid {
		got, err := manifest.ParseMode(s)
		if got != "" || !errors.Is(err, manifest.ErrInvalidMode) {
			t.Errorf("ParseMode(%q) = %q, %v; want ErrInvalidMode", s, got, err)
			continue
		}
		want := "invalid tracking mode " + strconv.Quote(s)
		if err.Error() != want {
			t.Errorf("ParseMode(%q) error = %q, want %q", s, err, want)
		}
	}
}

func TestModeZeroValue(t *testing.T) {
	t.Parallel()
	var m manifest.Mode
	if m.String() != "" {
		t.Errorf("zero Mode.String() = %q, want empty", m.String())
	}
	if (manifest.Submodule{}).Managed() {
		t.Errorf("Submodule without mode is managed")
	}
	if !(manifest.Submodule{Mode: manifest.ModeCommit}).Managed() {
		t.Errorf("Submodule with mode is not managed")
	}
}
