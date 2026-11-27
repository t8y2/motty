package workbench

import (
	"github.com/vivaui/viva/app"
)

// transferSleepReason is what the system's power diagnostics show while a
// transfer holds the machine awake.
const transferSleepReason = "Motty: file transfer"

// transferSleep is how many windows of this process have a transfer moving
// bytes. One system request serves them all, and it exists only while the count
// is above zero, so no timer or goroutine runs when nothing is transferring.
var transferSleep = &asyncSleepClaim{reason: transferSleepReason, prevent: app.PreventSleep}

func holdTransferSleep(held bool) { transferSleep.hold(held) }

// keepAwakeForTransfers follows this window's running transfers: the first adds
// its claim and the last one takes it away, so a paused or finished transfer
// does not keep a laptop awake.
func (state *State) keepAwakeForTransfers(active bool) {
	center := &state.transfers
	if center.awake == active {
		return
	}
	center.awake = active
	holdTransferSleep(active)
}

// desktopSleepReason is what the system's power diagnostics show while a
// remote desktop stays awake.
const desktopSleepReason = "Motty: remote desktop"

// desktopSleep is how many windows of this process show a remote desktop. One
// system request serves them all, and it exists only while the count is above
// zero, so no timer or goroutine runs when none is visible. The system call runs
// off the goroutine that paints, as transferSleep's does: switching to a
// desktop tab must not wait for the power service.
var desktopSleep = &asyncSleepClaim{reason: desktopSleepReason, prevent: app.PreventSleep}

// holdDesktopSleep adds or removes one window's claim, as holdTransferSleep
// does for transfers.
func holdDesktopSleep(held bool) { desktopSleep.hold(held) }

// syncDesktopSleep follows the remote desktops the user can see: one that is
// connected, not ended, and in front of them keeps the machine awake, as a
// transfer moving bytes does. A session behind a tab or in a hidden window
// does not.
func (state *State) syncDesktopSleep() {
	active := false
	for index := range state.sessions {
		session := &state.sessions[index]
		if session.desk != nil && !session.desk.ended && state.desktopVisible(session) {
			active = true
			break
		}
	}
	state.keepAwakeForDesktop(active)
}

// keepAwakeForDesktop follows this window's visible desktops: the first adds
// its claim and the last one takes it away.
func (state *State) keepAwakeForDesktop(active bool) {
	if state.desktopAwake == active {
		return
	}
	state.desktopAwake = active
	holdDesktopSleep(active)
}
