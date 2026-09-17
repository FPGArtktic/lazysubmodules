<!-- SPDX-License-Identifier: GPL-3.0-only -->
<!-- Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com> -->

## Summary

<!--
What does this change do, and why is it needed? Link the issue it
resolves, for example "Fixes #123". Security fixes are not discussed in
public pull requests; see SECURITY.md.
-->

## Testing

<!--
How did you test the change? Name the tests you added or changed, and
describe any manual check, for example on a superproject built with
scripts/demo.sh.
-->

## Checklist

<!--
See CONTRIBUTING.md for the details:
https://github.com/FPGArtktic/lazysubmodules/blob/main/CONTRIBUTING.md
-->

- [ ] Every commit subject starts with a subsystem prefix (`cli`, `core`,
      `git`, `manifest`, `lock`, `porcelain`, `tui`, `build`, `scripts`,
      `ci`, `release` or `docs`), is in the imperative mood and has at most
      75 characters.
- [ ] Every commit is one logical change, has a body wrapped at 75 columns
      that explains why, and has a `Signed-off-by:` line (`git commit -s`),
      which certifies the
      [Developer Certificate of Origin](https://github.com/FPGArtktic/lazysubmodules/blob/main/DCO).
- [ ] `gitlint` accepts the commit messages:
      `GITLINT_RANGE=origin/main..HEAD scripts/build-in-container.sh gitlint`
- [ ] `scripts/build-in-container.sh lint test` passes.
- [ ] New behavior has tests, and the tests work offline.
- [ ] `README.md` and `CONTRIBUTING.md` describe any changed behavior.
- [ ] The `status --porcelain=v1` output, the exit codes and the network
      policy are unchanged (see "Stable interfaces" in `CONTRIBUTING.md`).
