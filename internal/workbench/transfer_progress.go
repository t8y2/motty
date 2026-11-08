package workbench

// The desktop draws one icon for the whole program, so the work a transfer
// represents reaches it once, from the windows together, rather than from the
// window that happens to be moving the bytes. What the icon shows is what the
// transfer center already adds up for its own summary bar, plus the transfer
// the terminal can run itself: a program that is receiving files with sz, or
// sending them with rz, is as busy as one copying them through a file panel,
// and the user is looking at the same icon either way.
//
// The bar is the desktop's, so the calls into it are kept rare: it moves in
// hundredths of the whole, which is finer than it can draw, and a transfer
// moving bytes faster than that does not reach the desktop per chunk.

// transferProgress is how much of one window's work is done: the transfers it
// is running, and the bytes they have moved out of the bytes they announced.
type transferProgress struct {
	active      int
	done, total int64
}

// transferProgress reads the window's running transfers. It runs on the
// window's thread, which is the only thread that changes them.
func (state *State) transferProgress() (progress transferProgress) {
	center := &state.transfers
	progress.active, progress.done, progress.total = center.active, center.done, center.total
	// The terminal's own transfer is not one of the center's: it belongs to a
	// session rather than to a file panel, and it is over before a panel could
	// list it. One whose size the other end has not announced yet counts as
	// running but contributes no bytes.
	if transfer := &state.transfer; transfer.phase == transferRunning && transfer.progress.Size > 0 {
		progress.active++
		progress.done += min(transfer.progress.Done, transfer.progress.Size)
		progress.total += transfer.progress.Size
	}
	return progress
}

// reportTransferProgress hands the window's newest progress to the group,
// which is what the desktop hears from: the bar is drawn for the program, and
// this window is only one of the things it is doing.
func (state *State) reportTransferProgress() {
	if group := state.options.Group; group != nil {
		group.reportTransferProgress()
	}
}

// SetTransferProgress names what to tell the desktop about the work the windows
// are doing: how many transfers are running, and how much of the whole they have
// moved, or a negative fraction when there is nothing to draw. Nil leaves the
// icon alone. It is called from the window's thread, like the rest of Options.
func (group *Group) SetTransferProgress(report func(active int, fraction float32)) {
	group.mu.Lock()
	group.reportProgress = report
	group.mu.Unlock()
	group.reportTransferProgress()
}

// reportTransferProgress tells the desktop how far along the windows are, once,
// and only when what it would draw changed. The windows share one event loop,
// so reading their transfers here needs no lock of their own; the members are
// copied because a window may close while the desktop is being told.
func (group *Group) reportTransferProgress() {
	group.mu.Lock()
	report := group.reportProgress
	members := make([]*State, len(group.members))
	copy(members, group.members)
	group.mu.Unlock()
	if report == nil {
		return
	}
	var all transferProgress
	for _, member := range members {
		progress := member.transferProgress()
		all.active += progress.active
		all.done += progress.done
		all.total += progress.total
	}
	// A transfer that has not announced its size leaves the bar where it is
	// rather than showing it at its start: a bar at zero says the work has not
	// begun, and it has.
	step := -1
	if all.active > 0 && all.total > 0 {
		step = int(float64(all.done) / float64(all.total) * 100)
	}
	group.mu.Lock()
	changed := !group.progressReported || group.progressStep != step
	group.progressReported, group.progressStep = true, step
	group.mu.Unlock()
	if !changed {
		return
	}
	if step < 0 {
		report(0, -1)
		return
	}
	report(all.active, float32(all.done)/float32(all.total))
}
