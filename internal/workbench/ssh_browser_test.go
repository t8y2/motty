package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
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
		hit, found := hitByID(&canvas, hitTabMenuBrowserOpen)
		if found != want || found && hit.Disabled {
			t.Fatalf("SSH browser menu item = %+v, found %v, want %v", hit, found, want)
		}
		state.Click(hitTabMenuDismiss)
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
