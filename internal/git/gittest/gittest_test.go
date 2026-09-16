// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package gittest_test

import (
	"errors"
	"go/parser"
	"go/token"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/FPGArtktic/lazysubmodules/internal/git"
	"github.com/FPGArtktic/lazysubmodules/internal/git/gittest"
)

const importPath = "github.com/FPGArtktic/lazysubmodules/internal/git/gittest"

func TestEnv(t *testing.T) {
	t.Parallel()
	env := gittest.Env(t, "user.useConfigOnly=true", "x.y=a=b")
	want := []string{
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_PARAMETERS=",
		"GIT_AUTHOR_NAME=" + gittest.Name,
		"GIT_AUTHOR_EMAIL=" + gittest.Email,
		"GIT_AUTHOR_DATE=" + gittest.Date,
		"GIT_COMMITTER_NAME=" + gittest.Name,
		"GIT_COMMITTER_EMAIL=" + gittest.Email,
		"GIT_COMMITTER_DATE=" + gittest.Date,
		"GIT_TERMINAL_PROMPT=0",
		"GIT_ALLOW_PROTOCOL=file",
		"GIT_TRACE=0",
		"GIT_TRACE2=0",
		"GIT_CONFIG_COUNT=7",
		"GIT_CONFIG_KEY_0=protocol.file.allow",
		"GIT_CONFIG_VALUE_0=always",
		"GIT_CONFIG_KEY_1=init.defaultBranch",
		"GIT_CONFIG_VALUE_1=main",
		"GIT_CONFIG_KEY_2=advice.detachedHead",
		"GIT_CONFIG_VALUE_2=false",
		"GIT_CONFIG_KEY_5=user.useConfigOnly",
		"GIT_CONFIG_VALUE_5=true",
		"GIT_CONFIG_KEY_6=x.y",
		"GIT_CONFIG_VALUE_6=a=b",
	}
	for _, kv := range want {
		if !slices.Contains(env, kv) {
			t.Errorf("Env lacks %q", kv)
		}
	}
	home := ""
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, "HOME="); ok {
			home = v
		}
	}
	if fi, err := os.Stat(home); err != nil || !fi.IsDir() {
		t.Errorf("HOME=%q is not a directory: %v", home, err)
	}
}

func TestEnvBlocksNetworkTransports(t *testing.T) {
	t.Parallel()
	r := gittest.Runner(t)
	// Git checks the transport before it resolves the host or starts ssh.
	for _, url := range []string{
		"https://example.invalid/x.git",
		"ssh://example.invalid/x.git",
		"git://example.invalid/x.git",
		"example.invalid:x.git",
	} {
		_, err := r.Run(t.Context(), t.TempDir(), "ls-remote", "--", url)
		gitErr, ok := errors.AsType[*git.Error](err)
		if !ok || !strings.Contains(gitErr.Stderr, "not allowed") {
			t.Errorf("ls-remote %s = %v, want the transport to be refused", url, err)
		}
	}
}

func TestEnvIsolatesConfiguration(t *testing.T) {
	t.Parallel()
	dir := gittest.InitRepo(t, gittest.SHA1)
	// Only the repository configuration and the injected entries are visible.
	out := gittest.Git(t, dir, "config", "--list", "--show-scope")
	for line := range strings.Lines(out) {
		scope, _, _ := strings.Cut(line, "\t")
		if scope != "local" && scope != "command" {
			t.Errorf("unexpected configuration %q", strings.TrimSpace(line))
		}
	}
	if got := gittest.Git(t, dir, "config", "init.defaultBranch"); got != "main" {
		t.Errorf("init.defaultBranch = %q", got)
	}
	if got := gittest.Git(t, dir, "symbolic-ref", "HEAD"); got != "refs/heads/main" {
		t.Errorf("HEAD = %q", got)
	}
}

func TestConfigArgs(t *testing.T) {
	t.Parallel()
	got := gittest.ConfigArgs("a.b=1", "c.d=2")
	want := []string{"-c", "a.b=1", "-c", "c.d=2"}
	if !slices.Equal(got, want) {
		t.Errorf("ConfigArgs = %q, want %q", got, want)
	}
	if got := gittest.ConfigArgs(); len(got) != 0 {
		t.Errorf("ConfigArgs() = %q", got)
	}
}

func TestInitRepo(t *testing.T) {
	t.Parallel()
	for _, format := range []string{gittest.SHA1, gittest.SHA256} {
		dir := gittest.InitRepo(t, format)
		if got := gittest.Git(t, dir, "rev-parse", "--show-object-format"); got != format {
			t.Errorf("object format = %q, want %q", got, format)
		}
		if got := gittest.Git(t, dir, "rev-list", "--all"); got != "" {
			t.Errorf("new repository has commits: %q", got)
		}
	}
}

func TestTaggedUpstreamIsDeterministic(t *testing.T) {
	t.Parallel()
	for _, format := range []string{gittest.SHA1, gittest.SHA256} {
		t.Run(format, func(t *testing.T) {
			t.Parallel()
			up1, commits1 := gittest.NewTaggedUpstream(t, format)
			_, commits2 := gittest.NewTaggedUpstream(t, format)
			if !maps.Equal(commits1, commits2) {
				t.Errorf("commits differ:\n%v\n%v", commits1, commits2)
			}
			hexLen := map[string]int{gittest.SHA1: 40, gittest.SHA256: 64}[format]
			for tag, sha := range commits1 {
				if len(sha) != hexLen {
					t.Errorf("%s: SHA %q has length %d", tag, sha, len(sha))
				}
			}
			tags := strings.Fields(gittest.Git(t, up1.Bare, "tag", "--list"))
			want := []string{gittest.TagRC1, gittest.TagV100, gittest.TagV101, gittest.TagV200RC}
			slices.Sort(want)
			if !slices.Equal(tags, want) {
				t.Errorf("bare tags = %q, want %q", tags, want)
			}
			checkTagTypes(t, up1.Bare)
			checkRefs(t, up1.Bare, commits1)
		})
	}
}

// checkTagTypes verifies which fixture tags are annotated.
func checkTagTypes(t *testing.T, dir string) {
	t.Helper()
	types := map[string]string{
		gittest.TagRC1:    "commit",
		gittest.TagV100:   "tag",
		gittest.TagV101:   "commit",
		gittest.TagV200RC: "tag",
	}
	for tag, want := range types {
		if got := gittest.Git(t, dir, "cat-file", "-t", "refs/tags/"+tag); got != want {
			t.Errorf("%s is a %s object, want %s", tag, got, want)
		}
	}
}

// checkRefs verifies the history described by NewTaggedUpstream.
func checkRefs(t *testing.T, dir string, commits map[string]string) {
	t.Helper()
	refs := map[string]string{
		"refs/heads/main":                     commits[gittest.TagV200RC],
		"refs/heads/" + gittest.BranchStable:  commits[gittest.TagV101],
		"refs/tags/" + gittest.TagV100 + "^0": commits[gittest.TagV100],
		gittest.TagV101 + "^":                 commits[gittest.TagV100],
		gittest.TagV100 + "^0^":               commits[gittest.TagRC1],
	}
	for ref, want := range refs {
		if got := gittest.Git(t, dir, "rev-parse", ref); got != want {
			t.Errorf("%s = %s, want %s", ref, got, want)
		}
	}
	if n := gittest.Git(t, dir, "rev-list", "--count", "main"); n != "5" {
		t.Errorf("main has %s commits, want 5", n)
	}
}

func TestMoveTag(t *testing.T) {
	t.Parallel()
	up, commits := gittest.NewTaggedUpstream(t, gittest.SHA1)
	target := commits[gittest.TagRC1]
	up.MoveTag(t, gittest.TagV100, target)
	up.MoveTag(t, gittest.TagV101, target)
	for _, tag := range []string{gittest.TagV100, gittest.TagV101} {
		if got := gittest.Git(t, up.Bare, "rev-parse", tag+"^{commit}"); got != target {
			t.Errorf("moved %s = %s, want %s", tag, got, target)
		}
	}
	checkTagTypes(t, up.Bare)
}

func TestSuper(t *testing.T) {
	t.Parallel()
	up, commits := gittest.NewTaggedUpstream(t, gittest.SHA1)
	super := gittest.NewSuper(t, gittest.SHA1)
	if n := gittest.Git(t, super.Dir, "rev-list", "--count", "HEAD"); n != "1" {
		t.Errorf("new superproject has %s commits", n)
	}
	path := super.AddSubmodule(t, "lib", up)
	if path != "lib" {
		t.Errorf("AddSubmodule path = %q", path)
	}
	gitlink := gittest.Git(t, super.Dir, "rev-parse", "HEAD:lib")
	if gitlink != commits[gittest.TagV200RC] {
		t.Errorf("gitlink = %s", gitlink)
	}
	if status := gittest.Git(t, super.Dir, "status", "--porcelain"); status != "" {
		t.Errorf("status after AddSubmodule = %q", status)
	}

	super.SetKey(t, "lib", "lsm-mode", "tag")
	got := gittest.Git(t, super.Dir, "config", "-f", ".gitmodules", "submodule.lib.lsm-mode")
	if got != "tag" {
		t.Errorf("lsm-mode = %q", got)
	}
	super.Commit(t, "set mode")
	if status := gittest.Git(t, super.Dir, "status", "--porcelain"); status != "" {
		t.Errorf("status after Commit = %q", status)
	}

	super.MakeDirty(t, path)
	sub := filepath.Join(super.Dir, path)
	status := gittest.Git(t, sub, "status", "--porcelain")
	if status != "M "+gittest.TrackedFile {
		t.Errorf("submodule status after MakeDirty = %q", status)
	}
}

func TestSuperDeinitKeepsModule(t *testing.T) {
	t.Parallel()
	up := gittest.NewUpstream(t, gittest.SHA256)
	super := gittest.NewSuper(t, gittest.SHA256)
	path := super.AddSubmodule(t, "lib", up)
	super.Deinit(t, path)
	entries, err := os.ReadDir(filepath.Join(super.Dir, path))
	if err != nil || len(entries) != 0 {
		t.Errorf("submodule directory after deinit = %v, %v; want empty", entries, err)
	}
	module := filepath.Join(super.Dir, ".git", "modules", "lib")
	if got := gittest.Git(t, module, "rev-parse", "--is-bare-repository"); got == "" {
		t.Errorf("module repository missing")
	}
}

func TestNestedUpstream(t *testing.T) {
	t.Parallel()
	inner := gittest.NewUpstream(t, gittest.SHA1)
	outer := gittest.NewUpstream(t, gittest.SHA1)
	sha := outer.AddSubmodule(t, "inner", inner)
	if got := gittest.Git(t, outer.Bare, "rev-parse", "main"); got != sha {
		t.Errorf("outer main = %s, want %s", got, sha)
	}
	super := gittest.NewSuper(t, gittest.SHA1)
	path := super.AddSubmodule(t, "outer", outer)
	gittest.Git(t, super.Dir, "submodule", "update", "--init", "--recursive")
	nested := filepath.Join(super.Dir, path, "inner", gittest.TrackedFile)
	if _, err := os.Stat(nested); err != nil {
		t.Errorf("nested submodule not checked out: %v", err)
	}
}

func TestSSHSigningKey(t *testing.T) {
	t.Parallel()
	key, ok := gittest.SSHSigningKey(t)
	if !ok {
		t.Skip("ssh-keygen not installed")
	}
	for _, suffix := range []string{"", ".pub", ".allowed_signers"} {
		if _, err := os.Stat(key + suffix); err != nil {
			t.Error(err)
		}
	}
	signers, err := os.ReadFile(key + ".allowed_signers")
	if err != nil || !strings.HasPrefix(string(signers), gittest.Email+" ssh-ed25519 ") {
		t.Errorf("allowed signers = %q, %v", signers, err)
	}
	want := []string{
		"gpg.format=ssh",
		"user.signingKey=" + key,
		"gpg.ssh.allowedSignersFile=" + key + ".allowed_signers",
	}
	if got := gittest.SSHSigningConfig(key); !slices.Equal(got, want) {
		t.Errorf("SSHSigningConfig = %q, want %q", got, want)
	}
}

// TestOnlyImportedFromTests enforces that production code never depends on
// the fixtures.
func TestOnlyImportedFromTests(t *testing.T) {
	t.Parallel()
	root := moduleRoot(t)
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if path != root && (name == "vendor" || name == "testdata" ||
				strings.HasPrefix(name, ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") ||
			filepath.Dir(path) == filepath.Join(root, "internal", "git", "gittest") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imp := range file.Imports {
			if p, _ := strconv.Unquote(imp.Path.Value); p == importPath {
				t.Errorf("%s imports %s", path, importPath)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// moduleRoot returns the directory containing go.mod.
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		_, err := os.Stat(filepath.Join(dir, "go.mod"))
		if err == nil {
			return dir
		}
		if !errors.Is(err, fs.ErrNotExist) {
			t.Fatal(err)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}
