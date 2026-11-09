package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"motty/internal/desktop"
	layoutcore "motty/internal/workspace"
)

// tabMenuHits is the place of the controls this file paints.
var tabMenuHits = tree.Feature("tab-menu")

// The tab menu's hit identities: a block of their own, beside the terminal menu's.
var (
	hitTabMenuDismiss       = tree.ID(tabMenuHits, "tab-menu-dismiss")
	hitTabMenuClose         = tree.ID(tabMenuHits, "tab-menu-close")
	hitTabMenuCloseOthers   = tree.ID(tabMenuHits, "tab-menu-close-others")
	hitTabMenuCloseRight    = tree.ID(tabMenuHits, "tab-menu-close-right")
	hitTabMenuDuplicate     = tree.ID(tabMenuHits, "tab-menu-duplicate")
	hitTabMenuEditHost      = tree.ID(tabMenuHits, "tab-menu-edit-host")
	hitTabMenuNewWindow     = tree.ID(tabMenuHits, "tab-menu-new-window")
	hitTabMenuCopyAddress   = tree.ID(tabMenuHits, "tab-menu-copy-address")
	hitTabMenuCopyName      = tree.ID(tabMenuHits, "tab-menu-copy-name")
	hitTabMenuReconnect     = tree.ID(tabMenuHits, "tab-menu-reconnect")
	hitTabMenuRestart       = tree.ID(tabMenuHits, "tab-menu-restart")
	hitTabMenuDisconnect    = tree.ID(tabMenuHits, "tab-menu-disconnect")
	hitTabMenuRename        = tree.ID(tabMenuHits, "tab-menu-rename")
	hitTabMenuLock          = tree.ID(tabMenuHits, "tab-menu-lock")
	hitTabMenuSaveWorkspace = tree.ID(tabMenuHits, "tab-menu-save-workspace")
)

// The desktop rows of the menu, below the tab's own: the controls a desktop
// page's bar holds, for a host that hid its bars — and reachable without them.
var (
	hitTabMenuFit        = tree.ID(tabMenuHits, "tab-menu-fit")
	hitTabMenuActual     = tree.ID(tabMenuHits, "tab-menu-actual")
	hitTabMenuCtrlAltDel = tree.ID(tabMenuHits, "tab-menu-ctrl-alt-del")
	hitTabMenuHideBars   = tree.ID(tabMenuHits, "tab-menu-hide-bars")
	hitTabMenuFullscreen = tree.ID(tabMenuHits, "tab-menu-fullscreen")
	hitTabMenuLatency    = tree.ID(tabMenuHits, "tab-menu-latency")
)

// The rename dialog's identities, after the menu's.
var (
	hitTabRenameClose  = tree.ID(tabMenuHits, "tab-rename-close")
	hitTabRenameCancel = tree.ID(tabMenuHits, "tab-rename-cancel")
	hitTabRenameSubmit = tree.ID(tabMenuHits, "tab-rename-submit")
	hitTabRenameField  = tree.ID(tabMenuHits, "tab-rename-field")
)

// The confirmation before closing several tabs at once.
var (
	hitTabsCloseDismiss = tree.ID(tabMenuHits, "tabs-close-dismiss")
	hitTabsCloseClose   = tree.ID(tabMenuHits, "tabs-close-close")
	hitTabsCloseCancel  = tree.ID(tabMenuHits, "tabs-close-cancel")
	hitTabsCloseConfirm = tree.ID(tabMenuHits, "tabs-close-confirm")
	hitTabsCloseQuiet   = tree.ID(tabMenuHits, "tabs-close-quiet")

	hitSettingsCloseConfirmToggle = tree.ID(tabMenuHits, "settings-close-confirm")
)

// tabMenu is the right-click menu of a tab: which tab, and where it was asked for.
// Closed, it costs nothing.
type tabMenu struct {
	open      bool
	workspace layoutcore.WorkspaceID
	at        gfx.Point
}

// tabMenuPointer closes an open menu on a secondary press outside it (the
// backdrop swallows the press; only a primary one is also a click). Opening it is
// the paint of a tab (openTabMenuOn).
func (state *State) tabMenuPointer(event gfx.PointerEvent) (gfx.EventResult, bool) {
	if event.Kind != gfx.PointerDown || event.Button == gfx.PointerButtonNone || event.Button == gfx.PointerButtonPrimary {
		return gfx.EventResult{}, false
	}
	if state.tabMenu.open && event.Target == hitTabMenuDismiss {
		state.tabMenu.open = false
		return gfx.EventResult{Handled: true, Repaint: true}, true
	}
	return gfx.EventResult{}, false
}

// openTabMenuOn is called while painting a tab and its close button: when this
// paint is the event pass of a secondary press on either, it opens the tab's menu.
func (state *State) openTabMenuOn(canvas *gfx.Canvas, workspace layoutcore.WorkspaceID, tab, closeButton gfx.ID) {
	event, ok := canvas.PointerOn(tab)
	if !ok {
		event, ok = canvas.PointerOn(closeButton)
	}
	if !ok || event.Kind != gfx.PointerDown || event.Button != gfx.PointerButtonSecondary || state.modal != modalNone {
		return
	}
	if _, ok := state.workspaceTab(workspace); !ok {
		return
	}
	state.tabMenu = tabMenu{open: true, workspace: workspace, at: gfx.Point{X: event.X, Y: event.Y}}
	canvas.Consume()
}

// workspaceTab finds a tab by workspace, with its position among the tabs.
func (state *State) workspaceTab(id layoutcore.WorkspaceID) (int, bool) {
	for index, tab := range state.workspaceTabs {
		if tab.ID == id {
			return index, true
		}
	}
	return 0, false
}

// tabMenuSession is the session shown in the menu's tab, if it has one.
func (state *State) tabMenuSession() (*Session, bool) {
	paneID := state.deck.ActivePane(state.tabMenu.workspace)
	pane, ok := state.deck.Pane(state.tabMenu.workspace, paneID)
	if !ok || pane.SessionID == 0 {
		return nil, false
	}
	return state.sessionByIDPointer(uint32(pane.SessionID))
}

func (state *State) tabMenuHost(session *Session) (Host, bool) {
	if session == nil || session.host.CatalogID == "" {
		return Host{}, false
	}
	for _, host := range state.hosts {
		if host.CatalogID == session.host.CatalogID {
			return host, true
		}
	}
	return Host{}, false
}

func (state *State) paintTabMenu(canvas *gfx.Canvas, viewport gfx.Rect) {
	if !state.tabMenu.open {
		return
	}
	_, ok := state.workspaceTab(state.tabMenu.workspace)
	if !ok {
		state.tabMenu.open = false
		return
	}
	messages := state.messages
	session, hasSession := state.tabMenuSession()
	reconnect := hasSession && state.canReconnect(session)
	restart := hasSession && state.canReconnectLive(session)
	disconnect := hasSession && state.canDisconnect(session)
	var keys [6]string
	closeKeys := state.shortcuts.chords.Chord(shortcutCloseTab).Keycaps(keys[:0])
	icon := func(kind uint8) gfx.Icon { return lucideIcons[kind] }
	var rows menuRows
	address, name := "", ""
	if hasSession {
		address, name = connectionAddressAndPort(session.host), session.host.Label
	}
	_, hasHost := state.tabMenuHost(session)
	rows.add(components.ContextMenuItem{ID: hitTabMenuRename, Label: messages.MenuRenameTab, Icon: icon(iconPencil)})
	lockLabel := messages.MenuLockTab
	if state.deck.Locked(state.tabMenu.workspace) {
		lockLabel = messages.MenuUnlockTab
	}
	rows.add(components.ContextMenuItem{ID: hitTabMenuLock, Label: lockLabel, Icon: icon(iconLock)})
	rows.add(components.ContextMenuItem{ID: hitTabMenuDuplicate, Label: messages.MenuDuplicateTab, Icon: icon(iconCopy), Disabled: !hasSession})
	rows.add(components.ContextMenuItem{ID: hitTabMenuEditHost, Label: messages.EditHostTitle, Icon: icon(iconPencil), Disabled: !hasHost})
	rows.add(components.ContextMenuItem{ID: hitTabMenuNewWindow, Label: messages.MenuMoveTabToNewWindow, Icon: icon(iconExternal), Disabled: !state.canTearOffTab()})
	rows.separate()
	rows.add(components.ContextMenuItem{ID: hitTabMenuCopyAddress, Label: messages.CopyAddressAndPort, Icon: icon(iconCopy), Disabled: address == ""})
	rows.add(components.ContextMenuItem{ID: hitTabMenuCopyName, Label: messages.CopyConnectionName, Icon: icon(iconCopy), Disabled: name == ""})
	rows.separate()
	// A remote desktop keeps its scaling and keyboard controls in the page's
	// bar, which a host may have asked to hide: the menu holds them either way.
	if hasSession && session.desk != nil {
		desktop := messages.Desktop
		rows.add(components.ContextMenuItem{ID: hitTabMenuFit, Label: desktop.Fit, Icon: icon(iconDesktop), Kind: components.ContextMenuRadio, Selected: session.desk.fit})
		rows.add(components.ContextMenuItem{ID: hitTabMenuActual, Label: desktop.Actual, Icon: icon(iconGrid2x2), Kind: components.ContextMenuRadio, Selected: !session.desk.fit})
		rows.add(components.ContextMenuItem{ID: hitTabMenuCtrlAltDel, Label: desktop.CtrlAltDel, Icon: icon(iconKeyboard), Disabled: session.desk.ended})
		rows.add(components.ContextMenuItem{ID: hitTabMenuHideBars, Label: messages.Desktop.HideBars, Icon: icon(iconEyeOff), Kind: components.ContextMenuCheckbox, Checked: session.desk.chromeless})
		rows.add(components.ContextMenuItem{ID: hitTabMenuFullscreen, Label: desktop.Fullscreen, Icon: icon(iconDesktop), Keys: []string{"Ctrl", "Alt", "Enter"}, Disabled: session.desk.ended})
		if session.host.Protocol == protocolRDP {
			rows.add(components.ContextMenuItem{ID: hitTabMenuLatency, Label: desktop.ShowLatency, Icon: icon(iconActivity), Kind: components.ContextMenuCheckbox, Checked: state.terminalShowLatency})
		}
		rows.separate()
	}
	rows.separate()
	rows.add(components.ContextMenuItem{ID: hitTabMenuClose, Label: messages.LabelCloseTab, Icon: icon(iconClose), Keys: closeKeys})
	rows.add(components.ContextMenuItem{ID: hitTabMenuCloseOthers, Label: messages.MenuCloseOtherTabs, Icon: icon(iconCloseOthers), Disabled: len(state.workspaceTabs) < 2})
	closeFollowing, closeIcon := messages.MenuCloseTabsToRight, uint8(iconCloseRight)
	if state.tabOrientation == tabsVertical {
		closeFollowing, closeIcon = messages.MenuCloseTabsBelow, iconChevronDown
	}
	following := state.paintedWorkspaceTabsAfter(state.tabMenu.workspace)
	rows.add(components.ContextMenuItem{ID: hitTabMenuCloseRight, Label: closeFollowing, Icon: icon(closeIcon), Disabled: len(following) == 0})
	if reconnect || restart || disconnect {
		rows.separate()
	}
	if reconnect {
		rows.add(components.ContextMenuItem{ID: hitTabMenuReconnect, Label: messages.SessionReconnect, Icon: icon(iconRefresh)})
	}
	if restart {
		rows.add(components.ContextMenuItem{ID: hitTabMenuRestart, Label: messages.MenuReconnect, Icon: icon(iconRefresh)})
	}
	if disconnect {
		rows.add(components.ContextMenuItem{ID: hitTabMenuDisconnect, Label: messages.MenuDisconnect, Icon: icon(iconPlug)})
	}
	if _, temporary := state.temporaryWorkspaceLayouts[state.tabMenu.workspace]; temporary {
		rows.separate()
		rows.add(components.ContextMenuItem{ID: hitTabMenuSaveWorkspace, Label: messages.WorkspaceSaveMenu, Icon: icon(iconArchive)})
	}
	width, height := rows.size(canvas, state.contextMenuTheme())
	bounds := state.popupAt(canvas, viewport, state.tabMenu.at, width, height, 6)
	menu := components.BeginContextMenu(canvas, state.contextMenuTheme(), bounds, components.ContextMenuOptions{
		Layer: dialogLayer, Scope: dialogScope, DismissID: hitTabMenuDismiss, Viewport: viewport, Scroll: state.menuScrollFor(hitTabMenuDismiss),
		RowHeight: contextMenuRow,
	})
	state.menuPlaced(menu.Bounds())
	rows.paint(&menu, nil)
	menu.End()
}

// clickTabMenu runs a menu choice. A press anywhere else only closes the menu.
func (state *State) clickTabMenu(id gfx.ID) bool {
	if !state.tabMenu.open {
		return false
	}
	workspace := state.tabMenu.workspace
	state.tabMenu.open = false
	index, ok := state.workspaceTab(workspace)
	if !ok {
		return true
	}
	switch id {
	case hitTabMenuNewWindow:
		if frame, ok := state.windowFrameOnScreen(); ok && state.canTearOffTab() {
			state.tearOffTab(workspace, gfx.Point{X: frame.X + tearOffCascade, Y: frame.Y + tearOffCascade})
		}
	case hitTabMenuCopyAddress:
		if session, ok := state.tabMenuSessionFor(workspace); ok {
			state.copyConnectionText(connectionAddressAndPort(session.host))
		}
	case hitTabMenuCopyName:
		if session, ok := state.tabMenuSessionFor(workspace); ok {
			state.copyConnectionText(session.host.Label)
		}
	case hitTabMenuRename:
		state.openTabRename(workspace)
	case hitTabMenuLock:
		_ = state.deck.SetLocked(workspace, !state.deck.Locked(workspace))
		state.rebuildTabOptions()
		state.save()
	case hitTabMenuDuplicate:
		if session, ok := state.tabMenuSessionFor(workspace); ok {
			state.pendingWorkspace, state.pendingPane = 0, 0
			if session.isLocal() && state.localStart == localStartRemember {
				// A copy of a local tab is opened beside it, so it starts in
				// the same folder, the way a split does.
				state.pendingDirectory = session.localDirectory
			}
			state.newHostSession(session.host)
		}
	case hitTabMenuEditHost:
		if session, ok := state.tabMenuSessionFor(workspace); ok {
			if host, found := state.tabMenuHost(session); found {
				state.openEditHostID(host.ID)
			}
		}
	case hitTabMenuFit, hitTabMenuActual, hitTabMenuCtrlAltDel, hitTabMenuHideBars, hitTabMenuFullscreen, hitTabMenuLatency:
		session, ok := state.tabMenuSessionFor(workspace)
		if !ok || session.desk == nil {
			return true
		}
		state.activateWorkspace(workspace)
		switch id {
		case hitTabMenuFit:
			session.desk.fit = true
		case hitTabMenuActual:
			session.desk.fit = false
		case hitTabMenuCtrlAltDel:
			if !session.desk.ended {
				session.desk.tap([]desktop.Key{desktop.KeyControl, desktop.KeyAlt}, desktop.KeyEvent{Key: desktop.KeyDelete})
			}
		case hitTabMenuHideBars:
			state.setDesktopChromeless(session, !session.desk.chromeless)
		case hitTabMenuFullscreen:
			state.toggleDesktopFullscreen(session)
		case hitTabMenuLatency:
			state.terminalShowLatency = !state.terminalShowLatency
			state.syncLatencyToolbarEntry()
			state.save()
		}
	case hitTabMenuClose:
		state.closeWorkspace(workspace)
	case hitTabMenuCloseOthers:
		state.askCloseTabs(workspace, func(position int) bool { return position != index })
	case hitTabMenuCloseRight:
		following := state.workspaceTabsAfter(workspace)
		state.askCloseTabs(workspace, func(position int) bool {
			for _, tab := range following {
				if state.workspaceTabs[position].ID == tab.ID {
					return true
				}
			}
			return false
		})
	case hitTabMenuReconnect:
		if session, ok := state.tabMenuSessionFor(workspace); ok && state.canReconnect(session) {
			state.activateWorkspace(workspace)
			state.reconnect(session)
			state.save()
		}
	case hitTabMenuRestart:
		if session, ok := state.tabMenuSessionFor(workspace); ok && state.canReconnectLive(session) {
			state.activateWorkspace(workspace)
			state.askSessionLink(session, false)
		}
	case hitTabMenuDisconnect:
		if session, ok := state.tabMenuSessionFor(workspace); ok && state.canDisconnect(session) {
			state.activateWorkspace(workspace)
			state.askSessionLink(session, true)
		}
	case hitTabMenuSaveWorkspace:
		state.openWorkspaceSave(workspace)
	}
	return true
}

func (state *State) tabMenuSessionFor(workspace layoutcore.WorkspaceID) (*Session, bool) {
	state.tabMenu.workspace = workspace
	return state.tabMenuSession()
}

// closeTabs closes an already confirmed snapshot. Stable IDs keep a reorder or
// a newly opened tab from changing which tabs the confirmation applies to.
func (state *State) closeTabs(pending tabsClose) {
	if pending.count == 0 {
		return
	}
	if pending.keep != 0 {
		state.activateWorkspace(pending.keep)
	}
	for _, id := range pending.ids {
		state.closeWorkspaceNow(id)
	}
	if pending.keep != 0 && state.deck.ActiveWorkspace() != pending.keep {
		state.activateWorkspace(pending.keep)
	}
	state.save()
}
