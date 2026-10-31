package workbench

import (
	"fmt"
	"github.com/vivaui/viva/ui"

	layoutcore "motty/internal/workspace"
)

// Broadcast input: while a tab is broadcasting, what the user types into one of
// its panes is typed into all of them. It is for running one command on several
// servers, so it follows the user's hands only: output, mouse reports, and the
// assistant's commands never leave the pane they belong to.

// broadcastHits is the place of the controls this file paints.
var broadcastHits = tree.Feature("broadcast")

var hitBroadcast = tree.ID(broadcastHits, "broadcast") // broadcastPaneLimit bounds how many panes one broadcast reaches without
// allocating; a tab holds no more than the restored-session limit.
const broadcastPaneLimit = 16

// typed sends what the user typed to a session's shell and, when its tab is
// broadcasting, to the shells of the other panes of the tab.
func (state *State) typed(session *Session, input []byte) bool {
	accepted := session.term.send(input)
	if accepted && state.broadcast != 0 {
		state.broadcastInput(session.ID, input)
	}
	return accepted
}

func (state *State) broadcastInput(source uint32, input []byte) {
	workspaceID, _, ok := state.deck.PaneForSession(layoutcore.SessionID(source))
	if !ok || workspaceID != state.broadcast {
		return
	}
	var scratch [broadcastPaneLimit]layoutcore.Pane
	for _, pane := range state.deck.Panes(workspaceID, scratch[:0]) {
		if uint32(pane.SessionID) == source {
			continue
		}
		if peer, ok := state.sessionByIDPointer(uint32(pane.SessionID)); ok && peer.term != nil && peer.term.shell != nil {
			peer.term.send(input)
		}
	}
}

// broadcasting reports whether the session's tab is the one that broadcasts.
func (state *State) broadcasting(sessionID uint32) bool {
	if state.broadcast == 0 {
		return false
	}
	workspaceID, _, ok := state.deck.PaneForSession(layoutcore.SessionID(sessionID))
	return ok && workspaceID == state.broadcast
}

// toggleBroadcast starts or stops broadcasting in the active tab. A tab with one
// pane has nothing to broadcast to.
func (state *State) toggleBroadcast() {
	m := state.messages.Tools
	workspaceID := state.deck.ActiveWorkspace()
	if workspaceID != 0 && state.broadcast == workspaceID {
		state.broadcast = 0
		state.status = m.StatusBroadcastOff
		return
	}
	panes := len(state.deck.Panes(workspaceID, nil))
	if workspaceID == 0 || panes < 2 {
		state.showActionWarning(m.BroadcastStart, m.StatusBroadcastNeedsSplit)
		return
	}
	state.broadcast = workspaceID
	state.status = fmt.Sprintf(m.StatusBroadcastOnFmt, panes)
}

// clickTerminalTools handles the controls of the terminal's tools: color
// scheme cards, highlight rules, snippets and broadcast. It reports whether the
// id was one of them.
func (state *State) clickTerminalTools(id ui.ID) bool {
	switch {
	case id == hitBroadcast:
		state.toggleBroadcast()
	case id.Scope() == hitSettingsSchemeList:
		index := item(id)
		if index < schemeCount+len(state.dirSchemes) {
			schemeID, _, _ := state.schemeAt(index)
			if state.setTerminalScheme(schemeID) {
				state.save()
			}
		}
	case id == hitSchemeShowAllDark:
		state.schemesExpandedDark = !state.schemesExpandedDark
	case id == hitSchemeShowAllLight:
		state.schemesExpandedLight = !state.schemesExpandedLight
	case id == hitSettingsHighlightToggle:
		if state.setBuiltinHighlight(state.highlighting.builtinOff) {
			state.save()
		}
	case id == hitSettingsCursorToggle:
		state.terminalCursorSteady = !state.terminalCursorSteady
		state.save()
	case id == hitSettingsCopyOnSelect:
		state.terminalCopyOnSelect = !state.terminalCopyOnSelect
		state.save()
	case id == hitSettingsTabTitle:
		state.terminalTabTitle = !state.terminalTabTitle
		state.save()
	case id == hitSettingsLatencyToggle:
		state.terminalShowLatency = !state.terminalShowLatency
		state.save()
	case id == hitSettingsRightClickPaste:
		state.terminalRightClickPaste = !state.terminalRightClickPaste
		state.save()
	case id == hitSettingsBellToggle:
		state.terminalBellOff = !state.terminalBellOff
		state.save()
	case id == hitSettingsLinkPlainClick:
		state.terminalLinkPlainClick = !state.terminalLinkPlainClick
		state.save()
	case id == hitSettingsNotifyToggle:
		if state.setNotifications(state.notifyOff) {
			state.save()
		}
	case id == hitSettingsPasteImageToggle:
		if state.setPasteImage(state.pasteImageOff) {
			state.save()
		}
	case id == hitSettingsProxyConfigure:
		state.openOutboundProxyDialog()
	case id == hitSettingsFontPick:
		state.openFontPicker(false)
	case id == hitSettingsFallbackAdd:
		state.openFontPicker(true)
	case id == hitSettingsFallbackClear:
		if state.clearFallbackFonts() {
			state.save()
		}
	case id == hitImportSessions:
		state.modal = modalImportSessions
	case id == hitSettingsAutofillAdd:
		state.openAutofillDialog(nil)
	case id.Scope() == hitAutofillToggleList:
		state.toggleAutofillRule(item(id))
	case id.Scope() == hitAutofillEditList:
		if index := item(id); index >= 0 && index < len(state.autofillRules.rules) {
			state.openAutofillDialog(state.autofillRules.rules[index])
		}
	case id.Scope() == hitAutofillDeleteList:
		state.deleteAutofillRule(item(id))
	case id == hitSettingsHighlightAdd:
		state.openHighlightDialog()
	case id.Scope() == hitHighlightDeleteList:
		state.deleteHighlightRule(item(id))
	case id.Scope() == hitSnippetSendList:
		state.sendSnippet(item(id))
	default:
		return false
	}
	return true
}
