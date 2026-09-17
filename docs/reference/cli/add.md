<!-- SPDX-License-Identifier: GPL-3.0-only -->
<!-- Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com> -->

(ref-cli-add)=

# `add`

Clone a repository as a new managed submodule, resolve its ref, write the
lock entry and stage the result without committing it.

## Synopsis

```text
lazysubmodules add <url> <path> (--branch B | --tag T | --tag-pattern P | --commit SHA) [--include-prerelease]
```

`add` uses the network: it clones `<url>`.

## Help text

```{literalinclude} _generated/add.txt
:language: text
:lines: 5-
```

The usage line of the help text leaves out `--include-prerelease`, which
the option list includes.

## Arguments and options

| Argument or option | Effect |
|---|---|
| `<url>` | Repository to clone, as `git submodule add` accepts it. It must not be empty, start with `-` or contain control characters |
| `<path>` | Path of the new submodule, relative to the top level of the superproject. It is also the submodule name |
| `--branch <branch>` | Track the tip of the remote branch (`lsm-mode = branch`) |
| `--tag <tag>` | Track the tag (`lsm-mode = tag`) |
| `--tag-pattern <pattern>` | Track the highest version tag that matches the glob (`lsm-mode = tag-pattern`) |
| `--commit <commit>` | Track a commit, given with 7 to 40 lowercase hexadecimal digits (64 in SHA-256 repositories); it is stored as the full SHA |
| `--include-prerelease` | Let `--tag-pattern` select a pre-release tag |

Exactly one of `--branch`, `--tag`, `--tag-pattern` and `--commit` is
required. Branch and tag names must be valid for
`git check-ref-format`; a pattern must be a valid tag name once its glob
characters `*`, `?`, `[` and `]` are replaced. No value may be empty,
start with `-`, or contain white space or control characters.

## Behaviour

1. **Clone.** `add` runs `git submodule add`, with `-b <branch>` in branch
   mode, so the native `branch` key is written as well. The submodule is
   named after its path, as with plain Git.
2. **Configure.** It writes `lsm-mode` and `lsm-ref` to `.gitmodules`.
3. **Resolve.** It resolves the ref in the new clone, following the
   {ref}`resolution rules <expl-resolution>`, checks out the commit and
   writes the lock entry to `.lsm.lock`.
4. **Stage.** It stages `.gitmodules`, `.lsm.lock` and the new gitlink. It
   does not commit.

If a step after the clone fails, the new submodule is removed again and
the original error is reported. `git status --short` then shows no trace
of it.

### Paths

- **Inside the superproject.** `<path>` must be a relative path inside the
  superproject, other than its top level, and without a `.git` component.
  Otherwise `add` exits with status 2:

  ```text
  lazysubmodules: invalid path "../outside": must be a relative path inside the superproject
  lazysubmodules: invalid path "third_party/.git/x": contains a .git component
  ```

- **Not in use.** A path that already exists, belongs to another
  submodule, lies inside a submodule or leads through a file or a
  symbolic link is refused with status 3:

  ```text
  lazysubmodules: kernel: refused: path already exists: used by submodule kernel
  lazysubmodules: kernel/sub: refused: path already exists: inside submodule kernel
  lazysubmodules: src/drivers/README/x: refused: path already exists: it leads through a file
  ```

## Output

One line on standard output names the submodule, the resolved ref and the
abbreviated commit; in commit mode, only the commit. Git's clone progress
goes to standard error. On the demo:

```console
$ lazysubmodules add https://git.example.invalid/sdk.git third_party/sdk2 --tag-pattern 'v2.*'
Cloning into '$DEMO/firmware/third_party/sdk2'...
third_party/sdk2: added at v2.9.0 (68a8743)
$ git status --short
M  .gitmodules
M  .lsm.lock
 m apps/app
A  third_party/sdk2
 m tools/nested
```

The two `m` lines belong to the demo: they are the uncommitted change in
`apps/app` and the nested submodule of `tools/nested`. The other modes
print, for example:

```text
libs/mirror2: added at stable (71eb52c)
libs/crypto two: added at 5da6927
third_party/sdk-rc: added at v3.0.0-rc.2 (6308203)
third_party/theme2: added at v1.0.0 (05f49f3)
```

The third line comes from `--tag-pattern 'v*' --include-prerelease`.
Without `--include-prerelease`, the same pattern selects `v2.9.0`.

## Exit status

| Status | When |
|---|---|
| 0 | The submodule was added and staged |
| 2 | A usage error: a missing or extra argument, no or several tracking options, or an invalid URL, path, ref or pattern |
| 3 | Refused: the path is in use, or the ref does not resolve in the new clone (`ref not found in local refs`) |
| 5 | A `git` command failed, for example the clone |

See {ref}`ref-exit-codes` for all codes.

## Examples

The URLs below are placeholders for your own repositories.

```sh
# Follow the newest stable 2.x release.
lazysubmodules add https://git.example.org/sdk.git third_party/sdk --tag-pattern 'v2.*'

# Follow a branch.
lazysubmodules add https://git.example.org/u-boot.git bootloader/u-boot --branch main

# Pin a commit; an abbreviated SHA is expanded after the clone.
lazysubmodules add https://git.example.org/crypto-lib.git 'libs/crypto lib' --commit 5da6927

# Commit the new submodule with your usual workflow.
git commit -s -m 'deps: add sdk'
```

## See also

- {ref}`guide-add`: a walk-through with every tracking mode.
- {ref}`ref-files`: the keys that `add` writes.
- {ref}`guide-commit`: committing the staged result.
- {ref}`ref-cli-rules`: rules shared by all commands.
