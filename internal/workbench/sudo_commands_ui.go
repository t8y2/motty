package workbench

import (
	"context"

	"motty/internal/sshconn"
)

func (state *State) toggleSudoCommands(session *Session, returnTools bool) {
	if session.sudoCommands {
		if state.remoteWindow != nil {
			if owner, source, ok := state.remoteWindow.source(); ok {
				owner.setSudoCommands(source, "", false)
				state.remoteWindow.bind(owner, source)
			}
		} else {
			state.setSudoCommands(session, "", false)
		}
		if returnTools {
			state.refreshRemoteTools()
		}
		return
	}
	if _, ok := session.conn.(interface {
		RunSudoCommand(context.Context, string, string, int) ([]byte, error)
	}); !ok {
		state.showActionError(state.messages.SudoCommandsEnable, errSudoCommandUnavailable.Error())
		return
	}
	if session.host.System == sshconn.SystemWindows {
		return
	}
	state.sudoSFTP = sudoSFTPDialog{sessionID: session.ID, commands: true, returnTools: returnTools}
	for _, source := range state.dashboard.sources {
		if &source.session == session {
			state.sudoSFTP.dashboard = true
			break
		}
	}
	state.modal = modalSudoSFTP
}

func (state *State) setSudoCommands(session *Session, password string, enabled bool) {
	if session == nil || session.conn == nil || session.phase != sshconn.PhaseConnected {
		return
	}
	session.sudoCommands, session.sudoPassword = enabled, password
	for _, source := range state.dashboard.sources {
		if &source.session == session {
			state.dashboard.engine.BeginSource(source.session.host.CatalogID)
			if source.cancel != nil {
				source.cancel()
				source.cancel = nil
			}
			source.collecting = 0
			source.errors = nil
			source.blocked = false
			return
		}
	}
	if session.stats == nil {
		return
	}
	if session.stats.worker != nil {
		session.stats.worker.Close()
		session.stats.worker = nil
	}
	if session.stats.panel.worker != nil {
		session.stats.panel.worker.Close()
		session.stats.panel.worker = nil
	}
	session.stats.state = remoteStatsMeasuring
	state.ensureMonitor(session)
}
