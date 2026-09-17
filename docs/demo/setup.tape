# SPDX-License-Identifier: GPL-3.0-only
# Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>
#
# setup.tape - settings and hidden setup shared by the LazySubmodules tapes
#
# Every tape sources this file right after its Output line. The terminal is
# 110 columns wide and 30 rows high: VHS 0.11 sets the size in pixels,
# which gives exactly that grid with this font and padding. The hidden part
# builds the demo superproject with docs/demo/prepare.sh and enters it, so
# the recording starts at an empty prompt. VHS runs from the repository
# root; scripts/record-demos.sh runs it in a container with everything the
# tapes need.

Require git
Require lazysubmodules

Set Shell "bash"
Set FontFamily "JetBrains Mono"
Set FontSize 14
Set Width 1054
Set Height 590
Set Padding 16
Set Theme "Catppuccin Mocha"
Set CursorBlink false
Set Framerate 20
Set TypingSpeed 35ms
Set WaitTimeout 20s

Hide
Type@1ms "docs/demo/prepare.sh /tmp/lsm-demo && . /tmp/lsm-demo/shell.sh && clear"
Enter
Wait@120s /^firmware \$$/
Show
