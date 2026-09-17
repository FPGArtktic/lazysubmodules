// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package main

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/FPGArtktic/lazysubmodules/internal/core"
	"github.com/FPGArtktic/lazysubmodules/internal/manifest"
)

// updateCommand returns the "update" entry of the command table.
func updateCommand() command {
	return command{
		name:     "update",
		synopsis: "[<name>...] [--fetch] [--dry-run] [--commit] [--include-prerelease]",
		summary:  "move submodules to the refs they track and stage the result",
		help: "Resolve the tracked ref of managed submodules in the local refs, check out\n" +
			"the commit, update .lsm.lock and stage the gitlink, .gitmodules and\n" +
			".lsm.lock. Without names, every managed submodule is updated.\n" +
			"Uninitialized submodules are initialized, offline when their repository\n" +
			"still exists. Nothing is modified when a selected submodule has\n" +
			"uncommitted changes, a missing ref or needs a clone without --fetch.",
		run: runUpdate,
	}
}

// runUpdate implements "update".
func runUpdate(ctx context.Context, e *env, c command, args []string) int {
	fs := newFlagSet(c.name)
	fetch := fs.Bool("fetch", false, "fetch first; clone uninitialized submodules if needed")
	dryRun := fs.Bool("dry-run", false, "print the planned changes and modify nothing")
	commitResult := fs.Bool("commit", false, "create one commit with the result")
	prerelease := fs.Bool("include-prerelease", false,
		"let tag patterns select pre-release tags")
	names, code, done := e.parseArgs(c, fs, args)
	if done {
		return code
	}
	repo, err := e.openRepo(ctx)
	if err != nil {
		return e.fail(err)
	}
	opts := core.UpdateOptions{
		Names:             names,
		Fetch:             *fetch,
		DryRun:            *dryRun,
		Commit:            *commitResult,
		IncludePrerelease: *prerelease,
		Progress:          e.stderr,
	}
	res, err := repo.Update(ctx, opts)
	writeUpdate(e.stdout, opts, res, err)
	if err != nil {
		return e.fail(err)
	}
	return exitOK
}

// writeUpdate prints one line per submodule and what happened to the
// commit; err is the error of the update. A commit, and the subject that a
// dry run shows, describe only the submodules whose recorded state changes
// (see core.Change.RecordChanged), not those that are only initialized or
// checked out.
func writeUpdate(w io.Writer, opts core.UpdateOptions, res core.UpdateResult, err error) {
	if len(res.Changes) == 0 && err == nil {
		fmt.Fprintln(w, "no managed submodules")
		return
	}
	for _, ch := range res.Changes {
		fmt.Fprintln(w, changeLine(ch, opts.DryRun))
	}
	recorded := slices.ContainsFunc(res.Changes, core.Change.RecordChanged)
	switch {
	case res.Commit != "":
		fmt.Fprintf(w, "committed %s\n", res.Commit)
	case !opts.Commit || err != nil:
		// Nothing to report, or the error explains it.
	case !opts.DryRun || !recorded:
		fmt.Fprintln(w, "nothing to commit")
	case slices.ContainsFunc(res.Changes, unknownTarget):
		fmt.Fprintln(w, "would commit the result")
	default:
		subject, _, _ := strings.Cut(core.CommitMessage(res.Changes), "\n")
		fmt.Fprintf(w, "would commit %q\n", clean(subject))
	}
}

// unknownTarget reports whether a dry run could not resolve the target of
// a change.
func unknownTarget(c core.Change) bool {
	return c.Changed() && c.New.Commit == ""
}

// changeLine describes the update of one submodule, such as
// "kernel: v6.6.8 (a1b2c3d) -> v6.6.9 (e4f5a6b)". The old side is what the
// superproject recorded, as in the commit message; notes tell what else
// happens to the submodule: "recorded again" when the superproject records
// the same commit anew; "restored <files>" when the superproject records
// the target already and the working tree copies of these files are
// rewritten to match it (see core.Change.RestoredFiles); and "discarded the
// staged change" when --commit stages what HEAD records for the submodule
// in place of a different staged gitlink, lock entry or tracking keys (see
// core.Change.RestoresIndex).
func changeLine(c core.Change, dryRun bool) string {
	name := displayName(c.Submodule.Name)
	if !c.Changed() {
		return name + ": up to date"
	}
	from, to := oldSide(c), newSide(c)
	if c.Old != nil && c.Old.Commit == c.OldGitlink && c.New.Commit != "" &&
		c.Old.Mode != c.New.Mode {
		from, to = string(c.Old.Mode)+" "+from, string(c.New.Mode)+" "+to
	}
	text := to
	if from != to {
		text = from + " -> " + to
	}
	var notes []string
	switch {
	case c.Clone:
		notes = append(notes, pick(dryRun, "clone", "cloned"))
	case c.Init:
		notes = append(notes, pick(dryRun, "initialize", "initialized"))
	case c.OldHead != "" && c.OldHead != c.OldGitlink && c.OldHead != c.New.Commit &&
		c.New.Commit != "":
		notes = append(notes, pick(dryRun, "HEAD is ", "HEAD was ")+short(c.OldHead))
	case from == to && c.RecordChanged():
		notes = append(notes, pick(dryRun, "record again", "recorded again"))
	}
	if files := c.RestoredFiles(); len(files) > 0 {
		notes = append(notes, pick(dryRun, "restore ", "restored ")+strings.Join(files, " and "))
	}
	if c.RestoresIndex() {
		notes = append(notes, pick(dryRun, "discard", "discarded")+" the staged change")
	}
	text = strings.Join(append([]string{text}, notes...), ", ")
	if dryRun {
		return "would update " + name + ": " + text
	}
	return name + ": " + text
}

// pick returns dry in a dry run and done otherwise.
func pick(dryRun bool, dry, done string) string {
	if dryRun {
		return dry
	}
	return done
}

// oldSide describes what the superproject recorded before the update:
// the locked ref and the gitlink, "<commit> (unlocked)" without a lock
// entry, or "none" without a gitlink.
func oldSide(c core.Change) string {
	switch {
	case c.OldGitlink == "":
		return "none"
	case c.Old == nil:
		return short(c.OldGitlink) + " (unlocked)"
	case c.Old.Commit == c.OldGitlink:
		return refAt(c.Old.Mode, c.Old.Ref, c.Old.Commit)
	}
	return short(c.OldGitlink)
}

// newSide describes the target of the update.
func newSide(c core.Change) string {
	if c.New.Commit == "" {
		return "unknown until fetched"
	}
	return refAt(c.New.Mode, c.New.Ref, c.New.Commit)
}

// refAt describes a ref and its commit, such as "v6.6.9 (e4f5a6b)"; in
// commit mode, the abbreviated commit alone.
func refAt(mode manifest.Mode, ref, commit string) string {
	if mode == manifest.ModeCommit || ref == "" {
		return short(commit)
	}
	return clean(ref) + " (" + short(commit) + ")"
}
