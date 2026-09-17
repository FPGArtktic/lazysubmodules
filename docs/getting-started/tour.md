<!-- SPDX-License-Identifier: GPL-3.0-only -->
<!-- Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com> -->

(start-tour)=

# Tour of the demo

A superproject with fourteen submodules, built offline, that shows every
tracking mode and every state.

The repository contains `scripts/demo.sh`. It builds a firmware
superproject from local repositories and walks through LazySubmodules on
it in twelve short chapters. It is also an end-to-end test: every command
must exit with the status the story expects, or the demo fails.

## Run the demo

The demo needs Bash, Git 2.39 or later and a `lazysubmodules` binary.
For the signed tag in chapter 10 it also needs `ssh-keygen`.

```sh
git clone https://github.com/FPGArtktic/lazysubmodules.git
cd lazysubmodules
scripts/build-in-container.sh build   # binary in bin/lazysubmodules
scripts/demo.sh                       # press Enter between the chapters
```

With a host Go toolchain, `go build -o bin/lazysubmodules ./cmd/lazysubmodules`
builds the binary instead. By default the demo runs `bin/lazysubmodules`,
or else `lazysubmodules` from `PATH`.

| Option | Effect |
|---|---|
| `--keep DIR` | Build the demo in `DIR`, which must be new or empty, and keep it |
| `--setup-only` | Build and describe the superproject, then stop; needs `--keep` |
| `--no-pause` | Do not wait for Enter between the chapters |
| `--transcript FILE` | Also write the output to `FILE`, with the demo directory shown as `$DEMO` |
| `--binary PATH` | The `lazysubmodules` binary to run |

## What the demo isolates

- **Your configuration:** Git runs without your user and system
  configuration, and `HOME` and `TMPDIR` point into the demo directory.
- **The network:** Git may use only the file transport. Every submodule
  URL starts with `https://git.example.invalid/`, a host that does not
  exist, and `url.<base>.insteadOf` rewrites it to bare repositories in
  `DIR/mirror`, the way a company mirror is set up.
- **Randomness:** identities and dates are fixed, so the commit IDs and the
  output are the same in every run.

Without `--keep`, everything happens in a temporary directory that is
removed at the end.

## The superproject

| Submodule | Path | Tracks | Situation at the start |
|---|---|---|---|
| `kernel` | `kernel` | tag pattern `v6.6.*` | Locked at `v6.6.9`; `v6.6.10` and the pre-releases `v6.6.11-rc1` and `v6.7-rc1` exist |
| `u-boot` | `bootloader/u-boot` | branch `main` | `origin/main` has moved on |
| `fpga.core` | `ip/fpga-core` | tag `v2.3.1` | Name with a dot; the demo moves the tag upstream |
| `crypto lib` | `libs/crypto lib` | a commit | Name and path with a space |
| `legacy` | `vendor/legacy` | nothing | Unmanaged |
| `tools` | `tools/nested` | branch `develop` | Its nested submodule is at another commit |
| `theme` | `docs/theme` | tag `v1.0.0` | Deinitialized; its repository in `.git/modules` is kept |
| `fresh` | `third_party/fresh` | tag pattern `v1.*` | Never cloned |
| `app` | `apps/app` | tag `v1.2.0` | Uncommitted change |
| `sdk` | `sdk` | tag pattern `v*` | Locked at the release candidate `v3.0.0-rc.2` |
| `mirror-lib` | `libs/mirror` | branch `stable` | A branch other than `main` |
| `broken` | `libs/broken` | tag `v9.9.9` | The tag does not exist |
| `signed` | `libs/signed` | tag `v1.0.0` | The tag is SSH-signed |
| `quirky` | `libs/quirky` | tag `v1.0.0` | `update = none`, `ignore = all`, `shallow = true` |

## Explore it yourself

Keep the demo and stop before the story changes anything. `env.sh` sets
`HOME` and the Git environment of the demo, so source it in a separate
shell:

```sh
scripts/demo.sh --keep /tmp/lsm-demo --setup-only
. /tmp/lsm-demo/env.sh
cd /tmp/lsm-demo/firmware
```

`DIR/firmware` is the superproject, `DIR/mirror` holds the upstream
repositories (bare), and `DIR/work` is where their history was written.
Every command on this site that uses the demo names runs in such a
shell.

`lazysubmodules status` shows every state at once. It reads local refs
only, so it works offline:

```{literalinclude} ../../examples/transcript.txt
:language: console
:start-at: "$ lazysubmodules status"
:end-at: "[exit status 0: success]"
```

The `[exit status N: …]` lines come from the demo, not from
LazySubmodules. The states mean:

- [behind]{.lsm-state .lsm-state-behind}: `kernel` has a newer `v6.6.x`
  tag, and `origin/main` of `u-boot` has moved on.
- [uninitialized]{.lsm-state .lsm-state-uninitialized}: `theme` was
  deinitialized, `fresh` was never cloned.
- [dirty]{.lsm-state .lsm-state-dirty}: `app` has an uncommitted change.
- [missing-ref]{.lsm-state .lsm-state-missing-ref}: `broken` tracks tag
  `v9.9.9`, which does not exist.
- [unmanaged]{.lsm-state .lsm-state-unmanaged}: `legacy` has no
  `lsm-mode` key.
- [ok]{.lsm-state .lsm-state-ok}: `sdk` stays at its release candidate
  because `v3.0.0-rc.2` is still the highest tag matching its pattern, and
  the tag the lock file records is never skipped for being a pre-release;
  see {ref}`expl-resolution-never-downgrade`. `tools` is `ok` although
  its nested submodule is at another commit, and `quirky` although
  `.gitmodules` says `update = none` and `ignore = all`.

The seventh state, [drift]{.lsm-state .lsm-state-drift}, needs a
submodule that moved away from its lock entry; the demo produces it in
chapter 8, when a tag moves upstream.

{ref}`ref-states` defines every state and the order in which they are
checked. The machine-readable form of the same table is described in
{ref}`ref-porcelain`.

## Try the terminal interface

In the same shell, start the terminal interface:

```sh
lazysubmodules tui
```

:::{container} lsm-terminal

```{image} ../demo/hero.gif
---
alt: >-
  The LazySubmodules terminal interface on a superproject with fourteen
  submodules: a table with the name, mode, ref, lock and colored state of
  each submodule, and a preview of the selected one. The recording moves
  through the table, opens the details of u-boot, updates u-boot after a
  confirmation until its state turns ok, and shows the help page.
loading: lazy
---
```

:::

Move to `u-boot` with {kbd}`j`, press {kbd}`u`, and confirm with {kbd}`y`
once the planned update is shown. [Use the terminal interface](../guide/tui.md)
explains the rest.

## The chapters

Each chapter corresponds to a guide on this site:

1. **The superproject:** history, `.gitmodules` and `.lsm.lock`
   ({ref}`ref-files`).
2. **`status`:** every state at once ({ref}`ref-states`).
3. **`status --porcelain=v1`** and a script that uses it
   ([Scripts and CI](../guide/scripting.md)).
4. **`set`:** an unmanaged submodule is refused, then put under control
   ([Change what a submodule tracks](../guide/change-tracking.md)).
5. **`update --dry-run`**, with and without `--include-prerelease`
   ([Update submodules](../guide/update.md)).
6. **A refused `update`** (exit status 3), and how to fix each problem
   ([Update submodules](../guide/update.md)).
7. **`update --commit`** for a selection, and the generated commit message
   ([Commit updates](../guide/commit-updates.md)).
8. **A tag moved upstream:** `fetch`, `drift` and `verify` (exit status 4)
   ([Detect moved tags](../guide/moved-tags.md)).
9. **`update --fetch`** clones a submodule through the mirror
   ([Mirrors, credentials and offline work](../guide/network.md)).
10. **`verify --signatures`**
    ([Verify signatures](../guide/signatures.md)).
11. **`foreach`**, including a failing command
    ([Run a command in every submodule](../guide/foreach.md)).
12. **The final state:** every submodule is `ok`.

The complete output is in
[`examples/transcript.txt`](https://github.com/FPGArtktic/lazysubmodules/blob/main/examples/transcript.txt),
and the `.gitmodules` and `.lsm.lock` the demo ends with are in
[`examples/`](https://github.com/FPGArtktic/lazysubmodules/tree/main/examples).
To run the whole story without stopping and keep the result:

```sh
scripts/demo.sh --keep /tmp/lsm-demo-full --no-pause
```
