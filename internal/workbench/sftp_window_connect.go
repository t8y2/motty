package workbench

import (
	"fmt"
	"github.com/vivaui/viva/components"
	"motty/internal/sshconn"
)

// A host-menu connection has no terminal, pane or persisted session. SSH's
// existing authentication dialogs and vault still own host trust and secrets.
func (state *State) openHostSFTPWindow(host Host) {
	if !host.isSSH() || host.ID == localHostID || state.options.OpenContentWindow == nil {
		return
	}
	for _, w := range state.sftpWindows {
		if !w.closed && w.session.HostID == host.ID {
			if w.window != nil {
				w.window.Show()
			}
			return
		}
	}
	source := Session{ID: state.nextSessionID, HostID: host.ID, Title: host.Label, Address: state.sessionAddress(host), host: host, phase: sshconn.PhaseConnecting}
	state.nextSessionID++
	w := state.newSFTPWindow(&source)
	w.session.endpoint = sshconn.Endpoint{Host: host.Address, Port: host.Port, User: state.login(host)}
	config, valid := state.sshConfigFor(&w.session, host)
	if !valid {
		state.fileNotices.show(state.messages.SFTPWindow.Title, state.status, components.ToastError)
		w.onClosed()
		return
	}
	w.ownsConnection = true
	if state.shareConnections == shareConnectionsOn {
		w.session.conn, _ = state.shareFrom(&w.session, host)
	}
	if w.session.conn == nil {
		w.session.conn = state.options.Dial(config)
	}
	state.fileNotices.show(state.messages.SFTPWindow.Title, fmt.Sprintf(state.messages.SFTPWindow.ConnectingFmt, host.Label), components.ToastInfo)
	state.pumpSFTPConnections()
}

func (state *State) pumpSFTPConnections() {
	for _, w := range state.sftpWindows {
		if w.closed || !w.ownsConnection || w.session.conn == nil {
			continue
		}
		status := w.session.conn.Status()
		s := &w.session
		if s.version == status.Version && s.phase == status.Phase {
			continue
		}
		s.version, s.phase, s.attempt = status.Version, status.Phase, status.Attempt
		s.hostKey, s.questions, s.identity = status.HostKey, status.Questions, status.IdentityPath
		s.role, s.err = status.EndpointRole, status.Err
		if status.Endpoint.Host != "" {
			s.endpoint = status.Endpoint
		}
		if status.System != sshconn.SystemUnknown {
			s.host.System = status.System
		}
		if state.modalSession == s.ID && !isPromptPhase(s.phase) {
			state.closeModal()
		}
		if s.phase == sshconn.PhaseConnected {
			state.rememberSecret(s)
			if w.window == nil {
				w.show()
			}
		} else if w.window == nil && (s.phase == sshconn.PhaseFailed || s.phase == sshconn.PhaseClosed) {
			if status.Err != nil {
				state.fileNotices.show(state.messages.SFTPWindow.Title, fmt.Sprintf(state.messages.SFTPErrorFmt, status.Err), components.ToastError)
			}
			w.onClosed()
		}
		w.view.invalidateDetached()
	}
}

// Keep a standalone connection alive for remote documents opened from it, even
// after its file browser closes. Local documents do not hold the connection.
func (w *sftpWindow) releaseConnection() {
	if !w.closed {
		return
	}
	if w.ownsConnection {
		for _, d := range w.owner.fileWindows.documents {
			if d.origin == w && !d.local && !d.closed {
				return
			}
		}
		w.ownsConnection = false
		if w.session.conn != nil && w.session.conn.Status().Phase != sshconn.PhaseClosed {
			w.session.conn.Cancel()
		}
	}
	if w.owner.sftpWindows[w.session.ID] == w {
		delete(w.owner.sftpWindows, w.session.ID)
	}
}

func (state *State) releaseClosedSFTPConnections() {
	for _, w := range state.sftpWindows {
		if w.closed {
			w.releaseConnection()
		}
	}
}

// Include standalone SSH authentication without adding invisible terminal tabs.
func (state *State) connectionSessions() func(func(*Session) bool) {
	return func(yield func(*Session) bool) {
		for i := range state.sessions {
			if !yield(&state.sessions[i]) {
				return
			}
		}
		for _, w := range state.sftpWindows {
			if w.ownsConnection && !w.closed {
				if !yield(&w.session) {
					return
				}
			}
		}
	}
}
