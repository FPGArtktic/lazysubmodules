<!-- SPDX-License-Identifier: GPL-3.0-only -->
<!-- Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com> -->

# Generated branch

The ci workflow (`.github/workflows/ci.yml`) generates this branch
with `scripts/publish-coverage-badge.sh` and replaces it with a
single new commit after every successful run on `main`.
Do not commit to it: the next run discards the change.

- `coverage.json`: the test coverage of `main` for the
  badge in the README, in the shields.io endpoint format.

Source commit: `121c808c52e639f8cf0bfe6f9628c4540977f1a0`

Workflow run: https://github.com/FPGArtktic/lazysubmodules/actions/runs/35278181048
