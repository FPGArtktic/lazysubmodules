// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/FPGArtktic/lazysubmodules/internal/git/gittest"
)

// Files of the demo, relative to the package directory.
const (
	demoScript     = "../../scripts/demo.sh"
	demoTranscript = "../../examples/transcript.txt"
	exampleDir     = "../../examples"
)

// URL prefixes of the demo and of the example files that show its
// configuration.
const (
	demoHost    = "https://git.example.invalid/"
	exampleHost = "https://git.example.org/"
)

// demoRun is the outcome of one run of the demo.
type demoRun struct {
	// dir is the demo directory that --keep left behind.
	dir string
	// stdout is the standard output; transcript is the file that
	// --transcript wrote, and narration is the transcript without its
	// header: the standard output with the demo directory shown as $DEMO.
	stdout, transcript, narration string
	// signed reports whether ssh-keygen was available to sign a tag.
	signed bool
}

// TestDemo runs scripts/demo.sh with the built command, so that the demo
// keeps working. The script itself fails when a command exits with another
// status than the story expects; the test checks the key lines of the
// story, that nothing outside the demo directory was written, and that the
// files in examples/ still describe the demo.
func TestDemo(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("builds the command and runs the demo")
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}
	d := runDemo(t, bash, buildBinary(t))

	t.Run("transcript", d.checkTranscript)
	t.Run("story", d.checkStory)
	t.Run("final state", d.checkFinalState)
	t.Run("examples", d.checkExamples)
}

// runDemo runs the demo with a kept directory and a transcript, in an
// environment whose home and temporary directories must stay empty.
func runDemo(t *testing.T, bash, bin string) demoRun {
	t.Helper()
	root := t.TempDir()
	home, tmp := filepath.Join(root, "home"), filepath.Join(root, "tmp")
	for _, dir := range []string{home, tmp} {
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	d := demoRun{dir: filepath.Join(root, "demo")}
	transcript := filepath.Join(root, "transcript.txt")
	_, err := exec.LookPath("ssh-keygen")
	d.signed = err == nil

	cmd := exec.CommandContext(t.Context(), bash, demoScript, "--no-pause",
		"--binary", bin, "--keep", d.dir, "--transcript", transcript)
	// GIT_DIR checks that the demo ignores the git variables of its caller.
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "TMPDIR=" + tmp,
		"TERM=xterm", "GIT_DIR=" + root}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err = cmd.Run()
	d.stdout = stdout.String()
	if err != nil || stderr.Len() > 0 {
		t.Fatalf("demo.sh: %v\nstderr:\n%s\nend of stdout:\n%s", err, stderr.String(),
			lastLines(d.stdout, 40))
	}
	for _, dir := range []string{home, tmp} {
		if entries, err := os.ReadDir(dir); err != nil || len(entries) > 0 {
			t.Errorf("demo.sh wrote to %s: %v %v", dir, entries, err)
		}
	}
	data, err := os.ReadFile(transcript)
	if err != nil {
		t.Fatal(err)
	}
	d.transcript = string(data)
	_, d.narration, _ = strings.Cut(d.transcript, "\n\n")
	return d
}

// lastLines returns the last n lines of text.
func lastLines(text string, n int) string {
	lines := strings.SplitAfter(text, "\n")
	return strings.Join(lines[max(0, len(lines)-n):], "")
}

// skeleton returns the lines of a demo output that do not depend on the
// git version or the host: chapter titles, shown commands and exit
// statuses.
func skeleton(text string) []string {
	re := regexp.MustCompile(`^(━━ \d+\. |\$ |\(upstream [^)]*\) \$ |\[exit status )`)
	var lines []string
	for line := range strings.Lines(text) {
		if re.MatchString(line) {
			lines = append(lines, strings.TrimSuffix(line, "\n"))
		}
	}
	return lines
}

// checkTranscript compares the transcript with the standard output and the
// story with examples/transcript.txt.
func (d demoRun) checkTranscript(t *testing.T) {
	// The script shows the demo directory with symbolic links resolved.
	dir, err := filepath.EvalSymlinks(d.dir)
	if err != nil {
		t.Fatal(err)
	}
	header, _, _ := strings.Cut(d.transcript, "\n\n")
	stdout := strings.ReplaceAll(d.stdout, dir, "$DEMO")
	if d.narration != stdout ||
		!strings.HasPrefix(header, "# SPDX-License-Identifier: GPL-3.0-only\n") {
		t.Errorf("transcript is not the header and the standard output:\n%s",
			lineDiff(stdout, d.narration))
	}
	// Standard output shows the real directory, so that the commands for
	// exploring the kept demo work; only the transcript hides it.
	if !strings.Contains(d.stdout, ". "+dir+"/env.sh\n") {
		t.Errorf("standard output does not show the demo directory %s:\n%s", dir,
			lastLines(d.stdout, 10))
	}
	if strings.Contains(d.transcript, dir) || !strings.Contains(d.narration, "$DEMO/mirror") ||
		!strings.Contains(d.narration, ". $DEMO/env.sh\n") {
		t.Errorf("the transcript does not show the demo directory as $DEMO:\n%s",
			d.narration)
	}

	data, err := os.ReadFile(demoTranscript)
	if err != nil {
		t.Fatal(err)
	}
	want, got := skeleton(string(data)), skeleton(d.narration)
	if !slices.Equal(got, want) || len(got) < 50 {
		t.Errorf("%s is out of date; regenerate it with\n"+
			"  scripts/demo.sh --no-pause --transcript examples/transcript.txt\n%s",
			demoTranscript, lineDiff(strings.Join(want, "\n"), strings.Join(got, "\n")))
	}
}

// checkStory looks for the lines that prove the features the demo shows.
func (d demoRun) checkStory(t *testing.T) {
	sha7, sha12 := "[0-9a-f]{7}", "[0-9a-f]{12}"
	patterns := []string{
		// status shows every state.
		`kernel +kernel +tag-pattern +v6\.6\.\* +` + sha7 + ` \(v6\.6\.9\) .* behind`,
		`legacy +vendor/legacy +- +- +- +` + sha7 + ` +unmanaged`,
		`theme +docs/theme +tag +v1\.0\.0 +` + sha7 + ` +- +uninitialized`,
		`app +apps/app .* dirty`,
		`broken +libs/broken .* missing-ref`,
		`fpga\.core +ip/fpga-core .* drift`,
		`sdk +sdk +tag-pattern +v\* +` + sha7 + ` \(v3\.0\.0-rc\.2\) .* behind`,
		`# lsm-porcelain v1`,
		"theme\tdocs/theme\ttag\tv1\\.0\\.0\tv1\\.0\\.0\t[0-9a-f]{40}\t\tuninitialized",
		`broken: missing-ref`,
		// set and the first commit.
		`lazysubmodules: legacy: refused: submodule is not managed by lazysubmodules`,
		`legacy: tracks branch main`,
		`manifest: update legacy to main`,
		`Old: ` + sha12 + ` \(unlocked\)`,
		// Dry runs.
		`would update kernel: v6\.6\.9 \(` + sha7 + `\) -> v6\.6\.10 \(` + sha7 + `\)`,
		`would update theme: v1\.0\.0 \(` + sha7 + `\), initialize`,
		`would update kernel: v6\.6\.9 \(` + sha7 + `\) -> v6\.6\.11-rc1 \(` + sha7 + `\)`,
		`sdk: up to date`,
		// The refused update and its fixes.
		`lazysubmodules: fresh: refused: .*`,
		`lazysubmodules: app: refused: .*uncommitted.*`,
		`lazysubmodules: broken: refused: .*v9\.9\.9.*`,
		`broken: v1\.0\.0 \(` + sha7 + `\), recorded again`,
		// The commit of a selection.
		`lazysubmodules: refused: .*unrelated.*: src/drivers/README`,
		`theme: v1\.0\.0 \(` + sha7 + `\), initialized`,
		`committed [0-9a-f]{40}`,
		`    manifest: update 2 submodules`,
		`    Submodule "kernel":`,
		`      Old: ` + sha12 + ` \(v6\.6\.9\)`,
		`      New: ` + sha12 + ` \(v6\.6\.10\)`,
		`    Signed-off-by: Dana Developer <dana@example\.org>`,
		// The moved tag.
		`\(upstream fpga-core\) \$ git push --force origin v2\.3\.1`,
		`fpga\.core: fetched`,
		`  tag: tag v2\.3\.1 points to ` + sha12 + `, locked ` + sha12 + ` \(moved tag\)`,
		`lazysubmodules: verification failed: fpga\.core`,
		`fpga\.core: v2\.3\.1 \(` + sha7 + `\) -> v2\.3\.1 \(` + sha7 + `\)`,
		`sdk: v3\.0\.0-rc\.2 \(` + sha7 + `\) -> v3\.0\.0 \(` + sha7 + `\)`,
		`fpga\.core: ok \(7 checks\)`,
		// The clone through the mirror.
		`fresh: v1\.1\.0 \(` + sha7 + `\), cloned`,
		regexp.QuoteMeta(demoHost + "fresh.git"),
		regexp.QuoteMeta("file://$DEMO/mirror/fresh.git"),
		`fresh: ok \(7 checks\)`,
		// Signatures and foreach.
		`  signature: tag v1\.2\.0: .*`,
		`kernel \(tag-pattern\): Linux v6\.6\.10`,
		`crypto lib \(commit\): crypto: add sources`,
		`lazysubmodules: u-boot: git: exit status 128`,
	}
	// Without ssh-keygen, the tag of signed is not signed either.
	if d.signed {
		patterns = append(patterns, `signed: ok \(8 checks\)`,
			`lazysubmodules: verification failed: app`)
	} else {
		patterns = append(patterns, `lazysubmodules: verification failed: app, signed`)
	}
	for _, p := range patterns {
		if !regexp.MustCompile(`(?m)^` + p + `$`).MatchString(d.narration) {
			t.Errorf("no line matches %q", p)
		}
	}

	counts := map[string]int{}
	for _, line := range skeleton(d.narration) {
		if strings.HasPrefix(line, "[") {
			counts[line]++
		}
	}
	want := map[string]int{
		"[exit status 0: success]":             20,
		"[exit status 1: error]":               1,
		"[exit status 3: refused]":             5,
		"[exit status 4: verification failed]": 2,
	}
	for line, n := range want {
		if counts[line] != n {
			t.Errorf("%q appears %d times, want %d", line, counts[line], n)
		}
	}
	if len(counts) != len(want) {
		t.Errorf("exit statuses %v, want %v", counts, want)
	}
}

// checkFinalState checks that the last status table shows every submodule
// ok, and that the superproject has the commits of the story.
func (d demoRun) checkFinalState(t *testing.T) {
	_, last, ok := strings.Cut(d.narration, "12. Where we ended up")
	table, _, found := strings.Cut(last, "[exit status")
	if !ok || !found {
		t.Fatalf("no final status table:\n%s", last)
	}
	rows := 0
	for line := range strings.Lines(table) {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] == "$" || fields[0] == "NAME" {
			continue
		}
		rows++
		if fields[len(fields)-1] != "ok" {
			t.Errorf("final state: %s", line)
		}
	}
	if rows != 14 {
		t.Errorf("final status table has %d rows, want 14:\n%s", rows, table)
	}

	super := filepath.Join(d.dir, "firmware")
	got := gittest.Git(t, super, "log", "--format=%s")
	want := "manifest: update 2 submodules\n" +
		"manifest: update 2 submodules\n" +
		"manifest: update broken to v1.0.0\n" +
		"manifest: update legacy to main\n" +
		"manifest: update kernel to v6.6.9\n" +
		"track submodules with lazysubmodules\n" +
		"add theme, fresh, app, sdk, mirror-lib, broken, signed and quirky\n" +
		"add kernel, u-boot, fpga.core, crypto lib, legacy and tools\n" +
		"initial commit"
	if got != want {
		t.Errorf("history:\n%s\nwant\n%s", got, want)
	}
	for _, name := range []string{"env.sh", "mirror/fresh.git", "work/fresh"} {
		if _, err := os.Stat(filepath.Join(d.dir, name)); err != nil {
			t.Error(err)
		}
	}
}

// configList returns the entries of a git-config file, one per line.
func configList(t *testing.T, path string) string {
	t.Helper()
	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	return gittest.Git(t, t.TempDir(), "config", "--file", abs, "--list")
}

// checkExamples compares the example configuration files with the files
// the demo left behind.
func (d demoRun) checkExamples(t *testing.T) {
	for _, name := range []string{gittest.GitmodulesFile, gittest.LockFile} {
		want := configList(t, filepath.Join(d.dir, "firmware", name))
		got := configList(t, filepath.Join(exampleDir, name))
		got = strings.ReplaceAll(got, exampleHost, demoHost)
		if got != want {
			t.Errorf("examples/%s differs from the demo:\n%s", name, lineDiff(want, got))
		}
	}
}
