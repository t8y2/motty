//go:build !windows

package localshell

import (
	"os/exec"
	"syscall"
)

func prepareAvailabilityProcess(process *exec.Cmd) {
	process.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	process.Cancel = func() error {
		if process.Process == nil {
			return nil
		}
		return syscall.Kill(-process.Process.Pid, syscall.SIGKILL)
	}
}

func cleanupAvailabilityProcess(process *exec.Cmd) {
	if process.Process != nil {
		_ = syscall.Kill(-process.Process.Pid, syscall.SIGKILL)
	}
}
