#!/usr/bin/env bash
# SPDX-License-Identifier: GPL-3.0-only
# Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>
#
# build-in-container.sh - run LazySubmodules build targets in containers
#
# Most targets run inside the build image built from the Containerfile, so
# developer machines and CI use the same pinned toolchain. test-compat and
# package-test use other pinned images.
#
# Usage: scripts/build-in-container.sh [-h] TARGET...
#
# Targets run in the given order and the script stops at the first failure,
# e.g. "scripts/build-in-container.sh lint test build snapshot". Run with -h for
# the list of targets and environment variables.

set -euo pipefail

readonly SCRIPT_NAME="${0##*/}"
REPO_ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
readonly REPO_ROOT
readonly MODULE_PATH="github.com/FPGArtktic/lazysubmodules"
readonly IMAGE_NAME="localhost/lazysubmodules-build"
readonly TARGETS=(
	build test lint gitlint licenses snapshot release
	test-compat package-test
	image image-tag image-save image-load
)

# Go toolchain image with git 2.39.5 (Debian bookworm), the oldest git
# LazySubmodules supports. Used by test-compat.
readonly COMPAT_IMAGE="docker.io/library/golang:1.27.1-bookworm\
@sha256:648f440f42a0958804efb24df176f806f9d353b41f1c0627f666428e40310f6b"

# Distribution images that package-test installs the snapshot packages in.
readonly DEB_TEST_IMAGE="docker.io/library/debian:trixie-slim\
@sha256:d7e12182ce18b85b93007c1dedf31f2d29e01ccf3182cc4017c709b6259bc132"
readonly RPM_TEST_IMAGE="registry.fedoraproject.org/fedora:44\
@sha256:61beafd34111e1cb85fb49377ceadeee0a53622dbc20670ed8ca303f0e17ed9c"

# Mount point of the repository. The Containerfile marks it as a git
# safe.directory.
readonly SRC_DIR="/src"

# HOME inside the container. Rootless podman with --userns=keep-id sets HOME to
# the working directory otherwise, which would put tool state into the
# repository.
readonly CONTAINER_HOME="/tmp"

# Named cache volumes and their mount points. The mount points must match the
# GOCACHE, GOMODCACHE and GOLANGCI_LINT_CACHE values in the Containerfile, where
# they are created with mode 1777 so that fresh volumes are writable for the
# unprivileged container user.
readonly VOLUMES=(
	"lazysubmodules-go-build:/cache/go-build"
	"lazysubmodules-go-mod:/cache/go-mod"
	"lazysubmodules-golangci-lint:/cache/golangci-lint"
)

# Licenses accepted by "licenses" (google/licenseclassifier names). All are
# permissive and compatible with GPL-3.0-only. Weak copyleft (MPL-2.0) is left
# out on purpose: a dependency under it needs a manual review first.
readonly ALLOWED_LICENSES="Apache-2.0,BSD-2-Clause,BSD-3-Clause,ISC,MIT"

# Environment variables handed to "release". GoReleaser reads GITHUB_TOKEN,
# cosign keyless signing reads ACTIONS_ID_TOKEN_REQUEST_*, the AUR publisher
# reads AUR_KEY. Variables unset on the host stay unset in the container.
readonly RELEASE_ENV=(
	GITHUB_TOKEN
	ACTIONS_ID_TOKEN_REQUEST_URL
	ACTIONS_ID_TOKEN_REQUEST_TOKEN
	AUR_KEY
)

# Checks run by package-test after the package is installed: both command
# names work and print the same version, and git was installed as a
# dependency. The commands run in the container's shell, which expands them.
# shellcheck disable=SC2016
readonly PACKAGE_CHECK='
	command -v lazysubmodules
	test -L /usr/bin/lsm
	ls -l /usr/bin/lsm
	lazysubmodules version
	lsm version
	test "$(lsm version)" = "$(lazysubmodules version)"
	git --version
'

# Set once by select_engine and select_image. ENGINE_KIND is podman, docker
# (rootful) or docker-rootless.
ENGINE=""
ENGINE_KIND=""
ENGINE_USER_ARGS=()
IMAGE=""

# Temporary directory of this run, removed on exit.
WORK_DIR=""

usage()
{
	cat <<-EOF
		Usage: ${SCRIPT_NAME} [-h] TARGET...

		Run build targets in containers. Targets run in the given order; the
		first failure stops the run.

		Targets in the build image:
		  build         go build for the host architecture (binaries in bin/)
		  test          go test -race ./...
		  lint          golangci-lint, shellcheck scripts/*.sh,
		                scripts/check-headers.sh
		  gitlint       lint the commit messages selected by GITLINT_RANGE
		  licenses      go-licenses check ./... (allowed: ${ALLOWED_LICENSES})
		  snapshot      goreleaser release --snapshot --clean --skip=sign (dist/)
		  release       goreleaser release --clean (needs network and credentials)

		Targets in other pinned images:
		  test-compat   go test -race ./... with git 2.39, the oldest supported
		                git (golang:1.27.1-bookworm)
		  package-test  install the snapshot .deb (Debian trixie) and .rpm
		                (Fedora 44) from dist/ and run "lazysubmodules version"
		                and "lsm version"; needs network for the git dependency

		Build image management:
		  image         build the build image from the Containerfile
		  image-tag     print the build image reference
		  image-save    write the build image to the archive IMAGE_ARCHIVE
		  image-load    load the build image from the archive IMAGE_ARCHIVE

		The build image is x86_64 only. Every target that runs in it builds it
		first when it is missing. The image tag is derived from the Containerfile
		hash, so an edited Containerfile is rebuilt automatically.

		Only release, package-test and image use the network; all other targets
		run with --network=none, and Go dependencies come from vendor/.

		Environment:
		  CONTAINER_ENGINE  podman or docker (default: podman, else docker)
		  GITLINT_RANGE     commits for gitlint, passed to "gitlint --commits";
		                    "A..B" lints the commits after A up to B, a single ref
		                    lints its whole history, "A,B" lints exactly A and B
		                    (default: HEAD, i.e. the whole history)
		  IMAGE_ARCHIVE     image archive path for image-save and image-load
		  GITHUB_TOKEN, ACTIONS_ID_TOKEN_REQUEST_URL, ACTIONS_ID_TOKEN_REQUEST_TOKEN,
		  AUR_KEY, GITHUB_STEP_SUMMARY
		                    passed to release when set

		Caches live in the named volumes lazysubmodules-go-build,
		lazysubmodules-go-mod and lazysubmodules-golangci-lint; remove them with
		"<engine> volume rm" to start from scratch.
	EOF
}

log()
{
	printf '%s: %s\n' "$SCRIPT_NAME" "$*" >&2
}

die()
{
	log "error: $*"
	exit 1
}

cleanup()
{
	if [[ -n "$WORK_DIR" ]]; then
		rm -rf "$WORK_DIR"
	fi
}

die_usage()
{
	log "error: $*"
	printf "Try '%s -h' for more information.\n" "$SCRIPT_NAME" >&2
	exit 2
}

is_target()
{
	local target

	for target in "${TARGETS[@]}"; do
		if [[ "$1" == "$target" ]]; then
			return 0
		fi
	done
	return 1
}

# Pick the container engine: CONTAINER_ENGINE, else podman, else docker. Sets
# ENGINE, ENGINE_KIND and ENGINE_USER_ARGS, the options that run the container
# as the invoking user so that files written to the repository belong to that
# user.
select_engine()
{
	local version

	if [[ -n "${CONTAINER_ENGINE:-}" ]]; then
		ENGINE="$CONTAINER_ENGINE"
	elif command -v podman >/dev/null 2>&1; then
		ENGINE="podman"
	elif command -v docker >/dev/null 2>&1; then
		ENGINE="docker"
	else
		die "neither podman nor docker found; set CONTAINER_ENGINE"
	fi
	command -v "$ENGINE" >/dev/null 2>&1 || die "container engine not found: $ENGINE"

	# "docker" may be the podman compatibility wrapper.
	version="$("$ENGINE" --version)" || die "cannot run $ENGINE --version"
	if [[ "${version,,}" == *podman* ]]; then
		ENGINE_KIND="podman"
		if [[ "$("$ENGINE" info --format '{{.Host.Security.Rootless}}')" == "true" ]]; then
			# The invoking user keeps its UID and GID inside the container.
			ENGINE_USER_ARGS=(--userns=keep-id)
		else
			ENGINE_USER_ARGS=(--user "$(id -u):$(id -g)")
		fi
	elif [[ "$("$ENGINE" info --format '{{.SecurityOptions}}')" == *rootless* ]]; then
		# Rootless docker maps container root to the invoking user.
		ENGINE_KIND="docker-rootless"
		ENGINE_USER_ARGS=()
	else
		ENGINE_KIND="docker"
		ENGINE_USER_ARGS=(--user "$(id -u):$(id -g)")
	fi
	readonly ENGINE ENGINE_KIND ENGINE_USER_ARGS
}

# Derive the image reference from the Containerfile content. Sets IMAGE.
select_image()
{
	local sum

	sum="$(sha256sum "${REPO_ROOT}/Containerfile")" || die "cannot hash the Containerfile"
	IMAGE="${IMAGE_NAME}:${sum:0:12}"
	readonly IMAGE
}

# The Arch Linux base image of the build image is published for x86_64 only.
check_build_arch()
{
	local machine

	machine="$(uname -m)"
	if [[ "$machine" != "x86_64" ]]; then
		die "the build image is available for x86_64 only, not ${machine}"
	fi
}

build_image()
{
	local context
	local rc=0

	log "building image ${IMAGE}"
	# The Containerfile copies nothing, so an empty build context avoids
	# sending the repository to the engine.
	context="$(mktemp -d)" || return 1
	"$ENGINE" build --tag "$IMAGE" --file "${REPO_ROOT}/Containerfile" "$context" || rc=$?
	rmdir "$context"
	return "$rc"
}

ensure_image()
{
	if ! "$ENGINE" image inspect "$IMAGE" >/dev/null 2>&1; then
		build_image
	fi
}

# image_archive
#
# Print IMAGE_ARCHIVE, or fail when it is not set.
image_archive()
{
	if [[ -z "${IMAGE_ARCHIVE:-}" ]]; then
		log "error: IMAGE_ARCHIVE is not set"
		return 1
	fi
	printf '%s\n' "$IMAGE_ARCHIVE"
}

# passwd_file IMAGE
#
# Print the path of a copy of the passwd file of IMAGE with an entry for the
# invoking user whose home directory is CONTAINER_HOME. The file is created
# once per image and run.
passwd_file()
{
	local file uid gid name

	file="${WORK_DIR}/passwd-$(printf '%s' "$1" | sha256sum | cut -c 1-12)"
	if [[ ! -f "$file" ]]; then
		uid="$(id -u)"
		gid="$(id -g)"
		name="$(id -un 2>/dev/null)" || name="builder"
		"$ENGINE" run --rm --pull=missing --network=none --entrypoint cat "$1" /etc/passwd |
			awk -F: -v uid="$uid" -v name="$name" '$3 != uid && $1 != name' \
				>"${file}.tmp" || return 1
		printf '%s:x:%s:%s::%s:/bin/sh\n' "$name" "$uid" "$gid" "$CONTAINER_HOME" \
			>>"${file}.tmp"
		mv -f "${file}.tmp" "$file"
	fi
	printf '%s\n' "$file"
}

# run_container IMAGE NETWORK CGO [ENGINE-OPTION...] -- COMMAND [ARG...]
#
# Run COMMAND in IMAGE as the invoking user, with the repository mounted as the
# working directory. NETWORK is "none" for offline targets or "default" for the
# engine's default network. CGO is the CGO_ENABLED value.
run_container()
{
	local image="$1"
	local network="$2"
	local cgo="$3"
	local passwd
	local -a args=(run --rm)

	shift 3
	if [[ "$network" != "default" ]]; then
		args+=("--network=${network}")
	fi
	if [[ -t 0 && -t 1 ]]; then
		args+=(--interactive --tty)
	fi
	args+=("${ENGINE_USER_ARGS[@]}")
	# ssh and ssh-keygen (tests, AUR publishing) need a passwd entry for the
	# user and take the home directory from it, not from HOME. Podman would
	# use the working directory, the repository, and rootful docker has no
	# entry for the invoking user at all.
	case "$ENGINE_KIND" in
	podman)
		# Podman substitutes the variables itself.
		args+=(--passwd-entry "\$USERNAME:*:\$UID:\$GID:\$NAME:${CONTAINER_HOME}:/bin/sh")
		;;
	docker)
		passwd="$(passwd_file "$image")" || return 1
		args+=(--volume "${passwd}:/etc/passwd:ro,z")
		;;
	esac
	args+=(--volume "${REPO_ROOT}:${SRC_DIR}:Z" --workdir "$SRC_DIR")
	args+=(
		--env "HOME=${CONTAINER_HOME}"
		--env "GOFLAGS=-mod=vendor"
		--env "GOTOOLCHAIN=local"
		--env "CGO_ENABLED=${cgo}"
	)
	while (($# > 0)) && [[ "$1" != "--" ]]; do
		args+=("$1")
		shift
	done
	(($# > 0)) || die "run_container: missing -- before the command"
	shift
	"$ENGINE" "${args[@]}" "$image" "$@"
}

# run_in_image NETWORK CGO [ENGINE-OPTION...] -- COMMAND [ARG...]
#
# Run COMMAND in the build image, see run_container, with the cache volumes.
run_in_image()
{
	local volume
	local -a opts=(--pull=never)

	# :Z relabels the repository for SELinux (no-op elsewhere). The caches are
	# shared by every run, so they get the shared label (:z) instead: a private
	# label would be applied again, recursively, on each run.
	for volume in "${VOLUMES[@]}"; do
		opts+=(--volume "${volume}:z")
	done
	run_container "$IMAGE" "$1" "$2" "${opts[@]}" "${@:3}"
}

target_build()
{
	# -o with a trailing slash compiles every package and writes the commands
	# to bin/.
	run_in_image none 0 -- go build -trimpath -o bin/ ./...
}

target_test()
{
	# The race detector requires cgo.
	run_in_image none 1 -- go test -race ./...
}

target_lint()
{
	local -a scripts=(scripts/*.sh)
	local rc=0

	# Run every linter, then report the combined result.
	run_in_image none 0 -- golangci-lint run ./... || rc=1
	run_in_image none 0 -- shellcheck "${scripts[@]}" || rc=1
	run_in_image none 0 -- scripts/check-headers.sh || rc=1
	return "$rc"
}

target_gitlint()
{
	local range="${GITLINT_RANGE:-HEAD}"

	log "gitlint range: ${range}"
	run_in_image none 0 -- gitlint --ignore-stdin --commits "$range"
}

target_licenses()
{
	# The project itself (GPL-3.0-only) is not a dependency.
	run_in_image none 0 -- go-licenses check ./... \
		--ignore "$MODULE_PATH" --allowed_licenses="$ALLOWED_LICENSES"
}

target_snapshot()
{
	# Keyless cosign signing needs a CI identity token, so snapshots are
	# unsigned. Snapshots never publish, so they run offline.
	run_in_image none 0 -- goreleaser release --snapshot --clean --skip=sign
}

target_release()
{
	local name
	local -a opts=()

	for name in "${RELEASE_ENV[@]}"; do
		opts+=(--env "$name")
	done
	# GoReleaser appends a job summary when running in GitHub Actions.
	if [[ -n "${GITHUB_STEP_SUMMARY:-}" && -f "$GITHUB_STEP_SUMMARY" ]]; then
		opts+=(
			--env GITHUB_STEP_SUMMARY
			--volume "${GITHUB_STEP_SUMMARY}:${GITHUB_STEP_SUMMARY}:z"
		)
	fi
	run_in_image default 0 "${opts[@]}" -- goreleaser release --clean
}

target_test_compat()
{
	# The image has no cache volumes: fresh volumes would not be writable for
	# the container user, as the image lacks the mode 1777 directories. The
	# build cache stays in the container and is discarded with it.
	run_container "$COMPAT_IMAGE" none 1 --pull=missing -- \
		sh -euc 'git --version; go version; exec go test -race ./...'
}

# find_package EXTENSION ARCH
#
# Print the name of the single dist/*_linux_ARCH.EXTENSION package.
find_package()
{
	local -a found=()
	local file

	for file in "${REPO_ROOT}"/dist/*_linux_"$2"."$1"; do
		if [[ -f "$file" ]]; then
			found+=("${file##*/}")
		fi
	done
	case "${#found[@]}" in
	1)
		printf '%s\n' "${found[0]}"
		;;
	0)
		log "error: no dist/*_linux_${2}.${1} package; run the snapshot target first"
		return 1
		;;
	*)
		log "error: several dist/*_linux_${2}.${1} packages: ${found[*]}"
		return 1
		;;
	esac
}

# install_package IMAGE PACKAGE INSTALL-COMMAND
#
# Install dist/PACKAGE in a fresh container of IMAGE with INSTALL-COMMAND,
# which receives the package path as "$1", then run PACKAGE_CHECK. The
# container runs as its own root user, and dist/ is mounted read-only.
install_package()
{
	log "installing ${2} in ${1%%@*}"
	"$ENGINE" run --rm --pull=missing \
		--volume "${REPO_ROOT}/dist:/dist:ro,Z" \
		"$1" sh -euc "${3}${PACKAGE_CHECK}" sh "/dist/${2}"
}

target_package_test()
{
	local arch deb rpm

	case "$(uname -m)" in
	x86_64)
		arch="amd64"
		;;
	aarch64)
		arch="arm64"
		;;
	*)
		log "error: no packages are built for $(uname -m)"
		return 1
		;;
	esac
	deb="$(find_package deb "$arch")" || return 1
	rpm="$(find_package rpm "$arch")" || return 1

	# shellcheck disable=SC2016 # expanded by the container's shell
	install_package "$DEB_TEST_IMAGE" "$deb" '
		export DEBIAN_FRONTEND=noninteractive
		apt-get update -qq
		apt-get install -y -qq --no-install-recommends "$1" >/dev/null
		dpkg-query -W -f "\${Package} \${Version} depends: \${Depends}\n" lazysubmodules
	' || return 1
	# shellcheck disable=SC2016 # expanded by the container's shell
	install_package "$RPM_TEST_IMAGE" "$rpm" '
		dnf install -y -q --setopt=install_weak_deps=False "$1" >/dev/null
		rpm -q --queryformat "%{NAME} %{VERSION}-%{RELEASE}\n" lazysubmodules
		rpm -q --requires lazysubmodules
	'
}

target_image()
{
	build_image
}

target_image_tag()
{
	printf '%s\n' "$IMAGE"
}

target_image_save()
{
	local archive
	local rc=0

	archive="$(image_archive)" || return 1
	log "saving ${IMAGE} to ${archive}"
	# Write to a temporary name first, so that an interrupted save never
	# leaves a truncated archive behind.
	rm -f "${archive}.partial"
	"$ENGINE" save --output "${archive}.partial" "$IMAGE" || rc=$?
	if ((rc != 0)); then
		rm -f "${archive}.partial"
		return "$rc"
	fi
	mv -f "${archive}.partial" "$archive"
}

target_image_load()
{
	local archive

	archive="$(image_archive)" || return 1
	if [[ ! -f "$archive" ]]; then
		log "error: image archive not found: ${archive}"
		return 1
	fi
	log "loading ${archive}"
	"$ENGINE" load --input "$archive" || return 1
	if ! "$ENGINE" image inspect "$IMAGE" >/dev/null 2>&1; then
		log "error: ${archive} does not contain ${IMAGE}"
		return 1
	fi
}

main()
{
	local target
	local -a targets=()

	while (($# > 0)); do
		case "$1" in
		-h | --help)
			usage
			return 0
			;;
		-*)
			die_usage "unknown option: $1"
			;;
		*)
			is_target "$1" || die_usage "unknown target: $1"
			targets+=("$1")
			;;
		esac
		shift
	done
	((${#targets[@]} > 0)) || die_usage "no target given"
	[[ "$REPO_ROOT" != *:* ]] || die "repository path must not contain ':': $REPO_ROOT"

	cd "$REPO_ROOT"
	WORK_DIR="$(mktemp -d)" || die "cannot create a temporary directory"
	trap cleanup EXIT
	select_engine
	select_image
	for target in "${targets[@]}"; do
		log "target ${target} (${ENGINE}, ${IMAGE})"
		case "$target" in
		image-tag | test-compat | package-test) ;;
		image | image-load)
			check_build_arch
			;;
		*)
			check_build_arch
			ensure_image || die "cannot build image ${IMAGE}"
			;;
		esac
		"target_${target//-/_}" || die "target ${target} failed"
	done
}

main "$@"
