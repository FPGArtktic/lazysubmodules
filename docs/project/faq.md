<!-- SPDX-License-Identifier: GPL-3.0-only -->
<!-- Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com> -->

(project-faq)=

# FAQ

Short answers to the questions that come up most often, with links to
the pages that answer them in full.

## Using it with other people

**Does everyone on the team need LazySubmodules?**
No. The superproject records ordinary gitlinks, and the extra
configuration lives in keys that Git ignores. A plain
`git clone` followed by `git submodule update --init` gives the recorded
commits. See [Work with plain Git](../guide/plain-git.md).

**Can we keep using `git submodule update --remote`?**
Yes for branch-tracking submodules: LazySubmodules writes the native
`branch` key for them. For submodules that follow a tag, a tag pattern or
a commit, there is no `branch` key, and `--remote` moves them to the tip
of the remote's default branch instead.

**What happens when two branches change `.lsm.lock`?**
Git merges it like any other text file, and a real conflict has to be
resolved. While the file holds conflict markers, LazySubmodules stops
with exit status 5 and names the line:

```console
$ lazysubmodules status kernel
lazysubmodules: read .lsm.lock: git config -f .lsm.lock --null --list: exit status 128: fatal: bad config line 53 in file .lsm.lock
```

Resolve the conflict, for example by taking one side, then run
`lazysubmodules update` for the submodules in question and commit the
result.

## Everyday questions

**Why is a submodule still `behind` after I updated it?**
Either the update was not run for it, or it has no lock entry yet, or the
target moved again since the last fetch.
`lazysubmodules update --dry-run <name>` shows what an update would do
now. See [Troubleshooting](../guide/troubleshooting.md).

**Why does `verify` fail right after a successful `update`?**
`verify` reads the committed state. An update that is staged but not
committed fails the `gitlink` check until you commit it, or use
`update --commit`.

**Does `status` contact the server?**
No. Only `fetch`, `update --fetch` and `add` use the network. Everything
else works on the refs of the last fetch; see
[Mirrors, credentials and offline work](../guide/network.md).

**`update` printed refusals and changed nothing. Why not update the rest?**
Because a half-updated superproject is worse than none: the lock file
would describe neither the old nor the new state. Fix what the refusals
name, then run the update again. See
[Safety model](../explanation/safety.md).

**How do I pin a submodule to one commit?**
`lazysubmodules set <name> --commit <sha>`, then `update`. The
configuration records the full SHA, and updates only check that the
commit exists.

**How do I follow a release series?**
With a tag pattern, such as
`lazysubmodules set kernel --tag-pattern 'v6.6.*'`. Quote the pattern.
See [Change what a submodule tracks](../guide/change-tracking.md).

**Are pre-releases ever selected?**
A tag pattern skips them, unless `--include-prerelease` is given or the
lock file already records one, which keeps an update from going back to
an older tag. `tag` mode has no such rule: a pre-release named with
`set <name> --tag v2.0.0-rc.1` is selected as it stands, because you
asked for that tag. See {ref}`expl-resolution-never-downgrade`.

**Does it sign the commits it creates?**
It runs `git commit -s`, which adds the `Signed-off-by` line from
`user.name` and `user.email`. Cryptographic signing happens when your Git
configuration asks for it (`commit.gpgSign`).

**Is `lsm` a different program?**
No, it is an optional short name for the same binary. The packages
install it as a symbolic link. Messages always say `lazysubmodules`.

## Limits

**Does it handle submodules inside submodules?**
Not recursively: nested submodules are neither initialized nor updated.
`lazysubmodules foreach -- git submodule update --init --recursive`
brings them to the commits their parents record; see
{ref}`guide-foreach-nested`.

**Can it use a remote other than `origin`?**
No. Branch tracking resolves `refs/remotes/origin/<branch>`, and `fetch`
fetches from `origin`.

**Does it run on Windows or macOS?**
No. Releases are built for Linux on `amd64` and `arm64`.

**Which Git versions work?**
Git 2.39 and later. The test suite runs against Git 2.39.5 as well as a
current Git.

**Does it work in a linked worktree (`git worktree add`)?**
Yes; the worktree is a superproject like any other. Its submodules start
out uninitialized, so run `lazysubmodules fetch` or
`lazysubmodules update --fetch` there first.

**Is it a replacement for `repo`, `west` or `git subtree`?**
No. It manages the submodules of one superproject and nothing else, and
it does not manage credentials. See
[Design and limitations](../explanation/design.md).

## The project

**Is there a release yet?**
Yes: `v0.1.0-rc.1`, a release candidate, with archives and Debian and
RPM packages for `amd64` and `arm64`; Arch Linux has the AUR package
`lazysubmodules-git`. See
[Installation](../getting-started/installation.md) and
[Releases and versions](releases.md).

**How do I report a bug or ask for a feature?**
Through the issue forms on
[GitHub](https://github.com/FPGArtktic/lazysubmodules/issues). Include the
output of `lazysubmodules version`. Report security problems privately;
see [Security policy](security.md).
