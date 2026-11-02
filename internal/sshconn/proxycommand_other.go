//go:build !windows

package sshconn

import (
	"os/exec"
	"sync/atomic"
	"syscall"
)

// proxyCommandProcess runs a command line the way OpenSSH does: the shell reads
// it, and exec leaves the program in the shell's place. It leads a process group
// of its own, which is how the programs it starts are found again.
func proxyCommandProcess(line string) (*exec.Cmd, error) {
	process := exec.Command("/bin/sh", "-c", "exec "+line)
	process.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return process, nil
}

// startProxyCommand starts the program; killing its tree signals the process
// group it leads, until release says the program has been waited for and its
// number may belong to someone else.
func startProxyCommand(process *exec.Cmd) (proxyCommandTree, error) {
	if err := process.Start(); err != nil {
		return proxyCommandTree{}, err
	}
	group := process.Process.Pid
	var gone atomic.Bool
	return proxyCommandTree{
		kill: func() {
			if !gone.Load() {
				_ = syscall.Kill(-group, syscall.SIGKILL)
			}
		},
		release: func() { gone.Store(true) },
	}, nil
}
