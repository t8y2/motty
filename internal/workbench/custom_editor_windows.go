//go:build windows

package workbench

import (
	"os/exec"
	"syscall"
)

const createNoWindow = 0x08000000

func customEditorSupported() bool { return true }

func customEditorFilterPattern() string { return "*.exe" }

func customEditorExtension() string { return ".exe" }

func customEditorCommand(executable, path string) *exec.Cmd {
	command := exec.Command(executable, path)
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	return command
}

func launchCustomEditor(executable, path string) error {
	return startDetached(customEditorCommand(executable, path))
}
