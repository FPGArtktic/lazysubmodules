// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package main

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// openPTY opens a pseudo-terminal of 120x30 cells and returns its
// controlling side and the terminal. Both are closed when the test ends;
// the test is skipped when the system has no pseudo-terminals.
func openPTY(t *testing.T) (pty, tty *os.File) {
	t.Helper()
	pty, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrPermission) {
		t.Skipf("no pseudo-terminal: %v", err)
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pty.Close() })
	fd := int(pty.Fd())
	if err := unix.IoctlSetPointerInt(fd, unix.TIOCSPTLCK, 0); err != nil {
		t.Fatalf("unlock pseudo-terminal: %v", err)
	}
	n, err := unix.IoctlGetInt(fd, unix.TIOCGPTN)
	if err != nil {
		t.Fatalf("pseudo-terminal number: %v", err)
	}
	tty, err = os.OpenFile(fmt.Sprintf("/dev/pts/%d", n), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tty.Close() })
	ws := &unix.Winsize{Row: 30, Col: 120}
	if err := unix.IoctlSetWinsize(int(tty.Fd()), unix.TIOCSWINSZ, ws); err != nil {
		t.Fatalf("set terminal size: %v", err)
	}
	return pty, tty
}

// ptyProcess is a command running on a pseudo-terminal.
type ptyProcess struct {
	t    *testing.T
	cmd  *exec.Cmd
	pty  *os.File
	done chan struct{}

	mu     sync.Mutex
	output bytes.Buffer
}

// startOnPTY starts cmd with a new pseudo-terminal as its controlling
// terminal and standard streams. The output is collected until the
// process ends; the process is killed when the test ends.
func startOnPTY(t *testing.T, cmd *exec.Cmd) *ptyProcess {
	t.Helper()
	pty, tty := openPTY(t)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = tty, tty, tty
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	// Only the child keeps the terminal open, so reading ends when it exits.
	if err := tty.Close(); err != nil {
		t.Fatal(err)
	}
	p := &ptyProcess{t: t, cmd: cmd, pty: pty, done: make(chan struct{})}
	go func() {
		defer close(p.done)
		buf := make([]byte, 4096)
		for {
			n, err := pty.Read(buf)
			p.mu.Lock()
			p.output.Write(buf[:n])
			p.mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	return p
}

// Output returns what the process wrote so far.
func (p *ptyProcess) Output() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.output.String()
}

// wait waits for the process to end, typing keys every 200 ms meanwhile,
// and returns its exit status.
func (p *ptyProcess) wait(keys string) int {
	p.t.Helper()
	code, ok := p.waitWithin(20*time.Second, keys)
	if !ok {
		p.t.Fatalf("the process did not end; output:\n%q", p.Output())
	}
	return code
}

// waitWithin waits at most timeout for the process to end, typing keys
// every 200 ms meanwhile, and returns its exit status; ok is false when
// the process is still running.
func (p *ptyProcess) waitWithin(timeout time.Duration, keys string) (code int, ok bool) {
	p.t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		select {
		case <-p.done:
			// The terminal has no reader left; the process has exited.
			if err := p.cmd.Wait(); err != nil {
				if _, ok := errors.AsType[*exec.ExitError](err); !ok {
					p.t.Fatal(err)
				}
			}
			return p.cmd.ProcessState.ExitCode(), true
		case <-time.After(200 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			return 0, false
		}
		if keys != "" {
			// The process may have ended just now.
			_, _ = p.pty.WriteString(keys)
		}
	}
}
