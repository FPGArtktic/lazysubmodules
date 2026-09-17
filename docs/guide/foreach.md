<!-- SPDX-License-Identifier: GPL-3.0-only -->
<!-- Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com> -->

(guide-foreach)=

# Run a command in every submodule

`foreach` runs a program in each checked-out managed submodule, with the
submodule's tracking configuration in its environment.

```text
lazysubmodules foreach -- <command> [args...]
```

## How it runs

- **Where:** in every managed submodule that is checked out, in
  `.gitmodules` order, with the submodule as the working directory.
  Unmanaged submodules are left out.
- **Skipped submodules:** an uninitialized submodule is skipped with a
  note on standard error, and `foreach` goes on:

  ```console
  $ lazysubmodules foreach -- true
  skipping theme: submodule is not checked out
  skipping fresh: submodule is not checked out
  ```

- **No shell:** the command is executed directly. Use `sh -c '…'` when you
  need variables, pipes or `&&`.
- **`--`:** may be left out when the command does not start with `-`.
- **Failure:** `foreach` stops at the first command that fails, names the
  submodule, and exits with status 1.
- **Network:** `foreach` itself uses none, but the commands you run may.

## Environment

The command gets these variables in addition to the inherited
environment:

| Variable | Value | Example for `u-boot` |
|---|---|---|
| `name` | Submodule name | `u-boot` |
| `sm_path` | Path as recorded in `.gitmodules` | `bootloader/u-boot` |
| `displaypath` | Path for display, as in `git submodule foreach` | `bootloader/u-boot` |
| `sha1` | Commit checked out in the submodule | `29b295ca56c4418418d7bc58f14454e49bc51dbb` |
| `toplevel` | Absolute path of the superproject | `$DEMO/firmware` |
| `LSM_MODE` | Tracking mode (`lsm-mode`) | `branch` |
| `LSM_REF` | Configured ref (`lsm-ref`) | `main` |

The first five have the same names as in `git submodule foreach`, so
existing snippets keep working. One of them carries a different value:
`sha1` is the commit checked out in the submodule, where git sets it to
the commit the superproject records. The two differ whenever the
submodule is checked out at another commit than its gitlink, so a
borrowed snippet that reads `$sha1` as the gitlink reads the checkout
instead.

## Examples

Print the tracking mode and the subject of the checked-out commit:

```{literalinclude} ../../examples/transcript.txt
:language: console
:start-at: "$ lazysubmodules foreach -- sh -c"
:end-at: "[exit status 0: success]"
```

The `[exit status N: …]` lines come from the demo, not from
LazySubmodules. A failing command ends `foreach`. Here `git describe`
finds no tag at the commit of `u-boot`, which follows a branch:

```{literalinclude} ../../examples/transcript.txt
:language: console
:start-at: "$ lazysubmodules foreach -- git describe --tags --exact-match"
:end-at: "[exit status 1: error]"
```

The `fatal:` line comes from Git, and the last line from `foreach`. To
keep going, handle the failure inside a shell:

```console
$ lazysubmodules foreach -- sh -c 'git describe --tags 2>/dev/null || echo "$name: no tag"'
v6.6.9
u-boot: no tag
v2.3.1
crypto lib: no tag
tools: no tag
skipping theme: submodule is not checked out
skipping fresh: submodule is not checked out
v1.2.0
v3.0.0-rc.2
mirror-lib: no tag
v1.0.0
v1.0.0
v1.0.0
```

This ran on the demo in its first state, so `theme` and `fresh` were
skipped; the two notes went to standard error.

Other useful commands:

```sh
# Show which submodules follow which ref.
lazysubmodules foreach -- sh -c 'echo "$name: $LSM_MODE $LSM_REF at $sha1"'

# Describe every checkout, falling back to the abbreviated commit.
lazysubmodules foreach -- git describe --tags --always
```

A command that starts with `-` needs the `--`. Without it, `foreach`
reads the word as its own option and exits with a usage error:

```console
$ lazysubmodules foreach -x
lazysubmodules: foreach: flag provided but not defined: -x
Run 'lazysubmodules help' for usage.
```

(guide-foreach-nested)=

## Nested submodules

LazySubmodules does not manage submodules inside a managed submodule:
`update` neither initializes nor updates them. After an update, a nested
submodule may stay at a commit other than the one its parent records.
That alone does not make the parent [dirty]{.lsm-state .lsm-state-dirty};
modified files inside the nested submodule do.

In the demo, `tools` has such a nested submodule, and plain Git shows the
difference as a lowercase ` m`:

```console
$ git status --short
 m apps/app
 m tools/nested
$ git -C tools/nested submodule status
+ddb893ae184f61824eb0d98f2f1256f99f9313e6 inner (heads/main)
```

`foreach` brings nested submodules to the commits their parents record:

```console
$ lazysubmodules foreach -- git submodule update --init --recursive
Submodule path 'inner': checked out 'b758edb2c3c2e2cad8c28354e27510aece113c5c'
skipping theme: submodule is not checked out
skipping fresh: submodule is not checked out
$ git -C tools/nested submodule status
 b758edb2c3c2e2cad8c28354e27510aece113c5c inner (b758edb)
```

Run it after every `update` that moves a submodule with nested
submodules. The nested submodules keep plain Git semantics: they follow
the gitlinks of their parent, not tracking modes of their own.

## See also

- {ref}`ref-cli-foreach`: the complete reference.
- {ref}`ref-runtime`: environment variables and streams.
- [Security policy](../project/security.md): the commands you run with
  `foreach` are your own responsibility.
