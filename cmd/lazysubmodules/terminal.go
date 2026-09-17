// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package main

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/term"

	"github.com/FPGArtktic/lazysubmodules/internal/core"
	"github.com/FPGArtktic/lazysubmodules/internal/git"
)

// isTerminal reports whether stream is a file descriptor of a terminal.
func isTerminal(stream any) bool {
	f, ok := stream.(interface{ Fd() uintptr })
	return ok && term.IsTerminal(f.Fd())
}

// isTerminal reports whether stream is a terminal, as seen by e.
func (e *env) isTerminal(stream any) bool {
	if e.terminal != nil {
		return e.terminal(stream)
	}
	return isTerminal(stream)
}

// getenv returns the value of an environment variable of e, or "".
func (e *env) getenv(key string) string {
	for _, kv := range slices.Backward(e.environ) {
		if k, v, ok := strings.Cut(kv, "="); ok && k == key {
			return v
		}
	}
	return ""
}

// noColor reports whether the user asked for output without colors: any
// non-empty NO_COLOR value does, as https://no-color.org/ defines it.
func (e *env) noColor() bool {
	return e.getenv("NO_COLOR") != ""
}

// colorOutput reports whether standard output gets colors: it must be a
// terminal that is not "dumb", and NO_COLOR must be unset or empty.
func (e *env) colorOutput() bool {
	return !e.noColor() && e.getenv("TERM") != "dumb" && e.isTerminal(e.stdout)
}

// openRepo opens the superproject containing the working directory. Git
// runs with the environment of the process and e.gitEnv.
func (e *env) openRepo(ctx context.Context) (*core.Repo, error) {
	g, err := git.New(git.WithEnv(e.gitEnv...))
	if err != nil {
		return nil, err
	}
	dir := e.dir
	if dir == "" {
		dir, err = os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("find the working directory: %w", err)
		}
	}
	return core.Open(ctx, g, dir)
}

// clean replaces the characters of s that are not printable, so that text
// from repositories cannot send control sequences to the terminal. Tabs
// are kept.
func clean(s string) string {
	return strings.Map(func(c rune) rune {
		if c == '\t' || unicode.IsPrint(c) {
			return c
		}
		return unicode.ReplacementChar
	}, s)
}

// displayName returns a submodule name for output. As in the messages of
// the core package, a name that is empty, is not valid UTF-8 or contains
// quotes, backslashes or characters that are not printable is shown
// quoted; bytes that are not UTF-8, such as the 8-bit CSI 0x9b, would
// otherwise reach the terminal.
func displayName(name string) string {
	quote := name == "" || !utf8.ValidString(name) || strings.ContainsFunc(name,
		func(c rune) bool {
			return c == '"' || c == '\\' || !unicode.IsPrint(c)
		})
	if quote {
		return strconv.Quote(name)
	}
	return name
}

// shortLen is the length of abbreviated commits in the output.
const shortLen = 7

// short abbreviates a commit name.
func short(commit string) string {
	if len(commit) > shortLen {
		return commit[:shortLen]
	}
	return commit
}
