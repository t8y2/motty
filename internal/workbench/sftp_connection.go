package workbench

import (
	"errors"
	"fmt"
	"sync"

	"motty/internal/sftpclient"
	"motty/internal/sshconn"
)

// filesTransport is where a file panel's worker opens its backend. It starts as
// the session's own connection and becomes the panel's separate connection when
// the server refuses to open another channel on the session's transport. The
// worker opens files on its own goroutine, so both sides synchronize here.
type filesTransport struct {
	mu   sync.Mutex
	conn Connection
}

func (transport *filesTransport) connection() Connection {
	transport.mu.Lock()
	defer transport.mu.Unlock()
	return transport.conn
}

func (transport *filesTransport) use(conn Connection) {
	transport.mu.Lock()
	transport.conn = conn
	transport.mu.Unlock()
}

// sftpDedicated is the SSH connection a file panel opens for itself when the
// session's transport refuses another session channel. Embedded SSH servers —
// switches, routers and other appliances — commonly bind a connection to the
// service its first channel opened: once the terminal's shell owns the
// transport, SFTP cannot have a channel there, though the same account may open
// it on a connection of its own.
//
// The session in this struct is not a tab: it exists so the panel's connection
// asks its host-key and password questions through the same prompts, vault and
// host trust as every other connection.
type sftpDedicated struct {
	conn    Connection
	session Session
	// path is the folder to list once the connection is up; empty means the
	// account's home folder, as a listing of "" always does.
	path string
	// ready is set once the connection is authenticated and the panel has
	// switched to it.
	ready bool
}

// startFileConnection dials the connection a file panel uses in place of its
// session's transport. path is what to list once it is up.
func (state *State) startFileConnection(session *Session, view *sftpView, path string) {
	host := session.host
	dedicated := &sftpDedicated{path: path}
	dedicated.session = Session{
		ID:       state.nextSessionID,
		HostID:   session.HostID,
		Title:    session.Title,
		Address:  session.Address,
		host:     host,
		endpoint: sshconn.Endpoint{Host: host.Address, Port: host.Port, User: state.login(host)},
		role:     sshconn.EndpointTarget,
		phase:    sshconn.PhaseConnecting,
	}
	state.nextSessionID++
	// Progress wakes the window the terminal tab is in, and follows the tab if
	// it moves: the relay is the terminal session's, not the hidden session's.
	state.notifyFor(session)
	dedicated.session.relay = session.relay
	config, valid := state.sshConfigFor(&dedicated.session, host)
	// No shell rides on this connection, and the server may allow only one
	// session in all: a system probe must not take the file session's place.
	config.PrimaryShell = false
	config.NoProbes = true
	if !valid {
		view.busy, view.operation = false, 0
		view.errorText = fmt.Sprintf(state.messages.SFTPDedicatedFailedFmt, state.messages.SFTPUnavailable)
		return
	}
	dedicated.conn = state.options.Dial(config)
	dedicated.session.conn = dedicated.conn
	view.dedicated = dedicated
	view.busy, view.operation = true, sftpclient.OperationList
	view.errorText = ""
	view.status = state.messages.SFTPDedicatedConnecting
	state.options.Invalidate()
}

// separateFileConnection takes over a file worker error that says the session's
// transport refused a session channel. It starts the panel's own connection and
// reports that the error was handled; the listing is asked for again once that
// connection is up.
func (state *State) separateFileConnection(session *Session, view *sftpView, result sftpclient.Result) bool {
	if view.dedicated != nil || view.transport == nil || !sshconn.IsChannelRefused(result.Err) {
		return false
	}
	path := view.path
	if result.Operation == sftpclient.OperationList {
		path = result.Path
	}
	// Every later panel request goes to the separate connection, including one
	// made after the panel is closed and opened again on this session.
	session.fileConnectionSeparate = true
	if result.Operation == sftpclient.OperationList {
		view.navigation = sftpNavigation{}
	}
	view.refreshStatus = ""
	state.startFileConnection(session, view, path)
	return true
}

// pumpFileConnection moves a panel's own connection along: it publishes what
// the connection asks and switches the panel to it once it is authenticated.
func (state *State) pumpFileConnection(session *Session, view *sftpView) {
	dedicated := view.dedicated
	if dedicated == nil || dedicated.ready || dedicated.conn == nil {
		return
	}
	status := dedicated.conn.Status()
	hidden := &dedicated.session
	if hidden.version == status.Version && hidden.phase == status.Phase {
		return
	}
	hidden.version, hidden.phase, hidden.attempt = status.Version, status.Phase, status.Attempt
	hidden.hostKey, hidden.questions, hidden.identity = status.HostKey, status.Questions, status.IdentityPath
	hidden.role, hidden.err = status.EndpointRole, status.Err
	if status.Endpoint.Host != "" {
		hidden.endpoint = status.Endpoint
	}
	switch hidden.phase {
	case sshconn.PhaseConnected:
		dedicated.ready = true
		view.busy, view.operation = false, 0
		view.status = ""
		view.errorText = ""
		view.transport.use(dedicated.conn)
		state.rememberSecret(hidden)
		state.listSFTP(view, dedicated.path)
	case sshconn.PhaseFailed, sshconn.PhaseClosed:
		view.dedicated = nil
		view.busy, view.operation = false, 0
		view.status = ""
		if state.modalSession == hidden.ID {
			state.closeModal()
		}
		if errors.Is(hidden.err, sshconn.ErrCanceled) {
			view.errorText = state.messages.SFTPDedicatedCanceled
		} else if hidden.err != nil {
			view.errorText = fmt.Sprintf(state.messages.SFTPDedicatedFailedFmt, hidden.err)
		} else {
			view.errorText = fmt.Sprintf(state.messages.SFTPDedicatedFailedFmt, state.messages.SFTPUnavailable)
		}
	}
	state.options.Invalidate()
}

// dedicatedFileSession returns the hidden prompt session of a panel connection.
func (state *State) dedicatedFileSession(id uint32) *Session {
	for index := range state.sessions {
		files := state.sessions[index].files
		if files != nil && files.dedicated != nil && !files.dedicated.ready && files.dedicated.session.ID == id {
			return &files.dedicated.session
		}
	}
	return nil
}

// fileConnection is the SSH connection the panel's own commands run on: the
// session's transport, or the panel's separate connection once it is up.
func (view *sftpView) fileConnection(session *Session) Connection {
	if view.dedicated != nil && view.dedicated.ready {
		return view.dedicated.conn
	}
	return session.conn
}

// stopFileConnection closes the panel's own connection, if it has one, and
// drops any prompt that belongs to it.
func (state *State) stopFileConnection(view *sftpView) {
	dedicated := view.dedicated
	if dedicated == nil {
		return
	}
	view.dedicated = nil
	if state.modalSession == dedicated.session.ID {
		state.closeModal()
	}
	if dedicated.conn != nil && dedicated.conn.Status().Phase != sshconn.PhaseClosed {
		dedicated.conn.Cancel()
	}
}
