// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package core_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/FPGArtktic/lazysubmodules/internal/core"
	"github.com/FPGArtktic/lazysubmodules/internal/git"
	"github.com/FPGArtktic/lazysubmodules/internal/git/gittest"
	"github.com/FPGArtktic/lazysubmodules/internal/lock"
	"github.com/FPGArtktic/lazysubmodules/internal/manifest"
)

// openRepo opens the Repo containing dir.
func openRepo(t *testing.T, g *git.Runner, dir string) *core.Repo {
	t.Helper()
	r, err := core.Open(t.Context(), g, dir)
	if err != nil {
		t.Fatalf("Open(%s): %v", dir, err)
	}
	return r
}

// runGit runs git with g in dir and returns its trimmed output.
func runGit(t *testing.T, g *git.Runner, dir string, args ...string) string {
	t.Helper()
	out, err := g.Run(t.Context(), dir, args...)
	if err != nil {
		t.Fatalf("git %q: %v", args, err)
	}
	return strings.TrimSpace(out)
}

// mustRepoUpdate runs Update on r and fails the test on an error.
func mustRepoUpdate(t *testing.T, r *core.Repo, opts core.UpdateOptions) core.UpdateResult {
	t.Helper()
	res, err := r.Update(t.Context(), opts)
	if err != nil {
		t.Fatalf("Update(%+v): %v", opts, err)
	}
	return res
}

// wantRefusal checks that an update was refused with exactly the message
// want, wrapping every target, and returned no changes.
func wantRefusal(t *testing.T, what string, res core.UpdateResult, err error, want string,
	targets ...error,
) {
	t.Helper()
	wantErr(t, what, err, append([]error{core.ErrRefused}, targets...)...)
	if err != nil && err.Error() != want {
		t.Errorf("%s: error\n%s\nwant\n%s", what, err, want)
	}
	if len(res.Changes) != 0 || res.Commit != "" {
		t.Errorf("%s: result %+v", what, res)
	}
}

// statusRow is the expected state and reason of one submodule.
type statusRow struct {
	name   string
	state  core.State
	reason string
}

// wantStatusRows checks the names, states and reasons of a status list, in
// order.
func wantStatusRows(t *testing.T, got []core.Status, want []statusRow) {
	t.Helper()
	rows := make([]statusRow, 0, len(got))
	for _, st := range got {
		rows = append(rows, statusRow{st.Submodule.Name, st.State, st.Reason})
		if strings.ContainsFunc(st.Reason, unicode.IsControl) {
			t.Errorf("%q: reason %q contains control characters", st.Submodule.Name, st.Reason)
		}
	}
	if !slices.Equal(rows, want) {
		t.Errorf("status:\n%s\nwant\n%s", formatRows(rows), formatRows(want))
	}
}

// formatRows prints status rows one per line.
func formatRows(rows []statusRow) string {
	var b strings.Builder
	for _, r := range rows {
		b.WriteString(strconv.Quote(r.name) + "\t" + string(r.state) + "\t" +
			strconv.Quote(r.reason) + "\n")
	}
	return b.String()
}

// configOf returns the configuration that a fixture wrote for a submodule.
func configOf(s *gittest.Submodule) manifest.Submodule {
	return manifest.Submodule{Name: s.Name, Path: s.Path, URL: s.URL, Branch: s.Branch,
		Mode: manifest.Mode(s.Mode), Ref: s.Ref}
}

// lockOfFixture returns the lock entry that a fixture wrote for a
// submodule, or nil.
func lockOfFixture(s *gittest.Submodule) *lock.Entry {
	if s.Lock == nil {
		return nil
	}
	return &lock.Entry{Name: s.Name, Mode: manifest.Mode(s.Lock.Mode), Ref: s.Lock.Ref,
		Commit: s.Lock.Commit}
}

// lockedTarget returns the resolution that records the lock entry of a
// fixture submodule.
func lockedTarget(s *gittest.Submodule) *core.Resolution {
	return &core.Resolution{Mode: manifest.Mode(s.Lock.Mode), Ref: s.Lock.Ref,
		Commit: s.Lock.Commit}
}

// sameEntryPtr compares two optional lock entries by value.
func sameEntryPtr(a, b *lock.Entry) bool {
	return a == b || a != nil && b != nil && *a == *b
}

// sameResolutionPtr compares two optional resolutions by value.
func sameResolutionPtr(a, b *core.Resolution) bool {
	return a == b || a != nil && b != nil && *a == *b
}

// wantFixtureStatus checks that each status shows the configuration, lock
// entry and checked-out commit that the fixture recorded for the
// submodule at the same position, and the target in targets (by name).
func wantFixtureStatus(t *testing.T, got []core.Status, subs []*gittest.Submodule,
	targets map[string]*core.Resolution,
) {
	t.Helper()
	if len(got) != len(subs) {
		t.Fatalf("%d statuses, want %d", len(got), len(subs))
	}
	for i, st := range got {
		s := subs[i]
		if st.Submodule != configOf(s) {
			t.Errorf("%q: configuration %+v, want %+v", s.Name, st.Submodule, configOf(s))
		}
		if !sameEntryPtr(st.Lock, lockOfFixture(s)) || st.Head != s.Head {
			t.Errorf("%q: lock %+v, head %q; want %+v, %q", s.Name, st.Lock, st.Head,
				lockOfFixture(s), s.Head)
		}
		if want := targets[s.Name]; !sameResolutionPtr(st.Target, want) {
			t.Errorf("%q: target %+v, want %+v", s.Name, st.Target, want)
		}
	}
}

// changeView holds what a core.Change reports, so that changes compare as
// values.
type changeView struct {
	Submodule manifest.Submodule
	// Old is the zero Entry when Change.Old is nil.
	Old        lock.Entry
	OldHead    string
	OldGitlink string
	New        core.Resolution
	Init       bool
	Clone      bool
	Changed    bool
}

// viewOf returns the view of a change.
func viewOf(c core.Change) changeView {
	v := changeView{Submodule: c.Submodule, OldHead: c.OldHead, OldGitlink: c.OldGitlink,
		New: c.New, Init: c.Init, Clone: c.Clone, Changed: c.Changed()}
	if c.Old != nil {
		v.Old = *c.Old
	}
	return v
}

// fixtureView returns the view of a change of a fixture submodule from the
// state the fixture built to target: the recorded lock entry and gitlink,
// and the checked-out commit.
func fixtureView(s *gittest.Submodule, target core.Resolution, changed bool) changeView {
	v := changeView{Submodule: configOf(s), OldHead: s.Head, OldGitlink: s.Gitlink,
		New: target, Changed: changed}
	if e := lockOfFixture(s); e != nil {
		v.Old = *e
	}
	return v
}

// unchangedView returns the view of a fixture submodule that an update
// leaves at its locked commit.
func unchangedView(s *gittest.Submodule) changeView {
	return fixtureView(s, *lockedTarget(s), false)
}

// wantChanges compares the changes of an update with the expected views.
func wantChanges(t *testing.T, got []core.Change, want []changeView) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("%d changes, want %d", len(got), len(want))
	}
	for i := range min(len(got), len(want)) {
		if v := viewOf(got[i]); v != want[i] {
			t.Errorf("change %d:\n%+v\nwant\n%+v", i, v, want[i])
		}
	}
}

// statusNames lists the submodule names of statuses.
func statusNames(st []core.Status) []string {
	names := make([]string, 0, len(st))
	for _, s := range st {
		names = append(names, s.Submodule.Name)
	}
	return names
}

// fixtureNames lists the names of fixture submodules.
func fixtureNames(subs []*gittest.Submodule) []string {
	names := make([]string, 0, len(subs))
	for _, s := range subs {
		names = append(names, s.Name)
	}
	return names
}

// wantLockFile checks the entries of the lock file below root, in file
// order.
func wantLockFile(t *testing.T, g *git.Runner, root string, want []lock.Entry) {
	t.Helper()
	lk, err := lock.Load(t.Context(), g, root)
	if err != nil {
		t.Fatal(err)
	}
	if got := lk.Entries(); !slices.Equal(got, want) {
		t.Errorf("lock entries:\n%+v\nwant\n%+v", got, want)
	}
}

// fixtureLock returns the lock entries a fixture wrote, in file order, with
// the entries in replace substituted by name.
func fixtureLock(subs []*gittest.Submodule, replace ...lock.Entry) []lock.Entry {
	var entries []lock.Entry
	for _, s := range subs {
		e := lockOfFixture(s)
		if e == nil {
			continue
		}
		i := slices.IndexFunc(replace, func(r lock.Entry) bool { return r.Name == e.Name })
		if i >= 0 {
			e = &replace[i]
		}
		entries = append(entries, *e)
	}
	return entries
}

// tracedRunner returns a runner with the configuration entries that writes
// the trace of every git command, including the commands git starts for a
// transport, to the returned file.
func tracedRunner(t *testing.T, config ...string) (*git.Runner, string) {
	t.Helper()
	trace := filepath.Join(t.TempDir(), "trace")
	env := append(gittest.Env(t, config...), "GIT_TRACE="+trace)
	g, err := git.New(git.WithEnv(env...))
	if err != nil {
		t.Fatal(err)
	}
	return g, trace
}

// readTrace returns the trace written since offset, and the offset after
// it.
func readTrace(t *testing.T, trace string, offset int) (string, int) {
	t.Helper()
	data, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	return string(data[offset:]), len(data)
}

// transports returns the local repositories that the transfers in a trace
// were served from. It fails the test when git started a remote helper,
// which every http or https transfer needs, or served a transfer from
// another repository than bares.
func transports(t *testing.T, data string, bares ...string) map[string]bool {
	t.Helper()
	served := make(map[string]bool)
	for line := range strings.Lines(data) {
		switch {
		case strings.Contains(line, "remote-"):
			t.Errorf("git started a remote helper: %s", line)
		case !strings.Contains(line, "run_command") || !strings.Contains(line, "upload-pack"):
		default:
			i := slices.IndexFunc(bares, func(bare string) bool {
				return strings.Contains(line, "'"+bare+"'")
			})
			if i < 0 {
				t.Errorf("upload-pack for another repository: %s", line)
				continue
			}
			served[bares[i]] = true
		}
	}
	return served
}

// wantLocalTransport checks that the trace written since offset shows a
// transfer from each of bares, and only from them.
func wantLocalTransport(t *testing.T, trace string, offset int, bares ...string) int {
	t.Helper()
	data, end := readTrace(t, trace, offset)
	served := transports(t, data, bares...)
	for _, bare := range bares {
		if !served[bare] {
			t.Errorf("no upload-pack of %s in the trace:\n%s", bare, data)
		}
	}
	return end
}

// wantNoTransport checks that the trace written since offset shows no
// transfer at all.
func wantNoTransport(t *testing.T, trace string, offset int) int {
	t.Helper()
	data, end := readTrace(t, trace, offset)
	if served := transports(t, data); len(served) > 0 || strings.Contains(data, "upload-pack") {
		t.Errorf("transfers in the trace:\n%s", data)
	}
	return end
}

// wantTransportRefused checks that err is a failed git invocation whose
// transport the test environment refused, which happens before any
// connection is attempted.
func wantTransportRefused(t *testing.T, what string, err error) {
	t.Helper()
	gitErr, ok := errors.AsType[*git.Error](err)
	if !ok || !strings.Contains(gitErr.Stderr, "transport 'https' not allowed") {
		t.Errorf("%s: %v; want a refused https transport", what, err)
	}
}

// processSampler records how many processes run with a working directory
// below a directory at the same time, by sampling /proc.
type processSampler struct {
	dir     string
	stop    chan struct{}
	done    chan struct{}
	most    int
	samples int
}

// startSampler starts sampling the processes working below dir.
func startSampler(t *testing.T, dir string) *processSampler {
	t.Helper()
	p := &processSampler{dir: realDir(t, dir), stop: make(chan struct{}),
		done: make(chan struct{})}
	go p.run()
	return p
}

// run samples until stopped.
func (p *processSampler) run() {
	defer close(p.done)
	tick := time.NewTicker(200 * time.Microsecond)
	defer tick.Stop()
	for {
		select {
		case <-p.stop:
			return
		case <-tick.C:
		}
		p.most = max(p.most, p.count())
		p.samples++
	}
}

// count returns the number of processes working below the directory. A
// process that ends while it is inspected is not counted.
func (p *processSampler) count() int {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if _, err := strconv.Atoi(e.Name()); err != nil {
			continue
		}
		cwd, err := os.Readlink(filepath.Join("/proc", e.Name(), "cwd"))
		if err == nil && (cwd == p.dir || strings.HasPrefix(cwd, p.dir+"/")) {
			n++
		}
	}
	return n
}

// finish stops sampling.
func (p *processSampler) finish() (most, samples int) {
	close(p.stop)
	<-p.done
	return p.most, p.samples
}
