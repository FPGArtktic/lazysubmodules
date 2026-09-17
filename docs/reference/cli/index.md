<!-- SPDX-License-Identifier: GPL-3.0-only -->
<!-- Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com> -->

(ref-cli)=

# Command line

The synopsis of every `lazysubmodules` command, and the rules for help,
options, names, output and errors that all commands share.

## Synopsis

```text
lazysubmodules add <url> <path> (--branch B | --tag T | --tag-pattern P | --commit SHA) [--include-prerelease]
lazysubmodules set <name> (--branch B | --tag T | --tag-pattern P | --commit SHA)
lazysubmodules update [<name>...] [--fetch] [--dry-run] [--commit] [--include-prerelease]
lazysubmodules status [<name>...] [--porcelain=v1]
lazysubmodules fetch [<name>...]
lazysubmodules verify [<name>...] [--signatures]
lazysubmodules foreach -- <command> [args...]
lazysubmodules tui
lazysubmodules version
lazysubmodules help [<command>]
```

`lsm` is an optional short name for the same program: the Debian, RPM and
AUR packages install it as a symbolic link, and
{ref}`start-installation` shows how to add it by hand. This reference
always writes `lazysubmodules`.

(ref-cli-commands)=

## Commands

| Command | Purpose | Network |
|---|---|---|
| [`add`](add.md) | Clone a new submodule that tracks a ref, and stage it | Yes: clones |
| [`set`](set.md) | Change what a submodule tracks in `.gitmodules` | No |
| [`update`](update.md) | Move submodules to their targets and stage the result | Only with `--fetch` |
| [`status`](status.md) | Show the state of submodules | No |
| [`fetch`](fetch.md) | Download branches and tags, and clone missing submodules | Yes |
| [`verify`](verify.md) | Check that the lock file, the gitlinks and the checkouts agree | No |
| [`foreach`](foreach.md) | Run a command in every checked-out managed submodule | No, but the command may |
| [`tui`](tui.md) | Start the terminal interface | Only its fetch key, {kbd}`f` |
| [`version`](version.md) | Print the version, the commit and the commit date | No |
| [`help`](#ref-cli-help) | Show the list of commands, or the help of one command | No |

Only `fetch`, `update --fetch` and `add` contact remote repositories. All
other commands, including `update --dry-run --fetch`, work on the refs that
already exist locally. {ref}`guide-network` explains how Git's mirror and
credential settings apply.

## Help text

`lazysubmodules help` prints:

```{literalinclude} _generated/lazysubmodules.txt
:language: text
:lines: 5-
```

(ref-cli-help)=

## `help`

```{literalinclude} _generated/help.txt
:language: text
:lines: 5-
```

- `lazysubmodules help`, `lazysubmodules -h` and `lazysubmodules --help`
  print the list of commands on standard output and exit with status 0.
- `lazysubmodules help <command>`, `lazysubmodules <command> -h` and
  `lazysubmodules <command> --help` print the help of one command, also with
  status 0.
- `lazysubmodules` without arguments prints the list of commands on
  standard error and exits with status 2.
- `help` with an unknown command, or with more than one argument, is a
  usage error (status 2).

(ref-cli-rules)=

## General rules

### Options

- **Position.** Options may come before, between or after the submodule
  names. `lazysubmodules status kernel --porcelain=v1 legacy` is the same as
  `lazysubmodules status --porcelain=v1 kernel legacy`.
- **End of options.** `--` ends the options, and every later argument is a
  name. `lazysubmodules update -- --fetch` looks for a submodule named
  `--fetch`.
- **Spelling.** An option may be written with one or two dashes, and its
  value may follow as the next argument or after `=`:
  `--tag v1.0.0`, `--tag=v1.0.0` and `-tag v1.0.0` are the same. This
  documentation always uses two dashes.
- **Optional values.** The value of `status --porcelain` is optional and
  must be attached with `=`; see {ref}`ref-porcelain-option`.
- **Tracking options.** `add` and `set` take exactly one of `--branch`,
  `--tag`, `--tag-pattern` and `--commit`, given once.
- **`foreach`.** Its own options must come before the command, so the
  command's options need no `--`; see [`foreach`](foreach.md).
- **Unknown options** are usage errors, also when another command knows
  them: `lazysubmodules update --porcelain` fails with
  `update: flag provided but not defined: -porcelain`.

Quote tag patterns, so that the shell does not expand them:

```sh
lazysubmodules set kernel --tag-pattern 'v6.6.*'
```

### Names

- **Name, not path.** `<name>` is the submodule name from `.gitmodules`,
  which can differ from the path. In the demo, the submodule `u-boot` lives
  at `bootloader/u-boot`, and
  `lazysubmodules status bootloader/u-boot` fails with
  `bootloader/u-boot: no such submodule`. A submodule created with `add` is
  named after its path.
- **Exact match.** Names are compared exactly, including case:
  `lazysubmodules status Kernel` does not find `kernel`.
- **Spaces.** Quote names that contain spaces, such as
  `lazysubmodules status 'crypto lib'`.
- **No names.** Without names, a command works on every managed submodule.
  `status` lists every submodule, including unmanaged ones.
- **Order.** Each selected submodule is processed once, in `.gitmodules`
  order, whatever the order of the names on the command line.
- **Unknown names.** Naming a submodule that `.gitmodules` does not list is
  an error (status 1): `lazysubmodules: nosuch: no such submodule`.
- **Unmanaged names.** Naming an unmanaged submodule is refused (status 3)
  by `update`, `verify` and `fetch`:
  `lazysubmodules: legacy: refused: submodule is not managed by lazysubmodules`.
  `status` shows it, and `set` puts it under management.

### Working directory

- **Anywhere in the superproject.** Commands find the superproject from the
  current directory, so they work the same in a subdirectory such as
  `firmware/src` of the demo.
- **Inside a submodule,** the submodule is the repository. In the demo,
  `lazysubmodules status` in `firmware/kernel` prints `no submodules`.
- **Outside a repository,** commands fail with the error of Git and status 5:

  ```text
  lazysubmodules: open repository: git rev-parse --show-toplevel: exit status 128: fatal: not a git repository (or any parent up to mount point /); Stopping at filesystem boundary (GIT_DISCOVERY_ACROSS_FILESYSTEM not set).
  ```

- **Git.** The `git` program is looked up in `PATH`. Without it, commands
  that open a repository fail with status 1:
  `lazysubmodules: git executable not found: exec: "git": executable file not found in $PATH`.
  Git 2.39 or later is required.

### Output

- **Streams.** Results go to standard output. Errors, notes about skipped
  submodules and the output of `git` itself, such as clone and fetch
  progress, go to standard error. {ref}`ref-runtime-streams` lists them.
- **Order.** Submodules are reported in `.gitmodules` order.
- **Unusual characters.** A name or ref that is not valid UTF-8, or contains
  quotes, backslashes or characters that are not printable, is shown
  quoted, so that it cannot send control sequences to the terminal. The
  table of `status` shows a name made of `we"ird`, U+2028 and a TAB as
  `"we\"ird\u2028\t"`. The porcelain format uses Git's octal escapes
  instead; see {ref}`ref-porcelain-quoting`.
- **Stability.** The porcelain format, the exit codes, the `.gitmodules`
  keys `lsm-mode` and `lsm-ref`, and the network policy are stable
  interfaces; see {ref}`ref-porcelain-stability`. The table of `status` and
  the lines of `update`, `fetch` and `verify` are meant for people and may
  change between releases.

### Errors

- **Format.** Errors are printed to standard error as
  `lazysubmodules: <message>`. When several errors are reported, each line
  has the prefix.
- **Usage errors** in the command line, such as an unknown command or
  option or a missing argument, add a second line (status 2):

  ```text
  lazysubmodules: unknown command "frobnicate"
  Run 'lazysubmodules help' for usage.
  ```

- **Invalid values,** such as a malformed ref, path or URL, are usage errors
  (status 2) without the second line:
  `lazysubmodules: kernel: invalid tag "bad..ref": not a valid tag name`.
- **Exit codes** are listed in {ref}`ref-exit-codes`.

## Command pages

```{toctree}
:maxdepth: 1

add
set
update
status
fetch
verify
foreach
tui
version
```
