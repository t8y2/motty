package workbench

import (
	"github.com/vivaui/viva/gfx"
	layoutcore "motty/internal/workspace"
)

// duplicateTerminal 在聚焦终端下方打开同一主机，并继承它的工作目录。
func (state *State) duplicateTerminal(target gfx.ID) gfx.EventResult {
	session := state.focusedShortcutTerminal(target)
	if session == nil {
		return gfx.EventResult{}
	}
	workspaceID, paneID, _ := state.deck.PaneForSession(layoutcore.SessionID(session.ID))
	state.activatePane(workspaceID, paneID)
	state.splitWithCurrentHost(layoutcore.Vertical)
	return gfx.EventResult{Handled: true, Repaint: true, Focus: state.activeTerminalFocusHit()}
}

// focusedShortcutTerminal 解析快捷键的终端焦点，排除其他页面和工作区。
func (state *State) focusedShortcutTerminal(target gfx.ID) *Session {
	if state.activeSection != sectionTerminal {
		return nil
	}
	if _, local, embedded := state.decodePaneHit(target); embedded {
		target = local
	}
	session := state.terminalSession(target)
	if session == nil {
		session = state.terminalPageSession(target)
	}
	// 原生快捷键没有目标标识时，使用当前选中的终端。
	if session == nil && target.IsZero() {
		session, _ = state.sessionByIDPointer(state.activeSessionID)
	}
	if session == nil || session.term == nil || session.host.Address == "" {
		return nil
	}
	workspaceID, _, ok := state.deck.PaneForSession(layoutcore.SessionID(session.ID))
	if !ok || workspaceID != state.deck.ActiveWorkspace() {
		return nil
	}
	return session
}
