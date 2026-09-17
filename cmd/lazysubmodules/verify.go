// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package main

import (
	"context"
	"fmt"
	"io"

	"github.com/FPGArtktic/lazysubmodules/internal/core"
)

// verifyCommand returns the "verify" entry of the command table.
func verifyCommand() command {
	return command{
		name:     "verify",
		synopsis: "[<name>...] [--signatures]",
		summary:  "check that lock file, gitlinks and checkouts agree",
		help: "Compare the lock commit, the gitlink in the HEAD commit of the superproject\n" +
			"and the submodule HEAD. For tags and tag patterns, also check that the\n" +
			"locked tag still resolves to the locked commit. Every failed check is\n" +
			"printed, and the exit status is 4 when any check fails. Without names,\n" +
			"every managed submodule is verified.",
		run: runVerify,
	}
}

// runVerify implements "verify".
func runVerify(ctx context.Context, e *env, c command, args []string) int {
	fs := newFlagSet(c.name)
	signatures := fs.Bool("signatures", false,
		"also verify the signature of the locked tag or commit")
	names, code, done := e.parseArgs(c, fs, args)
	if done {
		return code
	}
	repo, err := e.openRepo(ctx)
	if err != nil {
		return e.fail(err)
	}
	results, err := repo.Verify(ctx, names, core.VerifyOptions{Signatures: *signatures})
	if len(results) == 0 && err == nil {
		fmt.Fprintln(e.stdout, "no managed submodules")
	}
	for _, v := range results {
		writeVerifyResult(e.stdout, v)
	}
	if err != nil {
		return e.fail(err)
	}
	return exitOK
}

// writeVerifyResult prints a summary line for one submodule, followed by
// one indented line per failed check.
func writeVerifyResult(w io.Writer, v core.VerifyResult) {
	name := displayName(v.Submodule.Name)
	failed := 0
	for _, ch := range v.Checks {
		if !ch.OK {
			failed++
		}
	}
	switch {
	case v.OK():
		fmt.Fprintf(w, "%s: ok (%d checks)\n", name, len(v.Checks))
		return
	case failed == 0:
		fmt.Fprintf(w, "%s: failed (no check ran)\n", name)
		return
	}
	fmt.Fprintf(w, "%s: failed (%d of %d checks)\n", name, failed, len(v.Checks))
	for _, ch := range v.Checks {
		if !ch.OK {
			fmt.Fprintf(w, "  %s: %s\n", clean(ch.Name), clean(ch.Detail))
		}
	}
}
