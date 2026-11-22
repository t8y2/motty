package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
)

// menuLabelState is the state a test built last: the one whose messages name the
// rows of the page menus a headless session shows.
var menuLabelState *State

// menuRowIDs are the rows of page menus, which the page names by their place and
// the tests by what the application calls the choice.
var menuRowIDs = map[gfx.ID]bool{}

func init() {
	for _, id := range []gfx.ID{
		hitHostMenuConnect, hitHostMenuSFTP, hitHostMenuAddress, hitHostMenuName, hitHostMenuCopy, hitHostMenuPaste, hitHostMenuCopyTo,
		hitHostMenuEdit, hitHostMenuCopyEdit, hitHostMenuSetJump, hitHostMenuMove, hitHostMenuDelete,
		hitGroupNewHost, hitGroupNewChild, hitGroupPaste, hitGroupRename, hitGroupMove, hitGroupPin, hitGroupUp, hitGroupDown, hitGroupDelete,
		hitHostBlankNewHost, hitHostBlankNewGroup, hitHostBlankPaste,
		hitTerminalMenuCopy, hitTerminalMenuPaste, hitTerminalMenuSelectAll, hitTerminalMenuClearScreen, hitTerminalMenuClear, hitTerminalMenuSave, hitTerminalMenuSplitRight, hitTerminalMenuSplitDown, hitTerminalMenuRename, hitTerminalMenuClosePane, hitTerminalMenuFolder, hitTerminalMenuFiles, hitTerminalMenuTransfer, hitTerminalMenuAsk, hitTerminalMenuExplain, hitTerminalMenuHistory, hitTerminalMenuSearch, hitTerminalMenuStats, hitTerminalMenuTools, hitTerminalMenuReconnect, hitTerminalMenuRestart, hitTerminalMenuDisconnect,
		hitSnippetMenuSend, hitSnippetMenuUp, hitSnippetMenuDown, hitSnippetMenuEdit, hitSnippetMenuDelete, hitWorkspaceTemplateMenuEdit, hitWorkspaceTemplateMenuDelete,
		hitToolbarDockTop, hitToolbarDockSideTop, hitToolbarDockSideBottom, hitToolbarDockLeftTop, hitToolbarDockLeftBottom, hitToolbarDockBottom, hitToolbarDockBottomRight, hitToolbarDockHide, hitToolbarDockCustomize, hitToolbarDockReset,
		hitTabMenuRename, hitTabMenuDuplicate, hitTabMenuEditHost, hitTabMenuNewWindow, hitTabMenuCopyHost, hitTabMenuCopyAddress, hitTabMenuCopyName, hitTabMenuBrowserClose, hitTabMenuBrowserOpen, hitTabMenuFit, hitTabMenuActual, hitTabMenuCtrlAltDel, hitTabMenuHideBars, hitTabMenuFullscreen, hitTabMenuLatency, hitTabMenuClose, hitTabMenuCloseOthers, hitTabMenuReconnect, hitTabMenuRestart, hitTabMenuDisconnect, hitTabMenuSaveWorkspace, hitTabMenuLock, hitTabMenuCloseRight,
		hitSFTPMenuOpen, hitSFTPMenuDownload, hitSFTPMenuDownloadHere, hitSFTPMenuSend, hitSFTPMenuInsertPath, hitSFTPMenuEdit, hitSFTPMenuRename, hitSFTPMenuMove, hitSFTPMenuChmod, hitSFTPMenuChown, hitSFTPMenuExtract, hitSFTPMenuCompress, hitSFTPMenuDelete, hitSFTPMenuSudo, hitSFTPMenuCopyFiles, hitSFTPMenuPasteFiles,
	} {
		menuRowIDs[id] = true
	}
	for _, id := range hitTerminalMenuAction {
		menuRowIDs[id] = true
	}
}

// menuRowLabel is the label the page paints for a row the application names id.
func menuRowLabel(id gfx.ID) (label string, ok bool) {
	if !menuRowIDs[id] || menuLabelState == nil {
		return "", false
	}
	defer func() {
		if recover() != nil {
			label, ok = "", false
		}
	}()
	return menuLabelOf(menuLabelState, id), true
}

// menuLabelOf names the row of a page menu that used to be a hit of its own: the
// identity the application keeps for the row's action, and the label the page paints.
func menuLabelOf(state *State, id gfx.ID) string {
	m := state.messages
	g := m.HostGroupActions
	switch id {
	case hitHostMenuConnect:
		return m.Connect
	case hitHostMenuSFTP:
		return m.SFTPWindow.Open
	case hitHostMenuAddress:
		return m.CopyAddressAndPort
	case hitHostMenuName:
		return m.CopyConnectionName
	case hitHostMenuCopy:
		return m.CopyHost
	case hitHostMenuPaste:
		return m.PasteHost
	case hitHostMenuCopyTo:
		return m.CopyHostTo
	case hitHostMenuEdit:
		return m.EditHost
	case hitHostMenuCopyEdit:
		return m.CopyHostAndEdit
	case hitHostMenuSetJump:
		return m.HostMenuSetJump
	case hitHostMenuMove:
		return m.MoveHostGroup
	case hitHostMenuDelete:
		return m.DeleteHost
	case hitGroupNewHost:
		return m.NewHost
	case hitGroupNewChild:
		return g.NewChild
	case hitGroupPaste:
		return m.PasteHost
	case hitGroupRename:
		return g.Rename
	case hitGroupMove:
		return g.Move
	case hitGroupPin:
		return g.Pin
	case hitGroupUp:
		return g.MoveUp
	case hitGroupDown:
		return g.MoveDown
	case hitGroupDelete:
		return g.Delete
	case hitHostBlankNewHost:
		return m.NewHost
	case hitHostBlankNewGroup:
		return m.NewGroup
	case hitHostBlankPaste:
		return m.PasteHost
	case hitTerminalMenuCopy:
		return m.MenuCopy
	case hitTerminalMenuPaste:
		return m.MenuPaste
	case hitTerminalMenuSelectAll:
		return m.MenuSelectAll
	case hitTerminalMenuClearScreen:
		return m.MenuClearScreen
	case hitTerminalMenuClear:
		return m.MenuClearScrollback
	case hitTerminalMenuSave:
		return m.Tools.MenuSaveOutput
	case hitTerminalMenuSplitRight:
		return m.PaneSplitHorizontal
	case hitTerminalMenuSplitDown:
		return m.PaneSplitVertical
	case hitTerminalMenuRename:
		return m.MenuRenameTerminal
	case hitTerminalMenuClosePane:
		return m.PaneClose
	case hitTerminalMenuFolder:
		return m.ShortcutTerminalFolder
	case hitTerminalMenuFiles:
		return m.SFTPFiles
	case hitTerminalMenuTransfer:
		return m.Tools.XYMenu
	case hitTerminalMenuAsk:
		return m.AIPanel.MenuAskSelection
	case hitTerminalMenuExplain:
		return m.AIPanel.MenuExplainOutput
	case hitTerminalMenuHistory:
		return m.History.Title
	case hitTerminalMenuSearch:
		return m.Tools.MenuSearchSelection
	case hitTerminalMenuStats:
		return m.MenuResourceMonitor
	case hitTerminalMenuTools:
		return m.Tools.RemoteMenu
	case hitTerminalMenuReconnect:
		return m.SessionReconnect
	case hitTerminalMenuRestart:
		return m.MenuReconnect
	case hitTerminalMenuDisconnect:
		return m.MenuDisconnect
	case hitSnippetMenuSend:
		return m.Tools.SnippetSend
	case hitSnippetMenuUp:
		return m.Tools.SnippetMoveUp
	case hitSnippetMenuDown:
		return m.Tools.SnippetMoveDown
	case hitSnippetMenuEdit:
		return m.Tools.SnippetEdit
	case hitSnippetMenuDelete:
		return m.Tools.SnippetDelete
	case hitWorkspaceTemplateMenuEdit:
		return m.WorkspaceTemplateEdit
	case hitWorkspaceTemplateMenuDelete:
		return m.WorkspaceTemplateDelete
	case hitToolbarDockTop:
		return m.Toolbars.MoveTop
	case hitToolbarDockSideTop:
		return m.Toolbars.MoveSideTop
	case hitToolbarDockSideBottom:
		return m.Toolbars.MoveSideBottom
	case hitToolbarDockLeftTop:
		return m.Toolbars.MoveLeftTop
	case hitToolbarDockLeftBottom:
		return m.Toolbars.MoveLeftBottom
	case hitToolbarDockBottom:
		return m.Toolbars.MoveBottom
	case hitToolbarDockBottomRight:
		return m.Toolbars.MoveBottomRight
	case hitToolbarDockHide:
		return m.Toolbars.Hide
	case hitToolbarDockCustomize:
		return m.Toolbars.Customize
	case hitToolbarDockReset:
		return m.Toolbars.ResetLayout
	case hitTabMenuRename:
		return m.MenuRenameTab
	case hitTabMenuDuplicate:
		return m.MenuDuplicateTab
	case hitTabMenuEditHost:
		return m.EditHostTitle
	case hitTabMenuNewWindow:
		return m.MenuMoveTabToNewWindow
	case hitTabMenuCopyHost:
		return m.CopyHostAddress
	case hitTabMenuCopyAddress:
		return m.CopyAddressAndPort
	case hitTabMenuCopyName:
		return m.CopyConnectionName
	case hitTabMenuBrowserClose:
		return m.MenuCloseSSHBrowser
	case hitTabMenuBrowserOpen:
		return m.MenuOpenSSHBrowser
	case hitTabMenuFit:
		return m.Desktop.Fit
	case hitTabMenuActual:
		return m.Desktop.Actual
	case hitTabMenuCtrlAltDel:
		return m.Desktop.CtrlAltDel
	case hitTabMenuHideBars:
		return m.Desktop.HideBars
	case hitTabMenuFullscreen:
		return m.Desktop.Fullscreen
	case hitTabMenuLatency:
		return m.Desktop.ShowLatency
	case hitTabMenuClose:
		return m.LabelCloseTab
	case hitTabMenuCloseOthers:
		return m.MenuCloseOtherTabs
	case hitTabMenuReconnect:
		return m.SessionReconnect
	case hitTabMenuRestart:
		return m.MenuReconnect
	case hitTabMenuDisconnect:
		return m.MenuDisconnect
	case hitTabMenuSaveWorkspace:
		return m.WorkspaceSaveMenu
	case hitTabMenuLock:
		if menuHasRow(state, m.MenuUnlockTab) {
			return m.MenuUnlockTab
		}
		return m.MenuLockTab
	case hitTabMenuCloseRight:
		if state.tabOrientation == tabsVertical {
			return m.MenuCloseTabsBelow
		}
		return m.MenuCloseTabsToRight
	}
	t := m.Tools
	w := m.SFTPWindow
	switch id {
	case hitSFTPMenuOpen:
		return m.SFTPOpen
	case hitSFTPMenuDownload:
		return m.SFTPDownload
	case hitSFTPMenuDownloadHere:
		return t.DownloadHere
	case hitSFTPMenuSend:
		return t.SendToServer
	case hitSFTPMenuInsertPath:
		return m.SFTPInsertPath
	case hitSFTPMenuEdit:
		return t.ExtEditButton
	case hitSFTPMenuRename:
		return m.SFTPRename
	case hitSFTPMenuMove:
		return m.SFTPMove
	case hitSFTPMenuChmod:
		return m.SFTPPermissions
	case hitSFTPMenuChown:
		return m.SFTPOwnership
	case hitSFTPMenuExtract:
		return t.Extract
	case hitSFTPMenuCompress:
		return t.Compress
	case hitSFTPMenuDelete:
		return m.SFTPDelete
	case hitSFTPMenuSudo:
		if menuHasRow(state, m.SFTPSudoDisable) {
			return m.SFTPSudoDisable
		}
		return m.SFTPSudoEnable
	case hitSFTPMenuCopyFiles:
		return w.CopyFiles
	case hitSFTPMenuPasteFiles:
		return w.PasteFiles
	case hitSFTPNewDirectory:
		return m.SFTPNewDirectory
	case hitSFTPUpload:
		return m.SFTPUpload
	case hitSFTPUploadDirectory:
		return m.SFTPUploadDirectory
	case hitSFTPNewLink:
		return t.TipNewLink
	case hitSFTPCopyPath:
		return w.CopyPath
	case hitSFTPProperties:
		return w.PropertiesTitle
	}
	if index, ok := terminalMenuActionIndex(id); ok {
		return state.actionLabel(state.terminalMenu.found[index])
	}
	panic("no label is known for the menu row " + tree.Text(id))
}

// chooseMenu chooses the row of the open page menu that id names.
func chooseMenu(t testing.TB, state *State, id gfx.ID) {
	t.Helper()
	label := menuLabelOf(state, id)
	if id == hitGroupPin && menuHasRow(state, state.messages.HostGroupActions.Unpin) {
		label = state.messages.HostGroupActions.Unpin
	}
	clickPageMenuRow(t, state, label)
}

// menuHasRow reports whether the window shows a page menu row with the label.
func menuHasRow(state *State, label string) bool {
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	return canvasHasMenuRow(&canvas, label)
}

// menuHit is the painted row of the open page menu that id names.
func menuHit(canvas *gfx.Canvas, state *State, id gfx.ID) (gfx.Hit, bool) {
	label := menuLabelOf(state, id)
	for _, hit := range canvas.Hits() {
		if hit.Role == gfx.RoleMenuItem && hit.Label == label {
			return hit, true
		}
	}
	return gfx.Hit{}, false
}

// terminalMenuActionIndex is the place of a row the terminal menu offers for the text
// under the pointer.
func terminalMenuActionIndex(id gfx.ID) (int, bool) {
	for index, action := range hitTerminalMenuAction {
		if action == id {
			return index, true
		}
	}
	return 0, false
}
