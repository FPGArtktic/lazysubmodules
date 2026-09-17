// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package main

import (
	"context"
	"fmt"

	"github.com/FPGArtktic/lazysubmodules/internal/core"
)

// fetchCommand returns the "fetch" entry of the command table.
func fetchCommand() command {
	return command{
		name:     "fetch",
		synopsis: "[<name>...]",
		summary:  "download branches and tags of submodules (network)",
		help: "Clone and initialize submodules that are not initialized, then run\n" +
			"'git fetch --tags --force --prune origin' in each of them. Tags moved on\n" +
			"the remote replace the local ones, which lets status and verify detect\n" +
			"them. Without names, every managed submodule is fetched. Git's progress\n" +
			"output goes to standard error.",
		run: runFetch,
	}
}

// runFetch implements "fetch".
func runFetch(ctx context.Context, e *env, c command, args []string) int {
	names, code, done := e.parseArgs(c, newFlagSet(c.name), args)
	if done {
		return code
	}
	repo, err := e.openRepo(ctx)
	if err != nil {
		return e.fail(err)
	}
	results, err := repo.Fetch(ctx, names, e.stderr)
	if len(results) == 0 && err == nil {
		fmt.Fprintln(e.stdout, "no managed submodules")
	}
	for _, r := range results {
		fmt.Fprintf(e.stdout, "%s: %s\n", displayName(r.Submodule.Name), fetchResult(r))
	}
	if err != nil {
		return e.fail(err)
	}
	return exitOK
}

// fetchResult describes what the fetch did for one submodule.
func fetchResult(r core.FetchResult) string {
	switch {
	case r.Cloned:
		return "cloned and fetched"
	case r.Init:
		return "initialized and fetched"
	}
	return "fetched"
}

// foreachCommand returns the "foreach" entry of the command table.
func foreachCommand() command {
	return command{
		name:     "foreach",
		synopsis: "-- <command> [args...]",
		summary:  "run a command in every managed submodule",
		help: "Run a command, without a shell, in every managed submodule that is checked\n" +
			"out, in .gitmodules order. Uninitialized submodules are skipped with a note.\n" +
			"The first command that fails ends foreach with exit status 1. The command\n" +
			"gets the variables name, sm_path, displaypath, sha1, toplevel, LSM_MODE and\n" +
			"LSM_REF. The '--' may be left out when the command does not start with '-'.",
		run: runForeach,
	}
}

// runForeach implements "foreach". Its flags must come before the command,
// so that the options of the command need no "--".
func runForeach(ctx context.Context, e *env, c command, args []string) int {
	fs := newFlagSet(c.name)
	err := fs.Parse(args)
	argv, code, done := e.checkParse(c, fs, fs.Args(), err)
	switch {
	case done:
		return code
	case len(argv) == 0 || argv[0] == "":
		return e.usageError("foreach: missing command")
	}
	repo, err := e.openRepo(ctx)
	if err != nil {
		return e.fail(err)
	}
	err = repo.Foreach(ctx, core.ForeachOptions{
		Args:   argv,
		Stdin:  e.stdin,
		Stdout: e.stdout,
		Stderr: e.stderr,
	})
	if err != nil {
		return e.fail(err)
	}
	return exitOK
}
