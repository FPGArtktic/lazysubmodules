// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package core_test

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode"

	"github.com/FPGArtktic/lazysubmodules/internal/core"
	"github.com/FPGArtktic/lazysubmodules/internal/git/gittest"
	"github.com/FPGArtktic/lazysubmodules/internal/lock"
	"github.com/FPGArtktic/lazysubmodules/internal/manifest"
)

// subjects strips the abbreviated commit (and a "< " or "> " marker) from
// log lines.
func subjects(t *testing.T, lines []string) []string {
	t.Helper()
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		l = strings.TrimPrefix(strings.TrimPrefix(l, "< "), "> ")
		sha, subject, ok := strings.Cut(l, " ")
		if !ok || len(sha) < 7 {
			t.Fatalf("malformed log line %q", l)
		}
		out = append(out, subject)
	}
	return out
}

// markers returns the "<" or ">" marker of each lock diff line.
func markers(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		out = append(out, l[:1])
	}
	return out
}

// kernelLog lists the subjects of the kernel history up to a release.
func kernelLog(last int) []string {
	out := make([]string, 0, last+1)
	for i := last; i >= 1; i-- {
		out = append(out, "Linux v6.6."+strconv.Itoa(i))
	}
	return append(out, "initial commit")
}

// kernelTags lists the kernel tags in the order git sorts them.
func kernelTags() []string {
	tags := []string{"v6.7-rc1", "v6.6.11-rc1"}
	for i := 10; i >= 1; i-- {
		tags = append(tags, "v6.6."+strconv.Itoa(i))
	}
	return tags
}

// wantLines compares two string lists, treating nil and empty alike.
func wantLines(t *testing.T, what string, got, want []string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Errorf("%s = %q, want %q", what, got, want)
	}
}

// previewCase is the expected preview of one submodule of the complex
// superproject as it is built.
type previewCase struct {
	name    string
	log     []string
	tags    []string
	pending []string
}

// complexPreviews lists the previews of the complex superproject.
func complexPreviews() []previewCase {
	return []previewCase{
		{name: "kernel", log: kernelLog(9), tags: kernelTags(),
			pending: []string{"Linux v6.6.10"}},
		{name: "u-boot", log: []string{"U-Boot v2026.01", "U-Boot v2025.10", "initial commit"},
			pending: []string{"board: add a new board"}},
		{name: "fpga.core", log: []string{"fpga-core v2.3.1", "fpga-core v2.3.0",
			"initial commit"}, tags: []string{"v2.3.1", "v2.3.0"}},
		{name: "legacy", log: []string{"legacy 1.0", "initial commit"}},
		{name: "theme", tags: []string{"v1.0.0"}},
		{name: "fresh"},
		{name: "app", log: []string{"app v1.2.0", "app v1.1.0", "initial commit"},
			tags: []string{"v1.3.0", "v1.2.0", "v1.1.0"}},
		{name: "sdk", log: []string{"sdk v3.0.0-rc.2", "sdk v3.0.0-rc.1", "sdk v2.9.0",
			"initial commit"}, tags: []string{"v3.0.0-rc.2", "v3.0.0-rc.1", "v2.9.0"}},
		{name: "broken", log: []string{"broken v1.0.0", "initial commit"},
			tags: []string{"v1.0.0"}},
	}
}

func TestInspectComplex(t *testing.T) {
	t.Parallel()
	c := gittest.NewComplexSuper(t, gittest.SHA1)
	g := c.Runner(t)
	r := openRepo(t, g, c.Dir)
	t.Run("read", func(t *testing.T) {
		t.Run("preview", func(t *testing.T) {
			t.Parallel()
			for _, tc := range complexPreviews() {
				p, err := r.Preview(t.Context(), tc.name)
				if err != nil {
					t.Fatalf("Preview(%s): %v", tc.name, err)
				}
				wantLines(t, tc.name+" log", subjects(t, p.Log), tc.log)
				wantLines(t, tc.name+" tags", p.Tags, tc.tags)
				wantLines(t, tc.name+" pending", subjects(t, p.Pending), tc.pending)
				wantLines(t, tc.name+" lock diff", p.LockDiff, nil)
			}
		})
		t.Run("log line format", func(t *testing.T) {
			t.Parallel()
			p, err := r.Preview(t.Context(), c.Kernel.Name)
			if err != nil {
				t.Fatal(err)
			}
			head := runGit(t, g, c.Kernel.Dir(), "rev-parse", "--short", "HEAD")
			if want := head + " Linux v6.6.9"; p.Log[0] != want {
				t.Errorf("Log[0] = %q, want %q", p.Log[0], want)
			}
			next := runGit(t, g, c.Kernel.Dir(), "rev-parse", "--short", "v6.6.10^{commit}")
			if want := next + " Linux v6.6.10"; p.Pending[0] != want {
				t.Errorf("Pending[0] = %q, want %q", p.Pending[0], want)
			}
		})
		t.Run("branches", func(t *testing.T) {
			t.Parallel()
			for name, want := range map[string][]string{
				"u-boot": {"main", "next"},
				"tools":  {"develop", "main"},
				"theme":  {"main"},
				"legacy": {"main"},
			} {
				got, err := r.Branches(t.Context(), name)
				if err != nil {
					t.Fatalf("Branches(%s): %v", name, err)
				}
				wantLines(t, name+" branches", got, want)
			}
		})
		t.Run("tags", func(t *testing.T) {
			t.Parallel()
			for _, tc := range []struct {
				name, pattern string
				want          []string
			}{
				{"kernel", "", kernelTags()},
				{"kernel", "v6.6.*", kernelTags()[1:]},
				{"kernel", "v6.6.1*", []string{"v6.6.11-rc1", "v6.6.10", "v6.6.1"}},
				{"kernel", "v[5]*", nil},
				{"theme", "v1.*", []string{"v1.0.0"}},
				{"legacy", "", nil},
			} {
				got, err := r.Tags(t.Context(), tc.name, tc.pattern)
				if err != nil {
					t.Fatalf("Tags(%s, %q): %v", tc.name, tc.pattern, err)
				}
				wantLines(t, tc.name+" "+tc.pattern, got, tc.want)
			}
		})
		t.Run("errors", func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			_, err := r.Preview(ctx, "nope")
			wantErr(t, "Preview(nope)", err, core.ErrNotFound)
			_, err = r.Branches(ctx, "nope")
			wantErr(t, "Branches(nope)", err, core.ErrNotFound)
			_, err = r.Tags(ctx, "nope", "")
			wantErr(t, "Tags(nope)", err, core.ErrNotFound)
			_, err = r.GitlinkDiff(ctx, "nope")
			wantErr(t, "GitlinkDiff(nope)", err, core.ErrNotFound)
			_, err = r.Branches(ctx, c.Fresh.Name)
			wantErr(t, "Branches(fresh)", err, core.ErrUninitialized)
			_, err = r.Tags(ctx, c.Fresh.Name, "v1.*")
			wantErr(t, "Tags(fresh)", err, core.ErrUninitialized)
			for _, pattern := range []string{"-v1", "v 1", "v1\x1b", "v1..*"} {
				_, err = r.Tags(ctx, c.Kernel.Name, pattern)
				wantErr(t, "Tags(kernel, "+pattern+")", err, core.ErrInvalidArgument)
			}
		})
		t.Run("gitlink diff clean", func(t *testing.T) {
			t.Parallel()
			modified := map[string]string{
				// A nested submodule at another commit, and a modified file.
				"tools": "Submodule tools/nested contains modified content\n",
				"app":   "Submodule apps/app contains modified content\n",
			}
			for _, s := range c.Submodules() {
				diff, err := r.GitlinkDiff(t.Context(), s.Name)
				if err != nil {
					t.Fatalf("GitlinkDiff(%s): %v", s.Name, err)
				}
				if diff != modified[s.Name] {
					t.Errorf("GitlinkDiff(%s) = %q, want %q", s.Name, diff, modified[s.Name])
				}
			}
		})
	})
	t.Run("moved", func(t *testing.T) { testInspectMoved(t, c, r) })
}

// testInspectMoved checks the lock diff and the gitlink diff after
// submodules of the complex superproject were moved away from their locked
// commits.
func testInspectMoved(t *testing.T, c *gittest.ComplexSuper, r *core.Repo) {
	g := c.Runner(t)
	ctx := t.Context()
	runGit(t, g, c.UBoot.Dir(), "checkout", "--quiet", "--detach", "origin/main")
	p, err := r.Preview(ctx, c.UBoot.Name)
	if err != nil {
		t.Fatal(err)
	}
	wantLines(t, "u-boot lock diff", subjects(t, p.LockDiff), []string{"board: add a new board"})
	wantLines(t, "u-boot markers", markers(p.LockDiff), []string{">"})
	wantLines(t, "u-boot pending", p.Pending, nil)
	diff, err := r.GitlinkDiff(ctx, c.UBoot.Name)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Submodule bootloader/u-boot ", "  > board: add a new board"} {
		if !strings.Contains(diff, want) {
			t.Errorf("u-boot diff %q lacks %q", diff, want)
		}
	}

	runGit(t, g, c.Kernel.Dir(), "checkout", "--quiet", "--detach", "v6.6.8")
	p, err = r.Preview(ctx, c.Kernel.Name)
	if err != nil {
		t.Fatal(err)
	}
	wantLines(t, "kernel lock diff", subjects(t, p.LockDiff), []string{"Linux v6.6.9"})
	wantLines(t, "kernel markers", markers(p.LockDiff), []string{"<"})
	wantLines(t, "kernel pending", subjects(t, p.Pending),
		[]string{"Linux v6.6.10", "Linux v6.6.9"})
	wantLines(t, "kernel log", subjects(t, p.Log), kernelLog(8))
	diff, err = r.GitlinkDiff(ctx, c.Kernel.Name)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff, "  < Linux v6.6.9") {
		t.Errorf("kernel diff %q lacks the removed commit", diff)
	}

	// A lock entry naming a commit that the submodule lacks has no diff.
	missing := strings.Repeat("0", 39) + "1"
	e := lock.Entry{Name: c.Tools.Name, Mode: manifest.ModeBranch, Ref: "develop", Commit: missing}
	if err := lock.Write(ctx, g, c.Dir, e); err != nil {
		t.Fatal(err)
	}
	p, err = r.Preview(ctx, c.Tools.Name)
	if err != nil {
		t.Fatal(err)
	}
	wantLines(t, "tools lock diff", p.LockDiff, nil)
	wantLines(t, "tools log", subjects(t, p.Log), []string{"tools: add inner", "initial commit"})

	// The empty directory of the deinitialized theme becomes a link.
	themeDir := c.Theme.Dir()
	if err := os.Remove(themeDir); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(c.Dir, "src"), themeDir); err != nil {
		t.Fatal(err)
	}
	_, err = r.GitlinkDiff(ctx, c.Theme.Name)
	wantErr(t, "GitlinkDiff(theme through a link)", err, core.ErrSymlinkPath)
	p, err = r.Preview(ctx, c.Theme.Name)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Log)+len(p.Tags)+len(p.Pending)+len(p.LockDiff) != 0 {
		t.Errorf("preview through a link = %+v, want nothing", p)
	}
	_, err = r.Tags(ctx, c.Theme.Name, "")
	wantErr(t, "Tags(theme through a link)", err, core.ErrUninitialized)
}

func TestInspectUnprintable(t *testing.T) {
	t.Parallel()
	for _, format := range formats() {
		t.Run(format, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t, format)
			evil := "evil \x1b]0;pwned\x07\ttab subject"
			f.up.Commit(t, evil)
			f.add("lib", manifest.ModeTag, gittest.TagV100)
			r := f.repo()
			p, err := r.Preview(t.Context(), "lib")
			if err != nil {
				t.Fatal(err)
			}
			want := "evil �]0;pwned��tab subject"
			if got := subjects(t, p.Log[:1]); got[0] != want {
				t.Errorf("Log[0] subject = %q, want %q", got[0], want)
			}
			f.checkout("lib", gittest.TagV100)
			diff, err := r.GitlinkDiff(t.Context(), "lib")
			if err != nil {
				t.Fatal(err)
			}
			if strings.ContainsFunc(diff, func(c rune) bool {
				return c != '\n' && unicode.IsControl(c)
			}) || !strings.Contains(diff, "< evil �]0;pwned� tab subject") {
				t.Errorf("diff %q is not sanitized", diff)
			}
			for _, l := range append(append(p.Log, p.Tags...), p.LockDiff...) {
				if strings.ContainsFunc(l, unicode.IsControl) {
					t.Errorf("line %q contains control characters", l)
				}
			}
		})
	}
}

func TestInspectPendingWithoutTarget(t *testing.T) {
	t.Parallel()
	f := newFixture(t, gittest.SHA1)
	f.add("lib", manifest.ModeTag, "v9.9.9")
	p, err := f.repo().Preview(t.Context(), "lib")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Pending) != 0 || len(p.Log) == 0 {
		t.Errorf("preview = %+v, want a log and nothing pending", p)
	}
}
