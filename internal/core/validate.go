// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package core

import (
	"context"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	"github.com/FPGArtktic/lazysubmodules/internal/git"
	"github.com/FPGArtktic/lazysubmodules/internal/manifest"
)

// minAbbrevLen is the shortest abbreviated commit name accepted in commit
// mode.
const minAbbrevLen = 7

// invalidError describes a value rejected by validation. It wraps
// ErrInvalidArgument.
type invalidError struct {
	what   string
	value  string
	reason string
}

// Error formats the rejected value and the reason.
//
// Context: any.
// Return: a message such as `invalid tag "a b": contains white space`.
func (e *invalidError) Error() string {
	return fmt.Sprintf("invalid %s %q: %s", e.what, e.value, e.reason)
}

// Unwrap lets errors.Is match ErrInvalidArgument.
//
// Context: any.
// Return: ErrInvalidArgument.
func (e *invalidError) Unwrap() error {
	return ErrInvalidArgument
}

// checkValue rejects values that git could misread: empty values, values
// starting with "-", which git may take for an option, and values with
// white space, control characters or other characters that are not
// printable, which would also be unsafe to show in a terminal.
func checkValue(what, value string) error {
	reason := ""
	switch {
	case value == "":
		reason = "empty value"
	case strings.HasPrefix(value, "-"):
		reason = `starts with "-"`
	case strings.ContainsFunc(value, func(c rune) bool { return c == ' ' || !unicode.IsPrint(c) }):
		reason = "contains white space or control characters"
	default:
		return nil
	}
	return &invalidError{what: what, value: value, reason: reason}
}

// refKind names the kind of configured ref of a mode in messages.
func refKind(mode manifest.Mode) string {
	switch mode {
	case manifest.ModeTagPattern:
		return "tag pattern"
	case manifest.ModeBranch, manifest.ModeTag, manifest.ModeCommit:
		return string(mode)
	}
	return "ref"
}

// checkRef validates the configured ref of a tracking mode, whether it comes
// from the caller or from .gitmodules: a branch or tag must be a valid
// name, a tag pattern must be a valid tag name once its glob characters
// "*?[]" are replaced, and a commit must be 7 to 40 (64 with SHA-256)
// lowercase hexadecimal digits.
//
// It returns an *invalidError (wrapping ErrInvalidArgument) for a rejected
// ref, or *git.Error when git could not run the check.
func (r *Repo) checkRef(ctx context.Context, mode manifest.Mode, ref string) error {
	what := refKind(mode)
	if err := checkValue(what, ref); err != nil {
		return err
	}
	var err error
	switch mode {
	case manifest.ModeBranch:
		err = r.git.CheckBranchName(ctx, ref)
	case manifest.ModeTag:
		err = r.git.CheckTagName(ctx, ref)
	case manifest.ModeTagPattern:
		err = r.git.CheckTagName(ctx, strings.Map(unglob, ref))
	case manifest.ModeCommit:
		return r.checkCommit(ref)
	default:
		return &invalidError{what: "mode", value: string(mode), reason: "unknown tracking mode"}
	}
	if errors.Is(err, git.ErrInvalidRefName) {
		return &invalidError{what: what, value: ref, reason: "not a valid " + what + " name"}
	}
	return err
}

// unglob replaces the glob characters of a tag pattern with a character
// that is valid in ref names.
func unglob(c rune) rune {
	switch c {
	case '*', '?', '[', ']':
		return 'x'
	}
	return c
}

// checkCommit validates an abbreviated or full commit name.
func (r *Repo) checkCommit(ref string) error {
	if len(ref) < minAbbrevLen || len(ref) > r.hexLen {
		return &invalidError{what: "commit", value: ref, reason: fmt.Sprintf(
			"must have %d to %d hexadecimal digits", minAbbrevLen, r.hexLen)}
	}
	if strings.ContainsFunc(ref, func(c rune) bool {
		return (c < '0' || c > '9') && (c < 'a' || c > 'f')
	}) {
		return &invalidError{what: "commit", value: ref,
			reason: "must consist of lowercase hexadecimal digits"}
	}
	return nil
}

// cleanPath validates the path of a new submodule and returns it in the
// form stored in .gitmodules: relative to the top level, cleaned and
// "/"-separated. The top level itself, paths leaving it and paths with a
// ".git" component, which git reserves, are rejected.
func cleanPath(p string) (string, error) {
	const what = "path"
	if strings.ContainsFunc(p, unicode.IsControl) {
		return "", &invalidError{what: what, value: p, reason: "contains control characters"}
	}
	clean := path.Clean(filepath.ToSlash(p))
	switch {
	case p == "":
		return "", &invalidError{what: what, value: p, reason: "empty value"}
	case clean == "." || !filepath.IsLocal(filepath.FromSlash(clean)):
		return "", &invalidError{what: what, value: p,
			reason: "must be a relative path inside the superproject"}
	case strings.HasPrefix(clean, "-"):
		return "", &invalidError{what: what, value: p, reason: `starts with "-"`}
	case slices.ContainsFunc(strings.Split(clean, "/"), isDotGit):
		return "", &invalidError{what: what, value: p, reason: "contains a .git component"}
	}
	return clean, nil
}

// isDotGit reports whether a path component names a git directory; git
// compares the name case-insensitively.
func isDotGit(comp string) bool {
	return strings.EqualFold(comp, ".git")
}

// checkURL validates the repository URL of a new submodule.
func checkURL(url string) error {
	const what = "URL"
	switch {
	case url == "":
		return &invalidError{what: what, value: url, reason: "empty value"}
	case strings.HasPrefix(url, "-"):
		return &invalidError{what: what, value: url, reason: `starts with "-"`}
	case strings.ContainsFunc(url, unicode.IsControl):
		return &invalidError{what: what, value: url, reason: "contains control characters"}
	}
	return nil
}
