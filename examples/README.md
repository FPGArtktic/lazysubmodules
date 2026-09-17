<!-- SPDX-License-Identifier: GPL-3.0-only -->
<!-- Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com> -->

# Examples

These files come from [`scripts/demo.sh`](../scripts/demo.sh), a demo that
builds a superproject with fourteen submodules and runs LazySubmodules on
it, entirely offline.

| File | Content |
|---|---|
| [`transcript.txt`](transcript.txt) | Complete output of the demo: every command and its output, and the exit status of each `lazysubmodules` command |
| [`.gitmodules`](.gitmodules) | The superproject's `.gitmodules` at the end of the demo, with comments. It covers every tracking mode, the native `branch` key and other native keys |
| [`.lsm.lock`](.lsm.lock) | The matching lock file |

The two configuration files are hidden files; use `ls -a` to see them.
Their URLs point to `git.example.org` and are only illustrations; the
demo itself uses `git.example.invalid` (see below). Otherwise they are
exactly what the demo leaves behind, so the commits in `.lsm.lock` are
the real commits of the demo repositories. A test checks this.

## Running the demo

The demo needs Bash, Git 2.39 or later and a `lazysubmodules` binary:

```sh
scripts/build-in-container.sh build   # binary in bin/lazysubmodules
scripts/demo.sh                       # press Enter between the chapters
```

By default the demo uses `bin/lazysubmodules`, or else `lazysubmodules`
from `PATH`. `--binary PATH` selects another binary.

- **Offline and self-contained:** everything happens in a new temporary
  directory, which is removed at the end. Git runs without your
  configuration and may use only the file transport. Every submodule URL
  names `https://git.example.invalid/`, a host that does not exist, and the
  demo configuration rewrites it with `url.<base>.insteadOf` to local bare
  repositories, the way a company mirror is set up.
- **Checked:** every command must exit with the status the story expects,
  or the demo stops with exit status 1. `go test ./cmd/lazysubmodules`
  runs the demo against a freshly built binary (`TestDemo`).
- **Signed tag:** tag `v1.0.0` of the submodule `signed` is signed with an
  SSH key that `ssh-keygen` creates for the demo. Without `ssh-keygen`,
  the tag stays unsigned and its signature check fails too.

### Options

| Option | Effect |
|---|---|
| `--keep DIR` | Build the demo in `DIR`, which must be new or empty, and keep it |
| `--setup-only` | Build and describe the superproject, then stop; needs `--keep` |
| `--no-pause` | Do not wait for Enter between the chapters |
| `--transcript FILE` | Also write the output to `FILE`, with the demo directory shown as `$DEMO`; standard output shows the real directory |
| `--binary PATH` | The `lazysubmodules` binary to run |

### Exploring a kept demo

`--keep DIR` leaves the repositories in `DIR`: the superproject in
`DIR/firmware`, the upstream repositories in `DIR/mirror` (bare) and
`DIR/work` (where their history was written). `DIR/env.sh` sets up a shell
with the demo environment: the isolated Git configuration, the URL
rewriting, the fixed identity and `lazysubmodules` on `PATH`.

```sh
scripts/demo.sh --keep /tmp/lsm-demo --setup-only
. /tmp/lsm-demo/env.sh
cd /tmp/lsm-demo/firmware
lazysubmodules tui
```

With `--setup-only`, every submodule is still in its first state
(`behind`, `dirty`, `uninitialized`, `missing-ref`, ...). Without it, the
demo has resolved all of them, and every submodule is `ok`.

## The superproject

| Submodule | Path | Tracks | Situation at the start |
|---|---|---|---|
| `kernel` | `kernel` | tag pattern `v6.6.*` | Locked at `v6.6.9`; `v6.6.10` and the pre-releases `v6.6.11-rc1` and `v6.7-rc1` exist |
| `u-boot` | `bootloader/u-boot` | branch `main` | `origin/main` has moved on |
| `fpga.core` | `ip/fpga-core` | tag `v2.3.1` | Name with a dot; the demo moves the tag upstream |
| `crypto lib` | `libs/crypto lib` | a commit | Name and path with a space |
| `legacy` | `vendor/legacy` | nothing | Unmanaged |
| `tools` | `tools/nested` | branch `develop` | Its nested submodule is at another commit |
| `theme` | `docs/theme` | tag `v1.0.0` | Deinitialized; its repository in `.git/modules` is kept |
| `fresh` | `third_party/fresh` | tag pattern `v1.*` | Never cloned |
| `app` | `apps/app` | tag `v1.2.0` | Uncommitted change |
| `sdk` | `sdk` | tag pattern `v*` | Locked at the release candidate `v3.0.0-rc.2` |
| `mirror-lib` | `libs/mirror` | branch `stable` | A branch other than `main` |
| `broken` | `libs/broken` | tag `v9.9.9` | The tag does not exist |
| `signed` | `libs/signed` | tag `v1.0.0` | The tag is SSH-signed |
| `quirky` | `libs/quirky` | tag `v1.0.0` | `update = none`, `ignore = all`, `shallow = true` |

The chapters of the demo:

1. The superproject: history, `.gitmodules` and `.lsm.lock`.
2. `status`: every state at once.
3. `status --porcelain=v1`, and a script that uses it.
4. `set`: an unmanaged submodule is refused, then put under control.
5. `update --dry-run`, with and without `--include-prerelease`.
6. A refused `update` (exit status 3), and how to fix each problem.
7. `update --commit` for a selection: refused with unrelated staged
   changes, then the generated commit message with `Signed-off-by`.
8. A tag moved upstream: `fetch`, `drift` and `verify` (exit status 4),
   then accepting the new commit.
9. `update --fetch` clones a submodule through the mirror.
10. `verify --signatures`.
11. `foreach`, including a failing command (exit status 1).
12. The final state.

## Regenerating the files

The demo uses fixed identities and dates, so its output is the same in
every run. Only the demo directory differs, and the transcript shows it
as `$DEMO`. Regenerate the transcript with a current binary:

```sh
scripts/demo.sh --no-pause --transcript examples/transcript.txt
```

The file in the repository was made with `ssh-keygen` installed. Without
it, the lines about the signed tag differ. Git 2.39.5 and 2.55.0 give the
same transcript, but other Git versions may word their messages
differently. `TestDemo` therefore compares only the chapter titles, the
commands and the exit statuses with the transcript. It fails when the
story has changed but the transcript has not been regenerated.

When the story changes the final `.gitmodules` or `.lsm.lock`, copy them
from a kept demo (`DIR/firmware`), replace `git.example.invalid` with
`git.example.org` and keep the comments. `TestDemo` compares the
configuration values, ignoring comments.
