// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package core_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FPGArtktic/lazysubmodules/internal/core"
	"github.com/FPGArtktic/lazysubmodules/internal/git/gittest"
	"github.com/FPGArtktic/lazysubmodules/internal/lock"
	"github.com/FPGArtktic/lazysubmodules/internal/manifest"
)

// Commits used by the message tests.
const (
	shaA = "a1b2c3d4e5f6a7b8c9d0a1b2c3d4e5f6a7b8c9d0"
	shaB = "e4f5a6b7c8d9e0f1a2b3c4d5e6f7a8b9c0d1e2f3"
	shaC = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
)

// tagChange returns a changed tag-pattern submodule.
func tagChange(name string) core.Change {
	return core.Change{
		Submodule: manifest.Submodule{Name: name, Path: name,
			Mode: manifest.ModeTagPattern, Ref: "v6.6.*"},
		Old:        &lock.Entry{Name: name, Mode: manifest.ModeTagPattern, Ref: "v6.6.8", Commit: shaA},
		OldHead:    shaA,
		OldGitlink: shaA,
		New:        core.Resolution{Mode: manifest.ModeTagPattern, Ref: "v6.6.9", Commit: shaB},
	}
}

// unchanged returns a submodule that is up to date.
func unchanged(name string) core.Change {
	return core.Change{
		Submodule:  manifest.Submodule{Name: name, Path: name, Mode: manifest.ModeTag, Ref: "v1"},
		Old:        &lock.Entry{Name: name, Mode: manifest.ModeTag, Ref: "v1", Commit: shaA},
		OldHead:    shaA,
		OldGitlink: shaA,
		New:        core.Resolution{Mode: manifest.ModeTag, Ref: "v1", Commit: shaA},
	}
}

func TestChanged(t *testing.T) {
	t.Parallel()
	branch := func(nativeBranch string) core.Change {
		return core.Change{
			Submodule: manifest.Submodule{Name: "b", Mode: manifest.ModeBranch, Ref: "main",
				Branch: nativeBranch},
			Old:        &lock.Entry{Name: "b", Mode: manifest.ModeBranch, Ref: "main", Commit: shaA},
			OldHead:    shaA,
			OldGitlink: shaA,
			New:        core.Resolution{Mode: manifest.ModeBranch, Ref: "main", Commit: shaA},
		}
	}
	modified := func(modify func(c *core.Change)) core.Change {
		c := unchanged("x")
		modify(&c)
		return c
	}
	// Changed, and RecordChanged: a Change built here has no other copy of
	// the recorded .gitmodules keys than its Submodule.
	for name, c := range map[string]struct {
		change            core.Change
		changed, recorded bool
	}{
		"unchanged":         {unchanged("x"), false, false},
		"branch key set":    {branch("main"), false, false},
		"branch key absent": {branch(""), true, true},
		"branch key stale":  {branch("develop"), true, true},
		"tag with branch key": {modified(func(c *core.Change) {
			c.Submodule.Branch = "main"
		}), true, true},
		"new target": {tagChange("x"), true, true},
		"no lock": {modified(func(c *core.Change) {
			c.Old = nil
		}), true, true},
		"head moved": {modified(func(c *core.Change) {
			c.OldHead = shaB
		}), true, false},
		"gitlink moved": {modified(func(c *core.Change) {
			c.OldGitlink = shaB
		}), true, true},
		"no gitlink": {modified(func(c *core.Change) {
			c.OldGitlink = ""
		}), true, true},
		"unborn head": {modified(func(c *core.Change) {
			c.OldHead = ""
		}), true, false},
		"lock mode": {modified(func(c *core.Change) {
			c.Old.Mode = manifest.ModeTagPattern
		}), true, true},
		"lock ref": {modified(func(c *core.Change) {
			c.Old.Ref = "v0"
		}), true, true},
		"lock commit": {modified(func(c *core.Change) {
			c.Old.Commit = shaB
		}), true, true},
		"lock name": {modified(func(c *core.Change) {
			c.Old.Name = "y"
		}), false, false},
		"initialized": {modified(func(c *core.Change) {
			c.Init, c.OldHead = true, ""
		}), true, false},
		"cloned": {modified(func(c *core.Change) {
			c.Init, c.Clone, c.OldHead = true, true, ""
		}), true, false},
		"unknown target": {modified(func(c *core.Change) {
			c.New = core.Resolution{}
		}), true, true},
		"unknown target of a clone": {modified(func(c *core.Change) {
			c.Old, c.OldHead, c.OldGitlink, c.New = nil, "", "", core.Resolution{}
			c.Init, c.Clone = true, true
		}), true, true},
	} {
		if got := c.change.Changed(); got != c.changed {
			t.Errorf("%s: Changed() = %v, want %v", name, got, c.changed)
		}
		if got := c.change.RecordChanged(); got != c.recorded {
			t.Errorf("%s: RecordChanged() = %v, want %v", name, got, c.recorded)
		}
	}
}

func TestCommitMessageRecordedOnly(t *testing.T) {
	t.Parallel()
	initialized := unchanged("theme")
	initialized.Init, initialized.OldHead = true, ""
	cloned := unchanged("fresh")
	cloned.Init, cloned.Clone, cloned.OldHead = true, true, ""
	checkedOut := unchanged("moved")
	checkedOut.OldHead = shaB
	single := "manifest: update kernel to v6.6.9\n\n" +
		"Tracking mode: tag-pattern v6.6.*\n" +
		"Old: a1b2c3d4e5f6 (v6.6.8)\n" +
		"New: e4f5a6b7c8d9 (v6.6.9)\n"
	for name, c := range map[string]struct {
		changes []core.Change
		want    string
	}{
		"initialized":    {[]core.Change{initialized}, ""},
		"cloned":         {[]core.Change{cloned}, ""},
		"checked out":    {[]core.Change{checkedOut}, ""},
		"all unrecorded": {[]core.Change{initialized, cloned, checkedOut}, ""},
		"with one recorded": {[]core.Change{initialized, tagChange("kernel"), cloned,
			checkedOut}, single},
		"with two recorded": {[]core.Change{initialized, tagChange("kernel"), cloned,
			tagChange("u-boot")}, "manifest: update 2 submodules\n\n" +
			"Submodule \"kernel\":\n" +
			"  Tracking mode: tag-pattern v6.6.*\n" +
			"  Old: a1b2c3d4e5f6 (v6.6.8)\n" +
			"  New: e4f5a6b7c8d9 (v6.6.9)\n\n" +
			"Submodule \"u-boot\":\n" +
			"  Tracking mode: tag-pattern v6.6.*\n" +
			"  Old: a1b2c3d4e5f6 (v6.6.8)\n" +
			"  New: e4f5a6b7c8d9 (v6.6.9)\n"},
	} {
		got := core.CommitMessage(c.changes)
		if got != c.want {
			t.Errorf("%s: CommitMessage =\n%s\nwant\n%s", name, got, c.want)
		}
		if got != "" {
			lintMessage(t, got+"\n"+signOff)
		}
	}
}

func TestCommitMessageSingle(t *testing.T) {
	t.Parallel()
	commit := func(old *lock.Entry, oldGitlink string) core.Change {
		return core.Change{
			Submodule: manifest.Submodule{Name: "crypto lib", Mode: manifest.ModeCommit,
				Ref: shaB[:10]},
			Old:        old,
			OldGitlink: oldGitlink,
			New:        core.Resolution{Mode: manifest.ModeCommit, Ref: shaB, Commit: shaB},
			Init:       true,
		}
	}
	branch := core.Change{
		Submodule:  manifest.Submodule{Name: "u-boot", Mode: manifest.ModeBranch, Ref: "main"},
		Old:        &lock.Entry{Name: "u-boot", Mode: manifest.ModeTag, Ref: "v2024.01", Commit: shaA},
		OldHead:    shaA,
		OldGitlink: shaA,
		New:        core.Resolution{Mode: manifest.ModeBranch, Ref: "main", Commit: shaB},
	}
	// The superproject records another commit than the lock entry, such as
	// an update that was staged but not committed.
	staged := tagChange("kernel")
	staged.Old.Commit = shaB
	staged.OldHead = shaB
	staged.OldGitlink = shaA
	for name, c := range map[string]struct {
		changes []core.Change
		want    string
	}{
		"tag pattern": {[]core.Change{unchanged("a"), tagChange("kernel"), unchanged("b")},
			"manifest: update kernel to v6.6.9\n\n" +
				"Tracking mode: tag-pattern v6.6.*\n" +
				"Old: a1b2c3d4e5f6 (v6.6.8)\n" +
				"New: e4f5a6b7c8d9 (v6.6.9)\n"},
		"mode changed": {[]core.Change{branch},
			"manifest: update u-boot to main\n\n" +
				"Tracking mode: branch main\n" +
				"Old: a1b2c3d4e5f6 (v2024.01)\n" +
				"New: e4f5a6b7c8d9 (main)\n"},
		"commit mode": {[]core.Change{commit(&lock.Entry{Name: "crypto lib",
			Mode: manifest.ModeCommit, Ref: shaA, Commit: shaA}, shaA)},
			"manifest: update crypto lib to e4f5a6b7c8d9\n\n" +
				"Tracking mode: commit e4f5a6b7c8\n" +
				"Old: a1b2c3d4e5f6\n" +
				"New: e4f5a6b7c8d9\n"},
		"unlocked": {[]core.Change{commit(nil, shaA)},
			"manifest: update crypto lib to e4f5a6b7c8d9\n\n" +
				"Tracking mode: commit e4f5a6b7c8\n" +
				"Old: a1b2c3d4e5f6 (unlocked)\n" +
				"New: e4f5a6b7c8d9\n"},
		"lock of another commit": {[]core.Change{staged},
			"manifest: update kernel to v6.6.9\n\n" +
				"Tracking mode: tag-pattern v6.6.*\n" +
				"Old: a1b2c3d4e5f6\n" +
				"New: e4f5a6b7c8d9 (v6.6.9)\n"},
		"no gitlink before": {[]core.Change{commit(nil, "")},
			"manifest: update crypto lib to e4f5a6b7c8d9\n\n" +
				"Tracking mode: commit e4f5a6b7c8\n" +
				"Old: none\n" +
				"New: e4f5a6b7c8d9\n"},
		"nothing changed": {[]core.Change{unchanged("a")}, ""},
		"no changes":      {nil, ""},
	} {
		if got := core.CommitMessage(c.changes); got != c.want {
			t.Errorf("%s: CommitMessage =\n%s\nwant\n%s", name, got, c.want)
		}
	}
}

func TestCommitMessageMultiple(t *testing.T) {
	t.Parallel()
	uboot := core.Change{
		Submodule:  manifest.Submodule{Name: "u-boot", Mode: manifest.ModeBranch, Ref: "main"},
		Old:        &lock.Entry{Name: "u-boot", Mode: manifest.ModeBranch, Ref: "main", Commit: shaB},
		OldHead:    shaB,
		OldGitlink: shaB,
		New:        core.Resolution{Mode: manifest.ModeBranch, Ref: "main", Commit: shaA},
	}
	fresh := core.Change{
		Submodule: manifest.Submodule{Name: `we"ird`, Mode: manifest.ModeTag, Ref: "v1.0.0"},
		New:       core.Resolution{Mode: manifest.ModeTag, Ref: "v1.0.0", Commit: shaC},
		Init:      true,
		Clone:     true,
	}
	sha256Commit := core.Change{
		Submodule:  manifest.Submodule{Name: "sdk", Mode: manifest.ModeCommit, Ref: shaC},
		Old:        &lock.Entry{Name: "sdk", Mode: manifest.ModeCommit, Ref: shaA, Commit: shaA},
		OldHead:    shaA,
		OldGitlink: shaA,
		New:        core.Resolution{Mode: manifest.ModeCommit, Ref: shaC, Commit: shaC},
	}
	got := core.CommitMessage([]core.Change{
		tagChange("kernel"), unchanged("same"), uboot, fresh, sha256Commit,
	})
	want := "manifest: update 4 submodules\n\n" +
		"Submodule \"kernel\":\n" +
		"  Tracking mode: tag-pattern v6.6.*\n" +
		"  Old: a1b2c3d4e5f6 (v6.6.8)\n" +
		"  New: e4f5a6b7c8d9 (v6.6.9)\n\n" +
		"Submodule \"u-boot\":\n" +
		"  Tracking mode: branch main\n" +
		"  Old: e4f5a6b7c8d9 (main)\n" +
		"  New: a1b2c3d4e5f6 (main)\n\n" +
		"Submodule \"we\\\"ird\":\n" +
		"  Tracking mode: tag v1.0.0\n" +
		"  Old: none\n" +
		"  New: 0123456789ab (v1.0.0)\n\n" +
		"Submodule \"sdk\":\n" +
		"  Tracking mode: commit\n" +
		"    " + shaC + "\n" +
		"  Old: a1b2c3d4e5f6\n" +
		"  New: 0123456789ab\n"
	if got != want {
		t.Errorf("CommitMessage =\n%s\nwant\n%s", got, want)
	}
}

func TestCommitMessageLongValues(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("n", 50)
	change := func(name, ref, tag string) core.Change {
		c := tagChange(name)
		c.Submodule.Ref = ref
		c.New.Ref = tag
		return c
	}
	for _, c := range []struct {
		name, ref, tag string
		subject        string
	}{
		// "manifest: update " (17) + name + " to " (4) + tag
		{strings.Repeat("k", 44), "v*", "v1.2.3.4.5", "manifest: update " +
			strings.Repeat("k", 44) + " to v1.2.3.4.5"},
		{strings.Repeat("k", 45), "v*", "v1.2.3.4.5", "manifest: update " +
			strings.Repeat("k", 45)},
		{"kernel", "v*", long, "manifest: update kernel"},
		{strings.Repeat("ż", 58), "v*", "v1", "manifest: update " +
			strings.Repeat("ż", 58)},
		{strings.Repeat("k", 59), "v*", "v1", "manifest: update 1 submodule"},
		{"lib", "v*", "v1.0,", "manifest: update lib"},
		{"lib.", "v*", "v1.0,", "manifest: update 1 submodule"},
		{"lib ", "v*", "v1", "manifest: update lib  to v1"},
		{"x", "v*", "wip-1", "manifest: update x"},
		{"x", "v*", "wipe-1", "manifest: update x to wipe-1"},
		{"Wip", "v*", "v1", "manifest: update 1 submodule"},
		{"a\tb", "v*", "v1", `manifest: update "a\tb" to v1`},
	} {
		msg := core.CommitMessage([]core.Change{change(c.name, c.ref, c.tag)})
		subject, _, _ := strings.Cut(msg, "\n")
		if subject != c.subject {
			t.Errorf("name %q, tag %q: subject %q, want %q", c.name, c.tag, subject, c.subject)
		}
		lintMessage(t, msg+"\n"+signOff)
	}

	// Long refs continue on the next line.
	c := change("kernel", "releases/"+long+"-*", "releases/"+long+"-1")
	want := "manifest: update kernel\n\n" +
		"Tracking mode: tag-pattern\n" +
		"  releases/" + long + "-*\n" +
		"Old: a1b2c3d4e5f6 (v6.6.8)\n" +
		"New: e4f5a6b7c8d9\n" +
		"  (releases/" + long + "-1)\n"
	if got := core.CommitMessage([]core.Change{c}); got != want {
		t.Errorf("CommitMessage =\n%s\nwant\n%s", got, want)
	}
	// The limit is 75 characters, not bytes; a line of exactly 75 stays.
	ref := strings.Repeat("ż", 75-len("Tracking mode: tag-pattern "))
	c = change("kernel", ref, "v6.6.9")
	if got := core.CommitMessage([]core.Change{c}); !strings.Contains(got,
		"\nTracking mode: tag-pattern "+ref+"\n") {
		t.Errorf("CommitMessage wrapped a line of 75 characters:\n%s", got)
	}
	ref += "x"
	c = change("kernel", ref, "v6.6.9")
	if got := core.CommitMessage([]core.Change{c}); !strings.Contains(got,
		"\nTracking mode: tag-pattern\n  "+ref+"\n") {
		t.Errorf("CommitMessage kept a line of 76 characters:\n%s", got)
	}
}

// TestCommitMessageGitlint runs the gitlint tool of the build image, with
// the repository configuration, on generated messages.
func TestCommitMessageGitlint(t *testing.T) {
	t.Parallel()
	gitlint, err := exec.LookPath("gitlint")
	if err != nil {
		t.Skip("gitlint is not installed")
	}
	config, err := filepath.Abs(filepath.Join("..", "..", ".gitlint"))
	if err != nil {
		t.Fatal(err)
	}
	fresh := tagChange("fresh")
	fresh.Old, fresh.OldHead, fresh.Init = nil, "", true
	initialized := unchanged("theme")
	initialized.Init, initialized.OldHead = true, ""
	for name, changes := range map[string][]core.Change{
		"single":      {tagChange("kernel")},
		"multiple":    {tagChange("kernel"), fresh, initialized, tagChange("u-boot")},
		"initialized": {initialized, tagChange("kernel")},
		"long name":   {tagChange(strings.Repeat("k", 70))},
		"wip":         {tagChange("wip")},
		"colon name":  {tagChange("a: b")},
	} {
		file := filepath.Join(t.TempDir(), "msg")
		msg := core.CommitMessage(changes) + "\n" + signOff + "\n"
		if err := os.WriteFile(file, []byte(msg), 0o600); err != nil {
			t.Fatal(err)
		}
		cmd := exec.CommandContext(t.Context(), gitlint, "--config", config,
			"--msg-filename", file)
		cmd.Dir = t.TempDir()
		cmd.Env = append(os.Environ(), gittest.Env(t)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Errorf("%s: gitlint: %v\n%s\nmessage:\n%s", name, err, out, msg)
		}
	}
}
