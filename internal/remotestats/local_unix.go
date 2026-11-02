//go:build !windows

package remotestats

import (
	"context"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// localCommand runs the fixed monitor script under the system shell, in its
// own process group so that canceling takes the pipeline's resident children
// (top, on macOS) with it. The system tool directories come first: sysctl and
// route live in /usr/sbin, which an application started from the Finder or a
// launcher may not have.
func localCommand(ctx context.Context, command string) *exec.Cmd {
	process := exec.CommandContext(ctx, "/bin/sh", "-c", command)
	process.Env = append(os.Environ(), "PATH=/usr/bin:/bin:/usr/sbin:/sbin:"+os.Getenv("PATH"))
	process.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	process.Cancel = func() error { return syscall.Kill(-process.Process.Pid, syscall.SIGKILL) }
	process.WaitDelay = time.Second
	return process
}
