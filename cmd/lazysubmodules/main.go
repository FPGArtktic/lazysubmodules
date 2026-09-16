// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

// Command lazysubmodules manages Git submodules that track branches or tags.
//
// Exit status: 0 on success, 1 on a generic error, 2 on a usage error.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"syscall"
)

// Build information, set at link time with
// -ldflags "-X main.version=... -X main.commit=... -X main.date=...".
var version, commit, date = "dev", "none", "unknown" //nolint:gochecknoglobals // set by -ldflags

const progName = "lazysubmodules"

// Exit statuses, documented in README.md.
const (
	exitOK    = 0
	exitError = 1
	exitUsage = 2
)

// env is the process environment of one run; tests provide their own.
type env struct {
	// args are the command line arguments without the program name.
	args   []string
	stdout io.Writer
	stderr io.Writer
}

// command is one entry of the command table.
type command struct {
	name     string
	synopsis string // arguments after the command name in the usage line
	summary  string // one line for the command list
	help     string // description printed by "<command> -h"
	// run executes the command; c is the table entry itself.
	run func(ctx context.Context, e *env, c command, args []string) int
}

// commands returns the command table in the order of the usage text.
func commands() []command {
	return []command{
		{
			name:     "help",
			synopsis: "[<command>]",
			summary:  "show help for lazysubmodules or a command",
			help:     "Show the list of commands, or the help of one command.",
			run:      runHelp,
		},
		{
			name:    "version",
			summary: "print version, commit and build date",
			help:    "Print the version, the source commit and the build date.",
			run:     runVersion,
		},
	}
}

// lookup finds a command by name.
func lookup(name string) (command, bool) {
	for _, c := range commands() {
		if c.name == name {
			return c, true
		}
	}
	return command{}, false
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, &env{args: os.Args[1:], stdout: os.Stdout, stderr: os.Stderr})
	stop()
	os.Exit(code)
}

// run executes the command line described by e.
func run(ctx context.Context, e *env) int {
	if len(e.args) == 0 {
		writeUsage(e.stderr)
		return exitUsage
	}
	name, args := e.args[0], e.args[1:]
	switch name {
	case "-h", "--help":
		name = "help"
	case "--version":
		name = "version"
	}
	c, ok := lookup(name)
	if !ok {
		kind := "command"
		if strings.HasPrefix(name, "-") {
			kind = "option"
		}
		e.usageError(fmt.Sprintf("unknown %s %q", kind, name))
		return exitUsage
	}
	return c.run(ctx, e, c, args)
}

// usageError reports a command line error.
func (e *env) usageError(msg string) {
	fmt.Fprintf(e.stderr, "%s: %s\nRun '%s help' for usage.\n", progName, msg, progName)
}

// writeUsage prints the program usage.
func writeUsage(w io.Writer) {
	fmt.Fprintf(w, "Usage: %s <command> [arguments]\n\n", progName)
	fmt.Fprintf(w, "Manage Git submodules that track branches or tags.\n\nCommands:\n")
	cmds := commands()
	width := 0
	for _, c := range cmds {
		width = max(width, len(c.name))
	}
	for _, c := range cmds {
		fmt.Fprintf(w, "  %-*s  %s\n", width, c.name, c.summary)
	}
	fmt.Fprintf(w, "\nRun '%s <command> -h' for help on a command.\n", progName)
}

// newFlagSet returns a flag set whose errors and help are reported by
// parseArgs instead of the flag package.
func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	return fs
}

// parseArgs parses the flags of command c, which may appear anywhere before
// "--", and handles -h and flag errors.
//
// It returns the positional arguments, or done=true with the exit status when
// the command must not continue.
func (e *env) parseArgs(c command, fs *flag.FlagSet, args []string) (
	positional []string, code int, done bool) {
	positional, err := splitArgs(fs, args)
	switch {
	case errors.Is(err, flag.ErrHelp):
		writeCommandUsage(e.stdout, c, fs)
		return nil, exitOK, true
	case err != nil:
		e.usageError(c.name + ": " + err.Error())
		return nil, exitUsage, true
	}
	return positional, exitOK, false
}

// splitArgs parses the flags in args, which may be interspersed with
// positional arguments, and returns the positional arguments. Everything
// after "--" is positional; a lone "-" is positional.
func splitArgs(fs *flag.FlagSet, args []string) ([]string, error) {
	var flags, positional []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--":
			positional = append(positional, args[i+1:]...)
			i = len(args)
		case len(arg) > 1 && arg[0] == '-':
			flags = append(flags, arg)
			if takesValue(fs, arg) && i+1 < len(args) {
				i++
				flags = append(flags, args[i])
			}
		default:
			positional = append(positional, arg)
		}
	}
	// The error text of the flag package is the message shown to the user.
	if err := fs.Parse(flags); err != nil {
		return nil, err
	}
	return positional, nil
}

// takesValue reports whether the flag in arg consumes the next argument.
func takesValue(fs *flag.FlagSet, arg string) bool {
	name := strings.TrimLeft(arg, "-")
	if strings.Contains(name, "=") {
		return false
	}
	f := fs.Lookup(name)
	if f == nil {
		return false
	}
	b, ok := f.Value.(interface{ IsBoolFlag() bool })
	return !ok || !b.IsBoolFlag()
}

// writeCommandUsage prints the help of command c.
func writeCommandUsage(w io.Writer, c command, fs *flag.FlagSet) {
	line := progName + " " + c.name
	if c.synopsis != "" {
		line += " " + c.synopsis
	}
	fmt.Fprintf(w, "Usage: %s\n\n%s\n", line, c.help)
	hasFlags := false
	fs.VisitAll(func(*flag.Flag) { hasFlags = true })
	if hasFlags {
		fmt.Fprintf(w, "\nOptions:\n")
		fs.SetOutput(w)
		fs.PrintDefaults()
		fs.SetOutput(io.Discard)
	}
}

// runHelp implements "help [<command>]".
func runHelp(ctx context.Context, e *env, c command, args []string) int {
	args, code, done := e.parseArgs(c, newFlagSet(c.name), args)
	switch {
	case done:
		return code
	case len(args) == 0:
		writeUsage(e.stdout)
		return exitOK
	case len(args) > 1:
		e.usageError("help: too many arguments")
		return exitUsage
	}
	target, ok := lookup(args[0])
	if !ok {
		e.usageError(fmt.Sprintf("help: unknown command %q", args[0]))
		return exitUsage
	}
	return target.run(ctx, e, target, []string{"-h"})
}

// buildInfo describes the running binary.
type buildInfo struct {
	version string
	commit  string
	date    string
}

// resolveBuildInfo completes the link-time values of a binary built without
// -ldflags with the module version and VCS data recorded by the go command.
func resolveBuildInfo(linked buildInfo, bi *debug.BuildInfo, ok bool) buildInfo {
	res := linked
	if linked.version != "dev" || !ok || bi == nil {
		return res
	}
	if v := bi.Main.Version; v != "" && v != "(devel)" {
		res.version = v
	}
	for _, s := range bi.Settings {
		switch {
		case s.Key == "vcs.revision" && res.commit == "none":
			res.commit = s.Value
		case s.Key == "vcs.time" && res.date == "unknown":
			res.date = s.Value
		}
	}
	return res
}

// runVersion implements "version".
func runVersion(_ context.Context, e *env, c command, args []string) int {
	args, code, done := e.parseArgs(c, newFlagSet(c.name), args)
	if done {
		return code
	}
	if len(args) > 0 {
		e.usageError(fmt.Sprintf("version: unexpected argument %q", args[0]))
		return exitUsage
	}
	bi, ok := debug.ReadBuildInfo()
	info := resolveBuildInfo(buildInfo{version: version, commit: commit, date: date}, bi, ok)
	_, err := fmt.Fprintf(e.stdout, "%s %s\ncommit: %s\ndate: %s\n",
		progName, info.version, info.commit, info.date)
	if err != nil {
		fmt.Fprintf(e.stderr, "%s: %v\n", progName, err)
		return exitError
	}
	return exitOK
}
