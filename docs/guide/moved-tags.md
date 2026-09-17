<!-- SPDX-License-Identifier: GPL-3.0-only -->
<!-- Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com> -->

(guide-moved-tags)=

# Detect moved tags

The lock file exposes a release tag that was moved upstream, and `verify`
fails until you accept or reject the move.

## Why tags need a lock

A tag is meant to name one commit forever, but it can be deleted and
created again, or force-pushed to another commit. A superproject that
only records "tag `v2.3.1`" cannot tell. `.lsm.lock` records both the tag
and the commit it resolved to, so LazySubmodules can compare them later:

```ini
[submodule "fpga.core"]
	mode = tag
	ref = v2.3.1
	commit = 556baa13f7892bcd8f0fbf14389847e0a72d8b36
```

When the local tag points to another commit than the lock records,
`status` shows [drift]{.lsm-state .lsm-state-drift} and the `tag` check of
`verify` fails with exit status 4. This works for `tag` and `tag-pattern`
mode.

:::{container} lsm-terminal

```{image} ../demo/drift.gif
---
alt: >-
  A release tag moved upstream: fpga.core follows tag v2.3.1 and is ok.
  The upstream repository moves the tag to another commit. After
  lazysubmodules fetch, status reports drift, and verify names the moved
  tag and exits with status 4.
loading: lazy
---
```

:::

## Walkthrough

Chapter 8 of the [demo](../getting-started/tour.md) plays this through.
The `[exit status N: …]` lines come from the demo, not from
LazySubmodules.

### 1. The tag moves upstream

The maintainer of `fpga-core` moves `v2.3.1` to a later commit and
force-pushes it:

```{literalinclude} ../../examples/transcript.txt
:language: console
:start-at: "(upstream fpga-core) $ git tag --force"
:end-at: "(forced update)"
```

### 2. Nothing changes locally yet

Local refs change only with a fetch, so `status` still reports `ok`:

```{literalinclude} ../../examples/transcript.txt
:language: console
:start-at: "$ lazysubmodules status fpga.core sdk"
:end-at: "[exit status 0: success]"
```

### 3. `fetch` replaces the tag

`fetch` runs `git fetch --tags --force --prune origin`. The `t` line is
Git reporting that a local tag was updated. Afterwards, `fpga.core` is in
`drift`:

```{literalinclude} ../../examples/transcript.txt
:language: console
:start-at: "$ lazysubmodules fetch fpga.core sdk"
:end-before: "fetch replaces moved tags."
```

`sdk` is [behind]{.lsm-state .lsm-state-behind} for an unrelated reason:
upstream released `v3.0.0` at the same time.

:::{warning}
Because of `--force`, `fetch` replaces a local tag with the remote tag of
the same name. Do not keep your own tags in a submodule under names that
upstream uses.
:::

### 4. `verify` fails

```{literalinclude} ../../examples/transcript.txt
:language: console
:start-at: "$ lazysubmodules verify fpga.core sdk"
:end-at: "[exit status 4: verification failed]"
```

The `tag` line names the new commit, the locked commit and the reason. In
CI, this exit status stops the pipeline; see
[Scripts and CI](scripting.md).

### 5. Review the new commit

The fetched tag is in the submodule, so ordinary Git commands show what
changed between the locked commit and the new one:

```console
$ git -C ip/fpga-core log --oneline 556baa1..v2.3.1
7c6069a timing: fix setup violation
$ git -C ip/fpga-core diff --stat 556baa1 v2.3.1
 NEWS | 1 +
 1 file changed, 1 insertion(+)
```

Then decide whether to accept or reject the move.

## Accept the move

`update` moves the submodule to the commit the tag points to now, and
records it in the lock file:

```{literalinclude} ../../examples/transcript.txt
:language: console
:start-at: "$ lazysubmodules update --commit fpga.core sdk"
:end-before: "$ lazysubmodules verify fpga.core sdk"
```

`verify` passes again:

```{literalinclude} ../../examples/transcript.txt
:language: console
:prepend: $ lazysubmodules verify fpga.core sdk
:start-at: "fpga.core: ok (7 checks)"
:end-at: "[exit status 0: success]"
```

## Reject the move

To stay at the reviewed commit, track that commit instead of the tag:

```console
$ lazysubmodules set fpga.core --commit 556baa1
fpga.core: tracks commit 556baa13f7892bcd8f0fbf14389847e0a72d8b36
$ lazysubmodules status fpga.core
NAME       PATH          MODE    REF      LOCK              HEAD     STATE
fpga.core  ip/fpga-core  commit  556baa1  556baa1 (v2.3.1)  556baa1  drift
$ lazysubmodules update --commit fpga.core
fpga.core: tag v2.3.1 (556baa1) -> commit 556baa1
committed 4d3b5aa749fe25e6706f63e571f679d1397055c2
$ git log -1 --format=%B
manifest: update fpga.core to 556baa13f789

Tracking mode: commit 556baa13f7892bcd8f0fbf14389847e0a72d8b36
Old: 556baa13f789 (v2.3.1)
New: 556baa13f789

Signed-off-by: Dana Developer <dana@example.org>

$ lazysubmodules verify fpga.core
fpga.core: ok (6 checks)
```

The checkout does not change, only the tracking configuration and the
lock entry. `status` keeps showing `drift` until the update, because the
lock entry still names the moved tag. Switch back to tag mode with
`lazysubmodules set fpga.core --tag <tag>` once upstream publishes a
release you accept.

## Tag patterns

In `tag-pattern` mode, the lock records the tag the pattern selected, and
the same check applies to it. When the locked tag moves, the submodule is
in `drift`. When the locked tag disappears, it is in `drift` as well,
`verify` fails, and `update` selects the highest remaining candidate:

```console
$ git -C kernel tag -d v6.6.9
Deleted tag 'v6.6.9' (was 5a65983)
$ lazysubmodules status kernel
NAME    PATH    MODE         REF     LOCK              HEAD     STATE
kernel  kernel  tag-pattern  v6.6.*  8106f61 (v6.6.9)  8106f61  drift
$ lazysubmodules verify kernel
kernel: failed (2 of 7 checks)
  lock-config: locked tag v6.6.9 does not exist or does not match v6.6.*
  tag: tag v6.6.9 does not exist
lazysubmodules: verification failed: kernel
$ lazysubmodules update --dry-run kernel
would update kernel: v6.6.9 (8106f61) -> v6.6.10 (08dcd0d)
```

`fetch` does not remove local tags: a tag that was deleted upstream stays
in the submodule, and so does the lock entry that names it.

## See also

- {ref}`ref-cli-fetch` and {ref}`ref-cli-verify-checks`.
- {ref}`ref-files-lock`: the format of the lock file.
- {ref}`ref-states`: `drift` and the other states.
- [Verify signatures](signatures.md): check who made the tagged commit.
