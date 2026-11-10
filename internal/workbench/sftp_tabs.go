package workbench

import (
	"fmt"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// sftpManagersAvailable reports whether the chosen host can accept a manager.
// Tabs need no native content-window factory; windows still do.
func (state *State) sftpManagersAvailable() bool {
	return state.sftpOpenMode == sftpOpenTab || state.options.OpenContentWindow != nil
}

func (state *State) openSFTPTab(w *sftpWindow) {
	if w == nil || w.closed {
		return
	}
	if !containsSFTPTab(state.sftpTabs, w) {
		state.sftpTabs = append(state.sftpTabs, w)
	}
	state.activeSFTPTab = w.session.ID
	state.activeSection = sectionSFTP
	state.rebuildFileTabOptions()
	state.options.Invalidate()
	w.startBrowser()
}

func (w *sftpWindow) openTab() {
	if w == nil || w.closed {
		return
	}
	w.owner.openSFTPTab(w)
}

func containsSFTPTab(tabs []*sftpWindow, target *sftpWindow) bool {
	for _, tab := range tabs {
		if tab == target {
			return true
		}
	}
	return false
}

func (state *State) sftpTabIndex(id uint32) int {
	for index, tab := range state.sftpTabs {
		if tab != nil && !tab.closed && tab.session.ID == id {
			return index
		}
	}
	return -1
}

func (state *State) activeSFTPWindow() *sftpWindow {
	if state.activeSection != sectionSFTP || state.activeSFTPTab == 0 {
		return nil
	}
	if index := state.sftpTabIndex(state.activeSFTPTab); index >= 0 {
		return state.sftpTabs[index]
	}
	return nil
}

func (state *State) activateSFTPTab(id uint32) bool {
	index := state.sftpTabIndex(id)
	if index < 0 {
		return false
	}
	state.activeSFTPTab = id
	state.activeSection = sectionSFTP
	state.options.Invalidate()
	return true
}

func (state *State) activateSFTPTabAt(index int) bool {
	if index < 0 || index >= len(state.sftpTabs) {
		return false
	}
	tab := state.sftpTabs[index]
	if tab == nil || tab.closed {
		return false
	}
	return state.activateSFTPTab(tab.session.ID)
}

func (state *State) cycleWindowTabs(delta int) bool {
	state.workspaceTabs = state.deck.Workspaces(state.workspaceTabs[:0])
	count := len(state.workspaceTabs) + len(state.sftpTabs)
	if count < 2 {
		return false
	}
	current := -1
	switch state.activeSection {
	case sectionTerminal:
		for index, tab := range state.workspaceTabs {
			if tab.ID == state.deck.ActiveWorkspace() {
				current = index
				break
			}
		}
	case sectionSFTP:
		if index := state.sftpTabIndex(state.activeSFTPTab); index >= 0 {
			current = len(state.workspaceTabs) + index
		}
	default:
		for index, tab := range state.workspaceTabs {
			if tab.ID == state.deck.ActiveWorkspace() {
				current = index
				break
			}
		}
	}
	if current < 0 {
		current = 0
	}
	next := (current + delta%count + count) % count
	if next < len(state.workspaceTabs) {
		return state.activateTab(next)
	}
	return state.activateSFTPTabAt(next - len(state.workspaceTabs))
}

// topTabCount is how many tabs the number shortcuts can reach: the workspace
// tabs on the tab bar, in the order the bar shows them, then the file tabs.
func (state *State) topTabCount() int {
	state.workspaceTabs = state.deck.Workspaces(state.workspaceTabs[:0])
	return len(state.visualWorkspaceTabs()) + len(state.sftpTabs)
}

func (state *State) activateTopTab(index int) bool {
	state.workspaceTabs = state.deck.Workspaces(state.workspaceTabs[:0])
	tabs := state.visualWorkspaceTabs()
	if index < len(tabs) {
		return state.activateTab(index)
	}
	return state.activateSFTPTabAt(index - len(tabs))
}

func (state *State) requestCloseSFTPTab(w *sftpWindow) {
	if w == nil || w.closed || state.sftpTabIndex(w.session.ID) < 0 {
		return
	}
	state.activateSFTPTab(w.session.ID)
	w.requestClose(func() { state.removeSFTPTab(w) })
}

func (state *State) removeSFTPTab(w *sftpWindow) {
	index := -1
	for at, tab := range state.sftpTabs {
		if tab == w {
			index = at
			break
		}
	}
	if index < 0 {
		return
	}
	wasActive := state.activeSection == sectionSFTP && state.activeSFTPTab == w.session.ID
	copy(state.sftpTabs[index:], state.sftpTabs[index+1:])
	state.sftpTabs[len(state.sftpTabs)-1] = nil
	state.sftpTabs = state.sftpTabs[:len(state.sftpTabs)-1]
	if wasActive {
		switch {
		case len(state.sftpTabs) == 0:
			state.activeSFTPTab = 0
			if state.deck.ActiveWorkspace() != 0 {
				state.activeSection = sectionTerminal
			} else {
				state.activeSection = sectionHosts
			}
		case index < len(state.sftpTabs):
			state.activeSFTPTab = state.sftpTabs[index].session.ID
		default:
			state.activeSFTPTab = state.sftpTabs[len(state.sftpTabs)-1].session.ID
		}
	}
	state.rebuildFileTabOptions()
	w.onClosed()
	state.options.Invalidate()
}

func (state *State) closeSFTPTabForSource(sessionID uint32) {
	index := state.sftpTabIndex(sessionID)
	if index < 0 {
		return
	}
	state.removeSFTPTab(state.sftpTabs[index])
}

func (state *State) syncSFTPTabs() {
	kept := state.sftpTabs[:0]
	for _, tab := range state.sftpTabs {
		if tab != nil && !tab.closed {
			kept = append(kept, tab)
		}
	}
	clear(state.sftpTabs[len(kept):])
	state.sftpTabs = kept
	if len(state.sftpTabs) == 0 {
		state.activeSFTPTab = 0
		if state.activeSection == sectionSFTP {
			if state.deck.ActiveWorkspace() != 0 {
				state.activeSection = sectionTerminal
			} else {
				state.activeSection = sectionHosts
			}
		}
	} else if state.sftpTabIndex(state.activeSFTPTab) < 0 {
		state.activeSFTPTab = state.sftpTabs[0].session.ID
	}
	state.rebuildFileTabOptions()
}

func (state *State) rebuildFileTabOptions() {
	if cap(state.fileTabOptions) < len(state.sftpTabs) {
		state.fileTabOptions = make([]components.TabOption, len(state.sftpTabs))
	} else {
		state.fileTabOptions = state.fileTabOptions[:len(state.sftpTabs)]
	}
	for index, tab := range state.sftpTabs {
		state.fileTabOptions[index] = components.TabOption{
			ID:    sftpFileTabID(tab.session.ID),
			Label: state.sftpFileTabTitle(tab),
		}
	}
}

func (state *State) sftpFileTabTitle(tab *sftpWindow) string {
	if tab == nil {
		return state.messages.SFTPWindow.Title
	}
	title := tab.session.Title
	if title == "" {
		title = tab.session.Address
	}
	if !tab.tabTitleReady || tab.tabTitleSource != title || tab.tabTitleLanguage != state.language {
		tab.tabTitle = state.messages.SFTPWindow.Title + " · " + title
		tab.tabTitleSource, tab.tabTitleLanguage, tab.tabTitleReady = title, state.language, true
	}
	return tab.tabTitle
}

func (state *State) activeTopTabFocusHit() gfx.ID {
	if state.activeSection == sectionSFTP {
		if state.activeSFTPWindow() != nil {
			return sftpWindowPath[0]
		}
	}
	return state.activeTerminalFocusHit()
}

// A file-manager tab is a component of the workbench page: one Part-named
// container at the rect the window gives it, with the two panes, their Lists and
// the rest of the window's view declared inside. The page routes pointer, key and
// text; the window's own hand-painted controls keep the identities they always
// had, and no identity is mounted or translated.
const (
	fileTabPart       = "sftp-file-tab"
	fileTabLocalPart  = "local"
	fileTabRemotePart = "remote"
)

// fileTabScope is where the shell page declares the file-manager tab.
func fileTabScope() gfx.Scope {
	return tree.Sub(tree.Feature(shellPageRoot), tree.Name(fileTabPart))
}

// fileTabOwns reports whether id is an element the workbench page declared
// inside the file-manager tab: its Lists, rows, or a control of the tab's view.
func (state *State) fileTabOwns(id gfx.ID) bool {
	return !id.IsZero() && tree.Within(id.Scope(), fileTabScope())
}

// fileTabHit reports whether id belongs to the file-manager tab as the window
// sees it: a page element, or one of the hand-painted controls that keep the
// sftp window's names.
func (state *State) fileTabHit(id gfx.ID) bool {
	if id.IsZero() {
		return false
	}
	return state.fileTabOwns(id) || tree.Within(id.Scope(), sftpWindowHits)
}

// sftpTabPaneRects lays the tab out as the window's view always did: padding
// around the body, a gap between the body, the transfer queue and the status
// line. The body takes the room the fixed children leave.
func sftpTabPaneRects(bounds gfx.Rect, queue bool) (body, queueRect, status gfx.Rect) {
	size := gfx.Size{Width: bounds.Width, Height: bounds.Height}
	inner := gfx.Rect{X: bounds.X + sftpWindowPad, Y: bounds.Y + sftpWindowPad,
		Width: max(size.Width-2*sftpWindowPad, 0), Height: max(size.Height-2*sftpWindowPad, 0)}
	children := 2
	queueHeight := float32(0)
	if queue {
		children = 3
		queueHeight = sftpQueueHeight(size)
	}
	body = gfx.Rect{X: inner.X, Y: inner.Y, Width: inner.Width,
		Height: max(inner.Height-sftpWindowGap*float32(children-1)-queueHeight-sftpWindowStatusHeight, 0)}
	if queue {
		queueRect = gfx.Rect{X: inner.X, Y: body.Y + body.Height + sftpWindowGap, Width: inner.Width, Height: queueHeight}
		status = gfx.Rect{X: inner.X, Y: queueRect.Y + queueRect.Height + sftpWindowGap, Width: inner.Width, Height: sftpWindowStatusHeight}
	} else {
		status = gfx.Rect{X: inner.X, Y: body.Y + body.Height + sftpWindowGap, Width: inner.Width, Height: sftpWindowStatusHeight}
	}
	return body, queueRect, status
}

// declareSFTPTab declares the active file-manager tab inside the workbench page.
// The tab's view is the window's own: the same panes, lists and status line, now
// named by the page and laid out at the tab's rectangle.
func (state *State) declareSFTPTab(c ui.C) {
	w := state.activeSFTPWindow()
	if w == nil {
		return
	}
	bounds := state.fileManagerPixelBounds(state.viewport)
	if bounds.Width <= 0 || bounds.Height <= 0 {
		return
	}
	state.fileManagerRect = bounds
	w.ui.size = gfx.Size{Width: bounds.Width, Height: bounds.Height}
	w.ui.bounds = bounds
	w.ui.tabBody, w.ui.tabQueue = gfx.Rect{}, gfx.Rect{}
	w.ui.tabPanes = [2]sftpWindowScratch{}
	c.Part(fileTabPart)
	ui.Column(c).Absolute().Left(bounds.X).Top(bounds.Y).Width(bounds.Width).Height(bounds.Height).
		Clip().Background(state.Theme().Background).Children(func() {
		if w.view.worker == nil {
			w.ui.connect = sftpWindowConnectScratch{}
			w.declareConnecting(c)
			return
		}
		state.declareSFTPTabContent(c, w, bounds)
	})
	if w.bookmark.open && w.view.worker != nil {
		w.declareBookmarkDialog(c, bounds)
	}
	if w.view.worker != nil && w.view.dialog != sftpDialogNone {
		state.declareSFTPDialog(c, bounds, w.view)
	}
	state.declareSFTPFileProperties(c, bounds, &w.fileProperties)
}

// declareSFTPTabContent declares the window's body, transfer queue and status
// line in the tab's container.
func (state *State) declareSFTPTabContent(c ui.C, w *sftpWindow, bounds gfx.Rect) {
	queue := w.queueCount() > 0
	body, queueRect, status := sftpTabPaneRects(bounds, queue)
	state.declareSFTPTabPanes(c, w, bounds, body)
	if queue {
		w.ui.tabQueue = queueRect
		w.declareQueue(c, queueRect, bounds)
	}
	state.declareSFTPTabStatus(c, w, bounds, status)
}

// declareSFTPTabPanes declares the two panes inside the split, in the order the
// window shows them. Each pane is a Part of its own, so its List's generated
// identities follow the logical side even when the panes swap places.
func (state *State) declareSFTPTabPanes(c ui.C, w *sftpWindow, bounds, body gfx.Rect) {
	w.ui.split = components.BeginResizable(nil, state.Theme(), hitSFTPWindowSplit, body, w.ui.fraction).LimitsPx(220, 220)
	panels := w.ui.split.Paint("")
	firstLocal := state.sftpWindowOrder == sftpLocalLeft
	for _, pane := range [2]struct {
		rect  gfx.Rect
		local bool
	}{{panels.First, firstLocal}, {panels.Second, !firstLocal}} {
		state.declareSFTPTabPane(c, w, bounds, pane.rect, pane.local)
	}
}

func (state *State) declareSFTPTabPane(c ui.C, w *sftpWindow, bounds, rect gfx.Rect, local bool) {
	if rect.Width <= 0 || rect.Height <= 0 {
		return
	}
	part := fileTabRemotePart
	if local {
		part = fileTabLocalPart
	}
	c.Part(part)
	pane := ui.Column(c).Absolute().
		Left(rect.X - bounds.X).Top(rect.Y - bounds.Y).
		Width(rect.Width).Height(rect.Height)
	s := &w.ui.tabPanes[sideIndex(local)]
	*s = sftpWindowScratch{local: local, bounds: rect}
	pane.Children(func() { w.declarePane(c, s) })
}

// declareSFTPTabStatus declares the line under the panes, as the window's view
// did: what a browse-synchronized pair shows, else the window's status.
func (state *State) declareSFTPTabStatus(c ui.C, w *sftpWindow, bounds, rect gfx.Rect) {
	ui.Column(c).Absolute().
		Left(rect.X - bounds.X).Top(rect.Y - bounds.Y).
		Width(rect.Width).Height(rect.Height).Children(func() {
		w.declareStatusLine(c)
	})
}

// paintSFTPTab paints what the workbench page's declaration leaves to the
// window: the split handle, the panes' hand-painted controls and scroll bars, the
// transfer queue, and the popups and messages the window owns. The tab's content
// itself is the page's, so it is painted already.
func (state *State) paintSFTPTab(canvas *gfx.Canvas, bounds gfx.Rect) {
	w := state.activeSFTPWindow()
	if w == nil || bounds.Width <= 0 || bounds.Height <= 0 {
		return
	}
	state.fileManagerRect = bounds
	canvas.Clip(bounds)
	defer canvas.Unclip()
	if w.view.worker == nil {
		w.paintConnectingDetails(canvas)
		return
	}
	w.paintTabDetails(canvas, bounds)
}

// paintTabDetails paints the window's own parts of the tab after the page.
func (w *sftpWindow) paintTabDetails(canvas *gfx.Canvas, bounds gfx.Rect) {
	state, m := w.owner, w.owner.messages
	w.ui.split = components.BeginResizable(canvas, state.Theme(), hitSFTPWindowSplit, w.ui.tabBody, w.ui.fraction).LimitsPx(220, 220)
	w.ui.split.Paint(m.SFTPWindow.Title)
	firstLocal := state.sftpWindowOrder == sftpLocalLeft
	if s := &w.ui.tabPanes[sideIndex(firstLocal)]; s.list.Valid() {
		w.paintPaneDetails(canvas, s, firstLocal)
	}
	if s := &w.ui.tabPanes[sideIndex(!firstLocal)]; s.list.Valid() {
		w.paintPaneDetails(canvas, s, !firstLocal)
	}
	if w.ui.tabQueue.Width > 0 {
		w.paintQueueDetails(canvas, w.ui.tabQueue)
	} else {
		w.ui.queueBounds = gfx.Rect{}
		w.ui.queueOffset = 0
	}
	if w.view.bookmarksOpen {
		w.paintDirectoryPlaces(canvas)
	}
	if w.closeConfirm {
		components.BeginAlertDialog(canvas, state.Theme(), bounds, components.AlertDialogOptions{Layer: dialogLayer + 1, Scope: dialogScope, ActionID: hitSFTPWindowCloseConfirm, CancelID: hitSFTPWindowCloseCancel, InitialFocusID: hitSFTPWindowCloseCancel, ActionVariant: components.ButtonDestructive}).Paint(m.SFTPWindow.CloseTitle, w.closeDescription(), m.SFTPWindow.CloseTransfers, m.Cancel)
	}
	if w.externalConflict != 0 && !w.closeConfirm && w.view.dialog == sftpDialogNone {
		if edit := state.editByID(w.externalConflict); edit != nil {
			tools := m.Tools
			components.BeginAlertDialog(canvas, state.Theme(), bounds, components.AlertDialogOptions{Layer: dialogLayer + 1, Scope: dialogScope, ActionID: hitSFTPWindowExternalReplace, CancelID: hitSFTPWindowExternalKeep, InitialFocusID: hitSFTPWindowExternalKeep, ActionVariant: components.ButtonDestructive}).Paint(tools.ExtEditConflictTitle, fmt.Sprintf(tools.ExtEditConflictDescriptionFmt, edit.name), tools.ExtEditReplace, m.Cancel)
		}
	}
	if w.ui.menuOpen {
		w.paintMenu(canvas)
	}
	if side, ok := w.headerMenu(); ok {
		// The tab's view is part of the page, so the page paints this menu over
		// the whole window; its anchor is already a window rectangle.
		state.requestSFTPWindowHeaderMenu(w, side, w.ui.headerAnchor[side])
	}
	if w.ui.profileQuickOpen && !w.bookmark.open {
		w.paintProfileQuickMenu(canvas)
	}
	if !w.bookmark.open && !w.ui.profileQuickOpen {
		w.paintProfileQuickPreview(canvas, bounds)
	}
	if w.drag.active {
		w.paintDrag(canvas)
	}
	w.notices.paint(canvas, state.Theme(), bounds)
	if !w.ui.profileHoverOpen {
		w.ui.tooltip.Paint(canvas, state.Theme(), bounds, components.TooltipOptions{Layer: tooltipLayer, SideOffset: tooltipGap}, components.HoverTooltipDelay)
	}
}

// SFTPTabInput resolves text fields painted by the file manager hosted in a
// main-window tab. Its hand-painted fields keep the window's own identities.
func (state *State) SFTPTabInput(context app.TextInputContext) *app.TextInput {
	w := state.activeSFTPWindow()
	if w == nil || context.ID.IsZero() {
		return nil
	}
	return w.textInput(context)
}

func (state *State) sftpTabPointer(event gfx.PointerEvent) (gfx.EventResult, bool) {
	w := state.activeSFTPWindow()
	if w == nil {
		return gfx.EventResult{}, false
	}
	// A narrow assistant is a drawer over the file manager. A field on the
	// drawer may decline the press so Viva's normal text-input handling can
	// place the caret; it still must not fall through to the page behind it.
	if state.aiPanelVisible() && state.ai.panel.rect.Width > 0 &&
		pointInRect(event.X, event.Y, state.ai.panel.rect) {
		return gfx.EventResult{}, false
	}
	owns := state.fileTabOwns(event.Target)
	if !owns && !state.fileTabHit(event.Target) && !pointInRect(event.X, event.Y, state.fileManagerRect) {
		return gfx.EventResult{}, false
	}
	// The window's own behavior: rows and the drag they arm, scroll bars, the
	// split handle, popups and dialogs. Coordinates are already the window's.
	return w.pointer(event), true
}

func (state *State) sftpTabKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	w := state.activeSFTPWindow()
	if w == nil || !state.fileTabHit(event.Target) {
		return gfx.EventResult{}, false
	}
	// A key over the file tab's hand-painted chrome is the file window's; the
	// page's Lists have already had their own keys.
	return w.key(event), true
}

func (state *State) sftpTabShortcut(event gfx.KeyEvent) (gfx.EventResult, bool) {
	w := state.activeSFTPWindow()
	if w == nil || !state.fileTabHit(event.Target) {
		return gfx.EventResult{}, false
	}
	result := w.shortcut(event)
	return result, result.Handled
}

func (state *State) sftpTabScroll(event gfx.ScrollEvent) (gfx.EventResult, bool) {
	w := state.activeSFTPWindow()
	if w == nil || !pointInRect(event.X, event.Y, state.fileManagerRect) {
		return gfx.EventResult{}, false
	}
	// The List is the page's now, but a wheel notch moves whole rows and the
	// window owns that; it hands the resulting offset to the List.
	return w.scroll(event), true
}

func (state *State) sftpTabFileDrop(event gfx.FileDropEvent) (gfx.EventResult, bool) {
	w := state.activeSFTPWindow()
	if w == nil || event.Phase != gfx.FileDropLeave && !pointInRect(event.X, event.Y, state.fileManagerRect) {
		return gfx.EventResult{}, false
	}
	return w.fileDrop(event), true
}
