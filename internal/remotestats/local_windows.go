package remotestats

import (
	"context"
	"os/exec"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

// localCommand supplies the compressed script over stdin directly to
// PowerShell. Keeping it the owned process lets cancellation stop the sampler.
func localCommand(ctx context.Context, command string) *exec.Cmd {
	process := exec.CommandContext(ctx, "powershell.exe")
	if pipe, ok := strings.CutPrefix(command, windowsCommandPrefix); ok {
		if payload, arguments, found := strings.Cut(pipe, windowsPowerShellPipe); found {
			command = "powershell.exe " + strings.TrimSuffix(arguments, `"`)
			process.Stdin = strings.NewReader(payload + "\n")
		}
	}
	process.SysProcAttr = &syscall.SysProcAttr{CmdLine: command, HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
	return process
}
