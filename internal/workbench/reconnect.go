package workbench

import (
	"errors"
	"fmt"
	"time"

	"github.com/vivaui/viva/ui"
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
	if session.reconnectPending {
		state.reconnectWaiters--
	}
	session.reconnectAttempt = 0
	session.reconnectDue = 0
	session.reconnectPending = false
	session.reconnectExhausted = false
	session.reconnectText = ""
	session.reconnectSecond = 0
}

func (state *State) queueReconnect(session *Session) bool {
	if session.reconnectPending {
		return true
	}
	next := session.reconnectAttempt + 1
	if !state.connectionReconnect.permits(next) {
		session.reconnectPending = false
		session.reconnectDue = 0
		session.reconnectExhausted = session.reconnectAttempt > 0
		session.reconnectText = ""
		state.recordConnectionEvent(session, connectionEventReconnectStopped, "")
		return false
	}
	session.reconnectAttempt = next
	session.reconnectDue = state.now() + reconnectDelay(next)
	session.reconnectPending = true
	state.reconnectWaiters++
	session.reconnectExhausted = false
	session.reconnectText = ""
	session.showLog = false
	state.recordConnectionEvent(session, connectionEventReconnectWaiting, "")
	return true
}

func (state *State) updateReconnect(session *Session, status sshconn.Status) {
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
		if session.reconnectAttempt > 0 {
			state.queueReconnect(session)
		}
	}
}

func (state *State) advanceReconnects() {
	if state.reconnectWaiters == 0 {
		return
	}
	now := state.now()
	for index := range state.sessions {
		session := &state.sessions[index]
		if !session.reconnectPending || now < session.reconnectDue {
			continue
		}
		session.reconnectPending = false
		state.reconnectWaiters--
		session.reconnectDue = 0
		state.startConnectionMode(session, session.host, true)
	}
}

func (state *State) requestReconnectFrames(canvas *ui.Canvas) {
	if state.reconnectWaiters == 0 {
		return
	}
	now := state.now()
	for index := range state.sessions {
		session := &state.sessions[index]
		if !session.reconnectPending {
			continue
		}
		canvas.RequestFrameIn(max(session.reconnectDue-now, time.Millisecond))
	}
}

func (state *State) reconnectStatusText(canvas *ui.Canvas, session *Session) string {
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
	for index := range state.sessions {
		session := &state.sessions[index]
		if policy == reconnectOff {
			state.cancelReconnect(session)
			continue
		}
		if policy == reconnectThree && session.reconnectAttempt >= 3 && session.reconnectPending {
			session.reconnectPending = false
			state.reconnectWaiters--
			session.reconnectDue = 0
			session.reconnectExhausted = true
			session.reconnectText = ""
		}
	}
	return true
}
