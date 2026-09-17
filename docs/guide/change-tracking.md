<!-- SPDX-License-Identifier: GPL-3.0-only -->
<!-- Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com> -->

(guide-change-tracking)=

# Change what a submodule tracks

Switch the tracking mode or the ref of a submodule with `set`, then apply
the change with `update`.

## `set` changes only `.gitmodules`

```text
lazysubmodules set <name> (--branch B | --tag T | --tag-pattern P | --commit SHA)
```

`set` writes the tracking configuration, `lsm-mode` and `lsm-ref`, to
`.gitmodules` and changes nothing else: not the checkout, not the lock
file, not the index. It works offline.

```console
$ lazysubmodules set kernel --tag-pattern 'v6.*'
kernel: tracks tag-pattern v6.*
$ git diff -- .gitmodules
diff --git a/.gitmodules b/.gitmodules
index 3afeb0c..d9c9f2d 100644
--- a/.gitmodules
+++ b/.gitmodules
@@ -2,7 +2,7 @@
 	path = kernel
 	url = https://git.example.invalid/linux.git
 	lsm-mode = tag-pattern
-	lsm-ref = v6.6.*
+	lsm-ref = v6.*
 [submodule "u-boot"]
 	path = bootloader/u-boot
 	url = https://git.example.invalid/u-boot.git
```

`status` shows the new configuration next to the old lock entry, and a
dry run shows what `update` would select:

```console
$ lazysubmodules status kernel
NAME    PATH    MODE         REF   LOCK              HEAD     STATE
kernel  kernel  tag-pattern  v6.*  8106f61 (v6.6.9)  8106f61  behind
$ lazysubmodules update --dry-run kernel
would update kernel: v6.6.9 (8106f61) -> v6.6.10 (08dcd0d)
```

The wider pattern also matches `v6.7-rc1`, but that is a pre-release, so
the update still selects `v6.6.10`. Run `update` to apply the change; it
stages `.gitmodules` together with the lock file and the gitlink, and
`update --commit` commits all three. See
[Update submodules](update.md).

## The four modes

`set` takes exactly one of four options. {ref}`expl-resolution` explains
how `update` resolves each of them.

`--branch <branch>`
: The submodule follows the tip of `origin/<branch>`, as of the last
  fetch. `update` moves it whenever the branch moves. `set` also writes the
  native `branch` key, so that `git submodule update --remote` follows the
  same branch.

`--tag <tag>`
: The submodule stays at the commit of one tag. `update` moves it only
  when you configure another tag, or when the tag was moved upstream and
  fetched (see [Detect moved tags](moved-tags.md)).

`--tag-pattern <pattern>`
: The submodule follows the highest version tag that matches a glob, such
  as `v6.6.*`. Pre-release tags count only with `update --include-prerelease`.

`--commit <commit>`
: The submodule stays at one commit. `update` only checks that the commit
  exists and checks it out.

Switching to another mode rewrites both keys. The native `branch` key is
written for branch mode and removed for every other mode:

```console
$ lazysubmodules set fpga.core --branch main
fpga.core: tracks branch main
$ git config -f .gitmodules --get-regexp '^submodule\.fpga\.core\.'
submodule.fpga.core.path ip/fpga-core
submodule.fpga.core.url https://git.example.invalid/fpga-core.git
submodule.fpga.core.lsm-mode branch
submodule.fpga.core.lsm-ref main
submodule.fpga.core.branch main
$ lazysubmodules update --dry-run fpga.core
would update fpga.core: tag v2.3.1 (556baa1) -> branch main (7c6069a)
$ lazysubmodules set fpga.core --tag v2.3.1
fpga.core: tracks tag v2.3.1
$ git config -f .gitmodules --get-regexp '^submodule\.fpga\.core\.'
submodule.fpga.core.path ip/fpga-core
submodule.fpga.core.url https://git.example.invalid/fpga-core.git
submodule.fpga.core.lsm-mode tag
submodule.fpga.core.lsm-ref v2.3.1
```

When the mode changes, the dry run shows it on both sides. `update` also
rewrites the native `branch` key when someone edited it by hand.

## Pin a release series

A tag pattern is the usual way to follow a release series: new patch
releases arrive with every `update`, and a new major or minor version
does not, until you widen the pattern.

| Pattern | Follows | Example selection in the demo |
|---|---|---|
| `v6.6.*` | Patch releases of 6.6 | `v6.6.10` |
| `v6.*` | Every 6.x release | `v6.6.10` (`v6.7-rc1` is a pre-release) |
| `v2.*` | Every 2.x release | `v2.9.0` for `sdk` |
| `v*` | Every release | `v3.0.0-rc.2` for `sdk`, kept from the lock file |

- **Glob syntax.** The pattern is a glob as `git tag --list` accepts it.
  `*` also matches dots, so `v6.6*` would match `v6.60.0` as well; write
  `v6.6.*` for the 6.6 series.
- **Version order.** Candidates are sorted as versions, so `v6.6.10` is
  above `v6.6.9`, and a release candidate is below its release.
- **Pre-releases.** A tag with a `-` anywhere after its first digit, such
  as `v6.6.11-rc1`, is a pre-release; `release-2.1` is not. The pattern skips it
  unless `update` gets `--include-prerelease`.
- **Never downgrade.** A pre-release that the lock file already records
  stays selected until a newer tag exists; see
  {ref}`expl-resolution-never-downgrade`.

Check a pattern against the local tags before you set it. This is the
same sort that LazySubmodules uses:

```console
$ git -C kernel -c versionsort.suffix=- tag --list 'v6.6.*' --sort=-v:refname
v6.6.11-rc1
v6.6.10
v6.6.9
v6.6.8
v6.6.7
v6.6.6
v6.6.5
v6.6.4
v6.6.3
v6.6.2
v6.6.1
```

The first entry that is not a pre-release is the one `update` selects,
unless the lock file records a pre-release that is still a candidate.

`set` does not check whether a pattern matches a tag. A pattern that
matches no local tag makes the submodule
[missing-ref]{.lsm-state .lsm-state-missing-ref}, and `update` refuses it:

```console
$ lazysubmodules set kernel --tag-pattern 'v7.*'
kernel: tracks tag-pattern v7.*
$ lazysubmodules update --dry-run kernel
lazysubmodules: kernel: refused: ref not found in local refs: no tag matching v7.* names a commit
```

Run `lazysubmodules fetch kernel` first when the new series was released
after your last fetch.

## Move to a specific tag

In tag mode, `update` moves to the configured tag even when it is older
than the locked one. The "never downgrade" rule applies only to tag
patterns:

```console
$ lazysubmodules set fpga.core --tag v2.3.0
fpga.core: tracks tag v2.3.0
$ lazysubmodules update --dry-run fpga.core
would update fpga.core: v2.3.1 (556baa1) -> v2.3.0 (a926cfc)
```

## Track a commit

An abbreviated SHA is expanded when the commit is available locally, and
`.gitmodules` always records the full SHA:

```console
$ lazysubmodules set 'crypto lib' --commit 5da6927
crypto lib: tracks commit 5da6927f70f5cb5408fe3a77c0aa33877a3062c5
```

Otherwise, give the full SHA. Both failures are usage errors (exit
status 2):

```console
$ lazysubmodules set u-boot --commit 1234567
lazysubmodules: u-boot: invalid commit "1234567": commit 1234567 does not exist or is ambiguous
$ lazysubmodules set fresh --commit f2de3e1
lazysubmodules: fresh: invalid commit "f2de3e1": cannot be expanded without the submodule repository; give the full commit name
```

Names with spaces need quotes in the shell, like any other argument.

## Adopt an unmanaged submodule

LazySubmodules leaves a submodule without `lsm-mode` alone, even when it
is named explicitly. In the demo, `legacy` is such a submodule:

```{literalinclude} ../../examples/transcript.txt
:language: console
:start-at: "$ lazysubmodules update legacy"
:end-at: "[exit status 3: refused]"
```

`set` puts it under control. For branch mode it writes the native
`branch` key as well:

```{literalinclude} ../../examples/transcript.txt
:language: console
:start-at: "$ lazysubmodules set legacy --branch main"
:end-at: "submodule.legacy.lsm-ref main"
```

The submodule is now [behind]{.lsm-state .lsm-state-behind}, because it has
no lock entry yet:

```{literalinclude} ../../examples/transcript.txt
:language: console
:start-at: "$ lazysubmodules status legacy"
:end-at: "[exit status 0: success]"
```

`update` records it. The commit message marks the old commit as
`(unlocked)`:

```{literalinclude} ../../examples/transcript.txt
:language: console
:start-at: "$ lazysubmodules update --commit legacy"
:end-at: "Signed-off-by: Dana Developer"
```

The `[exit status N: …]` lines come from the demo, not from
LazySubmodules. To adopt many branch-tracking submodules at once, see
{ref}`guide-plain-git-migrate`.

## Errors

| Mistake | Message | Exit status |
|---|---|---|
| No tracking option | `set: one of --branch, --tag, --tag-pattern or --commit is required` | 2 |
| Two tracking options | `set: only one of --branch, --tag, --tag-pattern or --commit may be given` | 2 |
| Invalid tag name | `kernel: invalid tag "bad..ref": not a valid tag name` | 2 |
| Ref that starts with `-` | `u-boot: invalid branch "-x": starts with "-"` | 2 |
| Unknown submodule | `nosuch: no such submodule` | 1 |

Every message starts with `lazysubmodules: `, and the first two add the
line `Run 'lazysubmodules help' for usage.` Branch and tag names are
checked with `git check-ref-format`.

## In the terminal interface

The keys {kbd}`b`, {kbd}`t` and {kbd}`p` do the same as `set` with
`--branch`, `--tag` and `--tag-pattern`: they pick a branch, pick a tag,
or take a pattern, and change the configuration after a confirmation.
Press {kbd}`u` or {kbd}`U` afterwards to update. See
[Use the terminal interface](tui.md).

## See also

- {ref}`ref-cli-set`: the complete reference.
- {ref}`ref-files-gitmodules`: every key in `.gitmodules`.
- [Tracking modes and resolution](../explanation/resolution.md).
