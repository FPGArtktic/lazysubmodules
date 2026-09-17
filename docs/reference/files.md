<!-- SPDX-License-Identifier: GPL-3.0-only -->
<!-- Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com> -->

(ref-files)=

# Files: .gitmodules and .lsm.lock

LazySubmodules keeps its data in two git-config files that are committed to
the superproject: the tracking configuration and the lock file.

| File | Content | Written by |
|---|---|---|
| `.gitmodules` | Git's submodule configuration, plus `lsm-mode` and `lsm-ref` | `add`, `set`, `update` |
| `.lsm.lock` | The resolved ref and commit of each managed submodule | `add`, `update` |

Both files live at the top level of the superproject. LazySubmodules reads
and writes them only with `git config -f <file>`, so Git does the parsing
and the quoting, and unknown keys stay in place.

## Common rules

- **Sections.** Each submodule has a section `[submodule "<name>"]`. The
  name is the submodule name; it may contain dots and spaces, and it is
  compared exactly. Section and key names are compared without regard to
  case, as Git does.
- **Duplicate keys.** When a key occurs more than once, the last value wins.
- **Regular files.** Both files must be regular files. A symbolic link in
  their place is refused (status 1), so that a cloned repository cannot
  redirect a write to a file outside of it:

  ```text
  lazysubmodules: read .lsm.lock: config file $DEMO/firmware/.lsm.lock: not a regular file
  ```

- **Syntax errors.** A file that Git cannot parse makes every command fail
  with the error of Git (status 5):

  ```text
  lazysubmodules: read .gitmodules: git config -f .gitmodules --null --list: exit status 128: fatal: bad config line 76 in file .gitmodules
  ```

- **Which copy.** `status` and `verify` read the copies in the working tree.
  `update` also reads the copies in the index and in `HEAD`, to find out
  what the superproject records.

(ref-files-gitmodules)=

## `.gitmodules`

Git ignores keys it does not know, so the tracking configuration lives in
the submodule sections of `.gitmodules`, under the `lsm-` prefix:

```ini
[submodule "kernel"]
	path = kernel
	url = https://git.example.org/linux.git
	lsm-mode = tag-pattern
	lsm-ref = v6.6.*
```

### Keys

| Key | Owner | Meaning |
|---|---|---|
| `lsm-mode` | LazySubmodules | Tracking mode: `branch`, `tag`, `tag-pattern` or `commit` |
| `lsm-ref` | LazySubmodules | Branch name, tag name, glob pattern, or commit SHA |
| `branch` | Git | Native key. LazySubmodules writes it for `lsm-mode = branch` and removes it for the other modes |
| `path` | Git | Path of the submodule; LazySubmodules reads it |
| `url` | Git | URL of the submodule; `git submodule` uses it to clone |
| `update`, `ignore`, `shallow` and others | Git | Left unchanged |

`lsm-mode` and `lsm-ref` are a stable interface.

### `lsm-mode`

- **Managed or not.** A submodule without `lsm-mode` is unmanaged:
  `status` shows it as [unmanaged]{.lsm-state .lsm-state-unmanaged}, and no
  command modifies it until `set` puts it under management. A stray
  `lsm-ref` or lock entry of an unmanaged submodule is ignored.
- **Exact spelling.** The value must be one of the four modes, spelled
  exactly. Any other value, including `Tag`, makes every command that reads
  the file fail with status 1:

  ```text
  lazysubmodules: .gitmodules: submodule "kernel": lsm-mode: invalid tracking mode "semver"
  ```

### `lsm-ref`

| Mode | Value | Example |
|---|---|---|
| `branch` | Branch name on `origin` | `main` |
| `tag` | Tag name | `v2.3.1` |
| `tag-pattern` | Glob as accepted by `git tag --list` | `v6.6.*` |
| `commit` | Commit SHA in lowercase hexadecimal, with 7 to 40 digits (64 in SHA-256 repositories); `add` and `set` always store the full SHA | `5da6927f70f5cb5408fe3a77c0aa33877a3062c5` |

- **Invalid values.** A value that is empty, starts with `-`, contains
  white space or control characters, or is not a valid ref name makes the
  submodule [missing-ref]{.lsm-state .lsm-state-missing-ref}. `update`
  refuses it, and `verify` fails its `lock-config` check:

  ```text
  lazysubmodules: kernel: refused: bad ref in .gitmodules: invalid tag pattern "": empty value
  ```

- **Existence.** A value that is valid but does not resolve in the local
  refs also gives `missing-ref`.

(ref-files-branch-key)=

### The native `branch` key

Git uses `submodule.<name>.branch` for `git submodule update --remote`.
LazySubmodules keeps it in step with the tracking mode:

- **Branch mode.** `add --branch`, `set --branch` and `update` write
  `branch = <lsm-ref>`, so that `git submodule update --remote` follows the
  same branch without LazySubmodules.
- **Other modes.** `set` and `update` remove the key. Without it,
  `git submodule update --remote` would use the default branch of the
  remote, which is not what a tag or commit pins; {ref}`guide-plain-git`
  shows the effect.
- **Repair.** When the key does not follow the mode, `update` rewrites it
  and reports `restored .gitmodules`.

On the demo, `set --branch main` on the unmanaged submodule `legacy`
writes all three keys:

```{literalinclude} ../../examples/transcript.txt
:language: console
:start-at: "$ git config -f .gitmodules --get-regexp"
:end-before: "$ lazysubmodules status legacy"
```

### Other rules

- **Skipped entries.** Entries that Git itself does not treat as a
  submodule are skipped: an entry without a path, one whose name is empty
  or has a `..` component, and one whose path is not a clean relative path
  inside the working tree. As in Git, `path` and `url` values that start
  with `-` are ignored.
- **Native keys stay.** `update = none` does not stop LazySubmodules from
  checking the submodule out, and `ignore = all` does not hide its real
  state from `status`.
- **Name of an added submodule.** `add` names the submodule after its path,
  as `git submodule add` does.

### Example

[`examples/.gitmodules`](https://github.com/FPGArtktic/lazysubmodules/blob/main/examples/.gitmodules)
is the file that the demo ends with, with comments. The URLs point to
`git.example.org` and are only illustrations.

```{literalinclude} ../../examples/.gitmodules
:language: ini
:start-after: "# See examples/README.md."
:caption: examples/.gitmodules
```

(ref-files-lock)=

## `.lsm.lock`

The lock file records, for each managed submodule, the mode and ref that
were resolved and the commit they resolved to:

```ini
[submodule "kernel"]
	mode = tag-pattern
	ref = v6.6.9
	commit = 8106f614767a75b6567271b1db80d6ade3acb7fb
```

### Keys

| Key | Meaning |
|---|---|
| `mode` | Tracking mode used for the resolution |
| `ref` | Resolved ref: the tag for `tag` and `tag-pattern`, the branch for `branch`, the full SHA for `commit` |
| `commit` | Full commit SHA in lowercase: 40 hexadecimal digits, or 64 in SHA-256 repositories |

For a tag pattern, `ref` is the tag that the pattern selected, so the lock
file shows which release is in use: `v6.6.*` in `.gitmodules`, `v6.6.9` in
`.lsm.lock`.

### Rules

- **Writers.** `add` and `update` write the entry of a submodule and stage
  the file together with the gitlink. `set` never touches the lock file.
- **Order.** A new entry is appended, so entries appear in the order in
  which they were first written. A rewritten entry keeps its place.
- **Other keys.** Keys and sections that LazySubmodules does not know are
  ignored and kept.
- **Ignore rules.** The file is staged even when an ignore rule such as
  `*.lock` matches it. Commit it to the superproject.
- **Missing file or entry.** A missing file is an empty lock. A managed
  submodule without an entry is [behind]{.lsm-state .lsm-state-behind}, and
  `verify` fails its `lock-entry` check.
- **Validation.** Every entry must have a known mode, a ref that is not
  empty, does not start with `-` and has no white space or control
  characters, and a full lowercase SHA. Otherwise every command that reads
  the file fails with status 1:

  ```text
  lazysubmodules: .lsm.lock: invalid lock entry "kernel": commit: "8106f61" is not a full lowercase hexadecimal SHA
  ```

- **Length of the SHA.** A SHA of the wrong length for the repository, such
  as 64 digits in a SHA-1 repository, fails the `lock-commit` check of
  `verify`.

### Why a lock file

Tags can be moved: a maintainer can force-push a release tag to another
commit. The lock file records the commit that the tag had when the
submodule was updated. After `fetch` has moved the local tag, `status`
shows [drift]{.lsm-state .lsm-state-drift} and `verify` fails with status 4.
{ref}`guide-moved-tags` walks through this.

The lock file is also what `status` compares with to report
[behind]{.lsm-state .lsm-state-behind}, and what the generated commit
message uses for the old ref.

### Example

[`examples/.lsm.lock`](https://github.com/FPGArtktic/lazysubmodules/blob/main/examples/.lsm.lock)
belongs to `examples/.gitmodules`. Its commits are the real commits of the
demo repositories.

```{literalinclude} ../../examples/.lsm.lock
:language: ini
:start-after: "# written; legacy came last. See examples/README.md."
:caption: examples/.lsm.lock
```

The entry of `legacy` comes last: the demo puts that submodule under
management with `set`, and its first `update` runs after the other entries
exist.

## Removing a submodule from management

Remove both tracking keys and the lock entry, then commit both files:

```sh
git config -f .gitmodules --unset submodule.fresh.lsm-mode
git config -f .gitmodules --unset submodule.fresh.lsm-ref
git config -f .lsm.lock --remove-section submodule.fresh
git add .gitmodules .lsm.lock
```

`status fresh` then shows `unmanaged`, and `verify` leaves the submodule
out.

## See also

- {ref}`expl-how`: how the files relate to the gitlinks and checkouts.
- {ref}`guide-change-tracking`: changing the tracking configuration.
- {ref}`guide-moved-tags`: the lock file at work.
- {ref}`ref-cli-verify-checks`: the checks that compare the files.
