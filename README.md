<!-- SPDX-License-Identifier: GPL-3.0-only -->
<!-- Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com> -->

# LazySubmodules

[![CI](https://github.com/FPGArtktic/lazysubmodules/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/FPGArtktic/lazysubmodules/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/FPGArtktic/lazysubmodules?include_prereleases&sort=semver)](https://github.com/FPGArtktic/lazysubmodules/releases)
[![Go version](https://img.shields.io/github/go-mod/go-version/FPGArtktic/lazysubmodules)](go.mod)
[![Go Reference](https://pkg.go.dev/badge/github.com/FPGArtktic/lazysubmodules.svg)](https://pkg.go.dev/github.com/FPGArtktic/lazysubmodules)
[![Linted by golangci-lint](https://img.shields.io/badge/linted%20by-golangci--lint-00ADD8?logo=go&logoColor=white)](.golangci.yml)
[![License: GPL-3.0-only](https://img.shields.io/badge/license-GPL--3.0--only-blue)](LICENSE)
[![Platform](https://img.shields.io/badge/platform-linux%20amd64%20%7C%20arm64-lightgrey)](#installation)
![Made in Poland](https://img.shields.io/badge/made%20in-Poland-DC143C?labelColor=white)

![The LazySubmodules terminal interface on a superproject with fourteen
submodules: a table with the name, mode, ref, lock and colored state of
each submodule, and a preview of the selected one. The recording moves
through the table, opens the details of u-boot, updates u-boot after a
confirmation until its state turns ok, and shows the help
page.](docs/demo/hero.gif)

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

- [Quick demo](#quick-demo)
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
- [Security](#security)
- [License](#license)
- [Author](#author)

## Quick demo

[`scripts/demo.sh`](scripts/demo.sh) builds a firmware superproject with
fourteen submodules, covering every tracking mode and every state, and
walks through LazySubmodules on it in twelve short chapters. It runs
offline and touches nothing outside its own directory: Git runs without
your configuration, may use only the file transport, and every submodule
URL is rewritten to a local mirror.

The demo needs Bash, Git 2.39 or later and a `lazysubmodules` binary:

```sh
git clone https://github.com/FPGArtktic/lazysubmodules.git
cd lazysubmodules
scripts/build-in-container.sh build   # binary in bin/lazysubmodules
scripts/demo.sh                       # press Enter between the chapters
```

A host Go toolchain can build the binary instead of the container, and
`scripts/demo.sh --binary PATH` runs another binary:

```sh
go build -o bin/lazysubmodules ./cmd/lazysubmodules
```

The demo shows every state in `status`, the porcelain format and a script
that reads it, `set` on an unmanaged submodule, dry runs with and without
pre-release tags, a refused update and how to fix it, `update --commit`
with the generated commit message, a tag moved upstream that `verify`
catches, a clone through the mirror, a signature check and `foreach`. Each
`lazysubmodules` command is followed by its exit status, and the demo
fails when a command exits with another status than the story expects.

To try the terminal interface on the demo superproject, keep it and stop
before the story changes anything. `env.sh` sets `HOME` and the Git
environment of the demo, so source it in a separate shell:

```sh
scripts/demo.sh --keep /tmp/lsm-demo --setup-only
. /tmp/lsm-demo/env.sh
cd /tmp/lsm-demo/firmware
lazysubmodules tui
```

[`examples/`](examples/) contains the complete
[transcript](examples/transcript.txt) of the demo and the
[`.gitmodules`](examples/.gitmodules) and [`.lsm.lock`](examples/.lsm.lock)
it ends with; its [README](examples/README.md) describes the options and
every submodule of the demo.

The animated GIFs in this README are recorded from the same demo with
[VHS](https://github.com/charmbracelet/vhs). `scripts/record-demos.sh`
regenerates them in a container (Podman or Docker); see
[`docs/demo/README.md`](docs/demo/README.md).

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

![The tag pattern dialog of the terminal interface. The pattern of kernel
changes from v6.6.* to v6.*, and the dialog counts the matching local tags
while it is typed, including the pre-releases v6.7-rc1 and v6.6.11-rc1.
After the confirmation, the update picks v6.6.10 and skips the
pre-releases, and lazysubmodules status shows v6.6.10 as the locked
tag.](docs/demo/tag-pattern.gif)

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

[`examples/.gitmodules`](examples/.gitmodules) is a commented example with
every tracking mode.

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

The binary is statically linked and contains the Go standard library and
a few Go modules under the MIT and BSD-3-Clause licenses. Their copyright
notices and license texts come with every release archive and package, as
described below.

### Release archives

Each [GitHub release](https://github.com/FPGArtktic/lazysubmodules/releases)
has a `lazysubmodules_<version>_linux_<arch>.tar.gz` archive for `amd64` and
`arm64`. It contains the binary, `LICENSE`, `README.md`, and the license
notices of the third-party code in the binary: `THIRD_PARTY_NOTICES` lists
each module with its version and license, and `licenses/` holds the
license texts.

```sh
VERSION=1.2.3   # release version without the leading "v"
ARCH=amd64      # or arm64
BASE="https://github.com/FPGArtktic/lazysubmodules/releases/download/v${VERSION}"
curl -fLO "${BASE}/lazysubmodules_${VERSION}_linux_${ARCH}.tar.gz"
tar -xzf "lazysubmodules_${VERSION}_linux_${ARCH}.tar.gz"
sudo install -m 0755 lazysubmodules /usr/local/bin/lazysubmodules
sudo ln -s lazysubmodules /usr/local/bin/lsm   # optional short name
```

Keep `LICENSE`, `THIRD_PARTY_NOTICES` and `licenses/` with the binary when
you pass it on. See [Verifying releases](#verifying-releases) to check the
download first.

### Debian and RPM packages

Each release also has `.deb` and `.rpm` packages for `amd64` and `arm64`,
named `lazysubmodules_<version>_linux_<arch>.deb` and
`lazysubmodules_<version>_linux_<arch>.rpm`. They depend on `git`, which
the package manager installs when it is missing, and install:

- `/usr/bin/lazysubmodules` and the short name `/usr/bin/lsm` (a symlink);
- `README.md` under `/usr/share/doc/lazysubmodules/`;
- **`.deb`:** `LICENSE` in the same directory, and the Debian copyright
  file `/usr/share/doc/lazysubmodules/copyright` with the notices and
  license texts of the third-party code;
- **`.rpm`:** `LICENSE`, `THIRD_PARTY_NOTICES` and `licenses/` under
  `/usr/share/licenses/lazysubmodules/` (`rpm -qL lazysubmodules` lists
  the license files).

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
CI installs the `amd64` packages built from every change in Debian and
Fedora, runs them and checks the installed license notices.

### Arch Linux (AUR)

An AUR package, `lazysubmodules-git`, is planned but not published yet.
Until this section links to it, a package of that name in the AUR does not
come from this project.

Its recipe is in this repository,
[`packaging/aur/lazysubmodules-git/PKGBUILD`](packaging/aur/lazysubmodules-git/PKGBUILD),
so you can already build and install the package locally with `makepkg`
(from the `base-devel` group):

```sh
git clone https://github.com/FPGArtktic/lazysubmodules.git
cd lazysubmodules/packaging/aur/lazysubmodules-git
makepkg -si
```

- **Source:** the recipe clones the default branch of the GitHub
  repository; it does not build your local checkout. It builds with the
  vendored Go modules, downloads none, and runs the test suite.
- **Dependencies:** `-s` installs the build dependencies (`go`,
  `go-licenses`, and `openssh` for the tests) with pacman, and `-i`
  installs the package.
- **Contents:** `/usr/bin/lazysubmodules`, the short name `/usr/bin/lsm`,
  the README under `/usr/share/doc/lazysubmodules-git/`, and `LICENSE`,
  `THIRD_PARTY_NOTICES` and `licenses/` under
  `/usr/share/licenses/lazysubmodules-git/`. The recipe builds no
  `lazysubmodules-git-debug` package, whatever `makepkg.conf` says: it
  builds with `-trimpath`, so the binary contains no paths of the build
  directory, and without those paths a debug package could not carry
  the sources.
- **Version:** derived from the Git history, for example
  `0.1.0.r3.g1234abc` for the third commit after `v0.1.0`, or
  `r54.3cf609f` before the first release. `lazysubmodules version` prints
  it, with the commit and the commit date.

Meanwhile, you can also use a [release archive](#release-archives) or
[Go](#go).

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

![A terminal session with the command line interface: lazysubmodules
status prints a table of all submodules and their states; status
--porcelain=v1, laid out with column, shows the header line and the
tab-separated records; update --dry-run lists the planned changes;
update --commit updates kernel and u-boot and prints the new commit; git
log shows the generated commit message with one block per submodule and
the Signed-off-by line.](docs/demo/cli.gif)

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

![A refused update: kernel is behind, app has an uncommitted change and
fresh was never cloned. update kernel app fresh prints both refusals and
exits with status 3, and status shows that nothing changed, not even
kernel. After the change in app is discarded, update --fetch clones fresh
through the mirror and updates kernel.](docs/demo/safety.gif)

`update` prints one line per submodule. The left side is what the
superproject records (in the index, or in `HEAD` with `--commit`): the
locked ref and commit, `<commit> (unlocked)` without a lock entry, or
`none` without a gitlink. The right side is the target:

```text
kernel: v6.6.9 (8106f61) -> v6.6.10 (08dcd0d)
u-boot: up to date
theme: v1.0.0 (05f49f3), initialized
sdk: tag-pattern v3.0.0-rc.2 (6308203) -> tag v2.9.0 (68a8743)
committed 613148ee5508e55f3702a142d5764a51d0d7bac4
```

- **Notes:**
  - `initialized` or `cloned` for a submodule that was not checked out;
  - `HEAD was <commit>` when the submodule was checked out at a commit
    other than the recorded one;
  - `recorded again` when the superproject records the same commit
    again, for example after `set` changed the ref but not the commit;
  - `restored .lsm.lock`, `restored .gitmodules` or
    `restored .gitmodules and .lsm.lock` when the superproject records
    the target already, but the working tree copy of the file does not:
    the lock entry of the submodule differs or is missing, or its native
    `branch` key does not follow the tracking mode. The update rewrites
    that entry or key to what the superproject records;
  - `discarded the staged change` (only with `--commit`) when `HEAD`
    records the target already, but the index holds a different gitlink,
    lock entry or tracking keys for the submodule. The update stages what
    `HEAD` records in their place, so the staged change is lost (see
    [`update --commit`](#update---commit)).

  The mode is shown on both sides when it changes.
- **Dry run:** each line starts with `would update`, and the notes read
  `initialize`, `clone`, `HEAD is <commit>`, `record again`,
  `restore <files>` and `discard the staged change`.
- **Commit:** with `--commit`, the last line is `committed <commit>` or
  `nothing to commit`. A dry run prints `would commit "<subject>"`,
  `would commit the result` when a target is not known before a clone, or
  `nothing to commit`.
- **Nothing selected:** without managed submodules, `update` prints
  `no managed submodules`.

This output is not a stable interface; scripts should use
`status --porcelain=v1`.

#### `update --commit`

- **Unrelated changes:** the commit may contain only the update. `update`
  refuses (exit code 3) when other paths than `.gitmodules`, `.lsm.lock`
  and the selected submodules are staged, or when `.gitmodules` or
  `.lsm.lock` differ from `HEAD` outside the sections of the selected
  submodules, staged or not. It also refuses while the index has
  unresolved merge conflicts.
- **Commit:** the commit is created with `git commit -s`, so the
  `Signed-off-by` line comes from `user.name` and `user.email`. Commit hooks
  run as configured.
- **What the commit records:** one commit per invocation. The commit and
  its message cover only the submodules whose gitlink, lock entry or
  tracking keys (`lsm-mode`, `lsm-ref`, the native `branch` key) change
  compared with `HEAD`.
- **Left out:** a submodule that is only initialized, cloned, or checked
  out at the commit that `HEAD` records is still updated, but it is
  neither in the commit nor in the message. The same applies when the
  update only rewrites its entries in `.gitmodules` or `.lsm.lock` to
  what `HEAD` records (`restored …`).
- **Nothing to commit:** without such a change, no commit is made and
  `update` prints `nothing to commit`.
- **Staged changes of a selected submodule:** the update stages the
  target of each selected submodule: its gitlink, its lock entry and its
  tracking keys, replacing whatever was staged for them. When `HEAD`
  records the target already, the submodule is left out of the commit,
  so a different staged value is discarded without being committed; the
  line then says `discarded the staged change`. Run
  `update --dry-run --commit` first to see it.

For example, when kernel moves to a new tag and theme is only
initialized, `update --commit theme kernel` creates a commit for kernel
alone. For one submodule the message looks like this:

```text
manifest: update kernel to v6.6.10

Tracking mode: tag-pattern v6.6.*
Old: 8106f614767a (v6.6.9)
New: 08dcd0dc8f98 (v6.6.10)

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
- **Long subjects:** a subject longer than 75 characters, or one that
  would break another subject rule (trailing punctuation or white space,
  the word "WIP" in any case), is shortened to `manifest: update <name>`,
  or to `manifest: update 1 submodule`.
- **Unusual characters:** a name or ref that is not valid UTF-8, or that
  contains quotes, backslashes or characters that are not printable (such
  as tabs or line separators), is quoted, as in the other output. The
  heading always quotes the name, and writes a space that is followed by
  another space as `\x20`.
- **Line length:** a body line longer than 75 columns continues on the
  next line, indented by two more spaces: the ref, or in a heading the
  quoted name, moves there. A ref or name that is still too long is split
  across several such lines, never after a space:

  ```text
  Submodule
    "a-submodule-whose-name-is-far-too-long-to-fit-on-one-line-with-the-headi
    ng":
  ```

  With the subject rules above, the message passes the `.gitlint` rules
  whatever the names and refs are.

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

![A release tag moved upstream: fpga.core follows tag v2.3.1 and is ok.
The upstream repository moves the tag to another commit. After
lazysubmodules fetch, status reports drift, and verify names the moved tag
and exits with status 4.](docs/demo/drift.gif)

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
commit date, the date of that commit in UTC. It is not the build date, so
a rebuild of the same commit prints the same:

```text
lazysubmodules 1.2.3
commit: <commit SHA>
date: <commit date, such as 2026-09-17T11:14:07Z>
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
  confirmation first. The question for `u` and `U` also says when the
  update changes nothing that the index (`u`) or `HEAD` (`U`) records, so
  that nothing is staged or committed: when it only initializes the
  submodule, checks out the recorded commit, or rewrites the working tree
  copy of `.gitmodules` or `.lsm.lock` to what is recorded. With `U`, it
  also says when a change staged for the submodule is discarded, as
  [`update --commit`](#update---commit) does. `b`, `t` and `p` change only
  the tracking configuration, like `lazysubmodules set`; press `u` or `U`
  afterwards to update.
- **Outcome:** the status bar says what happened, for example
  `kernel: updated to v6.6.10 (08dcd0d), staged`,
  `theme: initialized at v1.0.0 (05f49f3)`, or
  `sdk: .lsm.lock restored to v2.9.0 (68a8743)`. It adds
  `staged change discarded` when `U` discarded a staged change, and ends
  with `staged`, `committed <commit>` or `nothing to commit` only when
  that applies.
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
- **Release tags** are signed as well (`git tag -s`), and the release
  workflow publishes nothing for a tag without a good signature from a
  maintainer key. Check a tag with `git tag -v v<version>`, given the
  maintainer's public key.

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
(`test`, `lint`, `snapshot`, ...). A binary you build yourself does not
come with the third-party license notices; `scripts/third-party-licenses.sh`
collects them (see
[Third-party notices](CONTRIBUTING.md#third-party-notices)).

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
[Developer Certificate of Origin](DCO). Bug reports and feature requests
go to the
[issue tracker](https://github.com/FPGArtktic/lazysubmodules/issues); its
forms ask for the details that help, such as the output of
`lazysubmodules version` and `status --porcelain=v1`.

## Security

Do not report vulnerabilities in public issues. [SECURITY.md](SECURITY.md)
describes how to report them privately, what to include and what to
expect.

## License

LazySubmodules is free software: you can redistribute it and/or modify it
under the terms of the GNU General Public License, version 3 only
(`GPL-3.0-only`), as published by the Free Software Foundation. See
[LICENSE](LICENSE) for the full text. The same license covers the
documentation and the images in [`docs/`](docs/), such as the demo
recordings.

The release binaries also contain the Go standard library and Go modules
under the BSD-3-Clause and MIT licenses. Their notices and license texts
ship with every archive and package (`THIRD_PARTY_NOTICES` and
`licenses/`, or the Debian `copyright` file), as described in
[Installation](#installation).

## Author

Mateusz Okulanis <FPGArtktic@outlook.com>
