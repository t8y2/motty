package workbench

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
)

// A host can ask for its desktop page to be the picture alone: no bar above it,
// no status line under it. Their room goes to the picture, the keyboard note
// moves to a corner of the picture, the controls move to the tab's menu, and
// the choice travels with the host.

func TestChromelessLayoutGivesTheBarsRoomToThePicture(t *testing.T) {
	size := gfx.Size{Width: 1400, Height: 900}
	bars := desktopLayout(size, false)
	if bars.area.Y != windowBarHeight+infoBarHeight || bars.area.Height != size.Height-windowBarHeight-infoBarHeight-desktopStripHeight {
		t.Fatalf("a page with bars places its picture at %+v", bars.area)
	}
	if bars.strip.Height != desktopStripHeight {
		t.Fatalf("a page with bars keeps a status line of %v", bars.strip)
	}
	alone := desktopLayout(size, true)
	if alone.area.Y != windowBarHeight || alone.area.Height != size.Height-windowBarHeight {
		t.Fatalf("a chromeless page places its picture at %+v, want the whole room under the window bar", alone.area)
	}
	if alone.strip.Height != 0 {
		t.Fatalf("a chromeless page still keeps a status line of %v", alone.strip)
	}
}

func TestChromelessChoiceTravelsWithTheHost(t *testing.T) {
	for _, protocol := range []hostProtocol{protocolVNC, protocolRDP} {
		saved, err := json.Marshal(Host{Protocol: protocol, Chromeless: true}.transport())
		if err != nil {
			t.Fatalf("%v: %v", protocol, err)
		}
		if !strings.Contains(string(saved), `"chromeless":true`) {
			t.Fatalf("%v saves %s, want it to hold the choice", protocol, saved)
		}
		var read savedTransport
		if err := json.Unmarshal(saved, &read); err != nil {
			t.Fatalf("%v: %v", protocol, err)
		}
		host := Host{}
		host.applyTransport(read)
		if !host.Chromeless {
			t.Fatalf("%v round trips as %+v without the choice", protocol, host)
		}
		// A host that never asked keeps the bars, and says nothing about them.
		plain, err := json.Marshal(Host{Protocol: protocol}.transport())
		if err != nil {
			t.Fatalf("%v: %v", protocol, err)
		}
		if strings.Contains(string(plain), "chromeless") {
			t.Fatalf("%v without the choice saves %s", protocol, plain)
		}
	}
}

func TestChromelessDesktopShowsThePictureAlone(t *testing.T) {
	fixture := startVNCFixture(t, "secret")
	state, session := vncState(t, fixture)
	connectDesktop(t, state, session, "secret")
	session.desk.chromeless = true
	size := gfx.Size{Width: 1400, Height: 900}
	canvas := paintDesk(state)

	area, ok := hitByID(canvas, hitTerminalList.At(int(session.ID)))
	if !ok {
		t.Fatal("the picture has no hit")
	}
	if area.Bounds.Y != windowBarHeight || area.Bounds.Height != size.Height-windowBarHeight {
		t.Fatalf("the picture is shown at %+v, want the whole room under the window bar", area.Bounds)
	}
	if _, ok := hitByID(canvas, hitDesktopFit); ok {
		t.Fatal("a chromeless page still paints the bar's scaling buttons")
	}
	// The picture's edges are the remote's own taskbar and panels: nothing of
	// Motty's lies over them to hide them or take their clicks (#1800).
	state.terminalShowLatency = true
	canvas = paintDesk(state)
	for _, id := range []gfx.ID{hitDesktopCapture, hitDesktopLatency} {
		if hit, ok := hitByID(canvas, id); ok {
			t.Fatalf("a chromeless page lays %v at %+v over the picture", id, hit.Bounds)
		}
	}
	// The keyboard switch the bar would hold is in the tab's menu, with its
	// chord, and takes the keyboard back as the chord does.
	workspace := state.deck.ActiveWorkspace()
	rightClickTab(state, hitTabList.At(int(workspace)))
	canvas = paintDesk(state)
	capture, ok := menuHit(canvas, state, hitTabMenuCapture)
	if !ok || capture.Disabled || !capture.Checked {
		t.Fatalf("the tab menu offers the captured keyboard as %+v", capture)
	}
	chooseMenu(t, state, hitTabMenuCapture)
	if !session.desk.released {
		t.Fatal("the menu's keyboard switch does not release the keyboard")
	}
	rightClickTab(state, hitTabList.At(int(workspace)))
	canvas = paintDesk(state)
	if capture, _ := menuHit(canvas, state, hitTabMenuCapture); capture.Checked {
		t.Fatal("a released keyboard is still checked in the tab menu")
	}
	chooseMenu(t, state, hitTabMenuCapture)
	if session.desk.released || !session.desk.focusPending {
		t.Fatalf("the menu does not hand the keyboard back to the picture (released %v, focus %v)", session.desk.released, session.desk.focusPending)
	}

	// The tab's menu holds the controls the bar would: the scaling choice and
	// the bars themselves, and flipping them there moves the picture at once.
	session.desk.chromeless = false
	canvas = paintDesk(state)
	if _, ok := hitByID(canvas, hitDesktopFit); !ok {
		t.Fatal("a page with bars paints no scaling buttons")
	}
	rightClickTab(state, hitTabList.At(int(workspace)))
	canvas = paintDesk(state)
	hide, ok := menuHit(canvas, state, hitTabMenuHideBars)
	if !ok || hide.Disabled {
		t.Fatal("the tab menu of a desktop does not offer to hide its bars")
	}
	chooseMenu(t, state, hitTabMenuHideBars)
	if !session.desk.chromeless || !session.host.Chromeless {
		t.Fatalf("hiding the bars moved the view (%v) and the host (%v)", session.desk.chromeless, session.host.Chromeless)
	}
	for index := range state.hosts {
		if state.hosts[index].ID == session.HostID && !state.hosts[index].Chromeless {
			t.Fatal("the host in the catalog was not brought into step")
		}
	}
	canvas = paintDesk(state)
	if hit, _ := hitByID(canvas, hitTerminalList.At(int(session.ID))); hit.Bounds.Y != windowBarHeight {
		t.Fatalf("the picture still starts at %+v after the bars were hidden", hit.Bounds)
	}
}

func TestTheTabMenuHoldsTheDesktopControlsOnlyForADesktop(t *testing.T) {
	state := newTestState(t, testOptions(t))
	openHost(state)
	workspace := state.deck.ActiveWorkspace()
	rightClickTab(state, hitTabList.At(int(workspace)))
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	if _, ok := menuHit(&canvas, state, hitTabMenuFit); ok {
		t.Fatal("a terminal tab offers the desktop's scaling choice")
	}
}

func TestTheHostEditorOffersThePictureAloneSwitch(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.openHostDialog()
	clickHostChoice(t, state, state.messages.Desktop.ProtocolRDP)
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	if _, ok := hitByID(&canvas, hitDialogChromeless); !ok {
		t.Fatal("a desktop host's editor offers no picture-alone switch")
	}
	clickHostChoice(t, state, state.messages.Tools.ProtocolSSH)
	canvas = gfx.Canvas{}
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	if _, ok := hitByID(&canvas, hitDialogChromeless); ok {
		t.Fatal("an SSH host's editor offers the desktop's switch")
	}
	clickHostChoice(t, state, state.messages.Desktop.ProtocolVNC)
	state.clickHostProtocol(hitDialogChromeless) // the switch is one of the protocol clicks
	if host := state.editor.host(0); !host.Chromeless {
		t.Fatalf("the checked switch builds %+v", host)
	}
}
