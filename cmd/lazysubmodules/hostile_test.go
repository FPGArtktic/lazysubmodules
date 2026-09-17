// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FPGArtktic/lazysubmodules/internal/git/gittest"
)

// TestHostileSuperproject runs the commands on a superproject whose
// .gitmodules and lock file hold hostile values: the output never contains
// control characters, the exit statuses follow the refusals, and nothing
// is written outside the superproject.
func TestHostileSuperproject(t *testing.T) {
	t.Parallel()
	h := gittest.NewHostileSuper(t, gittest.SHA1)
	inv := invocation{dir: h.Dir, gitEnv: gittest.Env(t)}
	check := func(t *testing.T, code int, args ...string) result {
		t.Helper()
		res := inv.wantCode(t, code, args...)
		wantPrintable(t, strings.Join(args, " ")+" (stdout)", res.stdout)
		wantPrintable(t, strings.Join(args, " ")+" (stderr)", res.stderr)
		for line := range strings.Lines(res.stderr) {
			if res.code != exitOK && !strings.HasPrefix(line, "lazysubmodules: ") {
				t.Errorf("%q: stderr line %q", args, line)
			}
		}
		return res
	}

	t.Run("invalid lock entries", func(t *testing.T) {
		for _, args := range [][]string{{"status"}, {"update"}, {"verify"}} {
			res := check(t, exitError, args...)
			if !strings.Contains(res.stderr, `.lsm.lock: invalid lock entry "dash-ref"`) {
				t.Errorf("%q:\n%v", args, res)
			}
		}
		// Fetching does not read the lock file.
		check(t, exitRefused, "fetch")
	})
	for _, s := range []*gittest.Submodule{h.DashRef, h.CtrlRef} {
		gittest.Git(t, h.Dir, "config", "-f", gittest.LockFile, "--remove-section",
			"submodule."+s.Name)
	}

	t.Run("commands", func(t *testing.T) {
		res := check(t, exitOK, "status")
		if !strings.Contains(res.stdout, "\n"+`"we\"ird\\name"  weird  `) {
			t.Errorf("status:\n%v", res)
		}
		check(t, exitOK, "status", "--porcelain")
		res = check(t, exitRefused, "update")
		for _, want := range []string{"via-link: refused", "dash-url: refused",
			`ctrl-ref: refused: bad ref in .gitmodules: invalid tag "v1.0.0\x1b`} {
			if !strings.Contains(res.stderr, want) {
				t.Errorf("update: no %q in\n%v", want, res)
			}
		}
		check(t, exitRefused, "update", "--dry-run", "--fetch")
		check(t, exitRefused, "fetch")
		check(t, exitVerify, "verify")
		// The removed lock entries are unrelated to the commit.
		res = check(t, exitRefused, "update", "good", "--commit")
		if !strings.Contains(res.stderr, "unrelated changes: .lsm.lock") {
			t.Errorf("update --commit:\n%v", res)
		}
		check(t, exitOK, "update", "good")
		check(t, exitOK, "foreach", "true")
		check(t, exitOK, "set", `we"ird\name`, "--tag", "v1.0.0")
		check(t, exitRefused, "update", "--", "dash-url")
	})

	// Bytes that are not UTF-8 are quoted: the 8-bit CSI 0x9b would clear
	// the screen of a terminal that honors C1 controls.
	t.Run("8-bit control", func(t *testing.T) {
		const name, path = "csi\x9b2Jname", "csi\x9b2Jpath"
		for key, value := range map[string]string{"path": path, "url": "../lib",
			"lsm-mode": "tag", "lsm-ref": "v1.0.0"} {
			gittest.Git(t, h.Dir, "config", "-f", ".gitmodules", "submodule."+name+"."+key, value)
		}
		res := check(t, exitOK, "status")
		if !strings.Contains(res.stdout, `"csi\x9b2Jname"`) {
			t.Errorf("status:\n%v", res)
		}
		res = check(t, exitVerify, "verify", name)
		if !strings.Contains(res.stdout, `"csi\x9b2Jname": failed`) {
			t.Errorf("verify:\n%v", res)
		}
		check(t, exitOK, "foreach", "true")
		res = check(t, exitOK, "set", name, "--tag", "v1.0.0")
		if res.stdout != `"csi\x9b2Jname": tracks tag v1.0.0`+"\n" {
			t.Errorf("set:\n%v", res)
		}
		check(t, exitRefused, "update", name)
		check(t, exitOK, "status", "--porcelain=v1")
		gittest.Git(t, h.Dir, "config", "-f", ".gitmodules", "--remove-section",
			"submodule."+name)
	})

	t.Run("symbolic link", func(t *testing.T) {
		h.LinkLock(t)
		for _, args := range [][]string{{"status"}, {"update", "good"}, {"verify"}} {
			res := check(t, exitError, args...)
			if !strings.Contains(res.stderr, "not a regular file") {
				t.Errorf("%q:\n%v", args, res)
			}
		}
	})

	entries, err := os.ReadDir(h.Outside)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "pwned") {
			t.Errorf("a command wrote %s", filepath.Join(h.Outside, e.Name()))
		}
	}
}
