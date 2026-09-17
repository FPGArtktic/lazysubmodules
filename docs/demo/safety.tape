# SPDX-License-Identifier: GPL-3.0-only
# Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>
#
# safety.tape - an update is all or nothing
#
# kernel is behind, app has an uncommitted change and fresh was never
# cloned. The update of all three is refused with exit status 3 and changes
# nothing, not even kernel. After the change in app is discarded, an update
# with --fetch clones fresh through the mirror and updates kernel.
#
# Regenerate with scripts/record-demos.sh safety, from the repository root.

Output "docs/demo/safety.gif"
Source "docs/demo/setup.tape"

Type "lazysubmodules status kernel app fresh"
Sleep 400ms
Enter
Wait /^firmware \$$/
Sleep 3s

Type "lazysubmodules update kernel app fresh"
Sleep 400ms
Enter
Wait+Screen /exit status 3: refused/
Wait /^firmware \$$/
Sleep 3s

Type "# All or nothing: kernel was safe to update, but nothing changed:"
Enter
Type "git status --short; lazysubmodules status kernel"
Sleep 400ms
Enter
Wait /^firmware \$$/
Sleep 3.5s

Type "git -C apps/app restore NEWS"
Sleep 400ms
Enter
Wait /^firmware \$$/
Sleep 1s
Type "lazysubmodules update --fetch kernel app fresh"
Sleep 400ms
Enter
Wait+Screen /fresh: v1\.1\.0 .*cloned/
Wait /^firmware \$$/
Sleep 4s
