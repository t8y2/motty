package workbench

import (
	"errors"
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
	w.ownsConnection = true
	w.show()
	w.connect()
}

// Keep connection progress and errors in the transfer window. A retry replaces
// only its initial handshake; a live browser/document never changes connection.
func (w *sftpWindow) connect() {
	if w.closed || w.view.worker != nil {
		return
	}
	state, s := w.owner, &w.session
	if s.conn != nil {
		s.conn.Cancel()
		s.conn = nil
	}
	if state.modalSession == s.ID {
		state.closeModal()
	}
	s.phase, s.version, s.answered, s.otpSent = sshconn.PhaseConnecting, 0, 0, false
	s.attempt, s.questions, s.role = 0, nil, sshconn.EndpointTarget
	s.err, s.hostKey, s.identity = nil, sshconn.HostKeyInfo{}, ""
	s.pendingSecretName, s.pendingSecretValue = "", ""
	s.endpoint = sshconn.Endpoint{Host: s.host.Address, Port: s.host.Port, User: state.login(s.host)}
	config, valid := state.sshConfigFor(s, s.host)
	if !valid {
		s.phase, s.err = sshconn.PhaseFailed, errors.New(state.status)
		w.window.Invalidate()
		return
	}
	if state.shareConnections == shareConnectionsOn {
		s.conn, _ = state.shareFrom(s, s.host)
	}
	if s.conn == nil {
		s.conn = state.options.Dial(config)
	}
	if s.relay != nil {
		s.relay.set(func() { w.window.Invalidate(); state.options.Invalidate() })
	}
	state.pumpSFTPConnections()
	w.window.Invalidate()
}

func (state *State) raiseSFTPAuthentication(session *Session) {
	if w := state.sftpWindows[session.ID]; w != nil && w.ownsConnection && !w.closed && state.options.Raise != nil {
		state.options.Raise()
	}
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
			w.startBrowser()
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
