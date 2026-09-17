<!-- SPDX-License-Identifier: GPL-3.0-only -->
<!-- Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com> -->

(project-docs)=

# Building the documentation

How this site is built, and how it is kept in agreement with the
program.

## Layout

The pages live in `docs/` in the repository and are written in Markdown
(MyST), built with Sphinx and the Furo theme.

| Directory | Content |
|---|---|
| `docs/getting-started/` | Installation, the demo, the quick start |
| `docs/guide/` | Task guides |
| `docs/reference/` | Command line, files, states, porcelain format, exit codes, keys |
| `docs/explanation/` | How it works, resolution, safety, design |
| `docs/project/` | Contributing, this page, releases, security, license, FAQ |
| `docs/_static/` | Stylesheet, logo, favicon and the SVG diagrams |
| `docs/reference/cli/_generated/` | Captured help texts; see below |
| `docs/demo/` | The GIF recordings and their VHS tapes, shared with the README |
| `docs/assets/` | The social preview image, shared with the repository settings |

`docs/demo/` and `docs/assets/` are not pages. The README embeds the same
files, and `docs/demo/README.md` documents the recordings for
contributors; it stays on GitHub.

## Build it

The site has a build target of its own, like every other build of the
project:

```sh
scripts/build-in-container.sh docs
```

It runs `sphinx-build -W -b dirhtml` in the pinned
`python:3.13-slim-trixie` image, with `docs/requirements.txt` installed
into a virtual environment inside the container, and writes the site to
`docs/_build/html` and the Sphinx cache to `docs/_build/doctrees`. Read
the Docs uses Python 3.13 as well, and the requirements are resolved for
that version, so what builds here builds there. The target needs network
access on every run, because it installs the requirements each time;
`.gitignore` ignores `docs/_build/`.

- **`-W`** turns every warning into an error, as `fail_on_warning: true`
  does on Read the Docs. Sphinx collects the warnings of the whole run and
  fails at the end, so one build shows every problem.
- **`-b dirhtml`** produces the same clean URLs as the published site. A
  browser cannot follow them on the file system, so read the result
  through a server:

  ```sh
  python3 -m http.server -d docs/_build/html
  ```

Without a container, build in a virtual environment of your own. Python
3.13 or a later version works; the pinned set is resolved for 3.13:

```sh
python3.13 -m venv .venv-docs
.venv-docs/bin/pip install --require-hashes -r docs/requirements.txt
.venv-docs/bin/sphinx-build -W -b dirhtml docs docs/_build/html
```

### Update the pins

`docs/requirements.in` lists the version ranges, and `docs/requirements.txt`
holds the exact versions with hashes. Regenerate it with the command in
the header of `docs/requirements.in`, review the diff, and build the site
before you commit.

## Keep the site in agreement with the program

Three mechanisms catch documentation that drifts away from the program:

- **Captured help texts.** `scripts/gen-cli-docs.sh` runs the binary and
  writes the help of every command to
  `docs/reference/cli/_generated/<command>.txt`; the reference pages
  include those files. Regenerate them after a change to the help text.
  No CI job runs the check, so a stale file reaches the published site:
  run it yourself before you propose the commit.

  ```sh
  scripts/gen-cli-docs.sh                    # write the files
  scripts/gen-cli-docs.sh --check            # fail when a file is stale
  scripts/gen-cli-docs.sh --binary bin/lazysubmodules
  ```

  `--check` also reports a command without a reference page.
- **Included examples.** Several pages include parts of
  `examples/transcript.txt`, `examples/.gitmodules` and
  `examples/.lsm.lock` with `literalinclude`, selected by anchor strings
  such as `:start-at: "$ lazysubmodules fetch fpga.core sdk"`. When the
  demo changes, regenerate the transcript
  (`scripts/demo.sh --no-pause --transcript examples/transcript.txt`) and
  build the site: a missing anchor fails the build.
- **Verified examples.** Every other command and output on this site was
  run against a demo superproject. When you change behaviour, run the
  examples of the affected pages again.

Prose that describes behaviour, such as the terminal interface keys or
the commit message rules, exists both here and in `README.md`. Update
both when the behaviour changes.

## Read the Docs and CI

`.readthedocs.yaml` in the repository root describes the published build:
Ubuntu 24.04, Python 3.13, the requirements above, `docs/conf.py`, the
`dirhtml` builder, and warnings as errors. Read the Docs runs it when the
repository is pushed and publishes the result at
<https://lazysubmodules.readthedocs.io>.

The `docs` job of the CI workflow builds the same site with
`scripts/build-in-container.sh docs` for every pull request and every push
to `main`, and keeps it as the artifact `docs-site` for 14 days. A warning
therefore fails the pull request before it can break the published build.

## Writing rules

- **File headers.** Every page starts with the two SPDX and copyright
  lines as HTML comments, which do not render. SVG files use the same two
  comments and no XML declaration.
- **Structure.** After the header comes the label, `(section-page)=`, then
  the H1, then a summary of one or two sentences, which is also used as
  the page description.
- **Links.** Link pages by relative path
  (`[update](../reference/cli/update.md)`) and sections by label
  (`` {ref}`ref-states-precedence` ``). Files in the repository are linked
  by their URL on GitHub; a relative link out of `docs/` would break the
  build.
- **Code blocks.** ` ```sh ` for commands to copy, ` ```console ` for a
  session with `$ ` prompts and output, ` ```text ` for plain output,
  ` ```ini ` for `.gitmodules` and `.lsm.lock`.
- **Terminology.** Superproject, submodule, tracking mode, configured
  ref, lock file, lock entry, gitlink, state. Write "terminal interface"
  in prose and use "TUI" only in headings and tables.
- **Examples.** Every command and every output must be real. Use the
  names, paths and commit IDs of the demo, and never a path from your own
  machine.
- **Images.** The GIFs in `docs/demo/` are reused by relative path; no new
  binary files are added under `docs/`.

## See also

- [Contributing](contributing.md): the build and commit rules of the
  project.
- [Tour of the demo](../getting-started/tour.md): the superproject that
  the examples use.
