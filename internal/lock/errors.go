// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package lock

import "errors"

// ErrInvalidEntry is returned for a lock entry that is incomplete or holds a
// value that is not safe to use: an unknown mode, an unusable ref, or a
// commit that is not a full SHA.
var ErrInvalidEntry = errors.New("invalid lock entry")
