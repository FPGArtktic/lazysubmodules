#!/usr/bin/env bash
# SPDX-License-Identifier: GPL-3.0-only
# Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>
#
# record-demos.sh - record the demo GIFs in docs/demo with VHS
#
# Each GIF in docs/demo is recorded from the VHS tape of the same name.
# The script builds the recording image from docs/demo/Containerfile
# unless it exists: the official VHS image with git, column and gifsicle
# added, all pinned. It then runs VHS for each tape in a container without
# network access, with the repository mounted read-only, docs/demo
# writable and the lazysubmodules binary as /usr/local/bin/lazysubmodules.
# Every tape builds the offline demo superproject of scripts/demo.sh
# before it starts recording. The script optimizes each GIF losslessly
# with gifsicle and fails when a GIF is missing or larger than
# MAX_GIF_BYTES.
#
# Usage: scripts/record-demos.sh [-h] [--binary PATH] [TAPE...]
#
#   --binary PATH  the lazysubmodules binary to record, a statically linked
#                  Linux binary for this machine; by default the script builds
#                  bin/lazysubmodules with "scripts/build-in-container.sh
#                  build" first
#   TAPE           the tapes to record, by name (hero, tag-pattern, cli,
#                  drift, safety); by default all of them
#
# CONTAINER_ENGINE selects podman or docker, as for
# scripts/build-in-container.sh. Building the image needs network access
# once; recording does not.
#
# Exit status: 0 when every GIF was recorded, 1 on a failure, 2 on a usage
# error.

set -euo pipefail

readonly SCRIPT_NAME="${0##*/}"
REPO_ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
readonly REPO_ROOT

# The tapes, in the order they are recorded.
readonly TAPES=(hero tag-pattern cli drift safety)

readonly DEMO_DIR="docs/demo"
readonly IMAGE_NAME="localhost/lazysubmodules-vhs"

# Mount point of the repository and the working directory of VHS; the
# tapes name their files relative to it.
readonly SRC_DIR="/src"

# The largest GIF accepted, in bytes; README pages load every GIF.
readonly MAX_GIF_BYTES=1500000

# Chromium, which VHS drives, keeps its frames in /dev/shm.
readonly SHM_SIZE="512m"

BINARY=""
SELECTED=()

# Set by select_engine, select_image and main.
ENGINE=""
ENGINE_USER_ARGS=()
IMAGE=""
WORK_DIR=""

usage()
{
	cat <<-EOF
		Usage: ${SCRIPT_NAME} [-h] [--binary PATH] [TAPE...]

		Record the demo GIFs in ${DEMO_DIR} from their VHS tapes, in a container.

		Options:
		  --binary PATH  statically linked lazysubmodules binary to record
		                 (default: build bin/lazysubmodules with
		                 scripts/build-in-container.sh build)
		  -h, --help     show this help

		Tapes: ${TAPES[*]} (default: all)

		Environment: CONTAINER_ENGINE (podman or docker; default podman, then docker)
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

die_usage()
{
	log "$*"
	printf "Run '%s --help' for usage.\n" "$SCRIPT_NAME" >&2
	exit 2
}

cleanup()
{
	if [[ -n "$WORK_DIR" ]]; then
		rm -rf -- "$WORK_DIR"
	fi
}

# is_tape NAME
#
# Succeed when NAME is one of TAPES.
is_tape()
{
	local tape

	for tape in "${TAPES[@]}"; do
		if [[ "$tape" == "$1" ]]; then
			return 0
		fi
	done
	return 1
}

parse_args()
{
	while (($# > 0)); do
		case "$1" in
		-h | --help)
			usage
			exit 0
			;;
		--binary)
			if (($# < 2)) || [[ -z "$2" ]]; then
				die_usage "option --binary needs a value"
			fi
			BINARY="$2"
			shift
			;;
		-*)
			die_usage "unknown option: $1"
			;;
		*)
			is_tape "$1" || die_usage "unknown tape: $1"
			SELECTED+=("$1")
			;;
		esac
		shift
	done
	if ((${#SELECTED[@]} == 0)); then
		SELECTED=("${TAPES[@]}")
	fi
	readonly SELECTED
}

# Pick the container engine: CONTAINER_ENGINE, else podman, else docker.
# Sets ENGINE and ENGINE_USER_ARGS. Rootless engines map the container's
# root user to the invoking user, so the GIFs belong to that user; rootful
# engines run the container as the invoking user instead.
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
		if [[ "$("$ENGINE" info --format '{{.Host.Security.Rootless}}')" != "true" ]]; then
			ENGINE_USER_ARGS=(--user "$(id -u):$(id -g)")
		fi
	elif [[ "$("$ENGINE" info --format '{{.SecurityOptions}}')" != *rootless* ]]; then
		ENGINE_USER_ARGS=(--user "$(id -u):$(id -g)")
	fi
	readonly ENGINE ENGINE_USER_ARGS
}

# Derive the image reference from the Containerfile content. Sets IMAGE.
select_image()
{
	local sum

	sum="$(sha256sum "${REPO_ROOT}/${DEMO_DIR}/Containerfile")" ||
		die "cannot hash ${DEMO_DIR}/Containerfile"
	IMAGE="${IMAGE_NAME}:${sum:0:12}"
	readonly IMAGE
}

# Build the recording image unless it exists. Its tag is derived from the
# Containerfile, so an existing image was built from this Containerfile.
ensure_image()
{
	local context="${WORK_DIR}/context"

	if "$ENGINE" image inspect "$IMAGE" >/dev/null 2>&1; then
		return 0
	fi
	log "building image ${IMAGE}"
	# The Containerfile copies nothing, so the build context stays empty.
	mkdir -- "$context"
	"$ENGINE" build --tag "$IMAGE" --file "${REPO_ROOT}/${DEMO_DIR}/Containerfile" "$context" ||
		die "cannot build ${IMAGE}"
}

# Copy the binary to record into WORK_DIR, building it first unless
# --binary was given. The copy is mounted, so the engine relabels only the
# copy on SELinux hosts.
prepare_binary()
{
	if [[ -z "$BINARY" ]]; then
		log "building bin/lazysubmodules"
		"${REPO_ROOT}/scripts/build-in-container.sh" build ||
			die "cannot build bin/lazysubmodules"
		BINARY="${REPO_ROOT}/bin/lazysubmodules"
	fi
	if [[ ! -f "$BINARY" || ! -x "$BINARY" ]]; then
		die "not an executable file: $BINARY"
	fi
	cp -- "$BINARY" "${WORK_DIR}/lazysubmodules"
	chmod 0755 "${WORK_DIR}/lazysubmodules"
}

# run_in_image COMMAND [ARG...]
#
# Run COMMAND in the recording image, offline, in the repository.
run_in_image()
{
	"$ENGINE" run --rm --pull=never --network=none --shm-size="$SHM_SIZE" \
		"${ENGINE_USER_ARGS[@]}" \
		--env HOME=/tmp \
		--volume "${REPO_ROOT}:${SRC_DIR}:ro,Z" \
		--volume "${REPO_ROOT}/${DEMO_DIR}:${SRC_DIR}/${DEMO_DIR}:Z" \
		--volume "${WORK_DIR}/lazysubmodules:/usr/local/bin/lazysubmodules:ro,Z" \
		--workdir "$SRC_DIR" \
		--entrypoint /bin/bash \
		"$IMAGE" -c "$@"
}

# record TAPE
#
# Record DEMO_DIR/TAPE.gif from DEMO_DIR/TAPE.tape and optimize it. VHS
# reports a failed command or Wait, but not a failed ffmpeg run, so the GIF
# is removed first and must exist afterwards.
record()
{
	local tape="${DEMO_DIR}/$1.tape" gif="${DEMO_DIR}/$1.gif"
	local output="${WORK_DIR}/$1.log"
	local size

	log "recording ${gif}"
	rm -f -- "${REPO_ROOT:?}/${gif}"
	# The script runs in the container's bash; $1 and $2 are its arguments.
	# shellcheck disable=SC2016
	if ! run_in_image 'set -euo pipefail
		vhs "$1"
		gifsicle --info "$2" >/dev/null
		gifsicle --batch --optimize=3 "$2"' record "$tape" "$gif" >"$output" 2>&1; then
		tail -n 25 -- "$output" >&2
		die "recording ${gif} failed"
	fi
	size="$(stat -c %s -- "${REPO_ROOT}/${gif}")"
	printf '%-28s %9d bytes\n' "$gif" "$size"
	if ((size > MAX_GIF_BYTES)); then
		die "${gif} is larger than ${MAX_GIF_BYTES} bytes; shorten ${tape}"
	fi
}

main()
{
	local tape

	parse_args "$@"
	select_engine
	select_image
	WORK_DIR="$(mktemp -d "${TMPDIR:-/tmp}/record-demos.XXXXXX")" || die "mktemp failed"
	readonly WORK_DIR
	trap cleanup EXIT

	prepare_binary
	ensure_image
	run_in_image 'lazysubmodules version >/dev/null && git --version >/dev/null' ||
		die "the binary does not run in ${IMAGE}; is it a static Linux binary for this machine?"
	for tape in "${SELECTED[@]}"; do
		record "$tape"
	done
}

main "$@"
