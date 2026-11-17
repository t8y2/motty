package workbench

import (
	"context"
	"errors"
	"testing"

	"github.com/vivaui/viva/gfx"
	"motty/internal/desktop"
)

// shortcutLog records what the window was asked about the system's shortcuts.
type shortcutLog struct{ asked []bool }

func (log *shortcutLog) capture(want bool) error {
	log.asked = append(log.asked, want)
	return nil
}

func (log *shortcutLog) held() bool { return len(log.asked) > 0 && log.asked[len(log.asked)-1] }

// TestTheSystemShortcutsFollowWhoHoldsTheKeyboard follows a remote desktop through the states the
// user can put it in: the window takes the shortcuts exactly while the desktop is what the user
// types into, and gives them back at the escape chord, over a dialog, in another section, when
// the window is hidden, and when the connection ends.
func TestTheSystemShortcutsFollowWhoHoldsTheKeyboard(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	state.options.Invalidate = func() {}
	state.connectionReconnect = reconnectOff
	state.dialDesktop = func(context.Context, desktopRequest) (desktop.Session, error) {
		return newFollowingDesktop(1600, 900, false), nil
	}
	var log shortcutLog
	state.SetShortcutCapture(log.capture)
	shown := true
	state.SetWindow(func() (gfx.Rect, bool) { return gfx.Rect{}, true }, func() bool { return shown })

	screen := newFrames(state, 1)
	screen.paint(gfx.Size{Width: 1400, Height: 900}, 0)
	eventCanvases[state] = &screen.canvas
	addStreamHost(state, Host{Label: "Win", Address: "win.example.com", User: "alice", Protocol: protocolRDP})
	openHost(state)
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	waitUntil(t, state, "the picture", func() bool { return session.desk != nil && session.desk.width > 0 })
	state.Wake()
	if !log.held() {
		t.Fatalf("a desktop in front did not take the shortcuts: asked %v", log.asked)
	}

	state.toggleCapture(session.desk) // the escape chord hands the keyboard back
	state.Wake()
	if log.held() {
		t.Fatal("the shortcuts stayed with the desktop after the keyboard was handed back")
	}
	state.toggleCapture(session.desk)
	state.Wake()
	if !log.held() {
		t.Fatal("the shortcuts did not return with the keyboard")
	}

	shown = false // the window went to the tray
	state.Wake()
	if log.held() {
		t.Fatal("a hidden window kept the shortcuts")
	}
	shown = true
	state.Wake()
	if !log.held() {
		t.Fatal("the shortcuts did not return with the window")
	}

	state.modal = modalPassword // a Motty dialog is over the desktop
	state.Wake()
	if log.held() {
		t.Fatal("the shortcuts reached past a dialog of Motty's")
	}
	state.modal = modalNone

	state.activeSection = sectionSettings
	state.Wake()
	if log.held() {
		t.Fatal("the shortcuts stayed with the desktop while the user was in the settings")
	}
	state.activeSection = sectionTerminal
	state.Wake()
	if !log.held() {
		t.Fatal("the shortcuts did not return when the desktop did")
	}

	state.endDesktop(session)
	state.Wake()
	if log.held() {
		t.Fatal("an ended connection kept the shortcuts")
	}

	// A call that changes nothing is not repeated at the platform.
	before := len(log.asked)
	state.Wake()
	state.Wake()
	if len(log.asked) != before {
		t.Fatalf("an unchanged state asked the window again: %v", log.asked[before:])
	}
}

// A window that cannot take the system's shortcuts is not asked again.
func TestAWindowThatCannotTakeTheShortcutsIsLeftAlone(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	state.options.Invalidate = func() {}
	state.connectionReconnect = reconnectOff
	state.dialDesktop = func(context.Context, desktopRequest) (desktop.Session, error) {
		return newFollowingDesktop(1600, 900, false), nil
	}
	asked := 0
	state.SetShortcutCapture(func(bool) error { asked++; return errors.New("unsupported") })
	screen := newFrames(state, 1)
	screen.paint(gfx.Size{Width: 1400, Height: 900}, 0)
	eventCanvases[state] = &screen.canvas
	addStreamHost(state, Host{Label: "Win", Address: "win.example.com", User: "alice", Protocol: protocolRDP})
	openHost(state)
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	waitUntil(t, state, "the picture", func() bool { return session.desk != nil && session.desk.width > 0 })
	for range 5 {
		state.Wake()
	}
	if asked != 1 {
		t.Fatalf("a window that refused was asked %d times", asked)
	}
}
