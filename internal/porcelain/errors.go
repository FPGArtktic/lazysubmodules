// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package porcelain

import "errors"

// ErrUnknownState is returned for a submodule state that the porcelain
// format does not define; writing it would break the stable format.
var ErrUnknownState = errors.New("state not defined by the porcelain format")
