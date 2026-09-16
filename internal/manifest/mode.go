// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package manifest

import "fmt"

// Mode is the tracking mode of a managed submodule, stored in lsm-mode.
// The zero value means that the submodule is unmanaged.
type Mode string

// Tracking modes, as documented in README.md.
const (
	// ModeBranch follows the tip of a remote branch.
	ModeBranch Mode = "branch"
	// ModeTag pins the commit of a tag.
	ModeTag Mode = "tag"
	// ModeTagPattern follows the highest version tag matching a glob.
	ModeTagPattern Mode = "tag-pattern"
	// ModeCommit pins a commit.
	ModeCommit Mode = "commit"
)

// ParseMode converts an lsm-mode value to a Mode.
//
// The comparison is exact: the value must be spelled as in .gitmodules.
//
// Context: any.
// Return: the mode, or an error wrapping ErrInvalidMode for any value other
// than "branch", "tag", "tag-pattern" and "commit", including "".
func ParseMode(s string) (Mode, error) {
	switch m := Mode(s); m {
	case ModeBranch, ModeTag, ModeTagPattern, ModeCommit:
		return m, nil
	}
	return "", fmt.Errorf("%w %q", ErrInvalidMode, s)
}

// String returns the mode as written in .gitmodules.
//
// Context: any.
// Return: the lsm-mode value; empty for the zero Mode.
func (m Mode) String() string {
	return string(m)
}
