package gist

import (
	"os/exec"
	"syscall"
)

// hideWindow keeps gh from flashing a console window over the application.
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
}
