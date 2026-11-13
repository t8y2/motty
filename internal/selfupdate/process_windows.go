package selfupdate

import (
	"context"
	"errors"
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

const detachedProcess = 0x00000008

// detach makes cmd outlive the program that starts it. It must not set
// HideWindow: that puts SW_HIDE in the child's STARTUPINFO, and Windows applies
// it to the first ShowWindow the child makes, so the restarted application would
// run with its main window hidden. DETACHED_PROCESS already keeps a console from
// appearing.
//
// It must not set CREATE_NEW_PROCESS_GROUP: that disables Ctrl+C for every
// process in the new group, children included, and the restarted application
// passes the disabled state on to the console programs its local terminals host.
// DETACHED_PROCESS alone already lets the child outlive this program.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: detachedProcess}
}

// waitExit returns once the process pid is gone.
func waitExit(ctx context.Context, pid int) error {
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
			return nil // no such process
		}
		return err
	}
	defer windows.CloseHandle(handle)
	for {
		event, err := windows.WaitForSingleObject(handle, 100)
		if err != nil {
			return err
		}
		if event == windows.WAIT_OBJECT_0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}
}
