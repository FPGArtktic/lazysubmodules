<!-- SPDX-License-Identifier: GPL-3.0-only -->
<!-- Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com> -->

# Demo recordings

The animated GIFs in this directory show LazySubmodules at work on the
superproject that [`scripts/demo.sh`](../../scripts/demo.sh) builds: a
firmware project with fourteen submodules in every tracking mode and every
state, created offline from local repositories. Each GIF is recorded with
[VHS](https://github.com/charmbracelet/vhs) from the tape of the same name.

| GIF | What it shows |
|---|---|
| [`hero.gif`](hero.gif) | The terminal user interface: the table and the preview, the details of u-boot, an update of u-boot after the confirmation, its state turning `ok`, and the help |
| [`tag-pattern.gif`](tag-pattern.gif) | The pattern dialog (`p`) counting the matching tags while the pattern of kernel changes from `v6.6.*` to `v6.*`, and the update that picks `v6.6.10` and skips the pre-releases `v6.7-rc1` and `v6.6.11-rc1` |
| [`cli.gif`](cli.gif) | `status`, `status --porcelain=v1` laid out with `column`, `update --dry-run`, `update --commit` and the commit message it wrote |
| [`drift.gif`](drift.gif) | A release tag moved upstream: after `fetch`, `status` reports `drift` and `verify` fails with exit status 4 |
| [`safety.gif`](safety.gif) | An update refused with exit status 3 because one submodule has uncommitted changes and another was never cloned; nothing changes, not even the submodule that was safe. Then the change is discarded and `update --fetch` clones the missing submodule |

A failed command in the recordings is followed by its exit status in
brackets, as in `scripts/demo.sh`.

## Regenerating the GIFs

```sh
scripts/record-demos.sh                 # all GIFs
scripts/record-demos.sh hero cli        # only these
scripts/record-demos.sh --binary PATH   # record another binary
```

By default the script first builds `bin/lazysubmodules` with
`scripts/build-in-container.sh build`. It needs Podman or Docker
(`CONTAINER_ENGINE` selects one, as for the build script), and network
access once, to build the recording image. Recording itself runs offline
and takes about a minute per GIF.

For each tape, the script:

1. runs VHS in a container of the recording image, without network access,
   with the repository mounted read-only at `/src`, this directory
   writable, and the binary as `/usr/local/bin/lazysubmodules`;
2. lets the tape build the demo superproject in the container, hidden,
   before the recording starts (`setup.tape`, `prepare.sh`);
3. optimizes the GIF losslessly with `gifsicle --optimize=3`;
4. fails when the GIF is missing, or larger than 1.5 MB.

Commit the GIFs together with the tapes they come from. Two recordings of a
tape differ slightly in their frame timing, but they show the same thing:
the demo and the recording shell use fixed names and dates, so even the
commit IDs are the same every time.

## Files

| File | Content |
|---|---|
| `*.tape` except `setup.tape` | One recording each; the comment at the top says what it shows |
| `setup.tape` | Settings shared by all tapes, and the hidden setup |
| `prepare.sh` | Builds the demo superproject with `scripts/demo.sh --setup-only` and writes the shell setup of the recording: demo environment, prompt, exit status display, fixed commit dates |
| `Containerfile` | The recording image |

### Terminal size

Every recording shows a terminal of 110 columns and 30 rows, in the
Catppuccin Mocha theme. VHS 0.11 sets the size in pixels: JetBrains Mono at
14 pixels in a 1054 × 590 pixel frame with 16 pixels of padding gives
exactly that grid. Check the grid with `tput cols` and `tput lines` in a
tape after changing the font, its size or the padding.

### Writing a tape

- Start with the `Output` line, then `Source "docs/demo/setup.tape"`; the
  recording begins at an empty prompt in the superproject.
- Wait for every state the story depends on, with `Wait` for the prompt and
  `Wait+Screen` with a regular expression for anything else. VHS fails when
  a wait times out, so a tape cannot silently record something else.
- Keep the recording short. Every full-screen change costs size; the
  script refuses GIFs above 1.5 MB.
- Record with `scripts/record-demos.sh NAME`, then look at the result, for
  example frame by frame in an image viewer.

## Pinned versions

The recording image is the official VHS image with git, `column` (from
`bsdextrautils`) and gifsicle added. The VHS image is pinned by the digest
of its multi-architecture manifest list, and the Debian packages by version,
from the [snapshot.debian.org](https://snapshot.debian.org/) state of the
day the VHS image was built.

The image is VHS 0.11.0, not the newer 0.12.0: VHS 0.12.0 records the frames
but writes no GIF, because it starts ffmpeg with a context that it has
already canceled (upstream pull requests
[#788](https://github.com/charmbracelet/vhs/pull/788) and
[#789](https://github.com/charmbracelet/vhs/pull/789)). VHS 0.12.0 can set
the size in rows and columns; VHS 0.11.0 cannot, hence the pixel size
above.

To update the pins:

1. Pick a VHS release and read the digest of its image, for example with
   `skopeo inspect --raw docker://ghcr.io/charmbracelet/vhs:vX.Y.Z | sha256sum`.
2. Set `VHS_IMAGE_TAG` and `VHS_IMAGE_DIGEST` in the `Containerfile`,
   `DEBIAN_SNAPSHOT` to the day the image was built, and the package
   versions to those of that snapshot.
3. Run `scripts/record-demos.sh`, check the terminal size and look at every
   GIF before you commit them.

The script tags the image with a hash of the `Containerfile`, so a changed
`Containerfile` leads to a new image.
