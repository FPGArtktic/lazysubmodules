<!-- SPDX-License-Identifier: GPL-3.0-only -->
<!-- Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com> -->

(ref-cli-verify)=

# `verify`

Check that the lock file, the gitlinks committed in the superproject, the
checked-out submodules and the locked tags agree, and optionally their
signatures.

## Synopsis

```text
lazysubmodules verify [<name>...] [--signatures]
```

`verify` works offline and changes nothing.

## Help text

```{literalinclude} _generated/verify.txt
:language: text
:lines: 5-
```

## Options

| Option | Effect |
|---|---|
| `<name>...` | Verify these managed submodules. Without names, every managed submodule is verified |
| `--signatures` | Also verify the signature of the locked tag or commit |

## Behaviour

- **What is compared.** The configuration and the lock entry come from the
  working tree copies of `.gitmodules` and `.lsm.lock`. The gitlink comes
  from the `HEAD` commit of the superproject. The submodule `HEAD` and the
  tags come from the submodule.
- **Committed state.** The `gitlink` check reads `HEAD`, so an update that
  is staged but not yet committed fails `verify` until it is committed.
- **Local refs only.** `verify` never fetches. A tag that was moved on the
  remote is detected after [`fetch`](fetch.md) has updated the local tag.
- **Parallel.** Up to eight submodules are verified at the same time; the
  output keeps the `.gitmodules` order.

(ref-cli-verify-checks)=

## Checks

The checks run in this order. When a check fails, the checks that depend
on it are left out.

| Check | Passes when |
|---|---|
| `lock-entry` | `.lsm.lock` has an entry for the submodule |
| `lock-config` | The lock entry matches `.gitmodules`: the same mode; for `branch` and `tag`, the same ref; for `tag-pattern`, the locked tag exists and matches the pattern; for `commit`, the locked commit is the configured SHA |
| `lock-commit` | The locked commit is a full SHA with the length of the superproject's object format (40 or 64 hexadecimal digits) |
| `gitlink` | The gitlink in the `HEAD` commit of the superproject equals the locked commit |
| `initialized` | The submodule is checked out |
| `head` | The submodule `HEAD` equals the locked commit |
| `tag` | `tag` and `tag-pattern` mode only: the locked tag still resolves to the locked commit |
| `signature` | Only with `--signatures`: `git verify-tag` accepts the locked tag (`tag`, `tag-pattern`), or `git verify-commit` accepts the locked commit (`branch`, `commit`) |

- Without a lock entry, or with a locked commit of the wrong length, only
  `initialized` follows.
- A submodule that is not checked out gets no checks after `initialized`.

The number of checks therefore depends on the mode and the state:

| Situation | Checks |
|---|---|
| No lock entry | 2 |
| Wrong length of the locked commit | 4 |
| Not checked out | 5 |
| Checked out, `branch` or `commit` mode | 6 |
| Checked out, `tag` or `tag-pattern` mode | 7 |
| With `--signatures`, checked out | one more |

### Signatures

`--signatures` uses Git's own verification, so GPG, SSH and X.509
signatures work as configured in Git, for example with `gpg.format` and
`gpg.ssh.allowedSignersFile`. A lightweight tag has no signature and always
fails the check. {ref}`guide-signatures` shows the setup.

## Output

For each submodule, `verify` prints a summary line on standard output,
followed by one indented line per failed check. When a check fails, a last
line on standard error names the failed submodules. The demo in its first
state:

```text
kernel: ok (7 checks)
u-boot: ok (6 checks)
fpga.core: ok (7 checks)
crypto lib: ok (6 checks)
tools: ok (6 checks)
theme: failed (1 of 5 checks)
  initialized: submodule is not checked out
fresh: failed (2 of 5 checks)
  lock-config: cannot list tags: submodule repository is missing
  initialized: submodule is not checked out
app: ok (7 checks)
sdk: ok (7 checks)
mirror-lib: ok (6 checks)
broken: failed (1 of 7 checks)
  lock-config: lock records tag v1.0.0, configuration has v9.9.9
signed: ok (7 checks)
quirky: ok (7 checks)
lazysubmodules: verification failed: theme, fresh, broken
```

`legacy` is missing from the list because it is unmanaged. Without managed
submodules, `verify` prints `no managed submodules`.

### Failure messages

These lines were produced on the demo:

| Check | Line |
|---|---|
| `lock-entry` | `lock-entry: no entry in .lsm.lock` |
| `lock-config` | `lock-config: lock records mode branch, configuration has tag` |
| `lock-config` | `lock-config: locked tag v6.6.9 does not exist or does not match v6.7*` |
| `lock-config` | `lock-config: configured invalid tag pattern "": empty value` |
| `lock-commit` | `lock-commit: locked commit "29b2…bc58" is not a full commit name with 40 hexadecimal digits` (shortened here) |
| `gitlink` | `gitlink: HEAD of the superproject records 8106f614767a, locked 08dcd0dc8f98` |
| `gitlink` | `gitlink: HEAD of the superproject records no gitlink at third_party/sdk2` |
| `head` | `head: HEAD a926cfcc5101 differs from locked commit 556baa13f789` |
| `tag` | `tag: tag v2.3.1 points to 7c6069a8a36b, locked 556baa13f789 (moved tag)` |
| `signature` | `signature: tag v1.2.0: error: no signature found` |
| `signature` | `signature: tag v6.6.10: error: refs/tags/v6.6.10: cannot verify a non-tag object of type commit.` |
| `signature` | `signature: commit 8e9fc7a2511a: no valid signature` |

The first two `lock-config` lines follow a `set` that was not yet applied
with `update`. The second `gitlink` line comes from a submodule that `add`
created and that is not committed yet.

## Exit status

| Status | When |
|---|---|
| 0 | Every check of every selected submodule passed |
| 1 | An unknown name, or an invalid `.gitmodules` or `.lsm.lock` |
| 2 | A usage error |
| 3 | An unmanaged submodule was named |
| 4 | At least one check failed |
| 5 | A `git` command failed for another reason than a missing ref or a bad signature |

## Examples

```sh
# In CI, after cloning the superproject.
lazysubmodules fetch
lazysubmodules verify

# Include signatures of the locked tags and commits.
lazysubmodules verify --signatures

# One submodule.
lazysubmodules verify fpga.core
```

## See also

- {ref}`guide-moved-tags`: a moved tag from `fetch` to `verify`.
- {ref}`guide-signatures`: signature setup.
- {ref}`guide-scripting`: `verify` in CI.
- {ref}`ref-files-lock`: what the lock file records.
- {ref}`expl-how`: how the four records relate.
