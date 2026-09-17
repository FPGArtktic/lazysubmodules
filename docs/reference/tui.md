<!-- SPDX-License-Identifier: GPL-3.0-only -->
<!-- Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com> -->

(ref-tui)=

# Terminal interface keys

Every key of the terminal interface, the keys of its dialogs and views, its
layout rules and the messages of its status bar.

`lazysubmodules tui` starts the interface; {ref}`ref-cli-tui` lists its
requirements and {ref}`guide-tui` shows how to work with it.

## Main keys

| Key | Key bar label | Action |
|---|---|---|
| {kbd}`↑` / {kbd}`k` | `up` | Select the previous submodule |
| {kbd}`↓` / {kbd}`j` | `down` | Select the next submodule |
| {kbd}`PgUp` | `page up` | Page up |
| {kbd}`PgDn` | `page down` | Page down |
| {kbd}`g` / {kbd}`Home` | `first` | Select the first submodule |
| {kbd}`G` / {kbd}`End` | `last` | Select the last submodule |
| {kbd}`Enter` | `details` | Show the details of the selected submodule |
| {kbd}`u` | `update` | Update the selected submodule and stage the result |
| {kbd}`U` | `update+commit` | Update the selected submodule and commit it with `git commit -s` |
| {kbd}`b` | `branch` | Track a branch: pick one of the remote-tracking branches |
| {kbd}`t` | `tag` | Track a tag: pick one of the local tags |
| {kbd}`p` | `pattern` | Track a tag pattern: type the pattern |
| {kbd}`f` | `fetch` | Fetch the selected submodule, cloning it if needed. The only key that uses the network |
| {kbd}`v` | `verify` | Verify the selected submodule |
| {kbd}`d` | `diff` | Show how the submodule differs from its gitlink in `HEAD` |
| {kbd}`r` | `reload` | Reload the status of all submodules |
| {kbd}`?` | `help` | Show the key reference |
| {kbd}`q` | `quit` | Quit; in a dialog or view, close it first |
| {kbd}`Ctrl+C` | `quit now` | Quit at once, from anywhere, and interrupt a running operation |

The key bar at the bottom shows the main keys, most important first, and
more of them in wide terminals. At 80 columns it reads:

```text
 u update  b branch  t tag  p pattern  f fetch  v verify  ? help  q quit
```

At 120 columns, `U update+commit`, `d diff` and `r reload` are added before
`? help`. At 60 columns, only `u`, `b`, `t`, `p`, `?` and `q` fit.

## Dialog and view keys

| Where | Keys |
|---|---|
| Question | {kbd}`y` or {kbd}`Y` confirms; {kbd}`n`, {kbd}`N`, {kbd}`Esc` or {kbd}`q` cancels |
| Branch or tag list | {kbd}`↑` {kbd}`↓` (or {kbd}`k` {kbd}`j`), {kbd}`PgUp` {kbd}`PgDn` and {kbd}`g` {kbd}`G` move; {kbd}`/` filters, {kbd}`Enter` chooses, {kbd}`Esc` or {kbd}`q` closes |
| Filter of a list | Typed keys edit the filter, {kbd}`Enter` chooses, {kbd}`Esc` clears the filter |
| Pattern dialog | Typed keys edit the pattern, {kbd}`Enter` submits it, {kbd}`Esc` closes |
| Details, diff, verify result, help | {kbd}`↑` {kbd}`↓` (or {kbd}`k` {kbd}`j`), {kbd}`PgUp` {kbd}`PgDn`, {kbd}`Space`, {kbd}`Ctrl+U` {kbd}`Ctrl+D` and {kbd}`g` {kbd}`G` scroll; {kbd}`Esc` or {kbd}`q` closes |

- **Text fields.** In the pattern dialog and in the filter of a list,
  {kbd}`q` is typed as text; {kbd}`Esc` leaves the field.
- **Confirmation.** {kbd}`y` works only once the result of the dry run is
  shown.
- **Help view.** {kbd}`?` shows the keys in four groups, Navigation,
  Submodule, Tracking and General, followed by these notes:

  ```text
  In dialogs, y confirms and n or esc cancels. q closes a dialog, except
  in text fields (the pattern dialog and the filter of a list), where it
  is text; esc leaves them.
  While an operation runs, q asks before quitting; ctrl+c quits at once
  and interrupts the operation.
  Only f (fetch) uses the network.
  ```

## Layout

```text
┌ Submodules ─────────────────────────────────────────┬ Preview ───────────────┐
│ NAME       MODE        REF     LOCK    STATE        │ kernel behind          │
│ kernel     tag-pattern v6.6.*  a1b2c3d behind       │ update would select    │
│ ...                                                 │ ...                    │
└─────────────────────────────────────────────────────┴────────────────────────┘
 6 submodules, 4 not ok
 u update  b branch  t tag  p pattern  f fetch  v verify  ? help  q quit
```

This is an excerpt of a screen from the tests of the interface, which use
illustrative commits.

- **Table.** The submodules with their name, mode, configured ref, locked
  commit and state. In narrow terminals, the `LOCK` column is dropped
  first, then `MODE`.
- **Preview.** For the selected submodule: why it is in its state, its
  `HEAD`, lock and update target, the commits an update would add
  (`Update adds`), the difference between the lock and `HEAD`, the recent
  log and the tags. It uses local refs only, and it is hidden in terminals
  narrower than 80 columns.
- **Status bar.** The number of submodules and how many are not `ok`, or
  the result of the last action, or an error.
- **Key bar.** The main keys, as above.
- **Colors.** The states use the colors of `status`; see
  {ref}`ref-states`. `NO_COLOR` with any non-empty value turns colors off,
  while bold text and reverse video, which mark headings and the
  selection, remain.

## Actions

### Update: {kbd}`u` and {kbd}`U`

1. A dry run of the update runs first, as `update --dry-run` would, and its
   result is shown, for example
   `Update kernel: v6.6.9 (8106f61) -> v6.6.10 (08dcd0d)`.
2. A question asks whether to stage the result ({kbd}`u`) or to commit it
   ({kbd}`U`).
3. After {kbd}`y`, the update runs in the background.

- **Without a question.** A refusal, such as uncommitted changes, and
  `already up to date` are shown in the status bar at once.
- **Nothing recorded.** The question says so when the update changes
  nothing that the index ({kbd}`u`) or `HEAD` ({kbd}`U`) records: when it
  only initializes the submodule, checks out the recorded commit, or
  rewrites the working tree copy of `.gitmodules` or `.lsm.lock`.
- **Staged changes.** With {kbd}`U`, the question also says when a change
  staged for the submodule would be discarded, as
  [`update --commit`](cli/update.md) does, and when the update is staged
  already.
- **No fetching.** {kbd}`u` and {kbd}`U` never fetch. A submodule that needs
  a clone is refused with a hint to press {kbd}`f` first.

### Tracking: {kbd}`b`, {kbd}`t` and {kbd}`p`

- **Like `set`.** The keys change only the tracking configuration in
  `.gitmodules`, after a confirmation. Press {kbd}`u` or {kbd}`U` afterwards
  to update.
- **Lists.** {kbd}`b` lists the remote-tracking branches of the submodule,
  and {kbd}`t` its local tags.
- **Pattern dialog.** {kbd}`p` counts the local tags that match the pattern
  while it is typed, and lists the highest of them, including how many are
  pre-releases. An invalid pattern cannot be submitted.

### Background work

- **Spinner.** Long-running operations run in the background, and the
  interface stays responsive.
- **One at a time.** Only one modifying operation runs at a time. Other
  modifying keys are ignored meanwhile, with a note.
- **Errors** are shown in the status bar, or in a dialog when they are too
  long for it. The interface does not exit on errors.

## Status bar messages

Outcome messages name the submodule and what happened:

| Example | Meaning |
|---|---|
| `kernel: updated to v6.6.10 (08dcd0d), staged` | {kbd}`u` updated and staged the submodule |
| `theme: initialized at v1.0.0 (05f49f3)` | The submodule was only initialized |
| `sdk: .lsm.lock restored to v2.9.0 (68a8743)` | Only the working tree copy of the lock file was rewritten |

A message adds `staged change discarded` when {kbd}`U` discarded a staged
change, and ends with `staged`, `committed <commit>` or
`nothing to commit` only when that applies.

## Leaving the interface

- **{kbd}`q`** quits from the table. In a dialog, the details or the help,
  it closes that first.
- **While an operation runs,** {kbd}`q` asks before quitting, because
  quitting interrupts the operation.
- **{kbd}`Ctrl+C`** quits at once and interrupts a running operation, which
  puts back what it changed.
- **Late results.** `lazysubmodules tui` waits for an interrupted operation
  to finish and prints its outcome after the screen closed. A failure is
  reported as an error, and the exit status is that of the failure, for
  example 5 for an interrupted update.
- **Signals.** `SIGINT`, `SIGTERM` and `SIGHUP` sent to the process end the
  interface the same way. The error names the signal, and the exit status is
  1 unless an interrupted operation failed:

  ```text
  lazysubmodules: terminal interface: terminated signal received
  ```

## See also

- {ref}`guide-tui`: working with the interface.
- {ref}`ref-cli-tui`: requirements and exit status.
- {ref}`guide-network-tui`: credentials, host keys and hooks while the
  interface is shown.
