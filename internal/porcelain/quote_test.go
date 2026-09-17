// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package porcelain

import (
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/FPGArtktic/lazysubmodules/internal/git/gittest"
)

// quoteCase is a field and its expected quoted form.
type quoteCase struct {
	in, want string
}

// quoteCases lists fields that stay as they are and fields that are
// quoted, with every kind of escape.
func quoteCases() []quoteCase {
	return []quoteCase{
		{"", ""},
		{"kernel", "kernel"},
		{"fpga.core", "fpga.core"},
		{"libs/crypto lib", "libs/crypto lib"},
		{" lead and trail ", " lead and trail "},
		{"v6.6.*", "v6.6.*"},
		{"v[0-9]?", "v[0-9]?"},
		{"'single' `back` $dollar", "'single' `back` $dollar"},
		{"zażółć gęślą jaźń", "zażółć gęślą jaźń"},
		{"日本語", "日本語"},
		{"\u00a0nbsp", "\u00a0nbsp"},
		{"\ufffd", "\ufffd"},
		{"\u2028", `"\342\200\250"`},
		{"para\u2029graph", `"para\342\200\251graph"`},
		{"\u2027\u202a", "\u2027\u202a"},
		{"\U0001F600", "\U0001F600"},
		{"a\tb", `"a\tb"`},
		{"a\nb", `"a\nb"`},
		{"a\rb", `"a\rb"`},
		{"line\r\n", `"line\r\n"`},
		{`a"b`, `"a\"b"`},
		{`a\b`, `"a\\b"`},
		{`"`, `"\""`},
		{`\`, `"\\"`},
		{`"quoted"`, `"\"quoted\""`},
		{`\n`, `"\\n"`},
		{"\a\b\t\n\v\f\r", `"\a\b\t\n\v\f\r"`},
		{"\x00", `"\000"`},
		{"\x01\x06\x0e\x1f", `"\001\006\016\037"`},
		{"\x1b[31mred\x1b[0m", `"\033[31mred\033[0m"`},
		{"v1.0.0\x1b]0;pwned\a", `"v1.0.0\033]0;pwned\a"`},
		{"\x7f", `"\177"`},
		{"\u0080", `"\302\200"`},
		{"nel\u0085", `"nel\302\205"`},
		{"csi\u009b31m", `"csi\302\23331m"`},
		{"\u009f", `"\302\237"`},
		{"\xff", `"\377"`},
		{"\x80", `"\200"`},
		{"a\xc3", `"a\303"`},
		{"\xc3\x28", `"\303("`},
		{"\xed\xa0\x80", `"\355\240\200"`},
		{"\xf4\x90\x80\x80", `"\364\220\200\200"`},
		{"zażółć\tgęślą", "\"zażółć\\tgęślą\""},
		{"日本\n語", "\"日本\\n語\""},
		{"ok\xffzażółć", "\"ok\\377zażółć\""},
	}
}

// TestQuote checks quote with fields that need no quoting and with every
// kind of escape.
func TestQuote(t *testing.T) {
	t.Parallel()
	for _, tt := range quoteCases() {
		if got := quote(tt.in); got != tt.want {
			t.Errorf("quote(%q) = %s, want %s", tt.in, got, tt.want)
		}
		wantQuoteProperties(t, tt.in)
	}
}

// TestQuoteBytes checks the escape of every byte value between two
// ordinary characters.
func TestQuoteBytes(t *testing.T) {
	t.Parallel()
	letters := map[byte]string{
		'\a': `\a`, '\b': `\b`, '\t': `\t`, '\n': `\n`, '\v': `\v`, '\f': `\f`, '\r': `\r`,
		'"': `\"`, '\\': `\\`,
	}
	for i := range 256 {
		c := byte(i)
		in := "a" + string([]byte{c}) + "z"
		want := in
		if c < ' ' || c == 0x7f || c >= utf8.RuneSelf || c == '"' || c == '\\' {
			esc, ok := letters[c]
			if !ok {
				esc = fmt.Sprintf(`\%03o`, c)
			}
			want = `"a` + esc + `z"`
		}
		if got := quote(in); got != want {
			t.Errorf("quote(%q) = %s, want %s", in, got, want)
		}
		wantQuoteProperties(t, in)
	}
}

// TestQuoteMatchesGit compares quote with the quoting of path names by
// git. With core.quotePath=false, git escapes the same ASCII characters
// and leaves other non-ASCII characters alone; with core.quotePath=true,
// it escapes every non-ASCII byte as quote escapes C1 controls and
// invalid UTF-8.
func TestQuoteMatchesGit(t *testing.T) {
	t.Parallel()
	var ascii []string
	for c := byte(1); c < utf8.RuneSelf; c++ {
		if c != '/' {
			ascii = append(ascii, "a"+string([]byte{c})+"z")
		}
	}
	ascii = append(ascii, "plain", "with space", `"leading`, "tab\tnewline\nbackslash\\",
		"zażółć gęślą", "日本\n語", "\x1b[31mred\x1b[0m")
	wantGitQuoting(t, "false", ascii)
	highBytes := []string{"c1\u0080", "c1\u0085", "c1\u009b", "c1\u009f", "ls\u2028",
		"ps\u2029", "bad\xff",
		"bad\x80", "cut\xc3", "surrogate\xed\xa0\x80", "mixed\t\u009b\xff\"\\"}
	wantGitQuoting(t, "true", highBytes)
}

// wantGitQuoting records names in the index of a new repository and
// checks that "git ls-files" quotes them as quote does.
func wantGitQuoting(t *testing.T, quotePath string, names []string) {
	t.Helper()
	dir := gittest.InitRepo(t, gittest.SHA1)
	gittest.WriteFile(t, filepath.Join(dir, "blob"), "blob\n")
	blob := gittest.Git(t, dir, "hash-object", "-w", "blob")
	args := []string{"update-index", "--add"}
	for _, name := range names {
		args = append(args, "--cacheinfo", "100644,"+blob+","+name)
	}
	gittest.Git(t, dir, args...)
	out, err := gittest.Runner(t).Run(t.Context(), dir, "-c", "core.quotePath="+quotePath,
		"ls-files")
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	want := make([]string, 0, len(names))
	for _, name := range names {
		want = append(want, quote(name))
	}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("core.quotePath=%s: git quotes\n%q\nquote gives\n%q", quotePath, got, want)
	}
}

// FuzzQuote checks the properties of quote for arbitrary fields.
func FuzzQuote(f *testing.F) {
	for _, tt := range quoteCases() {
		f.Add(tt.in)
	}
	f.Fuzz(func(t *testing.T, in string) {
		wantQuoteProperties(t, in)
	})
}

// wantQuoteProperties checks that quote leaves a field alone exactly when
// it needs no escape, and that a quoted field is valid UTF-8 without
// control characters that strconv.Unquote decodes to the original field.
func wantQuoteProperties(t *testing.T, in string) {
	t.Helper()
	got := quote(in)
	plain := utf8.ValidString(in) && !strings.ContainsAny(in, "\"\\\u2028\u2029") &&
		!strings.ContainsFunc(in, unicode.IsControl)
	if plain {
		if got != in {
			t.Errorf("quote(%q) = %q, want it unchanged", in, got)
		}
		return
	}
	if !utf8.ValidString(got) || strings.ContainsFunc(got, unicode.IsControl) ||
		strings.ContainsAny(got, "\u2028\u2029") {
		t.Errorf("quote(%q) = %q, has control characters or invalid UTF-8", in, got)
	}
	if !strings.HasPrefix(got, `"`) || !strings.HasSuffix(got, `"`) || len(got) < 2 {
		t.Errorf("quote(%q) = %q, not in double quotes", in, got)
	}
	if u, err := strconv.Unquote(got); err != nil || u != in {
		t.Errorf("strconv.Unquote(quote(%q)) = %q, %v", in, u, err)
	}
}
