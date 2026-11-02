//go:build darwin

package workbench

import (
	"os/exec"
	"path/filepath"
	"strings"
)

func customEditorSupported() bool { return true }

// The wildcard keeps command-line editors selectable alongside application
// bundles. Viva's macOS picker treats it as an unrestricted application filter.
func customEditorFilterPattern() string { return "*.app;*" }

// macOS editors can be application bundles or command-line executables.
func customEditorExtension() string { return "" }

func launchCustomEditor(executable, path string) error {
	var command *exec.Cmd
	if strings.EqualFold(filepath.Ext(executable), ".app") {
		command = exec.Command("open", "-a", executable, path)
	} else {
		command = exec.Command(executable, path)
	}
	return command.Start()
}
