package workbench

import (
	"fmt"

	"github.com/vivaui/viva/gfx"
	"motty/internal/remotetools"
)

var hitRemoteSnapshotStatus = tree.ID(remoteToolsHits, "snapshot-status")

func remoteMutation(id gfx.ID) bool {
	return id == hitRemoteTerminate || id == hitRemoteKill || id == hitRemoteNiceLower || id == hitRemoteNiceNormal || id == hitRemoteNiceHigher ||
		id == hitRemoteStart || id == hitRemoteStop || id == hitRemoteRestart || id == hitRemoteDown || id == hitRemoteRemove || id == hitRemotePull || id == hitRemoteCreate || id == hitRemoteShell || id == hitRemoteLogs
}

// Confirmation captures the complete instance and source, never a row index or
// whichever PID now happens to occupy that row after a background refresh.
func (state *State) remoteProcessReady(process remotetools.Process, sourceID uint32) bool {
	view, m := &state.remote, state.messages.Tools
	if view.stale || view.loading || view.collectedAt.IsZero() {
		view.status, view.failed = m.RemoteRefreshRequired, true
		return false
	}
	current, selected := state.selectedProcess()
	if view.session != sourceID || view.tab != remoteProcesses || !selected || remotetools.ProcessKey(current) != remotetools.ProcessKey(process) {
		view.status, view.failed = m.RemoteProcessChanged, true
		return false
	}
	if process.PID <= 1 || process.StartIdentity == "" {
		view.status, view.failed = m.RemoteProcessUnverifiable, true
		return false
	}
	return true
}

func (state *State) updateRemoteSnapshotStatus() {
	view, m := &state.remote, state.messages.Tools
	view.snapshotStatus = ""
	if view.collectedAt.IsZero() {
		return
	}
	format := m.RemoteSnapshotCollectedFmt
	if view.stale {
		format = m.RemoteSnapshotStaleFmt
	}
	view.snapshotStatus = fmt.Sprintf(format, view.collectedAt.Format("2006-01-02 15:04:05"))
	if view.tab == remoteProcesses && !view.stale && len(view.procs) > 0 {
		for _, process := range view.procs {
			if process.PID > 1 && process.StartIdentity != "" {
				return
			}
		}
		view.snapshotStatus += " · " + m.RemoteProcessUnverifiable
	}
}
