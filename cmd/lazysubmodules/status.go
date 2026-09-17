// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package main

import (
	"context"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/FPGArtktic/lazysubmodules/internal/core"
	"github.com/FPGArtktic/lazysubmodules/internal/lock"
	"github.com/FPGArtktic/lazysubmodules/internal/manifest"
	"github.com/FPGArtktic/lazysubmodules/internal/porcelain"
)

// statusCommand returns the "status" entry of the command table.
func statusCommand() command {
	return command{
		name:     "status",
		synopsis: "[<name>...] [--porcelain=v1]",
		summary:  "show the state of submodules",
		help: "Show the tracking configuration, the lock entry, the checked-out commit and\n" +
			"the state of submodules: ok, behind, drift, dirty, uninitialized,\n" +
			"missing-ref or unmanaged. Without names, every submodule is shown,\n" +
			"including unmanaged ones. Only local refs are consulted; run 'fetch' first\n" +
			"to see what the remotes offer.",
		run: runStatus,
	}
}

// runStatus implements "status".
func runStatus(ctx context.Context, e *env, c command, args []string) int {
	fs := newFlagSet(c.name)
	var format porcelainVersion
	fs.Var(&format, "porcelain", "print the stable `v1` format for scripts instead of a table")
	names, code, done := e.parseArgs(c, fs, args)
	if done {
		return code
	}
	repo, err := e.openRepo(ctx)
	if err != nil {
		return e.fail(err)
	}
	st, err := repo.Status(ctx, names)
	if err != nil {
		return e.fail(err)
	}
	if format == porcelainV1 {
		err = porcelain.WriteStatusV1(e.stdout, st)
	} else {
		err = writeStatusTable(e.stdout, st, e.colorOutput())
	}
	if err != nil {
		return e.fail(err)
	}
	return exitOK
}

// SGR sequences of the status table; the colors are those of the terminal
// interface.
const (
	sgrReset   = "\x1b[0m"
	sgrBold    = "\x1b[1m"
	sgrRed     = "\x1b[31m"
	sgrGreen   = "\x1b[32m"
	sgrYellow  = "\x1b[33m"
	sgrMagenta = "\x1b[35m"
	sgrGray    = "\x1b[90m"
)

// stateColor returns the SGR sequence of a state.
func stateColor(s core.State) string {
	switch s {
	case core.StateOK:
		return sgrGreen
	case core.StateBehind:
		return sgrYellow
	case core.StateDrift, core.StateMissingRef:
		return sgrRed
	case core.StateDirty:
		return sgrMagenta
	case core.StateUninitialized, core.StateUnmanaged:
		return sgrGray
	}
	return ""
}

// statusHeader returns the column names of the status table.
func statusHeader() []string {
	return []string{"NAME", "PATH", "MODE", "REF", "LOCK", "HEAD", "STATE"}
}

// writeStatusTable writes the human-readable status table. Only the
// header and the last column are styled, so styling never shifts the
// alignment.
func writeStatusTable(w io.Writer, st []core.Status, color bool) error {
	if len(st) == 0 {
		_, err := io.WriteString(w, "no submodules\n")
		return err
	}
	rows := [][]string{statusHeader()}
	for _, s := range st {
		rows = append(rows, statusRow(s))
	}
	widths := make([]int, len(rows[0]))
	for _, row := range rows {
		for i, v := range row {
			widths[i] = max(widths[i], utf8.RuneCountInString(v))
		}
	}
	var b strings.Builder
	for i, row := range rows {
		last := len(row) - 1
		for j, v := range row[:last] {
			b.WriteString(v)
			b.WriteString(strings.Repeat(" ", widths[j]-utf8.RuneCountInString(v)+2))
		}
		style := ""
		if color && i == 0 {
			style = sgrBold
		} else if color {
			style = stateColor(st[i-1].State)
		}
		if style != "" {
			b.WriteString(style + row[last] + sgrReset)
		} else {
			b.WriteString(row[last])
		}
		b.WriteString("\n")
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// statusRow returns the cells of one submodule. Empty values are shown as
// "-"; the tracking columns of an unmanaged submodule are always empty, as
// in the porcelain format.
func statusRow(s core.Status) []string {
	sub := s.Submodule
	row := []string{displayName(sub.Name), cell(sub.Path), "", "", "", cell(short(s.Head)),
		cell(string(s.State))}
	if sub.Managed() {
		row[2] = cell(string(sub.Mode))
		row[3] = cell(configuredRef(sub))
		row[4] = cell(lockCell(sub, s.Lock))
	}
	for i, v := range row {
		if v == "" {
			row[i] = "-"
		}
	}
	return row
}

// cell returns a value for the table, quoted like a name when it contains
// characters that would break the layout or reach the terminal.
func cell(v string) string {
	if v == "" {
		return ""
	}
	return displayName(v)
}

// configuredRef returns the configured ref, abbreviated in commit mode.
func configuredRef(sub manifest.Submodule) string {
	if sub.Mode == manifest.ModeCommit && lock.ValidCommit(sub.Ref) {
		return short(sub.Ref)
	}
	return sub.Ref
}

// lockCell describes the lock entry: the abbreviated commit, followed by
// the locked ref when it is not the configured one, such as the tag that a
// tag pattern selected.
func lockCell(sub manifest.Submodule, e *lock.Entry) string {
	switch {
	case e == nil:
		return ""
	case e.Mode == manifest.ModeCommit || e.Ref == sub.Ref:
		return short(e.Commit)
	}
	return short(e.Commit) + " (" + e.Ref + ")"
}
