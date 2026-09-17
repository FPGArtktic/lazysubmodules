// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package gittest_test

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/FPGArtktic/lazysubmodules/internal/git"
	"github.com/FPGArtktic/lazysubmodules/internal/git/gittest"
)

// complexDigests pins the upstream commits of NewComplexSuper (see
// commitDigest). Scenario tests compare output that contains these commits,
// so a change here means their expected output changes as well.
func complexDigests() map[string]string {
	return map[string]string{
		gittest.SHA1:   "4d943a3dd1c1975f59c756ebfade92384c3de2e67f457b170f62d1d06ec46b5b",
		gittest.SHA256: "f252f32a74877324a61d25451548b17e17c5d198fe993f6e1922618eaf47c538",
	}
}

func TestComplexSuper(t *testing.T) {
	t.Parallel()
	for _, format := range formats() {
		t.Run(format, func(t *testing.T) {
			t.Parallel()
			c := gittest.NewComplexSuper(t, format)
			samePath(t, "main worktree", c.Main, c.Dir)
			samePath(t, "git dir", c.GitDir, filepath.Join(c.Dir, ".git"))
			if got := c.Git(t, c.Dir, "rev-parse", "HEAD"); got != c.History[4] {
				t.Errorf("HEAD %s, want %s", got, c.History[4])
			}
			checkComplex(t, c)
			t.Run("digest", func(t *testing.T) {
				if got, want := commitDigest(c), complexDigests()[format]; got != want {
					t.Errorf("upstream commits changed: digest %s, want %s\n%s", got, want,
						commitSummary(c))
				}
			})
			t.Run("native branch key", func(t *testing.T) {
				// Last: it moves the submodule.
				g := c.Runner(t)
				mustGit(t, g, c.Dir, "submodule", "update", "--remote", "--no-fetch", "--quiet",
					"--", c.UBoot.Path)
				if got := mustGit(t, g, c.UBoot.Dir(), "rev-parse", "HEAD"); got !=
					c.UBoot.Branches["main"] {
					t.Errorf("u-boot after update --remote at %s, want %s", got,
						c.UBoot.Branches["main"])
				}
			})
		})
	}
}

func TestLinkedWorktreeSuper(t *testing.T) {
	t.Parallel()
	for _, format := range formats() {
		t.Run(format, func(t *testing.T) {
			t.Parallel()
			c := gittest.NewLinkedWorktreeSuper(t, format)
			g := c.Runner(t)
			if fi, err := os.Lstat(filepath.Join(c.Dir, ".git")); err != nil || !fi.Mode().IsRegular() {
				t.Errorf(".git of the linked worktree: %v, %v; want a file", fi, err)
			}
			if got := mustGit(t, g, c.Dir, "symbolic-ref", "HEAD"); got != "refs/heads/linked" {
				t.Errorf("linked worktree on %s", got)
			}
			common := filepath.Join(c.Main, ".git")
			samePath(t, "common dir", mustGit(t, g, c.Dir, "rev-parse", "--path-format=absolute",
				"--git-common-dir"), common)
			samePath(t, "git dir", c.GitDir, filepath.Join(common, "worktrees", "linked"))
			checkComplex(t, c)

			// The main worktree keeps its own submodule repositories and states.
			for _, s := range c.Submodules() {
				if s.GitDir != "" && !strings.HasPrefix(s.GitDir, c.GitDir+"/") {
					t.Errorf("%s: repository %s outside %s", s.Name, s.GitDir, c.GitDir)
				}
				if s.Head == "" {
					continue
				}
				if got := mustGit(t, g, s.In(c.Main), "rev-parse", "HEAD"); got != s.Head {
					t.Errorf("%s: HEAD %s in the main worktree, want %s", s.Name, got, s.Head)
				}
			}
			if got := mustGit(t, g, c.FPGACore.In(c.Main), "rev-parse",
				c.FPGACore.Ref+"^{commit}"); got != c.FPGACore.Lock.Commit {
				t.Errorf("fpga.core tag in the main worktree at %s", got)
			}
		})
	}
}

func TestSubdirInvocation(t *testing.T) {
	t.Parallel()
	c, invocations := gittest.NewSubdirInvocation(t, gittest.SHA1)
	if !reflect.DeepEqual(invocations, c.Invocations()) || len(invocations) != 8 {
		t.Fatalf("invocations %+v", invocations)
	}
	checkInvocations(t, c, invocations)
}

// checkComplex verifies the state that NewComplexSuper promises.
func checkComplex(t *testing.T, c *gittest.ComplexSuper) {
	t.Helper()
	g := c.Runner(t)
	subs := c.Submodules()
	all := append(slices.Clone(subs), c.ToolsInner)
	t.Run("files", func(t *testing.T) {
		checkFiles(t, g, c.Dir, subs)
		checkGitlinks(t, g, c.Dir, "HEAD", subs)
		checkGitlinks(t, g, c.Dir, "", subs)
		checkGitlinks(t, g, c.Tools.Dir(), "HEAD", []*gittest.Submodule{c.ToolsInner})
		checkEntries(t, "tools .gitmodules", configEntries(t, g, c.Tools.Dir(), "-f",
			gittest.GitmodulesFile), wantGitmodules([]*gittest.Submodule{c.ToolsInner}))
	})
	t.Run("history", func(t *testing.T) { checkHistory(t, g, c) })
	t.Run("checkouts", func(t *testing.T) { checkCheckouts(t, g, c.GitDir, "", all) })
	t.Run("status", func(t *testing.T) { checkStatus(t, g, c) })
	t.Run("invocations", func(t *testing.T) { checkInvocations(t, c, c.Invocations()) })
	t.Run("kernel", func(t *testing.T) { checkKernel(t, g, c.Kernel) })
	t.Run("branches", func(t *testing.T) { checkBranches(t, g, c) })
	t.Run("moved tag", func(t *testing.T) { checkMovedTag(t, g, c.FPGACore) })
	t.Run("commit mode", func(t *testing.T) { checkCommitMode(t, g, c) })
	t.Run("repositories", func(t *testing.T) { checkRepositories(t, g, c) })
	t.Run("tags", func(t *testing.T) { checkTags(t, g, c) })
	t.Run("mirror", func(t *testing.T) { checkMirror(t, g, c) })
	t.Run("signed", func(t *testing.T) { checkSigned(t, g, c) })
	t.Run("quirky", func(t *testing.T) { checkQuirky(t, g, c.Quirky) })
}

// checkHistory verifies the superproject commits.
func checkHistory(t *testing.T, g *git.Runner, c *gittest.ComplexSuper) {
	t.Helper()
	if got := strings.Fields(mustGit(t, g, c.Dir, "rev-list", "--reverse", "HEAD")); !slices.Equal(
		got, c.History) || len(got) != 5 {
		t.Errorf("history %q, want %q", got, c.History)
	}
	subjects := strings.Split(mustGit(t, g, c.Dir, "log", "--reverse", "--format=%s"), "\n")
	want := []string{
		"initial commit",
		"add kernel, u-boot, fpga.core, crypto lib, legacy and tools",
		"add theme, fresh, app, sdk, mirror-lib, broken, signed and quirky",
		"track submodules with lazysubmodules",
		"manifest: update kernel to v6.6.9",
	}
	if !slices.Equal(subjects, want) {
		t.Errorf("subjects %q, want %q", subjects, want)
	}
	msg := mustGit(t, g, c.Dir, "log", "-1", "--format=%B")
	if !strings.HasSuffix(msg, "\n\nSigned-off-by: "+gittest.Name+" <"+gittest.Email+">") ||
		!strings.Contains(msg, "\nOld: "+c.Kernel.Tags["v6.6.8"][:12]+" (v6.6.8)\n") {
		t.Errorf("last message:\n%s", msg)
	}
	if n := len(gitlinks(t, g, c.Dir, c.History[1])); n != 6 {
		t.Errorf("second commit has %d gitlinks, want 6", n)
	}
	if n := len(gitlinks(t, g, c.Dir, c.History[2])); n != 14 {
		t.Errorf("third commit has %d gitlinks, want 14", n)
	}
	for _, e := range configEntries(t, g, c.Dir, "--blob="+c.History[2]+":.gitmodules") {
		if strings.Contains(e.Key, ".lsm-") || strings.HasSuffix(e.Key, ".branch") {
			t.Errorf("third commit has %s", e.Key)
		}
	}
	old := c.Kernel.Tags["v6.6.8"]
	if got := gitlinks(t, g, c.Dir, "HEAD~1")[c.Kernel.Path]; got != old {
		t.Errorf("kernel gitlink before the update %s, want %s", got, old)
	}
	lock := configEntries(t, g, c.Dir, "--blob=HEAD~1:"+gittest.LockFile)
	if !slices.Contains(lock, git.ConfigEntry{Key: "submodule.kernel.ref", Value: "v6.6.8"}) ||
		!slices.Contains(lock, git.ConfigEntry{Key: "submodule.kernel.commit", Value: old}) {
		t.Errorf("lock before the update: %q", lock)
	}
	// The lock file is tracked although .gitignore matches it.
	mustGit(t, g, c.Dir, "check-ignore", "--quiet", "--no-index", gittest.LockFile)
	if got := mustGit(t, g, c.Dir, "ls-files", "--", gittest.LockFile); got != gittest.LockFile {
		t.Errorf("lock file not tracked: %q", got)
	}
	if _, err := os.Stat(filepath.Join(c.Subdir, "README")); err != nil {
		t.Error(err)
	}
}

// checkStatus verifies what git status reports in the superproject: the
// dirty submodule and the one with a moved nested submodule, but not the
// quirky one, whose changes it ignores.
func checkStatus(t *testing.T, g *git.Runner, c *gittest.ComplexSuper) {
	t.Helper()
	got, err := g.Run(t.Context(), c.Dir, "status", "--porcelain=v1", "--ignore-submodules=none")
	if want := " M " + c.App.Path + "\n M " + c.Tools.Path + "\n"; err != nil || got != want {
		t.Errorf("status %q, %v; want %q", got, err, want)
	}
	got, err = g.Run(t.Context(), c.App.Dir(), "status", "--porcelain")
	if err != nil || got != " M "+gittest.TrackedFile+"\n" {
		t.Errorf("app status %q, %v", got, err)
	}
	// A nested submodule at another commit and nothing else.
	got = mustGit(t, g, c.Tools.Dir(), "status", "--porcelain=v2", "--untracked-files=no",
		"--ignore-submodules=none")
	if !strings.HasPrefix(got, "1 .M SC.. ") || !strings.HasSuffix(got, " inner") ||
		strings.Contains(got, "\n") {
		t.Errorf("tools status %q", got)
	}
}

// checkInvocations verifies the top level and the submodules that git
// finds from each start directory.
func checkInvocations(t *testing.T, c *gittest.ComplexSuper, invocations []gittest.Invocation) {
	t.Helper()
	g := c.Runner(t)
	for _, inv := range invocations {
		samePath(t, inv.Name, mustGit(t, g, inv.Dir, "rev-parse", "--show-toplevel"), inv.Root)
		var names []string
		file := filepath.Join(inv.Root, gittest.GitmodulesFile)
		if _, err := os.Stat(file); err == nil {
			for _, e := range configEntries(t, g, inv.Root, "-f", file) {
				if name, ok := strings.CutSuffix(strings.TrimPrefix(e.Key, "submodule."),
					".path"); ok {
					names = append(names, name)
				}
			}
		}
		if !slices.Equal(names, inv.Names) {
			t.Errorf("%s: submodules %q, want %q", inv.Name, names, inv.Names)
		}
	}
}

// checkKernel verifies the tags of the kernel submodule.
func checkKernel(t *testing.T, g *git.Runner, s *gittest.Submodule) {
	t.Helper()
	want := []string{"v6.6.11-rc1"}
	for i := 10; i >= 1; i-- {
		want = append(want, "v6.6."+strconv.Itoa(i))
	}
	if got := tagList(t, g, s.Dir(), s.Ref); !slices.Equal(got, want) {
		t.Errorf("kernel tags %q, want %q", got, want)
	}
	all := append(slices.Clone(want), "v6.7-rc1")
	if got := tagList(t, g, s.Dir(), ""); len(got) != len(all) || len(s.Tags) != len(all) {
		t.Errorf("kernel has tags %q, remote %v; want %q", got, s.Tags, all)
	}
	for _, tag := range all {
		kind := "commit"
		if n, _ := strconv.Atoi(strings.TrimPrefix(tag, "v6.6.")); n%2 == 1 ||
			tag == "v6.6.11-rc1" {
			kind = "tag"
		}
		if got := objectType(t, g, s.Dir(), "refs/tags/"+tag); got != kind {
			t.Errorf("kernel tag %s is a %s, want %s", tag, got, kind)
		}
		if got := mustGit(t, g, s.Dir(), "rev-parse", tag+"^{commit}"); got != s.Tags[tag] {
			t.Errorf("kernel tag %s at %s, remote %s", tag, got, s.Tags[tag])
		}
	}
	if s.Lock.Commit != s.Tags["v6.6.9"] || s.Branches["main"] != s.Tags["v6.7-rc1"] ||
		s.Branches["linux-6.6.y"] != s.Tags["v6.6.11-rc1"] {
		t.Errorf("kernel lock %+v, branches %v", s.Lock, s.Branches)
	}
}

// checkBranches verifies the submodules in branch mode.
func checkBranches(t *testing.T, g *git.Runner, c *gittest.ComplexSuper) {
	t.Helper()
	for _, s := range []*gittest.Submodule{c.UBoot, c.Tools, c.MirrorLib} {
		if s.Mode != gittest.ModeBranch || s.Branch != s.Ref || s.Lock.Ref != s.Ref {
			t.Errorf("%s: %+v", s.Name, s)
		}
		remote := mustGit(t, g, s.Dir(), "rev-parse", "refs/remotes/origin/"+s.Ref)
		if remote != s.Branches[s.Ref] || s.Head != s.Branches[s.Ref] && s != c.UBoot {
			t.Errorf("%s: origin/%s at %s, head %s, remote %v", s.Name, s.Ref, remote, s.Head,
				s.Branches)
		}
		if s.Branches["main"] == s.Head {
			t.Errorf("%s: checked out at the tip of main", s.Name)
		}
	}
	// u-boot has fetched main, which advanced after locking.
	if got := mustGit(t, g, c.UBoot.Dir(), "rev-parse", "origin/main~1"); got != c.UBoot.Head ||
		c.UBoot.Branches["next"] != c.UBoot.Branches["main"] {
		t.Errorf("u-boot: origin/main~1 %s, head %s, remote %v", got, c.UBoot.Head,
			c.UBoot.Branches)
	}
}

// checkMovedTag verifies that the tag of fpga.core moved on the remote
// only.
func checkMovedTag(t *testing.T, g *git.Runner, s *gittest.Submodule) {
	t.Helper()
	ref := "refs/tags/" + s.Ref
	local := mustGit(t, g, s.Dir(), "rev-parse", ref+"^{commit}")
	remote := mustGit(t, g, s.Upstream.Bare, "rev-parse", ref+"^{commit}")
	if local != s.Lock.Commit || local != s.Head || remote != s.Tags[s.Ref] ||
		remote == local || remote != s.Branches["main"] {
		t.Errorf("fpga.core: local %s, remote %s, lock %+v, remote refs %v %v", local, remote,
			s.Lock, s.Tags, s.Branches)
	}
	for _, dir := range []string{s.Dir(), s.Upstream.Bare} {
		if got := objectType(t, g, dir, ref); got != "tag" {
			t.Errorf("fpga.core tag is a %s in %s", got, dir)
		}
	}
	if objectType(t, g, s.Dir(), "refs/tags/v2.3.0") != "commit" || s.Name == s.Path {
		t.Errorf("fpga.core: v2.3.0 is not lightweight or name %q equals path", s.Name)
	}
}

// checkCommitMode verifies the submodules in commit mode and without mode.
func checkCommitMode(t *testing.T, g *git.Runner, c *gittest.ComplexSuper) {
	t.Helper()
	s := c.CryptoLib
	if len(s.Ref) != hexLen(c.Format) || s.Lock.Ref != s.Ref || s.Lock.Commit != s.Ref ||
		s.Head != s.Ref || len(s.Tags) != 0 {
		t.Errorf("crypto lib: %+v", s)
	}
	if got := mustGit(t, g, s.Upstream.Bare, "rev-parse", "main~1"); got != s.Ref {
		t.Errorf("crypto lib pinned at %s, main~1 is %s", s.Ref, got)
	}
	if fi, err := os.Stat(c.SubmoduleSubdir); err != nil || !fi.IsDir() ||
		filepath.Dir(c.SubmoduleSubdir) != s.Dir() {
		t.Errorf("submodule subdirectory %s: %v", c.SubmoduleSubdir, err)
	}
	l := c.Legacy
	if l.Mode != "" || l.Ref != "" || l.Lock != nil || l.Head != l.Branches["main"] {
		t.Errorf("legacy: %+v", l)
	}
}

// checkRepositories verifies the submodules that are not checked out.
func checkRepositories(t *testing.T, g *git.Runner, c *gittest.ComplexSuper) {
	t.Helper()
	theme := c.Theme
	if theme.Head != "" || theme.GitDir == "" {
		t.Errorf("theme: %+v", theme)
	}
	// Offline initialization finds the recorded commit and the tag.
	mustGit(t, g, theme.GitDir, "rev-parse", "--verify", "--quiet", theme.Gitlink+"^{commit}")
	if got := mustGit(t, g, theme.GitDir, "rev-parse", theme.Ref); got != theme.Gitlink {
		t.Errorf("theme tag at %s, want %s", got, theme.Gitlink)
	}
	fresh := c.Fresh
	if fresh.Head != "" || fresh.GitDir != "" || fresh.Lock.Commit != fresh.Tags["v1.1.0"] {
		t.Errorf("fresh: %+v", fresh)
	}
	if got := objectType(t, g, fresh.Upstream.Bare, "refs/tags/v1.1.0"); got != "tag" {
		t.Errorf("fresh v1.1.0 is a %s", got)
	}
	if _, ok := fresh.Tags["v2.0.0"]; !ok {
		t.Errorf("fresh tags %v", fresh.Tags)
	}
}

// checkTags verifies the submodules in tag modes that are checked out.
func checkTags(t *testing.T, g *git.Runner, c *gittest.ComplexSuper) {
	t.Helper()
	sdk := []string{"v3.0.0-rc.2", "v3.0.0-rc.1", "v2.9.0"}
	if got := tagList(t, g, c.SDK.Dir(), c.SDK.Ref); !slices.Equal(got, sdk) ||
		c.SDK.Lock.Ref != sdk[0] || c.SDK.Head != c.SDK.Tags[sdk[0]] {
		t.Errorf("sdk tags %q, lock %+v", got, c.SDK.Lock)
	}
	app := c.App
	if got := tagList(t, g, app.Dir(), ""); !slices.Equal(got, []string{"v1.3.0", "v1.2.0",
		"v1.1.0"}) || app.Head != app.Tags[app.Ref] || objectType(t, g, app.Dir(),
		"refs/tags/"+app.Ref) != "tag" {
		t.Errorf("app tags %q, head %s", got, app.Head)
	}
	broken := c.Broken
	for _, dir := range []string{broken.Dir(), broken.Upstream.Bare} {
		wantNoRef(t, g, dir, "refs/tags/"+broken.Ref)
	}
	if len(broken.Tags) != 1 || broken.Lock.Ref != "v1.0.0" ||
		broken.Head != broken.Tags["v1.0.0"] {
		t.Errorf("broken: %+v", broken)
	}
}

// checkMirror verifies that the mirror submodule reaches its repository
// only through the rewritten URL.
func checkMirror(t *testing.T, g *git.Runner, c *gittest.ComplexSuper) {
	t.Helper()
	s := c.MirrorLib
	// "git remote get-url" would apply the rewriting.
	if got := mustGit(t, g, s.Dir(), "config", "remote.origin.url"); got != gittest.MirrorURL {
		t.Errorf("mirror-lib origin %s", got)
	}
	if got := mustGit(t, g, c.ToolsInner.Dir(), "config", "remote.origin.url"); got !=
		gittest.InnerURL {
		t.Errorf("inner origin %s", got)
	}
	heads := mustGit(t, g, s.Dir(), "ls-remote", "--heads", "origin", "stable")
	if !strings.HasPrefix(heads, s.Branches["stable"]+"\t") {
		t.Errorf("ls-remote through the mirror: %q", heads)
	}
	_, err := tryGit(t, gittest.Runner(t), s.Dir(), "ls-remote", "--heads", "origin")
	if gitErr, ok := errors.AsType[*git.Error](err); !ok ||
		!strings.Contains(gitErr.Stderr, "not allowed") {
		t.Errorf("ls-remote without the mirror configuration: %v", err)
	}
}

// checkSigned verifies the signed tag.
func checkSigned(t *testing.T, g *git.Runner, c *gittest.ComplexSuper) {
	t.Helper()
	s := c.Signed
	if c.SigningKey == "" {
		t.Skip("ssh-keygen not installed")
	}
	mustGit(t, g, s.Dir(), "verify-tag", s.Ref)
	if _, err := tryGit(t, g, s.Dir(), "verify-tag", "v1.0.1"); err == nil {
		t.Error("unsigned tag v1.0.1 verified")
	}
	// Without the allowed signers of Config, nothing verifies.
	if _, err := tryGit(t, gittest.Runner(t), s.Dir(), "verify-tag", s.Ref); err == nil {
		t.Error("signed tag verified without the fixture configuration")
	}
	if !slices.Contains(c.Config, "gpg.ssh.allowedSignersFile="+c.SigningKey+
		".allowed_signers") {
		t.Errorf("config %q", c.Config)
	}
}

// checkQuirky verifies the submodule with unusual native settings.
func checkQuirky(t *testing.T, g *git.Runner, s *gittest.Submodule) {
	t.Helper()
	if got := mustGit(t, g, s.Dir(), "rev-parse", "--is-shallow-repository"); got != "true" {
		t.Errorf("quirky shallow: %s", got)
	}
	if got := mustGit(t, g, s.Dir(), "rev-list", "--count", "HEAD"); got != "1" ||
		!strings.HasPrefix(s.URL, "file:///") || s.Head != s.Tags[s.Ref] {
		t.Errorf("quirky: %s commits, %+v", got, s)
	}
}

// commitSummary lists the upstream commits of the fixture, one line per
// submodule, tag or branch.
func commitSummary(c *gittest.ComplexSuper) string {
	var lines []string
	for _, s := range append(c.Submodules(), c.ToolsInner) {
		lines = append(lines, fmt.Sprintf("%s gitlink %s head %s", s.Name, s.Gitlink, s.Head))
		if s.Lock != nil {
			lines = append(lines, fmt.Sprintf("%s lock %s %s %s", s.Name, s.Lock.Mode,
				s.Lock.Ref, s.Lock.Commit))
		}
		for _, name := range slices.Sorted(maps.Keys(s.Tags)) {
			lines = append(lines, fmt.Sprintf("%s tag %s %s", s.Name, name, s.Tags[name]))
		}
		for _, name := range slices.Sorted(maps.Keys(s.Branches)) {
			lines = append(lines, fmt.Sprintf("%s branch %s %s", s.Name, name,
				s.Branches[name]))
		}
	}
	return strings.Join(lines, "\n")
}

// commitDigest hashes commitSummary.
func commitDigest(c *gittest.ComplexSuper) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(commitSummary(c))))
}
