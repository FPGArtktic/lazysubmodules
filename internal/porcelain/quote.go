// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package porcelain

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// quote returns a field as the package documentation describes it: in
// double quotes with C-style escapes when it contains a character that must
// be escaped, and unchanged otherwise.
func quote(s string) string {
	if !strings.ContainsFunc(s, isSpecialRune) && utf8.ValidString(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte('"')
	for len(s) > 0 {
		r, size := utf8.DecodeRuneInString(s)
		if isSpecialRune(r) || r == utf8.RuneError && size == 1 {
			for i := range size {
				writeEscaped(&b, s[i])
			}
		} else {
			b.WriteString(s[:size])
		}
		s = s[size:]
	}
	b.WriteByte('"')
	return b.String()
}

// isSpecialRune reports whether a character must be escaped: a double
// quote, a backslash, a control character, or the line separator U+2028 or
// the paragraph separator U+2029, at which some line splitting functions
// break lines, as they do at the control characters.
func isSpecialRune(r rune) bool {
	return r == '"' || r == '\\' || unicode.IsControl(r) || unicode.In(r, unicode.Zl, unicode.Zp)
}

// writeEscaped writes the escape sequence of one byte, using the letter
// escapes of git where they exist and three octal digits otherwise.
func writeEscaped(b *strings.Builder, c byte) {
	b.WriteByte('\\')
	if letter := escapeLetter(c); letter != 0 {
		b.WriteByte(letter)
		return
	}
	b.WriteByte('0' + c>>6)
	b.WriteByte('0' + c>>3&7)
	b.WriteByte('0' + c&7)
}

// escapeLetter returns the character that follows the backslash in the
// short escape of c, or 0 when c has none.
func escapeLetter(c byte) byte {
	switch c {
	case '\a':
		return 'a'
	case '\b':
		return 'b'
	case '\t':
		return 't'
	case '\n':
		return 'n'
	case '\v':
		return 'v'
	case '\f':
		return 'f'
	case '\r':
		return 'r'
	case '"', '\\':
		return c
	}
	return 0
}
