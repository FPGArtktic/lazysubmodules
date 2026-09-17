<!-- SPDX-License-Identifier: GPL-3.0-only -->
<!-- Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com> -->

(expl-safety)=

# Safety model

LazySubmodules refuses rather than guesses, and changes nothing unless
every selected submodule is safe to update.

A superproject holds work that is easy to lose: uncommitted changes in
submodules, staged changes in the index, commits that exist in one
checkout only. LazySubmodules therefore follows a few rules throughout.

## All or nothing

`update` checks every selected submodule before it changes anything. A
single problem stops the whole update, with exit status 3, and every
problem is reported at once, one per line. Nothing is checked out,
written or staged; not even the submodules that were safe.

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

The alternative, updating what can be updated, would leave a
superproject in which some submodules moved and others did not, and a
lock file that describes neither the old nor the new state. A refusal
leaves you with the state you started from and a list of things to fix.
[Update submodules](../guide/update.md) lists every refusal.

With `--fetch`, the fetch runs before the checks that need fetched refs,
and its clones stay when such a check fails. Fetching and cloning change
no gitlink, lock entry or checkout that the superproject records.

## What counts as a change you could lose

- **Uncommitted changes** in a submodule's working tree block the update
  of that submodule. **Untracked files** do not, because a checkout does
  not touch them.
- **Nested submodules** that are only checked out at another commit do
  not block it either; LazySubmodules does not manage them. Modified files
  inside a nested submodule do.
- **Staged changes** to other paths of the superproject are kept by a
  plain `update`.
  `update --commit` refuses when they are not part of the update, so that
  the commit contains nothing else (see
  [Commit updates](../guide/commit-updates.md)).
- **Unresolved merge conflicts** in the index block `update --commit`.
- **Unmanaged submodules** are never touched. Naming one is refused.

A detached HEAD in a submodule is normal, not a problem: submodules are
checked out at commits, not branches.

## Rollback on interruption

{kbd}`Ctrl+C`, `SIGTERM` and `SIGHUP` cancel a running command and
terminate the `git` processes it started.

- An `update` interrupted before staging puts back what it changed:
  submodules it already checked out return to their previous commit, the
  lock entries and native `branch` keys get their previous values back,
  and nothing is staged. Submodules it initialized stay initialized, as
  after a refused `update --fetch`.
- Once the result is staged, only the commit remains. When `git commit`
  is interrupted or fails, for example in a hook, the update stays staged
  and no commit is made.
- A second signal ends LazySubmodules at once, without the clean-up, for
  the case where the clean-up itself hangs.
- `SIGINT` and `SIGHUP` stay ignored when they were ignored at start, as
  under `nohup`.

{ref}`ref-runtime-signals` has the details.

(expl-safety-untrusted)=

## Untrusted repositories

A cloned superproject is data from someone else. Its `.gitmodules` and
`.lsm.lock` may have been written to trick a tool into writing files
elsewhere, running commands or confusing a terminal. LazySubmodules
defends against that:

- **No symbolic links in place of its files.** `.gitmodules` and
  `.lsm.lock` must be regular files. A symbolic link is refused, so that
  a clone cannot redirect a write to a file outside the superproject:

  ```text
  lazysubmodules: read .lsm.lock: config file …/.lsm.lock: not a regular file
  ```

- **No paths through symbolic links or files.** A submodule path that
  leads through a symbolic link is refused by `update` and `add`, and a
  new path must lie inside the superproject.
- **Validated values.** Refs, patterns, paths and URLs are checked before
  they reach `git`: no empty values, no leading `-`, which `git` could
  read as an option, and no control characters. A configured ref that
  fails the check makes the submodule `missing-ref`.
- **No shell.** Every `git` command runs with its arguments passed
  directly. `foreach` runs your command without a shell as well.
- **No commands from `.gitmodules`.** Submodules are initialized with
  `git submodule update --checkout`, whatever the `update` key in
  `.gitmodules` says, and paths are passed as literal pathspecs.
- **Quoted output.** Names, refs and paths with unusual characters are
  printed quoted, so they cannot send control sequences to the terminal.
  A submodule named `evil` followed by an escape sequence appears as
  `"evil\x1b[31mred"` in the table and in messages, and as
  `"evil\033[31mred"` in the porcelain format.
- **Valid porcelain output.** `status --porcelain=v1` escapes TABs, line
  breaks, quotes and invalid UTF-8, so a hostile name can neither add a
  record nor shift the fields of one; the output is always valid UTF-8.

Git's own protections apply as well. For example, Git allows the `file`
transport for submodules only when `protocol.file.allow` permits it.

## Predictable network access

Only `fetch`, `update --fetch` and `add` use the network, and only
through `git`. Everything else, including every dry run, works on local
refs. This rule is part of the stable interface, for three reasons:

- **No surprises.** Reading the state of a superproject never contacts a
  server, so it works offline and never waits for one.
- **Consistent answers.** `status`, a dry run and an update without
  `--fetch` see the same refs, so the update does what the dry run
  showed.
- **Clear credentials.** Only commands that are expected to connect can
  ask for credentials.

## The terminal interface and Git

While the terminal interface is shown, Git runs in a session of its own,
without access to the terminal, and with `GIT_TERMINAL_PROMPT=0`. A
credential or host key prompt therefore fails cleanly instead of drawing
over the screen or waiting for input that never comes. See
{ref}`guide-network-tui`.

## Reporting a vulnerability

If you find a way around any of this, report it privately; see the
[Security policy](../project/security.md).

## See also

- [How LazySubmodules works](how-it-works.md): the steps of an update.
- {ref}`ref-exit-codes`: exit status 3 and the others.
