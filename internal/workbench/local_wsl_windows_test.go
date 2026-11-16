//go:build windows

package workbench

import (
	"testing"

	"motty/internal/localshell"
)

func TestADistributionsFolderIsPassedOnWithoutBeingOpenedFromWindows(t *testing.T) {
	folder := `\\wsl.localhost\Ubuntu-Not-Installed\home\me\gone`
	if got := localStartupDirectory(folder, localshell.WSLShell("Ubuntu-Not-Installed")); got != folder {
		t.Fatalf("a distribution's own shell was started in %q, want %q", got, folder)
	}
	// A Windows shell cannot start there, and finding that out would start WSL.
	if got := localStartupDirectory(folder, ""); got != "" {
		t.Fatalf("a Windows shell was started in %q", got)
	}
}
