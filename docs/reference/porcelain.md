<!-- SPDX-License-Identifier: GPL-3.0-only -->
<!-- Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com> -->

(ref-porcelain)=

# Porcelain format v1

The stable, machine-readable output of `lazysubmodules status
--porcelain=v1`: a header line, then eight TAB-separated fields per
submodule.

(ref-porcelain-stability)=

## Stability

The porcelain v1 format is a contract for scripts. The header line, the
number, order and meaning of the fields, the state names and the quoting
rules do not change. An incompatible change will only come as a new format,
`v2`, selected with its own option value.

The other stable interfaces of LazySubmodules are the
{ref}`exit codes <ref-exit-codes>`, the `.gitmodules` keys `lsm-mode` and
`lsm-ref`, and the network policy: only `fetch`, `update --fetch` and
`add` use the network. The table of `status` and the output of the other
commands are meant for people and may change.

(ref-porcelain-option)=

## Selecting the format

| Command line | Result |
|---|---|
| `status --porcelain=v1` | The v1 format |
| `status --porcelain` | The v1 format |
| `status --porcelain v1` | `v1` is a submodule name: `lazysubmodules: v1: no such submodule`, status 1 |
| `status --porcelain=v2` | Usage error, status 2 |
| `status --porcelain=` | Usage error, status 2 |

The value must be attached with `=`. Scripts should write
`--porcelain=v1`, which names the format explicitly. Names may come before
or after the option, and `--` goes before names that start with `-`:

```sh
lazysubmodules status --porcelain=v1
lazysubmodules status --porcelain=v1 -- kernel 'crypto lib'
```

The usage error for an unsupported value reads:

```text
lazysubmodules: status: invalid value "v2" for flag -porcelain: unsupported format "v2" (only v1 is supported)
Run 'lazysubmodules help' for usage.
```

## Format

```text
# lsm-porcelain v1
<name> TAB <path> TAB <mode> TAB <ref> TAB <lock ref> TAB <lock commit> TAB <HEAD> TAB <state> LF
...
```

- **Header.** The first line is exactly `# lsm-porcelain v1`.
- **Records.** One line per submodule follows, in `.gitmodules` order.
  Without names, every submodule is listed, including unmanaged ones.
- **Fields.** Each record has exactly eight fields, separated by a single
  TAB character.
- **Line ends.** Every line, the last one included, ends with LF. There is
  no trailing TAB, because the state is never empty.
- **No decoration.** The output has no colors or other escape sequences,
  whatever the terminal, and is always valid UTF-8.
- **No submodules.** A superproject without submodules prints only the
  header line.
- **All or nothing.** The output is built completely before it is written.
  When `status` fails, it writes no partial output to standard output.
- **Exit status.** `status` exits with 0 whatever the states are.

### Fields

| # | Field | Content | Example |
|---|---|---|---|
| 1 | name | Submodule name from `.gitmodules` | `kernel` |
| 2 | path | Path from `.gitmodules` | `kernel` |
| 3 | mode | Tracking mode: `branch`, `tag`, `tag-pattern` or `commit` | `tag-pattern` |
| 4 | ref | Configured ref (`lsm-ref`) | `v6.6.*` |
| 5 | lock ref | Ref recorded in the lock file | `v6.6.9` |
| 6 | lock commit | Commit recorded in the lock file, in full | `8106f614767a75b6567271b1db80d6ade3acb7fb` |
| 7 | HEAD | Commit checked out in the submodule, in full | `8106f614767a75b6567271b1db80d6ade3acb7fb` |
| 8 | state | `ok`, `behind`, `drift`, `dirty`, `uninitialized`, `missing-ref` or `unmanaged` | `behind` |

Full commits have 40 hexadecimal digits, or 64 in SHA-256 repositories.
{ref}`ref-states` defines the states.

### Empty fields

A field without a value is empty, so two TABs follow each other:

| Situation | Empty fields |
|---|---|
| Unmanaged submodule | 3 to 6, even when a stray `lsm-ref` key or lock entry exists |
| No lock entry | 5 and 6 |
| Not checked out, or an unborn `HEAD` | 7 |
| Missing or empty `lsm-ref` | 4 |

Fields 1, 2 and 8 are never empty.

(ref-porcelain-quoting)=

## Quoting

A field is written in double quotes, with C-style escapes, when it contains
any of these characters:

- TAB, LF, CR, or any other control character (U+0000 to U+001F and U+007F
  to U+009F);
- a double quote (`"`) or a backslash (`\`);
- the line separator U+2028 or the paragraph separator U+2029;
- a byte that is not part of valid UTF-8.

Inside the quotes, these characters are escaped the way Git quotes unusual
path names (`core.quotePath`):

| Character | Escape |
|---|---|
| BEL, BS, TAB, LF, VT, FF, CR | `\a`, `\b`, `\t`, `\n`, `\v`, `\f`, `\r` |
| `"` | `\"` |
| `\` | `\\` |
| Every other character to escape | A three-digit octal escape for each of its bytes, such as `\033` for ESC, `\342\200\250` for U+2028, `\302\205` for U+0085, and `\377` for the invalid byte 0xFF |

All other characters, including spaces and non-ASCII letters, are written
unchanged, also inside quotes. A field without a character to escape is
written as it is, without quotes.

As a consequence:

- a quoted field starts with `"`, and an unquoted field never does;
- no field contains a raw TAB, LF, CR, or another character at which common
  functions split lines;
- the output is always valid UTF-8, even for names that are not.

A submodule named `we"ird`, followed by U+2028 and a TAB, is listed as:

```text
"we\"ird\342\200\250\t"	vendor/legacy					2f54e11fe5a9c2cf222f3d113338e7e53ddc3620	unmanaged
```

A submodule whose name contains an ESC sequence and the byte 0xFF:

```text
"esc\033[31mred\377"	docs/theme	tag	v1.0.0				uninitialized
```

Both lines were produced by renaming sections of the demo's `.gitmodules`.
In the second one, the lock entry still has the old name, so fields 5 and 6
are empty.

## Example

The demo superproject in its first state, with every state at least once.
Fields are separated by TAB characters.

```text
# lsm-porcelain v1
kernel	kernel	tag-pattern	v6.6.*	v6.6.9	8106f614767a75b6567271b1db80d6ade3acb7fb	8106f614767a75b6567271b1db80d6ade3acb7fb	behind
u-boot	bootloader/u-boot	branch	main	main	29b295ca56c4418418d7bc58f14454e49bc51dbb	29b295ca56c4418418d7bc58f14454e49bc51dbb	behind
fpga.core	ip/fpga-core	tag	v2.3.1	v2.3.1	556baa13f7892bcd8f0fbf14389847e0a72d8b36	556baa13f7892bcd8f0fbf14389847e0a72d8b36	ok
crypto lib	libs/crypto lib	commit	5da6927f70f5cb5408fe3a77c0aa33877a3062c5	5da6927f70f5cb5408fe3a77c0aa33877a3062c5	5da6927f70f5cb5408fe3a77c0aa33877a3062c5	5da6927f70f5cb5408fe3a77c0aa33877a3062c5	ok
legacy	vendor/legacy					2f54e11fe5a9c2cf222f3d113338e7e53ddc3620	unmanaged
tools	tools/nested	branch	develop	develop	3e8e30783478eb8f3bc000bbed9847a034b0f17a	3e8e30783478eb8f3bc000bbed9847a034b0f17a	ok
theme	docs/theme	tag	v1.0.0	v1.0.0	05f49f3f8b5a18447c1e10f18f28fc7e221d8110		uninitialized
fresh	third_party/fresh	tag-pattern	v1.*	v1.1.0	f2de3e1ec47a9de1bd0316651efbfce990587c49		uninitialized
app	apps/app	tag	v1.2.0	v1.2.0	cfa9d86f3eae90a7660a9ab94d1386902f26fe3d	cfa9d86f3eae90a7660a9ab94d1386902f26fe3d	dirty
sdk	sdk	tag-pattern	v*	v3.0.0-rc.2	63082039d4fa21e093870826e08e50c4bb701dc8	63082039d4fa21e093870826e08e50c4bb701dc8	ok
mirror-lib	libs/mirror	branch	stable	stable	71eb52c533ca5105af902ec2f6bab8f0c1d8ac52	71eb52c533ca5105af902ec2f6bab8f0c1d8ac52	ok
broken	libs/broken	tag	v9.9.9	v1.0.0	52c4bdbcb500e78dc6ae9826b4b478165afa96b9	52c4bdbcb500e78dc6ae9826b4b478165afa96b9	missing-ref
signed	libs/signed	tag	v1.0.0	v1.0.0	5d1133e0516f426db2702661540e7f92fa4b4c89	5d1133e0516f426db2702661540e7f92fa4b4c89	ok
quirky	libs/quirky	tag	v1.0.0	v1.0.0	64eca61e3a67b19004201c98582e249fda26b9f6	64eca61e3a67b19004201c98582e249fda26b9f6	ok
```

Some details of this output:

- **`legacy`** is unmanaged, so fields 3 to 6 are empty.
- **`theme` and `fresh`** are not checked out, so field 7 is empty.
- **`broken`** is configured for `v9.9.9` (field 4) but locked at `v1.0.0`
  (field 5).
- **`kernel`, `fresh` and `sdk`** show in field 5 which tag their pattern
  selected.
- **`crypto lib`** contains a space, which needs no quoting.

(ref-porcelain-parsing)=

## Parsing

A script that reads the format should:

1. **Check the header.** The first line must be exactly
   `# lsm-porcelain v1`.
2. **Split into lines** at LF. No field contains a raw LF, CR or another
   line-breaking character.
3. **Split each line** at every TAB into exactly eight fields, keeping empty
   fields.
4. **Decode quoted fields.** For a field that starts with `"`, remove the
   quotes and resolve the escapes. The result is a byte string that is not
   necessarily valid UTF-8. Use other fields as they are.
5. **Expect full SHAs** of 40 or 64 hexadecimal digits in fields 6 and 7,
   when they are not empty.

```{warning}
Do not split with a function that merges adjacent TABs. Bash `read` with
`IFS=$'\t'` treats TAB as white space and drops empty fields: for the
unmanaged `legacy`, it puts the `HEAD` commit into the third variable and
leaves the eighth empty. `awk -F '\t'`, `cut -f` and the `split` functions
of most languages keep empty fields.
```

The examples below were run in the demo superproject in its first state.
The Python and Go examples also decode the two quoted names shown above
correctly.

### POSIX shell

List every submodule that is neither `ok` nor `unmanaged`, and fail on an
unexpected header or field count:

```sh
lazysubmodules status --porcelain=v1 |
	awk -F '\t' '
		NR == 1 { if ($0 != "# lsm-porcelain v1") exit 1; next }
		NF != 8 { exit 1 }
		$8 != "ok" && $8 != "unmanaged" { print $1 ": " $8 }
	'
```

```text
kernel: behind
u-boot: behind
theme: uninitialized
fresh: uninitialized
app: dirty
broken: missing-ref
```

`awk` sees quoted fields in their quoted form, which is safe to print.

Print one field of one submodule, here the locked commit of `kernel`:

```sh
lazysubmodules status --porcelain=v1 kernel |
	awk -F '\t' 'NR == 1 && $0 != "# lsm-porcelain v1" { exit 1 } NR == 2 { print $6 }'
```

```text
8106f614767a75b6567271b1db80d6ade3acb7fb
```

Select columns with `cut`, which keeps empty fields:

```sh
lazysubmodules status --porcelain=v1 | tail -n +2 | cut -f 1,8
```

### Python

`codecs.escape_decode` resolves the escapes of a quoted field and returns
bytes; `os.fsdecode` turns them into a string that can be passed back to
`lazysubmodules` or used as a path, even when it is not valid UTF-8.

```python
import codecs
import os
import subprocess

HEADER = b"# lsm-porcelain v1"
FIELDS = ("name", "path", "mode", "ref", "lock_ref", "lock_commit", "head", "state")


def decode(field: bytes) -> str:
    """Return a field as a string; quoted fields may hold any bytes."""
    if field.startswith(b'"'):
        field = codecs.escape_decode(field[1:-1])[0]
    return os.fsdecode(field)


def read_status(*names: str) -> list[dict[str, str]]:
    """Run status --porcelain=v1 and return one dict per submodule."""
    out = subprocess.run(
        ["lazysubmodules", "status", "--porcelain=v1", "--", *names],
        check=True,
        capture_output=True,
    ).stdout
    header, *lines = out.split(b"\n")[:-1]  # every line ends with LF
    if header != HEADER:
        raise ValueError(f"unexpected header {header!r}")
    records = []
    for line in lines:
        fields = line.split(b"\t")
        if len(fields) != len(FIELDS):
            raise ValueError(f"expected {len(FIELDS)} fields in {line!r}")
        records.append(dict(zip(FIELDS, map(decode, fields))))
    return records


for sub in read_status():
    if sub["state"] not in ("ok", "unmanaged"):
        print(f"{sub['name']!r} at {sub['path']}: {sub['state']}")
```

```text
'kernel' at kernel: behind
'u-boot' at bootloader/u-boot: behind
'theme' at docs/theme: uninitialized
'fresh' at third_party/fresh: uninitialized
'app' at apps/app: dirty
'broken' at libs/broken: missing-ref
```

`decode(b'"we\\"ird\\342\\200\\250\\t"')` returns `'we"ird\u2028\t'`.

### Go

`strconv.Unquote` accepts every escape of the format:

```go
// Command lsmstatus lists the submodules that need attention.
package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// Record is one submodule of the porcelain v1 format.
type Record struct {
	Name, Path, Mode, Ref, LockRef, LockCommit, Head, State string
}

// parse reads the output of "lazysubmodules status --porcelain=v1".
func parse(out string) ([]Record, error) {
	lines := strings.Split(out, "\n")
	if lines[0] != "# lsm-porcelain v1" || lines[len(lines)-1] != "" {
		return nil, errors.New("not in the porcelain v1 format")
	}
	var recs []Record
	for _, line := range lines[1 : len(lines)-1] {
		f := strings.Split(line, "\t")
		if len(f) != 8 {
			return nil, fmt.Errorf("expected 8 fields in %q", line)
		}
		for i, v := range f {
			if strings.HasPrefix(v, `"`) {
				u, err := strconv.Unquote(v)
				if err != nil {
					return nil, fmt.Errorf("field %d of %q: %w", i+1, line, err)
				}
				f[i] = u
			}
		}
		recs = append(recs, Record{f[0], f[1], f[2], f[3], f[4], f[5], f[6], f[7]})
	}
	return recs, nil
}

func main() {
	out, err := exec.Command("lazysubmodules", "status", "--porcelain=v1").Output()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	recs, err := parse(string(out))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	for _, r := range recs {
		if r.State != "ok" && r.State != "unmanaged" {
			fmt.Printf("%q at %s: %s\n", r.Name, r.Path, r.State)
		}
	}
}
```

```text
"kernel" at kernel: behind
"u-boot" at bootloader/u-boot: behind
"theme" at docs/theme: uninitialized
"fresh" at third_party/fresh: uninitialized
"app" at apps/app: dirty
"broken" at libs/broken: missing-ref
```

With the name that contains ESC and 0xFF, the program prints
`"esc\x1b[31mred\xff" at docs/theme: uninitialized`.

## See also

- {ref}`guide-scripting`: the format in scripts and CI.
- {ref}`ref-states`: the meaning of field 8.
- [`status`](cli/status.md): the command that prints the format.
- {ref}`project-contributing`: how the stable interfaces are protected.
