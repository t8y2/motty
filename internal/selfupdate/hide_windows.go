package selfupdate

import (
	"os/exec"
	"syscall"
)

// hide keeps a console window from flashing up for a child of a graphical program.
func hide(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
}
