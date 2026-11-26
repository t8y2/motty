package workbench

import (
	"errors"
	"fmt"
	"time"

	"github.com/vivaui/viva/gfx"
	"motty/internal/sshconn"
)

type reconnectPolicy uint8

const (
	reconnectOff reconnectPolicy = iota
	reconnectThree
	reconnectForever
)

func (policy reconnectPolicy) String() string {
	switch policy {
	case reconnectThree:
		return "three"
	case reconnectForever:
		return "forever"
	default:
		return "off"
	}
}

func reconnectPolicyFromString(value string) reconnectPolicy {
	switch value {
	case reconnectThree.String():
		return reconnectThree
	case reconnectForever.String():
		return reconnectForever
	default:
		return reconnectOff
	}
}

func (policy reconnectPolicy) permits(attempt int) bool {
	return policy == reconnectForever || policy == reconnectThree && attempt <= 3
}

func reconnectDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	delay := time.Second
	for step := 1; step < attempt && delay < 30*time.Second; step++ {
		delay *= 2
	}
	return min(delay, 30*time.Second)
}

func reconnectTerminalError(err error) bool {
	return errors.Is(err, sshconn.ErrCanceled) ||
		errors.Is(err, sshconn.ErrAuthFailed) ||
		errors.Is(err, sshconn.ErrHostKeyChanged) ||
		errors.Is(err, sshconn.ErrHostKeyRejected)
}

func (state *State) cancelReconnect(session *Session) {
	if session.reconnectStop != nil {
		session.reconnectStop()
		session.reconnectStop = nil
	}
	if session.reconnectPending {
		state.reconnectWaiters--
	}
	session.reconnectAttempt = 0
	session.reconnectDue = 0
	session.reconnectPending = false
	session.reconnectExhausted = false
	session.reconnectText = ""
	session.reconnectSecond = 0
	state.clearRevivingForwardsForSession(session.ID)
}

func (state *State) queueReconnect(session *Session) bool {
	if session.reconnectPending {
		return true
	}
	next := session.reconnectAttempt + 1
	if !state.connectionReconnect.permits(next) {
		if session.reconnectStop != nil {
			session.reconnectStop()
			session.reconnectStop = nil
		}
		session.reconnectPending = false
		session.reconnectDue = 0
		session.reconnectExhausted = session.reconnectAttempt > 0
		session.reconnectText = ""
		state.recordConnectionEvent(session, connectionEventReconnectStopped, "")
		return false
	}
	delay := reconnectDelay(next)
	session.reconnectAttempt = next
	session.reconnectDue = state.now() + delay
	session.reconnectPending = true
	state.reconnectWaiters++
	session.reconnectExhausted = false
	session.reconnectText = ""
	session.showLog = false
	state.recordConnectionEvent(session, connectionEventReconnectWaiting, "")
	if session.reconnectStop != nil {
		session.reconnectStop()
	}
	session.reconnectStop = state.scheduleWake(delay)
	return true
}

func (state *State) updateReconnect(session *Session, status sshconn.Status) {
	if session.containerCommand != "" && (status.Phase == sshconn.PhaseClosed || status.Phase == sshconn.PhaseFailed) {
		session.established = false
		state.cancelReconnect(session)
		return
	}
	switch status.Phase {
	case sshconn.PhaseConnected:
		session.established = true
		state.cancelReconnect(session)
	case sshconn.PhaseClosed:
		wasEstablished := session.established
		session.established = false
		if reconnectTerminalError(status.Err) {
			state.cancelReconnect(session)
			return
		}
		if state.connectionReconnect != reconnectOff && errors.Is(status.Err, sshconn.ErrDisconnected) && (wasEstablished || session.reconnectAttempt > 0) {
			state.queueReconnect(session)
		}
	case sshconn.PhaseFailed:
		session.established = false
		if reconnectTerminalError(status.Err) {
			state.cancelReconnect(session)
			return
		}
		// A first attempt that never reached the server is retried as well. The
		// moment a network path changes, the retry that follows it fails once
		// before the new path is ready, and without this the policy would have
		// spent its whole budget on nothing. The limit is queueReconnect's, and
		// like PhaseClosed above, "off" skips the call so a dial failure under
		// it logs no stopped-reconnect event.
		if state.connectionReconnect != reconnectOff {
			state.queueReconnect(session)
		}
	}
}

func (state *State) takeDueReconnect(session *Session, now time.Duration) bool {
	if !session.reconnectPending || now < session.reconnectDue {
		return false
	}
	if session.reconnectStop != nil {
		session.reconnectStop()
		session.reconnectStop = nil
	}
	session.reconnectPending = false
	state.reconnectWaiters--
	session.reconnectDue = 0
	return true
}

func (state *State) advanceReconnects() {
	if state.reconnectWaiters == 0 {
		return
	}
	now := state.now()
	for index := range state.sessions {
		session := &state.sessions[index]
		if state.takeDueReconnect(session, now) {
			state.startConnectionMode(session, session.host, true)
		}
	}
	for _, w := range state.sftpWindows {
		if w == nil || w.closed || !w.ownsConnection {
			continue
		}
		if state.takeDueReconnect(&w.session, now) {
			w.dialOwned(true)
		}
	}
}

func (state *State) requestReconnectFrames(canvas *gfx.Canvas) {
	now := state.now()
	for index := range state.sessions {
		session := &state.sessions[index]
		if !session.reconnectPending {
			continue
		}
		canvas.RequestFrameIn(max(session.reconnectDue-now, time.Millisecond))
	}
	for _, w := range state.sftpWindows {
		if w == nil || w.closed || !w.ownsConnection || !w.session.reconnectPending {
			continue
		}
		canvas.RequestFrameIn(max(w.session.reconnectDue-now, time.Millisecond))
	}
	// A tunnel waiting for its own retry is a waiter too, even when no tab is.
	for _, tunnel := range state.forwardTunnels {
		if !tunnel.retryPending {
			continue
		}
		canvas.RequestFrameIn(max(tunnel.retryDue-now, time.Millisecond))
	}
}

func (state *State) reconnectStatusText(canvas *gfx.Canvas, session *Session) string {
	if session.reconnectPending {
		remaining := max(session.reconnectDue-state.now(), 0)
		seconds := int64((remaining + time.Second - 1) / time.Second)
		if session.reconnectText == "" || session.reconnectSecond != seconds {
			session.reconnectSecond = seconds
			session.reconnectText = fmt.Sprintf(state.messages.ConnReconnectWaitingFmt, session.reconnectAttempt, clockText(seconds))
		}
		nextSecond := remaining - time.Duration(max(seconds-1, 0))*time.Second
		canvas.RequestFrameIn(min(max(nextSecond, time.Millisecond), max(remaining, time.Millisecond)))
		return session.reconnectText
	}
	if session.reconnectExhausted {
		if session.reconnectText == "" {
			session.reconnectText = fmt.Sprintf(state.messages.ConnReconnectStoppedFmt, session.reconnectAttempt)
		}
		return session.reconnectText
	}
	return ""
}

func (state *State) setReconnectPolicy(policy reconnectPolicy) bool {
	if state.connectionReconnect == policy {
		return false
	}
	state.connectionReconnect = policy
	state.applyReconnectPolicy()
	return true
}

func (state *State) applySessionReconnectPolicy(session *Session, policy reconnectPolicy) {
	if policy == reconnectOff {
		state.cancelReconnect(session)
		return
	}
	if policy == reconnectThree && session.reconnectAttempt >= 3 && session.reconnectPending {
		if session.reconnectStop != nil {
			session.reconnectStop()
			session.reconnectStop = nil
		}
		session.reconnectPending = false
		state.reconnectWaiters--
		session.reconnectDue = 0
		session.reconnectExhausted = true
		session.reconnectText = ""
	}
}

// applyReconnectPolicy applies the policy the page's choice already wrote.
func (state *State) applyReconnectPolicy() {
	policy := state.connectionReconnect
	for index := range state.sessions {
		state.applySessionReconnectPolicy(&state.sessions[index], policy)
	}
	for _, w := range state.sftpWindows {
		if w != nil && !w.closed && w.ownsConnection {
			state.applySessionReconnectPolicy(&w.session, policy)
		}
	}
	// Forwards that waited for a retry this change took away stop now.
	if policy == reconnectOff {
		state.clearAllRevivingForwards()
	}
	state.releaseWaitingForwards()
	state.settleForwardTunnelReconnects()
	state.syncLinkWatch()
}
