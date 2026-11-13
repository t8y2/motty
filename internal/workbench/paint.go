package workbench

import (
	"strings"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"motty/internal/sshconn"
	"motty/internal/workbench/icons"
	layoutcore "motty/internal/workspace"
)

const sectionCardMargin = float32(7)

// workspaceBorder is the hairline the workspace slot draws around the page a
// section paints, and the inset that keeps that page's content inside it.
const workspaceBorder = float32(1)

const (
	iconHosts uint8 = iota
	iconDesktop
	iconHelp
	iconKey
	iconGlobe
	iconForward
	iconKnown
	iconSettings
	iconSearch
	iconTerminal
	iconSerial
	iconPlus
	iconSun
	iconMoon
	iconClose
	iconHome
	iconCheck
	iconPause
	iconFile
	iconLink
	iconUpload
	iconDownload
	iconFolderUp
	iconFolderPlus
	iconPencil
	iconTrash
	iconLock
	iconRefresh
	iconArrowUp
	iconFolderOpen
	iconActivity
	iconRecord
	iconRecordStop
	iconPlay
	iconStop
	iconFilm
	iconFileDown
	iconColumns2
	iconRows2
	iconGrid2x2
	iconBot
	iconHistory
	iconCopy
	iconChevronRight
	iconChevronDown
	iconSliders
	iconAlert
	iconCircleCheck
	iconRadio
	iconKeyboard
	iconStar
	iconStarSolid
	iconArchive
	iconHardDrive
	iconExternal
	iconPlug
	iconPaste
	iconSelectAll
	iconEraser
	iconEye
	iconEyeOff
	iconEllipsis
	iconWrench
	iconCloseOthers
	iconCloseRight
	iconSparkles
	iconTransfer
	iconBug
	iconInfo
	iconQQ
	iconWeChat
	iconDiscord
	iconGitHub
	iconChevronLeft
	iconNote
	iconNotebook
	iconSFTPTransfer
	iconSFTPBrowseSync
	iconSFTPProfiles
	// iconFileClock is the session-log page's glyph; iconClock is the clock the
	// time-range filter shows beside its value. Both are Lucide sources compiled
	// by viva icons.
	iconFileClock
	iconClock
	iconPaperclip
	iconImage
	iconSave
	iconProcess
	iconDirectoryTree
	iconFollowFolder
	// iconArrowUpDown is the command bar's order button.
	iconArrowUpDown
)

// terminalPage reports whether the window shows the panes of a workspace rather
// than a section card under the navigation rail. A terminal section without a
// workspace still shows the section, so it is not a terminal page.
func (state *State) terminalPage() bool {
	return state.activeSection == sectionTerminal && state.deck.ActiveWorkspace() != 0
}

// paintsHostsPage reports whether Paint's section switch falls through to the
// hosts page: no other section card, no terminal pane and no file-manager tab
// takes the workspace.
func (state *State) paintsHostsPage() bool {
	if state.terminalPage() || state.activeSFTPWindow() != nil {
		return false
	}
	switch state.activeSection {
	case sectionWorkspaces, sectionDashboard, sectionKeys, sectionForwards, sectionProxies,
		sectionSettings, sectionKnownHosts, sectionNotes, sectionSnippets, sectionSessionLogs, sectionDebug:
		return false
	}
	return true
}

// Paint renders the application from state using stable hit identities.
func (state *State) Paint(canvas *gfx.Canvas, size gfx.Size) {
	defer state.devEndPaint(canvas, size)
	defer state.paintNoteExit(canvas, gfx.Rect{Width: size.Width, Height: size.Height})
	canvas.SetTree(&tree)
	state.autosaveNotes(canvas)
	state.syncNoteWindows()
	state.workspaceCardRegions = state.workspaceCardRegions[:0]
	state.pixelScale = canvas.PixelScale()
	// Motty keeps one central click handler. The event pass of a click names its
	// target here; the handler runs once this paint has returned, outside it,
	// because it may open a native dialog or block. The repaint that follows
	// shows the result. Asking also makes the runtime deliver clicks at all.
	if id, ok := canvas.ClickTarget(); ok {
		canvas.AfterPaint(func() { state.Click(id) })
	}
	state.refreshOutputAlerts(canvas)
	state.syncVisible()
	state.syncSFTPTabs()
	state.reveal.beginPaint()
	state.toolbarDock.count = 0
	state.toolbarDock.zones = [7]gfx.Rect{}
	state.toolbarDock.bottomViewports = [2]gfx.Rect{}
	state.toolbarDock.sideViewports = [4]gfx.Rect{}
	state.contextRect = gfx.Rect{}
	state.hostsBounds, state.chrome.hostList = gfx.Rect{}, gfx.Rect{}
	state.verticalTabs.bounds, state.verticalTabs.slot = gfx.Rect{}, ui.Box{}
	// slotRect is not cleared: it is the navigation rail's list rectangle from
	// the previous paint, and the next declaration of that list needs it for the
	// page-coordinate geometry an overlay would otherwise have provided.
	// A dialog or menu that opened during an earlier event owns the pointer; the
	// drag has no release left to finish, so let it go as Escape does. The host
	// grid's page drag and the drag overlays below menus are cancelled the same
	// way.
	state.reconcileDragHosts()
	state.devBeginPaint()
	state.ai.cfg.mcpSelectBounds = gfx.Rect{}
	state.ai.cfg.agentSelectBounds = gfx.Rect{}
	state.localShellMenu.bounds = gfx.Rect{}
	state.editorMenu.bounds = gfx.Rect{}
	state.fileManagerRect = gfx.Rect{}
	if state.activeSection != sectionSettings || state.settingsCategory != settingsFiles || state.settingsSearching() {
		state.editorMenu.open = false
	}
	if state.activeSection != sectionSettings || state.settingsCategory != settingsTerminal || state.settingsSearching() {
		state.localShellMenu.open = false
	}
	if state.tabInlineActive() {
		if _, ok := state.workspaceTab(state.tabInline.workspace); !ok {
			// The workspace left while its name was being typed; the edit has
			// nothing left to name.
			state.tabInline = tabInline{}
		}
	}
	state.ai.panel.targetBounds = gfx.Rect{}
	if !state.aiSettingsVisible() || state.settingsCategory != settingsMCP || state.settingsSearching() {
		state.ai.cfg.mcpSelectOpen = false
		state.ai.cfg.agentSelectOpen = false
	}
	state.aiPanelBeginPaint()
	if state.noteSidebarFocusBack && state.activeSection == sectionTerminal {
		state.noteSidebarFocusBack = false
		canvas.RequestFocus(state.activeTerminalFocusHit())
	}
	// A panel closed from inside itself returns the keyboard to the terminal it
	// covered. The request is made once: a later click decides focus again.
	if state.ai.panel.focusBack && !state.aiPanelVisible() {
		state.ai.panel.focusBack = false
		canvas.RequestFocus(state.activeTerminalFocusHit())
	}
	// A running dashboard keeps sampling on every page, but only the page it is
	// on needs the repaint each sample triggers.
	state.dashboard.visible.Store(state.activeSection == sectionDashboard)
	// The terminal layout follows the window and the font; remember what it
	// was painted with so a resize can work out the rows and columns.
	state.viewport = size
	state.measureTerminalCell(canvas)
	if session := state.fullscreenDesktop(); session != nil {
		state.beginMenus()
		bounds := gfx.Rect{Width: size.Width, Height: size.Height}
		state.windowPage().Draw(canvas, bounds, state.declareShell)
		state.paintDesktopDetails(canvas, session, bounds)
		state.endMenus()
		state.paintTransferProgressCard(canvas)
		state.tooltip.Paint(canvas, state.Theme(), state.windowRect(), components.TooltipOptions{Layer: tooltipLayer, SideOffset: tooltipGap}, components.HoverTooltipDelay)
		state.fileNotices.paint(canvas, state.Theme(), state.windowRect())
		return
	}
	// The window is a bar over a body. The terminals show the picture through their
	// own ground, so with one set only the bar is shell-colored; the shell behind
	// them must not cover it.
	picture := state.paintBackground(canvas, size)
	terminal := state.terminalPage()
	fileManager := state.activeSFTPWindow() != nil
	fullPage := terminal || fileManager
	chrome := &state.chrome
	geometry := state.windowBarGeometry(canvas, size.Width)
	activeIndex := -1
	if state.activeSection == sectionTerminal {
		for index, tab := range state.workspaceTabs {
			if tab.ID == state.deck.ActiveWorkspace() {
				activeIndex = index
				break
			}
		}
	} else if fileManager {
		if index := state.sftpTabIndex(state.activeSFTPTab); index >= 0 {
			activeIndex = len(state.workspaceTabs) + index
		}
	}
	scroll := float32(0)
	if state.tabOrientation == tabsHorizontal {
		topTabCount := len(state.workspaceTabs) + len(state.sftpTabs)
		scroll = state.tabStrip.layoutIndex(geometry.area, float32(topTabCount)*(geometry.tabWidth+barGap), topTabCount, activeIndex, geometry.tabWidth+barGap, geometry.tabWidth)
	} else if len(state.sftpTabs) > 0 {
		scroll = state.fileTabStrip.layoutIndex(geometry.area, float32(len(state.sftpTabs))*(geometry.tabWidth+barGap), len(state.sftpTabs), activeIndex-len(state.workspaceTabs), geometry.tabWidth+barGap, geometry.tabWidth)
	} else {
		state.fileTabStrip.area = gfx.Rect{}
		state.fileTabStrip.max = 0
		state.fileTabStrip.offset = 0
		if !terminal {
			state.tabStrip.area = gfx.Rect{}
		}
	}
	// The section and assistant are sibling cards, separated by the same margin
	// the cards keep from the window's right and bottom edges.
	cardRight := sectionCardMargin
	if reserve := state.aiSectionReserve(size); reserve > 0 {
		cardRight = reserve
	}
	cardRight += state.toolbarDockSideReserveFor(false, false)
	chrome.workspaceSlot = ui.Box{}
	chrome.shellPicture = picture
	chrome.shellHeight = size.Height
	chrome.shellGeometry = geometry
	chrome.shellScroll = scroll
	chrome.shellFullPage = fullPage
	chrome.shellCardRight = cardRight
	// The files panel is declared in the page and asks for its More menu as it
	// paints, so the menu lifecycle starts before the page. The embedded file
	// header also paints a popup: starting before workspace content keeps its
	// scroll/focus state.
	state.beginMenus()
	state.beginHostsPaint()
	state.windowPage().Draw(canvas, gfx.Rect{Width: size.Width, Height: size.Height}, state.declareShell)
	if state.verticalTabs.slot != (ui.Box{}) {
		state.verticalTabs.slotRect = state.verticalTabs.slot.Rect()
	}
	workspaceSlot := chrome.workspaceSlot
	state.paintWindowBarParts(canvas, geometry.area)
	state.paintWindowBarScrollbar(canvas)
	if !fullPage {
		state.paintLockup(canvas, chrome.lockup.Rect())
	}

	if terminal {
		state.paintWorkspacePanes(canvas, size)
		state.paintNotesPanel(canvas, size)
		state.paintIndependentMonitor(canvas, size)
		state.paintWorkspaceSideRail(canvas, size)
		state.paintWorkspacePaneTools(canvas)
		// Assistant popovers may intentionally extend to the window edge; paint
		// the rail first so they can remain above it and nothing is clipped.
		state.paintAIPanel(canvas, size)
		// The bottom command drawer sits beneath the panes, in the space the
		// panes gave up for it.
		state.paintCommandsDrawer(canvas, size)
	} else if fileManager {
		state.paintSFTPTab(canvas, state.fileManagerPixelBounds(size))
		state.paintAIPanel(canvas, size)
	} else {
		workspace := workspaceSlot.Rect().Inset(gfx.All(workspaceBorder))
		canvas.Clip(workspace)
		if !state.devPaintPage(canvas, workspace) {
			switch state.activeSection {
			case sectionDashboard:
				state.paintDashboard(canvas, workspace)
			case sectionSettings:
				state.paintSettingsDetails(canvas)
			case sectionNotes:
				state.paintNotesWorkspace(canvas, workspace)
			default:
				// The other catalog sections are painted by their own
				// declarations in the page; only the hosts page is left for the
				// window to paint here.
				if state.paintsHostsPage() {
					state.paintHostsWorkspace(canvas, workspace)
				}
			}
		}
		canvas.Unclip()
		state.paintAIPanel(canvas, size)
	}
	if !terminal {
		state.paintToolbarDockSide(canvas, size)
	}
	state.paintToolbarDockBottom(canvas, size)
	viewport := state.windowRect()
	state.paintAITargetSelect(canvas)
	state.paintMCPClientSelect(canvas)
	state.paintLocalShellSelect(canvas)
	state.paintFindPreferences(canvas)
	state.paintEditorSelect(canvas)
	state.paintStartupHostSelect(canvas)
	state.paintMonitorIntervalMenu(canvas)
	state.paintDashboardMenu(canvas)
	state.paintSessionLogFilters(canvas)
	switch state.modal {
	case modalDashboardAdd:
		state.paintDashboardAddMenu(canvas)
	case modalDashboard:
		state.paintDashboardDialog(canvas, viewport)
	case modalSessionLink:
		state.paintSessionLinkDialog(canvas, viewport)
	case modalDeleteRule:
		state.paintDeleteRuleDialog(canvas, viewport)
	case modalSFTPColumns:
		state.paintSFTPColumnSettings(canvas, viewport)
	case modalLocalProxy:
		state.paintLocalProxyScrollbar(canvas)
	}
	state.paintTerminalMenu(canvas, viewport)
	state.paintGhostMenu(canvas, viewport)
	state.paintToolbarDockMenu(canvas, viewport)
	state.paintTabMenu(canvas, viewport)
	state.paintHostMenu(canvas, viewport)
	state.paintHostGroupActions(canvas, viewport)
	state.paintWorkspaceTemplateMenu(canvas, viewport)
	state.paintLocalShellPicker(canvas, viewport)
	state.paintSnippetMenu(canvas, viewport)
	state.paintSFTPMenu(canvas, viewport)
	state.paintQueuedSFTPHeaderMenu(canvas)
	state.paintHostMenus(canvas, viewport)
	state.endMenus()
	state.paintSnippetHoverCard(canvas, viewport)
	state.requestReconnectFrames(canvas)
	state.requestAutofillFrames(canvas)
	state.requestLoginCommandFrames(canvas)
	state.paintLocalProxyFocus(canvas)
	state.paintTransferProgressCard(canvas)
	// Last, so a tooltip is never covered by what it describes.
	if _, formatting := canvas.Hovered().In(hitNoteFormats); !formatting {
		state.tooltip.Paint(canvas, state.Theme(), viewport, components.TooltipOptions{Layer: tooltipLayer, SideOffset: tooltipGap}, components.HoverTooltipDelay)
	}
	state.fileNotices.paint(canvas, state.Theme(), gfx.Rect{Y: windowBarHeight, Width: size.Width, Height: max(size.Height-windowBarHeight, 0)})
	if !state.workspaceTemplateFocusPending.IsZero() {
		if state.activeSection == sectionWorkspaces && state.modal == modalNone {
			canvas.RequestFocus(state.workspaceTemplateFocusPending)
		}
		state.workspaceTemplateFocusPending = gfx.ID{}
	}
}

// windowRect is the whole window as last painted: the viewport a popup gives
// its Viva component so a press anywhere outside it dismisses it.
func (state *State) windowRect() gfx.Rect {
	if state.noteTarget != nil && state.noteTarget.windowView {
		return state.noteTarget.viewport
	}
	return gfx.Rect{Width: state.viewport.Width, Height: state.viewport.Height}
}

// fileManagerPixelBounds reserves the same side rail and assistant card that
// ordinary sections use. The file manager remains a main-window tab, but it no
// longer paints underneath controls that must receive input.
func (state *State) fileManagerPixelBounds(size gfx.Size) gfx.Rect {
	left := float32(0)
	if state.tabOrientation == tabsVertical && len(state.workspaceTabs) > 0 {
		left = state.verticalTabsReserve(size)
	}
	right := sectionCardMargin
	if reserve := state.aiSectionReserve(size); reserve > 0 {
		right = reserve
	}
	left += state.toolbarDockSideReserveFor(true, false)
	right += state.toolbarDockSideReserveFor(false, false)
	return gfx.Rect{
		X: left, Y: windowBarHeight,
		Width:  max(size.Width-left-right, 0),
		Height: max(size.Height-windowBarHeight-state.toolbarDockBottomReserve(false), 0),
	}
}

// tooltipGap is the space between a control and the tooltip that names it. The
// framework default (4px) sits too close to the small icon buttons of the bars.
const tooltipGap = float32(10)

// Window-bar metrics. The bar leaves what the platform keeps of the strip's two
// ends (app.TitlebarInsets: macOS's traffic lights, or the buttons Viva draws on
// Windows and Linux) and starts barMargin inside what is left.
const (
	barButtonSize = float32(28) // the square hit area of a window-bar button
	barMargin     = float32(12)
	barTabHeight  = float32(28)
	// barTabReach is how far a horizontal tab's click region extends above and
	// below its painted body: the rest of the bar's height. The body floats in the
	// bar, and the strip above it must still switch tabs when the pointer is thrown
	// against the window's top edge (Fitts's law), as in Chrome and Edge.
	barTabReach = (TitlebarHeight - barTabHeight) / 2
	barGap      = float32(4)
	barTabMin   = float32(96)
	barTabMax   = float32(170)
)

// windowBar keeps the places the window bar's own painters need once it is laid
// out: the assistant's button, the room the active session's name is drawn in,
// and, for each tab, where its system and its light go.
type windowBar struct {
	assistant  ui.Box
	transfers  ui.Box
	title      ui.Box
	titleLabel string
	tabs       []barTab
}

type barTab struct {
	badge, light ui.Box
	pathLabel    string
	foreground   gfx.Color
	title        ui.Box
	output       outputAlertKind
	label        string
	number       string // the tab's number, empty for a tab no number key reaches
	workspace    layoutcore.WorkspaceInfo
	fileSession  uint32
	id, closeID  gfx.ID
	background   gfx.Color
	compact      bool
}

// barGeometry is what a first pass over the window bar says about its tabs: the
// room they and the new-tab button share (area), the row they clip to, and how
// wide each is. It depends on the window's width, the number of tabs and the
// window entries the user shows, so it is kept until one of them changes.
type barGeometry struct {
	width             float32
	tabCount          int
	barButtons        int
	locked            bool
	numbers           bool
	orientation       tabOrientation
	leading, trailing float32
	padRight          float32
	valid             bool
	area, row         gfx.Rect
	tabWidth          float32
}

// windowPage is the page the workbench window declares its shell in. Paint draws
// it; WindowConfig gives it the window's input.
func (state *State) windowPage() *ui.Page {
	chrome := &state.chrome
	if chrome.windowPage == nil {
		chrome.windowPage = ui.NewPage(ui.PageConfig{
			Tree: &tree, Root: shellPageRoot, Theme: &state.theme, View: state.declareShell,
			ScrollbarSize: catalogBarGutter,
		})
	}
	return chrome.windowPage
}

// WindowConfig returns base with the workbench page routing the shell's own
// controls: its dialogs, choices, fields, lists and the assistant's menus take
// their keys, pointer, wheel and text before the application's handlers. The
// window's Paint stays its own, because it also paints the panes, the docks and
// the bodies the page only lays out.
func (state *State) WindowConfig(base app.Config) app.Config {
	config := state.windowPage().Config(base)
	config.Paint = base.Paint
	// The assistant owns the wheel over its own panel: its lists keep their
	// offsets in the page, but a modal dialog must still keep them still, which
	// the page's generic scroll-area routing does not know. Everything else the
	// event reaches goes on to the page as before.
	config.OnScroll = state.scrollWindow
	return config
}

// scrollWindow gives the wheel to the assistant's panel first, then to the
// page's other scroll areas and the window's own handlers. A modal dialog owns
// the wheel over its backdrop: the panel keeps still behind it, as it did when
// its list was painted by hand and the window's own handler saw the event first.
func (state *State) scrollWindow(event gfx.ScrollEvent) gfx.EventResult {
	if state.aiMenuShown() {
		// An open chip menu is the page's popup and owns the wheel; its own
		// scroll route (and its backdrop) decide where the event goes.
		return state.windowPage().Wheel(event)
	}
	panel := &state.ai.panel
	if panel.rect.Width > 0 && pointInRect(event.X, event.Y, panel.rect) && state.aiBackgroundScrollBlocked() {
		return gfx.EventResult{Handled: true}
	}
	if result, handled := state.aiPanelScroll(event); handled {
		return result
	}
	// A window dialog owns the wheel over its backdrop. Ask the window's own
	// handler before the page: the page hands the wheel to the scroll area,
	// table or list under the pointer first, and those lie behind the dialog
	// too, so the page would scroll them out from under it.
	if state.modal != modalNone {
		return state.Scroll(event)
	}
	return state.windowPage().Wheel(event)
}

// aiBackgroundScrollBlocked reports whether a dialog that holds the wheel is
// open over the assistant.
func (state *State) aiBackgroundScrollBlocked() bool {
	return state.modal != modalNone || state.knownHosts.action != knownHostsActionNone ||
		(state.aiSettingsVisible() && state.ai.cfg.dialog != aiDialogNone)
}

// windowFrameColor is the surface of the chrome around the panes: the window
// bar, the side rails and the strip below. A full-page pane fills the window,
// so the chrome there takes the workspace surface it frames, as the panes do;
// the shell around a workspace card stays a step away from the card.
func (state *State) windowFrameColor() gfx.Color {
	if state.chrome.shellFullPage {
		return state.pal.workspace
	}
	return state.pal.shell
}

// windowBarRightPad keeps the bar's last control centred over the side rail's
// column, where that rail puts its tools. The rail comes and goes with the
// session, so the pad follows the toolbar size rather than the rail itself:
// connecting must not shift the bar's right end sideways. Where the platform
// draws the window controls in the corner, their inset owns the space and the
// bar keeps its own margin; the rail is left where it is.
func (state *State) windowBarRightPad() float32 {
	_, trailing := app.TitlebarInsets()
	centred := state.terminalToolbarMetrics().railExtent/2 - barButtonSize/2
	if centred <= trailing {
		return trailing + barMargin
	}
	return centred
}

// declareShell declares the window's shell as a page: the bar over the body,
// and under it, unless a pane fills the window, the navigation rail beside the
// slot the sections are painted into. The inputs come from chromeLayout, kept by
// Paint before the page is declared, because the page's view takes no arguments.
func (state *State) declareShell(c ui.C) {
	// A fullscreen desktop is the window's one page too: it declares its bars
	// and picture area in the same page, with no shell around them.
	if session := state.fullscreenDesktop(); session != nil {
		state.declareDesktopBody(c, session, gfx.Rect{Width: state.viewport.Width, Height: state.viewport.Height})
		return
	}
	chrome, pal := &state.chrome, state.pal
	root := ui.Column(c).Fill()
	if !chrome.shellPicture {
		root.Background(state.windowFrameColor())
	}
	root.Children(func() {
		state.declareWindowBar(c, &chrome.bar, false, chrome.shellPicture, chrome.shellGeometry, chrome.shellScroll)
		if chrome.shellFullPage {
			return
		}
		ui.Row(c).Grow(1).Shrink(1).MarginSides(0, 0, state.toolbarDockBottomReserve(false), 0).Children(func() {
			ui.Slot(c).Width(state.toolbarDockSideReserveFor(true, false)).Shrink(0)
			state.declareNavigation(c)
			workspace := ui.Slot(c).Grow(1).MarginSides(0, chrome.shellCardRight, sectionCardMargin, 0).
				Radius(radius.surface).Border(workspaceBorder, pal.windowStroke).Background(pal.workspace)
			// The hosts page is part of this page like the panes: its header is a
			// slot the window paints, and its Tree names, scrolls and routes its
			// rows here rather than in a page of its own.
			if state.paintsHostsPage() {
				workspace.Children(func() { state.declareHostsWorkspace(c) })
			}
			workspace.Children(func() { state.devDeclareWorkspace(c) })
			workspace.Children(func() { state.declareDashboardEmpty(c) })
			// The catalog sections are part of this page too: each declares its
			// body here, beside its own slots, and paints the rows and buttons it
			// lays out once the page has placed them.
			workspace.Children(func() { state.declareCatalogSection(c) })
			chrome.workspaceSlot = workspace
		})
	})
	if chrome.shellFullPage {
		if state.terminalPage() {
			state.declareWorkspacePanes(c)
			state.declareContextPanel(c)
		} else {
			// The file-manager tab is part of the page like the panes.
			state.declareSFTPTab(c)
		}
		// The sidebar is an overlay over the full-page workspace: it paints on
		// the pane-chrome band and blocks presses on its blank surface.
		state.declareVerticalTabsOverlay(c)
	}
	// The bottom strip and each side rail are containers of this page too, at
	// the outer bands their reserves keep free. Their tools are painted by the
	// window after the page, into the slots these declarations leave.
	state.declareToolbarDockBottom(c)
	state.declareToolbarDockSides(c)
	state.declareTransferCenter(c)
	if !chrome.shellFullPage && state.activeSection == sectionSettings {
		state.declareSettingsWorkspace(c)
	}
	state.declareAIPanel(c)
	// The command palette is a dialog of the window's own page: its search field,
	// its list and its sort switch are page elements.
	state.declareCommandPalette(c)
	// The command-history picker is a dialog of the same page.
	state.declareHistory(c)
	// The workbench's modal dialogs are overlays of this same page.
	state.declareAIDialogs(c)
	state.declareCredentialDialog(c)
	state.declareImportSessions(c)
	state.declareMobaEncoding(c)
	state.declareSessionImportPreview(c)
	state.declareDevelopmentImport(c)
	state.declareWorkspaceDraft(c)
	state.declareWorkspaceSave(c)
	// The modal dialogs of the form family are page dialogs too: their bodies,
	// fields and buttons keep the application's identities, and the page owns
	// them.
	state.declareDialogs(c)
	state.declareKnownHostsDialog(c)
	// The drag overlay is hitless and paints over the page on the drag layer.
	state.declareTabDragOverlays(c)
	state.declareWorkspaceCardDragOverlay(c)
	state.declareToolbarDockDragOverlay(c)
}

// windowBarGeometry lays the bar out once without its contents to find out how
// wide the tabs come out: they stay barTabMax wide and shrink together to barTabMin
// to share the room left of the buttons.
func (state *State) windowBarGeometry(canvas *gfx.Canvas, width float32) barGeometry {
	chrome := &state.chrome
	leading, trailing := app.TitlebarInsets()
	padRight := state.windowBarRightPad()
	buttons := state.windowBarButtons()
	tabCount := len(state.workspaceTabs) + len(state.sftpTabs)
	locked := false
	for _, tab := range state.workspaceTabs {
		locked = locked || tab.Locked
	}
	geometry := &chrome.geometry
	if geometry.valid && geometry.width == width && geometry.tabCount == tabCount &&
		geometry.barButtons == buttons &&
		geometry.orientation == state.tabOrientation &&
		geometry.locked == locked &&
		geometry.numbers == state.tabNumbers &&
		geometry.leading == leading && geometry.trailing == trailing &&
		geometry.padRight == padRight {
		return *geometry
	}
	chrome.probeInput = barGeometry{locked: locked, numbers: state.tabNumbers}
	if chrome.probePage == nil {
		chrome.probePage = ui.NewPage(ui.PageConfig{Root: shellProbeRoot, Theme: &state.theme})
		chrome.probeView = state.declareProbeBar
	}
	chrome.probePage.Draw(canvas, gfx.Rect{Width: width, Height: windowBarHeight}, chrome.probeView)
	*geometry = barGeometry{
		width: width, tabCount: tabCount, barButtons: buttons, leading: leading, trailing: trailing, padRight: padRight, valid: true,
		locked:      locked,
		numbers:     state.tabNumbers,
		orientation: state.tabOrientation,
		area:        chrome.probeArea.Rect(), row: chrome.probeRow.Rect(), tabWidth: barTabMax,
	}
	if len(chrome.probeBar.tabs) > 0 {
		geometry.tabWidth = chrome.probeTabs[0].Rect().Width
	}
	return *geometry
}

// declareProbeBar declares the window bar for measurement only: no picture, no
// hits, just the places whose rectangles give the tabs their width.
func (state *State) declareProbeBar(c ui.C) {
	chrome := &state.chrome
	ui.Column(c).Fill().Children(func() {
		state.declareWindowBar(c, &chrome.probeBar, true, false, chrome.probeInput, 0)
	})
}

// declareWindowBar declares the strip the custom titlebar leaves for the app: the
// hosts button and one tab per session on the left, the local terminal, search,
// language, theme and settings buttons on the right. The tabs share the room left of
// the buttons; when even the narrowest do not fit they scroll under a clip, and the
// new-tab button stays at the end of that room instead of being pushed out of the
// window. With probe set only the places are declared, for windowBarGeometry.
func (state *State) declareWindowBar(c ui.C, bar *windowBar, probe, picture bool, geometry barGeometry, scroll float32) {
	m, pal, chrome := state.messages, state.pal, &state.chrome
	leading, _ := app.TitlebarInsets()
	visibleFileTabs := 0
	for _, tab := range state.sftpTabs {
		if tab != nil && !tab.closed {
			visibleFileTabs++
		}
	}
	bar.tabs = bar.tabs[:0]
	bar.title = ui.Box{}
	bar.titleLabel = ""
	if probe {
		chrome.probeTabs = chrome.probeTabs[:0]
	}
	row := ui.Row(c).Height(windowBarHeight).PadSides(0, state.windowBarRightPad(), 0, leading+barMargin).Items(ui.Center)
	if !probe {
		// The bar is where the window is moved from; its buttons and tabs, declared
		// after it, take their own presses.
		row.MovesWindow()
	}
	if picture {
		row.Background(state.windowFrameColor())
	}
	row.Children(func() {
		state.barButton(c, probe, hitHome, iconHome, m.NavHosts).MarginSides(0, barGap+2, 0, 0)
		if state.tabOrientation == tabsVertical {
			if probe {
				ui.Spacer(c)
			} else {
				// The name is drawn after the bar is laid out, centred over the
				// workspace it names: see paintWindowBarTitle. The slot keeps the
				// height of a tab so the drawn line sits on the row's middle.
				bar.title = ui.Slot(c).Grow(1).Height(barTabHeight)
				bar.titleLabel = state.windowBarTitle()
			}
		}
		// The room of the tabs and the new-tab button. The tabs stay as wide as they
		// like (barTabMax) and shrink together to barTabMin; the new-tab button
		// follows the last of them, or ends the room when they do not fit.
		tabRoom := ui.Stack(c).Grow(1).MarginSides(0, barGap, 0, 0)
		if state.tabOrientation == tabsVertical && visibleFileTabs == 0 {
			tabRoom.Grow(0).Width(barButtonSize)
		}
		tabRoom.Children(func() {
			area := ui.Slot(c).Fill().MarginSides(0, barButtonSize, 0, 0)
			ui.Row(c).Fill().Items(ui.Center).Children(func() {
				tabs := ui.Row(c)
				if probe {
					chrome.probeRow = tabs
				} else {
					// As tall as the bar, so the clip that scrolls the tabs sideways does
					// not cut off the click regions that reach the bar's top edge.
					tabs.Width(geometry.row.Width).Height(windowBarHeight).Items(ui.Center).Shrink(0).Clip()
				}
				tabs.Children(func() {
					for index := range state.workspaceTabs {
						if state.tabOrientation == tabsVertical {
							break
						}
						workspace := state.workspaceTabs[index]
						var tab ui.Box
						if probe {
							minimum := barTabMin
							if geometry.locked {
								// Leave the title its normal minimum room even with a
								// lock beside it. The strip uses a uniform tab pitch.
								minimum += 18
							}
							if geometry.numbers {
								// The number sits beside the name, which keeps its room.
								minimum += tabNumberMinExtra
							}
							tab = ui.Slot(c).Width(barTabMax).MinWidth(minimum).Height(barTabHeight).Shrink(1).MarginSides(0, barGap, 0, 0)
							chrome.probeTabs = append(chrome.probeTabs, tab)
							bar.tabs = append(bar.tabs, barTab{})
							continue
						}
						first := float32(0)
						if index == 0 {
							first = -scroll
						}
						entry := state.declareWorkspaceTab(c, workspace, geometry.tabWidth, barTabHeight, first, false, false, index+1)
						bar.tabs = append(bar.tabs, entry)
					}
					for fileIndex, fileTab := range state.sftpTabs {
						if fileTab == nil || fileTab.closed {
							continue
						}
						index := len(state.workspaceTabs) + fileIndex
						id, closeID := sftpFileTabID(fileTab.session.ID), sftpFileTabCloseID(fileTab.session.ID)
						if probe {
							minimum := barTabMin
							if geometry.locked {
								minimum += 18
							}
							if geometry.numbers {
								minimum += tabNumberMinExtra
							}
							tab := ui.Slot(c).Width(barTabMax).MinWidth(minimum).Height(barTabHeight).Shrink(1).MarginSides(0, barGap, 0, 0)
							chrome.probeTabs = append(chrome.probeTabs, tab)
							bar.tabs = append(bar.tabs, barTab{})
							continue
						}
						entry := barTab{fileSession: fileTab.session.ID, id: id, closeID: closeID}
						if state.tabNumbers {
							entry.number = tabNumber(index + 1)
						}
						active := state.activeSection == sectionSFTP && state.activeSFTPTab == fileTab.session.ID
						hovered := c.Canvas().Hovered() == id || c.Canvas().Hovered() == closeID
						foreground := pal.muted
						fill, edge := pal.tab, pal.line
						switch {
						case active:
							fill, edge, foreground = pal.tabActive, pal.windowStroke, pal.text
						case hovered:
							fill, foreground = pal.tabHover, pal.text
						}
						amount := float32(.08)
						if active {
							amount = .18
						} else if hovered {
							amount = .12
						}
						fill = tintHostSurface(fill, fileTab.session.host.Color, amount)
						edge = tintHostSurface(edge, fileTab.session.host.Color, .35)
						entry.background = fill
						first := float32(0)
						if state.tabOrientation == tabsVertical {
							if fileIndex == 0 {
								first = -scroll
							}
						} else if index == 0 {
							first = -scroll
						}
						label := state.sftpFileTabTitle(fileTab)
						ui.Row(c).Size(geometry.tabWidth, barTabHeight).Shrink(0).MarginSides(0, barGap, 0, first).
							HitOutset(barTabReach, 0, barTabReach, 0).PadSides(0, 3, 0, 4).Items(ui.Center).Radius(radius.item).Border(1, edge).Background(fill).
							HitWith(id, label, gfx.HitOptions{Role: gfx.RoleTab, Selected: active}).Children(func() {
							entry.badge = ui.Slot(c).Size(20, 20).MarginSides(0, 7, 0, 0)
							if state.tabNumbers && entry.number != "" {
								// File tabs follow the session tabs in the number row,
								// as they do for the number shortcuts.
								ui.Text(c, entry.number).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(tabNumberColor(foreground)).LineHeight(20).Shrink(0).MarginSides(0, tabNumberGap, 0, 0)
							}
							ui.Text(c, label).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(foreground).LineHeight(20).Ellipsis()
							entry.light = ui.Slot(c).Size(9, 20).MarginSides(0, 0, 0, 7)
							ui.Spacer(c)
							if !active && !hovered {
								ui.Slot(c).Size(20, 20)
								return
							}
							var closeFill gfx.Color
							if c.Canvas().Hovered() == closeID {
								closeFill = pal.line
								if active {
									closeFill = pal.tabCloseHot
								}
							}
							ui.Row(c).Size(20, 20).Shrink(0).Center().Radius(radius.item).Background(closeFill).
								HitWith(closeID, m.LabelCloseTab, gfx.HitOptions{Role: gfx.RoleButton, SkipFocus: true}).Children(func() {
								ui.Icon(c, lucideIcons[iconClose]).Size(14, 14).Color(foreground)
							})
						})
						bar.tabs = append(bar.tabs, entry)
					}
				})
				state.barButton(c, probe, hitNewTab, iconPlus, m.LabelNewTab)
			})
			if probe {
				chrome.probeArea = area
			}
		})
		state.declareToolbarDockTop(c, probe)
	})
}

// declareWorkspaceTab shares tab styling and hit identities between both
// orientations. number is the tab's place in the number row, zero for a tab no
// number key reaches.
func (state *State) declareWorkspaceTab(c ui.C, workspace layoutcore.WorkspaceInfo, width, height, first float32, vertical, compact bool, number int) barTab {
	m, pal := state.messages, state.pal
	id, closeID := hitTabList.At(int(workspace.ID)), hitTabCloseList.At(int(workspace.ID))
	entry := barTab{workspace: workspace, id: id, closeID: closeID}
	if state.tabNumbers {
		// Numbering costs a string per tab, so a bar without numbers pays none.
		entry.number = tabNumber(number)
	}
	active := state.activeSection == sectionTerminal && state.deck.ActiveWorkspace() == workspace.ID
	hovered := c.Canvas().Hovered() == id || c.Canvas().Hovered() == closeID
	foreground := pal.muted
	fill, edge := pal.tab, pal.line
	switch {
	case active:
		fill, edge, foreground = pal.tabActive, pal.windowStroke, pal.text
	case hovered:
		fill, foreground = pal.tabHover, pal.text
	}
	if session := state.tabSession(workspace); session != nil {
		amount := float32(.08)
		if active {
			amount = .18
		} else if hovered {
			amount = .12
		}
		fill = tintHostSurface(fill, session.host.Color, amount)
		edge = tintHostSurface(edge, session.host.Color, .35)
	}
	entry.background = fill
	entry.foreground = foreground
	label := state.tabLabel(workspace)
	entry.label = label
	entry.output = state.workspaceOutputAlert(workspace.ID)
	editing := !compact && state.tabInlineEditing(workspace.ID)
	tabOptions := gfx.HitOptions{Role: gfx.RoleTab, Selected: active}
	// Every tab shows its full name as a tooltip on hover, so a long, elided
	// label (a deep path or a long host label among many tabs) stays readable.
	// The shared hover tooltip paints Hit, so naming it here is all it takes.
	if !editing {
		// A tooltip must not cover the field the name is being typed in.
		tabOptions.Hint = label
		if workspace.Locked {
			tabOptions.Hint = m.TabLockedHint
		}
		if directory := state.tabDirectory(workspace); directory != "" {
			if tabOptions.Hint != "" && workspace.Locked {
				tabOptions.Hint += "\n" + directory
			} else {
				tabOptions.Hint = directory
			}
		}
		// A desktop keeps its size and protection in the status line under the
		// picture, which the host may have hidden: the tab says it on hover instead.
		if session := state.tabSession(workspace); session != nil && session.desk != nil {
			if hint := state.desktopStripLeft(session.desk, session); hint != "" {
				tabOptions.Hint = hint
			}
		}
	}
	if editing {
		// The tab under the field is not a second focusable control: the field
		// is what the keyboard belongs to, and a press anywhere else ends the
		// edit before it lands. It still takes presses, so its menu and its
		// close button keep working.
		tabOptions.SkipFocus = true
	}
	row := ui.Row(c).Size(width, height).Shrink(0)
	if vertical {
		row.MarginSides(first, 0, barGap, 0)
	} else {
		row.MarginSides(0, barGap, 0, first).HitOutset(barTabReach, 0, barTabReach, 0)
	}
	row.
		PadSides(0, 3, 0, 4).Items(ui.Center).Radius(radius.item).Border(1, edge).Background(fill).
		HitWith(id, label, tabOptions).Children(func() {
		if compact {
			ui.Spacer(c)
			entry.badge = ui.Slot(c).Size(20, 20).Shrink(0)
			ui.Spacer(c)
			return
		}
		entry.badge = ui.Slot(c).Size(20, 20).MarginSides(0, 7, 0, 0)
		if state.tabNumbers && entry.number != "" {
			// The number names the name beside it, so it carries less ink.
			ui.Text(c, entry.number).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(tabNumberColor(foreground)).LineHeight(20).Shrink(0).MarginSides(0, tabNumberGap, 0, 0)
		}
		if editing {
			// The title is the field itself, and the room the lock, the light
			// and the close button took belongs to the name while it is typed.
			// The parts painter fills the slot with the framework's field,
			// which declares the field's own hit after layout.
			entry.title = ui.Slot(c).Size(0, 20).Grow(1).Basis(0)
			return
		}
		titleColor := foreground
		if entry.output != outputAlertNone {
			titleColor.Alpha = 0
		}
		var title ui.Box
		if state.tabDirectory(workspace) != "" {
			// Reserve the same layout space as text, then shorten the path once
			// the actual width is known. Ordinary names keep tail ellipsis.
			entry.pathLabel = label
			title = ui.Slot(c).Size(c.Canvas().TextWidthWeight(label, metrics.FontSizeS, gfx.WeightMedium), 20).Shrink(1)
		} else {
			title = ui.Text(c, label).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(titleColor).LineHeight(20).Ellipsis()
		}
		entry.title = title
		if vertical {
			title.Grow(1).Basis(0)
		}
		if workspace.Locked {
			ui.Icon(c, lucideIcons[iconLock]).Size(13, 13).Shrink(0).MarginSides(0, 0, 0, 5).Color(foreground)
		}
		entry.light = ui.Slot(c).Size(9, 20).MarginSides(0, 0, 0, 7)
		if !vertical {
			ui.Spacer(c)
		}
		if !active && !hovered {
			ui.Slot(c).Size(20, 20)
			return
		}
		var closeFill gfx.Color
		if c.Canvas().Hovered() == closeID {
			closeFill = pal.line
			if active {
				closeFill = pal.tabCloseHot
			}
		}
		ui.Row(c).Size(20, 20).Shrink(0).Center().Radius(radius.item).Background(closeFill).
			HitWith(closeID, m.LabelCloseTab, gfx.HitOptions{Role: gfx.RoleButton, SkipFocus: true}).Children(func() {
			ui.Icon(c, lucideIcons[iconClose]).Size(14, 14).Color(foreground)
		})
	})
	entry.compact = compact
	return entry
}

// barButton declares a square icon button of the window bar. These buttons do not
// take keyboard focus when pressed, so using one never pulls the keyboard out of a
// terminal. With probe set it is only the place.
func (state *State) barButton(c ui.C, probe bool, id gfx.ID, icon uint8, label string) ui.Box {
	if probe {
		return ui.Slot(c).Size(barButtonSize, barButtonSize)
	}
	pal, canvas := state.pal, c.Canvas()
	foreground := pal.muted
	var fill gfx.Color
	switch {
	case canvas.Pressed() == id:
		fill, foreground = pal.tabActive, pal.text
	case canvas.Hovered() == id:
		fill, foreground = pal.tabHover, pal.text
	}
	return ui.Row(c).Size(barButtonSize, barButtonSize).Shrink(0).Center().Radius(radius.item).Background(fill).
		HitWith(id, label, gfx.HitOptions{Role: gfx.RoleButton, SkipFocus: true, Hint: label}).Children(func() {
		ui.Icon(c, lucideIcons[icon]).Color(foreground)
	})
}

// paintWindowBarParts draws what the bar's tabs and assistant button hold that is
// drawn rather than declared: the system of a session and its light, in the room
// the layout kept for them, and the session's name.
func (state *State) paintWindowBarParts(canvas *gfx.Canvas, strip gfx.Rect) {
	bar := &state.chrome.bar
	state.paintWorkspaceTabParts(canvas, strip, bar.tabs)
	state.paintWindowBarTitle(canvas)
	state.paintToolbarDockTop(canvas)
}

// windowBarTitle names what the window shows when the tabs are in the sidebar:
// the file tab in front, else the active session, else the app's own name.
func (state *State) windowBarTitle() string {
	if manager := state.activeSFTPWindow(); manager != nil {
		return state.sftpFileTabTitle(manager)
	}
	if state.terminalPage() {
		for _, tab := range state.workspaceTabs {
			if tab.ID == state.deck.ActiveWorkspace() {
				return state.tabLabel(tab)
			}
		}
	}
	return state.messages.TrayTitle
}

// paintWindowBarTitle draws that name in the bar's free room, centred over the
// workspace the name belongs to. With the tabs in the left sidebar the middle of
// the bar is empty: a name left against the hosts button reads as part of that
// group of buttons rather than as the name of what is on screen, and one name in
// a wide empty strip reads as the strip's own title.
func (state *State) paintWindowBarTitle(canvas *gfx.Canvas) {
	bar := &state.chrome.bar
	if bar.titleLabel == "" {
		return
	}
	band := bar.title.Rect()
	const pad = float32(10)
	room := band.Width - 2*pad
	if room < 24 {
		return
	}
	label := elide(canvas, bar.titleLabel, metrics.FontSizeS, gfx.WeightRegular, room)
	width := canvas.TextWidth(label, metrics.FontSizeS)
	// The workspace is what the name is about: centre it over that column rather
	// than over the whole bar, which carries the sidebar on its left. File tabs
	// share the band, so the name still stops at the band's ends.
	column := state.verticalTabsColumn(state.viewport)
	center := column + (state.viewport.Width-column)/2
	x := min(max(center-width/2, band.X+pad), band.X+band.Width-pad-width)
	canvas.Label(label, gfx.Rect{X: x, Y: band.Y, Width: width, Height: band.Height},
		gfx.TextStyle{Size: metrics.FontSizeS, Color: state.pal.muted})
}

// elide shortens text to fit width, ending it with an ellipsis. Weight is the
// one the text is drawn at, since a heavier line is wider.
func elide(canvas *gfx.Canvas, value string, size, weight, width float32) string {
	if canvas.TextWidthWeight(value, size, weight) <= width {
		return value
	}
	// The longest prefix that fits with the ellipsis, found by halving: a long
	// path in a narrow cell would otherwise be measured once for every rune.
	runes := []rune(value)
	fits := 0
	for low, high := 1, len(runes)-1; low <= high; {
		middle := (low + high) / 2
		if canvas.TextWidthWeight(string(runes[:middle])+"…", size, weight) <= width {
			fits, low = middle, middle+1
		} else {
			high = middle - 1
		}
	}
	if fits == 0 {
		return "…"
	}
	return string(runes[:fits]) + "…"
}

// shellPageRoot names the shell's controls in the window's Tree, and
// shellProbeRoot the window-bar page that measures the tabs. They are separate
// pages, so each needs a root of its own.
const (
	shellPageRoot  = "window-shell"
	shellProbeRoot = "window-bar-probe"
)

// windowPageOwns reports whether id is named by the workbench page: one of its
// elements, a popup it declares, or a dialog body it declares. The page root is
// what WindowConfig routes.
func windowPageOwns(id gfx.ID) bool {
	return !id.IsZero() && tree.Within(id.Scope(), tree.Feature(shellPageRoot))
}

// chromeLayout keeps what the window page's declaration leaves for the painters
// that run after it, and the one shared layout View for the small controls that
// are painted into rectangles the page does not lay out.
type chromeLayout struct {
	// headerPlan is what the hosts header's declaration worked out from the
	// window, and headerBoxes the slots it left for the painter.
	headerPlan  hostsHeaderPlan
	headerBoxes hostsHeaderBoxes
	// hostToggle is the view and sort controls of the hosts header, declared in
	// the header's page.
	hostToggle hostToggleBoxes
	// windowPage declares the shell (the window bar, the navigation rail and the
	// workspace slot). workspaceSlot is the slot the sections paint into, and the
	// shell* fields are what the cached view reads for the paint in progress.
	// probePage declares the window bar once without its contents to find how wide
	// the tabs come out; probeInput is what its cached view reads.
	windowPage     *ui.Page
	workspaceSlot  ui.Box
	shellPicture   bool
	shellHeight    float32
	shellGeometry  barGeometry
	shellScroll    float32
	shellFullPage  bool
	shellCardRight float32
	probePage      *ui.Page
	probeView      func(ui.C)
	probeInput     barGeometry
	// tabDragDropDraw, tabDragGhostDraw, tabDragHintDraw and tabDragEdgeDraw
	// paint the hitless elements of a tab drag. They are made once, so a warm
	// repaint declares the overlays without allocating a method value.
	tabDragDropDraw  func(*ui.DrawContext)
	tabDragGhostDraw func(*ui.DrawContext)
	tabDragHintDraw  func(*ui.DrawContext)
	tabDragEdgeDraw  func(*ui.DrawContext)
	// workspaceCardDragDraw and toolbarDockDragDraw paint the hitless elements of
	// the two remaining drag overlays. They are made once, so a warm repaint
	// declares the overlay without allocating a method value.
	workspaceCardDragDraw func(*ui.DrawContext)
	toolbarDockDragDraw   func(*ui.DrawContext)
	// passwordSave is the slot the save box of the password dialog was laid out
	// in.
	passwordSave ui.Box
	// passwordFieldsDraw paints the password dialog's fields into the slots its
	// body declared; it is kept so a warm repaint allocates nothing.
	passwordFieldsDraw func(*ui.DrawContext)
	// dialogFooter is the actions and painter of the open dialog's hand-painted
	// footer; it is kept so a repaint declares the footer without allocating.
	dialogFooter dialogFooterScratch
	// hostDialogTitle labels the host form's scroll viewport. The handlers below
	// are the form's Draw and DrawOver declarations kept once, so a warm repaint
	// of the form allocates nothing.
	hostDialogTitle       string
	hostDialogCancelLabel string
	hostDialogSubmitLabel string
	hostFormViewportDraw  func(*ui.DrawContext)
	hostFormScrollbarDraw func(*ui.DrawContext)
	hostFormFieldsDraw    func(*ui.DrawContext)
	hostDialogFooterDraw  func(*ui.DrawContext)
	// newTabAdd and newTabAll are the empty tab's footer buttons.
	newTabAdd   ui.Box
	newTabAll   ui.Box
	newTabShown bool
	// importButton is where the toolbar's Import button was painted: the menu
	// it opens hangs under it, right-aligned.
	importButton gfx.Rect
	// chips are the slots of the group chips of the hosts page, in the order
	// they are painted: All, the groups that fit, then More. The slice is kept
	// so a repaint does not allocate.
	chips []ui.Box
	// bar and probeBar are the window bar as laid out for painting and for finding
	// the tabs' width; geometry is what the second found, and probeArea, probeRow and
	// probeTabs the places it was read from.
	bar, probeBar       windowBar
	geometry            barGeometry
	probeArea, probeRow ui.Box
	probeTabs           []ui.Box
	// lockup is where the sidebar header's logo is painted.
	lockup ui.Box
	// newTabBadges are the places of the systems' badges on the new-tab page.
	newTabBadges []ui.Box
	// newTabShells is the arrow beside the Local Terminal row that opens the
	// shell menu, kept to place that menu under it.
	newTabShells ui.Box
	// hostList is the hosts list: where the wheel is aimed.
	hostList gfx.Rect
	// sftpPanel, sftpEditor and sftpEmpty are the slots the docked files panel's
	// page view declares. The view is part of the workbench page now; the slots
	// let the hand-painted parts find their rectangles after layout.
	sftpPanel  sftpPanelScratch
	sftpEditor sftpEditorScratch
	sftpEmpty  sftpEmptyScratch
}

// hostsHeaderAction is one action of the hosts header's checked-row bar.
type hostsHeaderAction struct {
	id      gfx.ID
	label   string
	icon    uint8
	enabled bool
	danger  bool
}

// hostsHeaderPlan is what the hosts header works out from the window before it
// is declared: the widths of its controls, which are condensed, and the actions
// shown while hosts are checked. It is kept in chromeLayout between the header's
// declaration and its painting.
type hostsHeaderPlan struct {
	condensed                    bool
	connectWidth, importWidth    float32
	newHostWidth                 float32
	connectIcon, importIcon      uint8
	newHostIcon                  uint8
	anySelected                  bool
	actions                      [7]hostsHeaderAction
	actionWidths                 [7]float32
	actionsCondensed             bool
	showChips, showGroupControls bool
	compactHostControls          bool
	headerHeight                 float32
}

// hostsHeaderBoxes are the slots the hosts header's declaration leaves for the
// painter: the toolbar, its buttons, the checked-row actions and the view
// controls.
type hostsHeaderBoxes struct {
	search, connect, importButton, newHost ui.Box
	toggle, selectAll, orderButton         ui.Box
	buttons                                [7]ui.Box
	groupControls                          [2]ui.Box
}

// hostToggleBoxes are the slots of the hosts header's view and sort controls.
type hostToggleBoxes struct {
	list, grid, sort, privacy, order ui.Box
}

// declareNavigation declares the sidebar: the name of the application over the
// pages, and the way to a shell on this computer and to the settings at the bottom.
func (state *State) declareNavigation(c ui.C) {
	m := state.messages

	items := [...]struct {
		id     gfx.ID
		icon   uint8
		label  string
		active bool
	}{
		{hitHosts, iconHosts, m.NavHosts, state.activeSection == sectionHosts},
		{hitWorkspaces, iconColumns2, m.NavWorkspaces, state.activeSection == sectionWorkspaces},
		{hitDashboard, iconActivity, m.Dashboard.Title, state.activeSection == sectionDashboard},
		{hitSessionLogs, iconFileClock, m.SessionLogs.Title, state.activeSection == sectionSessionLogs},
		{hitKeychain, iconKey, m.NavKeychain, state.activeSection == sectionKeys},
		{hitProxies, iconGlobe, m.NavProxies, state.activeSection == sectionProxies},
		{hitPortForwarding, iconForward, m.NavPortForwarding, state.activeSection == sectionForwards},
		{hitKnownHosts, iconKnown, m.NavKnownHosts, state.activeSection == sectionKnownHosts},
		{hitSnippets, iconNote, m.NavSnippets, state.activeSection == sectionSnippets},
		{hitNotes, iconNotebook, state.noteText("Notes", "笔记"), state.activeSection == sectionNotes},
	}
	ui.Column(c).Width(navigationWidth).Shrink(0).Children(func() {
		// The logo with the name beside it, painted into this slot after layout.
		state.chrome.lockup = ui.Slot(c).Size(lockupWidth, lockupHeight).MarginSides(15, 0, 20, 0).Self(ui.Center)
		for _, item := range items {
			state.navigationItem(c, item.id, item.icon, item.label, item.active, 4)
		}
		if state.tabOrientation == tabsVertical && len(state.workspaceTabs) > 0 {
			state.declareVerticalTabsNav(c)
		} else {
			ui.Spacer(c)
		}
		// A shell on this computer needs no host, so its way in is always at hand:
		// above the Settings, at the bottom of the sidebar. A dev build also puts
		// the component gallery there (see devDeclareNavigation).
		state.navigationItem(c, hitLocalTerminal, iconTerminal, m.LocalTerminal, false, 4)
		state.openLocalShellPickerOn(c.Canvas(), hitLocalTerminal, false)
		state.devDeclareNavigation(c)
		state.navigationItem(c, hitSettings, iconSettings, m.NavSettings, state.activeSection == sectionSettings, 16)
	})
}

// navigationItem declares one row of the sidebar: an icon and a name, filled while
// it is the page on show or the pointer is on it.
func (state *State) navigationItem(c ui.C, id gfx.ID, icon uint8, label string, active bool, below float32) {
	pal := state.pal
	var fill gfx.Color
	if active {
		fill = pal.surfaceHot
	} else if c.Canvas().Hovered() == id {
		fill = pal.surfaceSoft
	}
	ui.Row(c).Height(36).MarginSides(0, 9, below, 9).PadSides(0, 9, 0, 15).Gap(12).Items(ui.Center).Radius(radius.control).Background(fill).
		HitWith(id, label, gfx.HitOptions{Role: gfx.RoleMenuItem, Selected: active}).Children(func() {
		ui.Icon(c, lucideIcons[icon]).IconTransform(state.iconMotion(c.Canvas(), id, icon)).Color(pal.text)
		ui.Text(c, label).FontSize(metrics.FontSizeM).Weight(gfx.WeightMedium).Color(pal.text)
	})
}

func (state *State) paintHostsWorkspace(canvas *gfx.Canvas, bounds gfx.Rect) {
	headerHeight := state.hostHeaderHeight(bounds.Width, len(state.hostSelections) > 0)
	state.paintHostsHeader(canvas)
	list := gfx.Rect{X: bounds.X + 18, Y: bounds.Y + headerHeight, Width: max(bounds.Width-36, 0), Height: max(bounds.Height-headerHeight-18, 0)}
	state.hostList.bounds = list
	state.chrome.hostList = list

	switch {
	case len(state.hostTreeRows) > 0 && state.hostView == hostViewList:
		// The page has already painted and declared the Tree; what is left is
		// the bar beside the rows and the focus a reveal asked for.
		state.paintHostListTable(canvas, list)
	case len(state.hostTreeRows) > 0:
		// The page's declaration laid the cards out at this scroll position and
		// set the content height; the paint adds the bar beside them.
		state.paintHostGridTable(canvas, list)
	default:
		state.hostsContent = 0
		if len(state.hosts) == 0 {
			state.paintEmptyHosts(canvas)
		} else if len(state.visible) == 0 {
			state.paintNoMatches(canvas)
		}
	}
	state.paintHostSortDrag(canvas, list)
	state.hostsBounds = bounds
}

// paintHostMenus draws the hosts page's dropdowns above everything else, with
// the whole window as their viewport, so a press anywhere closes them.
func (state *State) paintHostMenus(canvas *gfx.Canvas, viewport gfx.Rect) {
	bounds := state.hostsBounds
	if bounds.Width == 0 {
		return
	}
	if state.hostGroupMenu {
		state.paintHostGroupMenu(canvas, bounds, viewport)
	}
	if state.hostFilterMenu {
		state.paintHostFilterMenu(canvas, viewport)
	}
	if state.chipMenu.open {
		state.paintHostChipMenu(canvas, viewport)
	}
	if state.importMenu {
		state.paintImportMenu(canvas, viewport)
	}
	if state.hostPrivacy.menu {
		state.paintHostPrivacyMenu(canvas, viewport)
	}
	state.paintHostOrder(canvas, viewport)
}

// hostsHeaderPlan works out the header of the hosts page from the window: the
// widths its controls need, which of them are condensed, and the actions shown
// while hosts are checked. The declaration and the painter share it, so the
// controls painted into the slots are the ones the page laid out.
func (state *State) hostsHeaderPlan(canvas *gfx.Canvas, bounds gfx.Rect) hostsHeaderPlan {
	m := state.messages
	// The buttons keep their natural width, so the search box gives way to them
	// rather than the other way round: a label that is wider than expected (a
	// heavier or wider font) must not slide a button under its neighbour.
	const (
		toolbarMargin      = float32(18)  // the toolbar row's own margin inside the section card
		buttonGap          = float32(10)  // the air between the search box and the buttons
		searchLeast        = float32(140) // the narrowest the search box stays a search box at
		toggleWidth        = float32(56)
		rowPad             = float32(18)
		actionGap          = float32(4)
		groupControlsWidth = float32(56)
	)
	var plan hostsHeaderPlan
	// A section squeezed by the assistant panel has no room for the labels: the
	// buttons keep only their icons, whose tooltips name them, and the search box
	// keeps the least width it works at.
	labeled := searchLeast + buttonGap + toolbarButtonWidth(canvas, m.Connect, false) +
		buttonGap + toolbarButtonWidth(canvas, m.ImportMenu, true) +
		buttonGap + toolbarButtonWidth(canvas, m.NewHost, true)
	plan.condensed = labeled > bounds.Width-2*toolbarMargin
	plan.connectWidth, plan.connectIcon = toolbarButtonLayout(canvas, m.Connect, 0, iconPlug, plan.condensed)
	plan.importWidth, plan.importIcon = toolbarButtonLayout(canvas, m.ImportMenu, iconDownload, iconDownload, plan.condensed)
	plan.newHostWidth, plan.newHostIcon = toolbarButtonLayout(canvas, m.NewHost, iconPlus, iconPlus, plan.condensed)
	plan.anySelected = len(state.hostSelections) > 0
	// While something is checked the first label lines up with the list's left
	// edge, so the button's padding hangs outside it (a negative margin).
	const quietPadding = float32(12)
	plan.actions = [...]hostsHeaderAction{
		{hitExportHosts, m.ExportHosts, iconFileDown, len(state.hosts) > 0, false},
		{hitConnectSelectedHosts, state.messages.WorkspaceDraftTemporary, iconPlug, false, false},
		{hitEditHost, m.EditHost, iconPencil, false, false},
		{hitCopyHost, m.CopyHost, iconCopy, false, false},
		{hitMoveHostGroup, m.MoveHostGroup, iconFolderOpen, false, false},
		{hitDashboardAddHosts, state.messages.Dashboard.AddHosts, iconActivity, false, false},
		{hitDeleteHost, m.DeleteHost, iconTrash, false, true},
	}
	// The actions keep their words while the row has room for them and the count
	// of what is checked; a narrow row keeps only their icons.
	if plan.anySelected {
		enabled := state.selectedHostID != 0
		single := enabled && len(state.hostSelections) <= 1
		for i := range plan.actions {
			switch plan.actions[i].id {
			case hitConnectSelectedHosts:
				plan.actions[i].enabled = state.selectedHostsWorkspaceEligible()
			case hitEditHost, hitCopyHost:
				plan.actions[i].enabled = single
			case hitMoveHostGroup, hitDeleteHost:
				plan.actions[i].enabled = enabled
			case hitDashboardAddHosts:
				plan.actions[i].enabled = state.dashboardCanAddHosts()
			}
		}
		used := canvas.TextWidthWeight(state.hostSelectionText, metrics.FontSizeS, gfx.WeightMedium) + 2*hostHeaderRowGap
		for _, action := range plan.actions {
			used += canvas.TextWidthWeight(action.label, metrics.FontSizeS, gfx.WeightSemibold) + 4 + 2*quietPadding + actionGap
		}
		plan.actionsCondensed = used > bounds.Width-2*rowPad
		for index, action := range plan.actions {
			plan.actionWidths[index] = canvas.TextWidthWeight(action.label, metrics.FontSizeS, gfx.WeightSemibold) + 4 + 2*quietPadding
		}
		if plan.actionsCondensed {
			for index := range plan.actionWidths {
				plan.actionWidths[index] = actionIconSize
			}
		}
	}
	// Which chips fit decides what is declared; where they sit is the row's.
	plan.showChips = state.hasHostFilter()
	plan.showGroupControls = plan.showChips && !state.hostFilterOn
	plan.compactHostControls = bounds.Width < 450
	viewControlsWidth := toggleWidth + 6 + 26 + 6 + 26
	selectionControlsWidth := float32(0)
	if !plan.compactHostControls {
		viewControlsWidth += 32
		selectionControlsWidth = 52
	}
	plan.headerHeight = state.hostHeaderHeight(bounds.Width, plan.anySelected)
	if plan.showChips {
		room := bounds.Width - 2*rowPad - viewControlsWidth - hostHeaderRowGap - selectionControlsWidth
		if plan.showGroupControls {
			room -= groupControlsWidth + hostHeaderRowGap
		}
		state.fitHostChips(canvas, room)
	}
	return plan
}

// declareHostsHeader declares the toolbar, persistent group filters and, while
// hosts are checked, the separate row of batch actions. The controls themselves
// are painted into the slots it leaves.
func (state *State) declareHostsHeader(c ui.C, bounds gfx.Rect) {
	m, pal := state.messages, state.pal
	chrome := &state.chrome
	plan := state.hostsHeaderPlan(c.Canvas(), bounds)
	chrome.headerPlan = plan
	chrome.headerBoxes = hostsHeaderBoxes{}
	b := &chrome.headerBoxes
	const (
		toolbarMargin = float32(18)
		buttonGap     = float32(10)
		searchLeast   = float32(140)
		rowPad        = float32(18)
		actionGap     = float32(4)
	)
	quietPadding := float32(12)
	searchWidth := min(max(bounds.Width*0.41, 330), 485)
	ui.Column(c).Fill().Children(func() {
		ui.Column(c).Height(plan.headerHeight).Shrink(0).PadSides(14, 0, 0, 0).Children(func() {
			ui.Row(c).Height(toolbarHeight).MarginX(toolbarMargin).Items(ui.Center).Children(func() {
				b.search = ui.Slot(c).Basis(searchWidth).Shrink(1).MinWidth(searchLeast).Height(toolbarHeight).MarginSides(0, buttonGap, 0, 0)
				b.connect = state.declareButtonSized(c, hitConnect, m.Connect, plan.connectIcon, false, false, state.typedQuery() != "", toolbarFont, plan.condensed).Size(plan.connectWidth, toolbarHeight).MarginSides(0, buttonGap, 0, 0)
				ui.Spacer(c)
				b.importButton = state.declareButtonSized(c, hitImportMenu, m.ImportMenu, plan.importIcon, false, false, true, toolbarFont, plan.condensed).Size(plan.importWidth, toolbarHeight).MarginSides(0, buttonGap, 0, 0)
				b.newHost = state.declareButtonSized(c, hitAddHost, m.NewHost, plan.newHostIcon, true, false, true, toolbarFont, plan.condensed).Size(plan.newHostWidth, toolbarHeight)
			})
			// Filters and view controls stay in place when the selection changes.
			ui.Row(c).Height(hostHeaderRowHeight).MarginSides(12, 0, 0, 0).PadSides(0, 18, 0, 18).Gap(12).Items(ui.Center).Children(func() {
				if !plan.compactHostControls {
					b.selectAll = ui.Slot(c).Size(40, hostHeaderRowHeight).Shrink(0)
				}
				if plan.showChips {
					ui.Row(c).Gap(6).Children(func() {
						chrome.chips = chrome.chips[:0]
						chrome.chips = append(chrome.chips, state.declareFilterChip(c, filterChip{
							id: hitHostFilterAll, label: m.HostFilterAll, digits: state.hostTotalDigits, chosen: !state.hostFilterOn,
						}).Size(state.filterChipWidth(c.Canvas(), m.HostFilterAll, state.hostTotalDigits), hostHeaderRowHeight))
						for _, index := range state.hostChips[:state.hostFilterHidden] {
							label, digits, chosen := state.hostChipView(index)
							var chevron gfx.ID
							if state.hostChipHasMenu(index) {
								chevron = hitHostFilterChevronList.At(index)
							}
							chrome.chips = append(chrome.chips, state.declareFilterChip(c, filterChip{
								id: hitHostFilterList.At(index), chevron: chevron, label: label, digits: digits,
								pinned: state.hostGroups[index].Pinned, chosen: chosen,
							}).Size(state.hostChipWidth(c.Canvas(), index), hostHeaderRowHeight))
						}
						if state.hostFilterHidden < len(state.hostChips) {
							hiddenChosen := false
							if state.hostFilterOn {
								for _, index := range state.hostChips[state.hostFilterHidden:] {
									if _, _, chosen := state.hostChipView(index); chosen {
										hiddenChosen = true
									}
								}
							}
							chrome.chips = append(chrome.chips, state.declareFilterChip(c, filterChip{
								id: hitHostFilterMore, label: m.HostFilterMore, chosen: hiddenChosen,
							}).Size(state.filterChipWidth(c.Canvas(), m.HostFilterMore, ""), hostHeaderRowHeight))
						}
					})
				}
				ui.Spacer(c)
				if plan.showGroupControls {
					ui.Row(c).Gap(2).Children(func() {
						expand, collapse := state.hostGroupsExpansionActions()
						b.groupControls[0] = state.declareIconButton(c, hitHostGroupsExpandAll, iconChevronDown, m.HostGroupActions.ExpandAll, !expand, iconButtonQuiet).Size(26, 26)
						b.groupControls[1] = state.declareIconButton(c, hitHostGroupsCollapseAll, iconChevronRight, m.HostGroupActions.CollapseAll, !collapse, iconButtonQuiet).Size(26, 26)
					})
				}
				viewControlsWidth := float32(56 + 6 + 26 + 6 + 26)
				b.toggle = ui.Slot(c).Size(viewControlsWidth, hostHeaderRowHeight).MarginSides(0, 0, 0, 2)
				chrome.hostToggle = hostToggleBoxes{}
				b.toggle.Children(func() {
					ui.Row(c).Fill().Items(ui.Center).Children(func() {
						ui.Row(c).Width(54).Gap(2).Children(func() {
							listStyle, gridStyle := iconButtonQuiet, iconButtonQuiet
							if state.hostView == hostViewList {
								listStyle = iconButtonActive
							} else {
								gridStyle = iconButtonActive
							}
							chrome.hostToggle.list = state.declareIconButton(c, hitHostViewList, iconRows2, state.messages.Tools.HostViewList, false, listStyle).Size(26, 26)
							chrome.hostToggle.grid = state.declareIconButton(c, hitHostViewGrid, iconHome, state.messages.Tools.HostViewGrid, false, gridStyle).Size(26, 26)
						})
						privacyIcon, privacyStyle := uint8(iconEye), iconButtonQuiet
						if state.hostPrivacy.enabled {
							privacyIcon, privacyStyle = iconEyeOff, iconButtonActive
						}
						chrome.hostToggle.privacy = state.declareIconButton(c, hitHostPrivacy, privacyIcon, state.messages.Tools.HostPrivacy, false, privacyStyle).Size(26, 26).MarginSides(0, 0, 0, 6)
						orderStyle := iconButtonQuiet
						if state.hostOrder != hostOrderManual {
							orderStyle = iconButtonActive
						}
						// The far-right control is the display-order menu, wearing the
						// manual-sort mark; manual moves are made by dragging a host.
						chrome.hostToggle.order = state.declareIconButton(c, hitHostOrder, iconTransfer, state.messages.HostCatalog.Order, false, orderStyle).Size(26, 26).MarginSides(0, 0, 0, 6)
					})
				})
			})
			if plan.compactHostControls {
				ui.Row(c).Height(hostHeaderRowHeight).MarginSides(hostHeaderRowGap, 0, 0, 0).PadSides(0, rowPad, 0, rowPad).Gap(8).Items(ui.Center).Children(func() {
					b.selectAll = ui.Slot(c).Size(40, hostHeaderRowHeight).Shrink(0)
					ui.Spacer(c)
				})
			}
			if plan.anySelected {
				ui.Row(c).Height(hostHeaderRowHeight).MarginSides(hostHeaderRowGap, 0, 0, 0).PadSides(0, rowPad, 0, rowPad).Gap(hostHeaderRowGap).Items(ui.Center).Children(func() {
					actionsLeft := -quietPadding
					if plan.actionsCondensed {
						actionsLeft = 0
					}
					ui.Row(c).Gap(actionGap).MarginSides(0, 0, 0, actionsLeft).Children(func() {
						for index := range b.buttons {
							b.buttons[index] = ui.Slot(c).Size(plan.actionWidths[index], hostHeaderRowHeight)
						}
					})
					ui.Spacer(c)
					ui.Text(c, state.hostSelectionText).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(pal.muted).Ellipsis().Shrink(1)
				})
			}
		})
	})
}

// paintHostsHeader paints the toolbar, persistent group filters and, while hosts
// are checked, a separate row of batch actions into the slots the header's
// declaration left. Hits are added in the order the controls were painted: Tab
// follows it.
func (state *State) paintHostsHeader(canvas *gfx.Canvas) {
	m := state.messages
	chrome := &state.chrome
	plan := chrome.headerPlan
	b := &chrome.headerBoxes
	if plan.headerHeight <= 0 {
		return
	}
	// The box filters the list as you type and is what Connect opens, so it is
	// a real field: a magnifier addon, the placeholder when empty, and the
	// platform editor once it holds text.
	value := state.search.String()
	components.PaintInputGroup(canvas, state.Theme(), b.search.Rect(), components.InputGroupSnapshot{
		ID: hitSearch, Label: m.NavSearch, Value: value, Placeholder: m.SearchPlaceholder,
		Disabled:      state.hostSort.enabled,
		NativeEditing: !state.hostSort.enabled && canvas.Focused() == hitSearch && value != "",
		InlineStart:   components.InputGroupSlot{Kind: components.InputGroupSlotIcon, Icon: lucideIcons[iconSearch]},
	})
	// Connect needs something to connect to.
	chrome.importButton = b.importButton.Rect()

	state.hostOrderButton = chrome.hostToggle.order.Rect()
	state.hostPrivacy.button = chrome.hostToggle.privacy.Rect()
	state.paintHostScopeCheckbox(canvas, hitHostSelectAll, b.selectAll.Rect(), state.hostScopeSelection.all)
	if plan.showChips {
		state.paintHostFilterChips(canvas, chrome.chips)
	}
	if plan.anySelected {
		for index, action := range plan.actions {
			theme := state.Theme().WithControlHeight(hostHeaderRowHeight).WithFontSize(metrics.FontSizeS)
			if action.danger {
				theme.Foreground = theme.Danger
			}
			button := components.BeginButton(canvas, theme, action.id, b.buttons[index].Rect())
			if plan.actionsCondensed {
				button = button.WithIcon(lucideIcons[action.icon]).Icon()
			}
			if !action.enabled {
				button = button.Disabled()
			}
			button.Ghost(action.label)
			if action.id == hitDashboardAddHosts {
				state.dashboard.addAnchor = b.buttons[index].Rect()
			}
			if action.id == hitMoveHostGroup {
				state.groupMenu.button = b.buttons[index].Rect()
			}
		}
	}
}

// paintImportMenu lists the three ways to bring hosts in under the Import
// button, right-aligned with it.
func (state *State) paintImportMenu(canvas *gfx.Canvas, viewport gfx.Rect) {
	m := state.messages
	menu := components.BeginDropdownMenu(canvas, state.Theme(), state.popupBelow(canvas, state.chrome.importButton, 200, 4*32+8+9, ui.End),
		components.DropdownMenuOptions{Layer: dialogLayer, Scope: dialogScope, DismissID: hitImportMenuDismiss, Viewport: viewport, Scroll: state.menuScrollFor(hitImportMenuDismiss)})
	state.menuPlaced(menu.Bounds())
	menu.AddItem(components.DropdownMenuItem{ID: hitImportSessions, Label: m.Tools.ImportSessions})
	menu.AddItem(components.DropdownMenuItem{ID: hitImportHostCatalog, Label: m.ImportCatalog})
	menu.AddItem(components.DropdownMenuItem{ID: hitImportSSHConfig, Label: m.SSHConfigImport})
	// With nothing checked the row of actions is away, and exporting the whole
	// catalog lives next to importing one.
	menu.AddSeparator()
	menu.AddItem(components.DropdownMenuItem{ID: hitExportHosts, Label: m.ExportHosts, Disabled: len(state.hosts) == 0})
	menu.End()
}

// popupBelow is where a menu width wide and height tall hangs under anchor, 4
// below it, from its left edge (ui.Start) or its right edge (ui.End).
func (state *State) popupBelow(_ *gfx.Canvas, anchor gfx.Rect, width, height float32, edge ui.Align) gfx.Rect {
	x := anchor.X
	if edge == ui.End {
		x = anchor.X + anchor.Width - width
	}
	return gfx.Rect{X: x, Y: anchor.Y + anchor.Height + 4, Width: width, Height: height}
}

// popupAt is where a menu width wide and height tall opens at a point of the
// window: at the point, or pushed back inside the window's edge, with edge to spare.
func (state *State) popupAt(_ *gfx.Canvas, viewport gfx.Rect, at gfx.Point, width, height, edge float32) gfx.Rect {
	x := max(min(at.X, viewport.Width-edge-width), 0)
	y := max(min(at.Y, viewport.Height-edge-height), 0)
	return gfx.Rect{X: viewport.X + x, Y: viewport.Y + y, Width: width, Height: height}
}

// fitHostChips decides how many group chips fit in room, leaving the rest to the
// More menu: hostFilterHidden is the position in hostChips of the first one left out.
func (state *State) fitHostChips(canvas *gfx.Canvas, room float32) {
	const gap = float32(6)
	state.fitChipGroups()
	moreWidth := state.filterChipWidth(canvas, state.messages.HostFilterMore, "")
	state.hostFilterHidden = len(state.hostChips)

	// Do they all fit? If not, room is kept for More.
	used := state.filterChipWidth(canvas, state.messages.HostFilterAll, state.hostTotalDigits)
	for _, index := range state.hostChips {
		used += gap + state.hostChipWidth(canvas, index)
	}
	limit := room
	if used > room {
		limit = room - moreWidth - gap
	}
	x := state.filterChipWidth(canvas, state.messages.HostFilterAll, state.hostTotalDigits) + gap
	for position, index := range state.hostChips {
		width := state.hostChipWidth(canvas, index)
		if x+width > limit {
			state.hostFilterHidden = position
			return
		}
		x += width + gap
	}
}

// filterChipWidth is a chip's width: its label, the count after it and room
// around both.
func (state *State) filterChipWidth(canvas *gfx.Canvas, label, digits string) float32 {
	width := 2*float32(12) + canvas.TextWidthWeight(label, metrics.FontSizeS, gfx.WeightSemibold)
	if digits != "" {
		width += 6 + canvas.TextWidthWeight(digits, metrics.FontSizeS, gfx.WeightMedium)
	}
	return width + 2
}

// hostChipWidth is the width of the chip of the group at index: its label, its
// count, and room for the chevron that opens its nested groups.
func (state *State) hostChipWidth(canvas *gfx.Canvas, index int) float32 {
	label, digits, _ := state.hostChipView(index)
	width := state.filterChipWidth(canvas, label, digits)
	if state.hostGroups[index].Pinned {
		width += chipPinWidth
	}
	if state.hostChipHasMenu(index) {
		width += chipChevronWidth
	}
	return width
}

// chipPinWidth is what the pin marker adds to a chip: the gap before it and its glyph.
const chipPinWidth float32 = 18

// chipChevronWidth is what a chevron adds to a chip: the gap before it and its glyph.
const chipChevronWidth = 18

// filterChip is one chip to declare: what it shows, the control it answers on,
// the second target that opens its menu, and the two states it can be in. Named
// fields keep those two states from being swapped by a caller.
type filterChip struct {
	id, chevron gfx.ID
	label       string
	digits      string
	pinned      bool
	chosen      bool
}

// declareFilterChip declares one chip in the page that lays the row out: a pill
// that is filled while it is the choice. A pinned chip carries an accent star
// before its name. A chip with a chevron ID has a second target at its end that
// opens a menu.
func (state *State) declareFilterChip(c ui.C, chip filterChip) ui.Box {
	pal := state.pal
	foreground := pal.muted
	var fill gfx.Color
	if chip.chosen {
		fill, foreground = pal.surfaceHot, pal.text
	}
	chipBox := ui.ButtonBase(c, chip.label).Radius(999).Shrink(0).PadSides(0, 14, 0, 12).Gap(6).Items(ui.Center).
		Background(fill).
		HitWith(chip.id, chip.label, gfx.HitOptions{Role: gfx.RoleButton, Selected: chip.chosen}).
		Focus(ui.Look{}.Ring(1.5, pal.accent))
	if !chip.chosen {
		chipBox = chipBox.Hover(ui.Look{}.Background(pal.surfaceSoft)).
			Pressed(ui.Look{}.Background(pal.surfaceHot))
	}
	chipBox.Children(func() {
		if chip.pinned {
			ui.Icon(c, lucideIcons[iconStar]).Size(12, 12).Color(pal.accent).Shrink(0)
		}
		ui.Text(c, chip.label).FontSize(metrics.FontSizeS).Weight(gfx.WeightSemibold).Color(foreground)
		if chip.digits != "" {
			ui.Text(c, chip.digits).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(pal.faint)
		}
		if !chip.chevron.IsZero() {
			tint := foreground
			if state.chipMenu.open {
				tint = pal.text
			}
			chevron := ui.Icon(c, lucideIcons[iconChevronDown]).Size(12, 12).Color(tint).Shrink(0)
			chevron.HitOutset(12, 12, 12, 4).
				HitWith(chip.chevron, chip.label, gfx.HitOptions{Role: gfx.RoleButton, Expanded: state.chipMenu.open}).
				Hover(ui.Look{}.Color(pal.text))
		}
	})
	return chipBox
}

// paintHostFilterChips keeps what the chips' boxes say after the page laid them
// out: the place the open menu hangs under, the More button a menu opens from,
// and the secondary presses the group rows answer.
func (state *State) paintHostFilterChips(canvas *gfx.Canvas, chips []ui.Box) {
	for position, index := range state.hostChips[:state.hostFilterHidden] {
		if state.hostChipHasMenu(index) && state.chipMenu.open && state.chipMenu.root == state.hostGroups[index].Name {
			state.chipMenu.anchor = chips[1+position].Rect()
		}
		state.openHostGroupActionsOn(canvas, hitHostFilterList.At(index), index)
	}
	if state.hostFilterHidden < len(state.hostChips) {
		state.filterMoreButton = chips[1+state.hostFilterHidden].Rect()
	}
}

// paintHostFilterMenu lists the groups whose chips did not fit.
func (state *State) paintHostFilterMenu(canvas *gfx.Canvas, viewport gfx.Rect) {
	hidden := len(state.hostChips) - state.hostFilterHidden
	if hidden <= 0 {
		return
	}
	menu := components.BeginDropdownMenu(canvas, state.Theme(), state.popupBelow(canvas, state.filterMoreButton, 202, float32(32*hidden+8), ui.Start),
		components.DropdownMenuOptions{Layer: dialogLayer, Scope: dialogScope, DismissID: hitHostFilterMenuDismiss, Viewport: viewport, Scroll: state.menuScrollFor(hitHostFilterMenuDismiss)})
	state.menuPlaced(menu.Bounds())
	for _, index := range state.hostChips[state.hostFilterHidden:] {
		menu.AddItem(components.DropdownMenuItem{ID: hitHostFilterMenuList.At(index), Label: state.hostGroupLabel(state.hostGroups[index])})
		state.openHostGroupActionsOn(canvas, hitHostFilterMenuList.At(index), index)
	}
	menu.End()
}

func (state *State) paintFlatHostGroupMenu(canvas *gfx.Canvas, bounds, viewport gfx.Rect) {
	// The ungrouped bucket is in the list with an empty name; "No group" already
	// says it, so it is not offered a second time as a blank row.
	named := 0
	for _, group := range state.hostGroups {
		if group.Name != "" {
			named++
		}
	}
	height := float32(32*(named+2) + 17)
	// Under the button the action row painted, or, when a host's own menu handed
	// over, where that menu was: it takes the menu's place, flipped back inside
	// the window the same way.
	anchor := state.groupMenu
	var at gfx.Rect
	if anchor.menu {
		at = state.popupAt(canvas, viewport, anchor.point, 202, height, 6)
	} else {
		at = state.popupBelow(canvas, anchor.button, 202, height, ui.Start)
	}
	menu := components.BeginDropdownMenu(canvas, state.Theme(), at,
		components.DropdownMenuOptions{Layer: dialogLayer, Scope: dialogScope, DismissID: hitHostGroupDismiss, Viewport: viewport, Scroll: state.menuScrollFor(hitHostGroupDismiss)})
	state.menuPlaced(menu.Bounds())
	menu.AddItem(components.DropdownMenuItem{ID: hitHostGroupNone, Label: state.messages.MoveGroupNone})
	for index, group := range state.hostGroups {
		if group.Name == "" {
			continue
		}
		menu.AddItem(components.DropdownMenuItem{ID: hitHostGroupItemList.At(index), Label: group.Name, Value: group.Name})
	}
	menu.AddSeparator()
	menu.AddItem(components.DropdownMenuItem{ID: hitHostGroupNew, Label: state.messages.MoveGroupNew})
	menu.End()
}

const (
	toolbarPadding    = float32(16)
	toolbarIconGap    = float32(8)
	toolbarIcon       = float32(16)
	toolbarHeight     = float32(36) // the search box and its buttons: 13 pt labels in 36, not 12 in 40
	toolbarFont       = float32(13)
	toolbarIconButton = float32(36) // a toolbar button that shows only its icon: as wide as the row is tall
	actionIconSize    = float32(28) // a checked-row action that shows only its icon
)

// toolbarButtonWidth is the width a toolbar button needs for its label, plus
// the icon when it has one.
func toolbarButtonWidth(canvas *gfx.Canvas, label string, hasIcon bool) float32 {
	width := toolbarPadding*2 + canvas.TextWidthWeight(label, toolbarFont, gfx.WeightSemibold) + 4
	if hasIcon {
		width += toolbarIcon + toolbarIconGap
	}
	return width
}

// toolbarButtonLayout is the width a toolbar button takes in a row and the icon
// it shows there: its label and the icon it already had, or the icon alone when
// the row is condensed. compact is the icon a button that has none shows then,
// since a button without words has to say what it does some other way.
func toolbarButtonLayout(canvas *gfx.Canvas, label string, icon, compact uint8, condensed bool) (width float32, shown uint8) {
	if condensed {
		return toolbarIconButton, compact
	}
	return toolbarButtonWidth(canvas, label, icon != 0), icon
}

// paintToolbarButton draws a toolbar button whose top left corner is (x, y) and
// that is width wide, for a page that has not yet declared its button's place.
func (state *State) paintToolbarButton(canvas *gfx.Canvas, id gfx.ID, x, y, width float32, label string, icon uint8, primary, enabled, condensed bool) {
	state.paintToolbarButtonIn(canvas, id, gfx.Rect{X: x, Y: y, Width: width, Height: toolbarHeight}, label, icon, primary, enabled, condensed)
}

// paintToolbarButtonIn draws a toolbar button in bounds. Icon and label are
// centered together as one group, so the button reads as balanced at any label
// width. Condensed leaves the label out for a row too narrow to hold the words:
// it becomes the tooltip and the name assistive technology reads, so the button
// gives up its width and not its meaning.
func (state *State) paintToolbarButtonIn(canvas *gfx.Canvas, id gfx.ID, bounds gfx.Rect, label string, icon uint8, primary, enabled, condensed bool) {
	state.paintButtonSized(canvas, id, bounds, label, icon, primary, false, enabled, toolbarFont, condensed)
}

// paintButton is the workbench's one button: a filled surface with a hairline
// edge, or the accent when primary. danger keeps that look and turns the label
// red, so a destructive action is marked without becoming the loudest thing on the
// page.
func (state *State) paintButton(canvas *gfx.Canvas, id gfx.ID, bounds gfx.Rect, label string, icon uint8, primary, danger, enabled bool) {
	state.paintButtonSized(canvas, id, bounds, label, icon, primary, danger, enabled, 12, false)
}

// declareButtonSized declares the workbench's one button in a page at the place
// the caller's layout gives it; the caller sizes it. It is paintButton at
// another font size, and with the label left out when iconOnly: the label is
// then the tooltip and the accessible name. A filled surface with a hairline
// edge, or the accent when primary; danger keeps that look and turns the label
// red, so a destructive action is marked without becoming the loudest thing on
// the page.
func (state *State) declareButtonSized(c ui.C, id gfx.ID, label string, icon uint8, primary, danger, enabled bool, font float32, iconOnly bool) ui.Box {
	pal := state.pal
	background, foreground := pal.surface, pal.text
	if danger {
		foreground = pal.danger
	}
	switch {
	case !enabled:
		foreground = pal.faint
	case primary:
		background, foreground = pal.accent, pal.onAccent
	}
	border := background
	if !primary {
		border = pal.line
	}
	// Icon and label are centered together as one group, so the button reads as
	// balanced at any label width; the label has a little air after it.
	hint := ""
	if iconOnly {
		hint = label
	}
	button := ui.ButtonBase(c, label).Radius(radius.control).Shrink(0).
		Border(1, border.Over(pal.workspace)).
		Background(background.Over(pal.workspace))
	if enabled {
		hover := pal.surfaceHot
		if primary {
			hover = pal.accentHot
		}
		button = button.Hover(ui.Look{}.Background(hover.Over(pal.workspace)))
	}
	button = button.HitWith(id, label, gfx.HitOptions{Role: gfx.RoleButton, Disabled: !enabled, Hint: hint})
	button.Children(func() {
		if iconOnly {
			ui.Icon(c, lucideIcons[icon]).Color(foreground)
			return
		}
		if icon == 0 {
			ui.Text(c, label).FontSize(font).Weight(gfx.WeightSemibold).Color(foreground)
			return
		}
		ui.Icon(c, lucideIcons[icon]).Color(foreground).MarginSides(0, toolbarIconGap, 0, 0)
		ui.Text(c, label).FontSize(font).Weight(gfx.WeightSemibold).Color(foreground).MarginSides(0, 4, 0, 0)
	})
	return button
}

// paintButtonSized is paintButton at another font size, and with the label left
// out when iconOnly: the label is then the tooltip and the accessible name.
func (state *State) paintButtonSized(canvas *gfx.Canvas, id gfx.ID, bounds gfx.Rect, label string, icon uint8, primary, danger, enabled bool, font float32, iconOnly bool) {
	pal := state.pal
	background, foreground := pal.surface, pal.text
	if danger {
		foreground = pal.danger
	}
	switch {
	case !enabled:
		foreground = pal.faint
	case primary:
		background, foreground = pal.accent, pal.onAccent
		if canvas.Hovered() == id {
			background = pal.accentHot
		}
	case canvas.Hovered() == id:
		background = pal.surfaceHot
	}
	border := background
	if !primary {
		border = pal.line
	}
	// Icon and label are centered together as one group, so the button reads as
	// balanced at any label width; the label has a little air after it.
	hint := ""
	if iconOnly {
		hint = label
	}
	// Icon and label are centered together as one group, so the button reads as
	// balanced at any label width; the label has a little air after it.
	if background.Alpha > 0 {
		canvas.RoundedBorder(bounds, radius.control, 1, border.Over(pal.workspace), background.Over(pal.workspace))
	}
	style := gfx.TextStyle{Size: font, Weight: gfx.WeightSemibold, Color: foreground}
	const glyphSide = float32(16)
	switch {
	case iconOnly:
		rect := gfx.Rect{
			X: bounds.X + (bounds.Width-glyphSide)/2, Y: bounds.Y + (bounds.Height-glyphSide)/2,
			Width: glyphSide, Height: glyphSide,
		}
		canvas.DrawIcon(lucideIcons[icon], rect, foreground)
	case icon == 0:
		line := font + 7
		canvas.LabelCenter(label, gfx.Rect{X: bounds.X, Y: bounds.Y + (bounds.Height-line)/2, Width: bounds.Width, Height: line}, style)
	default:
		textWidth := canvas.TextWidthWeight(label, font, gfx.WeightSemibold)
		total := glyphSide + toolbarIconGap + textWidth + 4
		x := bounds.X + (bounds.Width-total)/2
		rect := gfx.Rect{X: x, Y: bounds.Y + (bounds.Height-glyphSide)/2, Width: glyphSide, Height: glyphSide}
		canvas.DrawIcon(lucideIcons[icon], rect, foreground)
		line := font + 7
		canvas.Label(label, gfx.Rect{X: x + glyphSide + toolbarIconGap, Y: bounds.Y + (bounds.Height-line)/2, Width: textWidth, Height: line}, style)
	}
	canvas.HitWithOptions(id, bounds, label, gfx.HitOptions{Role: gfx.RoleButton, Disabled: !enabled, Hint: hint})
}

// hostStatus is what the table says about a host: the state of its session,
// or idle when no connection has been made to it.
func (state *State) hostStatus(host Host) (string, gfx.Color) {
	kind, color := state.hostLight(host)
	switch kind {
	case lightOn:
		return state.messages.HostStateConnected, color
	case lightBusy:
		return state.messages.HostStateConnecting, color
	case lightWaiting:
		return state.messages.ConnDetailShellWaiting, color
	case lightFault:
		return state.messages.HostStateFailed, color
	default:
		return state.messages.HostStateIdle, color
	}
}

// sessionStatus says in words what state a session is in, with the color of
// its light.
func (state *State) sessionStatus(session *Session) (string, gfx.Color) {
	m := state.messages
	_, color := state.sessionLight(session)
	switch {
	case session.term != nil && session.term.ended:
		return m.HostStateIdle, color
	case session.phase == sshconn.PhaseConnected:
		if session.shellPending() {
			if session.term.shell == nil {
				return m.ConnDetailShellOpening, color
			}
			return m.ConnDetailShellWaiting, color
		}
		return m.HostStateConnected, color
	case session.phase == sshconn.PhaseFailed:
		return m.HostStateFailed, color
	case session.phase == sshconn.PhaseClosed:
		return m.HostStateIdle, color
	}
	return m.HostStateConnecting, color
}

// paintHostGridTable paints the card grid's scroll bar over the page's
// declaration. The cards keep Motty's own scroll position and paint themselves
// in the page; only the bar is painted after it. Only the list view is a
// framework Tree.
func (state *State) paintHostGridTable(canvas *gfx.Canvas, treeBounds gfx.Rect) {
	// The cards were declared at this scroll position; the bar is Viva's scroll
	// area, and it paints nothing at all while the content fits.
	area, geometry := hostListScrollArea(canvas, state.Theme(), treeBounds, state.hostsContent, state.hostsScroll, state.hostListBarRevealed())
	state.hostsGridViewport = geometry.Viewport
	area.EndContent()
}

// paintHostListTable paints what the page left for the list view after its
// declaration: the scroll bar beside the rows and the focus a reveal asked for.
// The rows themselves are the page's Tree, already painted there.
func (state *State) paintHostListTable(canvas *gfx.Canvas, treeBounds gfx.Rect) {
	list := &state.hostList
	if !list.area.Valid() {
		state.hostsContent = state.hostListHeight(treeBounds.Width)
		return
	}
	content := state.hostListHeight(treeBounds.Width)
	state.hostsContent = content
	if list.scroll != nil {
		content = list.scroll.Content
		state.hostsScroll, state.hostsContent = list.scroll.Offset, content
	}
	list.applied = state.hostsScroll
	viewport := list.area.Rect()
	list.viewport = viewport
	if state.hostRevealID != 0 {
		state.focusHostListRow(canvas, state.hostRevealID)
		state.hostRevealID = 0
	}
	// The bar is the one the list has always painted: it keeps its strip out of
	// the rows and shows the Tree's offset over the list's content.
	state.paintHostListBar(canvas, treeBounds, content)
}

// paintHostListBar paints the bar the list view keeps beside its rows.
func (state *State) paintHostListBar(canvas *gfx.Canvas, treeBounds gfx.Rect, content float32) {
	bar, _ := hostListScrollArea(canvas, state.Theme(), treeBounds, content, state.hostsScroll, state.hostListBarRevealed())
	bar.EndContent()
}

// declareHostListRowContent declares the content of a list row in the page: the
// group's name and count, or the host's columns. The row painter draws the
// ground and the controls into the slots this leaves.
func (state *State) declareHostListRowContent(c ui.C, d *hostRowDrawer) {
	entry := state.hostTreeRows[d.index]
	left := float32(entry.Depth) * hostTreeIndent
	if entry.Kind == hostTreeGroup {
		left += 24 // the Tree's chevron takes this before the content
	}
	contentWidth := max(state.hostList.rowWidth-left-8, 0)
	switch entry.Kind {
	case hostTreeGroup:
		state.declareHostGroupTreeRow(c, d, max(contentWidth-38, 0))
	case hostTreeHost:
		state.declareHostTreeRow(c, d, state.hostList.rowWidth, left, state.hosts[entry.HostIndex])
	}
}

// declareHostGroupTreeRow declares a group row's content: the pin, the name and
// the count. The actions button and the selection checkbox are painted after the
// page, at the slots kept here.
func (state *State) declareHostGroupTreeRow(c ui.C, d *hostRowDrawer, width float32) {
	pal := state.pal
	group := state.hostGroups[state.hostTreeRows[d.index].GroupIndex]
	ui.Row(c).Width(width).Height(hostGroupRowHeight).PadSides(0, 0, 0, 2).Gap(6).Items(ui.Center).Children(func() {
		d.groupCheck = ui.Slot(c).Size(40, 28).Shrink(0)
		if group.Pinned {
			ui.Icon(c, lucideIcons[iconStar]).Size(14, 14).Color(pal.accent)
		}
		label := group.Label
		if group.Name == "" {
			label = state.messages.HostGroupDefault
		}
		ui.Text(c, label).FontSize(metrics.FontSizeS).Weight(gfx.WeightSemibold).Color(pal.text).LineHeight(23).Ellipsis().Shrink(1)
		ui.Text(c, group.Count).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(pal.faint).LineHeight(21)
	})
}

// declareHostTreeRow declares a host row's columns in the page: the checkbox or
// sort grip, the avatar, the name and subtitle, the address, the tags and the
// status. The widths follow the row's width exactly as the row painter did when
// it laid them out itself.
func (state *State) declareHostTreeRow(c ui.C, d *hostRowDrawer, rowWidth, left float32, host Host) {
	pal, canvas := state.pal, c.Canvas()
	const (
		statusLeast = float32(124) // the status in words, with the light before it
		lightWidth  = float32(7)   // the size of the connection light
		nameLeast   = float32(170)
	)
	leadWidth := float32(92) // the leading control, the system and the gaps before the name
	indent := left
	// A row too narrow to hold the name beside the status in words shows the
	// light alone: the name is what the row is for.
	statusWidth := statusLeast
	if rowWidth-statusLeast-indent-leadWidth < nameLeast {
		statusWidth = lightWidth
	}
	statusWords := statusWidth > lightWidth
	statusStart := rowWidth - statusWidth - indent
	nameWidth := min(max(rowWidth*0.24, nameLeast), max(statusStart-leadWidth-8, nameLeast/2))
	addressStart := min(max(rowWidth*0.37, float32(330)), max(statusStart-180, leadWidth+nameWidth+8))
	tagsStart := max(rowWidth*0.65, float32(560))
	addressWidth := max(min(rowWidth*0.25, statusStart-addressStart-8), 0)
	tagsWidth := min(rowWidth*0.18, statusStart-tagsStart-8)
	status, _ := state.hostStatus(host)
	checkMargin := 46 - 10 - components.CheckboxExtent(state.Theme())
	protocolLabel := state.hostProtocolTagLabel(host.Protocol)
	protocolWidth := canvas.TextWidthWeight(protocolLabel, metrics.FontSizeS, gfx.WeightMedium) + 14
	showProtocol := tagsWidth >= protocolWidth
	tagCount, tagsRoom := 0, tagsWidth
	var tagList [maxHostTags]string
	if showProtocol {
		tagCount = state.fitHostTags(canvas, host, max(tagsWidth-protocolWidth-5, 0), &tagList)
	} else if tagsWidth >= 60 {
		tagCount = state.fitHostTags(canvas, host, tagsWidth, &tagList)
	}
	ui.Stack(c).Width(rowWidth).Height(hostRowHeight).Children(func() {
		ui.Row(c).Fill().Items(ui.Center).Children(func() {
			ui.Row(c).Width(addressStart).Shrink(0).Items(ui.Center).Children(func() {
				checkWidth, checkHeight := components.CheckboxHitExtent(state.Theme())
				if state.hostSort.enabled {
					// Flex measures a checkbox by its full desktop interaction
					// region, not by the 16px square it paints. Reserve that exact
					// box for the grip so every following column stays pixel-stable.
					d.grip = ui.Slot(c).Size(checkWidth, checkHeight).MarginSides(0, checkMargin, 0, 10)
				} else {
					d.check = ui.Slot(c).Size(checkWidth, checkHeight).MarginSides(0, checkMargin, 0, 10)
				}
				d.avatar = ui.Slot(c).Size(36, 36).MarginSides(0, 10, 0, 0)
				ui.Column(c).Width(nameWidth).Shrink(0).Gap(2).Children(func() {
					if state.hostPrivacy.enabled {
						d.name = ui.Slot(c).Height(22)
						d.subtitle = ui.Slot(c).Height(19)
					} else {
						ui.Text(c, host.Label).FontSize(metrics.FontSizeM).Weight(gfx.WeightSemibold).Color(pal.text).LineHeight(22).Ellipsis()
						ui.Text(c, state.hostSubtitle(host)).FontSize(metrics.FontSizeS).Color(pal.muted).LineHeight(19).Ellipsis()
					}
				})
			})
			ui.Row(c).Width(tagsStart - addressStart).Shrink(0).Children(func() {
				if addressWidth < 60 { // a sliver of an address says less than nothing
					return
				}
				if state.hostPrivacy.enabled {
					d.address = ui.Slot(c).Height(22).Width(addressWidth).Shrink(0)
				} else {
					ui.Text(c, host.target).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(pal.text).LineHeight(22).Width(addressWidth).Shrink(0).Ellipsis()
				}
			})
			if showProtocol || tagCount > 0 {
				ui.Row(c).Width(tagsRoom).Shrink(0).Gap(5).Items(ui.Center).Children(func() {
					if showProtocol {
						color := hostProtocolTagColor(pal, host.Protocol)
						ui.Row(c).Height(24).PadX(7).Items(ui.Center).Radius(12).Background(withAlpha(color, 0.14)).Children(func() {
							ui.Text(c, protocolLabel).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(color)
						})
					}
					for _, tag := range tagList[:tagCount] {
						ui.Row(c).Height(24).PadX(7).Items(ui.Center).Radius(12).Background(pal.surface).Children(func() {
							ui.Text(c, tag).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(pal.muted)
						})
					}
				})
			}
		})
		ui.Row(c).Fill().Justify(ui.End).Items(ui.Center).Children(func() {
			if !statusWords {
				// The light alone, still inset from the row's right edge.
				d.light = ui.Slot(c).Size(lightWidth, lightWidth).MarginSides(0, indent, 3, 0)
				return
			}
			d.light = ui.Slot(c).Size(lightWidth, lightWidth).MarginSides(0, 8, 3, 0)
			ui.Text(c, status).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(pal.muted).LineHeight(22).Width(statusWidth-15).Shrink(0).MarginSides(0, indent, 0, 0)
		})
	})
}

// paintHostListRow paints one row of the list view inside the Tree's row. The
// Tree has already indented the content and drawn its chevron; the workbench
// paints the row's own rectangle for the columns that follow the content.
func (state *State) paintHostListRow(ctx *ui.DrawContext, d *hostRowDrawer) {
	i := d.index
	if i < 0 || i >= len(state.hostTreeRows) {
		return
	}
	entry := state.hostTreeRows[i]
	canvas := ctx.Canvas
	left := float32(entry.Depth) * hostTreeIndent
	if entry.Kind == hostTreeGroup {
		left += 24 // the Tree's chevron takes this before the content
	}
	content := ctx.Rect
	row := gfx.Rect{X: content.X - left, Y: content.Y, Width: content.Width + left + 8, Height: content.Height}
	switch entry.Kind {
	case hostTreeGroup:
		state.paintHostGroupTreeRow(canvas, row, content, entry.GroupIndex, d)
	case hostTreeHost:
		host := state.hosts[entry.HostIndex]
		list := &state.hostList
		hovered := canvas.Hovered() == list.tree.RowID(i) || canvas.Hovered() == list.tree.DisclosureID(i)
		background := state.paintHostTreeRow(canvas, row, content, host, hovered)
		state.paintHostTreeRowControls(canvas, row, host, d, background)
	}
}

// paintHostGroupTreeRow paints the contents of a group row: the pin, the name,
// the count and the group's selection checkbox, with the actions button at the
// row's right edge. The content is the page's; the checkbox and the button are
// painted into the slots the declaration left. The row itself is named by the
// Tree, which also reports a click on it and a drop onto it.
func (state *State) paintHostGroupTreeRow(canvas *gfx.Canvas, row, content gfx.Rect, groupIndex int, d *hostRowDrawer) {
	pal := state.pal
	state.groupActions.rows[groupIndex] = row
	button := gfx.Rect{X: row.X + row.Width - 34, Y: row.Y + (row.Height-28)/2, Width: 28, Height: 28}
	state.paintIconButton(canvas, hitHostGroupMore.At(groupIndex), iconEllipsis, button, state.messages.HostGroupActions.Actions, false, iconButtonQuiet, pal.workspace)
	state.paintHostScopeCheckbox(canvas, hitHostGroupCheck.At(groupIndex), d.groupCheck.Rect(), state.hostScopeSelection.groups[groupIndex])
}

// hostListTop is the base list offset below the toolbar and group filters;
// checked hosts reserve an additional batch-action row.
const hostListTop = 102

const (
	hostGroupRowHeight = 42
	hostRowHeight      = 64
	// The header's rows, shared by its paint and by the page that gives it
	// room: the filter line and, while hosts are checked, the batch actions.
	hostHeaderRowHeight = 28
	hostHeaderRowGap    = 12
)

// hostListHeight is the height of every row of the hosts list.
func (state *State) hostListHeight(width float32) float32 {
	if state.hostView == hostViewGrid {
		return state.hostGridHeight(width)
	}
	var height float32
	for _, entry := range state.hostTreeRows {
		if entry.Kind == hostTreeGroup {
			height += hostGroupRowHeight
		} else {
			height += hostRowHeight
		}
	}
	return height
}

// hostListScrollArea is how the hosts list scrolls: Viva's scroll area owns the
// bar, keeping its strip out of the rows and painting the thumb from the list's
// content height. Nothing is painted while the content fits.
func hostListScrollArea(canvas *gfx.Canvas, theme gfx.Theme, bounds gfx.Rect, content, offset float32, revealed bool) (components.ScrollArea, components.ScrollAreaGeometry) {
	// Width zero says the list has no horizontal extent of its own: it is laid out
	// to geometry.Viewport.Width, so no horizontal bar is ever due. The bar is
	// hover-revealed but keeps its reserved strip, so revealing it never moves a
	// row.
	area := components.BeginScrollArea(canvas, theme, bounds, gfx.Size{Height: content}).
		Offset(0, offset).
		ScrollbarSize(hostListBarWidth).
		ReserveScrollbars().
		HoverScrollbars(revealed).
		VerticalScrollbar(hitHostListBar, "", "")
	return area, area.Geometry()
}

// hostListBarWidth is the hosts list scroll bar's strip and thickness.
const hostListBarWidth = float32(10)

// scrollHosts moves the hosts list with the wheel. The list view's Tree is part
// of the workbench page, so the page's wheel route has already offered the
// wheel to the rows; what is left is the strip the bar keeps beside them, which
// moves the same scroll state. The grid keeps the offset the cards are painted
// at.
func (state *State) scrollHosts(event gfx.ScrollEvent) gfx.EventResult {
	if state.hostView == hostViewList {
		list := &state.hostList
		if list.scroll == nil || !pointInRect(event.X, event.Y, list.bounds) {
			return gfx.EventResult{}
		}
		result := list.scroll.Wheel(event)
		if result.Handled {
			state.hostsScroll, state.hostsContent = list.scroll.Offset, list.scroll.Content
		}
		return result
	}
	step := event.StepY(24)
	before := state.hostsScroll
	visible := state.chrome.hostList.Height
	state.hostsScroll = min(max(before+step, 0), max(state.hostsContent-visible, 0))
	return gfx.EventResult{Handled: true, Repaint: state.hostsScroll != before}
}

func (state *State) paintHostTreeRow(canvas *gfx.Canvas, row, content gfx.Rect, host Host, hovered bool) gfx.Color {
	pal := state.pal
	// The selection fill is the tree's own accent (theme.Accent is surfaceHot);
	// keep that tone underneath the connection tint so a colored selected row
	// still reads as selected, not as merely hovered.
	rowBackground := pal.workspace
	switch {
	case state.hostSortDragging(host.ID):
		rowBackground = pal.accentSoft
	case state.hostSortTarget(host.ID):
		rowBackground = pal.surfaceSoft
	case host.ID == state.selectedHostID:
		rowBackground = pal.surfaceHot
	case state.hostSortHovered(canvas, host.ID):
		rowBackground = pal.accentSoft
	case hovered:
		rowBackground = pal.surfaceSoft
	}
	hasColor := hostAccent(host.Color).Alpha > 0
	if hasColor {
		rowBackground = tintHostSurface(rowBackground, host.Color, .10)
	}
	if state.hostSortDragging(host.ID) {
		canvas.Elevate(row, radius.control, gfx.ShadowMD)
	}
	switch {
	case state.hostSortTarget(host.ID):
		canvas.RoundedBorder(row, radius.control, 2, pal.accent, rowBackground)
	case rowBackground != pal.workspace || hasColor:
		canvas.Rounded(row, radius.control, rowBackground)
	}
	if hasColor {
		paintHostColorMark(canvas, row, host.Color)
	}
	return rowBackground
}

// paintHostTreeRowControls paints a host row's selection or sort grip, its
// avatar and its connection light into the slots the page declared, and hands
// the row to the hand-drawn sort overlay.
func (state *State) paintHostTreeRowControls(canvas *gfx.Canvas, row gfx.Rect, host Host, d *hostRowDrawer, background gfx.Color) {
	if d.check.Valid() {
		side := components.CheckboxExtent(state.Theme())
		box := d.check.Rect()
		bounds := gfx.Rect{X: box.X + (box.Width-side)/2, Y: box.Y + (box.Height-side)/2, Width: side, Height: side}
		components.BeginCheckbox(canvas, state.Theme(), hitHostCheckList.At(int(host.ID)), bounds, state.hostSelected(host.ID)).Label(state.hostPrivacySemanticLabel(host)).Paint()
	}
	if state.hostSort.enabled && d.grip.Valid() {
		state.declareHostSortItem(host, row, d.grip.Rect())
	}
	if state.hostPrivacy.enabled {
		name := d.name.Rect()
		if state.hostPrivacy.nameHidden() {
			state.paintHostPrivacyMask(canvas, name, host.Label, gfx.TextStyle{Size: metrics.FontSizeM, Weight: gfx.WeightSemibold, Color: state.pal.text})
		} else {
			paintHostGridLine(canvas, host.Label, name, 22, gfx.TextStyle{Size: metrics.FontSizeM, Weight: gfx.WeightSemibold, Color: state.pal.text})
		}
		subtitle := d.subtitle.Rect()
		if state.hostPrivacy.userHidden() && state.hostPrivacySubtitleIsUser(host) {
			state.paintHostPrivacyMask(canvas, subtitle, state.hostSubtitle(host), gfx.TextStyle{Size: metrics.FontSizeS, Color: state.pal.muted})
		} else {
			paintHostGridLine(canvas, state.hostSubtitle(host), subtitle, 19, gfx.TextStyle{Size: metrics.FontSizeS, Color: state.pal.muted})
		}
		if d.address.Valid() {
			state.paintHostPrivacyTarget(canvas, d.address.Rect(), host, 22, gfx.TextStyle{Size: metrics.FontSizeS, Weight: gfx.WeightMedium, Color: state.pal.text})
		}
	}
	state.paintHostCatalogIcon(canvas, host, d.avatar.Rect(), background)
	_, dot := state.hostStatus(host)
	kind, _ := state.hostLight(host)
	lightRect := d.light.Rect()
	paintLight(canvas, lightRect.X+lightRect.Width/2, lightRect.Y+lightRect.Height/2, kind, dot, background)
}

// fitHostTags picks the tags of a host whose pills fit whole in width, in order,
// and returns how many there are.
func (state *State) fitHostTags(canvas *gfx.Canvas, host Host, width float32, shown *[maxHostTags]string) int {
	count, used := 0, float32(0)
	for remaining := host.Tags; remaining != "" && count < len(shown); {
		tag, tail, found := strings.Cut(remaining, ", ")
		pill := canvas.TextWidthWeight(tag, metrics.FontSizeS, gfx.WeightMedium) + 14
		if used+pill > width {
			break
		}
		shown[count] = tag
		count++
		used += pill + 5
		if !found {
			break
		}
		remaining = tail
	}
	return count
}

func (state *State) paintHostCatalogIcon(canvas *gfx.Canvas, host Host, bounds gfx.Rect, background gfx.Color) {
	if host.Icon == hostIconAuto {
		if host.isDesktop() && host.System == sshconn.SystemUnknown {
			canvas.Rounded(bounds, bounds.Width*0.24, state.hostBadgeFill(background))
			paintIcon(canvas, iconDesktop, insetRect(bounds, 0.24), state.pal.accent)
			return
		}
		paintSystemBadge(canvas, host.System, bounds, state.pal)
		return
	}
	if system, ok := host.Icon.system(); ok {
		paintSystemBadge(canvas, system, bounds, state.pal)
		return
	}
	kind := iconHosts
	switch host.Icon {
	case hostIconTerminal:
		kind = iconTerminal
	case hostIconSerial:
		kind = iconSerial
	case hostIconGlobe:
		kind = iconGlobe
	case hostIconKey:
		kind = iconKey
	case hostIconDesktop:
		kind = iconDesktop
	}
	canvas.Rounded(bounds, bounds.Width*0.24, state.hostBadgeFill(background))
	paintIcon(canvas, kind, insetRect(bounds, 0.24), state.pal.accent)
}

// hostBadgeFill keeps the line icon's rounded square visible on a selected grid
// card, whose own background is the same accentSoft the badge normally uses.
func (state *State) hostBadgeFill(background gfx.Color) gfx.Color {
	if background == state.pal.accentSoft {
		return state.pal.surface
	}
	return state.pal.accentSoft
}

// paintEmptyHosts is the first-run screen: no hosts are invented, the user is
// invited to add one. The page declared the state; this paints its action.
func (state *State) paintEmptyHosts(canvas *gfx.Canvas) {
	state.paintEmptyPage(canvas, iconHosts)
}

// paintNoMatches is what the list shows when the search box hides every host.
func (state *State) paintNoMatches(canvas *gfx.Canvas) {
	state.paintEmptyPage(canvas, iconSearch)
}

// lucideIcons maps the internal icon kinds to compiled Lucide icons
// generated by `viva icons` from the SVG sources in icons/.
var lucideIcons = [...]gfx.Icon{
	iconHosts:    icons.Server,
	iconDesktop:  icons.Monitor,
	iconHelp:     icons.CircleHelp,
	iconKey:      icons.Key,
	iconGlobe:    icons.Globe,
	iconForward:  icons.ArrowRightLeft,
	iconKnown:    icons.BookOpen,
	iconSettings: icons.Settings,
	iconSearch:   icons.Search,
	iconTerminal: icons.Terminal,
	iconSerial:   icons.Serial,
	iconPlus:     icons.Plus,
	iconSun:      icons.Sun,
	iconMoon:     icons.Moon,
	iconClose:    icons.X,
	iconHome:     icons.Grid2x2,
	iconCheck:    icons.Check,
	iconPause:    icons.Pause,

	iconFile:       icons.File,
	iconLink:       icons.Link,
	iconUpload:     icons.Upload,
	iconDownload:   icons.Download,
	iconFolderUp:   icons.FolderUp,
	iconFolderPlus: icons.FolderPlus,
	iconPencil:     icons.Pencil,
	iconTrash:      icons.Trash2,
	iconLock:       icons.Lock,
	iconRefresh:    icons.RotateCw,
	iconArrowUp:    icons.ArrowUp,
	iconFolderOpen: icons.FolderOpen,
	iconActivity:   icons.Activity,
	iconRecord:     icons.CircleDot,
	iconRecordStop: icons.CircleStop,
	iconPlay:       icons.Play,
	iconStop:       icons.CircleStop,
	iconFilm:       icons.Film,
	iconFileDown:   icons.FileDown,
	iconColumns2:   icons.Columns2,
	iconRows2:      icons.Rows2,
	iconGrid2x2:    icons.Grid2x2,

	iconBot:            icons.Bot,
	iconHistory:        icons.History,
	iconCopy:           icons.Copy,
	iconChevronRight:   icons.ChevronRight,
	iconChevronLeft:    icons.ChevronLeft,
	iconChevronDown:    icons.ChevronDown,
	iconSliders:        icons.SlidersHorizontal,
	iconAlert:          icons.CircleAlert,
	iconCircleCheck:    icons.CircleCheck,
	iconRadio:          icons.RadioTower,
	iconKeyboard:       icons.Keyboard,
	iconStar:           icons.Star,
	iconStarSolid:      icons.StarSolid,
	iconArchive:        icons.Archive,
	iconHardDrive:      icons.HardDrive,
	iconExternal:       icons.SquarePen,
	iconPlug:           icons.Plug,
	iconPaperclip:      icons.Paperclip,
	iconImage:          icons.Image,
	iconSave:           icons.Download,
	iconPaste:          icons.ClipboardPaste,
	iconSelectAll:      icons.SquareDashed,
	iconEraser:         icons.Eraser,
	iconEye:            icons.Eye,
	iconEyeOff:         icons.EyeOff,
	iconEllipsis:       icons.Ellipsis,
	iconWrench:         icons.Wrench,
	iconProcess:        icons.Process,
	iconDirectoryTree:  icons.PanelLeft,
	iconArrowUpDown:    icons.ArrowUpDown,
	iconCloseOthers:    icons.ListX,
	iconCloseRight:     icons.ArrowRightToLine,
	iconSparkles:       icons.Sparkles,
	iconTransfer:       icons.ArrowUpDown,
	iconBug:            icons.Bug,
	iconInfo:           icons.Info,
	iconQQ:             icons.Qq,
	iconWeChat:         icons.Wechat,
	iconDiscord:        icons.Discord,
	iconGitHub:         icons.Github,
	iconNote:           icons.StickyNote,
	iconNotebook:       icons.Notebook,
	iconSFTPTransfer:   icons.SftpTransfer,
	iconSFTPBrowseSync: icons.SftpSync,
	iconSFTPProfiles:   icons.SftpProfiles,
	iconFileClock:      icons.FileClock,
	iconClock:          icons.Clock3,
	iconFollowFolder:   icons.LocateFixed,
}

func paintIcon(canvas *gfx.Canvas, kind uint8, bounds gfx.Rect, foreground gfx.Color) {
	if int(kind) >= len(lucideIcons) {
		return
	}
	canvas.DrawIcon(lucideIcons[kind], bounds, foreground)
}

// dialogContentHeight is the height a dialog's surface takes when it fits a body
// of bodyHeight: the same number components.Dialog's FitBody works out, clamped
// to the frame. Pass it as ui.DialogSpec.Height so the body keeps exactly the
// height the old components.Dialog gave it, whatever its declared children come
// to.
func dialogContentHeight(canvas *gfx.Canvas, theme gfx.Theme, viewport gfx.Rect, width float32, description string, bodyHeight float32) float32 {
	layout := components.BeginDialog(canvas, theme, viewport, components.DialogOptions{
		ContentSize: gfx.Size{Width: width}, FitBody: true, BodyHeight: bodyHeight,
	}).Layout(description)
	return layout.Content.Height
}

// dialogContentRect is dialogContentHeight's layout: where the dialog's header,
// body and footer go, for a caller that paints into the body besides declaring
// it.
func dialogContentRect(canvas *gfx.Canvas, theme gfx.Theme, viewport gfx.Rect, width float32, description string, bodyHeight float32) components.DialogLayout {
	return components.BeginDialog(canvas, theme, viewport, components.DialogOptions{
		ContentSize: gfx.Size{Width: width}, FitBody: true, BodyHeight: bodyHeight,
	}).Layout(description)
}

// dialogPageOwns reports whether target is declared in the application's dialog
// scope, where ui.Dialog names the contents of a dialog the workbench page
// declares.
func dialogPageOwns(target gfx.ID) bool {
	return !target.IsZero() && tree.Within(target.Scope(), dialogScope.Scope())
}

// dialogButton declares one of a Dialog footer's buttons with the look
// components.ButtonAction gave it, as a page-owned control: the page names it,
// so the keyboard and the event pass reach it, and Clicked reports the press.
func dialogButton(c ui.C, label string, variant components.ButtonVariant, disabled bool) ui.Box {
	button := ui.Button(c, label)
	switch variant {
	case components.ButtonSecondary:
		button = button.Secondary()
	case components.ButtonOutline:
		button = button.Outline()
	case components.ButtonGhost:
		button = button.Ghost()
	case components.ButtonDestructive:
		button = button.Destructive()
	case components.ButtonDestructiveOutline:
		button = button.DestructiveOutline()
	}
	return button.Disabled(disabled)
}

// dialogFooterScratch keeps the state a dialog footer paints from: the
// actions of the open dialog, and the handler that paints them. The handler is
// made once, so a warm repaint declares the footer without allocating.
type dialogFooterScratch struct {
	actions []components.ButtonAction
	draw    func(*ui.DrawContext)
}

// dialogActionFooter paints a footer whose buttons keep the application's own
// identities, for a dialog that opens with one of them focused: DialogSpec's
// InitialFocusID names a hit, and the page names its own buttons, so these
// stay hand-painted beside the page's slots as the dialog's other controls are.
// Their actions stay in the central click dispatcher.
func (state *State) dialogActionFooter(c ui.C, actions ...components.ButtonAction) {
	scratch := &state.chrome.dialogFooter
	if cap(scratch.actions) < len(actions) {
		scratch.actions = make([]components.ButtonAction, len(actions))
	}
	scratch.actions = scratch.actions[:len(actions)]
	copy(scratch.actions, actions)
	if scratch.draw == nil {
		scratch.draw = state.drawDialogActions
	}
	ui.Slot(c).Fill().DrawOver(scratch.draw)
}

// drawDialogActions paints the open dialog's hand-painted footer.
func (state *State) drawDialogActions(d *ui.DrawContext) {
	components.BeginButtonRow(d.Canvas, *d.Theme, d.Rect).Size(components.ButtonSizeDefault).Paint(state.chrome.dialogFooter.actions...)
}

// dialogViewport is the frame the dialogs are centered in.
func dialogViewport(c ui.C) gfx.Rect {
	return gfx.Rect{Width: c.Bounds().Width, Height: c.Bounds().Height}
}

// declareDialogs declares the open modal dialog of the form family in the
// workbench page, as the palette is declared: the page owns its body and
// footer, and the application keeps the identities the central click handler
// dispatches.
func (state *State) declareDialogs(c ui.C) {
	switch state.modal {
	case modalOpenLink:
		state.declareLaunchDialog(c)
	case modalStateNotice:
		state.declareStateNoticeDialog(c)
	case modalShortcutCapture:
		state.declareShortcutCaptureDialog(c)
	case modalWorkspaceTemplateDelete:
		state.declareWorkspaceTemplateDeleteDialog(c)
	case modalKeyPassphrase:
		state.declareKeyPassphraseDialog(c)
	case modalRenameTab:
		state.declareTabRenameDialog(c)
	case modalAIRename:
		state.declareAIRenameDialog(c)
	case modalHostImportResult:
		state.declareHostImportResult(c)
	case modalFileProperties:
		state.declareFileProperties(c)
	case modalLocalProxy:
		state.declareLocalProxyDialog(c)
	case modalUpdate:
		state.declareUpdateDialog(c)
	case modalUpdateProgress:
		state.declareUpdateProgress(c)
	case modalNewHost:
		state.declareHostDialog(c)
	case modalTerminalInput:
		state.declareTerminalInputConfirm(c)
	case modalRemoteClipboard:
		state.declareRemoteClipboardDialog(c)
	case modalFileTransfer:
		state.declareFileTransferDialog(c)
	case modalDeleteHost:
		state.declareDeleteHostDialog(c)
	case modalMoveHostGroup:
		state.declareMoveHostGroupDialog(c)
	case modalHostKey:
		state.declareHostKeyDialog(c)
	case modalPassword:
		state.declarePasswordDialog(c)
	case modalCloseTabs:
		state.declareCloseTabsDialog(c)
	case modalRemoteTools:
		state.declareRemoteToolsModal(c)
	case modalSudoSFTP:
		state.declareSudoSFTPDialog(c)
	case modalForm:
		state.declareFormDialog(c)
	case modalFontPicker:
		state.declareFontPicker(c)
	case modalOutboundProxy:
		state.declareOutboundProxyDialog(c)
	case modalAutofillRule:
		state.declareAutofillDialog(c)
	case modalAutofillConfirm:
		state.declareAutofillConfirm(c)
	case modalToolbarSettings:
		state.declareToolbarSettings(c)
	case modalHighlightRule:
		state.declareHighlightDialog(c)
	case modalNewForward:
		state.declareForwardDialog(c)
	case modalNewProxy:
		state.declareProxyDialog(c)
	case modalXYTransfer:
		state.declareXYDialog(c)
	case modalSnippet:
		state.declareSnippetDialog(c)
	case modalSnippetDelete:
		state.declareSnippetDeleteDialog(c)
	case modalSnippetVariables:
		state.declareSnippetVariablesDialog(c)
	case modalVaultUnlock:
		state.declareVaultUnlockDialog(c)
	case modalVaultForget:
		state.declareVaultForgetDialog(c)
	}
}

// declareHostDialog declares the New/Edit Host form as a dialog of the workbench
// page.
func (state *State) declareHostDialog(c ui.C) {
	m := state.messages
	title, submit := m.DialogTitle, m.AddHost
	switch state.hostDialogMode {
	case hostDialogEdit:
		title, submit = m.EditHostTitle, m.SaveHost
	case hostDialogCopy:
		title, submit = m.CopyHostTitle, m.AddHost
	}
	form := &state.editor.form
	form.count = 0
	if form.lastError != state.dialogError {
		form.lastError = state.dialogError
		if state.dialogError != "" {
			form.offset = state.hostFormHeight()
		}
	}
	// Not a menu: a press on the dimmed area beside the form does nothing, so a
	// half-filled form is not lost by a click beside it. Escape, the close button
	// and Cancel still cancel it.
	viewport := dialogViewport(c)
	bodyHeight := state.hostFormHeight()
	height := dialogContentHeight(c.Canvas(), *c.Theme(), viewport, 680, "", bodyHeight)
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: title, Width: 680, Height: height, Layer: dialogLayer,
		Scope: dialogScope, CloseID: hitDialogClose, InitialFocusID: hitDialogLabel,
	})
	dialog.Body(func() { state.declareHostFormBody(c, title) })
	dialog.Footer(func() { state.declareHostDialogFooter(c, m.Cancel, submit) })
}

// declareHostDialogFooter declares the form's footer: the Test connection
// button and what the last test found at the left, and the dialog's own
// buttons against the right edge. The buttons keep the application's
// identities because they are stops of the form's own tab ring (see
// hostFormKey), which matches the focused identity against them; the page
// owns the footer slot they are painted into.
func (state *State) declareHostDialogFooter(c ui.C, cancel, submit string) {
	chrome := &state.chrome
	chrome.hostDialogCancelLabel, chrome.hostDialogSubmitLabel = cancel, submit
	if chrome.hostDialogFooterDraw == nil {
		chrome.hostDialogFooterDraw = state.drawHostDialogFooter
	}
	ui.Slot(c).Fill().DrawOver(chrome.hostDialogFooterDraw)
}

// drawHostDialogFooter paints the footer the page laid out: the buttons against
// the right edge and the test control at the left.
func (state *State) drawHostDialogFooter(d *ui.DrawContext) {
	chrome := &state.chrome
	actions := [...]components.ButtonAction{
		{ID: hitDialogCancel, Label: chrome.hostDialogCancelLabel, Variant: components.ButtonSecondary},
		{ID: hitDialogSubmit, Label: chrome.hostDialogSubmitLabel},
	}
	components.BeginButtonRow(d.Canvas, *d.Theme, d.Rect).Size(components.ButtonSizeDefault).Paint(actions[:]...)
	state.paintHostTest(d.Canvas, *d.Theme, d.Rect, chrome.hostDialogCancelLabel, chrome.hostDialogSubmitLabel)
	form := &state.editor.form
	if state.hostTestAvailable() {
		form.addStop(hitDialogTest, d.Rect, false)
	}
	form.addStop(hitDialogCancel, d.Rect, false)
	form.addStop(hitDialogSubmit, d.Rect, false)
}

// hostFormScrollGeometry is where the form's scroll area lies inside the dialog
// body, for the body rect the dialog's layout gives.
func (state *State) hostFormScrollGeometry(body gfx.Rect) components.ScrollAreaGeometry {
	area := components.BeginScrollArea(nil, state.Theme(), body, gfx.Size{Height: state.hostFormHeight()}).
		Offset(0, state.editor.form.offset)
	return area.Geometry()
}

// hostFormChoice declares one labelled Choice of a form page: the field title
// the form's hand-painted fields show above their control, then the Choice. The
// returned Box is the control, for the caller's Changed handling.
func hostFormChoice[T comparable](c ui.C, title string, height float32, value *T, choices []T, label func(T) string) ui.Box {
	theme, metrics := c.Theme(), c.Theme().Metrics
	var choice ui.Box
	ui.Column(c).Height(height).Children(func() {
		ui.Text(c, title).FontSize(metrics.FontSizeM).Weight(gfx.WeightMedium).Color(theme.Foreground).LineHeight(metrics.FontSizeM + 4).Ellipsis()
		choice = ui.Choice(c, value, choices, label).Height(metrics.ControlHeight).MarginSides(metrics.GapItem/2, 0, 0, 0)
	})
	return choice
}

// declareHostFormBody lays out the host form: the protocol choice, the fields,
// and the sections whose controls are painted into the slots by hand. The
// controls stay painted by hand so their focus order, clip and invalid looks
// are the ones they already have. The body scrolls in the dialog's own height,
// as the form's scroll area did.
func (state *State) declareHostFormBody(c ui.C, title string) {
	editor := &state.editor
	form := &editor.form
	b := &editor.form.body
	ssh := editor.protocol == protocolSSH
	networkHost := editor.protocol != protocolSerial
	vncHost := editor.protocol == protocolVNC || editor.protocol == protocolRDP
	rowHeight := float32(hostFormRowHeight)
	// The rows this frame lays out are the ones before a choice writes its new
	// value at the end of the declaration; the form paints the protocol and the
	// algorithm mode they were laid out for.
	form.layoutProtocol = editor.protocol
	form.algoLayout = editor.algoMode
	*b = hostFormBody{}
	scroll := state.hostFormScrollGeometry(state.hostFormBodyRect(c))
	if scroll.Viewport != form.geometry.Viewport {
		form.lastFocus = gfx.ID{}
	}
	form.geometry, form.offset = scroll, scroll.OffsetY
	chrome := &state.chrome
	chrome.hostDialogTitle = title
	if chrome.hostFormViewportDraw == nil {
		chrome.hostFormViewportDraw = state.drawHostFormViewport
		chrome.hostFormScrollbarDraw = state.drawHostFormScrollbar
		chrome.hostFormFieldsDraw = state.drawHostFormFields
	}
	viewport := ui.Column(c).Fill().Clip()
	viewport.Draw(chrome.hostFormViewportDraw)
	viewport.DrawOver(chrome.hostFormScrollbarDraw)
	viewport.Children(func() {
		content := ui.Column(c).FillWidth().Height(state.hostFormHeight()).MarginTop(-scroll.OffsetY).PadSides(2, 4, 0, 4)
		if scroll.MaxOffsetY > 0 {
			content = content.MarginRight(12)
		}
		content.Children(func() {
			b.protocol = ui.Slot(c).Height(34).MarginSides(0, 0, 10, 0)
			ui.Row(c).Height(rowHeight).Gap(8).Children(func() {
				b.label = ui.Slot(c).Grow(1)
				b.colorField = ui.Slot(c).Grow(1)
			})
			b.endpoint = ui.Slot(c).Height(float32(state.hostEndpointRows()) * rowHeight)
			state.declareHostEndpointSlots(c, b.endpoint, rowHeight)
			ui.Row(c).Height(rowHeight).Gap(8).Children(func() {
				b.group = ui.Slot(c).Grow(1)
				b.tags = ui.Slot(c).Grow(1)
			})
			ui.Row(c).Height(rowHeight).Gap(8).Children(func() {
				b.iconField = ui.Slot(c).Grow(1)
				if ssh {
					b.transfer = ui.Slot(c).Grow(1)
				}
				if state.hostShowsEncoding() {
					b.encoding = ui.Slot(c).Grow(1)
				}
			})
			if networkHost {
				// The proxy is its own row: sharing the icon row clipped both the
				// icon value and the proxy summary on VNC, RDP and Telnet.
				b.proxy = state.declareHostProxyRow(c)
			}
			if ssh {
				b.jump = state.declareHostJumpRow(c)
				b.advanced = ui.Slot(c).Height(state.advancedSectionHeight())
				advanced := b.advanced
				advanced.Children(func() {
					b.advancedHeader, b.advancedHeaderIcon = state.declareHostAdvancedHeader(c)
					if editor.advancedOpen {
						// The rows and the choice are declared as the same overlay:
						// the choice keeps its press for after the rows laid out.
						ui.Column(c).Absolute().Left(0).Right(0).Top(advancedHeaderHeight).Children(func() {
							state.declareHostAlgoRows(c)
						})
						ui.Column(c).Absolute().Left(0).Right(0).Top(advancedHeaderHeight).Height(rowHeight).Children(func() {
							b.algo = state.declareHostAlgoChoice(c)
						})
					}
				})
			}
			if vncHost {
				b.vncRows = ui.Slot(c).Height(state.desktopRowsHeight())
				b.vncRows.Children(func() { state.declareHostVNCRows(c) })
			}
			if state.dialogError != "" {
				// The serial line's error is long: up to two lines.
				ui.Paragraph(c, state.dialogError, 2).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(state.pal.danger).LineHeight(16)
			}
			// The choices are declared after the rows they sit in: the frame that
			// delivers a click lays out what it did before it, the choice writes
			// its value at the end, and the next frame draws it.
			if editor.protocol == protocolSerial {
				// The device and line rows are painted by hand; the flow control
				// takes the row under them.
				endpoint := b.endpoint
				endpoint.Children(func() {
					ui.Column(c).Absolute().Left(0).Right(0).Top(2 * rowHeight).Height(rowHeight).Children(func() {
						b.flow = state.declareHostFlowChoice(c)
					})
				})
			}
			if ssh {
				transfer := b.transfer
				transfer.Children(func() {
					b.transfer = state.declareHostTransferChoice(c)
				})
			}
			protocol := b.protocol
			protocol.Children(func() {
				b.protocol = state.declareHostProtocolChoice(c)
			})
		})
		content.DrawOver(chrome.hostFormFieldsDraw)
	})
}

// drawHostFormViewport names the scroll viewport of the host form, whose title
// the declaration keeps.
func (state *State) drawHostFormViewport(d *ui.DrawContext) {
	if d.Rect.Width > 0 && d.Rect.Height > 0 {
		d.Canvas.HitWithOptions(hitHostFormViewport, d.Rect, state.chrome.hostDialogTitle, gfx.HitOptions{Role: gfx.RoleGroup})
	}
}

// drawHostFormScrollbar paints the form's scrollbar over the viewport.
func (state *State) drawHostFormScrollbar(d *ui.DrawContext) {
	state.paintHostFormScrollbar(d.Canvas)
}

// drawHostFormFields paints the form's controls into the slots the page laid
// out.
func (state *State) drawHostFormFields(d *ui.DrawContext) {
	state.paintHostFormFields(d.Canvas)
}

// hostFormBodyRect is where the dialog lays the form's body, as the dialog
// declaration lays it out.
func (state *State) hostFormBodyRect(c ui.C) gfx.Rect {
	layout := dialogContentRect(c.Canvas(), *c.Theme(), dialogViewport(c), 680, "", state.hostFormHeight())
	return layout.Body
}

// paintHostFormScrollbar paints the form's scrollbar beside its content after
// the page laid the body out.
func (state *State) paintHostFormScrollbar(canvas *gfx.Canvas) {
	form := &state.editor.form
	area := components.BeginScrollArea(canvas, state.Theme(), form.geometry.Bounds, gfx.Size{Height: state.hostFormHeight()}).
		Offset(0, form.offset).VerticalScrollbar(hitHostFormScrollbar, state.hostDialogTitle(), "")
	area.EndContent()
}

// hostDialogTitle is the form's title, by the mode the dialog is open in.
func (state *State) hostDialogTitle() string {
	switch state.hostDialogMode {
	case hostDialogEdit:
		return state.messages.EditHostTitle
	case hostDialogCopy:
		return state.messages.CopyHostTitle
	}
	return state.messages.DialogTitle
}

// paintHostFormFields paints the form's controls into the slots the page laid
// out.
func (state *State) paintHostFormFields(canvas *gfx.Canvas) {
	editor := &state.editor
	m := state.messages
	// Which sections this frame's body laid out is fixed before the page's
	// protocol choice writes the protocol a click chose; paint those sections,
	// as the hand-painted endpoint rows paint the protocol now in effect.
	protocol := editor.form.layoutProtocol
	ssh := protocol == protocolSSH
	networkHost := protocol != protocolSerial
	vncHost := protocol == protocolVNC || protocol == protocolRDP
	b := &editor.form.body
	editor.form.clipped = true
	defer func() { editor.form.clipped = false }()
	state.paintDialogField(canvas, hitDialogLabel, fieldLabel, m.FieldLabel, m.FieldLabelPlaceholder, b.label.Rect())
	state.paintHostEndpointRows(canvas)
	state.paintHostGroupPicker(canvas, b.group.Rect())
	state.paintHostTags(canvas, b.tags.Rect())
	if b.encoding.Valid() {
		state.paintHostEncodingPicker(canvas, b.encoding.Rect())
	}
	state.paintHostIconPicker(canvas, b.iconField.Rect())
	state.paintHostColorChoice(canvas, b.colorField.Rect())
	if networkHost {
		state.paintHostProxyRow(canvas)
	}
	if vncHost {
		state.paintVNCRows(canvas)
	}
	if ssh {
		state.paintHostJumpRow(canvas)
		state.paintAdvancedSection(canvas)
	}
	// The choices are the page's controls: keep the option the tab ring reaches
	// in the form's stops, by the identity its paint reported.
	state.rememberHostChoice(canvas, b.protocol.Rect(), &editor.form.choiceIDs.protocol, state.hostProtocolChoiceLabel(editor.protocol))
	if b.flow.Valid() {
		state.rememberHostChoice(canvas, b.flow.Rect(), &editor.form.choiceIDs.flow, state.hostFlowChoiceLabel(editor.flow))
	}
	if b.transfer.Valid() {
		state.rememberHostChoice(canvas, b.transfer.Rect(), &editor.form.choiceIDs.transfer, state.hostTransferChoiceLabel(editor.transfer))
	}
	if b.algo.Valid() {
		state.rememberHostChoice(canvas, b.algo.Rect(), &editor.form.choiceIDs.algo, state.hostAlgoChoiceLabel(editor.algoMode))
	}
	// The picker's backdrop must reach the whole window, not the scrolling body
	// the form's clip covers.
	portal := state.beginHostFormPopup(canvas, state.editor.colorPicker.open)
	state.paintHostColorPicker(canvas)
	state.endHostFormPopup(canvas, portal)
}

// declareDeleteHostDialog declares the delete confirmation as a dialog of the
// workbench page.
func (state *State) declareDeleteHostDialog(c ui.C) {
	body := float32(0)
	if state.dialogError != "" {
		body = 20
	}
	viewport := dialogViewport(c)
	height := dialogContentHeight(c.Canvas(), *c.Theme(), viewport, 520, state.hostDialogText, body)
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: state.messages.DeleteHostTitle, Description: state.hostDialogText,
		Width: 520, Height: height, Layer: dialogLayer,
		Scope: dialogScope, DismissID: hitHostDeleteDismiss, CloseID: hitHostDeleteClose, InitialFocusID: hitHostDeleteCancel,
	})
	dialog.Body(func() { state.declareDeleteHostBody(c) })
	dialog.Footer(func() {
		state.dialogActionFooter(c,
			components.ButtonAction{ID: hitHostDeleteCancel, Label: state.messages.Cancel, Variant: components.ButtonSecondary},
			components.ButtonAction{ID: hitHostDeleteConfirm, Label: state.messages.DeleteHostConfirm, Variant: components.ButtonDestructive},
		)
	})
}

// declareDeleteHostBody lays out the confirmation's error line, when the host
// could not be deleted.
func (state *State) declareDeleteHostBody(c ui.C) {
	if state.dialogError == "" {
		return
	}
	ui.Column(c).Fill().Children(func() {
		ui.Text(c, state.dialogError).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(state.pal.danger).LineHeight(20)
	})
}

// declareMoveHostGroupDialog declares the move/rename group dialog as a dialog
// of the workbench page.
func (state *State) declareMoveHostGroupDialog(c ui.C) {
	if state.groupActions.op == hostGroupDelete {
		state.declareHostGroupDeleteDialog(c)
		return
	}
	layout := dialogContentRect(c.Canvas(), *c.Theme(), dialogViewport(c), 460, "", 90)
	title, submit, label := state.messages.MoveGroupTitle, state.messages.MoveGroupSubmit, state.messages.MoveGroupField
	switch state.groupActions.op {
	case hostGroupRename:
		title, submit, label = state.messages.HostGroupActions.Rename, state.messages.HostGroupActions.Save, state.messages.HostGroupActions.Name
	case hostGroupCreateChild:
		title, submit, label = state.messages.HostGroupActions.NewChild, state.messages.HostGroupActions.Create, state.messages.HostGroupActions.Name
	case hostGroupRelocate:
		title, submit, label = state.messages.HostGroupActions.Move, state.messages.MoveGroupSubmit, state.messages.HostGroupActions.Parent
	}
	description := ""
	if state.groupActions.op != hostGroupMoveHosts {
		description = elide(c.Canvas(), state.groupActions.path, c.Theme().Metrics.FontSizeM, gfx.WeightRegular, layout.Header.Width)
	}
	viewport := dialogViewport(c)
	height := dialogContentHeight(c.Canvas(), *c.Theme(), viewport, 460, description, 90)
	// No scrim dismiss: a press beside the form must not discard its input.
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: title, Description: description, Width: 460, Height: height, Layer: dialogLayer,
		Scope: dialogScope, CloseID: hitMoveGroupClose, InitialFocusID: hitMoveGroupField,
	})
	dialog.Body(func() { state.declareMoveGroupBody(c, label, viewport) })
	dialog.Footer(func() {
		if dialogButton(c, state.messages.Cancel, components.ButtonSecondary, false).Clicked() {
			c.After(func() { state.clickMoveHostGroupDialog(hitMoveGroupCancel) })
		}
		if dialogButton(c, submit, components.ButtonDefault, false).Clicked() {
			c.After(func() { state.clickMoveHostGroupDialog(hitMoveGroupSubmit) })
		}
	})
}

// declareHostGroupDeleteDialog declares the delete-group variant of the group
// dialog: two radio choices for what happens to the hosts in it.
func (state *State) declareHostGroupDeleteDialog(c ui.C) {
	m := state.messages.HostGroupActions
	layout := dialogContentRect(c.Canvas(), *c.Theme(), dialogViewport(c), 480, "", 130)
	description := elide(c.Canvas(), state.groupActions.path, c.Theme().Metrics.FontSizeM, gfx.WeightRegular, layout.Header.Width)
	viewport := dialogViewport(c)
	height := dialogContentHeight(c.Canvas(), *c.Theme(), viewport, 480, description, 130)
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: m.Delete, Description: description, Width: 480, Height: height, Layer: dialogLayer,
		Scope: dialogScope, DismissID: hitMoveGroupDismiss, CloseID: hitMoveGroupClose, InitialFocusID: hitGroupDeleteParent,
	})
	dialog.Body(func() {
		column := ui.Column(c).Fill()
		column.DrawOver(func(d *ui.DrawContext) {
			canvas, theme := d.Canvas, *d.Theme
			choices := state.groupDeleteChoices()
			fullLabels := [2]string{choices[0].Label, choices[1].Label}
			for index := range choices {
				choices[index].Label = elide(canvas, choices[index].Label, theme.Metrics.FontSizeM, gfx.WeightMedium, d.Rect.Width-32)
			}
			selected := hitGroupDeleteParent
			if state.groupActions.ungroup {
				selected = hitGroupDeleteUngroup
			}
			components.BeginRadioGroup(canvas, theme, d.Rect, choices[:], selected).Paint()
			for index := range canvas.Hits() {
				hit := &canvas.Hits()[index]
				for option := range choices {
					if hit.ID == choices[option].ID {
						hit.Label, hit.Value, hit.Hint = fullLabels[option], fullLabels[option], fullLabels[option]
					}
				}
			}
		})
	})
	dialog.Footer(func() {
		if dialogButton(c, state.messages.Cancel, components.ButtonSecondary, false).Clicked() {
			c.After(func() { state.clickMoveHostGroupDialog(hitMoveGroupCancel) })
		}
		if dialogButton(c, m.Delete, components.ButtonDestructive, false).Clicked() {
			c.After(func() { state.clickMoveHostGroupDialog(hitMoveGroupSubmit) })
		}
	})
}

// declareMoveGroupBody lays out the one field the dialog shows and paints it
// into its slot.
func (state *State) declareMoveGroupBody(c ui.C, label string, viewport gfx.Rect) {
	column := ui.Column(c).Fill()
	column.Children(func() {
		state.groupActions.slot = ui.Slot(c).Height(90)
	})
	column.DrawOver(func(d *ui.DrawContext) {
		canvas, theme := d.Canvas, *d.Theme
		slot := state.groupActions.slot.Rect()
		field := components.BeginField(canvas, theme, slot).For(hitMoveGroupField)
		if state.dialogError != "" {
			field = field.Invalid()
		}
		content := field.Paint(label, "", state.dialogError)
		value := state.hostGroupEditor.String()
		focused := canvas.Focused() == hitMoveGroupField
		content.Height = 32
		if state.groupActions.op == hostGroupRelocate {
			state.paintHostGroupParentSelect(canvas, content, viewport)
		} else {
			components.PaintInput(canvas, theme, content, components.InputSnapshot{
				ID: hitMoveGroupField, Label: label, Value: value,
				Placeholder: state.messages.FieldGroupPlaceholder, Focused: focused, PlatformEditing: focused && value != "",
			})
		}
	})
}

// paintDialogField paints one label + input row wired to its editor. The
// focused field hands text rendering to the platform editor; the others paint
// their value statically. The placeholder says what the field takes; there is
// no second caption.
// hostSystemChoices are the systems whose badge a host can be given, in the order
// the list shows them. The names are the systems' own, the same in every language.
var hostSystemChoices = [...]struct {
	system sshconn.RemoteSystem
	name   string
}{
	{sshconn.SystemUbuntu, "Ubuntu"}, {sshconn.SystemDebian, "Debian"}, {sshconn.SystemFedora, "Fedora"},
	{sshconn.SystemArch, "Arch Linux"}, {sshconn.SystemCentOS, "CentOS"}, {sshconn.SystemRedHat, "Red Hat"},
	{sshconn.SystemRocky, "Rocky Linux"}, {sshconn.SystemAlma, "AlmaLinux"}, {sshconn.SystemOpenSUSE, "openSUSE"},
	{sshconn.SystemKali, "Kali Linux"}, {sshconn.SystemMint, "Linux Mint"}, {sshconn.SystemRaspberryPi, "Raspberry Pi OS"},
	{sshconn.SystemAlpine, "Alpine"}, {sshconn.SystemNixOS, "NixOS"}, {sshconn.SystemGentoo, "Gentoo"},
	{sshconn.SystemManjaro, "Manjaro"},
	{sshconn.SystemSynology, "Synology DSM"}, {sshconn.SystemQNAP, "QNAP QTS"}, {sshconn.SystemFnOS, "fnOS"},
	{sshconn.SystemTrueNAS, "TrueNAS"}, {sshconn.SystemUnraid, "Unraid"},
	{sshconn.SystemOpenMediaVault, "OpenMediaVault"}, {sshconn.SystemProxmox, "Proxmox VE"},
	{sshconn.SystemOpenWrt, "OpenWrt"}, {sshconn.SystemDeepin, "deepin"}, {sshconn.SystemUOS, "UOS"},
	{sshconn.SystemKylin, "Kylin"},
	{sshconn.SystemPopOS, "Pop!_OS"},
	{sshconn.SystemZorin, "Zorin OS"}, {sshconn.SystemElementary, "elementary OS"},
	{sshconn.SystemVoid, "Void Linux"}, {sshconn.SystemSlackware, "Slackware"},
	{sshconn.SystemAnolis, "Anolis OS"}, {sshconn.SystemTencentOS, "TencentOS"},
	{sshconn.SystemAmazonLinux, "Amazon Linux"}, {sshconn.SystemAzureLinux, "Azure Linux"},
	{sshconn.SystemOracleLinux, "Oracle Linux"}, {sshconn.SystemESXi, "VMware ESXi"},
	{sshconn.SystemFreeBSD, "FreeBSD"}, {sshconn.SystemLinux, "Linux"},
	{sshconn.SystemWindows, "Windows"}, {sshconn.SystemDarwin, "macOS"},
}

// hostIconLabel is the name of an icon in the list.
func (state *State) hostIconLabel(icon hostIcon) string {
	m := state.messages
	if system, ok := icon.system(); ok {
		for _, choice := range hostSystemChoices {
			if choice.system == system {
				return choice.name
			}
		}
	}
	switch icon {
	case hostIconServer:
		return m.IconServer
	case hostIconTerminal:
		return m.IconTerminal
	case hostIconSerial:
		return m.IconSerial
	case hostIconGlobe:
		return m.IconGlobe
	case hostIconKey:
		return m.IconKey
	case hostIconDesktop:
		return m.IconDesktop
	default:
		return m.IconAuto
	}
}

// hostIconArt is what an icon looks like in the list: a system's badge, or a line
// icon. The automatic icon is the badge of the system the host was detected to
// run, until there is one.
func (state *State) hostIconArt(icon hostIcon, detected sshconn.RemoteSystem, desktop bool) gfx.Icon {
	if icon == hostIconAuto {
		if art, ok := systemBadgeIcon(detected, state.pal.dark); ok {
			return art
		}
		if desktop {
			return icons.Monitor
		}
		return icons.Server
	}
	if system, ok := icon.system(); ok {
		art, _ := systemBadgeIcon(system, state.pal.dark)
		return art
	}
	switch icon {
	case hostIconTerminal:
		return icons.Terminal
	case hostIconSerial:
		return icons.Serial
	case hostIconGlobe:
		return icons.Globe
	case hostIconKey:
		return icons.Key
	case hostIconDesktop:
		return icons.Monitor
	}
	return icons.Server
}

// hostIconChoices are the icons the list offers, in order.
func hostIconChoices() []hostIcon {
	choices := make([]hostIcon, 0, 1+len(hostSystemChoices)+6)
	choices = append(choices, hostIconAuto)
	for _, choice := range hostSystemChoices {
		choices = append(choices, hostIconOf(choice.system))
	}
	return append(choices, hostIconServer, hostIconTerminal, hostIconSerial, hostIconGlobe, hostIconKey, hostIconDesktop)
}

func (state *State) paintDialogField(canvas *gfx.Canvas, id gfx.ID, field hostField, title, placeholder string, bounds gfx.Rect) {
	state.paintDialogFieldWithSecurity(canvas, id, field, title, placeholder, bounds, false)
}

func (state *State) paintSecureDialogField(canvas *gfx.Canvas, id gfx.ID, field hostField, title, placeholder string, bounds gfx.Rect) {
	state.paintDialogFieldWithSecurity(canvas, id, field, title, placeholder, bounds, true)
}

func (state *State) paintDialogFieldWithSecurity(canvas *gfx.Canvas, id gfx.ID, field hostField, title, placeholder string, bounds gfx.Rect, secure bool) {
	theme := state.Theme()
	content := components.BeginField(canvas, theme, bounds).For(id).Paint(title, "", "")
	state.revealHostField(canvas, id, content)
	if id == hitDialogPassword && state.hostOffersCredential() {
		state.paintHostPasswordWithCredential(canvas, content, title, placeholder)
		return
	}
	value := state.editor.field(field).String()
	focused := canvas.Focused() == id
	state.paintSecretInput(canvas, content, components.InputSnapshot{
		ID:              id,
		Label:           title,
		Value:           value,
		Placeholder:     placeholder,
		Secure:          secure,
		Focused:         focused,
		PlatformEditing: focused && value != "",
	})
}

// declareHostKeyDialog declares the host-key question as a dialog of the
// workbench page. The safe choice, Reject, holds the initial focus.
func (state *State) declareHostKeyDialog(c ui.C) {
	m := state.messages
	session := state.promptSession()
	if session == nil {
		return
	}
	changed := session.hostKey.Changed
	title, trustLabel := m.HostKeyTitle, m.HostKeyTrust
	if changed {
		title, trustLabel = m.HostKeyChangedTitle, m.HostKeyUpdateAndContinue
	}
	saved := 0
	if changed {
		saved = len(session.hostKey.Known)
	}
	bodyHeight := float32((3+saved)*30 - 8) // address, type, fingerprint and the saved ones
	viewport := dialogViewport(c)
	height := dialogContentHeight(c.Canvas(), *c.Theme(), viewport, 540, state.hostKeyDescription, bodyHeight)
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: title, Description: state.hostKeyDescription,
		Width: 540, Height: height, Layer: dialogLayer,
		Scope: dialogScope, DismissID: hitHostKeyDismiss, CloseID: hitHostKeyClose, InitialFocusID: hitHostKeyReject,
	})
	dialog.Body(func() { state.declareHostKeyBody(c) })
	dialog.Footer(func() {
		actions := [...]components.ButtonAction{
			{ID: hitHostKeyReject, Label: m.HostKeyReject, Variant: components.ButtonSecondary},
			{ID: hitHostKeyOnce, Label: m.HostKeyOnce, Variant: components.ButtonOutline},
			{ID: hitHostKeyTrust, Label: trustLabel},
			{ID: hitHostKeyForget, Label: m.HostKeyForget, Variant: components.ButtonOutline},
		}
		count := 3
		if canForgetServerIdentity(session) {
			count++
		}
		state.dialogActionFooter(c, actions[:count]...)
	})
}

// declareHostKeyBody lays out the address, type and fingerprint of the key.
func (state *State) declareHostKeyBody(c ui.C) {
	session := state.promptSession()
	if session == nil {
		return
	}
	m, pal := state.messages, state.pal
	type keyRow struct{ label, value string }
	rows := []keyRow{
		{m.FieldAddress, session.hostKey.Address},
		{m.HostKeyType, session.hostKey.Type},
		{m.HostKeyFingerprint, session.hostKey.Fingerprint},
	}
	labelWidth := float32(90)
	if session.hostKey.Changed {
		labelWidth = 110
		for _, fingerprint := range session.hostKey.Known {
			rows = append(rows, keyRow{m.HostKeySavedFingerprint, fingerprint})
		}
	}
	ui.Column(c).Fill().Gap(8).Children(func() {
		for _, row := range rows {
			ui.Row(c).Height(22).Gap(6).Children(func() {
				ui.Text(c, row.label).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(pal.muted).LineHeight(22).Width(labelWidth).Shrink(0)
				ui.Text(c, row.value).FontSize(metrics.FontSizeS).Weight(gfx.WeightRegular).Color(pal.text).LineHeight(22)
			})
		}
	})
}

// declarePasswordDialog declares the authentication questions as a dialog of
// the workbench page, each in a field of its own and in the server's order.
func (state *State) declarePasswordDialog(c ui.C) {
	m := state.messages
	viewport := dialogViewport(c)
	bodyHeight := state.passwordBodyHeight()
	height := dialogContentHeight(c.Canvas(), *c.Theme(), viewport, 460, state.passwordDescription, bodyHeight)
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: m.PasswordTitle, Description: state.passwordDescription,
		Width: 460, Height: height, Layer: dialogLayer,
		Scope: dialogScope, DismissID: hitPasswordDismiss, CloseID: hitPasswordClose, InitialFocusID: hitPasswordField,
	})
	dialog.Body(func() { state.declarePasswordBody(c) })
	dialog.Footer(func() {
		if dialogButton(c, m.Cancel, components.ButtonSecondary, false).Clicked() {
			c.After(func() { state.clickPassword(hitPasswordCancel) })
		}
		if dialogButton(c, m.Connect, components.ButtonDefault, false).Clicked() {
			c.After(func() { state.clickPassword(hitPasswordSubmit) })
		}
	})
}

// declarePasswordBody lays out one field per question, the hint and error under
// them and, when the vault can keep it, the box that offers to save it. The
// fields themselves are painted into their slots by the body's DrawOver.
func (state *State) declarePasswordBody(c ui.C) {
	boxes := state.passwordBoxes
	if len(boxes) != len(state.passwordQuestions) {
		boxes = make([]ui.Box, len(state.passwordQuestions))
		state.passwordBoxes = boxes
	}
	multi := len(boxes) > 1
	state.chrome.passwordSave = ui.Box{}
	column := ui.Column(c).Fill().PadSides(2, 4, 0, 4)
	column.Children(func() {
		for index := range boxes {
			boxes[index] = ui.Slot(c).Height(62).MarginSides(0, 0, 4, 0)
		}
		state.hintLineC(c)
		state.problemLineC(c, state.passwordError)
		if state.secrets.canSave {
			if multi {
				state.chrome.passwordSave = ui.Slot(c).Height(20).MarginSides(12, 0, 0, 0)
			} else {
				state.chrome.passwordSave = ui.Slot(c).Height(20).MarginSides(8, 0, 0, 0)
			}
			state.chrome.passwordSave.Children(func() {
				state.declareSaveBox(c, hitPasswordSave, state.messages.PasswordSave, state.passwordHintText(), hitPasswordHint)
			})
		}
	})
	column.DrawOver(state.passwordFieldsDraw())
}

// passwordFieldsDraw returns the handler that paints the password dialog's
// fields, made once so a warm repaint allocates nothing.
func (state *State) passwordFieldsDraw() func(*ui.DrawContext) {
	if state.chrome.passwordFieldsDraw == nil {
		state.chrome.passwordFieldsDraw = state.drawPasswordFields
	}
	return state.chrome.passwordFieldsDraw
}

// drawPasswordFields paints a field per question into the slots the body
// declared. The box that offers to save the password is a page element, declared
// by declarePasswordBody.
func (state *State) drawPasswordFields(d *ui.DrawContext) {
	canvas, theme := d.Canvas, *d.Theme
	for index := range state.passwordBoxes {
		question := state.passwordQuestions[index]
		label := strings.TrimSpace(question.Text)
		if label == "" {
			label = state.messages.PasswordLabel
		}
		field := components.BeginField(canvas, theme, state.passwordBoxes[index].Rect())
		content := field.Paint(label, "", "")
		value := state.passwordFields[index].String()
		id := hitPasswordFields.At(index + 1)
		focused := canvas.Focused() == id
		state.paintSecretInput(canvas, content, components.InputSnapshot{
			ID: id, Label: label, Value: value, Secure: question.Secret,
			Focused: focused, PlatformEditing: focused && (value != "" || !question.Secret), Invalid: state.passwordError != "",
		})
	}
}

// passwordBodyHeight is tall enough for every question's field, the error line
// and the box that offers to save the password.
func (state *State) passwordBodyHeight() float32 {
	count := max(len(state.passwordQuestions), 1)
	if count == 1 {
		// One field is 62 high plus its 4px bottom margin. The hint line under
		// it, the error line, when present, and the save box follow directly;
		// no large fixed gap is reserved for an error that may never appear.
		height := float32(66)
		if !state.secrets.canSave && state.passwordHintText() != "" {
			height += 18
		}
		if state.passwordError != "" {
			height += 18
		}
		if state.secrets.canSave {
			height += 28 // 8px above the 20px save row
		}
		return height
	}
	// Several fields follow the form dialog's rhythm: 62 pixel rows four
	// apart, then the error line and the save box.
	height := float32(count*66 + 2)
	if state.passwordError != "" {
		height += 18
	}
	if state.secrets.canSave {
		height += 32
	}
	return height
}

// passwordHintText is the guidance about submitting and saving: a help icon on
// the save box when it can save, otherwise a line under the field. A
// multi-field round shows none; its labels and buttons are explanation enough.
func (state *State) passwordHintText() string {
	hint := state.messages.PasswordHint
	switch {
	case state.secrets.canSave:
	default:
		hint = state.messages.PasswordHintUnsaved
	}
	if len(state.passwordQuestions) > 1 {
		hint = ""
	}
	return hint
}

// hintLineC declares the hint row under the fields. The field's description
// slot would draw past its fixed 62 pixel slot onto the error line, so the
// hint is a row of its own.
func (state *State) hintLineC(c ui.C) {
	if state.secrets.canSave {
		return
	}
	if hint := state.passwordHintText(); hint != "" {
		ui.Text(c, hint).FontSize(metrics.FontSizeS).Weight(gfx.WeightRegular).Color(state.pal.muted).LineHeight(18)
	}
}

func (state *State) declareKeyPassphraseDialog(c ui.C) {
	m := state.messages
	viewport := dialogViewport(c)
	height := dialogContentHeight(c.Canvas(), *c.Theme(), viewport, 540, state.keyPassphraseDescription, state.secretFieldBody(state.keyPassphraseError))
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: m.KeyPassphraseTitle, Description: state.keyPassphraseDescription,
		Width: 540, Height: height, Layer: dialogLayer,
		Scope: dialogScope, DismissID: hitKeyPassphraseDismiss, CloseID: hitKeyPassphraseClose,
		InitialFocusID: hitKeyPassphraseField,
	})
	dialog.Body(func() {
		column := ui.Column(c).Fill().PadSides(2, 4, 0, 4)
		column.Children(func() {
			state.keyPassphraseField = ui.Slot(c).Height(62)
			if state.keyPassphraseError != "" {
				ui.Text(c, state.keyPassphraseError).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(state.pal.danger).LineHeight(18).MarginSides(12, 0, 0, 0)
			}
			if state.secrets.canSave {
				// The save box sits 99 pixels below the body's top, as it always has.
				if state.keyPassphraseError != "" {
					ui.Slot(c).Height(5)
				} else {
					ui.Slot(c).Height(35)
				}
				state.declareSaveBox(c, hitKeyPassphraseSave, m.KeyPassphraseSave, "", gfx.ID{})
			}
		})
		if state.keyPassphraseDraw == nil {
			state.keyPassphraseDraw = state.drawKeyPassphraseField
		}
		column.DrawOver(state.keyPassphraseDraw)
	})
	dialog.Footer(func() {
		state.dialogActionFooter(c,
			components.ButtonAction{ID: hitKeyPassphraseSkip, Label: m.KeyPassphraseSkip, Variant: components.ButtonSecondary},
			components.ButtonAction{ID: hitKeyPassphraseSubmit, Label: m.KeyUnlock},
		)
	})
}

// drawKeyPassphraseField paints the passphrase field into the slot the dialog's
// body laid out.
func (state *State) drawKeyPassphraseField(d *ui.DrawContext) {
	m := state.messages
	field := components.BeginField(d.Canvas, *d.Theme, state.keyPassphraseField.Rect())
	content := field.Paint(m.KeyPassphraseLabel, m.KeyPassphraseHint, "")
	value := state.keyPassphrase.String()
	focused := d.Canvas.Focused() == hitKeyPassphraseField
	state.paintSecretInput(d.Canvas, content, components.InputSnapshot{
		ID: hitKeyPassphraseField, Label: m.KeyPassphraseLabel, Value: value, Secure: true,
		Focused: focused, PlatformEditing: focused && value != "", Invalid: state.keyPassphraseError != "",
	})
}
