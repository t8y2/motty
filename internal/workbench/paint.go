package workbench

import (
	"math"
	"strings"
	"time"

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
	// iconGauge is the resource monitor's own glyph: the speedometer that says
	// performance, where iconActivity is the pulse of latency and live traffic.
	iconGauge
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
	iconChevronsExpand
	iconChevronsCollapse
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
	iconSnippets
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
	iconListTree
	iconFollowFolder
	// iconArrowUpDown is the command bar's order button.
	iconArrowUpDown
	iconLocalProxy
	// The solid folders mark a group in the host trees, open or closed.
	iconFolderSolid
	iconFolderSolidOpen
	iconPaneMaximize
	iconPaneRestore
	// The fullscreen desktop bar's pin: pinned, it keeps the bar visible.
	iconPin
	iconPinOff
	// The dedicated SFTP file operation glyphs: up directory, upload, upload folder, new file, new folder.
	iconSFTPUp
	iconSFTPUpload
	iconSFTPUploadDirectory
	iconSFTPNewFile
	iconSFTPNewDirectory
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
	canvas.SetTree(&tree)
	if state.appLocked() {
		state.paintAppLockPage(canvas, size)
		return
	}
	state.lockUI.active = false
	state.autosaveNotes(canvas)
	state.syncNoteWindows()
	state.workspaceCardRegions = state.workspaceCardRegions[:0]
	state.pixelScale = canvas.PixelScale()
	state.titlebarLeading, state.titlebarTrailing = canvas.TitlebarInsets()
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
	state.chrome.flags.used, state.chrome.flags.stopCount = 0, 0
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
	state.fileManagerRect = gfx.Rect{}
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
	state.aiPanelBeginPaint()
	if state.noteSidebarFocusBack && state.activeSection == sectionTerminal {
		state.noteSidebarFocusBack = false
		canvas.RequestFocus(state.activeTerminalFocusHit())
	}
	// A click that acted on the terminal (a quick-command chip, say) asks for
	// the keyboard back here: it has no editable result of its own to keep it.
	if !state.pendingFocus.IsZero() {
		// A dialog opened by the same click (a quick command with variables)
		// asks for its first field; let it keep the keyboard. The request is
		// dropped either way so a later paint does not steal it back.
		// The terminal has to be the page on screen to take the keyboard too.
		if state.modal == modalNone && state.terminalPage() {
			canvas.RequestFocusOnce(state.pendingFocus)
		}
		state.pendingFocus = gfx.ID{}
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
	state.requestPasteFrames(canvas)
	if session := state.fullscreenDesktop(); session != nil {
		state.beginMenus()
		bounds := gfx.Rect{Width: size.Width, Height: size.Height}
		state.windowPage().Draw(canvas, bounds, state.declareShell)
		state.paintDesktopDetails(canvas, session, bounds)
		state.endMenus()
		state.tooltip.Paint(canvas, state.Theme(), state.windowRect(), components.TooltipOptions{Layer: tooltipLayer, SideOffset: tooltipGap}, components.HoverTooltipDelay)
		state.fileNotices.paint(canvas, state.Theme(), state.windowRect())
		return
	}
	// The shell normally paints one opaque root. A terminal can instead show
	// the desktop or a picture through its own ground; give the bar its own fill.
	picture := state.paintBackground(canvas, size)
	terminal := state.terminalPage()
	fileManager := state.activeSFTPWindow() != nil
	fullPage := terminal || fileManager
	chrome := &state.chrome
	geometry := state.windowBarGeometry(canvas, size.Width)
	activeIndex := -1
	if active, ok := state.activeWindowTab(); ok {
		activeIndex = state.windowTabIndex(active)
	}
	scroll := float32(0)
	if state.tabOrientation == tabsHorizontal {
		count := state.windowTabCount()
		scroll = state.tabStrip.layoutIndex(geometry.area, float32(count)*(geometry.tabWidth+barGap), count, activeIndex, geometry.tabWidth+barGap, geometry.tabWidth)
	} else if state.fileTabCount() > 0 {
		fileTabs := state.fileTabCount()
		scroll = state.fileTabStrip.layoutIndex(geometry.area, float32(fileTabs)*(geometry.tabWidth+barGap), fileTabs, activeIndex-len(state.workspaceTabs), geometry.tabWidth+barGap, geometry.tabWidth)
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
	chrome.shellBackdrop = picture || (terminal && state.desktopTransparency() > 0)
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
		state.paintIndependentMonitor(canvas, size)
		state.paintWorkspaceSideRail(canvas, size)
		// Assistant popovers may intentionally extend to the window edge; paint
		// the rail first so they can remain above it and nothing is clipped.
		state.paintAIPanel(canvas, size)
		state.paintCommandsDrawer(canvas)
	} else if fileManager {
		state.paintSFTPTab(canvas, state.fileManagerPixelBounds(size))
		state.paintAIPanel(canvas, size)
	} else {
		workspace := workspaceSlot.Rect().Inset(gfx.All(workspaceBorder))
		canvas.Clip(workspace)
		if !state.devPaintPage(canvas, workspace) {
			switch state.activeSection {
			case sectionSettings:
				state.paintSettingsDetails(canvas)
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
	state.settleFindPreferences(canvas)
	state.paintQueuedSFTPHeaderMenu(canvas)
	state.paintHostMenus(canvas, viewport)
	state.endMenus()
	state.recordTransferDetailAnchors(canvas)
	state.requestReconnectFrames(canvas)
	state.requestAutofillFrames(canvas)
	state.requestLoginCommandFrames(canvas)
	state.requestInjectionFrames(canvas)
	state.requestIdleKeepAliveFrames(canvas)
	state.paintLocalProxyFocus(canvas)
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
// ends (the canvas's TitlebarInsets: macOS's traffic lights, or the buttons Viva draws on
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

// tabGlideMotion is how a horizontal tab slides to the place its neighbours'
// closing, adding or reordering gave it: quick and without bounce, so the tab
// under the pointer never overshoots it.
var tabGlideMotion = gfx.SpringMotion(220*time.Millisecond, 0)

// rowGlideMotion is how list rows slide to the place a reorder, a move to another
// group, an add or a delete gave them.
var rowGlideMotion = gfx.SpringMotion(220*time.Millisecond, 0)

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
	box          ui.Box // the tab's place, for a file drag looking for a target
	pathLabel    string
	foreground   gfx.Color
	title        ui.Box
	output       outputAlertKind
	label        string
	number       string // the tab's number, empty for a tab no number key reaches
	workspace    layoutcore.WorkspaceInfo
	fileSession  uint32
	// settings marks the settings page's tab, which has no session or workspace.
	settings    bool
	id, closeID gfx.ID
	background  gfx.Color
	// base is the fill before tint colours it; the alert title is backed by it
	// so its ink keeps the contrast it has on an uncoloured tab. tint is the
	// tab colour, empty when the tab has none.
	base    gfx.Color
	tint    string
	compact bool
	// glided marks a tab that slides to a new place; glideScroll is how far the
	// strip is scrolled, which is not a move.
	glided      bool
	glideScroll gfx.Point
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
			Tree: &tree, Root: shellPageRoot, Theme: &state.theme, EditMenu: &state.editMenu, View: state.declareWorkbenchPage,
			ScrollbarSize: catalogBarGutter, Styles: &ui.Styles{Switch: state.switchLook},
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
	config := ui.Configure(state.windowPage(), base)
	config.Paint = base.Paint
	if state.options.TerminalTransparency {
		// The terminal's default fill is translucent over the desktop, so the
		// window itself must composite: nothing under the shell is opaque.
		config.Transparent = true
		config.ClearColor = app.Color{}
		config.OnTransparentChange = func(active bool) {
			state.windowComposited.Store(active)
			if state.options.Invalidate != nil {
				state.options.Invalidate()
			}
		}
	}
	// The assistant owns the wheel over its own panel: its lists keep their
	// offsets in the page, but a modal dialog must still keep them still, which
	// the page's generic scroll-area routing does not know. Everything else the
	// event reaches goes on to the page as before.
	config.OnScroll = state.scrollWindow
	config.OnActivate = func() {
		if base.OnActivate != nil {
			base.OnActivate()
		}
		if w := state.activeSFTPWindow(); w != nil && state.modal == modalNone {
			w.activate()
		}
	}
	// A lock may arrive between the previous paint and the next input. Do not
	// let a stale business control in the page consume that event before the
	// workbench handler can check the profile lock.
	key, shortcut, pointer, input := config.OnKey, config.OnShortcut, config.OnPointer, config.TextInput
	config.OnKey = func(event gfx.KeyEvent) gfx.EventResult {
		if state.modal == modalAppLock && state.appLockBusy() {
			return gfx.EventResult{Handled: true}
		}
		if state.appAccessBlocked() && (!event.Target.IsZero() && !appLockOwns(event.Target)) {
			return gfx.EventResult{Handled: true}
		}
		if state.appLocked() && event.Target.IsZero() {
			result := state.appLockKey(event)
			result.Handled = true
			return result
		}
		result := key(event)
		if state.appAccessBlocked() {
			result.Handled = true
		}
		return result
	}
	config.OnShortcut = func(event gfx.KeyEvent) gfx.EventResult {
		if state.appAccessBlocked() || state.modal == modalAppLock {
			return state.Shortcut(event)
		}
		return shortcut(event)
	}
	config.OnPointer = func(event gfx.PointerEvent) gfx.EventResult {
		if state.modal == modalAppLock && state.appLockBusy() {
			return gfx.EventResult{Handled: true}
		}
		if state.appAccessBlocked() && !appLockOwns(event.Target) {
			// Viva runs its window buttons after OnPointer. Leave the trailing
			// titlebar inset unhandled without routing it to the workbench page.
			buttons := gfx.Rect{
				X: event.Viewport.Width - state.titlebarTrailing, Width: state.titlebarTrailing, Height: windowBarHeight,
			}
			return gfx.EventResult{Handled: !pointInRect(event.X, event.Y, buttons)}
		}
		return pointer(event)
	}
	config.TextInput = func(context app.TextInputContext) *app.TextInput {
		if (state.appAccessBlocked() || state.modal == modalAppLock) && state.appLockEditor(context.ID) == nil {
			return nil
		}
		return input(context)
	}
	return config
}

// scrollWindow gives the wheel to the assistant's panel first, then to the
// page's other scroll areas and the window's own handlers. A modal dialog owns
// the wheel over its backdrop: the panel keeps still behind it, as it did when
// its list was painted by hand and the window's own handler saw the event first.
func (state *State) scrollWindow(event gfx.ScrollEvent) gfx.EventResult {
	if state.appAccessBlocked() {
		return gfx.EventResult{Handled: true}
	}
	if state.hostGroupMenu {
		// The move picker and its backdrop own the wheel over the host list.
		return state.Scroll(event)
	}
	// 模态弹窗中的浮动下拉选择框拥有最顶层视觉层级，滚轮事件必须优先处理，
	// 避免穿透并触发底层页面或弹窗内的 ui.Scroll 列表。
	if state.modal == modalNewHost && state.editor.groupPicker.control.Open {
		p := &state.editor.groupPicker
		return gfx.EventResult{Handled: true, Repaint: pointInRect(event.X, event.Y, p.popup) && p.control.Wheel(event)}
	}
	if state.modal == modalNewHost && state.editor.tagPicker.control.Open {
		p := &state.editor.tagPicker
		return gfx.EventResult{Handled: true, Repaint: pointInRect(event.X, event.Y, p.popup) && p.control.Wheel(event)}
	}
	if state.aiMenuShown() {
		// An open chip menu is the page's popup and owns the wheel; its own
		// scroll route (and its backdrop) decide where the event goes.
		return state.windowPage().RouteWheel(event)
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
		if state.modal == modalProxyBatchAssociate {
			// The jump picker's open list and the candidates are the page's.
			result := state.windowPage().RouteWheel(event)
			result.Handled = true
			return result
		}
		return state.Scroll(event)
	}
	// The notes panel's preview is an overlay of the panes: its scroll area is
	// laid out outside the page's flow, so the wheel is applied here to keep it
	// where the pointer is.
	if state.noteSidebarVisible() {
		if result, handled := state.noteSidebarWheel(event); handled {
			return result
		}
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
	_, trailing := state.windowTitlebarInsets()
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
		state.declareTransferProgressCards(c)
		state.declareDashboardDialog(c)
		state.declareAgentHostsDialog(c)
		state.declareDashboardAddMenu(c)
		return
	}
	chrome, pal := &state.chrome, state.pal
	root := ui.Column(c).Fill()
	if !chrome.shellBackdrop {
		root.Background(state.windowFrameColor())
	}
	root.Children(func() {
		state.declareWindowBar(c, &chrome.bar, false, chrome.shellBackdrop, chrome.shellGeometry, chrome.shellScroll)
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
			state.declareRemoteLogs(c)
			state.declareContextPanel(c)
			state.declareHostsPanel(c)
			state.declareNoteSidebar(c)
		} else {
			// The file-manager tab is part of the page like the panes.
			state.declareSFTPTab(c)
		}
		// The sidebar is an overlay over the full-page workspace: it paints on
		// the pane-chrome band and blocks presses on its blank surface.
		state.declareVerticalTabsOverlay(c)
	}
	// The connection monitor's panel is part of this page too: a drawer at
	// narrow widths, otherwise the region the panes leave beside them.
	state.declareIndependentMonitor(c)
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
	state.declareNoteExit(c)
	state.declareMenus(c)
	// The command drawer's filter and order dropdowns are page menus too; the
	// bar under them is the window's.
	state.declareCommandsMenus(c)
	// The dashboard's own dialog and its hosts-page add menu are page controls
	// too: they are open over whatever section asked for them.
	state.declareDashboardDialog(c)
	state.declareAgentHostsDialog(c)
	state.declareDashboardAddMenu(c)
	// The drag overlay is hitless and paints over the page on the drag layer.
	state.declareTabDragOverlays(c)
	state.declareWorkspaceFocusDragOverlay(c)
	state.declareWorkspaceCardDragOverlay(c)
	state.declareToolbarDockDragOverlay(c)
	state.declareTransferProgressCards(c)
}

// windowBarGeometry lays the bar out once without its contents to find out how
// wide the tabs come out: they stay barTabMax wide and shrink together to barTabMin
// to share the room left of the buttons.
func (state *State) windowBarGeometry(canvas *gfx.Canvas, width float32) barGeometry {
	chrome := &state.chrome
	leading, trailing := state.windowTitlebarInsets()
	padRight := state.windowBarRightPad()
	buttons := state.windowBarButtons()
	tabCount := len(state.workspaceTabs) + state.fileTabCount()
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
func (state *State) declareWindowBar(c ui.C, bar *windowBar, probe, backdrop bool, geometry barGeometry, scroll float32) {
	m, pal, chrome := state.messages, state.pal, &state.chrome
	leading, _ := state.windowTitlebarInsets()
	visibleFileTabs := 0
	for _, tab := range state.sftpTabs {
		if tab != nil && !tab.closed {
			visibleFileTabs++
		}
	}
	visibleFileTabs += state.settingsTabCount()
	settingsPlace := state.settingsTabPlace()
	settingsDeclared := false
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
	if backdrop {
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
					firstPending := !probe
					if settingsPlace == 0 {
						first := float32(0)
						if firstPending {
							first, firstPending = -scroll, false
						}
						bar.tabs = append(bar.tabs, state.declareSettingsTab(c, probe, geometry, first, scroll))
						settingsDeclared = true
					}
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
						} else {
							first := float32(0)
							if firstPending {
								first, firstPending = -scroll, false
							}
							number := state.windowTabIndex(windowTab{kind: windowTabWorkspace, id: uint32(workspace.ID)}) + 1
							entry := state.declareWorkspaceTab(c, workspace, geometry.tabWidth, barTabHeight, first, gfx.Point{X: scroll}, false, false, number)
							bar.tabs = append(bar.tabs, entry)
						}
						if settingsPlace == index+1 {
							bar.tabs = append(bar.tabs, state.declareSettingsTab(c, probe, geometry, 0, scroll))
							settingsDeclared = true
						}
					}
					visibleFileIndex := 0
					for _, fileTab := range state.sftpTabs {
						if fileTab == nil || fileTab.closed {
							continue
						}
						baseIndex := len(state.workspaceTabs) + visibleFileIndex
						visibleFileIndex++
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
						} else {
							entry := barTab{fileSession: fileTab.session.ID, id: id, closeID: closeID}
							if state.tabNumbers {
								entry.number = tabNumber(state.windowTabIndex(windowTab{kind: windowTabFile, id: fileTab.session.ID}) + 1)
							}
							active := state.activeSection == sectionSFTP && state.activeSFTPTab == fileTab.session.ID
							hovered := c.Canvas().Hovered() == id || c.Canvas().Hovered() == closeID
							dropped := state.foreignFileTab == fileTab.session.ID
							foreground := pal.muted
							fill := pal.tab
							switch {
							case active:
								fill, foreground = pal.tabActive, pal.text
							case hovered:
								fill, foreground = pal.tabHover, pal.text
							}
							color := fileTab.session.host.Color
							entry.tint = color
							entry.base = fill
							fill = tabFill(pal, fill, color, active, hovered)
							edge := tabBorderColor(pal, color, active)
							foreground = tabInk(pal, color, foreground)
							if dropped {
								// A file drag from another window would land in this
								// tab's server folder.
								fill, edge, foreground = pal.tabActive, pal.accent, pal.text
							}
							entry.background = fill
							first := float32(0)
							if firstPending {
								first, firstPending = -scroll, false
							}
							label := state.sftpFileTabTitle(fileTab)
							entry.glided, entry.glideScroll = true, gfx.Point{X: scroll}
							entry.box = ui.Row(c).Size(geometry.tabWidth, barTabHeight).Shrink(0).MarginSides(0, barGap, 0, first).
								Glide(tabGlideMotion, gfx.Point{X: scroll}).HitOutset(barTabReach, 0, barTabReach, 0).PadSides(0, 3, 0, 4).Items(ui.Center).Radius(radius.item).Border(1, edge).Background(fill).
								HitWith(id, label, gfx.HitOptions{Role: gfx.RoleTab, Selected: active, Hint: label}).Children(func() {
								entry.badge = ui.Slot(c).Size(20, 20).MarginSides(0, 7, 0, 0)
								if state.tabNumbers && entry.number != "" {
									// File tabs follow the session tabs in the number row,
									// as they do for the number shortcuts.
									ui.Text(c, entry.number).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(tabNumberInk(foreground, fileTab.session.host.Color)).LineHeight(20).Shrink(0).MarginSides(0, tabNumberGap, 0, 0)
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
						if settingsPlace == baseIndex+1 {
							bar.tabs = append(bar.tabs, state.declareSettingsTab(c, probe, geometry, 0, scroll))
							settingsDeclared = true
						}
					}
					if settingsPlace >= 0 && !settingsDeclared {
						bar.tabs = append(bar.tabs, state.declareSettingsTab(c, probe, geometry, 0, scroll))
					}
				})
				state.barButton(c, probe, hitNewTab, iconPlus, m.LabelNewTab)
			})
			if !probe {
				// Over the tab row, as the stack's later child: the arrows take no
				// width from it, and their margin leaves the new-tab button clear.
				state.declareWindowBarTabArrows(c)
			}
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
func (state *State) declareWorkspaceTab(c ui.C, workspace layoutcore.WorkspaceInfo, width, height, first float32, scroll gfx.Point, vertical, compact bool, number int) barTab {
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
	fill := pal.tab
	switch {
	case active:
		fill, foreground = pal.tabActive, pal.text
	case hovered:
		fill, foreground = pal.tabHover, pal.text
	}
	color := state.tabColor(workspace)
	entry.tint = color
	entry.base = fill
	fill = tabFill(pal, fill, color, active, hovered)
	edge := tabBorderColor(pal, color, active)
	foreground = tabInk(pal, color, foreground)
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
	if id == state.tabLanding {
		// A tab dropped by the pointer slides from where it was let go.
		c.Canvas().GlideFrom(id, 0, state.tabGhost)
		state.tabLanding = gfx.ID{}
	}
	row.Glide(tabGlideMotion, scroll)
	entry.box, entry.glided, entry.glideScroll = row, true, scroll
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
			ui.Text(c, entry.number).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(tabNumberInk(foreground, color)).LineHeight(20).Shrink(0).MarginSides(0, tabNumberGap, 0, 0)
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
	return state.barIconButton(c, id, icon, label, label)
}

// barIconButton is a window-bar button drawn now, with its tooltip given apart:
// hint is what the tooltip shows, and empty when the icon's meaning needs none.
func (state *State) barIconButton(c ui.C, id gfx.ID, icon uint8, label, hint string) ui.Box {
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
		HitWith(id, label, gfx.HitOptions{Role: gfx.RoleButton, SkipFocus: true, Hint: hint}).Children(func() {
		ui.Icon(c, lucideIcons[icon]).Color(foreground)
	})
}

// paintWindowBarParts draws what the bar's tabs and assistant button hold that is
// drawn rather than declared: the system of a session and its light, in the room
// the layout kept for them, and the session's name.
func (state *State) paintWindowBarParts(canvas *gfx.Canvas, strip gfx.Rect) {
	bar := &state.chrome.bar
	state.paintWorkspaceTabParts(canvas, state.windowBarTabsRoom(canvas, strip), bar.tabs)
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
	shellBackdrop  bool
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
	// flags are the values the page's checkboxes and switches are declared over.
	flags pageFlags
	// searchField is the hosts toolbar's page search field over state.search.
	searchField ui.Field
	searchBound bool
	// hostDialogTitle labels the host form's scroll viewport. The handlers below
	// are the form's Draw and DrawOver declarations kept once, so a warm repaint
	// of the form allocates nothing.
	hostDialogTitle       string
	hostDialogCancelLabel string
	hostDialogSubmitLabel string
	hostFormViewportDraw  func(*ui.DrawContext)
	hostFormFieldsDraw    func(*ui.DrawContext)
	// hostTestButton, hostTestMessage, hostCancelButton and hostSubmitButton are
	// the host form footer's page controls; the message slot's Draw keeps the
	// controls in the form's tab ring and paints what the last test found.
	hostTestButton      ui.Box
	hostTestMessage     ui.Box
	hostCancelButton    ui.Box
	hostSubmitButton    ui.Box
	hostTestMessageDraw func(*ui.DrawContext)
	// newTabAdd and newTabAll are the empty tab's footer buttons.
	newTabAdd   ui.Box
	newTabAll   ui.Box
	newTabShown bool
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
	sideVisible                  bool
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
	tree, list, grid, sort, privacy, order ui.Box
}

// declareNavigation declares the sidebar: the name of the application over the
// pages, and the way to a shell on this computer and to the settings at the bottom.
// A page the user hid from Settings keeps everything but its row here.
func (state *State) declareNavigation(c ui.C) {
	m := state.messages
	ui.Column(c).Width(navigationWidth).Shrink(0).Children(func() {
		// The logo with the name beside it, painted into this slot after layout.
		state.chrome.lockup = ui.Slot(c).Size(lockupWidth, lockupHeight).MarginSides(15, 0, 20, 0).Self(ui.Center)
		for _, item := range sidebarPages {
			if state.hiddenNav.has(item.page) {
				continue
			}
			state.navigationItem(c, item.id, item.icon, state.navPageLabel(item.page), state.activeSection == item.section, 4)
		}
		if state.tabOrientation == tabsVertical && len(state.workspaceTabs) > 0 {
			state.declareVerticalTabsNav(c)
		} else {
			ui.Spacer(c)
		}
		// A shell on this computer needs no host, so its way in is always at hand:
		// above the Settings, at the bottom of the sidebar. A dev build also puts
		// the component gallery there (see devDeclareNavigation).
		if state.appLockEnabled() {
			state.navigationItem(c, hitAppLock, iconLock, m.AppLock.Lock, false, 4)
		}
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
	layout := state.hostsLayout(bounds.Width, bounds.Height)
	state.hostSide.visible = layout.sideVisible
	state.hostSide.visibleWidth = layout.side.Width
	state.paintHostsRightHeader(canvas, layout)
	state.hostSide.bounds = gfx.Rect{
		X: bounds.X + layout.sideTree.X, Y: bounds.Y + layout.sideTree.Y,
		Width: layout.sideTree.Width, Height: layout.sideTree.Height,
	}
	list := gfx.Rect{
		X: bounds.X + layout.list.X, Y: bounds.Y + layout.list.Y,
		Width: layout.list.Width, Height: layout.list.Height,
	}
	state.hostList.bounds = list
	state.chrome.hostList = list

	switch {
	case len(state.hostTreeRows) > 0 && state.hostView != hostViewGrid:
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
	state.paintHostDragGhost(canvas, bounds)
	state.hostsBounds = bounds
}

// paintHostMenus draws the hosts page's hierarchy menus above everything else:
// the move/copy destination and a chip's nested groups, which a page menu cannot
// express as panels yet.
func (state *State) paintHostMenus(canvas *gfx.Canvas, viewport gfx.Rect) {
	bounds := state.hostsBounds
	if bounds.Width == 0 {
		bounds = state.windowRect()
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
}

// hostsHeaderPlan works out the header of the hosts page from the window: the
// widths its controls need, which of them are condensed, and the actions shown
// while hosts are checked. The declaration and the painter share it, so the
// controls painted into the slots are the ones the page laid out.
func (state *State) hostsHeaderPlan(canvas *gfx.Canvas, layout hostsLayout) hostsHeaderPlan {
	m := state.messages
	// The buttons keep their natural width, so the search box gives way to them
	// rather than the other way round: a label that is wider than expected (a
	// heavier or wider font) must not slide a button under its neighbour.
	const (
		toolbarMargin      = float32(18)  // the toolbar row's own margin inside the section card
		buttonGap          = float32(10)  // the air between the search box and the buttons
		searchLeast        = float32(140) // the narrowest the search box stays a search box at
		toggleWidth        = float32(82)
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
	plan.condensed = labeled > layout.pageWidth-2*toolbarMargin
	plan.connectWidth, plan.connectIcon = toolbarButtonLayout(canvas, m.Connect, 0, iconPlug, plan.condensed)
	plan.importWidth, plan.importIcon = toolbarButtonLayout(canvas, m.ImportMenu, iconDownload, iconDownload, plan.condensed)
	plan.newHostWidth, plan.newHostIcon = toolbarButtonLayout(canvas, m.NewHost, iconPlus, iconPlus, plan.condensed)
	plan.anySelected = layout.anySelected
	// While something is checked the first label lines up with the list's left
	// edge, so the button's padding hangs outside it (a negative margin).
	const quietPadding = float32(12)
	plan.actions = [...]hostsHeaderAction{
		{hitExportHosts, m.ExportHosts, iconFileDown, len(state.hosts) > 0, false},
		{hitConnectSelectedHosts, state.messages.WorkspaceDraftTemporary, iconPlug, false, false},
		{hitEditHost, m.EditHost, iconPencil, false, false},
		{hitCopyHost, m.CopyHost, iconCopy, false, false},
		{hitMoveHostGroup, m.MoveHostGroup, iconFolderOpen, false, false},
		{hitDashboardAddHosts, state.messages.Dashboard.AddHosts, iconGauge, false, false},
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
		plan.actionsCondensed = used > layout.main.Width-2*rowPad
		for index, action := range plan.actions {
			plan.actionWidths[index] = canvas.TextWidthWeight(action.label, metrics.FontSizeS, gfx.WeightSemibold) + 4 + 2*quietPadding
		}
		if plan.actionsCondensed {
			for index := range plan.actionWidths {
				plan.actionWidths[index] = actionIconSize
			}
		}
	}
	// The tree takes the group filter's place while it is shown; in a window
	// too narrow for both panes the chips come back over the list.
	plan.sideVisible = layout.sideVisible
	plan.showChips = state.hasHostFilter() && !layout.sideVisible
	plan.showGroupControls = plan.showChips && !state.hostFilterOn
	plan.compactHostControls = layout.compact
	viewControlsWidth := toggleWidth + 6 + 26 + 6 + 26
	if plan.showChips {
		// A short window keeps the chips on a row of their own, above the
		// select-all box and the controls.
		const chipRowPad = float32(30) // the row's left and right padding
		room := layout.main.Width - chipRowPad - hostHeaderRowGap
		if !plan.compactHostControls {
			room -= viewControlsWidth + 40 + 3*hostHeaderRowGap
			if plan.showGroupControls {
				room -= groupControlsWidth + hostHeaderRowGap
			}
		}
		state.fitHostChips(canvas, room)
	}
	return plan
}

// declareHostsToolbar declares the hosts page's toolbar across the top of the
// page. The search field is painted into the slot it leaves; the buttons paint
// themselves.
func (state *State) declareHostsToolbar(c ui.C, width float32, plan hostsHeaderPlan) {
	m := state.messages
	b := &state.chrome.headerBoxes
	const (
		buttonGap   = float32(10)
		searchLeast = float32(140)
	)
	searchWidth := min(max(width*0.41, 330), 485)
	ui.Row(c).Absolute().Left(hostsBodyMargin).Right(hostsBodyMargin).Top(hostsToolbarTop).Height(toolbarHeight).Items(ui.Center).Children(func() {
		b.search = ui.Slot(c).Basis(searchWidth).Shrink(1).MinWidth(searchLeast).Height(toolbarHeight).MarginSides(0, buttonGap, 0, 0)
		b.search.Children(func() {
			if !state.chrome.searchBound {
				state.chrome.searchField.Bind(&state.search)
				state.chrome.searchBound = true
			}
			state.chrome.searchField.Disabled = state.hostSort.enabled
			field := ui.TextField(c, &state.chrome.searchField, m.NavSearch, m.SearchPlaceholder, ui.WithID(hitSearch)).
				Icon(lucideIcons[iconSearch]).FillWidth().Height(toolbarHeight).Disabled(state.hostSort.enabled)
			if state.search.RuneCount() > 0 && !state.hostSort.enabled {
				field = field.Action(lucideIcons[iconClose], m.SearchClear)
				if field.ActionClicked() {
					state.clearSearch()
				}
			}
		})
		b.connect = state.declareButtonSized(c, hitConnect, m.Connect, plan.connectIcon, false, false, state.typedQuery() != "", toolbarFont, plan.condensed).Size(plan.connectWidth, toolbarHeight).MarginSides(0, buttonGap, 0, 0)
		ui.Spacer(c)
		b.importButton = state.declareButtonSized(c, hitImportMenu, m.ImportMenu, plan.importIcon, false, false, true, toolbarFont, plan.condensed).Size(plan.importWidth, toolbarHeight).MarginSides(0, buttonGap, 0, 0)
		state.declareImportMenu(c, b.importButton)
		b.newHost = state.declareButtonSized(c, hitAddHost, m.NewHost, plan.newHostIcon, true, false, true, toolbarFont, plan.condensed).Size(plan.newHostWidth, toolbarHeight)
	})
}

// declareHostBatchAction declares one of the batch actions over the checked
// hosts: a ghost button of the width the header measured, or its icon alone in a
// row too narrow for words.
func (state *State) declareHostBatchAction(c ui.C, action hostsHeaderAction, width float32, condensed bool) ui.Box {
	var button ui.Box
	if condensed {
		button = ui.Button(c, action.label).Ghost().Icon(lucideIcons[action.icon]).IconOnly().
			Size(actionIconSize, hostHeaderRowHeight)
	} else {
		button = ui.Button(c, action.label).Ghost().XSmall().Size(width, hostHeaderRowHeight)
	}
	if action.danger {
		button = button.ButtonRecipe(hostsDangerButtonRecipe)
	}
	return button.Hit(action.id, action.label).Disabled(!action.enabled)
}

// hostsDangerButtonRecipe is a button whose label is the danger colour, as the
// batch row marks a destructive action without making it the loudest control.
func hostsDangerButtonRecipe(t gfx.Theme, variant components.ButtonVariant, state gfx.State) components.ButtonLook {
	t.Foreground = t.Danger
	return components.ButtonRecipe(t, variant, state)
}

// declareHostsRightHeader declares the controls over the host list or grid:
// checking every host, the group chips shown while the tree is closed, the view
// and order controls, and, while hosts are checked, the row of batch actions. A
// window too narrow for one row puts the chips above them, so neither has to
// give way.
func (state *State) declareHostsRightHeader(c ui.C, layout hostsLayout, plan hostsHeaderPlan) {
	pal := state.pal
	chrome := &state.chrome
	b := &chrome.headerBoxes
	const (
		rowPad    = float32(18)
		actionGap = float32(4)
		leftPad   = float32(12)
	)
	quietPadding := float32(12)
	ui.Column(c).Absolute().Left(layout.rightHeader.X).Top(layout.rightHeader.Y).
		Width(layout.rightHeader.Width).Height(layout.rightHeader.Height).Children(func() {
		if plan.compactHostControls {
			ui.Row(c).Height(hostHeaderRowHeight).PadSides(0, rowPad, 0, leftPad).Gap(12).Items(ui.Center).Children(func() {
				if plan.showChips {
					state.declareHostChips(c)
				}
				ui.Spacer(c)
			})
			ui.Row(c).Height(hostHeaderRowHeight).MarginSides(hostHeaderRowGap, 0, 0, 0).PadSides(0, rowPad, 0, leftPad).Gap(12).Items(ui.Center).Children(func() {
				b.selectAll = ui.Slot(c).Size(40, hostHeaderRowHeight).Shrink(0)
				state.declareHostScopeCheck(c, b.selectAll, hitHostSelectAll, state.hostScopeSelection.all.label, state.hostScopeSelection.all, state.hostScopeSelection.all.total == 0 || state.hostSort.enabled)
				state.declareHostGroupControls(c, plan)
				ui.Spacer(c)
				state.declareHostViewControls(c)
			})
		} else {
			ui.Row(c).Height(hostHeaderRowHeight).PadSides(0, rowPad, 0, leftPad).Gap(12).Items(ui.Center).Children(func() {
				b.selectAll = ui.Slot(c).Size(40, hostHeaderRowHeight).Shrink(0)
				state.declareHostScopeCheck(c, b.selectAll, hitHostSelectAll, state.hostScopeSelection.all.label, state.hostScopeSelection.all, state.hostScopeSelection.all.total == 0 || state.hostSort.enabled)
				if plan.showChips {
					state.declareHostChips(c)
				}
				ui.Spacer(c)
				state.declareHostGroupControls(c, plan)
				state.declareHostViewControls(c)
			})
		}
		if plan.anySelected {
			ui.Row(c).Height(hostHeaderRowHeight).MarginSides(hostHeaderRowGap, 0, 0, 0).PadSides(0, rowPad, 0, leftPad).Gap(hostHeaderRowGap).Items(ui.Center).Children(func() {
				actionsLeft := -quietPadding
				if plan.actionsCondensed {
					actionsLeft = 0
				}
				ui.Row(c).Gap(actionGap).MarginSides(0, 0, 0, actionsLeft).Children(func() {
					for index, action := range plan.actions {
						b.buttons[index] = state.declareHostBatchAction(c, action, plan.actionWidths[index], plan.actionsCondensed)
					}
				})
				ui.Spacer(c)
				ui.Text(c, state.hostSelectionText).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(pal.muted).Ellipsis().Shrink(1)
			})
		}
	})
}

// declareHostChips declares the group chips that stand in for the tree in the
// list and grid views.
func (state *State) declareHostChips(c ui.C) {
	m := state.messages
	chrome := &state.chrome
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

// declareHostGroupControls declares the one-click expand and collapse buttons
// where the page offers them.
func (state *State) declareHostGroupControls(c ui.C, plan hostsHeaderPlan) {
	if !plan.showGroupControls {
		return
	}
	m := state.messages
	ui.Row(c).Gap(2).Children(func() {
		expand, collapse := state.hostGroupsExpansionActions()
		state.chrome.headerBoxes.groupControls[0] = state.declareIconButton(c, hitHostGroupsExpandAll, iconChevronsExpand, m.HostGroupActions.ExpandAll, !expand, iconButtonQuiet).Size(26, 26)
		state.chrome.headerBoxes.groupControls[1] = state.declareIconButton(c, hitHostGroupsCollapseAll, iconChevronsCollapse, m.HostGroupActions.CollapseAll, !collapse, iconButtonQuiet).Size(26, 26)
	})
}

// declareHostViewControls declares the tree pane's toggle, the list and grid
// choices, and the privacy and order controls beside them.
func (state *State) declareHostViewControls(c ui.C) {
	m := state.messages
	chrome := &state.chrome
	viewControlsWidth := float32(82 + 6 + 26 + 6 + 26)
	chrome.headerBoxes.toggle = ui.Slot(c).Size(viewControlsWidth, hostHeaderRowHeight).MarginSides(0, 0, 0, 2)
	chrome.hostToggle = hostToggleBoxes{}
	chrome.headerBoxes.toggle.Children(func() {
		ui.Row(c).Fill().Items(ui.Center).Children(func() {
			ui.Row(c).Width(82).Gap(2).Children(func() {
				listStyle, gridStyle, treeStyle := iconButtonQuiet, iconButtonQuiet, iconButtonQuiet
				switch state.hostView {
				case hostViewList:
					listStyle = iconButtonActive
				case hostViewGrid:
					gridStyle = iconButtonActive
				}
				treeOpen := state.hostSide.open && state.hasHostFilter()
				if treeOpen {
					treeStyle = iconButtonActive
				}
				chrome.hostToggle.tree = state.declareIconButton(c, hitHostViewTree, iconListTree, m.Tools.HostViewTree, !state.hasHostFilter(), treeStyle).Size(26, 26)
				chrome.hostToggle.list = state.declareIconButton(c, hitHostViewList, iconRows2, m.Tools.HostViewList, false, listStyle).Size(26, 26)
				chrome.hostToggle.grid = state.declareIconButton(c, hitHostViewGrid, iconHome, m.Tools.HostViewGrid, false, gridStyle).Size(26, 26)
			})
			privacyIcon, privacyStyle := uint8(iconEye), iconButtonQuiet
			if state.hostPrivacy.enabled {
				privacyIcon, privacyStyle = iconEyeOff, iconButtonActive
			}
			chrome.hostToggle.privacy = state.declareIconButton(c, hitHostPrivacy, privacyIcon, m.Tools.HostPrivacy, false, privacyStyle).Size(26, 26).MarginSides(0, 0, 0, 6)
			orderStyle := iconButtonQuiet
			if state.hostOrder != hostOrderManual {
				orderStyle = iconButtonActive
			}
			// The far-right control is the display-order menu, wearing the
			// manual-sort mark; manual moves are made by dragging a host.
			chrome.hostToggle.order = state.declareIconButton(c, hitHostOrder, iconTransfer, m.HostCatalog.Order, false, orderStyle).Size(26, 26).MarginSides(0, 0, 0, 6)
			state.declareHostPrivacyMenu(c, chrome.hostToggle.privacy)
			state.declareHostOrderMenu(c, chrome.hostToggle.order)
		})
	})
}

// paintHostsRightHeader paints the list's header controls over the slots the
// page left: the select-all checkbox, the fallback chips, and the batch
// actions.
func (state *State) paintHostsRightHeader(canvas *gfx.Canvas, layout hostsLayout) {
	chrome := &state.chrome
	plan := chrome.headerPlan
	b := &chrome.headerBoxes
	if !layout.sideVisible && layout.main.Width <= 0 {
		return
	}
	state.hostOrderButton = chrome.hostToggle.order.Rect()
	state.hostPrivacy.button = chrome.hostToggle.privacy.Rect()
	if plan.showChips {
		state.paintHostFilterChips(canvas, chrome.chips)
	}
	if plan.anySelected {
		for index, action := range plan.actions {
			if action.id == hitDashboardAddHosts {
				state.dashboard.addAnchor = b.buttons[index].Rect()
			}
			if action.id == hitMoveHostGroup {
				state.groupMenu.button = b.buttons[index].Rect()
			}
		}
	}
}

// declareImportMenu declares the menu of ways to bring hosts in under the
// Import button, with the whole catalog's export beside them. A choice runs as
// if its control had been clicked.
func (state *State) declareImportMenu(c ui.C, anchor ui.Box) {
	if !state.importMenu {
		return
	}
	m := state.messages
	ui.PopupLayer(c, dialogLayer)
	ui.Menu(c, anchor, &state.importMenu, func(menu *ui.MenuItems) {
		menu.WidthRange(200, 200)
		for _, source := range [...]struct {
			id    gfx.ID
			label string
		}{
			{hitImportSessions, m.Tools.ImportSessions},
			{hitImportHostCatalog, m.ImportCatalog},
			{hitImportSSHConfig, m.SSHConfigImport},
		} {
			if menu.Item(source.label).Clicked() {
				c.After(func() { state.click(source.id, true) })
			}
		}
		// With nothing checked the row of actions is away, and exporting the whole
		// catalog lives next to importing one.
		menu.Separator()
		if menu.Item(m.ExportHosts).Disabled(len(state.hosts) == 0).Clicked() {
			c.After(func() { state.click(hitExportHosts, true) })
		}
	})
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
		ui.Text(c, chip.label).FontSize(metrics.FontSizeS).Weight(gfx.WeightSemibold).Color(foreground).
			StateFrom(chipBox).Hover(ui.Look{}.Color(pal.text))
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

// paintHostFilterMenu lists the groups whose chips did not fit. The menu keeps
// its own rows' secondary presses (they open the group's actions), which a page
// menu cannot express yet.
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
	textColor, countColor, pinColor := pal.text, pal.faint, pal.accent
	if from, _, ok := state.hostList.tree.DragSource(); ok && from == d.index {
		// The row stays where it is while the ghost carries its name.
		textColor.Alpha *= 0.5
		countColor.Alpha *= 0.5
		pinColor.Alpha *= 0.5
	}
	ui.Row(c).Width(width).Height(hostGroupRowHeight).PadSides(0, 0, 0, 2).Gap(6).Items(ui.Center).Children(func() {
		d.groupCheck = ui.Slot(c).Size(40, 28).Shrink(0)
		groupIndex := state.hostTreeRows[d.index].GroupIndex
		selection := state.hostScopeSelection.groups[groupIndex]
		state.declareHostScopeCheck(c, d.groupCheck, hitHostGroupCheck.At(groupIndex), selection.label, selection, selection.total == 0 || state.hostSort.enabled)
		if group.Pinned {
			ui.Icon(c, lucideIcons[iconStar]).Size(14, 14).Color(pinColor)
		}
		label := group.Label
		if group.Name == "" {
			label = state.messages.HostGroupDefault
		}
		folder := lucideIcons[iconFolderSolid]
		if group.Expanded || strings.TrimSpace(state.visibleQuery) != "" {
			folder = lucideIcons[iconFolderSolidOpen]
		}
		ui.Icon(c, folder).Size(16, 16).Shrink(0)
		ui.Text(c, label).FontSize(metrics.FontSizeM).Weight(gfx.WeightSemibold).Color(textColor).LineHeight(23).Ellipsis().Shrink(1)
		ui.Text(c, group.Count).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(countColor).LineHeight(21)
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
	tagsStart := max(rowWidth*0.65, float32(560))
	// The name column takes a fair share of the row on its own, but the address
	// keeps its own share before the tag column: a long host name must not be
	// cut short beside columns that have space to spare (#1064), and a masked
	// address must not be squeezed out by that name.
	addressShare := max(rowWidth*0.45, float32(330))
	addressTarget := min(rowWidth*0.25, max(min(tagsStart, statusStart)-leadWidth-nameLeast-16, 0))
	addressStart := max(leadWidth+nameLeast+8, min(addressShare, min(tagsStart, statusStart)-8-addressTarget))
	nameWidth := min(max(rowWidth*0.34, nameLeast), max(addressStart-leadWidth-8, nameLeast))
	addressWidth := max(min(rowWidth*0.25, min(tagsStart, statusStart)-addressStart-8), 0)
	tagsWidth := min(rowWidth*0.18, statusStart-tagsStart-8)
	status, _ := state.hostStatus(host)
	checkMargin := 46 - 10 - components.CheckboxExtent(state.Theme())
	protocolLabel := state.hostProtocolTagLabel(host)
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
					state.declareHostRowCheck(c, d.check, hitHostCheckList.At(int(host.ID)), state.hostPrivacySemanticLabel(host), state.hostSelected(host.ID))
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
						ui.Badge(c, protocolLabel).Height(24).PadX(7).PadY(0).Items(ui.Center).Radius(12).
							Background(withAlpha(color, 0.14)).Children(func() {
							ui.Text(c, protocolLabel).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(color)
						})
					}
					for _, tag := range tagList[:tagCount] {
						ui.Badge(c, tag).Height(24).PadX(7).PadY(0).Items(ui.Center).Radius(12).
							Background(pal.surface).Children(func() {
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
}

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
	if result, handled := state.scrollHostSideTree(event); handled {
		return result
	}
	if state.hostView != hostViewGrid {
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
	if host.ID == localHostID {
		state.paintLocalShellIcon(canvas, host, bounds, background)
		return
	}
	if host.Icon == hostIconAuto {
		if host.isWSL() {
			state.paintLocalShellIcon(canvas, host, bounds, background)
			return
		}
		system := autoIconSystem(host.System, host.Protocol)
		if host.isDesktop() && system == sshconn.SystemUnknown {
			canvas.Rounded(bounds, bounds.Width*0.24, state.hostBadgeFill(background))
			paintIcon(canvas, iconDesktop, insetRect(bounds, 0.24), state.pal.accent)
			return
		}
		paintSystemBadge(canvas, system, bounds, state.pal)
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

	iconFile:            icons.File,
	iconLink:            icons.Link,
	iconUpload:          icons.Upload,
	iconDownload:        icons.Download,
	iconFolderUp:        icons.FolderUp,
	iconFolderPlus:      icons.FolderPlus,
	iconPencil:          icons.Pencil,
	iconTrash:           icons.Trash2,
	iconLock:            icons.Lock,
	iconRefresh:         icons.RotateCw,
	iconArrowUp:         icons.ArrowUp,
	iconFolderOpen:      icons.FolderOpen,
	iconFolderSolid:     icons.FolderSolid,
	iconFolderSolidOpen: icons.FolderSolidOpen,
	iconActivity:        icons.Activity,
	iconGauge:           icons.Gauge,
	iconRecord:          icons.CircleDot,
	iconRecordStop:      icons.CircleStop,
	iconPlay:            icons.Play,
	iconStop:            icons.CircleStop,
	iconFilm:            icons.Film,
	iconFileDown:        icons.FileDown,
	iconColumns2:        icons.Columns2,
	iconRows2:           icons.Rows2,
	iconGrid2x2:         icons.Grid2x2,

	iconBot:                 icons.Bot,
	iconHistory:             icons.History,
	iconCopy:                icons.Copy,
	iconChevronRight:        icons.ChevronRight,
	iconChevronLeft:         icons.ChevronLeft,
	iconChevronDown:         icons.ChevronDown,
	iconPaneMaximize:        remoteLogExpandIcon,
	iconPaneRestore:         remoteLogRestoreIcon,
	iconPin:                 icons.Pin,
	iconPinOff:              icons.PinOff,
	iconChevronsExpand:      icons.ChevronsUpDown,
	iconChevronsCollapse:    icons.ChevronsDownUp,
	iconSliders:             icons.SlidersHorizontal,
	iconAlert:               icons.CircleAlert,
	iconCircleCheck:         icons.CircleCheck,
	iconRadio:               icons.RadioTower,
	iconKeyboard:            icons.Keyboard,
	iconStar:                icons.Star,
	iconStarSolid:           icons.StarSolid,
	iconArchive:             icons.Archive,
	iconHardDrive:           icons.HardDrive,
	iconExternal:            icons.SquarePen,
	iconPlug:                icons.Plug,
	iconPaperclip:           icons.Paperclip,
	iconImage:               icons.Image,
	iconSave:                icons.Download,
	iconPaste:               icons.ClipboardPaste,
	iconSelectAll:           icons.SquareDashed,
	iconEraser:              icons.Eraser,
	iconEye:                 icons.Eye,
	iconEyeOff:              icons.EyeOff,
	iconEllipsis:            icons.Ellipsis,
	iconWrench:              icons.Wrench,
	iconProcess:             icons.Process,
	iconDirectoryTree:       icons.PanelLeft,
	iconListTree:            icons.ListTree,
	iconArrowUpDown:         icons.ArrowUpDown,
	iconCloseOthers:         icons.ListX,
	iconCloseRight:          icons.ArrowRightToLine,
	iconSparkles:            icons.Sparkles,
	iconTransfer:            icons.ArrowUpDown,
	iconBug:                 icons.Bug,
	iconInfo:                icons.Info,
	iconQQ:                  icons.Qq,
	iconWeChat:              icons.Wechat,
	iconDiscord:             icons.Discord,
	iconGitHub:              icons.Github,
	iconNotebook:            icons.Notebook,
	iconSnippets:            icons.Zap,
	iconLocalProxy:          icons.Waypoints,
	iconSFTPTransfer:        icons.SftpTransfer,
	iconSFTPBrowseSync:      icons.SftpSync,
	iconSFTPProfiles:        icons.SftpProfiles,
	iconFileClock:           icons.FileClock,
	iconClock:               icons.Clock3,
	iconFollowFolder:        icons.LocateFixed,
	iconSFTPUp:              icons.SftpUp,
	iconSFTPUpload:          icons.SftpUpload,
	iconSFTPUploadDirectory: icons.SftpUploadFolder,
	iconSFTPNewFile:         icons.SftpNewFile,
	iconSFTPNewDirectory:    icons.SftpNewFolder,
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
	return ui.Button(c, label).Variant(variant).Disabled(disabled)
}

// dialogActionFooter declares a footer whose buttons keep the application's
// own identities, for a dialog that opens with one of them focused:
// DialogSpec's InitialFocusID names a hit. Their actions stay in the central
// click dispatcher.
func (state *State) dialogActionFooter(c ui.C, actions ...components.ButtonAction) {
	for _, action := range actions {
		dialogButton(c, action.Label, action.Variant, action.Disabled).Hit(action.ID, action.Label)
	}
}

// dialogActionFooterSmall is dialogActionFooter with the small button size the
// alert dialogs and the old Dialog.Actions gave their footer buttons, so a
// footer whose surface was measured with 28px buttons keeps them 28px high.
func (state *State) dialogActionFooterSmall(c ui.C, actions ...components.ButtonAction) {
	for _, action := range actions {
		dialogButton(c, action.Label, action.Variant, action.Disabled).Small().Hit(action.ID, action.Label)
	}
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
	case modalAppLock:
		state.declareAppLockDialog(c)
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
	case modalLocalStartupCommand:
		state.declareLocalStartupCommandDialog(c)
	case modalDesktopResolution:
		state.declareDesktopResolutionDialog(c)
	case modalAIRename:
		state.declareAIRenameDialog(c)
	case modalHostImportResult:
		state.declareHostImportResult(c)
	case modalRecordingPlayback:
		state.declareRecordingPlayback(c)
	case modalFileProperties:
		state.declareFileProperties(c)
	case modalLocalProxy:
		state.declareLocalProxyDialog(c)
	case modalSessionLink:
		state.declareSessionLinkDialog(c)
	case modalDeleteRule:
		state.declareDeleteRuleDialog(c)
	case modalUpdate:
		state.declareUpdateDialog(c)
	case modalUpdateProgress:
		state.declareUpdateProgress(c)
	case modalProxyProfile:
		state.declareProxyProfileDialog(c)
	case modalNewHost:
		state.declareHostDialog(c)
	case modalTerminalInput:
		state.declareTerminalInputConfirm(c)
	case modalPasteReview:
		state.declarePasteReviewDialog(c)
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
	case modalQuitKeepWindows:
		state.declareQuitKeepWindowsDialog(c)
	case modalRemoteTools:
		state.declareRemoteToolsModal(c)
	case modalSudoSFTP:
		state.declareSudoSFTPDialog(c)
	case modalForm:
		state.declareFormDialog(c)
	case modalFontPicker:
		state.declareFontPicker(c)
	case modalIconColorPicker:
		state.declareIconColorPicker(c)
	case modalOutboundProxy:
		state.declareOutboundProxyDialog(c)
	case modalAutofillRule:
		state.declareAutofillDialog(c)
	case modalAutofillConfirm:
		state.declareAutofillConfirm(c)
	case modalToolbarSettings:
		state.declareToolbarSettings(c)
	case modalSFTPColumns:
		state.declareSFTPColumnSettings(c)
	case modalSidebarSettings:
		state.declareSidebarSettings(c)
	case modalHighlightRule:
		state.declareHighlightDialog(c)
	case modalRuleManager:
		state.declareRuleManager(c)
	case modalNewForward:
		state.declareForwardDialog(c)
	case modalNewProxy:
		state.declareProxyDialog(c)
	case modalProxyBatchAssociate:
		state.declareProxyBatchDialog(c)
	case modalProxyDeleteConfirm:
		state.declareProxyDeleteConfirmDialog(c)
	case modalProxyBatchDeleteConfirm:
		state.declareProxyBatchDeleteConfirmDialog(c)
	case modalXYTransfer:
		state.declareXYDialog(c)
	case modalSnippetDelete:
		state.declareSnippetDeleteDialog(c)
	case modalSnippetGroups:
		state.declareSnippetGroupsDialog(c)
	case modalSnippetGroupRename:
		state.declareSnippetGroupRenameDialog(c)
	case modalSnippetGroupDelete:
		state.declareSnippetGroupDeleteDialog(c)
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
	if state.editor.protocol == protocolWSL {
		state.adoptWSLDistros()
	}
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
	if chrome.hostTestMessageDraw == nil {
		chrome.hostTestMessageDraw = state.drawHostTestMessage
	}
	m, gap := state.messages, c.Theme().Metrics.GapItem
	chrome.hostTestButton, chrome.hostTestMessage = ui.Box{}, ui.Box{}
	chrome.hostCancelButton, chrome.hostSubmitButton = ui.Box{}, ui.Box{}
	ui.Row(c).Fill().Items(ui.Center).Gap(gap).Children(func() {
		if state.hostTestAvailable() {
			chrome.hostTestButton = ui.Button(c, m.HostTest).Outline().
				Hit(hitDialogTest, m.HostTest).Disabled(state.hostTest.outcome == hostTestRunning)
		}
		// The message starts half an item gap nearer than the row's gap, as the
		// painted footer's gap*1.5 from the test button did.
		chrome.hostTestMessage = ui.Slot(c).Grow(1).MarginLeft(gap / 2)
		chrome.hostTestMessage.Draw(chrome.hostTestMessageDraw)
		chrome.hostCancelButton = ui.Button(c, cancel).Secondary().Hit(hitDialogCancel, cancel)
		chrome.hostSubmitButton = ui.Button(c, submit).Hit(hitDialogSubmit, submit)
	})
}

// drawHostTestMessage paints what the last test found into its slot and keeps
// the footer's controls in the form's tab ring.
func (state *State) drawHostTestMessage(d *ui.DrawContext) {
	chrome := &state.chrome
	form := &state.editor.form
	if chrome.hostTestButton.Valid() {
		form.addStop(hitDialogTest, chrome.hostTestButton.Rect(), false)
	}
	if chrome.hostCancelButton.Valid() {
		form.addStop(hitDialogCancel, chrome.hostCancelButton.Rect(), false)
	}
	if chrome.hostSubmitButton.Valid() {
		form.addStop(hitDialogSubmit, chrome.hostSubmitButton.Rect(), false)
	}
	state.paintHostTestMessage(d.Canvas, d.Rect)
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

// declareHostFormBody lays out the host form as one page: the protocol choice,
// the endpoint fields, the pickers and the sections, all inside the body's own
// scroll area. The fields and selects are page controls; the controls whose
// popup carries content a page select cannot (the group, tag and icon pickers,
// the shared-password field) are painted into the slots their rows leave.
func (state *State) declareHostFormBody(c ui.C, title string) {
	editor := &state.editor
	form := &editor.form
	b := &editor.form.body
	ssh := editor.protocol == protocolSSH
	wsl := editor.protocol == protocolWSL
	terminalHost := idleKeepAliveProtocol(editor.protocol) || wsl
	networkHost := editor.protocol != protocolSerial && !wsl
	vncHost := editor.protocol == protocolVNC || editor.protocol == protocolRDP
	rowHeight := float32(hostFormRowHeight)
	// The rows this frame lays out are the ones before a choice writes its new
	// value at the end of the declaration; the form paints the protocol and the
	// algorithm mode they were laid out for.
	form.layoutProtocol = editor.protocol
	form.algoLayout = editor.algoMode
	*b = hostFormBody{}
	// The page controls this form declared last paint are stale now; a box is
	// valid only until the end of the paint that made it.
	form.passwordControl = ui.Box{}
	form.authBox, form.portsBox, form.wslDistroBox = ui.Box{}, ui.Box{}, ui.Box{}
	form.encodingBox, form.iconBox = ui.Box{}, ui.Box{}
	form.idleIntervalBox, form.idleUnitBox, form.idleInputBox = ui.Box{}, ui.Box{}, ui.Box{}
	for index := range form.colorSwatchBoxes {
		form.colorSwatchBoxes[index] = ui.Box{}
	}
	form.colorPickerBox = ui.Box{}
	chrome := &state.chrome
	chrome.hostDialogTitle = title
	if chrome.hostFormViewportDraw == nil {
		chrome.hostFormViewportDraw = state.drawHostFormViewport
		chrome.hostFormFieldsDraw = state.drawHostFormFields
	}
	body := state.hostFormBodyRect(c)
	if form.viewport != body {
		form.lastFocus = gfx.ID{}
	}
	viewport := ui.Column(c).Fill()
	viewport.Children(func() {
		scroller := ui.Scroll(c).Fill().AlwaysScrollbars().ScrollbarSize(hostFormBarWidth).
			ScrollbarIDs(hitHostFormScrollbar, gfx.ID{}).
			ScrollbarLabels(state.chrome.hostDialogTitle, "")
		form.pageScroll = scroller.ScrollState()
		form.scroller = scroller
		scroller.Draw(chrome.hostFormViewportDraw)
		scroller.Children(func() {
			content := ui.Column(c).FillWidth().MinHeight(state.hostFormHeight()).PadSides(2, 4, 0, 4)
			if state.hostFormHeight() > body.Height {
				content = content.MarginRight(12)
			}
			content.Children(func() {
				b.protocol = ui.Slot(c).Height(34).MarginSides(0, 0, 10, 0)
				if editor.moshTerminal() {
					ui.Paragraph(c, state.messages.Tools.MoshHint, 2).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(state.pal.muted).LineHeight(16)
				}
				ui.Row(c).Height(rowHeight).Gap(8).Children(func() {
					b.label = ui.Slot(c).Grow(1)
					b.label.Children(func() {
						form.fieldLabel.declare(c, state, &editor.label, dialogFieldSpec{
							ID: hitDialogLabel, Label: state.messages.FieldLabel, Placeholder: state.messages.FieldLabelPlaceholder, Height: rowHeight,
						})
					})
					b.addField(hitDialogLabel, &form.fieldLabel)
					b.colorField = ui.Slot(c).Grow(1)
					state.declareHostColorChoice(c, b.colorField, rowHeight)
				})
				b.endpoint = ui.Slot(c).Height(float32(state.hostEndpointRows()) * rowHeight)
				state.declareHostEndpointSlots(c, b.endpoint, rowHeight)
				ui.Row(c).Height(rowHeight).Gap(8).Children(func() {
					b.group = ui.Slot(c).Grow(1)
					b.tags = ui.Slot(c).Grow(1)
				})
				ui.Row(c).Height(rowHeight).Gap(8).Children(func() {
					b.iconField = ui.Slot(c).Grow(1)
					state.declareHostIconPicker(c, b.iconField, rowHeight)
					if ssh && !editor.moshTerminal() {
						b.transfer = ui.Slot(c).Grow(1)
					}
					if state.hostShowsEncoding() {
						b.encoding = ui.Slot(c).Grow(1)
						state.declareHostEncodingPicker(c, b.encoding, rowHeight)
					}
				})
				if networkHost {
					// The proxy is its own row: sharing the icon row clipped both the
					// icon value and the proxy summary on VNC, RDP and Telnet.
					b.proxy = state.declareHostProxyRow(c)
				}
				if ssh || vncHost {
					b.jump = state.declareHostJumpRow(c)
				}
				if terminalHost {
					b.advanced = ui.Slot(c).Height(state.advancedSectionHeight())
					advanced := b.advanced
					advanced.Children(func() {
						b.advancedHeader, b.advancedHeaderIcon = state.declareHostAdvancedHeader(c)
						if editor.advancedOpen {
							if ssh {
								// The rows and the choice are declared as the same overlay:
								// the choice keeps its press for after the rows laid out.
								ui.Column(c).Absolute().Left(0).Right(0).Top(advancedHeaderHeight).Children(func() {
									state.declareHostAlgoRows(c)
								})
								ui.Column(c).Absolute().Left(0).Right(0).Top(advancedHeaderHeight).Height(rowHeight).Children(func() {
									b.algo = state.declareHostAlgoChoice(c)
								})
							} else {
								ui.Column(c).Absolute().Left(0).Right(0).Top(advancedHeaderHeight).Children(func() {
									if wsl {
										state.declareWSLAdvanced(c, rowHeight)
										return
									}
									state.declareHostIdleKeepAliveRows(c)
								})
							}
						}
					})
				}
				if vncHost {
					b.vncRows = ui.Slot(c).Height(state.desktopRowsHeight())
					b.vncRows.Children(func() { state.declareHostVNCRows(c) })
				}
				if state.dialogError != "" {
					// The serial line's error is long: up to two lines.
					ui.TextSelectionArea(c, &state.dialogErrorSelection).Children(func() {
						ui.Paragraph(c, state.dialogError, 2).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(state.pal.danger).LineHeight(16)
					})
					if state.isHostDialogKeyLost() {
						ui.Row(c).Height(30).Items(ui.Center).MarginSides(8, 0, 0, 0).Children(func() {
							ui.Button(c, state.messages.SettingsVaultForgetAll).Small().
								DestructiveOutline().
								Hit(hitDialogVaultForget, state.messages.SettingsVaultForgetAll)
						})
					}
				}
				// The choices are declared after the rows they sit in: the frame that
				// delivers a click lays out what it did before it, the choice writes
				// its value at the end, and the next frame draws it.
				if editor.protocol == protocolSerial {
					// The device and line rows are the page's fields; the flow
					// control and the Backspace key share the row under them.
					endpoint := b.endpoint
					endpoint.Children(func() {
						ui.Column(c).Absolute().Left(0).Right(0).Top(2 * rowHeight).Children(func() {
							ui.Row(c).Height(rowHeight).Gap(8).Children(func() {
								// A zero basis: with Grow alone the wider choice starts ahead and
								// takes more than half the row.
								ui.Column(c).Basis(0).Grow(1).Children(func() {
									b.flow = state.declareHostFlowChoice(c)
								})
								ui.Column(c).Basis(0).Grow(1).Children(func() {
									b.backspace = state.declareHostBackspaceChoice(c)
								})
							})
						})
					})
				}
				if editor.protocol == protocolTelnet {
					// Telnet has the Backspace key alone, under its address.
					endpoint := b.endpoint
					endpoint.Children(func() {
						ui.Column(c).Absolute().Left(0).Right(0).Top(rowHeight).Height(rowHeight).Children(func() {
							b.backspace = state.declareHostBackspaceChoice(c)
						})
					})
				}
				if ssh && !editor.moshTerminal() {
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
	})
	state.declareHostCredentialMenu(c)
}

// drawHostFormViewport names the scroll viewport of the host form, whose title
// the declaration keeps.
func (state *State) drawHostFormViewport(d *ui.DrawContext) {
	if d.Rect.Width > 0 && d.Rect.Height > 0 {
		d.Canvas.HitWithOptions(hitHostFormViewport, d.Rect, state.chrome.hostDialogTitle, gfx.HitOptions{Role: gfx.RoleGroup})
	}
}

// drawHostFormFields paints the form's controls into the slots the page laid
// out.
func (state *State) drawHostFormFields(d *ui.DrawContext) {
	state.paintHostFormFields(d.Canvas)
}

// hostFormBarWidth is the form's scroll bar: it keeps a strip the same width as
// the hosts list's bar.
const hostFormBarWidth = float32(10)

// hostFormBodyRect is where the dialog lays the form's body, as the dialog
// declaration lays it out.
func (state *State) hostFormBodyRect(c ui.C) gfx.Rect {
	layout := dialogContentRect(c.Canvas(), *c.Theme(), dialogViewport(c), 680, "", state.hostFormHeight())
	return layout.Body
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

// paintHostFormFields keeps the page's controls in the form's tab ring and
// paints the controls whose popup carries content a page select cannot.
func (state *State) paintHostFormFields(canvas *gfx.Canvas) {
	editor := &state.editor
	form := &editor.form
	if form.scroller.Valid() {
		form.viewport = form.scroller.Rect()
	}
	if form.pageScroll != nil {
		form.offset, form.maxOffset = form.pageScroll.Offset, form.pageScroll.Max()
	}
	editor.form.clipped = true
	// The endpoint's page text fields, and the label, are stops even while they
	// lie outside the clipped viewport.
	for i := 0; i < form.body.fieldCount; i++ {
		ref := form.body.fields[i]
		state.revealHostField(canvas, ref.id, ref.field.box.Rect())
	}
	if form.authBox.Valid() {
		state.rememberHostPageSelect(canvas, form.authBox, hitDialogAuth)
	}
	if form.portsBox.Valid() {
		state.rememberHostPageSelect(canvas, form.portsBox, hitDialogPortsTrigger)
	}
	if form.wslDistroBox.Valid() {
		state.rememberHostPageSelect(canvas, form.wslDistroBox, hitDialogWSLDistro)
	}
	if form.encodingBox.Valid() {
		state.rememberHostPageSelect(canvas, form.encodingBox, hitDialogEncoding)
	}
	if form.iconBox.Valid() {
		state.rememberHostPageSelect(canvas, form.iconBox, hitDialogIcon)
	}
	state.rememberHostIdleSelects(canvas)
	state.rememberHostColor(canvas)
	// Which sections this frame's body laid out is fixed before the page's
	// protocol choice writes the protocol a click chose; paint those sections,
	// as the hand-painted endpoint rows paint the protocol now in effect.
	protocol := form.layoutProtocol
	ssh := protocol == protocolSSH
	networkHost := protocol != protocolSerial && protocol != protocolWSL
	vncHost := protocol == protocolVNC || protocol == protocolRDP
	b := &editor.form.body
	state.paintHostEndpointRows(canvas)
	state.paintHostGroupPicker(canvas, b.group.Rect())
	state.paintHostTags(canvas, b.tags.Rect())
	if networkHost {
		state.paintHostProxyRow(canvas)
	}
	if ssh || vncHost {
		state.paintHostJumpRow(canvas)
	}
	if vncHost {
		state.paintVNCRows(canvas)
	}
	if idleKeepAliveProtocol(protocol) || protocol == protocolWSL {
		state.paintAdvancedSection(canvas)
	}
	// The checkboxes and the choices are the page's controls: keep them in the form's
	// stops, by the identity their paint reported.
	state.rememberDialogChecks()
	state.rememberHostChoice(canvas, b.protocol.Rect(), &editor.form.choiceIDs.protocol, state.hostProtocolChoiceLabel(editor.protocol))
	if b.flow.Valid() {
		state.rememberHostChoice(canvas, b.flow.Rect(), &editor.form.choiceIDs.flow, state.hostFlowChoiceLabel(editor.flow))
	}
	if b.backspace.Valid() {
		state.rememberHostChoice(canvas, b.backspace.Rect(), &editor.form.choiceIDs.backspace, state.hostBackspaceChoiceLabel(editor.backspace))
	}
	if b.transfer.Valid() {
		state.rememberHostChoice(canvas, b.transfer.Rect(), &editor.form.choiceIDs.transfer, state.hostTransferChoiceLabel(editor.transfer))
	}
	if b.algo.Valid() {
		state.rememberHostChoice(canvas, b.algo.Rect(), &editor.form.choiceIDs.algo, state.hostAlgoChoiceLabel(editor.algoMode))
	}
	if editor.form.algoBody.loginPolicy.Valid() {
		state.rememberHostChoice(canvas, editor.form.algoBody.loginPolicy.Rect(), &editor.form.choiceIDs.loginPolicy, state.hostLoginPolicyChoiceLabel(editor.loginFailurePolicy))
	}
	if editor.form.algoBody.backspace.Valid() {
		state.rememberHostChoice(canvas, editor.form.algoBody.backspace.Rect(), &editor.form.choiceIDs.backspace, state.hostBackspaceChoiceLabel(editor.backspace))
	}
	editor.form.clipped = false
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
		ui.TextSelectionArea(c, &state.dialogErrorSelection).Children(func() {
			ui.Text(c, state.dialogError).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(state.pal.danger).LineHeight(20)
		})
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
	case hostGroupCreateRoot:
		title, submit, label = state.messages.NewGroup, state.messages.HostGroupActions.Create, state.messages.HostGroupActions.Name
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

// The delete-a-group dialog's body is the two choice rows; a refusal notice,
// when shown, adds its measured lines above them.
const (
	groupDeleteChoiceRows = float32(130)
	groupDeleteNoticeLine = float32(20)
	groupDeleteNoticeGap  = float32(4)
)

// declareHostGroupDeleteDialog declares the delete-group variant of the group
// dialog: two radio choices for what happens to the hosts in it.
func (state *State) declareHostGroupDeleteDialog(c ui.C) {
	m := state.messages.HostGroupActions
	body := groupDeleteChoiceRows
	if state.dialogError != "" {
		// The reason can name a host, so it may wrap: give it as many lines as its
		// words take at the body's width, measured the way it is drawn.
		probe := dialogContentRect(c.Canvas(), *c.Theme(), dialogViewport(c), 480, "", body)
		var scratch [8]string
		lines := c.Canvas().WrapLines(scratch[:0], state.dialogError, metrics.FontSizeS, gfx.WeightMedium, max(probe.Body.Width, 1), math.MaxInt32)
		body += float32(len(lines))*groupDeleteNoticeLine + groupDeleteNoticeGap
	}
	layout := dialogContentRect(c.Canvas(), *c.Theme(), dialogViewport(c), 480, "", body)
	description := elide(c.Canvas(), state.groupActions.path, c.Theme().Metrics.FontSizeM, gfx.WeightRegular, layout.Header.Width)
	viewport := dialogViewport(c)
	height := dialogContentHeight(c.Canvas(), *c.Theme(), viewport, 480, description, body)
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: m.Delete, Description: description, Width: 480, Height: height, Layer: dialogLayer,
		Scope: dialogScope, DismissID: hitMoveGroupDismiss, CloseID: hitMoveGroupClose, InitialFocusID: hitGroupDeleteParent,
	})
	dialog.Body(func() {
		choices := state.groupDeleteChoices()
		ui.Column(c).Fill().Children(func() {
			// A deletion the parent group cannot take is refused, and the dialog
			// is the only place that can say why: it stays open, so the reason
			// goes here rather than into the status line behind the scrim.
			if state.dialogError != "" {
				ui.TextSelectionArea(c, &state.dialogErrorSelection).Children(func() {
					ui.Paragraph(c, state.dialogError, 0).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(state.pal.danger).LineHeight(groupDeleteNoticeLine)
				})
			}
			// The body's remaining 130 pixels are the two choices' rows, as the
			// hand-painted group stretched them: a radio centres its circle and
			// label in the row.
			ui.RadioGroup(c).Fill().Children(func() {
				ui.Radio(c, &state.groupActions.ungroup, false, choices[0], ui.WithID(hitGroupDeleteParent)).Grow(1)
				ui.Radio(c, &state.groupActions.ungroup, true, choices[1], ui.WithID(hitGroupDeleteUngroup)).Grow(1)
			})
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

// declareMoveGroupBody declares the one field the dialog shows. The relocate
// case keeps its custom parent select painted into the field's control slot.
func (state *State) declareMoveGroupBody(c ui.C, label string, viewport gfx.Rect) {
	if state.groupActions.op != hostGroupRelocate {
		ui.Column(c).Fill().Children(func() {
			state.groupActions.input.declare(c, state, &state.hostGroupEditor, dialogFieldSpec{
				ID: hitMoveGroupField, Label: label, Placeholder: state.messages.FieldGroupPlaceholder,
				Error: state.dialogError, Height: 90,
			})
		})
		return
	}
	column := ui.Column(c).Fill()
	column.Children(func() {
		state.groupActions.parentField.Error = state.dialogError
		state.groupActions.parentField.Invalid = state.dialogError != ""
		state.groupActions.parentField.For = hitMoveGroupField
		ui.FormField(c, &state.groupActions.parentField, label).FillWidth().Height(90).Children(func() {
			state.groupActions.slot = ui.Slot(c).Fill()
		})
	})
	column.DrawOver(func(d *ui.DrawContext) {
		content := state.groupActions.slot.Rect()
		content.Height = 32
		state.paintHostGroupParentSelect(d.Canvas, content, viewport)
	})
}

// hostSystemChoices are the systems whose badge a host can be given, listed A-Z
// by name. The names are the systems' own, the same in every language.
var hostSystemChoices = [...]struct {
	system sshconn.RemoteSystem
	name   string
}{
	{sshconn.SystemAIX, "AIX"},
	{sshconn.SystemAlibabaCloudLinux, "Alibaba Cloud Linux"},
	{sshconn.SystemAlma, "AlmaLinux"},
	{sshconn.SystemAlpine, "Alpine"},
	{sshconn.SystemAmazonLinux, "Amazon Linux"},
	{sshconn.SystemAnolis, "Anolis OS"},
	{sshconn.SystemArch, "Arch Linux"},
	{sshconn.SystemAzureLinux, "Azure Linux"},
	{sshconn.SystemCentOS, "CentOS"},
	{sshconn.SystemDebian, "Debian"},
	{sshconn.SystemDeepin, "deepin"},
	{sshconn.SystemElementary, "elementary OS"},
	{sshconn.SystemFedora, "Fedora"},
	{sshconn.SystemFnOS, "fnOS"},
	{sshconn.SystemFreeBSD, "FreeBSD"},
	{sshconn.SystemGentoo, "Gentoo"},
	{sshconn.SystemKali, "Kali Linux"},
	{sshconn.SystemKylin, "Kylin"},
	{sshconn.SystemLinux, "Linux"},
	{sshconn.SystemMint, "Linux Mint"},
	{sshconn.SystemDarwin, "macOS"},
	{sshconn.SystemManjaro, "Manjaro"},
	{sshconn.SystemNixOS, "NixOS"},
	{sshconn.SystemOpenMediaVault, "OpenMediaVault"},
	{sshconn.SystemOpenSUSE, "openSUSE"},
	{sshconn.SystemOpenWrt, "OpenWrt"},
	{sshconn.SystemOracleLinux, "Oracle Linux"},
	{sshconn.SystemPopOS, "Pop!_OS"},
	{sshconn.SystemProxmox, "Proxmox VE"},
	{sshconn.SystemQNAP, "QNAP QTS"},
	{sshconn.SystemRaspberryPi, "Raspberry Pi OS"},
	{sshconn.SystemRedHat, "Red Hat"},
	{sshconn.SystemRocky, "Rocky Linux"},
	{sshconn.SystemSlackware, "Slackware"},
	{sshconn.SystemSynology, "Synology DSM"},
	{sshconn.SystemTencentOS, "TencentOS"},
	{sshconn.SystemTrueNAS, "TrueNAS"},
	{sshconn.SystemUbuntu, "Ubuntu"},
	{sshconn.SystemUnraid, "Unraid"},
	{sshconn.SystemUOS, "UOS"},
	{sshconn.SystemESXi, "VMware ESXi"},
	{sshconn.SystemVoid, "Void Linux"},
	{sshconn.SystemWindows, "Windows"},
	{sshconn.SystemZorin, "Zorin OS"},
}

// hostSystemDisplayName is the system's own name, the same in every language.
// It returns "" for a system the list does not name.
func hostSystemDisplayName(system sshconn.RemoteSystem) string {
	for _, choice := range hostSystemChoices {
		if choice.system == system {
			return choice.name
		}
	}
	return ""
}

// hostIconLabel is the name of an icon in the list.
func (state *State) hostIconLabel(icon hostIcon) string {
	m := state.messages
	if system, ok := icon.system(); ok {
		if name := hostSystemDisplayName(system); name != "" {
			return name
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
// run or, for RDP, the one its protocol implies, until there is one.
func (state *State) hostIconArt(icon hostIcon, system sshconn.RemoteSystem, protocol hostProtocol) gfx.Icon {
	if icon == hostIconAuto {
		if art, ok := systemBadgeIcon(autoIconSystem(system, protocol), state.pal.dark); ok {
			return art
		}
		if protocol.isDesktop() {
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
// hostKeyRow is one labelled line of the host-key dialog's body.
type hostKeyRow struct{ label, value string }

func (state *State) declareHostKeyBody(c ui.C) {
	session := state.promptSession()
	if session == nil {
		return
	}
	m, pal := state.messages, state.pal
	// The rows reuse a slice on the state so a steady paint does not allocate
	// the list of three lines (and the saved fingerprints) every frame.
	rows := append(state.hostKeyRows[:0],
		hostKeyRow{m.FieldAddress, session.hostKey.Address},
		hostKeyRow{m.HostKeyType, session.hostKey.Type},
		hostKeyRow{m.HostKeyFingerprint, session.hostKey.Fingerprint},
	)
	labelWidth := float32(90)
	if session.hostKey.Changed {
		labelWidth = 110
		for _, fingerprint := range session.hostKey.Known {
			rows = append(rows, hostKeyRow{m.HostKeySavedFingerprint, fingerprint})
		}
	}
	state.hostKeyRows = rows
	// The user checks the fingerprint against one the server's owner gave them,
	// often by pasting it: the rows are selectable.
	ui.TextSelectionArea(c, &state.hostKeySelection).Fill().Gap(8).Children(func() {
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
	multi := len(state.passwordQuestions) > 1
	if len(state.passwordInputs) != len(state.passwordQuestions) {
		state.passwordInputs = make([]dialogField, len(state.passwordQuestions))
	}
	state.chrome.passwordSave = ui.Box{}
	column := ui.Column(c).Fill().PadSides(2, 4, 0, 4)
	column.Children(func() {
		for index, question := range state.passwordQuestions {
			label := strings.TrimSpace(question.Text)
			if label == "" {
				label = state.messages.PasswordLabel
			}
			ui.Column(c.KeyInt(index)).Height(62).MarginSides(0, 0, 4, 0).Children(func() {
				state.passwordInputs[index].declare(c, state, &state.passwordFields[index], dialogFieldSpec{
					ID: hitPasswordFields.At(index + 1), Label: label, Secret: question.Secret,
					Invalid: state.passwordError != "", Height: 62,
				})
			})
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
			state.keyPassphraseInput.declare(c, state, &state.keyPassphrase, dialogFieldSpec{
				ID: hitKeyPassphraseField, Label: m.KeyPassphraseLabel, Description: m.KeyPassphraseHint,
				Secret: true, Invalid: state.keyPassphraseError != "", Height: 62,
			})
			if state.keyPassphraseError != "" {
				ui.TextSelectionArea(c, &state.keyPassphraseErrorSelection).Children(func() {
					ui.Text(c, state.keyPassphraseError).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(state.pal.danger).LineHeight(18).MarginSides(12, 0, 0, 0)
				})
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
	})
	dialog.Footer(func() {
		state.dialogActionFooter(c,
			components.ButtonAction{ID: hitKeyPassphraseSkip, Label: m.KeyPassphraseSkip, Variant: components.ButtonSecondary},
			components.ButtonAction{ID: hitKeyPassphraseSubmit, Label: m.KeyUnlock},
		)
	})
}
