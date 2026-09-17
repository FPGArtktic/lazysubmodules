<!-- SPDX-License-Identifier: GPL-3.0-only -->
<!-- Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com> -->

# Security policy

## Supported versions

Security fixes are made on the `main` branch and published in a new
release. Older releases do not get fixes; upgrade to the latest one.

| Version | Supported |
|---|---|
| Latest release | Yes |
| `main` branch, including packages built with the recipe in [`packaging/aur/`](https://github.com/FPGArtktic/lazysubmodules/tree/main/packaging/aur) | Yes |
| Older releases | No |
| Pre-releases (`vX.Y.Z-rc.N`) | No, once the release or a newer pre-release is out |

`lazysubmodules version` prints the version, the source commit and the
commit date of a binary.

The AUR package `lazysubmodules-git` is planned but not published yet.
Until the
[README](https://github.com/FPGArtktic/lazysubmodules#arch-linux-aur)
links to it, a package of that name in the AUR does not come from this
project and is not supported here.

## Reporting a vulnerability

**Do not report vulnerabilities in public issues, pull requests or
discussions.** Report them privately, in one of these ways:

- **GitHub:** open the
  [Security tab](https://github.com/FPGArtktic/lazysubmodules/security)
  of the repository and click **Report a vulnerability**, or go straight
  to the
  [private report form](https://github.com/FPGArtktic/lazysubmodules/security/advisories/new).
  Only you and the maintainers can see the report.
- **E-mail:** if the button or the form is not available, or you cannot
  use GitHub, send an e-mail to Mateusz Okulanis
  <FPGArtktic@outlook.com> with "lazysubmodules security" in the
  subject.

## What to include

- The version (`lazysubmodules version`), the installation method, the
  output of `git --version` and the operating system.
- The kind of problem and its impact: what an attacker controls, and what
  they can achieve.
- Steps to reproduce. A script that builds a small superproject from local
  repositories, without network access, is the most helpful.
- A proof of concept, logs or screenshots, if you have them.
- Whether the problem is already public or known to others, and any
  disclosure deadline you have in mind.
- A suggested fix, if you have one.

## What to expect

LazySubmodules is maintained by one person in their spare time, so the
times below are goals rather than guarantees:

- **Acknowledgement** of your report within 7 days.
- **Assessment** within 14 days: whether the report is accepted, and how
  severe the problem is.
- **Fix:** a release with the fix, normally within 90 days of the report.
  You get updates on the progress, and a draft of the fix to test if you
  like.
- **Disclosure:** once the fixed release is out, the problem is published
  as a GitHub security advisory, with a CVE identifier when it warrants
  one. You are credited in the advisory unless you prefer otherwise.

Please keep the problem private until the advisory is published.

## Scope

LazySubmodules reads repositories that may come from untrusted sources.
Reports are especially welcome about:

- a cloned superproject whose `.gitmodules` or `.lsm.lock` makes
  LazySubmodules write outside the superproject, run unexpected commands or
  crash;
- names, refs or other repository data that inject control sequences into
  the terminal or break the `status --porcelain=v1` format;
- network access by commands other than `fetch`, `update --fetch` and
  `add`;
- checks of `verify`, including `--signatures`, that pass when they should
  fail;
- the release process: signed release tags, the cosign signatures of the
  checksums, and the published archives and packages (see
  [Verifying releases](https://github.com/FPGArtktic/lazysubmodules#verifying-releases)).

Out of scope:

- vulnerabilities in Git itself, which go to the Git project, and in Go or
  the Go modules LazySubmodules uses, which go to their maintainers (please
  still tell us if LazySubmodules is affected);
- the commands you run with `lazysubmodules foreach`, and Git hooks and
  configuration that you installed yourself.
