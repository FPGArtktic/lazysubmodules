// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

// Package tui implements the terminal user interface of LazySubmodules.
//
// The interface lists the submodules of a superproject in a table, shows a
// preview of the selected one next to it, and runs the operations of the
// command line interface on the selected submodule. It holds no business
// logic: every action is a call of a Backend, which *core.Repo implements.
// Calls run in the background with the context of the program while a
// spinner turns, only one call that changes something runs at a time,
// operations that modify the superproject ask for confirmation, and errors
// are shown in the status bar instead of ending the program.
package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"sync"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"

	"github.com/FPGArtktic/lazysubmodules/internal/core"
	"github.com/FPGArtktic/lazysubmodules/internal/manifest"
)

// Backend is the business logic the interface calls. *core.Repo implements
// it; the methods are documented there.
type Backend interface {
	// Status reports the state of submodules.
	Status(ctx context.Context, names []string) ([]core.Status, error)
	// Preview summarizes the local history of a submodule.
	Preview(ctx context.Context, name string) (core.Preview, error)
	// Update moves managed submodules to their targets.
	Update(ctx context.Context, opts core.UpdateOptions) (core.UpdateResult, error)
	// Set changes the tracking configuration of a submodule.
	Set(ctx context.Context, name string, mode manifest.Mode, ref string) (
		manifest.Submodule, error)
	// Fetch downloads the refs of managed submodules; it is the only call
	// that uses the network.
	Fetch(ctx context.Context, names []string, progress io.Writer) ([]core.FetchResult, error)
	// Verify checks the lock file, the gitlinks and the checkouts.
	Verify(ctx context.Context, names []string, opts core.VerifyOptions) (
		[]core.VerifyResult, error)
	// Branches lists the remote-tracking branches of a submodule.
	Branches(ctx context.Context, name string) ([]string, error)
	// Tags lists the local tags of a submodule matching a pattern.
	Tags(ctx context.Context, name, pattern string) ([]string, error)
	// GitlinkDiff shows how a submodule differs from its gitlink in HEAD.
	GitlinkDiff(ctx context.Context, name string) (string, error)
}

// Options configures the terminal.
type Options struct {
	// Input is the keyboard input. When nil, standard input is used, or
	// the controlling terminal when standard input is not a terminal.
	Input io.Reader
	// Output receives the screen. When nil, standard output is used.
	Output io.Writer
	// Environ is the environment used to detect the capabilities of the
	// terminal. When nil, the environment of the process is used.
	Environ []string
	// NoColor disables colors, as NO_COLOR asks for. Bold text and
	// reverse video, which mark headings and the selection, remain.
	NoColor bool
}

// New creates the model of the interface.
//
// Run uses it; tests drive it with a Bubble Tea program of their own. The
// model starts loading the status of all submodules when the program
// initializes it.
//
// Context: ctx is the program context; every Backend call runs with it,
// so canceling it stops running calls. opts.Input, opts.Output and
// opts.Environ are not used by the model.
// Return: the model.
func New(ctx context.Context, b Backend, opts Options) Model {
	return newModel(ctx, b, opts.NoColor)
}

// Run shows the interface until the user quits.
//
// The screen switches to the alternate buffer. Colors are chosen from the
// capabilities of the terminal and turned off with opts.NoColor. When the
// program ends, the context of the Backend calls is canceled, and Run
// returns only after every running call has returned, so that an
// interrupted update can put things back. The outcome of a modifying
// operation that ended after the screen was closed is reported then: a
// success on opts.Output, a failure in the returned error.
//
// Context: the terminal must be a TTY; the caller checks that. Canceling
// ctx ends the program; Run handles no signals itself, so the caller turns
// SIGINT and SIGTERM into a cancellation.
// Return: nil when the user quit and every operation succeeded; otherwise
// an error joining the reasons: tea.ErrInterrupted, tea.ErrProgramKilled
// together with the cause of the cancellation of ctx, and the errors of
// operations whose outcome was not shown.
func Run(ctx context.Context, b Backend, opts Options) error {
	parent := ctx
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	environ := opts.Environ
	if environ == nil {
		environ = os.Environ()
	}
	var output io.Writer = os.Stdout
	if opts.Output != nil {
		output = opts.Output
	}
	profile, noColor := colors(output, environ, opts.NoColor)
	m := New(ctx, b, Options{NoColor: noColor})
	popts := []tea.ProgramOption{
		tea.WithContext(ctx),
		// The signal handler of Bubble Tea would compete with the caller
		// for SIGINT and SIGTERM, and once ctx has ended it blocks the
		// shutdown of the program for good.
		tea.WithoutSignalHandler(),
		tea.WithOutput(output),
		tea.WithEnvironment(environ),
		tea.WithColorProfile(profile),
	}
	if opts.Input != nil {
		popts = append(popts, tea.WithInput(opts.Input))
	}
	_, err := tea.NewProgram(m, popts...).Run()
	cancel()
	unseen := m.jobs.close()
	if err != nil && parent.Err() != nil {
		err = &stopError{cause: context.Cause(parent), err: err}
	}
	if err != nil {
		err = fmt.Errorf("terminal interface: %w", err)
	}
	return report(output, unseen, err)
}

// stopError reports that the program ended because its context ended. Its
// text is the cause of the cancellation, such as the signal received.
type stopError struct {
	cause, err error
}

// Error returns the cause of the cancellation.
func (e *stopError) Error() string {
	return e.cause.Error()
}

// Unwrap returns the cause and the error of the program.
func (e *stopError) Unwrap() []error {
	return []error{e.cause, e.err}
}

// report writes the successful outcomes that the screen did not show and
// joins the failed ones with err.
func report(w io.Writer, outcomes []outcome, err error) error {
	errs := []error{err}
	for _, o := range outcomes {
		if o.err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", o.label, o.err))
			continue
		}
		fmt.Fprintln(w, o.text)
	}
	return errors.Join(errs...)
}

// colors returns the color profile of the output and whether the styles
// without colors are needed. The profile is detected from the output and
// the environment; noColor limits it to text attributes. The detection
// honors NO_COLOR only for values such as "1" or "true", while NO_COLOR
// asks to disable colors for any non-empty value, so the caller decides
// and passes noColor.
func colors(output io.Writer, environ []string, noColor bool) (colorprofile.Profile, bool) {
	profile := colorprofile.Detect(output, environ)
	if noColor {
		profile = min(profile, colorprofile.ASCII)
	}
	return profile, profile <= colorprofile.ASCII
}

// jobs tracks the Backend calls in progress, so that Run can wait for
// them, and the outcomes of modifying operations until the model shows
// them. Once closed, no call starts any more.
type jobs struct {
	mu     sync.Mutex
	closed bool
	wg     sync.WaitGroup
	// last numbers the outcomes; unseen holds those not shown yet.
	last   int
	unseen map[int]outcome
}

// outcome is the result of a modifying operation.
type outcome struct {
	label string
	text  string
	err   error
}

// begin registers a call that is about to start; it returns false when the
// program has ended and the call must not start.
func (j *jobs) begin() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.closed {
		return false
	}
	j.wg.Add(1)
	return true
}

// end registers the end of a call started after begin returned true.
func (j *jobs) end() {
	j.wg.Done()
}

// finished records the outcome of an operation until shown is called with
// the number it returns.
func (j *jobs) finished(o outcome) int {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.unseen == nil {
		j.unseen = map[int]outcome{}
	}
	j.last++
	j.unseen[j.last] = o
	return j.last
}

// shown forgets an outcome that the model has shown.
func (j *jobs) shown(id int) {
	j.mu.Lock()
	defer j.mu.Unlock()
	delete(j.unseen, id)
}

// close stops further calls, waits for the running ones and returns the
// outcomes that were not shown, oldest first.
func (j *jobs) close() []outcome {
	j.mu.Lock()
	j.closed = true
	j.mu.Unlock()
	j.wg.Wait()
	j.mu.Lock()
	defer j.mu.Unlock()
	ids := slices.Sorted(maps.Keys(j.unseen))
	out := make([]outcome, 0, len(ids))
	for _, id := range ids {
		out = append(out, j.unseen[id])
	}
	return out
}
