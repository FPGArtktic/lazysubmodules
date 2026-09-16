#!/usr/bin/env bash
# SPDX-License-Identifier: GPL-3.0-only
# Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>
#
# check-headers.sh - verify the SPDX and copyright header of every file
#
# Checks all tracked files and all untracked files that are not ignored, so new
# files are caught before they are committed. The header must match exactly:
#
#   Go (*.go, go.mod):  "// SPDX-..." and "// Copyright ..." on lines 1-2,
#                       line 3 empty
#   Markdown (*.md):    "<!-- SPDX-... -->" and "<!-- Copyright ... -->"
#   Shell (*.sh):       "#!/usr/bin/env bash", then "# SPDX-..." and
#                       "# Copyright ..." on lines 2-3
#   Other scripts:      any "#!" line, then the "#" header on lines 2-3
#   Config files:       "# SPDX-..." and "# Copyright ..." on lines 1-2
#
# LICENSE, DCO, go.sum, vendor/, testdata/ and *.golden are exempt. Files of
# any other type are reported, so that a header rule is added for them.
#
# Usage: scripts/check-headers.sh [-h]
#
# Exit status: 0 when every file is correct, 1 when a header is wrong or the
# files cannot be listed, 2 on a usage error. The expected header lines are
# SPDX and COPYRIGHT below.

set -euo pipefail

readonly SCRIPT_NAME="${0##*/}"
readonly SPDX="SPDX-License-Identifier: GPL-3.0-only"
readonly COPYRIGHT="Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>"
readonly BASH_SHEBANG="#!/usr/bin/env bash"

usage()
{
	printf 'Usage: %s [-h]\n\n' "$SCRIPT_NAME"
	printf 'Check the SPDX and copyright header of every file in the repository.\n'
}

report()
{
	printf '%s: %s\n' "$1" "$2" >&2
}

is_exempt()
{
	case "$1" in
	LICENSE | DCO | go.sum | */go.sum | vendor/* | testdata/* | */testdata/* | *.golden)
		return 0
		;;
	esac
	return 1
}

# header_style FILE LINE1
#
# Print the comment style of FILE: go, markdown, shell, script, hash or
# unknown. LINE1 is the first line of the file.
header_style()
{
	case "${1##*/}" in
	*.go | go.mod | go.work)
		echo go
		;;
	*.md)
		echo markdown
		;;
	*.sh | *.bash)
		echo shell
		;;
	*.yml | *.yaml | *.toml | *.ini | *.cfg | *.conf | \
		Containerfile | Dockerfile | Makefile | .containerignore | .dockerignore | \
		.gitignore | .gitattributes | .gitlint | .editorconfig)
		echo hash
		;;
	*)
		if [[ "$2" == "#!"* ]]; then
			echo script
		else
			echo unknown
		fi
		;;
	esac
}

# check_file FILE
#
# Report the first header problem of FILE. Return: 0 when the header is correct.
check_file()
{
	local file="$1"
	local l1="" l2="" l3=""

	{
		IFS= read -r l1 || true
		IFS= read -r l2 || true
		IFS= read -r l3 || true
	} <"$file"

	case "$(header_style "$file" "$l1")" in
	go)
		if [[ "$l1" != "// ${SPDX}" || "$l2" != "// ${COPYRIGHT}" ]]; then
			report "$file" "lines 1-2 must be the '//' SPDX and copyright header"
			return 1
		fi
		if [[ -n "$l3" ]]; then
			report "$file" "line 3 must be empty"
			return 1
		fi
		;;
	markdown)
		if [[ "$l1" != "<!-- ${SPDX} -->" || "$l2" != "<!-- ${COPYRIGHT} -->" ]]; then
			report "$file" "lines 1-2 must be the '<!-- -->' SPDX and copyright header"
			return 1
		fi
		;;
	shell)
		if [[ "$l1" != "$BASH_SHEBANG" ]]; then
			report "$file" "line 1 must be '${BASH_SHEBANG}'"
			return 1
		fi
		if [[ "$l2" != "# ${SPDX}" || "$l3" != "# ${COPYRIGHT}" ]]; then
			report "$file" "lines 2-3 must be the '#' SPDX and copyright header"
			return 1
		fi
		;;
	script)
		if [[ "$l2" != "# ${SPDX}" || "$l3" != "# ${COPYRIGHT}" ]]; then
			report "$file" "lines 2-3 must be the '#' SPDX and copyright header"
			return 1
		fi
		;;
	hash)
		if [[ "$l1" != "# ${SPDX}" || "$l2" != "# ${COPYRIGHT}" ]]; then
			report "$file" "lines 1-2 must be the '#' SPDX and copyright header"
			return 1
		fi
		;;
	*)
		report "$file" "unknown file type; add a header rule to ${SCRIPT_NAME}"
		return 1
		;;
	esac
}

main()
{
	local top file
	local checked=0 failed=0

	case "${1:-}" in
	"") ;;
	-h | --help)
		usage
		return 0
		;;
	*)
		usage >&2
		return 2
		;;
	esac

	top="$(git rev-parse --show-toplevel)" || {
		printf '%s: not inside a git work tree\n' "$SCRIPT_NAME" >&2
		return 1
	}
	cd "$top"

	while IFS= read -r -d '' file; do
		# Skip symlinks, submodules and files deleted from the work tree.
		if [[ -L "$file" || ! -f "$file" ]] || is_exempt "$file"; then
			continue
		fi
		checked=$((checked + 1))
		check_file "$file" || failed=$((failed + 1))
	done < <(git ls-files -z --cached --others --exclude-standard --deduplicate)
	wait "$!" || {
		printf '%s: git ls-files failed\n' "$SCRIPT_NAME" >&2
		return 1
	}

	if ((failed > 0)); then
		printf '%s: %d of %d files have a bad header\n' \
			"$SCRIPT_NAME" "$failed" "$checked" >&2
		return 1
	fi
	printf '%s: %d files checked, all headers correct\n' "$SCRIPT_NAME" "$checked"
}

main "$@"
