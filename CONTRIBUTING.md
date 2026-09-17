<!-- SPDX-License-Identifier: GPL-3.0-only -->
<!-- Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com> -->

# Contributing to LazySubmodules

Thank you for helping. This guide covers building, testing, code style,
commit messages and dependencies. The rules are enforced by the same
containerized tooling on developer machines and in CI, so a change that
passes locally passes in CI too.

By contributing you agree that your work is licensed under the project
license, `GPL-3.0-only` (see [LICENSE](LICENSE)), and you certify the
[Developer Certificate of Origin](DCO) by signing off every commit.

## Contents

- [Prerequisites](#prerequisites)
- [Building and testing](#building-and-testing)
- [Build image](#build-image)
- [Architecture](#architecture)
- [Coding style](#coding-style)
- [File headers](#file-headers)
- [Commit messages](#commit-messages)
- [Testing rules](#testing-rules)
- [Test coverage](#test-coverage)
- [Demo and recordings](#demo-and-recordings)
- [Dependencies](#dependencies)
- [Third-party notices](#third-party-notices)
- [AUR recipe](#aur-recipe)
- [Community files](#community-files)
- [Stable interfaces](#stable-interfaces)
- [Pull requests and releases](#pull-requests-and-releases)

## Prerequisites

- Linux on x86_64 (`amd64`) for the container targets; the build image
  exists for x86_64 only (see [Build image](#build-image)).
- Git, Bash, `tar` and GNU coreutils.
- Podman (preferred, rootless works) or Docker with BuildKit, its default
  builder (the script sets `DOCKER_BUILDKIT=1`; the `Containerfile` uses
  `RUN --mount`, which the legacy builder lacks).
- Optional, for quick local loops: Go 1.27.1 or later.
- Optional, for the commit-msg hook: `gitlint` 0.19.1 (see
  [Checking commit messages locally](#checking-commit-messages-locally)).
- For `scripts/update-builder-pins.sh`: `curl` and network access.
- For `scripts/demo.sh`: Bash, Git 2.39 or later and, for the signed tag
  of the demo, `ssh-keygen` (see
  [Demo and recordings](#demo-and-recordings)).
- For `scripts/record-demos.sh`: Podman or Docker, and network access
  once, to build the recording image.
- For the AUR recipe: an Arch Linux system or container with `base-devel`
  (see [AUR recipe](#aur-recipe)).

All other tools (Go, GoReleaser, golangci-lint, cosign, syft, gitlint,
shellcheck, go-licenses) come at pinned versions from the build image
defined in the `Containerfile`. No tool is downloaded while the project is
built.

## Building and testing

Every build step runs through one script:

```sh
scripts/build-in-container.sh [-h] TARGET...
```

Targets run in the given order and the script stops at the first failure:

| Target | Action |
|---|---|
| `build` | `go build` for the host architecture; binaries land in `bin/` |
| `test` | `go test -race ./...` |
| `coverage` | `go test -race ./...` with statement coverage; report in `coverage/` (see [Test coverage](#test-coverage)) |
| `lint` | `golangci-lint run`, `shellcheck` on `scripts/*.sh`, `docs/demo/*.sh` and `packaging/aur/*/PKGBUILD`, and `scripts/check-headers.sh` |
| `gitlint` | Validate the commit messages selected by `GITLINT_RANGE` |
| `licenses` | `go-licenses check ./...` (see [Dependencies](#dependencies)) |
| `snapshot` | `goreleaser release --snapshot --clean`, unsigned, output in `dist/` |
| `release` | `goreleaser release --clean` (CI only: needs network and credentials) |
| `test-compat` | `go test -race ./...` with Git 2.39.5, the oldest supported Git |
| `package-test` | Install the `dist/` `.deb` in Debian and the `.rpm` in Fedora, for the host architecture, run `lazysubmodules version` and `lsm version`, and check the installed license notices (see [Third-party notices](#third-party-notices)) |
| `image` | Build the build image from `Containerfile`, unless it exists |
| `image-tag` | Print the build image reference |
| `image-save` | Save the build image to the archive `IMAGE_ARCHIVE` |
| `image-load` | Check that the archive `IMAGE_ARCHIVE` holds the build image, then load it |

The targets from `build` to `release` run in the build image. `test-compat`
uses the official `golang:1.27.1-bookworm` image (Debian bookworm ships Git
2.39.5), and `package-test` uses `debian:trixie-slim` and `fedora:44`; all
three are pinned by digest in the script. `package-test` needs the packages
of a previous `snapshot`, and it fails when `dist/` has none. It installs
only the packages for the host architecture, so CI tests the `amd64`
packages; the `arm64` ones are built but not installed.

Before you propose a commit, run at least:

```sh
scripts/build-in-container.sh lint test
```

The full CI sequence is:

```sh
scripts/build-in-container.sh image gitlint lint licenses test build snapshot package-test
scripts/build-in-container.sh image coverage
scripts/build-in-container.sh test-compat
```

How the script behaves:

- **Engine:** `CONTAINER_ENGINE` selects the engine; the default is
  `podman`, with `docker` as the fallback. Rootless Podman runs with
  `--userns=keep-id`, so files written to the repository belong to you.
- **User:** the container user has a passwd entry with the home directory
  `/tmp`, which is also `HOME`. `ssh` and `ssh-keygen` (used by tests and by
  AUR publishing) read the home directory from that entry, so they never
  write into the checkout. Podman gets the entry with `--passwd-entry`;
  rootful Docker gets a copy of the image's passwd file with the entry
  added.
- **Image:** every target that runs in the build image, and `image` itself,
  builds it only when it is missing. The image tag is derived from the
  `Containerfile` hash, so an edited `Containerfile` is rebuilt
  automatically, and `image-tag` prints the reference (CI uses it as the
  cache key). An existing image is never rebuilt: an image restored with
  `image-load` has no build cache, so a rebuild would start from scratch. To
  rebuild an unchanged `Containerfile` anyway, remove the image first:

  ```sh
  podman rmi "$(scripts/build-in-container.sh image-tag)"
  ```

- **Image archive:** `image-save` and `image-load` need `IMAGE_ARCHIVE`; a
  relative path is relative to the current directory. Both check it before
  any target runs.
- **Offline:** the containers of all targets except `release` and
  `package-test` run with `--network=none`. Dependencies come from the
  committed `vendor/` directory (`GOFLAGS=-mod=vendor`), and
  `GOTOOLCHAIN=local` prevents Go toolchain downloads.
- **Images need the network when missing:** building the build image
  (`image`, or any target that runs in it while the image is missing)
  downloads the toolchain, and `test-compat` and `package-test` pull their
  images. On a machine without network access, load the build image with
  `image-load` and pull the `golang` image of `test-compat` beforehand.
- **Network for `package-test`:** `apt` and `dnf` download the `git`
  dependency of the packages from the distribution mirrors.
- **cgo:** builds use `CGO_ENABLED=0`. The race detector needs cgo, so
  `test`, `coverage` and `test-compat` enable it; `gcc` comes with the
  images.
- **Mounts:** the repository is mounted with `:Z` for SELinux hosts. The
  cache volumes are shared by every run and use the shared label `:z`
  instead, because a private label would be applied again, recursively, on
  each run.
- **CI annotations:** in GitHub Actions, a failed image build or target
  also prints an error annotation with the last 25 lines of its output.
  Annotations of a public repository are readable without signing in,
  unlike the job logs.
- **Init process:** the build image runs every command under `catatonit`,
  which reaps orphaned processes. Without it the command itself is PID 1,
  and the zombies of detached git processes (such as automatic
  maintenance) pile up until the container's process limit is reached.
- **Worktrees:** in a linked worktree (`git worktree add`), `.git` is a
  file that points to a git directory outside the checkout. The script
  mounts that directory and the repository's common git directory, with the
  shared label `:z`, at the paths the pointers lead to inside the container,
  so `lint`, `gitlint`, `snapshot` and `release` work there too. Those four
  targets refuse to run where git cannot work in the container: in a
  submodule checkout, whose git directory names the work tree relative to
  the host layout (`core.worktree`), and when a pointer leads into `/src`,
  where the checkout is mounted (for example a worktree with relative paths
  next to a repository named `src`). `build`, `test` and the other targets
  still work there.
- **Release:** `release` passes `GITHUB_TOKEN`,
  `ACTIONS_ID_TOKEN_REQUEST_URL`, `ACTIONS_ID_TOKEN_REQUEST_TOKEN`,
  `AUR_KEY` and `GITHUB_STEP_SUMMARY` into the container when they are
  set. GoReleaser reads no other `GITHUB_*` variable. The image pins the
  SSH host key of `aur.archlinux.org`.
- **Caches:** Go and linter caches live in the named volumes
  `lazysubmodules-go-build`, `lazysubmodules-go-mod` and
  `lazysubmodules-golangci-lint`. Remove them with `podman volume rm` (or
  `docker volume rm`) to start from scratch.
- **gitlint range:** `GITLINT_RANGE` is passed to `gitlint --commits`.
  `A..B` lints the commits after `A` up to `B`, a single ref lints its
  whole history, and the default is `HEAD`. To lint only your branch:

  ```sh
  GITLINT_RANGE=origin/main..HEAD scripts/build-in-container.sh gitlint
  ```

Deliberate choices, for the reasons given above:

- `test`, `coverage` and `test-compat` run with `CGO_ENABLED=1`, which
  `-race` requires; everything else builds with `CGO_ENABLED=0`.
- The cache volumes use `:z`; only the repository mount uses `:Z`.
- `snapshot` adds `--skip=sign`: keyless signing needs the identity token
  that only a CI release run has.
- `release` passes the `GITHUB_*` variables GoReleaser actually reads
  (`GITHUB_TOKEN`, `GITHUB_STEP_SUMMARY`) instead of the whole `GITHUB_*`
  context, plus `AUR_KEY`.
- `test-compat` mounts no cache volumes: the `golang` image lacks the
  world-writable cache directories that make fresh volumes writable, so its
  build cache lives in the container and is discarded with it.
- `package-test` runs the distribution images as their own root user, with
  `dist/` mounted read-only, because installing packages needs root.

For quick edit-compile-test loops you can use a host Go toolchain as well:

```sh
go build ./...
go vet ./...
go test -race ./...
```

The container targets are the authoritative gate.

## Build image

The build image is based on **Arch Linux**. Arch packages current releases
of Go and of every tool the build needs, and two pins make the image
reproducible even though Arch is a rolling release:

- **Arch Linux Archive snapshot.** The base image is a dated
  `archlinux:base-devel` tag, pinned by digest. pacman then uses the
  [Arch Linux Archive](https://archive.archlinux.org/) snapshot of the same
  day as its only server, upgrades the image to exactly that snapshot and
  installs the tools from it. A snapshot never changes, so a rebuild gets
  the same package versions, and every package is still verified against
  its signature.
- **Pinned AUR commits.** GoReleaser (`goreleaser-bin`) and gitlint are not
  in the official repositories. Their recipes are fetched by commit ID from
  the official GitHub mirror of the AUR
  (`https://github.com/archlinux/aur.git`, one branch per package) and
  built with `makepkg` as an unprivileged user in a separate build stage.
  `makepkg` checks the source checksums and runs gitlint's test suite. The
  GoReleaser checksum must also match the upstream `checksums.txt`, and the
  packaged binary must be identical to the upstream one.

go-licenses publishes no release binaries. The build image builds it from
source with `go install` at a pinned version, which the Go checksum
database verifies, rather than installing the `go-licenses` package of
Arch Linux; the [AUR recipe](#aur-recipe) uses that package. All pins are
`ARG` lines in the `Containerfile`.

The official Arch Linux image exists for x86_64 only, so the build image
does too: the script refuses to build or run it on other architectures. The
release binaries and packages are still built for `amd64` and `arm64`.

### Updating the pins

`scripts/update-builder-pins.sh` resolves the newest dated
`archlinux:base-devel` tag and its digest, the matching archive date, the
newest commits of the AUR packages and the newest go-licenses release,
checks them, and rewrites the pins in the `Containerfile`. It changes
nothing when the pins are current.

```sh
scripts/update-builder-pins.sh --dry-run   # show what would change
scripts/update-builder-pins.sh
git diff Containerfile                     # review, including the AUR recipes
scripts/build-in-container.sh image gitlint lint licenses test build snapshot package-test
```

Review what changed in the AUR recipes between the old and the new commits
(`https://github.com/archlinux/aur/compare/<old>...<new>`), then commit the
`Containerfile` alone with the `build:` prefix, for example
`build: update build image to the 2026-09-13 Arch snapshot`.

Update the pins regularly: newer pins pick up tool releases and security
fixes, and pacman may reject the signatures of old packages once the
packager key has expired, even though the archive keeps the packages. The
`golang`, `debian` and `fedora` images used by `test-compat` and
`package-test` are pinned by digest in `scripts/build-in-container.sh` and
are updated by hand.

## Architecture

| Layer | Responsibility |
|---|---|
| `internal/git` | Thin wrapper over the `git` binary. No business logic |
| `internal/manifest` | Read and write the `.gitmodules` keys |
| `internal/lock` | Read and write `.lsm.lock` |
| `internal/core` | Resolution, update, status, verify. All business logic |
| `internal/porcelain` | Stable machine-readable output |
| `internal/tui` | Bubble Tea model, update, view. Calls `internal/core` only |
| `cmd/lazysubmodules` | CLI entry point, flag parsing, exit codes |

Rules:

- The TUI contains **no business logic**. Every action is a call into
  `internal/core`.
- Git is accessed only through the `git` CLI via `os/exec`. `go-git`,
  `libgit2` and `gitoxide` are not used.
- `internal/git` never invokes a shell (`sh -c`). Arguments are passed as a
  slice, and `git` is located with `exec.LookPath`.
- Every `git` invocation sets `LC_ALL=C`, so that output parsing does not
  depend on the locale.
- Paths use `path/filepath`. Paths stored in `.gitmodules` use `/`.
- User-controlled strings (refs, patterns, paths, URLs) are validated before
  they reach `git`: no empty values, no leading `-`, no control characters.
- The minimum supported Git version is 2.39. Do not use newer Git features,
  and use the classic `git config` options (`--get`, `--unset-all`,
  `--list`), not the subcommands added in Git 2.46.
- **No implicit network access.** Only `fetch`, `update --fetch` and `add`
  may use the network (see the network policy in [README.md](README.md)).

## Coding style

The project follows the Linux kernel process documentation, adapted to Go:

- [`Documentation/process/coding-style.rst`][coding-style]
- [`Documentation/process/submitting-patches.rst`][submitting-patches]
- [`Documentation/process/maintainer-handbooks.rst`][maintainer-handbooks]
- [`Documentation/doc-guide/kernel-doc.rst`][kernel-doc]

[coding-style]: https://docs.kernel.org/process/coding-style.html
[submitting-patches]: https://docs.kernel.org/process/submitting-patches.html
[maintainer-handbooks]: https://docs.kernel.org/process/maintainer-handbooks.html
[kernel-doc]: https://docs.kernel.org/doc-guide/kernel-doc.html

### Go adaptation

| Kernel rule | Applied rule |
|---|---|
| Tab indentation | Enforced by `gofmt` |
| 80 columns (soft 100) | 100 columns, linter `lll` |
| `snake_case` names | **Go naming conventions apply** |
| `/* */` comments | **`//` comments apply** |
| kernel-doc | godoc with `Context:` and `Return:` sections |
| Short functions, max 3–4 indent levels | Enforced by `gocognit`, `nestif`, `funlen` |
| Comments explain what and why, not how | Mandatory |
| SPDX header on first line | Mandatory in every file |
| English comments | Mandatory |
| No dead code, no commented-out code | Mandatory |

### Go example

Every exported identifier has a godoc comment. Exported functions and
methods also describe their calling context and return values:

```go
// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

// Package git is a thin wrapper over the git command line tool.
package git

// ResolveTag returns the commit SHA referenced by a tag in a submodule.
//
// Annotated tags are dereferenced to the underlying commit. The function
// does not access the network; only local refs are consulted.
//
// Context: the submodule must be initialized.
// Return: full commit SHA, or ErrTagNotFound.
func ResolveTag(ctx context.Context, path, tag string) (string, error) {
	// ...
}
```

### Go rules

- Wrap errors with `fmt.Errorf("...: %w", err)`. Each package keeps its
  sentinel errors in `errors.go`.
- `context.Context` is the first argument of every function that runs `git`.
- No global mutable state. Sentinel error variables are fine; the version
  variables set with `-ldflags` are the only other exception.
- No `panic` outside `main` initialization.
- No `init()` with side effects.
- Keep functions short: `funlen` allows 80 lines or 50 statements,
  `gocognit` a complexity of 20, `nestif` a nesting complexity of 4.
- Doc comments end with a period (`godot`).
- A `//nolint` directive must name the linter and give a reason, for example
  `//nolint:gochecknoglobals // set by -ldflags`.
- The command line uses the standard `flag` package.

The complete linter configuration is in `.golangci.yml`.

### Shell scripts

- First line `#!/usr/bin/env bash`, followed by the header (see
  [File headers](#file-headers)).
- A header comment gives the script name, purpose and usage.
- Tab indentation.
- The opening brace of a function goes on a new line.
- `set -euo pipefail`.
- `shellcheck` reports zero warnings. `lint` checks `scripts/*.sh`,
  `docs/demo/*.sh` and the AUR recipes; add a shell script in another
  directory to the list in `target_lint` of
  `scripts/build-in-container.sh`.
- Variables are quoted; constants are `readonly`.

## File headers

Every file we author starts with the SPDX license identifier and the
copyright line, written exactly as shown below.

Go files (`*.go`, `go.mod`) use lines 1–2, and line 3 stays empty. The
header is its own comment group, separate from the package documentation,
as in the example above:

```go
// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>
```

Shell scripts (`*.sh`) put the header on lines 2–3, after the shebang:

```sh
#!/usr/bin/env bash
# SPDX-License-Identifier: GPL-3.0-only
# Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>
```

Other files that start with a `#!` line, whatever the interpreter, have
the header on lines 2–3 as well.

These files use lines 1–2:

```sh
# SPDX-License-Identifier: GPL-3.0-only
# Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>
```

- YAML, TOML, INI and similar configuration files: `Containerfile`,
  `.gitignore`, `.gitlint`, ...;
- the example `.gitmodules` and `.lsm.lock` in `examples/`;
- text files (`*.txt`), such as the demo transcript;
- the AUR recipe (`PKGBUILD`);
- VHS tapes (`*.tape`).

Markdown files and SVG images use HTML comments on lines 1–2; an SVG image
therefore has no XML declaration:

```html
<!-- SPDX-License-Identifier: GPL-3.0-only -->
<!-- Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com> -->
```

Exempt files:

- `LICENSE`, `DCO`, `go.sum` and everything under `vendor/`;
- test data under `testdata/`, including `*.golden` files.

Files that cannot have a header are checked in another way:

- **Images** have no comments. GIF images are allowed only directly in
  `docs/demo/` and PNG images only directly in `docs/assets/`, and they
  must start with the GIF or PNG signature. The License section of the
  README covers them.
- **AUR recipe directories** under `packaging/aur/`: `LICENSE` must be an
  exact copy of the top-level `LICENSE`, and `.SRCINFO`, which `makepkg`
  generates, must start with `pkgbase = `.

`scripts/check-headers.sh`, which is part of the `lint` target, checks every
tracked or new file. It reports files of an unknown type, so a new kind of
file needs a header rule in that script.

## Commit messages

Commit messages follow the Linux kernel format and are validated by
`gitlint` with the rules in `.gitlint`.

### Format

```text
<subsystem>: <imperative summary>

Explain the problem and why the change is needed. Wrap at 75 columns.

Fixes: 1a2b3c4d5e6f ("git: resolve annotated tags")
Signed-off-by: Name <email>
```

- **Subject:** at most 75 characters, imperative mood ("add", not "added"),
  no trailing period, and it starts with a subsystem prefix.
- **Body:** required for non-trivial changes and wrapped at 75 columns. It
  explains what was wrong and why the change is needed, not only what
  changed.
- **Sign-off:** a `Signed-off-by:` line is required. Use `git commit -s`.
  With it you certify the [Developer Certificate of Origin](DCO). Use your
  real name and a working e-mail address.
- **One logical change per commit.** Do not mix refactoring with behaviour
  changes. Every commit should build and pass its tests.

### Subsystem prefixes

| Prefix | Typical scope |
|---|---|
| `cli` | `cmd/lazysubmodules` |
| `core` | `internal/core` |
| `git` | `internal/git` |
| `manifest` | `internal/manifest` |
| `lock` | `internal/lock` |
| `porcelain` | `internal/porcelain` |
| `tui` | `internal/tui` |
| `build` | Go module, `vendor/`, `Containerfile` and its pins, linter configuration |
| `scripts` | `scripts/`, including the demo and the recording script |
| `ci` | `.github/workflows/ci.yml` |
| `release` | `.goreleaser.yaml`, `.github/workflows/release.yml`, `packaging/` |
| `docs` | `README.md`, `CONTRIBUTING.md`, `SECURITY.md`, `LICENSE`, `DCO`, `docs/` (recordings, tapes, images), `examples/`, the issue forms and the pull request template in `.github/` |

No other prefixes are accepted. The release changelog is grouped by these
prefixes.

### Fixes tag

A commit that fixes a bug introduced by an earlier commit names that commit
with its first 12 SHA characters and its subject:

```text
Fixes: 1a2b3c4d5e6f ("git: resolve annotated tags")
```

Git can produce the line for you:

```sh
git log -1 --abbrev=12 --format='Fixes: %h ("%s")' <commit>
```

### Checking commit messages locally

Install gitlint 0.19.1, for example with pipx (the `gitlint-core` package
provides the command), or from your distribution:

```sh
pipx install gitlint-core==0.19.1
```

Then install the commit-msg hook once, from the repository root:

```sh
gitlint install-hook
```

The hook checks every message when you commit. To check a whole branch
with the pinned gitlint from the build image, run:

```sh
GITLINT_RANGE=origin/main..HEAD scripts/build-in-container.sh gitlint
```

## Testing rules

| Level | Tool | Scope |
|---|---|---|
| Unit | `go test` | Parsers, resolution logic, porcelain formatting |
| Integration | `go test` | Real `git` against local repositories in `t.TempDir()` |
| TUI | `teatest` | Key bindings, state transitions, golden output |

- **No network:** tests never access the network. Remotes are local bare
  repositories, and the `gittest` environment sets
  `GIT_ALLOW_PROTOCOL=file`, so Git refuses every other transport.
- **Isolation:** each test runs with `GIT_CONFIG_GLOBAL=/dev/null`,
  `GIT_CONFIG_NOSYSTEM=1` and fixed `GIT_AUTHOR_*` and `GIT_COMMITTER_*`
  values. Local submodules need `-c protocol.file.allow=always`.
- **Fixtures:** use the helpers in `internal/git/gittest`. They set up the
  isolated environment, upstream repositories and superprojects, with fixed
  dates for reproducible SHAs, in both SHA-1 and SHA-256 formats.
- **Parallel tests:** tests call `t.Parallel()` where possible. Therefore
  never use `t.Setenv`; pass the environment to the runner with
  `git.WithEnv(...)` instead.
- **Required fixtures:** lightweight tag, annotated tag, moved tag
  (force-push), pre-release tag, nested submodule, dirty submodule.
- **Golden files:** golden files live under `testdata/`. TUI golden output
  is rendered deterministically, with a fixed color profile and
  environment. See [Golden files](#golden-files) for how to update them.
- **Demo:** `TestDemo` in `cmd/lazysubmodules` builds the binary and runs
  `scripts/demo.sh` against it (see
  [Demo and recordings](#demo-and-recordings)). It needs Bash and is
  skipped with `go test -short`.
- **Race detector:** `go test -race` must pass.

Every edge case below has a test:

| Case | Required behaviour |
|---|---|
| Annotated tag | Dereference to commit |
| Tag moved on remote | `status` shows `drift`; `verify` fails with code 4 |
| Detached HEAD in submodule | Normal state, not an error |
| Uninitialized submodule | `status` shows `uninitialized`; `update` initializes it |
| Nested submodules | Not managed recursively in v1 |
| Missing ref after fetch | `missing-ref`; `update` refuses with code 3 |
| Dirty submodule | `update` refuses with code 3 |
| SHA-256 repositories | SHA length not hardcoded |
| Mirrors (`insteadOf`) | Transparent, no special handling |
| Non-TTY for `tui` | Exit code 2 |

### Golden files

After a deliberate change of the output, rewrite the golden files with
the `-update` flag of the package's tests, then review the diff:

```sh
go test ./internal/porcelain -run Golden -update
go test ./cmd/lazysubmodules -update
go test ./internal/tui -run TestGolden -update
```

- **Porcelain:** `internal/porcelain/testdata/` holds the stable v1
  format (see [Stable interfaces](#stable-interfaces)). A golden file there
  changes only together with the fixture it describes, never with the
  format.
- **Command line:** `cmd/lazysubmodules/testdata/` holds the help text,
  the status table and a copy of the porcelain output of the complex
  superproject, `status-complex-sha1.golden`, which must stay identical to
  `internal/porcelain/testdata/complex-sha1.golden`.
- **TUI:** `internal/tui/testdata/` holds the screens, with the escape
  sequences of the pinned color profile.

## Test coverage

`scripts/build-in-container.sh coverage` runs the tests of `test` once
more, with statement coverage, offline in the build image:

```sh
go test -race -covermode=atomic -coverprofile=coverage/coverage.out ./...
```

It writes its report to `coverage/`, which git ignores, and replaces an
earlier report first:

| File | Content |
|---|---|
| `coverage.out` | The coverage profile |
| `coverage.html` | The source files with their covered and uncovered statements (`go tool cover -html`) |
| `func.txt` | The coverage of every function (`go tool cover -func`) |
| `test.log` | The output of `go test` |
| `summary.md` | The coverage of every package and in total, as a Markdown table |
| `badge.json` | The total for the README badge, in the [shields.io endpoint](https://shields.io/badges/endpoint-badge) format |

- **What is measured:** each package counts only the statements that its
  own tests run, and a package without tests counts with 0%, as
  `go test -cover ./...` prints it in the same run. The total is the share
  of all statements of all packages, as `go tool cover -func` prints it.
  The target compares its table and total with the output of both tools
  and fails when they differ. Test helper packages such as
  `internal/git/gittest` are packages like the others and count as well.
- **Not to the last statement:** the numbers of unchanged code differ
  between runs by a tenth or two of a point, so the badge can change
  without a commit. Some error paths run only when a test cancels work
  that runs in parallel, and where that cancellation lands differs from
  run to run; `TestStatusGitFailure` of `internal/core`, for example,
  covers two error returns of `status.go` in about half of the runs. The
  comparison with the Go tools still holds, because it compares the
  report with the output of the same run. Read the numbers as "about
  95%", not as a value to defend.
- **No `-coverpkg`:** with `-coverpkg=./...`, the statements that the
  tests of other packages run would count too, for example the
  `internal/core` code that runs under the command line tests. The total
  would hardly change, because the packages' own tests already cover them
  well, but the per-package numbers would lose their meaning: `go test`
  then reports for each package the share of the statements of the whole
  module that its tests run. Every test binary would also be instrumented
  for every package, which costs a little more time.
- **Mode:** the race detector needs the `atomic` counter mode. The
  percentages do not depend on the mode.
- **Badge colors**, for the rounded value that the badge shows:
  90% and more `brightgreen`, 80% `green`, 70% `yellowgreen`, 60%
  `yellow`, 50% `orange`, below 50% `red`.
- **Cost:** `coverage` takes about as long as `test`. Most of the time
  goes to the race detector and to the `git` processes that the tests
  start; the coverage counters add little. With empty caches on four
  cores, both used about 940 CPU seconds (September 2026). A repeated run
  with unchanged code takes well under a minute, because `go test` caches
  most results of coverage runs in the `lazysubmodules-go-build` volume,
  as it does for `test`. In CI the `coverage` job runs in parallel with
  the build job, so the workflow takes no longer, but it occupies a
  second runner for about as long as the `test` step.

### Coverage in CI

The `coverage` job of `.github/workflows/ci.yml` runs next to the build
job, for every pull request and every push to `main`. It restores or
builds the build image like the build job (only the build job saves it),
runs `coverage`, adds `summary.md` to the job summary and uploads
`coverage/` as the artifact `coverage`, kept for 14 days.

On a push to `main`, its last step runs
`scripts/publish-coverage-badge.sh coverage/badge.json`. The script
replaces the branch `badges` with a single new commit by
`github-actions[bot]` that holds `coverage.json`, a copy of `badge.json`,
and a README that says that the branch is generated. The README badge
reads `coverage.json` through `raw.githubusercontent.com` and shields.io,
which both cache it for a few minutes.

- **Only the newest commit:** runs on `main` are never cancelled, so they
  can finish out of order. The script publishes only when its commit is
  still the tip of `main`, and pushes with `--force-with-lease`, so a
  late run of an older commit never overwrites the badge of a newer one.
  When the run of the newest commit fails, the badge keeps the previous
  value until a later run succeeds.
- **Token:** only the `coverage` job has `contents: write`, and only its
  last step receives `GITHUB_TOKEN`; it does not run for pull requests.
  The checkout keeps no credentials (`persist-credentials: false`). The
  script runs `git` only in a new repository in a temporary directory and
  passes the token as an HTTP header in `GIT_CONFIG_*` environment
  variables, so it is written neither to a `.git/config` nor to a command
  line, and it ignores the Git configuration of the user, of the system
  and of the checkout.
- **What the container does not protect:** the tests run before that step
  in a container without network access and without the token, but the
  checkout is mounted into the container, so they can change the files
  that the step afterwards runs, the script included. The step therefore
  trusts the code of `main` as much as the workflow file itself, which
  the same code review protects. Publishing from a job of its own, with a
  fresh checkout and the badge from the artifact, would make the
  container a boundary for the token as well.
- **No loops:** a push made with `GITHUB_TOKEN` starts no workflow run,
  and `ci.yml` runs only for pull requests and pushes to `main` anyway.
- **The `badges` branch** is not part of the history of `main`. Do not
  commit to it or open pull requests against it; the next run replaces it.
  Branch rules must allow `github-actions[bot]` to force-push it. Until
  the first run on `main` has created the branch, the README badge reads
  "custom badge: resource not found".
- **Trying the script:** it publishes to any remote given with
  `--remote`, such as a local bare repository with a `main` branch, and
  needs no token for a remote that is not `https://`. After `coverage`:

  ```sh
  git init --bare --initial-branch=main /tmp/badges.git
  git push /tmp/badges.git HEAD:main
  scripts/publish-coverage-badge.sh --remote /tmp/badges.git \
      --commit "$(git rev-parse HEAD)" coverage/badge.json
  git -C /tmp/badges.git log --stat badges
  ```

- **Why no coverage service:** services such as Codecov or Coveralls need
  an app or a token with access to the repository, receive the test data,
  and make CI depend on another service. The report is produced offline
  in the build image like the other targets, the job summary and the
  artifact stay on GitHub, and shields.io only reads the public
  `coverage.json` to draw the badge.

## Demo and recordings

### The demo

`scripts/demo.sh` builds a firmware superproject with fourteen submodules
from local repositories and tells a story with LazySubmodules commands in
twelve chapters. It is documentation and an end-to-end test at once:
every command must exit with the status the story expects, or the demo
fails. The topology is the same as that of `gittest.NewComplexSuper`,
which the core, command line and TUI tests use; keep the two in sync.
[`examples/README.md`](examples/README.md) describes the options, the
submodules and the chapters.

```sh
scripts/build-in-container.sh build   # or a host build into bin/
scripts/demo.sh --no-pause            # uses bin/lazysubmodules
scripts/demo.sh --keep /tmp/lsm-demo --setup-only   # first state, kept
```

- **Isolation:** the demo runs Git without the user and system
  configuration, allows only the file transport, rewrites every
  submodule URL to local bare repositories with `url.<base>.insteadOf`,
  and keeps `HOME` and `TMPDIR` inside the demo directory. Identities and
  dates are fixed, so commit IDs and output are the same in every run.
- **`TestDemo`** (`cmd/lazysubmodules/demo_test.go`) runs the demo with
  `--keep` and `--transcript` against a freshly built binary and checks
  the exit status of every command, the key lines of the story, the final
  state and history, and that the files in `examples/` match. It needs
  Bash and is skipped with `go test -short`. Without `ssh-keygen`, it
  expects the demo's tag to be unsigned and its signature check to fail.

When the story or its output changes, regenerate the transcript with a
current binary, and commit it together with the change:

```sh
scripts/demo.sh --no-pause --transcript examples/transcript.txt
```

`TestDemo` compares the chapter titles, the commands and the exit
statuses with the committed transcript, so a stale transcript fails the
test. When the final `.gitmodules` or `.lsm.lock` change, copy them from
`DIR/firmware` of a demo kept with `--keep DIR`, replace
`git.example.invalid` with `git.example.org`, and keep their comments.

### The recordings

The animated GIFs in `docs/demo/`, which the README embeds, are recorded
with [VHS](https://github.com/charmbracelet/vhs) from the tape of the same
name, on the demo superproject in its first state. `scripts/record-demos.sh`
records them in a container without network access:

```sh
scripts/record-demos.sh                 # build bin/lazysubmodules, record all
scripts/record-demos.sh hero cli        # only these
scripts/record-demos.sh --binary PATH   # record another binary
```

- **Requirements:** Podman or Docker (`CONTAINER_ENGINE` selects one, as
  for the build script). Building the recording image from
  `docs/demo/Containerfile` needs network access once; recording takes
  about a minute per GIF.
- **Checks:** every tape waits for the states it shows, so VHS fails
  instead of recording something else. The script fails when a GIF is
  missing or larger than 1.5 MB.
- **When:** record again when the interface or the output that a GIF
  shows changes, look at the result, and commit the GIFs together with
  their tapes.

[`docs/demo/README.md`](docs/demo/README.md) describes the tapes, the
terminal size, how to write a tape and how to update the pinned VHS
image.

## Dependencies

The dependency set is deliberately small: Bubble Tea, Lip Gloss and Bubbles
for the TUI (plus what they pull in). The CLI uses the standard `flag`
package. A new dependency needs a good reason and a compatible license.

- **Allowed licenses:** `licenses` accepts only these permissive licenses,
  all compatible with `GPL-3.0-only`: Apache-2.0, BSD-2-Clause,
  BSD-3-Clause, ISC and MIT. Anything else, including weak copyleft such as
  MPL-2.0, needs a manual review and a change to the allowed list in
  `scripts/build-in-container.sh`.
- **Kernel tools:** `checkpatch.pl` (GPL-2.0-only) is not used and not
  vendored.
- **Vendoring:** `vendor/` is committed so that builds work without network
  access. Adding or upgrading a dependency needs network access, so do it
  with a host Go toolchain:

  ```sh
  go get example.org/module@vX.Y.Z
  go mod tidy
  go mod vendor
  scripts/build-in-container.sh licenses lint test
  ```

  Commit `go.mod`, `go.sum` and `vendor/` together.

## Third-party notices

The release binary is statically linked. It contains the Go standard
library and the modules from `vendor/` that `cmd/lazysubmodules` imports.
Their licenses (currently MIT and BSD-3-Clause) require their copyright
notices and license texts to accompany every binary distribution. Modules
that only tests import are not linked and need no notice.

`scripts/third-party-licenses.sh` collects the notices. GoReleaser runs it
as a before hook, so `snapshot` and `release` include them without a
manual step. The script works offline from `vendor/` and runs
`go-licenses save` from the build image for `linux/amd64` and
`linux/arm64`. It writes `build/third-party/`, which git ignores:

| File | Content |
|---|---|
| `THIRD_PARTY_NOTICES` | One stanza per module: `Module`, `Version`, `License` (SPDX identifier) and `Text`, the license file relative to the notices file |
| `licenses/` | The license files; `licenses/std/LICENSE` is the license of Go |
| `copyright` | Debian copyright file: the project notice, the stanzas and all license texts |

Where the notices are installed:

| Artifact | Location |
|---|---|
| Archives | `THIRD_PARTY_NOTICES` and `licenses/`, next to `LICENSE` |
| `.rpm` | `/usr/share/licenses/lazysubmodules/`: `LICENSE` and `THIRD_PARTY_NOTICES` (both `%license`) and `licenses/` |
| `.deb` | `/usr/share/doc/lazysubmodules/copyright` |
| AUR `-bin` recipe | `/usr/share/licenses/lazysubmodules/`, laid out as in the archive |
| AUR `-git` recipe | `/usr/share/licenses/lazysubmodules-git/`, laid out as in the archive; its `build()` runs the script |

The locations follow each distribution's rules:

- **Fedora:** license files belong in `/usr/share/licenses/<package>/`.
  rpm installs `%license` files even when documentation is excluded, as it
  is in the Fedora container images.
- **Debian:** Debian Policy puts all copyright and license information in
  `/usr/share/doc/<package>/copyright`. Minimal installations, such as the
  Debian container images, exclude every other file in `/usr/share/doc`.
- **Arch Linux:** license files belong in `/usr/share/licenses/<package>/`.

The notices are checked in two places:

- **The script** fails when go-licenses recognizes no license for a linked
  module, when a license file is outside that module's directory in
  `vendor/`, or when go-licenses saves a file that no stanza names.
- **`package-test`** compares the installed notices with the build
  information of the installed binary. Every module listed there, and
  `std`, needs a stanza with the same version, and every license text must
  be installed. For the `.rpm`, the `License` tag must also equal
  `GPL-3.0-only` joined with the licenses in the notices by `AND`.

When the dependencies change:

- **A license new to the binary:** add it to the SPDX expression in the
  `license` field of `nfpms` in `.goreleaser.yaml`. The generated `-bin`
  recipe reuses that field, and `package-test` fails until the field is
  updated. Update the `license` array of
  `packaging/aur/lazysubmodules-git/PKGBUILD` and its `.SRCINFO` by hand.
  The license must be on the allowed list (see
  [Dependencies](#dependencies)).
- **A `NOTICE` file:** go-licenses saves `NOTICE`, `NOTICE.txt` and
  `NOTICE.md` next to the license of a module; Apache-2.0 requires them
  to be passed on. The script does not reference them in the stanzas or
  the Debian `copyright` file yet, so it fails until that support is
  added (together with a check in `package-test`).
- **A new target platform:** add it to the arguments of the before hook as
  well. go-licenses only looks at the packages built for the platforms it
  is given.
- **A license that requires distributing the source code:**
  `go-licenses save` would copy the module's source into `licenses/`,
  which makes the script fail as well. Such licenses are not on the
  allowed list and need a review first.

To inspect the notices, run `snapshot` and read `build/third-party/`.
With `go` and `go-licenses` installed on the host, run the script
directly: `scripts/third-party-licenses.sh -h` lists its options.

## AUR recipe

`packaging/aur/lazysubmodules-git/` holds the recipe of the planned AUR
package `lazysubmodules-git`, which builds the default branch of the
GitHub repository. The package is not published yet.

| File | Content |
|---|---|
| `PKGBUILD` | The recipe |
| `.SRCINFO` | The metadata that the AUR reads, generated from `PKGBUILD` |
| `LICENSE` | A copy of the GPL for the recipe itself, as the AUR asks for |

What the recipe does:

- **`pkgver()`:** derives the version from the newest annotated `v*` tag
  with `git describe`, for example `0.1.0.r3.g1234abc` three commits after
  `v0.1.0`. A pre-release loses its separators (`v0.1.0-rc.1` becomes
  `0.1.0rc1`), so that `vercmp` sorts it below the release. Before the
  first release tag, the version is `r<commit count>.<commit>`.
- **`build()`:** follows the Go package guidelines of Arch Linux: PIE,
  `-trimpath`, the cgo flags from `makepkg.conf`, and external linking
  with the Arch `LDFLAGS`. It builds from `vendor/` with `GOPROXY=off`
  and `GOTOOLCHAIN=local`, sets the version, the commit and the commit
  date with `-ldflags`, and runs `scripts/third-party-licenses.sh`. The
  commit date, as in the release binaries, keeps a rebuild of the same
  commit identical.
- **No debug package:** `options=('!debug')`. With `-trimpath`, the binary
  records no paths of the build directory, so `debugedit` would find no
  sources for a `lazysubmodules-git-debug` package.
- **`check()`:** runs `go test ./...`, which uses `ssh-keygen`
  (`openssh` in `checkdepends`).
- **`package()`:** installs the binary, the `lsm` symlink, the license
  with the third-party notices, and the README.

After every change of `PKGBUILD`, regenerate `.SRCINFO` and commit both
with the `release:` prefix:

```sh
cd packaging/aur/lazysubmodules-git
makepkg --printsrcinfo > .SRCINFO
```

- **Testing:** build the package in a clean Arch Linux container as an
  unprivileged user with `makepkg -s`, which also runs `check()`, install
  it with `pacman -U`, and run `namcap` on the recipe and the package.
  `makepkg` must build no `-debug` package, and
  `strings /usr/bin/lazysubmodules` must show no path of the build
  directory. Expected namcap reports: for the recipe, `git` as a make
  dependency that is already included as a dependency, and for the
  package, `git` as a dependency that may not be needed. `git` stays in
  `makedepends`, as the VCS package guidelines ask, and in `depends`,
  because the binary runs it.
- **Unpublished changes:** the recipe clones the GitHub repository. To
  build a change that is not pushed yet, point `source` at a local clone
  for the test, for example `git+file:///path/to/clone`, and do not commit
  that.
- **`pkgver` in the repository** only records the version of the last
  publication; `makepkg` computes the current one when it builds.

Publishing is up to the maintainers, once the package is registered in the
AUR; only then does the README link to it:

1. Clone `ssh://aur@aur.archlinux.org/lazysubmodules-git.git`.
2. Copy `PKGBUILD`, `.SRCINFO` and `LICENSE` into it.
3. Run `makepkg -od` to refresh `pkgver`, then
   `makepkg --printsrcinfo > .SRCINFO`.
4. Commit and push, and bring the refreshed `PKGBUILD` and `.SRCINFO`
   back into this repository.

## Community files

| File | Purpose |
|---|---|
| `.github/ISSUE_TEMPLATE/1-bug-report.yml` | Bug report form: version, installation method, system, Git version, interface, steps, expected and actual behavior, and optionally the porcelain output and the submodule configuration. Applies the label `bug` |
| `.github/ISSUE_TEMPLATE/2-feature-request.yml` | Feature request form. Applies the label `enhancement` |
| `.github/ISSUE_TEMPLATE/config.yml` | Turns off blank issues, and links to the security policy and the README |
| `.github/pull_request_template.md` | Summary, testing, and a checklist of the rules in this guide |
| `SECURITY.md` | Supported versions, private reporting of vulnerabilities, response goals and scope |
| `docs/assets/social-preview.svg` and `.png` | The social preview image of the repository |

- **Consistency:** when a rule in this guide changes, such as the prefix
  list or a command, update the pull request template as well; when the
  release process or the supported versions change, update `SECURITY.md`.
- **Repository settings:** `SECURITY.md` sends reporters to private
  vulnerability reporting (Settings, Advanced Security), which provides
  the "Report a vulnerability" button; keep it turned on. Without it,
  reporters can only use the e-mail address in `SECURITY.md`. The social
  preview is uploaded under Settings, General. The forms use the labels
  `bug` and `enhancement`; keep them in the repository.
- **Social preview:** edit the SVG, then render the PNG (1280 × 640
  pixels) and commit both:

  ```sh
  rsvg-convert -w 1280 -h 640 -o docs/assets/social-preview.png docs/assets/social-preview.svg
  ```

## Stable interfaces

- **Porcelain v1:** the output of `status --porcelain=v1` is a stable
  contract for scripts. This covers the header line, the field count, order
  and meaning, the state names and the quoting rule. Do not change it. An
  incompatible change requires a new `--porcelain=v2` format, and the golden
  files in `internal/porcelain/testdata/` must keep passing.
- **Exit codes** (0–5) and the `.gitmodules` keys `lsm-mode` and `lsm-ref`
  are part of the user interface as well.
- **Network policy:** do not add network access outside `fetch`,
  `update --fetch` and `add`.

## Pull requests and releases

- CI (`.github/workflows/ci.yml`) runs `gitlint`, `lint`, `licenses`,
  `test`, `build`, `snapshot` and `package-test` through the same script for
  every pull request and every push to `main`, and uploads the snapshot.
  A second job runs `test-compat`, and a third runs `coverage` and, on
  `main`, publishes the coverage badge (see
  [Coverage in CI](#coverage-in-ci)).
- CI caches the build image with `image-save` and `image-load`, keyed by the
  reference `image-tag` prints; a cache miss builds the image with `image`.
- Third-party GitHub Actions are pinned by commit SHA.
- Releases are cut by maintainers with a signed SemVer tag, for example
  `git tag -s v1.2.3`; pre-releases use `vX.Y.Z-rc.N`. Pushing the tag
  runs `.github/workflows/release.yml`, which checks the tag signature,
  restores or builds the image and runs `test` and `release`.
- The tag check accepts only an annotated tag at the checked-out commit
  with a good OpenPGP signature from one of the keys in the repository
  variable `RELEASE_TAG_KEYS` (Settings, Secrets and variables, Actions,
  Variables). Set it before the first release to the ASCII-armored public
  keys of the maintainers, for example the output of
  `gpg --armor --export <key-id>`; without it, every release fails. The
  keys live in a variable rather than in the repository because whoever
  pushes a tag controls the files of the tagged commit, while variables
  can only be changed in the repository settings.
- GoReleaser injects the version, commit and commit date with `-ldflags`
  (the commit date rather than the build date, so that a rebuild of the
  same commit gives identical archives and packages), and
  signs `checksums.txt` with cosign keyless signing.
- No AUR package is published yet. The planned `lazysubmodules-git`
  package builds from this repository with the recipe in
  `packaging/aur/` (see [AUR recipe](#aur-recipe)); register it in the
  AUR before the README links to it. GoReleaser also generates a
  `lazysubmodules-bin` recipe, but publishes it only when the `AUR_KEY`
  secret is set, which the project does not do.
- Release archives and packages include the third-party license notices
  (see [Third-party notices](#third-party-notices)).
