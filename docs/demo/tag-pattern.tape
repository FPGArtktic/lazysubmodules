# SPDX-License-Identifier: GPL-3.0-only
# Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>
#
# tag-pattern.tape - follow the newest release matching a tag pattern
#
# kernel follows the pattern v6.6.* and is locked at v6.6.9. In the
# terminal user interface, "p" opens the pattern dialog, which counts the
# matching local tags while the pattern is edited. The new pattern v6.*
# also matches the pre-releases v6.7-rc1 and v6.6.11-rc1; the update skips
# them and picks v6.6.10, which "lazysubmodules status" then shows as the
# locked tag.
#
# Regenerate with scripts/record-demos.sh tag-pattern, from the repository
# root.

Output "docs/demo/tag-pattern.gif"
Source "docs/demo/setup.tape"

Type "lazysubmodules tui"
Sleep 400ms
Enter
Wait+Screen /quirky .* ok/
Wait+Screen /Update adds \(1\)/
Sleep 2s

# Edit the pattern; the dialog counts the matching tags as you type.
Type "p"
Wait+Screen /11 local tags match \(1 pre-release\)/
Sleep 2.5s
Left
Sleep 300ms
Backspace@500ms 2
Wait+Screen /pattern: v6\.\*/
Wait+Screen /12 local tags match \(2 pre-release\)/
Sleep 3s
Enter
Wait+Screen /It matches 12 local tags/
Sleep 2s
Type "y"
Wait+Screen /kernel +tag-pattern +v6\.\*/
Sleep 2s

# Update: the dry run in the dialog shows the pick, v6.6.10.
Type "u"
Wait+Screen /Update kernel: v6\.6\.9 .* -> v6\.6\.10/
Sleep 3s
Type "y"
Wait+Screen /kernel: updated to v6\.6\.10/
Wait+Screen /kernel +tag-pattern +v6\.\* +08dcd0d ok/
Sleep 3s

Type "q"
Wait /^firmware \$$/
Sleep 500ms
Type "lazysubmodules status kernel"
Sleep 400ms
Enter
Wait /^firmware \$$/
Sleep 3s
