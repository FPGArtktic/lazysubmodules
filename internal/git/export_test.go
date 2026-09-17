// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package git

import (
	"os"
	"syscall"
	"time"
)

// SignalTreeWithin is SignalTree with another limit for freezing the
// process tree, so that a test does not depend on the load of the machine.
//
// Context: tests of the package only.
// Return: as SignalTree.
func SignalTreeWithin(p *os.Process, sig syscall.Signal, timeout time.Duration) error {
	return signalTree(p, sig, timeout)
}
