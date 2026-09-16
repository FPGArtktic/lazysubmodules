<!-- SPDX-License-Identifier: GPL-3.0-only -->
<!-- Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com> -->

# Contributing to LazySubmodules

Thank you for helping. This guide covers building, testing, code style, commit
messages and dependencies. The rules are enforced by the same containerized
tooling on developer machines and in CI, so a change that passes locally
passes in CI too.

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
- [Dependencies](#dependencies)
- [Stable interfaces](#stable-interfaces)
- [Pull requests and releases](#pull-requests-and-releases)

## Prerequisites

- Linux on x86_64 (`amd64`) for the container targets; the build image
  exists for x86_64 only (see [Build image](#build-image)).
- Git and Bash.
- Podman (preferred, rootless works) or Docker.
- Optional, for quick local loops: Go 1.27.1 or later.
- Optional, for the commit-msg hook: `gitlint` 0.19.1 (see
  [Checking commit messages locally](#checking-commit-messages-locally)).
- For `scripts/update-builder-pins.sh`: `curl` and network access.

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
| `lint` | `golangci-lint run`, `shellcheck scripts/*.sh` and `scripts/check-headers.sh` |
| `gitlint` | Validate the commit messages selected by `GITLINT_RANGE` |
| `licenses` | `go-licenses check ./...` (see [Dependencies](#dependencies)) |
| `snapshot` | `goreleaser release --snapshot --clean`, unsigned, output in `dist/` |
| `release` | `goreleaser release --clean` (CI only: needs network and credentials) |
| `test-compat` | `go test -race ./...` with Git 2.39.5, the oldest supported Git |
| `package-test` | Install the `dist/` `.deb` in Debian and the `.rpm` in Fedora, then run `lazysubmodules version` and `lsm version` |
| `image` | Build the build image from `Containerfile` |
| `image-tag` | Print the build image reference |
| `image-save` | Save the build image to the archive `IMAGE_ARCHIVE` |
| `image-load` | Load the build image from the archive `IMAGE_ARCHIVE` |

The targets from `build` to `release` run in the build image. `test-compat`
uses the official `golang:1.27.1-bookworm` image (Debian bookworm ships Git
2.39.5), and `package-test` uses `debian:trixie-slim` and `fedora:44`; all
three are pinned by digest in the script. `package-test` needs the packages
of a previous `snapshot`, and it fails when `dist/` has none.

Before you propose a commit, run at least:

```sh
scripts/build-in-container.sh lint test
```

The full CI sequence is:

```sh
scripts/build-in-container.sh image gitlint lint licenses test build snapshot package-test
scripts/build-in-container.sh test-compat
```

How the script behaves:

- **Engine:** `CONTAINER_ENGINE` selects the engine; the default is `podman`,
  with `docker` as the fallback. Rootless Podman runs with `--userns=keep-id`,
  so files written to the repository belong to you.
- **User:** the container user has a passwd entry with the home directory
  `/tmp`, which is also `HOME`. `ssh` and `ssh-keygen` (used by tests and by
  AUR publishing) read the home directory from that entry, so they never
  write into the checkout. Podman gets the entry with `--passwd-entry`;
  rootful Docker gets a copy of the image's passwd file with the entry added.
- **Image:** every target that runs in the build image builds it first when
  it is missing. The image tag is derived from the `Containerfile` hash, so
  an edited `Containerfile` is rebuilt automatically, and
  `image-tag` prints the reference (CI uses it as the cache key).
- **Offline:** all targets except `release`, `package-test` and `image` run
  without network access. Dependencies come from the committed `vendor/`
  directory (`GOFLAGS=-mod=vendor`), and `GOTOOLCHAIN=local` prevents Go
  toolchain downloads.
- **Network for `package-test`:** `apt` and `dnf` download the `git`
  dependency of the packages from the distribution mirrors.
- **cgo:** builds use `CGO_ENABLED=0`. The race detector needs cgo, so `test`
  and `test-compat` enable it; `gcc` comes with the images.
- **Mounts:** the repository is mounted with `:Z` for SELinux hosts. The
  cache volumes are shared by every run and use the shared label `:z`
  instead, because a private label would be applied again, recursively, on
  each run.
- **Release:** `release` passes `GITHUB_TOKEN`,
  `ACTIONS_ID_TOKEN_REQUEST_URL`, `ACTIONS_ID_TOKEN_REQUEST_TOKEN`, `AUR_KEY`
  and `GITHUB_STEP_SUMMARY` into the container when they are set. GoReleaser
  reads no other `GITHUB_*` variable. The image pins the SSH host key of
  `aur.archlinux.org`.
- **Caches:** Go and linter caches live in the named volumes
  `lazysubmodules-go-build`, `lazysubmodules-go-mod` and
  `lazysubmodules-golangci-lint`. Remove them with `podman volume rm` (or
  `docker volume rm`) to start from scratch.
- **gitlint range:** `GITLINT_RANGE` is passed to `gitlint --commits`. `A..B`
  lints the commits after `A` up to `B`, a single ref lints its whole
  history, and the default is `HEAD`. To lint only your branch:

  ```sh
  GITLINT_RANGE=origin/main..HEAD scripts/build-in-container.sh gitlint
  ```

Deliberate choices, for the reasons given above:

- `test` and `test-compat` run with `CGO_ENABLED=1`, which `-race`
  requires; everything else builds with `CGO_ENABLED=0`.
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

go-licenses, which has no packages, is built with `go install` at a pinned
version; the Go checksum database verifies it. All pins are `ARG` lines in
the `Containerfile`.

The official Arch Linux image exists for x86_64 only, so the build image
does too: the script refuses to build or run it on other architectures. The
release binaries and packages are still built for `amd64` and `arm64`.

### Updating the pins

`scripts/update-builder-pins.sh` resolves the newest dated
`archlinux:base-devel` tag and its digest, the matching archive date and the
newest commits of the AUR packages, checks them, and rewrites the pins in
the `Containerfile`. It changes nothing when the pins are current.

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
- `shellcheck` reports zero warnings.
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

YAML, INI and similar configuration files (`Containerfile`, `.gitignore`,
`.gitlint`, ...) use lines 1–2:

```sh
# SPDX-License-Identifier: GPL-3.0-only
# Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>
```

Markdown files use HTML comments on lines 1–2:

```html
<!-- SPDX-License-Identifier: GPL-3.0-only -->
<!-- Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com> -->
```

Exempt files: `LICENSE`, `DCO`, `go.sum`, everything under `vendor/`, and
test data under `testdata/` (including `*.golden` files).

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
| `scripts` | `scripts/` |
| `ci` | `.github/workflows/ci.yml` |
| `release` | `.goreleaser.yaml`, `.github/workflows/release.yml` |
| `docs` | `README.md`, `CONTRIBUTING.md`, `LICENSE`, `DCO` |

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
  is rendered deterministically, without color.
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
  A second job runs `test-compat`.
- CI caches the build image with `image-save` and `image-load`, keyed by the
  reference `image-tag` prints; a cache miss builds the image with `image`.
- Third-party GitHub Actions are pinned by commit SHA.
- Releases are cut by maintainers with a signed SemVer tag, for example
  `git tag -s v1.2.3`; pre-releases use `vX.Y.Z-rc.N`. Pushing the tag
  runs `.github/workflows/release.yml`, which restores or builds the image
  and runs `test` and `release`.
- GoReleaser injects the version, commit and build date with `-ldflags`, and
  signs `checksums.txt` with cosign keyless signing.
- The AUR package `lazysubmodules-git` builds from this repository; its
  recipe lives in the AUR. GoReleaser also generates a `lazysubmodules-bin`
  recipe, but publishes it only when the `AUR_KEY` secret is set, which the
  project does not do.
