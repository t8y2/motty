//go:build windows

package localshell

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWindowsShellDiscoveryFindsInstallationsOutsidePATH(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "PowerShell", "7", "pwsh.exe")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("test executable location"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())
	t.Setenv("ProgramW6432", root)
	t.Setenv("ProgramFiles", root)
	t.Setenv("ProgramFiles(x86)", "")
	choices := InstalledWindowsShells()
	for _, choice := range choices {
		if choice.Name == "PowerShell 7" {
			if choice.Path != path {
				t.Fatalf("PowerShell path=%q, want %q", choice.Path, path)
			}
			return
		}
	}
	t.Fatalf("installed PowerShell was not detected: %+v", choices)
}

// TestWindowsShellDiscoveryFindsCygwin covers the setup program's default
// roots beside the system drive, which PATH does not carry (issue #435).
func TestWindowsShellDiscoveryFindsCygwin(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "cygwin64", "bin", "bash.exe")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("test executable location"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())
	t.Setenv("SystemDrive", root)
	choices := InstalledWindowsShells()
	for _, choice := range choices {
		if choice.Name == "Cygwin" {
			if choice.Path != path {
				t.Fatalf("Cygwin path=%q, want %q", choice.Path, path)
			}
			return
		}
	}
	t.Fatalf("installed Cygwin was not detected: %+v", choices)
}
