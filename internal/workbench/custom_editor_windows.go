//go:build windows

package workbench

func customEditorSupported() bool { return true }

func customEditorFilterPattern() string { return "*.exe" }

func customEditorExtension() string { return ".exe" }
