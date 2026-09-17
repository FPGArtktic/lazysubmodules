// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package main

import (
	"context"
	"fmt"

	"github.com/FPGArtktic/lazysubmodules/internal/git"
	"github.com/FPGArtktic/lazysubmodules/internal/tui"
)

// tuiCommand returns the "tui" entry of the command table.
func tuiCommand() command {
	return command{
		name:    "tui",
		summary: "start the interactive terminal interface",
		help: "Start the terminal user interface. Standard input and output must be a\n" +
			"terminal that supports cursor movement (TERM set and not \"dumb\").\n" +
			"Git runs without the terminal there, so fetching cannot answer prompts: it\n" +
			"needs a credential helper or an SSH agent, and SSH host keys that are\n" +
			"already known.",
		run: runTUI,
	}
}

// tuiGitOptions returns the options of git while the interface owns the
// terminal: neither git nor the programs it starts, such as ssh, may prompt
// there.
func tuiGitOptions() []git.Option {
	return []git.Option{git.WithEnv("GIT_TERMINAL_PROMPT=0"), git.WithoutTerminal()}
}

// runTUI implements "tui".
func runTUI(ctx context.Context, e *env, c command, args []string) int {
	args, code, done := e.parseArgs(c, newFlagSet(c.name), args)
	if done {
		return code
	}
	if len(args) > 0 {
		return e.usageError(fmt.Sprintf("tui: unexpected argument %q", args[0]))
	}
	if msg := e.tuiUnsupported(); msg != "" {
		fmt.Fprintf(e.stderr, "%s: %s\n", progName, msg)
		return exitUsage
	}
	repo, err := e.openRepo(ctx, tuiGitOptions()...)
	if err != nil {
		return e.fail(err)
	}
	err = tui.Run(ctx, repo, tui.Options{
		Input:   e.stdin,
		Output:  e.stdout,
		Environ: e.environ,
		NoColor: e.noColor(),
	})
	if err != nil {
		return e.fail(err)
	}
	return exitOK
}

// tuiUnsupported explains why the interface cannot run on the terminal of
// e, or returns "". Without a terminal the screen has no size and keys
// cannot be read one by one; a terminal without a known type cannot show
// the selection, since all text attributes are removed for it.
func (e *env) tuiUnsupported() string {
	const msg = "tui requires a terminal"
	switch term := e.getenv("TERM"); {
	case !e.isTerminal(e.stdin) || !e.isTerminal(e.stdout):
		return msg
	case term == "":
		return msg + " (TERM is not set)"
	case term == "dumb":
		return msg + " with cursor movement (TERM is dumb)"
	}
	return ""
}
