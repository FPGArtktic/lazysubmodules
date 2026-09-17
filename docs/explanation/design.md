<!-- SPDX-License-Identifier: GPL-3.0-only -->
<!-- Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com> -->

(expl-design)=

# Design and limitations

What LazySubmodules is for, how it stays compatible with Git, and what it
deliberately does not do.

## The gap in native Git

Git can make a submodule follow a branch: set `submodule.<name>.branch`
and run `git submodule update --remote`. It has no way to say "this
submodule follows tag `v2.3.1`" or "this submodule follows the newest
`v6.6.*` release". Projects that pin dependencies to releases, such as
firmware, embedded Linux or FPGA designs, update those submodules by
hand: look up the newest tag, check it out, stage the gitlink, and write
a commit message that says what changed.

LazySubmodules automates exactly that, and adds a lock file so that the
superproject remembers which tag led to which commit.

## Compatible by construction

The design goal is that a superproject stays an ordinary Git
superproject:

- **Namespaced keys.** The tracking configuration uses the keys
  `lsm-mode` and `lsm-ref` in `.gitmodules`. Git ignores unknown keys,
  and the prefix avoids collisions with keys that Git may add later.
- **The native `branch` key** is written for branch mode and removed for
  the other modes, so `git submodule update --remote` does the right
  thing for branch-tracking submodules.
- **Ordinary gitlinks.** The superproject records submodule commits as
  usual. A clone works with `git submodule update --init` for anyone who
  does not use LazySubmodules.
- **Plain files.** `.lsm.lock` has the same git-config format as
  `.gitmodules`, is read and written with `git config -f`, and diffs and
  merges like any text file.
- **Native keys stay.** `update`, `ignore`, `shallow` and other keys are
  left as they are.

See [Work with plain Git](../guide/plain-git.md) for what this means in
practice.

(expl-design-git)=

## Git compatibility

LazySubmodules needs Git 2.39 or later, the version in Debian 12
(bookworm). It uses no Git feature added after 2.39:

- **Classic `git config` options.** It uses `--get`, `--unset-all` and
  `--list` rather than the subcommands that Git 2.46 added.
- **Tested on both ends.** The test suite runs against Git 2.39.5 and
  against a current Git, and the demo gives the same transcript on Git
  2.39.5 and 2.55.0.
- **Locale-independent.** Every `git` invocation sets `LC_ALL=C`, so the
  output that LazySubmodules parses is always in English.
- **SHA-256 repositories** work: commit IDs are 40 or 64 hex digits, and
  nothing assumes a length.

Git's own messages, such as clone progress, go to standard error and
may differ between Git versions.

## Two interfaces over one core

- **The command line** is for scripts and for people who prefer
  commands. Its porcelain format and exit codes are stable.
- **The terminal interface** is for browsing, previewing and updating
  interactively. It contains no business logic: every action calls the
  same core as the command line.

Both are one binary, `lazysubmodules`, with `lsm` as an optional short
name.

(expl-design-stable)=

## Stable interfaces

These are part of the user interface, and the project keeps them
unchanged:

- **Porcelain v1:** the header line, the field count, order and meaning,
  the state names and the quoting rule of `status --porcelain=v1`. An
  incompatible change would come as `--porcelain=v2`, next to v1. See
  {ref}`ref-porcelain`.
- **Exit codes** 0 to 5; see {ref}`ref-exit-codes`.
- **The `.gitmodules` keys** `lsm-mode` and `lsm-ref`.
- **The network policy:** only `fetch`, `update --fetch` and `add` use the
  network.

Everything else, such as the table layout of `status`, the wording of
messages and the output of `update`, is meant for people and may change.

## Scope

LazySubmodules is deliberately small:

- **Not a replacement** for `repo`, `west`, `git subtree` or monorepo
  tooling. It manages the submodules of one superproject; it does not
  assemble a workspace from a separate manifest.
- **No credential management.** Git uses its configured credential
  helpers, SSH agent and keys.
- **No hosting integration.** It does not talk to GitHub, GitLab or any
  other service; it talks to Git remotes through `git`.

(expl-design-limitations)=

## Known limitations

- **Nested submodules** are not managed recursively: submodules inside a
  managed submodule are neither initialized nor updated. After an update,
  a nested submodule may stay at a commit other than the one the new
  commit records. That alone does not make the managed submodule
  `dirty`; modified files inside the nested submodule do. See
  {ref}`guide-foreach-nested` for a workaround.
- **Linux only**, on `amd64` and `arm64`. Windows and macOS are not
  supported.
- **The remote is always `origin`.** Branch tracking resolves
  `refs/remotes/origin/<branch>`, and `fetch` fetches from `origin`.
- **Local resolution.** `status`, `verify` and `update` without `--fetch`
  see only what was fetched before.
- **Git 2.39 or later** is required.
- **No prompts in the terminal interface.** Git cannot ask for
  credentials or host keys there; see {ref}`guide-network-tui`.

## See also

- [How LazySubmodules works](how-it-works.md)
- [Safety model](safety.md)
- [FAQ](../project/faq.md)
