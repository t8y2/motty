//go:build darwin || linux || windows

package workbench

import (
	"context"
	"runtime"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"motty/internal/i18n"
	"motty/internal/localshell"
	"motty/internal/remotestats"
	"motty/internal/sshconn"
)

func TestSavedWSLHostKeepsItsCatalogIdentityAndSettings(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.openHostDialog()
	editor := &state.editor
	editor.setProtocol(protocolWSL)
	editor.label.Insert("Project shell")
	editor.group.Insert("Development")
	editor.tags.Insert("Linux, Projects")
	editor.wslUseUser = true
	if got := editor.validate(nil, 0, state.messages); got != state.messages.WSL.ErrUser {
		t.Fatalf("empty custom user: %q", got)
	}
	editor.wslUser.Insert("alice")
	editor.wslDirectory.Insert("/home/alice/my project")
	editor.wslCommand.Insert("printf 'ready\\n'")
	editor.wslNoGUI, editor.noHistory = true, true
	state.submitHostDialog()
	host, ok := state.hostByID(state.selectedHostID)
	if state.dialogError != "" || !ok || !host.isWSL() || host.Address != "" || host.Port != 0 {
		t.Fatalf("save default WSL host: %+v, %q", host, state.dialogError)
	}
	state.sessions = append(state.sessions, Session{ID: 1, HostID: host.ID, host: host, Title: host.Label})
	state.sessions[0].localDirectory = `\\wsl.localhost\Ubuntu\home\alice`
	saved := state.snapshot()
	if len(saved.Sessions) != 1 || saved.Sessions[0].Local || saved.Sessions[0].CatalogID != host.CatalogID {
		t.Fatalf("tab lost its catalog identity: %+v", saved.Sessions)
	}
	state.hosts, _ = decodeHosts(saved.Hosts, nil, 1)
	if sources := state.restoreSessions(saved.Sessions, 1); len(sources) != 1 || sources[0] == 0 {
		t.Fatalf("default WSL tab did not restore: %v", sources)
	}
	restored := state.sessions[0]
	if got := restored.host; got.CatalogID != host.CatalogID || got.User != "alice" || got.Group != host.Group ||
		got.Tags != host.Tags || got.WSLDirectory != host.WSLDirectory || got.LoginCommand != host.LoginCommand || !got.WSLNoGUI || !got.NoHistory {
		t.Fatalf("restored host = %+v", got)
	}
	if runtime.GOOS == "windows" && restored.localDirectory != saved.Sessions[0].LocalDirectory {
		t.Fatalf("restored folder = %q", restored.localDirectory)
	}
	entry := saved.Hosts[len(saved.Hosts)-1]
	imported, ok := state.hostFromCatalogEntry(entry)
	if !ok || imported.Address != host.Address || imported.User != host.User || imported.WSLDirectory != host.WSLDirectory || imported.LoginCommand != host.LoginCommand || imported.WSLNoGUI != host.WSLNoGUI {
		t.Fatalf("imported WSL host = %+v (%v)", imported, ok)
	}
}

func TestSavedWSLHostSharesDistributionBetweenShellFilesAndMonitor(t *testing.T) {
	host := Host{ID: 42, Protocol: protocolWSL, User: "alice", WSLDirectory: "/home/alice/my project", LoginCommand: "echo ready", WSLNoGUI: true}
	conn := newWSLHostConn(host, "", func() string { return "Ubuntu-22.04" }, i18n.For(i18n.English).WSL)
	if conn.startErr != nil {
		t.Fatal(conn.startErr)
	}
	config := conn.shellConfig(sshconn.ShellConfig{IntegrateShell: true, IntegrationToken: "test-token"}, "windows")
	if config.Shell != localshell.WSLShell("Ubuntu-22.04") || config.WSLUser != host.User || config.WSLDirectory != host.WSLDirectory ||
		!strings.Contains(config.WSLScript, "unset DISPLAY WAYLAND_DISPLAY PULSE_SERVER") {
		t.Fatalf("WSL shell config = %+v", config)
	}
	state := &State{}
	session := &Session{HostID: host.ID, host: host, conn: conn, dialedProtocol: protocolWSL}
	if got := state.localPanelStart(session); got != `\\wsl.localhost\Ubuntu-22.04\` {
		t.Fatalf("files open at %q", got)
	}
	if runner, ok := monitorRunnerFor(session).(remotestats.WSL); !ok || runner.Distro != "Ubuntu-22.04" || runner.User != host.User {
		t.Fatalf("monitor runner = %+v", runner)
	}
	var term terminal
	term.armInitialization(state, session)
	if term.login.line != host.LoginCommand || !term.login.local {
		t.Fatalf("startup command = %+v", term.login)
	}
}

func TestSavedWSLHostFormListsDistributionsAndOptionalUser(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("WSL is a Windows host protocol")
	}
	state := newTestState(t, testOptions(t))
	state.localShellMenu.discoverWSL = func(context.Context) []localshell.Choice {
		return localshell.WSLChoices([]string{"Ubuntu", "Debian"})
	}
	state.openHostDialog()
	base := app.Config{Width: 960, Height: 720, Tree: Tree(), Paint: state.Paint, OnResize: state.Resize, OnPointer: state.Pointer, OnKey: state.Key}
	view, err := headless.Open(state.WindowConfig(base), nil, headless.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	click := func(label string) {
		t.Helper()
		for _, hit := range view.Snapshot().Hits {
			if hit.Label == label && hit.Bounds.Width > 0 && hit.Bounds.Height > 0 {
				must(view.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2))
				must(view.Rest())
				return
			}
		}
		t.Fatalf("no control %q", label)
	}
	must(view.Rest())
	click("WSL")
	if state.editor.protocol != protocolWSL || state.editor.wslDistro != "" {
		t.Fatalf("new WSL form = %v, %q", state.editor.protocol, state.editor.wslDistro)
	}
	if listing := state.localShellMenu.listing; listing != nil {
		choices := <-listing
		listing <- choices
	}
	click(state.messages.WSL.UseUser)
	if !state.editor.wslUseUser {
		t.Fatal("custom user option did not turn on")
	}
	for _, hit := range view.Snapshot().Hits {
		if hit.Role == gfx.RoleCombobox && hit.Label == state.messages.WSL.Distribution {
			must(view.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2))
			must(view.Rest())
			click("Debian")
			if state.editor.wslDistro != "Debian" {
				t.Fatalf("selected distribution = %q", state.editor.wslDistro)
			}
			return
		}
	}
	t.Fatal("distribution picker is missing")
}
