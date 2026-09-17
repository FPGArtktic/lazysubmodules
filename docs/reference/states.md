<!-- SPDX-License-Identifier: GPL-3.0-only -->
<!-- Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com> -->

(ref-states)=

# States

`status` reports exactly one of seven states for each submodule; the first
state whose condition matches wins.

## The seven states

| State | Meaning | In the demo | What `update` does |
|---|---|---|---|
| [ok]{.lsm-state .lsm-state-ok} | `HEAD` is the locked commit, and no other target is available locally | `fpga.core`, `sdk` | Nothing: `up to date` |
| [behind]{.lsm-state .lsm-state-behind} | `update` would select another mode, ref or commit than the lock entry records, or there is no lock entry yet | `kernel`, `u-boot` | Moves the submodule to the target |
| [drift]{.lsm-state .lsm-state-drift} | `HEAD` differs from the locked commit, or the locked tag now points elsewhere or is gone | `fpga.core` after its tag moved | Checks out the target and records it |
| [dirty]{.lsm-state .lsm-state-dirty} | The submodule working tree has uncommitted changes | `app` | Refuses (status 3) |
| [uninitialized]{.lsm-state .lsm-state-uninitialized} | The submodule is not checked out | `theme`, `fresh` | Initializes it offline if its repository exists; otherwise refuses without `--fetch` |
| [missing-ref]{.lsm-state .lsm-state-missing-ref} | The configured ref is invalid or not found in the local refs | `broken` | Refuses (status 3) |
| [unmanaged]{.lsm-state .lsm-state-unmanaged} | The submodule has no `lsm-mode` key | `legacy` | Skips it; refuses when it is named |

The states come from the demo superproject in its first state:

```{literalinclude} ../../examples/transcript.txt
:language: text
:start-after: "$ lazysubmodules status"
:end-before: "[exit status 0: success]"
```

{ref}`guide-troubleshooting` explains how to resolve each state.

(ref-states-precedence)=

## Precedence

The first matching state wins, in this order:

1. **unmanaged:** there is no `lsm-mode` key in `.gitmodules`.
2. **uninitialized:** the submodule is not checked out.
3. **dirty:** the working tree has uncommitted changes.
4. **missing-ref:** the configured ref is invalid, or does not resolve in
   the local refs.
5. **drift:** a lock entry exists, and either `HEAD` differs from the locked
   commit, or, in `tag` and `tag-pattern` mode, the locked tag now points to
   a different commit or no longer exists.
6. **behind:** there is no lock entry yet, or `update` would select another
   mode, ref or commit than the lock entry records.
7. **ok:** none of the above.

:::{container} lsm-diagram

```{raw} html
:file: ../_static/diagrams/state-precedence.svg
```

:::

{.lsm-caption}
`status` checks the conditions from top to bottom and reports the first
state that matches.

A dirty submodule is therefore reported as `dirty` even when a newer tag
exists, and a submodule whose tag moved is reported as `drift` even when
`update` would also select a newer tag.

## Details

- **Local refs only.** States are computed from the refs that exist
  locally. A new tag or branch commit on the remote shows up only after
  [`fetch`](cli/fetch.md).
- **Branch mode.** `behind` compares with the remote-tracking branch
  `origin/<branch>` as of the last fetch.
- **Never downgrade.** In `tag-pattern` mode, the locked tag stays a
  candidate while it exists and matches the pattern, even when it is a
  pre-release. The demo's `sdk` is `ok` at `v3.0.0-rc.2` although `v2.9.0`
  is the newest stable tag; {ref}`expl-resolution` explains the rule.
- **Detached `HEAD`** is the normal state of a submodule and not reported.
- **What counts as dirty.** Uncommitted changes to tracked files. Untracked
  files do not count, and neither does a nested submodule that is only
  checked out at another commit; modified files inside a nested submodule
  do. The demo's `tools` is `ok` although its nested submodule is at another
  commit.
- **Native keys.** `update = none` and `ignore = all` in `.gitmodules` do
  not change the state; the demo's `quirky` has both and is `ok`.
- **The lock file in the working tree** is compared, so a staged update
  already shows `ok`. [`verify`](cli/verify.md) checks the committed state.
- **Invalid configuration.** An `lsm-mode` that is not one of the four
  modes is an error for the whole command, not a state; see
  {ref}`ref-files-gitmodules`.

## Where the states appear

- **Table.** The `STATE` column of `lazysubmodules status`.
- **Porcelain format.** Field 8 of `status --porcelain=v1`. The state names
  are part of the {ref}`stable format <ref-porcelain>`.
- **Terminal interface.** The `STATE` column of the table, and the preview,
  which explains why the submodule is in its state.

## Colors

In a terminal, `status` and the terminal interface color the states. This
site uses the same color families for its state labels.

| State | Terminal color |
|---|---|
| [ok]{.lsm-state .lsm-state-ok} | green |
| [behind]{.lsm-state .lsm-state-behind} | yellow |
| [drift]{.lsm-state .lsm-state-drift} | red |
| [missing-ref]{.lsm-state .lsm-state-missing-ref} | red |
| [dirty]{.lsm-state .lsm-state-dirty} | magenta |
| [uninitialized]{.lsm-state .lsm-state-uninitialized} | gray |
| [unmanaged]{.lsm-state .lsm-state-unmanaged} | gray |

`status` uses colors only when standard output is a terminal, `NO_COLOR`
is unset or empty, and `TERM` is not `dumb`; see
{ref}`ref-cli-status-colors`.

## See also

- {ref}`expl-how`: the records that the states compare.
- {ref}`guide-troubleshooting`: causes and fixes.
- {ref}`ref-porcelain`: the states in scripts.
