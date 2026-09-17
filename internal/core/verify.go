// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package core

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/FPGArtktic/lazysubmodules/internal/git"
	"github.com/FPGArtktic/lazysubmodules/internal/lock"
	"github.com/FPGArtktic/lazysubmodules/internal/manifest"
)

// Names of the verification checks, in the order they are reported.
const (
	// CheckLockEntry passes when the lock file has an entry for the
	// submodule.
	CheckLockEntry = "lock-entry"
	// CheckLockConfig passes when the lock entry matches .gitmodules: the
	// same mode; the same branch or tag; for a tag pattern, a locked tag
	// that exists and matches; for a commit, the configured commit.
	CheckLockConfig = "lock-config"
	// CheckLockCommit passes when the locked commit is a full commit name
	// for the object format of the superproject.
	CheckLockCommit = "lock-commit"
	// CheckGitlink passes when the HEAD commit of the superproject records
	// the locked commit for the submodule.
	CheckGitlink = "gitlink"
	// CheckInitialized passes when the submodule is checked out.
	CheckInitialized = "initialized"
	// CheckHead passes when the submodule HEAD is the locked commit.
	CheckHead = "head"
	// CheckTag passes when the locked tag still resolves to the locked
	// commit; it runs for tag and tag-pattern entries only.
	CheckTag = "tag"
	// CheckSignature passes when the locked tag (tag modes) or the locked
	// commit (other modes) has a good signature; it runs only on request.
	CheckSignature = "signature"
)

// VerifyOptions selects optional verification checks.
type VerifyOptions struct {
	// Signatures adds CheckSignature, which runs "git verify-tag" or "git
	// verify-commit" with the signature configuration of git.
	Signatures bool
}

// Check is the outcome of one verification check.
type Check struct {
	// Name is one of the Check* constants.
	Name string
	// OK reports whether the check passed.
	OK bool
	// Detail explains the outcome in one line.
	Detail string
}

// VerifyResult lists the checks run for one submodule.
type VerifyResult struct {
	// Submodule is the configuration from .gitmodules.
	Submodule manifest.Submodule
	// Checks are the checks that ran, in the order of the Check* constants.
	Checks []Check
}

// OK reports whether the submodule passed verification.
//
// Context: any.
// Return: true when at least one check ran and every check passed.
func (v VerifyResult) OK() bool {
	return len(v.Checks) > 0 && !slices.ContainsFunc(v.Checks, func(c Check) bool {
		return !c.OK
	})
}

// Verify checks that the lock file, the gitlinks committed in the
// superproject and the checked-out submodules agree.
//
// The checks are listed with the Check* constants. A failed check is
// reported and the checks that depend on it are left out: without a lock
// entry, or with a malformed locked commit, only CheckInitialized follows;
// a submodule that is not checked out gets no further checks. Only local
// refs are consulted; up to eight submodules are verified at the same time.
//
// Context: names select managed submodules; without names, every managed
// submodule is verified.
// Return: one VerifyResult per selected submodule, in .gitmodules order,
// and an error wrapping ErrVerify when any result is not OK. Without
// results, the error wraps ErrNotFound or ErrUnmanaged for a name that
// cannot be verified, or reports a failure to read .gitmodules or the lock
// file, or the first *git.Error of an invocation that failed for another
// reason than a missing ref or a bad signature.
func (r *Repo) Verify(ctx context.Context, names []string, opts VerifyOptions) (
	[]VerifyResult, error,
) {
	subs, lk, err := r.load(ctx, names, selectManaged)
	if err != nil {
		return nil, err
	}
	results := make([]VerifyResult, len(subs))
	err = forEach(ctx, len(subs), maxParallel, func(ctx context.Context, i int) error {
		res, err := r.verify(ctx, subs[i], lockEntry(lk, subs[i].Name), opts)
		if err != nil {
			return wrapName(subs[i].Name, err)
		}
		results[i] = res
		return nil
	})
	if err != nil {
		return nil, err
	}
	var failed []string
	for _, res := range results {
		if !res.OK() {
			failed = append(failed, displayName(res.Submodule.Name))
		}
	}
	if len(failed) > 0 {
		return results, fmt.Errorf("%w: %s", ErrVerify, strings.Join(failed, ", "))
	}
	return results, nil
}

// verifier collects the checks of one submodule.
type verifier struct {
	repo   *Repo
	sub    manifest.Submodule
	loc    location
	locked *lock.Entry
	checks []Check
}

// add records a check and returns its outcome. Characters of refs, tag
// names or paths that are not printable are replaced in the detail.
func (v *verifier) add(name string, ok bool, detail string) bool {
	v.checks = append(v.checks, Check{Name: name, OK: ok, Detail: printable(detail)})
	return ok
}

// verify runs the checks for one managed submodule.
func (r *Repo) verify(ctx context.Context, sub manifest.Submodule, locked *lock.Entry,
	opts VerifyOptions,
) (VerifyResult, error) {
	loc, err := r.locate(ctx, sub)
	if err != nil {
		return VerifyResult{}, err
	}
	v := &verifier{repo: r, sub: sub, loc: loc, locked: locked}
	err = v.run(ctx, opts)
	return VerifyResult{Submodule: sub, Checks: v.checks}, err
}

// run adds the checks in their order.
func (v *verifier) run(ctx context.Context, opts VerifyOptions) error {
	if !v.add(CheckLockEntry, v.locked != nil, v.lockEntryDetail()) {
		v.addInitialized()
		return nil
	}
	if err := v.checkLockConfig(ctx); err != nil {
		return err
	}
	if !v.checkLockCommit() {
		v.addInitialized()
		return nil
	}
	if err := v.checkGitlink(ctx); err != nil {
		return err
	}
	if !v.addInitialized() {
		return nil
	}
	if err := v.checkHead(ctx); err != nil {
		return err
	}
	if isTagMode(v.locked.Mode) {
		if err := v.checkTag(ctx); err != nil {
			return err
		}
	}
	if opts.Signatures {
		return v.checkSignature(ctx)
	}
	return nil
}

// lockEntryDetail describes the lock entry.
func (v *verifier) lockEntryDetail() string {
	if v.locked == nil {
		return "no entry in " + lock.File
	}
	return fmt.Sprintf("%s %s at %s", v.locked.Mode, v.locked.Ref, abbrev(v.locked.Commit))
}

// addInitialized adds CheckInitialized.
func (v *verifier) addInitialized() bool {
	if v.loc.populated {
		return v.add(CheckInitialized, true, "submodule is checked out")
	}
	return v.add(CheckInitialized, false, uninitializedReason(v.loc))
}

// checkLockConfig adds CheckLockConfig.
func (v *verifier) checkLockConfig(ctx context.Context) error {
	ok, detail, err := v.lockConfig(ctx)
	if err != nil {
		return err
	}
	v.add(CheckLockConfig, ok, detail)
	return nil
}

// lockConfig compares the lock entry with the configuration.
func (v *verifier) lockConfig(ctx context.Context) (bool, string, error) {
	sub, locked := v.sub, v.locked
	err := v.repo.checkRef(ctx, sub.Mode, sub.Ref)
	if invalid, ok := errors.AsType[*invalidError](err); ok {
		return false, "configured " + invalid.Error(), nil
	}
	if err != nil {
		return false, "", err
	}
	if locked.Mode != sub.Mode {
		return false, fmt.Sprintf("lock records mode %s, configuration has %s",
			locked.Mode, sub.Mode), nil
	}
	switch sub.Mode {
	case manifest.ModeTagPattern:
		return v.lockedTagMatches(ctx)
	case manifest.ModeCommit:
		if locked.Ref != locked.Commit {
			return false, fmt.Sprintf("lock records ref %s for commit %s",
				locked.Ref, abbrev(locked.Commit)), nil
		}
		if !strings.HasPrefix(locked.Commit, sub.Ref) {
			return false, fmt.Sprintf("lock records commit %s, configuration has %s",
				abbrev(locked.Commit), sub.Ref), nil
		}
	default:
		if locked.Ref != sub.Ref {
			return false, fmt.Sprintf("lock records %s %s, configuration has %s",
				sub.Mode, locked.Ref, sub.Ref), nil
		}
	}
	return true, fmt.Sprintf("%s %s", sub.Mode, sub.Ref), nil
}

// lockedTagMatches checks that the tag of a tag-pattern entry exists and
// matches the configured pattern.
func (v *verifier) lockedTagMatches(ctx context.Context) (bool, string, error) {
	dir, ok := v.loc.refDir()
	if !ok {
		return false, "cannot list tags: submodule repository is missing", nil
	}
	tags, err := v.repo.git.ListTags(ctx, dir, v.sub.Ref)
	if err != nil {
		return false, "", err
	}
	if !slices.Contains(tags, v.locked.Ref) {
		return false, fmt.Sprintf("locked tag %s does not exist or does not match %s",
			v.locked.Ref, v.sub.Ref), nil
	}
	return true, fmt.Sprintf("tag %s matches %s", v.locked.Ref, v.sub.Ref), nil
}

// checkLockCommit adds CheckLockCommit.
func (v *verifier) checkLockCommit() bool {
	commit := v.locked.Commit
	if !lock.ValidCommit(commit) || len(commit) != v.repo.hexLen {
		return v.add(CheckLockCommit, false, fmt.Sprintf(
			"locked commit %q is not a full commit name with %d hexadecimal digits",
			commit, v.repo.hexLen))
	}
	return v.add(CheckLockCommit, true, commit)
}

// checkGitlink adds CheckGitlink.
func (v *verifier) checkGitlink(ctx context.Context) error {
	r := v.repo
	gitlink, err := r.git.TreeGitlink(ctx, r.root, "HEAD", v.sub.Path)
	switch {
	case errors.Is(err, git.ErrRefNotFound):
		v.add(CheckGitlink, false,
			"HEAD of the superproject records no gitlink at "+displayName(v.sub.Path))
	case err != nil:
		return err
	case gitlink != v.locked.Commit:
		v.add(CheckGitlink, false, fmt.Sprintf(
			"HEAD of the superproject records %s, locked %s",
			abbrev(gitlink), abbrev(v.locked.Commit)))
	default:
		v.add(CheckGitlink, true, "HEAD of the superproject records "+abbrev(gitlink))
	}
	return nil
}

// checkHead adds CheckHead.
func (v *verifier) checkHead(ctx context.Context) error {
	head, err := v.repo.head(ctx, v.loc.worktree)
	if err != nil {
		return err
	}
	if head != v.locked.Commit {
		v.add(CheckHead, false, fmt.Sprintf("HEAD %s differs from locked commit %s",
			displayCommit(head), abbrev(v.locked.Commit)))
		return nil
	}
	v.add(CheckHead, true, "HEAD is "+abbrev(head))
	return nil
}

// checkTag adds CheckTag.
func (v *verifier) checkTag(ctx context.Context) error {
	tag := v.locked.Ref
	commit, err := v.repo.git.ResolveCommit(ctx, v.loc.worktree, tagsPrefix+tag)
	switch {
	case errors.Is(err, git.ErrRefNotFound):
		v.add(CheckTag, false, fmt.Sprintf("tag %s does not exist", tag))
	case err != nil:
		return err
	case commit != v.locked.Commit:
		v.add(CheckTag, false, fmt.Sprintf("tag %s points to %s, locked %s (moved tag)",
			tag, abbrev(commit), abbrev(v.locked.Commit)))
	default:
		v.add(CheckTag, true, fmt.Sprintf("tag %s points to %s", tag, abbrev(commit)))
	}
	return nil
}

// checkSignature adds CheckSignature.
func (v *verifier) checkSignature(ctx context.Context) error {
	g, dir := v.repo.git, v.loc.worktree
	var (
		what string
		err  error
	)
	if isTagMode(v.locked.Mode) {
		what = "tag " + v.locked.Ref
		err = g.VerifyTag(ctx, dir, v.locked.Ref)
	} else {
		what = "commit " + abbrev(v.locked.Commit)
		err = g.VerifyCommit(ctx, dir, v.locked.Commit)
	}
	if errors.Is(err, git.ErrBadSignature) {
		v.add(CheckSignature, false, what+": "+signatureDetail(err))
		return nil
	}
	if err != nil {
		return err
	}
	v.add(CheckSignature, true, what+": good signature")
	return nil
}

// signatureDetail extracts the first diagnostic line of a failed signature
// verification.
func signatureDetail(err error) string {
	if gitErr, ok := errors.AsType[*git.Error](err); ok {
		for line := range strings.Lines(gitErr.Stderr) {
			if line = strings.TrimSpace(line); line != "" {
				return line
			}
		}
	}
	return "no valid signature"
}
