# SPDX-License-Identifier: GPL-3.0-only
# Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>
#
# Build image for LazySubmodules, based on Arch Linux.
#
# Build it with "scripts/build-in-container.sh image". Every other target of
# that script runs inside this image, so developer machines and CI share one
# toolchain. Tools are downloaded only here, never while the project is built.
#
# Reproducibility comes from four pins, all kept current by
# scripts/update-builder-pins.sh:
#
#   * the dated archlinux:base-devel tag, pinned by digest;
#   * the Arch Linux Archive snapshot of the same day: pacman installs and
#     upgrades every package from that immutable snapshot, never from a
#     live mirror;
#   * the commits of the AUR packages (goreleaser-bin, gitlint), fetched by
#     commit ID from the official GitHub mirror of the AUR;
#   * the go-licenses release, verified against the Go checksum database.
#
# Building needs BuildKit or podman/buildah: the final stage uses
# "RUN --mount", which the legacy docker builder does not support.
#
# The official Arch Linux image exists for x86_64 (linux/amd64) only, so this
# image does too.
#
# There is deliberately no SHELL instruction: it is not part of the OCI image
# format and podman warns about it. RUN steps use /bin/sh, which is bash on
# Arch Linux and supports "set -o pipefail".
#
# The container runs as the invoking host user (podman --userns=keep-id or
# docker --user), not as root. Everything installed is therefore world-readable,
# the cache directories are world-writable (mode 1777, so that fresh named
# volumes inherit a writable mode), and nothing writes into the image at run
# time.

ARG ARCH_IMAGE_TAG=base-devel-20260913.0.592969
ARG ARCH_IMAGE_DIGEST=sha256:70d777aaeb45befc04150df137c4d7c1b5042be442b4c904c38c6f6880bb7844
ARG ARCH_ARCHIVE_DATE=2026/09/13

# ---------------------------------------------------------------------------
# base: the Arch Linux image, upgraded to the archive snapshot.
# ---------------------------------------------------------------------------
FROM docker.io/library/archlinux:${ARCH_IMAGE_TAG}@${ARCH_IMAGE_DIGEST} AS base

# TARGETARCH is set by podman/buildah and BuildKit, the builders this file
# needs. The fallback to the architecture of the build container only covers
# a builder that leaves it unset.
ARG TARGETARCH
ARG ARCH_ARCHIVE_DATE

# pacman configuration:
#
#   * The only server is the archive snapshot. pacman.conf of the image lists
#     the mirrorlist and itself in NoExtract, so upgrades keep this setup.
#   * The download sandbox is disabled and downloads are not handed to the
#     unprivileged "alpm" user. Both fail in some container builds: switching
#     users needs subordinate IDs, which rootless engines do not always map,
#     and the landlock and seccomp filters of pacman 7 clash with older
#     container seccomp profiles. Every package is still verified against its
#     signature before it is installed.
#
# Keyring: the image ships public keys without a local master key, so
# pacman-key cannot certify keys added by a newer archlinux-keyring. A fresh
# master key is created, the keyring is upgraded first, and the private key is
# deleted afterwards, as in the upstream image. Signatures made by a packager
# key that has expired since the snapshot are rejected; bumping the pins fixes
# that.
#
# "-Suu" upgrades, and where the image is newer than the snapshot downgrades,
# every package to the exact snapshot version.
RUN set -euo pipefail; \
    arch="${TARGETARCH:-$(uname -m)}"; \
    case "$arch" in \
    amd64 | x86_64) ;; \
    *) \
      echo "unsupported architecture: $arch (the Arch Linux base image is x86_64 only)" >&2; \
      exit 1 ;; \
    esac; \
    case "$ARCH_ARCHIVE_DATE" in \
    [0-9][0-9][0-9][0-9]/[0-9][0-9]/[0-9][0-9]) ;; \
    *) \
      echo "ARCH_ARCHIVE_DATE must be YYYY/MM/DD: $ARCH_ARCHIVE_DATE" >&2; \
      exit 1 ;; \
    esac; \
    server="https://archive.archlinux.org/repos/${ARCH_ARCHIVE_DATE}"; \
    printf 'Server = %s/$repo/os/$arch\n' "$server" > /etc/pacman.d/mirrorlist; \
    sed -i 's/^[[:space:]]*DownloadUser[[:space:]]*=/#&/' /etc/pacman.conf; \
    printf '\n[options]\nDisableSandbox\n' >> /etc/pacman.conf; \
    conf="$(pacman-conf DownloadUser DisableSandbox | tr '\n' ' ')"; \
    test "$conf" = "DisableSandboxFilesystem DisableSandboxSyscalls "; \
    test "$(pacman-conf --repo=core Server)" = "${server}/core/os/x86_64"; \
    test "$(pacman-conf --repo=extra Server)" = "${server}/extra/os/x86_64"; \
    pacman-key --init; \
    pacman-key --populate archlinux; \
    pacman -Syy --noconfirm --needed archlinux-keyring; \
    pacman -Suu --noconfirm; \
    gpgconf --homedir /etc/pacman.d/gnupg --kill all; \
    rm -rf /etc/pacman.d/gnupg/openpgp-revocs.d/* \
      /etc/pacman.d/gnupg/private-keys-v1.d/* \
      /etc/pacman.d/gnupg/pubring.gpg~ \
      /etc/pacman.d/gnupg/S.* \
      /var/cache/pacman/pkg/*

# ---------------------------------------------------------------------------
# aur: build the AUR packages as an unprivileged user.
# ---------------------------------------------------------------------------
FROM base AS aur

# Commits of the package branches on https://github.com/archlinux/aur.git.
# The versions and the GoReleaser checksum are asserted against the fetched
# recipes. AUR_GORELEASER_SHA256 is the upstream checksum of
# goreleaser_Linux_x86_64.tar.gz (checksums.txt of the GoReleaser release), an
# independent cross-check of the AUR recipe.
#
# RUN sees build arguments as environment variables. The AUR_ prefix keeps
# them apart from the GITLINT_* variables that gitlint reads as options.
ARG AUR_GORELEASER_COMMIT=64c5771095b489ad090d06ce62e132f480891b7e
ARG AUR_GORELEASER_VERSION=2.18.1
ARG AUR_GORELEASER_SHA256=0c6122af0ad8fd65638889bf7d3757148b2f80eeff9f079682f0655df66ec8e8
ARG AUR_GITLINT_COMMIT=37d34d511deecb9636d30b6a4fa1f8a65ace1226
ARG AUR_GITLINT_VERSION=0.19.1

# makepkg packs the files as the recipes install them: prebuilt binaries are
# not stripped, man pages are not recompressed, and no debug packages are
# built.
RUN set -euo pipefail; \
    pacman -S --noconfirm --needed git; \
    rm -rf /var/cache/pacman/pkg/*; \
    echo 'OPTIONS+=(!strip !zipman !debug)' > /etc/makepkg.conf.d/lazysubmodules.conf; \
    useradd --create-home --user-group aurbuild; \
    install -d -o aurbuild -g aurbuild /aur /aur/src /aur/pkg

USER aurbuild
WORKDIR /aur

# Fetch exactly the pinned commits. transfer.fsckObjects verifies every
# received object.
RUN set -euo pipefail; \
    srcinfo() { \
      sed -n "s/^[[:space:]]*$2 = //p" "$1/.SRCINFO"; \
    }; \
    fetch() { \
      git init --quiet --initial-branch=aur "$1"; \
      git -C "$1" -c transfer.fsckObjects=true fetch --quiet --depth=1 \
        https://github.com/archlinux/aur.git "$2"; \
      git -C "$1" -c advice.detachedHead=false checkout --quiet FETCH_HEAD; \
      test "$(git -C "$1" rev-parse --verify 'HEAD^{commit}')" = "$2"; \
      test "$(srcinfo "$1" pkgname)" = "$1"; \
    }; \
    fetch goreleaser-bin "$AUR_GORELEASER_COMMIT"; \
    fetch gitlint "$AUR_GITLINT_COMMIT"; \
    test "$(srcinfo goreleaser-bin pkgver)" = "$AUR_GORELEASER_VERSION"; \
    test "$(srcinfo goreleaser-bin sha256sums_x86_64)" = "$AUR_GORELEASER_SHA256"; \
    test "$(srcinfo gitlint pkgver)" = "$AUR_GITLINT_VERSION"

# Build, check and runtime dependencies come from the archive snapshot. They
# are read from .SRCINFO and installed before makepkg runs, which then only
# confirms that they are present.
USER root
RUN set -euo pipefail; \
    sed -En 's/^[[:space:]]*(make|check)?depends(_x86_64)? = //p' /aur/*/.SRCINFO \
      | sort -u | xargs -r pacman -S --noconfirm --needed --asdeps; \
    rm -rf /var/cache/pacman/pkg/*

# makepkg verifies the source checksums of each recipe and runs its check()
# function (gitlint's test suite). The GoReleaser binary in the package must
# be bit-identical to the one in the upstream archive.
USER aurbuild
RUN set -euo pipefail; \
    export SRCDEST=/aur/src PKGDEST=/aur/pkg; \
    for pkg in goreleaser-bin gitlint; do \
      (cd "$pkg" && makepkg --cleanbuild --noconfirm --noprogressbar); \
    done; \
    upstream="${SRCDEST}/goreleaser-bin_${AUR_GORELEASER_VERSION}_x86_64.tar.gz"; \
    echo "${AUR_GORELEASER_SHA256}  ${upstream}" | sha256sum --strict --check --quiet -; \
    test "$(tar -xzOf "$upstream" goreleaser | sha256sum)" = \
      "$(bsdtar -xOf "${PKGDEST}/goreleaser-bin-${AUR_GORELEASER_VERSION}"-*-x86_64.pkg.tar.zst \
        usr/bin/goreleaser | sha256sum)"; \
    ls -l "$PKGDEST"

# ---------------------------------------------------------------------------
# Final image.
# ---------------------------------------------------------------------------
FROM base

ARG ARCH_IMAGE_TAG
ARG ARCH_IMAGE_DIGEST
ARG ARCH_ARCHIVE_DATE

# The labels replace those inherited from the Arch Linux image, which describe
# that image instead of this one.
LABEL org.opencontainers.image.title="lazysubmodules-build" \
      org.opencontainers.image.description="Pinned build toolchain for LazySubmodules" \
      org.opencontainers.image.licenses="GPL-3.0-only" \
      org.opencontainers.image.authors="Mateusz Okulanis <FPGArtktic@outlook.com>" \
      org.opencontainers.image.url="https://github.com/FPGArtktic/lazysubmodules" \
      org.opencontainers.image.source="https://github.com/FPGArtktic/lazysubmodules" \
      org.opencontainers.image.documentation="https://github.com/FPGArtktic/lazysubmodules/blob/main/CONTRIBUTING.md" \
      org.opencontainers.image.version="" \
      org.opencontainers.image.revision="" \
      org.opencontainers.image.created="" \
      org.opencontainers.image.base.name="docker.io/library/archlinux:${ARCH_IMAGE_TAG}" \
      org.opencontainers.image.base.digest="${ARCH_IMAGE_DIGEST}" \
      io.github.fpgartktic.lazysubmodules.arch-archive-date="${ARCH_ARCHIVE_DATE}"

# Toolchain from the archive snapshot. base-devel provides gcc for cgo, which
# the race detector needs.
RUN set -euo pipefail; \
    pacman -S --noconfirm --needed \
      catatonit cosign git gnupg go golangci-lint openssh python shellcheck \
      syft; \
    rm -rf /var/cache/pacman/pkg/*

# The AUR packages; their dependencies come from the snapshot.
RUN --mount=type=bind,from=aur,source=/aur/pkg,target=/tmp/aur-pkg \
    set -euo pipefail; \
    pacman -U --noconfirm /tmp/aur-pkg/*.pkg.tar.zst; \
    rm -rf /var/cache/pacman/pkg/*

# go-licenses has no release binaries. The module and its dependencies are
# verified against go.sum and the Go checksum database; temporary caches keep
# the build output out of the image.
#
# go-licenses recognizes standard library packages by the GOROOT prefix taken
# from runtime.GOROOT(), which is empty for a -trimpath binary unless GOROOT is
# set. Without it every package counts as standard library and "check" passes
# without checking anything. The build therefore proves both directions on
# go-licenses' own dependencies: cobra is reported as Apache-2.0, and a check
# that allows only MIT fails because of it.
#
# Arch builds Go with DWARF 5 disabled by default, for the sake of its debug
# package tooling. GOEXPERIMENT restores the upstream default, so that the Go
# version recorded in binaries and SBOMs is plain "go1.27.1" rather than
# "go1.27.1-X:nodwarf5", which version parsers do not understand.
ARG GO_LICENSES_VERSION=2.0.1
ENV GOROOT=/usr/lib/go \
    GOTOOLCHAIN=local \
    GOEXPERIMENT=dwarf5

RUN set -euo pipefail; \
    test "$(go env GOROOT)" = "$GOROOT"; \
    tmp="$(mktemp -d)"; \
    export GOCACHE="$tmp/build" GOMODCACHE="$tmp/mod" GOPATH="$tmp/path" \
      GOFLAGS=-mod=mod CGO_ENABLED=0; \
    GOBIN=/usr/local/bin go install -trimpath \
      "github.com/google/go-licenses/v2@v${GO_LICENSES_VERSION}"; \
    chmod 0755 /usr/local/bin/go-licenses; \
    case "$(go version /usr/local/bin/go-licenses)" in \
    *-X:*) \
      echo "binaries record Go experiments: $(go version /usr/local/bin/go-licenses)" >&2; \
      exit 1 ;; \
    esac; \
    cd "$GOMODCACHE/github.com/google/go-licenses/v2@v${GO_LICENSES_VERSION}"; \
    go-licenses report . 2>/dev/null | grep -q '^github.com/spf13/cobra,.*,Apache-2.0$'; \
    if go-licenses check . --allowed_licenses=MIT >"$tmp/check.log" 2>&1; then \
      echo "go-licenses check passed although Apache-2.0 is not allowed" >&2; \
      exit 1; \
    fi; \
    grep -q "Not allowed license 'Apache-2.0'" "$tmp/check.log"; \
    cd /; \
    rm -rf "$tmp"

# Cache locations, mounted as named volumes by scripts/build-in-container.sh.
# The paths must match the volume mounts in that script. No implicit network
# access: no Go toolchain downloads (GOTOOLCHAIN above), no syft update check.
ENV GOCACHE=/cache/go-build \
    GOMODCACHE=/cache/go-mod \
    GOLANGCI_LINT_CACHE=/cache/golangci-lint \
    SYFT_CHECK_FOR_APP_UPDATE=false

# /src is where the script mounts the repository. It is owned by the invoking
# user, so the entry only matters when ownership differs (e.g. sudo checkouts).
#
# The release publishes to the AUR over ssh. Its host key is pinned, so that ssh
# neither trusts the first key it sees nor writes a known_hosts file. The key
# matches the fingerprint SHA256:RFzBCUItH9LZS0cKB5UE6ceAYhBD5C8GeOBip8Z11+4
# published on https://aur.archlinux.org/.
RUN set -eu; \
    install -d -m 1777 /cache/go-build /cache/go-mod /cache/golangci-lint; \
    git config --system --add safe.directory /src; \
    echo 'aur.archlinux.org ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIEuBKrPzbawxA/k2g6NcyV5jmqwJ2s+zpgZGZ7tpLIcN' \
      >> /etc/ssh/ssh_known_hosts; \
    chmod 0644 /etc/ssh/ssh_known_hosts

# Print package and tool versions as an unprivileged user: proves every tool
# is usable by the non-root user the container runs as.
RUN set -euo pipefail; \
    pacman -Q catatonit cosign gcc git gitlint gnupg go golangci-lint \
      goreleaser-bin openssh pacman python shellcheck syft; \
    setpriv --reuid=65534 --regid=65534 --clear-groups env HOME=/tmp sh -euc ' \
      set -o pipefail; \
      go version; \
      gcc --version | head -n 1; \
      git --version; \
      goreleaser --version | grep "^GitVersion"; \
      golangci-lint version; \
      cosign version | grep "^GitVersion"; \
      syft version | grep "^Version"; \
      shellcheck --version | grep "^version"; \
      go version -m /usr/local/bin/go-licenses | grep "^[[:space:]]*mod[[:space:]]"; \
      gitlint --version; \
      python --version; \
      ssh -V 2>&1; \
    '

# catatonit runs as PID 1 and reaps orphaned processes. The command itself
# would be PID 1 otherwise, and neither go nor the other tools wait for
# processes they did not start: git leaves detached background processes
# (e.g. automatic maintenance), whose zombies would pile up until the
# container's process limit is reached. This does not depend on the host
# engine providing an init (podman --init needs catatonit on the host).
ENTRYPOINT ["/usr/bin/catatonit", "--"]
