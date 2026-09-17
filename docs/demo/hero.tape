# SPDX-License-Identifier: GPL-3.0-only
# Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>
#
# hero.tape - overview of the terminal user interface
#
# Starts "lazysubmodules tui" on the demo superproject, moves through the
# table while the preview follows, opens the details of u-boot, updates
# u-boot (branch main, behind) after the confirmation, waits for its state
# to turn ok, shows the help and quits.
#
# Regenerate with scripts/record-demos.sh hero, from the repository root.

Output "docs/demo/hero.gif"
Source "docs/demo/setup.tape"

Type "lazysubmodules tui"
Sleep 400ms
Enter
Wait+Screen /quirky .* ok/
Sleep 2s

# Walk down the table; the preview follows the selection.
Down
Sleep 1.5s
Down@300ms 7
Wait+Screen /app dirty/
Sleep 2s
Type "g"
Sleep 400ms
Down
Wait+Screen /Update adds \(1\)/
Sleep 1.5s

# Details of u-boot.
Enter
Wait+Screen /Details of u-boot/
Sleep 3s
Escape
Sleep 800ms

# Update u-boot: the dialog shows the dry run first.
Type "u"
Wait+Screen /Stage the result/
Sleep 2.5s
Type "y"
Wait+Screen /u-boot: updated to main .*staged/
Wait+Screen /u-boot +branch +main +8e9fc7a ok/
Sleep 2.5s

# Help, then quit.
Type "?"
Wait+Screen /Navigation/
Sleep 3s
Type "q"
Sleep 800ms
Type "q"
Wait /^firmware \$$/
Sleep 1s
