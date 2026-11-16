package remotestats

import (
	"context"
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// localCommand runs a command line the way WindowsCommand builds it: the
// program is its first word and reads the rest of the line itself. No console
// window opens for it.
func localCommand(ctx context.Context, command string) *exec.Cmd {
	process := exec.CommandContext(ctx, "powershell.exe")
	process.SysProcAttr = &syscall.SysProcAttr{CmdLine: command, HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
	return process
}
