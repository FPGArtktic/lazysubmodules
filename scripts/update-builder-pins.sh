#!/usr/bin/env bash
# SPDX-License-Identifier: GPL-3.0-only
# Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>
#
# update-builder-pins.sh - move the build image pins to the newest versions
#
# Resolves the newest dated archlinux:base-devel tag and its digest on Docker
# Hub, the Arch Linux Archive snapshot of the same day, the newest commits of
# the AUR packages on the official GitHub mirror of the AUR, and the newest
# go-licenses release on the Go module proxy. The versions of the AUR packages
# are read from the fetched recipes, and the GoReleaser checksum of the recipe
# must match the checksums.txt of the upstream release. The ARG pins in the
# Containerfile are then rewritten in place.
#
# Usage: scripts/update-builder-pins.sh [-h] [-n]
#
# Needs curl and git, and network access. Nothing is changed when the pins are
# already current. Review the result with "git diff Containerfile", rebuild
# with "scripts/build-in-container.sh image" and commit it with the prefix
# "build:".
#
# Exit status: 0 on success, 1 on errors, 2 on usage errors.

set -euo pipefail

readonly SCRIPT_NAME="${0##*/}"
REPO_ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
readonly REPO_ROOT
readonly CONTAINERFILE="${REPO_ROOT}/Containerfile"

readonly REGISTRY="https://registry-1.docker.io"
readonly REGISTRY_AUTH="https://auth.docker.io/token"
readonly ARCHIVE_URL="https://archive.archlinux.org/repos"
readonly AUR_MIRROR="https://github.com/archlinux/aur.git"
readonly GORELEASER_RELEASES="https://github.com/goreleaser/goreleaser/releases/download"
readonly GORELEASER_ASSET="goreleaser_Linux_x86_64.tar.gz"
readonly GO_PROXY="https://proxy.golang.org"
readonly GO_LICENSES_MODULE="github.com/google/go-licenses/v2"
MANIFEST_TYPES="application/vnd.oci.image.index.v1+json"
MANIFEST_TYPES+=", application/vnd.docker.distribution.manifest.list.v2+json"
readonly MANIFEST_TYPES

# Pin names in the order they appear in the Containerfile, and the new values
# resolved by resolve_pins.
readonly PINS=(
	ARCH_IMAGE_TAG
	ARCH_IMAGE_DIGEST
	ARCH_ARCHIVE_DATE
	AUR_GORELEASER_COMMIT
	AUR_GORELEASER_VERSION
	AUR_GORELEASER_SHA256
	AUR_GITLINT_COMMIT
	AUR_GITLINT_VERSION
	GO_LICENSES_VERSION
)
declare -A NEW=()

WORK_DIR=""

usage()
{
	cat <<-EOF
		Usage: ${SCRIPT_NAME} [-h] [-n]

		Update the pins of the build image in the Containerfile: the dated
		archlinux:base-devel tag and digest, the Arch Linux Archive date, the
		commits and versions of the AUR packages goreleaser-bin and gitlint, and
		the go-licenses version.

		Options:
		  -n, --dry-run  print the new pins without changing the Containerfile
		  -h, --help     show this help
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
	log "error: $*"
	printf "Try '%s -h' for more information.\n" "$SCRIPT_NAME" >&2
	exit 2
}

cleanup()
{
	if [[ -n "$WORK_DIR" ]]; then
		rm -rf "$WORK_DIR"
	fi
}

fetch()
{
	curl --proto '=https' --tlsv1.2 --fail --silent --show-error --location "$@"
}

# expect NAME VALUE REGEX
#
# Fail unless VALUE matches the extended regular expression REGEX as a whole.
expect()
{
	if [[ ! "$2" =~ ^($3)$ ]]; then
		die "unexpected ${1}: '${2}'"
	fi
}

# current_pin NAME
#
# Print the value of "ARG NAME=VALUE" in the Containerfile.
current_pin()
{
	local -a values

	mapfile -t values < <(sed -n "s/^ARG ${1}=//p" "$CONTAINERFILE")
	if ((${#values[@]} != 1)); then
		die "expected one 'ARG ${1}=' line in the Containerfile, found ${#values[@]}"
	fi
	printf '%s\n' "${values[0]}"
}

# registry_token
#
# Print an anonymous pull token for library/archlinux.
registry_token()
{
	fetch --get --data service=registry.docker.io \
		--data scope=repository:library/archlinux:pull "$REGISTRY_AUTH" |
		grep -Eo '"token": *"[^"]+"' | head -n 1 | cut -d '"' -f 4
}

# newest_image_tag TOKEN
#
# Print the newest base-devel-YYYYMMDD.N.BUILD tag, following the pagination
# of the registry's tag list.
newest_image_tag()
{
	local token="$1"
	local path="/v2/library/archlinux/tags/list?n=1000"
	local headers="${WORK_DIR}/tags.headers"
	local page="${WORK_DIR}/tags.json"
	local tags="${WORK_DIR}/tags.txt"

	: >"$tags"
	while [[ -n "$path" ]]; do
		fetch --header "Authorization: Bearer ${token}" --dump-header "$headers" \
			--output "$page" "${REGISTRY}${path}" || return 1
		grep -Eo '"base-devel-[0-9]{8}\.[0-9]+\.[0-9]+"' "$page" | tr -d '"' >>"$tags" || true
		path="$(tr -d '\r' <"$headers" | sed -n 's/^link: *<\([^>]*\)>; *rel="next".*/\1/Ip')"
	done
	sort -V "$tags" | tail -n 1
}

# image_digest TOKEN TAG
#
# Print the digest of the image index TAG, after checking that it has a
# linux/amd64 image.
image_digest()
{
	local url="${REGISTRY}/v2/library/archlinux/manifests/${2}"
	local amd64='"platform":\{("architecture":"amd64","os":"linux"'
	local digest

	amd64+='|"os":"linux","architecture":"amd64")'

	digest="$(fetch --head --header "Authorization: Bearer ${1}" \
		--header "Accept: ${MANIFEST_TYPES}" "$url" |
		tr -d '\r' | sed -n 's/^docker-content-digest: *\(sha256:[0-9a-f]*\)$/\1/Ip')" ||
		return 1
	expect "digest of ${2}" "$digest" 'sha256:[0-9a-f]{64}'
	fetch --header "Authorization: Bearer ${1}" --header "Accept: ${MANIFEST_TYPES}" \
		--output "${WORK_DIR}/index.json" "${url%/*}/${digest}" || return 1
	if ! tr -d ' \n' <"${WORK_DIR}/index.json" | grep -Eq "$amd64"; then
		log "error: ${2} has no linux/amd64 image"
		return 1
	fi
	printf '%s\n' "$digest"
}

# fetch_aur PACKAGE
#
# Fetch the newest commit of the AUR package branch into WORK_DIR/PACKAGE and
# print its commit ID.
fetch_aur()
{
	local dir="${WORK_DIR}/${1}"
	local commit

	commit="$(git ls-remote "$AUR_MIRROR" "refs/heads/${1}" | cut -f 1)" || return 1
	expect "commit of ${1}" "$commit" '[0-9a-f]{40}'
	git init --quiet --initial-branch=aur "$dir" || return 1
	git -C "$dir" -c transfer.fsckObjects=true fetch --quiet --depth=1 \
		"$AUR_MIRROR" "$commit" || return 1
	git -C "$dir" -c advice.detachedHead=false checkout --quiet FETCH_HEAD || return 1
	printf '%s\n' "$commit"
}

# srcinfo PACKAGE KEY
#
# Print the values of KEY in the .SRCINFO of the fetched PACKAGE.
srcinfo()
{
	sed -n "s/^[[:space:]]*${2} = //p" "${WORK_DIR}/${1}/.SRCINFO"
}

# upstream_goreleaser_sha256 VERSION
#
# Print the checksum of the x86_64 archive from the release's checksums.txt.
upstream_goreleaser_sha256()
{
	fetch "${GORELEASER_RELEASES}/v${1}/checksums.txt" |
		sed -n "s/^\([0-9a-f]\{64\}\)  ${GORELEASER_ASSET}\$/\1/p"
}

# latest_module_version MODULE
#
# Print the newest release of the Go module MODULE (a lowercase path) that the
# Go module proxy reports.
latest_module_version()
{
	fetch "${GO_PROXY}/${1}/@latest" |
		grep -Eo '"Version": *"[^"]+"' | head -n 1 | cut -d '"' -f 4
}

resolve_pins()
{
	local token tag day aur_sha256 upstream_sha256 version

	log "resolving archlinux:base-devel"
	token="$(registry_token)" || die "cannot get a Docker Hub token"
	[[ -n "$token" ]] || die "cannot get a Docker Hub token"
	tag="$(newest_image_tag "$token")" || die "cannot list the archlinux tags"
	expect "archlinux tag" "$tag" 'base-devel-[0-9]{8}\.[0-9]+\.[0-9]+'
	NEW[ARCH_IMAGE_TAG]="$tag"
	NEW[ARCH_IMAGE_DIGEST]="$(image_digest "$token" "$tag")" ||
		die "cannot resolve the digest of ${tag}"

	day="${tag#base-devel-}"
	day="${day:0:8}"
	NEW[ARCH_ARCHIVE_DATE]="${day:0:4}/${day:4:2}/${day:6:2}"
	log "checking the archive snapshot ${NEW[ARCH_ARCHIVE_DATE]}"
	fetch --head --output /dev/null \
		"${ARCHIVE_URL}/${NEW[ARCH_ARCHIVE_DATE]}/core/os/x86_64/core.db" ||
		die "the Arch Linux Archive has no snapshot for ${NEW[ARCH_ARCHIVE_DATE]}"

	log "resolving the AUR packages"
	NEW[AUR_GORELEASER_COMMIT]="$(fetch_aur goreleaser-bin)" ||
		die "cannot fetch goreleaser-bin"
	NEW[AUR_GORELEASER_VERSION]="$(srcinfo goreleaser-bin pkgver)"
	expect "goreleaser-bin version" "${NEW[AUR_GORELEASER_VERSION]}" '[0-9]+\.[0-9]+\.[0-9]+'
	aur_sha256="$(srcinfo goreleaser-bin sha256sums_x86_64)"
	upstream_sha256="$(upstream_goreleaser_sha256 "${NEW[AUR_GORELEASER_VERSION]}")" ||
		die "cannot fetch the checksums of GoReleaser v${NEW[AUR_GORELEASER_VERSION]}"
	expect "upstream checksum of ${GORELEASER_ASSET}" "$upstream_sha256" '[0-9a-f]{64}'
	if [[ "$aur_sha256" != "$upstream_sha256" ]]; then
		die "goreleaser-bin checksum ${aur_sha256} differs from upstream ${upstream_sha256}"
	fi
	NEW[AUR_GORELEASER_SHA256]="$upstream_sha256"

	NEW[AUR_GITLINT_COMMIT]="$(fetch_aur gitlint)" || die "cannot fetch gitlint"
	NEW[AUR_GITLINT_VERSION]="$(srcinfo gitlint pkgver)"
	expect "gitlint version" "${NEW[AUR_GITLINT_VERSION]}" '[0-9]+(\.[0-9]+)+'

	# The image build verifies the module against the Go checksum database.
	log "resolving go-licenses"
	version="$(latest_module_version "$GO_LICENSES_MODULE")" ||
		die "cannot query the Go module proxy for ${GO_LICENSES_MODULE}"
	expect "go-licenses version" "$version" 'v[0-9]+\.[0-9]+\.[0-9]+'
	NEW[GO_LICENSES_VERSION]="${version#v}"
}

# rewrite_pins
#
# Replace the ARG lines of the Containerfile with the new values. The values
# were validated by resolve_pins and contain no sed metacharacters.
rewrite_pins()
{
	local name
	local -a script=()

	for name in "${PINS[@]}"; do
		script+=(-e "s|^ARG ${name}=.*\$|ARG ${name}=${NEW[$name]}|")
	done
	sed -i "${script[@]}" "$CONTAINERFILE" || die "cannot rewrite $CONTAINERFILE"
	for name in "${PINS[@]}"; do
		if [[ "$(current_pin "$name")" != "${NEW[$name]}" ]]; then
			die "rewriting ${name} failed"
		fi
	done
}

main()
{
	local name old
	local dry_run=0
	local changed=0

	while (($# > 0)); do
		case "$1" in
		-h | --help)
			usage
			return 0
			;;
		-n | --dry-run)
			dry_run=1
			;;
		*)
			die_usage "unknown argument: $1"
			;;
		esac
		shift
	done
	[[ -f "$CONTAINERFILE" ]] || die "Containerfile not found: $CONTAINERFILE"
	for name in "${PINS[@]}"; do
		current_pin "$name" >/dev/null
	done

	WORK_DIR="$(mktemp -d)"
	trap cleanup EXIT
	resolve_pins

	printf '%-24s %s\n' PIN VALUE
	for name in "${PINS[@]}"; do
		old="$(current_pin "$name")"
		if [[ "$old" == "${NEW[$name]}" ]]; then
			printf '%-24s %s\n' "$name" "$old"
		else
			printf '%-24s %s -> %s\n' "$name" "$old" "${NEW[$name]}"
			changed=$((changed + 1))
		fi
	done

	if ((changed == 0)); then
		log "the Containerfile pins are current"
	elif ((dry_run)); then
		log "dry run: ${changed} pin(s) would change"
	else
		rewrite_pins
		log "updated ${changed} pin(s) in ${CONTAINERFILE}"
		log "next: git diff --stat Containerfile;" \
			"scripts/build-in-container.sh image lint test;" \
			"commit with the prefix 'build:'"
	fi
}

main "$@"
