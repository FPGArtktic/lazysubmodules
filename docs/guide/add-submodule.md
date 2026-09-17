<!-- SPDX-License-Identifier: GPL-3.0-only -->
<!-- Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com> -->

(guide-add)=

# Add a submodule

Clone a repository as a new submodule that already follows a branch, a
tag, a tag pattern or a commit.

## Synopsis

```text
lazysubmodules add <url> <path> (--branch B | --tag T | --tag-pattern P | --commit SHA) [--include-prerelease]
```

Give exactly one tracking option. `add` clones, so it needs network access
to the URL; everything else it does is local.

## Examples

::::{tab-set}

:::{tab-item} Branch

```console
$ lazysubmodules add https://git.example.invalid/u-boot.git third_party/u-boot --branch main
Cloning into '$DEMO/firmware/third_party/u-boot'...
third_party/u-boot: added at main (8e9fc7a)
```

The submodule follows the tip of `origin/main`. `add` also writes the
native `branch` key, so `git submodule update --remote` follows the same
branch.
:::

:::{tab-item} Tag

```console
$ lazysubmodules add https://git.example.invalid/app.git third_party/app --tag v1.2.0
Cloning into '$DEMO/firmware/third_party/app'...
third_party/app: added at v1.2.0 (cfa9d86)
```

The submodule stays at the commit of `v1.2.0` until you change the
tracking configuration.
:::

:::{tab-item} Tag pattern

```console
$ lazysubmodules add https://git.example.invalid/sdk.git third_party/sdk2 --tag-pattern 'v2.*'
Cloning into '$DEMO/firmware/third_party/sdk2'...
third_party/sdk2: added at v2.9.0 (68a8743)
```

The submodule follows the highest stable tag that matches `v2.*`. Quote
the pattern, so that the shell does not expand it.
:::

:::{tab-item} Pre-release

```console
$ lazysubmodules add https://git.example.invalid/sdk.git third_party/sdk3 --tag-pattern 'v*' --include-prerelease
Cloning into '$DEMO/firmware/third_party/sdk3'...
third_party/sdk3: added at v3.0.0-rc.2 (6308203)
```

`--include-prerelease` lets the pattern select a pre-release tag, here the
release candidate `v3.0.0-rc.2`. Later updates keep it until a newer tag
exists; see {ref}`expl-resolution-never-downgrade`.
:::

:::{tab-item} Commit

```console
$ lazysubmodules add https://git.example.invalid/crypto-lib.git third_party/crypto --commit 5da6927
Cloning into '$DEMO/firmware/third_party/crypto'...
third_party/crypto: added at 5da6927
```

An abbreviated SHA is expanded after the clone; `.gitmodules` records the
full SHA.
:::

::::

The `Cloning into` line comes from Git and goes to standard error. The
last line, on standard output, names the ref and the abbreviated commit
the submodule was added at.

## What `add` does

1. **Clone.** `add` runs `git submodule add`. Branch mode passes
   `-b <branch>`. The submodule name is its path, as with plain Git.
2. **Configure.** It writes `lsm-mode` and `lsm-ref` to `.gitmodules`.
3. **Resolve.** It resolves the ref in the new clone, checks out the
   commit and writes the lock entry to `.lsm.lock`.
4. **Stage.** It stages `.gitmodules`, `.lsm.lock` and the new gitlink. It
   does not commit.

If a step after the clone fails, the new submodule is removed again and
the original error is reported. Nothing is left behind, not even the Git
directory under `.git/modules/`:

```console
$ lazysubmodules add https://git.example.invalid/app.git third_party/nope --tag v9.9.9
Cloning into '$DEMO/firmware/third_party/nope'...
lazysubmodules: third_party/nope: refused: ref not found in local refs: tag v9.9.9 does not exist or does not point to a commit
```

The exit status is 3.

## After `add`

The result is staged:

```console
$ git status --short
M  .gitmodules
M  .lsm.lock
 m apps/app
A  third_party/sdk2
 m tools/nested
```

The ` m` lines are unrelated changes of the demo. Review the staged
change and commit it with your usual workflow:

```sh
git diff --cached -- .gitmodules .lsm.lock
git commit -s -m "add the sdk 2.x series"
```

The new submodule's name equals its path, so later commands take the
path as the name:

```console
$ lazysubmodules status third_party/sdk2
NAME              PATH              MODE         REF   LOCK              HEAD     STATE
third_party/sdk2  third_party/sdk2  tag-pattern  v2.*  68a8743 (v2.9.0)  68a8743  ok
```

:::{tip}
A staged `add` is part of the commit that `update --commit` makes when
the update covers the new submodule too, which it does without names.
When you name other submodules instead, the staged `add` is an unrelated
change and the commit is refused, so commit the new submodule first; see
[Commit updates](commit-updates.md).
:::

## Path rules

`<path>` must be a relative path inside the superproject. A path outside
it is a usage error (exit status 2):

```console
$ lazysubmodules add https://git.example.invalid/app.git ../outside --tag v1.2.0
lazysubmodules: invalid path "../outside": must be a relative path inside the superproject
```

A path that is already taken is refused (exit status 3) before anything
is cloned:

| Situation | Message |
|---|---|
| The path exists | `src: refused: path already exists` |
| The path belongs to another submodule | `kernel: refused: path already exists: used by submodule kernel` |
| The path lies inside a submodule | `kernel/sub: refused: path already exists: inside submodule kernel` |
| The path leads through a file | `src/drivers/README/x: refused: path already exists: it leads through a file` |
| The path leads through a symbolic link | `lnk/app: refused: submodule path contains a symbolic link` |

Every message starts with `lazysubmodules: `. Missing parent directories
are fine: `add` creates them, as `git submodule add` does.

## Option errors

Exactly one tracking option is required. Both mistakes are usage errors
(exit status 2):

```console
$ lazysubmodules add https://git.example.invalid/app.git third_party/x
lazysubmodules: add: one of --branch, --tag, --tag-pattern or --commit is required
Run 'lazysubmodules help' for usage.
$ lazysubmodules add https://git.example.invalid/app.git third_party/x --tag v1 --branch main
lazysubmodules: add: only one of --branch, --tag, --tag-pattern or --commit may be given
Run 'lazysubmodules help' for usage.
```

## See also

- {ref}`ref-cli-add`: the complete reference.
- [Change what a submodule tracks](change-tracking.md): switch the mode or
  the ref later.
- [Mirrors, credentials and offline work](network.md): how the clone
  reaches the URL.
