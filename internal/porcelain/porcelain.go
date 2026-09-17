// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

// Package porcelain writes the machine-readable output of LazySubmodules.
//
// The v1 status format is a stable interface for scripts: an incompatible
// change needs a new format version. Its first line is HeaderV1. Every
// following line describes one submodule with eight fields, separated by a
// single TAB:
//
//  1. name: the submodule name from .gitmodules;
//  2. path: the path from .gitmodules;
//  3. mode: the tracking mode (branch, tag, tag-pattern or commit);
//  4. ref: the configured branch, tag, tag pattern or commit;
//  5. lock ref: the ref recorded in the lock file;
//  6. lock commit: the full commit name recorded in the lock file;
//  7. HEAD: the full commit name checked out in the submodule;
//  8. state: ok, behind, drift, dirty, uninitialized, missing-ref or
//     unmanaged.
//
// A field without a value is empty: fields 3 to 6 of an unmanaged
// submodule, even when a stray lsm-ref key or lock entry exists for it;
// fields 5 and 6 without a lock entry; field 7 when the submodule is not
// checked out or its HEAD is unborn. The state is never empty, so a line
// never ends with a TAB. Every line ends with LF, and the output contains
// no colors or other escape sequences.
//
// # Quoting
//
// A field that contains a double quote, a backslash, a control character,
// a line or paragraph separator or a byte that is not part of valid UTF-8
// is written in double quotes with the C-style escapes that git uses for
// unusual path names (see core.quotePath in git-config(1)): \a, \b, \t, \n,
// \v, \f and \r for those control characters, \" and \\, and a three-digit
// octal escape such as \033 for every other byte to escape. Control
// characters are U+0000 to U+001F and U+007F to U+009F, the separators
// U+2028 and U+2029; each byte of such a character is escaped. All other
// characters, including spaces and non-ASCII letters, are written
// unchanged, also inside quotes; a field without a character to escape is
// written as it is.
//
// Consequently, a quoted field starts with a double quote, an unquoted one
// never does, and no field contains a raw TAB, LF, other control character
// or other character at which common functions split lines. A quoted field
// can be decoded with the unquoting rules of git, or with strconv.Unquote
// in Go.
package porcelain

import (
	"fmt"
	"io"
	"strings"

	"github.com/FPGArtktic/lazysubmodules/internal/core"
)

// HeaderV1 is the first line of the v1 status format.
const HeaderV1 = "# lsm-porcelain v1"

// fieldsV1 is the number of fields of a submodule line in the v1 status
// format.
const fieldsV1 = 8

// WriteStatusV1 writes the state of submodules in the v1 status format
// described in the package documentation.
//
// The header comes first, followed by one line per status, in the order
// given. The output is built completely before it is written, so nothing is
// written when a status cannot be represented.
//
// Context: any; st is normally the result of core.Repo.Status. Return: nil;
// an error naming the submodule and wrapping ErrUnknownState for a state
// that the v1 format does not define; or an error wrapping the error of w.
func WriteStatusV1(w io.Writer, st []core.Status) error {
	var b strings.Builder
	b.WriteString(HeaderV1)
	b.WriteByte('\n')
	for _, s := range st {
		fields, err := fieldsOfV1(s)
		if err != nil {
			return err
		}
		for i, f := range fields {
			if i > 0 {
				b.WriteByte('\t')
			}
			b.WriteString(quote(f))
		}
		b.WriteByte('\n')
	}
	if _, err := io.WriteString(w, b.String()); err != nil {
		return fmt.Errorf("write porcelain status: %w", err)
	}
	return nil
}

// fieldsOfV1 returns the unquoted fields of the v1 line of a status.
func fieldsOfV1(s core.Status) ([fieldsV1]string, error) {
	var f [fieldsV1]string
	if !knownState(s.State) {
		return f, fmt.Errorf("%s: %w: %q", quote(s.Submodule.Name), ErrUnknownState, s.State)
	}
	f[0], f[1] = s.Submodule.Name, s.Submodule.Path
	f[6], f[7] = s.Head, string(s.State)
	if !s.Submodule.Managed() {
		return f, nil
	}
	f[2], f[3] = s.Submodule.Mode.String(), s.Submodule.Ref
	if s.Lock != nil {
		f[4], f[5] = s.Lock.Ref, s.Lock.Commit
	}
	return f, nil
}

// knownState reports whether the v1 format defines a state.
func knownState(state core.State) bool {
	switch state {
	case core.StateOK, core.StateBehind, core.StateDrift, core.StateDirty,
		core.StateUninitialized, core.StateMissingRef, core.StateUnmanaged:
		return true
	}
	return false
}
