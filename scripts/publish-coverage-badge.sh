#!/usr/bin/env bash
# SPDX-License-Identifier: GPL-3.0-only
# Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>
#
# publish-coverage-badge.sh - publish the coverage badge to the badges branch
#
# CI runs this after "scripts/build-in-container.sh coverage" on a push to
# main. It replaces the branch "badges" of the repository with a single
# commit that holds coverage.json, a copy of BADGE (the shields.io endpoint
# file that the coverage badge of the README reads), and a README that says
# that the branch is generated.
#
# Usage: scripts/publish-coverage-badge.sh [-h] [-r REMOTE] [-c COMMIT] BADGE
#
# Only the run of the current tip of main publishes: a run of an older commit
# that finishes late leaves the badge alone. The push uses --force-with-lease,
# so a concurrent update of the branch makes it fail instead of being lost.
#
# Git runs only in a new repository in a temporary directory and ignores the
# user and system configuration. It never runs in the checkout, so the
# .git/config of the checkout, which earlier steps of the job could have
# changed, is neither read nor written. For an https remote, GITHUB_TOKEN
# reaches git as an HTTP authorization header through GIT_CONFIG_*
# environment variables of the git commands that contact the remote; it
# never appears on a command line or in a file. Run with -h for the options
# and environment variables.

set -euo pipefail

readonly SCRIPT_NAME="${0##*/}"

# The branch that is replaced, and the branch whose tip it describes.
readonly BADGE_BRANCH="badges"
readonly SOURCE_BRANCH="main"

# The file name on the badge branch; the README badge reads it.
readonly BADGE_FILE="coverage.json"

# The exact format that "scripts/build-in-container.sh coverage" writes.
readonly BADGE_PATTERN='^\{"schemaVersion":1,"label":"coverage","message":"[0-9]{1,3}\.[0-9]%","color":"[a-z]+"\}$'

# The identity that GitHub shows for commits of GitHub Actions.
readonly BOT_NAME="github-actions[bot]"
readonly BOT_EMAIL="41898282+github-actions[bot]@users.noreply.github.com"

# Environment variables that would make git use another repository or
# configuration, or ask for credentials.
readonly GIT_UNSET=(
	GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE GIT_OBJECT_DIRECTORY
	GIT_ALTERNATE_OBJECT_DIRECTORIES GIT_COMMON_DIR GIT_NAMESPACE
	GIT_CEILING_DIRECTORIES GIT_TEMPLATE_DIR GIT_CONFIG GIT_CONFIG_PARAMETERS
	GIT_CONFIG_COUNT GIT_ASKPASS SSH_ASKPASS
)

# Set once by main.
REMOTE=""
AUTH_HEADER=""
REPO=""

# Temporary directory of this run, removed on exit.
WORK_DIR=""

usage()
{
	cat <<-EOF
		Usage: ${SCRIPT_NAME} [-h] [-r REMOTE] [-c COMMIT] BADGE

		Replace the branch "${BADGE_BRANCH}" of REMOTE with a single commit that
		holds BADGE as ${BADGE_FILE} and a README. BADGE is the badge.json that
		"scripts/build-in-container.sh coverage" writes. Nothing is published
		unless COMMIT is the tip of "${SOURCE_BRANCH}" on REMOTE.

		Options:
		  -r, --remote REMOTE  repository to publish to (default:
		                       \$GITHUB_SERVER_URL/\$GITHUB_REPOSITORY.git, where
		                       GITHUB_SERVER_URL defaults to https://github.com)
		  -c, --commit COMMIT  full ID of the commit that BADGE describes
		                       (default: \$GITHUB_SHA)
		  -h, --help           show this help

		Environment:
		  GITHUB_TOKEN       token with permission to push to REMOTE; required
		                     for https remotes and not used for others, such as
		                     a local path
		  GITHUB_REPOSITORY, GITHUB_SERVER_URL, GITHUB_SHA
		                     defaults of the options, set by GitHub Actions
		  GITHUB_RUN_ID      the workflow run, linked in the commit message and
		                     the README when set

		Needs git 2.32 or later.
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

# remote_git ARG...
#
# Run git with the authorization header for REMOTE, if there is one. Only
# this git process and its children see the header, in their environment.
remote_git()
{
	if [[ -z "$AUTH_HEADER" ]]; then
		git "$@"
		return
	fi
	GIT_CONFIG_COUNT=1 \
		GIT_CONFIG_KEY_0="http.${REMOTE}.extraheader" \
		GIT_CONFIG_VALUE_0="$AUTH_HEADER" \
		git "$@"
}

# remote_ref REFS NAME
#
# Print the object ID of the ref NAME in REFS, the output of git ls-remote,
# or nothing when REFS does not list it.
remote_ref()
{
	awk -v name="$2" '$2 == name { print $1 }' <<<"$1"
}

# readme COMMIT RUN-URL
#
# Print the README of the badge branch. RUN-URL may be empty.
readme()
{
	cat <<-EOF
		<!-- SPDX-License-Identifier: GPL-3.0-only -->
		<!-- Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com> -->

		# Generated branch

		The ci workflow (\`.github/workflows/ci.yml\`) generates this branch
		with \`scripts/publish-coverage-badge.sh\` and replaces it with a
		single new commit after every successful run on \`${SOURCE_BRANCH}\`.
		Do not commit to it: the next run discards the change.

		- \`${BADGE_FILE}\`: the test coverage of \`${SOURCE_BRANCH}\` for the
		  badge in the README, in the shields.io endpoint format.

		Source commit: \`${1}\`
	EOF
	if [[ -n "$2" ]]; then
		printf '\nWorkflow run: %s\n' "$2"
	fi
}

# publish BADGE COMMIT OLD
#
# Commit BADGE and the README in REPO and push the commit to BADGE_BRANCH of
# REMOTE, provided the branch is still at OLD (empty: does not exist).
publish()
{
	local badge=$1 commit=$2 old=$3
	local repo="$REPO"
	local pattern='"message":"([^"]*)"'
	local message body run_url=""

	[[ "$badge" =~ $pattern ]] || die "no message in the badge"
	message="${BASH_REMATCH[1]}"
	body="Test coverage ${message} of ${SOURCE_BRANCH} at ${commit}."
	if [[ -n "${GITHUB_RUN_ID:-}" && -n "${GITHUB_REPOSITORY:-}" ]]; then
		run_url="${GITHUB_SERVER_URL:-https://github.com}/${GITHUB_REPOSITORY}"
		run_url+="/actions/runs/${GITHUB_RUN_ID}"
		body+=$'\n'"Workflow run: ${run_url}"
	fi

	printf '%s\n' "$badge" >"${repo}/${BADGE_FILE}"
	readme "$commit" "$run_url" >"${repo}/README.md"
	git -C "$repo" add -- "$BADGE_FILE" README.md
	GIT_AUTHOR_NAME="$BOT_NAME" GIT_AUTHOR_EMAIL="$BOT_EMAIL" \
		GIT_COMMITTER_NAME="$BOT_NAME" GIT_COMMITTER_EMAIL="$BOT_EMAIL" \
		git -C "$repo" commit --quiet --no-verify --no-gpg-sign \
		--message="ci: publish the coverage badge" --message="$body" \
		--message="This branch is generated and replaced on every run."

	remote_git -C "$repo" push --quiet --no-verify \
		--force-with-lease="refs/heads/${BADGE_BRANCH}:${old}" \
		"$REMOTE" "HEAD:refs/heads/${BADGE_BRANCH}" ||
		die "cannot push to ${BADGE_BRANCH} of ${REMOTE}"
	log "published coverage ${message} of ${commit} to ${BADGE_BRANCH}" \
		"($(git -C "$repo" rev-parse HEAD))"
}

main()
{
	local remote="" commit="${GITHUB_SHA:-}"
	local badge_path badge refs tip old name

	while (($# > 0)); do
		case "$1" in
		-h | --help)
			usage
			return 0
			;;
		-r | --remote | -c | --commit)
			(($# > 1)) || die_usage "option $1 needs a value"
			case "$1" in
			-r | --remote) remote=$2 ;;
			*) commit=$2 ;;
			esac
			shift
			;;
		--)
			shift
			break
			;;
		-*)
			die_usage "unknown option: $1"
			;;
		*)
			break
			;;
		esac
		shift
	done
	(($# == 1)) || die_usage "expected one BADGE file"
	badge_path=$1

	if [[ -z "$remote" ]]; then
		[[ -n "${GITHUB_REPOSITORY:-}" ]] ||
			die_usage "no remote: use --remote or set GITHUB_REPOSITORY"
		remote="${GITHUB_SERVER_URL:-https://github.com}/${GITHUB_REPOSITORY}.git"
	fi
	[[ -n "$remote" && "$remote" != -* ]] || die_usage "invalid remote: ${remote}"
	# Git runs in another directory; a relative path must still work.
	if [[ "$remote" != /* && -d "$remote" ]]; then
		remote="${PWD}/${remote}"
	fi
	[[ "$commit" =~ ^([0-9a-f]{40}|[0-9a-f]{64})$ ]] ||
		die_usage "COMMIT must be a full commit ID (--commit or GITHUB_SHA): ${commit}"
	[[ -f "$badge_path" ]] || die "badge file not found: ${badge_path}"
	badge="$(<"$badge_path")"
	[[ "$badge" =~ $BADGE_PATTERN ]] || die "${badge_path} is not a coverage badge file"

	REMOTE="$remote"
	if [[ "$REMOTE" == https://* ]]; then
		[[ -n "${GITHUB_TOKEN:-}" ]] || die "GITHUB_TOKEN is required for ${REMOTE}"
		AUTH_HEADER="AUTHORIZATION: basic $(printf 'x-access-token:%s' "$GITHUB_TOKEN" |
			base64 --wrap=0)"
	fi
	readonly REMOTE AUTH_HEADER

	# Only the remote and the new commit matter: no user or system
	# configuration (hooks, signing, URL rewriting, proxies, credential
	# helpers), and no prompts.
	for name in "${GIT_UNSET[@]}"; do
		unset "$name"
	done
	export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1 GIT_TERMINAL_PROMPT=0

	WORK_DIR="$(mktemp -d)" || die "cannot create a temporary directory"
	trap cleanup EXIT

	# The same holds for the configuration of the checkout: every git
	# command runs outside it, in the new repository REPO, and "git -C REPO"
	# reads the configuration of REPO only.
	cd -- "$WORK_DIR" || die "cannot change to ${WORK_DIR}"
	REPO="${WORK_DIR}/repo"
	readonly REPO
	git init --quiet --initial-branch="$BADGE_BRANCH" "$REPO"

	refs="$(remote_git -C "$REPO" ls-remote "$REMOTE" \
		"refs/heads/${SOURCE_BRANCH}" "refs/heads/${BADGE_BRANCH}")" ||
		die "cannot list the branches of ${REMOTE}"
	tip="$(remote_ref "$refs" "refs/heads/${SOURCE_BRANCH}")"
	old="$(remote_ref "$refs" "refs/heads/${BADGE_BRANCH}")"
	[[ -n "$tip" ]] || die "${REMOTE} has no branch ${SOURCE_BRANCH}"
	if [[ "$tip" != "$commit" ]]; then
		log "${SOURCE_BRANCH} is at ${tip} now, not at ${commit}; the run of" \
			"the newer commit publishes the badge"
		return 0
	fi
	publish "$badge" "$commit" "$old"
}

main "$@"
