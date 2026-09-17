<!-- SPDX-License-Identifier: GPL-3.0-only -->
<!-- Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com> -->

(ref-cli-version)=

# `version`

Print the version of LazySubmodules, the commit it was built from and the
date of that commit.

## Synopsis

```text
lazysubmodules version
lazysubmodules --version
```

`version` works offline and needs neither Git nor a repository.

## Help text

```{literalinclude} _generated/version.txt
:language: text
:lines: 5-
```

## Output

Three lines on standard output:

```text
lazysubmodules 0.1.0-rc.1
commit: dd57e1b64d7d7faea17a5969041dde0387ed2bf1
date: 2026-09-17T19:42:54Z
```

- **Version.** The release version, or what the build recorded.
- **Commit.** The full SHA of the source commit.
- **Date.** The commit date in UTC, in the form `2026-09-17T11:14:07Z`. It
  is not the build date, so a rebuild of the same commit prints the same.
- **Program name.** The first line always says `lazysubmodules`, also when
  the program runs under the short name `lsm`.

Values that the build did not record are printed as `dev`, `none` and
`unknown`.

(ref-cli-version-builds)=

## Output per build

| Build | Output |
|---|---|
| Release archives and packages | The version without `v`, the commit and the commit date, set at build time |
| AUR package `lazysubmodules-git` | The package version, such as `0.1.0rc1.r0.gdd57e1b`, the commit and the commit date |
| `go install …@v0.1.0-rc.1` or `@latest` | The module version with `v` (`lazysubmodules v0.1.0-rc.1`), `commit: none` and `date: unknown`: Go records no commit for a downloaded module |
| `go build` or `go install` in a Git checkout | The version that Go derives from Git: the tag at the checked-out commit, such as `v0.1.0-rc.1`, or else a pseudo-version built from the newest tag below the commit, such as `v0.1.0-rc.1.0.20260917201232-e7f79f776c38`, with `+dirty` for uncommitted changes. Then the commit and the commit date that Go records |
| `go build` without Git information | `lazysubmodules dev`, `commit: none` and `date: unknown` |

A local build in a checkout with uncommitted changes prints, for example:

```text
lazysubmodules v0.1.0-rc.1.0.20260917201232-e7f79f776c38+dirty
commit: e7f79f776c380865aeac0005f9c1f08ccecc5eb5
date: 2026-09-17T20:12:32Z
```

The pseudo-version names the newest tag below the commit, then the commit
date and the commit itself, so it differs for every build of a different
commit.

A build with `go build -buildvcs=false` prints:

```text
lazysubmodules dev
commit: none
date: unknown
```

## Exit status

| Status | When |
|---|---|
| 0 | The version was printed |
| 2 | An argument was given, such as `version extra` |

## Examples

```sh
lazysubmodules version
lazysubmodules --version

# Include the version in a bug report together with the state.
lazysubmodules version && lazysubmodules status --porcelain=v1
```

## See also

- {ref}`start-installation`: how each kind of build is installed.
- {ref}`project-releases`: how releases are versioned and verified.
