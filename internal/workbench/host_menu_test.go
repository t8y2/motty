package workbench

import (
	"errors"
	"net"
	"strconv"
	"testing"

	"github.com/vivaui/viva/gfx"
)

func rightClickHost(state *State, host uint32) {
	pointer(state, gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonSecondary, Target: hitHostList.At(int(host)), X: 400, Y: 300})
}

func TestRightClickOnAHostOpensItsMenuWithoutChangingWhatIsChecked(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	first, second := state.hosts[0].ID, state.hosts[1].ID
	state.Click(hitHostCheckList.At(int(first)))

	rightClickHost(state, second)
	if !state.hostMenu.open || state.hostMenu.host != second {
		t.Fatalf("menu = %+v", state.hostMenu)
	}
	if len(state.hostSelections) != 1 || state.hostSelections[0] != first || state.selectedHostID != second {
		t.Fatalf("selection = %v selected %d, want the original check and the pointed host highlighted", state.hostSelections, state.selectedHostID)
	}
	canvas, _ := paintedTexts(state)
	for _, id := range []gfx.ID{hitHostMenuConnect, hitHostMenuAddress, hitHostMenuName, hitHostMenuEdit, hitHostMenuCopy, hitHostMenuMove, hitHostMenuDelete} {
		if hit, ok := hitByID(canvas, id); !ok || hit.Disabled {
			t.Errorf("menu item %d: %+v found=%v", id, hit, ok)
		}
	}
	chooseMenu(t, state, hitHostMenuConnect)
	if state.hostMenu.open || len(world.conns) != 1 {
		t.Fatalf("Connect: open=%v dialed=%d", state.hostMenu.open, len(world.conns))
	}
	opened := state.activeSessionID
	state.Click(hitHosts)
	rightClickHost(state, second)
	chooseMenu(t, state, hitHostMenuConnect)
	if len(world.conns) != 1 || len(state.sessions) != 1 || state.activeSessionID != opened {
		t.Fatal("Connect must reuse the session from the first menu action")
	}
}

func TestHostMenuCopiesThePointedHostAddressAndConnectionName(t *testing.T) {
	state := newTestState(t, testOptions(t))
	target := state.hosts[1]
	var copied []string
	state.options.SetClipboardText = func(value string) error {
		copied = append(copied, value)
		return nil
	}

	rightClickHost(state, target.ID)
	chooseMenu(t, state, hitHostMenuAddress)
	wantAddress := net.JoinHostPort(target.Address, strconv.Itoa(int(target.Port)))
	if state.hostMenu.open || len(copied) != 1 || copied[0] != wantAddress {
		t.Fatalf("Copy Address and Port: open=%v copied=%q target=%q", state.hostMenu.open, copied, wantAddress)
	}

	rightClickHost(state, target.ID)
	chooseMenu(t, state, hitHostMenuName)
	if state.hostMenu.open || len(copied) != 2 || copied[1] != target.Label {
		t.Fatalf("Copy Connection Name: open=%v copied=%q target=%q", state.hostMenu.open, copied, target.Label)
	}
}

func TestConnectionAddressAndPortHandlesIPv6AndPortlessHosts(t *testing.T) {
	for _, test := range []struct {
		name string
		host Host
		want string
	}{
		{name: "hostname", host: Host{Address: "server.example.com", Port: 2222}, want: "server.example.com:2222"},
		{name: "IPv4", host: Host{Address: "192.0.2.10", Port: 22}, want: "192.0.2.10:22"},
		{name: "IPv6", host: Host{Address: "2001:db8::10", Port: 22}, want: "[2001:db8::10]:22"},
		{name: "serial", host: Host{Address: "/dev/ttyUSB0"}},
		{name: "missing address", host: Host{Port: 22}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := connectionAddressAndPort(test.host); got != test.want {
				t.Fatalf("connectionAddressAndPort() = %q, want %q", got, test.want)
			}
		})
	}
}

// A clipboard that refuses the write must say so: the menu closes either way, so
// silence would leave the choice looking as if it had worked.
func TestAFailedConnectionCopyIsReported(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.options.SetClipboardText = func(string) error { return errors.New("clipboard unavailable") }
	rightClickHost(state, state.hosts[1].ID)
	chooseMenu(t, state, hitHostMenuAddress)
	if state.status != "clipboard unavailable" {
		t.Fatalf("status = %q, want the clipboard's reason", state.status)
	}
}

// The connection copies read as one group: a separator between the address, the
// name and the host would say they are different kinds of action.
func TestHostMenuKeepsTheConnectionCopyRowsTogether(t *testing.T) {
	state := newTestState(t, testOptions(t))
	rightClickHost(state, state.hosts[1].ID)
	canvas, _ := paintedTexts(state)
	order := []gfx.ID{hitHostMenuAddress, hitHostMenuName, hitHostMenuCopy, hitHostMenuEdit}
	previous := float32(0)
	for index, id := range order {
		hit, ok := hitByID(canvas, id)
		if !ok {
			t.Fatalf("menu item %d is missing", id)
		}
		if index > 0 && hit.Bounds.Y <= previous {
			t.Fatalf("row %d sits at %.0f, above the row before it at %.0f", id, hit.Bounds.Y, previous)
		}
		previous = hit.Bounds.Y
	}
}

func TestHostMenuEditsCopiesAndDeletesTheHostPointedAt(t *testing.T) {
	state := newTestState(t, testOptions(t))
	target := state.hosts[1]
	checked := state.hosts[0].ID
	state.Click(hitHostCheckList.At(int(checked)))

	rightClickHost(state, target.ID)
	chooseMenu(t, state, hitHostMenuEdit)
	if state.modal != modalNewHost || state.hostDialogMode != hostDialogEdit || state.hostDialogID != target.ID {
		t.Fatalf("Edit: modal %v mode %v id %d", state.modal, state.hostDialogMode, state.hostDialogID)
	}
	state.Click(hitDialogCancel)

	rightClickHost(state, target.ID)
	chooseMenu(t, state, hitHostMenuCopyEdit)
	if state.modal != modalNewHost || state.hostDialogMode != hostDialogCopy {
		t.Fatalf("Copy and Edit: modal %v mode %v", state.modal, state.hostDialogMode)
	}
	state.Click(hitDialogCancel)

	rightClickHost(state, target.ID)
	chooseMenu(t, state, hitHostMenuDelete)
	if state.modal != modalDeleteHost || len(state.hostActionIDs) != 1 || state.hostActionIDs[0] != target.ID {
		t.Fatalf("Delete: modal %v ids %v", state.modal, state.hostActionIDs)
	}
	if len(state.hostSelections) != 1 || state.hostSelections[0] != checked {
		t.Fatalf("context actions changed the checked hosts to %v", state.hostSelections)
	}
}

func TestHostMenuMovesOnlyTheUncheckedHostPointedAt(t *testing.T) {
	state := newTestState(t, testOptions(t))
	checked, target := state.hosts[0].ID, state.hosts[1].ID
	state.Click(hitHostCheckList.At(int(checked)))

	rightClickHost(state, target)
	chooseMenu(t, state, hitHostMenuMove)
	if !state.hostGroupMenu || len(state.hostActionIDs) != 1 || state.hostActionIDs[0] != target {
		t.Fatalf("Move: open=%v ids=%v", state.hostGroupMenu, state.hostActionIDs)
	}
	if len(state.hostSelections) != 1 || state.hostSelections[0] != checked {
		t.Fatalf("Move changed the checked hosts to %v", state.hostSelections)
	}
}

func TestRightClickInsideSeveralCheckedHostsKeepsThemAndDisablesTheSingleOnes(t *testing.T) {
	state := newTestState(t, testOptions(t))
	a, b := state.hosts[0].ID, state.hosts[1].ID
	state.Click(hitHostCheckList.At(int(a)))
	state.Click(hitHostCheckList.At(int(b)))

	rightClickHost(state, b)
	if len(state.hostSelections) != 2 {
		t.Fatalf("selection = %v, want both kept", state.hostSelections)
	}
	canvas, _ := paintedTexts(state)
	for id, disabled := range map[gfx.ID]bool{hitHostMenuConnect: true, hitHostMenuAddress: true, hitHostMenuName: true, hitHostMenuEdit: true, hitHostMenuCopyEdit: true, hitHostMenuCopy: false, hitHostMenuCopyTo: false, hitHostMenuMove: false, hitHostMenuDelete: false} {
		if hit, ok := hitByID(canvas, id); !ok || hit.Disabled != disabled {
			t.Errorf("item %d: found=%v disabled=%v, want disabled=%v", id, ok, hit.Disabled, disabled)
		}
	}
	chooseMenu(t, state, hitHostMenuDelete)
	if len(state.hostActionIDs) != 2 {
		t.Fatalf("Delete acts on %v, want both", state.hostActionIDs)
	}
}

func TestHostMenuIsAsWideAsItsLabelsAskInBothLanguages(t *testing.T) {
	for _, chinese := range []bool{false, true} {
		state := newTestState(t, testOptions(t))
		if chinese {
			useChinese(t, state)
		}
		rightClickHost(state, state.hosts[0].ID)
		canvas, _ := paintedTexts(state)
		hit, ok := menuHit(canvas, state, hitHostMenuMove)
		if !ok {
			t.Fatal("no menu")
		}
		widest := float32(0)
		for _, label := range []string{state.messages.Connect, state.messages.CopyAddressAndPort, state.messages.CopyConnectionName, state.messages.EditHost, state.messages.CopyHost, state.messages.PasteHost, state.messages.CopyHostTo, state.messages.CopyHostAndEdit, state.messages.MoveHostGroup, state.messages.DeleteHost} {
			widest = max(widest, canvas.TextWidth(label, 13))
		}
		// A context menu is never narrower than Vega's 9rem, whatever its labels.
		if hit.Bounds.Width > max(widest+80, 144) || hit.Bounds.Width < widest+8 {
			t.Errorf("chinese=%v: a row is %.0f wide for a %.0f wide label", chinese, hit.Bounds.Width, widest)
		}
	}
}

// Choosing Move group hands the menu over to the group picker: the picker takes
// the menu's place, at the point the menu was opened at, rather than opening in
// the window's top left corner.
func TestMoveGroupFromAHostsMenuOpensThePickerWhereTheMenuWas(t *testing.T) {
	state := newTestState(t, testOptions(t))
	rightClickHost(state, state.hosts[1].ID)
	canvas, _ := paintedTexts(state)
	connect, ok := hitByID(canvas, hitHostMenuConnect)
	if !ok || connect.Bounds.X <= 0 || connect.Bounds.Y <= 0 {
		t.Fatalf("the host menu was not opened at the pointer: %+v", connect.Bounds)
	}
	chooseMenu(t, state, hitHostMenuMove)
	canvas, _ = paintedTexts(state)
	first, ok := hitByID(canvas, hitHostGroupNone)
	if !ok || first.Bounds.X != connect.Bounds.X || first.Bounds.Y != connect.Bounds.Y {
		t.Fatalf("the group picker first row opened at %+v, want where the menu's first row was %+v", first.Bounds, connect.Bounds)
	}
}
