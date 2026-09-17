<!-- SPDX-License-Identifier: GPL-3.0-only -->
<!-- Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com> -->

(guide-scripting)=

# Scripts and CI

Automate checks with the porcelain format, the exit codes and `verify`.

Three interfaces are stable across releases and meant for scripts:

- the output of `status --porcelain=v1`;
- the exit codes 0 to 5;
- the `.gitmodules` keys `lsm-mode` and `lsm-ref`.

The human-readable output of the other commands may change; do not parse
it.

## Read the porcelain format

`status --porcelain=v1` prints a header line, then one record per
submodule with eight TAB-separated fields:

```{literalinclude} ../../examples/transcript.txt
:language: console
:start-at: "$ lazysubmodules status --porcelain=v1 kernel legacy theme"
:end-at: "[exit status 0: success]"
```

The fields are name, path, mode, configured ref, locked ref, locked
commit, HEAD commit and state. Fields without a value are empty, such as
fields 3 to 6 of the unmanaged `legacy` and field 7 of the uninitialized
`theme`. The `[exit status N: …]` lines come from the demo, not from
LazySubmodules.

Write the option as `--porcelain=v1`, or `--porcelain` alone. In
`status --porcelain v1`, `v1` is a submodule name, and the command fails
with `lazysubmodules: v1: no such submodule`.

### In a shell

`awk -F '\t'` splits at every TAB and keeps empty fields. This lists every
submodule that is not `ok`:

```{literalinclude} ../../examples/transcript.txt
:language: console
:start-at: "$ lazysubmodules status --porcelain=v1 |"
:end-at: "broken: missing-ref"
```

A stricter version also checks the header and skips unmanaged
submodules:

```sh
lazysubmodules status --porcelain=v1 |
	awk -F '\t' '
		NR == 1 { if ($0 != "# lsm-porcelain v1") exit 1; next }
		$8 != "ok" && $8 != "unmanaged" { print $1 ": " $8 }
	'
```

:::{warning}
Do not split records with Bash `read` and `IFS=$'\t'`. Bash treats TAB as
white space there and merges adjacent TABs, so empty fields disappear and
the following fields shift. Use `awk -F '\t'` or `cut -f`.
:::

### In Python

A field that contains a TAB, a line break, a quote, a backslash or
another unusual character is written in double quotes with C-style
escapes. This script decodes such fields and prints the submodules that
need attention:

```python
import codecs
import subprocess
import sys

FIELDS = ("name", "path", "mode", "ref", "lock_ref", "lock_commit", "head", "state")


def decode(field):
    """Return a porcelain field as bytes, with the quoting undone."""
    if field.startswith('"'):
        return codecs.escape_decode(field[1:-1].encode())[0]
    return field.encode()


out = subprocess.run(
    ["lazysubmodules", "status", "--porcelain=v1"],
    check=True, capture_output=True, text=True, encoding="utf-8",
).stdout
header, *lines = out.split("\n")[:-1]
if header != "# lsm-porcelain v1":
    sys.exit(f"unexpected porcelain header: {header!r}")
for line in lines:
    record = dict(zip(FIELDS, map(decode, line.split("\t")), strict=True))
    if record["state"] not in (b"ok", b"unmanaged"):
        name = record["name"].decode(errors="backslashreplace")
        print(f"{name}: {record['state'].decode()}")
```

On the demo in its first state, it prints:

```text
kernel: behind
u-boot: behind
theme: uninitialized
fresh: uninitialized
app: dirty
broken: missing-ref
```

- `split("\n")` splits at LF, the only line break the format produces:
  every other character that `splitlines()` breaks at is a control
  character or U+2028 or U+2029, and those are escaped inside a quoted
  field; see {ref}`ref-porcelain-quoting`.
- Decoded fields are bytes, because a quoted field may hold bytes that are
  not valid UTF-8. The output as a whole is always valid UTF-8.
- `check=True` raises an exception for any exit status other than 0.

{ref}`ref-porcelain-parsing` lists the parsing rules, and
{ref}`ref-porcelain-quoting` the escapes.

## Use the exit codes

| Code | Meaning |
|---|---|
| 0 | Success |
| 1 | Generic error, such as an unknown submodule name or a failing `foreach` command |
| 2 | Usage error |
| 3 | Refused: an unsafe state, nothing was changed |
| 4 | Verification failed |
| 5 | A `git` command failed or was interrupted |

A script can react to each one:

```sh
lazysubmodules update --fetch
case $? in
0) echo "updated" ;;
3) echo "refused: fix the submodules named above, then retry" >&2; exit 1 ;;
*) echo "update failed" >&2; exit 1 ;;
esac
```

Under `set -e`, capture the status without ending the script:

```sh
status=0
lazysubmodules verify || status=$?
case $status in
0) ;;
4) echo "lock file, gitlinks, checkouts or tags disagree" >&2 ;;
*) echo "verify could not run (exit status $status)" >&2 ;;
esac
exit "$status"
```

Every error message goes to standard error as
`lazysubmodules: <message>`. {ref}`ref-exit-codes` has examples for each
code.

(guide-scripting-ci)=

## Verify submodules in CI

`verify` compares the lock file, the gitlinks in the `HEAD` commit, the
checkouts and the locked tags, and exits with status 4 when any of them
disagree. It catches:

- a gitlink that was changed without updating `.lsm.lock`, for example by
  a plain `git add` or `git submodule update --remote`;
- a lock entry that no longer matches the configuration in
  `.gitmodules`;
- a release tag that was moved upstream (see
  [Detect moved tags](moved-tags.md)).

The job needs two commands after the checkout of the superproject:

```sh
lazysubmodules fetch      # clones, initializes and fetches every managed submodule
lazysubmodules verify     # exit status 4 when a check fails
```

Use `lazysubmodules fetch` rather than `git submodule update --init`.
Plain Git skips submodules with `update = none` in `.gitmodules`, and
`verify` then fails their `initialized` check. On a clone of the finished
demo:

```console
$ git submodule update --init
…
Skipping submodule 'libs/quirky'
$ lazysubmodules verify quirky
quirky: failed (1 of 5 checks)
  initialized: submodule is not checked out
lazysubmodules: verification failed: quirky
```

After `lazysubmodules fetch`, every submodule of the same clone passes.

`verify` reads the committed state. A pipeline that runs on a commit
passes; a script that verifies right after a staged `update` fails,
because the gitlink in `HEAD` is still the old one.

The examples below install LazySubmodules with `go install` in the
`golang:1.27.1-bookworm` image, which the project also uses to test Git
2.39, the oldest supported version. Once releases exist, downloading a
verified release archive is faster; see
[Installation](../getting-started/installation.md).

::::{tab-set}
:sync-group: ci

:::{tab-item} GitHub Actions
:sync: github

```yaml
# .github/workflows/submodules.yml
name: submodules

on:
  push:
  pull_request:

permissions:
  contents: read

jobs:
  verify:
    runs-on: ubuntu-24.04
    container: golang:1.27.1-bookworm   # pin by digest in real use
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
        with:
          persist-credentials: false
      - name: Install lazysubmodules
        run: go install github.com/FPGArtktic/lazysubmodules/cmd/lazysubmodules@latest
      - name: Trust the checkout
        # The container runs as root, the workspace belongs to the runner.
        run: git config --global --add safe.directory "$GITHUB_WORKSPACE"
      - name: Fetch submodules
        run: lazysubmodules fetch
      - name: Verify submodules
        run: lazysubmodules verify
```

`actions/checkout` does not initialize submodules unless its `submodules`
input asks for it, so `lazysubmodules fetch` clones them. For private
submodules, give Git credentials, for example with a credential helper or
an `url.<base>.insteadOf` rule that carries a token.
:::

:::{tab-item} GitLab CI
:sync: gitlab

```yaml
# .gitlab-ci.yml
verify-submodules:
  image: golang:1.27.1-bookworm   # pin by digest in real use
  variables:
    GIT_SUBMODULE_STRATEGY: none
  script:
    - go install github.com/FPGArtktic/lazysubmodules/cmd/lazysubmodules@latest
    - lazysubmodules fetch
    - lazysubmodules verify
```

`GIT_SUBMODULE_STRATEGY: none` leaves the submodules to
`lazysubmodules fetch`. For submodules on the same GitLab instance, a
rule like this lets Git use the job token; add it before the `fetch`
line and replace the host:

```sh
git config --global url."https://gitlab-ci-token:${CI_JOB_TOKEN}@gitlab.example.com/".insteadOf "https://gitlab.example.com/"
```
:::

::::

The `golang` image puts `$(go env GOPATH)/bin`, where `go install` writes
the binary, on `PATH`. Add `lazysubmodules verify --signatures` when your
submodules use signed tags or commits and the job has the public keys;
see [Verify signatures](signatures.md).

### Check for available updates

A scheduled job can report submodules with newer refs without changing
anything. After a fetch, `status` shows them as `behind`:

```sh
lazysubmodules fetch
lazysubmodules status --porcelain=v1 |
	awk -F '\t' 'NR > 1 && $8 == "behind" { print $1; n++ } END { exit n > 0 }'
```

The pipeline fails when at least one submodule is `behind`, and the job
log names them. `lazysubmodules update --dry-run` shows what an update
would select.

## Report a bug

The bug report form on GitHub asks for the output of
`lazysubmodules version` and, if you can share it,
`lazysubmodules status --porcelain=v1`. Both are safe to run: neither
changes anything or uses the network.

## See also

- {ref}`ref-porcelain`: the complete format.
- {ref}`ref-exit-codes`: every exit code with examples.
- {ref}`ref-cli-verify`: every check of `verify`.
