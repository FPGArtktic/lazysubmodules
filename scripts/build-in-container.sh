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

# Checks of the license notices, run by package-test after PACKAGE_CHECK. The
# install command sets "notices" to the installed notices file, and "texts"
# to "files" when the license texts are files next to it or to "inline" when
# they follow its stanzas after a line "==> FILE <==". The binary is stripped,
# but its build information keeps plain text lines "dep<TAB>module<TAB>version",
# each optionally followed by a replacement "=><TAB>module<TAB>version". Every
# module listed there, and the Go standard library ("std"), needs a stanza
# with the same version, and every license text a stanza names must be
# there. Debian's sh has no pipefail, hence the checks of the pipeline
# results.
# shellcheck disable=SC2016
readonly NOTICES_CHECK='
	fail() { echo "error: $*" >&2; exit 1; }
	tab="$(printf "\t")"
	test -s "$notices" || fail "${notices} is missing or empty"
	grep -a -E "^(dep|=>)${tab}" "$(command -v lazysubmodules)" | cut -f 1-3 >/tmp/deps
	test -s /tmp/deps || fail "no modules in the build information of the binary"
	cat >/tmp/deps.awk <<-"EOF"
		$1 == "dep" { if (p != "") print p; m = $2; p = $2 " " $3 }
		$1 == "=>" { p = m " " ($2 == m ? $3 : $2 " " $3) }
		END { if (p != "") print p }
	EOF
	cat >/tmp/stanzas.awk <<-"EOF"
		/^==> / { exit }
		/^Module: / { m = substr($0, 9) }
		/^Version: / { print m " " substr($0, 10) }
	EOF
	awk -F "$tab" -f /tmp/deps.awk /tmp/deps | LC_ALL=C sort -u >/tmp/want
	awk -f /tmp/stanzas.awk "$notices" | LC_ALL=C sort -u >/tmp/have
	test -s /tmp/want || fail "no modules in the build information of the binary"
	if LC_ALL=C comm -23 /tmp/want /tmp/have | grep .; then
		fail "modules of the binary without a notice in ${notices}"
	fi
	grep -q "^std go[0-9]" /tmp/have || fail "no notice for the Go standard library (std)"
	sed -n "/^==> /q; s/^Text: //p" "$notices" | LC_ALL=C sort -u >/tmp/texts
	test -s /tmp/texts || fail "no license texts named in ${notices}"
	while IFS= read -r text; do
		case "$texts" in
		files) test -s "${notices%/*}/${text}" ;;
		inline) grep -q -x -F "==> ${text} <==" "$notices" ;;
		*) false ;;
		esac || fail "license text ${text} missing"
	done </tmp/texts
	echo "notices: $(wc -l </tmp/want) modules and std," \
		"$(wc -l </tmp/texts) license texts in ${notices}"
'

# Set once by select_engine and select_image. ENGINE_KIND is podman, docker
# (rootful) or docker-rootless.
ENGINE=""
ENGINE_KIND=""
ENGINE_USER_ARGS=()
IMAGE=""

# Set once by select_git_dir. GIT_DIR_ARGS holds volume options, empty unless
# .git is a file that points to the git directory elsewhere. GIT_DIR_PROBLEM
# says why git cannot work inside the container, if it cannot.
GIT_DIR_ARGS=()
GIT_DIR_PROBLEM=""

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
		  licenses      go-licenses check ./..., allowing only the licenses
		                ${ALLOWED_LICENSES}
		  snapshot      goreleaser release --snapshot --clean --skip=sign (dist/)
		  release       goreleaser release --clean (needs network and credentials)

		Targets in other pinned images:
		  test-compat   go test -race ./... with git 2.39, the oldest supported
		                git (golang:1.27.1-bookworm)
		  package-test  install the snapshot .deb (Debian trixie) and .rpm
		                (Fedora 44) for the host architecture from dist/, run
		                "lazysubmodules version" and "lsm version", and check
		                the installed license notices; needs network for the
		                git dependency

		Build image management:
		  image         build the build image from the Containerfile, unless it
		                exists
		  image-tag     print the build image reference
		  image-save    write the build image to the archive IMAGE_ARCHIVE
		  image-load    load the build image from the archive IMAGE_ARCHIVE

		The build image is x86_64 only. Every target that runs in it builds it
		first when it is missing. The image tag is derived from the Containerfile
		hash, so an edited Containerfile is rebuilt automatically; to rebuild an
		unchanged one, remove the image first ("<engine> rmi" with the reference
		that image-tag prints).

		The containers of all targets except release and package-test run with
		--network=none, and Go dependencies come from vendor/. Images need the
		network when they are missing: building the build image (image, or any
		target that runs in it) downloads the toolchain, and test-compat and
		package-test pull their images. To work offline, load the build image
		with image-load and pull the test-compat image beforehand.

		The build image needs podman or docker with BuildKit (the default
		builder of docker; the script sets DOCKER_BUILDKIT=1).

		Environment:
		  CONTAINER_ENGINE  podman or docker (default: podman, else docker)
		  GITLINT_RANGE     commits for gitlint, passed to "gitlint --commits";
		                    "A..B" lints the commits after A up to B, a single ref
		                    lints its whole history, "A,B" lints exactly A and B
		                    (default: HEAD, i.e. the whole history)
		  IMAGE_ARCHIVE     image archive path, required by image-save and
		                    image-load; a relative path is relative to the
		                    current directory
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

# annotate_failure NAME FILE
#
# Print a GitHub Actions error annotation for the failed step NAME, with the
# last lines of its output in FILE. Annotations of a public repository can be
# read without signing in, unlike its job logs.
annotate_failure()
{
	local name=$1 file=$2
	local message

	message="$(tail -n 25 -- "$file" | cut -c 1-200)"
	message="${message//'%'/'%25'}"
	message="${message//$'\r'/'%0D'}"
	message="${message//$'\n'/'%0A'}"
	printf '::error title=%s: %s failed::%s\n' "$SCRIPT_NAME" "$name" "$message"
}

# run_step NAME COMMAND...
#
# Run COMMAND and return its exit status. In GitHub Actions the output is also
# kept, so that a failure can be reported with annotate_failure.
run_step()
{
	local name=$1
	local output rc
	local -a status

	shift
	if [[ "${GITHUB_ACTIONS:-}" != true ]]; then
		"$@"
		return
	fi
	output="${WORK_DIR}/step.log"
	set +e
	"$@" 2>&1 | tee "$output"
	status=("${PIPESTATUS[@]}")
	set -e
	rc="${status[0]}"
	if ((rc != 0)); then
		annotate_failure "$name" "$output"
	fi
	return "$rc"
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

# container_path PATH
#
# Print where PATH leads to inside the container. git resolves a relative
# PATH from a .git file against the directory of that file, the repository
# mount. The result is normalized without looking at the host file system.
container_path()
{
	local path="$1"

	if [[ "$path" != /* ]]; then
		path="${SRC_DIR}/${path}"
	fi
	realpath --canonicalize-missing --no-symlinks -- "$path"
}

# host_git_path OPTION
#
# Print the physical path of the directory that "git rev-parse OPTION" names
# for the repository.
host_git_path()
{
	local path

	path="$(git -C "$REPO_ROOT" rev-parse "$1" 2>/dev/null)" || return 1
	(cd -- "$REPO_ROOT" && cd -- "$path" 2>/dev/null && pwd -P)
}

# git_dir_volume HOST-DIR CONTAINER-DIR
#
# Make HOST-DIR visible at CONTAINER-DIR: add a volume to GIT_DIR_ARGS, or
# nothing when the repository mount shows HOST-DIR there already. Fail with the
# reason in GIT_DIR_PROBLEM when neither works: a volume inside the repository
# mount would hide part of the checkout and create its mount point there.
git_dir_volume()
{
	local visible

	if [[ "$2" == "$SRC_DIR" || "$2" == "$SRC_DIR"/* ]]; then
		visible="$(cd -- "${REPO_ROOT}${2#"$SRC_DIR"}" 2>/dev/null && pwd -P)" ||
			visible=""
		if [[ "$visible" == "$1" ]]; then
			return 0
		fi
		GIT_DIR_PROBLEM="the git directory ${1} would have to appear at ${2},"
		GIT_DIR_PROBLEM+=" inside the repository mount ${SRC_DIR} of the"
		GIT_DIR_PROBLEM+=" container; use a clone, or move the worktree"
		return 1
	fi
	if [[ "$1" == *:* || "$2" == *:* ]]; then
		GIT_DIR_PROBLEM="git directory paths must not contain ':': ${1} (${2})"
		return 1
	fi
	# Shared SELinux label, as for the caches: the directory belongs to more
	# than this checkout.
	GIT_DIR_ARGS+=(--volume "${1}:${2}:z")
}

# git_dir_volumes
#
# Add the volumes for a repository whose .git is a file to GIT_DIR_ARGS, or
# fail with the reason in GIT_DIR_PROBLEM.
git_dir_volumes()
{
	local pointer common host_dir host_common ctr_dir ctr_common config

	pointer="$(sed -n '1{s/\r$//;s/^gitdir: //p;}' "${REPO_ROOT}/.git")"
	host_dir="$(host_git_path --git-dir)" || host_dir=""
	host_common="$(host_git_path --git-common-dir)" || host_common=""
	if [[ -z "$pointer" || -z "$host_dir" || -z "$host_common" ]]; then
		GIT_DIR_PROBLEM="cannot find the git directory that ${REPO_ROOT}/.git"
		GIT_DIR_PROBLEM+=" points to"
		return 1
	fi
	for config in "${host_dir}/config" "${host_dir}/config.worktree"; do
		if [[ -f "$config" ]] &&
			git config --file "$config" --get core.worktree >/dev/null; then
			GIT_DIR_PROBLEM="the git directory of ${REPO_ROOT} sets"
			GIT_DIR_PROBLEM+=" core.worktree (a submodule checkout?), which git"
			GIT_DIR_PROBLEM+=" cannot follow inside the container; use a clone"
			GIT_DIR_PROBLEM+=" or a linked worktree"
			return 1
		fi
	done
	ctr_dir="$(container_path "$pointer")" || return 1
	ctr_common="$ctr_dir"
	if [[ "$host_common" != "$host_dir" ]]; then
		# commondir is absolute, or relative to the git directory.
		if ! common="$(<"${host_dir}/commondir")"; then
			GIT_DIR_PROBLEM="cannot read ${host_dir}/commondir"
			return 1
		fi
		if [[ "$common" != /* ]]; then
			common="${ctr_dir}/${common}"
		fi
		ctr_common="$(container_path "${common%$'\r'}")" || return 1
	fi
	git_dir_volume "$host_common" "$ctr_common" || return 1
	# A linked worktree's git directory is normally inside the common
	# directory, and then visible through its volume already.
	if [[ "$host_dir" != "$host_common" &&
		"${ctr_common}${host_dir#"$host_common"}" != "$ctr_dir" ]]; then
		git_dir_volume "$host_dir" "$ctr_dir" || return 1
	fi
}

# In a linked worktree, .git is a file that points to the worktree's git
# directory, which points on to the common git directory of the repository.
# Both are outside the repository mount. Mount them where the pointers lead to
# inside the container, so that git works there as it does on the host.
#
# A submodule checkout (or a separate git directory) also has a .git file, but
# its git directory names the work tree in core.worktree, relative to the host
# layout, which the container does not reproduce. Git cannot work there. Such
# problems are recorded in GIT_DIR_PROBLEM, so that only the targets that need
# git fail.
#
# Sets GIT_DIR_ARGS and GIT_DIR_PROBLEM.
select_git_dir()
{
	if [[ -f "${REPO_ROOT}/.git" ]] && ! git_dir_volumes; then
		GIT_DIR_ARGS=()
		if [[ -z "$GIT_DIR_PROBLEM" ]]; then
			GIT_DIR_PROBLEM="cannot map the git directory of ${REPO_ROOT}"
		fi
	fi
	readonly GIT_DIR_ARGS GIT_DIR_PROBLEM
}

# resolve_image_archive
#
# Check that IMAGE_ARCHIVE is set, and make it absolute: main changes to the
# repository root before the targets run, while a relative path is meant
# relative to the caller's directory.
resolve_image_archive()
{
	[[ -n "${IMAGE_ARCHIVE:-}" ]] || die_usage "image-save and image-load need IMAGE_ARCHIVE"
	if [[ "$IMAGE_ARCHIVE" != /* ]]; then
		IMAGE_ARCHIVE="${PWD}/${IMAGE_ARCHIVE}"
	fi
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
	# The Containerfile uses RUN --mount, which only BuildKit supports; docker
	# would use its legacy builder if DOCKER_BUILDKIT=0 were set. Podman
	# ignores the variable.
	DOCKER_BUILDKIT=1 "$ENGINE" build --tag "$IMAGE" \
		--file "${REPO_ROOT}/Containerfile" "$context" || rc=$?
	rmdir "$context"
	return "$rc"
}

image_exists()
{
	"$ENGINE" image inspect "$IMAGE" >/dev/null 2>&1
}

# Build the image unless it exists. The tag is derived from the Containerfile
# content, so an existing image was built from this Containerfile. Building it
# again would start from scratch when the image came from image-load, which
# brings no build cache.
ensure_image()
{
	if ! image_exists; then
		build_image
	fi
}

# archive_has_image ARCHIVE
#
# Succeed when ARCHIVE, written by "<engine> save", holds IMAGE. Podman and
# docker both list the image references in manifest.json. Checking the loaded
# image instead would prove nothing when the image existed before.
archive_has_image()
{
	# grep reads all of its input, so tar never gets SIGPIPE.
	tar -xOf "$1" manifest.json | grep -F -- "\"${IMAGE}\"" >/dev/null
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
	args+=(--volume "${REPO_ROOT}:${SRC_DIR}:Z" "${GIT_DIR_ARGS[@]}" --workdir "$SRC_DIR")
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
# which receives the package path as "$1" and sets the variables of
# NOTICES_CHECK, then run PACKAGE_CHECK and NOTICES_CHECK. The container runs
# as its own root user, and dist/ is mounted read-only.
install_package()
{
	log "installing ${2} in ${1%%@*}"
	"$ENGINE" run --rm --pull=missing \
		--volume "${REPO_ROOT}/dist:/dist:ro,Z" \
		"$1" sh -euc "${3}${PACKAGE_CHECK}${NOTICES_CHECK}" sh "/dist/${2}"
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
		# The only file of /usr/share/doc that the image does not exclude.
		notices=/usr/share/doc/lazysubmodules/copyright
		texts=inline
		dpkg-query -L lazysubmodules | grep -q -x -F "$notices"
	' || return 1
	# shellcheck disable=SC2016 # expanded by the container's shell
	install_package "$RPM_TEST_IMAGE" "$rpm" '
		dnf install -y -q --setopt=install_weak_deps=False "$1" >/tmp/dnf.log 2>&1 ||
			{ cat /tmp/dnf.log >&2; exit 1; }
		rpm -q --queryformat "%{NAME} %{VERSION}-%{RELEASE}\n" lazysubmodules
		rpm -q --requires lazysubmodules
		notices=/usr/share/licenses/lazysubmodules/THIRD_PARTY_NOTICES
		texts=files
		# %license files, installed although the image excludes documentation
		rpm -q --licensefiles lazysubmodules | grep -q -x -F "$notices"
		# The License tag names the project license and those of the notices.
		license="GPL-3.0-only$(sed -n "s/^License: / AND /p" "$notices" |
			LC_ALL=C sort -u | tr -d "\n")"
		rpm -q --queryformat "License: %{LICENSE}\n" lazysubmodules
		test "$(rpm -q --queryformat "%{LICENSE}" lazysubmodules)" = "$license" || {
			echo "error: the License tag does not match the notices: ${license}" >&2
			exit 1
		}
	'
}

target_image()
{
	if image_exists; then
		log "image ${IMAGE} exists; to rebuild it, remove it first:" \
			"${ENGINE} rmi ${IMAGE}"
		return 0
	fi
	build_image
}

target_image_tag()
{
	printf '%s\n' "$IMAGE"
}

target_image_save()
{
	local archive="$IMAGE_ARCHIVE"
	local rc=0

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
	local archive="$IMAGE_ARCHIVE"

	if [[ ! -f "$archive" ]]; then
		log "error: image archive not found: ${archive}"
		return 1
	fi
	if ! archive_has_image "$archive"; then
		log "error: ${archive} does not contain ${IMAGE}"
		return 1
	fi
	log "loading ${archive}"
	"$ENGINE" load --input "$archive" || return 1
	if ! image_exists; then
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
	# Fail before any target runs; image-save may build the image first.
	for target in "${targets[@]}"; do
		case "$target" in
		image-save | image-load)
			resolve_image_archive
			;;
		esac
	done
	[[ "$REPO_ROOT" != *:* ]] || die "repository path must not contain ':': $REPO_ROOT"

	cd "$REPO_ROOT"
	WORK_DIR="$(mktemp -d)" || die "cannot create a temporary directory"
	trap cleanup EXIT
	select_engine
	select_image
	select_git_dir
	# Targets that need the repository metadata: check-headers.sh (lint),
	# gitlint and GoReleaser. Without it a snapshot would silently lack the
	# version and commit.
	for target in "${targets[@]}"; do
		case "$target" in
		lint | gitlint | snapshot | release)
			if [[ -n "$GIT_DIR_PROBLEM" ]]; then
				die "target ${target} needs git: ${GIT_DIR_PROBLEM}"
			fi
			;;
		esac
	done
	for target in "${targets[@]}"; do
		log "target ${target} (${ENGINE}, ${IMAGE})"
		case "$target" in
		image-tag | test-compat | package-test) ;;
		image | image-load)
			check_build_arch
			;;
		*)
			check_build_arch
			run_step "image build" ensure_image ||
				die "cannot build image ${IMAGE}"
			;;
		esac
		run_step "target ${target}" "target_${target//-/_}" ||
			die "target ${target} failed"
	done
}

main "$@"
