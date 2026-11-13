//go:build windows

package selfupdate

import (
	"os/exec"
	"testing"

	"golang.org/x/sys/windows"
)

// The restarted application must not join a new process group: Windows disables
// Ctrl+C for every process in one, and the console programs the application's
// local terminals host inherit the same state, so the user's interrupt stops
// working (issue 804). Being detached is what lets it outlive the update helper.
func TestDetachDoesNotDisableCtrlCForTheRestartedApplication(t *testing.T) {
	cmd := exec.Command("motty.exe")
	detach(cmd)
	if cmd.SysProcAttr == nil {
		t.Fatal("detach left the child attached")
	}
	if cmd.SysProcAttr.CreationFlags&windows.CREATE_NEW_PROCESS_GROUP != 0 {
		t.Fatal("detach puts the restarted application in a Ctrl+C-disabled process group")
	}
	if cmd.SysProcAttr.CreationFlags&detachedProcess == 0 {
		t.Fatal("detach does not detach the restarted application")
	}
}
