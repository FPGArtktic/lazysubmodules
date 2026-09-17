// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package core

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/FPGArtktic/lazysubmodules/internal/git"
	"github.com/FPGArtktic/lazysubmodules/internal/lock"
	"github.com/FPGArtktic/lazysubmodules/internal/manifest"
)

// Limits of the lists in a Preview.
const (
	previewLogMax  = 20
	previewTagsMax = 30
)

// Markers of the lines in Preview.LockDiff.
const (
	lockSideMarker = "< "
	headSideMarker = "> "
)

// Preview summarizes the history of a submodule for the terminal interface.
//
// Every line is "<abbreviated commit> <subject>" or a tag name. Characters
// that are not printable, such as terminal control sequences in a commit
// subject, are replaced.
type Preview struct {
	// Log lists the latest commits of the submodule HEAD, newest first.
	Log []string
	// Tags lists the local tags of the submodule, highest version first.
	Tags []string
	// Pending lists the commits that an update would check out in addition
	// to HEAD: reachable from the target, but not from HEAD.
	Pending []string
	// LockDiff lists the commits by which HEAD and the locked commit
	// differ. Lines starting with "< " are only in the history of the locked
	// commit, lines starting with "> " only in the history of HEAD.
	LockDiff []string
}

// Preview collects the recent history of a submodule, its tags, the commits
// an update would add, and how HEAD differs from the lock entry.
//
// Log, Pending and LockDiff need a checked-out submodule; for a submodule
// that is not checked out, only Tags is filled, from its repository in the
// git directory of the superproject when that exists. Pending is empty
// when the tracking configuration does not resolve, and LockDiff when
// there is no lock entry, it records HEAD, or the locked commit is not
// available locally. Each list holds at most 20 lines (Tags: 30).
//
// Context: offline; only local refs and objects are read. Unmanaged
// submodules are accepted.
// Return: the preview; an error wrapping ErrNotFound for an unknown name; an
// error from reading .gitmodules or the lock file; or *git.Error.
func (r *Repo) Preview(ctx context.Context, name string) (Preview, error) {
	sub, locked, err := r.inspected(ctx, name)
	if err != nil {
		return Preview{}, err
	}
	loc, err := r.locate(ctx, sub)
	if err != nil {
		return Preview{}, wrapName(sub.Name, err)
	}
	var p Preview
	dir, ok := loc.refDir()
	if !ok {
		return p, nil
	}
	if p.Tags, err = r.git.ListTags(ctx, dir, ""); err != nil {
		return Preview{}, wrapName(sub.Name, err)
	}
	p.Tags = printableLines(p.Tags[:min(len(p.Tags), previewTagsMax)])
	if !loc.populated {
		return p, nil
	}
	if err := r.previewHistory(ctx, &p, sub, loc, locked); err != nil {
		return Preview{}, wrapName(sub.Name, err)
	}
	return p, nil
}

// inspected returns the configuration and lock entry of the named
// submodule, which may be unmanaged.
func (r *Repo) inspected(ctx context.Context, name string) (
	manifest.Submodule, *lock.Entry, error,
) {
	subs, lk, err := r.load(ctx, []string{name}, selectAll)
	if err != nil {
		return manifest.Submodule{}, nil, err
	}
	return subs[0], lockEntry(lk, subs[0].Name), nil
}

// previewHistory fills the lists of a preview that need the working tree
// of a checked-out submodule. Its errors do not name the submodule.
func (r *Repo) previewHistory(ctx context.Context, p *Preview, sub manifest.Submodule,
	loc location, locked *lock.Entry,
) error {
	head, err := r.head(ctx, loc.worktree)
	if err != nil || head == "" {
		return err
	}
	dir := loc.worktree
	if p.Log, err = r.logLines(ctx, dir, previewLogMax, head); err != nil {
		return err
	}
	if p.Pending, err = r.pending(ctx, sub, loc, locked, head); err != nil {
		return err
	}
	if locked == nil || locked.Commit == head {
		return nil
	}
	p.LockDiff, err = r.lockDiff(ctx, dir, head, locked.Commit)
	return err
}

// pending lists the commits that an update of a checked-out submodule would
// add to head; none when the submodule is unmanaged or its target does not
// resolve.
func (r *Repo) pending(ctx context.Context, sub manifest.Submodule, loc location,
	locked *lock.Entry, head string,
) ([]string, error) {
	if !sub.Managed() {
		return nil, nil
	}
	target, _, err := r.target(ctx, sub, loc, locked)
	if err != nil || target == nil || target.Commit == head {
		return nil, err
	}
	return r.logLines(ctx, loc.worktree, previewLogMax, head+".."+target.Commit)
}

// lockDiff lists the commits by which head and the locked commit differ, at
// most previewLogMax of them, or none when the locked commit is missing.
func (r *Repo) lockDiff(ctx context.Context, dir, head, locked string) ([]string, error) {
	_, err := r.git.ResolveCommit(ctx, dir, locked)
	if errors.Is(err, git.ErrRefNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	lockSide, err := r.logLines(ctx, dir, previewLogMax, head+".."+locked)
	if err != nil {
		return nil, err
	}
	headSide, err := r.logLines(ctx, dir, previewLogMax-len(lockSide), locked+".."+head)
	if err != nil {
		return nil, err
	}
	lines := make([]string, 0, len(lockSide)+len(headSide))
	for _, l := range lockSide {
		lines = append(lines, lockSideMarker+l)
	}
	for _, l := range headSide {
		lines = append(lines, headSideMarker+l)
	}
	return lines, nil
}

// logLines runs git log with a positive limit and makes the lines
// printable. A limit of zero or less lists nothing.
func (r *Repo) logLines(ctx context.Context, dir string, limit int, rev string) (
	[]string, error,
) {
	if limit <= 0 {
		return nil, nil
	}
	lines, err := r.git.Log(ctx, dir, limit, rev)
	return printableLines(lines), err
}

// printableLines applies printable to every line in place.
func printableLines(lines []string) []string {
	for i, l := range lines {
		lines[i] = printable(l)
	}
	return lines
}

// Branches lists the remote-tracking branches of origin in a submodule,
// which branch mode can track.
//
// Context: offline; the branches are read from the submodule working tree
// or from its repository in the git directory of the superproject, so they
// are as recent as the last fetch. Unmanaged submodules are accepted.
// Return: the branch names sorted by name; an error wrapping ErrNotFound for
// an unknown name, or ErrUninitialized when no repository of the submodule
// can be read; an error from reading .gitmodules or the lock file; or
// *git.Error.
func (r *Repo) Branches(ctx context.Context, name string) ([]string, error) {
	sub, dir, err := r.refsDir(ctx, name)
	if err != nil {
		return nil, err
	}
	branches, err := r.git.ListRemoteBranches(ctx, dir, "origin")
	if err != nil {
		return nil, wrapName(sub.Name, err)
	}
	return printableLines(branches), nil
}

// Tags lists the local tags of a submodule that match a glob, as tag and
// tag-pattern mode see them.
//
// The pattern is validated like a configured tag pattern; an empty pattern
// lists every tag. Tags are sorted by version, highest first, and a
// pre-release (see IsPrerelease) follows its release; tag-pattern mode
// considers them in this order.
//
// Context: offline; the tags are read from the submodule working tree or
// from its repository in the git directory of the superproject. Unmanaged
// submodules are accepted.
// Return: the matching tags; an error wrapping ErrInvalidArgument for an
// invalid pattern, ErrNotFound for an unknown name, or ErrUninitialized when
// no repository of the submodule can be read; an error from reading
// .gitmodules or the lock file; or *git.Error.
func (r *Repo) Tags(ctx context.Context, name, pattern string) ([]string, error) {
	if pattern != "" {
		if err := r.checkRef(ctx, manifest.ModeTagPattern, pattern); err != nil {
			return nil, wrapName(name, err)
		}
	}
	sub, dir, err := r.refsDir(ctx, name)
	if err != nil {
		return nil, err
	}
	tags, err := r.git.ListTags(ctx, dir, pattern)
	if err != nil {
		return nil, wrapName(sub.Name, err)
	}
	return printableLines(tags), nil
}

// refsDir returns the configuration of the named submodule and the
// directory its refs can be read in.
func (r *Repo) refsDir(ctx context.Context, name string) (manifest.Submodule, string, error) {
	sub, _, err := r.inspected(ctx, name)
	if err != nil {
		return sub, "", err
	}
	loc, err := r.locate(ctx, sub)
	if err != nil {
		return sub, "", wrapName(sub.Name, err)
	}
	dir, ok := loc.refDir()
	if !ok {
		return sub, "", wrapName(sub.Name, ErrUninitialized)
	}
	return sub, dir, nil
}

// GitlinkDiff shows how the commit checked out in a submodule differs from
// the commit that the HEAD commit of the superproject records for it, as
// "git diff --submodule=log HEAD" shows it: a heading with both commits and
// the commits added (">") or removed ("<"), and a note when the submodule
// working tree contains modified content. In a superproject without
// commits, the staged gitlink is compared with nothing. Characters that are
// not printable are replaced, except line breaks; tabs become spaces.
//
// Context: offline. Unmanaged submodules are accepted.
// Return: the diff text, empty when the commits agree; an error wrapping
// ErrNotFound for an unknown name or ErrSymlinkPath for a path through a
// symbolic link; an error from reading .gitmodules or the lock file; or
// *git.Error.
func (r *Repo) GitlinkDiff(ctx context.Context, name string) (string, error) {
	sub, _, err := r.inspected(ctx, name)
	if err != nil {
		return "", err
	}
	link, err := hasSymlink(r.root, sub.Path)
	if err != nil {
		return "", wrapName(sub.Name, err)
	}
	if link {
		return "", wrapName(sub.Name, ErrSymlinkPath)
	}
	diff, err := r.git.SubmoduleDiff(ctx, r.root, sub.Path)
	if err != nil {
		return "", fmt.Errorf("%s: gitlink diff: %w", displayName(sub.Name), err)
	}
	return printableText(diff), nil
}

// printableText applies printable to every line of s, keeping the line
// breaks; tabs become spaces.
func printableText(s string) string {
	lines := strings.Split(strings.ReplaceAll(s, "\t", " "), "\n")
	return strings.Join(printableLines(lines), "\n")
}
