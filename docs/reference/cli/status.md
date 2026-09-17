<!-- SPDX-License-Identifier: GPL-3.0-only -->
<!-- Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com> -->

(ref-cli-status)=

# `status`

Show the tracking configuration, the lock entry, the checked-out commit and
the state of submodules, as a table or in the stable porcelain format.

## Synopsis

```text
lazysubmodules status [<name>...] [--porcelain=v1]
```

`status` works offline and changes nothing.

## Help text

```{literalinclude} _generated/status.txt
:language: text
:lines: 5-
```

## Options

| Option | Effect |
|---|---|
| `<name>...` | Show these submodules, unmanaged ones included. Without names, every submodule in `.gitmodules` is shown |
| `--porcelain`, `--porcelain=v1` | Print the {ref}`porcelain v1 format <ref-porcelain>` instead of the table |

The value of `--porcelain` must be attached with `=`. In
`lazysubmodules status --porcelain v1`, `v1` is a submodule name, so the
command fails with `lazysubmodules: v1: no such submodule` and status 1.
Any value other than `v1` is a usage error:

```text
lazysubmodules: status: invalid value "v2" for flag -porcelain: unsupported format "v2" (only v1 is supported)
Run 'lazysubmodules help' for usage.
```

## Behaviour

- **Local refs only.** `status` compares the configuration, the lock file
  in the working tree, the submodule `HEAD` and the local refs. It never
  fetches; run [`fetch`](fetch.md) first to see what the remotes offer.
- **Branch mode.** `behind` compares with the remote-tracking branch
  `origin/<branch>` as of the last fetch.
- **Target.** For each managed submodule, `status` resolves what
  `update` without options would select, so `behind` means that `update`
  would change something.
- **Staged updates.** The lock file is read from the working tree, so a
  submodule whose update is staged but not committed is already `ok`.
  [`verify`](verify.md) compares with the committed state instead.
- **Nested submodules** are not listed. Only modified files inside them
  make the submodule `dirty`.
- **`.gitmodules` settings.** The native `update` and `ignore` keys do not
  change the state; `status` looks at the real state of the checkout.

## Table

```{literalinclude} ../../../examples/transcript.txt
:language: text
:start-after: "$ lazysubmodules status"
:end-before: "[exit status 0: success]"
```

This is the demo superproject in its first state, as printed by
`lazysubmodules status`.

| Column | Content |
|---|---|
| `NAME` | Submodule name from `.gitmodules` |
| `PATH` | Path from `.gitmodules` |
| `MODE` | Tracking mode (`lsm-mode`) |
| `REF` | Configured ref (`lsm-ref`); a full SHA in commit mode is abbreviated to 7 characters, a shorter one is shown as it stands |
| `LOCK` | Locked commit, abbreviated to 7 characters, followed by the locked ref in parentheses when it differs from `REF`, as for a tag pattern |
| `HEAD` | Commit checked out in the submodule, abbreviated to 7 characters |
| `STATE` | One of the seven {ref}`states <ref-states>` |

- **Empty cells** show `-`: the tracking columns of an unmanaged
  submodule, `LOCK` without a lock entry, and `HEAD` when the submodule is
  not checked out.
- **Quoting.** A name, path or ref with unusual characters is shown in
  double quotes with Go-style escapes, such as `"we\"ird\u2028\t"`.
- **No submodules.** A superproject without submodules prints
  `no submodules`.

(ref-cli-status-colors)=

### Colors

The `STATE` heading is bold, and each state is colored, only when standard
output is a terminal, `NO_COLOR` is unset or empty, and `TERM` is not
`dumb`. Other cells are never styled, so the columns stay aligned.

| State | Color | SGR sequence |
|---|---|---|
| [ok]{.lsm-state .lsm-state-ok} | green | `ESC[32m` |
| [behind]{.lsm-state .lsm-state-behind} | yellow | `ESC[33m` |
| [drift]{.lsm-state .lsm-state-drift}, [missing-ref]{.lsm-state .lsm-state-missing-ref} | red | `ESC[31m` |
| [dirty]{.lsm-state .lsm-state-dirty} | magenta | `ESC[35m` |
| [uninitialized]{.lsm-state .lsm-state-uninitialized}, [unmanaged]{.lsm-state .lsm-state-unmanaged} | gray | `ESC[90m` |

## Porcelain format

With `--porcelain=v1`, `status` prints a header line and one line of eight
TAB-separated fields per submodule:

```{literalinclude} ../../../examples/transcript.txt
:language: console
:start-at: "$ lazysubmodules status --porcelain=v1 kernel legacy theme"
:end-before: "[exit status 0: success]"
```

{ref}`ref-porcelain` specifies the format and shows how to parse it.

## Exit status

| Status | When |
|---|---|
| 0 | The status was printed, whatever the states are |
| 1 | An unknown name, an invalid `lsm-mode`, a malformed lock entry, or a symbolic link in place of `.gitmodules` or `.lsm.lock` |
| 2 | A usage error, including an unsupported `--porcelain` value |
| 5 | A `git` command failed, for example outside a repository or for a file that Git cannot parse |

The states never change the exit status. Use [`verify`](verify.md), or
read the porcelain format, to fail a script on a state.

## Examples

```sh
# Everything, as a table.
lazysubmodules status

# Two submodules; the name with a space is quoted for the shell.
lazysubmodules status kernel 'crypto lib'

# For scripts: every submodule that is neither ok nor unmanaged.
lazysubmodules status --porcelain=v1 |
	awk -F '\t' 'NR > 1 && $8 != "ok" && $8 != "unmanaged" { print $1 ": " $8 }'
```

## See also

- {ref}`ref-states`: the meaning and precedence of each state.
- {ref}`ref-porcelain`: the stable format for scripts.
- {ref}`guide-troubleshooting`: what to do about each state.
- {ref}`guide-scripting`: status in scripts and CI.
