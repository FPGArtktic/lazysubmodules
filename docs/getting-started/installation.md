<!-- SPDX-License-Identifier: GPL-3.0-only -->
<!-- Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com> -->

(start-installation)=

# Installation

LazySubmodules runs on Linux (amd64 and arm64) and needs Git 2.39 or
later. Install it from a release, a package, Go or the source.

## Requirements

- **System:** Linux on `amd64` or `arm64`. Windows and macOS are not
  supported.
- **Git:** version 2.39 or later at run time. LazySubmodules does all its
  work through the `git` command, so `git` must be on `PATH`.
- **Terminal interface:** a terminal that can move the cursor; see
  {ref}`guide-tui`.

The binaries in the release archives and in the `.deb` and `.rpm`
packages are statically linked and need no C library; a build with a host
Go toolchain and the AUR package link against the C library of the system
instead. Either way the binary contains the Go standard library and a few
Go modules under the MIT and BSD-3-Clause licenses, whose notices come
with every release archive and package (see
[License](../project/license.md)).

:::{important}
The newest release, `v0.1.0-rc.1`, is a release candidate: it has not been
used against remotes over a network yet, and nobody has run the `arm64`
build. Everything below installs it.
:::

## Choose a method

Every release on the
[GitHub releases page](https://github.com/FPGArtktic/lazysubmodules/releases)
has archives and packages for `amd64` and `arm64`. In the commands below,
set `VERSION` to the release version without the leading `v`.

::::::{tab-set}
:sync-group: install

:::::{tab-item} Release archive
:sync: archive

The archive `lazysubmodules_<version>_linux_<arch>.tar.gz` contains the
binary, `LICENSE`, `README.md`, and the notices of the third-party code in
the binary: `THIRD_PARTY_NOTICES` lists each module with its version and
license, and `licenses/` holds the license texts.

```sh
VERSION=0.1.0-rc.1   # release version without the leading "v"
ARCH=amd64      # or arm64
BASE="https://github.com/FPGArtktic/lazysubmodules/releases/download/v${VERSION}"
curl -fLO "${BASE}/lazysubmodules_${VERSION}_linux_${ARCH}.tar.gz"
tar -xzf "lazysubmodules_${VERSION}_linux_${ARCH}.tar.gz"
sudo install -m 0755 lazysubmodules /usr/local/bin/lazysubmodules
sudo ln -s lazysubmodules /usr/local/bin/lsm   # optional short name
```

Keep `LICENSE`, `THIRD_PARTY_NOTICES` and `licenses/` with the binary when
you pass it on.
:::::

:::::{tab-item} Debian/Ubuntu
:sync: deb

The `.deb` package depends on `git`, which `apt` installs when it is
missing.

```sh
VERSION=0.1.0-rc.1   # release version without the leading "v"
ARCH=amd64      # or arm64
BASE="https://github.com/FPGArtktic/lazysubmodules/releases/download/v${VERSION}"
curl -fLO "${BASE}/lazysubmodules_${VERSION}_linux_${ARCH}.deb"
sudo apt install "./lazysubmodules_${VERSION}_linux_${ARCH}.deb"
```

The package itself is not signed. Check it against the signed
`checksums.txt` first; see {ref}`project-releases-verify`.
:::::

:::::{tab-item} Fedora/RPM
:sync: rpm

The `.rpm` package depends on `git`, which `dnf` installs when it is
missing.

```sh
VERSION=0.1.0-rc.1   # release version without the leading "v"
ARCH=amd64      # or arm64
BASE="https://github.com/FPGArtktic/lazysubmodules/releases/download/v${VERSION}"
curl -fLO "${BASE}/lazysubmodules_${VERSION}_linux_${ARCH}.rpm"
sudo dnf install "./lazysubmodules_${VERSION}_linux_${ARCH}.rpm"
```

The package itself is not signed. Check it against the signed
`checksums.txt` first; see {ref}`project-releases-verify`.
:::::

:::::{tab-item} Arch Linux
:sync: aur

The package is
[`lazysubmodules-git`](https://aur.archlinux.org/packages/lazysubmodules-git).
With an AUR helper:

```sh
yay -S lazysubmodules-git     # or: paru -S lazysubmodules-git
```

Without one, or to read the recipe before you build it (`makepkg` comes
with the `base-devel` group):

```sh
git clone https://aur.archlinux.org/lazysubmodules-git.git
cd lazysubmodules-git
makepkg -si
```

- The recipe clones the default branch from GitHub; it does not build your
  local checkout. It builds with the vendored Go modules and runs the test
  suite.
- `-s` installs the build dependencies (`go`, `go-licenses`, and
  `openssh` for the tests), and `-i` installs the package.
- The package version is derived from the Git history at build time, so it
  follows the default branch: `0.1.0rc1.r0.gdd57e1b` at the commit that
  `v0.1.0-rc.1` tags, `0.1.0.r3.g1234abc` three commits after a `v0.1.0`
  release. A
  pre-release loses its separators, so that `vercmp` sorts it below the
  release of the same version.
:::::

:::::{tab-item} Go
:sync: go

(start-installation-go)=

With Go 1.27.1 or later:

```sh
go install github.com/FPGArtktic/lazysubmodules/cmd/lazysubmodules@latest
```

- The binary goes to `$(go env GOPATH)/bin`, or to `GOBIN` when it is set.
  Make sure that directory is on `PATH`.
- `version` prints the module version with its leading `v`, but neither
  the commit nor the commit date (`commit: none`, `date: unknown`).
- The binary comes without the third-party license notices;
  `go version -m "$(command -v lazysubmodules)"` lists the modules it
  contains.
:::::

:::::{tab-item} From source
:sync: source

(start-installation-source)=

The supported build runs in a container with a pinned toolchain. It needs
an x86_64 Linux host with Podman (preferred) or Docker:

```sh
git clone https://github.com/FPGArtktic/lazysubmodules.git
cd lazysubmodules
scripts/build-in-container.sh build   # binary in bin/lazysubmodules
```

With a host Go toolchain (Go 1.27.1 or later) instead:

```sh
go build -trimpath -o lazysubmodules ./cmd/lazysubmodules
```

Both builds use the vendored modules in `vendor/` and download nothing.
Install the result as shown for the release archive. A binary you build
yourself comes without the third-party notices;
`scripts/third-party-licenses.sh` collects them (see
[CONTRIBUTING.md](https://github.com/FPGArtktic/lazysubmodules/blob/main/CONTRIBUTING.md#third-party-notices)).
:::::

::::::

## Installed files

Every package installs `/usr/bin/lazysubmodules` and the short name
`/usr/bin/lsm`, a symbolic link. The program behaves the same under both
names, and its messages always say `lazysubmodules`. They differ only in
where they put the documentation and the license files:

`.deb`
: `README.md` and `LICENSE` under `/usr/share/doc/lazysubmodules/`, and
  the Debian copyright file
  `/usr/share/doc/lazysubmodules/copyright` with the third-party notices.

`.rpm`
: `README.md` under `/usr/share/doc/lazysubmodules/`, and `LICENSE`,
  `THIRD_PARTY_NOTICES` and `licenses/` under
  `/usr/share/licenses/lazysubmodules/`. `rpm -qL lazysubmodules` lists
  the first two, the files marked `%license`; the texts in `licenses/`
  are plain files, which `rpm -ql` lists.

AUR `lazysubmodules-git`
: `README.md` under `/usr/share/doc/lazysubmodules-git/`, and `LICENSE`,
  `THIRD_PARTY_NOTICES` and `licenses/` under
  `/usr/share/licenses/lazysubmodules-git/`.

The archive holds the same files next to the binary, and the `.deb` and
`.rpm` packages depend on `git`.

## Check the installation

`lazysubmodules version` prints the version, the source commit and the
date of that commit:

```text
lazysubmodules 0.1.0-rc.1
commit: dd57e1b64d7d7faea17a5969041dde0387ed2bf1
date: 2026-09-17T19:42:54Z
```

A binary from `go install`, `go build` or the AUR recipe prints a
different version string; {ref}`ref-cli-version-builds` lists every form.

`lazysubmodules help` lists the commands:

```{literalinclude} ../reference/cli/_generated/lazysubmodules.txt
:language: text
:lines: 5-
```

## Verify a download

Release checksums are signed with cosign keyless signing, and release
tags are signed with `git tag -s`. {ref}`project-releases-verify` shows
how to check both before you install.

## Next steps

- [Tour of the demo](tour.md): try every command on a prepared
  superproject, without touching your own repositories.
- [Quick start](quickstart.md): put a submodule of your own project under
  LazySubmodules.
- {ref}`ref-cli-version`: the `version` command in detail.
