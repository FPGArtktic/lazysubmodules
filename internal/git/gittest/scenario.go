// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package gittest

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/FPGArtktic/lazysubmodules/internal/git"
)

// Tracking modes and file names as the scenario fixtures write them. The
// fixtures do not use the manifest and lock packages, whose tests import
// this package.
const (
	ModeBranch     = "branch"
	ModeTag        = "tag"
	ModeTagPattern = "tag-pattern"
	ModeCommit     = "commit"

	// GitmodulesFile and LockFile are relative to the superproject top
	// level.
	GitmodulesFile = ".gitmodules"
	LockFile       = ".lsm.lock"
)

// FakeHost is the base URL of repositories that do not exist. Scenario
// fixtures record URLs below it and pass config entries that rewrite them to
// local repositories with url.<base>.insteadOf. Only the file transport is
// allowed by Env, so a URL that is not rewritten fails before any host is
// contacted.
const FakeHost = "https://git.example.invalid/"

// LockEntry is the lock file section of a submodule.
type LockEntry struct {
	// Mode is the tracking mode, such as ModeTag.
	Mode string
	// Ref is the resolved ref: a tag or branch name, or the commit in commit
	// mode.
	Ref string
	// Commit is the full commit SHA.
	Commit string
}

// Submodule describes a submodule of a scenario fixture as it was built. The
// values are recorded when the fixture is created; they do not follow later
// changes.
type Submodule struct {
	// Name is the subsection name in .gitmodules.
	Name string
	// Path is the "path" value: "/"-separated and normally relative to the
	// top level of the superproject.
	Path string
	// URL is the "url" value.
	URL string
	// Branch is the native "branch" value, empty when unset.
	Branch string
	// Mode and Ref are the "lsm-mode" and "lsm-ref" values; Mode is empty
	// for an unmanaged submodule.
	Mode, Ref string
	// Extra lists the other variables of the section in file order; Key is
	// the variable name only.
	Extra []git.ConfigEntry
	// Lock is the committed lock file section, nil when there is none.
	Lock *LockEntry
	// Gitlink is the commit recorded for Path in the HEAD commit of the
	// superproject (in the index when HEAD is unborn), empty when none is
	// recorded.
	Gitlink string
	// Head is the commit checked out in the working tree of the submodule,
	// empty when it is not checked out.
	Head string
	// GitDir is the repository of the submodule inside the git directory of
	// the superproject, empty when it does not exist.
	GitDir string
	// Upstream is the repository that URL names, directly or through the
	// config entries of the fixture.
	Upstream *Upstream
	// Tags maps the tags of Upstream.Bare to their commits (annotated tags
	// peeled) and Branches its branches to their commits, both as of the
	// creation of the fixture. Fixtures whose submodules share one upstream
	// share these maps; they must not be modified.
	Tags, Branches map[string]string

	// root is the top level of the superproject that contains Path.
	root string
	// state is the working tree state the fixture builds.
	state subState
}

// Dir returns the working tree directory of the submodule.
//
// Context: any.
// Return: Path joined to the top level of the superproject, or Path itself
// when it is absolute.
func (s *Submodule) Dir() string {
	return s.In(s.root)
}

// In returns the directory of the submodule below another top level, such
// as a linked worktree of the same superproject.
//
// Context: any.
// Return: Path joined to root, or Path itself when it is absolute.
func (s *Submodule) In(root string) string {
	p := filepath.FromSlash(s.Path)
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(root, p)
}

// subState is the working tree state of a scenario submodule.
type subState int

const (
	// populated: cloned and checked out at the gitlink.
	populated subState = iota
	// dirty: populated, with a modified tracked file.
	dirty
	// deinitialized: populated, then deinitialized; the repository stays.
	deinitialized
	// absent: never cloned; the directory exists and is empty.
	absent
)

// tagKind selects how a scenario tag is created.
type tagKind int

const (
	lightweight tagKind = iota
	annotated
	// signed is an annotated tag signed with the SSH key of the builder,
	// or an unsigned annotated tag when there is no key.
	signed
)

// release is a commit of a scenario upstream, optionally tagged.
type release struct {
	msg  string
	tag  string
	kind tagKind
}

// builder runs the git commands that create a scenario fixture, all with one
// runner.
type builder struct {
	t      testing.TB
	format string
	g      *git.Runner
	// key is the SSH signing key, empty when ssh-keygen is missing.
	key string
}

// newBuilder returns a builder whose runner uses the config entries.
func newBuilder(t testing.TB, format string, config ...string) *builder {
	t.Helper()
	return &builder{t: t, format: format, g: Runner(t, config...)}
}

// git runs git in dir and returns its trimmed output; a failure fails the
// test.
func (b *builder) git(dir string, args ...string) string {
	b.t.Helper()
	return mustRun(b.t, b.g, dir, args...)
}

// with returns a copy of b that reports to t.
func (b *builder) with(t testing.TB) *builder {
	cp := *b
	cp.t = t
	return &cp
}

// record creates one commit per release on main in the work repository of
// u, as Upstream.Commit does, with its tag, and returns the commits. Changes
// to be committed besides TrackedFile must be staged. Nothing is pushed.
func (b *builder) record(u *Upstream, releases ...release) []string {
	b.t.Helper()
	for _, rel := range releases {
		u.writeNext(b.t, rel.msg)
		b.git(u.Work, "commit", "--quiet", "--all", "--message="+rel.msg)
		if rel.tag != "" {
			b.tag(u, rel.tag, rel.kind)
		}
	}
	commits := strings.Fields(b.git(u.Work, "rev-list", "--reverse",
		"--max-count="+strconv.Itoa(len(releases)), "HEAD"))
	if len(commits) != len(releases) {
		b.t.Fatalf("gittest: recorded %d commits, want %d", len(commits), len(releases))
	}
	return commits
}

// tag tags HEAD in the work repository of u.
func (b *builder) tag(u *Upstream, name string, kind tagKind) {
	b.t.Helper()
	args := []string{"tag", "--annotate", "--message=release " + name, name}
	switch {
	case kind == lightweight:
		args = []string{"tag", name}
	case kind == signed && b.key != "":
		args = append(ConfigArgs(SSHSigningConfig(b.key)...),
			"tag", "--sign", "--message=release "+name, name)
	}
	b.git(u.Work, args...)
}

// publish force-pushes every branch and tag of the work repository of u to
// its bare repository.
func (b *builder) publish(u *Upstream) {
	b.t.Helper()
	b.git(u.Work, "push", "--quiet", "--force", "origin",
		"refs/heads/*:refs/heads/*", "refs/tags/*:refs/tags/*")
}

// remoteRefs returns the tags (peeled) and branches of the bare repository
// of u.
func (b *builder) remoteRefs(u *Upstream) (tags, branches map[string]string) {
	b.t.Helper()
	tags, branches = map[string]string{}, map[string]string{}
	out := b.git(u.Bare, "for-each-ref",
		"--format=%(refname) %(objectname) %(*objectname)", "refs/heads", "refs/tags")
	for line := range strings.Lines(out) {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			b.t.Fatalf("gittest: unexpected for-each-ref line %q", line)
		}
		commit := fields[len(fields)-1]
		if name, ok := strings.CutPrefix(fields[0], "refs/tags/"); ok {
			tags[name] = commit
		} else {
			branches[strings.TrimPrefix(fields[0], "refs/heads/")] = commit
		}
	}
	return tags, branches
}

// initRepo creates an empty repository at dir, which may exist.
func (b *builder) initRepo(dir string) {
	b.t.Helper()
	b.git(filepath.Dir(dir), "init", "--quiet", "--object-format="+b.format, dir)
}

// commit commits the index of dir.
func (b *builder) commit(dir, msg string) {
	b.t.Helper()
	b.git(dir, "commit", "--quiet", "--message="+msg)
}

// writeGitmodules writes the .gitmodules file of dir for subs. Without
// tracking, only the path and url variables are written, as "git submodule
// add" does.
func (b *builder) writeGitmodules(dir string, subs []*Submodule, tracking bool) {
	b.t.Helper()
	sections := make([]configSection, 0, len(subs))
	for _, s := range subs {
		vars := []git.ConfigEntry{{Key: "path", Value: s.Path}, {Key: "url", Value: s.URL}}
		if tracking {
			vars = append(vars, trackingVars(s)...)
		}
		sections = append(sections, configSection{name: s.Name, vars: vars})
	}
	WriteFile(b.t, filepath.Join(dir, GitmodulesFile), configText("submodule", sections))
}

// trackingVars returns the variables of s that follow path and url.
func trackingVars(s *Submodule) []git.ConfigEntry {
	var vars []git.ConfigEntry
	if s.Branch != "" {
		vars = append(vars, git.ConfigEntry{Key: "branch", Value: s.Branch})
	}
	vars = append(vars, s.Extra...)
	if s.Mode != "" {
		vars = append(vars,
			git.ConfigEntry{Key: "lsm-mode", Value: s.Mode},
			git.ConfigEntry{Key: "lsm-ref", Value: s.Ref})
	}
	return vars
}

// writeLock writes the lock file of dir with the entries of subs.
func (b *builder) writeLock(dir string, subs []*Submodule) {
	b.t.Helper()
	WriteFile(b.t, filepath.Join(dir, LockFile), lockText(subs))
}

// lockText returns the lock file content with the entries of subs.
func lockText(subs []*Submodule) string {
	var sections []configSection
	for _, s := range subs {
		if s.Lock == nil {
			continue
		}
		sections = append(sections, configSection{name: s.Name, vars: []git.ConfigEntry{
			{Key: "mode", Value: s.Lock.Mode},
			{Key: "ref", Value: s.Lock.Ref},
			{Key: "commit", Value: s.Lock.Commit},
		}})
	}
	return configText("submodule", sections)
}

// setGitlinks records the gitlinks of subs in the index of dir and creates
// their directories, as a checkout of the superproject does. Submodules
// without a gitlink are left out.
func (b *builder) setGitlinks(dir string, subs []*Submodule) {
	b.t.Helper()
	args := []string{"update-index", "--add"}
	for _, s := range subs {
		if s.Gitlink != "" {
			args = append(args, "--cacheinfo", "160000,"+s.Gitlink+","+s.Path)
			mkdir(b.t, s.In(dir))
		}
	}
	b.git(dir, args...)
}

// clone clones and checks out the submodules of the superproject dir whose
// state requires it, ignoring any "update" setting. It creates the
// directories of all submodules with a gitlink.
func (b *builder) clone(dir string, subs []*Submodule) {
	b.t.Helper()
	args := []string{"submodule", "update", "--init", "--checkout", "--quiet", "--jobs=8", "--"}
	n := len(args)
	for _, s := range subs {
		if s.Gitlink == "" {
			continue
		}
		mkdir(b.t, s.In(dir))
		if s.state != absent {
			args = append(args, s.Path)
		}
	}
	if len(args) > n {
		b.git(dir, args...)
	}
}

// finish records where the submodules of the superproject dir, whose git
// directory is gitDir, live and what they have checked out.
func finish(dir, gitDir string, subs []*Submodule) {
	for _, s := range subs {
		s.root = dir
		s.Head, s.GitDir = "", ""
		if s.state != absent {
			s.GitDir = filepath.Join(gitDir, "modules", filepath.FromSlash(s.Name))
		}
		if s.state == populated || s.state == dirty {
			s.Head = s.Gitlink
		}
	}
}

// mkdir creates a directory with its parents.
func mkdir(t testing.TB, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("gittest: %v", err)
	}
}

// configSection is a section of a git configuration file.
type configSection struct {
	name string
	vars []git.ConfigEntry
}

// configText formats sections of type kind, such as "submodule", in the
// syntax git writes.
func configText(kind string, sections []configSection) string {
	var b strings.Builder
	subsection := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	for _, s := range sections {
		b.WriteString("[" + kind + ` "` + subsection.Replace(s.name) + "\"]\n")
		for _, v := range s.vars {
			b.WriteString("\t" + v.Key + " = " + quoteValue(v.Value) + "\n")
		}
	}
	return b.String()
}

// quoteValue quotes a configuration value when git would otherwise read it
// differently. A subsection name cannot hold a newline, a value can.
func quoteValue(s string) string {
	if s == strings.TrimSpace(s) && !strings.ContainsAny(s, "\"\\;#\n\t\b") {
		return s
	}
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\t", `\t`, "\b", `\b`)
	return `"` + r.Replace(s) + `"`
}

// parallel runs each function in a goroutine of its own and waits for all
// of them. The functions get a testing.TB whose Fatal methods end only
// their goroutine; the test fails when one of them failed. Skip methods
// must not be used.
func parallel(t testing.TB, fns ...func(t testing.TB)) {
	t.Helper()
	var (
		wg     sync.WaitGroup
		failed atomic.Bool
	)
	for _, fn := range fns {
		wg.Add(1)
		go func() {
			defer wg.Done()
			fn(goroutineTB{TB: t, failed: &failed})
		}()
	}
	wg.Wait()
	if failed.Load() {
		t.FailNow()
	}
}

// goroutineTB is a testing.TB for a goroutine other than the one running
// the test, which must not call FailNow of the test.
type goroutineTB struct {
	testing.TB
	failed *atomic.Bool
}

// Fatal logs args as an error and ends the goroutine.
//
// Context: the goroutine started by parallel.
// Return: never.
func (g goroutineTB) Fatal(args ...any) {
	g.Helper()
	g.Error(args...)
	g.FailNow()
}

// Fatalf logs a formatted error and ends the goroutine.
//
// Context: the goroutine started by parallel.
// Return: never.
func (g goroutineTB) Fatalf(format string, args ...any) {
	g.Helper()
	g.Errorf(format, args...)
	g.FailNow()
}

// FailNow marks the test as failed and ends the goroutine.
//
// Context: the goroutine started by parallel.
// Return: never.
func (g goroutineTB) FailNow() {
	g.failed.Store(true)
	g.Fail()
	runtime.Goexit()
}
