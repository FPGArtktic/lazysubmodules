// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package core

import (
	"errors"
	"fmt"
)

var (
	// ErrNotFound is returned when a submodule named by the caller is not
	// listed in .gitmodules.
	ErrNotFound = errors.New("no such submodule")
	// ErrInvalidArgument is returned for a value given by the caller that is
	// not acceptable, such as a malformed ref name or path.
	ErrInvalidArgument = errors.New("invalid argument")
	// ErrRefused is wrapped by every error that reports a state in which an
	// operation would be unsafe.
	ErrRefused = errors.New("refused")
	// ErrUnmanaged is returned when a command other than status names a
	// submodule without tracking configuration.
	ErrUnmanaged = fmt.Errorf("%w: submodule is not managed by lazysubmodules", ErrRefused)
	// ErrDirty is returned when a submodule working tree has uncommitted
	// changes.
	ErrDirty = fmt.Errorf("%w: submodule has uncommitted changes", ErrRefused)
	// ErrMissingRef is returned when the configured ref of a submodule is
	// invalid or does not resolve to a commit in the local refs.
	ErrMissingRef = fmt.Errorf("%w: ref not found in local refs", ErrRefused)
	// ErrUninitialized is returned when a submodule is not checked out and
	// its repository is not available locally.
	ErrUninitialized = fmt.Errorf("%w: submodule is not initialized (use --fetch)", ErrRefused)
	// ErrUnrelatedStaged is returned when a commit of the updated submodules
	// would include changes that concern no selected submodule: other
	// staged paths, or changes of .gitmodules or the lock file outside the
	// sections of the selected submodules, staged or not.
	ErrUnrelatedStaged = fmt.Errorf("%w: the commit would include unrelated changes",
		ErrRefused)
	// ErrUnmergedIndex is returned when a commit is requested while the
	// index of the superproject holds unresolved merge conflicts.
	ErrUnmergedIndex = fmt.Errorf("%w: the index has unresolved merge conflicts", ErrRefused)
	// ErrSymlinkPath is returned when the path of a submodule leads through a
	// symbolic link. Git never checks out a submodule there, and following
	// the link could modify an unrelated repository.
	ErrSymlinkPath = fmt.Errorf("%w: submodule path contains a symbolic link", ErrRefused)
	// ErrNotSubmodule is returned when the index of the superproject records
	// no submodule (gitlink) at the path that .gitmodules names. Git ignores
	// such an entry; the path may be a directory of the superproject itself or
	// a repository inside another submodule, which must not be modified.
	ErrNotSubmodule = fmt.Errorf("%w: the index records no submodule at its path",
		ErrRefused)
	// ErrPathExists is returned when a new submodule would be added at a
	// path that already exists or belongs to another submodule.
	ErrPathExists = fmt.Errorf("%w: path already exists", ErrRefused)
	// ErrVerify is returned when at least one verification check failed.
	ErrVerify = errors.New("verification failed")
)
