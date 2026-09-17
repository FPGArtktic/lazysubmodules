// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package tui

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/teatest/v2"
)

// eastAsianVar makes the width functions count characters of ambiguous
// width, such as box drawing characters, as two cells. They read it when
// the process starts.
const eastAsianVar = "RUNEWIDTH_EASTASIAN"

// TestMain runs the tests with the cell widths of most terminals: a process
// whose environment asks for East Asian widths runs the test binary again
// without them, since the layout under test depends on the widths.
func TestMain(m *testing.M) {
	if v, ok := os.LookupEnv(eastAsianVar); ok && v != "0" {
		env := slices.DeleteFunc(os.Environ(), func(kv string) bool {
			return strings.HasPrefix(kv, eastAsianVar+"=")
		})
		exe, err := os.Executable()
		if err == nil {
			err = syscall.Exec(exe, os.Args, append(env, eastAsianVar+"=0"))
		}
		fmt.Fprintf(os.Stderr, "run the tests with %s=0: %v\n", eastAsianVar, err)
		os.Exit(1)
	}
	m.Run()
}

// waitTimeout bounds every wait of the tests; the operations of the fake
// backend finish at once, so only a hang reaches it.
const waitTimeout = 20 * time.Second

// probeMsg runs fn on the model inside the program, between two messages.
// The program filter consumes it, so the model never sees it.
type probeMsg struct {
	fn func(Model)
}

// probeFilter runs probes; every other message passes.
func probeFilter(m tea.Model, msg tea.Msg) tea.Msg {
	if p, ok := msg.(probeMsg); ok {
		p.fn(m.(Model))
		return nil
	}
	return msg
}

// harness drives a Model in a teatest program.
//
// Output-based waits are unreliable because the renderer writes only the
// cells that changed, so the tests inspect the model itself: a probe sent
// after some keys runs after the program has handled them.
type harness struct {
	t  *testing.T
	tm *teatest.TestModel
}

// harnessOptions configures a harness.
type harnessOptions struct {
	width, height int
	noColor       bool
	profile       colorprofile.Profile
}

// start runs the model of a backend at 100x30 without colors.
func start(t *testing.T, b Backend) *harness {
	t.Helper()
	return startWith(t, b, harnessOptions{width: 100, height: 30, noColor: true,
		profile: colorprofile.ASCII})
}

// startWith runs the model of a backend. The color profile and the
// environment are fixed, so that the output does not depend on the
// terminal of the test run; all program options go into one
// WithProgramOptions call, since a later call replaces an earlier one.
func startWith(t *testing.T, b Backend, o harnessOptions) *harness {
	t.Helper()
	m := New(t.Context(), b, Options{NoColor: o.noColor})
	tm := teatest.NewTestModel(t, m,
		teatest.WithInitialTermSize(o.width, o.height),
		teatest.WithProgramOptions(
			tea.WithColorProfile(o.profile),
			tea.WithEnvironment([]string{"TERM=xterm-256color"}),
			tea.WithFilter(probeFilter),
		))
	h := &harness{t: t, tm: tm}
	h.waitIdle()
	return h
}

// with returns a harness for the same program that reports to t, such as
// a subtest.
func (h *harness) with(t *testing.T) *harness {
	return &harness{t: t, tm: h.tm}
}

// probe runs fn on the current model and waits for it.
func (h *harness) probe(fn func(Model)) {
	h.t.Helper()
	done := make(chan struct{})
	h.tm.Send(probeMsg{fn: func(m Model) {
		fn(m)
		close(done)
	}})
	select {
	case <-done:
	case <-time.After(waitTimeout):
		h.t.Fatal("the program did not handle a probe")
	}
}

// screen returns the current screen without styles.
func (h *harness) screen() string {
	h.t.Helper()
	var out string
	h.probe(func(m Model) { out = ansi.Strip(m.View().Content) })
	return out
}

// until waits until cond holds for the model.
func (h *harness) until(what string, cond func(Model) bool) {
	h.t.Helper()
	deadline := time.Now().Add(waitTimeout)
	for {
		ok := false
		var screen string
		h.probe(func(m Model) {
			ok = cond(m)
			if !ok {
				screen = ansi.Strip(m.View().Content)
			}
		})
		if ok {
			return
		}
		if time.Now().After(deadline) {
			h.t.Fatalf("timed out waiting for %s; screen:\n%s", what, screen)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// idle reports whether nothing is in progress or scheduled.
func idle(m Model) bool {
	return m.busy == "" && m.loads == 0 && !m.preview.pending &&
		(m.overlay.kind != overlayPattern || !m.overlay.loading)
}

// waitIdle waits until the model is idle.
func (h *harness) waitIdle() {
	h.t.Helper()
	h.until("idle", idle)
}

// waitDialog waits until the open dialog contains every text.
func (h *harness) waitDialog(texts ...string) string {
	h.t.Helper()
	var box string
	h.until("dialog with "+strings.Join(texts, ", "), func(m Model) bool {
		box = ansi.Strip(strings.Join(m.overlayBox(), "\n"))
		for _, t := range texts {
			if !strings.Contains(box, t) {
				return false
			}
		}
		return true
	})
	return box
}

// waitScreen waits until the screen contains every text.
func (h *harness) waitScreen(texts ...string) {
	h.t.Helper()
	h.until("screen with "+strings.Join(texts, ", "), func(m Model) bool {
		screen := ansi.Strip(m.View().Content)
		for _, t := range texts {
			if !strings.Contains(screen, t) {
				return false
			}
		}
		return true
	})
}

// wantScreen checks that the current screen contains or lacks texts.
func (h *harness) wantScreen(contains []string, lacks ...string) {
	h.t.Helper()
	screen := h.screen()
	for _, t := range contains {
		if !strings.Contains(screen, t) {
			h.t.Errorf("screen lacks %q:\n%s", t, screen)
		}
	}
	for _, t := range lacks {
		if strings.Contains(screen, t) {
			h.t.Errorf("screen shows %q:\n%s", t, screen)
		}
	}
}

// press sends keys: names of special keys, or text typed as one key per
// character.
func (h *harness) press(keys ...string) {
	for _, k := range keys {
		if msg, ok := specialKey(k); ok {
			h.tm.Send(msg)
			continue
		}
		h.tm.Type(k)
	}
}

// specialKey returns the message of a named key.
func specialKey(name string) (tea.KeyPressMsg, bool) {
	codes := map[string]rune{
		"enter": tea.KeyEnter, "esc": tea.KeyEscape, "up": tea.KeyUp, "down": tea.KeyDown,
		"pgup": tea.KeyPgUp, "pgdown": tea.KeyPgDown, "home": tea.KeyHome, "end": tea.KeyEnd,
		"backspace": tea.KeyBackspace,
	}
	if name == "ctrl+c" {
		return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}, true
	}
	code, ok := codes[name]
	return tea.KeyPressMsg{Code: code}, ok
}

// confirmWhenReady answers the open confirmation with y once its question
// is shown; y is ignored while the question loads.
func (h *harness) confirmWhenReady() {
	h.t.Helper()
	h.until("confirmation ready", func(m Model) bool {
		return m.overlay.kind == overlayConfirm && !m.overlay.loading
	})
	h.press("y")
}

// resize sends a new terminal size.
func (h *harness) resize(width, height int) {
	h.tm.Send(tea.WindowSizeMsg{Width: width, Height: height})
}

// selectName moves the cursor down to the named submodule.
func (h *harness) selectName(name string) {
	h.t.Helper()
	h.press("home")
	for range 50 {
		var current string
		h.probe(func(m Model) {
			if st, ok := m.selected(); ok {
				current = st.Submodule.Name
			}
		})
		if current == name {
			h.waitIdle()
			return
		}
		h.press("down")
	}
	h.t.Fatalf("submodule %s not found", name)
}

// finish quits the program with ctrl+c and returns the final model.
func (h *harness) finish() Model {
	h.t.Helper()
	h.press("ctrl+c")
	return h.final()
}

// final waits for the program to end and returns the final model.
func (h *harness) final() Model {
	h.t.Helper()
	fm := h.tm.FinalModel(h.t, teatest.WithFinalTimeout(waitTimeout))
	m, ok := fm.(Model)
	if !ok {
		h.t.Fatalf("final model is %T", fm)
	}
	return m
}
