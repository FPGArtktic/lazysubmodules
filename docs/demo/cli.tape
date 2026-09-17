# SPDX-License-Identifier: GPL-3.0-only
# Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>
#
# cli.tape - the command line interface and its output for scripts
#
# Shows "lazysubmodules status", the porcelain v1 format laid out by column
# (with the path, the configured ref and the two commit IDs hidden, so that
# the table fits), a dry run and a real update of kernel and u-boot with
# --commit, and the commit message that update wrote.
#
# Regenerate with scripts/record-demos.sh cli, from the repository root.

Output "docs/demo/cli.gif"
Source "docs/demo/setup.tape"

Type "lazysubmodules status"
Sleep 400ms
Enter
Wait /^firmware \$$/
Sleep 3.5s

Ctrl+L
Type "# Porcelain v1 for scripts: TAB-separated fields, stable across releases"
Enter
Type `lazysubmodules status --porcelain=v1 | column -t -s $'\t' -H 2,4,6,7`
Sleep 400ms
Enter
Wait /^firmware \$$/
Sleep 3.5s

Ctrl+L
Type "lazysubmodules update --dry-run kernel u-boot"
Sleep 400ms
Enter
Wait /^firmware \$$/
Sleep 2s
Type "lazysubmodules update --commit kernel u-boot"
Sleep 400ms
Enter
Wait /^firmware \$$/
Sleep 2s
Type "git log -1"
Sleep 400ms
Enter
Wait /^firmware \$$/
Sleep 5s
