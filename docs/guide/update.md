<!-- SPDX-License-Identifier: GPL-3.0-only -->
<!-- Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com> -->

(guide-update)=

# Update submodules

Move managed submodules to their targets, stage the result, and find out
why an update was refused.

## The workflow

```sh
lazysubmodules fetch                 # download branches and tags (network)
lazysubmodules update --dry-run      # show what would change
lazysubmodules update                # check out, write .lsm.lock, stage
git commit -s                        # or: lazysubmodules update --commit
```

`update` resolves targets in the refs that already exist locally, so
fetch first. `update --fetch` combines both steps.

```text
lazysubmodules update [<name>...] [--fetch] [--dry-run] [--commit] [--include-prerelease]
```

- **Selection.** Without names, `update` works on every managed submodule.
  With names, only on those. Either way, submodules are processed and
  reported in `.gitmodules` order.
- **Unmanaged submodules** are skipped when no names are given, and
  refused (exit status 3) when named.

| Option | Effect |
|---|---|
| `--fetch` | Fetch from `origin` first, and clone uninitialized submodules when needed |
| `--dry-run` | Print the plan only. Never fetches, initializes or clones, even with `--fetch` |
| `--commit` | Create one commit with the result; see [Commit updates](commit-updates.md) |
| `--include-prerelease` | Let tag patterns select pre-release tags |

## Preview with a dry run

A dry run resolves the targets in the local refs and changes nothing:

```{literalinclude} ../../examples/transcript.txt
:language: console
:start-at: "$ lazysubmodules update --dry-run kernel u-boot theme sdk"
:end-at: "[exit status 0: success]"
```

- `kernel` would move to `v6.6.10`: version sorting puts it above
  `v6.6.9`, and `v6.6.11-rc1` is a pre-release.
- `theme` would be initialized from the repository that
  `git submodule deinit` left behind, without network access.
- `sdk` keeps its release candidate, because `v3.0.0-rc.2` is still its
  highest matching tag and the locked tag is never skipped for being a
  pre-release.

Pre-release tags count only on request:

```{literalinclude} ../../examples/transcript.txt
:language: console
:start-at: "$ lazysubmodules update --dry-run --include-prerelease kernel sdk"
:end-at: "[exit status 0: success]"
```

The `[exit status N: …]` lines come from the demo, not from
LazySubmodules.

A dry run never uses the network, even with `--fetch`. A submodule that
would need a clone is listed with the target `unknown until fetched`:

```console
$ lazysubmodules update --dry-run --fetch fresh
would update fresh: v1.1.0 (f2de3e1) -> unknown until fetched, clone
```

## Update and stage

Without `--dry-run`, `update` checks out each target, writes the lock
entries and stages the gitlinks, `.gitmodules` and `.lsm.lock`:

```console
$ lazysubmodules update kernel u-boot
kernel: v6.6.9 (8106f61) -> v6.6.10 (08dcd0d)
u-boot: main (29b295c) -> main (8e9fc7a)
$ git status --short
M  .lsm.lock
 m apps/app
M  bootloader/u-boot
M  kernel
 m tools/nested
```

The submodule HEAD is detached at the target commit, which is normal for
submodules. Commit the staged result yourself, or use `--commit`.

:::{important}
`verify` checks the committed state. Until the staged update is
committed, it fails for the updated submodules:

```console
$ lazysubmodules verify kernel
kernel: failed (1 of 7 checks)
  gitlink: HEAD of the superproject records 8106f614767a, locked 08dcd0dc8f98
lazysubmodules: verification failed: kernel
```
:::

## Uninitialized submodules

`update` initializes submodules that are not checked out:

- **Offline**, when the submodule's Git directory still exists, for
  example after `git submodule deinit`. The line then ends with
  `initialized`, as for `theme` in the demo.
- **With a clone**, only when `--fetch` is given. Without it, `update`
  refuses, because a clone uses the network:

```{literalinclude} ../../examples/transcript.txt
:language: console
:start-at: "$ lazysubmodules update fresh"
:end-before: "The clone keeps the URL"
```

## All or nothing

Before it changes anything, `update` checks every selected submodule and
reports every problem, one per line. If a single submodule is refused,
nothing is checked out, written or staged, and the exit status is 3:

```{literalinclude} ../../examples/transcript.txt
:language: console
:start-after: "nothing changes until all selected submodules are safe:"
:end-at: " m tools/nested"
```

The index is untouched; the two ` m` lines are the change in `app` and
the nested submodule of `tools`, which were there before.

:::{container} lsm-terminal

```{image} ../demo/safety.gif
---
alt: >-
  A refused update: kernel is behind, app has an uncommitted change and
  fresh was never cloned. update kernel app fresh prints both refusals and
  exits with status 3, and status shows that nothing changed, not even
  kernel. After the change in app is discarded, update --fetch clones
  fresh through the mirror and updates kernel.
loading: lazy
---
```

:::

`update` refuses when:

- a submodule working tree has uncommitted changes. Untracked files do
  not count, nor does a nested submodule that is only checked out at
  another commit;
- the resolved ref does not exist locally (run `fetch`, or use
  `--fetch`);
- a submodule is not initialized, its Git directory is missing, and
  `--fetch` was not given;
- `--commit` is given and the commit would include unrelated changes, or
  the index has unresolved merge conflicts;
- an unmanaged submodule is named explicitly;
- a submodule cannot be updated safely: its path leads through a symbolic
  link, the index records no submodule at its path, or it would be
  initialized but has no usable URL, or something other than a Git
  repository is in the place of its Git directory.

Fix each problem, then run the update again. In the demo:

```{literalinclude} ../../examples/transcript.txt
:language: console
:start-at: "$ git -C apps/app status --short"
:end-at: "$ git -C apps/app restore NEWS"
```

```{literalinclude} ../../examples/transcript.txt
:language: console
:start-at: "$ git -C libs/broken tag --list"
:end-before: "fresh needs a clone"
```

```{literalinclude} ../../examples/transcript.txt
:language: console
:start-after: "reports only fresh:"
:end-before: "━━ 7."
```

[Troubleshooting](troubleshooting.md) lists every refusal with its fix.

### With `--fetch`

`--fetch` runs checks that need no fetched refs, such as uncommitted
changes, before it fetches. The fetch itself then clones and initializes
submodules as needed, and the remaining checks run on the fetched refs.
A submodule refused at that point still stops the update, but the clones
and fetched refs stay:

```console
$ git -C apps/app restore NEWS
$ lazysubmodules update --fetch
Submodule 'theme' (https://git.example.invalid/theme.git) registered for path 'docs/theme'
Submodule path 'docs/theme': checked out '05f49f3f8b5a18447c1e10f18f28fc7e221d8110'
Submodule 'fresh' (https://git.example.invalid/fresh.git) registered for path 'third_party/fresh'
Cloning into '$DEMO/firmware/third_party/fresh'...
Submodule path 'third_party/fresh': checked out 'f2de3e1ec47a9de1bd0316651efbfce990587c49'
lazysubmodules: broken: refused: ref not found in local refs: tag v9.9.9 does not exist or does not point to a commit
```

Here `theme` and `fresh` are now checked out at the commits the
superproject records, but no submodule was moved to a new target, the
lock file is unchanged and nothing is staged.

(guide-update-output)=

## Reading the output

`update` prints one line per submodule:

```text
kernel: v6.6.9 (8106f61) -> v6.6.10 (08dcd0d)
u-boot: up to date
theme: v1.0.0 (05f49f3), initialized
```

- **Left side:** what the superproject records, in the index, or in
  `HEAD` with `--commit`. It shows the locked ref and commit,
  `<commit> (unlocked)` without a lock entry, `none` without a gitlink,
  and the bare commit when a lock entry exists but the superproject
  records another commit, as after a checkout inside the submodule.
- **Right side:** the target. When the tracking mode changes, both sides
  show the mode.
- **`up to date`:** nothing to do for this submodule.

Notes at the end of a line explain what else happened:

| Note | Meaning |
|---|---|
| `initialized`, `cloned` | The submodule was not checked out before |
| `HEAD was <commit>` | The submodule was checked out at another commit than the recorded one |
| `recorded again` | The superproject records the same commit again, for example after `set` changed the ref but not the commit |
| `restored .lsm.lock`, `restored .gitmodules`, `restored .gitmodules and .lsm.lock` | The superproject records the target already, but the working tree copy of the file does not: the lock entry differs or is missing, or the native `branch` key does not follow the tracking mode. The update rewrites them to what the superproject records |
| `discarded the staged change` | Only with `--commit`: `HEAD` records the target already, and a different staged value was replaced; see [Commit updates](commit-updates.md) |

A dry run starts each line with `would update`, and its notes read
`initialize`, `clone`, `HEAD is <commit>`, `record again`,
`restore <files>` and `discard the staged change`. Without managed
submodules, `update` prints `no managed submodules`.

This output is meant for people and may change. Scripts should read
`status --porcelain=v1` instead; see [Scripts and CI](scripting.md).

## Interrupting an update

{kbd}`Ctrl+C`, `SIGTERM` and `SIGHUP` cancel the update and terminate the
`git` processes it started.

- An update interrupted **before staging** puts back what it changed:
  submodules it already checked out return to their previous commit, and
  nothing is staged.
- **Once the result is staged,** only the commit remains. When
  `git commit` is interrupted or fails, for example in a hook, the update
  stays staged.
- The command exits with the status of the failure, usually 5, because a
  `git` command was killed. A second signal ends LazySubmodules at once,
  without waiting for the clean-up.

See {ref}`ref-runtime-signals` for the details.

## See also

- {ref}`ref-cli-update`: every option and message.
- [Commit updates](commit-updates.md): `--commit` and its message.
- [Safety model](../explanation/safety.md): why `update` refuses.
- [Mirrors, credentials and offline work](network.md): what `--fetch`
  needs.
