<!-- SPDX-License-Identifier: GPL-3.0-only -->
<!-- Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com> -->

(index)=

# [Lazy]{.lsm-accent}Submodules

Track Git submodules by branch, tag, tag pattern or commit, with a lock
file that catches moved tags.

::::{container} lsm-hero

{.lsm-tagline}
A command line tool for scripts and a terminal interface for people.
Configuration lives in `.gitmodules`, all work is done by `git`, and only
`fetch`, `update --fetch`, `add` and the fetch key of the terminal
interface use the network.

:::{container} lsm-hero-actions

```{button-ref} getting-started/quickstart
:ref-type: doc
:color: primary
:shadow:

Get started
```

```{button-ref} getting-started/installation
:ref-type: doc
:color: secondary
:outline:

Install
```

```{button-link} https://github.com/FPGArtktic/lazysubmodules
:color: secondary
:outline:

{octicon}`mark-github` GitHub
```

:::

:::{container} lsm-terminal

```{image} demo/hero.gif
---
alt: >-
  The LazySubmodules terminal interface on a superproject with fourteen
  submodules: a table with the name, mode, ref, lock and colored state of
  each submodule, and a preview of the selected one. The recording moves
  through the table, opens the details of u-boot, updates u-boot after a
  confirmation until its state turns ok, and shows the help page.
---
```

:::

{.lsm-caption}
The terminal interface on the demo superproject.

::::

## What it does

::::{grid} 1 2 2 3
:gutter: 3

:::{grid-item-card} {octicon}`git-branch` Four tracking modes
:link: explanation/resolution
:link-type: doc
:class-card: lsm-card

Follow a branch, a tag, the highest version tag matching a glob such as
`v2.*`, or a fixed commit.
:::

:::{grid-item-card} {octicon}`lock` Lock file
:link: guide/moved-tags
:link-type: doc
:class-card: lsm-card

`.lsm.lock` records what each submodule resolved to, so a tag moved
upstream shows up as `drift`.
:::

:::{grid-item-card} {octicon}`shield-check` Safe updates
:link: explanation/safety
:link-type: doc
:class-card: lsm-card

Every selected submodule is checked first; one refusal means nothing
changes.
:::

:::{grid-item-card} {octicon}`terminal` Terminal interface
:link: guide/tui
:link-type: doc
:class-card: lsm-card

Browse states, preview updates and re-target submodules with single keys.
:::

:::{grid-item-card} {octicon}`code` Made for scripts
:link: guide/scripting
:link-type: doc
:class-card: lsm-card

A stable porcelain format, `verify` with signature checks, and distinct
exit codes.
:::

:::{grid-item-card} {octicon}`git-merge` Native Git underneath
:link: guide/plain-git
:link-type: doc
:class-card: lsm-card

Plain `git submodule update --init` still works for everyone else.
:::

::::

## Install

LazySubmodules runs on Linux (`amd64` and `arm64`) and needs Git 2.39 or
later.

{.lsm-note}
The newest release, `v0.1.0-rc.1`, is a release candidate: it has not been
used against remotes over a network yet.

:::::{tab-set}
:sync-group: install

::::{tab-item} Release archive
:sync: archive

```sh
VERSION=0.1.0-rc.1   # release version without the leading "v"
ARCH=amd64      # or arm64
BASE="https://github.com/FPGArtktic/lazysubmodules/releases/download/v${VERSION}"
curl -fLO "${BASE}/lazysubmodules_${VERSION}_linux_${ARCH}.tar.gz"
tar -xzf "lazysubmodules_${VERSION}_linux_${ARCH}.tar.gz"
sudo install -m 0755 lazysubmodules /usr/local/bin/lazysubmodules
```
::::

::::{tab-item} Debian/Ubuntu
:sync: deb

```sh
VERSION=0.1.0-rc.1 ARCH=amd64
curl -fLO "https://github.com/FPGArtktic/lazysubmodules/releases/download/v${VERSION}/lazysubmodules_${VERSION}_linux_${ARCH}.deb"
sudo apt install "./lazysubmodules_${VERSION}_linux_${ARCH}.deb"
```
::::

::::{tab-item} Fedora/RPM
:sync: rpm

```sh
VERSION=0.1.0-rc.1 ARCH=amd64
curl -fLO "https://github.com/FPGArtktic/lazysubmodules/releases/download/v${VERSION}/lazysubmodules_${VERSION}_linux_${ARCH}.rpm"
sudo dnf install "./lazysubmodules_${VERSION}_linux_${ARCH}.rpm"
```
::::

::::{tab-item} Arch Linux
:sync: aur

```sh
yay -S lazysubmodules-git     # or: paru -S lazysubmodules-git
```
::::

::::{tab-item} Go
:sync: go

```sh
go install github.com/FPGArtktic/lazysubmodules/cmd/lazysubmodules@latest
```
::::

::::{tab-item} From source
:sync: source

```sh
git clone https://github.com/FPGArtktic/lazysubmodules.git
cd lazysubmodules
scripts/build-in-container.sh build   # binary in bin/lazysubmodules
```
::::

:::::

[Installation](getting-started/installation.md) covers every method, the
installed files and how to verify a download.

## First steps

```sh
# Let the existing submodule "kernel" follow the newest stable v6.6.x tag.
lazysubmodules set kernel --tag-pattern 'v6.6.*'

# Fetch, resolve, check out, update .lsm.lock and commit the result.
lazysubmodules update kernel --fetch --commit

# Add a new submodule that follows the branch "main" (changes are staged).
lazysubmodules add https://git.example.org/u-boot.git u-boot --branch main

# Inspect and verify.
lazysubmodules status
lazysubmodules verify

# Or work interactively.
lazysubmodules tui
```

Quote glob patterns such as `'v6.6.*'`, so that the shell does not expand
them. The [quick start](getting-started/quickstart.md) walks through these
steps one at a time.

## Why LazySubmodules

Native Git tracks only branches: set `submodule.<name>.branch` and run
`git submodule update --remote`. There is no native way to say "this
submodule follows tag `v2.3.1`" or "this submodule follows the newest
`v6.6.*` release", so projects that pin dependencies to releases update
them by hand.

LazySubmodules adds tags, tag patterns and fixed commits without breaking
native Git. Its configuration lives in namespaced keys that Git ignores,
the superproject still records ordinary gitlinks, and a plain
`git submodule update --init` works for anyone who does not use it.

It is not a replacement for `repo`, `west`, `git subtree` or monorepo
tooling, and it does not manage credentials: Git uses its configured
credential helpers.

## See it work

:::::{tab-set}

::::{tab-item} Command line

:::{container} lsm-terminal

```{image} demo/cli.gif
---
alt: >-
  A terminal session with the command line interface: lazysubmodules
  status prints a table of all submodules and their states; status
  --porcelain=v1, laid out with column, shows the header line and the
  tab-separated records; update --dry-run lists the planned changes;
  update --commit updates kernel and u-boot and prints the new commit; git
  log shows the generated commit message with one block per submodule and
  the Signed-off-by line.
loading: lazy
---
```

:::

`status`, the porcelain format, a dry run and `update --commit` with its
generated message. See [Update submodules](guide/update.md).
::::

::::{tab-item} Tag patterns

:::{container} lsm-terminal

```{image} demo/tag-pattern.gif
---
alt: >-
  The tag pattern dialog of the terminal interface. The pattern of
  kernel changes from v6.6.* to v6.*, and the dialog counts the matching
  local tags while it is typed, including the pre-releases v6.7-rc1 and
  v6.6.11-rc1. After the confirmation, the update picks v6.6.10 and skips
  the pre-releases, and lazysubmodules status shows v6.6.10 as the locked
  tag.
loading: lazy
---
```

:::

A wider pattern still selects the newest stable tag. See
[Tracking modes and resolution](explanation/resolution.md).
::::

::::{tab-item} Moved tag

:::{container} lsm-terminal

```{image} demo/drift.gif
---
alt: >-
  A release tag moved upstream: fpga.core follows tag v2.3.1 and is
  ok. The upstream repository moves the tag to another commit. After
  lazysubmodules fetch, status reports drift, and verify names the moved
  tag and exits with status 4.
loading: lazy
---
```

:::

The lock file exposes a release tag that was moved upstream. See
[Detect moved tags](guide/moved-tags.md).
::::

::::{tab-item} Refused update

:::{container} lsm-terminal

```{image} demo/safety.gif
---
alt: >-
  A refused update: kernel is behind, app has an uncommitted change
  and fresh was never cloned. update kernel app fresh prints both
  refusals and exits with status 3, and status shows that nothing
  changed, not even kernel. After the change in app is discarded, update
  --fetch clones fresh through the mirror and updates kernel.
loading: lazy
---
```

:::

One unsafe submodule stops the whole update. See
[Safety model](explanation/safety.md).
::::

:::::

## Where next

::::{grid} 1 2 2 4
:gutter: 3

:::{grid-item-card} {octicon}`play` Tour the demo
:link: getting-started/tour
:link-type: doc
:class-card: lsm-card

Fourteen submodules in every state, built offline.
:::

:::{grid-item-card} {octicon}`book` Guides
:link: guide/update
:link-type: doc
:class-card: lsm-card

Update, commit, verify and automate.
:::

:::{grid-item-card} {octicon}`terminal` CLI reference
:link: reference/cli/index
:link-type: doc
:class-card: lsm-card

Every command, option and exit code.
:::

:::{grid-item-card} {octicon}`people` Contributing
:link: project/contributing
:link-type: doc
:class-card: lsm-card

Build, test and send a change.
:::

::::

```{toctree}
:hidden:
:caption: Getting started

getting-started/installation
getting-started/tour
getting-started/quickstart
```

```{toctree}
:hidden:
:caption: Guides

guide/add-submodule
guide/change-tracking
guide/update
guide/commit-updates
guide/moved-tags
guide/tui
guide/scripting
guide/signatures
guide/foreach
guide/network
guide/plain-git
guide/troubleshooting
```

```{toctree}
:hidden:
:caption: Reference

reference/cli/index
reference/files
reference/states
reference/porcelain
reference/exit-codes
reference/commit-messages
reference/tui
reference/runtime
reference/glossary
```

```{toctree}
:hidden:
:caption: Explanation

explanation/how-it-works
explanation/resolution
explanation/safety
explanation/design
```

```{toctree}
:hidden:
:caption: Project

project/contributing
project/documentation
project/releases
project/security
project/license
project/faq
GitHub <https://github.com/FPGArtktic/lazysubmodules>
```
