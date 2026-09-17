// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package core

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/FPGArtktic/lazysubmodules/internal/manifest"
)

// Layout of commit messages. The limits and the subsystem prefix follow the
// commit message rules of the project (see CONTRIBUTING.md), so that
// update commits pass the same checks as hand-written ones.
const (
	// maxLineLen is the longest line, in characters.
	maxLineLen = 75
	// subjectPrefix starts every subject.
	subjectPrefix = "manifest: update "
	// blockIndent indents the lines of one submodule in a message that
	// lists several, and continued lines.
	blockIndent = "  "
	// headingWord starts the heading of a submodule in a message that
	// lists several.
	headingWord = "Submodule"
	// wipWord is a word that the rules forbid in a subject.
	wipWord = "wip"
	// titlePunctuation lists the characters a subject may not end with.
	titlePunctuation = "?:!.,;"
)

// CommitMessage returns the message of the commit that records changes.
//
// The message describes what the commit changes: only submodules whose
// gitlink, lock entry or tracking keys change (see Change.RecordChanged)
// are listed, not those that an update only initializes, clones or checks
// out at their recorded commit. For one submodule, the subject is
// "manifest: update <name> to <ref>", where ref is the new tag or branch,
// or the abbreviated commit in commit mode. A subject that would be longer
// than 75 characters or break another subject rule (trailing punctuation
// or white space, the word WIP) becomes "manifest: update <name>", or else
// "manifest: update 1 submodule". The body reads:
//
//	Tracking mode: <mode> <configured ref>
//	Old: <commit> (<ref>)
//	New: <commit> (<ref>)
//
// Commits are abbreviated to 12 digits, and the ref in parentheses is left
// out in commit mode. The old commit is the one the superproject recorded
// (Change.OldGitlink), with the ref of the previous lock entry when that
// entry records the same commit; without a previous lock entry, the old
// line is "Old: <commit> (unlocked)", and it is "Old: none" when the
// superproject recorded no commit. For several submodules, the subject is
// "manifest: update <N> submodules" and the body has one block per
// submodule, separated by blank lines: a heading `Submodule "<name>":`
// followed by the three lines, indented by two spaces. The heading quotes
// the name as a Go string literal, and writes a space that another space
// follows as \x20. It keeps git from taking the last block for trailers,
// so "git commit -s" adds the sign-off after a blank line.
//
// Names and refs are shown as in other messages of this package: quoted
// when they are not valid UTF-8 or contain quotes, backslashes or
// characters that are not printable, such as line separators. A line
// longer than 75 characters continues on the next lines, indented by two
// more spaces: the ref, or the quoted name and colon of a heading, moves
// there, split into pieces that fit and never end with a space. Every line
// of the message therefore passes the length and white space rules,
// whatever the names and refs.
//
// Context: any; the message has no Signed-off-by trailer, which "git commit
// -s" adds.
// Return: the message, ending with a newline, or "" when no submodule
// changes what the superproject records.
func CommitMessage(changes []Change) string {
	var changed []Change
	for _, c := range changes {
		if c.RecordChanged() {
			changed = append(changed, c)
		}
	}
	var b strings.Builder
	switch len(changed) {
	case 0:
		return ""
	case 1:
		b.WriteString(subject(changed[0]))
		b.WriteString("\n\n")
		writeBody(&b, changed[0], "")
		return b.String()
	}
	fmt.Fprintf(&b, "%s%d submodules\n", subjectPrefix, len(changed))
	for _, c := range changed {
		b.WriteString("\n")
		writeLine(&b, "", headingWord, headingName(c.Submodule.Name)+":")
		writeBody(&b, c, blockIndent)
	}
	return b.String()
}

// subject returns the subject for a single change.
func subject(c Change) string {
	name := displayName(c.Submodule.Name)
	var candidates []string
	if c.New.Commit != "" {
		candidates = append(candidates, subjectPrefix+name+" to "+shownRef(c.New))
	}
	candidates = append(candidates, subjectPrefix+name)
	for _, s := range candidates {
		if validSubject(s) {
			return s
		}
	}
	return subjectPrefix + "1 submodule"
}

// shownRef returns the ref that names a resolution in a subject.
func shownRef(res Resolution) string {
	if res.Mode == manifest.ModeCommit {
		return abbrev(res.Commit)
	}
	return displayName(res.Ref)
}

// validSubject reports whether s satisfies the subject rules: at most
// maxLineLen characters, all of them printable (no tab or line separator),
// no trailing white space or punctuation, and no word "WIP" in any case.
func validSubject(s string) bool {
	last, _ := utf8.DecodeLastRuneInString(s)
	if utf8.RuneCountInString(s) > maxLineLen || !utf8.ValidString(s) ||
		strings.ContainsFunc(s, func(c rune) bool { return !unicode.IsPrint(c) }) ||
		unicode.IsSpace(last) || strings.ContainsRune(titlePunctuation, last) {
		return false
	}
	return !containsWord(s, wipWord)
}

// containsWord reports whether s contains word, an ASCII word, in any case,
// as the case-insensitive word match of gitlint finds it. That match runs
// in Python on the lowercased subject, which differs from Go in two ways:
//
//   - Python lowercases "İ" to "i" and a combining dot, and takes "ı", "ſ"
//     and the Kelvin sign for "i", "s" and "k".
//   - Words are made of the characters that Python counts as letters and
//     digits, which vary with its Unicode version.
//
// The first is mapped here; for the second, a word is a run of ASCII
// letters, digits and underscores. Every word that gitlint finds is then
// found here, and some more, which only makes the subject shorter.
func containsWord(s, word string) bool {
	s = strings.NewReplacer(
		"\u0130", "i\u0307", // capital I with dot above
		"\u0131", "i", // dotless i
		"\u017f", "s", // long s
		"\u212a", "k", // Kelvin sign
	).Replace(s)
	words := strings.FieldsFunc(s, func(c rune) bool {
		return c != '_' && (c >= utf8.RuneSelf || !unicode.IsLetter(c) && !unicode.IsDigit(c))
	})
	for _, w := range words {
		if strings.EqualFold(w, word) {
			return true
		}
	}
	return false
}

// headingName returns the quoted name of a heading: a Go string literal
// in which a space that another space follows is written as \x20. A
// heading split across lines can then always end a line before a space.
func headingName(name string) string {
	quoted := strconv.Quote(name)
	var b strings.Builder
	for i, c := range quoted {
		if c == ' ' && strings.HasPrefix(quoted[i+1:], " ") {
			b.WriteString(`\x20`)
			continue
		}
		b.WriteRune(c)
	}
	return b.String()
}

// writeBody writes the three lines describing a change, each prefixed with
// indent.
func writeBody(b *strings.Builder, c Change, indent string) {
	sub := c.Submodule
	ref := sub.Ref
	if ref != "" {
		ref = displayName(ref)
	}
	writeLine(b, indent, "Tracking mode: "+string(sub.Mode), ref)
	old := "Old: " + abbrev(c.OldGitlink)
	switch {
	case c.OldGitlink == "":
		writeLine(b, indent, "Old: none", "")
	case c.Old == nil:
		writeLine(b, indent, old, "(unlocked)")
	case c.Old.Commit == c.OldGitlink:
		writeLine(b, indent, old, refNote(c.Old.Mode, c.Old.Ref))
	default:
		// The lock entry records another commit, so the old ref is unknown.
		writeLine(b, indent, old, "")
	}
	if c.New.Commit == "" {
		writeLine(b, indent, "New: unknown", "")
		return
	}
	writeLine(b, indent, "New: "+abbrev(c.New.Commit), refNote(c.New.Mode, c.New.Ref))
}

// refNote returns the parenthesized ref of a commit line, or "" in commit
// mode, where the ref is the commit itself.
func refNote(mode manifest.Mode, ref string) string {
	if mode == manifest.ModeCommit {
		return ""
	}
	return "(" + displayName(ref) + ")"
}

// writeLine writes "<indent><head> <tail>". When that line is too long, the
// tail continues on the next lines, indented by two more spaces, in pieces
// that fit. head is short, and tail has no two spaces in a row.
func writeLine(b *strings.Builder, indent, head, tail string) {
	b.WriteString(indent)
	b.WriteString(head)
	if tail != "" && utf8.RuneCountInString(indent+head+" "+tail) <= maxLineLen {
		b.WriteString(" ")
		b.WriteString(tail)
		tail = ""
	}
	b.WriteString("\n")
	indent += blockIndent
	width := maxLineLen - utf8.RuneCountInString(indent)
	for rest := []rune(tail); len(rest) > 0; {
		n := min(width, len(rest))
		// A line must not end with white space, which git would also
		// remove. The piece then ends before it, and the next piece
		// starts with it.
		for n > 1 && n < len(rest) && unicode.IsSpace(rest[n-1]) {
			n--
		}
		b.WriteString(indent)
		b.WriteString(string(rest[:n]))
		b.WriteString("\n")
		rest = rest[n:]
	}
}
