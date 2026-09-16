// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package git

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Exit status of "git config" when the key or section to change is missing.
const configExitMissing = 5

// ConfigEntry is one variable of a git configuration file.
type ConfigEntry struct {
	// Key is "section.subsection.variable"; the section and variable names
	// are lowercase, the subsection keeps its case.
	Key string
	// Value is the raw value; a variable without "=" (implicit true) has an
	// empty value.
	Value string
}

// ConfigList reads all variables of a configuration file.
//
// The file is read with "git config -f <file> --null --list"; include
// directives are not followed. Like the other Config helpers, it refuses a
// file that exists but is not a regular file, such as a symbolic link.
//
// Context: file is relative to dir unless absolute.
// Return: the entries in file order, nil entries and a nil error when the
// file does not exist, an error wrapping ErrNotRegularFile, or an error.
func (r *Runner) ConfigList(ctx context.Context, dir, file string) ([]ConfigEntry, error) {
	exists, err := configFileExists(dir, file)
	if err != nil || !exists {
		return nil, err
	}
	out, err := r.Run(ctx, dir, "config", "-f", file, "--null", "--list")
	if err != nil {
		return nil, err
	}
	return parseConfigList(out), nil
}

// parseConfigList parses the output of "git config --null --list", where
// each record is "key\nvalue\0", or "key\0" for an implicit true.
func parseConfigList(out string) []ConfigEntry {
	records := splitNUL(out)
	if len(records) == 0 {
		return nil
	}
	entries := make([]ConfigEntry, 0, len(records))
	for _, rec := range records {
		key, value, _ := strings.Cut(rec, "\n")
		entries = append(entries, ConfigEntry{Key: key, Value: value})
	}
	return entries
}

// configFileExists reports whether file, relative to dir unless absolute,
// exists. The file itself, not the directories leading to it, must be a
// regular file: git follows a symbolic link when it writes the file, so a
// link committed to a repository could redirect a write outside of it.
func configFileExists(dir, file string) (bool, error) {
	if !filepath.IsAbs(file) {
		file = filepath.Join(dir, file)
	}
	fi, err := os.Lstat(file)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("config file: %w", err)
	case !fi.Mode().IsRegular():
		return false, fmt.Errorf("config file %s: %w", file, ErrNotRegularFile)
	}
	return true, nil
}

// ConfigSet sets a variable in a configuration file, creating the file when
// needed.
//
// All existing values of the variable are replaced by the single new value.
//
// Context: file is relative to dir unless absolute; key is
// "section.subsection.variable".
// Return: nil, an error wrapping ErrNotRegularFile, or an error such as
// *Error.
func (r *Runner) ConfigSet(ctx context.Context, dir, file, key, value string) error {
	if _, err := configFileExists(dir, file); err != nil {
		return err
	}
	_, err := r.Run(ctx, dir, "config", "-f", file, "--replace-all", key, value)
	return err
}

// ConfigUnset removes all values of a variable from a configuration file.
//
// Context: file is relative to dir unless absolute.
// Return: nil, also when the variable or the file does not exist, an error
// wrapping ErrNotRegularFile, or an error such as *Error.
func (r *Runner) ConfigUnset(ctx context.Context, dir, file, key string) error {
	exists, err := configFileExists(dir, file)
	if err != nil || !exists {
		return err
	}
	_, err = r.Run(ctx, dir, "config", "-f", file, "--unset-all", key)
	if exitCode(err) == configExitMissing {
		return nil
	}
	return err
}

// ConfigRemoveSection removes a section, such as "submodule.kernel", with all
// its variables from a configuration file.
//
// Git creates a missing file and fails for a missing section, so the file is
// checked first, and a failure is ignored when the file has no variable in
// that section. A section header without variables is removed as well.
//
// Context: file is relative to dir unless absolute.
// Return: nil, also when the section or the file does not exist, an error
// wrapping ErrNotRegularFile, or an error such as *Error.
func (r *Runner) ConfigRemoveSection(ctx context.Context, dir, file, section string) error {
	exists, err := configFileExists(dir, file)
	if err != nil || !exists {
		return err
	}
	_, err = r.Run(ctx, dir, "config", "-f", file, "--remove-section", section)
	if err == nil {
		return nil
	}
	entries, listErr := r.ConfigList(ctx, dir, file)
	if listErr != nil {
		return errors.Join(err, listErr)
	}
	for _, e := range entries {
		if inSection(e.Key, section) {
			return err
		}
	}
	return nil
}

// inSection reports whether key belongs to section. Section names compare
// case-insensitively, subsection names exactly.
func inSection(key, section string) bool {
	i := strings.LastIndexByte(key, '.')
	if i < 0 {
		return false
	}
	keyName, keySub, keyHasSub := strings.Cut(key[:i], ".")
	name, sub, hasSub := strings.Cut(section, ".")
	return strings.EqualFold(keyName, name) && keyHasSub == hasSub && keySub == sub
}
