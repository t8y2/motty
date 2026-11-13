package workbench

import (
	"errors"
	"fmt"
	"time"

	"motty/internal/sshconn"
)

// forwardTunnel is the SSH transport a forward uses when no open tab already
// carries one. It has no terminal, pane or saved tab: it exists only while at
// least one forward uses it, and its host-key and password questions are the
// ordinary dialogs, answered under the session ID reserved here.
type forwardTunnel struct {
	id      uint32
	host    Host
	session Session
	// connected is set once the transport is up; the forwards bound to it start
	// at that point.
	connected bool
	// retryAttempt counts automatic retries after a lost connection, and
	// retryDue is when the next dial is made. A zero retryDue means no retry is
	// waiting.
	retryAttempt int
	retryDue     time.Duration
	closed       bool
}

func (state *State) forwardTunnelByID(id uint32) *forwardTunnel {
	for _, tunnel := range state.forwardTunnels {
		if tunnel.id == id {
			return tunnel
		}
	}
	return nil
}

// forwardTunnelForHost returns the tunnel that is connecting, waiting to retry,
// or connected to this host. There is at most one, so forwards on the same host
// never dial twice.
func (state *State) forwardTunnelForHost(host Host) *forwardTunnel {
	for _, tunnel := range state.forwardTunnels {
		if !tunnel.closed && tunnel.host.ID == host.ID {
			return tunnel
		}
	}
	return nil
}

func (state *State) connectedForwardTunnel(host Host) *forwardTunnel {
	if tunnel := state.forwardTunnelForHost(host); tunnel != nil && tunnel.connected {
		return tunnel
	}
	return nil
}

// newForwardTunnel reserves the session ID its prompts are answered under, so
// the auth dialogs can name it among the connections that ask questions.
func (state *State) newForwardTunnel(host Host) *forwardTunnel {
	id := state.nextSessionID
	state.nextSessionID++
	tunnel := &forwardTunnel{id: id, host: host}
	tunnel.session = Session{
		ID: id, HostID: host.ID, Title: host.Label,
		Address: state.sessionAddress(host), host: host, phase: sshconn.PhaseConnecting,
	}
	state.forwardTunnels = append(state.forwardTunnels, tunnel)
	return tunnel
}

func (state *State) dialForwardTunnel(tunnel *forwardTunnel) {
	session := &tunnel.session
	if session.conn != nil {
		session.conn.Cancel()
	}
	session.version, session.phase = 0, sshconn.PhaseConnecting
	session.attempt, session.answered, session.otpSent = 0, 0, false
	session.questions, session.identity = nil, ""
	session.hostKey, session.err = sshconn.HostKeyInfo{}, nil
	session.endpoint = sshconn.Endpoint{Host: tunnel.host.Address, Port: tunnel.host.Port, User: state.login(tunnel.host)}
	tunnel.connected = false
	tunnel.retryDue = 0
	if !tunnel.host.isSSH() || tunnel.host.mosh() {
		state.failForwardTunnel(tunnel, errors.New(state.messages.ForwardHostUnavailable))
		return
	}
	config, valid := state.sshConfigFor(session, tunnel.host)
	if !valid {
		state.failForwardTunnel(tunnel, errors.New(state.status))
		return
	}
	session.conn = state.options.Dial(config)
}

// pumpForwardTunnels reads every tunnel's connection and makes a due retry. It
// runs in the window's own pump, so an idle tunnel costs a status read and
// nothing else.
func (state *State) pumpForwardTunnels() {
	now := state.now()
	for index := 0; index < len(state.forwardTunnels); {
		tunnel := state.forwardTunnels[index]
		if tunnel.retryDue != 0 && now >= tunnel.retryDue {
			state.dialForwardTunnel(tunnel)
			if tunnel.closed {
				// The dial was refused outright: the tunnel removed itself, and
				// the next one now occupies this index.
				continue
			}
		}
		session := &tunnel.session
		if session.conn != nil {
			status := session.conn.Status()
			if status.Version != session.version || status.Phase != session.phase {
				state.adoptForwardTunnelStatus(tunnel, status)
				if tunnel.closed {
					continue
				}
			}
		}
		index++
	}
}

// adoptForwardTunnelStatus takes what a tunnel's connection reports: the same
// fields a tab's session keeps, so the shared prompt dialogs can answer for it.
func (state *State) adoptForwardTunnelStatus(tunnel *forwardTunnel, status sshconn.Status) {
	session := &tunnel.session
	session.version = status.Version
	session.phase = status.Phase
	session.attempt = status.Attempt
	session.hostKey = status.HostKey
	session.questions = status.Questions
	session.identity = status.IdentityPath
	if status.Endpoint.Host != "" {
		session.endpoint = status.Endpoint
		session.role = status.EndpointRole
	}
	if status.ServerVersion != "" {
		session.serverVersion = status.ServerVersion
	}
	if status.Method != "" {
		session.method = status.Method
	}
	session.err = status.Err
	if state.modalSession == session.ID &&
		(state.modal == modalHostKey || state.modal == modalKeyPassphrase || state.modal == modalPassword) &&
		!isPromptPhase(session.phase) {
		state.closeModal()
	}
	switch status.Phase {
	case sshconn.PhaseConnected:
		tunnel.connected = true
		tunnel.retryAttempt = 0
		state.rememberSecret(session)
		state.startForwardTunnelForwards(tunnel)
	case sshconn.PhaseFailed, sshconn.PhaseClosed:
		state.failForwardTunnel(tunnel, status.Err)
	}
}

// startForwardTunnelForwards starts the forwards that waited for this tunnel to
// connect. A forward whose own listener fails is stopped by startForward; the
// tunnel then goes away if nothing else uses it.
func (state *State) startForwardTunnelForwards(tunnel *forwardTunnel) {
	for index := range state.forwards {
		forward := &state.forwards[index]
		if forward.sessionID == tunnel.id && forward.pending {
			state.startForward(forward, &tunnel.session)
		}
	}
	state.releaseForwardTunnel(tunnel.id)
}

// failForwardTunnel answers a lost or refused transport. Forwards that asked to
// resume stay waiting while the automatic reconnect settings allow another
// attempt; every other one stops where it is.
func (state *State) failForwardTunnel(tunnel *forwardTunnel, err error) {
	if tunnel == nil || tunnel.closed {
		return
	}
	retryable := !reconnectTerminalError(err)
	waiting := 0
	for index := range state.forwards {
		forward := &state.forwards[index]
		if forward.sessionID != tunnel.id {
			continue
		}
		if forward.runner != nil {
			forward.runner.CloseAsync()
			forward.runner = nil
			forward.runnerVersion = 0
		}
		forward.starting, forward.startID = false, 0
		if forward.Resume && retryable && state.connectionReconnect != reconnectOff &&
			state.connectionReconnect.permits(tunnel.retryAttempt+1) {
			forward.pending = true
			forward.lastError = ""
			forward.refresh(state.messages, state.forwardHostAvailable(*forward))
			waiting++
			continue
		}
		lastError := state.messages.ForwardConnectionClosed
		if !tunnel.connected && err != nil {
			lastError = err.Error()
		}
		forward.stop()
		forward.lastError = lastError
		forward.refresh(state.messages, state.forwardHostAvailable(*forward))
	}
	if waiting > 0 {
		tunnel.retryAttempt++
		tunnel.retryDue = state.now() + reconnectDelay(tunnel.retryAttempt)
		tunnel.connected = false
		if tunnel.session.conn != nil {
			tunnel.session.conn.Cancel()
			tunnel.session.conn = nil
		}
		return
	}
	connected := tunnel.connected
	state.closeForwardTunnel(tunnel)
	if !connected && err != nil {
		state.status = fmt.Sprintf(state.messages.ForwardStartFailedFmt, err)
	}
}

// abandonForwardTunnel stops the forwards of a tunnel that will not be retried,
// as when the reconnect setting is turned off while it waits.
func (state *State) abandonForwardTunnel(tunnel *forwardTunnel) {
	for index := range state.forwards {
		forward := &state.forwards[index]
		if forward.sessionID != tunnel.id {
			continue
		}
		forward.stop()
		forward.lastError = state.messages.ForwardConnectionClosed
		forward.refresh(state.messages, state.forwardHostAvailable(*forward))
	}
	state.closeForwardTunnel(tunnel)
}

func (state *State) forwardTunnelBound(id uint32) bool {
	for index := range state.forwards {
		if state.forwards[index].sessionID == id {
			return true
		}
	}
	return false
}

// releaseForwardTunnel closes a tunnel no forward uses any more. Forwards that
// borrowed a tab's connection pass a tab ID, which is not a tunnel.
func (state *State) releaseForwardTunnel(id uint32) {
	tunnel := state.forwardTunnelByID(id)
	if tunnel == nil || state.forwardTunnelBound(id) {
		return
	}
	state.closeForwardTunnel(tunnel)
}

func (state *State) closeForwardTunnel(tunnel *forwardTunnel) {
	if tunnel == nil || tunnel.closed {
		return
	}
	tunnel.closed = true
	tunnel.connected = false
	tunnel.retryDue = 0
	if tunnel.session.conn != nil {
		tunnel.session.conn.Cancel()
		tunnel.session.conn = nil
	}
	if state.modalSession == tunnel.session.ID &&
		(state.modal == modalHostKey || state.modal == modalKeyPassphrase || state.modal == modalPassword) {
		state.closeModal()
	}
	for index, other := range state.forwardTunnels {
		if other == tunnel {
			state.forwardTunnels = append(state.forwardTunnels[:index], state.forwardTunnels[index+1:]...)
			break
		}
	}
}

// closeForwardTunnels releases every tunnel of a window that is closing.
func (state *State) closeForwardTunnels() {
	for len(state.forwardTunnels) > 0 {
		state.closeForwardTunnel(state.forwardTunnels[0])
	}
}

// settleForwardTunnelReconnects stops the tunnels that wait for an automatic
// retry the current policy no longer allows.
func (state *State) settleForwardTunnelReconnects() {
	for index := 0; index < len(state.forwardTunnels); {
		tunnel := state.forwardTunnels[index]
		if tunnel.retryDue == 0 {
			index++
			continue
		}
		if state.connectionReconnect == reconnectOff ||
			state.connectionReconnect == reconnectThree && tunnel.retryAttempt >= 3 {
			state.abandonForwardTunnel(tunnel)
			continue
		}
		index++
	}
}
