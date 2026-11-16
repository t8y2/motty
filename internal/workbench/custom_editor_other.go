//go:build !windows && !darwin

package workbench

func customEditorSupported() bool { return false }

func customEditorFilterPattern() string { return "" }

func customEditorExtension() string { return "" }
