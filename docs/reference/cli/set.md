<!-- SPDX-License-Identifier: GPL-3.0-only -->
<!-- Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com> -->

(ref-cli-set)=

# `set`

Change the tracking mode and ref of a submodule in `.gitmodules`, and
nothing else; `update` applies the change afterwards.

## Synopsis

```text
lazysubmodules set <name> (--branch B | --tag T | --tag-pattern P | --commit SHA)
```

`set` works offline.

## Help text

```{literalinclude} _generated/set.txt
:language: text
:lines: 5-
```

## Arguments and options

| Argument or option | Effect |
|---|---|
| `<name>` | The submodule name from `.gitmodules`; exactly one |
| `--branch <branch>` | Track the tip of `origin/<branch>` |
| `--tag <tag>` | Track the tag |
| `--tag-pattern <pattern>` | Track the highest version tag that matches the glob |
| `--commit <commit>` | Track a commit, given with 7 to 40 lowercase hexadecimal digits (64 in SHA-256 repositories) |

Exactly one tracking option is required, given once.

## Behaviour

- **Only `.gitmodules`.** `set` writes `lsm-mode` and `lsm-ref` for the
  submodule. It does not touch the submodule, the lock file or the index.
  Run [`update`](update.md) afterwards; it stages `.gitmodules` together
  with the lock file and the gitlink.
- **Native `branch` key.** In branch mode, `set` also writes the native
  `branch` key; for the other modes it removes it. See
  {ref}`ref-files-gitmodules`.
- **Unmanaged submodules.** `set` is how an unmanaged submodule comes under
  management. It is the only command besides `status` that accepts an
  unmanaged name.
- **Validation of names.** Branch and tag names are checked with
  `git check-ref-format`. A pattern must be a valid tag name once its glob
  characters `*`, `?`, `[` and `]` are replaced. No value may be empty,
  start with `-`, or contain white space or control characters.
- **No lookup of branches and tags.** `set` does not check that a branch or
  tag exists; `status` then shows
  [missing-ref]{.lsm-state .lsm-state-missing-ref} until the ref has been
  fetched.
- **Commits.** In commit mode, the commit must exist in the submodule. An
  abbreviated SHA is expanded, and `.gitmodules` always stores the full
  SHA.

## Output

One line: the name, the mode and the stored ref.

```console
$ lazysubmodules set kernel --tag-pattern 'v6.*'
kernel: tracks tag-pattern v6.*
$ git diff -- .gitmodules
diff --git a/.gitmodules b/.gitmodules
index 3afeb0c..d9c9f2d 100644
--- a/.gitmodules
+++ b/.gitmodules
@@ -2,7 +2,7 @@
 	path = kernel
 	url = https://git.example.invalid/linux.git
 	lsm-mode = tag-pattern
-	lsm-ref = v6.6.*
+	lsm-ref = v6.*
 [submodule "u-boot"]
 	path = bootloader/u-boot
 	url = https://git.example.invalid/u-boot.git
```

An abbreviated commit is shown expanded:

```console
$ lazysubmodules set 'crypto lib' --commit c1d65e3
crypto lib: tracks commit c1d65e3a65f825464a005a24da69ce49714357ce
```

## Errors

| Message | Status |
|---|---|
| `set: missing <name>` | 2 |
| `set: unexpected argument "u-boot"` | 2 |
| `set: one of --branch, --tag, --tag-pattern or --commit is required` | 2 |
| `set: only one of --branch, --tag, --tag-pattern or --commit may be given` | 2 |
| `kernel: invalid tag "bad..ref": not a valid tag name` | 2 |
| `u-boot: invalid branch "-x": starts with "-"` | 2 |
| `kernel: invalid branch "feature branch": contains white space or control characters` | 2 |
| `kernel: invalid tag pattern "": empty value` | 2 |
| `crypto lib: invalid commit "5da69": must have 7 to 40 hexadecimal digits` | 2 |
| `crypto lib: invalid commit "5DA6927": must consist of lowercase hexadecimal digits` | 2 |
| `u-boot: invalid commit "1234567": commit 1234567 does not exist or is ambiguous` | 2 |
| `nosuch: no such submodule` | 1 |

Each message starts with `lazysubmodules: `. The usage errors in the first
four rows are followed by `Run 'lazysubmodules help' for usage.`

## Exit status

| Status | When |
|---|---|
| 0 | The tracking configuration was written |
| 1 | The submodule does not exist, or `.gitmodules` cannot be used |
| 2 | A usage error or an invalid value |
| 5 | A `git` command failed |

## Examples

```sh
# Put an unmanaged submodule under management, then record it.
lazysubmodules set legacy --branch main
lazysubmodules update --commit legacy

# Follow every stable 6.x release instead of 6.6.x only.
lazysubmodules set kernel --tag-pattern 'v6.*'
lazysubmodules update --dry-run kernel

# Pin a commit.
lazysubmodules set 'crypto lib' --commit 5da6927
```

## See also

- {ref}`guide-change-tracking`: switching modes step by step.
- {ref}`expl-resolution`: what each mode selects.
- [`update`](update.md): applying the new configuration.
- {ref}`ref-files-gitmodules`: the keys that `set` writes.
