// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/FPGArtktic/lazysubmodules/internal/manifest"
)

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
	positional []string, code int, done bool,
) {
	positional, err := splitArgs(fs, args)
	return e.checkParse(c, fs, positional, err)
}

// checkParse handles the result of parsing the flags of command c: help is
// printed for -h, and any other error is a usage error.
func (e *env) checkParse(c command, fs *flag.FlagSet, positional []string, err error) (
	[]string, int, bool,
) {
	switch {
	case errors.Is(err, flag.ErrHelp):
		writeCommandUsage(e.stdout, c, fs)
		return nil, exitOK, true
	case err != nil:
		return nil, e.usageError(c.name + ": " + err.Error()), true
	}
	return positional, exitOK, false
}

// optionalValue is implemented by the value of a flag whose argument is
// optional: the argument is given as "--name=value", and "--name" alone
// stands for "--name=<Implied()>". The next command line argument is never
// taken as the value.
type optionalValue interface {
	flag.Value
	// Implied returns the value of the flag given without an argument.
	Implied() string
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
			f := lookupFlag(fs, arg)
			if v, ok := optional(f); ok {
				arg += "=" + v.Implied()
			}
			flags = append(flags, arg)
			if f != nil && !isBoolFlag(f) && !isOptional(f) && i+1 < len(args) {
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

// lookupFlag returns the flag named by arg when arg has no "=value" part,
// and nil otherwise or when there is no such flag.
func lookupFlag(fs *flag.FlagSet, arg string) *flag.Flag {
	name := strings.TrimLeft(arg, "-")
	if strings.Contains(name, "=") || len(arg)-len(name) > 2 {
		return nil
	}
	return fs.Lookup(name)
}

// optional returns the value of f when its argument is optional.
func optional(f *flag.Flag) (optionalValue, bool) {
	if f == nil {
		return nil, false
	}
	v, ok := f.Value.(optionalValue)
	return v, ok
}

// isOptional reports whether the argument of f is optional.
func isOptional(f *flag.Flag) bool {
	_, ok := optional(f)
	return ok
}

// isBoolFlag reports whether f is set without a value, like a bool flag.
func isBoolFlag(f *flag.Flag) bool {
	b, ok := f.Value.(interface{ IsBoolFlag() bool })
	return ok && b.IsBoolFlag()
}

// writeCommandUsage prints the help of command c.
func writeCommandUsage(w io.Writer, c command, fs *flag.FlagSet) {
	line := progName + " " + c.name
	if c.synopsis != "" {
		line += " " + c.synopsis
	}
	fmt.Fprintf(w, "Usage: %s\n\n%s\n", line, c.help)
	writeOptions(w, fs)
}

// writeOptions lists the flags of fs in the "--name <value>" form that
// README.md uses.
func writeOptions(w io.Writer, fs *flag.FlagSet) {
	type option struct{ name, usage string }
	var opts []option
	width := 0
	fs.VisitAll(func(f *flag.Flag) {
		value, usage := flag.UnquoteUsage(f)
		name := "--" + f.Name
		switch {
		case isOptional(f):
			name += "[=" + value + "]"
		case !isBoolFlag(f):
			name += " <" + value + ">"
		}
		width = max(width, len(name))
		opts = append(opts, option{name, usage})
	})
	if len(opts) == 0 {
		return
	}
	fmt.Fprintf(w, "\nOptions:\n")
	for _, o := range opts {
		fmt.Fprintf(w, "  %-*s  %s\n", width, o.name, o.usage)
	}
}

// tracking collects the mutually exclusive --branch, --tag, --tag-pattern
// and --commit flags.
type tracking struct {
	// set lists the modes in the order their flags were given.
	set []manifest.Mode
	ref string
}

// trackingValue is the flag.Value of one tracking flag.
type trackingValue struct {
	t    *tracking
	mode manifest.Mode
}

// String implements flag.Value.
//
// Context: any.
// Return: the ref given for this mode, or "".
func (v trackingValue) String() string {
	if v.t == nil || len(v.t.set) == 0 || v.t.set[len(v.t.set)-1] != v.mode {
		return ""
	}
	return v.t.ref
}

// Set implements flag.Value.
//
// Context: called by the flag package for each occurrence of the flag.
// Return: nil; conflicts are reported by tracking.mode.
func (v trackingValue) Set(ref string) error {
	v.t.set = append(v.t.set, v.mode)
	v.t.ref = ref
	return nil
}

// addTrackingFlags defines the tracking flags on fs.
func addTrackingFlags(fs *flag.FlagSet) *tracking {
	t := &tracking{}
	for _, f := range []struct {
		mode  manifest.Mode
		usage string
	}{
		{manifest.ModeBranch, "track the tip of the remote `branch`"},
		{manifest.ModeTag, "track the `tag`"},
		{manifest.ModeTagPattern, "track the highest version tag matching `pattern`"},
		{manifest.ModeCommit, "track the `commit` (a full or abbreviated SHA)"},
	} {
		fs.Var(trackingValue{t: t, mode: f.mode}, string(f.mode), f.usage)
	}
	return t
}

// trackingFlagNames lists the tracking flags for messages.
const trackingFlagNames = "--branch, --tag, --tag-pattern or --commit"

// mode returns the tracking mode and ref given on the command line.
//
// Return: an error unless exactly one tracking flag was given once.
func (t *tracking) mode() (manifest.Mode, string, error) {
	switch len(t.set) {
	case 0:
		return "", "", fmt.Errorf("one of %s is required", trackingFlagNames)
	case 1:
		return t.set[0], t.ref, nil
	}
	return "", "", fmt.Errorf("only one of %s may be given", trackingFlagNames)
}

// porcelainVersion is the value of "status --porcelain[=v1]".
type porcelainVersion string

// porcelainV1 is the only porcelain format.
const porcelainV1 porcelainVersion = "v1"

// String implements flag.Value.
//
// Context: any.
// Return: the selected format, or "" for the human-readable table.
func (p *porcelainVersion) String() string {
	if p == nil {
		return ""
	}
	return string(*p)
}

// Set implements flag.Value.
//
// Context: called by the flag package.
// Return: nil, or an error for a format other than v1.
func (p *porcelainVersion) Set(s string) error {
	if s != string(porcelainV1) {
		return fmt.Errorf("unsupported format %q (only v1 is supported)", s)
	}
	*p = porcelainV1
	return nil
}

// Implied lets a bare --porcelain select v1, as in git.
//
// Context: any.
// Return: "v1".
func (p *porcelainVersion) Implied() string {
	return string(porcelainV1)
}
