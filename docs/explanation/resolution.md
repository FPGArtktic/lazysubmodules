<!-- SPDX-License-Identifier: GPL-3.0-only -->
<!-- Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com> -->

(expl-resolution)=

# Tracking modes and resolution

How each tracking mode picks a commit, using only the refs that exist
locally.

## Tracking modes

| Mode | Option | Example ref | Behaviour |
|---|---|---|---|
| `branch` | `--branch B` | `main` | Floating: `update` moves the submodule to the tip of the remote branch |
| `tag` | `--tag T` | `v2.3.1` | Pinned: `update` resolves the tag to its commit |
| `tag-pattern` | `--tag-pattern P` | `v6.6.*` | `update` selects the highest version tag that matches the glob |
| `commit` | `--commit SHA` | `5da6927f70f5…` | Pinned to a commit: `update` only checks that it exists and checks it out |

Choose `branch` for dependencies you develop alongside the superproject,
`tag-pattern` for releases you want to follow within a series, and `tag`
or `commit` for dependencies that must not move without a deliberate
change.

## Resolution is local

`status` and `verify` never fetch, and `update` fetches only with
`--fetch`. They resolve refs in what the last fetch left behind:

- in the submodule's working tree, when it is checked out;
- otherwise in its Git directory under `.git/modules/`, for example after
  `git submodule deinit`.

Run `lazysubmodules fetch`, or `update --fetch`, to see what the remotes
offer now. The remote is always the one named `origin`.

This keeps the network use predictable, and it makes a dry run, a
`status` and an `update` agree with each other: they all see the same
refs.

## Per mode

`branch`
: Resolves `refs/remotes/origin/<branch>` in the submodule, the
  remote-tracking branch as of the last fetch. A local branch of the same
  name does not matter.

`tag`
: Resolves `refs/tags/<tag>`. An annotated tag is dereferenced to its
  commit, as with `git rev-parse "<tag>^{commit}"`.

`tag-pattern`
: Lists the local tags that match the pattern, sorts them by version and
  takes the highest one that qualifies (see below).

`commit`
: Resolves the configured SHA, which may be abbreviated to 7 digits or
  more. It must exist locally and be unambiguous; a ref of the same
  spelling makes it ambiguous. The lock file records the full SHA.

(expl-resolution-patterns)=

## How a tag pattern selects a tag

1. **Match.** The pattern is a glob, as `git tag --list` accepts it. `*`
   matches any characters, including dots.
2. **Sort.** The candidates are sorted by version, highest first:

   ```sh
   git -c versionsort.suffix=- tag --list <pattern> --sort=-v:refname
   ```

   Numbers compare as numbers, so `v1.10.0` sorts above `v1.9.0`, and
   because of `versionsort.suffix=-`, `v1.0.0-rc.1` sorts below `v1.0.0`.
3. **Skip pre-releases.** A tag with a `-` anywhere after its first digit
   is a pre-release: `v1.0.0-rc.1` and `v6.6-rc3` are pre-releases,
   `release-2.1` is not. Pre-releases are skipped unless `update` or `add`
   is given `--include-prerelease`.
4. **Keep the locked tag.** The tag in the lock entry stays a candidate
   even when it is a pre-release (see below).
5. **Select** the highest remaining candidate.

In the demo, `kernel` has the tags `v6.6.1` to `v6.6.10`, `v6.6.11-rc1`
and `v6.7-rc1`, and its lock entry records `v6.6.9`:

:::{container} lsm-diagram

```{raw} html
:file: ../_static/diagrams/tag-selection.svg
```

:::

{.lsm-caption}
The same tags, selected by `v6.6.*`, by `v6.6.*` with
`--include-prerelease`, and by `v6.*`.

Chapter 5 of the demo shows the second case, with the pre-release
selected on request:

```{literalinclude} ../../examples/transcript.txt
:language: console
:start-at: "$ lazysubmodules update --dry-run --include-prerelease kernel sdk"
:end-at: "sdk: up to date"
```

`sdk` is up to date here because `v3.0.0-rc.2` is its highest tag. The
next rule explains why it stays there without `--include-prerelease`.

(expl-resolution-never-downgrade)=

## Never downgrade

In `tag-pattern` mode, the tag recorded in `.lsm.lock` stays a candidate
as long as it still exists locally and matches the pattern, even when it
is a pre-release. An update therefore never moves back to an older tag
just because the newer one is a pre-release.

The demo's `sdk` follows `v*` and is locked at the release candidate
`v3.0.0-rc.2`. The newest stable tag is `v2.9.0`, but a plain update
keeps the release candidate:

```console
$ lazysubmodules update --dry-run sdk
sdk: up to date
$ git -C sdk -c versionsort.suffix=- tag --list 'v*' --sort=-v:refname
v3.0.0-rc.2
v3.0.0-rc.1
v2.9.0
```

Once upstream releases `v3.0.0` and you fetch it, the submodule moves on,
because `v3.0.0` sorts above `v3.0.0-rc.2`. From chapter 8 of the demo:

```{literalinclude} ../../examples/transcript.txt
:language: console
:start-at: "$ lazysubmodules update --commit fpga.core sdk"
:end-at: "committed 878b0f15f9949a97c856cfbbc1655f9f15cffd18"
```

Without this rule, an update after `update --include-prerelease` would
jump back to the newest stable release, and the next
`--include-prerelease` would jump forward again.

The rule applies only to tag patterns. In `tag` mode, `update` moves to
the configured tag even when it is older than the locked one, because
you asked for exactly that tag.

## What `status` compares

`status` resolves the target the same way, and reports
[behind]{.lsm-state .lsm-state-behind} when the result differs from the
lock entry in its mode, its ref or its commit. A difference between HEAD
and the lock entry is [drift]{.lsm-state .lsm-state-drift} instead, which
`status` checks first; see {ref}`ref-states-precedence`. For branches,
`behind` means "behind the remote-tracking branch as of the last fetch":
after a fetch, a submodule that was `ok` can become `behind` without any
local change.

## See also

- [Change what a submodule tracks](../guide/change-tracking.md), with a
  recipe for release series.
- {ref}`ref-files-lock`: what the lock entry records per mode.
- {ref}`ref-cli-update`: the options `--fetch` and
  `--include-prerelease`.
