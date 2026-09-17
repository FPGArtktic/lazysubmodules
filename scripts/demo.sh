#!/usr/bin/env bash
# SPDX-License-Identifier: GPL-3.0-only
# Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>
#
# demo.sh - walk through LazySubmodules on a complex superproject, offline
#
# The script builds a firmware superproject with fourteen submodules that
# cover every tracking mode and every state, then tells a story with
# LazySubmodules commands. Each command is shown before its output, and each
# lazysubmodules command is followed by its exit status. The script fails
# when a command exits with another status than the story expects, so it
# doubles as an end-to-end test.
#
# Everything happens inside the demo directory. Git runs without the user and
# system configuration and may use the file transport only. Every submodule
# URL names the host git.example.invalid, which does not exist; the demo
# configuration rewrites it with url.<base>.insteadOf to bare repositories in
# the directory "mirror", as a company mirror would. Identities and dates are
# fixed, so commit IDs and output are the same in every run.
#
# Usage: scripts/demo.sh [-h] [--keep DIR] [--binary PATH] [--no-pause]
#                        [--transcript FILE] [--setup-only]
#
#   --keep DIR         build the demo in DIR, which must be new or empty, and
#                      keep it; ". DIR/env.sh" prepares a shell for more
#                      commands in DIR/firmware, such as lazysubmodules tui
#   --binary PATH      the lazysubmodules binary; the default is
#                      bin/lazysubmodules of this repository if it exists,
#                      else lazysubmodules from PATH
#   --no-pause         do not wait for Enter between the chapters; there is
#                      no pause either when standard input is not a terminal
#   --transcript FILE  also write the narration to FILE, with the demo
#                      directory shown as $DEMO; commands then write to a
#                      pipe, so their output has no colors
#   --setup-only       build and describe the superproject, then stop; needs
#                      --keep, and leaves every submodule in its first state
#
# Requires bash, git 2.39 or later and coreutils. Tag v1.0.0 of the
# submodule "signed" is signed with an SSH key made by ssh-keygen; without
# ssh-keygen the tag is not signed, and the signature check says so.
#
# Exit status: 0 when every command behaved as the story expects, 1 when one
# did not or the setup failed, 2 on a usage error.

set -euo pipefail

readonly SCRIPT_NAME="${0##*/}"
REPO_ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
readonly REPO_ROOT

# Every submodule URL starts with FAKE_HOST; the host does not exist.
readonly FAKE_HOST="https://git.example.invalid/"

# 2026-01-01T00:00:00Z. Each commit, tag and shown command is one hour
# later than the one before.
readonly START_EPOCH=1767225600

readonly UPSTREAM_NAME="Rita Upstream"
readonly UPSTREAM_EMAIL="rita@example.org"
readonly DEVELOPER_NAME="Dana Developer"
readonly DEVELOPER_EMAIL="dana@example.org"

# The file that every upstream commit appends a line to.
readonly NEWS="NEWS"

# How the demo directory appears in the transcript.
readonly DEMO_LABEL="\$DEMO"

# Width of the chapter rules and the prompt of superproject commands.
readonly WIDTH=72
readonly PROMPT='$ '

# SGR parameters of the narration on a terminal.
readonly SGR_TITLE="1;36"
readonly SGR_COMMAND="1"
readonly SGR_OK="32"
readonly SGR_FAILED="33"
readonly SGR_NOTE="2"

readonly RULE_CHAR="━"

# Command line settings.
KEEP_DIR=""
BINARY=""
PAUSE=1
TRANSCRIPT=""
SETUP_ONLY=0

# The demo directory, the superproject in it, the setup log, the file that
# captures command output and the SSH signing key (empty without
# ssh-keygen). DEMO is removed on exit unless --keep was given.
DEMO=""
SUPER=""
SETUP_LOG=""
CAPTURE=""
SIGNING_KEY=""
REMOVE_DEMO=0

# Commits, tags and commands so far; tick derives dates from it.
CLOCK=0

# DIRECT: commands write to the terminal themselves, with colors. COLOR: the
# narration uses colors. CHAPTER: number of the current chapter. SHOWN_PROMPT:
# prompt of the next shown command.
DIRECT=0
COLOR=0
CHAPTER=0
SHOWN_PROMPT="$PROMPT"

usage()
{
	cat <<-EOF
		Usage: ${SCRIPT_NAME} [-h] [--keep DIR] [--binary PATH] [--no-pause]
		       ${SCRIPT_NAME//?/ } [--transcript FILE] [--setup-only]

		Build a superproject with fourteen submodules from local repositories
		and walk through LazySubmodules on it. Nothing uses the network, and
		nothing outside the demo directory is changed.

		Options:
		  --keep DIR         build in DIR (new or empty) and keep it; ". DIR/env.sh"
		                     prepares a shell for more commands in DIR/firmware
		  --binary PATH      lazysubmodules binary (default: bin/lazysubmodules
		                     of the repository if it exists, else from PATH)
		  --no-pause         do not wait for Enter between the chapters
		  --transcript FILE  also write the narration to FILE, with the demo
		                     directory shown as \$DEMO
		  --setup-only       build and describe the superproject, then stop
		                     (needs --keep)
		  -h, --help         show this help

		Exit status: 0 when every command behaved as expected, 1 otherwise,
		2 on a usage error.
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

# option_value OPTION ARGC VALUE
#
# Print VALUE, the value of OPTION; ARGC counts OPTION and the arguments
# after it. Fail with a usage error when the value is missing or looks like
# an option.
option_value()
{
	if (($2 < 2)); then
		die_usage "option $1 needs a value"
	fi
	if [[ -z "$3" || "$3" == -* ]]; then
		die_usage "invalid value for $1: '$3'"
	fi
	printf '%s\n' "$3"
}

parse_args()
{
	while (($# > 0)); do
		case "$1" in
		-h | --help)
			usage
			exit 0
			;;
		--keep)
			KEEP_DIR="$(option_value "$1" "$#" "${2:-}")"
			shift
			;;
		--binary)
			BINARY="$(option_value "$1" "$#" "${2:-}")"
			shift
			;;
		--transcript)
			TRANSCRIPT="$(option_value "$1" "$#" "${2:-}")"
			shift
			;;
		--no-pause)
			PAUSE=0
			;;
		--setup-only)
			SETUP_ONLY=1
			;;
		*)
			die_usage "unknown argument: $1"
			;;
		esac
		shift
	done
	if ((SETUP_ONLY)) && [[ -z "$KEEP_DIR" ]]; then
		die_usage "--setup-only needs --keep"
	fi
	if [[ ! -t 0 ]]; then
		PAUSE=0
	fi
}

# absolute PATH
#
# Print PATH as an absolute path without symbolic links in its directory;
# the directory must exist.
absolute()
{
	local dir

	dir="$(cd -- "$(dirname -- "$1")" 2>/dev/null && pwd -P)" ||
		die "no such directory: $(dirname -- "$1")"
	printf '%s/%s\n' "$dir" "$(basename -- "$1")"
}

find_binary()
{
	if [[ -z "$BINARY" ]]; then
		if [[ -x "${REPO_ROOT}/bin/lazysubmodules" ]]; then
			BINARY="${REPO_ROOT}/bin/lazysubmodules"
		elif ! BINARY="$(command -v lazysubmodules)"; then
			die "lazysubmodules not found; build it with" \
				"'scripts/build-in-container.sh build' or pass --binary"
		fi
	fi
	if [[ ! -f "$BINARY" || ! -x "$BINARY" ]]; then
		die "not an executable file: $BINARY"
	fi
	BINARY="$(absolute "$BINARY")"
	readonly BINARY
}

# open_transcript
#
# Create the transcript file with its header.
open_transcript()
{
	if [[ -z "$TRANSCRIPT" ]]; then
		return 0
	fi
	TRANSCRIPT="$(absolute "$TRANSCRIPT")"
	readonly TRANSCRIPT
	cat >"$TRANSCRIPT" <<-EOF || die "cannot write $TRANSCRIPT"
		# SPDX-License-Identifier: GPL-3.0-only
		# Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>
		#
		# Transcript of scripts/demo.sh, which writes it with
		#   scripts/demo.sh --no-pause --transcript FILE
		# ${DEMO_LABEL} stands for the demo directory. Only lazysubmodules commands
		# are followed by their exit status.

	EOF
}

# make_demo_dir
#
# Create the demo directory: KEEP_DIR, or a new temporary directory that is
# removed on exit.
make_demo_dir()
{
	if [[ -n "$KEEP_DIR" ]]; then
		mkdir -p -- "$KEEP_DIR" || die "cannot create $KEEP_DIR"
		if [[ -n "$(ls -A -- "$KEEP_DIR")" ]]; then
			die "$KEEP_DIR is not empty"
		fi
		DEMO="$(cd -- "$KEEP_DIR" && pwd -P)"
	else
		DEMO="$(mktemp -d "${TMPDIR:-/tmp}/lsm-demo.XXXXXX")" || die "mktemp failed"
		REMOVE_DEMO=1
		DEMO="$(cd -- "$DEMO" && pwd -P)"
	fi
	readonly DEMO
	SUPER="${DEMO}/firmware"
	SETUP_LOG="${DEMO}/setup.log"
	CAPTURE="${DEMO}/tmp/output"
	readonly SUPER SETUP_LOG CAPTURE
	mkdir -p -- "${DEMO}/bin" "${DEMO}/home" "${DEMO}/mirror" "${DEMO}/tmp" "${DEMO}/work"
	ln -s -- "$BINARY" "${DEMO}/bin/lazysubmodules"
	: >"$SETUP_LOG"
}

cleanup()
{
	if ((REMOVE_DEMO)) && [[ -n "$DEMO" ]]; then
		chmod -R u+w -- "$DEMO" 2>/dev/null || true
		rm -rf -- "$DEMO"
	fi
}

# config_env KEY=VALUE...
#
# Print "export" lines that pass the entries to git with GIT_CONFIG_COUNT.
config_env()
{
	local i=0 entry

	printf 'export GIT_CONFIG_COUNT=%d\n' "$#"
	for entry in "$@"; do
		printf 'export GIT_CONFIG_KEY_%d=%q GIT_CONFIG_VALUE_%d=%q\n' \
			"$i" "${entry%%=*}" "$i" "${entry#*=}"
		i=$((i + 1))
	done
}

# write_env FILE
#
# Write the environment of the demo to FILE, a script for any POSIX shell to
# source. The demo sources it, and so can a shell that explores a kept demo.
write_env()
{
	local -a config=(
		"protocol.file.allow=always"
		"url.file://${DEMO}/mirror/.insteadOf=${FAKE_HOST}"
		"init.defaultBranch=main"
		"advice.detachedHead=false"
		"gc.auto=0"
		"maintenance.auto=false"
		"color.ui=auto"
	)

	if [[ -n "$SIGNING_KEY" ]]; then
		config+=(
			"gpg.format=ssh"
			"gpg.ssh.allowedSignersFile=${SIGNING_KEY}.allowed_signers"
		)
	fi
	{
		printf '# Environment of the LazySubmodules demo in %q.\n' "$DEMO"
		printf '# Use it in a shell with: . %q\n' "$1"
		# The unset loop drops git variables of the calling environment,
		# such as GIT_DIR; it is expanded when the file is sourced.
		# shellcheck disable=SC2016
		printf '%s\n' \
			'for v in $(env | sed -n "s/^\(GIT_[A-Za-z0-9_]*\)=.*/\1/p"); do' \
			'	unset "$v"' \
			'done' \
			'unset v'
		printf 'export PATH=%q:"%s"\n' "${DEMO}/bin" "\$PATH"
		printf 'export HOME=%q XDG_CONFIG_HOME=%q TMPDIR=%q\n' \
			"${DEMO}/home" "${DEMO}/home/.config" "${DEMO}/tmp"
		printf 'export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1 GIT_ATTR_NOSYSTEM=1\n'
		printf 'export GIT_ALLOW_PROTOCOL=file GIT_TERMINAL_PROMPT=0 GIT_PAGER=cat\n'
		printf 'export GIT_AUTHOR_NAME=%q GIT_AUTHOR_EMAIL=%q\n' \
			"$DEVELOPER_NAME" "$DEVELOPER_EMAIL"
		printf 'export GIT_COMMITTER_NAME=%q GIT_COMMITTER_EMAIL=%q\n' \
			"$DEVELOPER_NAME" "$DEVELOPER_EMAIL"
		printf 'export SSH_AUTH_SOCK= TZ=UTC\n'
		config_env "${config[@]}"
	} >"$1"
}

# make_signing_key
#
# Create the SSH key that signs tag v1.0.0 of "signed", and the allowed
# signers file that trusts it. Without ssh-keygen, SIGNING_KEY stays empty.
make_signing_key()
{
	local key="${DEMO}/home/signing_key"
	local -a pub

	if ! command -v ssh-keygen >/dev/null; then
		return 0
	fi
	ssh-keygen -q -t ed25519 -N "" -C "$UPSTREAM_EMAIL" -f "$key" \
		>>"$SETUP_LOG" 2>&1 </dev/null || die "ssh-keygen failed; see $SETUP_LOG"
	read -r -a pub <"${key}.pub"
	printf '%s namespaces="git" %s %s\n' "$UPSTREAM_EMAIL" "${pub[0]}" "${pub[1]}" \
		>"${key}.allowed_signers"
	SIGNING_KEY="$key"
}

setup_env()
{
	export LC_ALL=C
	make_signing_key
	write_env "${DEMO}/env.sh"
	# shellcheck source=/dev/null
	. "${DEMO}/env.sh"
}

# g ARG...
#
# Run git for the setup; its output goes to the setup log.
g()
{
	if ! git "$@" >>"$SETUP_LOG" 2>&1 </dev/null; then
		tail -n 20 -- "$SETUP_LOG" >&2
		die "git $* failed"
	fi
}

# tick
#
# Give the next commit or tag a date of its own.
tick()
{
	CLOCK=$((CLOCK + 1))
	export GIT_AUTHOR_DATE="@$((START_EPOCH + CLOCK * 3600)) +0000"
	export GIT_COMMITTER_DATE="$GIT_AUTHOR_DATE"
}

# as NAME EMAIL
#
# Make NAME <EMAIL> the author and committer of what follows.
as()
{
	export GIT_AUTHOR_NAME="$1" GIT_COMMITTER_NAME="$1"
	export GIT_AUTHOR_EMAIL="$2" GIT_COMMITTER_EMAIL="$2"
}

# new_upstream REPO
#
# Create the bare repository mirror/REPO.git and the work repository
# work/REPO, where its history is written, with an initial commit. The work
# repository pushes to the fake URL, which the configuration rewrites.
new_upstream()
{
	local bare="${DEMO}/mirror/$1.git" work="${DEMO}/work/$1"

	g init --quiet --bare "$bare"
	# A push to a local repository does not pass the configuration of the
	# environment on; keep automatic maintenance from running there.
	g -C "$bare" config receive.autogc false
	g -C "$bare" config gc.auto 0
	g -C "$bare" config maintenance.auto false
	g init --quiet "$work"
	g -C "$work" remote add origin "${FAKE_HOST}$1.git"
	release "$1" "initial commit"
}

# release REPO MESSAGE [TAG [lightweight|annotated|signed]]
#
# Add a line to NEWS in work/REPO, commit it and optionally tag the commit.
# A signed tag is an unsigned annotated tag when there is no signing key.
release()
{
	local work="${DEMO}/work/$1" msg="$2" tag="${3:-}" kind="${4:-lightweight}"

	tick
	printf '%s\n' "$msg" >>"${work}/${NEWS}"
	g -C "$work" add -- "$NEWS"
	g -C "$work" commit --quiet --message="$msg"
	case "${tag:+tag}:${kind}:${SIGNING_KEY}" in
	:*) ;;
	tag:lightweight:*)
		g -C "$work" tag "$tag"
		;;
	tag:annotated:* | tag:signed:)
		g -C "$work" tag --annotate --message="release $tag" "$tag"
		;;
	tag:signed:*)
		g -C "$work" -c user.signingKey="$SIGNING_KEY" tag --sign \
			--message="release $tag" "$tag"
		;;
	*)
		die "unknown tag kind: $kind"
		;;
	esac
}

# publish REPO
#
# Push every branch and tag of work/REPO to its bare repository.
publish()
{
	g -C "${DEMO}/work/$1" push --quiet --force origin \
		"refs/heads/*:refs/heads/*" "refs/tags/*:refs/tags/*"
}

# rev REPO REVISION
#
# Print the full ID of the commit that REVISION names in work/REPO.
rev()
{
	git -C "${DEMO}/work/$1" rev-parse --verify --quiet "$2^{commit}" ||
		die "$1: no revision $2"
}

make_upstreams()
{
	local i kind

	as "$UPSTREAM_NAME" "$UPSTREAM_EMAIL"

	# kernel: v6.6.1 to v6.6.10, the odd ones annotated, and two
	# pre-releases.
	new_upstream linux
	for i in 1 2 3 4 5 6 7 8 9 10; do
		kind=lightweight
		if ((i % 2)); then
			kind=annotated
		fi
		release linux "Linux v6.6.$i" "v6.6.$i" "$kind"
	done
	release linux "Linux v6.6.11-rc1" v6.6.11-rc1 annotated
	g -C "${DEMO}/work/linux" branch linux-6.6.y
	release linux "Linux v6.7-rc1" v6.7-rc1
	publish linux

	# u-boot: main has moved on since the superproject locked it.
	new_upstream u-boot
	release u-boot "U-Boot v2025.10"
	release u-boot "U-Boot v2026.01"
	release u-boot "board: add a new board"
	g -C "${DEMO}/work/u-boot" branch next
	publish u-boot

	# fpga.core: annotated tag v2.3.1, which the story moves.
	new_upstream fpga-core
	release fpga-core "fpga-core v2.3.0" v2.3.0
	release fpga-core "fpga-core v2.3.1" v2.3.1 annotated
	release fpga-core "timing: fix setup violation"
	publish fpga-core

	# crypto lib: pinned to the commit that added the sources.
	new_upstream crypto-lib
	mkdir -p -- "${DEMO}/work/crypto-lib/src"
	printf 'int crypto_init(void);\n' >"${DEMO}/work/crypto-lib/src/crypto.c"
	g -C "${DEMO}/work/crypto-lib" add -- src/crypto.c
	release crypto-lib "crypto: add sources"
	release crypto-lib "crypto: speed up"
	publish crypto-lib

	new_upstream legacy
	release legacy "legacy 1.0"
	publish legacy

	make_tools

	new_upstream theme
	release theme "theme v1.0.0" v1.0.0
	release theme "theme: dark mode"
	publish theme

	new_upstream fresh
	release fresh "fresh v1.0.0" v1.0.0
	release fresh "fresh v1.1.0" v1.1.0 annotated
	release fresh "fresh v2.0.0" v2.0.0
	publish fresh

	new_upstream app
	release app "app v1.1.0" v1.1.0
	release app "app v1.2.0" v1.2.0 annotated
	release app "app v1.3.0" v1.3.0
	publish app

	# sdk: the superproject opted in to the release candidate.
	new_upstream sdk
	release sdk "sdk v2.9.0" v2.9.0 annotated
	release sdk "sdk v3.0.0-rc.1" v3.0.0-rc.1
	release sdk "sdk v3.0.0-rc.2" v3.0.0-rc.2 annotated
	publish sdk

	new_upstream mirror-lib
	release mirror-lib "mirror-lib 1.0"
	g -C "${DEMO}/work/mirror-lib" branch stable
	release mirror-lib "mirror-lib 1.1"
	publish mirror-lib

	new_upstream broken
	release broken "broken v1.0.0" v1.0.0
	publish broken

	new_upstream signed
	release signed "signed v1.0.0" v1.0.0 signed
	release signed "signed v1.0.1" v1.0.1 annotated
	publish signed

	new_upstream quirky
	release quirky "quirky v1.0.0" v1.0.0
	publish quirky
}

# make_tools
#
# tools records the first commit of its own submodule "inner"; its branch
# develop is one commit behind main.
make_tools()
{
	new_upstream tools-inner
	release tools-inner "inner 1"
	release tools-inner "inner 2"
	publish tools-inner

	new_upstream tools
	g -C "${DEMO}/work/tools" submodule add --quiet --name inner -- \
		"${FAKE_HOST}tools-inner.git" inner
	g -C "${DEMO}/work/tools/inner" checkout --quiet --detach "$(rev tools-inner main~1)"
	g -C "${DEMO}/work/tools" add -- inner
	release tools "tools: add inner"
	g -C "${DEMO}/work/tools" branch develop
	release tools "tools: next"
	publish tools
}

# commit MESSAGE [OPTION...]
#
# Commit the index of the superproject.
commit()
{
	tick
	g -C "$SUPER" commit --quiet --message="$1" "${@:2}"
}

# declare_submodule NAME PATH REPO REVISION [KEY=VALUE...]
#
# Record a submodule in .gitmodules and its gitlink in the index, as a
# colleague's "git submodule add" would, without cloning it. The keys follow
# path and url.
declare_submodule()
{
	local name="$1" path="$2" repo="$3" commit entry

	commit="$(rev "$repo" "$4")"
	shift 4
	g -C "$SUPER" config -f .gitmodules "submodule.${name}.path" "$path"
	g -C "$SUPER" config -f .gitmodules "submodule.${name}.url" "${FAKE_HOST}${repo}.git"
	for entry in "$@"; do
		g -C "$SUPER" config -f .gitmodules "submodule.${name}.${entry%%=*}" "${entry#*=}"
	done
	g -C "$SUPER" update-index --add --cacheinfo "160000,${commit},${path}"
	mkdir -p -- "${SUPER}/${path}"
}

# track NAME MODE REF
#
# Write the LazySubmodules keys of a submodule to .gitmodules, as
# "lazysubmodules set" does.
track()
{
	g -C "$SUPER" config -f .gitmodules "submodule.$1.lsm-mode" "$2"
	g -C "$SUPER" config -f .gitmodules "submodule.$1.lsm-ref" "$3"
}

# lock NAME MODE REF REPO REVISION
#
# Write the lock entry of a submodule, as "lazysubmodules update" does.
lock()
{
	g -C "$SUPER" config -f .lsm.lock "submodule.$1.mode" "$2"
	g -C "$SUPER" config -f .lsm.lock "submodule.$1.ref" "$3"
	g -C "$SUPER" config -f .lsm.lock "submodule.$1.commit" "$(rev "$4" "$5")"
}

make_superproject()
{
	local crypto

	crypto="$(rev crypto-lib main~1)"
	as "$DEVELOPER_NAME" "$DEVELOPER_EMAIL"
	g init --quiet "$SUPER"
	mkdir -p -- "${SUPER}/src/drivers"
	printf 'Firmware for the example board.\n' >"${SUPER}/README"
	printf 'Board drivers.\n' >"${SUPER}/src/drivers/README"
	printf '# build products\n*.lock\n' >"${SUPER}/.gitignore"
	g -C "$SUPER" add --all
	commit "initial commit"

	declare_submodule kernel kernel linux v6.6.8
	declare_submodule u-boot bootloader/u-boot u-boot main~1 branch=main
	declare_submodule fpga.core ip/fpga-core fpga-core v2.3.1
	declare_submodule "crypto lib" "libs/crypto lib" crypto-lib main~1
	declare_submodule legacy vendor/legacy legacy main
	declare_submodule tools tools/nested tools develop branch=develop
	g -C "$SUPER" add -- .gitmodules
	commit "add kernel, u-boot, fpga.core, crypto lib, legacy and tools"

	declare_submodule theme docs/theme theme v1.0.0
	declare_submodule fresh third_party/fresh fresh v1.1.0
	declare_submodule app apps/app app v1.2.0
	declare_submodule sdk sdk sdk v3.0.0-rc.2
	declare_submodule mirror-lib libs/mirror mirror-lib stable branch=stable
	declare_submodule broken libs/broken broken v1.0.0
	declare_submodule signed libs/signed signed v1.0.0
	declare_submodule quirky libs/quirky quirky v1.0.0 \
		update=none ignore=all shallow=true
	g -C "$SUPER" add -- .gitmodules
	commit "add theme, fresh, app, sdk, mirror-lib, broken, signed and quirky"

	track kernel tag-pattern "v6.6.*"
	track u-boot branch main
	track fpga.core tag v2.3.1
	track "crypto lib" commit "$crypto"
	track tools branch develop
	track theme tag v1.0.0
	track fresh tag-pattern "v1.*"
	track app tag v1.2.0
	track sdk tag-pattern "v*"
	track mirror-lib branch stable
	# A typing error: the tag is v1.0.0.
	track broken tag v9.9.9
	track signed tag v1.0.0
	track quirky tag v1.0.0

	lock kernel tag-pattern v6.6.8 linux v6.6.8
	lock u-boot branch main u-boot main~1
	lock fpga.core tag v2.3.1 fpga-core v2.3.1
	lock "crypto lib" commit "$crypto" crypto-lib "$crypto"
	lock tools branch develop tools develop
	lock theme tag v1.0.0 theme v1.0.0
	lock fresh tag-pattern v1.1.0 fresh v1.1.0
	lock app tag v1.2.0 app v1.2.0
	lock sdk tag-pattern v3.0.0-rc.2 sdk v3.0.0-rc.2
	lock mirror-lib branch stable mirror-lib stable
	lock broken tag v1.0.0 broken v1.0.0
	lock signed tag v1.0.0 signed v1.0.0
	lock quirky tag v1.0.0 quirky v1.0.0
	# .gitignore matches *.lock, hence --force.
	g -C "$SUPER" add --force -- .gitmodules .lsm.lock
	commit "track submodules with lazysubmodules"

	lock kernel tag-pattern v6.6.9 linux v6.6.9
	g -C "$SUPER" update-index --cacheinfo "160000,$(rev linux v6.6.9),kernel"
	g -C "$SUPER" add --force -- .lsm.lock
	commit "manifest: update kernel to v6.6.9

Tracking mode: tag-pattern v6.6.*
Old: $(rev linux v6.6.8 | cut -c 1-12) (v6.6.8)
New: $(rev linux v6.6.9 | cut -c 1-12) (v6.6.9)" --signoff
}

# populate
#
# Bring the submodules into their states: clone and check out all but
# fresh, which this clone of the superproject never initialized; check out a
# newer commit in the nested submodule of tools; deinitialize theme, which
# keeps its repository; and change a file in app.
populate()
{
	# --checkout overrides "update = none" of quirky, which is cloned with
	# depth 1 because of "shallow = true".
	g -C "$SUPER" submodule update --init --checkout --jobs 4 -- \
		kernel bootloader/u-boot ip/fpga-core "libs/crypto lib" vendor/legacy \
		tools/nested docs/theme apps/app sdk libs/mirror libs/broken libs/signed \
		libs/quirky
	g -C "${SUPER}/tools/nested" submodule update --init
	g -C "${SUPER}/tools/nested/inner" checkout --quiet --detach origin/main
	g -C "$SUPER" submodule deinit --force -- docs/theme
	printf 'work in progress\n' >>"${SUPER}/apps/app/${NEWS}"
}

# setup_output
#
# Decide how commands and the narration write to standard output.
setup_output()
{
	if [[ -t 1 && -z "${NO_COLOR:-}" && "${TERM:-dumb}" != dumb ]]; then
		COLOR=1
	fi
	if [[ -t 1 && -z "$TRANSCRIPT" ]]; then
		DIRECT=1
	fi
}

# emit SGR TEXT
#
# Print a line of narration, in the style SGR on a terminal, and add it to
# the transcript. Standard output shows the demo directory as it is, so
# that the commands printed for a kept demo work; only the transcript
# shows it as $DEMO.
emit()
{
	local text="$2"

	if ((COLOR)) && [[ -n "$1" ]]; then
		printf '\033[%sm%s\033[0m\n' "$1" "$text"
	else
		printf '%s\n' "$text"
	fi
	if [[ -n "$TRANSCRIPT" ]]; then
		printf '%s\n' "${text//"$DEMO"/"$DEMO_LABEL"}" >>"$TRANSCRIPT"
	fi
}

# say LINE...
#
# Print narration lines followed by an empty line.
say()
{
	local line

	for line in "$@"; do
		emit "" "$line"
	done
	emit "" ""
}

# note LINE...
#
# Print remarks that are not part of the story, dimmed on a terminal.
note()
{
	local line

	for line in "$@"; do
		emit "$SGR_NOTE" "$line"
	done
	emit "" ""
}

# rule TEXT
#
# Print TEXT followed by a heavy rule, WIDTH columns wide in total.
rule()
{
	local fill

	printf -v fill '%*s' $((WIDTH - ${#1} - 1)) ""
	emit "$SGR_TITLE" "$1 ${fill// /$RULE_CHAR}"
}

# chapter TITLE
#
# Start the next chapter, after a pause when pauses are enabled.
chapter()
{
	CHAPTER=$((CHAPTER + 1))
	if ((PAUSE)); then
		read -r -s -p "Press Enter for chapter ${CHAPTER}..." </dev/tty || true
		printf '\r\033[K' >&2
	fi
	emit "" ""
	rule "${RULE_CHAR}${RULE_CHAR} ${CHAPTER}. $1"
	emit "" ""
}

# quote_words WORD...
#
# Print the words as a shell command line, quoting where needed.
quote_words()
{
	local word line="" plain='^[-A-Za-z0-9_./:=@%+,]+$' escaped="'\\''"

	for word in "$@"; do
		if [[ ! "$word" =~ $plain ]]; then
			word="'${word//\'/$escaped}'"
		fi
		line+="${line:+ }${word}"
	done
	printf '%s\n' "$line"
}

# exit_meaning STATUS
#
# Print what an exit status of lazysubmodules means.
exit_meaning()
{
	case "$1" in
	0) echo "success" ;;
	1) echo "error" ;;
	2) echo "usage error" ;;
	3) echo "refused" ;;
	4) echo "verification failed" ;;
	5) echo "git failed" ;;
	*) echo "unexpected" ;;
	esac
}

# run_shown EXPECTED LSM COMMAND...
#
# Run a command that was shown, print its output and, when LSM is 1, its
# exit status. Fail when the exit status is not EXPECTED.
run_shown()
{
	local want="$1" lsm="$2" rc=0 blanks=0 line sgr="$SGR_OK"

	shift 2
	tick
	if ((DIRECT)); then
		"$@" </dev/null || rc=$?
	else
		"$@" >"$CAPTURE" 2>&1 </dev/null || rc=$?
		# Trailing white space, such as that of the empty message lines
		# of "git log", and empty lines at the end, such as the one after
		# "git log --format=%B", are left out.
		while IFS= read -r line || [[ -n "$line" ]]; do
			line="${line%"${line##*[![:blank:]]}"}"
			if [[ -z "$line" ]]; then
				blanks=$((blanks + 1))
				continue
			fi
			for (( ; blanks > 0; blanks--)); do
				emit "" ""
			done
			emit "" "$line"
		done <"$CAPTURE"
	fi
	if ((lsm)); then
		if ((rc != 0)); then
			sgr="$SGR_FAILED"
		fi
		emit "$sgr" "[exit status ${rc}: $(exit_meaning "$rc")]"
	fi
	emit "" ""
	if ((rc != want)); then
		die "chapter ${CHAPTER}: exit status ${rc} instead of ${want}"
	fi
}

# show [-e EXPECTED] COMMAND...
#
# Show a command line, run it and show its output. The exit status must be
# EXPECTED, 0 by default.
show()
{
	local want=0 lsm=0

	if [[ "$1" == -e ]]; then
		want="$2"
		shift 2
	fi
	if [[ "$1" == lazysubmodules ]]; then
		lsm=1
	fi
	emit "$SGR_COMMAND" "${SHOWN_PROMPT}$(quote_words "$@")"
	run_shown "$want" "$lsm" "$@"
}

# show_sh [-e EXPECTED] SCRIPT
#
# Show a shell command line and run it with bash.
show_sh()
{
	local want=0

	if [[ "$1" == -e ]]; then
		want="$2"
		shift 2
	fi
	emit "$SGR_COMMAND" "${PROMPT}$1"
	run_shown "$want" 0 bash -o pipefail -c "$1"
}

# upstream REPO COMMAND...
#
# Show and run a command in the work repository of an upstream project, as
# its maintainer.
upstream()
{
	local repo="$1"

	shift
	as "$UPSTREAM_NAME" "$UPSTREAM_EMAIL"
	SHOWN_PROMPT="(upstream ${repo}) ${PROMPT}"
	cd -- "${DEMO}/work/${repo}"
	show "$@"
	cd -- "$SUPER"
	SHOWN_PROMPT="$PROMPT"
	as "$DEVELOPER_NAME" "$DEVELOPER_EMAIL"
}

introduce()
{
	local signed="SSH-signed tag"
	local crypto

	crypto="$(rev crypto-lib main~1 | cut -c 1-7)"
	if [[ -z "$SIGNING_KEY" ]]; then
		signed="unsigned (no ssh-keygen)"
	fi
	say "The superproject \"firmware\" has five commits and fourteen submodules." \
		"Their upstream repositories are bare repositories in ${DEMO}/mirror." \
		"Every URL in .gitmodules starts with ${FAKE_HOST}," \
		"a host that does not exist; url.<base>.insteadOf rewrites it to that" \
		"directory, as it would for a company mirror. Git may use the file" \
		"transport only, so nothing in this demo reaches the network."
	emit "" "  SUBMODULE   PATH               TRACKS              SITUATION"
	emit "" "  kernel      kernel             tag-pattern v6.6.*  newer v6.6.x tag available"
	emit "" "  u-boot      bootloader/u-boot  branch main         upstream moved on"
	emit "" "  fpga.core   ip/fpga-core       tag v2.3.1          dotted name, tag will move"
	emit "" "  crypto lib  libs/crypto lib    commit ${crypto}      spaces in name and path"
	emit "" "  legacy      vendor/legacy      -                   not managed"
	emit "" "  tools       tools/nested       branch develop      has a nested submodule"
	emit "" "  theme       docs/theme         tag v1.0.0          deinitialized, repo kept"
	emit "" "  fresh       third_party/fresh  tag-pattern v1.*    never cloned"
	emit "" "  app         apps/app           tag v1.2.0          uncommitted change"
	emit "" "  sdk         sdk                tag-pattern v*      locked at a pre-release"
	emit "" "  mirror-lib  libs/mirror        branch stable       branch other than main"
	emit "" "  broken      libs/broken        tag v9.9.9          tag does not exist"
	emit "" "  signed      libs/signed        tag v1.0.0          ${signed}"
	emit "" "  quirky      libs/quirky        tag v1.0.0          update=none, shallow"
	emit "" ""
}

chapter_tour()
{
	chapter "The superproject"
	say "Tracking settings live in .gitmodules, next to Git's own keys, and" \
		".lsm.lock records what each submodule was resolved to. Both files" \
		"are plain git-config files, committed with the gitlinks."
	show git log --oneline
	show head -n 11 .gitmodules
	show head -n 4 .lsm.lock
	say "The kernel follows the newest stable v6.6.x tag, which was v6.6.9 at" \
		"the last update. u-boot follows the branch main, and the native key" \
		"\"branch\" keeps \"git submodule update --remote\" working without" \
		"LazySubmodules."
}

chapter_status()
{
	chapter "Where do we stand? (status)"
	say "status reads local refs only, so it works offline."
	show lazysubmodules status
	say "- behind: kernel has a newer v6.6.x tag, v6.6.10, and origin/main of" \
		"  u-boot has moved on." \
		"- uninitialized: theme was deinitialized, fresh was never cloned." \
		"- dirty: app has an uncommitted change." \
		"- missing-ref: broken tracks tag v9.9.9, which does not exist." \
		"- unmanaged: legacy has no lsm-mode key." \
		"- ok: sdk stays at its release candidate although v2.9.0 is the" \
		"  newest stable tag: an update never goes back from the locked tag." \
		"  tools is ok although its nested submodule is at another commit," \
		"  and quirky although .gitmodules says update = none and ignore = all."
}

chapter_porcelain()
{
	chapter "Output for scripts (status --porcelain=v1)"
	say "The porcelain format is a stable interface: a header line, then" \
		"eight TAB-separated fields per submodule, with full commit IDs and" \
		"empty fields where there is no value."
	show lazysubmodules status --porcelain=v1 kernel legacy theme
	say "A script can list what needs attention:"
	show_sh "$(
		cat <<-'EOF'
			lazysubmodules status --porcelain=v1 |
			  awk -F '\t' 'NR > 1 && $8 != "ok" { print $1 ": " $8 }'
		EOF
	)"
}

chapter_set()
{
	chapter "Opting in (set)"
	say "LazySubmodules leaves unmanaged submodules alone, even when named:"
	show -e 3 lazysubmodules update legacy
	say "set writes the tracking keys to .gitmodules, and for branch mode the" \
		"native branch key too. It changes nothing else."
	show lazysubmodules set legacy --branch main
	show git config -f .gitmodules --get-regexp '^submodule\.legacy\.'
	show lazysubmodules status legacy
	say "legacy is behind because it has no lock entry yet. update records" \
		"it, and --commit commits the result:"
	show lazysubmodules update --commit legacy
	show git log -1 --format=%B
}

chapter_dry_run()
{
	chapter "What would change? (update --dry-run)"
	say "A dry run resolves the targets in the local refs and changes nothing."
	show lazysubmodules update --dry-run kernel u-boot theme sdk
	say "kernel would move to v6.6.10: version sorting puts it above v6.6.9," \
		"and v6.6.11-rc1 is a pre-release. theme would be initialized from the" \
		"repository that deinit left behind, without network access. sdk keeps" \
		"its release candidate. Pre-releases count only on request:"
	show lazysubmodules update --dry-run --include-prerelease kernel sdk
}

chapter_refusal()
{
	chapter "All or nothing (refused update)"
	say "An update of every submodule is refused. The refusal names every" \
		"problem, and nothing changes until all selected submodules are safe:"
	show -e 3 lazysubmodules update
	show git status --short
	say "The index is untouched; the two \"m\" lines are the change in app and" \
		"the nested submodule of tools. Now fix the problems. The change in" \
		"app is not worth keeping:"
	show git -C apps/app status --short
	show git -C apps/app restore NEWS
	say "broken asks for tag v9.9.9, which its repository does not have:"
	show git -C libs/broken tag --list
	show lazysubmodules set broken --tag v1.0.0
	show lazysubmodules update --commit broken
	say "fresh needs a clone, which is network access, and update uses the" \
		"network only with --fetch (chapter 9). The dry run of everything now" \
		"reports only fresh:"
	show -e 3 lazysubmodules update --dry-run
}

chapter_commit()
{
	chapter "Update and commit a selection (update --commit)"
	say "--commit creates one commit with a generated message. It refuses when" \
		"the index holds anything that does not belong to the update:"
	show_sh 'echo "Revision B" >>src/drivers/README'
	show git add src/drivers/README
	show -e 3 lazysubmodules update --commit kernel u-boot theme
	show git restore --staged src/drivers/README
	show lazysubmodules update --commit kernel u-boot theme
	show git log -1
	say "theme was only initialized; its recorded commit is the same, so the" \
		"message leaves it out. \"git commit -s\" added the Signed-off-by line."
	show lazysubmodules status kernel u-boot theme
}

chapter_moved_tag()
{
	chapter "A tag moved upstream (fetch, drift, verify)"
	say "The maintainer of fpga-core moves the release tag v2.3.1 to a later" \
		"commit and force-pushes it:"
	upstream fpga-core git tag --force -a -m "release v2.3.1" v2.3.1 main
	upstream fpga-core git push --force origin v2.3.1
	as "$UPSTREAM_NAME" "$UPSTREAM_EMAIL"
	release sdk "sdk v3.0.0" v3.0.0 annotated
	publish sdk
	as "$DEVELOPER_NAME" "$DEVELOPER_EMAIL"
	say "The sdk project releases v3.0.0 as well. Local refs do not change" \
		"until a fetch, so status still sees nothing new:"
	show lazysubmodules status fpga.core sdk
	show lazysubmodules fetch fpga.core sdk
	show lazysubmodules status fpga.core sdk
	say "fetch replaces moved tags. The locked commit of fpga.core no longer" \
		"matches its tag, which verify reports with exit status 4:"
	show -e 4 lazysubmodules verify fpga.core sdk
	say "After reviewing the new commit, accept it with an update. sdk moves on" \
		"to the stable v3.0.0, which is newer than its release candidate:"
	show lazysubmodules update --commit fpga.core sdk
	show git log -1 --format=%B
	show lazysubmodules verify fpga.core sdk
}

chapter_clone()
{
	chapter "Clone through the mirror (update --fetch)"
	say "fresh was never cloned, and update does not clone without --fetch:"
	show -e 3 lazysubmodules update fresh
	show lazysubmodules update --fetch fresh
	say "The clone keeps the URL from .gitmodules. Git rewrites it to the" \
		"mirror whenever it connects:"
	show git -C third_party/fresh config remote.origin.url
	show git -C third_party/fresh remote get-url origin
	say "Lock file, gitlinks, checkouts and tags now agree for every submodule:"
	show lazysubmodules verify
}

chapter_signatures()
{
	chapter "Signatures (verify --signatures)"
	say "--signatures also checks the signature of the locked tag, or of the" \
		"locked commit in branch and commit mode, using Git's settings." \
		"The demo trusts the SSH key that signed tag v1.0.0 of signed. The tag" \
		"of app is annotated but not signed:"
	if [[ -z "$SIGNING_KEY" ]]; then
		note "ssh-keygen is not installed, so tag v1.0.0 of signed is not" \
			"signed either, and its check fails as well."
	fi
	show -e 4 lazysubmodules verify --signatures signed app
}

chapter_foreach()
{
	chapter "Run a command everywhere (foreach)"
	say "foreach runs a command in every checked-out managed submodule. It" \
		"starts no shell of its own, so this example asks for one. The command" \
		"finds name, sm_path, sha1, LSM_MODE and LSM_REF in its environment:"
	# shellcheck disable=SC2016 # expanded by the shell that foreach starts
	show lazysubmodules foreach -- sh -c \
		'echo "$name ($LSM_MODE): $(git log -1 --format=%s)"'
	say "foreach stops at the first command that fails and exits with status 1." \
		"Is every submodule checked out exactly at a tag?"
	show -e 1 lazysubmodules foreach -- git describe --tags --exact-match
}

chapter_end()
{
	chapter "Where we ended up"
	show lazysubmodules status
	show git log --oneline
	say "Exit statuses: 0 success, 1 error (such as a failed foreach command)," \
		"2 usage error, 3 refused, 4 verification failed, 5 a git command" \
		"failed."
}

# farewell
#
# Tell how to explore a kept demo.
farewell()
{
	if [[ -z "$KEEP_DIR" ]]; then
		say "Run \"scripts/demo.sh --keep DIR\" to keep the demo, or add" \
			"--setup-only to explore the superproject in its first state."
		return 0
	fi
	local env super

	printf -v env '%q' "${DEMO}/env.sh"
	printf -v super '%q' "$SUPER"
	say "The demo stays in ${DEMO}. To explore it, run:" \
		"" \
		"  . ${env}" \
		"  cd ${super}" \
		"  lazysubmodules tui"
}

main()
{
	parse_args "$@"
	find_binary
	open_transcript
	trap cleanup EXIT
	make_demo_dir
	setup_output
	setup_env

	rule "LazySubmodules demo"
	emit "" ""
	if [[ -n "$KEEP_DIR" ]]; then
		emit "" "Demo directory: ${DEMO}"
	else
		emit "" "Demo directory: ${DEMO} (temporary, removed at the end)"
	fi
	emit "" "Building the upstream repositories and the superproject..."
	emit "" ""
	make_upstreams
	make_superproject
	populate
	introduce
	cd -- "$SUPER"
	if ((!SETUP_ONLY)); then
		chapter_tour
		chapter_status
		chapter_porcelain
		chapter_set
		chapter_dry_run
		chapter_refusal
		chapter_commit
		chapter_moved_tag
		chapter_clone
		chapter_signatures
		chapter_foreach
		chapter_end
	fi
	farewell
}

main "$@"
