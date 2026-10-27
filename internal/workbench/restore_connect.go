package workbench

import (
	"github.com/vivaui/viva/ui"
	"motty/internal/sshconn"
)

// restoreConnectPolicy is what reopening the app does with the tabs of the
// last session: nothing, connect the tab in front (all its panes), or connect
// them all. Either way a tab whose host needs the user (a password, a key passphrase or a host
// key to confirm) is left on its page instead of raising a dialog nobody asked
// for; it says so and waits for Connect.
type restoreConnectPolicy uint8

const (
	// restoreConnectOff is the zero value and the default: a restored tab is
	// metadata until the user connects it, so reopening the app never dials.
	restoreConnectOff restoreConnectPolicy = iota
	// restoreConnectActive is the tab in front, all its panes, when the user
	// chose to connect on restore.
	restoreConnectActive
	// restoreConnectAll is every restored tab, when the user chose it.
	restoreConnectAll
)

// maxAutoRestoreConnections bounds how many tabs one restore dials at once, so
// reopening a large workspace does not open dozens of connections together.
const maxAutoRestoreConnections = 8

func (policy restoreConnectPolicy) String() string {
	switch policy {
	case restoreConnectActive:
		return "current"
	case restoreConnectAll:
		return "all-tabs"
	default:
		return "off"
	}
}

// restoreConnectPolicyFromString reads a saved policy. Files written before the
// default changed carry "active" or "all": "active" was the old default and
// cannot be told apart from a choice, so both read as off and the user chooses
// again. Choices written since then use tokens of their own ("current",
// "all-tabs"), which is what keeps them from being taken for a legacy value. A
// state saved before the setting existed has none at all, which is off too.
func restoreConnectPolicyFromString(value string) restoreConnectPolicy {
	switch value {
	case restoreConnectActive.String():
		return restoreConnectActive
	case restoreConnectAll.String():
		return restoreConnectAll
	default:
		return restoreConnectOff
	}
}

// connectRestoredSessions dials the restored tabs the policy names. It runs
// once, from the first Wake after the saved state was applied: the window must
// exist so the connections can wake it.
func (state *State) connectRestoredSessions() {
	if state.restoreConnect == restoreConnectOff {
		return
	}
	// "Active" means what is in front: every pane of the active tab, not just the
	// focused one, since a split tab shows them all.
	var visible map[uint32]bool
	if state.restoreConnect == restoreConnectActive {
		workspaceID := state.deck.ActiveWorkspace()
		panes := state.deck.Panes(workspaceID, nil)
		visible = make(map[uint32]bool, len(panes)+1)
		visible[state.activeSessionID] = true
		for _, pane := range panes {
			visible[uint32(pane.SessionID)] = true
		}
	}
	dialed := 0
	for index := range state.sessions {
		session := &state.sessions[index]
		if !session.restored || !state.canReconnect(session) {
			continue
		}
		if visible != nil && !visible[session.ID] {
			continue
		}
		if dialed == maxAutoRestoreConnections {
			break
		}
		state.reconnect(session)
		session.autoRestore = true
		dialed++
	}
}

// isPromptPhase reports whether the connection is waiting for the user.
func isPromptPhase(phase sshconn.Phase) bool {
	return phase == sshconn.PhaseHostKeyPrompt || phase == sshconn.PhaseKeyPassphrasePrompt || phase == sshconn.PhasePasswordPrompt
}

// abandonRestoredConnect takes a restored tab back to its page when the
// connection restore started reaches a question for the user. The connection is
// cancelled, so no dialog opens; the page explains, and Connect starts it again
// as an ordinary connection that may ask.
func (state *State) abandonRestoredConnect(session *Session) {
	if session.conn != nil {
		session.conn.Cancel()
	}
	if state.modalSession == session.ID && (state.modal == modalHostKey || state.modal == modalKeyPassphrase || state.modal == modalPassword) {
		state.closeModal()
	}
	session.conn = nil
	session.term = nil
	session.restored = true
	session.autoRestore = false
	session.phase = sshconn.PhaseClosed
	session.attempt = 0
	session.busy = false
	session.tone = toneBusy // the zero tone, as a freshly restored tab has
	session.stage, session.since, session.finished = stageConnect, 0, 0
	session.err = nil
	session.restoreNote = state.messages.SessionRestoreNeedsYou
	state.status = state.messages.StatusReadyIdle
}

func (state *State) setRestoreConnectPolicy(policy restoreConnectPolicy) bool {
	if state.restoreConnect == policy {
		return false
	}
	state.restoreConnect = policy
	return true
}

// restoredPageKey lets Enter bring a restored tab back from its page. Nothing on
// that page has the keyboard (it has no terminal yet), so the key is the page's.
func (state *State) restoredPageKey(event ui.KeyEvent) (ui.EventResult, bool) {
	if event.Key != ui.KeyEnter || event.Repeat || state.modal != modalNone || state.activeSection != sectionTerminal {
		return ui.EventResult{}, false
	}
	// Choosing a tab leaves the keyboard on that tab, and a live terminal takes it
	// from there by itself. This page has nothing to take it with, so Enter on the
	// tab that is already showing it means the page's one action.
	if !event.Target.IsZero() && event.Target != hitTabList.At(int(state.deck.ActiveWorkspace())) {
		return ui.EventResult{}, false
	}
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	if !ok || !session.restored || session.term != nil {
		return ui.EventResult{}, false
	}
	state.reconnect(session)
	return ui.EventResult{Handled: true, Repaint: true}, true
}

// shareConnectionsPolicy is whether a new tab on a host that another tab of the
// window is already connected to borrows that connection. It is the zero value,
// on: a second shell then needs no handshake, no host-key check and no password.
type shareConnectionsPolicy uint8

const (
	shareConnectionsOn shareConnectionsPolicy = iota
	shareConnectionsOff
)

func (policy shareConnectionsPolicy) String() string {
	if policy == shareConnectionsOff {
		return "off"
	}
	return "on"
}

func shareConnectionsPolicyFromString(value string) shareConnectionsPolicy {
	if value == shareConnectionsOff.String() {
		return shareConnectionsOff
	}
	return shareConnectionsOn
}

func (state *State) setShareConnections(policy shareConnectionsPolicy) bool {
	if state.shareConnections == policy {
		return false
	}
	state.shareConnections = policy
	return true
}
