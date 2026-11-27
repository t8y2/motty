package workbench

import (
	"fmt"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"slices"

	layoutcore "motty/internal/workspace"
)

// Broadcast input: while a tab is broadcasting, what the user types into one of
// its panes is typed into all of them. It is for running one command on several
// servers, so it follows the user's hands only: output, mouse reports, and the
// assistant's commands never leave the pane they belong to. A broadcast to all
// tabs reaches every connected pane of the window instead, so a tab need not be
// split, or the servers gathered into one tab, to run one command on them.

// broadcastHits is the place of the controls this file paints.
var broadcastHits = tree.Feature("broadcast")

var (
	hitBroadcast    = tree.ID(broadcastHits, "broadcast")
	hitBroadcastAll = tree.ID(broadcastHits, "broadcast-all")
	// hitBroadcastTab is the command palette's entry that toggles the tab's
	// broadcast; the toolbar's hitBroadcast opens the menu that chooses the scope.
	hitBroadcastTab = tree.ID(broadcastHits, "broadcast-tab")

	hitBroadcastMenuDismiss = tree.ID(broadcastHits, "broadcast-menu-dismiss")
	hitBroadcastMenuGo      = tree.ID(broadcastHits, "broadcast-menu-go")
	hitBroadcastMenuAll     = tree.ID(broadcastHits, "broadcast-menu-all")
	hitBroadcastMenuCurrent = tree.ID(broadcastHits, "broadcast-menu-current")
	hitBroadcastMenuTabs    = tree.Sub(broadcastHits, tree.Name("broadcast-menu-tab"))
)

// broadcastMenu is the popover under the toolbar's broadcast button, where the
// scope of the broadcast is chosen. Closed, it costs nothing.
type broadcastMenu struct {
	open   bool
	anchor gfx.Rect
	// picked are the tabs the broadcast would reach, chosen before it starts.
	picked []layoutcore.WorkspaceID
}

// broadcastPaneLimit bounds how many panes one broadcast reaches without
// allocating; a tab holds no more than the restored-session limit.
const broadcastPaneLimit = 16

// typed sends what the user typed to a session's shell and, when its tab is
// broadcasting, to the shells of the other panes of the tab.
func (state *State) typed(session *Session, input []byte) bool {
	accepted := session.term.send(input)
	if accepted && state.broadcastOn() {
		state.broadcastInput(session.ID, input, input, true, false)
	}
	return accepted
}

// typedKeyState is typed for keys that carry key state instead of typing: a
// bare modifier or a release. The panes of a broadcasting tab need the same
// state, but their selection and view stay where the user put them.
func (state *State) typedKeyState(session *Session, input []byte) bool {
	accepted := session.term.sendKeyState(input)
	if accepted && state.broadcastOn() {
		state.broadcastInput(session.ID, input, input, false, false)
	}
	return accepted
}

func (state *State) typedWin32Key(session *Session, input, logical []byte, typing bool) bool {
	if session.term.transferHoldsInput {
		return false
	}
	unknown := typing && len(logical) == 0
	accepted := session.term.sendLogicalInput(input, logical, true, typing, unknown)
	if accepted && state.broadcastOn() {
		state.broadcastInput(session.ID, input, logical, typing, unknown)
	}
	return accepted
}

// broadcastOn reports whether typing is being copied to other panes, in one tab
// or in all of them.
func (state *State) broadcastOn() bool {
	return state.broadcast != 0 || state.broadcastAll
}

func (state *State) broadcastInput(source uint32, input, logical []byte, typing, unknown bool) {
	workspaceID, _, ok := state.deck.PaneForSession(layoutcore.SessionID(source))
	if !ok {
		return
	}
	if state.broadcastAll {
		for index := range state.sessions {
			peer := &state.sessions[index]
			if peer.ID == source || !state.sessionInTab(peer.ID) || workspaceSessionState(peer) != workspacePaneConnected {
				continue
			}
			if !peer.term.transferHoldsInput {
				peer.term.sendLogicalInput(input, logical, true, typing, unknown)
			}
		}
		return
	}
	if workspaceID != state.broadcast {
		return
	}
	var scratch [broadcastPaneLimit]layoutcore.Pane
	for _, pane := range state.deck.Panes(workspaceID, scratch[:0]) {
		if uint32(pane.SessionID) == source {
			continue
		}
		if peer, ok := state.sessionByIDPointer(uint32(pane.SessionID)); ok && workspaceSessionState(peer) == workspacePaneConnected {
			if !peer.term.transferHoldsInput {
				peer.term.sendLogicalInput(input, logical, true, typing, unknown)
			}
		}
	}
}

// broadcastScope is what a broadcast reaches, kept to put it back when an
// operation that cleared it is rolled back.
type broadcastScope struct {
	tab  layoutcore.WorkspaceID
	all  bool
	tabs []layoutcore.WorkspaceID
}

func (state *State) broadcastScope() broadcastScope {
	return broadcastScope{tab: state.broadcast, all: state.broadcastAll, tabs: state.broadcastTabs}
}

func (state *State) restoreBroadcast(scope broadcastScope) {
	state.broadcast, state.broadcastAll, state.broadcastTabs = scope.tab, scope.all, scope.tabs
}

// clearBroadcast stops input from reaching other panes, in one tab or in all of
// them. Callers use it before a connection can change who is typing to whom.
func (state *State) clearBroadcast() {
	state.broadcast = 0
	state.broadcastAll = false
	state.broadcastTabs = nil
}

// clearTabBroadcast stops the broadcast of one tab when the tab in view changes.
// A broadcast to all tabs is made to be typed from any of them and stays.
func (state *State) clearTabBroadcast() {
	state.broadcast = 0
}

// clearWorkspaceBroadcast closes broadcast when workspaceID is its target, or
// when the broadcast reaches all tabs and so reaches this one.
func (state *State) clearWorkspaceBroadcast(workspaceID layoutcore.WorkspaceID) {
	if state.broadcast == workspaceID {
		state.broadcast = 0
	}
	state.broadcastAll = false
	state.broadcastTabs = nil
}

// broadcasting reports whether typing into the session's pane is copied to
// other panes.
func (state *State) broadcasting(sessionID uint32) bool {
	if state.broadcastAll {
		return state.sessionInTab(sessionID)
	}
	if state.broadcast == 0 {
		return false
	}
	workspaceID, _, ok := state.deck.PaneForSession(layoutcore.SessionID(sessionID))
	return ok && workspaceID == state.broadcast
}

func (state *State) sessionInTab(sessionID uint32) bool {
	workspaceID, _, ok := state.deck.PaneForSession(layoutcore.SessionID(sessionID))
	return ok && (state.broadcastTabs == nil || slices.Contains(state.broadcastTabs, workspaceID))
}

// broadcastAllStatus counts the shells a multi-tab broadcast reaches and the
// panes that are still connecting.
func (state *State) broadcastAllStatus() (connected, pending int) {
	return state.broadcastCount(state.broadcastTabs)
}

// broadcastCount is broadcastAllStatus for the tabs, or for every tab when nil.
func (state *State) broadcastCount(tabs []layoutcore.WorkspaceID) (connected, pending int) {
	for index := range state.sessions {
		session := &state.sessions[index]
		workspaceID, _, ok := state.deck.PaneForSession(layoutcore.SessionID(session.ID))
		if !ok || (tabs != nil && !slices.Contains(tabs, workspaceID)) {
			continue
		}
		switch workspaceSessionState(session) {
		case workspacePaneConnected:
			connected++
		case workspacePanePending:
			pending++
		}
	}
	return connected, pending
}

// toggleBroadcast starts or stops broadcasting in the active tab. A tab with one
// pane has nothing to broadcast to.
func (state *State) toggleBroadcast() {
	m := state.messages.Tools
	workspaceID := state.deck.ActiveWorkspace()
	if state.broadcastAll || (workspaceID != 0 && state.broadcast == workspaceID) {
		state.clearBroadcast()
		state.status = m.StatusBroadcastOff
		return
	}
	status := state.workspaceConnectionState(workspaceID)
	if workspaceID == 0 || status.Panes < 2 {
		state.showActionWarning(m.BroadcastStart, m.StatusBroadcastNeedsSplit)
		return
	}
	if status.Pending > 0 {
		state.showActionWarning(m.BroadcastStart, m.StatusBroadcastWait)
		return
	}
	if status.Connected < 2 {
		state.showActionWarning(m.BroadcastStart, m.StatusBroadcastNeedsTwo)
		return
	}
	state.broadcast = workspaceID
	state.status = fmt.Sprintf(m.StatusBroadcastOnFmt, status.Connected)
}

// toggleBroadcastAll starts or stops broadcasting to every tab of the window.
func (state *State) toggleBroadcastAll() {
	m := state.messages.Tools
	if state.broadcastAll || state.broadcast != 0 {
		state.clearBroadcast()
		state.status = m.StatusBroadcastOff
		return
	}
	connected, pending := state.broadcastAllStatus()
	switch {
	case pending > 0:
		state.showActionWarning(m.BroadcastAllStart, m.StatusBroadcastWait)
	case connected < 2:
		state.showActionWarning(m.BroadcastAllStart, m.StatusBroadcastNeedsTwo)
	default:
		state.broadcastAll = true
		state.status = fmt.Sprintf(m.StatusBroadcastAllOnFmt, connected)
	}
}

// broadcastAvailable reports which scopes could start now: the active tab's
// panes, and every tab of the window.
func (state *State) broadcastAvailable() (tab, all bool) {
	tab = workspaceCanBroadcast(state.workspaceConnectionState(state.deck.ActiveWorkspace()))
	connected, pending := state.broadcastAllStatus()
	return tab, pending == 0 && connected >= 2
}

func (state *State) openBroadcastMenu() {
	if state.modal != modalNone {
		return
	}
	menu := &state.broadcastMenu
	menu.open, menu.anchor = true, state.toolbarDockBounds(hitBroadcast)
	menu.picked = menu.picked[:0]
	if state.broadcastAll && state.broadcastTabs != nil {
		menu.picked = append(menu.picked, state.broadcastTabs...)
		return
	}
	for _, tab := range state.workspaceTabs {
		menu.picked = append(menu.picked, tab.ID)
	}
}

func (state *State) declareBroadcastMenu(c ui.C) {
	menu := &state.broadcastMenu
	if !menu.open {
		return
	}
	if state.modal != modalNone || !state.terminalPage() {
		menu.open = false
		return
	}
	m := state.messages.Tools
	on := state.broadcastOn()
	connected, pending := state.broadcastCount(menu.picked)
	var rows menuRows
	active := state.deck.ActiveWorkspace()
	allPicked := len(menu.picked) == len(state.workspaceTabs)
	rows.addStay(components.ContextMenuItem{ID: hitBroadcastMenuAll, Label: m.BroadcastScopeAll, Kind: components.ContextMenuRadio,
		Selected: allPicked, Disabled: on})
	rows.addStay(components.ContextMenuItem{ID: hitBroadcastMenuCurrent, Label: m.BroadcastScopeTab, Kind: components.ContextMenuRadio,
		Selected: len(menu.picked) == 1 && menu.picked[0] == active && !allPicked, Disabled: on})
	rows.separate()
	for index, tab := range state.workspaceTabs {
		rows.addStay(components.ContextMenuItem{ID: hitBroadcastMenuTabs.At(index), Label: state.tabLabel(tab), Kind: components.ContextMenuCheckbox,
			Checked: slices.Contains(menu.picked, tab.ID), Disabled: on})
	}
	rows.separate()
	if on {
		rows.add(components.ContextMenuItem{ID: hitBroadcastMenuGo, Label: m.BroadcastEnd})
	} else {
		rows.add(components.ContextMenuItem{ID: hitBroadcastMenuGo, Label: m.BroadcastGo, Disabled: pending > 0 || connected < 2})
	}
	state.declareMenuAt(c, &menu.open, menu.anchor, false, &rows, func(id gfx.ID) { state.clickBroadcastMenu(id) })
}

// clickBroadcastMenu handles the menu's rows: a tab's checkbox, which leaves the
// menu open, and the row that starts or stops the broadcast over the ticked tabs.
func (state *State) clickBroadcastMenu(id gfx.ID) bool {
	menu := &state.broadcastMenu
	if index, ok := id.In(hitBroadcastMenuTabs); ok {
		if state.broadcastOn() || index < 0 || index >= len(state.workspaceTabs) {
			return true
		}
		tab := state.workspaceTabs[index].ID
		if at := slices.Index(menu.picked, tab); at >= 0 {
			menu.picked = slices.Delete(menu.picked, at, at+1)
		} else {
			menu.picked = append(menu.picked, tab)
		}
		return true
	}
	if id == hitBroadcastMenuAll || id == hitBroadcastMenuCurrent {
		if state.broadcastOn() {
			return true
		}
		menu.picked = menu.picked[:0]
		if id == hitBroadcastMenuCurrent {
			menu.picked = append(menu.picked, state.deck.ActiveWorkspace())
			return true
		}
		for _, tab := range state.workspaceTabs {
			menu.picked = append(menu.picked, tab.ID)
		}
		return true
	}
	if id != hitBroadcastMenuGo {
		return false
	}
	m := state.messages.Tools
	menu.open = false
	state.pendingFocus = state.activeTerminalFocusHit()
	if state.broadcastOn() {
		state.clearBroadcast()
		state.status = m.StatusBroadcastOff
		return true
	}
	connected, _ := state.broadcastCount(menu.picked)
	state.broadcastTabs = slices.Clone(menu.picked)
	state.broadcastAll = true
	state.status = fmt.Sprintf(m.StatusBroadcastAllOnFmt, connected)
	return true
}

// clickTerminalTools handles the controls of the terminal's tools: color
// scheme cards, highlight rules, snippets and broadcast. It reports whether the
// id was one of them.
func (state *State) clickTerminalTools(id gfx.ID) bool {
	switch {
	case id == hitBroadcast:
		state.openBroadcastMenu()
	case id == hitBroadcastTab:
		state.toggleBroadcast()
	case id == hitBroadcastAll:
		state.toggleBroadcastAll()
	case state.clickBroadcastMenu(id):
	case id.Scope() == hitSettingsSchemeList || id.Scope() == hitSchemeDrawerList:
		index := item(id)
		if index < state.schemeTotal() {
			schemeID, _, _ := state.schemeAt(index)
			if state.setTerminalScheme(schemeID) {
				state.save()
			}
		}
	case id == hitSchemeBrowseAll:
		state.openSchemeBrowser()
	case id == hitSettingsHighlightToggle:
		if state.setBuiltinHighlight(state.highlighting.builtinOff) {
			state.save()
		}
	case id == hitSettingsCursorToggle:
		state.terminalCursorSteady = !state.terminalCursorSteady
		state.save()
	case id == hitSettingsCursorProgram:
		state.terminalCursorProgram = !state.terminalCursorProgram
		state.applyTerminalCursorStyle()
		state.save()
	case id == hitSettingsCopyOnSelect:
		state.terminalCopyOnSelect = !state.terminalCopyOnSelect
		state.save()
	case id == hitSettingsLatencyToggle:
		state.terminalShowLatency = !state.terminalShowLatency
		state.syncLatencyToolbarEntry()
		state.save()
	case id == hitSettingsRightClickPaste:
		state.terminalRightClickPaste = !state.terminalRightClickPaste
		state.save()
	case id == hitSettingsInputClick:
		state.terminalInputClickOff = !state.terminalInputClickOff
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
	case id == hitSettingsPasteConfirmToggle:
		state.pasteConfirmOff = !state.pasteConfirmOff
		if state.pasteConfirmOff {
			state.cancelAllPasteQueues()
		}
		state.save()
	case id == hitSettingsCloseConfirmToggle:
		state.closeConfirmOff = !state.closeConfirmOff
		state.save()
	case id == hitSettingsCloseWindowConfirmToggle:
		state.closeWindowConfirmOff = !state.closeWindowConfirmOff
		state.save()
	case id == hitSettingsPasswordHintToggle:
		if state.setPasswordHint(state.passwordHintOff) {
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
	case state.clickCredentialSettings(id):
	case state.clickProxyProfileSettings(id):
	case state.clickHostTagSettings(id):
	case id == hitSettingsHighlightManage:
		state.openRuleManager(ruleDeleteHighlight)
	case id == hitSettingsAutofillManage:
		state.openRuleManager(ruleDeleteAutofill)
	case id == hitSettingsAutofillAdd:
		state.openAutofillDialog(nil)
	case id == hitSettingsAutofillSudo:
		state.openAutofillSudoPreset()
	case id.Scope() == hitAutofillToggleList:
		state.toggleAutofillRule(item(id))
	case id.Scope() == hitAutofillEditList:
		if index := item(id); index >= 0 && index < len(state.autofillRules.rules) {
			state.openAutofillDialog(state.autofillRules.rules[index])
		}
	case id.Scope() == hitAutofillDeleteList:
		state.askDeleteAutofillRule(item(id))
	case id == hitSettingsHighlightAdd:
		state.openHighlightDialog()
	case id == hitSettingsHighlightImport:
		state.importHighlightRules()
	case id.Scope() == hitHighlightDeleteList:
		state.askDeleteHighlightRule(item(id))
	case id.Scope() == hitSnippetSendList:
		state.sendSnippet(item(id))
		if state.modal == modalNone {
			state.returnKeyboardToTerminal()
		}
	default:
		return false
	}
	return true
}
