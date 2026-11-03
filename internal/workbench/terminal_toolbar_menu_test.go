package workbench

import (
	"testing"

	"github.com/vivaui/viva/ui"
)

func TestTerminalToolbarMenuOpensOnlyFromRailBackground(t *testing.T) {
	state, _, _, _ := liveSession(t)
	setAllTerminalToolbarSides(state, terminalToolbarRight)
	size := ui.Size{Width: 1100, Height: 760}
	state.Resize(size)

	var canvas ui.Canvas
	state.Paint(&canvas, size)
	control, ok := hitByID(&canvas, hitSFTPOpen)
	if !ok {
		t.Fatal("files control is missing")
	}
	if result := state.Pointer(ui.PointerEvent{
		Kind: ui.PointerDown, Button: ui.PointerButtonSecondary,
		Target:   control.ID,
		X:        control.Bounds.X + control.Bounds.Width/2,
		Y:        control.Bounds.Y + control.Bounds.Height/2,
		Viewport: size,
	}); result.Handled || state.terminalToolbarMenu.open {
		t.Fatalf("right click on a control opened the background menu: result=%+v open=%v", result, state.terminalToolbarMenu.open)
	}

	rail, ok := state.terminalSideRailBoundsFor(size, terminalToolbarRight)
	if !ok {
		t.Fatal("right rail is missing")
	}
	at := ui.Point{X: rail.X + rail.Width/2, Y: rail.Y + rail.Height - 10}
	if !terminalRailBlankAt(&state.sessions[0], at.X, at.Y) {
		t.Fatal("the chosen point is not rail background")
	}
	if result := state.Pointer(ui.PointerEvent{
		Kind: ui.PointerDown, Button: ui.PointerButtonSecondary,
		X: at.X, Y: at.Y, Viewport: size,
	}); !result.Handled || !result.Repaint || !state.terminalToolbarMenu.open {
		t.Fatalf("right click on rail background: result=%+v open=%v", result, state.terminalToolbarMenu.open)
	}

	canvas = ui.Canvas{}
	state.Paint(&canvas, size)
	small, ok := hitByID(&canvas, hitTerminalToolbarMenuSmall)
	if !ok || !small.Selected {
		t.Fatalf("small size row = %+v, %v; want selected", small, ok)
	}
	for _, id := range []ui.ID{
		hitTerminalToolbarMenuMedium,
		hitTerminalToolbarMenuLarge,
		hitTerminalToolbarMenuReset,
	} {
		if _, ok := hitByID(&canvas, id); !ok {
			t.Errorf("menu item %d is missing", id)
		}
	}
}

func TestTerminalToolbarMenuChangesSizeAndRestoresDefaults(t *testing.T) {
	state, _, _, _ := liveSession(t)
	size := ui.Size{Width: 1100, Height: 760}
	state.Resize(size)

	state.terminalToolbarMenu.open = true
	state.Click(hitTerminalToolbarMenuMedium)
	if state.terminalToolbarSize != terminalToolbarMedium || state.terminalToolbarMenu.open {
		t.Fatalf("size=%v open=%v after choosing medium", state.terminalToolbarSize, state.terminalToolbarMenu.open)
	}

	state.terminalToolbarSize = terminalToolbarLarge
	state.toolbars.layouts[toolbarTerminal] = terminalToolbarDefaultsOn(terminalToolbarLeft)
	for index := range state.toolbars.layouts[toolbarTerminal] {
		state.toolbars.layouts[toolbarTerminal][index].Zone = terminalToolbarZoneLeftBottom.String()
	}
	for index := range state.toolbars.layouts[toolbarTerminal] {
		if state.toolbars.layouts[toolbarTerminal][index].Key == "latency" {
			state.toolbars.layouts[toolbarTerminal][index].Enabled = true
			break
		}
	}
	state.syncTerminalStatusEntries()
	state.terminalToolbarMenu.open = true
	state.Click(hitTerminalToolbarMenuReset)

	if state.terminalToolbarSize != terminalToolbarSmall {
		t.Fatalf("size=%v after restore, want small", state.terminalToolbarSize)
	}
	if state.toolbars.layouts[toolbarTerminal] != nil {
		t.Fatalf("terminal toolbar override survived restore: %+v", state.toolbars.layouts[toolbarTerminal])
	}
	for _, entry := range state.toolbars.entries(toolbarTerminal) {
		if zone := terminalToolbarZoneForEntry(entry, terminalToolbarRight); zone != terminalToolbarZoneRightTop {
			t.Fatalf("restore left entry %q in zone %v, want right-top", entry.Key, zone)
		}
	}
	if state.terminalShowLatency || state.terminalHideConnectionColor {
		t.Fatalf("status defaults latency=%v hide-color=%v, want both false", state.terminalShowLatency, state.terminalHideConnectionColor)
	}
	if state.terminalToolbarMenu.open {
		t.Fatal("restore left the menu open")
	}
}

func TestTerminalToolbarMenuClosesOnEscape(t *testing.T) {
	state, _, _, _ := liveSession(t)
	state.terminalToolbarMenu.open = true
	if result := state.Key(ui.KeyEvent{Key: ui.KeyEscape}); !result.Handled || state.terminalToolbarMenu.open {
		t.Fatalf("Escape result=%+v open=%v", result, state.terminalToolbarMenu.open)
	}
}
