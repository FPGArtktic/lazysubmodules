// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package tui

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
)

// syncBuffer is a bytes.Buffer safe for concurrent use.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

// Write implements io.Writer.
func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

// String returns the written text.
func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// testEnviron is the terminal environment of the Run tests.
func testEnviron() []string {
	return []string{"TERM=xterm-256color"}
}

// runAsync starts Run and returns its result channel and the input pipe.
// The pipe is closed when the test ends.
func runAsync(ctx context.Context, t *testing.T, b Backend, out io.Writer) (
	<-chan error, *io.PipeWriter,
) {
	t.Helper()
	in, keys := io.Pipe()
	t.Cleanup(func() { _ = keys.Close() })
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, b, Options{Input: in, Output: out, Environ: testEnviron()})
	}()
	return done, keys
}

// wait returns the result of Run.
func wait(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(waitTimeout):
		t.Fatal("Run did not return")
		return nil
	}
}

func TestRunQuit(t *testing.T) {
	t.Parallel()
	out := &syncBuffer{}
	err := Run(t.Context(), newFake(), Options{Input: strings.NewReader("q"), Output: out,
		Environ: testEnviron()})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out.String(), "\x1b[?1049h") {
		t.Errorf("no alternate screen in %q", out.String())
	}
}

func TestRunWaitsForInterruptedOperation(t *testing.T) {
	t.Parallel()
	f := newFake()
	f.gate, f.started = make(chan struct{}), make(chan string, 1)
	done, keys := runAsync(t.Context(), t, f, io.Discard)
	startUpdate(t, f, keys)
	if _, err := keys.Write([]byte("\x03")); err != nil {
		t.Fatal(err)
	}
	// The screen was closed before the update ended, so Run reports its
	// failure.
	err := wait(t, done)
	if !errors.Is(err, context.Canceled) || err.Error() != "updating kernel: context canceled" {
		t.Errorf("Run = %v, want the interrupted update", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.interrupted) != 1 || !errors.Is(f.interrupted[0], context.Canceled) {
		t.Errorf("Run returned before the update saw the cancellation: %v", f.interrupted)
	}
}

// TestRunReportsLateOutcome quits while an update runs that does not stop
// on the cancellation: its outcome is reported after the screen is closed.
func TestRunReportsLateOutcome(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		updateErr error
		wantErr   string
		wantOut   string
	}{
		{"success", nil, "", "kernel: updated to v6.6.9 (e4f5a6b), staged\n"},
		{"failure", errFake, "updating kernel: fake failure", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newFake()
			f.gate, f.started, f.holdOnCancel = make(chan struct{}), make(chan string, 1), true
			f.updateErr = tc.updateErr
			out := &syncBuffer{}
			done, keys := runAsync(t.Context(), t, f, out)
			startUpdate(t, f, keys)
			if _, err := keys.Write([]byte("qy")); err != nil {
				t.Fatal(err)
			}
			// Run waits for the update once the screen is closed.
			deadline := time.Now().Add(waitTimeout)
			for !strings.Contains(out.String(), "\x1b[?1049l") {
				if time.Now().After(deadline) {
					t.Fatalf("the screen was not closed: %q", out.String())
				}
				time.Sleep(5 * time.Millisecond)
			}
			select {
			case err := <-done:
				t.Fatalf("Run returned before the update ended: %v", err)
			case <-time.After(50 * time.Millisecond):
			}
			close(f.gate)
			err := wait(t, done)
			if tc.wantErr == "" && err != nil || tc.wantErr != "" &&
				(err == nil || err.Error() != tc.wantErr || !errors.Is(err, errFake)) {
				t.Errorf("Run = %v, want %q", err, tc.wantErr)
			}
			_, after, _ := strings.Cut(out.String(), "\x1b[?1049l")
			if !strings.HasSuffix(after, tc.wantOut) || tc.wantOut == "" &&
				strings.Contains(after, "kernel") {
				t.Errorf("output after the screen %q, want it to end with %q", after, tc.wantOut)
			}
		})
	}
}

// startUpdate types keys until the update of kernel is held by the fake.
// The keys are repeated until the status has loaded and the confirmation
// is ready; before that, u finds no submodule and y is ignored.
func startUpdate(t *testing.T, f *fakeBackend, keys io.Writer) {
	t.Helper()
	deadline := time.Now().Add(waitTimeout)
	for {
		if _, err := keys.Write([]byte("uy")); err != nil {
			t.Fatal(err)
		}
		select {
		case <-f.started:
			return
		case <-time.After(20 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			t.Fatal("the update did not start")
		}
	}
}

func TestRunCancelled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	f := newFake()
	done, _ := runAsync(ctx, t, f, io.Discard)
	for f.countCalls("Status") == 0 {
		time.Sleep(time.Millisecond)
	}
	cancel()
	err := wait(t, done)
	if !errors.Is(err, context.Canceled) || !errors.Is(err, tea.ErrProgramKilled) ||
		err.Error() != "terminal interface: context canceled" {
		t.Errorf("Run = %v, want the cancellation", err)
	}
}

// TestRunCancelCause checks that the cause of the cancellation, such as a
// signal, explains the end of the program.
func TestRunCancelCause(t *testing.T) {
	t.Parallel()
	cause := errors.New("terminated signal received")
	ctx, cancel := context.WithCancelCause(t.Context())
	f := newFake()
	done, _ := runAsync(ctx, t, f, io.Discard)
	for f.countCalls("Status") == 0 {
		time.Sleep(time.Millisecond)
	}
	cancel(cause)
	err := wait(t, done)
	if !errors.Is(err, cause) || !errors.Is(err, tea.ErrProgramKilled) ||
		err.Error() != "terminal interface: terminated signal received" {
		t.Errorf("Run = %v, want the cause", err)
	}
}

func TestColors(t *testing.T) {
	t.Parallel()
	tty := []string{"TERM=xterm-256color", "TTY_FORCE=1"}
	for _, tc := range []struct {
		name        string
		environ     []string
		noColor     bool
		wantProfile colorprofile.Profile
		wantPlain   bool
	}{
		{"terminal", tty, false, colorprofile.ANSI256, false},
		{"truecolor", append(tty, "COLORTERM=truecolor"), false, colorprofile.TrueColor, false},
		{"option", append(tty, "COLORTERM=truecolor"), true, colorprofile.ASCII, true},
		{"NO_COLOR=1", append(tty, "NO_COLOR=1"), false, colorprofile.ASCII, true},
		{"dumb", []string{"TERM=dumb", "TTY_FORCE=1"}, false, colorprofile.NoTTY, true},
		{"dumb and option", []string{"TERM=dumb", "TTY_FORCE=1"}, true, colorprofile.NoTTY, true},
		{"not a terminal", []string{"TERM=xterm-256color"}, false, colorprofile.NoTTY, true},
	} {
		profile, plain := colors(io.Discard, tc.environ, tc.noColor)
		if profile != tc.wantProfile || plain != tc.wantPlain {
			t.Errorf("%s: colors = %v, %t; want %v, %t", tc.name, profile, plain,
				tc.wantProfile, tc.wantPlain)
		}
	}
}
