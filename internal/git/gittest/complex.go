// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package gittest

import (
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/FPGArtktic/lazysubmodules/internal/git"
)

// URLs of the complex superproject that its Config rewrites.
const (
	// MirrorURL is the URL of the submodule "mirror-lib".
	MirrorURL = FakeHost + "mirror-lib.git"
	// InnerURL is the URL of the nested submodule "inner" of "tools".
	InnerURL = FakeHost + "tools-inner.git"
)

// Paths of the complex superproject, relative to its top level.
const (
	// SubdirPath is a directory of the superproject outside every submodule.
	SubdirPath = "src/drivers"
	// submoduleSubdir is a directory below the top level of "crypto lib".
	submoduleSubdir = "src"
)

// ComplexSuper is the superproject built by NewComplexSuper and
// NewLinkedWorktreeSuper. The submodule fields describe the fourteen
// submodules in .gitmodules order.
type ComplexSuper struct {
	*Super
	// Format is the object format.
	Format string
	// Main is the top level of the main worktree; it differs from Dir only
	// for NewLinkedWorktreeSuper.
	Main string
	// GitDir is the absolute git directory of the worktree Dir; compare it
	// with git.SamePath.
	GitDir string
	// History lists the commits of the superproject, oldest first.
	History []string
	// Subdir is the directory SubdirPath; SubmoduleSubdir is a directory
	// below the top level of CryptoLib.
	Subdir, SubmoduleSubdir string
	// Config holds the config entries that every runner used with the
	// fixture needs (see Runner): url.<base>.insteadOf entries for MirrorURL
	// and InnerURL, and SSHSigningConfig for SigningKey when there is one.
	Config []string
	// SigningKey is the SSH key that signed the tag of Signed, empty when
	// ssh-keygen is not installed.
	SigningKey string

	// Kernel ("kernel"): tag-pattern "v6.6.*". Tags v6.6.1 to v6.6.10 (odd
	// ones annotated), the pre-releases v6.6.11-rc1 (annotated) and
	// v6.7-rc1 (the tip of main), branch linux-6.6.y at v6.6.11-rc1. Locked
	// at v6.6.9; the previous superproject commit locked v6.6.8.
	Kernel *Submodule
	// UBoot ("u-boot" at "bootloader/u-boot"): branch "main" with the
	// native branch key. Locked at main~1; main (and branch next) advanced
	// after locking, and the submodule has already fetched it.
	UBoot *Submodule
	// FPGACore ("fpga.core" at "ip/fpga-core"): tag "v2.3.1", annotated.
	// After the submodule was cloned, the tag was moved on the remote to
	// the tip of main; the submodule still has the old tag, which the lock
	// records. Tag v2.3.0 is lightweight.
	FPGACore *Submodule
	// CryptoLib ("crypto lib" at "libs/crypto lib"): commit mode with the
	// full SHA of main~1, which contains the directory "src".
	CryptoLib *Submodule
	// Legacy ("legacy" at "vendor/legacy"): no tracking keys, no lock
	// entry, checked out at the tip of main.
	Legacy *Submodule
	// Tools ("tools" at "tools/nested"): branch "develop" with the native
	// branch key, locked at develop, which is main~1. It has the nested
	// submodule ToolsInner.
	Tools *Submodule
	// Theme ("theme" at "docs/theme"): tag "v1.0.0" (lightweight), main~1.
	// Deinitialized; its repository stays in GitDir.
	Theme *Submodule
	// Fresh ("fresh" at "third_party/fresh"): tag-pattern "v1.*" with tags
	// v1.0.0, v1.1.0 (annotated) and v2.0.0. Locked at v1.1.0 but never
	// cloned: the directory is empty and there is no repository.
	Fresh *Submodule
	// App ("app" at "apps/app"): tag "v1.2.0" (annotated), between v1.1.0
	// and v1.3.0. TrackedFile is modified in its working tree.
	App *Submodule
	// SDK ("sdk"): tag-pattern "v*" with v2.9.0 (annotated), v3.0.0-rc.1
	// and v3.0.0-rc.2 (annotated, the tip of main), locked at v3.0.0-rc.2.
	SDK *Submodule
	// MirrorLib ("mirror-lib" at "libs/mirror"): branch "stable" (main~1)
	// with the native branch key. Its URL is MirrorURL, also as the origin
	// of the submodule; Config rewrites it.
	MirrorLib *Submodule
	// Broken ("broken" at "libs/broken"): tag "v9.9.9", which does not
	// exist. Locked at v1.0.0, the only tag.
	Broken *Submodule
	// Signed ("signed" at "libs/signed"): tag "v1.0.0", signed with
	// SigningKey. Tag v1.0.1 (the tip of main) is annotated and unsigned.
	Signed *Submodule
	// Quirky ("quirky" at "libs/quirky"): tag "v1.0.0" (the tip of main),
	// a file:// URL and update = none, ignore = all, shallow = true. Cloned
	// shallow (depth 1) by "git submodule update --init --checkout".
	Quirky *Submodule
	// ToolsInner ("inner" in Tools): unmanaged, URL InnerURL. Tools records
	// main~1 of its upstream, the checkout is at main, which "git status" in
	// Tools reports as a new commit only.
	ToolsInner *Submodule
}

// NewComplexSuper creates a superproject with fourteen submodules covering
// every tracking mode and the states that matter for them; the fields of
// ComplexSuper list them.
//
// The superproject has five commits: an initial one with README, SubdirPath
// and a .gitignore that ignores "*.lock"; two that add the submodules; one
// that adds the tracking keys and the lock file, which locks the kernel at
// v6.6.8; and "manifest: update kernel to v6.6.9", which updates its gitlink
// and lock entry. Every gitlink equals the commit of the lock entry (Legacy
// and ToolsInner have none). Submodule working trees are detached at their
// gitlinks, except where the fields say otherwise.
//
// Upstream commits are the same in every run for a given object format;
// superproject commits are not, since .gitmodules records temporary paths.
// Each call builds everything anew with a few hundred git commands, run in
// parallel where possible; that takes a few seconds (3.5 s on a busy
// four-core host), so a test should build it once and use subtests. Every
// git command run against the fixture needs Config.
//
// Context: test helper; format is SHA1 or SHA256. Without ssh-keygen, the
// tag of Signed is not signed; this is logged.
// Return: the superproject.
func NewComplexSuper(t testing.TB, format string) *ComplexSuper {
	t.Helper()
	return buildComplex(t, format).c
}

// NewLinkedWorktreeSuper creates a NewComplexSuper and a linked worktree of
// it on the new branch "linked", and returns the fixture for the linked
// worktree.
//
// In the linked worktree, ".git" is a file, and the submodule repositories
// live in its own git directory (GitDir, below the common git directory in
// Main), as git keeps them per worktree. The submodules are brought into
// the same states as in the main worktree, which stays as NewComplexSuper
// leaves it; FPGACore is cloned with the moved tag and then gets the old tag
// back from the main worktree. It takes about one and a half times as long
// as NewComplexSuper.
//
// Context: test helper; format is SHA1 or SHA256.
// Return: the superproject as seen from the linked worktree.
func NewLinkedWorktreeSuper(t testing.TB, format string) *ComplexSuper {
	t.Helper()
	b := buildComplex(t, format)
	main := b.c
	wt := filepath.Join(t.TempDir(), "linked")
	b.git(main.Dir, "worktree", "add", "--quiet", "-b", "linked", wt)
	c := main.rebase(wt)
	b.populate(c)
	b.git(c.FPGACore.Dir(), "fetch", "--quiet", "--no-tags", main.FPGACore.GitDir,
		"+refs/tags/"+c.FPGACore.Ref+":refs/tags/"+c.FPGACore.Ref)
	return c
}

// Invocation is a directory that a command can be started from.
type Invocation struct {
	// Name describes the directory.
	Name string
	// Dir is the start directory.
	Dir string
	// Root is the top level of the working tree that git finds from Dir;
	// compare it with git.SamePath.
	Root string
	// Names lists the submodules in the .gitmodules file of Root in file
	// order; it is empty when Root has none.
	Names []string
}

// NewSubdirInvocation creates a NewComplexSuper and lists the directories a
// command can be started from (see ComplexSuper.Invocations).
//
// Context: test helper; format is SHA1 or SHA256.
// Return: the superproject and the start directories.
func NewSubdirInvocation(t testing.TB, format string) (*ComplexSuper, []Invocation) {
	t.Helper()
	c := NewComplexSuper(t, format)
	return c, c.Invocations()
}

// Invocations lists directories a command can be started from: the top
// level, a subdirectory and the empty directories of two submodules that
// are not checked out, where git finds the superproject; and the top level
// and a subdirectory of a checked-out submodule, a checked-out submodule
// with a nested submodule and that nested submodule, where git finds the
// submodule itself.
//
// Context: any.
// Return: the directories in the order described.
func (c *ComplexSuper) Invocations() []Invocation {
	var names []string
	for _, s := range c.Submodules() {
		names = append(names, s.Name)
	}
	crypto, tools, inner := c.CryptoLib.Dir(), c.Tools.Dir(), c.ToolsInner.Dir()
	return []Invocation{
		{Name: "top level", Dir: c.Dir, Root: c.Dir, Names: names},
		{Name: "subdirectory", Dir: c.Subdir, Root: c.Dir, Names: names},
		{Name: "submodule never cloned", Dir: c.Fresh.Dir(), Root: c.Dir, Names: names},
		{Name: "deinitialized submodule", Dir: c.Theme.Dir(), Root: c.Dir, Names: names},
		{Name: "submodule", Dir: crypto, Root: crypto},
		{Name: "submodule subdirectory", Dir: c.SubmoduleSubdir, Root: crypto},
		{Name: "submodule with a nested submodule", Dir: tools, Root: tools,
			Names: []string{c.ToolsInner.Name}},
		{Name: "nested submodule", Dir: inner, Root: inner},
	}
}

// Submodules returns the submodules of the superproject in .gitmodules
// order; ToolsInner is not one of them.
//
// Context: any.
// Return: the fourteen submodule fields.
func (c *ComplexSuper) Submodules() []*Submodule {
	fields := c.fields()
	subs := make([]*Submodule, len(fields))
	for i, f := range fields {
		subs[i] = *f
	}
	return subs
}

// fields returns the addresses of the submodule fields in .gitmodules order.
func (c *ComplexSuper) fields() []**Submodule {
	return []**Submodule{
		&c.Kernel, &c.UBoot, &c.FPGACore, &c.CryptoLib, &c.Legacy, &c.Tools, &c.Theme,
		&c.Fresh, &c.App, &c.SDK, &c.MirrorLib, &c.Broken, &c.Signed, &c.Quirky,
	}
}

// Runner returns a runner with Env and Config.
//
// Context: test helper; fails the test when git is missing.
// Return: the runner.
func (c *ComplexSuper) Runner(t testing.TB) *git.Runner {
	t.Helper()
	return Runner(t, c.Config...)
}

// Git runs git in dir like the package function Git, with Config.
//
// Context: test helper; fails the test when git fails.
// Return: the standard output without surrounding white space.
func (c *ComplexSuper) Git(t testing.TB, dir string, args ...string) string {
	t.Helper()
	return mustRun(t, c.Runner(t), dir, args...)
}

// rebase returns a copy of c for the worktree at dir, with copies of the
// submodule descriptions.
func (c *ComplexSuper) rebase(dir string) *ComplexSuper {
	cp := *c
	cp.Super = &Super{Dir: dir}
	for _, f := range cp.fields() {
		s := **f
		*f = &s
	}
	inner := *c.ToolsInner
	cp.ToolsInner = &inner
	return &cp
}

// complexBuilder builds a ComplexSuper.
type complexBuilder struct {
	*builder
	c *ComplexSuper
	// kernelOld is the commit that the kernel was locked at before the last
	// superproject commit.
	kernelOld string
	// fpgaMoved is the commit that the remote tag of FPGACore moves to.
	fpgaMoved string
	// innerHead is the commit checked out in ToolsInner.
	innerHead string
}

// buildComplex builds the fixture of NewComplexSuper.
func buildComplex(t testing.TB, format string) *complexBuilder {
	t.Helper()
	key, ok := SSHSigningKey(t)
	if !ok {
		t.Log(`gittest: ssh-keygen is not installed; tag v1.0.0 of "signed" is not signed`)
	}
	var mirror, inner *Upstream
	parallel(t,
		func(t testing.TB) { mirror = NewUpstream(t, format) },
		func(t testing.TB) { inner = NewUpstream(t, format) })
	c := &ComplexSuper{
		Super:      &Super{Dir: InitRepo(t, format)},
		Format:     format,
		SigningKey: key,
		Config: []string{
			"url." + mirror.Bare + ".insteadOf=" + MirrorURL,
			"url." + inner.Bare + ".insteadOf=" + InnerURL,
		},
	}
	if ok {
		c.Config = append(c.Config, SSHSigningConfig(key)...)
	}
	c.Main = c.Dir
	b := &complexBuilder{builder: newBuilder(t, format, c.Config...), c: c}
	b.key = key
	b.submodules(mirror, inner)
	b.superproject()
	b.populate(c)
	c.FPGACore.Upstream.MoveTag(t, c.FPGACore.Ref, b.fpgaMoved)
	refs := map[*Upstream][2]map[string]string{}
	for _, s := range append(c.Submodules(), c.ToolsInner) {
		r, ok := refs[s.Upstream]
		if !ok {
			r[0], r[1] = b.remoteRefs(s.Upstream)
			refs[s.Upstream] = r
		}
		s.Tags, s.Branches = r[0], r[1]
	}
	return b
}

// submodules creates the upstreams, in parallel, and describes the
// submodules.
func (b *complexBuilder) submodules(mirror, inner *Upstream) {
	b.t.Helper()
	c := b.c
	parallel(b.t,
		func(t testing.TB) { c.Kernel, b.kernelOld = b.with(t).kernel() },
		func(t testing.TB) { c.UBoot = b.with(t).uBoot() },
		func(t testing.TB) { c.FPGACore, b.fpgaMoved = b.with(t).fpgaCore() },
		func(t testing.TB) { c.CryptoLib = b.with(t).cryptoLib() },
		func(t testing.TB) { c.Legacy = b.with(t).legacy() },
		func(t testing.TB) { c.Tools, c.ToolsInner, b.innerHead = b.with(t).tools(inner) },
		func(t testing.TB) {
			c.Theme = b.with(t).simple("theme", "docs/theme", ModeTag, "v1.0.0", "v1.0.0",
				release{msg: "theme v1.0.0", tag: "v1.0.0"}, release{msg: "theme: dark mode"})
			c.Theme.state = deinitialized
		},
		func(t testing.TB) {
			c.Fresh = b.with(t).simple("fresh", "third_party/fresh", ModeTagPattern, "v1.*",
				"v1.1.0", release{msg: "fresh v1.0.0", tag: "v1.0.0"},
				release{msg: "fresh v1.1.0", tag: "v1.1.0", kind: annotated},
				release{msg: "fresh v2.0.0", tag: "v2.0.0"})
			c.Fresh.state = absent
		},
		func(t testing.TB) {
			c.App = b.with(t).simple("app", "apps/app", ModeTag, "v1.2.0", "v1.2.0",
				release{msg: "app v1.1.0", tag: "v1.1.0"},
				release{msg: "app v1.2.0", tag: "v1.2.0", kind: annotated},
				release{msg: "app v1.3.0", tag: "v1.3.0"})
			c.App.state = dirty
		},
		func(t testing.TB) {
			c.SDK = b.with(t).simple("sdk", "sdk", ModeTagPattern, "v*", "v3.0.0-rc.2",
				release{msg: "sdk v2.9.0", tag: "v2.9.0", kind: annotated},
				release{msg: "sdk v3.0.0-rc.1", tag: "v3.0.0-rc.1"},
				release{msg: "sdk v3.0.0-rc.2", tag: "v3.0.0-rc.2", kind: annotated})
		},
		func(t testing.TB) { c.MirrorLib = b.with(t).mirrorLib(mirror) },
		func(t testing.TB) {
			c.Broken = b.with(t).simple("broken", "libs/broken", ModeTag, "v9.9.9", "v1.0.0",
				release{msg: "broken v1.0.0", tag: "v1.0.0"})
		},
		func(t testing.TB) {
			c.Signed = b.with(t).simple("signed", "libs/signed", ModeTag, "v1.0.0", "v1.0.0",
				release{msg: "signed v1.0.0", tag: "v1.0.0", kind: signed},
				release{msg: "signed v1.0.1", tag: "v1.0.1", kind: annotated})
		},
		func(t testing.TB) { c.Quirky = b.with(t).quirky() },
	)
}

// tracked describes a managed submodule whose gitlink is the locked commit.
func tracked(name, p string, u *Upstream, mode, ref, lockRef, commit string) *Submodule {
	return &Submodule{
		Name: name, Path: p, URL: u.Bare, Mode: mode, Ref: ref, Upstream: u,
		Lock:    &LockEntry{Mode: mode, Ref: lockRef, Commit: commit},
		Gitlink: commit,
	}
}

// simple creates an upstream with the releases and describes a submodule
// locked at the release tagged lockRef.
func (b *builder) simple(name, p, mode, ref, lockRef string, releases ...release) *Submodule {
	b.t.Helper()
	u := NewUpstream(b.t, b.format)
	commits := b.record(u, releases...)
	b.publish(u)
	i := slices.IndexFunc(releases, func(rel release) bool { return rel.tag == lockRef })
	if i < 0 {
		b.t.Fatalf("gittest: %s: no release %s", name, lockRef)
	}
	return tracked(name, p, u, mode, ref, lockRef, commits[i])
}

// kernel describes ComplexSuper.Kernel and returns the commit of v6.6.8.
func (b *builder) kernel() (*Submodule, string) {
	b.t.Helper()
	u := NewUpstream(b.t, b.format)
	releases := make([]release, 0, 12)
	for i := 1; i <= 10; i++ {
		tag := "v6.6." + strconv.Itoa(i)
		kind := lightweight
		if i%2 == 1 {
			kind = annotated
		}
		releases = append(releases, release{msg: "Linux " + tag, tag: tag, kind: kind})
	}
	releases = append(releases,
		release{msg: "Linux v6.6.11-rc1", tag: "v6.6.11-rc1", kind: annotated},
		release{msg: "Linux v6.7-rc1", tag: "v6.7-rc1"})
	commits := b.record(u, releases...)
	b.git(u.Work, "branch", "linux-6.6.y", commits[10])
	b.publish(u)
	s := tracked("kernel", "kernel", u, ModeTagPattern, "v6.6.*", "v6.6.9", commits[8])
	return s, commits[7]
}

// uBoot describes ComplexSuper.UBoot.
func (b *builder) uBoot() *Submodule {
	b.t.Helper()
	u := NewUpstream(b.t, b.format)
	commits := b.record(u, release{msg: "U-Boot v2025.10"}, release{msg: "U-Boot v2026.01"},
		release{msg: "board: add a new board"})
	b.git(u.Work, "branch", "next", commits[2])
	b.publish(u)
	s := tracked("u-boot", "bootloader/u-boot", u, ModeBranch, "main", "main", commits[1])
	s.Branch = "main"
	return s
}

// fpgaCore describes ComplexSuper.FPGACore and returns the commit its tag
// moves to after the clone.
func (b *builder) fpgaCore() (*Submodule, string) {
	b.t.Helper()
	u := NewUpstream(b.t, b.format)
	commits := b.record(u, release{msg: "fpga-core v2.3.0", tag: "v2.3.0"},
		release{msg: "fpga-core v2.3.1", tag: "v2.3.1", kind: annotated},
		release{msg: "timing: fix setup violation"})
	b.publish(u)
	s := tracked("fpga.core", "ip/fpga-core", u, ModeTag, "v2.3.1", "v2.3.1", commits[1])
	return s, commits[2]
}

// cryptoLib describes ComplexSuper.CryptoLib.
func (b *builder) cryptoLib() *Submodule {
	b.t.Helper()
	u := NewUpstream(b.t, b.format)
	WriteFile(b.t, filepath.Join(u.Work, submoduleSubdir, "crypto.c"), "int crypto_init(void);\n")
	b.git(u.Work, "add", "--all")
	commits := b.record(u, release{msg: "crypto: add sources"}, release{msg: "crypto: speed up"})
	b.publish(u)
	pin := commits[0]
	return tracked("crypto lib", "libs/crypto lib", u, ModeCommit, pin, pin, pin)
}

// legacy describes ComplexSuper.Legacy.
func (b *builder) legacy() *Submodule {
	b.t.Helper()
	u := NewUpstream(b.t, b.format)
	commits := b.record(u, release{msg: "legacy 1.0"})
	b.publish(u)
	return &Submodule{Name: "legacy", Path: "vendor/legacy", URL: u.Bare, Gitlink: commits[0],
		Upstream: u}
}

// tools describes ComplexSuper.Tools and ComplexSuper.ToolsInner, and
// returns the commit checked out in the latter.
func (b *builder) tools(inner *Upstream) (*Submodule, *Submodule, string) {
	b.t.Helper()
	nested := b.record(inner, release{msg: "inner 1"}, release{msg: "inner 2"})
	b.publish(inner)
	u := NewUpstream(b.t, b.format)
	b.git(u.Work, "submodule", "add", "--quiet", "--name", "inner", "--", InnerURL, "inner")
	b.git(filepath.Join(u.Work, "inner"), "checkout", "--quiet", "--detach", nested[0])
	commits := b.record(u, release{msg: "tools: add inner"}, release{msg: "tools: next"})
	b.git(u.Work, "branch", "develop", commits[0])
	b.publish(u)
	s := tracked("tools", "tools/nested", u, ModeBranch, "develop", "develop", commits[0])
	s.Branch = "develop"
	in := &Submodule{Name: "inner", Path: "inner", URL: InnerURL, Gitlink: nested[0],
		Upstream: inner}
	return s, in, nested[1]
}

// mirrorLib describes ComplexSuper.MirrorLib.
func (b *builder) mirrorLib(u *Upstream) *Submodule {
	b.t.Helper()
	commits := b.record(u, release{msg: "mirror-lib 1.0"}, release{msg: "mirror-lib 1.1"})
	b.git(u.Work, "branch", "stable", commits[0])
	b.publish(u)
	s := tracked("mirror-lib", "libs/mirror", u, ModeBranch, "stable", "stable", commits[0])
	s.URL, s.Branch = MirrorURL, "stable"
	return s
}

// quirky describes ComplexSuper.Quirky. A shallow clone has only the tip of
// the default branch, so the gitlink must be that tip.
func (b *builder) quirky() *Submodule {
	b.t.Helper()
	s := b.simple("quirky", "libs/quirky", ModeTag, "v1.0.0", "v1.0.0",
		release{msg: "quirky v1.0.0", tag: "v1.0.0"})
	s.URL = "file://" + filepath.ToSlash(s.Upstream.Bare)
	s.Extra = []git.ConfigEntry{
		{Key: "update", Value: "none"},
		{Key: "ignore", Value: "all"},
		{Key: "shallow", Value: "true"},
	}
	return s
}

// superproject creates the history of the superproject.
func (b *complexBuilder) superproject() {
	b.t.Helper()
	c := b.c
	dir := c.Dir
	WriteFile(b.t, filepath.Join(dir, "README"), "superproject\n")
	WriteFile(b.t, filepath.Join(dir, SubdirPath, "README"), "drivers\n")
	WriteFile(b.t, filepath.Join(dir, ".gitignore"), "# build products\n*.lock\n")
	b.git(dir, "add", "--all")
	b.commit(dir, "initial commit")

	subs := c.Submodules()
	old := *c.Kernel
	old.Gitlink = b.kernelOld
	old.Lock = &LockEntry{Mode: ModeTagPattern, Ref: "v6.6.8", Commit: b.kernelOld}
	before := slices.Clone(subs)
	before[0] = &old
	b.writeGitmodules(dir, subs[:6], false)
	b.setGitlinks(dir, before[:6])
	b.git(dir, "add", GitmodulesFile)
	b.commit(dir, "add kernel, u-boot, fpga.core, crypto lib, legacy and tools")
	b.writeGitmodules(dir, subs, false)
	b.setGitlinks(dir, subs[6:])
	b.git(dir, "add", GitmodulesFile)
	b.commit(dir, "add theme, fresh, app, sdk, mirror-lib, broken, signed and quirky")
	b.writeGitmodules(dir, subs, true)
	b.writeLock(dir, before)
	b.git(dir, "add", "--force", GitmodulesFile, LockFile)
	b.commit(dir, "track submodules with lazysubmodules")

	b.writeLock(dir, subs)
	b.setGitlinks(dir, subs[:1])
	b.git(dir, "add", "--force", LockFile)
	b.git(dir, "commit", "--quiet", "--signoff", "--message="+
		"manifest: update kernel to v6.6.9\n\n"+
		"Tracking mode: tag-pattern v6.6.*\n"+
		"Old: "+b.kernelOld[:12]+" (v6.6.8)\n"+
		"New: "+c.Kernel.Gitlink[:12]+" (v6.6.9)\n")
	c.History = strings.Fields(b.git(dir, "rev-list", "--reverse", "HEAD"))
}

// populate brings the submodules of the worktree c.Dir into their states.
func (b *complexBuilder) populate(c *ComplexSuper) {
	b.t.Helper()
	subs := c.Submodules()
	b.clone(c.Dir, subs)
	tools := c.Tools.In(c.Dir)
	b.git(tools, "submodule", "update", "--init", "--quiet")
	b.git(c.ToolsInner.In(tools), "checkout", "--quiet", "--detach", b.innerHead)
	b.git(c.Dir, "submodule", "deinit", "--force", "--quiet", "--", c.Theme.Path)
	makeDirty(b.t, c.App.In(c.Dir))

	c.GitDir = b.git(c.Dir, "rev-parse", "--absolute-git-dir")
	c.Subdir = filepath.Join(c.Dir, filepath.FromSlash(SubdirPath))
	finish(c.Dir, c.GitDir, subs)
	c.SubmoduleSubdir = filepath.Join(c.CryptoLib.Dir(), submoduleSubdir)
	inner := c.ToolsInner
	inner.root = tools
	inner.GitDir = filepath.Join(c.Tools.GitDir, "modules", inner.Name)
	inner.Head = b.innerHead
}
