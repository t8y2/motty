package workbench

import (
	"time"

	"github.com/vivaui/viva/app"
	"motty/internal/term"
)

// Desktop notifications a program in a terminal asks for (OSC 9, 777 and 99).
// They are shown by the system, so a remote program can reach outside the window:
// the user can turn them off, each terminal is held to a few a second at most,
// and what is shown has been stripped of control characters by the emulator.

// notifyHits is the place of the controls this file paints.
var notifyHits = tree.Feature("notify")

var (
	hitSettingsNotifyToggle = tree.ID(notifyHits, "settings-notify")
)

// notifyInterval is the least time between two notifications of one terminal.
const notifyInterval = time.Second

func (state *State) setNotifications(on bool) bool {
	if state.notifyOff == !on {
		return false
	}
	state.notifyOff = !on
	return true
}

// terminalNotification is what a terminal's program asked to show.
func (state *State) terminalNotification(sessionID uint32, note term.Notification) {
	if state.notifyOff {
		return
	}
	session, ok := state.sessionByIDPointer(sessionID)
	if !ok {
		return
	}
	now := state.now()
	if session.didNotify && now-session.notified < notifyInterval {
		return // a program that notifies in a loop is shown the first time only
	}
	session.notified, session.didNotify = now, true
	title := session.Title
	if note.Title != "" {
		title += " · " + note.Title
	}
	if err := state.options.Notify(app.Notification{Title: title, Body: note.Body}); err != nil {
		state.status = err.Error()
	}
}

func notifyString(off bool) string {
	if off {
		return "off"
	}
	return ""
}
