// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"slices"
	"strings"
	"sync"

	"github.com/FPGArtktic/lazysubmodules/internal/core"
	"github.com/FPGArtktic/lazysubmodules/internal/lock"
	"github.com/FPGArtktic/lazysubmodules/internal/manifest"
)

// errFake is the error the fake backend returns when told to fail.
var errFake = errors.New("fake failure")

// sha returns a fake full commit name starting with prefix.
func sha(prefix string) string {
	return prefix + strings.Repeat("0", 40-len(prefix))
}

// fakeBackend implements Backend in memory. Modifying calls can be held
// at a gate, and every call is recorded.
type fakeBackend struct {
	mu sync.Mutex

	statuses  []core.Status
	statusErr error
	previews  map[string]core.Preview
	branches  map[string][]string
	tags      map[string][]string
	diffs     map[string]string
	checks    map[string][]core.Check

	updateErr, planErr, setErr, fetchErr, previewErr, tagsErr error
	// committed holds the lock entries of HEAD that differ from the index,
	// which Status reports; an update with a commit compares with them.
	committed map[string]lock.Entry

	// gate, when set, holds modifying calls until it is closed or, unless
	// holdOnCancel is set, their context ends; started receives the name of
	// each held call.
	gate         chan struct{}
	started      chan string
	holdOnCancel bool
	// interrupted records the context errors seen by held calls.
	interrupted []error

	calls   []string
	updates []core.UpdateOptions
	sets    []string
	fetches [][]string
}

// newFake returns a fake with the submodules of the screenshots in the
// documentation.
func newFake() *fakeBackend {
	return &fakeBackend{
		statuses: sampleStatuses(),
		previews: map[string]core.Preview{
			"kernel": {
				Log: []string{"a1b2c3d Linux v6.6.8", "9f8e7d6 Linux v6.6.7",
					"8e7d6c5 Linux v6.6.6"},
				Tags:    []string{"v6.6.9", "v6.6.8", "v6.6.7", "v6.6.6"},
				Pending: []string{"e4f5a6b Linux v6.6.9"},
			},
			"u-boot": {
				Log:     []string{"d4e5f6a U-Boot v2026.01"},
				Pending: []string{"77aa88b board: add a new board"},
			},
		},
		branches: map[string][]string{
			"u-boot": {"main", "next", "stable"},
			"kernel": {"linux-6.6.y", "master"},
		},
		tags: map[string][]string{
			"kernel": {"v6.7-rc1", "v6.6.10", "v6.6.9", "v6.6.8", "v6.6.1"},
			"u-boot": {"v2026.01", "v2025.10"},
		},
		diffs: map[string]string{
			"kernel": "Submodule kernel a1b2c3d..e4f5a6b:\n  > Linux v6.6.9\n",
		},
		checks: map[string][]core.Check{
			"kernel": {
				{Name: core.CheckLockEntry, OK: true, Detail: "tag-pattern v6.6.8"},
				{Name: core.CheckHead, OK: true, Detail: "HEAD is the locked commit"},
			},
			"fpga-ip": {
				{Name: core.CheckLockEntry, OK: true, Detail: "tag v2.3.1"},
				{Name: core.CheckTag, OK: false, Detail: "tag v2.3.1 now points to 5c6d7e8f9a0b"},
			},
		},
	}
}

// sampleStatuses returns the status of the sample superproject.
func sampleStatuses() []core.Status {
	entry := func(name string, mode manifest.Mode, ref, commit string) *lock.Entry {
		return &lock.Entry{Name: name, Mode: mode, Ref: ref, Commit: commit}
	}
	sub := func(name string, mode manifest.Mode, ref string) manifest.Submodule {
		return manifest.Submodule{Name: name, Path: name, URL: "https://git.example.org/" + name,
			Mode: mode, Ref: ref}
	}
	return []core.Status{
		{Submodule: sub("kernel", manifest.ModeTagPattern, "v6.6.*"),
			Lock: entry("kernel", manifest.ModeTagPattern, "v6.6.8", sha("a1b2c3d4e5f6")),
			Head: sha("a1b2c3d4e5f6"),
			Target: &core.Resolution{Mode: manifest.ModeTagPattern, Ref: "v6.6.9",
				Commit: sha("e4f5a6b7c8d9")},
			State: core.StateBehind, Reason: "update would select tag-pattern v6.6.9 instead of " +
				"tag-pattern v6.6.8"},
		{Submodule: sub("u-boot", manifest.ModeBranch, "main"),
			Lock:   entry("u-boot", manifest.ModeBranch, "main", sha("d4e5f6a7")),
			Head:   sha("d4e5f6a7"),
			Target: &core.Resolution{Mode: manifest.ModeBranch, Ref: "main", Commit: sha("77aa88bb")},
			State:  core.StateBehind, Reason: "branch main now resolves to 77aa88bb0000, " +
				"locked d4e5f6a70000"},
		{Submodule: sub("fpga-ip", manifest.ModeTag, "v2.3.1"),
			Lock:   entry("fpga-ip", manifest.ModeTag, "v2.3.1", sha("0718ab2")),
			Head:   sha("0718ab2"),
			Target: &core.Resolution{Mode: manifest.ModeTag, Ref: "v2.3.1", Commit: sha("5c6d7e8f")},
			State:  core.StateDrift, Reason: "locked tag v2.3.1 now points to 5c6d7e8f0000, " +
				"not 0718ab200000"},
		{Submodule: sub("crypto lib", manifest.ModeCommit, sha("3f2a1b")),
			Lock:   entry("crypto lib", manifest.ModeCommit, sha("3f2a1b"), sha("3f2a1b")),
			Head:   sha("3f2a1b"),
			Target: &core.Resolution{Mode: manifest.ModeCommit, Ref: sha("3f2a1b"), Commit: sha("3f2a1b")},
			State:  core.StateOK, Reason: "up to date"},
		{Submodule: sub("legacy", "", ""), Head: sha("99"), State: core.StateUnmanaged,
			Reason: "no lsm-mode key"},
		{Submodule: sub("theme", manifest.ModeTag, "v1.0.0"),
			Lock:  entry("theme", manifest.ModeTag, "v1.0.0", sha("5e5e")),
			State: core.StateUninitialized, Reason: "submodule is not checked out"},
	}
}

// record appends a call to the log.
func (f *fakeBackend) record(format string, args ...any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, fmt.Sprintf(format, args...))
}

// hold waits at the gate of modifying calls.
func (f *fakeBackend) hold(ctx context.Context, what string) error {
	f.mu.Lock()
	gate, started, done := f.gate, f.started, ctx.Done()
	if f.holdOnCancel {
		done = nil
	}
	f.mu.Unlock()
	if gate == nil {
		return nil
	}
	if started != nil {
		started <- what
	}
	select {
	case <-gate:
		return nil
	case <-done:
		f.mu.Lock()
		f.interrupted = append(f.interrupted, ctx.Err())
		f.mu.Unlock()
		return ctx.Err()
	}
}

// Status implements Backend.
func (f *fakeBackend) Status(_ context.Context, names []string) ([]core.Status, error) {
	f.record("Status %q", names)
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.statuses), f.statusErr
}

// Preview implements Backend.
func (f *fakeBackend) Preview(_ context.Context, name string) (core.Preview, error) {
	f.record("Preview %s", name)
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.previews[name], f.previewErr
}

// Update implements Backend. A successful update locks the target; a dry
// run, which is logged as "Plan", changes nothing. A submodule that is not
// checked out is initialized at its lock entry when it has no target.
func (f *fakeBackend) Update(ctx context.Context, opts core.UpdateOptions) (core.UpdateResult,
	error,
) {
	if opts.DryRun {
		f.record("Plan %q commit=%t fetch=%t", opts.Names, opts.Commit, opts.Fetch)
	} else {
		f.record("Update %q commit=%t fetch=%t", opts.Names, opts.Commit, opts.Fetch)
		f.mu.Lock()
		f.updates = append(f.updates, opts)
		f.mu.Unlock()
		if err := f.hold(ctx, "update"); err != nil {
			return core.UpdateResult{}, err
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case opts.DryRun && f.planErr != nil:
		return core.UpdateResult{}, f.planErr
	case !opts.DryRun && f.updateErr != nil:
		return core.UpdateResult{}, f.updateErr
	}
	var res core.UpdateResult
	for i, st := range f.statuses {
		c, ok := f.change(st, opts.Commit)
		if !slices.Contains(opts.Names, st.Submodule.Name) || !ok {
			continue
		}
		res.Changes = append(res.Changes, c)
		if opts.Commit && recordsChange(c) {
			res.Commit = sha("c0ffee")
		}
		if opts.DryRun {
			continue
		}
		t := c.New
		f.statuses[i].Lock = &lock.Entry{Name: st.Submodule.Name, Mode: t.Mode, Ref: t.Ref,
			Commit: t.Commit}
		f.statuses[i].Head = t.Commit
		f.statuses[i].Target = &t
		f.statuses[i].State, f.statuses[i].Reason = core.StateOK, "up to date"
		if opts.Commit {
			delete(f.committed, st.Submodule.Name)
		}
	}
	return res, nil
}

// change returns the update of a submodule. The superproject records its
// lock entry as the gitlink, or with commit the entry of committed.
func (f *fakeBackend) change(st core.Status, commit bool) (core.Change, bool) {
	c := core.Change{Submodule: st.Submodule, Old: st.Lock, OldHead: st.Head,
		Init: st.Head == ""}
	if e, ok := f.committed[st.Submodule.Name]; ok && commit {
		c.Old = &e
	}
	if c.Old != nil {
		c.OldGitlink = c.Old.Commit
	}
	switch {
	case st.Target != nil:
		c.New = *st.Target
	case st.Lock != nil && c.Init:
		c.New = core.Resolution{Mode: st.Lock.Mode, Ref: st.Lock.Ref, Commit: st.Lock.Commit}
	default:
		return core.Change{}, false
	}
	return c, true
}

// Set implements Backend.
func (f *fakeBackend) Set(ctx context.Context, name string, mode manifest.Mode, ref string) (
	manifest.Submodule, error,
) {
	f.record("Set %s %s %s", name, mode, ref)
	f.mu.Lock()
	f.sets = append(f.sets, fmt.Sprintf("%s %s %s", name, mode, ref))
	f.mu.Unlock()
	if err := f.hold(ctx, "set"); err != nil {
		return manifest.Submodule{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.setErr != nil {
		return manifest.Submodule{}, f.setErr
	}
	for i, st := range f.statuses {
		if st.Submodule.Name == name {
			f.statuses[i].Submodule.Mode, f.statuses[i].Submodule.Ref = mode, ref
			return f.statuses[i].Submodule, nil
		}
	}
	return manifest.Submodule{}, fmt.Errorf("%s: %w", name, core.ErrNotFound)
}

// Fetch implements Backend.
func (f *fakeBackend) Fetch(ctx context.Context, names []string, _ io.Writer) (
	[]core.FetchResult, error,
) {
	f.record("Fetch %q", names)
	f.mu.Lock()
	f.fetches = append(f.fetches, names)
	f.mu.Unlock()
	if err := f.hold(ctx, "fetch"); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fetchErr != nil {
		return nil, f.fetchErr
	}
	res := make([]core.FetchResult, 0, len(names))
	for _, n := range names {
		res = append(res, core.FetchResult{Submodule: manifest.Submodule{Name: n}})
	}
	return res, nil
}

// Verify implements Backend.
func (f *fakeBackend) Verify(_ context.Context, names []string, _ core.VerifyOptions) (
	[]core.VerifyResult, error,
) {
	f.record("Verify %q", names)
	f.mu.Lock()
	defer f.mu.Unlock()
	var res []core.VerifyResult
	var err error
	for _, n := range names {
		r := core.VerifyResult{Submodule: manifest.Submodule{Name: n}, Checks: f.checks[n]}
		if !r.OK() {
			err = fmt.Errorf("%s: %w", n, core.ErrVerify)
		}
		res = append(res, r)
	}
	return res, err
}

// Branches implements Backend.
func (f *fakeBackend) Branches(_ context.Context, name string) ([]string, error) {
	f.record("Branches %s", name)
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.branches[name], nil
}

// Tags implements Backend with path.Match as the glob.
func (f *fakeBackend) Tags(_ context.Context, name, pattern string) ([]string, error) {
	f.record("Tags %s %q", name, pattern)
	if strings.ContainsAny(pattern, " -") {
		return nil, fmt.Errorf("%s: invalid tag pattern %q: %w", name, pattern,
			core.ErrInvalidArgument)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.tagsErr != nil {
		return nil, f.tagsErr
	}
	var out []string
	for _, t := range f.tags[name] {
		if ok, _ := path.Match(pattern, t); ok || pattern == "" {
			out = append(out, t)
		}
	}
	return out, nil
}

// GitlinkDiff implements Backend.
func (f *fakeBackend) GitlinkDiff(_ context.Context, name string) (string, error) {
	f.record("GitlinkDiff %s", name)
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.diffs[name], nil
}

// callLog returns the recorded calls.
func (f *fakeBackend) callLog() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

// countCalls returns how many recorded calls start with prefix.
func (f *fakeBackend) countCalls(prefix string) int {
	n := 0
	for _, c := range f.callLog() {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

// set changes the fake under its lock.
func (f *fakeBackend) set(fn func(f *fakeBackend)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f)
}
