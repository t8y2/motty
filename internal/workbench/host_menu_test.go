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
	state.Click(hitHostMenuConnect)
	if state.hostMenu.open || len(world.conns) != 1 {
		t.Fatalf("Connect: open=%v dialed=%d", state.hostMenu.open, len(world.conns))
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
	state.Click(hitHostMenuAddress)
	wantAddress := net.JoinHostPort(target.Address, strconv.Itoa(int(target.Port)))
	if state.hostMenu.open || len(copied) != 1 || copied[0] != wantAddress {
		t.Fatalf("Copy Address and Port: open=%v copied=%q target=%q", state.hostMenu.open, copied, wantAddress)
	}

	rightClickHost(state, target.ID)
	state.Click(hitHostMenuName)
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
	state.Click(hitHostMenuAddress)
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
	state.Click(hitHostMenuEdit)
	if state.modal != modalNewHost || state.hostDialogMode != hostDialogEdit || state.hostDialogID != target.ID {
		t.Fatalf("Edit: modal %v mode %v id %d", state.modal, state.hostDialogMode, state.hostDialogID)
	}
	state.Click(hitDialogCancel)

	rightClickHost(state, target.ID)
	state.Click(hitHostMenuCopy)
	if state.modal != modalNewHost || state.hostDialogMode != hostDialogCopy {
		t.Fatalf("Copy: modal %v mode %v", state.modal, state.hostDialogMode)
	}
	state.Click(hitDialogCancel)

	rightClickHost(state, target.ID)
	state.Click(hitHostMenuDelete)
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
	state.Click(hitHostMenuMove)
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
	for id, disabled := range map[gfx.ID]bool{hitHostMenuConnect: true, hitHostMenuAddress: true, hitHostMenuName: true, hitHostMenuEdit: true, hitHostMenuCopy: true, hitHostMenuMove: false, hitHostMenuDelete: false} {
		if hit, ok := hitByID(canvas, id); !ok || hit.Disabled != disabled {
			t.Errorf("item %d: found=%v disabled=%v, want disabled=%v", id, ok, hit.Disabled, disabled)
		}
	}
	state.Click(hitHostMenuDelete)
	if len(state.hostActionIDs) != 2 {
		t.Fatalf("Delete acts on %v, want both", state.hostActionIDs)
	}
}

func TestHostMenuClosesOnAnOutsidePressAndIgnoresOtherRightClicks(t *testing.T) {
	state := newTestState(t, testOptions(t))
	rightClickHost(state, state.hosts[0].ID)
	state.Click(hitHostMenuDismiss)
	if state.hostMenu.open {
		t.Fatal("an outside press left the menu open")
	}
	pointer(state, gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonSecondary, Target: hitSearch, X: 50, Y: 50})
	if state.hostMenu.open {
		t.Fatal("a right click away from a host opened the menu")
	}
	rightClickHost(state, state.hosts[0].ID)
	if result := pointer(state, gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonSecondary, Target: hitHostMenuDismiss}); !result.Handled || state.hostMenu.open {
		t.Fatalf("a secondary press outside: %+v open=%v", result, state.hostMenu.open)
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
		hit, ok := hitByID(canvas, hitHostMenuMove)
		if !ok {
			t.Fatal("no menu")
		}
		widest := float32(0)
		for _, label := range []string{state.messages.Connect, state.messages.CopyAddressAndPort, state.messages.CopyConnectionName, state.messages.EditHost, state.messages.CopyHostTitle, state.messages.MoveHostGroup, state.messages.DeleteHost} {
			widest = max(widest, canvas.TextWidth(label, 13))
		}
		// A context menu is never narrower than Vega's 9rem, whatever its labels.
		if hit.Bounds.Width > max(widest+80, 144) || hit.Bounds.Width < widest+8 {
			t.Errorf("chinese=%v: a row is %.0f wide for a %.0f wide label", chinese, hit.Bounds.Width, widest)
		}
	}
}

// Typing a row's first letters highlights it, and typing the same letter again
// cycles to the next row with that initial, as Base UI menus do.
func TestHostMenuTypeaheadHighlightsRowsByName(t *testing.T) {
	state := newTestState(t, testOptions(t))
	rightClickHost(state, state.hosts[0].ID)
	paintAt(state, gfx.Size{Width: 900, Height: 600}) // list the menu's rows for the keyboard

	key := func(text string) gfx.ID {
		t.Helper()
		result := state.Key(gfx.KeyEvent{Key: gfx.KeyUnknown, Text: text})
		if !result.Handled || result.Focus.IsZero() {
			t.Fatalf("%q: %+v", text, result)
		}
		return result.Focus
	}
	if got := key("c"); got != hitHostMenuConnect {
		t.Fatalf("c focused %v, want Connect", got)
	}
	if got := key("c"); got != hitHostMenuAddress {
		t.Fatalf("second c focused %v, want Copy Address and Port", got)
	}
	if got := key("c"); got != hitHostMenuName {
		t.Fatalf("third c focused %v, want Copy Connection Name", got)
	}
	if got := key("c"); got != hitHostMenuCopy {
		t.Fatalf("fourth c focused %v, want Copy Host", got)
	}
}

// Choosing Move group hands the menu over to the group picker: the picker takes
// the menu's place, at the point the menu was opened at, rather than opening in
// the window's top left corner.
func TestMoveGroupFromAHostsMenuOpensThePickerWhereTheMenuWas(t *testing.T) {
	state := newTestState(t, testOptions(t))
	rightClickHost(state, state.hosts[1].ID)
	paintedTexts(state)
	menu := state.popups.surface
	if menu.X <= 0 || menu.Y <= 0 {
		t.Fatalf("the host menu was not opened at the pointer: %+v", menu)
	}
	state.Click(hitHostMenuMove)
	paintedTexts(state)
	if picker := state.popups.surface; picker.X != menu.X || picker.Y != menu.Y {
		t.Fatalf("the group picker opened at %+v, want the menu's place %+v", picker, menu)
	}
}

// A context menu keeps Tab inside itself, as Base UI's modal context menu does;
// only dropdowns and selects hand Tab back to the page.
func TestTabStaysInsideAContextMenu(t *testing.T) {
	state := newTestState(t, testOptions(t))
	rightClickHost(state, state.hosts[0].ID)
	paintAt(state, gfx.Size{Width: 900, Height: 600})
	state.Key(gfx.KeyEvent{Key: gfx.KeyTab, Target: hitHostMenuConnect})
	if !state.hostMenu.open {
		t.Fatal("Tab left the context menu")
	}
}
