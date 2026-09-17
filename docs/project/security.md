<!-- SPDX-License-Identifier: GPL-3.0-only -->
<!-- Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com> -->

(project-security)=

# Security policy

How to report a vulnerability privately, what to include, and what to
expect.

This page summarizes
[SECURITY.md](https://github.com/FPGArtktic/lazysubmodules/blob/main/SECURITY.md)
in the repository, which is the authoritative version.

:::{important}
Do not report vulnerabilities in public issues, pull requests or
discussions.
:::

## Report a vulnerability

- **GitHub:** open the
  [Security tab](https://github.com/FPGArtktic/lazysubmodules/security) of
  the repository and click **Report a vulnerability**, or go straight to
  the
  [private report form](https://github.com/FPGArtktic/lazysubmodules/security/advisories/new).
  Only you and the maintainers can see the report.
- **E-mail:** if the form is not available to you, send an e-mail to
  Mateusz Okulanis <FPGArtktic@outlook.com> with
  "lazysubmodules security" in the subject.

## What to include

- The version (`lazysubmodules version`), the installation method, the
  output of `git --version` and the operating system.
- The kind of problem and its impact: what an attacker controls, and what
  they can achieve.
- Steps to reproduce. A script that builds a small superproject from
  local repositories, without network access, is the most helpful; the
  demo in `scripts/demo.sh` shows the pattern.
- A proof of concept, logs or screenshots, if you have them.
- Whether the problem is already public or known to others, and any
  disclosure deadline you have in mind.
- A suggested fix, if you have one.

## What to expect

LazySubmodules is maintained by one person in their spare time, so these
times are goals rather than guarantees:

| Step | Goal |
|---|---|
| Acknowledgement of the report | 7 days |
| Assessment: accepted or not, and how severe | 14 days |
| A release with the fix | normally 90 days |

You get updates on the progress, and a draft of the fix to test if you
like. Once the fixed release is out, the problem is published as a GitHub
security advisory, with a CVE identifier when it warrants one. You are
credited unless you prefer otherwise. Please keep the problem private
until the advisory is published.

## Supported versions

| Version | Supported |
|---|---|
| Latest release | Yes |
| `main` branch, including packages built from the recipe in `packaging/aur/` | Yes |
| Older releases | No |
| Pre-releases (`vX.Y.Z-rc.N`) | No, once the release or a newer pre-release is out |

Security fixes are made on `main` and published in a new release; upgrade
rather than patching an old one. The AUR package
[`lazysubmodules-git`](https://aur.archlinux.org/packages/lazysubmodules-git)
builds from `main`, so it carries a fix as soon as it is pushed; it is
maintained by this project.

## Scope

LazySubmodules reads repositories that may come from untrusted sources.
Reports are especially welcome about:

- a cloned superproject whose `.gitmodules` or `.lsm.lock` makes
  LazySubmodules write outside the superproject, run unexpected commands
  or crash;
- names, refs or other repository data that inject control sequences into
  the terminal or break the `status --porcelain=v1` format;
- network access by commands other than `fetch`, `update --fetch` and
  `add`;
- checks of `verify`, including `--signatures`, that pass when they
  should fail;
- the release process: signed release tags, the cosign signatures of the
  checksums, and the published archives and packages (see
  [Releases and versions](releases.md)).

[Safety model](../explanation/safety.md) describes the defenses these
reports would test.

Out of scope:

- vulnerabilities in Git itself, which go to the Git project, and in Go
  or the Go modules LazySubmodules uses, which go to their maintainers.
  Please still tell us if LazySubmodules is affected;
- the commands you run with `lazysubmodules foreach`, and Git hooks and
  configuration that you installed yourself.
