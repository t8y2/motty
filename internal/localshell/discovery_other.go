//go:build !windows

package localshell

import (
	"os/exec"
)

// InstalledWindowsShells is the cross-platform name for local shell discovery.
// On non-Windows systems it scans standard Unix shell paths and PATH.
func InstalledWindowsShells() []Choice {
	return discoverShells([]shellCandidate{
		{"zsh", []string{"/bin/zsh", "/usr/local/bin/zsh", "/opt/homebrew/bin/zsh"}},
		{"bash", []string{"/bin/bash", "/usr/local/bin/bash", "/opt/homebrew/bin/bash"}},
		{"fish", []string{"/usr/bin/fish", "/usr/local/bin/fish", "/opt/homebrew/bin/fish"}},
		{"nu", []string{"nu"}},
		{"sh", []string{"/bin/sh"}},
	}, defaultShell(), exec.LookPath)
}
