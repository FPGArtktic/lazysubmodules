// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package git

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// procDir is the mount point of the Linux proc file system.
const procDir = "/proc"

// signalTree sends sig to the process pid and to all its descendants.
//
// Signaling git alone is not enough: "git submodule" runs a shell script
// that starts "git submodule--helper" without exec, which in turn runs "git
// clone" and a transport such as ssh. When only git is signaled, the script
// exits and its children keep running, holding the output pipes open. The
// descendants are therefore collected before any signal is sent, while their
// parent links are intact. Signals to descendants that exited meanwhile are
// ignored.
//
// Return: the result of signaling pid.
func signalTree(pid int, sig syscall.Signal) error {
	children := descendants(pid)
	err := syscall.Kill(pid, sig)
	for _, child := range children {
		_ = syscall.Kill(child, sig)
	}
	if err != nil {
		return os.NewSyscallError("kill", err)
	}
	return nil
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
		if parent, ok := parentPID(stat); ok {
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

// parentPID reads the parent process ID from a /proc/<pid>/stat file, whose
// content is "<pid> (<comm>) <state> <ppid> ...". The command name may
// contain spaces and parentheses, so the fields after the last ")" are used.
func parentPID(stat string) (int, bool) {
	data, err := os.ReadFile(stat)
	if err != nil {
		return 0, false
	}
	i := strings.LastIndexByte(string(data), ')')
	if i < 0 {
		return 0, false
	}
	fields := strings.Fields(string(data[i+1:]))
	if len(fields) < 2 {
		return 0, false
	}
	ppid, err := strconv.Atoi(fields[1])
	return ppid, err == nil
}
