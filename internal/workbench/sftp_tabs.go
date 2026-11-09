package workbench

import (
	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
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
		if manager := state.activeSFTPWindow(); manager != nil {
			return tree.MountID(sftpWindowMount(manager), sftpWindowPath[0])
		}
	}
	return state.activeTerminalFocusHit()
}

func (state *State) paintSFTPTab(canvas *gfx.Canvas, bounds gfx.Rect) {
	w := state.activeSFTPWindow()
	if w == nil || bounds.Width <= 0 || bounds.Height <= 0 {
		return
	}
	mount := sftpWindowMount(w)
	state.fileManagerView.BeginEmbedded(canvas, mount)
	w.paint(&state.fileManagerView, gfx.Size{Width: bounds.Width, Height: bounds.Height})
	if clickID, ok := state.fileManagerView.ClickTarget(); ok {
		canvas.AfterPaint(func() { w.click(clickID) })
	}
	state.fileManagerView.EndFrame()
	state.fileManagerRect = bounds
	canvas.Clip(bounds)
	canvas.Embed(&state.fileManagerView, gfx.Point{X: bounds.X, Y: bounds.Y}, mount)
	canvas.Unclip()
	adoptEmbeddedFocus(canvas, &state.fileManagerView, mount)
}

func (state *State) sftpTabLocalID(id gfx.ID) (gfx.ID, *sftpWindow, bool) {
	w := state.activeSFTPWindow()
	if w == nil || id.IsZero() {
		return gfx.ID{}, nil, false
	}
	local, ok := tree.Local(sftpWindowMount(w), id)
	return local, w, ok
}

// SFTPTabInput resolves text fields painted by the file manager embedded in a
// main-window tab. The child view names fields in its own tree; the host receives
// the mounted identities.
func (state *State) SFTPTabInput(context app.TextInputContext) *app.TextInput {
	_, w, ok := state.sftpTabLocalID(context.ID)
	if !ok || w == nil {
		return nil
	}
	context.ID, _ = tree.Local(sftpWindowMount(w), context.ID)
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
	local, _, mounted := state.sftpTabLocalID(event.Target)
	if !mounted && !pointInRect(event.X, event.Y, state.fileManagerRect) {
		return gfx.EventResult{}, false
	}
	mount := sftpWindowMount(w)
	event.Target = local
	event.X -= state.fileManagerRect.X
	event.Y -= state.fileManagerRect.Y
	event.Viewport = gfx.Size{Width: state.fileManagerRect.Width, Height: state.fileManagerRect.Height}
	result := w.pointer(event)
	result.Focus = tree.MountID(mount, result.Focus)
	return result, true
}

func (state *State) sftpTabKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	w := state.activeSFTPWindow()
	if w == nil {
		return gfx.EventResult{}, false
	}
	local, _, mounted := state.sftpTabLocalID(event.Target)
	if !mounted {
		return gfx.EventResult{}, false
	}
	mount := sftpWindowMount(w)
	event.Target = local
	result := w.key(event)
	result.Focus = tree.MountID(mount, result.Focus)
	return result, true
}

func (state *State) sftpTabShortcut(event gfx.KeyEvent) (gfx.EventResult, bool) {
	w := state.activeSFTPWindow()
	if w == nil {
		return gfx.EventResult{}, false
	}
	local, _, mounted := state.sftpTabLocalID(event.Target)
	if !mounted {
		return gfx.EventResult{}, false
	}
	mount := sftpWindowMount(w)
	event.Target = local
	result := w.shortcut(event)
	result.Focus = tree.MountID(mount, result.Focus)
	return result, result.Handled
}

func (state *State) sftpTabScroll(event gfx.ScrollEvent) (gfx.EventResult, bool) {
	w := state.activeSFTPWindow()
	if w == nil || !pointInRect(event.X, event.Y, state.fileManagerRect) {
		return gfx.EventResult{}, false
	}
	mount := sftpWindowMount(w)
	event.X -= state.fileManagerRect.X
	event.Y -= state.fileManagerRect.Y
	result := w.scroll(event)
	result.Focus = tree.MountID(mount, result.Focus)
	return result, true
}

func (state *State) sftpTabFileDrop(event gfx.FileDropEvent) (gfx.EventResult, bool) {
	w := state.activeSFTPWindow()
	if w == nil || event.Phase != gfx.FileDropLeave && !pointInRect(event.X, event.Y, state.fileManagerRect) {
		return gfx.EventResult{}, false
	}
	mount := sftpWindowMount(w)
	event.X -= state.fileManagerRect.X
	event.Y -= state.fileManagerRect.Y
	result := w.fileDrop(event)
	result.Focus = tree.MountID(mount, result.Focus)
	return result, true
}
