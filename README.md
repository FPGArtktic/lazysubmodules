<!-- SPDX-License-Identifier: GPL-3.0-only -->
<!-- Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com> -->

# LazySubmodules

[![CI](https://github.com/FPGArtktic/lazysubmodules/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/FPGArtktic/lazysubmodules/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/FPGArtktic/lazysubmodules?include_prereleases&sort=semver)](https://github.com/FPGArtktic/lazysubmodules/releases)
[![AUR](https://img.shields.io/aur/version/lazysubmodules-git?label=AUR%20lazysubmodules-git)](https://aur.archlinux.org/packages/lazysubmodules-git)
[![Go version](https://img.shields.io/github/go-mod/go-version/FPGArtktic/lazysubmodules)](go.mod)
[![Go Reference](https://pkg.go.dev/badge/github.com/FPGArtktic/lazysubmodules.svg)](https://pkg.go.dev/github.com/FPGArtktic/lazysubmodules)
[![Go Report Card](https://goreportcard.com/badge/github.com/FPGArtktic/lazysubmodules)](https://goreportcard.com/report/github.com/FPGArtktic/lazysubmodules)
[![License: GPL-3.0-only](https://img.shields.io/badge/license-GPL--3.0--only-blue)](LICENSE)
[![Platform](https://img.shields.io/badge/platform-linux%20amd64%20%7C%20arm64-lightgrey)](#installation)
![Made in Poland](https://img.shields.io/badge/made%20in-Poland-DC143C?labelColor=white)

LazySubmodules manages Git submodules that track **branches, tags, tag
patterns or fixed commits**. It has a scriptable command line interface
and an interactive terminal user interface (TUI). The binary is called
`lazysubmodules`, and `lsm` is an optional short name.

- **Tracking modes:** a submodule can follow a branch, a tag, the highest
  version tag matching a glob such as `v2.*`, or a fixed commit.
- **Lock file:** `.lsm.lock` records what each submodule was resolved to, so
  tags that were moved on the remote are detected.
- **Predictable network use:** only `fetch`, `update --fetch` and `add`
  touch the network; everything else works on local refs.
- **Native Git underneath:** configuration lives in `.gitmodules`, and all
  work is done by the `git` command line tool. Plain
  `git submodule update --remote` keeps working for branch-tracking
  submodules.
- **Scripting:** a stable, machine-readable status format, `verify` with
  optional signature checks, and distinct exit codes.

## Contents

- [Why LazySubmodules](#why-lazysubmodules)
- [Quick start](#quick-start)
- [Tracking modes](#tracking-modes)
- [Data format](#data-format)
- [Installation](#installation)
- [Command reference](#command-reference)
- [Terminal user interface](#terminal-user-interface)
- [Verifying releases](#verifying-releases)
- [Building from source](#building-from-source)
- [Known limitations](#known-limitations)
- [Contributing](#contributing)
- [License](#license)
- [Author](#author)

## Why LazySubmodules

Native Git can track only **branches**: set `submodule.<name>.branch` and
run `git submodule update --remote`. There is no native way to say "this
submodule follows tag `v2.3.1`" or "this submodule follows the newest
`v6.6.*` release". Many projects pin dependencies to releases, so these
updates are done by hand.

LazySubmodules fills this gap without breaking native Git behaviour. The
extra configuration is stored in namespaced keys that Git ignores, the
superproject still records ordinary gitlinks, and a clone works with plain
`git submodule update --init` for anyone who does not use LazySubmodules.

It is not a replacement for `repo`, `west`, `git subtree` or monorepo
tooling, and it does not manage credentials: Git uses its configured
credential helpers.

## Quick start

```sh
# Let the existing submodule "kernel" follow the newest stable v6.6.x tag.
lazysubmodules set kernel --tag-pattern 'v6.6.*'

# Fetch, resolve, check out, update .lsm.lock and commit the result.
lazysubmodules update kernel --fetch --commit

# Add a new submodule that follows the branch "main" (changes are staged).
lazysubmodules add https://git.example.org/u-boot.git u-boot --branch main

# Inspect and verify.
lazysubmodules status
lazysubmodules verify

# Or work interactively.
lazysubmodules tui
```

Quote glob patterns such as `'v6.6.*'`, so that the shell does not expand
them.

## Tracking modes

| Mode | Option | Example ref | Semantics |
|---|---|---|---|
| `branch` | `--branch B` | `main` | Floating. `update` moves the submodule to the tip of the remote branch |
| `tag` | `--tag T` | `v2.3.1` | Pinned. `update` resolves the tag to its commit |
| `tag-pattern` | `--tag-pattern P` | `v2.*` | `update` selects the highest version tag matching the glob |
| `commit` | `--commit SHA` | `a1b2c3d…` | Pinned to a SHA. `update` only verifies the commit and checks it out |

### Resolution rules

- **Local only.** Resolution uses refs that already exist locally, so run
  `lazysubmodules fetch` or `update --fetch` first. Refs are looked up in the
  submodule's working tree or, when it is not checked out, in its Git
  directory under `.git/modules/`.
- **Branch:** resolves `refs/remotes/origin/<branch>` in the submodule. The
  remote is always `origin`.
- **Tag:** resolves `refs/tags/<tag>`. Annotated tags are dereferenced to
  their commit, as with `git rev-parse "<tag>^{commit}"`.
- **Tag pattern:** the pattern is a glob as accepted by `git tag --list`.
  Candidates are sorted by version with

  ```sh
  git -c versionsort.suffix=- tag --list <pattern> --sort=-v:refname
  ```

  so `v1.10.0` sorts above `v1.9.0` and `v1.0.0-rc.1` sorts below `v1.0.0`.
  The highest candidate wins.
- **Pre-release tags** are excluded from `tag-pattern` unless `update` is
  given `--include-prerelease`. A tag counts as a pre-release when a `-`
  follows its first digit: `v1.0.0-rc.1` and `v6.6-rc3` are pre-releases,
  `release-2.1` is not.
- **Never downgrade:** in `tag-pattern` mode, the tag recorded in
  `.lsm.lock` stays a candidate as long as it still exists locally and
  matches the pattern, even when it is a pre-release. For example, after an
  update with `--include-prerelease` to `v2.1.0-rc.1`, a plain `update`
  keeps `v2.1.0-rc.1` instead of going back to `v2.0.3`. It moves on to
  `v2.1.0` once that tag exists.
- **Commit:** the configured SHA must exist locally.

## Data format

### `.gitmodules`

Git ignores unknown keys, so the tracking configuration lives in
`.gitmodules` under the `lsm-` prefix, which avoids collisions with future
Git keys:

```ini
[submodule "kernel"]
	path = kernel
	url = https://git.example.org/linux.git
	lsm-mode = tag-pattern
	lsm-ref = v6.6.*

[submodule "u-boot"]
	path = u-boot
	url = https://git.example.org/u-boot.git
	branch = main
	lsm-mode = branch
	lsm-ref = main
```

| Key | Meaning |
|---|---|
| `lsm-mode` | `branch`, `tag`, `tag-pattern` or `commit` |
| `lsm-ref` | Branch name, tag name, glob pattern or full commit SHA |
| `branch` | Native Git key. Written for `lsm-mode = branch`, removed for all other modes |

- The native `branch` key keeps `git submodule update --remote` working
  without LazySubmodules.
- The file is read and written only with `git config -f .gitmodules`.
- Submodules without `lsm-mode` are shown as `unmanaged` and never
  modified, until you opt in with `lazysubmodules set`.
- An unknown `lsm-mode` value is reported as an error. An invalid
  `lsm-ref` makes the submodule show up as `missing-ref`.

### `.lsm.lock`

The lock file records the resolved ref and commit of each managed
submodule. It uses the same git-config format and is read and written with
`git config -f .lsm.lock`:

```ini
[submodule "kernel"]
	mode = tag-pattern
	ref = v6.6.8
	commit = a1b2c3d4e5f6a7b8c9d0a1b2c3d4e5f6a7b8c9d0

[submodule "u-boot"]
	mode = branch
	ref = main
	commit = d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3
```

| Key | Meaning |
|---|---|
| `mode` | Tracking mode used for the resolution |
| `ref` | Resolved ref: the tag for `tag` and `tag-pattern`, the branch for `branch`, the full SHA for `commit` |
| `commit` | Full commit SHA: 40 hex digits, or 64 in SHA-256 repositories |

- `update` and `add` write the lock file and stage it together with the
  gitlink change, even when an ignore rule such as `*.lock` matches it.
  Commit it to the superproject.
- `.gitmodules` and `.lsm.lock` must be regular files. A symbolic link in
  their place is refused, so that a cloned repository cannot redirect a
  write to a file outside of it.
- Comparing the lock with the local tags reveals tags that were moved on
  the remote (force-pushed): `status` shows `drift` and `verify` fails.

## Installation

LazySubmodules runs on Linux (`amd64` and `arm64`) and needs Git 2.39 or
later at run time.

### Release archives

Each [GitHub release](https://github.com/FPGArtktic/lazysubmodules/releases)
has a `lazysubmodules_<version>_linux_<arch>.tar.gz` archive for `amd64` and
`arm64`. It contains the binary, `LICENSE` and `README.md`:

```sh
VERSION=1.2.3   # release version without the leading "v"
ARCH=amd64      # or arm64
BASE="https://github.com/FPGArtktic/lazysubmodules/releases/download/v${VERSION}"
curl -fLO "${BASE}/lazysubmodules_${VERSION}_linux_${ARCH}.tar.gz"
tar -xzf "lazysubmodules_${VERSION}_linux_${ARCH}.tar.gz"
sudo install -m 0755 lazysubmodules /usr/local/bin/lazysubmodules
sudo ln -s lazysubmodules /usr/local/bin/lsm   # optional short name
```

See [Verifying releases](#verifying-releases) to check the download first.

### Debian and RPM packages

Each release also has `.deb` and `.rpm` packages for `amd64` and `arm64`,
named `lazysubmodules_<version>_linux_<arch>.deb` and
`lazysubmodules_<version>_linux_<arch>.rpm`. They install
`/usr/bin/lazysubmodules`, the short name `/usr/bin/lsm` (a symlink), and
`LICENSE` and `README.md` under `/usr/share/doc/lazysubmodules/`, and they
depend on `git`, which the package manager installs when it is missing:

```sh
VERSION=1.2.3   # release version without the leading "v"
ARCH=amd64      # or arm64
BASE="https://github.com/FPGArtktic/lazysubmodules/releases/download/v${VERSION}"

# Debian, Ubuntu
curl -fLO "${BASE}/lazysubmodules_${VERSION}_linux_${ARCH}.deb"
sudo apt install "./lazysubmodules_${VERSION}_linux_${ARCH}.deb"

# Fedora and other RPM-based systems
curl -fLO "${BASE}/lazysubmodules_${VERSION}_linux_${ARCH}.rpm"
sudo dnf install "./lazysubmodules_${VERSION}_linux_${ARCH}.rpm"
```

The packages are not signed themselves; check them against the signed
`checksums.txt` first (see [Verifying releases](#verifying-releases)).
CI installs the packages built from every change in Debian and Fedora and
runs them.

### Arch Linux (AUR)

The AUR package `lazysubmodules-git` builds the latest `main` branch from
source. Install it with your AUR helper, for example:

```sh
yay -S lazysubmodules-git
```

or without a helper:

```sh
git clone https://aur.archlinux.org/lazysubmodules-git.git
cd lazysubmodules-git
makepkg -si
```

The package also provides `lsm` as a symlink to `lazysubmodules`.

### Go

With Go 1.27.1 or later:

```sh
go install github.com/FPGArtktic/lazysubmodules/cmd/lazysubmodules@latest
```

The binary is installed to `$(go env GOPATH)/bin`, or to `GOBIN` when it is
set.

## Command reference

```text
lazysubmodules add <url> <path> (--branch B | --tag T | --tag-pattern P | --commit SHA)
lazysubmodules set <name> (--branch B | --tag T | --tag-pattern P | --commit SHA)
lazysubmodules update [<name>...] [--fetch] [--dry-run] [--commit] [--include-prerelease]
lazysubmodules status [<name>...] [--porcelain=v1]
lazysubmodules fetch [<name>...]
lazysubmodules verify [<name>...] [--signatures]
lazysubmodules foreach -- <command> [args...]
lazysubmodules tui
lazysubmodules version
```

General rules:

- **Help:** `lazysubmodules help [<command>]`, `-h` and `--help` print usage
  (exit code 0).
- **Flags:** flags may appear before or after the submodule names, and `--`
  ends the flags. `add` and `set` take exactly one of `--branch`, `--tag`,
  `--tag-pattern` and `--commit`.
- **Usage errors:** an unknown command or flag, or a missing or extra
  argument, exits with code 2.
- **Names:** `<name>` is the submodule name from `.gitmodules`. Without
  names, a command works on all managed submodules (`status` lists all
  submodules, including unmanaged ones). Naming an unknown submodule is an
  error. Naming an unmanaged submodule is refused (exit code 3) by every
  command except `status` and `set`.
- **Output:** submodules are processed and reported in `.gitmodules` order.

### `add`

```sh
lazysubmodules add <url> <path> (--branch B | --tag T | --tag-pattern P | --commit SHA)
```

Adds a new managed submodule:

1. Clones it with `git submodule add`; branch mode passes `-b <branch>`. The
   submodule name is its path, as with plain Git.
2. Writes `lsm-mode` and `lsm-ref` to `.gitmodules`.
3. Resolves the ref, checks out the commit and writes `.lsm.lock`.
4. Stages `.gitmodules`, `.lsm.lock` and the new gitlink. It does not
   commit.

`<path>` must be a relative path inside the superproject that is not
already a submodule. If a step after the clone fails, the new submodule is
removed again and the original error is reported.

### `set`

```sh
lazysubmodules set <name> (--branch B | --tag T | --tag-pattern P | --commit SHA)
```

- **Effect:** changes the tracking configuration of an existing submodule in
  `.gitmodules`, and nothing else. It does not touch the submodule, the lock
  file or the index; run `update` afterwards. `update` then stages
  `.gitmodules` together with the lock file and the gitlink.
- **Unmanaged submodules:** `set` is how you put an unmanaged submodule
  under LazySubmodules' control.
- **Validation:** branch and tag names are checked with
  `git check-ref-format`.
- **Commit mode:** an abbreviated SHA is expanded when the commit is
  available locally; otherwise a full SHA is required.

### `update`

```sh
lazysubmodules update [<name>...] [--fetch] [--dry-run] [--commit] [--include-prerelease]
```

For each selected managed submodule:

1. Resolve the target commit according to the
   [resolution rules](#resolution-rules).
2. Check out the commit in the submodule. A detached HEAD is expected.
3. Update `.lsm.lock`.
4. **Default:** stage the gitlink, `.gitmodules` and `.lsm.lock` with
   `git add`.
5. **`--commit`:** additionally create a commit (see below).
6. **`--dry-run`:** print the planned changes and modify nothing.

Options:

| Option | Effect |
|---|---|
| `--fetch` | Fetch from `origin` before resolving, and clone uninitialized submodules when needed |
| `--dry-run` | Print the plan only. Never fetches, initializes or clones, even with `--fetch` |
| `--commit` | Create one commit with the result |
| `--include-prerelease` | Let `tag-pattern` select pre-release tags |

- **Uninitialized submodules** are initialized. When the submodule's Git
  directory still exists (for example after `git submodule deinit`), this
  happens offline. When a clone is needed, `update` refuses unless
  `--fetch` is given.
- **Dry run:** `--dry-run` resolves against the refs that exist locally. A
  submodule that would need a clone is listed without a target.
- **Native key:** `update` also rewrites the native `branch` key in
  `.gitmodules` to match the tracking mode.

`update` refuses with exit code 3 when:

- a submodule working tree has uncommitted changes (untracked files do not
  count, nor does a nested submodule that is only checked out at another
  commit);
- the resolved ref does not exist locally (`fetch` first, or use `--fetch`);
- a submodule is not initialized, its Git directory is missing and
  `--fetch` was not given;
- `--commit` is given and the index already contains unrelated changes;
- an unmanaged submodule is named explicitly.

All checks run for every selected submodule before anything is modified.
If one submodule is refused, nothing changes.

`update` prints one line per submodule, for example:

```text
kernel: v6.6.8 (a1b2c3d) -> v6.6.9 (e4f5a6b)
u-boot: up to date
```

With `--commit`, it also prints the new commit. The human-readable output is
not a stable interface; scripts should use `status --porcelain=v1`.

#### `update --commit`

- **Staged changes:** the index may contain only `.gitmodules`, `.lsm.lock`
  and the paths of the updated submodules. Anything else is refused (exit
  code 3).
- **Commit:** the commit is created with `git commit -s`, so the
  `Signed-off-by` line comes from `user.name` and `user.email`. Commit hooks
  run as configured.
- **One commit per invocation.** When nothing changed, no commit is created.

For one submodule the message looks like this:

```text
manifest: update kernel to v6.6.9

Tracking mode: tag-pattern v6.6.*
Old: a1b2c3d4e5f6 (v6.6.8)
New: e4f5a6b7c8d9 (v6.6.9)

Signed-off-by: Your Name <you@example.org>
```

For several submodules the subject is `manifest: update N submodules`, and
the body lists each submodule:

```text
manifest: update 2 submodules

Submodule "kernel":
  Tracking mode: tag-pattern v6.6.*
  Old: a1b2c3d4e5f6 (v6.6.8)
  New: e4f5a6b7c8d9 (v6.6.9)

Submodule "u-boot":
  Tracking mode: branch main
  Old: d4e5f6a7b8c9 (main)
  New: 3f2e1d0c9b8a (main)

Signed-off-by: Your Name <you@example.org>
```

The quoted `Submodule "<name>":` heading keeps Git from reading the last
block as a trailer block (like `Signed-off-by:`), so `git commit -s` always
separates the sign-off with a blank line.

Details:

- **SHAs:** commits are shown with 12 characters.
- **Commit mode:** the subject shows the 12-character SHA, and the
  parenthesized ref is omitted.
- **No previous lock entry:** the old line reads
  `Old: <sha> (unlocked)`, or `Old: none` when the submodule was not checked
  out either.
- **Long subjects:** a subject longer than 75 characters is shortened to
  `manifest: update <name>`, or to `manifest: update 1 submodule`.
- **Line length:** lines are kept within 75 columns where possible.

### Network policy

| Command | Network |
|---|---|
| `fetch` | Yes |
| `update --fetch` | Yes, before resolution |
| `add` | Yes (clone) |
| All other commands | **No** |

- **Dry run:** `update --dry-run --fetch` does not use the network either.
- **TUI:** in the TUI, `f` (fetch) is the only network action.
- **Mirrors** configured with `url.<base>.insteadOf` work transparently,
  because all network access goes through `git`.
- **Credentials:** they come from Git's credential helpers. The TUI cannot
  answer interactive credential prompts, so fetches from the TUI need a
  credential helper or an SSH agent.
- **`foreach`:** the command itself does not use the network, but the
  commands you run with it may.

### `fetch`

```sh
lazysubmodules fetch [<name>...]
```

For each selected managed submodule, `fetch` first clones and initializes
the submodule if it is not initialized. It then runs
`git fetch --tags --force --prune origin`.

`--force` lets local tags follow tags that were moved on the remote, which
is how `status` and `verify` detect moved tags. A local tag with the same
name as a remote tag is therefore replaced.

### `status`

```sh
lazysubmodules status [<name>...] [--porcelain=v1]
```

Without `--porcelain`, `status` prints a table with the columns `NAME`,
`PATH`, `MODE`, `REF`, `LOCK`, `HEAD` and `STATE`, with abbreviated SHAs.
States are colored only when standard output is a terminal, `NO_COLOR` is
unset or empty, and `TERM` is not `dumb`.

`status` works offline: for `branch` mode, `behind` compares against the
remote-tracking branch as of the last fetch.

| State | Meaning |
|---|---|
| `ok` | HEAD equals the lock commit; no newer ref is available locally |
| `behind` | A newer commit or tag is available in local refs |
| `drift` | HEAD differs from the lock commit, or the locked tag now resolves to a different commit |
| `dirty` | The submodule working tree has uncommitted changes |
| `uninitialized` | The submodule is not checked out |
| `missing-ref` | The configured ref is not found locally |
| `unmanaged` | The submodule has no `lsm-mode` key |

The first matching state wins, in this order:

1. `unmanaged`: no `lsm-mode` key.
2. `uninitialized`: the submodule is not checked out.
3. `dirty`: the working tree has uncommitted changes.
4. `missing-ref`: the configured ref is invalid or does not resolve locally.
5. `drift`: a lock entry exists, and either HEAD differs from the locked
   commit, or, in `tag` and `tag-pattern` mode, the locked tag now points
   to a different commit or no longer exists.
6. `behind`: there is no lock entry yet, or `update` would select a
   different ref or commit than the lock records, or HEAD differs from that
   target.
7. `ok`: otherwise.

#### `status --porcelain=v1`

The porcelain v1 format is a stable contract for scripts. Incompatible
changes will only come as a new `v2` format. Any `--porcelain` value other
than `v1` is a usage error (exit code 2).

- **Header:** the first line is `# lsm-porcelain v1`.
- **Records:** then one line per submodule, with eight fields separated by
  a single TAB. Lines end with LF, there is no trailing TAB, and there are
  no colors.
- **Empty fields:** a field without a value is empty, for example the mode
  and refs of an unmanaged submodule, or the HEAD of an uninitialized one.

| # | Field | Example |
|---|---|---|
| 1 | name | `kernel` |
| 2 | path | `kernel` |
| 3 | mode | `tag-pattern` |
| 4 | ref (configured) | `v6.6.*` |
| 5 | resolved ref (lock) | `v6.6.8` |
| 6 | lock commit | full SHA |
| 7 | HEAD commit | full SHA |
| 8 | state | `ok`, `behind`, `drift`, `dirty`, `uninitialized`, `missing-ref`, `unmanaged` |

**Quoting.** A field that contains a TAB, LF, CR, double quote (`"`),
backslash (`\`) or any other control character is written in double quotes
with C-style escapes, the way Git quotes unusual path names (for example
`"a\tb"`). All other fields are written as they are. A field that starts
with `"` is therefore always quoted.

Example (fields separated by TABs):

```text
# lsm-porcelain v1
kernel	kernel	tag-pattern	v6.6.*	v6.6.8	a1b2c3d4e5f6a7b8c9d0a1b2c3d4e5f6a7b8c9d0	a1b2c3d4e5f6a7b8c9d0a1b2c3d4e5f6a7b8c9d0	ok
u-boot	u-boot	branch	main	main	d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3	d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3	behind
fpga-ip	ip/fpga	tag-pattern	v2.*	v2.1.0	0718ab2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a	9c8b7a6f5e4d3c2b1a0f9e8d7c6b5a4f3e2d1c0b	drift
theme	docs/theme	tag	v1.4.0	v1.4.0	5a6b7c8d9e0f1a2b3c4d5e6f7a8b9c0d1e2f3a4b		uninitialized
```

To list every submodule that is not `ok`:

```sh
lazysubmodules status --porcelain=v1 |
	awk -F '\t' 'NR > 1 && $8 != "ok" { print $1 ": " $8 }'
```

### `verify`

```sh
lazysubmodules verify [<name>...] [--signatures]
```

`verify` compares the lock commit, the gitlink in the superproject's `HEAD`
commit and the submodule HEAD. For `tag` and `tag-pattern` it also checks
that the locked tag still resolves to the locked commit. It prints every
failed check and exits with code 4 when any check fails. It works offline.

| Check | Passes when |
|---|---|
| `lock-entry` | `.lsm.lock` has an entry for the submodule |
| `lock-config` | The lock entry matches `.gitmodules`: same mode; for `branch` and `tag` the same ref; for `tag-pattern` the locked tag exists and matches the pattern; for `commit` the locked commit is the configured SHA |
| `lock-commit` | The locked commit is a well-formed full SHA for the repository |
| `gitlink` | The gitlink in the superproject's `HEAD` commit equals the locked commit |
| `initialized` | The submodule is checked out |
| `head` | The submodule HEAD equals the locked commit |
| `tag` | `tag` and `tag-pattern` only: the locked tag still resolves to the locked commit |
| `signature` | Only with `--signatures`: see below |

- **Committed state:** the `gitlink` check reads the committed state of the
  superproject. An update that is staged but not yet committed therefore
  fails `verify`.
- **`--signatures`:** runs `git verify-tag` on the locked tag in `tag` and
  `tag-pattern` mode, and `git verify-commit` on the locked commit in
  `branch` and `commit` mode. GPG and SSH signatures work as configured in
  Git (for example `gpg.format` and `gpg.ssh.allowedSignersFile`).

### `foreach`

```sh
lazysubmodules foreach -- <command> [args...]
```

- **What runs:** `<command>` runs in every managed, initialized submodule,
  in `.gitmodules` order, with the submodule as its working directory.
  Uninitialized submodules are skipped with a note on standard error.
- **No shell:** the command is executed directly. Use `sh -c` when you need
  one.
- **Failure:** `foreach` stops at the first command that fails and exits
  with a non-zero code.

The command gets these environment variables in addition to the inherited
environment:

| Variable | Value |
|---|---|
| `name` | Submodule name |
| `sm_path` | Submodule path as recorded in `.gitmodules` |
| `displaypath` | Submodule path for display, as in `git submodule foreach` |
| `sha1` | Commit checked out in the submodule |
| `toplevel` | Absolute path of the superproject |
| `LSM_MODE` | Tracking mode (`lsm-mode`) |
| `LSM_REF` | Configured ref (`lsm-ref`) |

```sh
lazysubmodules foreach -- git describe --tags
lazysubmodules foreach -- sh -c 'echo "$name: $LSM_MODE $LSM_REF at $sha1"'
```

### `tui`

`tui` starts the [terminal user interface](#terminal-user-interface). If
standard input or output is not a terminal, it exits with code 2 and the
message `lazysubmodules: tui requires a terminal`.

### `version`

`version` (or `--version`) prints the version, the source commit and the
build date:

```text
lazysubmodules 1.2.3
commit: <commit SHA>
date: <build date>
```

Binaries built with `go install` take this information from the Go build
information.

### Exit codes

| Code | Meaning | Examples |
|---|---|---|
| 0 | Success | |
| 1 | Generic error | Unknown submodule name, unreadable configuration |
| 2 | Usage error | Unknown flag, conflicting tracking options, `tui` without a terminal |
| 3 | Refused (unsafe state) | Dirty submodule, missing ref, unrelated staged changes |
| 4 | Verification failed | Moved tag, lock and gitlink disagree, bad signature |
| 5 | Git command failed | A `git` invocation exited with an error |

Errors are printed to standard error as `lazysubmodules: <message>`.

## Terminal user interface

```text
┌ Submodules ─────────────────────────────────────┬ Preview ───────────┐
│ NAME     MODE        REF     LOCK     STATE     │ git log --oneline  │
│ kernel   tag         v6.6.8  a1b2c3d  ok        │ tag list           │
│ u-boot   branch      main    d4e5f6a  behind    │ lock vs HEAD diff  │
│ fpga-ip  tag-pattern v2.*    0718ab2  drift     │                    │
└─────────────────────────────────────────────────┴────────────────────┘
 u update  b branch  t tag  p pattern  f fetch  v verify  ? help  q quit
```

The table lists the submodules. The preview panel shows the recent log,
the tags, and the difference between the lock and HEAD of the selected
submodule, using local refs only. The preview is hidden in terminals
narrower than 80 columns.

| Key | Action |
|---|---|
| `↑` `↓` / `k` `j` | Navigate |
| `Enter` | Submodule details (`Esc` or `q` to go back) |
| `u` | Update selected submodule (stage) |
| `U` | Update selected submodule with commit |
| `b` / `t` / `p` | Change tracking mode: pick a branch, pick a tag, or enter a tag pattern |
| `f` | Fetch the selected submodule (the only network action) |
| `v` | Verify the selected submodule |
| `d` | Show the gitlink diff in the superproject |
| `r` | Reload |
| `?` | Help |
| `q` / `Ctrl+C` | Quit (`q` closes an open overlay first) |

- **Confirmation:** actions that modify the superproject ask for
  confirmation first. `b`, `t` and `p` change only the tracking
  configuration, like `lazysubmodules set`; press `u` or `U` afterwards to
  update.
- **Pattern entry:** `p` shows how many local tags match the pattern while
  you type.
- **Background work:** long-running operations run in the background with a
  spinner, so the interface never blocks. Only one modifying operation runs
  at a time.
- **Errors** are shown in the status bar; the TUI does not exit on errors.
- **Colors** respect [`NO_COLOR`](https://no-color.org/).

## Verifying releases

Release checksums are signed with [cosign](https://github.com/sigstore/cosign)
keyless signing from the release workflow. Each release has
`checksums.txt` (SHA-256) for all archives, packages and SBOMs, its
signature `checksums.txt.sig`, the signing certificate `checksums.txt.pem`,
and the same signature as a Sigstore bundle, `checksums.txt.sigstore.json`.
An SPDX SBOM generated by syft is published for each archive
(`<archive>.sbom.json`).

```sh
VERSION=1.2.3
BASE="https://github.com/FPGArtktic/lazysubmodules/releases/download/v${VERSION}"
for f in checksums.txt checksums.txt.sig checksums.txt.pem; do
	curl -fLO "${BASE}/${f}"
done

cosign verify-blob \
	--certificate checksums.txt.pem \
	--signature checksums.txt.sig \
	--certificate-identity-regexp '^https://github\.com/FPGArtktic/lazysubmodules/\.github/workflows/release\.yml@refs/tags/v.+$' \
	--certificate-oidc-issuer https://token.actions.githubusercontent.com \
	checksums.txt

# Then check the downloaded archives and packages against the checksums.
sha256sum --ignore-missing -c checksums.txt
```

- **Result:** cosign prints `Verified OK` on success.
- **Bundle:** with a recent cosign, `--bundle checksums.txt.sigstore.json`
  can replace the `--certificate` and `--signature` options.
- **Network:** verification looks up the signature in the public Sigstore
  transparency log, so it needs network access.
- **Deprecation warnings:** recent cosign versions warn that `--certificate`
  and `--signature` are deprecated. The flags still work.
- **Release tags** are signed as well (`git tag -s`). Check one with
  `git tag -v v<version>`, given the maintainer's public key.

## Building from source

The supported build runs in a container with a pinned toolchain, on an
x86_64 (amd64) Linux host. It needs Podman (preferred) or Docker:

```sh
git clone https://github.com/FPGArtktic/lazysubmodules.git
cd lazysubmodules
scripts/build-in-container.sh build   # binary in bin/lazysubmodules
```

Build and test with a host Go toolchain (Go 1.27.1 or later) instead:

```sh
go build -trimpath -o lazysubmodules ./cmd/lazysubmodules
go test -race ./...
```

Dependencies are vendored in `vendor/`, so neither build downloads
modules. [CONTRIBUTING.md](CONTRIBUTING.md) describes all build targets
(`test`, `lint`, `snapshot`, ...).

## Known limitations

- **Nested submodules** are not managed recursively in v1: submodules inside
  a managed submodule are neither initialized nor updated by
  LazySubmodules. After an update, a nested submodule may stay at a commit
  other than the one the new commit records. That alone does not make the
  managed submodule `dirty`; modified files inside the nested submodule do.
- **Linux only** (`amd64`, `arm64`). Windows and macOS are not supported.
- **The remote is always `origin`.** Branch tracking resolves
  `refs/remotes/origin/<branch>`, and `fetch` fetches from `origin`.
- **Local resolution.** `status`, `verify` and `update` without `--fetch`
  see only what was fetched before.
- **Git 2.39 or later** is required.

## Contributing

Contributions are welcome. Read [CONTRIBUTING.md](CONTRIBUTING.md) for the
build and test workflow, the coding style and the commit message rules.
Every commit needs a `Signed-off-by` line, which certifies the
[Developer Certificate of Origin](DCO).

## License

LazySubmodules is free software: you can redistribute it and/or modify it
under the terms of the GNU General Public License, version 3 only
(`GPL-3.0-only`), as published by the Free Software Foundation. See
[LICENSE](LICENSE) for the full text.

## Author

Mateusz Okulanis <FPGArtktic@outlook.com>
