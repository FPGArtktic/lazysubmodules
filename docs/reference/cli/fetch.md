<!-- SPDX-License-Identifier: GPL-3.0-only -->
<!-- Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com> -->

(ref-cli-fetch)=

# `fetch`

Download the branches and tags of managed submodules from `origin`, and
clone or initialize the submodules that are not checked out.

## Synopsis

```text
lazysubmodules fetch [<name>...]
```

`fetch` uses the network.

## Help text

```{literalinclude} _generated/fetch.txt
:language: text
:lines: 5-
```

## Behaviour

For each selected managed submodule, in `.gitmodules` order:

1. **Initialize.** A submodule that is not checked out is initialized with
   `git submodule update --init`, at the commit that the superproject
   records. Its repository is cloned when it is missing. The native `update`
   setting of the submodule, such as `update = none`, does not prevent this.
2. **Fetch.** `fetch` runs `git fetch --tags --force --prune origin` in the
   submodule. Nested submodules are not fetched, whatever
   `fetch.recurseSubmodules` and `submodule.recurse` say.

Without names, every managed submodule is fetched.

```{warning}
`--force` lets local tags follow tags that were moved on the remote, which
is how `status` and `verify` detect moved tags. A local tag with the same
name as a remote tag is therefore replaced, and `--prune` removes
remote-tracking branches that no longer exist on the remote.
```

`fetch` changes neither `.gitmodules`, `.lsm.lock` nor the index, and it
moves no submodule that is already checked out. Run
[`update`](update.md) to apply what was fetched.

## Output

One line per submodule on standard output. Git's own output goes to
standard error.

| Line | Meaning |
|---|---|
| `<name>: fetched` | The submodule was checked out already and has been fetched |
| `<name>: initialized and fetched` | Its repository existed, for example after `git submodule deinit`; it was checked out and fetched |
| `<name>: cloned and fetched` | Its repository was cloned, checked out and fetched |
| `no managed submodules` | There is nothing to fetch |

A moved tag and a new release on the demo:

```{literalinclude} ../../../examples/transcript.txt
:language: console
:start-at: "$ lazysubmodules fetch fpga.core sdk"
:end-before: "[exit status 0: success]"
```

`t [tag update]` is Git's notice that the local tag `v2.3.1` now follows
the remote. The `From file://…` lines appear because the demo rewrites its
URLs to a local mirror.

Fetching every submodule of the demo in its first state initializes
`theme` and clones `fresh`:

```text
theme: initialized and fetched
fresh: cloned and fetched
```

## Exit status

| Status | When |
|---|---|
| 0 | Every selected submodule was fetched |
| 1 | An unknown name, or an invalid `.gitmodules` or `.lsm.lock` |
| 2 | A usage error |
| 3 | An unmanaged submodule was named |
| 5 | A `git` command failed, for example when the remote cannot be reached |

## Examples

```sh
# Download what the remotes offer, then look at it.
lazysubmodules fetch
lazysubmodules status

# After a fresh clone of the superproject: clone, initialize and fetch
# every managed submodule, then check the result.
lazysubmodules fetch
lazysubmodules verify
```

## See also

- {ref}`guide-network`: mirrors, credentials and offline work.
- {ref}`guide-moved-tags`: what a moved tag looks like after `fetch`.
- [`update`](update.md): `update --fetch` fetches before it resolves.
- {ref}`ref-states`: states that change after a fetch.
