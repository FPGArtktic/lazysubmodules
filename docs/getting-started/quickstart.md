<!-- SPDX-License-Identifier: GPL-3.0-only -->
<!-- Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com> -->

(start-quickstart)=

# Quick start

Put an existing submodule under LazySubmodules, update it, commit the
result and check it.

The steps work in any superproject. The examples use the names and the
output of the [demo superproject](tour.md), so you can follow them there
first. Run every command from inside the superproject; any directory of
it works, but not a checked-out submodule: there `lazysubmodules` takes
the submodule for the superproject and reports no submodules.

## 1. See where you stand

```console
$ lazysubmodules status kernel
NAME    PATH    MODE         REF     LOCK              HEAD     STATE
kernel  kernel  tag-pattern  v6.6.*  8106f61 (v6.6.9)  8106f61  behind
```

Without names, `status` lists every submodule. It uses only local refs and
never touches the network. A submodule that LazySubmodules does not manage
yet shows `-` in the `MODE`, `REF` and `LOCK` columns and the state
[unmanaged]{.lsm-state .lsm-state-unmanaged}. {ref}`ref-states` explains
every state.

## 2. Choose what to track

Tell LazySubmodules to follow the newest stable `v6.6.x` tag:

```console
$ lazysubmodules set kernel --tag-pattern 'v6.6.*'
kernel: tracks tag-pattern v6.6.*
```

- Quote glob patterns, so that the shell does not expand them.
- `set` writes `lsm-mode` and `lsm-ref` to `.gitmodules`, and in branch
  mode also Git's own `branch` key; it changes nothing else. In the demo,
  `kernel` already tracks this pattern, so the file stays the same.
- The other tracking modes are `--branch`, `--tag` and `--commit`; see
  [Change what a submodule tracks](../guide/change-tracking.md) and
  [Tracking modes and resolution](../explanation/resolution.md).

## 3. Download branches and tags

```console
$ lazysubmodules fetch kernel
kernel: fetched
```

`fetch` is one of the three commands that use the network. It runs
`git fetch --tags --force --prune origin` in the submodule, and clones the
submodule first if it is not initialized.

## 4. Preview the update

```console
$ lazysubmodules update --dry-run kernel
would update kernel: v6.6.9 (8106f61) -> v6.6.10 (08dcd0d)
```

The left side is what the superproject records now, the right side is the
target. A dry run changes nothing.

## 5. Update and commit

```console
$ lazysubmodules update --commit kernel
kernel: v6.6.9 (8106f61) -> v6.6.10 (08dcd0d)
committed 4bc179bb537bb8dceccaaff5052bac6e1029f9d2
```

`update` checks out the target, writes `.lsm.lock` and stages the gitlink,
`.gitmodules` and `.lsm.lock`. With `--commit` it also creates a commit
with `git commit -s`, whose message describes the update:

```console
$ git log -1 --format=%B
manifest: update kernel to v6.6.10

Tracking mode: tag-pattern v6.6.*
Old: 8106f614767a (v6.6.9)
New: 08dcd0dc8f98 (v6.6.10)

Signed-off-by: Dana Developer <dana@example.org>
```

Steps 3 to 5 also fit into one command:

```sh
lazysubmodules update kernel --fetch --commit
```

Without `--commit`, the update is only staged, and you commit it
yourself. [Update submodules](../guide/update.md) and
[Commit updates](../guide/commit-updates.md) cover the details.

## 6. Check the result

```console
$ lazysubmodules status kernel
NAME    PATH    MODE         REF     LOCK               HEAD     STATE
kernel  kernel  tag-pattern  v6.6.*  08dcd0d (v6.6.10)  08dcd0d  ok
$ lazysubmodules verify kernel
kernel: ok (7 checks)
```

`verify` compares the lock file, the gitlink in the `HEAD` commit and the
checkout, and checks that the locked tag still points to the locked
commit. It exits with status 4 when a check fails, which makes it a good
CI step; see [Scripts and CI](../guide/scripting.md).

## 7. Add a new submodule

`add` clones a repository as a new submodule that is managed from the
start:

```console
$ lazysubmodules add https://git.example.invalid/u-boot.git third_party/u-boot --branch main
Cloning into '$DEMO/firmware/third_party/u-boot'...
third_party/u-boot: added at main (8e9fc7a)
$ git status --short
M  .gitmodules
M  .lsm.lock
 m apps/app
A  third_party/u-boot
 m tools/nested
```

The result is staged but not committed. Commit it as usual, for example
with `git commit -s`. The two ` m` lines are the demo's own changes in
other submodules. See [Add a submodule](../guide/add-submodule.md).

## 8. What your colleagues do

Nothing special. A plain clone works:

```sh
git clone <superproject-url>
cd <superproject>
git submodule update --init
```

`git submodule update --init` checks out the commits the superproject
records, which are the ones LazySubmodules locked. It skips submodules
with `update = none` in `.gitmodules`; `lazysubmodules fetch` clones and
initializes every managed submodule regardless. See
[Work with plain Git](../guide/plain-git.md).

## Next steps

::::{grid} 1 2 2 2
:gutter: 3

:::{grid-item-card} {octicon}`sync` Update submodules
:link: ../guide/update
:link-type: doc
:class-card: lsm-card

Selections, dry runs, refusals and what the output means.
:::

:::{grid-item-card} {octicon}`git-branch` Tracking modes
:link: ../explanation/resolution
:link-type: doc
:class-card: lsm-card

How branches, tags, tag patterns and commits are resolved.
:::

:::{grid-item-card} {octicon}`terminal` Terminal interface
:link: ../guide/tui
:link-type: doc
:class-card: lsm-card

The same work, interactively.
:::

:::{grid-item-card} {octicon}`list-unordered` Command line reference
:link: ../reference/cli/index
:link-type: doc
:class-card: lsm-card

Every command and option.
:::

::::
