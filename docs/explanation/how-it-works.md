<!-- SPDX-License-Identifier: GPL-3.0-only -->
<!-- Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com> -->

(expl-how)=

# How LazySubmodules works

Four records describe each submodule, and LazySubmodules keeps them in
agreement using nothing but the `git` command.

## Four records

Plain Git knows two things about a submodule: the gitlink, the commit
that the superproject records, and the checkout, the commit that is
checked out in the submodule. LazySubmodules adds two records of its own:

Tracking configuration
: `lsm-mode` and `lsm-ref` in `.gitmodules`: what the submodule should
  follow, such as the tag pattern `v6.6.*`. You change it with `set`.

Lock entry
: `mode`, `ref` and `commit` in `.lsm.lock`: what the configuration
  resolved to at the last update, such as tag `v6.6.10` at commit
  `08dcd0dc8f98…`. `update` and `add` write it.

Gitlink
: The commit that the superproject records, in the index and, once
  committed, in `HEAD`.

Checkout
: The commit that is checked out in the submodule, usually as a detached
  HEAD.

When everything is in order, the lock commit, the gitlink and the
checkout are the same commit, and the lock entry is what the
configuration selects today.

:::{container} lsm-diagram

```{raw} html
:file: ../_static/diagrams/records.svg
```

:::

{.lsm-caption}
The four records and the commands that read and write them.

Each command looks at a different part:

- **`status`** compares the tracking configuration, the lock entry and
  the checkout with the local refs: is the configured ref still
  available, and would an update select another target? It does not read
  the gitlink, which is why a staged or committed gitlink that disagrees
  with the lock entry shows up in `verify` and not here. The result is
  one state per submodule; see {ref}`ref-states`.
- **`verify`** compares the lock entry with the gitlink in the committed
  `HEAD`, with the checkout and, for tags, with the tag the lock names.
  It also checks that the lock entry still matches the configuration.
  See {ref}`ref-cli-verify-checks`.
- **`update`** reads the configuration and writes the other three: it
  checks out the target, writes the lock entry and stages the gitlink.

## Why a lock file

A gitlink says which commit the superproject uses, but not why. With a
lock entry, the superproject also records the tag or branch that led to
that commit. That has two uses:

- **Moved tags become visible.** A release tag should never move, but it
  can be force-pushed. When the local tag no longer points to the locked
  commit, `status` reports `drift` and `verify` fails. See
  [Detect moved tags](../guide/moved-tags.md).
- **Updates are predictable.** In `tag-pattern` mode, the locked tag stays
  a candidate, so an update never goes back to an older tag; see
  {ref}`expl-resolution-never-downgrade`.

Both files are plain git-config files that are committed with the
gitlinks, so their history is the history of your dependencies. The
format is described in {ref}`ref-files`.

(expl-how-update)=

## What `update` does

:::{container} lsm-diagram

```{raw} html
:file: ../_static/diagrams/update-flow.svg
```

:::

{.lsm-caption}
The steps of an update. Dashed steps run only with their option.

1. **Select.** The named submodules, or every managed one, in
   `.gitmodules` order.
2. **Check.** Every selected submodule is checked: uncommitted changes, a
   path that cannot be updated safely, and the syntax of the configured
   ref. Without `--fetch`, the target is resolved in the local refs here
   as well. One refusal stops the whole update before anything is fetched
   or checked out; a dry run stops here and prints the plan.
3. **Fetch.** Only with `--fetch`: fetch from `origin`, and clone
   submodules that need it. The targets are resolved afterwards, in the
   refs the fetch brought in, so a target that is still missing is
   refused at this point — after the clones, which stay.
4. **Check out** each target, initializing submodules where needed.
5. **Write** the lock entries, and the native `branch` key where the
   tracking mode needs it.
6. **Stage** the gitlinks, `.gitmodules` and `.lsm.lock` with `git add`.
7. **Commit.** Only with `--commit`: one commit with `git commit -s` and a
   generated message.

An interruption before step 6 puts back what steps 4 and 5 changed: the
submodules it moved return to their previous commit, and the lock entries
and native `branch` keys get their previous values back. Submodules that
the update initialized stay initialized. Once the result is staged, only
the commit remains; when it fails, the update stays staged.
{ref}`expl-safety` explains the checks in step 2.

## Everything through `git`

LazySubmodules has no Git implementation of its own. It runs the `git`
command for every read and write, with arguments passed directly and
never through a shell.

- **Configuration applies.** Mirrors (`url.<base>.insteadOf`), credential
  helpers, proxies, SSH settings, hooks and signing keys work as they do
  for Git itself.
- **Files are read and written with `git config -f`.** `.gitmodules` and
  `.lsm.lock` are never parsed by hand.
- **Output is parsed in the C locale.** Every `git` invocation sets
  `LC_ALL=C`, so the language of your system does not matter.
- **Git 2.39 is the floor.** LazySubmodules uses no newer Git features,
  and it uses the classic `git config` options (`--get`, `--unset-all`,
  `--list`) instead of the subcommands that Git 2.46 added. The test
  suite runs against Git 2.39.5 as well as current Git.

## Inside the binary

The program is written in Go and built as one static binary. It has two
front ends over one core:

| Layer | Responsibility |
|---|---|
| `internal/git` | Thin wrapper over the `git` binary, without business logic |
| `internal/manifest` | Read and write the keys in `.gitmodules` |
| `internal/lock` | Read and write `.lsm.lock` |
| `internal/core` | Resolution, update, status and verify: all business logic |
| `internal/porcelain` | The stable machine-readable output |
| `internal/tui` | The terminal interface; it calls `internal/core` only |
| `cmd/lazysubmodules` | The command line: flag parsing and exit codes |

The terminal interface contains no business logic. Every action is a
call into `internal/core`, the same code that the command line uses, so
both front ends behave the same.

## See also

- {ref}`ref-files`: the format of both files.
- {ref}`ref-states`: how `status` turns the records into a state.
- [Safety model](safety.md): what `update` refuses, and why.
- [CONTRIBUTING.md](https://github.com/FPGArtktic/lazysubmodules/blob/main/CONTRIBUTING.md#architecture):
  the architecture rules for contributors.
