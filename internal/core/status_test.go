// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package core_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/FPGArtktic/lazysubmodules/internal/core"
	"github.com/FPGArtktic/lazysubmodules/internal/git"
	"github.com/FPGArtktic/lazysubmodules/internal/git/gittest"
	"github.com/FPGArtktic/lazysubmodules/internal/lock"
	"github.com/FPGArtktic/lazysubmodules/internal/manifest"
)

// statusCase prepares the submodule "lib" and describes its expected state.
type statusCase struct {
	name   string
	setup  func(f *fixture)
	state  core.State
	reason string
	check  func(t *testing.T, f *fixture, st core.Status)
}

// statusCases lists one scenario per state and the precedence between the
// states.
func statusCases() []statusCase {
	tag := manifest.ModeTag
	pattern := manifest.ModeTagPattern
	return slices.Concat(okCases(), []statusCase{
		// behind
		{name: "no lock entry", state: core.StateBehind, reason: "no lock entry",
			setup: func(f *fixture) {
				f.add("lib", tag, gittest.TagV101)
				f.checkout("lib", f.commits[gittest.TagV101])
				f.super.Commit(f.t, "checkout")
			},
			check: func(t *testing.T, f *fixture, st core.Status) {
				t.Helper()
				if st.Lock != nil || st.Target == nil ||
					st.Target.Commit != f.commits[gittest.TagV101] {
					t.Errorf("lock %+v, target %+v", st.Lock, st.Target)
				}
			}},
		{name: "newer tag", state: core.StateBehind,
			reason: "select tag-pattern v1.0.1 instead of tag-pattern v1.0.0",
			setup: func(f *fixture) {
				f.track("lib", pattern, "v1.*", gittest.TagV100, f.commits[gittest.TagV100])
			}},
		{name: "branch advanced", state: core.StateBehind, reason: "branch main now resolves",
			setup: func(f *fixture) {
				f.track("lib", manifest.ModeBranch, "main", "main", f.commits[gittest.TagV200RC])
				f.up.Commit(f.t, "newer")
				f.fetchLib()
			}},
		{name: "mode changed by set", state: core.StateBehind,
			reason: "select branch stable instead of tag v1.0.1",
			setup: func(f *fixture) {
				f.track("lib", tag, gittest.TagV101, gittest.TagV101, f.commits[gittest.TagV101])
				f.configure("lib", manifest.ModeBranch, gittest.BranchStable)
			}},
		{name: "only mode changed by set", state: core.StateBehind,
			reason: "select tag v1.0.1 instead of tag-pattern v1.0.1",
			setup: func(f *fixture) {
				f.track("lib", pattern, "v1.*", gittest.TagV101, f.commits[gittest.TagV101])
				f.configure("lib", tag, gittest.TagV101)
			}},
		{name: "ref changed by set", state: core.StateBehind,
			reason: "select tag v1.0.1 instead of tag v1.0.0",
			setup: func(f *fixture) {
				f.track("lib", tag, gittest.TagV100, gittest.TagV100, f.commits[gittest.TagV100])
				f.configure("lib", tag, gittest.TagV101)
			}},
		{name: "commit changed by set", state: core.StateBehind, reason: "instead of commit",
			setup: func(f *fixture) {
				v100, v101 := f.commits[gittest.TagV100], f.commits[gittest.TagV101]
				f.track("lib", manifest.ModeCommit, v100, v100, v100)
				f.configure("lib", manifest.ModeCommit, v101)
			}},
		{name: "abbreviated commit in lock", state: core.StateBehind, reason: "instead of commit",
			setup: func(f *fixture) {
				v100 := f.commits[gittest.TagV100]
				f.track("lib", manifest.ModeCommit, v100, v100[:7], v100)
			}},
		// drift
		{name: "HEAD moved", state: core.StateDrift, reason: "differs from locked commit",
			setup: func(f *fixture) {
				f.track("lib", tag, gittest.TagV101, gittest.TagV101, f.commits[gittest.TagV101])
				f.checkout("lib", f.commits[gittest.TagV100])
			},
			check: func(t *testing.T, f *fixture, st core.Status) {
				t.Helper()
				if st.Head != f.commits[gittest.TagV100] {
					t.Errorf("head %s", st.Head)
				}
			}},
		{name: "commit on top", state: core.StateDrift, reason: "differs from locked commit",
			setup: func(f *fixture) {
				f.track("lib", tag, gittest.TagV101, gittest.TagV101, f.commits[gittest.TagV101])
				gittest.Git(f.t, f.dir("lib"), "commit", "--quiet", "--allow-empty", "-m", "local")
			}},
		{name: "tag moved on remote", state: core.StateDrift, reason: "locked tag v1.0.1 now points",
			setup: func(f *fixture) {
				f.track("lib", tag, gittest.TagV101, gittest.TagV101, f.commits[gittest.TagV101])
				f.up.MoveTag(f.t, gittest.TagV101, f.commits[gittest.TagV200RC])
				f.fetchLib()
			}},
		{name: "annotated tag moved on remote", state: core.StateDrift,
			reason: "locked tag v1.0.0 now points",
			setup: func(f *fixture) {
				f.track("lib", pattern, "v1.0.0", gittest.TagV100, f.commits[gittest.TagV100])
				f.up.MoveTag(f.t, gittest.TagV100, f.commits[gittest.TagRC1])
				f.fetchLib()
			}},
		{name: "locked tag deleted", state: core.StateDrift, reason: "v1.0.1 no longer exists",
			setup: func(f *fixture) {
				f.track("lib", pattern, "v1.*", gittest.TagV101, f.commits[gittest.TagV101])
				gittest.Git(f.t, f.dir("lib"), "tag", "--delete", gittest.TagV101)
			}},
		{name: "unborn HEAD", state: core.StateDrift, reason: "HEAD (unborn) differs",
			setup: func(f *fixture) {
				f.track("lib", tag, gittest.TagV101, gittest.TagV101, f.commits[gittest.TagV101])
				gittest.Git(f.t, f.dir("lib"), "checkout", "--quiet", "--orphan", "empty")
				gittest.Git(f.t, f.dir("lib"), "rm", "-r", "--quiet", "--cached", ".")
			},
			check: func(t *testing.T, _ *fixture, st core.Status) {
				t.Helper()
				if st.Head != "" || st.Target == nil {
					t.Errorf("head %q, target %+v", st.Head, st.Target)
				}
			}},
		{name: "drift before behind", state: core.StateDrift, reason: "differs",
			setup: func(f *fixture) {
				f.track("lib", pattern, "v1.*", gittest.TagV100, f.commits[gittest.TagV100])
				f.checkout("lib", f.commits[gittest.TagRC1])
			}},
		// dirty
		{name: "dirty", state: core.StateDirty, reason: "uncommitted changes",
			setup: func(f *fixture) {
				f.track("lib", tag, gittest.TagV101, gittest.TagV101, f.commits[gittest.TagV101])
				f.super.MakeDirty(f.t, "lib")
			}},
		{name: "staged change", state: core.StateDirty, reason: "uncommitted changes",
			setup: func(f *fixture) {
				f.track("lib", tag, gittest.TagV101, gittest.TagV101, f.commits[gittest.TagV101])
				f.super.MakeDirty(f.t, "lib")
				gittest.Git(f.t, f.dir("lib"), "add", gittest.TrackedFile)
			}},
		{name: "dirty before drift", state: core.StateDirty, reason: "uncommitted changes",
			setup: func(f *fixture) {
				f.track("lib", tag, gittest.TagV101, gittest.TagV101, f.commits[gittest.TagV101])
				f.checkout("lib", f.commits[gittest.TagV100])
				f.super.MakeDirty(f.t, "lib")
			}},
		{name: "dirty before missing-ref", state: core.StateDirty, reason: "uncommitted changes",
			setup: func(f *fixture) {
				f.track("lib", tag, gittest.TagV101, gittest.TagV101, f.commits[gittest.TagV101])
				f.configure("lib", tag, "v9")
				f.super.MakeDirty(f.t, "lib")
			}},
		// missing-ref
		{name: "missing tag", state: core.StateMissingRef, reason: "tag v9 does not exist",
			setup: func(f *fixture) {
				f.track("lib", tag, gittest.TagV101, gittest.TagV101, f.commits[gittest.TagV101])
				f.configure("lib", tag, "v9")
			},
			check: func(t *testing.T, _ *fixture, st core.Status) {
				t.Helper()
				if st.Target != nil || st.Lock == nil {
					t.Errorf("target %+v, lock %+v", st.Target, st.Lock)
				}
			}},
		{name: "locked tag deleted in tag mode", state: core.StateMissingRef,
			reason: "tag v1.0.1 does not exist",
			setup: func(f *fixture) {
				f.track("lib", tag, gittest.TagV101, gittest.TagV101, f.commits[gittest.TagV101])
				gittest.Git(f.t, f.dir("lib"), "tag", "--delete", gittest.TagV101)
			}},
		{name: "branch deleted on remote", state: core.StateMissingRef,
			reason: "remote branch origin/stable does not exist",
			setup: func(f *fixture) {
				f.track("lib", manifest.ModeBranch, gittest.BranchStable, gittest.BranchStable,
					f.commits[gittest.TagV101])
				gittest.Git(f.t, f.up.Work, "push", "--quiet", "origin", "--delete",
					gittest.BranchStable)
				f.fetchLib()
			}},
		{name: "invalid ref", state: core.StateMissingRef, reason: `invalid tag "v1..0"`,
			setup: func(f *fixture) { f.add("lib", tag, "v1..0") }},
		{name: "invalid commit", state: core.StateMissingRef, reason: "invalid commit",
			setup: func(f *fixture) { f.add("lib", manifest.ModeCommit, "HEAD") }},
		{name: "only pre-releases", state: core.StateMissingRef, reason: "pre-release",
			setup: func(f *fixture) { f.add("lib", pattern, "v2.*") }},
		{name: "missing-ref before drift", state: core.StateMissingRef, reason: "v9",
			setup: func(f *fixture) {
				f.track("lib", tag, gittest.TagV101, gittest.TagV101, f.commits[gittest.TagV101])
				f.checkout("lib", f.commits[gittest.TagV100])
				f.configure("lib", tag, "v9")
			}},
	}, uninitializedCases(), unmanagedCases())
}

// okCases lists states that are ok.
func okCases() []statusCase {
	tag := manifest.ModeTag
	return []statusCase{
		{name: "lightweight tag", state: core.StateOK, reason: "up to date",
			setup: func(f *fixture) {
				f.track("lib", tag, gittest.TagV101, gittest.TagV101, f.commits[gittest.TagV101])
			},
			check: func(t *testing.T, f *fixture, st core.Status) {
				t.Helper()
				want := core.Resolution{Mode: tag, Ref: gittest.TagV101,
					Commit: f.commits[gittest.TagV101]}
				if st.Head != want.Commit || st.Target == nil || *st.Target != want ||
					st.Lock == nil || st.Lock.Commit != want.Commit {
					t.Errorf("head %s, target %+v, lock %+v", st.Head, st.Target, st.Lock)
				}
			}},
		{name: "annotated tag", state: core.StateOK,
			setup: func(f *fixture) {
				f.track("lib", tag, gittest.TagV100, gittest.TagV100, f.commits[gittest.TagV100])
			}},
		{name: "tag pattern", state: core.StateOK,
			setup: func(f *fixture) {
				f.track("lib", manifest.ModeTagPattern, "v*", gittest.TagV101,
					f.commits[gittest.TagV101])
			}},
		{name: "locked pre-release", state: core.StateOK,
			setup: func(f *fixture) {
				f.track("lib", manifest.ModeTagPattern, "v*", gittest.TagV200RC,
					f.commits[gittest.TagV200RC])
			}},
		{name: "branch", state: core.StateOK,
			setup: func(f *fixture) {
				f.track("lib", manifest.ModeBranch, "main", "main", f.commits[gittest.TagV200RC])
			}},
		{name: "commit", state: core.StateOK,
			setup: func(f *fixture) {
				v100 := f.commits[gittest.TagV100]
				f.track("lib", manifest.ModeCommit, v100, v100, v100)
			}},
		{name: "abbreviated commit", state: core.StateOK,
			setup: func(f *fixture) {
				v100 := f.commits[gittest.TagV100]
				f.track("lib", manifest.ModeCommit, v100[:10], v100, v100)
			}},
		{name: "attached HEAD", state: core.StateOK,
			setup: func(f *fixture) {
				f.add("lib", manifest.ModeBranch, "main")
				f.lock("lib", manifest.ModeBranch, "main", f.commits[gittest.TagV200RC])
				f.super.Commit(f.t, "lock")
				if gittest.Git(f.t, f.dir("lib"), "symbolic-ref", "HEAD") != "refs/heads/main" {
					f.t.Fatal("HEAD is detached")
				}
			}},
		{name: "untracked file", state: core.StateOK,
			setup: func(f *fixture) {
				f.track("lib", tag, gittest.TagV101, gittest.TagV101, f.commits[gittest.TagV101])
				gittest.WriteFile(f.t, filepath.Join(f.dir("lib"), "untracked"), "x\n")
			}},
		{name: "tag moved but not fetched", state: core.StateOK,
			setup: func(f *fixture) {
				f.track("lib", tag, gittest.TagV101, gittest.TagV101, f.commits[gittest.TagV101])
				f.up.MoveTag(f.t, gittest.TagV101, f.commits[gittest.TagV200RC])
			}},
	}
}

// uninitializedCases lists states of submodules that are not checked out.
func uninitializedCases() []statusCase {
	tag := manifest.ModeTag
	return []statusCase{
		{name: "deinitialized", state: core.StateUninitialized, reason: "not checked out",
			setup: func(f *fixture) {
				f.track("lib", tag, gittest.TagV101, gittest.TagV101, f.commits[gittest.TagV101])
				f.super.Deinit(f.t, "lib")
			},
			check: func(t *testing.T, f *fixture, st core.Status) {
				t.Helper()
				// The target comes from the repository in .git/modules.
				if st.Head != "" || st.Target == nil ||
					st.Target.Commit != f.commits[gittest.TagV101] {
					t.Errorf("head %q, target %+v", st.Head, st.Target)
				}
			}},
		{name: "no module repository", state: core.StateUninitialized,
			setup: func(f *fixture) {
				f.track("lib", tag, gittest.TagV101, gittest.TagV101, f.commits[gittest.TagV101])
				f.super.Deinit(f.t, "lib")
				f.removeModule("lib")
			},
			check: func(t *testing.T, _ *fixture, st core.Status) {
				t.Helper()
				if st.Target != nil || st.Lock == nil {
					t.Errorf("target %+v, lock %+v", st.Target, st.Lock)
				}
			}},
		{name: "uninitialized before missing-ref", state: core.StateUninitialized,
			setup: func(f *fixture) {
				f.add("lib", tag, "v9")
				f.super.Deinit(f.t, "lib")
			}},
		{name: "uninitialized before invalid ref", state: core.StateUninitialized,
			setup: func(f *fixture) {
				f.add("lib", tag, "bad ref")
				f.super.Deinit(f.t, "lib")
			}},
		{name: "working tree removed", state: core.StateUninitialized,
			setup: func(f *fixture) {
				f.track("lib", tag, gittest.TagV101, gittest.TagV101, f.commits[gittest.TagV101])
				if err := os.RemoveAll(f.dir("lib")); err != nil {
					f.t.Fatal(err)
				}
			}},
	}
}

// unmanagedCases lists states of submodules without tracking configuration.
func unmanagedCases() []statusCase {
	return []statusCase{
		{name: "unmanaged", state: core.StateUnmanaged, reason: "no lsm-mode key",
			setup: func(f *fixture) { f.add("lib", "", "") },
			check: func(t *testing.T, f *fixture, st core.Status) {
				t.Helper()
				if st.Head != f.commits[gittest.TagV200RC] || st.Target != nil || st.Lock != nil {
					t.Errorf("head %q, target %+v, lock %+v", st.Head, st.Target, st.Lock)
				}
			}},
		{name: "unmanaged before uninitialized", state: core.StateUnmanaged,
			setup: func(f *fixture) {
				f.add("lib", "", "")
				f.super.Deinit(f.t, "lib")
			},
			check: func(t *testing.T, _ *fixture, st core.Status) {
				t.Helper()
				if st.Head != "" {
					t.Errorf("head %q", st.Head)
				}
			}},
		{name: "unmanaged with lock entry", state: core.StateUnmanaged,
			setup: func(f *fixture) {
				f.add("lib", "", "")
				f.pin("lib", manifest.ModeTag, gittest.TagV100, f.commits[gittest.TagV100])
			},
			check: func(t *testing.T, f *fixture, st core.Status) {
				t.Helper()
				if st.Lock == nil || st.Lock.Commit != f.commits[gittest.TagV100] {
					t.Errorf("lock %+v", st.Lock)
				}
			}},
	}
}

func TestStatusStates(t *testing.T) {
	t.Parallel()
	for _, format := range formats() {
		for _, c := range statusCases() {
			t.Run(format+"/"+c.name, func(t *testing.T) {
				t.Parallel()
				f := newFixture(t, format)
				c.setup(f)
				st := f.status("lib")
				wantState(t, st, c.state, c.reason)
				if st.Submodule.Name != "lib" || st.Submodule.Path != "lib" {
					t.Errorf("submodule %+v", st.Submodule)
				}
				if st.Head != "" && len(st.Head) != len(f.commits[gittest.TagV100]) {
					t.Errorf("head %q has the wrong length for %s", st.Head, format)
				}
				if c.check != nil {
					c.check(t, f, st)
				}
			})
		}
	}
}

func TestStatusSelection(t *testing.T) {
	t.Parallel()
	f := newFixture(t, gittest.SHA1)
	v101 := f.commits[gittest.TagV101]
	f.track("c", manifest.ModeTag, gittest.TagV101, gittest.TagV101, v101)
	f.add("b", "", "")
	f.track("a", manifest.ModeTag, gittest.TagV101, gittest.TagV101, v101)
	r := f.repo()
	ctx := t.Context()
	names := func(st []core.Status) []string {
		var out []string
		for _, s := range st {
			out = append(out, s.Submodule.Name+"="+string(s.State))
		}
		return out
	}
	for _, c := range []struct {
		names []string
		want  []string
	}{
		{nil, []string{"c=ok", "b=unmanaged", "a=ok"}},
		{[]string{}, []string{"c=ok", "b=unmanaged", "a=ok"}},
		{[]string{"a", "c", "a"}, []string{"c=ok", "a=ok"}},
		{[]string{"b"}, []string{"b=unmanaged"}},
	} {
		st, err := r.Status(ctx, c.names)
		if got := names(st); err != nil || !slices.Equal(got, c.want) {
			t.Errorf("Status(%q) = %q, %v; want %q", c.names, got, err, c.want)
		}
	}

	st, err := r.Status(ctx, []string{"x", "a", "y\x1b", "x"})
	wantErr(t, "Status(unknown)", err, core.ErrNotFound)
	if st != nil || err == nil ||
		err.Error() != "x: no such submodule\n\"y\\x1b\": no such submodule" {
		t.Errorf("Status(unknown) = %v, %q", st, err)
	}
}

func TestStatusWithoutSubmodules(t *testing.T) {
	t.Parallel()
	super := gittest.NewSuper(t, gittest.SHA256)
	r, err := core.Open(t.Context(), gittest.Runner(t), super.Dir)
	if err != nil {
		t.Fatal(err)
	}
	st, err := r.Status(t.Context(), nil)
	if err != nil || len(st) != 0 {
		t.Errorf("Status = %v, %v; want nothing", st, err)
	}
	_, err = r.Status(t.Context(), []string{"lib"})
	wantErr(t, "Status(lib)", err, core.ErrNotFound)
}

func TestStatusInvalidFiles(t *testing.T) {
	t.Parallel()
	f := newFixture(t, gittest.SHA1)
	f.add("lib", manifest.ModeTag, gittest.TagV101)
	r := f.repo()
	gittest.WriteFile(t, filepath.Join(f.super.Dir, lock.File),
		"[submodule \"lib\"]\n\tmode = tag\n\tref = v1\n\tcommit = abc\n")
	_, err := r.Status(t.Context(), nil)
	wantErr(t, "Status(invalid lock)", err, lock.ErrInvalidEntry)

	f.super.SetKey(t, "lib", manifest.KeyMode, "tags")
	_, err = r.Status(t.Context(), nil)
	wantErr(t, "Status(invalid mode)", err, manifest.ErrInvalidMode)
}

func TestStatusGitFailure(t *testing.T) {
	t.Parallel()
	f := newFixture(t, gittest.SHA1)
	v101 := f.commits[gittest.TagV101]
	f.track("lib", manifest.ModeTag, gittest.TagV101, gittest.TagV101, v101)
	f.track("other", manifest.ModeTag, gittest.TagV101, gittest.TagV101, v101)
	index := gittest.Git(t, f.dir("lib"), "rev-parse", "--path-format=absolute",
		"--git-path", "index")
	gittest.WriteFile(t, index, "corrupt")
	st, err := f.repo().Status(t.Context(), nil)
	gitErr, ok := errors.AsType[*git.Error](err)
	if st != nil || !ok || !strings.HasPrefix(err.Error(), "lib: ") {
		t.Errorf("Status = %v, %v; want a *git.Error for lib", st, err)
	}
	if ok && gitErr.ExitCode != 128 {
		t.Errorf("exit code %d", gitErr.ExitCode)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := f.repo().Status(ctx, nil); !errors.Is(err, context.Canceled) {
		t.Errorf("Status(canceled) = %v", err)
	}
}

func TestStatusSymlinkedPath(t *testing.T) {
	t.Parallel()
	f := newFixture(t, gittest.SHA1)
	f.track("lib", manifest.ModeTag, gittest.TagV101, gittest.TagV101,
		f.commits[gittest.TagV101])
	// A crafted .gitmodules names paths that lead through symbolic links
	// to a repository with the requested tag.
	target := t.TempDir()
	gittest.Git(t, target, "clone", "--quiet", f.up.Bare, "repo")
	for link, dest := range map[string]string{"link": filepath.Join(target, "repo"), "dir": target} {
		if err := os.Symlink(dest, filepath.Join(f.super.Dir, link)); err != nil {
			t.Fatal(err)
		}
	}
	for name, path := range map[string]string{"link": "link", "nested": "dir/repo"} {
		f.super.SetKey(t, name, manifest.KeyPath, path)
		f.super.SetKey(t, name, manifest.KeyURL, f.up.Bare)
		f.configure(name, manifest.ModeTag, gittest.TagV101)
	}
	st, err := f.repo().Status(t.Context(), []string{"link", "nested"})
	if err != nil || len(st) != 2 {
		t.Fatalf("Status = %v, %v", st, err)
	}
	for _, s := range st {
		wantState(t, s, core.StateUninitialized, "symbolic link")
		if s.Head != "" || s.Target != nil {
			t.Errorf("%s: head %q, target %+v", s.Submodule.Name, s.Head, s.Target)
		}
	}
	_, err = f.repo().Resolve(t.Context(), st[0].Submodule, nil, core.ResolveOptions{})
	wantErr(t, "Resolve(link)", err, core.ErrUninitialized)
}

func TestStatusFreshClone(t *testing.T) {
	t.Parallel()
	f := newFixture(t, gittest.SHA1)
	f.track("lib", manifest.ModeTag, gittest.TagV101, gittest.TagV101,
		f.commits[gittest.TagV101])
	clone := filepath.Join(t.TempDir(), "clone")
	gittest.Git(t, f.super.Dir, "clone", "--quiet", f.super.Dir, clone)
	r, err := core.Open(t.Context(), f.g, filepath.Join(clone, "lib"))
	if err != nil {
		t.Fatal(err)
	}
	st, err := r.Status(t.Context(), nil)
	if err != nil || len(st) != 1 {
		t.Fatalf("Status = %v, %v", st, err)
	}
	wantState(t, st[0], core.StateUninitialized, "not checked out")
	if st[0].Target != nil || st[0].Lock == nil {
		t.Errorf("target %+v, lock %+v", st[0].Target, st[0].Lock)
	}
}

func TestStatusNestedSubmodule(t *testing.T) {
	t.Parallel()
	inner := gittest.NewUpstream(t, gittest.SHA1)
	outer := gittest.NewUpstream(t, gittest.SHA1)
	release := outer.AddSubmodule(t, "inner", inner)
	outer.Tag(t, "v1.0.0", release)
	super := gittest.NewSuper(t, gittest.SHA1)
	f := &fixture{t: t, up: outer, super: super, g: gittest.Runner(t)}
	f.track("outer", manifest.ModeTag, "v1.0.0", "v1.0.0", release)
	check := func(what string, state core.State) {
		t.Helper()
		st, err := f.repo().Status(t.Context(), nil)
		if err != nil || len(st) != 1 {
			t.Fatalf("%s: Status = %v, %v; want only outer", what, st, err)
		}
		if st[0].State != state {
			t.Errorf("%s: state %s (%s), want %s", what, st[0].State, st[0].Reason, state)
		}
	}
	check("nested submodule not initialized", core.StateOK)

	nested := filepath.Join(f.dir("outer"), "inner")
	gittest.Git(t, f.dir("outer"), "submodule", "update", "--init", "--quiet")
	check("nested submodule initialized", core.StateOK)
	newer := inner.Commit(t, "newer inner")
	gittest.Git(t, nested, "fetch", "--quiet", "origin")
	gittest.Git(t, nested, "checkout", "--quiet", "--detach", newer)
	check("nested submodule at another commit", core.StateOK)
	gittest.WriteFile(t, filepath.Join(nested, gittest.TrackedFile), "changed\n")
	check("nested submodule modified", core.StateDirty)
}

func TestStatusConcurrent(t *testing.T) {
	t.Parallel()
	const n = 100
	f := newFixture(t, gittest.SHA1)
	v100, v101 := f.commits[gittest.TagV100], f.commits[gittest.TagV101]
	tip := f.commits[gittest.TagV200RC]
	// Cheaper than "git submodule add": clones that share the objects of the
	// upstream, gitlinks staged at once and files written directly.
	var gitmodules, locks, index strings.Builder
	want := make([]string, 0, n)
	for i := range n {
		name := fmt.Sprintf("lib%03d", i)
		gittest.Git(t, f.super.Dir, "clone", "--quiet", "--shared", f.up.Bare, name)
		fmt.Fprintf(&gitmodules, "[submodule %q]\n\tpath = %s\n\turl = %s\n",
			name, name, f.up.Bare)
		entry := func(mode manifest.Mode, ref, lockRef, commit string) {
			fmt.Fprintf(&gitmodules, "\t%s = %s\n\t%s = %s\n",
				manifest.KeyMode, mode, manifest.KeyRef, ref)
			fmt.Fprintf(&locks, "[submodule %q]\n\tmode = %s\n\tref = %s\n\tcommit = %s\n",
				name, mode, lockRef, commit)
		}
		head := tip
		switch i % 4 {
		case 0:
			entry(manifest.ModeTag, gittest.TagV101, gittest.TagV101, v101)
			head = v101
			want = append(want, name+"=ok")
		case 1:
			entry(manifest.ModeTagPattern, "v1.*", gittest.TagV100, v100)
			head = v100
			want = append(want, name+"=behind")
		case 2:
			entry(manifest.ModeBranch, "main", "main", v101)
			want = append(want, name+"=drift")
		default:
			want = append(want, name+"=unmanaged")
		}
		if head != tip {
			f.checkout(name, head)
		}
		fmt.Fprintf(&index, "160000 %s\t%s\n", head, name)
	}
	gittest.WriteFile(t, filepath.Join(f.super.Dir, manifest.File), gitmodules.String())
	gittest.WriteFile(t, filepath.Join(f.super.Dir, lock.File), locks.String())
	_, err := f.g.Exec(t.Context(), git.Cmd{
		Dir:   f.super.Dir,
		Args:  []string{"update-index", "--add", "--index-info"},
		Stdin: strings.NewReader(index.String()),
	})
	if err != nil {
		t.Fatal(err)
	}
	f.super.Commit(t, "add submodules")
	r := f.repo()
	for range 3 {
		st, err := r.Status(t.Context(), nil)
		if err != nil {
			t.Fatal(err)
		}
		got := make([]string, 0, len(st))
		for _, s := range st {
			got = append(got, s.Submodule.Name+"="+string(s.State))
		}
		if !slices.Equal(got, want) {
			t.Errorf("Status = %q\nwant %q", got, want)
		}
		res, err := r.Verify(t.Context(), nil, core.VerifyOptions{})
		wantErr(t, "Verify", err, core.ErrVerify)
		if len(res) != n-n/4 {
			t.Errorf("Verify returned %d results", len(res))
		}
	}
}
