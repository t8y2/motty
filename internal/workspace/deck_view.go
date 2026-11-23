package workspace

// PaneMaximized 返回标签页是否只显示当前活动分屏。
func (deck *Deck) PaneMaximized(id WorkspaceID) bool {
	workspace, ok := deck.workspace(id)
	return ok && workspace.paneMaximized && len(workspace.panes) > 1
}

// SetPaneMaximized 只改变显示方式，不修改基础分屏树。
func (deck *Deck) SetPaneMaximized(id WorkspaceID, enabled bool) bool {
	workspace, ok := deck.workspace(id)
	if !ok {
		return false
	}
	enabled = enabled && len(workspace.panes) > 1
	if workspace.paneMaximized == enabled {
		return false
	}
	workspace.paneMaximized = enabled
	workspace.revision++
	return true
}
