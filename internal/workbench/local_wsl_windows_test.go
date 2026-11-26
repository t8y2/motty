//go:build windows

package workbench

import (
	"context"
	"strings"
	"testing"
	"time"

	"motty/internal/localshell"
	"motty/internal/remotestats"
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

func TestSavedWSLHostOpensAndFollowsTheLiveDefaultDistribution(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	distro := localshell.DefaultWSLDistro()
	if distro == "" || len(localshell.InstalledWSL(ctx)) == 0 {
		t.Skip("no accessible default WSL distribution")
	}
	runner := remotestats.WSL{Exe: localshell.WSLExecutable(), Distro: distro}
	out, err := runner.RunCommand(ctx, "id -un", 256)
	if err != nil {
		t.Fatal(err)
	}
	user := strings.TrimSpace(string(out))
	state := newTestState(t, testOptions(t))
	state.openHostDialog()
	state.editor.setProtocol(protocolWSL)
	state.editor.label.Insert("Live WSL")
	state.editor.wslUseUser = true
	state.editor.wslUser.Insert(user)
	state.editor.wslDirectory.Insert("/tmp")
	state.editor.wslCommand.Insert(`test "$PWD" = /tmp && test -z "${DISPLAY-}${WAYLAND_DISPLAY-}${PULSE_SERVER-}" && cd /usr/lib`)
	state.editor.wslNoGUI = true
	state.submitHostDialog()
	host, ok := state.hostByID(state.selectedHostID)
	if !ok || !host.isWSL() || state.dialogError != "" {
		t.Fatalf("save WSL host: %q", state.dialogError)
	}
	session := state.openHostSession(host)
	if session == nil || session.wslDistro() != distro {
		t.Fatalf("session did not open in %q", distro)
	}
	state.openSFTP(session)
	for {
		state.Wake()
		state.followTerminalDirectory(session)
		if directory, ok := localshell.WSLPath(session.files.lfolder.path, distro); ok && directory == "/usr/lib" {
			break
		}
		if session.term.ended || ctx.Err() != nil {
			t.Fatalf("files at %q, want /usr/lib in %s; terminal: %s", session.files.lfolder.path, distro, session.term.screen.Text())
		}
		time.Sleep(10 * time.Millisecond)
	}
	state.startupReuse = true
	if reused := state.openHostSession(host); reused == nil || reused.ID != session.ID || len(state.sessions) != 1 {
		t.Fatal("opening the saved WSL host again did not reuse its tab")
	}
	out, err = monitorRunnerFor(session).RunCommand(ctx, "head -n 1 /proc/stat", 1024)
	if err != nil || !strings.HasPrefix(string(out), "cpu ") {
		t.Fatalf("WSL monitor did not read Linux CPU stats: %v, %q", err, out)
	}
}
