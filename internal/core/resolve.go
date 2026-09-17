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

// Ref name prefixes consulted by the resolution.
const (
	tagsPrefix   = "refs/tags/"
	originPrefix = "refs/remotes/origin/"
)

// ResolveOptions adjusts the resolution of tag patterns.
type ResolveOptions struct {
	// IncludePrerelease lets a tag pattern select pre-release tags (see
	// IsPrerelease).
	IncludePrerelease bool
}

// Resolution is the commit a tracking configuration selects.
type Resolution struct {
	// Mode is the tracking mode that was resolved.
	Mode manifest.Mode
	// Ref is the resolved ref: the branch name, the selected tag, or the
	// full commit name in commit mode. It is what the lock file records.
	Ref string
	// Commit is the full name of the selected commit.
	Commit string
}

// refError reports a configured ref that is invalid or does not resolve.
// It wraps ErrMissingRef.
type refError struct {
	name   string
	detail string
	// invalid reports that the configured ref is not a valid name, rather
	// than a valid name that does not resolve.
	invalid bool
}

// Error formats the submodule name, the refusal and the details.
//
// Context: any.
// Return: a message such as
// "kernel: refused: ref not found in local refs: tag v9 does not exist", or
// for an invalid ref "kernel: refused: bad ref in .gitmodules: invalid tag
// "v 1": contains white space".
func (e *refError) Error() string {
	reason := ErrMissingRef.Error()
	if e.invalid {
		reason = ErrRefused.Error() + ": bad ref in " + manifest.File
	}
	return displayName(e.name) + ": " + reason + ": " + printable(e.detail)
}

// Unwrap lets errors.Is match ErrMissingRef.
//
// Context: any.
// Return: ErrMissingRef.
func (e *refError) Unwrap() error {
	return ErrMissingRef
}

// Resolve selects the commit that the tracking configuration of a
// submodule points to.
//
// Only local refs are consulted: refs/remotes/origin/<branch> in branch
// mode, refs/tags/<tag> in tag mode, and the configured commit in commit
// mode, which may be abbreviated. Annotated tags are dereferenced to their
// commit. In tag-pattern mode, the tags matching the glob are sorted by
// version as "git -c versionsort.suffix=- tag --sort=-v:refname" sorts them,
// and the highest tag that names a commit is selected. Pre-release tags are
// skipped unless opts.IncludePrerelease is set, except for the tag recorded
// in a tag-pattern lock entry: an update never goes back from that tag
// while it still exists and matches the pattern.
//
// The refs are read from the submodule working tree, or, when it is not
// checked out, from the submodule repository in the git directory of the
// superproject.
//
// Context: locked is the current lock entry of the submodule, or nil.
// Return: the resolution; an error wrapping ErrUnmanaged for a submodule
// without tracking configuration, ErrMissingRef when the configured ref is
// invalid or does not resolve, ErrNotSubmodule when the index records no
// submodule at its path, or ErrUninitialized when no repository of the
// submodule can be read; or *git.Error.
func (r *Repo) Resolve(ctx context.Context, sub manifest.Submodule, locked *lock.Entry,
	opts ResolveOptions,
) (Resolution, error) {
	if !sub.Managed() {
		return Resolution{}, wrapName(sub.Name, ErrUnmanaged)
	}
	if err := r.checkConfiguredRef(ctx, sub); err != nil {
		return Resolution{}, withName(sub.Name, err)
	}
	loc, err := r.locate(ctx, sub)
	if err != nil {
		return Resolution{}, wrapName(sub.Name, err)
	}
	dir, ok := loc.refDir()
	switch {
	case ok:
	case !loc.symlink && !loc.tracked():
		return Resolution{}, wrapName(sub.Name, ErrNotSubmodule)
	default:
		return Resolution{}, wrapName(sub.Name, ErrUninitialized)
	}
	res, err := r.resolveIn(ctx, dir, sub, locked, opts)
	return res, withName(sub.Name, err)
}

// withName prefixes err with the name of the submodule, unless err is nil
// or a *refError, which names the submodule already.
func withName(name string, err error) error {
	if _, ok := errors.AsType[*refError](err); ok || err == nil {
		return err
	}
	return wrapName(name, err)
}

// checkConfiguredRef validates the ref read from .gitmodules. A rejected
// ref is reported as *refError; other errors do not name the submodule.
func (r *Repo) checkConfiguredRef(ctx context.Context, sub manifest.Submodule) error {
	err := r.checkRef(ctx, sub.Mode, sub.Ref)
	if invalid, ok := errors.AsType[*invalidError](err); ok {
		return &refError{name: sub.Name, detail: invalid.Error(), invalid: true}
	}
	return err
}

// resolveIn implements Resolve with the refs of dir, for a submodule whose
// configured ref was validated. A ref that does not resolve is reported as
// *refError; other errors do not name the submodule.
func (r *Repo) resolveIn(ctx context.Context, dir string, sub manifest.Submodule,
	locked *lock.Entry, opts ResolveOptions,
) (Resolution, error) {
	switch sub.Mode {
	case manifest.ModeBranch:
		return r.resolveRef(ctx, dir, sub, originPrefix+sub.Ref,
			"remote branch origin/"+sub.Ref+" does not exist")
	case manifest.ModeTag:
		return r.resolveRef(ctx, dir, sub, tagsPrefix+sub.Ref,
			"tag "+sub.Ref+" does not exist or does not point to a commit")
	case manifest.ModeTagPattern:
		return r.resolvePattern(ctx, dir, sub, locked, opts)
	}
	// manifest.ModeCommit; checkRef rejects every other mode.
	return r.resolveCommit(ctx, dir, sub)
}

// resolveRef resolves the branch or tag of a submodule through its full ref
// name; missing explains a ref that does not resolve.
func (r *Repo) resolveRef(ctx context.Context, dir string, sub manifest.Submodule,
	ref, missing string,
) (Resolution, error) {
	commit, err := r.git.ResolveCommit(ctx, dir, ref)
	if errors.Is(err, git.ErrRefNotFound) {
		return Resolution{}, &refError{name: sub.Name, detail: missing}
	}
	if err != nil {
		return Resolution{}, err
	}
	return Resolution{Mode: sub.Mode, Ref: sub.Ref, Commit: commit}, nil
}

// resolvePattern selects the highest acceptable tag matching the pattern of
// a submodule. Tags that do not name a commit, such as a tag of a tree, are
// skipped.
func (r *Repo) resolvePattern(ctx context.Context, dir string, sub manifest.Submodule,
	locked *lock.Entry, opts ResolveOptions,
) (Resolution, error) {
	tags, err := r.git.ListTags(ctx, dir, sub.Ref)
	if err != nil {
		return Resolution{}, err
	}
	keep := ""
	if locked != nil && locked.Mode == manifest.ModeTagPattern {
		keep = locked.Ref
	}
	skipped := 0
	for _, tag := range tags {
		if !opts.IncludePrerelease && tag != keep && IsPrerelease(tag) {
			skipped++
			continue
		}
		commit, err := r.git.ResolveCommit(ctx, dir, tagsPrefix+tag)
		if errors.Is(err, git.ErrRefNotFound) {
			continue
		}
		if err != nil {
			return Resolution{}, err
		}
		return Resolution{Mode: sub.Mode, Ref: tag, Commit: commit}, nil
	}
	detail := fmt.Sprintf("no tag matching %s names a commit", sub.Ref)
	if skipped > 0 {
		detail += fmt.Sprintf(" (%d pre-release tags skipped)", skipped)
	}
	return Resolution{}, &refError{name: sub.Name, detail: detail}
}

// resolveCommit expands the configured commit of a submodule. Git prefers a
// ref over an abbreviated commit name of the same spelling, so the result
// must start with the configured name; otherwise a ref such as a tag named
// like the commit would silently select another commit.
func (r *Repo) resolveCommit(ctx context.Context, dir string, sub manifest.Submodule) (
	Resolution, error,
) {
	commit, err := r.git.ResolveCommit(ctx, dir, sub.Ref)
	if errors.Is(err, git.ErrRefNotFound) {
		return Resolution{}, &refError{name: sub.Name,
			detail: fmt.Sprintf("commit %s does not exist or is ambiguous", sub.Ref)}
	}
	if err != nil {
		return Resolution{}, err
	}
	if !strings.HasPrefix(commit, sub.Ref) {
		return Resolution{}, &refError{name: sub.Name, detail: fmt.Sprintf(
			"commit %s is ambiguous: a ref of that name points to %s", sub.Ref, abbrev(commit))}
	}
	return Resolution{Mode: sub.Mode, Ref: commit, Commit: commit}, nil
}

// IsPrerelease reports whether a tag names a pre-release.
//
// A tag is a pre-release when a "-" follows its first ASCII digit, which is
// where "versionsort.suffix=-" sorts a suffix before the release: v1.0.0-rc.1
// and v6.6-rc3 are pre-releases, v1.0.0 and release-2.1 are not. Date-like
// tags such as 2024-01-01 count as pre-releases too.
//
// Context: any.
// Return: true for a pre-release tag.
func IsPrerelease(tag string) bool {
	i := strings.IndexFunc(tag, func(c rune) bool { return '0' <= c && c <= '9' })
	return i >= 0 && strings.Contains(tag[i+1:], "-")
}
