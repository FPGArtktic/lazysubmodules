<!-- SPDX-License-Identifier: GPL-3.0-only -->
<!-- Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com> -->

(guide-troubleshooting)=

# Troubleshooting

What each state, refusal and failed check means, and how to resolve it.

Start with `lazysubmodules status`, which shows the state of every
submodule, and `lazysubmodules verify`, which names every check that
fails. Both are offline and change nothing.

## States

A submodule is in the first state that applies, in the order of this
table; see {ref}`ref-states-precedence`.

| State | Cause | Fix |
|---|---|---|
| [unmanaged]{.lsm-state .lsm-state-unmanaged} | No `lsm-mode` key in `.gitmodules` | Nothing, if intended. Otherwise `lazysubmodules set <name> --branch …` (or another mode); see [Change what a submodule tracks](change-tracking.md) |
| [uninitialized]{.lsm-state .lsm-state-uninitialized} | The submodule is not checked out | `lazysubmodules update <name>` when its Git directory still exists (offline); otherwise `update --fetch <name>` or `fetch <name>` |
| [dirty]{.lsm-state .lsm-state-dirty} | The working tree has uncommitted changes | Commit, stash or discard them inside the submodule, for example `git -C apps/app restore NEWS` |
| [missing-ref]{.lsm-state .lsm-state-missing-ref} | The configured ref is invalid or not in the local refs | `lazysubmodules fetch <name>`; if the ref really does not exist, `set` one that does |
| [drift]{.lsm-state .lsm-state-drift} | HEAD differs from the locked commit, or the locked tag moved or disappeared | Find out why (below), then `update <name>` or `git submodule update <path>` |
| [behind]{.lsm-state .lsm-state-behind} | No lock entry yet, or `update` would select another target | `lazysubmodules update <name>` |
| [ok]{.lsm-state .lsm-state-ok} | Nothing to do | — |

### A submodule is in `drift`

`drift` has two causes. When the `HEAD` column of `status` differs from
`LOCK`, the checkout moved; otherwise the tag did. `verify <name>` names
the failed check:

- **The checkout moved.** Someone checked out another commit in the
  submodule, or ran `git submodule update --remote`. `verify` reports a
  failed `head` check. Run `git submodule update <path>` to return to the
  commit the superproject records, or `lazysubmodules update <name>` to
  return to the locked target.
- **The tag moved.** A fetch brought in a tag that now points elsewhere.
  `verify` reports a failed `tag` check with `(moved tag)`. See
  [Detect moved tags](moved-tags.md) for how to review, accept or reject
  the move.

### A submodule stays `behind`

- **After `set`:** the configuration changed, but the lock file did not.
  Run `update`.
- **Without a lock entry:** a submodule that was just put under control
  is `behind` until the first `update` records it.
- **After `update` without `--commit`:** `status` compares with the lock
  file in the working tree, so it shows `ok`; `verify` fails until you
  commit. See [Update submodules](update.md).
- **Branch mode:** `behind` compares with `origin/<branch>` as of the last
  fetch. `status` never fetches.

## Refusals (exit status 3)

`update` checks every selected submodule before it changes anything, and
reports every refusal; `add` and `fetch` refuse for their own reasons.
Each line starts with `lazysubmodules: `.

`<name>: refused: submodule is not initialized (use --fetch)`
: The submodule needs a clone, which uses the network. Run
  `lazysubmodules update --fetch <name>`, or `lazysubmodules fetch <name>`
  first. In the terminal interface, the hint reads `(press f to fetch)`.
  The longer form `…: its repository lacks the recorded commit <sha>`
  means the repository is there but that commit is not; the same fetch
  brings it.

`<name>: refused: submodule has uncommitted changes`
: Commit, stash or discard the changes inside the submodule. Untracked
  files do not count.

`<name>: refused: ref not found in local refs: …`
: The configured ref does not resolve. The rest of the line says why:
  `tag v9.9.9 does not exist or does not point to a commit`,
  `remote branch origin/nosuch does not exist`,
  `no tag matching v7.* names a commit`, or
  `commit 1111111111111111111111111111111111111111 does not exist or is ambiguous`.
  Fetch first; if the ref still does not exist, `set` another one.

`<name>: refused: bad ref in .gitmodules: …`
: The configured ref is not well formed for its mode, for example
  `invalid tag pattern "v 1": contains white space or control
  characters`. `set` never writes such a value, so this comes from a
  hand-edited `.gitmodules`; `status` reports the submodule
  [missing-ref]{.lsm-state .lsm-state-missing-ref}. Run `set` with a
  ref that {ref}`ref-files-gitmodules` accepts.

`refused: the commit would include unrelated changes: …`
: `update --commit` found other staged changes, or changes in
  `.gitmodules` or `.lsm.lock` outside the selected submodules. Commit or
  unstage them first; see [Commit updates](commit-updates.md).

`refused: the index has unresolved merge conflicts: …`
: `update --commit` does not commit over a merge in progress. Resolve the
  conflicts and commit the merge, or abandon it with `git merge --abort`,
  then run the update again. An `update` without `--commit` is not
  affected.

`<name>: refused: submodule is not managed by lazysubmodules`
: The named submodule has no `lsm-mode` key. `update`, `verify` and
  `fetch` refuse it when named; `set` puts it under control.

`<name>: refused: the index records no submodule at its path`
: `.gitmodules` names the submodule, but the index of the superproject
  has no gitlink at its path. Git ignores such an entry, and the path may
  belong to the superproject itself, so LazySubmodules does not write
  there. Either restore the gitlink, for example
  `git checkout <commit> -- <path>`, or remove the stale section with
  `git config -f .gitmodules --remove-section submodule.<name>`.

`<name>: refused: .gitmodules records no usable url`
: The submodule has to be initialized, but `.gitmodules` records no URL
  that git accepts for it (it ignores a URL starting with `-`). Set one
  with `git config -f .gitmodules submodule.<name>.url <url>`.

`<name>: refused: the submodule repository directory is not a repository: <path> (remove it)`
: The submodule has to be initialized, but something that is not a
  repository sits where its repository belongs in `.git/modules/`. Git
  neither uses nor replaces it. Remove the named directory and run the
  command again.

`<path>: refused: submodule path contains a symbolic link`
: A symbolic link leads to the path, so git never checks a submodule out
  there and following the link could write into an unrelated repository.
  `add` refuses such a path (see
  [Add a submodule](add-submodule.md#path-rules)), and `update` and
  `fetch` refuse a submodule that already has one, which happens when a
  directory above it was replaced by a link; `verify` fails its
  `initialized` check with the same text. Put a real directory back.

`<path>: refused: path already exists…`
: `add` was given a path that is taken. See
  [Add a submodule](add-submodule.md#path-rules).

## Failed checks of `verify` (exit status 4)

`verify` prints one line per failed check under the submodule, and names
the failed submodules on standard error.

| Check | Example line | Fix |
|---|---|---|
| `lock-entry` | `lock-entry: no entry in .lsm.lock` | Run `update` for the submodule, and commit |
| `lock-config` | `lock-config: lock records tag v1.0.0, configuration has v9.9.9` | The configuration changed after the last update: run `update`, or restore `.gitmodules` |
| `lock-config` | `lock-config: locked tag v6.6.9 does not exist or does not match v6.6.*` | Fetch, or run `update` to select an existing tag |
| `lock-commit` | — | The lock entry holds no well-formed full SHA; restore `.lsm.lock` from Git and run `update` |
| `gitlink` | `gitlink: HEAD of the superproject records 8106f614767a, locked 08dcd0dc8f98` | Commit the staged update, or run `update` and commit when the gitlink was changed without it |
| `initialized` | `initialized: submodule is not checked out` | `lazysubmodules fetch <name>`, or `update` |
| `head` | `head: HEAD 8ec67d38d377 differs from locked commit 08dcd0dc8f98` | `git submodule update <path>`, or `update` |
| `tag` | `tag: tag v2.3.1 points to 7c6069a8a36b, locked 556baa13f789 (moved tag)` | Review the move; see [Detect moved tags](moved-tags.md) |
| `tag` | `tag: tag v6.6.9 does not exist` | Fetch, or run `update` |
| `signature` | `signature: tag v1.2.0: error: no signature found` | See [Verify signatures](signatures.md) |

{ref}`ref-cli-verify-checks` defines every check.

## Errors (exit status 1, 2 and 5)

`<name>: no such submodule` (1)
: The name is not in `.gitmodules`. Names are the `[submodule "…"]`
  names, which can differ from paths. `lazysubmodules status` lists both.

`v1: no such submodule` (1)
: `status --porcelain v1` was read as the name `v1`. Write
  `--porcelain=v1`.

`.gitmodules: submodule "kernel": lsm-mode: invalid tracking mode "bogus"` (1)
: An unknown `lsm-mode` value. Every command that reads the configuration
  fails until it is corrected to `branch`, `tag`, `tag-pattern` or
  `commit`.

`.lsm.lock: invalid lock entry "kernel": commit: "xyz" is not a full lowercase hexadecimal SHA` (1)
: A damaged lock file. Restore it with `git restore .lsm.lock`, or remove
  the entry and run `update`.

`read .lsm.lock: config file …/.lsm.lock: not a regular file` (1)
: `.lsm.lock` or `.gitmodules` is a symbolic link, which LazySubmodules
  refuses. Replace it with a regular file.

`unknown command "frobnicate"`, `status: flag provided but not defined: -bogus` (2)
: A usage error. `lazysubmodules help` and `lazysubmodules <command> -h`
  show the correct usage.

`status: invalid value "v2" for flag -porcelain: unsupported format "v2" (only v1 is supported)` (2)
: Only the v1 porcelain format exists.

`tui requires a terminal` (2)
: `tui` needs a terminal on standard input and output. The variants
  `(TERM is not set)` and `with cursor movement (TERM is dumb)` name the
  `TERM` problem; see {ref}`ref-cli-tui`.

`open repository: git rev-parse --show-toplevel: exit status 128: fatal: not a git repository …` (5)
: The current directory is not inside a Git repository. Change into the
  superproject.

`fatal: transport 'file' not allowed` (5)
: A URL, or a URL rewritten by `insteadOf`, uses `file://`, which Git
  allows for submodules only with `protocol.file.allow=always`. See
  {ref}`guide-network-mirrors`.

`Host key verification failed` (5)
: In the terminal interface, SSH cannot ask to accept a new host key.
  Connect once from a normal shell; see {ref}`guide-network-tui`.

Exit status 5 always means that a `git` command failed or was
interrupted; the message ends with Git's own error. {ref}`ref-exit-codes`
lists every code.

## Known limitations

Some behaviour is by design:

- **Nested submodules** are not managed; see
  {ref}`guide-foreach-nested`.
- **Linux only** (`amd64`, `arm64`).
- **The remote is always `origin`.**
- **Resolution is local.** `status`, `verify` and `update` without
  `--fetch` see only what was fetched before.
- **Git 2.39 or later** is required.
- **No prompts in the terminal interface.**

See {ref}`expl-design-limitations` for the reasons.

## Still stuck?

Search the [issue tracker](https://github.com/FPGArtktic/lazysubmodules/issues)
or open a bug report there. Include the output of
`lazysubmodules version` and, if you can, `lazysubmodules status --porcelain=v1`.
Report security problems privately instead; see
[Security policy](../project/security.md).
