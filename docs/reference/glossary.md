<!-- SPDX-License-Identifier: GPL-3.0-only -->
<!-- Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com> -->

(ref-glossary)=

# Glossary

The terms that this documentation uses, each with a short definition and
a link to the page that describes it.

:::{glossary}
behind
  The {term}`state` of a submodule for which `update` would select another
  mode, ref or commit than the {term}`lock entry` records, or which has no
  lock entry yet. See {ref}`ref-states`.

configured ref
  The value of `lsm-ref` in `.gitmodules`: a branch name, a tag name, a tag
  pattern or a commit SHA. See {ref}`ref-files-gitmodules`.

dirty
  The state of a submodule whose working tree has uncommitted changes to
  tracked files. `update` refuses such a submodule. See {ref}`ref-states`.

drift
  The state of a submodule whose `HEAD` differs from the
  {term}`locked commit`, or whose locked tag now points to another commit
  or no longer exists. See {ref}`ref-states`.

dry run
  A run of `update --dry-run`, which resolves the targets in the local refs
  and prints what would change, without fetching or changing anything. See
  {ref}`ref-cli-update-output`.

gitlink
  The entry in a tree or in the index of the {term}`superproject` that
  records the commit of a submodule. `update` stages it, and `verify`
  compares the one in `HEAD` with the lock file. See {ref}`expl-how`.

lock entry
  The section of a submodule in the {term}`lock file`, with the keys
  `mode`, `ref` and `commit`. See {ref}`ref-files-lock`.

lock file
  `.lsm.lock`, the file in the superproject that records the mode, the
  resolved ref and the commit of each managed submodule. See
  {ref}`ref-files-lock`.

locked commit
  The full commit SHA that a {term}`lock entry` records.

locked ref
  The ref that a {term}`lock entry` records: the selected tag in `tag` and
  `tag-pattern` mode, the branch in `branch` mode, and the commit in
  `commit` mode.

managed
  A submodule is managed when `.gitmodules` has an `lsm-mode` key for it.
  Commands without names work on the managed submodules. See
  {ref}`ref-files-gitmodules`.

missing-ref
  The state of a submodule whose {term}`configured ref` is invalid or does
  not resolve in the local refs. `update` refuses such a submodule. See
  {ref}`ref-states`.

moved tag
  A tag that was moved to another commit upstream, usually with a force
  push. After `fetch`, the submodule shows {term}`drift` and `verify`
  fails. See {ref}`guide-moved-tags`.

native branch key
  The Git key `submodule.<name>.branch` in `.gitmodules`, which
  `git submodule update --remote` uses. LazySubmodules writes it in branch
  mode and removes it in the other modes. See {ref}`ref-files-branch-key`.

network policy
  The rule that only `fetch`, `update --fetch` and `add` use the network.
  See {ref}`ref-cli-commands`.

ok
  The state of a submodule whose `HEAD` is the {term}`locked commit`, with
  no other {term}`target` available locally. See {ref}`ref-states`.

porcelain v1 format
  The stable, TAB-separated output of `status --porcelain=v1` for scripts.
  See {ref}`ref-porcelain`.

pre-release
  A tag with a `-` after its first digit, such as `v6.6.11-rc1` or
  `v3.0.0-rc.2`. A {term}`tag pattern` selects pre-releases only with
  `--include-prerelease`, or when the lock file records one already. See
  {ref}`expl-resolution`.

refused
  The outcome of a command that would be unsafe, such as an update of a
  dirty submodule. The command exits with status 3, and a refused update
  moves no submodule. See {ref}`ref-exit-codes`.

resolution
  Finding the commit that a submodule should be at, from its
  {term}`tracking mode`, its {term}`configured ref` and the local refs.
  Also called resolving. See {ref}`expl-resolution`.

state
  The one-word summary that `status` reports for a submodule: `ok`,
  `behind`, `drift`, `dirty`, `uninitialized`, `missing-ref` or
  `unmanaged`. See {ref}`ref-states`.

submodule
  A Git repository that is embedded in the {term}`superproject` at a path
  and recorded by a {term}`gitlink`.

submodule name
  The name of the submodule's section in `.gitmodules`, such as `u-boot`.
  Commands take names, not paths. The name can differ from the path, such
  as `bootloader/u-boot`. See {ref}`ref-cli-rules`.

superproject
  The Git repository that contains the submodules, and `.gitmodules` and
  `.lsm.lock` at its top level.

tag pattern
  A glob, as accepted by `git tag --list`, that selects the highest
  matching version tag, such as `v6.6.*`. Also the name of the tracking
  mode `tag-pattern`. See {ref}`expl-resolution`.

target
  The commit and ref that `update` selects for a submodule by
  {term}`resolution`.

terminal interface
  The interactive, full-screen interface that `lazysubmodules tui` starts;
  also called TUI. See {ref}`ref-tui`.

tracking configuration
  The keys `lsm-mode` and `lsm-ref` of a submodule in `.gitmodules`. `add`
  and `set` write them. See {ref}`ref-files-gitmodules`.

tracking mode
  How a submodule selects its target: `branch`, `tag`, `tag-pattern` or
  `commit`, stored as `lsm-mode`. See {ref}`expl-resolution`.

uninitialized
  The state of a submodule that is not checked out. `update` initializes it
  offline when its repository still exists, and otherwise needs `--fetch`.
  See {ref}`ref-states`.

unmanaged
  The state of a submodule without an `lsm-mode` key. LazySubmodules shows
  it but does not change it until `set` puts it under management. See
  {ref}`ref-states`.
:::
