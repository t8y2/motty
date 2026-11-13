//go:build windows

package remotestats

import (
	"context"
	"errors"
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// startCommand starts /bin/sh in the distribution with one script. --exec runs
// the program directly, without the distribution's login shell, and no console
// window opens for it.
func (w WSL) startCommand(ctx context.Context, command string) (*exec.Cmd, error) {
	if w.Exe == "" || w.Distro == "" {
		return nil, errors.New("remote stats: WSL executable and distribution are required")
	}
	process := exec.CommandContext(ctx, w.Exe, wslArgs(w.Distro, command)...)
	process.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
	return process, nil
}
