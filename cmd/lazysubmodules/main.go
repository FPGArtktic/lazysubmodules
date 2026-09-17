// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

// Command lazysubmodules manages Git submodules that track branches, tags,
// tag patterns or commits.
//
// Exit status: 0 on success, 1 on a generic error, 2 on a usage error, 3 when
// an operation is refused because the state is unsafe, 4 when verification
// fails and 5 when a git command fails. README.md describes every command.
package main

import (
	"context"
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

// env is the process environment of one run; tests provide their own.
type env struct {
	// args are the command line arguments without the program name.
	args []string
	// dir is the directory the command runs in; empty means the current
	// working directory.
	dir string
	// environ is the process environment, consulted for NO_COLOR and TERM
	// and passed to the terminal interface.
	environ []string
	// gitEnv holds KEY=VALUE pairs added to the environment of every git
	// invocation, after the inherited environment.
	gitEnv []string
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
	// terminal reports whether a stream is a terminal; nil means isTerminal.
	terminal func(stream any) bool
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
		addCommand(),
		setCommand(),
		updateCommand(),
		statusCommand(),
		fetchCommand(),
		verifyCommand(),
		foreachCommand(),
		tuiCommand(),
		{
			name:    "version",
			summary: "print version, commit and build date",
			help:    "Print the version, the source commit and the build date.",
			run:     runVersion,
		},
		{
			name:     "help",
			synopsis: "[<command>]",
			summary:  "show help for lazysubmodules or a command",
			help:     "Show the list of commands, or the help of one command.",
			run:      runHelp,
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
	ctx, stop := signal.NotifyContext(context.Background(), stopSignals()...)
	// The first signal cancels the running operation, which then puts
	// things back; a second one ends the process at once.
	context.AfterFunc(ctx, stop)
	code := run(ctx, &env{
		args:    os.Args[1:],
		environ: os.Environ(),
		stdin:   os.Stdin,
		stdout:  os.Stdout,
		stderr:  os.Stderr,
	})
	stop()
	os.Exit(code)
}

// stopSignals returns the signals that cancel the running operation:
// SIGTERM, SIGINT and SIGHUP, which a terminal sends when it is closed.
// SIGINT and SIGHUP stay ignored when the process was started with them
// ignored, as nohup does, since handling a signal ends its inherited
// ignoring.
func stopSignals() []os.Signal {
	signals := []os.Signal{syscall.SIGTERM}
	for _, sig := range []os.Signal{os.Interrupt, syscall.SIGHUP} {
		if !signal.Ignored(sig) {
			signals = append(signals, sig)
		}
	}
	return signals
}

// run executes the command line described by e and returns the exit status.
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
		return e.usageError(fmt.Sprintf("unknown %s %q", kind, name))
	}
	return c.run(ctx, e, c, args)
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
		return e.usageError("help: too many arguments")
	}
	target, ok := lookup(args[0])
	if !ok {
		return e.usageError(fmt.Sprintf("help: unknown command %q", args[0]))
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
		return e.usageError(fmt.Sprintf("version: unexpected argument %q", args[0]))
	}
	bi, ok := debug.ReadBuildInfo()
	info := resolveBuildInfo(buildInfo{version: version, commit: commit, date: date}, bi, ok)
	_, err := fmt.Fprintf(e.stdout, "%s %s\ncommit: %s\ndate: %s\n",
		progName, info.version, info.commit, info.date)
	if err != nil {
		return e.fail(err)
	}
	return exitOK
}
