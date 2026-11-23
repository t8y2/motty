package workbench

import (
	"github.com/vivaui/viva/gfx"
	layoutcore "motty/internal/workspace"
)

// duplicateTerminal 在聚焦终端后插入副本，均衡平铺并保留显示模式。
func (state *State) duplicateTerminal(target gfx.ID) gfx.EventResult {
	session := state.focusedShortcutTerminal(target)
	if session == nil {
		return gfx.EventResult{}
	}
	workspaceID, paneID, _ := state.deck.PaneForSession(layoutcore.SessionID(session.ID))
	if len(state.deck.Panes(workspaceID, nil)) >= layoutcore.MaxPanes {
		state.showActionWarning(state.messages.ShortcutDuplicateTerminal, state.messages.WorkspaceDraftLimit)
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	// 创建会话可能重新分配 sessions，提前复制源信息。
	host, directory := session.host, session.splitDirectory()
	title, custom := session.Title, session.customTitle
	state.activatePane(workspaceID, paneID)
	created, err := state.deck.Split(workspaceID, paneID, layoutcore.Vertical)
	if err != nil {
		return gfx.EventResult{Handled: true}
	}
	panes := state.deck.Panes(workspaceID, nil)
	order := make([]layoutcore.PaneID, len(panes))
	for index, pane := range panes {
		order[index] = pane.ID
	}
	spec := duplicateTerminalGrid(len(panes))
	if err := state.deck.SetLayout(workspaceID, order, spec); err != nil {
		return gfx.EventResult{Handled: true}
	}
	// 调整布局不应把普通终端标签转换成带工具栏的工作区。
	if state.workspaceManaged(workspaceID) {
		state.managedWorkspaceLayouts[workspaceID] = spec
	}
	if _, temporary := state.temporaryWorkspaceLayouts[workspaceID]; temporary {
		state.temporaryWorkspaceLayouts[workspaceID] = spec
	}
	state.syncActiveSession()
	state.refreshWorkspaceViews()
	state.pendingWorkspace, state.pendingPane = workspaceID, created
	state.pendingDirectory = directory
	if copied := state.newHostSession(host); copied != nil && custom {
		state.renameSession(copied, title)
		copied.customTitle = true
	}
	state.rebuildManagedWorkspace(workspaceID)
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

// duplicateTerminalGrid 使 12 个面板最多占用三行，末行自动均分宽度。
func duplicateTerminalGrid(count int) layoutcore.LayoutSpec {
	columns := uint8(1)
	switch {
	case count >= 10:
		columns = 4
	case count >= 5:
		columns = 3
	case count >= 2:
		columns = 2
	}
	return layoutcore.GridSpec(columns)
}
