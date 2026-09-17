// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/FPGArtktic/lazysubmodules/internal/core"
	"github.com/FPGArtktic/lazysubmodules/internal/git"
)

// Exit statuses, documented in README.md.
const (
	exitOK      = 0
	exitError   = 1
	exitUsage   = 2
	exitRefused = 3
	exitVerify  = 4
	exitGit     = 5
)

// exitCode maps the error of a command to the exit status. Invalid values
// given by the user are usage errors; the other classes follow the error
// wrapping of the core and git packages.
func exitCode(err error) int {
	var gitErr *git.Error
	switch {
	case err == nil:
		return exitOK
	case errors.Is(err, core.ErrInvalidArgument):
		return exitUsage
	case errors.Is(err, core.ErrRefused):
		return exitRefused
	case errors.Is(err, core.ErrVerify):
		return exitVerify
	case errors.As(err, &gitErr):
		return exitGit
	}
	return exitError
}

// fail reports err on standard error and returns its exit status. Every
// line of a joined error gets its own prefix.
func (e *env) fail(err error) int {
	for line := range strings.SplitSeq(err.Error(), "\n") {
		fmt.Fprintf(e.stderr, "%s: %s\n", progName, clean(line))
	}
	return exitCode(err)
}

// usageError reports a command line error and returns the exit status of a
// usage error.
func (e *env) usageError(msg string) int {
	fmt.Fprintf(e.stderr, "%s: %s\nRun '%s help' for usage.\n", progName, clean(msg), progName)
	return exitUsage
}
