package workbench

import (
	"testing"

	"github.com/vivaui/viva/ui"
)

func TestSFTPPathWindowsNativeInputNavigatesWithoutSendingKeysToTheShell(t *testing.T) {
	state, session, connection, _ := openTestSFTP(t)
	state.sftpPathInput = true
	driver := formEditingDriver(t, state)
	if err := driver.Pump(); err != nil {
		t.Fatal(err)
	}
	focusFormField(t, driver, contextID(hitSFTPPath))
	formChord(t, driver, ui.ModifierControl, 'a')
	if err := driver.Type("/home/test/docs"); err != nil {
		t.Fatal(err)
	}
	if got := state.EditorFor(hitSFTPPath).String(); got != "/home/test/docs" {
		t.Fatalf("native editor contains %q", got)
	}
	if err := driver.KeyDown(ui.KeyEnter, ""); err != nil {
		t.Fatal(err)
	}
	waitSFTP(t, state, session, func(view *sftpView) bool { return !view.busy && view.path == "/home/test/docs" })
	if got := connection.shell(t).typed(); got != "" {
		t.Fatalf("path editing sent %q to the terminal", got)
	}
}
