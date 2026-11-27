package workbench

import layoutcore "motty/internal/workspace"

// tabDoubleClickAction is what a double click on a session tab does. It is a
// saved string rather than a run-local number so a state file that predates the
// setting reads as the default: rename, the behavior it has always had.
type tabDoubleClickAction string

const (
	tabDoubleClickDuplicate tabDoubleClickAction = "duplicate"
	tabDoubleClickRename    tabDoubleClickAction = "rename"
	tabDoubleClickClose     tabDoubleClickAction = "close"
	tabDoubleClickNone      tabDoubleClickAction = "none"
)

func (action tabDoubleClickAction) String() string { return string(action) }

// tabDoubleClickFromString reads a saved choice. An unknown value, including a
// state file that predates the setting, is the default.
func tabDoubleClickFromString(value string) tabDoubleClickAction {
	switch action := tabDoubleClickAction(value); action {
	case tabDoubleClickDuplicate, tabDoubleClickClose, tabDoubleClickNone:
		return action
	}
	return tabDoubleClickRename
}

// tabSessionForWorkspace is the session behind the pane a tab shows.
func (state *State) tabSessionForWorkspace(workspace layoutcore.WorkspaceID) (*Session, bool) {
	paneID := state.deck.ActivePane(workspace)
	pane, ok := state.deck.Pane(workspace, paneID)
	if !ok || pane.SessionID == 0 {
		return nil, false
	}
	return state.sessionByIDPointer(uint32(pane.SessionID))
}

// duplicateTab opens an independent session for the tab's host and puts the new
// tab immediately after it. The tab menu, a double click and the duplicate
// shortcut all go through here, so the new tab lands in the same place however
// it was asked for.
func (state *State) duplicateTab(workspace layoutcore.WorkspaceID) bool {
	session, ok := state.tabSessionForWorkspace(workspace)
	if !ok {
		return false
	}
	index, ok := state.workspaceTab(workspace)
	if !ok {
		return false
	}
	state.pendingWorkspace, state.pendingPane, state.pendingDirectory = 0, 0, ""
	if session.isLocal() && state.localStart == localStartRemember {
		// A copy of a local tab starts in the same folder, the way a split does.
		state.pendingDirectory = session.localDirectory
	}
	if state.newHostSession(session.host) == nil {
		return false
	}
	if duplicate := state.deck.ActiveWorkspace(); duplicate != 0 {
		_ = state.deck.MoveWorkspace(duplicate, index+1)
		state.rebuildTabOptions()
		state.save()
	}
	return true
}
