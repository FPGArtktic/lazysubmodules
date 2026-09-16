// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package lock

import (
	"fmt"
	"strings"

	"github.com/FPGArtktic/lazysubmodules/internal/manifest"
)

// Lengths of a full commit SHA in hexadecimal digits.
const (
	sha1HexLen   = 40
	sha256HexLen = 64
)

// ValidCommit reports whether s is a full commit SHA as stored in the lock
// file.
//
// The object format of the repository is not known here, so both lengths
// are accepted. Git prints object names in lowercase, and so does the lock;
// uppercase digits are rejected.
//
// Context: any; callers that know the object format must compare the length
// themselves.
// Return: true when s consists of exactly 40 (SHA-1) or 64 (SHA-256)
// lowercase hexadecimal digits.
func ValidCommit(s string) bool {
	if len(s) != sha1HexLen && len(s) != sha256HexLen {
		return false
	}
	for i := range len(s) {
		if !isLowerHex(s[i]) {
			return false
		}
	}
	return true
}

// isLowerHex reports whether c is a lowercase hexadecimal digit.
func isLowerHex(c byte) bool {
	return '0' <= c && c <= '9' || 'a' <= c && c <= 'f'
}

// validate checks that the entry is complete, that Load reads back what
// Write stores, and that the values are safe to pass to git.
func (e Entry) validate() error {
	if !validName(e.Name) {
		return fmt.Errorf("%w %q: submodule name is empty or contains a newline or NUL",
			ErrInvalidEntry, e.Name)
	}
	if _, err := manifest.ParseMode(string(e.Mode)); err != nil {
		return fmt.Errorf("%w %q: %s: %w", ErrInvalidEntry, e.Name, keyMode, err)
	}
	if !validRef(e.Ref) {
		return fmt.Errorf("%w %q: %s: %q is empty, starts with \"-\" or contains white space "+
			"or control characters", ErrInvalidEntry, e.Name, keyRef, e.Ref)
	}
	if !ValidCommit(e.Commit) {
		return fmt.Errorf("%w %q: %s: %q is not a full lowercase hexadecimal SHA",
			ErrInvalidEntry, e.Name, keyCommit, e.Commit)
	}
	return nil
}

// validName reports whether name can be stored as a subsection name: git
// accepts any name except one containing a newline or NUL, and an empty name
// never denotes a submodule.
func validName(name string) bool {
	return name != "" && !strings.ContainsAny(name, "\n\x00")
}

// validRef reports whether ref is not empty, cannot be taken for a command
// line option and contains no ASCII white space or control characters, none
// of which git allows in a ref name.
func validRef(ref string) bool {
	return ref != "" && !strings.HasPrefix(ref, "-") &&
		!strings.ContainsFunc(ref, func(r rune) bool { return r <= ' ' || r == 0x7f })
}
