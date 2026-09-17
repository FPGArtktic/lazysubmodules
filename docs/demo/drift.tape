# SPDX-License-Identifier: GPL-3.0-only
# Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>
#
# drift.tape - detect a release tag that was moved upstream
#
# fpga.core follows tag v2.3.1 and is ok. The upstream repository (a work
# repository of the demo that pushes to the mirror) moves the tag to a
# later commit. After "lazysubmodules fetch", status reports drift and
# verify fails with exit status 4.
#
# Regenerate with scripts/record-demos.sh drift, from the repository root.

Output "docs/demo/drift.gif"
Source "docs/demo/setup.tape"

Type "lazysubmodules status fpga.core"
Sleep 400ms
Enter
Wait /^firmware \$$/
Sleep 2.5s

Type "# Upstream, the maintainer of fpga-core moves release tag v2.3.1:"
Enter
Type "git -C ../work/fpga-core tag -f -a -m 'release v2.3.1' v2.3.1 main"
Sleep 400ms
Enter
Wait /^firmware \$$/
Sleep 1s
Type "git -C ../work/fpga-core push -q -f origin v2.3.1"
Sleep 400ms
Enter
Wait /^firmware \$$/
Sleep 1.5s

Type "lazysubmodules fetch fpga.core"
Sleep 400ms
Enter
Wait /^firmware \$$/
Sleep 2s
Type "lazysubmodules status fpga.core"
Sleep 400ms
Enter
Wait+Screen /fpga\.core .* drift/
Wait /^firmware \$$/
Sleep 2.5s
Type "lazysubmodules verify fpga.core"
Sleep 400ms
Enter
Wait+Screen /exit status 4: verification failed/
Wait /^firmware \$$/
Sleep 5s
