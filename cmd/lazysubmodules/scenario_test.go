// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/FPGArtktic/lazysubmodules/internal/git/gittest"
)

// cliScenario runs the commands against one complex superproject. Its
// steps build on each other.
type cliScenario struct {
	c   *gittest.ComplexSuper
	inv invocation
}

// complexRefusal is the refusal of "update" in the complex superproject
// as it is built.
const complexRefusal = "" +
	"lazysubmodules: fresh: refused: submodule is not initialized (use --fetch)\n" +
	"lazysubmodules: app: refused: submodule has uncommitted changes\n" +
	"lazysubmodules: broken: refused: ref not found in local refs: " +
	"tag v9.9.9 does not exist or does not point to a commit\n"

// signOff is the trailer that "git commit -s" adds in the test environment.
const signOff = "Signed-off-by: " + gittest.Name + " <" + gittest.Email + ">\n"

func TestComplexScenario(t *testing.T) {
	t.Parallel()
	c := gittest.NewComplexSuper(t, gittest.SHA1)
	s := &cliScenario{c: c, inv: invocation{dir: c.Dir, gitEnv: gittest.Env(t, c.Config...)}}
	for _, step := range []struct {
		name string
		run  func(t *testing.T)
	}{
		{"status", s.status},
		{"selection", s.selection},
		{"refusal", s.refusal},
		{"dry run", s.dryRun},
		{"verify", s.verify},
		{"foreach", s.foreach},
		{"commit", s.commit},
		{"moved tag", s.movedTag},
		{"stage", s.stage},
		{"set", s.set},
		{"clone", s.clone},
		{"add", s.add},
	} {
		if !t.Run(step.name, step.run) {
			t.FailNow()
		}
	}
}

// in returns the invocation of the scenario in another directory.
func (s *cliScenario) in(dir string) invocation {
	inv := s.inv
	inv.dir = dir
	return inv
}

// git runs git in the superproject.
func (s *cliScenario) git(t *testing.T, args ...string) string {
	t.Helper()
	return s.c.Git(t, s.c.Dir, args...)
}

// snapshot describes everything a command could change: the index, the
// working tree, HEAD, the configuration files, the checked-out commit of
// every submodule and the submodule repositories.
func (s *cliScenario) snapshot(t *testing.T) string {
	t.Helper()
	c := s.c
	parts := []string{
		s.git(t, "rev-parse", "HEAD"),
		s.git(t, "ls-files", "--stage"),
		s.git(t, "status", "--porcelain=v1", "--untracked-files=all",
			"--ignore-submodules=none"),
		s.git(t, "config", "--list", "--local"),
	}
	for _, name := range []string{gittest.GitmodulesFile, gittest.LockFile} {
		data, err := os.ReadFile(filepath.Join(c.Dir, name))
		if err != nil {
			t.Fatal(err)
		}
		parts = append(parts, string(data))
	}
	for _, sub := range c.Submodules() {
		head := "not checked out"
		if _, err := os.Lstat(filepath.Join(sub.Dir(), ".git")); err == nil {
			head = c.Git(t, sub.Dir(), "rev-parse", "HEAD")
		}
		parts = append(parts, sub.Name+" "+head)
	}
	modules, err := os.ReadDir(filepath.Join(c.GitDir, "modules"))
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range modules {
		parts = append(parts, "module "+m.Name())
	}
	return strings.Join(parts, "\n")
}

// unchanged runs steps and checks that they change nothing.
func (s *cliScenario) unchanged(t *testing.T, what string, steps func()) {
	t.Helper()
	before := s.snapshot(t)
	steps()
	if after := s.snapshot(t); after != before {
		t.Errorf("%s changed the superproject:\n%s", what, lineDiff(before, after))
	}
}

// porcelainLine returns the porcelain line of one submodule.
func (s *cliScenario) porcelainLine(t *testing.T, name string) string {
	t.Helper()
	res := s.inv.wantCode(t, exitOK, "status", "--porcelain", name)
	lines := strings.Split(strings.TrimSuffix(res.stdout, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("status --porcelain %s:\n%v", name, res)
	}
	return lines[1]
}

// wantState checks the porcelain state of a submodule.
func (s *cliScenario) wantState(t *testing.T, name, state string) {
	t.Helper()
	line := s.porcelainLine(t, name)
	if !strings.HasSuffix(line, "\t"+state) {
		t.Errorf("%s: %q, want state %s", name, line, state)
	}
}

// sgr matches an SGR escape sequence.
func sgr() *regexp.Regexp {
	return regexp.MustCompile("\x1b\\[[0-9;:]*m")
}

func (s *cliScenario) status(t *testing.T) {
	s.unchanged(t, "status", func() {
		res := s.inv.wantCode(t, exitOK, "status", "--porcelain=v1")
		wantGolden(t, "status-complex-sha1", res.stdout)
		for _, inv := range []invocation{s.inv, s.in(s.c.Subdir), s.in(s.c.Theme.Dir())} {
			inv.want(t, []string{"status", "--porcelain"}, exitOK, res.stdout, "")
		}
		lines := strings.SplitAfter(res.stdout, "\n")
		s.inv.want(t, []string{"status", "legacy", "--porcelain", "kernel", "legacy"}, exitOK,
			lines[0]+lines[1]+lines[5], "")

		table := s.inv.wantCode(t, exitOK, "status")
		wantGolden(t, "status-complex-sha1-table", table.stdout)
		wantPrintable(t, "table", table.stdout)
		for _, environ := range [][]string{
			{"TERM=xterm", "NO_COLOR=1"}, {"TERM=dumb"}, {"NO_COLOR=0"},
		} {
			inv := s.inv
			inv.terminal, inv.environ = true, environ
			inv.want(t, []string{"status"}, exitOK, table.stdout, "")
		}
		inv := s.inv
		inv.terminal = true
		colored := inv.wantCode(t, exitOK, "status")
		if sgr().ReplaceAllString(colored.stdout, "") != table.stdout ||
			!strings.Contains(colored.stdout, "  \x1b[33mbehind\x1b[0m\n") ||
			!strings.Contains(colored.stdout, "  \x1b[31mmissing-ref\x1b[0m\n") {
			t.Errorf("colored table:\n%q", colored.stdout)
		}
	})
}

func (s *cliScenario) selection(t *testing.T) {
	inv := s.inv
	s.unchanged(t, "commands with bad names or values", func() {
		const unknown = "lazysubmodules: nope: no such submodule\n"
		for _, args := range [][]string{
			{"update", "nope"}, {"update", "legacy", "nope", "--dry-run"},
			{"status", "kernel", "nope"}, {"verify", "nope"}, {"fetch", "nope", "legacy"},
		} {
			inv.want(t, args, exitError, "", unknown)
		}
		res := inv.wantCode(t, exitError, "set", "nope", "--tag", "v1")
		if !strings.Contains(res.stderr, "nope") || res.stdout != "" {
			t.Errorf("set nope:\n%v", res)
		}

		const unmanaged = "lazysubmodules: legacy: refused: " +
			"submodule is not managed by lazysubmodules\n"
		for _, args := range [][]string{
			{"update", "legacy"}, {"update", "kernel", "legacy", "--dry-run", "--fetch"},
			{"verify", "legacy"}, {"fetch", "legacy"},
		} {
			inv.want(t, args, exitRefused, "", unmanaged)
		}

		for _, args := range [][]string{
			{"set", "kernel", "--tag", "bad ref"},
			{"set", "kernel", "--branch", "-x"},
			{"set", "kernel", "--tag-pattern="},
			{"set", "crypto lib", "--commit", "xyz"},
			{"add", s.c.Kernel.Upstream.Bare, "../escape", "--tag", "v6.6.1"},
			{"add", "--tag", "v6.6.1", "--", "-u", "libs/new"},
		} {
			res := inv.wantCode(t, exitUsage, args...)
			if res.stdout != "" || !strings.HasPrefix(res.stderr, "lazysubmodules: ") ||
				!strings.Contains(res.stderr, "invalid") {
				t.Errorf("%q:\n%v", args, res)
			}
		}
	})
}

func (s *cliScenario) refusal(t *testing.T) {
	inv := s.inv
	s.unchanged(t, "refused updates", func() {
		for _, args := range [][]string{
			{"update"},
			{"update", "--commit", "--include-prerelease"},
			{"update", "--dry-run"},
		} {
			inv.want(t, args, exitRefused, "", complexRefusal)
		}
		inv.want(t, []string{"update", "broken", "kernel", "app", "--fetch"}, exitRefused, "",
			"lazysubmodules: app: refused: submodule has uncommitted changes\n")
		inv.want(t, []string{"update", "broken", "kernel", "app"}, exitRefused, "",
			"lazysubmodules: app: refused: submodule has uncommitted changes\n"+
				"lazysubmodules: broken: refused: ref not found in local refs: "+
				"tag v9.9.9 does not exist or does not point to a commit\n")
		inv.want(t, []string{"update", "fresh"}, exitRefused, "",
			"lazysubmodules: fresh: refused: submodule is not initialized (use --fetch)\n")
	})
}

// short7 abbreviates a commit like the output does.
func short7(commit string) string {
	return commit[:7]
}

func (s *cliScenario) dryRun(t *testing.T) {
	c, inv := s.c, s.inv
	s.unchanged(t, "dry runs", func() {
		inv.want(t, []string{"update", "kernel", "u-boot", "theme", "sdk", "--dry-run",
			"--commit"}, exitOK,
			"would update kernel: v6.6.9 ("+short7(c.Kernel.Lock.Commit)+") -> v6.6.10 ("+
				short7(c.Kernel.Tags["v6.6.10"])+")\n"+
				"would update u-boot: main ("+short7(c.UBoot.Lock.Commit)+") -> main ("+
				short7(c.UBoot.Branches["main"])+")\n"+
				"would update theme: v1.0.0 ("+short7(c.Theme.Gitlink)+"), initialize\n"+
				"sdk: up to date\n"+
				"would commit \"manifest: update 2 submodules\"\n", "")
		inv.want(t, []string{"update", "--dry-run", "--fetch", "fresh", "broken"}, exitOK,
			"would update fresh: v1.1.0 ("+short7(c.Fresh.Gitlink)+
				") -> unknown until fetched, clone\n"+
				"would update broken: v1.0.0 ("+short7(c.Broken.Gitlink)+
				") -> unknown until fetched\n", "")
		inv.want(t, []string{"update", "sdk", "kernel", "--dry-run", "--include-prerelease"},
			exitOK,
			"would update kernel: v6.6.9 ("+short7(c.Kernel.Lock.Commit)+") -> v6.6.11-rc1 ("+
				short7(c.Kernel.Tags["v6.6.11-rc1"])+")\nsdk: up to date\n", "")
		inv.want(t, []string{"update", "--dry-run", "--commit", "sdk", "crypto lib"}, exitOK,
			"crypto lib: up to date\nsdk: up to date\nnothing to commit\n", "")
	})
}

func (s *cliScenario) verify(t *testing.T) {
	inv := s.inv
	s.unchanged(t, "verify", func() {
		inv.want(t, []string{"verify"}, exitVerify, ""+
			"kernel: ok (7 checks)\n"+
			"u-boot: ok (6 checks)\n"+
			"fpga.core: ok (7 checks)\n"+
			"crypto lib: ok (6 checks)\n"+
			"tools: ok (6 checks)\n"+
			"theme: failed (1 of 5 checks)\n"+
			"  initialized: submodule is not checked out\n"+
			"fresh: failed (2 of 5 checks)\n"+
			"  lock-config: cannot list tags: submodule repository is missing\n"+
			"  initialized: submodule is not checked out\n"+
			"app: ok (7 checks)\n"+
			"sdk: ok (7 checks)\n"+
			"mirror-lib: ok (6 checks)\n"+
			"broken: failed (1 of 7 checks)\n"+
			"  lock-config: lock records tag v1.0.0, configuration has v9.9.9\n"+
			"signed: ok (7 checks)\n"+
			"quirky: ok (7 checks)\n",
			"lazysubmodules: verification failed: theme, fresh, broken\n")
		inv.want(t, []string{"verify", "fpga.core", "kernel", "u-boot"}, exitOK,
			"kernel: ok (7 checks)\nu-boot: ok (6 checks)\nfpga.core: ok (7 checks)\n", "")

		res := inv.wantCode(t, exitVerify, "verify", "--signatures", "kernel")
		if !strings.HasPrefix(res.stdout, "kernel: failed (1 of 8 checks)\n  signature: ") {
			t.Errorf("unsigned tag:\n%v", res)
		}
		if s.c.SigningKey != "" {
			inv.want(t, []string{"verify", "signed", "--signatures"}, exitOK,
				"signed: ok (8 checks)\n", "")
		}
	})
}

func (s *cliScenario) foreach(t *testing.T) {
	c, inv := s.c, s.inv
	const script = `printf '%s|%s|%s|%s|%s|%s\n' "$name" "$sm_path" "$displaypath" ` +
		`"$LSM_MODE" "$LSM_REF" "$sha1"`
	var want, wantSub strings.Builder
	for _, sub := range c.Submodules() {
		if sub.Mode == "" || sub.Head == "" {
			continue
		}
		fmt.Fprintf(&want, "%s|%s|%s|%s|%s|%s\n", sub.Name, sub.Path, sub.Path, sub.Mode,
			sub.Ref, sub.Head)
		fmt.Fprintf(&wantSub, "%s|%s|../../%s|%s|%s|%s\n", sub.Name, sub.Path, sub.Path,
			sub.Mode, sub.Ref, sub.Head)
	}
	s.unchanged(t, "foreach", func() {
		var skipped string
		for _, sub := range []invocation{s.inv, s.in(c.Subdir)} {
			res := sub.wantCode(t, exitOK, "foreach", "--", "sh", "-c", script)
			want := want.String()
			if sub.dir == c.Subdir {
				want = wantSub.String()
			}
			notes := strings.Split(res.stderr, "\n")
			if res.stdout != want || len(notes) != 3 ||
				!strings.HasPrefix(notes[0], "skipping theme: ") ||
				!strings.HasPrefix(notes[1], "skipping fresh: ") {
				t.Errorf("foreach in %s:\n%v\nwant stdout\n%s", sub.dir, res, want)
			}
			skipped = res.stderr
		}
		inv.want(t, []string{"foreach", "sh", "-c", `test "$name" != fpga.core`}, exitError, "",
			"lazysubmodules: fpga.core: sh: exit status 1\n")
		res := inv.wantCode(t, exitError, "foreach", "--", "lsm-no-such-command", "-x")
		if !strings.HasPrefix(res.stderr, "lazysubmodules: kernel: lsm-no-such-command: ") {
			t.Errorf("missing command:\n%v", res)
		}
		stdin := inv
		stdin.stdin = strings.NewReader("from stdin\n")
		stdin.want(t, []string{"foreach", "--", "cat"}, exitOK, "from stdin\n", skipped)
	})
}

func (s *cliScenario) commit(t *testing.T) {
	c, inv := s.c, s.inv
	c.Git(t, c.App.Dir(), "checkout", "--", gittest.TrackedFile)
	readme := gittest.SubdirPath + "/README"
	gittest.WriteFile(t, filepath.Join(c.Subdir, "README"), "unrelated change\n")
	s.git(t, "add", "--", readme)
	s.unchanged(t, "refused commit", func() {
		inv.want(t, []string{"update", "--commit", "kernel", "u-boot"}, exitRefused, "",
			"lazysubmodules: refused: the commit would include unrelated changes: "+
				readme+"\n")
	})
	s.git(t, "reset", "--quiet", "--", readme)

	k, u := c.Kernel, c.UBoot
	res := inv.wantCode(t, exitOK, "update", "--commit", "u-boot", "kernel")
	head := s.git(t, "rev-parse", "HEAD")
	wantStdout := "kernel: v6.6.9 (" + short7(k.Lock.Commit) + ") -> v6.6.10 (" +
		short7(k.Tags["v6.6.10"]) + ")\n" +
		"u-boot: main (" + short7(u.Lock.Commit) + ") -> main (" +
		short7(u.Branches["main"]) + ")\n" +
		"committed " + head + "\n"
	if res.stdout != wantStdout || res.stderr != "" {
		t.Errorf("update --commit:\n%v\nwant stdout\n%s", res, wantStdout)
	}
	want := "manifest: update 2 submodules\n\n" +
		"Submodule \"kernel\":\n" +
		"  Tracking mode: tag-pattern v6.6.*\n" +
		"  Old: " + k.Lock.Commit[:12] + " (v6.6.9)\n" +
		"  New: " + k.Tags["v6.6.10"][:12] + " (v6.6.10)\n\n" +
		"Submodule \"u-boot\":\n" +
		"  Tracking mode: branch main\n" +
		"  Old: " + u.Lock.Commit[:12] + " (main)\n" +
		"  New: " + u.Branches["main"][:12] + " (main)\n\n" + signOff
	if got := s.git(t, "log", "-1", "--format=%B") + "\n"; got != want {
		t.Errorf("commit message:\n%s\nwant\n%s", got, want)
	}
	if got := s.git(t, "rev-parse", "HEAD^"); got != c.History[len(c.History)-1] {
		t.Errorf("HEAD^ is %s, not the last fixture commit", got)
	}
	if got := s.git(t, "diff", "--name-only", "HEAD^", "HEAD"); got !=
		".lsm.lock\nbootloader/u-boot\nkernel" {
		t.Errorf("committed files %q", got)
	}
	if got := s.git(t, "status", "--porcelain", "--", readme); got != "M "+readme &&
		got != " M "+readme {
		t.Errorf("unrelated change: %q", got)
	}
	s.unchanged(t, "second commit", func() {
		inv.want(t, []string{"update", "kernel", "u-boot", "--commit"}, exitOK,
			"kernel: up to date\nu-boot: up to date\nnothing to commit\n", "")
	})
}

func (s *cliScenario) movedTag(t *testing.T) {
	f, inv := s.c.FPGACore, s.inv
	s.wantState(t, f.Name, "ok")
	res := inv.wantCode(t, exitOK, "fetch", f.Name)
	if res.stdout != "fpga.core: fetched\n" || !strings.Contains(res.stderr, "v2.3.1") {
		t.Errorf("fetch:\n%v", res)
	}
	s.wantState(t, f.Name, "drift")
	inv.want(t, []string{"verify", f.Name}, exitVerify,
		"fpga.core: failed (1 of 7 checks)\n"+
			"  tag: tag v2.3.1 points to "+f.Tags[f.Ref][:12]+", locked "+f.Lock.Commit[:12]+
			" (moved tag)\n",
		"lazysubmodules: verification failed: fpga.core\n")
}

func (s *cliScenario) stage(t *testing.T) {
	c, f, inv := s.c, s.c.FPGACore, s.inv
	moved := f.Tags[f.Ref]
	fpgaLine := "fpga.core: v2.3.1 (" + short7(f.Lock.Commit) + ") -> v2.3.1 (" +
		short7(moved) + ")\n"
	res := inv.wantCode(t, exitOK, "update", "theme", "fpga.core")
	if res.stdout != fpgaLine+"theme: v1.0.0 ("+short7(c.Theme.Gitlink)+"), initialized\n" ||
		!strings.Contains(res.stderr, c.Theme.Path) {
		t.Errorf("update:\n%v", res)
	}
	if got := s.git(t, "diff", "--cached", "--name-only"); got != ".lsm.lock\nip/fpga-core" {
		t.Errorf("staged %q", got)
	}
	if got := c.Git(t, f.Dir(), "rev-parse", "HEAD"); got != moved {
		t.Errorf("fpga.core at %s, want %s", got, moved)
	}
	s.wantState(t, f.Name, "ok")
	s.wantState(t, c.Theme.Name, "ok")

	res = inv.wantCode(t, exitOK, "update", "fpga.core", "theme", "--commit")
	head := s.git(t, "rev-parse", "HEAD")
	if res.stdout != fpgaLine+"theme: up to date\ncommitted "+head+"\n" || res.stderr != "" {
		t.Errorf("update --commit:\n%v", res)
	}
	want := "manifest: update fpga.core to v2.3.1\n\n" +
		"Tracking mode: tag v2.3.1\n" +
		"Old: " + f.Lock.Commit[:12] + " (v2.3.1)\n" +
		"New: " + moved[:12] + " (v2.3.1)\n\n" + signOff
	if got := s.git(t, "log", "-1", "--format=%B") + "\n"; got != want {
		t.Errorf("commit message:\n%s\nwant\n%s", got, want)
	}
	inv.want(t, []string{"verify", "fpga.core", "theme"}, exitOK,
		"fpga.core: ok (7 checks)\ntheme: ok (7 checks)\n", "")
}

func (s *cliScenario) set(t *testing.T) {
	c, inv := s.c, s.inv
	staged := s.git(t, "diff", "--cached", "--name-only")
	inv.want(t, []string{"set", "legacy", "--branch", "main"}, exitOK,
		"legacy: tracks branch main\n", "")
	got := s.git(t, "config", "-f", gittest.GitmodulesFile, "--get-regexp",
		`^submodule\.legacy\.`)
	want := "submodule.legacy.path vendor/legacy\nsubmodule.legacy.url " + c.Legacy.URL +
		"\nsubmodule.legacy.branch main\nsubmodule.legacy.lsm-mode branch" +
		"\nsubmodule.legacy.lsm-ref main"
	if got != want {
		t.Errorf("legacy configuration:\n%s\nwant\n%s", got, want)
	}
	s.wantState(t, c.Legacy.Name, "behind")

	tip := c.CryptoLib.Branches["main"]
	inv.want(t, []string{"set", "--commit", short7(tip), "crypto lib"}, exitOK,
		"crypto lib: tracks commit "+tip+"\n", "")
	s.wantState(t, c.CryptoLib.Name, "behind")
	inv.want(t, []string{"set", "kernel", "--tag", "v6.6.10"}, exitOK,
		"kernel: tracks tag v6.6.10\n", "")
	s.wantState(t, c.Kernel.Name, "behind")
	k10 := short7(c.Kernel.Tags["v6.6.10"])
	inv.want(t, []string{"update", "kernel", "--dry-run"}, exitOK,
		"would update kernel: tag-pattern v6.6.10 ("+k10+") -> tag v6.6.10 ("+k10+")\n", "")
	if got := s.git(t, "diff", "--cached", "--name-only"); got != staged {
		t.Errorf("set staged %q", got)
	}
}

func (s *cliScenario) clone(t *testing.T) {
	c, inv := s.c, s.inv
	res := inv.wantCode(t, exitOK, "fetch", "fresh")
	if res.stdout != "fresh: cloned and fetched\n" ||
		!strings.Contains(res.stderr, "Cloning into") {
		t.Errorf("fetch fresh:\n%v", res)
	}
	s.wantState(t, c.Fresh.Name, "ok")
	inv.want(t, []string{"fetch", "fresh", "quirky"}, exitOK,
		"fresh: fetched\nquirky: fetched\n", "")
}

func (s *cliScenario) add(t *testing.T) {
	c, inv := s.c, s.inv
	bare := c.Kernel.Upstream.Bare
	want := []string{".gitmodules", ".lsm.lock", "third_party/linux", "third_party/rc"}
	if staged := s.git(t, "diff", "--cached", "--name-only"); staged != "" {
		want = append(want, strings.Split(staged, "\n")...)
	}
	slices.Sort(want)
	want = slices.Compact(want)
	res := inv.wantCode(t, exitOK, "add", bare, "third_party/linux", "--tag-pattern", "v6.6.*")
	if res.stdout != "third_party/linux: added at v6.6.10 ("+
		short7(c.Kernel.Tags["v6.6.10"])+")\n" ||
		!strings.Contains(res.stderr, "Cloning into") {
		t.Errorf("add:\n%v", res)
	}
	res = inv.wantCode(t, exitOK, "add", "--include-prerelease", bare, "third_party/rc",
		"--tag-pattern=v6.6.*")
	if res.stdout != "third_party/rc: added at v6.6.11-rc1 ("+
		short7(c.Kernel.Tags["v6.6.11-rc1"])+")\n" ||
		!strings.Contains(res.stderr, "Cloning into") {
		t.Errorf("add with pre-releases:\n%v", res)
	}
	if got := strings.Split(s.git(t, "diff", "--cached", "--name-only"), "\n"); !slices.Equal(
		got, want) {
		t.Errorf("staged %q, want %q", got, want)
	}
	s.wantState(t, "third_party/linux", "ok")
	s.wantState(t, "third_party/rc", "ok")

	s.unchanged(t, "refused add", func() {
		res := inv.wantCode(t, exitRefused, "add", bare, "third_party/linux", "--tag",
			"v6.6.1")
		if res.stdout != "" || !strings.Contains(res.stderr, "path already exists") {
			t.Errorf("add again:\n%v", res)
		}
	})
}
