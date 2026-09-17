#!/usr/bin/env bash
# SPDX-License-Identifier: GPL-3.0-only
# Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>
#
# third-party-licenses.sh - collect the license notices of the release binary
#
# The lazysubmodules binary is statically linked. It contains the Go standard
# library and third-party Go modules whose licenses (MIT, BSD) require their
# copyright notices and license texts to accompany binary distributions. This
# script writes them to OUTPUT-DIR for the release archives and packages:
#
#   THIRD_PARTY_NOTICES  one stanza per module: module path, version, SPDX
#                        license identifier and license file, relative to the
#                        notices file
#   licenses/            the license files saved by go-licenses, plus the
#                        license of the Go distribution in licenses/std/
#   copyright            Debian copyright file: the project notice, the
#                        stanzas and every license text in a single file
#
# Only packages linked into ./cmd/lazysubmodules for the given platforms
# count, so test-only dependencies are left out, and so is the project's own
# module. Modules come from vendor/, and the script never uses the network.
# OUTPUT-DIR is replaced as a whole. If it exists, it must be empty or hold
# the output of an earlier run. When SOURCE_DATE_EPOCH is set, every output
# file gets it as modification time, so that archives and packages that copy
# the time stay reproducible.
#
# Usage: scripts/third-party-licenses.sh [-h] [-o OUTPUT-DIR] [PLATFORM...]
#
# PLATFORM is GOOS/GOARCH, e.g. linux/arm64; the default is the platform of
# the go command. OUTPUT-DIR defaults to build/third-party in the repository;
# a relative OUTPUT-DIR is relative to the current directory.
# Requires go and go-licenses, both in the build image. GoReleaser runs the
# script as a before hook (see .goreleaser.yaml).

set -euo pipefail

readonly SCRIPT_NAME="${0##*/}"
REPO_ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
readonly REPO_ROOT
readonly MODULE_PATH="github.com/FPGArtktic/lazysubmodules"
readonly PROJECT_URL="https://github.com/FPGArtktic/lazysubmodules"
readonly PROJECT_COPYRIGHT="Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>"
readonly MAIN_PACKAGE="./cmd/lazysubmodules"
readonly DEFAULT_OUTPUT="${REPO_ROOT}/build/third-party"
readonly NOTICES="THIRD_PARTY_NOTICES"
readonly COPYRIGHT="copyright"
readonly LICENSES="licenses"

# The Go distribution: pseudo module "std" (the module name of the standard
# library in GOROOT/src/go.mod) with the license of the Go project.
readonly STD_MODULE="std"
readonly STD_LICENSE="BSD-3-Clause"

# The build settings of .goreleaser.yaml that change which packages are
# linked, and offline module resolution from vendor/.
readonly GO_ENV=(
	CGO_ENABLED=0
	GOFLAGS=-mod=vendor
	GOPROXY=off
	GOTOOLCHAIN=local
	GOWORK=off
)

# go list template: module path and version of each package's module, except
# the main module; a replacement is shown instead of the replaced version.
# go list ends every result with a newline, including the empty ones of
# standard library packages.
# shellcheck disable=SC2016 # a Go template, not a shell expansion
readonly MODULE_TEMPLATE='{{with .Module}}{{if not .Main}}{{$m := .}}{{.Path}}{{"\t"}}
{{- with .Replace}}{{if ne .Path $m.Path}}{{.Path}} {{end}}{{.Version}}
{{- else}}{{.Version}}{{end}}{{end}}{{end}}'

# go-licenses report template: library name, license identifier, license file.
readonly REPORT_TEMPLATE='{{range .}}{{.Name}}{{"\t"}}{{.LicenseName}}{{"\t"}}
{{- .LicensePath}}{{"\n"}}{{end}}'

# Temporary directory of this run, removed on exit.
WORK_DIR=""

usage()
{
	cat <<-EOF
		Usage: ${SCRIPT_NAME} [-h] [-o OUTPUT-DIR] [PLATFORM...]

		Write the license notices of the Go standard library and of the
		third-party modules linked into lazysubmodules to OUTPUT-DIR
		(default: ${DEFAULT_OUTPUT}):

		  ${NOTICES}  module, version, license and license file
		  ${LICENSES}/             license texts
		  ${COPYRIGHT}            Debian copyright file with all texts

		PLATFORM is GOOS/GOARCH (default: the platform of the go command).
		Modules come from vendor/; the network is never used. Requires go and
		go-licenses.

		Environment:
		  SOURCE_DATE_EPOCH  modification time of the output files (seconds
		                     since the epoch; default: the current time)

		Options:
		  -o OUTPUT-DIR  output directory, replaced as a whole
		  -h             show this help
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
		rm -rf -- "$WORK_DIR"
	fi
}

# check_output DIR
#
# Refuse to replace DIR unless it is missing, empty, or holds exactly what
# this script writes.
check_output()
{
	local dir="$1"
	local entries expected

	if [[ ! -e "$dir" && ! -L "$dir" ]]; then
		return 0
	fi
	if [[ -L "$dir" || ! -d "$dir" ]]; then
		die "output ${dir} exists and is not a directory"
	fi
	entries="$(find "$dir" -mindepth 1 -maxdepth 1 -printf '%f\n' | LC_ALL=C sort)"
	expected="$(printf '%s\n' "$NOTICES" "$COPYRIGHT" "$LICENSES" | LC_ALL=C sort)"
	if [[ -n "$entries" && "$entries" != "$expected" ]]; then
		die "output ${dir} is not empty and was not written by ${SCRIPT_NAME}"
	fi
}

# run_logged LOG COMMAND...
#
# Run COMMAND with its standard error appended to LOG, and print LOG when
# COMMAND fails.
run_logged()
{
	local log_file="$1"

	shift
	if ! "$@" 2>>"$log_file"; then
		cat -- "$log_file" >&2
		return 1
	fi
}

# collect PLATFORM INDEX
#
# Append the modules of PLATFORM to WORK_DIR/modules and the go-licenses report
# to WORK_DIR/report, and merge the saved license files into
# WORK_DIR/out/licenses.
collect()
{
	local platform="$1"
	local save="${WORK_DIR}/save-$2"
	local log_file="${WORK_DIR}/go-licenses.log"
	local -a vars=("${GO_ENV[@]}" "GOOS=${platform%/*}" "GOARCH=${platform#*/}")
	local -a flags=(--ignore "$MODULE_PATH" --skip_headers)

	log "collecting licenses for ${platform}"
	env "${vars[@]}" go list -deps -f "$MODULE_TEMPLATE" "$MAIN_PACKAGE" \
		>>"${WORK_DIR}/modules" || return 1
	run_logged "$log_file" env "${vars[@]}" go-licenses report "$MAIN_PACKAGE" "${flags[@]}" \
		--template "${WORK_DIR}/report.tmpl" >>"${WORK_DIR}/report" || return 1
	run_logged "$log_file" env "${vars[@]}" go-licenses save "$MAIN_PACKAGE" "${flags[@]}" \
		--save_path "$save" || return 1
	cp -R -- "${save}/." "${WORK_DIR}/out/${LICENSES}/"
}

# go_license
#
# Print the path of the license file of the Go distribution: GOROOT/LICENSE
# in upstream releases, /usr/share/licenses/go/LICENSE in the Arch Linux
# package that the build image uses.
go_license()
{
	local file

	for file in "${GOROOT}/LICENSE" /usr/share/licenses/go/LICENSE; do
		if [[ -f "$file" ]] && grep -q 'Copyright .* The Go Authors' "$file"; then
			printf '%s\n' "$file"
			return 0
		fi
	done
	log "error: no license file of the Go distribution in ${GOROOT}/LICENSE" \
		"or /usr/share/licenses/go/LICENSE"
	return 1
}

# go_version
#
# Print the Go release, without the experiment suffix that a distribution
# build of the toolchain may report (e.g. go1.27.1-X:nodwarf5).
go_version()
{
	local version

	version="$(go env GOVERSION)" || return 1
	version="${version%%[- ]X:*}"
	[[ -n "$version" ]] || return 1
	printf '%s\n' "$version"
}

# module_of NAME MODULE...
#
# Print the longest MODULE that NAME, a package path, belongs to.
module_of()
{
	local name="$1"
	local best="" module

	shift
	for module in "$@"; do
		if [[ ("$name" == "$module" || "$name" == "$module"/*) &&
			${#module} -gt ${#best} ]]; then
			best="$module"
		fi
	done
	[[ -n "$best" ]] || return 1
	printf '%s\n' "$best"
}

# build_rows
#
# Print one line per license: module, version, license and license file,
# separated by tabs and sorted. Fail unless every module has a license.
build_rows()
{
	local path version name license file module rel text std_file std_text
	local -A versions=() covered=()
	local -a lines=()

	while IFS=$'\t' read -r path version; do
		[[ -n "$path" ]] || continue
		versions["$path"]="${version% }"
	done < <(LC_ALL=C sort -u -- "${WORK_DIR}/modules")

	while IFS=$'\t' read -r name license file; do
		if [[ -z "$license" || "$license" == "Unknown" ]]; then
			die "go-licenses found no known license for ${name} (${file:-no file})"
		fi
		module="$(module_of "$name" "${!versions[@]}")" ||
			die "library ${name} belongs to none of the listed modules"
		rel="${file#"${REPO_ROOT}/vendor/"}"
		if [[ "$rel" == "$file" || "$rel" != "$module"/* ]]; then
			die "license file of ${name} is outside vendor/${module}: ${file}"
		fi
		text="${LICENSES}/${name}/${file##*/}"
		[[ -f "${WORK_DIR}/out/${text}" ]] || die "go-licenses did not save ${text}"
		lines+=("${module}"$'\t'"${versions[$module]}"$'\t'"${license}"$'\t'"${text}")
		covered["$module"]=1
	done < <(LC_ALL=C sort -u -- "${WORK_DIR}/report")

	for module in "${!versions[@]}"; do
		[[ -n "${covered[$module]:-}" ]] || die "no license file for module ${module}"
	done

	std_file="$(go_license)" || return 1
	std_text="${LICENSES}/${STD_MODULE}/LICENSE"
	install -D -m 0644 -- "$std_file" "${WORK_DIR}/out/${std_text}" || return 1
	version="$(go_version)" || die "cannot read the Go version"
	lines+=("${STD_MODULE}"$'\t'"${version}"$'\t'"${STD_LICENSE}"$'\t'"${std_text}")

	printf '%s\n' "${lines[@]}" | LC_ALL=C sort -u
}

# check_saved ROWS-FILE
#
# Fail when WORK_DIR/out/licenses holds a file that no row names as its
# license text. go-licenses also saves NOTICE files next to a license file,
# as Apache-2.0 requires them to be passed on, and the source code of
# modules whose license requires that. Neither is supported: such a file
# would ship without a stanza and be missing from the copyright file.
check_saved()
{
	local extra

	(cd -- "${WORK_DIR}/out" && find "$LICENSES" ! -type d) |
		LC_ALL=C sort >"${WORK_DIR}/saved" || return 1
	cut -f 4 -- "$1" | LC_ALL=C sort -u >"${WORK_DIR}/named" || return 1
	extra="$(LC_ALL=C comm -23 -- "${WORK_DIR}/saved" "${WORK_DIR}/named")" || return 1
	if [[ -n "$extra" ]]; then
		die "go-licenses saved files that no notice names, such as NOTICE" \
			"files, which this script does not support yet: ${extra//$'\n'/, }"
	fi
}

# write_stanzas ROWS-FILE
#
# Print a stanza for every row, each after an empty line.
write_stanzas()
{
	local module version license text

	while IFS=$'\t' read -r module version license text; do
		printf '\nModule: %s\nVersion: %s\nLicense: %s\nText: %s\n' \
			"$module" "$version" "$license" "$text"
	done <"$1"
}

# write_notices ROWS-FILE PLATFORM...
#
# Print THIRD_PARTY_NOTICES.
write_notices()
{
	local rows_file="$1"

	shift
	cat <<-EOF
		Third-party notices for lazysubmodules

		lazysubmodules is licensed under GPL-3.0-only; see LICENSE. The binary
		also contains the Go standard library and runtime (module "std") and
		the third-party Go modules listed below. Their licenses require these
		notices to accompany the binary. Each stanza names a module, its
		version, its license (SPDX identifier) and the file with the license
		text, relative to this file.

		Platforms: $*
	EOF
	write_stanzas "$rows_file"
}

# write_copyright ROWS-FILE PLATFORM...
#
# Print the Debian copyright file: the project notice, the stanzas and the
# license texts, each after an empty line and a line "==> FILE <==".
write_copyright()
{
	local rows_file="$1"
	local text

	shift
	cat <<-EOF
		lazysubmodules
		Source: ${PROJECT_URL}

		${PROJECT_COPYRIGHT}
		License: GPL-3.0-only

		On Debian systems, the complete text of the GNU General Public License
		version 3 can be found in /usr/share/common-licenses/GPL-3.

		Third-party notices

		The binary also contains the Go standard library and runtime (module
		"std") and the third-party Go modules listed below. Their licenses
		require these notices to accompany the binary. Each stanza names a
		module, its version, its license (SPDX identifier) and the license
		text, which follows the stanzas after a line "==> FILE <==".

		Platforms: $*
	EOF
	write_stanzas "$rows_file"
	while IFS= read -r text; do
		printf '\n==> %s <==\n' "$text"
		cat -- "${WORK_DIR}/out/${text}"
		if [[ -n "$(tail -c 1 -- "${WORK_DIR}/out/${text}")" ]]; then
			printf '\n'
		fi
	done < <(cut -f 4 -- "$rows_file" | LC_ALL=C sort -u)
}

main()
{
	local output="$DEFAULT_OUTPUT"
	local opt platform parent known index=0
	local -a platforms=()

	while getopts ':ho:' opt; do
		case "$opt" in
		h)
			usage
			return 0
			;;
		o)
			output="$OPTARG"
			;;
		:)
			die_usage "option -${OPTARG} needs an argument"
			;;
		*)
			die_usage "unknown option: -${OPTARG}"
			;;
		esac
	done
	shift $((OPTIND - 1))
	platforms=("$@")
	[[ -n "$output" ]] || die_usage "empty output directory"
	if [[ -n "${SOURCE_DATE_EPOCH:-}" && ! "$SOURCE_DATE_EPOCH" =~ ^[0-9]+$ ]]; then
		die_usage "SOURCE_DATE_EPOCH is not a number of seconds: ${SOURCE_DATE_EPOCH}"
	fi

	command -v go >/dev/null 2>&1 || die "go not found"
	command -v go-licenses >/dev/null 2>&1 || die "go-licenses not found"
	output="$(realpath --canonicalize-missing --no-symlinks -- "$output")"
	check_output "$output"
	# go list and go-licenses take the packages relative to the repository.
	cd -- "$REPO_ROOT"

	# go-licenses tells standard library packages apart by the GOROOT prefix;
	# without GOROOT it takes every package for one and reports nothing.
	GOROOT="${GOROOT:-$(go env GOROOT)}"
	[[ -n "$GOROOT" ]] || die "cannot determine GOROOT"
	export GOROOT
	if ((${#platforms[@]} == 0)); then
		platforms=("$(go env GOOS)/$(go env GOARCH)")
	fi
	known="$(go tool dist list)" || die "cannot list the platforms of go"
	for platform in "${platforms[@]}"; do
		grep -qxF -- "$platform" <<<"$known" ||
			die_usage "unknown platform: ${platform} (see go tool dist list)"
	done

	parent="$(dirname -- "$output")"
	mkdir -p -- "$parent"
	# Next to the output, so that the result is moved into place in one step.
	WORK_DIR="$(mktemp -d -- "${parent}/.${output##*/}.XXXXXX")" ||
		die "cannot create a temporary directory in ${parent}"
	trap cleanup EXIT
	mkdir -p -- "${WORK_DIR}/out/${LICENSES}"
	printf '%s' "$REPORT_TEMPLATE" >"${WORK_DIR}/report.tmpl"
	: >"${WORK_DIR}/modules"
	: >"${WORK_DIR}/report"
	: >"${WORK_DIR}/go-licenses.log"

	for platform in "${platforms[@]}"; do
		index=$((index + 1))
		collect "$platform" "$index" || die "cannot collect the licenses for ${platform}"
	done
	# Vendored modules have no version for go-licenses, which then warns about
	# the main module once per library; its license URLs are not used.
	awk -v skip="module ${MODULE_PATH} has empty version" \
		'index($0, skip) == 0 && !seen[$0]++' "${WORK_DIR}/go-licenses.log" >&2

	build_rows >"${WORK_DIR}/rows"
	check_saved "${WORK_DIR}/rows" || die "cannot compare the saved license files"
	write_notices "${WORK_DIR}/rows" "${platforms[@]}" >"${WORK_DIR}/out/${NOTICES}"
	write_copyright "${WORK_DIR}/rows" "${platforms[@]}" >"${WORK_DIR}/out/${COPYRIGHT}"
	chmod -R u=rwX,go=rX -- "${WORK_DIR}/out"
	if [[ -n "${SOURCE_DATE_EPOCH:-}" ]]; then
		find "${WORK_DIR}/out" -exec touch -d "@${SOURCE_DATE_EPOCH}" -- {} +
	fi

	rm -rf -- "$output"
	mv -- "${WORK_DIR}/out" "$output"
	log "wrote $(wc -l <"${WORK_DIR}/rows") notices to ${output}"
}

main "$@"
