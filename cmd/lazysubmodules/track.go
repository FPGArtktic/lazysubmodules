// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package main

import (
	"context"
	"fmt"

	"github.com/FPGArtktic/lazysubmodules/internal/core"
	"github.com/FPGArtktic/lazysubmodules/internal/manifest"
)

// trackingSynopsis is the synopsis of the tracking flags.
const trackingSynopsis = "(--branch B | --tag T | --tag-pattern P | --commit SHA)"

// addCommand returns the "add" entry of the command table.
func addCommand() command {
	return command{
		name:     "add",
		synopsis: "<url> <path> " + trackingSynopsis,
		summary:  "clone a new submodule that tracks a ref (network)",
		help: "Clone a repository with 'git submodule add' as a new submodule named after\n" +
			"its path, write its tracking configuration, check out the resolved commit,\n" +
			"write .lsm.lock and stage the result. Exactly one of --branch, --tag,\n" +
			"--tag-pattern and --commit is required. If a step after the clone fails,\n" +
			"the new submodule is removed again.",
		run: runAdd,
	}
}

// runAdd implements "add".
func runAdd(ctx context.Context, e *env, c command, args []string) int {
	fs := newFlagSet(c.name)
	track := addTrackingFlags(fs)
	prerelease := fs.Bool("include-prerelease", false,
		"let a tag pattern select a pre-release tag")
	args, code, done := e.parseArgs(c, fs, args)
	if done {
		return code
	}
	switch {
	case len(args) == 0:
		return e.usageError("add: missing <url> and <path>")
	case len(args) == 1:
		return e.usageError("add: missing <path>")
	case len(args) > 2:
		return e.usageError(fmt.Sprintf("add: unexpected argument %q", args[2]))
	}
	mode, ref, err := track.mode()
	if err != nil {
		return e.usageError("add: " + err.Error())
	}
	repo, err := e.openRepo(ctx)
	if err != nil {
		return e.fail(err)
	}
	ch, err := repo.Add(ctx, core.AddOptions{
		URL:               args[0],
		Path:              args[1],
		Mode:              mode,
		Ref:               ref,
		IncludePrerelease: *prerelease,
		Progress:          e.stderr,
	})
	if err != nil {
		return e.fail(err)
	}
	fmt.Fprintf(e.stdout, "%s: added at %s\n", displayName(ch.Submodule.Name), newSide(ch))
	return exitOK
}

// setCommand returns the "set" entry of the command table.
func setCommand() command {
	return command{
		name:     "set",
		synopsis: "<name> " + trackingSynopsis,
		summary:  "change what a submodule tracks",
		help: "Change the tracking configuration of a submodule in .gitmodules, and nothing\n" +
			"else; run 'update' afterwards. An unmanaged submodule becomes managed.\n" +
			"Exactly one of --branch, --tag, --tag-pattern and --commit is required;\n" +
			"an abbreviated commit is expanded when it is available locally.",
		run: runSet,
	}
}

// runSet implements "set".
func runSet(ctx context.Context, e *env, c command, args []string) int {
	fs := newFlagSet(c.name)
	track := addTrackingFlags(fs)
	args, code, done := e.parseArgs(c, fs, args)
	if done {
		return code
	}
	switch {
	case len(args) == 0:
		return e.usageError("set: missing <name>")
	case len(args) > 1:
		return e.usageError(fmt.Sprintf("set: unexpected argument %q", args[1]))
	}
	mode, ref, err := track.mode()
	if err != nil {
		return e.usageError("set: " + err.Error())
	}
	repo, err := e.openRepo(ctx)
	if err != nil {
		return e.fail(err)
	}
	sub, err := repo.Set(ctx, args[0], mode, ref)
	if err != nil {
		return e.fail(err)
	}
	fmt.Fprintf(e.stdout, "%s: tracks %s %s\n", displayName(sub.Name),
		mode, trackedRef(sub, mode, ref))
	return exitOK
}

// trackedRef returns the ref that set stored, which in commit mode may be
// the expansion of the given one.
func trackedRef(sub manifest.Submodule, mode manifest.Mode, given string) string {
	if sub.Mode == mode && sub.Ref != "" {
		return clean(sub.Ref)
	}
	return clean(given)
}
