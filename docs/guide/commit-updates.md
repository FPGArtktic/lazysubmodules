<!-- SPDX-License-Identifier: GPL-3.0-only -->
<!-- Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com> -->

(guide-commit)=

# Commit updates

`update --commit` creates one signed-off commit that contains the update
and nothing else.

## One command

```sh
lazysubmodules update --commit kernel u-boot
lazysubmodules update --fetch --commit          # every managed submodule
```

The update runs as usual (see [Update submodules](update.md)); then
`update` commits the staged result with `git commit -s` and prints
`committed <commit>` as its last line. A dry run with `--commit` shows the
subject it would use:

```console
$ lazysubmodules update --dry-run --commit kernel u-boot
would update kernel: v6.6.9 (8106f61) -> v6.6.10 (08dcd0d)
would update u-boot: main (29b295c) -> main (8e9fc7a)
would commit "manifest: update 2 submodules"
```

## Only the update goes into the commit

The commit may contain only the selected submodules and their entries in
`.gitmodules` and `.lsm.lock`. `update --commit` refuses with exit status
3 when:

- other paths are staged;
- `.gitmodules` or `.lsm.lock` differ from `HEAD` outside the sections of
  the selected submodules, staged or not;
- the index has unresolved merge conflicts.

In the demo, a staged change to another file stops the commit, and the
update goes through once that change is unstaged:

```{literalinclude} ../../examples/transcript.txt
:language: console
:start-at: '$ echo "Revision B" >>src/drivers/README'
:end-at: "[exit status 3: refused]"
```

```{literalinclude} ../../examples/transcript.txt
:language: console
:start-at: "$ git restore --staged src/drivers/README"
:end-before: "theme was only initialized"
```

The `[exit status N: …]` lines come from the demo, not from
LazySubmodules. The message names the first paths and counts the rest.
A staged `lazysubmodules add`, for example, touches both files and the new
gitlink:

```text
lazysubmodules: refused: the commit would include unrelated changes: .gitmodules (staged changes outside the selected submodules), .lsm.lock (staged changes outside the selected submodules), third_party/app, and 4 more
```

Commit or unstage such changes first. `git restore --staged <path>`
unstages a path and keeps the change in the working tree.

## What the commit records

- **One commit per invocation.** The commit and its message cover the
  submodules whose gitlink, lock entry or tracking keys (`lsm-mode`,
  `lsm-ref` and the native `branch` key) change compared with `HEAD`.
- **Left out:** a submodule that is only initialized, cloned, or checked
  out at the commit `HEAD` records is still updated, but it is neither in
  the commit nor in the message. In the demo, `theme` was only
  initialized, so the message above lists `kernel` and `u-boot` only. The
  same applies when the update only restores entries in `.gitmodules` or
  `.lsm.lock` to what `HEAD` records.
- **Nothing to commit:** without such a change, no commit is made, and the
  last line reads `nothing to commit`:

  ```console
  $ lazysubmodules update kernel --fetch --commit
  kernel: up to date
  nothing to commit
  ```

## Sign-off and hooks

- The commit is created with `git commit -s`, so the `Signed-off-by` line
  comes from `user.name` and `user.email`.
- Commit hooks run as configured, and so does commit signing
  (`commit.gpgSign`).
- When a hook fails, or `git commit` is interrupted, the update stays
  staged and no commit is made. Fix the problem and commit with
  `git commit -s`, or run `update --commit` again.

## Staged changes of a selected submodule

`update` stages the target of every selected submodule, replacing
whatever was staged for it. When `HEAD` already records that target, the
submodule is left out of the commit, so a different staged value is
discarded without being committed. The line then says
`discarded the staged change`.

For example, after `kernel` was committed at `v6.6.10`, someone checks out
an older commit in the submodule and stages it:

```console
$ git -C kernel checkout -q v6.6.9
$ git add kernel
$ lazysubmodules update --dry-run --commit kernel
would update kernel: v6.6.10 (08dcd0d), HEAD is 8106f61, discard the staged change
nothing to commit
$ lazysubmodules update --commit kernel
kernel: v6.6.10 (08dcd0d), HEAD was 8106f61, discarded the staged change
nothing to commit
```

Run `update --dry-run --commit` first whenever something is staged for a
selected submodule.

## The message

For one submodule, the subject names it and its new ref:

```text
manifest: update kernel to v6.6.10

Tracking mode: tag-pattern v6.6.*
Old: 8106f614767a (v6.6.9)
New: 08dcd0dc8f98 (v6.6.10)

Signed-off-by: Dana Developer <dana@example.org>
```

For several, the subject counts them, and each gets a block with a quoted
`Submodule "<name>":` heading, as in the `git log -1` output above. The
`Old` line reads `(unlocked)` for a submodule without a previous lock
entry, and `none` when `HEAD` recorded no gitlink for it.

The messages pass the gitlint rules that the LazySubmodules project uses
for its own commits, whatever the names and refs are. {ref}`ref-commit-messages` describes the format
in full.

## See also

- {ref}`ref-cli-update`: every option and message.
- [Update submodules](update.md): selections, refusals and output.
- {ref}`ref-commit-messages`: subject shortening, quoting and wrapping.
