//go:build !windows

package selfupdate

import (
	"context"
	"errors"
	"os/exec"
	"syscall"
	"time"
)

// detach makes cmd outlive the program that starts it.
func detach(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }

// waitExit returns once the process pid is gone. It polls: the helper is not
// the process's parent and there is nothing to be woken by.
func waitExit(ctx context.Context, pid int) error {
	for {
		err := syscall.Kill(pid, 0)
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}
