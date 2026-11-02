package workbench

import (
	"strings"

	layoutcore "motty/internal/workspace"
)

// tabLabel is the name a tab shows. A tab follows the title its active
// terminal reports (OSC 0 and 2, which tmux sets from its window and session
// names with set-titles) when the setting is on, unless the user named the tab
// themself. The stored workspace title stays the host's or the user's name: the
// reported one is a property of what is running right now, so it is neither
// saved nor used to address the session in questions.
func (state *State) tabLabel(workspace layoutcore.WorkspaceInfo) string {
	if !state.terminalTabTitle || workspace.Named {
		return workspace.Title
	}
	session := state.tabSession(workspace)
	if session == nil || session.term == nil || session.term.screen == nil {
		return workspace.Title
	}
	if reported := strings.TrimSpace(session.term.screen.Title()); reported != "" {
		return reported
	}
	return workspace.Title
}

// tabSession returns the session behind the pane the tab snapshot currently
// represents. Tab labels and badges share this lookup so a pane activation
// updates both together.
func (state *State) tabSession(workspace layoutcore.WorkspaceInfo) *Session {
	pane, ok := state.deck.Pane(workspace.ID, workspace.ActivePane)
	if !ok || pane.SessionID == 0 {
		return nil
	}
	session, _ := state.sessionByIDPointer(uint32(pane.SessionID))
	return session
}
