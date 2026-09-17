// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package git_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/FPGArtktic/lazysubmodules/internal/git"
)

func TestSignalTreeReachesDescendants(t *testing.T) {
	t.Parallel()
	ready := filepath.Join(t.TempDir(), "ready")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	// The shell waits for a background sleep that shares its output pipe. If
	// only the shell got the signal, the sleep would hold the pipe open and
	// Wait would return only after WaitDelay.
	cmd := exec.CommandContext(ctx, "sh", "-c",
		`trap 'echo got-term; exit 3' TERM; sleep 30 & : >"$1"; wait`, "sh", ready)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Cancel = func() error {
		return git.SignalTree(cmd.Process, syscall.SIGTERM)
	}
	cmd.WaitDelay = 20 * time.Second
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waitForFile(t, ready)
	start := time.Now()
	cancel()
	err := cmd.Wait()
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("Wait returned after %v", elapsed)
	}
	if exitErr, ok := errors.AsType[*exec.ExitError](err); !ok || exitErr.ExitCode() != 3 {
		t.Errorf("Wait = %v, want exit status 3 from the trap", err)
	}
	if got := out.String(); got != "got-term\n" {
		t.Errorf("output = %q, want the trap message", got)
	}
}

func TestSignalTreeWaitedProcess(t *testing.T) {
	t.Parallel()
	// Once a process has been waited for, its ID may belong to another one.
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	if err := git.SignalTree(cmd.Process, syscall.SIGTERM); !errors.Is(err, os.ErrProcessDone) {
		t.Errorf("SignalTree(waited) = %v, want os.ErrProcessDone", err)
	}
	// Linux process IDs never exceed 1<<22.
	p, err := os.FindProcess(1<<22 + 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := git.SignalTree(p, syscall.SIGTERM); !errors.Is(err, os.ErrProcessDone) {
		t.Errorf("SignalTree(unused) = %v, want os.ErrProcessDone", err)
	}
}

func TestSignalTreeCancelAtExit(t *testing.T) {
	t.Parallel()
	// exec.Cmd may cancel a command after it has waited for the process. The
	// cancellation must not turn a successful command into a failure.
	for i := range 100 {
		ctx, cancel := context.WithCancel(t.Context())
		cmd := exec.CommandContext(ctx, "true")
		cmd.Cancel = func() error {
			return git.SignalTree(cmd.Process, syscall.SIGTERM)
		}
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		proc := filepath.Join("/proc", strconv.Itoa(cmd.Process.Pid))
		go func() {
			defer cancel()
			for {
				if _, err := os.Stat(proc); err != nil {
					return
				}
				runtime.Gosched()
			}
		}()
		err := cmd.Wait()
		cancel()
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("run %d: Wait = %v, want nil or context.Canceled", i, err)
		}
	}
}

func TestSignalTreeStopsNewProcesses(t *testing.T) {
	t.Parallel()
	// Bash blocks SIGTERM while it starts a process, which then does not
	// inherit the pending signal: the loop starts processes all the time,
	// so a signal that lands in between, or a scan of /proc that is not
	// repeated, leaves a sleep running.
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not installed")
	}
	for run := range 10 {
		marker := newMarker(t)
		cmd := exec.Command(bash, "-c", "while :; do sleep 3600 & done")
		cmd.Env = markedEnv(marker)
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Duration(run) * time.Millisecond)
		if err := git.SignalTree(cmd.Process, syscall.SIGTERM); err != nil {
			t.Errorf("run %d: SignalTree = %v", run, err)
		}
		if err := cmd.Wait(); !killedBy(err, syscall.SIGTERM) {
			t.Errorf("run %d: Wait = %v, want the signal", run, err)
		}
		wantNoProcess(t, marker)
	}
}

// blockedFork is a perl script that blocks SIGTERM, creates the file
// $ARGV[0], and then, while the signal is still blocked, starts "sleep" in
// a new process, as bash and git do it: the new process unblocks the signal
// before it runs sleep. Then the script unblocks the signal and waits.
const blockedFork = `use POSIX;
my $term = POSIX::SigSet->new(SIGTERM);
sigprocmask(SIG_BLOCK, $term) or die "block: $!";
open(my $ready, ">", $ARGV[0]) or die "ready: $!";
close($ready);
select(undef, undef, undef, 0.05);
my $pid = fork() // die "fork: $!";
if ($pid == 0) {
	sigprocmask(SIG_UNBLOCK, $term) or die "unblock: $!";
	exec("sleep", "3600") or die "exec: $!";
}
sigprocmask(SIG_UNBLOCK, $term) or die "unblock: $!";
sleep(30);
`

func TestSignalTreeBlockedSignal(t *testing.T) {
	t.Parallel()
	// A process started while the signal is blocked in its parent does not
	// inherit the pending signal, and the parent exits as soon as it
	// unblocks the signal: the sleep would be left running unless the
	// script is let run until it unblocks the signal.
	perl, err := exec.LookPath("perl")
	if err != nil {
		t.Skip("perl is not installed")
	}
	for run := range 3 {
		ready := filepath.Join(t.TempDir(), "ready")
		marker := newMarker(t)
		var stderr bytes.Buffer
		cmd := exec.Command(perl, "-e", blockedFork, ready)
		cmd.Env = markedEnv(marker)
		cmd.Stderr = &stderr
		// A sleep left running would hold the pipe of stderr open.
		cmd.WaitDelay = time.Second
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		waitForFile(t, ready)
		if err := git.SignalTreeWithin(cmd.Process, syscall.SIGTERM, time.Minute); err != nil {
			t.Errorf("run %d: SignalTree = %v", run, err)
		}
		if err := cmd.Wait(); !killedBy(err, syscall.SIGTERM) {
			t.Errorf("run %d: Wait = %v, want the signal; stderr: %s", run, err, &stderr)
		}
		wantNoProcess(t, marker)
	}
}

// killedBy reports whether err reports a process that sig ended.
func killedBy(err error, sig syscall.Signal) bool {
	exitErr, ok := errors.AsType[*exec.ExitError](err)
	if !ok {
		return false
	}
	status, ok := exitErr.Sys().(syscall.WaitStatus)
	return ok && status.Signaled() && status.Signal() == sig
}

// markerVar is an environment variable that marks the processes a test
// starts. The processes they start inherit it, even before they run
// another program, so a process left running can be told by it.
const markerVar = "LSM_TEST_MARKER"

// newMarker returns a value of markerVar that no other test uses.
func newMarker(t *testing.T) string {
	return fmt.Sprintf("%s/%d/%d", t.Name(), os.Getpid(), time.Now().UnixNano())
}

// markedEnv returns the environment of the test process with the marker.
func markedEnv(marker string) []string {
	return append(os.Environ(), markerVar+"="+marker)
}

// wantNoProcess checks that no process with the marker is left once the
// processes that are exiting are gone, and kills those that are.
func wantNoProcess(t *testing.T, marker string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		left := markedProcesses(marker)
		if len(left) == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Errorf("processes left running: %v", left)
			for _, pid := range left {
				_ = syscall.Kill(pid, syscall.SIGKILL)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// markedProcesses lists the processes whose environment holds the marker.
// Zombies have an empty environment.
func markedProcesses(marker string) []int {
	envs, err := filepath.Glob("/proc/[0-9]*/environ")
	if err != nil {
		return nil
	}
	var found []int
	for _, env := range envs {
		data, err := os.ReadFile(env)
		if err != nil || !slices.Contains(strings.Split(string(data), "\x00"), markerVar+"="+marker) {
			continue
		}
		if pid, err := strconv.Atoi(filepath.Base(filepath.Dir(env))); err == nil {
			found = append(found, pid)
		}
	}
	return found
}
