package workbench

import (
	"fmt"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/ui"
	"motty/internal/sessionlog"
	"motty/internal/sshconn"
)

// The confirmation before a live session is reconnected or disconnected.
var (
	hitSessionLinkDismiss = tree.ID(tabMenuHits, "session-link-dismiss")
	hitSessionLinkClose   = tree.ID(tabMenuHits, "session-link-close")
	hitSessionLinkCancel  = tree.ID(tabMenuHits, "session-link-cancel")
	hitSessionLinkConfirm = tree.ID(tabMenuHits, "session-link-confirm")
)

// sessionLink is the action a confirmation is about: which session, and whether
// its connection is to be ended or made again.
type sessionLink struct {
	session    uint32
	disconnect bool
}

// canDisconnect reports whether a tab has a connection to end: a running
// session, which is what the menu cannot reconnect and a stuck program needs
// stopped without closing the tab.
func (state *State) canDisconnect(session *Session) bool {
	return session != nil && session.term != nil && !session.term.ended && session.phase == sshconn.PhaseConnected
}

// canReconnectLive reports whether a session that is still running can be
// connected again in its tab.
func (state *State) canReconnectLive(session *Session) bool {
	return state.canDisconnect(session) && session.containerCommand == ""
}

// askSessionLink confirms ending or restarting a live session: both stop
// whatever runs in it, which a close button's neighbour must not do unasked.
func (state *State) askSessionLink(session *Session, disconnect bool) {
	state.sessionLink = sessionLink{session: session.ID, disconnect: disconnect}
	state.modal = modalSessionLink
}

func (state *State) clickSessionLinkDialog(id ui.ID) {
	switch id {
	case hitSessionLinkDismiss, hitSessionLinkClose, hitSessionLinkCancel:
		state.modal = modalNone
		state.sessionLink = sessionLink{}
	case hitSessionLinkConfirm:
		pending := state.sessionLink
		state.modal = modalNone
		state.sessionLink = sessionLink{}
		session, ok := state.sessionByIDPointer(pending.session)
		if !ok {
			return
		}
		if pending.disconnect {
			if state.canDisconnect(session) {
				state.disconnect(session)
			}
		} else if state.canReconnectLive(session) {
			state.reconnect(session)
		}
		state.save()
	}
}

// sessionLinkKey makes Enter on the confirm button confirm.
func (state *State) sessionLinkKey(event ui.KeyEvent) (ui.EventResult, bool) {
	if state.modal != modalSessionLink || event.Key != ui.KeyEnter || event.Target != hitSessionLinkConfirm || event.Repeat {
		return ui.EventResult{}, false
	}
	state.clickSessionLinkDialog(hitSessionLinkConfirm)
	return ui.EventResult{Handled: true, Repaint: true}, true
}

func (state *State) paintSessionLinkDialog(canvas *ui.Canvas, viewport ui.Rect) {
	messages := state.messages
	dialog := components.BeginDialog(canvas, state.Theme(), viewport, components.DialogOptions{
		Layer: dialogLayer, Scope: dialogScope,
		DismissID: hitSessionLinkDismiss, CloseID: hitSessionLinkClose, InitialFocusID: hitSessionLinkCancel,
		ContentSize: ui.Size{Width: 520}, FitBody: true,
	})
	title, description, confirm := messages.ReconnectTitleFmt, messages.ReconnectDescription, messages.ReconnectConfirm
	if state.sessionLink.disconnect {
		title, description, confirm = messages.DisconnectTitleFmt, messages.DisconnectDescription, messages.DisconnectConfirm
	}
	name := ""
	if session, ok := state.sessionByIDPointer(state.sessionLink.session); ok {
		name = session.Title
	}
	layout := dialog.Begin(fmt.Sprintf(title, name), description)
	dialog.Actions(layout,
		components.ButtonAction{ID: hitSessionLinkCancel, Label: messages.Cancel, Variant: components.ButtonSecondary},
		components.ButtonAction{ID: hitSessionLinkConfirm, Label: confirm, Variant: components.ButtonDestructive},
	)
	dialog.End()
}

// disconnect ends a session's connection and what runs over it and leaves the
// tab open, as it is when the server hangs up: the screen stays, the light goes
// idle, and the tab can connect again.
func (state *State) disconnect(session *Session) {
	if session.term == nil {
		return
	}
	session.disconnected = true
	state.cancelReconnect(session)
	state.finishSessionLog(session, sessionlog.StatusEnded, sessionlog.ReasonUserClosed)
	state.stopSessionForwards(session.ID, true)
	state.closeSFTP(session)
	state.stopRemoteStats(session)
	if session.conn != nil {
		session.conn.Cancel()
	}
	state.endDesktop(session)
	state.closeDisconnectedShell(session)
	session.term.abandonShell()
	// Not every connection reports its own end (a local shell has no peer), and
	// the shell is already gone, so the tab is ended here rather than by a status.
	session.term.ended = true
	session.pendingSecretName, session.pendingSecretValue = "", ""
	session.tone = toneBusy
	if session.finished == 0 {
		session.finished = state.now()
	}
	if session.term.told < toldEnded {
		state.tell(session, toldEnded, sshconn.Status{Phase: sshconn.PhaseClosed})
		if session.containerCommand == "" {
			session.term.say(sgrMuted, state.messages.SessionReconnectHint)
		}
	}
	state.describe(session)
	state.status = state.messages.ConnClosed
}
