<!-- SPDX-License-Identifier: GPL-3.0-only -->
<!-- Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com> -->

(ref-exit-codes)=

# Exit codes

Every `lazysubmodules` command exits with one of six codes, which are a
stable interface for scripts.

## Codes

| Code | Meaning | Typical causes |
|---|---|---|
| 0 | Success | The command did what was asked, including dry runs, `up to date` and `nothing to commit` |
| 1 | Error | An unknown submodule name, an invalid `lsm-mode` or lock entry, a symbolic link in place of `.gitmodules` or `.lsm.lock`, a failing `foreach` command, `git` not found, a signal that ended the terminal interface |
| 2 | Usage error | An unknown command or option, a missing or extra argument, conflicting tracking options, an invalid ref, pattern, commit, path or URL, `tui` without a usable terminal |
| 3 | Refused | A dirty submodule, a missing ref, a submodule that needs a clone without `--fetch`, unrelated changes for `--commit`, an unmanaged submodule named explicitly, a path in use for `add` |
| 4 | Verification failed | A moved tag, a lock file that disagrees with the gitlink or the checkout, a bad signature |
| 5 | Git failed | A `git` command exited with an error or was interrupted, including a `.gitmodules` or `.lsm.lock` that Git cannot parse, and commands run outside a repository |

- **Refused means unchanged.** With status 3, `update` has moved no
  submodule and changed neither `.gitmodules`, `.lsm.lock` nor the index:
  every selected submodule is checked before the first change. With
  `--fetch`, refs that were fetched and submodules that were initialized
  before the refusal remain.
- **Interrupted commands** usually exit with 5, because the `git` process
  they were waiting for was killed; see {ref}`ref-runtime-signals`.
- **States do not count.** `status` exits with 0 whatever the states are.
  Use `verify`, or read the porcelain format, to fail on a state.
- **`foreach`** exits with 1 when the command fails; the command's own
  status is not passed on.

## Examples

These messages were produced on the demo. Each goes to standard error.

| Code | Command | Message |
|---|---|---|
| 1 | `status nosuch` | `lazysubmodules: nosuch: no such submodule` |
| 1 | `status --porcelain v1` | `lazysubmodules: v1: no such submodule` |
| 1 | `status`, with `lsm-mode = semver` | `lazysubmodules: .gitmodules: submodule "kernel": lsm-mode: invalid tracking mode "semver"` |
| 1 | `foreach -- git describe --tags --exact-match`, after the demo story | `lazysubmodules: u-boot: git: exit status 128` |
| 2 | `lazysubmodules frobnicate` | `lazysubmodules: unknown command "frobnicate"` |
| 2 | `status --bogus` | `lazysubmodules: status: flag provided but not defined: -bogus` |
| 2 | `set kernel --tag 'bad..ref'` | `lazysubmodules: kernel: invalid tag "bad..ref": not a valid tag name` |
| 2 | `add https://git.example.invalid/sdk.git ../outside --branch main` | `lazysubmodules: invalid path "../outside": must be a relative path inside the superproject` |
| 2 | `tui`, without a terminal | `lazysubmodules: tui requires a terminal` |
| 3 | `update` | `lazysubmodules: app: refused: submodule has uncommitted changes` |
| 3 | `update fresh` | `lazysubmodules: fresh: refused: submodule is not initialized (use --fetch)` |
| 3 | `verify legacy` | `lazysubmodules: legacy: refused: submodule is not managed by lazysubmodules` |
| 3 | `add https://git.example.invalid/sdk.git kernel --tag v1.0.0` | `lazysubmodules: kernel: refused: path already exists: used by submodule kernel` |
| 3 | `update --commit kernel u-boot theme`, with a staged file | `lazysubmodules: refused: the commit would include unrelated changes: src/drivers/README` |
| 4 | `verify fpga.core sdk`, after the tag moved | `lazysubmodules: verification failed: fpga.core` |
| 5 | `status`, outside a repository | `lazysubmodules: open repository: git rev-parse --show-toplevel: exit status 128: fatal: not a git repository …` |
| 5 | `update kernel u-boot`, interrupted with `SIGTERM` | `lazysubmodules: kernel: git -c advice.detachedHead=false checkout … --: context canceled: signal: terminated` |

The usage errors that come from the command line itself, the first two
rows with code 2, are followed by a second line:
`Run 'lazysubmodules help' for usage.`

## Error format

- **Prefix.** Every error line starts with `lazysubmodules: `. When a command
  reports several errors, such as the refusals of an update, each gets its
  own line and prefix:

  ```text
  lazysubmodules: fresh: refused: submodule is not initialized (use --fetch)
  lazysubmodules: app: refused: submodule has uncommitted changes
  lazysubmodules: broken: refused: ref not found in local refs: tag v9.9.9 does not exist or does not point to a commit
  ```

- **Submodule.** Messages about one submodule name it after the prefix.
- **Git errors** include the `git` command line, its exit status and the
  first lines of its error output.
- **Safe text.** Characters that are not printable are replaced, so that an
  error cannot send control sequences to the terminal.

## Using the codes in a shell

```sh
lazysubmodules update --fetch --commit
case $? in
0) echo "updated" ;;
3) echo "refused: fix the reported submodules and run again" >&2 ;;
*) echo "update failed" >&2; exit 1 ;;
esac
```

```sh
status=0
lazysubmodules verify || status=$?
if [ "$status" -eq 4 ]; then
	echo "lock file, gitlinks or tags disagree" >&2
fi
exit "$status"
```

## Stability

The codes 0 to 5 and their meanings are part of the user interface, like
the {ref}`porcelain format <ref-porcelain-stability>`.

## See also

- {ref}`ref-cli-rules`: usage rules shared by all commands.
- {ref}`guide-troubleshooting`: how to resolve refusals and failures.
- {ref}`guide-scripting`: exit codes in CI.
