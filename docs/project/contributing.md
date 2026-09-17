<!-- SPDX-License-Identifier: GPL-3.0-only -->
<!-- Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com> -->

(project-contributing)=

# Contributing

A summary of how to build, test and propose a change. The complete
rules are in CONTRIBUTING.md in the repository.

::::{grid} 1 1 2 2
:gutter: 3

:::{grid-item-card} {octicon}`book` CONTRIBUTING.md
:link: https://github.com/FPGArtktic/lazysubmodules/blob/main/CONTRIBUTING.md
:class-card: lsm-card

Build targets, coding style, file headers, commit messages, tests,
dependencies and releases.
:::

:::{grid-item-card} {octicon}`issue-opened` Issue tracker
:link: https://github.com/FPGArtktic/lazysubmodules/issues
:class-card: lsm-card

Bug reports and feature requests, through issue forms.
:::

::::

## Before you start

- **License.** Contributions are licensed under the project license,
  `GPL-3.0-only`.
- **Sign-off.** Every commit needs a `Signed-off-by` line, which certifies
  the
  [Developer Certificate of Origin](https://github.com/FPGArtktic/lazysubmodules/blob/main/DCO).
  Use `git commit -s`, with your real name and a working e-mail address.
- **Security problems** are not reported in public issues; see
  [Security policy](security.md).

## Build and test

Every build step runs through one script, in a container with pinned
tools. It needs Linux on x86_64 and Podman (preferred) or Docker:

```sh
scripts/build-in-container.sh [-h] TARGET...
```

| Target | Action |
|---|---|
| `build` | Build the binary into `bin/` |
| `test` | `go test -race ./...` |
| `lint` | `golangci-lint`, `shellcheck` and the file header check |
| `coverage` | Tests with statement coverage; report in `coverage/` |
| `gitlint` | Check the commit messages of `GITLINT_RANGE` |
| `licenses` | Check the licenses of the Go dependencies |
| `snapshot` | Build unsigned release archives and packages into `dist/` |
| `test-compat` | Run the tests with Git 2.39.5, the oldest supported Git |
| `package-test` | Install the snapshot packages in Debian and Fedora and check them |
| `docs` | Build this documentation site with Sphinx; a warning fails it |

Before you propose a commit, run at least:

```sh
scripts/build-in-container.sh lint test
```

The full CI sequence is:

```sh
scripts/build-in-container.sh image gitlint lint licenses test build snapshot package-test
scripts/build-in-container.sh image coverage
scripts/build-in-container.sh test-compat
scripts/build-in-container.sh docs
```

Most containers run without network access; Go dependencies come from the
committed `vendor/` directory. The exceptions are `package-test`, whose
Debian and Fedora containers download the `git` dependency of the
packages, and `docs`, which installs the pinned Python requirements on
every run. For quick loops, a host Go toolchain
(1.27.1 or later) works too: `go build ./...`, `go vet ./...` and
`go test -race ./...`. The container targets are the authoritative gate.

## Commit messages

Messages follow the Linux kernel format and are checked by gitlint:

```text
<subsystem>: <imperative summary>

Explain the problem and why the change is needed. Wrap at 75 columns.

Signed-off-by: Name <email>
```

- **Subject:** at most 75 characters, imperative mood, no trailing
  period, and one of the prefixes below.
- **Body:** required for non-trivial changes; it explains what was wrong
  and why.
- **One logical change per commit.** Every commit builds and passes its
  tests.
- **`Fixes:`** names the commit that introduced a bug, with 12 SHA
  characters and its subject.

| Prefix | Scope |
|---|---|
| `cli` | `cmd/lazysubmodules` |
| `core` | `internal/core` |
| `git` | `internal/git` |
| `manifest` | `internal/manifest` |
| `lock` | `internal/lock` |
| `porcelain` | `internal/porcelain` |
| `tui` | `internal/tui` |
| `build` | Go module, `vendor/`, `Containerfile`, linter configuration |
| `scripts` | `scripts/`, including the demo |
| `ci` | `.github/workflows/ci.yml` |
| `release` | `.goreleaser.yaml`, the release workflow, `packaging/` |
| `docs` | `README.md`, `CONTRIBUTING.md`, `SECURITY.md`, `LICENSE`, `DCO`, `docs/`, `examples/`, the issue forms and the pull request template |

To check your branch locally:

```sh
GITLINT_RANGE=origin/main..HEAD scripts/build-in-container.sh gitlint
```

## Code and tests

- **Architecture.** All business logic lives in `internal/core`; the
  terminal interface and the command line only call it. Git is used only
  through the `git` command, never through a shell. See
  [How LazySubmodules works](../explanation/how-it-works.md).
- **Style.** The Linux kernel coding style, adapted to Go: `gofmt`, 100
  columns, documented exported identifiers, short functions.
- **File headers.** Every authored file starts with the SPDX identifier
  and the copyright line; `scripts/check-headers.sh` checks them.
- **No network in tests.** Remotes are local bare repositories, and Git
  may use only the file transport.
- **Isolation.** Tests run without the user and system Git
  configuration, with fixed identities and dates.
- **Golden files** under `testdata/` hold the help texts, the status
  table, the porcelain output and the terminal interface screens. Update
  them with the `-update` flag of the package's tests and review the
  diff.
- **Stable interfaces.** The porcelain v1 format, the exit codes, the
  keys `lsm-mode` and `lsm-ref`, and the network policy must not change.

## The demo and the recordings

- **`scripts/demo.sh`** is documentation and an end-to-end test at once.
  `TestDemo` runs it against a freshly built binary and compares the
  result with `examples/`. When the story or its output changes,
  regenerate the transcript:

  ```sh
  scripts/demo.sh --no-pause --transcript examples/transcript.txt
  ```

  Several pages of this site include parts of the transcript, so check
  the documentation build afterwards; see
  [Building the documentation](documentation.md).
- **`scripts/record-demos.sh`** records the GIFs in `docs/demo/` with VHS
  0.11.0, in a container, on a terminal of 110 columns and 30 rows in the
  Catppuccin Mocha theme. Record again when the output that a GIF shows
  changes. [`docs/demo/README.md`](https://github.com/FPGArtktic/lazysubmodules/blob/main/docs/demo/README.md)
  describes the tapes.

## Issues

The issue forms ask for the details that help: the output of
`lazysubmodules version`, the installation method, the Git version, and
optionally the output of `lazysubmodules status --porcelain=v1` and the
submodule configuration.

## Pull requests

CI runs the full sequence above for every pull request. Third-party
GitHub Actions are pinned by commit SHA. The pull request template lists
the rules of this page as a checklist.
