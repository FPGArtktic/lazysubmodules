// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package git_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/FPGArtktic/lazysubmodules/internal/git"
	"github.com/FPGArtktic/lazysubmodules/internal/git/gittest"
)

// lazyRunner returns a Runner that fetches objects missing from a partial
// clone on demand, as git does by default, to set up partial clones.
func lazyRunner(t *testing.T) *git.Runner {
	t.Helper()
	r, err := git.New(git.WithEnv(append(gittest.Env(t), "GIT_NO_LAZY_FETCH=0")...))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// mustRun runs git with r in dir and fails the test on error.
func mustRun(t *testing.T, r *git.Runner, dir string, args ...string) {
	t.Helper()
	if _, err := r.Run(t.Context(), dir, args...); err != nil {
		t.Fatal(err)
	}
}

// missingObjects lists the objects that the partial clone at dir lacks,
// without fetching them.
func missingObjects(t *testing.T, dir string) []string {
	t.Helper()
	out := gittest.Git(t, dir, "rev-list", "--objects", "--all", "--missing=print")
	var missing []string
	for line := range strings.Lines(out) {
		if object, ok := strings.CutPrefix(strings.TrimSpace(line), "?"); ok {
			missing = append(missing, object)
		}
	}
	return missing
}

// wantGitError fails the test unless err is a *git.Error.
func wantGitError(t *testing.T, what string, err error) {
	t.Helper()
	if _, ok := errors.AsType[*git.Error](err); !ok {
		t.Fatalf("%s = %v, want *git.Error", what, err)
	}
}

// newPartialFixture returns a fixture whose submodule repository is a
// partial clone without blobs: only the blobs of the checked-out tip of
// main are present. The runners of the tests allow the file transport, so
// only the network mode of a helper keeps it from fetching the others.
func newPartialFixture(t *testing.T, format string) *fixture {
	t.Helper()
	f := newFixture(t, format)
	gittest.Git(t, f.up.Bare, "config", "uploadpack.allowFilter", "true")
	f.super.Deinit(t, f.path)
	if err := os.RemoveAll(filepath.Join(f.super.Dir, ".git", "modules", "lib")); err != nil {
		t.Fatal(err)
	}
	// Git ignores the filter when it clones a local path, but not a file URL.
	mustRun(t, lazyRunner(t), f.super.Dir,
		"-c", "url.file://"+f.up.Bare+".insteadOf="+f.up.Bare,
		"submodule", "update", "--quiet", "--init", "--filter=blob:none", "--", f.path)
	if len(missingObjects(t, f.sub)) == 0 {
		t.Fatal("the submodule repository is not a partial clone")
	}
	return f
}

func TestCheckoutPartialClone(t *testing.T) {
	t.Parallel()
	for _, format := range formats() {
		t.Run(format, func(t *testing.T) {
			t.Parallel()
			f := newPartialFixture(t, format)
			r := gittest.Runner(t)
			ctx := t.Context()
			missing := missingObjects(t, f.sub)
			tip, target := f.commits[gittest.TagV200RC], f.commits[gittest.TagRC1]

			err := r.Checkout(ctx, f.sub, target, git.Offline)
			wantGitError(t, "Checkout(Offline)", err)
			if got := missingObjects(t, f.sub); !slices.Equal(got, missing) {
				t.Errorf("missing objects after Checkout(Offline) = %q, want %q", got, missing)
			}
			if head, err := r.Head(ctx, f.sub); err != nil || head != tip {
				t.Errorf("Head after Checkout(Offline) = %q, %v; want %q", head, err, tip)
			}
			if dirty, err := r.IsDirty(ctx, f.sub); err != nil || dirty {
				t.Errorf("IsDirty after Checkout(Offline) = %v, %v; want false", dirty, err)
			}

			if err := r.Checkout(ctx, f.sub, target, git.Online); err != nil {
				t.Fatalf("Checkout(Online) = %v", err)
			}
			if head, err := r.Head(ctx, f.sub); err != nil || head != target {
				t.Errorf("Head after Checkout(Online) = %q, %v; want %q", head, err, target)
			}
			if got := missingObjects(t, f.sub); len(got) >= len(missing) {
				t.Errorf("missing objects after Checkout(Online) = %q, want fewer", got)
			}
		})
	}
}

func TestSubmoduleInitPartialClone(t *testing.T) {
	t.Parallel()
	f := newPartialFixture(t, gittest.SHA1)
	r := gittest.Runner(t)
	ctx := t.Context()
	old := f.commits[gittest.TagRC1]
	// The superproject records a commit whose blobs the repository lacks.
	gittest.Git(t, f.super.Dir, "update-index", "--cacheinfo", "160000,"+old+","+f.path)
	gittest.Git(t, f.super.Dir, "commit", "--quiet", "-m", "downgrade lib")
	f.super.Deinit(t, f.path)
	modules := filepath.Join(f.super.Dir, ".git", "modules", "lib")
	missing := missingObjects(t, modules)

	err := r.SubmoduleInit(ctx, f.super.Dir, f.path, git.Offline, nil)
	wantGitError(t, "SubmoduleInit(Offline)", err)
	if got := missingObjects(t, modules); !slices.Equal(got, missing) {
		t.Errorf("missing objects after SubmoduleInit(Offline) = %q, want %q", got, missing)
	}

	f.super.Deinit(t, f.path)
	if err := r.SubmoduleInit(ctx, f.super.Dir, f.path, git.Online, nil); err != nil {
		t.Fatalf("SubmoduleInit(Online) = %v", err)
	}
	if head, err := r.Head(ctx, f.sub); err != nil || head != old {
		t.Errorf("Head after SubmoduleInit(Online) = %q, %v; want %q", head, err, old)
	}
}

func TestReadHelpersPartialClone(t *testing.T) {
	t.Parallel()
	f := newFixture(t, gittest.SHA1)
	// The clones check out HEAD, not HEAD~1, which records another
	// .gitmodules blob.
	f.super.SetKey(t, "lib", "branch", "main")
	f.super.Commit(t, "track main")
	bare := filepath.Join(t.TempDir(), "super.git")
	gittest.Git(t, f.super.Dir, "clone", "--quiet", "--bare", "--", f.super.Dir, bare)
	gittest.Git(t, bare, "config", "uploadpack.allowFilter", "true")
	r := gittest.Runner(t)
	ctx := t.Context()

	for _, filter := range []string{"blob:none", "tree:0"} {
		clone := filepath.Join(t.TempDir(), "clone")
		mustRun(t, lazyRunner(t), t.TempDir(), "clone", "--quiet", "--filter="+filter, "--",
			"file://"+bare, clone)
		missing := missingObjects(t, clone)

		// A treeless clone has the trees of HEAD only.
		gitlink, err := r.TreeGitlink(ctx, clone, "HEAD~1", f.path)
		_, isGitErr := errors.AsType[*git.Error](err)
		if treeless := filter == "tree:0"; isGitErr != treeless || (!treeless && err != nil) {
			t.Errorf("%s: TreeGitlink(HEAD~1) = %q, %v", filter, gitlink, err)
		}
		if got := missingObjects(t, clone); !slices.Equal(got, missing) {
			t.Errorf("%s: missing objects changed from %q to %q", filter, missing, got)
		}
	}
}

// hookRunner returns a Runner whose hooks record the network variables they
// see, and a function returning the distinct records made since its last
// call.
func hookRunner(t *testing.T) (*git.Runner, func() []string) {
	t.Helper()
	dir := t.TempDir()
	logFile := filepath.Join(dir, "log")
	script := "#!/bin/sh\n" +
		`printf 'NLF=%s GAP=%s\n' "${GIT_NO_LAZY_FETCH-unset}" ` +
		`"${GIT_ALLOW_PROTOCOL-unset}" >>'` + logFile + "'\n"
	hooks := filepath.Join(dir, "hooks")
	for _, hook := range []string{"reference-transaction", "post-checkout", "pre-commit"} {
		gittest.WriteFile(t, filepath.Join(hooks, hook), script)
		if err := os.Chmod(filepath.Join(hooks, hook), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	records := func() []string {
		data, err := os.ReadFile(logFile)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err == nil {
			err = os.Remove(logFile)
		}
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(strings.TrimSpace(string(data)), "\n")
		slices.Sort(lines)
		return slices.Compact(lines)
	}
	return gittest.Runner(t, "core.hooksPath="+hooks), records
}

func TestHelpersNetworkEnvironment(t *testing.T) {
	t.Parallel()
	const (
		offline = "NLF=1 GAP="
		// The tests allow the file transport.
		online = "NLF=0 GAP=file"
		hooks  = "NLF=1 GAP=file"
	)
	f := newFixture(t, gittest.SHA1)
	other := gittest.NewUpstream(t, gittest.SHA1)
	clone := filepath.Join(t.TempDir(), "clone")
	gittest.Git(t, f.super.Dir, "clone", "--quiet", "--", f.super.Dir, clone)
	r, records := hookRunner(t)
	ctx := t.Context()
	tests := []struct {
		name string
		run  func() error
		want string
	}{
		{"Checkout(Offline)", func() error {
			return r.Checkout(ctx, f.sub, f.commits[gittest.TagV100], git.Offline)
		}, offline},
		{"Checkout(Online)", func() error {
			return r.Checkout(ctx, f.sub, f.commits[gittest.TagV101], git.Online)
		}, online},
		{"Fetch", func() error {
			f.up.Commit(t, "fetched")
			return r.Fetch(ctx, f.sub, "origin", nil)
		}, online},
		{"SubmoduleInit(Offline)", func() error {
			f.super.Deinit(t, f.path)
			return r.SubmoduleInit(ctx, f.super.Dir, f.path, git.Offline, nil)
		}, offline},
		{"SubmoduleInit(Online)", func() error {
			return r.SubmoduleInit(ctx, clone, f.path, git.Online, nil)
		}, online},
		{"SubmoduleAdd", func() error {
			return r.SubmoduleAdd(ctx, f.super.Dir, other.Bare, "other", "main", nil)
		}, online},
		{"Commit", func() error {
			return r.Commit(ctx, f.super.Dir, "core: record the hook environment\n")
		}, hooks},
	}
	for _, tt := range tests {
		if err := tt.run(); err != nil {
			t.Fatalf("%s = %v", tt.name, err)
		}
		got := records()
		if len(got) == 0 || slices.ContainsFunc(got, func(rec string) bool { return rec != tt.want }) {
			t.Errorf("%s: hooks saw %q, want only %q", tt.name, got, tt.want)
		}
	}
}
