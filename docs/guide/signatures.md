<!-- SPDX-License-Identifier: GPL-3.0-only -->
<!-- Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com> -->

(guide-signatures)=

# Verify signatures

`verify --signatures` checks the locked tag or commit of each submodule
with Git's own signature verification.

## What is checked

`--signatures` adds one check, `signature`, to the checks of `verify`:

| Tracking mode | Command | Object |
|---|---|---|
| `tag`, `tag-pattern` | `git verify-tag` | The locked tag |
| `branch`, `commit` | `git verify-commit` | The locked commit |

The check passes when Git reports a good signature. Which keys count as
good is decided by your Git configuration, not by LazySubmodules. The
check is offline, like the rest of `verify`.

```sh
lazysubmodules verify --signatures              # every managed submodule
lazysubmodules verify --signatures signed app   # selected submodules
```

## Configure Git

**GPG** signatures work as configured in Git and GnuPG: the signing keys
must be in your keyring and trusted.

**SSH** signatures need two settings:

```sh
git config --global gpg.format ssh
git config --global gpg.ssh.allowedSignersFile ~/.config/git/allowed_signers
```

Each line of the allowed signers file names a signer, the namespaces the
key may sign for, and the public key, for example:

```text
rita@example.org namespaces="git" ssh-ed25519 AAAAC3NzaC1lZD…
```

The demo sets exactly these two settings in its `env.sh`. Check a single
tag with Git first, to rule out configuration problems:

```console
$ git -C libs/signed tag -v v1.0.0
Good "git" signature for rita@example.org with ED25519 key SHA256:0jzFw2zflzAtkx0mH9Xx/AygPB2Di20GVOOgJ6jTDWg
object 5d1133e0516f426db2702661540e7f92fa4b4c89
type commit
tag v1.0.0
tagger Rita Upstream <rita@example.org> 1767420000 +0000

release v1.0.0
```

The key fingerprint differs in every run of the demo, because the demo
creates a new key.

## Example

In the demo, `signed` follows an SSH-signed tag, and `app` follows an
annotated tag without a signature:

```{literalinclude} ../../examples/transcript.txt
:language: console
:start-at: "$ lazysubmodules verify --signatures signed app"
:end-at: "[exit status 4: verification failed]"
```

The `[exit status N: …]` line comes from the demo, not from
LazySubmodules. With `--signatures`, every submodule has one check more,
so `signed` passes 8 checks instead of 7.

## Failure messages

The `signature` line repeats what Git reported:

| Situation | Line |
|---|---|
| Annotated tag without a signature | `signature: tag v1.2.0: error: no signature found` |
| Lightweight tag | `signature: tag v6.6.10: error: refs/tags/v6.6.10: cannot verify a non-tag object of type commit.` |
| Commit without a good signature (branch or commit mode) | `signature: commit 29b295ca56c4: no valid signature` |

A lightweight tag is only a name for a commit and cannot carry a
signature. For such a release, track the tag anyway and rely on the lock
file, or ask upstream to publish signed tags.

## Requirements of the demo

The demo signs tag `v1.0.0` of `signed` with an SSH key that `ssh-keygen`
creates. Without `ssh-keygen`, the tag stays unsigned, and its signature
check fails as well.

## See also

- {ref}`ref-cli-verify-checks`: every check of `verify`.
- [Detect moved tags](moved-tags.md): what the `tag` check catches.
- [Scripts and CI](scripting.md): run `verify` in a pipeline.
- [Releases and versions](../project/releases.md): how LazySubmodules
  signs its own releases.
