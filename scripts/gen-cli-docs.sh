#!/usr/bin/env bash
# SPDX-License-Identifier: GPL-3.0-only
# Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>
#
# gen-cli-docs.sh - capture the help texts of lazysubmodules for the docs
#
# The command line reference in docs/reference/cli/ includes the help texts
# of the program from text files in docs/reference/cli/_generated/, so the
# documentation shows exactly what the binary prints. This script writes
# those files: lazysubmodules.txt from "lazysubmodules help", and
# <command>.txt from "lazysubmodules <command> -h" for every command listed
# in the "Commands:" block of that help text, so a new command gets a file
# without a change here. Each file starts with the SPDX header and a note
# that it is generated, followed by an empty line; the pages include it from
# line 5 on.
#
# Every help invocation runs with LC_ALL=C and NO_COLOR=1, and must exit with
# status 0 and write nothing to standard error.
#
# Usage: scripts/gen-cli-docs.sh [-h] [--check] [--binary PATH]
#
#   --check        write nothing; compare the committed files with the output
#                  of the binary, and fail when a file differs, is missing or
#                  is left over, or when a command other than help has no
#                  page docs/reference/cli/<command>.md
#   --binary PATH  the lazysubmodules binary; the default is
#                  bin/lazysubmodules of this repository if it exists, else
#                  lazysubmodules from PATH
#
# Exit status: 0 when the files were written or are current, 1 when they are
# stale or a help text cannot be captured, 2 on a usage error.

set -euo pipefail

readonly SCRIPT_NAME="${0##*/}"
REPO_ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
readonly REPO_ROOT

readonly PROG="lazysubmodules"
readonly PAGE_DIR="docs/reference/cli"
readonly OUT_DIR="${PAGE_DIR}/_generated"
readonly SPDX="# SPDX-License-Identifier: GPL-3.0-only"
readonly COPYRIGHT="# Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>"

# Command line settings.
CHECK=0
BINARY=""

# Temporary directory, removed on exit.
WORK=""

usage()
{
	cat <<-EOF
		Usage: ${SCRIPT_NAME} [-h] [--check] [--binary PATH]

		Write the help texts of ${PROG} to ${OUT_DIR}/, which
		the command line reference of the documentation includes.

		Options:
		  --check        compare the committed files with the binary instead of
		                 writing them; fail when they are stale or a command has
		                 no page in ${PAGE_DIR}/
		  --binary PATH  ${PROG} binary (default: bin/${PROG} of the
		                 repository if it exists, else from PATH)
		  -h, --help     show this help

		Exit status: 0 when the files are written or current, 1 when they are
		stale or a help text cannot be captured, 2 on a usage error.
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
	if [[ -n "$WORK" ]]; then
		rm -rf -- "$WORK"
	fi
}

parse_args()
{
	while (($# > 0)); do
		case "$1" in
		-h | --help)
			usage
			exit 0
			;;
		--check)
			CHECK=1
			;;
		--binary)
			if (($# < 2)) || [[ -z "$2" || "$2" == -* ]]; then
				die_usage "option --binary needs a path"
			fi
			BINARY="$2"
			shift
			;;
		*)
			die_usage "unknown argument: $1"
			;;
		esac
		shift
	done
}

# find_binary
#
# Set BINARY to the absolute path of the binary to run, chosen as the usage
# describes it.
find_binary()
{
	local dir

	if [[ -z "$BINARY" ]]; then
		if [[ -x "${REPO_ROOT}/bin/${PROG}" ]]; then
			BINARY="${REPO_ROOT}/bin/${PROG}"
		elif ! BINARY="$(command -v "$PROG")"; then
			die "${PROG} not found; build it with" \
				"'scripts/build-in-container.sh build' or pass --binary"
		fi
	fi
	if [[ ! -f "$BINARY" || ! -x "$BINARY" ]]; then
		die "not an executable file: $BINARY"
	fi
	dir="$(cd -- "$(dirname -- "$BINARY")" && pwd -P)" ||
		die "no such directory: $(dirname -- "$BINARY")"
	BINARY="${dir}/$(basename -- "$BINARY")"
	readonly BINARY
}

# capture OUTPUT ARGS...
#
# Run the binary with ARGS and write its standard output to OUTPUT. Fail
# unless it exits with status 0 and writes nothing to standard error.
capture()
{
	local output="$1"
	local status=0

	shift
	LC_ALL=C NO_COLOR=1 "$BINARY" "$@" >"$output" 2>"${WORK}/stderr" || status=$?
	if ((status != 0)); then
		cat -- "${WORK}/stderr" >&2
		die "'${PROG} $*' exited with status ${status}"
	fi
	if [[ -s "${WORK}/stderr" ]]; then
		cat -- "${WORK}/stderr" >&2
		die "'${PROG} $*' wrote to standard error"
	fi
}

# list_commands HELP
#
# Print the command names of the "Commands:" block of the help text in the
# file HELP, one per line.
list_commands()
{
	awk '
		/^Commands:$/ { listing = 1; next }
		listing && /^$/ { exit }
		listing { print $1 }
	' "$1"
}

# write_help DIR NAME ARGS...
#
# Write DIR/NAME.txt: the header, then the output of the binary with ARGS.
write_help()
{
	local file="$1/$2.txt"

	shift 2
	capture "${WORK}/output" "$@"
	{
		printf '%s\n%s\n' "$SPDX" "$COPYRIGHT"
		printf "# Generated by scripts/%s from '%s'; do not edit.\n\n" \
			"$SCRIPT_NAME" "${PROG} $*"
		cat -- "${WORK}/output"
	} >"$file"
}

# generate DIR
#
# Write every help file to DIR and the command names to WORK/commands.
generate()
{
	local dir="$1"
	local cmd

	mkdir -p -- "$dir"
	write_help "$dir" "$PROG" help
	list_commands "${dir}/${PROG}.txt" >"${WORK}/commands"
	if [[ ! -s "${WORK}/commands" ]]; then
		die "'${PROG} help' lists no commands"
	fi
	while IFS= read -r cmd; do
		if [[ "$cmd" == "$PROG" || ! "$cmd" =~ ^[a-z][a-z0-9-]*$ ]]; then
			die "unexpected command name in '${PROG} help': $cmd"
		fi
		write_help "$dir" "$cmd" "$cmd" -h
	done <"${WORK}/commands"
}

# missing_pages
#
# Print a line for every command except help without a reference page.
missing_pages()
{
	local cmd

	while IFS= read -r cmd; do
		if [[ "$cmd" != help && ! -f "${PAGE_DIR}/${cmd}.md" ]]; then
			printf '%s\n' "${PAGE_DIR}/${cmd}.md: no page for command ${cmd}"
		fi
	done <"${WORK}/commands"
}

# check NEW
#
# Compare OUT_DIR with the files generated in NEW, and report every
# difference. Return: 0 when OUT_DIR is current.
check()
{
	local new="$1"
	local file name stale=0

	for file in "${new}"/*.txt; do
		name="${file##*/}"
		if [[ ! -f "${OUT_DIR}/${name}" ]]; then
			log "${OUT_DIR}/${name}: missing"
			stale=1
		elif ! diff -u --label "${OUT_DIR}/${name}" --label "${PROG} output" \
			-- "${OUT_DIR}/${name}" "$file" >&2; then
			stale=1
		fi
	done
	for file in "${OUT_DIR}"/*.txt; do
		[[ -e "$file" ]] || continue
		name="${file##*/}"
		if [[ ! -f "${new}/${name}" ]]; then
			log "${OUT_DIR}/${name}: no such command any more"
			stale=1
		fi
	done
	missing_pages >"${WORK}/pages"
	if [[ -s "${WORK}/pages" ]]; then
		while IFS= read -r file; do
			log "$file"
		done <"${WORK}/pages"
		stale=1
	fi
	if ((stale)); then
		log "the command line reference is stale; run scripts/${SCRIPT_NAME}" \
			"and add a page for every new command"
		return 1
	fi
	printf '%s: %s is current\n' "$SCRIPT_NAME" "$OUT_DIR"
}

# install_files NEW
#
# Replace the files in OUT_DIR with those generated in NEW.
install_files()
{
	local new="$1"
	local file line count=0

	mkdir -p -- "$OUT_DIR"
	for file in "${OUT_DIR}"/*.txt; do
		if [[ -e "$file" && ! -f "${new}/${file##*/}" ]]; then
			rm -f -- "$file"
			log "removed ${file}"
		fi
	done
	for file in "${new}"/*.txt; do
		cp -- "$file" "${OUT_DIR}/${file##*/}"
		count=$((count + 1))
	done
	missing_pages >"${WORK}/pages"
	while IFS= read -r line; do
		log "note: $line"
	done <"${WORK}/pages"
	printf '%s: wrote %d files to %s\n' "$SCRIPT_NAME" "$count" "$OUT_DIR"
}

main()
{
	parse_args "$@"
	find_binary
	cd "$REPO_ROOT"
	WORK="$(mktemp -d "${TMPDIR:-/tmp}/${SCRIPT_NAME%.sh}.XXXXXX")"
	trap cleanup EXIT
	generate "${WORK}/new"
	if ((CHECK)); then
		check "${WORK}/new"
	else
		install_files "${WORK}/new"
	fi
}

main "$@"
