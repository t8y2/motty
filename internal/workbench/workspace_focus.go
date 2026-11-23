package workbench

import (
	"github.com/vivaui/viva/gfx"
	layoutcore "motty/internal/workspace"
)

var (
	hitPaneMaximize     = tree.ID(shellHits, "pane-maximize")
	hitPaneMaximizeList = tree.Sub(shellHits, tree.Name("pane-maximize-button"))
)

func (state *State) workspaceSinglePaneView(id layoutcore.WorkspaceID) bool {
	return state.deck.PaneMaximized(id) || state.workspaceTabbed(id)
}

// setPaneMaximized 将显示目标与输入目标一起更新，然后保存完整窗口快照。
func (state *State) setPaneMaximized(id layoutcore.WorkspaceID, pane layoutcore.PaneID, enabled bool) {
	if len(state.deck.Panes(id, nil)) < 2 {
		return
	}
	previous := state.deck.ActivePane(id)
	if !state.activatePane(id, pane) {
		return
	}
	changed := state.deck.SetPaneMaximized(id, enabled)
	if !changed && previous == pane {
		return
	}
	state.cancelTabDrag()
	state.workspaceCardDrag = workspaceCardDrag{}
	state.paneSplitters = state.paneSplitters[:0]
	state.refreshWorkspaceViews()
	state.fitWorkspaceTerminals(id)
	state.fitSFTP(state.viewport)
	state.workspaceHeaderFocusPending = state.activePaneContentFocus()
	state.save()
}

func (state *State) togglePaneMaximized(pane layoutcore.PaneID) {
	if !state.terminalPage() {
		return
	}
	id := state.deck.ActiveWorkspace()
	if pane == 0 {
		pane = state.deck.ActivePane(id)
	}
	state.setPaneMaximized(id, pane, !state.deck.PaneMaximized(id))
}

// 未最大化区域保留给日志、工具栏和侧栏，底部条只占终端区域。
func (state *State) workspaceFocusTabsBounds(id layoutcore.WorkspaceID, size gfx.Size) gfx.Rect {
	frame := state.workspacePixelBoundsFor(id, size)
	return gfx.Rect{X: frame.X, Y: frame.Y + frame.Height, Width: frame.Width, Height: workspacePaneTabsHeight}
}

// 恢复页和空分屏没有终端输入控件，焦点应回到其主要内容动作。
func (state *State) activePaneContentFocus() gfx.ID {
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	if !ok {
		return hitNewTabLocal
	}
	if session.restored {
		return state.paneIdentity(session, hitConnRetry)
	}
	if session.desk != nil {
		return hitTerminalList.At(int(session.ID))
	}
	if state.showsProgress(session) {
		return state.paneIdentity(session, hitConnClose)
	}
	return state.activeTerminalFocusHit()
}
