<!-- SPDX-License-Identifier: GPL-3.0-only -->
<!-- Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com> -->

(ref-commit-messages)=

# Generated commit messages

The format of the message that `lazysubmodules update --commit` writes,
for one submodule and for several, and the rules that keep it valid.

## Where messages come from

`update --commit` creates one commit per invocation with
`git commit -s`, and the message lists only the submodules whose recorded
state changes: their gitlink, lock entry or tracking keys. Submodules that
are only initialized, cloned, or checked out at the commit that `HEAD`
records are updated but not mentioned. When no submodule qualifies, no
commit is made and `update` prints `nothing to commit`.

`update --dry-run --commit` prints the subject that the commit would get:

```text
would commit "manifest: update kernel to v6.6.10"
```

The terminal interface uses the same messages for {kbd}`U`.

## One submodule

```text
manifest: update <name> to <ref>

Tracking mode: <mode> <configured ref>
Old: <commit> (<ref>)
New: <commit> (<ref>)

Signed-off-by: <user.name> <<user.email>>
```

On the demo:

```console
$ lazysubmodules update --commit kernel
kernel: v6.6.9 (8106f61) -> v6.6.10 (08dcd0d)
committed 9f79840667708019431339f48a62542240739f62
$ git log -1 --format=%B
manifest: update kernel to v6.6.10

Tracking mode: tag-pattern v6.6.*
Old: 8106f614767a (v6.6.9)
New: 08dcd0dc8f98 (v6.6.10)

Signed-off-by: Dana Developer <dana@example.org>
```

The commit SHA differs from run to run, because the demo's clock moves on
with every command. A submodule without a previous lock entry, from chapter
4 of the demo:

```{literalinclude} ../../examples/transcript.txt
:language: console
:start-at: "$ lazysubmodules update --commit legacy"
:end-before: "━━ 5."
```

The `[exit status 0: success]` line is printed by the demo, not by
LazySubmodules.

## Several submodules

```text
manifest: update <N> submodules

Submodule "<name>":
  Tracking mode: <mode> <configured ref>
  Old: <commit> (<ref>)
  New: <commit> (<ref>)

Submodule "<name>":
  ...

Signed-off-by: <user.name> <<user.email>>
```

The blocks follow the `.gitmodules` order. From chapter 8 of the demo:

```{literalinclude} ../../examples/transcript.txt
:language: console
:start-at: "$ lazysubmodules update --commit fpga.core sdk"
:end-before: "$ lazysubmodules verify fpga.core sdk"
```

The quoted `Submodule "<name>":` heading keeps Git from reading the last
block as a trailer block, like `Signed-off-by:`. `git commit -s` therefore
always separates the sign-off with a blank line.

## Lines

### Subject

| Case | Subject |
|---|---|
| One submodule | `manifest: update <name> to <ref>`, where `<ref>` is the new tag or branch, or the 12-character commit in commit mode |
| One submodule, subject too long or not valid | `manifest: update <name>` |
| One submodule, still too long or not valid | `manifest: update 1 submodule` |
| Several submodules | `manifest: update <N> submodules` |

A subject is not valid when it is longer than 75 characters, ends with
white space or one of `?:!.,;`, contains a character that is not printable,
or contains the word `WIP` in any case. Verified on the demo, with renamed
submodules:

```text
manifest: update firmware-kernel-for-the-evaluation-board-revision-c
manifest: update 1 submodule
manifest: update "tab\there" to v2.3.0
```

The first subject leaves out ` to v6.6.10`, which would make it 79
characters long. The second belongs to a submodule named `WIP`. The third
shows a quoted name.

### Tracking mode

`Tracking mode: <mode> <configured ref>` shows the configuration after the
update, as in `.gitmodules`. A previous configuration is not listed; the
diff of `.gitmodules` in the same commit shows it. In commit mode, the
configured ref is the full SHA:

```text
manifest: update crypto lib to c1d65e3a65f8

Tracking mode: commit c1d65e3a65f825464a005a24da69ce49714357ce
Old: 5da6927f70f5
New: c1d65e3a65f8
```

A change of mode, here from `tag-pattern v*` to `tag v2.9.0`, shows only
the new mode:

```text
manifest: update sdk to v2.9.0

Tracking mode: tag v2.9.0
Old: 63082039d4fa (v3.0.0-rc.2)
New: 68a87436fe8c (v2.9.0)
```

### Old and New

Commits are shown with 12 characters. The ref in parentheses is left out in
commit mode.

| Line | Meaning |
|---|---|
| `Old: <commit> (<ref>)` | `HEAD` of the superproject recorded `<commit>`, and the previous lock entry records the same commit with `<ref>` |
| `Old: <commit>` | The previous lock entry records another commit, so the old ref is unknown |
| `Old: <commit> (unlocked)` | There was no previous lock entry |
| `Old: none` | `HEAD` recorded no gitlink for the submodule, for example after `add` |
| `New: <commit> (<ref>)` | The target of the update |

Verified examples of the other `Old` forms: a gitlink that was committed by
hand without updating the lock file, and a submodule added with `add`:

```text
manifest: update u-boot to main

Tracking mode: branch main
Old: 8e9fc7a2511a
New: 8e9fc7a2511a (main)
```

```text
manifest: update third_party/sdk2 to v2.9.0

Tracking mode: tag-pattern v2.*
Old: none
New: 68a87436fe8c (v2.9.0)
```

## Names and refs

- **Quoting.** A name or ref that is not valid UTF-8, or contains quotes,
  backslashes or characters that are not printable, such as tabs or line
  separators, is quoted as in the other output, for example `"tab\there"`.
- **Headings** always quote the name. A space that another space follows is
  written as `\x20`, so a long heading can be split before a space:
  `Submodule "two\x20 spaces":`.

## Line length

A body line longer than 75 columns continues on the next line, indented by
two more spaces. The ref moves there, or in a heading the quoted name and
the colon. A ref or name that is still too long is split across several
such lines, and never after a space. From the demo, with a long submodule
name:

```text
manifest: update 2 submodules

Submodule
  "a-submodule-whose-name-is-far-too-long-to-fit-on-one-line-with-the-headi
  ng":
  Tracking mode: tag-pattern v6.6.*
  Old: 8106f614767a (v6.6.9)
  New: 08dcd0dc8f98 (v6.6.10)

Submodule "two\x20 spaces":
  Tracking mode: branch main
  Old: 29b295ca56c4 (main)
  New: 8e9fc7a2511a (main)

Signed-off-by: Dana Developer <dana@example.org>
```

## Commit message checks

With these rules, every generated message passes the commit message checks
of the LazySubmodules project itself (its `.gitlint` rules), whatever the
names and refs are:

- the subject has at most 75 characters, starts with the subsystem prefix
  `manifest:`, and does not end with punctuation or white space;
- no body line is longer than 75 columns;
- the body ends with a `Signed-off-by` line.

A superproject with its own commit rules can amend the commit, or use
`update` without `--commit` and commit by hand.

## Signing, hooks and failures

- **Sign-off.** `git commit -s` takes the `Signed-off-by` line from
  `user.name` and `user.email`.
- **Hooks** such as `commit-msg` run as configured, and so does commit
  signing with `commit.gpgSign`.
- **Failure.** When `git commit` fails, for example in a hook, the update
  stays staged and no commit is made. `update` exits with status 5.

## See also

- {ref}`guide-commit`: committing updates step by step.
- [`update`](cli/update.md): the options and the other output.
- {ref}`project-contributing`: the commit rules of this project.
