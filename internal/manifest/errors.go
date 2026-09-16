// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package manifest

import "errors"

var (
	// ErrInvalidMode is returned for an lsm-mode value that is not a known
	// tracking mode.
	ErrInvalidMode = errors.New("invalid tracking mode")
	// ErrNotFound is returned when .gitmodules has no submodule of the given
	// name.
	ErrNotFound = errors.New("no such submodule")
)
