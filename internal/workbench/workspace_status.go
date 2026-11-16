package workbench

import (
	"motty/internal/sshconn"
	layoutcore "motty/internal/workspace"
)

type workspacePaneState uint8

const (
	workspacePaneSettled workspacePaneState = iota
	workspacePanePending
	workspacePaneConnected
	workspacePaneFailed
)

// workspaceConnectionState is derived from the Sessions currently bound to one
// workspace. Pending includes dialing, host-key and password prompts, and the
// wait for a usable shell.
type workspaceConnectionState struct {
	Panes     int
	Pending   int
	Connected int
	Failed    int
	Closed    int
	Empty     int
}

func (state *State) workspaceConnectionState(workspaceID layoutcore.WorkspaceID) workspaceConnectionState {
	var result workspaceConnectionState
	panes := state.deck.Panes(workspaceID, state.workspaceStatusPanes[:0])
	for _, pane := range panes {
		result.Panes++
		if pane.SessionID == 0 {
			result.Empty++
			continue
		}
		session, ok := state.sessionByIDPointer(uint32(pane.SessionID))
		if !ok {
			result.Failed++
			continue
		}
		switch workspaceSessionState(session) {
		case workspacePanePending:
			result.Pending++
		case workspacePaneConnected:
			result.Connected++
		case workspacePaneFailed:
			result.Failed++
		case workspacePaneSettled:
			result.Closed++
		}
	}
	return result
}

func workspaceSessionState(session *Session) workspacePaneState {
	if session == nil {
		return workspacePaneFailed
	}
	switch session.phase {
	case sshconn.PhaseConnecting, sshconn.PhaseHostKeyPrompt, sshconn.PhaseAuthenticating,
		sshconn.PhaseKeyPassphrasePrompt, sshconn.PhasePasswordPrompt:
		return workspacePanePending
	case sshconn.PhaseConnected:
		// Local transports stay connected after a shell exits or fails to start.
		// Count that outcome before looking for a shell that will never arrive.
		if session.term != nil && session.term.ended {
			if session.shellFailed || !session.term.live {
				return workspacePaneFailed
			}
			return workspacePaneSettled
		}
		if session.shellPending() {
			return workspacePanePending
		}
		if session.term != nil && session.term.shell != nil {
			return workspacePaneConnected
		}
		return workspacePanePending
	case sshconn.PhaseFailed:
		return workspacePaneFailed
	case sshconn.PhaseClosed:
		if session.reconnectPending {
			return workspacePanePending
		}
		if session.err != nil || session.term != nil && session.term.ended && !session.term.live {
			return workspacePaneFailed
		}
		return workspacePaneSettled
	default:
		if session.term != nil && session.term.live && session.term.shell != nil {
			return workspacePaneConnected
		}
		if session.term != nil && session.term.ended {
			return workspacePaneFailed
		}
		return workspacePanePending
	}
}

func workspaceCanBroadcast(status workspaceConnectionState) bool {
	return status.Pending == 0 && status.Connected >= 2
}
