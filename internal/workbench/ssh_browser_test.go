package workbench

import (
	"fmt"
	"testing"

	"github.com/vivaui/viva/gfx"
	"motty/internal/portforward"
	"motty/internal/sshbrowser"
	"motty/internal/sshconn"
)

func TestTabMenuOffersSSHBrowserOnlyWhileConnected(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	conn := connect(t, state, world)
	tab := state.deck.ActiveWorkspace()
	check := func(want bool) {
		t.Helper()
		var canvas gfx.Canvas
		state.Paint(&canvas, state.viewport)
		rightClickTab(state, hitTabList.At(int(tab)))
		state.Paint(&canvas, state.viewport)
		hit, found := menuHit(&canvas, state, hitTabMenuBrowserOpen)
		if found != want || found && hit.Disabled {
			t.Fatalf("SSH browser menu item = %+v, found %v, want %v", hit, found, want)
		}
		state.tabMenu.open = false
	}

	check(false)
	conn.set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseConnected })
	state.Wake()
	check(true)

	session := &state.sessions[0]
	session.host.Protocol = protocolTelnet
	check(false)
	session.host.Protocol = protocolSSH

	state.disconnect(session)
	check(false)
}

func TestSSHBrowserOpensTheChosenFamily(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	state.sshBrowser = sshbrowser.FamilyEdge
	conn := connect(t, state, world)
	conn.set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseConnected })
	state.Wake()

	previous := startSSHBrowser
	t.Cleanup(func() { startSSHBrowser = previous })
	var opened sshbrowser.Family
	startSSHBrowser = func(_ portforward.DialFunc, family sshbrowser.Family) (*sshbrowser.Browser, error) {
		opened = family
		return nil, nil
	}
	state.openSSHBrowser(&state.sessions[0])
	if opened != sshbrowser.FamilyEdge {
		t.Fatalf("opened family = %q, want %q", opened, sshbrowser.FamilyEdge)
	}
}

func TestSSHBrowserNamesAMissingChosenFamily(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	state.sshBrowser = sshbrowser.FamilyChrome
	conn := connect(t, state, world)
	conn.set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseConnected })
	state.Wake()

	previous := startSSHBrowser
	t.Cleanup(func() { startSSHBrowser = previous })
	startSSHBrowser = func(portforward.DialFunc, sshbrowser.Family) (*sshbrowser.Browser, error) {
		return nil, sshbrowser.ErrBrowserNotFound
	}
	state.openSSHBrowser(&state.sessions[0])
	want := fmt.Sprintf(state.messages.SSHBrowserChosenNotFoundFmt, state.messages.SSHBrowserChrome)
	if state.status != want {
		t.Fatalf("status = %q, want %q", state.status, want)
	}
}
