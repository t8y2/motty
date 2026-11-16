package workbench

import (
	"time"

	"github.com/vivaui/viva/app"
)

// The terminal bell (BEL, 0x07) a program rings, for instance printf '\a' or
// tput bel, so a long command on a server can call the user back. It sounds on
// this machine through the system's alert sound. Each terminal is held to a few
// a second: cat on a binary file is a stream of them.

// bellHits is the place of the controls this file paints.
var bellHits = tree.Feature("bell")

var (
	hitSettingsBellToggle = tree.ID(bellHits, "settings-bell")
)

// bellInterval is the least time between two bells of one terminal.
const bellInterval = 200 * time.Millisecond

func (state *State) terminalBell(sessionID uint32) {
	if state.terminalBellOff {
		return
	}
	session, ok := state.sessionByIDPointer(sessionID)
	if !ok {
		return
	}
	now := state.now()
	if session.didBell && now-session.belled < bellInterval {
		return
	}
	session.belled, session.didBell = now, true
	// A system without an alert sound has nothing to tell the user about.
	_ = state.options.Bell()
}

func playBell() error { return app.PlaySystemSound(app.SoundDefault) }
