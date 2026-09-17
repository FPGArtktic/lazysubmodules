<!-- SPDX-License-Identifier: GPL-3.0-only -->
<!-- Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com> -->

(ref-cli-foreach)=

# `foreach`

Run a command, without a shell, in every managed submodule that is checked
out, in `.gitmodules` order.

## Synopsis

```text
lazysubmodules foreach -- <command> [args...]
```

`foreach` itself works offline; the command it runs may use the network.

## Help text

```{literalinclude} _generated/foreach.txt
:language: text
:lines: 5-
```

## Arguments

| Argument | Effect |
|---|---|
| `--` | Ends the options of `foreach`. It may be left out when the command does not start with `-` |
| `<command>` | The program to run. It is looked up in `PATH` unless it contains a `/`; a relative path is relative to the submodule |
| `[args...]` | Its arguments, passed unchanged |

`foreach` has no options of its own besides `-h`. Everything from the first
argument that is not an option is the command, so the command's options
need no `--`: `lazysubmodules foreach git log -1 --oneline` works. A
command that starts with `-` needs `--`; without it, `foreach` reports a
usage error:

```text
lazysubmodules: foreach: flag provided but not defined: -x
Run 'lazysubmodules help' for usage.
```

## Behaviour

- **Where.** The command runs in every managed submodule that is checked
  out, with the submodule working tree as its current directory.
  Unmanaged submodules are not visited.
- **Skipped submodules.** A managed submodule that is not checked out is
  skipped with a note on standard error, such as
  `skipping theme: submodule is not checked out`.
- **No shell.** The command is executed directly. Use `sh -c '…'` for
  pipes, redirections or variable expansion.
- **Streams.** The command inherits the standard input, output and error of
  `foreach`.
- **Failure.** The first command that fails, or cannot be started, ends
  `foreach` with status 1. The error names the submodule.
- **Signals.** When `foreach` is interrupted, the running command and the
  processes it started receive `SIGTERM`; see {ref}`ref-runtime-signals`.

(ref-cli-foreach-env)=

## Environment

The command inherits the environment of `lazysubmodules`, without the
variables that tie Git to one repository, such as `GIT_DIR` and
`GIT_INDEX_FILE`. These variables are added:

| Variable | Value | Demo value for `kernel` |
|---|---|---|
| `name` | Submodule name | `kernel` |
| `sm_path` | Path recorded in `.gitmodules` | `kernel` |
| `displaypath` | Path of the submodule relative to the current directory | `kernel`, or `../kernel` when run in `src/` |
| `sha1` | Commit checked out in the submodule; empty for an unborn `HEAD` | `8106f614767a75b6567271b1db80d6ade3acb7fb` |
| `toplevel` | Absolute path of the superproject | `$DEMO/firmware` |
| `LSM_MODE` | Tracking mode (`lsm-mode`) | `tag-pattern` |
| `LSM_REF` | Configured ref (`lsm-ref`) | `v6.6.*` |

The first five variables are the ones that `git submodule foreach` sets.
Unlike Git, `sha1` is the commit that is checked out, not the one that the
superproject records.

## Output

`foreach` prints nothing itself except the notes about skipped submodules
and its errors. On the demo, after the story:

```{literalinclude} ../../../examples/transcript.txt
:language: console
:start-at: "$ lazysubmodules foreach -- git describe --tags --exact-match"
:end-before: "[exit status 1: error]"
```

`fatal: No names found…` comes from `git describe` in `u-boot`, which is
checked out at a branch tip without a tag. The last line is the error of
`foreach`.

A command that cannot be started is reported the same way:

```text
lazysubmodules: kernel: -x: exec: "-x": executable file not found in $PATH
```

## Exit status

| Status | When |
|---|---|
| 0 | The command succeeded in every visited submodule, or there was none |
| 1 | A command failed or could not be started; or `.gitmodules` or `.lsm.lock` is invalid |
| 2 | A usage error, such as a missing command |
| 5 | A `git` command of `foreach` itself failed |

`foreach` does not pass on the exit status of the command; it exits with 1.

## Examples

```sh
# The checked-out commit of each submodule; no "--" is needed.
lazysubmodules foreach git log -1 --oneline

# Use a shell to read the variables.
lazysubmodules foreach -- sh -c 'echo "$name: $LSM_MODE $LSM_REF at $sha1"'

# Keep going where a command fails: let the shell handle the failure.
lazysubmodules foreach -- sh -c 'git describe --tags 2>/dev/null || echo "$name: no tag"'
```

On the demo in its first state, the last command prints the following
lines, and the notes about `theme` and `fresh` on standard error:

```text
v6.6.9
u-boot: no tag
v2.3.1
crypto lib: no tag
tools: no tag
v1.2.0
v3.0.0-rc.2
mirror-lib: no tag
v1.0.0
v1.0.0
v1.0.0
```

## See also

- {ref}`guide-foreach`: more examples.
- {ref}`ref-runtime`: the environment of LazySubmodules and Git.
- {ref}`project-security`: commands you run are your responsibility.
