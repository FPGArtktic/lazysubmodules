<!-- SPDX-License-Identifier: GPL-3.0-only -->
<!-- Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com> -->

(guide-plain-git)=

# Work with plain Git

LazySubmodules stores its data where Git ignores it, so collaborators and
tools without it keep working.

## Why plain Git keeps working

- **Namespaced keys.** The tracking configuration lives in `.gitmodules`
  under `lsm-mode` and `lsm-ref`. Git ignores keys it does not know, and
  the `lsm-` prefix avoids collisions with future Git keys.
- **Ordinary gitlinks.** The superproject records submodule commits
  exactly as plain Git does. `.lsm.lock` is an extra file next to them.
- **The native `branch` key.** For branch-tracking submodules,
  LazySubmodules also writes `submodule.<name>.branch`, which
  `git submodule update --remote` reads.
- **Native keys stay.** `update`, `ignore`, `shallow` and the other keys
  that Git reads are left as they are.

LazySubmodules is not a replacement for `repo`, `west`, `git subtree` or
monorepo tooling. It manages ordinary submodules and nothing else.

## Clone a superproject

A colleague without LazySubmodules clones as usual:

```sh
git clone <superproject-url>
cd <superproject>
git submodule update --init
```

`git submodule update --init` checks out the commits that the
superproject records, which are the commits LazySubmodules locked. The
exception is a submodule with `update = none` in `.gitmodules`, which Git
skips:

```console
$ git submodule update --init
…
Skipping submodule 'libs/quirky'
```

With LazySubmodules, `lazysubmodules fetch` clones, initializes and
fetches every managed submodule, including such submodules. That is the
better choice for CI; see [Scripts and CI](scripting.md).

## `git submodule update --remote`

For branch-tracking submodules, `git submodule update --remote` does what
`lazysubmodules update` does, except for the lock file: it moves the
submodule to the tip of the configured branch.

:::{warning}
For submodules in `tag`, `tag-pattern` or `commit` mode there is no
native `branch` key, so `git submodule update --remote` moves them to the
tip of the remote's default branch instead. Name only branch-tracking
submodules, by path, when you use it.
:::

On a clone of the finished demo, `kernel` follows the tag pattern
`v6.6.*`. `--remote` moves it to the tip of `origin/HEAD`, a pre-release
commit, and LazySubmodules reports the difference:

```console
$ git submodule update --remote kernel bootloader/u-boot
Submodule path 'kernel': checked out '8ec67d38d377155f9933e80b3b64063a28aa4de7'
$ lazysubmodules status kernel u-boot
NAME    PATH               MODE         REF     LOCK               HEAD     STATE
kernel  kernel             tag-pattern  v6.6.*  08dcd0d (v6.6.10)  8ec67d3  drift
u-boot  bootloader/u-boot  branch       main    8e9fc7a            8e9fc7a  ok
$ git -C kernel log -1 --format=%s
Linux v6.7-rc1
```

`u-boot` did not move, because it already was at the tip of
`origin/main`. Git takes paths here, not submodule names:
`git submodule update --remote u-boot` fails with
`error: pathspec 'u-boot' did not match any file(s) known to git`.

To undo the change, check out the recorded commit again:

```console
$ git submodule update kernel
Submodule path 'kernel': checked out '08dcd0dc8f98ab3da3943153cad056f346d18ded'
$ lazysubmodules status kernel
NAME    PATH    MODE         REF     LOCK               HEAD     STATE
kernel  kernel  tag-pattern  v6.6.*  08dcd0d (v6.6.10)  08dcd0d  ok
```

When a commit made with plain Git changes a gitlink without updating
`.lsm.lock`, `verify` fails until someone runs `lazysubmodules update`
for that submodule:

```console
$ git submodule update -q --remote kernel
$ git commit -q -m "move kernel" kernel
$ lazysubmodules verify kernel
kernel: failed (2 of 7 checks)
  gitlink: HEAD of the superproject records 8ec67d38d377, locked 08dcd0dc8f98
  head: HEAD 8ec67d38d377 differs from locked commit 08dcd0dc8f98
lazysubmodules: verification failed: kernel
```

(guide-plain-git-migrate)=

## Migrate from `git submodule update --remote`

A superproject that already follows branches with
`submodule.<name>.branch` needs one `set` per submodule. This loop puts
every unmanaged submodule with a native `branch` key under
LazySubmodules, tracking the same branch:

```sh
git config -z -f .gitmodules --get-regexp '^submodule\..*\.branch$' |
while IFS= read -r -d '' entry; do
	key=${entry%%$'\n'*}      # submodule.<name>.branch
	branch=${entry#*$'\n'}
	name=${key#submodule.}
	name=${name%.branch}
	if ! git config -f .gitmodules --get "submodule.$name.lsm-mode" >/dev/null; then
		lazysubmodules set --branch "$branch" -- "$name"
	fi
done
```

- `git config -z` separates entries with NUL and key from value with a
  newline, so names with spaces or dots are safe.
- `--` ends the options of `set`, so a name that starts with `-` is safe
  as well.

Then record the current state in the lock file and commit it:

```sh
lazysubmodules update --dry-run      # review
lazysubmodules update --commit
```

The first update records every newly managed submodule. A submodule
without a lock entry shows `(unlocked)` on the left side, and its
checkout moves to the tip of the branch as of your last fetch, just as
`git submodule update --remote` would move it.

After that, move release dependencies from branches to tags or tag
patterns one at a time, with `lazysubmodules set <name> --tag-pattern
'<pattern>'`; see [Change what a submodule tracks](change-tracking.md).

## Stop using LazySubmodules for a submodule

Remove the tracking keys and the lock entry, then commit both files:

```sh
git config -f .gitmodules --unset submodule.sdk.lsm-mode
git config -f .gitmodules --unset submodule.sdk.lsm-ref
git config -f .lsm.lock --remove-section submodule.sdk
git commit -s -m "stop tracking sdk with lazysubmodules" -- .gitmodules .lsm.lock
```

The submodule is then [unmanaged]{.lsm-state .lsm-state-unmanaged}.
`status` still lists it, `update` and `verify` leave it out, and naming
it is refused:

```console
$ lazysubmodules status sdk
NAME  PATH  MODE  REF  LOCK  HEAD     STATE
sdk   sdk   -     -    -     6308203  unmanaged
$ lazysubmodules verify sdk
lazysubmodules: sdk: refused: submodule is not managed by lazysubmodules
```

A submodule in branch mode keeps its native `branch` key, so
`git submodule update --remote` goes on working for it. To stop using
LazySubmodules altogether, do the same for every submodule and delete
`.lsm.lock`.

## See also

- {ref}`ref-files`: every key in `.gitmodules` and `.lsm.lock`.
- {ref}`ref-states`: `drift`, `unmanaged` and the other states.
- [Design and limitations](../explanation/design.md): why the data lives
  where it does.
