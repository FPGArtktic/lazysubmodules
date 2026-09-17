<!-- SPDX-License-Identifier: GPL-3.0-only -->
<!-- Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com> -->

(ref-cli-update)=

# `update`

Resolve the tracked ref of managed submodules in the local refs, check out
the target commit, update the lock file and stage the result, or commit it.

## Synopsis

```text
lazysubmodules update [<name>...] [--fetch] [--dry-run] [--commit] [--include-prerelease]
```

`update` uses the network only with `--fetch`, and never together with
`--dry-run`.

## Help text

```{literalinclude} _generated/update.txt
:language: text
:lines: 5-
```

## Options

| Option | Effect |
|---|---|
| `<name>...` | Update these managed submodules. Without names, every managed submodule is updated |
| `--fetch` | Fetch from `origin` before resolving, and clone uninitialized submodules whose repository is missing |
| `--dry-run` | Print the plan and modify nothing. Never fetches, initializes or clones, even with `--fetch` |
| `--commit` | Create one commit with the result, using `git commit -s` |
| `--include-prerelease` | Let tag patterns select pre-release tags |

## Behaviour

For each selected managed submodule, in `.gitmodules` order:

1. **Resolve** the target commit in the local refs, following the
   {ref}`resolution rules <expl-resolution>`. With `--fetch`, every
   selected submodule is fetched first, as [`fetch`](fetch.md) does it.
2. **Initialize** a submodule that is not checked out. When its Git
   directory still exists, for example after `git submodule deinit`, this
   happens offline. When a clone is needed, `update` refuses unless
   `--fetch` is given.
3. **Check out** the target commit in the submodule. A detached `HEAD` is
   expected. Nested submodules are left alone.
4. **Write** the lock entry to `.lsm.lock`, and rewrite the native `branch`
   key in `.gitmodules` to match the tracking mode.
5. **Stage** the gitlink, `.gitmodules` and `.lsm.lock` with `git add`.
6. **Commit** with `--commit`; see [below](#ref-cli-update-commit).

With `--dry-run`, `update` resolves the targets against the refs that exist
locally and prints what it would do. A submodule that needs a clone is
listed with the target `unknown until fetched`.

(ref-cli-update-refusals)=

### Refusals

All checks run for every selected submodule before anything is modified.
Every refusal is reported, one per line, and if any submodule is refused,
nothing changes. `update` refuses with status 3 when:

- a submodule working tree has uncommitted changes. Untracked files do not
  count, and neither does a nested submodule that is only checked out at
  another commit;
- the configured ref is invalid, or does not resolve in the local refs
  (run `fetch` first, or use `--fetch`);
- a submodule is not initialized, its Git directory is missing, and
  `--fetch` was not given;
- `--commit` is given and the commit would include unrelated changes, or
  the index has unresolved merge conflicts;
- an unmanaged submodule is named explicitly;
- a submodule cannot be updated safely: its path leads through a symbolic
  link, the index records no submodule at its path, or it would be
  initialized but has no usable URL, or something other than a Git
  repository is in the place of its Git directory.

On the demo in its first state:

```console
$ lazysubmodules update
lazysubmodules: fresh: refused: submodule is not initialized (use --fetch)
lazysubmodules: app: refused: submodule has uncommitted changes
lazysubmodules: broken: refused: ref not found in local refs: tag v9.9.9 does not exist or does not point to a commit
```

An empty or malformed `lsm-ref` is refused as well, and so is a commit
while the index has conflicts:

```text
lazysubmodules: kernel: refused: bad ref in .gitmodules: invalid tag pattern "": empty value
lazysubmodules: refused: the index has unresolved merge conflicts: <paths>
```

With `--fetch`, a ref that can only be checked after fetching is checked
then. Such a refusal keeps the fetched refs and the submodules that were
initialized, but moves no other submodule and changes neither
`.gitmodules`, `.lsm.lock` nor the index.

### Failures

When a step fails after the first change, `update` puts back what it
changed: the submodules it moved are checked out at their previous commit
again, and the lock entries and `branch` keys it wrote get their previous
values back. Submodules it initialized stay initialized. Once the result is
staged, only the commit remains; see [`--commit`](#ref-cli-update-commit).

(ref-cli-update-output)=

## Output

`update` prints one line per selected submodule on standard output. Git's
own output, such as clone progress, goes to standard error.

```text
<name>: <old> -> <new>[, <note>...]
<name>: <new>[, <note>...]
<name>: up to date
```

- **Old side:** what the superproject records before the update: in the
  index, or in `HEAD` with `--commit`. It is the locked ref and commit, such
  as `v6.6.9 (8106f61)`; the commit alone when the lock entry records
  another commit; `<commit> (unlocked)` without a lock entry; or `none`
  without a gitlink.
- **New side:** the target, such as `v6.6.10 (08dcd0d)`. In commit mode,
  both sides show only the abbreviated commit.
- **Same sides:** when the superproject records the target already, only
  the target is printed, followed by the notes.
- **Mode change:** when the tracking mode changes, both sides name it:
  `sdk: tag-pattern v3.0.0-rc.2 (6308203) -> tag v2.9.0 (68a8743)`.
- **Up to date:** nothing changes for the submodule.
- **No submodules:** without managed submodules, `update` prints
  `no managed submodules`.

### Notes

| Note | Dry-run wording | Meaning |
|---|---|---|
| `initialized` | `initialize` | The submodule was not checked out; its repository was still there |
| `cloned` | `clone` | The submodule was cloned (only with `--fetch`) |
| `HEAD was <commit>` | `HEAD is <commit>` | The submodule was checked out at a commit other than the recorded one |
| `recorded again` | `record again` | The superproject records the same commit anew, for example after `set` changed the ref but not the commit |
| `restored .lsm.lock`, `restored .gitmodules`, `restored .gitmodules and .lsm.lock` | `restore <files>` | The superproject records the target already, but the working tree copy of the file differs: the lock entry differs or is missing, or the native `branch` key does not follow the mode. The entry or key is rewritten to what the superproject records |
| `discarded the staged change` | `discard the staged change` | Only with `--commit`: `HEAD` records the target already, but the index held a different gitlink, lock entry or tracking keys. What `HEAD` records is staged in their place, so the staged change is lost |

A dry run starts each changing line with `would update`:

```console
$ lazysubmodules update --dry-run --fetch --commit kernel u-boot theme sdk
would update kernel: v6.6.9 (8106f61) -> v6.6.10 (08dcd0d)
would update u-boot: main (29b295c) -> main (8e9fc7a)
would update theme: v1.0.0 (05f49f3), initialize
sdk: up to date
would commit "manifest: update 2 submodules"
$ lazysubmodules update --dry-run --fetch fresh
would update fresh: v1.1.0 (f2de3e1) -> unknown until fetched, clone
```

Other lines from the demo:

```text
legacy: 2f54e11 (unlocked) -> main (2f54e11)
u-boot: 8e9fc7a -> main (8e9fc7a)
broken: v1.0.0 (52c4bdb), recorded again
fpga.core: v2.3.1 (556baa1), HEAD was a926cfc
signed: v1.0.0 (5d1133e), restored .lsm.lock
u-boot: main (8e9fc7a), HEAD was 29b295c, discarded the staged change
third_party/sdk2: none -> v2.9.0 (68a8743)
crypto lib: 5da6927 -> c1d65e3
```

### Commit line

With `--commit`, the last line is one of:

| Line | Meaning |
|---|---|
| `committed <commit>` | The commit was created; the full SHA follows |
| `nothing to commit` | No selected submodule changes what `HEAD` records; also in a dry run |
| `would commit "<subject>"` | Dry run: the subject of the commit |
| `would commit the result` | Dry run: a target is not known before a clone |

This output is not a stable interface. Scripts should read
[`status --porcelain=v1`](../porcelain.md) instead.

(ref-cli-update-commit)=

## `--commit`

- **Only the update.** The commit may contain only the update. `update`
  refuses with status 3 when paths other than `.gitmodules`, `.lsm.lock` and
  the selected submodules are staged, or when `.gitmodules` or `.lsm.lock`
  differ from `HEAD` outside the sections of the selected submodules,
  staged or not. It also refuses while the index has unresolved merge
  conflicts:

  ```text
  lazysubmodules: refused: the commit would include unrelated changes: src/drivers/README
  lazysubmodules: refused: the commit would include unrelated changes: .gitmodules (staged changes outside the selected submodules), .lsm.lock (staged changes outside the selected submodules), third_party/sdk2
  ```

  The note after `.gitmodules` or `.lsm.lock` says `staged`, `unstaged`
  or `staged and unstaged`. At most three paths are named, and the list
  then ends with `and <N> more`.
- **Sign-off and hooks.** The commit is created with `git commit -s`, so
  the `Signed-off-by` line comes from `user.name` and `user.email`. Hooks
  and commit signing run as configured. When `git commit` fails, for
  example in a hook, the update stays staged, no commit is made, and
  `update` exits with status 5:

  ```text
  sdk: tag-pattern v3.0.0-rc.2 (6308203) -> tag v2.9.0 (68a8743)
  lazysubmodules: git commit --quiet -s -F -: exit status 1: commit-msg: rejected
  ```

  The second line comes from a `commit-msg` hook that printed
  `commit-msg: rejected` and failed.
- **What the commit records.** One commit per invocation. It covers only
  the submodules whose gitlink, lock entry or tracking keys (`lsm-mode`,
  `lsm-ref`, the native `branch` key) change compared with `HEAD`.
- **Left out.** A submodule that is only initialized, cloned, or checked out
  at the commit that `HEAD` records is still updated, but it is neither in
  the commit nor in the message. The same applies when the update only
  rewrites its entries to what `HEAD` records (`restored …`).
- **Staged changes of a selected submodule** are replaced by its target.
  When `HEAD` records the target already, the submodule is left out of the
  commit, so a different staged value is discarded without being
  committed, and the line says `discarded the staged change`. Run
  `update --dry-run --commit` first to see it.
- **Message.** {ref}`ref-commit-messages` describes the generated message.

## Exit status

| Status | When |
|---|---|
| 0 | Success, including dry runs, `up to date` and `nothing to commit` |
| 1 | An unknown name, or an invalid `.gitmodules` or `.lsm.lock` |
| 2 | A usage error |
| 3 | Refused; no submodule was moved, and `.gitmodules`, `.lsm.lock` and the index are unchanged |
| 5 | A `git` command failed or was interrupted |

An interrupted update puts back what it changed before staging; see
{ref}`ref-runtime-signals`.

## Examples

```sh
# Preview, then update everything and stage the result.
lazysubmodules update --dry-run
lazysubmodules update

# Fetch, update two submodules and commit them in one step.
lazysubmodules update --fetch --commit kernel u-boot

# Allow release candidates for tag patterns.
lazysubmodules update --dry-run --include-prerelease kernel

# See what a commit would record, and whether a staged change is lost.
lazysubmodules update --dry-run --commit
```

## See also

- {ref}`guide-update` and {ref}`guide-commit`: the workflows.
- {ref}`expl-safety`: why updates are all or nothing.
- {ref}`guide-troubleshooting`: how to resolve each refusal.
- {ref}`ref-states`: what `status` reports before and after.
