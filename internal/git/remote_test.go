// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package git_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/FPGArtktic/lazysubmodules/internal/git"
	"github.com/FPGArtktic/lazysubmodules/internal/git/gittest"
)

func TestFetchMovedTag(t *testing.T) {
	t.Parallel()
	for _, format := range formats() {
		t.Run(format, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t, format)
			r := gittest.Runner(t)
			ctx := t.Context()
			newCommit := f.up.Commit(t, "rewritten release")
			f.up.MoveTag(t, gittest.TagV100, newCommit) // annotated
			f.up.MoveTag(t, gittest.TagV101, newCommit) // lightweight
			f.up.Tag(t, "v3.0.0", newCommit)

			// Resolution is local: nothing changes before the fetch.
			for _, tag := range []string{gittest.TagV100, gittest.TagV101} {
				got, err := r.ResolveCommit(ctx, f.sub, "refs/tags/"+tag)
				if err != nil || got != f.commits[tag] {
					t.Errorf("before fetch %s = %q, %v; want %q", tag, got, err, f.commits[tag])
				}
			}
			if err := r.Fetch(ctx, f.sub, "origin", nil); err != nil {
				t.Fatal(err)
			}
			for _, tag := range []string{gittest.TagV100, gittest.TagV101, "v3.0.0"} {
				got, err := r.ResolveCommit(ctx, f.sub, "refs/tags/"+tag)
				if err != nil || got != newCommit {
					t.Errorf("after fetch %s = %q, %v; want %q", tag, got, err, newCommit)
				}
			}
			got, err := r.ResolveCommit(ctx, f.sub, "refs/remotes/origin/main")
			if err != nil || got != newCommit {
				t.Errorf("origin/main = %q, %v; want %q", got, err, newCommit)
			}
		})
	}
}

func TestFetchPrunesBranches(t *testing.T) {
	t.Parallel()
	f := newFixture(t, gittest.SHA1)
	r := gittest.Runner(t)
	ctx := t.Context()
	gittest.Git(t, f.up.Work, "push", "--quiet", "origin", ":refs/heads/"+gittest.BranchStable)
	if err := r.Fetch(ctx, f.sub, "origin", nil); err != nil {
		t.Fatal(err)
	}
	got, err := r.ListRemoteBranches(ctx, f.sub, "origin")
	if err != nil || !slices.Equal(got, []string{"main"}) {
		t.Errorf("ListRemoteBranches = %q, %v; want [main]", got, err)
	}
}

func TestFetchProgressAndErrors(t *testing.T) {
	t.Parallel()
	f := newFixture(t, gittest.SHA1)
	r := gittest.Runner(t)
	ctx := t.Context()
	f.up.Commit(t, "more")

	var progress bytes.Buffer
	if err := r.Fetch(ctx, f.sub, "origin", &progress); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(progress.String(), "main") {
		t.Errorf("progress = %q, want the updated branch", progress.String())
	}

	err := r.Fetch(ctx, f.sub, "nope", nil)
	gitErr, ok := errors.AsType[*git.Error](err)
	if !ok || gitErr.ExitCode <= 0 || gitErr.Stderr == "" {
		t.Errorf("Fetch(nope) = %#v, want *git.Error with diagnostics", err)
	}

	progress.Reset()
	err = r.Fetch(ctx, f.sub, "nope", &progress)
	gitErr, ok = errors.AsType[*git.Error](err)
	if !ok || gitErr.Stderr != "" || !strings.Contains(progress.String(), "nope") {
		t.Errorf("Fetch(nope, progress) = %v, progress %q", err, progress.String())
	}
}

func TestSubmoduleInitOffline(t *testing.T) {
	t.Parallel()
	for _, format := range formats() {
		t.Run(format, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t, format)
			r := gittest.Runner(t)
			ctx := t.Context()
			f.super.Deinit(t, f.path)
			// Offline init must not need the remote.
			if err := os.Rename(f.up.Bare, f.up.Bare+".gone"); err != nil {
				t.Fatal(err)
			}
			if err := r.SubmoduleInit(ctx, f.super.Dir, f.path, true, nil); err != nil {
				t.Fatal(err)
			}
			root, err := r.IsWorktreeRoot(ctx, f.sub)
			if err != nil || !root {
				t.Errorf("IsWorktreeRoot after init = %v, %v", root, err)
			}
			head, err := r.Head(ctx, f.sub)
			if err != nil || head != f.commits[gittest.TagV200RC] {
				t.Errorf("Head after init = %q, %v; want %q", head, err, f.commits[gittest.TagV200RC])
			}
		})
	}
}

func TestSubmoduleInitOfflineDoesNotClone(t *testing.T) {
	t.Parallel()
	f := newFixture(t, gittest.SHA1)
	r := gittest.Runner(t)
	ctx := t.Context()
	// A fresh clone of the superproject has no submodule repositories, and
	// protocol.file.allow=always would allow cloning the local upstream.
	clone := filepath.Join(t.TempDir(), "clone")
	gittest.Git(t, f.super.Dir, "clone", "--quiet", "--", f.super.Dir, clone)
	err := r.SubmoduleInit(ctx, clone, f.path, true, nil)
	gitErr, ok := errors.AsType[*git.Error](err)
	if !ok || !strings.Contains(gitErr.Stderr, "not allowed") {
		t.Errorf("SubmoduleInit(no fetch) = %v, want *git.Error refusing the transport", err)
	}
	modules, err := r.SubmoduleGitDir(ctx, clone, "lib")
	if err != nil || exists(t, modules) {
		t.Errorf("module directory %q exists after offline init (%v)", modules, err)
	}
	if root, err := r.IsWorktreeRoot(ctx, filepath.Join(clone, f.path)); err != nil || root {
		t.Errorf("IsWorktreeRoot after offline init = %v, %v; want false", root, err)
	}
}

func TestSubmoduleInitIgnoresUpdateNone(t *testing.T) {
	t.Parallel()
	f := newFixture(t, gittest.SHA1)
	r := gittest.Runner(t)
	ctx := t.Context()
	f.super.SetKey(t, "lib", "update", "none")
	f.super.Commit(t, "do not update lib")
	f.super.Deinit(t, f.path)
	if err := r.SubmoduleInit(ctx, f.super.Dir, f.path, true, nil); err != nil {
		t.Fatal(err)
	}
	if root, err := r.IsWorktreeRoot(ctx, f.sub); err != nil || !root {
		t.Errorf("IsWorktreeRoot after init = %v, %v; want true", root, err)
	}
}

// processExited reports whether the process pid has exited. A zombie, which
// nobody reaps when the tests run as PID 1 of a container, has exited.
func processExited(pid int) bool {
	stat, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return true
	}
	fields := strings.Fields(string(stat[bytes.LastIndexByte(stat, ')')+1:]))
	return len(fields) == 0 || fields[0] == "Z"
}

// killSleep kills the process pid if it still runs sleep.
func killSleep(pid int) {
	cmdline, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "cmdline"))
	if err == nil && !processExited(pid) && bytes.HasPrefix(cmdline, []byte("sleep\x00")) {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
}

func TestSubmoduleInitCancelStopsClone(t *testing.T) {
	t.Parallel()
	f := newFixture(t, gittest.SHA1)
	clone := filepath.Join(t.TempDir(), "clone")
	gittest.Git(t, f.super.Dir, "clone", "--quiet", "--", f.super.Dir, clone)
	gittest.Git(t, clone, "config", "-f", ".gitmodules", "submodule.lib.url",
		"ssh://example.invalid/lib.git")

	// The fake ssh connects nowhere: it records its process ID and hangs.
	tmp := t.TempDir()
	pidFile := filepath.Join(tmp, "pid")
	fakeSSH := filepath.Join(tmp, "fake-ssh")
	gittest.WriteFile(t, fakeSSH, "#!/bin/sh\n"+
		`echo $$ >"$LSM_PID_FILE.tmp" && mv "$LSM_PID_FILE.tmp" "$LSM_PID_FILE"`+"\n"+
		"exec sleep 30\n")
	if err := os.Chmod(fakeSSH, 0o755); err != nil {
		t.Fatal(err)
	}
	r, err := git.New(git.WithEnv(append(gittest.Env(t), "GIT_ALLOW_PROTOCOL=ssh",
		"GIT_SSH_COMMAND="+fakeSSH, "LSM_PID_FILE="+pidFile)...))
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- r.SubmoduleInit(ctx, clone, f.path, false, nil)
	}()
	waitForFile(t, pidFile)
	data, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { killSleep(pid) })

	start := time.Now()
	cancel()
	err = <-done
	// Without signals to the whole process tree, the orphaned clone keeps
	// the output pipes open until the wait delay of 10 s has passed.
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("cancellation took %v", elapsed)
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("SubmoduleInit = %v, want context.Canceled", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for !processExited(pid) {
		if time.Now().After(deadline) {
			t.Fatalf("transport process %d still runs after cancellation", pid)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestSubmoduleInitMissingCommitOffline(t *testing.T) {
	t.Parallel()
	f := newFixture(t, gittest.SHA1)
	r := gittest.Runner(t)
	ctx := t.Context()
	// Record a commit the local submodule repository does not have yet.
	newCommit := f.up.Commit(t, "unknown locally")
	gittest.Git(t, f.super.Dir, "update-index", "--cacheinfo", "160000,"+newCommit+","+f.path)
	gittest.Git(t, f.super.Dir, "commit", "--quiet", "-m", "bump lib")
	f.super.Deinit(t, f.path)

	err := r.SubmoduleInit(ctx, f.super.Dir, f.path, true, nil)
	if _, ok := errors.AsType[*git.Error](err); !ok {
		t.Fatalf("SubmoduleInit(no fetch) = %v, want *git.Error", err)
	}
	var progress bytes.Buffer
	if err := r.SubmoduleInit(ctx, f.super.Dir, f.path, false, &progress); err != nil {
		t.Fatalf("SubmoduleInit(fetch) = %v\n%s", err, progress.String())
	}
	if head, err := r.Head(ctx, f.sub); err != nil || head != newCommit {
		t.Errorf("Head = %q, %v; want %q", head, err, newCommit)
	}
	if !strings.Contains(progress.String(), f.path) {
		t.Errorf("progress = %q, want the submodule path", progress.String())
	}
}

func TestSubmoduleInitClones(t *testing.T) {
	t.Parallel()
	f := newFixture(t, gittest.SHA1)
	r := gittest.Runner(t)
	ctx := t.Context()
	// A fresh clone of the superproject has no submodule repositories.
	clone := filepath.Join(t.TempDir(), "clone")
	gittest.Git(t, f.super.Dir, "clone", "--quiet", "--", f.super.Dir, clone)
	sub := filepath.Join(clone, f.path)
	if root, err := r.IsWorktreeRoot(ctx, sub); err != nil || root {
		t.Fatalf("IsWorktreeRoot before init = %v, %v", root, err)
	}
	modules, err := r.GitPath(ctx, clone, "modules/lib")
	if err != nil || exists(t, modules) {
		t.Fatalf("module directory %q exists before init (%v)", modules, err)
	}
	if err := r.SubmoduleInit(ctx, clone, f.path, false, nil); err != nil {
		t.Fatal(err)
	}
	if head, err := r.Head(ctx, sub); err != nil || head != f.commits[gittest.TagV200RC] {
		t.Errorf("Head = %q, %v", head, err)
	}
	if !exists(t, modules) {
		t.Errorf("module directory %q missing after init", modules)
	}
}

func TestSubmoduleAdd(t *testing.T) {
	t.Parallel()
	for _, format := range formats() {
		t.Run(format, func(t *testing.T) {
			t.Parallel()
			up, commits := gittest.NewTaggedUpstream(t, format)
			super := gittest.NewSuper(t, format)
			r := gittest.Runner(t)
			ctx := t.Context()

			var progress bytes.Buffer
			err := r.SubmoduleAdd(ctx, super.Dir, up.Bare, "libs/stable", gittest.BranchStable,
				&progress)
			if err != nil {
				t.Fatalf("SubmoduleAdd = %v\n%s", err, progress.String())
			}
			if err := r.SubmoduleAdd(ctx, super.Dir, up.Bare, "plain", "", nil); err != nil {
				t.Fatal(err)
			}

			want := []git.ConfigEntry{
				{Key: "submodule.libs/stable.path", Value: "libs/stable"},
				{Key: "submodule.libs/stable.url", Value: up.Bare},
				{Key: "submodule.libs/stable.branch", Value: gittest.BranchStable},
				{Key: "submodule.plain.path", Value: "plain"},
				{Key: "submodule.plain.url", Value: up.Bare},
			}
			got, err := r.ConfigList(ctx, super.Dir, ".gitmodules")
			if err != nil || !slices.Equal(got, want) {
				t.Errorf(".gitmodules = %q, %v; want %q", got, err, want)
			}
			heads := map[string]string{
				"libs/stable": commits[gittest.TagV101],
				"plain":       commits[gittest.TagV200RC],
			}
			for path, want := range heads {
				got, err := r.IndexGitlink(ctx, super.Dir, path)
				if err != nil || got != want {
					t.Errorf("IndexGitlink(%s) = %q, %v; want %q", path, got, err, want)
				}
			}
			staged, err := r.StagedPaths(ctx, super.Dir)
			if want := []string{".gitmodules", "libs/stable", "plain"}; err != nil ||
				!slices.Equal(staged, want) {
				t.Errorf("StagedPaths = %q, %v; want %q", staged, err, want)
			}
		})
	}
}

func TestSubmoduleAddFailure(t *testing.T) {
	t.Parallel()
	super := gittest.NewSuper(t, gittest.SHA1)
	r := gittest.Runner(t)
	missing := filepath.Join(t.TempDir(), "missing.git")
	err := r.SubmoduleAdd(t.Context(), super.Dir, missing, "lib", "", nil)
	gitErr, ok := errors.AsType[*git.Error](err)
	if !ok || gitErr.Stderr == "" {
		t.Errorf("SubmoduleAdd(missing) = %v, want *git.Error with diagnostics", err)
	}
	if exists(t, filepath.Join(super.Dir, ".gitmodules")) {
		t.Errorf(".gitmodules created by a failed add")
	}
}

func TestSubmoduleAddLiteralPath(t *testing.T) {
	t.Parallel()
	up := gittest.NewUpstream(t, gittest.SHA1)
	super := gittest.NewSuper(t, gittest.SHA1)
	r := gittest.Runner(t)
	// As a pathspec, "lib*" would match the tracked file "libx".
	gittest.WriteFile(t, filepath.Join(super.Dir, "libx"), "x\n")
	super.Commit(t, "add libx")
	if err := r.SubmoduleAdd(t.Context(), super.Dir, up.Bare, "lib*", "", nil); err != nil {
		t.Fatal(err)
	}
	want := gittest.Git(t, up.Work, "rev-parse", "HEAD")
	if got, err := r.IndexGitlink(t.Context(), super.Dir, "lib*"); err != nil || got != want {
		t.Errorf("IndexGitlink(lib*) = %q, %v; want %q", got, err, want)
	}
	if got := readFile(t, filepath.Join(super.Dir, "libx")); got != "x\n" {
		t.Errorf("libx = %q", got)
	}
}

func TestNestedSubmodule(t *testing.T) {
	t.Parallel()
	inner := gittest.NewUpstream(t, gittest.SHA1)
	outer := gittest.NewUpstream(t, gittest.SHA1)
	outer.AddSubmodule(t, "inner", inner)
	super := gittest.NewSuper(t, gittest.SHA1)
	path := super.AddSubmodule(t, "outer", outer)
	r := gittest.Runner(t)
	ctx := t.Context()
	outerDir := filepath.Join(super.Dir, path)

	entries, err := r.ConfigList(ctx, outerDir, ".gitmodules")
	if err != nil || len(entries) != 2 || entries[0].Value != "inner" {
		t.Fatalf("nested .gitmodules = %q, %v", entries, err)
	}
	innerDir := filepath.Join(outerDir, "inner")
	if root, err := r.IsWorktreeRoot(ctx, innerDir); err != nil || root {
		t.Errorf("nested submodule populated without recursion: %v, %v", root, err)
	}
	if err := r.SubmoduleInit(ctx, outerDir, "inner", false, nil); err != nil {
		t.Fatal(err)
	}
	if root, err := r.IsWorktreeRoot(ctx, innerDir); err != nil || !root {
		t.Errorf("IsWorktreeRoot(nested) = %v, %v; want true", root, err)
	}
	// Changes inside the nested submodule make both levels dirty.
	gittest.WriteFile(t, filepath.Join(innerDir, gittest.TrackedFile), "changed\n")
	for _, dir := range []string{innerDir, outerDir} {
		if dirty, err := r.IsDirty(ctx, dir); err != nil || !dirty {
			t.Errorf("IsDirty(%s) = %v, %v; want true", dir, dirty, err)
		}
	}
}
