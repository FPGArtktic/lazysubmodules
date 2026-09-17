// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package git

import (
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"unicode"
)

var (
	// ErrGitNotFound is returned by New when no git binary is found in PATH.
	ErrGitNotFound = errors.New("git executable not found")
	// ErrRefNotFound is returned when a revision, ref or gitlink does not exist.
	ErrRefNotFound = errors.New("ref not found")
	// ErrBadSignature is returned when a tag or commit signature cannot be verified.
	ErrBadSignature = errors.New("signature verification failed")
	// ErrInvalidRefName is returned when a branch, tag or revision name is
	// not acceptable, for example because it could be taken for an option.
	ErrInvalidRefName = errors.New("invalid ref name")
	// ErrNotRegularFile is returned when a configuration file exists but is
	// not a regular file. Git follows symbolic links when it writes such a
	// file, so a link committed to a repository could redirect a write to a
	// file outside of it.
	ErrNotRegularFile = errors.New("not a regular file")
	// ErrUnmerged is returned when the index holds the conflict stages of an
	// unfinished merge for a path instead of a single entry.
	ErrUnmerged = errors.New("unmerged path")
)

// redacted replaces the user information of URLs in messages.
const redacted = "<redacted>"

// stderrLines is the number of stderr lines included in Error.Error.
const stderrLines = 3

// Error describes a failed git invocation.
//
// Every failure of Runner.Exec is reported as *Error, so callers can use
// errors.As to inspect the exit code and the diagnostic output of git.
type Error struct {
	// Args are the arguments of the invocation without the binary.
	Args []string
	// Dir is the working directory of the invocation.
	Dir string
	// ExitCode is the exit status, or -1 when git did not start or was killed.
	ExitCode int
	// Stderr is the trimmed diagnostic output, empty when it was streamed.
	// Credentials in URLs are redacted.
	Stderr string
	// Err is the underlying *exec.ExitError or start error. When the context
	// ended, it also wraps the context error.
	Err error
}

// newError builds the Error for a failed invocation.
func newError(args []string, dir string, err, ctxErr error, stderr string) *Error {
	e := &Error{
		Args:     args,
		Dir:      dir,
		ExitCode: -1,
		Stderr:   redactURLs(strings.TrimSpace(stderr)),
		Err:      err,
	}
	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
		e.ExitCode = exitErr.ExitCode()
	}
	// A context that ended before the start is returned by exec as is.
	if ctxErr != nil && !errors.Is(err, ctxErr) {
		e.Err = fmt.Errorf("%w: %w", ctxErr, err)
	}
	return e
}

// Error formats the failed command line, the exit status and the first
// lines of the diagnostic output.
//
// The user information of URLs in the arguments, which may hold a password
// or token, is replaced by "<redacted>"; the Args field keeps the raw values.
//
// Context: any.
// Return: a single-line message such as
// "git fetch origin: exit status 128: fatal: ...".
func (e *Error) Error() string {
	var b strings.Builder
	b.WriteString("git")
	for _, arg := range e.Args {
		b.WriteByte(' ')
		b.WriteString(quoteArg(redactURLs(arg)))
	}
	switch {
	case e.ExitCode >= 0:
		b.WriteString(": exit status ")
		b.WriteString(strconv.Itoa(e.ExitCode))
	case e.Err != nil:
		b.WriteString(": ")
		b.WriteString(e.Err.Error())
	}
	if msg := firstLines(e.Stderr, stderrLines); msg != "" {
		b.WriteString(": ")
		b.WriteString(msg)
	}
	return b.String()
}

// Unwrap gives access to the underlying process or context error.
//
// Context: any.
// Return: the value of Err.
func (e *Error) Unwrap() error {
	return e.Err
}

// quoteArg quotes an argument when it would be ambiguous in a message.
func quoteArg(arg string) string {
	ambiguous := arg == "" || strings.ContainsFunc(arg, func(r rune) bool {
		return unicode.IsSpace(r) || r == '"' || r == '\'' || r == '\\' || !unicode.IsPrint(r)
	})
	if ambiguous {
		return strconv.Quote(arg)
	}
	return arg
}

// redactURLs replaces the user information of every URL in s, as in
// "https://user:token@host/", with redacted.
func redactURLs(s string) string {
	var b strings.Builder
	for {
		i := strings.Index(s, "://")
		if i < 0 {
			break
		}
		b.WriteString(s[:i+len("://")])
		rest := s[i+len("://"):]
		end := strings.IndexFunc(rest, func(r rune) bool {
			return r == '/' || r == '?' || r == '#' || r == '\'' || r == '"' ||
				unicode.IsSpace(r)
		})
		if end < 0 {
			end = len(rest)
		}
		if at := strings.LastIndexByte(rest[:end], '@'); at >= 0 {
			b.WriteString(redacted)
			rest = rest[at:]
		}
		s = rest
	}
	b.WriteString(s)
	return b.String()
}

// firstLines joins up to n non-empty lines of s with "; ".
func firstLines(s string, n int) string {
	lines := make([]string, 0, n)
	for line := range strings.Lines(s) {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lines = append(lines, line)
		if len(lines) == n {
			break
		}
	}
	return strings.Join(lines, "; ")
}

// exitCode returns the exit status carried by err, or -1 when err does not
// describe a git process that ran to completion.
func exitCode(err error) int {
	if gitErr, ok := errors.AsType[*Error](err); ok {
		return gitErr.ExitCode
	}
	return -1
}
