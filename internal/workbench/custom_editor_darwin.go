//go:build darwin

package workbench

func customEditorSupported() bool { return true }

// The wildcard keeps command-line editors selectable alongside application
// bundles. Viva's macOS picker treats it as an unrestricted application filter.
func customEditorFilterPattern() string { return "*.app;*" }

// macOS editors can be application bundles or command-line executables.
func customEditorExtension() string { return "" }
