// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package core_test

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FPGArtktic/lazysubmodules/internal/core"
	"github.com/FPGArtktic/lazysubmodules/internal/git"
	"github.com/FPGArtktic/lazysubmodules/internal/git/gittest"
	"github.com/FPGArtktic/lazysubmodules/internal/manifest"
)

// complexParts checks features of the complex superproject that concern a
// few of its submodules each. The parts run in order on one fixture and
// touch different submodules.
type complexParts struct {
	c    *gittest.ComplexSuper
	g    *git.Runner
	repo *core.Repo
}

func TestScenarioComplexParts(t *testing.T) {
	t.Parallel()
	for _, format := range formats() {
		t.Run(format, func(t *testing.T) {
			t.Parallel()
			c := gittest.NewComplexSuper(t, format)
			p := &complexParts{c: c, g: c.Runner(t)}
			p.repo = openRepo(t, p.g, c.Dir)
			for _, part := range []struct {
				name string
				run  func(t *testing.T)
			}{
				{"foreach", p.foreach},
				{"manual checkout", p.manualCheckout},
				{"signatures", p.signatures},
				{"nested submodule", p.nested},
				{"quirky settings", p.quirky},
			} {
				if !t.Run(part.name, part.run) {
					t.FailNow()
				}
			}
		})
	}
}

// runForeach runs Foreach in the Repo opened at dir and returns the output
// streams.
func (p *complexParts) runForeach(t *testing.T, dir string, args ...string) (
	string, string, error,
) {
	t.Helper()
	var stdout, stderr strings.Builder
	err := openRepo(t, p.g, dir).Foreach(t.Context(), core.ForeachOptions{
		Args: args, Stdout: &stdout, Stderr: &stderr,
	})
	return stdout.String(), stderr.String(), err
}

// foreach runs a command in every checked-out managed submodule.
func (p *complexParts) foreach(t *testing.T) {
	c := p.c
	top := realDir(t, c.Dir)
	var want strings.Builder
	for _, s := range c.Submodules() {
		if s.Mode == "" || s.Head == "" {
			continue
		}
		fmt.Fprintf(&want, "%s|%s|../../%s|%s|%s|%s|%s|%s\n", s.Name, s.Path, s.Path, s.Head,
			top, s.Mode, s.Ref, realDir(t, s.Dir()))
	}
	before := treeState(t, c.Dir)
	stdout, stderr, err := p.runForeach(t, c.Subdir, "sh", "-c", envScript)
	// Neither the unmanaged submodule nor the nested one is visited.
	notes := "skipping theme: submodule is not checked out\n" +
		"skipping fresh: submodule is not checked out\n"
	if err != nil || stdout != want.String() || stderr != notes {
		t.Errorf("Foreach = %v\nstdout:\n%s\nwant:\n%s\nstderr:\n%s", err, stdout, want.String(),
			stderr)
	}

	// The first failure ends the run.
	stdout, _, err = p.runForeach(t, c.Dir, "sh", "-c", `echo "$name"; test "$name" != tools`)
	if _, ok := errors.AsType[*exec.ExitError](err); !ok ||
		err.Error() != "tools: sh: exit status 1" ||
		stdout != "kernel\nu-boot\nfpga.core\ncrypto lib\ntools\n" {
		t.Errorf("failing Foreach = %v\nstdout:\n%s", err, stdout)
	}
	wantSameTree(t, "foreach", before, treeState(t, c.Dir))
}

// manualCheckout moves the submodule in commit mode away from its pinned
// commit by hand; the update moves it back.
func (p *complexParts) manualCheckout(t *testing.T) {
	c, r := p.c, p.repo
	crypto := c.CryptoLib
	tip := crypto.Branches["main"]
	runGit(t, p.g, crypto.Dir(), "checkout", "--quiet", "--detach", tip)
	st, err := r.Status(t.Context(), []string{crypto.Name})
	if err != nil {
		t.Fatal(err)
	}
	wantStatusRows(t, st, []statusRow{{crypto.Name, core.StateDrift, fmt.Sprintf(
		"HEAD %s differs from locked commit %s", tip[:12], crypto.Lock.Commit[:12])}})
	vr, err := r.Verify(t.Context(), []string{crypto.Name}, core.VerifyOptions{})
	wantErr(t, "Verify", err, core.ErrVerify)
	if len(vr) != 1 {
		t.Fatalf("Verify = %+v, %v", vr, err)
	}
	wantChecks(t, vr[0], branchChecks(), []string{core.CheckHead}, "HEAD "+tip[:12])

	want := unchangedView(crypto)
	want.OldHead, want.Changed = tip, true
	res := mustRepoUpdate(t, r, core.UpdateOptions{Names: []string{crypto.Name}})
	wantChanges(t, res.Changes, []changeView{want})
	if got := headOf(t, crypto.Dir()); got != crypto.Gitlink {
		t.Errorf("crypto lib at %s, want %s", got, crypto.Gitlink)
	}
	// The superproject recorded the pinned commit already.
	wantStaged(t, c.Dir)
	st, err = r.Status(t.Context(), []string{crypto.Name})
	if err != nil {
		t.Fatal(err)
	}
	wantStatusRows(t, st, []statusRow{{crypto.Name, core.StateOK, "up to date"}})
}

// signatures verifies signed and unsigned tags.
func (p *complexParts) signatures(t *testing.T) {
	c, r := p.c, p.repo
	if c.SigningKey == "" {
		t.Skip("ssh-keygen is not installed")
	}
	opts := core.VerifyOptions{Signatures: true}
	signedChecks := append(tagChecks(), core.CheckSignature)
	res, err := r.Verify(t.Context(), []string{c.Signed.Name}, opts)
	if err != nil || len(res) != 1 {
		t.Fatalf("Verify(signed) = %+v, %v", res, err)
	}
	wantChecks(t, res[0], signedChecks, nil, "")
	if got := checkDetail(t, res[0], core.CheckSignature); got != "tag v1.0.0: good signature" {
		t.Errorf("signature detail %q", got)
	}
	// Without the option, no signature is checked.
	res, err = r.Verify(t.Context(), []string{c.App.Name}, core.VerifyOptions{})
	if err != nil || len(res) != 1 {
		t.Fatalf("Verify(app) = %+v, %v", res, err)
	}
	wantChecks(t, res[0], tagChecks(), nil, "")
	res, err = r.Verify(t.Context(), []string{c.App.Name, c.Signed.Name}, opts)
	wantErr(t, "Verify(app, signed)", err, core.ErrVerify)
	if err == nil || err.Error() != "verification failed: app" || len(res) != 2 {
		t.Fatalf("Verify(app, signed) = %+v, %v", res, err)
	}
	wantChecks(t, res[0], signedChecks, []string{core.CheckSignature}, "tag v1.2.0: ")
	wantChecks(t, res[1], signedChecks, nil, "")

	// The next release of signed is not signed.
	if _, err := r.Set(t.Context(), c.Signed.Name, manifest.ModeTag, "v1.0.1"); err != nil {
		t.Fatal(err)
	}
	up := mustRepoUpdate(t, r, core.UpdateOptions{Names: []string{c.Signed.Name}, Commit: true})
	if up.Commit == "" {
		t.Fatalf("Update(signed) = %+v", up)
	}
	if got, want := headMessage(t, c.Dir), "manifest: update signed to v1.0.1\n\n"+
		"Tracking mode: tag v1.0.1\n"+
		"Old: "+c.Signed.Gitlink[:12]+" (v1.0.0)\n"+
		"New: "+c.Signed.Tags["v1.0.1"][:12]+" (v1.0.1)\n\n"+signOff; got != want {
		t.Errorf("commit message:\n%s\nwant\n%s", got, want)
	}
	res, err = r.Verify(t.Context(), []string{c.Signed.Name}, opts)
	wantErr(t, "Verify(unsigned)", err, core.ErrVerify)
	if len(res) != 1 {
		t.Fatalf("Verify(unsigned) = %+v, %v", res, err)
	}
	wantChecks(t, res[0], signedChecks, []string{core.CheckSignature}, "tag v1.0.1: ")
	if detail := checkDetail(t, res[0], core.CheckSignature); !strings.Contains(detail,
		"no signature") {
		t.Errorf("unsigned tag detail %q", detail)
	}
}

// nested moves the gitlink of the nested submodule of tools upstream and
// updates tools, which leaves the nested submodule as it is.
func (p *complexParts) nested(t *testing.T) {
	c, r := p.c, p.repo
	tools, inner := c.Tools, c.ToolsInner
	innerDir := inner.In(tools.Dir())
	work := tools.Upstream.Work
	newer := inner.Upstream.Commit(t, "inner 3")
	runGit(t, p.g, filepath.Join(work, inner.Path), "fetch", "--quiet", "origin")
	runGit(t, p.g, filepath.Join(work, inner.Path), "checkout", "--quiet", "--detach", newer)
	runGit(t, p.g, work, "commit", "--quiet", "--all", "--message=tools: move inner")
	runGit(t, p.g, work, "push", "--quiet", "origin", "HEAD:refs/heads/"+tools.Ref)
	develop := runGit(t, p.g, work, "rev-parse", "HEAD")
	worktree := treeState(t, innerDir)

	if _, err := r.Fetch(t.Context(), []string{tools.Name}, nil); err != nil {
		t.Fatal(err)
	}
	st, err := r.Status(t.Context(), []string{tools.Name})
	if err != nil {
		t.Fatal(err)
	}
	wantStatusRows(t, st, []statusRow{{tools.Name, core.StateBehind, fmt.Sprintf(
		"branch develop now resolves to %s, locked %s", develop[:12], tools.Lock.Commit[:12])}})
	target := core.Resolution{Mode: manifest.ModeBranch, Ref: tools.Ref, Commit: develop}
	res := mustRepoUpdate(t, r, core.UpdateOptions{Names: []string{tools.Name}, Commit: true})
	wantChanges(t, res.Changes, []changeView{fixtureView(tools, target, true)})
	if res.Commit == "" || headOf(t, tools.Dir()) != develop {
		t.Fatalf("Update(tools) = %+v", res)
	}

	// The nested submodule keeps its checkout; tools records another commit
	// for it now, which does not make tools dirty.
	wantSameTree(t, "update of tools", worktree, treeState(t, innerDir))
	if got := headOf(t, innerDir); got != inner.Head {
		t.Errorf("nested submodule at %s, want %s", got, inner.Head)
	}
	if got := gitlinkAt(t, tools.Dir(), "HEAD", inner.Path); got != newer {
		t.Errorf("tools records %s for the nested submodule, want %s", got, newer)
	}
	st, err = r.Status(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if st[5].Submodule.Name != tools.Name || st[5].State != core.StateOK {
		t.Errorf("tools: %+v", st[5])
	}
	if vr, err := r.Verify(t.Context(), []string{tools.Name}, core.VerifyOptions{}); err != nil {
		t.Errorf("Verify(tools) = %+v, %v", vr, err)
	}

	// Modified files in the nested submodule do make tools dirty.
	gittest.WriteFile(t, filepath.Join(innerDir, gittest.TrackedFile), "modified\n")
	st, err = r.Status(t.Context(), []string{tools.Name})
	if err != nil {
		t.Fatal(err)
	}
	wantStatusRows(t, st, []statusRow{{tools.Name, core.StateDirty,
		"working tree has uncommitted changes"}})
	_, err = r.Update(t.Context(), core.UpdateOptions{Names: []string{tools.Name}})
	wantErr(t, "Update(tools)", err, core.ErrDirty)
	runGit(t, p.g, innerDir, "checkout", "--", gittest.TrackedFile)
}

// quirky initializes the submodule whose settings keep "git submodule
// update" from checking it out and "git status" from reporting it, and
// which git clones shallow.
func (p *complexParts) quirky(t *testing.T) {
	c, r := p.c, p.repo
	q := c.Quirky
	shallow := func(what string) {
		t.Helper()
		if got := runGit(t, p.g, q.Dir(), "rev-parse", "--is-shallow-repository"); got != "true" {
			t.Errorf("%s: shallow repository %s", what, got)
		}
	}
	status := func(what string, state core.State, reason string) {
		t.Helper()
		st, err := r.Status(t.Context(), []string{q.Name})
		if err != nil {
			t.Fatalf("%s: %v", what, err)
		}
		wantStatusRows(t, st, []statusRow{{q.Name, state, reason}})
	}
	shallow("fixture")
	runGit(t, p.g, c.Dir, "submodule", "deinit", "--force", "--quiet", "--", q.Path)
	status("deinitialized", core.StateUninitialized, "submodule is not checked out")

	want := unchangedView(q)
	want.OldHead, want.Init, want.Changed = "", true, true
	res := mustRepoUpdate(t, r, core.UpdateOptions{Names: []string{q.Name}})
	wantChanges(t, res.Changes, []changeView{want})
	if got := headOf(t, q.Dir()); got != q.Gitlink {
		t.Errorf("quirky at %s, want %s", got, q.Gitlink)
	}
	shallow("offline initialization")
	status("initialized", core.StateOK, "up to date")

	// Git ignores the submodule, lazysubmodules does not.
	gittest.WriteFile(t, filepath.Join(q.Dir(), gittest.TrackedFile), "modified\n")
	if got := runGit(t, p.g, c.Dir, "status", "--porcelain", "--", q.Path); got != "" {
		t.Errorf("git status reports %q", got)
	}
	status("modified", core.StateDirty, "working tree has uncommitted changes")
	_, err := r.Update(t.Context(), core.UpdateOptions{Names: []string{q.Name}})
	wantErr(t, "Update(modified)", err, core.ErrDirty)

	// A new clone is shallow as well.
	runGit(t, p.g, c.Dir, "submodule", "deinit", "--force", "--quiet", "--", q.Path)
	if err := os.RemoveAll(q.GitDir); err != nil {
		t.Fatal(err)
	}
	_, err = r.Update(t.Context(), core.UpdateOptions{Names: []string{q.Name}})
	wantErr(t, "Update(removed)", err, core.ErrUninitialized)
	want.Clone = true
	res = mustRepoUpdate(t, r, core.UpdateOptions{Names: []string{q.Name}, Fetch: true})
	wantChanges(t, res.Changes, []changeView{want})
	shallow("clone")
	status("cloned", core.StateOK, "up to date")
	if vr, err := r.Verify(t.Context(), []string{q.Name}, core.VerifyOptions{}); err != nil {
		t.Errorf("Verify(quirky) = %+v, %v", vr, err)
	}
	wantStaged(t, c.Dir)
}
