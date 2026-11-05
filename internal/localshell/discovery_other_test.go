//go:build !windows

package localshell

import (
	"path/filepath"
	"testing"
)

// TestUnixShellDiscoveryFindsTheStandardShells checks the non-Windows list the
// settings offer: at least the platform's /bin/sh, by an absolute path.
func TestUnixShellDiscoveryFindsTheStandardShells(t *testing.T) {
	choices := InstalledWindowsShells()
	if len(choices) == 0 {
		t.Fatal("no local shells were discovered")
	}
	found := false
	for _, choice := range choices {
		if !filepath.IsAbs(choice.Path) {
			t.Fatalf("shell %q is not an absolute path", choice.Path)
		}
		if choice.Path == "/bin/sh" {
			found = true
		}
	}
	if !found {
		t.Fatalf("shells %+v do not include /bin/sh", choices)
	}
}
