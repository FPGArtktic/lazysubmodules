<!-- SPDX-License-Identifier: GPL-3.0-only -->
<!-- Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com> -->

(project-license)=

# License and authors

LazySubmodules is free software under the GNU General Public License,
version 3 only, and so are this documentation and its images.

## The project

LazySubmodules is free software: you can redistribute it and modify it
under the terms of the GNU General Public License, version 3 only
(`GPL-3.0-only`), as published by the Free Software Foundation. The full
text is in
[LICENSE](https://github.com/FPGArtktic/lazysubmodules/blob/main/LICENSE)
in the repository.

The same license covers this documentation, the pages in `docs/`, the
demo recordings and the social preview image. Every file that can hold a
comment carries the identifier:

```text
SPDX-License-Identifier: GPL-3.0-only
Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>
```

The GIF recordings and the social preview image carry no header, because
their formats have no place for one; this page and the License section of
the README cover them.

Contributions are licensed under the same terms, and every commit
certifies the
[Developer Certificate of Origin](https://github.com/FPGArtktic/lazysubmodules/blob/main/DCO)
with a `Signed-off-by` line; see [Contributing](contributing.md).

(project-license-third-party)=

## Third-party notices

The release binary is statically linked. It contains the Go standard
library and the Go modules that the program imports: the Charm terminal
libraries (Bubble Tea, Bubbles and Lip Gloss) and what they pull in, plus
parts of `golang.org/x`. Their licenses, currently MIT and BSD-3-Clause,
require their copyright notices and license texts to accompany every
binary distribution, so every release artifact carries them:

| Artifact | Where the notices are |
|---|---|
| Release archive | `THIRD_PARTY_NOTICES` and `licenses/`, next to `LICENSE` |
| `.deb` | `/usr/share/doc/lazysubmodules/copyright`, the Debian copyright file |
| `.rpm` | `/usr/share/licenses/lazysubmodules/`: `LICENSE`, `THIRD_PARTY_NOTICES` and `licenses/` |
| AUR `lazysubmodules-git` | `/usr/share/licenses/lazysubmodules-git/`, laid out as in the archive |

`THIRD_PARTY_NOTICES` lists every module with its version, its license
identifier and the file that holds the license text.

A binary you build yourself, and one installed with `go install`, comes
without these files. `scripts/third-party-licenses.sh` collects them from
the vendored modules, and
`go version -m "$(command -v lazysubmodules)"` lists the modules a binary
contains.

Only permissive licenses that are compatible with `GPL-3.0-only` are
allowed for dependencies: Apache-2.0, BSD-2-Clause, BSD-3-Clause, ISC and
MIT. The `licenses` build target checks this.

## Author

Mateusz Okulanis <FPGArtktic@outlook.com>

Thanks to everyone who reports issues and contributes changes; the
commit history and the GitHub contributors page name them.

## This site

Built with [Sphinx](https://www.sphinx-doc.org/), the
[Furo](https://github.com/pradyunsg/furo) theme and
[MyST-Parser](https://myst-parser.readthedocs.io/), and published by
[Read the Docs](https://about.readthedocs.com/). Those projects have
their own licenses. See [Building the documentation](documentation.md).
