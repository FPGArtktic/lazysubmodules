// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package git

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	// procDir is the mount point of the Linux proc file system.
	procDir = "/proc"
	// freezeTimeout bounds how long SignalTree waits for the processes it
	// stops to reach a state in which they cannot start processes.
	freezeTimeout = time.Second
	// uninterruptibleWait bounds how long SignalTree waits for a process in
	// an uninterruptible wait, which stops only once the wait ends.
	uninterruptibleWait = 200 * time.Millisecond
	// stopPollInterval is the delay between two checks of that state, and
	// how long a process that blocks the signal may run.
	stopPollInterval = time.Millisecond
)

// SignalTree sends sig to the process p and to all its descendants.
//
// Signaling a process alone is often not enough: "git submodule" runs a
// shell script that starts "git submodule--helper" without exec, which in
// turn runs "git clone" and a transport such as ssh. When only git is
// signaled, the script exits and its children keep running, holding the
// output pipes open, so that exec.Cmd.Wait blocks until its WaitDelay has
// passed.
//
// The descendants are found in /proc through their parent links, which a
// process loses when its parent exits, and a process may start another one
// at any time. The tree is therefore frozen first: p is stopped with
// SIGSTOP, then every descendant that a scan of /proc finds once the
// processes stopped before have stopped, until a scan finds no new one. A
// stopped process neither starts nor reaps processes, so the scans miss
// none and the process IDs found stay valid. A process in an uninterruptible
// wait, which may be one that is starting a process, stops only when the
// wait ends; it is waited for 200 ms at most, since the wait may last, for
// example for a child started with vfork. A process that blocks sig when it
// stops, as bash and git do while they start a process, is let run for a
// moment and stopped again until it does not: a process started then would
// not inherit the pending signal. Then the descendants, children first, and
// p get sig, and all of them get SIGCONT in the same order, so that they
// can handle sig, for example by removing lock files. The freezing ends
// after one second in any case. The process group is left alone, so an
// interactive command stays in the foreground group of its terminal.
//
// Context: Linux; p is a child of the caller, such as the process of an
// exec.Cmd in its Cancel function; sig ends processes, such as SIGTERM or
// SIGKILL. A process that ignores sig or blocks it for longer keeps
// running, and so may the processes it starts later.
// Return: nil; an error wrapping os.ErrProcessDone, without signaling any
// process, when p has been waited for (exec.Cmd.Cancel may run after that,
// and exec.Cmd ignores this error); or another error from signaling p.
func SignalTree(p *os.Process, sig syscall.Signal) error {
	return signalTree(p, sig, freezeTimeout)
}

// signalTree implements SignalTree; the freezing ends after timeout.
func signalTree(p *os.Process, sig syscall.Signal, timeout time.Duration) error {
	if err := p.Signal(syscall.SIGSTOP); err != nil {
		return err
	}
	t := &procTree{root: p, sig: sig}
	t.freeze(time.Now().Add(timeout))
	slices.Reverse(t.pids)
	for _, pid := range t.pids {
		_ = syscall.Kill(pid, sig)
	}
	err := p.Signal(sig)
	for _, pid := range t.pids {
		_ = syscall.Kill(pid, syscall.SIGCONT)
	}
	if contErr := p.Signal(syscall.SIGCONT); err == nil {
		err = contErr
	}
	return err
}

// procTree is a process and its descendants while SignalTree freezes them.
type procTree struct {
	root *os.Process
	sig  syscall.Signal
	// pids lists the descendants found, parents before children.
	pids []int
}

// freeze stops the descendants of the root, which has been sent SIGSTOP,
// and records them, until the deadline. A process ID in /proc names a
// descendant of the root only while the root has not been waited for; once
// it has, the scans end. A process that cannot be stopped, for example one
// running as another user, is not waited for.
func (t *procTree) freeze(deadline time.Time) {
	seen := map[int]bool{t.root.Pid: true}
	stopping := []int{t.root.Pid}
	for {
		t.settle(stopping, deadline)
		var found []int
		for _, pid := range descendants(t.root.Pid) {
			if !seen[pid] {
				seen[pid] = true
				found = append(found, pid)
			}
		}
		if len(found) == 0 || t.root.Signal(syscall.Signal(0)) != nil {
			return
		}
		stopping = stopping[:0]
		for _, pid := range found {
			if syscall.Kill(pid, syscall.SIGSTOP) == nil {
				stopping = append(stopping, pid)
			}
		}
		t.pids = append(t.pids, found...)
		if !time.Now().Before(deadline) {
			return
		}
	}
}

// settle waits until no process in pids runs, and no stopped one blocks the
// signal of the tree, or until the deadline. A stopped process that blocks
// the signal is continued for a moment and stopped again; one still in an
// uninterruptible wait would not get to run.
func (t *procTree) settle(pids []int, deadline time.Time) {
	for {
		waitStopped(pids, deadline)
		var blocked []int
		for _, pid := range pids {
			if state(pid) == 'T' && blocks(pid, t.sig) {
				blocked = append(blocked, pid)
			}
		}
		if len(blocked) == 0 || !time.Now().Before(deadline) {
			return
		}
		for _, pid := range blocked {
			_ = t.kill(pid, syscall.SIGCONT)
		}
		time.Sleep(stopPollInterval)
		for _, pid := range blocked {
			_ = t.kill(pid, syscall.SIGSTOP)
		}
		pids = blocked
	}
}

// kill sends sig to a process of the tree; the root is signaled through its
// os.Process, since its ID is freed when the caller waits for it.
func (t *procTree) kill(pid int, sig syscall.Signal) error {
	if pid == t.root.Pid {
		return t.root.Signal(sig)
	}
	return syscall.Kill(pid, sig)
}

// waitStopped waits until no process in pids runs, or until the deadline. A
// process in an uninterruptible wait counts as running for
// uninterruptibleWait.
func waitStopped(pids []int, deadline time.Time) {
	pids = slices.Clone(pids)
	patience := time.Now().Add(uninterruptibleWait)
	for {
		waitUninterruptible := time.Now().Before(patience)
		pids = slices.DeleteFunc(pids, func(pid int) bool {
			return !running(pid, waitUninterruptible)
		})
		if len(pids) == 0 || !time.Now().Before(deadline) {
			return
		}
		time.Sleep(stopPollInterval)
	}
}

// running reports whether the process pid runs or sleeps interruptibly, or,
// with uninterruptible, sleeps uninterruptibly: such a process may start or
// reap a process before it stops.
func running(pid int, uninterruptible bool) bool {
	return runningState(state(pid), uninterruptible)
}

// runningState implements running for a process state as /proc shows it.
func runningState(s byte, uninterruptible bool) bool {
	switch s {
	case 'R', 'S':
		return true
	case 'D':
		return uninterruptible
	}
	return false
}

// state returns the state of the process pid as /proc shows it, such as 'R'
// (running), 'S' (sleeping) or 'T' (stopped), or 0 when the process cannot
// be inspected, for example because it has ended.
func state(pid int) byte {
	s, _, _ := readStat(procFile(pid, "stat"))
	return s
}

// blocks reports whether the main thread of the process pid blocks sig. A
// process that cannot be inspected blocks nothing.
func blocks(pid int, sig syscall.Signal) bool {
	mask, ok := blockedSignals(procFile(pid, "status"))
	return ok && sig > 0 && mask&(1<<(sig-1)) != 0
}

// blockedSignals reads the mask of blocked signals from a
// /proc/<pid>/status file, where the line "SigBlk:" holds it in
// hexadecimal, with bit n-1 standing for signal n.
func blockedSignals(status string) (uint64, bool) {
	data, err := os.ReadFile(status)
	if err != nil {
		return 0, false
	}
	for line := range strings.Lines(string(data)) {
		if value, found := strings.CutPrefix(line, "SigBlk:"); found {
			mask, err := strconv.ParseUint(strings.TrimSpace(value), 16, 64)
			return mask, err == nil
		}
	}
	return 0, false
}

// procFile returns the path of a file in the /proc directory of a process.
func procFile(pid int, name string) string {
	return filepath.Join(procDir, strconv.Itoa(pid), name)
}

// descendants returns the processes below pid, parents before their
// children. Processes that cannot be inspected are left out; without a proc
// file system the result is empty.
func descendants(pid int) []int {
	stats, err := filepath.Glob(filepath.Join(procDir, "[0-9]*", "stat"))
	if err != nil {
		return nil
	}
	children := make(map[int][]int)
	for _, stat := range stats {
		child, err := strconv.Atoi(filepath.Base(filepath.Dir(stat)))
		if err != nil {
			continue
		}
		if _, parent, ok := readStat(stat); ok {
			children[parent] = append(children[parent], child)
		}
	}
	// The snapshot is not atomic: with reused process IDs the links may form
	// a cycle, so every process is visited once.
	seen := map[int]bool{pid: true}
	var found []int
	queue := children[pid]
	for len(queue) > 0 {
		next := queue[0]
		queue = queue[1:]
		if seen[next] {
			continue
		}
		seen[next] = true
		found = append(found, next)
		queue = append(queue, children[next]...)
	}
	return found
}

// readStat reads the state and the parent process ID from a /proc/<pid>/stat
// file, whose content is "<pid> (<comm>) <state> <ppid> ...". The command
// name may contain spaces and parentheses, so the fields after the last ")"
// are used.
func readStat(stat string) (byte, int, bool) {
	data, err := os.ReadFile(stat)
	if err != nil {
		return 0, 0, false
	}
	i := strings.LastIndexByte(string(data), ')')
	if i < 0 {
		return 0, 0, false
	}
	fields := strings.Fields(string(data[i+1:]))
	if len(fields) < 2 || len(fields[0]) != 1 {
		return 0, 0, false
	}
	ppid, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0, 0, false
	}
	return fields[0][0], ppid, true
}
