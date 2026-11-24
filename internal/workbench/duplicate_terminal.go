package workbench

import (
	"github.com/vivaui/viva/gfx"
	layoutcore "motty/internal/workspace"
)

// duplicateTerminal 在聚焦终端后插入副本，保留自定义布局和显示模式。
func (state *State) duplicateTerminal(target gfx.ID) gfx.EventResult {
	session := state.focusedShortcutTerminal(target)
	if session == nil {
		return gfx.EventResult{}
	}
	host, directory := session.host, session.splitDirectory()
	title, custom := session.Title, session.customTitle
	if !state.prepareAdjacentTerminal(session, state.messages.ShortcutDuplicateTerminal) {
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	workspaceID := state.pendingWorkspace
	state.pendingDirectory = directory
	if copied := state.newHostSession(host); copied != nil && custom {
		state.renameSession(copied, title)
		copied.customTitle = true
	}
	state.rebuildManagedWorkspace(workspaceID)
	return gfx.EventResult{Handled: true, Repaint: true, Focus: state.activeTerminalFocusHit()}
}

// prepareAdjacentTerminal 统一新增和复制终端的布局及最大化处理。
func (state *State) prepareAdjacentTerminal(session *Session, title string) bool {
	workspaceID, paneID, _ := state.deck.PaneForSession(layoutcore.SessionID(session.ID))
	if len(state.deck.Panes(workspaceID, nil)) >= layoutcore.MaxPanes {
		state.showActionWarning(title, state.messages.WorkspaceDraftLimit)
		return false
	}
	balanced := state.duplicateTerminalUsesGrid(workspaceID)
	previousPane := state.deck.ActivePane(workspaceID)
	maximized := state.deck.PaneMaximized(workspaceID)
	created, err := state.deck.Split(workspaceID, paneID, layoutcore.Vertical)
	if err != nil {
		return false
	}
	panes := state.deck.Panes(workspaceID, nil)
	order := make([]layoutcore.PaneID, len(panes))
	for index, pane := range panes {
		order[index] = pane.ID
	}
	layout, _ := state.deck.Layout(workspaceID)
	spec := layout.Spec()
	if balanced {
		spec = duplicateTerminalGrid(len(panes))
		if err := state.deck.SetLayout(workspaceID, order, spec); err != nil {
			// 删除刚拆出的空面板，恢复焦点及最大化状态。
			state.deck.ClosePane(workspaceID, created)
			state.deck.ActivatePane(workspaceID, previousPane)
			state.deck.SetPaneMaximized(workspaceID, maximized)
			return false
		}
	}
	if state.workspaceTabbed(workspaceID) {
		spec = layoutcore.LayoutSpec{Kind: layoutcore.LayoutTabs}
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
	return true
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
