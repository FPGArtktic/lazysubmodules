<!-- SPDX-License-Identifier: GPL-3.0-only -->
<!-- Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com> -->

(ref-cli-tui)=

# `tui`

Start the interactive terminal interface on the superproject that contains
the current directory.

## Synopsis

```text
lazysubmodules tui
```

`tui` takes no arguments. Inside the interface, only the fetch key
{kbd}`f` uses the network.

## Help text

```{literalinclude} _generated/tui.txt
:language: text
:lines: 5-
```

## Requirements

- **A terminal.** Standard input and standard output must both be a
  terminal.
- **Cursor movement.** `TERM` must be set, and not to `dumb`.

Otherwise `tui` exits with status 2 and one of these messages, without the
usage hint:

| Situation | Message |
|---|---|
| Standard input or output is not a terminal | `lazysubmodules: tui requires a terminal` |
| `TERM` is not set | `lazysubmodules: tui requires a terminal (TERM is not set)` |
| `TERM` is `dumb` | `lazysubmodules: tui requires a terminal with cursor movement (TERM is dumb)` |

The terminal checks come first, so these messages appear even outside a
repository.

## Behaviour

- **Git without a terminal.** While the interface is shown, Git runs in a
  session of its own, without access to the terminal, and with
  `GIT_TERMINAL_PROMPT=0`. Fetching and cloning need a credential helper
  or an SSH agent, and SSH host keys that are already known;
  {ref}`guide-network-tui` explains the details.
- **Colors** follow the terminal. `NO_COLOR` with any non-empty value turns
  them off; bold text and reverse video remain.
- **Signals.** `SIGINT`, `SIGTERM` and `SIGHUP` sent to the process end the
  interface. Inside the interface, {kbd}`Ctrl+C` is a key that quits at
  once; see {ref}`ref-runtime-signals`.
- **Late results.** When an operation is interrupted by quitting,
  `lazysubmodules tui` waits for it to finish and prints its outcome after
  the screen closed.

The keys, dialogs and messages of the interface are listed in
{ref}`ref-tui`.

## Exit status

| Status | When |
|---|---|
| 0 | The interface was closed, and every operation succeeded or its outcome was shown |
| 1 | A signal ended the interface, for example `lazysubmodules: terminal interface: terminated signal received` |
| 2 | No usable terminal, or an argument was given |
| 5 | The current directory is not inside a Git repository |
| 1 to 5 | An operation that was interrupted by quitting failed afterwards; the status is that of the failure, for example 5 for an interrupted update |

Errors inside the interface do not end it; they are shown in its status
bar.

## Examples

```sh
lazysubmodules tui

# Without colors.
NO_COLOR=1 lazysubmodules tui
```

## See also

- {ref}`guide-tui`: working with the interface.
- {ref}`ref-tui`: keys and messages.
- {ref}`guide-network-tui`: credentials and host keys.
