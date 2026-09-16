// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package gittest

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// sshSigningKey creates the key files of SSHSigningKey in dir.
func sshSigningKey(t testing.TB, dir string) (string, bool) {
	t.Helper()
	keygen, err := exec.LookPath("ssh-keygen")
	if err != nil {
		return "", false
	}
	key := filepath.Join(dir, "signing_key")
	cmd := exec.CommandContext(t.Context(), keygen,
		"-q", "-t", "ed25519", "-N", "", "-C", Email, "-f", key)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("gittest: ssh-keygen: %v\n%s", err, out)
	}
	pub, err := os.ReadFile(key + ".pub")
	if err != nil {
		t.Fatalf("gittest: %v", err)
	}
	// "<type> <base64 key> <comment>"
	fields := strings.Fields(string(pub))
	if len(fields) < 2 {
		t.Fatalf("gittest: malformed public key %q", pub)
	}
	WriteFile(t, key+".allowed_signers", Email+" "+fields[0]+" "+fields[1]+"\n")
	return key, true
}
