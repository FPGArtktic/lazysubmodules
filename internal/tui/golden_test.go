// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package tui

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/exp/teatest/v2"
)

// colorParams reports whether a screen sets colors with SGR sequences.
func colorParams(content string) []string {
	sgr := regexp.MustCompile(`\x1b\[([0-9;:]*)m`)
	var colors []string
	for _, match := range sgr.FindAllStringSubmatch(content, -1) {
		for p := range strings.SplitSeq(match[1], ";") {
			n, err := strconv.Atoi(strings.SplitN(p, ":", 2)[0])
			if err == nil && (n >= 30 && n <= 49 || n >= 90 && n <= 107) {
				colors = append(colors, match[0])
			}
		}
	}
	return colors
}

// TestGoldenMainScreen compares the main screen with golden files. The
// screen is taken from the final model rather than from the output stream,
// whose frames depend on timing. Update the files with "go test
// ./internal/tui -run TestGoldenMainScreen -update".
func TestGoldenMainScreen(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name          string
		width, height int
		noColor       bool
		profile       colorprofile.Profile
	}{
		{"80x24", 80, 24, true, colorprofile.ASCII},
		{"120x30", 120, 30, true, colorprofile.ASCII},
		{"120x30-color", 120, 30, false, colorprofile.TrueColor},
		{"60x16", 60, 16, true, colorprofile.ASCII},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := startWith(t, newFake(), harnessOptions{width: tc.width, height: tc.height,
				noColor: tc.noColor, profile: tc.profile})
			m := h.finish()
			content := m.View().Content
			checkScreenSize(t, strings.Split(content, "\n"), tc.width, tc.height)
			colors := colorParams(content)
			if tc.noColor && len(colors) > 0 {
				t.Errorf("colors without color: %q", colors)
			}
			if !tc.noColor && len(colors) == 0 {
				t.Error("no colors")
			}
			teatest.RequireEqualOutput(t, []byte(content))
		})
	}
}

func TestGoldenDialogs(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		keys []string
		wait string
	}{
		{"confirm", []string{"U"}, "Update and commit"},
		{"picker", []string{"t"}, "> v6.7-rc1"},
		{"pattern", []string{"p"}, "4 local tags match"},
		{"help", []string{"?"}, "Navigation"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := startWith(t, newFake(), harnessOptions{width: 80, height: 24, noColor: true,
				profile: colorprofile.ASCII})
			h.press(tc.keys...)
			h.waitScreen(tc.wait)
			h.waitIdle()
			m := h.finish()
			content := m.View().Content
			checkScreenSize(t, strings.Split(content, "\n"), 80, 24)
			teatest.RequireEqualOutput(t, []byte(content))
		})
	}
}
