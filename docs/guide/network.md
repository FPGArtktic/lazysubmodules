<!-- SPDX-License-Identifier: GPL-3.0-only -->
<!-- Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com> -->

(guide-network)=

# Mirrors, credentials and offline work

Which commands use the network, how Git's configuration applies to them,
and how to work without a connection.

## Network policy

| Command | Network |
|---|---|
| `fetch` | Yes |
| `update --fetch` | Yes, before resolution |
| `add` | Yes, to clone |
| Every other command | **No** |

- **Dry runs:** `update --dry-run --fetch` does not use the network
  either. A submodule that would need a clone shows the target
  `unknown until fetched`.
- **Terminal interface:** {kbd}`f` is the only key that uses the network;
  {kbd}`u` and {kbd}`U` never fetch.
- **`foreach`:** uses no network itself, but the commands you run with it
  may.

This policy is part of the stable interface. `status`, `verify`, `set`
and `update` without `--fetch` therefore work offline, on the refs that
the last fetch left behind.

## How LazySubmodules reaches a remote

All network access goes through the `git` command, so Git's own
configuration applies unchanged: URL rewriting, credential helpers,
proxies, SSH settings and `protocol.*` rules. LazySubmodules adds nothing
of its own and always uses the remote named `origin`.

(guide-network-mirrors)=

## Use a mirror

`url.<base>.insteadOf` makes Git replace the start of a URL whenever it
connects. The URL in `.gitmodules` stays as it is, so the superproject
works for everyone, and each machine decides where to fetch from:

```sh
git config --global url."https://mirror.example.com/github/".insteadOf "https://github.com/"
```

The demo uses the same mechanism: every submodule URL starts with
`https://git.example.invalid/`, and a rule rewrites it to bare
repositories on the local disk. Chapter 9 clones a submodule through
that mirror. The `[exit status N: …]` lines come from the demo, not from
LazySubmodules.

```{literalinclude} ../../examples/transcript.txt
:language: console
:start-at: "$ lazysubmodules update fresh"
:end-before: "The clone keeps the URL"
```

The clone keeps the URL from `.gitmodules`. Git applies the rule each
time it connects:

```{literalinclude} ../../examples/transcript.txt
:language: console
:start-at: "$ git -C third_party/fresh config remote.origin.url"
:end-at: "file://$DEMO/mirror/fresh.git"
```

Use `git remote get-url origin` inside a submodule to check where a fetch
will go.

## Credentials

LazySubmodules does not store or ask for credentials. Git gets them the
usual way:

- **HTTPS:** a credential helper (`credential.helper`), such as a
  keyring-based helper or `store`, or a token in an `insteadOf` rule.
- **SSH:** an SSH agent, or a key without a passphrase. Host keys must be
  known for the terminal interface; see below.

On the command line, Git may still prompt in the terminal when no helper
answers.

(guide-network-tui)=

## Git without a terminal in the TUI

While the terminal interface is shown, Git runs in a session of its own,
without access to the terminal, and with `GIT_TERMINAL_PROMPT=0`. Git
cannot ask anything there, so a prompt fails instead of drawing over the
screen. This affects fetching and cloning with {kbd}`f`, and the hooks
that run during {kbd}`u` and {kbd}`U`:

- **Credentials** must come from a credential helper that does not prompt
  in the terminal, from an SSH agent, or from a key without a passphrase.
- **SSH host keys** must already be known. An unknown host fails with
  `Host key verification failed`; connect once with `ssh` or `git fetch`
  in a normal shell to accept the key.
- **Hooks** must not read from the terminal.
- **Graphical prompts** still work: an `SSH_ASKPASS` or `GIT_ASKPASS`
  program, or a graphical pinentry.

With `commit.gpgSign` set, a terminal pinentry such as `pinentry-curses`
finds the terminal through `GPG_TTY` and can still draw over the
interface. Use a graphical pinentry or a cached passphrase, or commit with
`lazysubmodules update --commit` outside the interface.

(guide-network-offline)=

## Work offline

Everything except `fetch`, `update --fetch` and `add` works without a
connection, on the refs that exist locally:

- `status` compares with the remote-tracking branches and tags as of the
  last fetch. A submodule can be `ok` offline and `behind` after the next
  fetch.
- `update` resolves targets in the local refs. It initializes a
  deinitialized submodule offline when its Git directory still exists,
  and refuses when a clone is needed.
- `verify` needs no remote at all.

Before you go offline, fetch everything:

```sh
lazysubmodules fetch
```

This also clones and initializes every managed submodule that is not
checked out yet.

## Air-gapped networks

On a machine that never reaches the upstream hosts, serve the submodule
repositories from a host or a directory it can reach, and rewrite the
upstream URLs to it:

```sh
# Bare mirrors on an internal server:
git config --global url."https://git.internal.example/mirror/".insteadOf "https://github.com/"

# Or bare mirrors on a local disk or a mounted volume:
git config --global url."file:///srv/git-mirror/".insteadOf "https://github.com/"
```

- **Mirror layout.** Each mirror must be a bare repository at the path
  that the rewritten URL names, with the branches and tags that the
  superproject tracks. `git clone --mirror` on a connected machine creates
  one, and `git remote update` in it refreshes it.
- **File transport.** Git allows `file://` URLs for submodules only when
  `protocol.file.allow` permits it, for example
  `git config --global protocol.file.allow always`. Allow it only for
  mirrors you control.
- **Moved tags.** `fetch` replaces local tags with the mirror's, so a tag
  moved upstream reaches you when the mirror is refreshed, and `verify`
  reports it as usual.

The demo works exactly this way. Its `env.sh`, created by
`scripts/demo.sh --keep DIR`, shows a complete configuration, including
`GIT_ALLOW_PROTOCOL=file`, which forbids every other transport.

## See also

- {ref}`ref-cli-fetch`: what `fetch` runs.
- {ref}`expl-design-limitations`: the remote is always `origin`, and
  resolution is local.
- [Use the terminal interface](tui.md).
