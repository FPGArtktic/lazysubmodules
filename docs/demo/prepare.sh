#!/usr/bin/env bash
# SPDX-License-Identifier: GPL-3.0-only
# Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>
#
# prepare.sh - build the demo superproject for a VHS tape
#
# The tapes in this directory start with a hidden command that runs this
# script and sources the shell setup it writes. The script builds the
# superproject of scripts/demo.sh in its first state (--setup-only), with
# the lazysubmodules binary from PATH, and writes DIR/shell.sh. Sourcing
# that file in bash enters the demo environment (see scripts/demo.sh),
# changes into the superproject and sets a short prompt. A command that
# fails is followed by its exit status, as in scripts/demo.sh. New commits
# and tags get a fixed date, so the recordings show the same commit IDs
# every time.
#
# Usage: docs/demo/prepare.sh DIR
#
#   DIR  directory to create; it must not exist yet
#
# Exit status: 0 on success, 1 when the demo cannot be built, 2 on a usage
# error.

set -euo pipefail

readonly SCRIPT_NAME="${0##*/}"
REPO_ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)"
readonly REPO_ROOT

# Date of every commit and tag made during a recording: 2026-01-05 10:00 UTC,
# after the history that scripts/demo.sh writes.
readonly RECORDING_DATE="@1767607200 +0000"

die()
{
	printf '%s: %s\n' "$SCRIPT_NAME" "$*" >&2
	exit 1
}

usage()
{
	printf 'Usage: %s DIR\n' "$SCRIPT_NAME"
}

# write_shell_setup DIR
#
# Write DIR/shell.sh for the interactive shell of a tape.
write_shell_setup()
{
	local dir="$1"

	{
		printf '# Shell setup of a LazySubmodules VHS tape, for bash.\n'
		printf '. %q\n' "${dir}/demo/env.sh"
		printf 'cd %q\n' "${dir}/demo/firmware"
		printf 'export GIT_AUTHOR_DATE=%q GIT_COMMITTER_DATE=%q\n' \
			"$RECORDING_DATE" "$RECORDING_DATE"
		# The rest is expanded when the file is sourced.
		cat <<-'EOF'
			export COLORTERM=truecolor

			# Print the exit status of a failed command, with the meaning
			# that lazysubmodules gives it. An empty or comment line keeps
			# the status of the command before it, so the DEBUG trap marks
			# the lines that ran a command.
			show_exit_status()
			{
				local rc=$? ran="${lsm_demo_ran:-}" meaning=""

				lsm_demo_ran=""
				if [[ -z "$ran" ]] || ((rc == 0)); then
					return 0
				fi
				case "$rc" in
				1) meaning=": error" ;;
				2) meaning=": usage error" ;;
				3) meaning=": refused" ;;
				4) meaning=": verification failed" ;;
				5) meaning=": git failed" ;;
				esac
				printf '\033[33m[exit status %d%s]\033[0m\n' "$rc" "$meaning"
			}
			trap '[[ "$BASH_COMMAND" == show_exit_status ]] || lsm_demo_ran=1' DEBUG
			PROMPT_COMMAND=show_exit_status
			PS1='\[\e[1;34m\]firmware\[\e[0m\] $ '
		EOF
	} >"${dir}/shell.sh"
}

main()
{
	local dir binary

	if (($# != 1)) || [[ -z "$1" || "$1" == -* ]]; then
		usage >&2
		exit 2
	fi
	dir="$1"
	[[ ! -e "$dir" ]] || die "$dir exists already"
	binary="$(command -v lazysubmodules)" || die "lazysubmodules is not in PATH"
	mkdir -p -- "$dir"
	dir="$(cd -- "$dir" && pwd -P)"

	"${REPO_ROOT}/scripts/demo.sh" --keep "${dir}/demo" --binary "$binary" \
		--setup-only --no-pause >"${dir}/setup.log" 2>&1 ||
		die "scripts/demo.sh failed; see ${dir}/setup.log"
	write_shell_setup "$dir"
}

main "$@"
