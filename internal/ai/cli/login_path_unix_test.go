//go:build !windows

package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadLoginPathTakesPATHFromShellOutput(t *testing.T) {
	shell := filepath.Join(t.TempDir(), "shell")
	script := "#!/bin/sh\necho 'motd noise'\necho 'PATH=/opt/homebrew/bin:/usr/bin'\n"
	if err := os.WriteFile(shell, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	if got := readLoginPath(shell); got != "/opt/homebrew/bin:/usr/bin" {
		t.Fatalf("login PATH = %q", got)
	}
	if got := readLoginPath(filepath.Join(t.TempDir(), "missing")); got != "" {
		t.Fatalf("missing shell reported a PATH: %q", got)
	}
}
