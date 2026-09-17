<!-- SPDX-License-Identifier: GPL-3.0-only -->
<!-- Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com> -->

(ref-runtime)=

# Environment and signals

The environment variables, Git settings, streams and signals that affect
LazySubmodules, and the environment it gives to Git and to `foreach`.

(ref-runtime-env)=

## Environment variables

### Read by LazySubmodules

| Variable | Effect |
|---|---|
| `NO_COLOR` | Any non-empty value turns colors off, in the table of `status` and in the terminal interface. Bold text and reverse video remain in the interface |
| `TERM` | `status` uses no colors when it is `dumb`. `tui` requires it to be set and not `dumb` |
| `COLORTERM` and similar | The terminal interface detects the color depth of the terminal from its environment |
| `PATH` | Where `git` and the commands of `foreach` are found |

Besides the environment, `status` uses colors only when standard output is
a terminal, and `tui` requires standard input and output to be terminals.

### Set for every `git` command

| Variable | Value | Why |
|---|---|---|
| `LC_ALL` | `C` | Output parsing does not depend on the locale. Git's messages in errors are therefore in English |
| `GIT_OPTIONAL_LOCKS` | `0` | Read-only commands take no optional locks |
| `GIT_NO_LAZY_FETCH` | `1`, or `0` for network commands | A partial clone does not fetch missing objects on demand, except in `fetch`, `update --fetch` and `add` |
| `GIT_ALLOW_PROTOCOL` | empty, for offline commands only | Permits no transport at all, whatever `protocol.<name>.allow` says. Network commands and `git commit` keep the inherited value |
| `GIT_TERMINAL_PROMPT` | `0`, in the terminal interface only | Git cannot ask for credentials there |

Hooks that Git runs, such as the commit hooks of `update --commit`, see
the same environment.

### Removed before running `git`

Variables that tie Git to one repository are removed, so that a
surrounding Git hook or a shell setting cannot redirect a command to
another repository: `GIT_DIR`, `GIT_WORK_TREE`, `GIT_INDEX_FILE`,
`GIT_OBJECT_DIRECTORY`, `GIT_ALTERNATE_OBJECT_DIRECTORIES`, `GIT_CONFIG`,
`GIT_COMMON_DIR`, `GIT_PREFIX`, `GIT_GRAFT_FILE`,
`GIT_NO_REPLACE_OBJECTS`, `GIT_REPLACE_REF_BASE`, `GIT_SHALLOW_FILE`,
`GIT_IMPLICIT_WORK_TREE` and `GIT_INTERNAL_SUPER_PREFIX`. On the demo:

```console
$ env GIT_DIR=/nonexistent GIT_INDEX_FILE=/nonexistent lazysubmodules status kernel
NAME    PATH    MODE         REF     LOCK              HEAD     STATE
kernel  kernel  tag-pattern  v6.6.*  8106f61 (v6.6.9)  8106f61  behind
```

All other variables are passed on. This includes configuration given in
the environment with `GIT_CONFIG_COUNT`, `GIT_CONFIG_KEY_<n>` and
`GIT_CONFIG_VALUE_<n>` or `GIT_CONFIG_PARAMETERS`, as well as
`GIT_CONFIG_GLOBAL`, `GIT_CONFIG_NOSYSTEM`, `GIT_SSH_COMMAND`,
`GIT_ASKPASS` and `SSH_AUTH_SOCK`. The demo's `env.sh` uses them to
isolate Git from the user's configuration.

### Set for `foreach` commands

A command run by [`foreach`](cli/foreach.md) inherits the environment of
`lazysubmodules`, without the repository variables above, and gets these
variables:

| Variable | Value |
|---|---|
| `name` | Submodule name |
| `sm_path` | Path recorded in `.gitmodules` |
| `displaypath` | Path of the submodule relative to the current directory |
| `sha1` | Commit checked out in the submodule |
| `toplevel` | Absolute path of the superproject |
| `LSM_MODE` | Tracking mode (`lsm-mode`) |
| `LSM_REF` | Configured ref (`lsm-ref`) |

The command does not get the `LC_ALL` and `GIT_*` values that
LazySubmodules sets for its own `git` commands.

(ref-runtime-git-config)=

## Git configuration

All work goes through the `git` command line tool, so Git's configuration
applies as usual. These settings matter most:

| Setting | Effect on LazySubmodules |
|---|---|
| `url.<base>.insteadOf` | Clones and fetches go to the rewritten URL, such as a company mirror. `.gitmodules` and `remote.origin.url` keep the original URL |
| `credential.*` | Credentials for `fetch`, `update --fetch` and `add` come from Git's credential helpers |
| `core.sshCommand`, `GIT_SSH_COMMAND` | How Git runs SSH |
| `protocol.<name>.allow`, `GIT_ALLOW_PROTOCOL` | The transports that the network commands may use |
| `user.name`, `user.email` | Author and `Signed-off-by` line of `update --commit` |
| `commit.gpgSign`, `gpg.*` | Signing of the commit of `update --commit` |
| `gpg.format`, `gpg.ssh.allowedSignersFile` and other `gpg.*` settings | How `verify --signatures` checks GPG, SSH and X.509 signatures |
| `core.hooksPath` | The hooks that `git commit` runs for `update --commit` |
| `safe.directory` | Git's ownership check applies; a refused repository makes commands fail |

Some settings are overridden or do not apply:

| Setting | What LazySubmodules does |
|---|---|
| `submodule.<name>.update` | Ignored: `update` and `fetch` check the submodule out even with `update = none` |
| `submodule.<name>.ignore` | Ignored: `status` looks at the real state of the checkout |
| `submodule.recurse`, `fetch.recurseSubmodules` | Overridden: nested submodules are neither fetched nor updated |
| `advice.detachedHead` | Turned off for the checkouts of `update` |
| `versionsort.suffix` | Set to `-` for the version sort of tag patterns |
| Remote name | Always `origin`; other remotes are not used |

A transport that the environment does not allow makes a network command
fail with status 5:

```console
$ env GIT_ALLOW_PROTOCOL=https lazysubmodules fetch kernel
fatal: transport 'file' not allowed
lazysubmodules: kernel: git fetch --tags --force --prune --no-recurse-submodules --end-of-options origin: exit status 128
```

The demo's submodules are only reachable through the `file` transport.

(ref-runtime-streams)=

## Standard streams

| Stream | Content |
|---|---|
| Standard output | Results: the table or porcelain format of `status`; the lines of `add`, `set`, `update`, `fetch` and `verify`; `version`; `help` and `-h`; the output of `foreach` commands |
| Standard error | Errors and usage hints; the `verification failed` summary of `verify`; the notes about submodules that `foreach` skips; the output of `git` itself, such as clone and fetch progress; the error output of `foreach` commands |
| Standard input | Passed to `foreach` commands, and read by the terminal interface. The `git` commands of LazySubmodules do not read it |

`lazysubmodules` without arguments prints its usage on standard error.
Output written to a pipe has no colors.

(ref-runtime-signals)=

## Signals

### Command line

- **Cancel.** `SIGINT` ({kbd}`Ctrl+C`), `SIGTERM` and `SIGHUP`, which a
  terminal sends when it is closed, cancel the running command. The `git`
  processes it started, and the processes they started, receive `SIGTERM`,
  and are killed if they have not exited 10 seconds later. The same applies
  to a command run by `foreach`.
- **Rollback.** An interrupted `update` puts back what it changed before
  staging: submodules it already checked out return to their previous
  commit, and nothing is staged. Once the result is staged, only the
  commit remains; when `git commit` is interrupted or fails, the update
  stays staged and no commit is made.
- **Exit status.** The command prints the error and exits with its code,
  usually 5, because a `git` command was killed.
- **Second signal.** A second signal ends LazySubmodules at once, without
  waiting for the clean-up.
- **Ignored signals.** `SIGINT` and `SIGHUP` stay ignored when they were
  ignored at start, as under `nohup`, or for a background job of a
  non-interactive shell. Such a job can still be stopped with `SIGTERM`.

An update of the demo, interrupted with `SIGTERM` while it checked out
`kernel`:

```text
lazysubmodules: kernel: git -c advice.detachedHead=false checkout --quiet --no-recurse-submodules --detach 08dcd0dc8f98ab3da3943153cad056f346d18ded --: context canceled: signal: terminated
```

The command exited with status 5, and `git status --short` and
`lazysubmodules status kernel u-boot` showed the state from before the
update.

### Terminal interface

- **Keys.** In the interface, {kbd}`Ctrl+C` is a key: it quits at once and
  interrupts a running operation, which then puts back what it changed.
- **Signals.** `SIGINT`, `SIGTERM` and `SIGHUP` sent to the process end the
  interface the same way. The error names the signal, and the exit status
  is 1 unless an interrupted operation failed:

  ```text
  lazysubmodules: terminal interface: terminated signal received
  ```

- **Late results.** `lazysubmodules tui` waits for an interrupted operation
  to finish and prints its outcome after the screen closed.

(ref-runtime-tui-git)=

## Git in the terminal interface

While the interface is shown, Git runs in a session of its own, without
access to the terminal, and with `GIT_TERMINAL_PROMPT=0`. A prompt
therefore fails instead of drawing over the screen:

- **Credentials** must come from a credential helper that does not prompt
  in the terminal, from an SSH agent, or from a key without a passphrase.
- **SSH host keys** must already be known; an unknown host fails with
  `Host key verification failed`.
- **Hooks** must not read from the terminal.
- **Graphical prompts** still work: an `SSH_ASKPASS` or `GIT_ASKPASS`
  program, or a graphical pinentry.
- **Commit signing.** With `commit.gpgSign` set, a terminal pinentry such
  as `pinentry-curses` finds the terminal through `GPG_TTY` and can still
  draw over the interface. Use a graphical pinentry or a cached passphrase,
  or commit with `lazysubmodules update --commit`.

## Requirements

- **System.** Linux on `amd64` or `arm64`.
- **Git** 2.39 or later, found in `PATH`. Without it, commands that open a
  repository fail with status 1:
  `lazysubmodules: git executable not found: exec: "git": executable file not found in $PATH`.
  `version` and `help` work without Git.

## See also

- {ref}`guide-network`: mirrors, credentials and offline work.
- {ref}`guide-network-tui`: the terminal interface without a terminal for
  Git.
- {ref}`ref-exit-codes`: the exit status after an interruption.
- {ref}`expl-safety`: why updates roll back.
