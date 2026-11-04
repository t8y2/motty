package workbench

import (
	"strings"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/flex"
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
)

// terminalPage reports whether the window shows the panes of a workspace rather
// than a section card under the navigation rail. A terminal section without a
// workspace still shows the section, so it is not a terminal page.
func (state *State) terminalPage() bool {
	return state.activeSection == sectionTerminal && state.deck.ActiveWorkspace() != 0
}

// Paint renders the application from state using stable hit identities.
func (state *State) Paint(canvas *ui.Canvas, size ui.Size) {
	canvas.SetTree(&tree)
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
	state.toolbarDock.zones = [7]ui.Rect{}
	state.toolbarDock.bottomViewports = [2]ui.Rect{}
	state.toolbarDock.sideViewports = [4]ui.Rect{}
	state.contextRect = ui.Rect{}
	state.hostsBounds, state.chrome.hostList = ui.Rect{}, ui.Rect{}
	state.verticalTabs.bounds, state.verticalTabs.slot = ui.Rect{}, flex.Box{}
	state.devBeginPaint()
	state.ai.cfg.mcpSelectBounds = ui.Rect{}
	state.ai.cfg.agentSelectBounds = ui.Rect{}
	state.localShellMenu.bounds = ui.Rect{}
	state.editorMenu.bounds = ui.Rect{}
	state.fileManagerRect = ui.Rect{}
	if state.activeSection != sectionSettings || state.settingsCategory != settingsFiles || state.settingsSearching() {
		state.editorMenu.open = false
	}
	if state.activeSection != sectionSettings || state.settingsCategory != settingsTerminal || state.settingsSearching() {
		state.localShellMenu.open = false
	}
	state.ai.panel.targetBounds = ui.Rect{}
	if !state.aiSettingsVisible() || state.settingsCategory != settingsMCP || state.settingsSearching() {
		state.ai.cfg.mcpSelectOpen = false
		state.ai.cfg.agentSelectOpen = false
	}
	state.aiPanelBeginPaint()
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
	state.cellWidth, state.cellHeight = canvas.MonoCellSize(state.terminalFontSize, state.terminalFontFace)
	if session := state.fullscreenDesktop(); session != nil {
		state.beginMenus()
		state.paintDesktopPage(canvas, size, session)
		state.endMenus()
		state.tooltip.Paint(canvas, state.Theme(), state.windowRect(), components.TooltipOptions{Layer: tooltipLayer, SideOffset: tooltipGap}, components.HoverTooltipDelay)
		state.fileNotices.paint(canvas, state.Theme(), state.windowRect())
		return
	}
	pal := state.pal
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
		state.fileTabStrip.area = ui.Rect{}
		state.fileTabStrip.max = 0
		state.fileTabStrip.offset = 0
		if !terminal {
			state.tabStrip.area = ui.Rect{}
		}
	}
	var workspaceSlot flex.Box
	// The section and assistant are sibling cards, separated by the same margin
	// the cards keep from the window's right and bottom edges.
	cardRight := sectionCardMargin
	if reserve := state.aiSectionReserve(size); reserve > 0 {
		cardRight = reserve
	}
	cardRight += state.toolbarDockSideReserveFor(false, false)
	f := chrome.window.Begin(canvas, state.Theme(), ui.Rect{Width: size.Width, Height: size.Height})
	root := f.Column().Fill()
	if !picture {
		root.Background(pal.shell)
	}
	root.Children(func() {
		state.declareWindowBar(f, &chrome.bar, false, picture, geometry, scroll)
		if !fullPage {
			f.Row().Grow(1).Shrink(1).MarginSides(0, 0, state.toolbarDockBottomReserve(false), 0).Children(func() {
				f.Slot().Width(state.toolbarDockSideReserveFor(true, false)).Shrink(0)
				state.declareNavigation(f)
				workspaceSlot = f.Slot().Grow(1).MarginSides(0, cardRight, sectionCardMargin, 0).
					Radius(radius.surface).Border(workspaceBorder, pal.windowStroke).Background(pal.workspace)
			})
		}
	})
	f.End()
	state.paintWindowBarParts(canvas, geometry.area)
	if !fullPage {
		state.paintLockup(canvas, chrome.lockup.Rect())
	}

	// The embedded file header also paints a popup: start the shared menu
	// lifecycle before workspace content so its scroll/focus state survives.
	state.beginMenus()
	if terminal {
		state.paintWorkspacePanes(canvas, size)
		state.paintIndependentMonitor(canvas, size)
		state.paintWorkspaceSideRail(canvas, size)
		state.paintWorkspacePaneTools(canvas)
		// Assistant popovers may intentionally extend to the window edge; paint
		// the rail first so they can remain above it and nothing is clipped.
		state.paintAIPanel(canvas, size)
	} else if fileManager {
		state.paintSFTPTab(canvas, state.fileManagerPixelBounds(size))
		state.paintAIPanel(canvas, size)
	} else {
		workspace := workspaceSlot.Rect().Inset(ui.All(workspaceBorder))
		canvas.Clip(workspace)
		if !state.devPaintPage(canvas, workspace) {
			switch state.activeSection {
			case sectionWorkspaces:
				state.paintWorkspaceLibrary(canvas, workspace)
			case sectionDashboard:
				state.paintDashboard(canvas, workspace)
			case sectionKeys:
				state.paintKeysWorkspace(canvas, workspace)
			case sectionForwards:
				state.paintForwardsWorkspace(canvas, workspace)
			case sectionProxies:
				state.paintProxiesWorkspace(canvas, workspace)
			case sectionSettings:
				state.paintSettingsWorkspace(canvas, workspace)
			case sectionKnownHosts:
				state.paintKnownHostsWorkspace(canvas, workspace)
			case sectionSnippets:
				state.paintSnippetsWorkspace(canvas, workspace)
			case sectionSessionLogs:
				state.paintSessionLogsWorkspace(canvas, workspace)
			default:
				state.paintHostsWorkspace(canvas, workspace)
			}
		}
		canvas.Unclip()
		state.paintAIPanel(canvas, size)
	}
	if !terminal {
		state.paintToolbarDockSide(canvas, size)
	}
	state.paintToolbarDockBottom(canvas, size)
	state.paintVerticalTabs(canvas, size)
	viewport := state.windowRect()
	state.paintAITargetSelect(canvas)
	state.paintMCPClientSelect(canvas)
	state.paintLocalShellSelect(canvas)
	state.paintEditorSelect(canvas)
	state.paintMonitorIntervalMenu(canvas)
	state.paintDashboardMenu(canvas)
	state.paintSessionLogFilters(canvas)
	state.paintTransferCenter(canvas)
	// The gallery's select is painted here, outside the workspace's clip and
	// inside the frame's menu pass, so its popup can leave the page and keeps the
	// menu state its keyboard handling needs.
	state.devPaintOverlay(canvas)
	switch state.modal {
	case modalTerminalInput:
		state.paintTerminalInputConfirm(canvas, viewport)
	case modalDashboardAdd:
		state.paintDashboardAddMenu(canvas)
	case modalDashboard:
		state.paintDashboardDialog(canvas, viewport)
	case modalNewHost:
		state.paintHostDialog(canvas, viewport)
	case modalDeleteHost:
		state.paintDeleteHostDialog(canvas, viewport)
	case modalMoveHostGroup:
		state.paintMoveHostGroupDialog(canvas, viewport)
	case modalRenameTab:
		state.paintTabRenameDialog(canvas, viewport)
	case modalAIRename:
		state.paintAIRenameDialog(canvas, viewport)
	case modalCloseTabs:
		state.paintCloseTabsDialog(canvas, viewport)
	case modalSessionLink:
		state.paintSessionLinkDialog(canvas, viewport)
	case modalHostImportResult:
		state.paintHostImportResult(canvas, viewport)
	case modalSessionImportPreview:
		state.paintSessionImportPreview(canvas, viewport)
	case modalMobaEncoding:
		state.paintMobaEncoding(canvas, viewport)
	case modalNewForward:
		state.paintForwardDialog(canvas, viewport)
	case modalNewProxy:
		state.paintProxyDialog(canvas, viewport)
	case modalHostKey:
		state.paintHostKeyDialog(canvas, viewport)
	case modalKeyPassphrase:
		state.paintKeyPassphraseDialog(canvas, viewport)
	case modalPassword:
		state.paintPasswordDialog(canvas, viewport)
	case modalVaultUnlock:
		state.paintVaultUnlockDialog(canvas, viewport)
	case modalVaultForget:
		state.paintVaultForgetDialog(canvas, viewport)
	case modalHighlightRule:
		state.paintHighlightDialog(canvas, viewport)
	case modalAutofillRule:
		state.paintAutofillDialog(canvas, viewport)
	case modalAutofillConfirm:
		state.paintAutofillConfirm(canvas, viewport)
	case modalDeleteRule:
		state.paintDeleteRuleDialog(canvas, viewport)
	case modalSnippet:
		state.paintSnippetDialog(canvas, viewport)
	case modalImportSessions:
		state.paintImportSessionsDialog(canvas, viewport)
	case modalFontPicker:
		state.paintFontPicker(canvas, viewport)
	case modalOutboundProxy:
		state.paintOutboundProxyDialog(canvas, viewport)
	case modalForm:
		state.paintFormDialog(canvas, viewport)
	case modalRemoteTools:
		state.paintRemoteTools(canvas, viewport)
	case modalToolbarSettings:
		state.paintToolbarSettings(canvas, viewport)
	case modalSFTPColumns:
		state.paintSFTPColumnSettings(canvas, viewport)
	case modalRemoteClipboard:
		state.paintRemoteClipboardDialog(canvas, viewport)
	case modalFileTransfer:
		state.paintFileTransferDialog(canvas, viewport)
	case modalXYTransfer:
		state.paintXYDialog(canvas, viewport)
	case modalOpenLink:
		state.paintLaunchDialog(canvas, viewport)
	case modalStateNotice:
		state.paintStateNotice(canvas, viewport)
	case modalDevelopmentImport:
		state.paintDevelopmentImport(canvas, viewport)
	case modalUpdate:
		state.paintUpdateDialog(canvas, viewport)
	case modalUpdateProgress:
		state.paintUpdateProgress(canvas, viewport)
	case modalFileProperties:
		state.paintFileProperties(canvas, viewport)
	case modalCommandPalette:
		state.paintCommandPalette(canvas, viewport)
	case modalHistory:
		state.paintHistory(canvas, viewport)
	case modalShortcutCapture:
		state.paintShortcutCaptureDialog(canvas, viewport)
	case modalWorkspaceDraft:
		state.paintWorkspaceDraft(canvas, viewport)
	case modalWorkspaceSave:
		state.paintWorkspaceSaveDialog(canvas, viewport)
	case modalWorkspaceTemplateDelete:
		state.paintWorkspaceTemplateDelete(canvas, viewport)
	case modalLocalProxy:
		state.paintLocalProxyDialog(canvas, viewport)
	}
	if state.knownHosts.action != knownHostsActionNone {
		state.paintKnownHostsDialog(canvas, viewport)
	}
	state.paintAIDialogs(canvas, viewport)
	state.paintTerminalMenu(canvas, viewport)
	state.paintTabDrag(canvas, viewport)
	state.paintToolbarDockDrag(canvas)
	state.paintToolbarDockMenu(canvas, viewport)
	state.paintTabMenu(canvas, viewport)
	state.paintHostMenu(canvas, viewport)
	state.paintHostGroupActions(canvas, viewport)
	state.paintWorkspaceTemplateMenu(canvas, viewport)
	state.paintLocalShellPicker(canvas, viewport)
	state.paintSnippetMenu(canvas, viewport)
	state.paintSFTPMenu(canvas, viewport)
	state.paintHostMenus(canvas, viewport)
	state.endMenus()
	state.requestReconnectFrames(canvas)
	state.requestAutofillFrames(canvas)
	state.requestLoginCommandFrames(canvas)
	state.paintLocalProxyFocus(canvas)
	// Last, so a tooltip is never covered by what it describes.
	state.tooltip.Paint(canvas, state.Theme(), viewport, components.TooltipOptions{Layer: tooltipLayer, SideOffset: tooltipGap}, components.HoverTooltipDelay)
	state.fileNotices.paint(canvas, state.Theme(), ui.Rect{Y: windowBarHeight, Width: size.Width, Height: max(size.Height-windowBarHeight, 0)})
	if !state.workspaceTemplateFocusPending.IsZero() {
		if state.activeSection == sectionWorkspaces && state.modal == modalNone {
			canvas.RequestFocus(state.workspaceTemplateFocusPending)
		}
		state.workspaceTemplateFocusPending = ui.ID{}
	}
}

// windowRect is the whole window as last painted: the viewport a popup gives
// its Viva component so a press anywhere outside it dismisses it.
func (state *State) windowRect() ui.Rect {
	return ui.Rect{Width: state.viewport.Width, Height: state.viewport.Height}
}

// fileManagerPixelBounds reserves the same side rail and assistant card that
// ordinary sections use. The file manager remains a main-window tab, but it no
// longer paints underneath controls that must receive input.
func (state *State) fileManagerPixelBounds(size ui.Size) ui.Rect {
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
	return ui.Rect{
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
	barGap        = float32(4)
	barTabMin     = float32(96)
	barTabMax     = float32(170)
)

// windowBar keeps the places the window bar's own painters need once it is laid
// out: the assistant's button, the room the active session's name is drawn in,
// and, for each tab, where its system and its light go.
type windowBar struct {
	assistant  flex.Box
	transfers  flex.Box
	title      flex.Box
	titleLabel string
	tabs       []barTab
}

type barTab struct {
	badge, light flex.Box
	title        flex.Box
	output       outputAlertKind
	label        string
	workspace    layoutcore.WorkspaceInfo
	fileSession  uint32
	id, closeID  ui.ID
	background   ui.Color
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
	orientation       tabOrientation
	leading, trailing float32
	valid             bool
	area, row         ui.Rect
	tabWidth          float32
}

// windowBarGeometry lays the bar out once without its contents to find out how
// wide the tabs come out: they stay barTabMax wide and shrink together to barTabMin
// to share the room left of the buttons.
func (state *State) windowBarGeometry(canvas *ui.Canvas, width float32) barGeometry {
	chrome := &state.chrome
	leading, trailing := app.TitlebarInsets()
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
		geometry.leading == leading && geometry.trailing == trailing {
		return *geometry
	}
	f := chrome.probe.Begin(canvas, state.Theme(), ui.Rect{Width: width, Height: windowBarHeight})
	f.Column().Fill().Children(func() {
		state.declareWindowBar(f, &chrome.probeBar, true, false, barGeometry{locked: locked}, 0)
	})
	f.End()
	*geometry = barGeometry{
		width: width, tabCount: tabCount, barButtons: buttons, leading: leading, trailing: trailing, valid: true,
		locked:      locked,
		orientation: state.tabOrientation,
		area:        chrome.probeArea.Rect(), row: chrome.probeRow.Rect(), tabWidth: barTabMax,
	}
	if len(chrome.probeBar.tabs) > 0 {
		geometry.tabWidth = chrome.probeTabs[0].Rect().Width
	}
	return *geometry
}

// declareWindowBar declares the strip the custom titlebar leaves for the app: the
// hosts button and one tab per session on the left, the local terminal, search,
// language, theme and settings buttons on the right. The tabs share the room left of
// the buttons; when even the narrowest do not fit they scroll under a clip, and the
// new-tab button stays at the end of that room instead of being pushed out of the
// window. With probe set only the places are declared, for windowBarGeometry.
func (state *State) declareWindowBar(f *flex.Frame, bar *windowBar, probe, picture bool, geometry barGeometry, scroll float32) {
	m, pal, chrome := state.messages, state.pal, &state.chrome
	leading, trailing := app.TitlebarInsets()
	visibleFileTabs := 0
	for _, tab := range state.sftpTabs {
		if tab != nil && !tab.closed {
			visibleFileTabs++
		}
	}
	bar.tabs = bar.tabs[:0]
	bar.title = flex.Box{}
	bar.titleLabel = ""
	if probe {
		chrome.probeTabs = chrome.probeTabs[:0]
	}
	row := f.Row().Height(windowBarHeight).PadSides(0, trailing+barMargin, 0, leading+barMargin).Items(flex.Center)
	if !probe {
		// The bar is where the window is moved from; its buttons and tabs, declared
		// after it, take their own presses.
		row.MovesWindow()
	}
	if picture {
		row.Background(pal.shell)
	}
	row.Children(func() {
		state.barButton(f, probe, hitHome, iconHome, m.NavHosts).MarginSides(0, barGap+2, 0, 0)
		if state.tabOrientation == tabsVertical {
			if probe {
				f.Spacer()
			} else {
				// The name is drawn after the bar is laid out, centred over the
				// workspace it names: see paintWindowBarTitle. The slot keeps the
				// height of a tab so the drawn line sits on the row's middle.
				bar.title = f.Slot().Grow(1).Height(barTabHeight)
				bar.titleLabel = state.windowBarTitle()
			}
		}
		// The room of the tabs and the new-tab button. The tabs stay as wide as they
		// like (barTabMax) and shrink together to barTabMin; the new-tab button
		// follows the last of them, or ends the room when they do not fit.
		tabRoom := f.Stack().Grow(1).MarginSides(0, barGap, 0, 0)
		if state.tabOrientation == tabsVertical && visibleFileTabs == 0 {
			tabRoom.Grow(0).Width(barButtonSize)
		}
		tabRoom.Children(func() {
			area := f.Slot().Fill().MarginSides(0, barButtonSize, 0, 0)
			f.Row().Fill().Children(func() {
				tabs := f.Row()
				if probe {
					chrome.probeRow = tabs
				} else {
					tabs.Width(geometry.row.Width).Shrink(0).Clip()
				}
				tabs.Children(func() {
					for index := range state.workspaceTabs {
						if state.tabOrientation == tabsVertical {
							break
						}
						workspace := state.workspaceTabs[index]
						var tab flex.Box
						if probe {
							minimum := barTabMin
							if geometry.locked {
								// Leave the title its normal minimum room even with a
								// lock beside it. The strip uses a uniform tab pitch.
								minimum += 18
							}
							tab = f.Slot().Width(barTabMax).MinWidth(minimum).Height(barTabHeight).Shrink(1).MarginSides(0, barGap, 0, 0)
							chrome.probeTabs = append(chrome.probeTabs, tab)
							bar.tabs = append(bar.tabs, barTab{})
							continue
						}
						first := float32(0)
						if index == 0 {
							first = -scroll
						}
						entry := state.declareWorkspaceTab(f, workspace, geometry.tabWidth, barTabHeight, first, false, false)
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
							tab := f.Slot().Width(barTabMax).MinWidth(minimum).Height(barTabHeight).Shrink(1).MarginSides(0, barGap, 0, 0)
							chrome.probeTabs = append(chrome.probeTabs, tab)
							bar.tabs = append(bar.tabs, barTab{})
							continue
						}
						entry := barTab{fileSession: fileTab.session.ID, id: id, closeID: closeID}
						active := state.activeSection == sectionSFTP && state.activeSFTPTab == fileTab.session.ID
						hovered := f.Canvas().Hovered() == id || f.Canvas().Hovered() == closeID
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
						f.Row().Size(geometry.tabWidth, barTabHeight).Shrink(0).MarginSides(0, barGap, 0, first).
							PadSides(0, 3, 0, 4).Items(flex.Center).Radius(radius.item).Border(1, edge).Background(fill).
							HitWith(id, label, ui.HitOptions{Role: ui.RoleTab, Selected: active}).Children(func() {
							entry.badge = f.Slot().Size(20, 20).MarginSides(0, 7, 0, 0)
							f.Text(label).FontSize(metrics.FontSizeS).Weight(ui.WeightMedium).Color(foreground).LineHeight(20).Ellipsis()
							entry.light = f.Slot().Size(9, 20).MarginSides(0, 0, 0, 7)
							f.Spacer()
							if !active && !hovered {
								f.Slot().Size(20, 20)
								return
							}
							var closeFill ui.Color
							if f.Canvas().Hovered() == closeID {
								closeFill = pal.line
								if active {
									closeFill = pal.tabCloseHot
								}
							}
							f.Row().Size(20, 20).Shrink(0).Center().Radius(radius.item).Background(closeFill).
								HitWith(closeID, m.LabelCloseTab, ui.HitOptions{Role: ui.RoleButton, SkipFocus: true}).Children(func() {
								f.Icon(lucideIcons[iconClose]).Size(14, 14).Color(foreground)
							})
						})
						bar.tabs = append(bar.tabs, entry)
					}
				})
				state.barButton(f, probe, hitNewTab, iconPlus, m.LabelNewTab)
			})
			if probe {
				chrome.probeArea = area
			}
		})
		state.declareToolbarDockTop(f, probe)
	})
}

// declareWorkspaceTab shares tab styling and hit identities between both orientations.
func (state *State) declareWorkspaceTab(f *flex.Frame, workspace layoutcore.WorkspaceInfo, width, height, first float32, vertical, compact bool) barTab {
	m, pal := state.messages, state.pal
	id, closeID := hitTabList.At(int(workspace.ID)), hitTabCloseList.At(int(workspace.ID))
	entry := barTab{workspace: workspace, id: id, closeID: closeID}
	active := state.activeSection == sectionTerminal && state.deck.ActiveWorkspace() == workspace.ID
	hovered := f.Canvas().Hovered() == id || f.Canvas().Hovered() == closeID
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
	label := state.tabLabel(workspace)
	entry.label = label
	entry.output = state.workspaceOutputAlert(workspace.ID)
	tabOptions := ui.HitOptions{Role: ui.RoleTab, Selected: active}
	// Every tab shows its full name as a tooltip on hover, so a long, elided
	// label (a deep path or a long host label among many tabs) stays readable.
	// The shared hover tooltip paints Hit, so naming it here is all it takes.
	tabOptions.Hint = label
	if workspace.Locked {
		tabOptions.Hint = m.TabLockedHint
	}
	// A desktop keeps its size and protection in the status line under the
	// picture, which the host may have hidden: the tab says it on hover instead.
	if session := state.tabSession(workspace); session != nil && session.desk != nil {
		if hint := state.desktopStripLeft(session.desk, session); hint != "" {
			tabOptions.Hint = hint
		}
	}
	row := f.Row().Size(width, height).Shrink(0)
	if vertical {
		row.MarginSides(first, 0, barGap, 0)
	} else {
		row.MarginSides(0, barGap, 0, first)
	}
	row.
		PadSides(0, 3, 0, 4).Items(flex.Center).Radius(radius.item).Border(1, edge).Background(fill).
		HitWith(id, label, tabOptions).Children(func() {
		if compact {
			f.Spacer()
			entry.badge = f.Slot().Size(20, 20).Shrink(0)
			f.Spacer()
			return
		}
		entry.badge = f.Slot().Size(20, 20).MarginSides(0, 7, 0, 0)
		titleColor := foreground
		if entry.output != outputAlertNone {
			titleColor.Alpha = 0
		}
		title := f.Text(label).FontSize(metrics.FontSizeS).Weight(ui.WeightMedium).Color(titleColor).LineHeight(20).Ellipsis()
		entry.title = title
		if vertical {
			title.Grow(1).Basis(0)
		}
		if workspace.Locked {
			f.Icon(lucideIcons[iconLock]).Size(13, 13).Shrink(0).MarginSides(0, 0, 0, 5).Color(foreground)
		}
		entry.light = f.Slot().Size(9, 20).MarginSides(0, 0, 0, 7)
		if !vertical {
			f.Spacer()
		}
		if !active && !hovered {
			f.Slot().Size(20, 20)
			return
		}
		var closeFill ui.Color
		if f.Canvas().Hovered() == closeID {
			closeFill = pal.line
			if active {
				closeFill = pal.tabCloseHot
			}
		}
		f.Row().Size(20, 20).Shrink(0).Center().Radius(radius.item).Background(closeFill).
			HitWith(closeID, m.LabelCloseTab, ui.HitOptions{Role: ui.RoleButton, SkipFocus: true}).Children(func() {
			f.Icon(lucideIcons[iconClose]).Size(14, 14).Color(foreground)
		})
	})
	entry.compact = compact
	return entry
}

// barButton declares a square icon button of the window bar. These buttons do not
// take keyboard focus when pressed, so using one never pulls the keyboard out of a
// terminal. With probe set it is only the place.
func (state *State) barButton(f *flex.Frame, probe bool, id ui.ID, icon uint8, label string) flex.Box {
	if probe {
		return f.Slot().Size(barButtonSize, barButtonSize)
	}
	pal, canvas := state.pal, f.Canvas()
	foreground := pal.muted
	var fill ui.Color
	switch {
	case canvas.Pressed() == id:
		fill, foreground = pal.tabActive, pal.text
	case canvas.Hovered() == id:
		fill, foreground = pal.tabHover, pal.text
	}
	return f.Row().Size(barButtonSize, barButtonSize).Shrink(0).Center().Radius(radius.item).Background(fill).
		HitWith(id, label, ui.HitOptions{Role: ui.RoleButton, SkipFocus: true, Hint: label}).Children(func() {
		f.Icon(lucideIcons[icon]).Color(foreground)
	})
}

// paintWindowBarParts draws what the bar's tabs and assistant button hold that is
// drawn rather than declared: the system of a session and its light, in the room
// the layout kept for them, and the session's name.
func (state *State) paintWindowBarParts(canvas *ui.Canvas, strip ui.Rect) {
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
func (state *State) paintWindowBarTitle(canvas *ui.Canvas) {
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
	label := elide(canvas, bar.titleLabel, metrics.FontSizeS, ui.WeightRegular, room)
	width := canvas.TextWidth(label, metrics.FontSizeS)
	// The workspace is what the name is about: centre it over that column rather
	// than over the whole bar, which carries the sidebar on its left. File tabs
	// share the band, so the name still stops at the band's ends.
	column := state.verticalTabsColumn(state.viewport)
	center := column + (state.viewport.Width-column)/2
	x := min(max(center-width/2, band.X+pad), band.X+band.Width-pad-width)
	canvas.Label(label, ui.Rect{X: x, Y: band.Y, Width: width, Height: band.Height},
		ui.TextStyle{Size: metrics.FontSizeS, Color: state.pal.muted})
}

// elide shortens text to fit width, ending it with an ellipsis. Weight is the
// one the text is drawn at, since a heavier line is wider.
func elide(canvas *ui.Canvas, value string, size, weight, width float32) string {
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

// chromeLayout keeps the flex.View of each area of the window chrome that is laid
// out by github.com/vivaui/viva/flex, and the few rectangles another painter needs from them.
type chromeLayout struct {
	window, probe, header, hostRow, empty, popup flex.View
	// widget lays out one small control at a time (a bar or toolbar button, a chip,
	// an icon button): each paints what it declared before the next begins, so the
	// view is shared. dialog is the body of one dialog, and newTab the page of an
	// empty tab.
	widget, dialog, newTab flex.View
	// importButton is where the toolbar's Import button was painted: the menu
	// it opens hangs under it, right-aligned.
	importButton ui.Rect
	// chips are the slots of the group chips of the hosts page, in the order
	// they are painted: All, the groups that fit, then More. The slice is kept
	// so a repaint does not allocate.
	chips []flex.Box
	// bar and probeBar are the window bar as laid out for painting and for finding
	// the tabs' width; geometry is what the second found, and probeArea, probeRow and
	// probeTabs the places it was read from.
	bar, probeBar       windowBar
	geometry            barGeometry
	probeArea, probeRow flex.Box
	probeTabs           []flex.Box
	// lockup is where the sidebar header's logo is painted.
	lockup flex.Box
	// newTabBadges are the places of the systems' badges on the new-tab page.
	newTabBadges []flex.Box
	// newTabShells is the arrow beside the Local Terminal row that opens the
	// shell menu, kept to place that menu under it.
	newTabShells flex.Box
	// hostList is the hosts list: where the wheel is aimed.
	hostList ui.Rect
}

// declareNavigation declares the sidebar: the name of the application over the
// pages, and the way to a shell on this computer and to the settings at the bottom.
func (state *State) declareNavigation(f *flex.Frame) {
	m := state.messages

	items := [...]struct {
		id     ui.ID
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
	}
	f.Column().Width(navigationWidth).Shrink(0).Children(func() {
		// The logo with the name beside it, painted into this slot after layout.
		state.chrome.lockup = f.Slot().Size(lockupWidth, lockupHeight).MarginSides(15, 0, 20, 0).Self(flex.Center)
		for _, item := range items {
			state.navigationItem(f, item.id, item.icon, item.label, item.active, 4)
		}
		if state.tabOrientation == tabsVertical && len(state.workspaceTabs) > 0 {
			state.verticalTabs.slot = f.Slot().Grow(1).Basis(0).MarginSides(8, 9, 8, 9)
		}
		// A shell on this computer needs no host, so its way in is always at hand:
		// above the Settings, at the bottom of the sidebar. A dev build also puts
		// the component gallery there (see declareDevNavigation).
		if state.tabOrientation != tabsVertical || len(state.workspaceTabs) == 0 {
			f.Spacer()
		}
		state.navigationItem(f, hitLocalTerminal, iconTerminal, m.LocalTerminal, false, 4)
		state.openLocalShellPickerOn(f.Canvas(), hitLocalTerminal, false)
		state.devDeclareNavigation(f)
		state.navigationItem(f, hitSettings, iconSettings, m.NavSettings, state.activeSection == sectionSettings, 16)
	})
}

// navigationItem declares one row of the sidebar: an icon and a name, filled while
// it is the page on show or the pointer is on it.
func (state *State) navigationItem(f *flex.Frame, id ui.ID, icon uint8, label string, active bool, below float32) {
	pal := state.pal
	var fill ui.Color
	if active {
		fill = pal.surfaceHot
	} else if f.Canvas().Hovered() == id {
		fill = pal.surfaceSoft
	}
	f.Row().Height(36).MarginSides(0, 9, below, 9).PadSides(0, 9, 0, 15).Gap(12).Items(flex.Center).Radius(radius.control).Background(fill).
		HitWith(id, label, ui.HitOptions{Role: ui.RoleMenuItem, Selected: active}).Children(func() {
		f.Icon(lucideIcons[icon]).IconTransform(state.iconMotion(f.Canvas(), id, icon)).Color(pal.text)
		f.Text(label).FontSize(metrics.FontSizeM).Weight(ui.WeightMedium).Color(pal.text)
	})
}

func (state *State) paintHostsWorkspace(canvas *ui.Canvas, bounds ui.Rect) {
	list := state.paintHostsHeader(canvas, bounds)
	state.chrome.hostList = list

	state.hostsContent = 0
	switch {
	case len(state.hostTreeRows) > 0:
		state.paintHostTable(canvas, list)
	case len(state.hosts) == 0:
		state.paintEmptyHosts(canvas, bounds)
	case len(state.visible) == 0:
		state.paintNoMatches(canvas, bounds)
	default:
		state.paintHostTable(canvas, list)
	}
	state.hostsBounds = bounds
}

// paintHostMenus draws the hosts page's dropdowns above everything else, with
// the whole window as their viewport, so a press anywhere closes them.
func (state *State) paintHostMenus(canvas *ui.Canvas, viewport ui.Rect) {
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
	if state.importMenu {
		state.paintImportMenu(canvas, viewport)
	}
}

// paintHostsHeader is the toolbar, persistent group filters and, while hosts
// are checked, a separate row of batch actions. Hits are added in the order the
// controls were painted before the page was declared: Tab follows it. It returns
// where the list goes under them.
func (state *State) paintHostsHeader(canvas *ui.Canvas, bounds ui.Rect) ui.Rect {
	m, pal := state.messages, state.pal
	chrome := &state.chrome
	// The buttons keep their natural width, so the search box gives way to them
	// rather than the other way round: a label that is wider than expected (a
	// heavier or wider font) must not slide a button under its neighbour.
	const (
		toolbarMargin = float32(18)  // the toolbar row's own margin inside the section card
		buttonGap     = float32(10)  // the air between the search box and the buttons
		searchLeast   = float32(140) // the narrowest the search box stays a search box at
	)
	// A section squeezed by the assistant panel has no room for the labels: the
	// buttons keep only their icons, whose tooltips name them, and the search box
	// keeps the least width it works at.
	labeled := searchLeast + buttonGap + toolbarButtonWidth(canvas, m.Connect, false) +
		buttonGap + toolbarButtonWidth(canvas, m.ImportMenu, true) +
		buttonGap + toolbarButtonWidth(canvas, m.NewHost, true)
	condensed := labeled > bounds.Width-2*toolbarMargin
	connectWidth, connectIcon := toolbarButtonLayout(canvas, m.Connect, 0, iconPlug, condensed)
	importWidth, importIcon := toolbarButtonLayout(canvas, m.ImportMenu, iconDownload, iconDownload, condensed)
	newHostWidth, newHostIcon := toolbarButtonLayout(canvas, m.NewHost, iconPlus, iconPlus, condensed)
	searchWidth := min(max(bounds.Width*0.41, 330), 485)

	const (
		rowHeight          = float32(28)
		toggleWidth        = float32(56)
		rowGap             = float32(12)
		rowPad             = float32(18)
		actionGap          = float32(4)
		groupControlsWidth = float32(56)
	)
	anySelected := len(state.hostSelections) > 0
	// While something is checked the first label lines up with the list's left
	// edge, so the button's padding hangs outside it (a negative margin).
	const quietPadding = float32(12)
	actions := [...]struct {
		id      ui.ID
		label   string
		icon    uint8
		enabled bool
		danger  bool
	}{
		{hitExportHosts, m.ExportHosts, iconFileDown, len(state.hosts) > 0, false},
		{hitEditHost, m.EditHost, iconPencil, false, false},
		{hitCopyHost, m.CopyHost, iconCopy, false, false},
		{hitMoveHostGroup, m.MoveHostGroup, iconFolderOpen, false, false},
		{hitDashboardAddHosts, state.messages.Dashboard.AddHosts, iconActivity, false, false},
		{hitDeleteHost, m.DeleteHost, iconTrash, false, true},
	}
	var actionWidths [len(actions)]float32
	// The actions keep their words while the row has room for them and the count
	// of what is checked; a narrow row keeps only their icons.
	actionsCondensed := false
	if anySelected {
		enabled := state.selectedHostID != 0
		single := enabled && len(state.hostSelections) <= 1
		for i := range actions {
			switch actions[i].id {
			case hitEditHost, hitCopyHost:
				actions[i].enabled = single
			case hitMoveHostGroup, hitDeleteHost:
				actions[i].enabled = enabled
			case hitDashboardAddHosts:
				actions[i].enabled = state.dashboardCanAddHosts()
			}
		}
		used := canvas.TextWidthWeight(state.hostSelectionText, metrics.FontSizeS, ui.WeightMedium) + 2*rowGap
		for _, action := range actions {
			used += canvas.TextWidthWeight(action.label, metrics.FontSizeS, ui.WeightSemibold) + 4 + 2*quietPadding + actionGap
		}
		actionsCondensed = used > bounds.Width-2*rowPad
		for index, action := range actions {
			actionWidths[index] = canvas.TextWidthWeight(action.label, metrics.FontSizeS, ui.WeightSemibold) + 4 + 2*quietPadding
		}
		if actionsCondensed {
			for index := range actionWidths {
				actionWidths[index] = actionIconSize
			}
		}
	}
	// Which chips fit decides what is declared; where they sit is the row's.
	showChips := state.hasHostFilter()
	showGroupControls := showChips && !state.hostFilterOn
	headerHeight := float32(hostListTop)
	if anySelected {
		headerHeight += rowHeight + rowGap
	}
	if showChips {
		room := bounds.Width - 2*rowPad - toggleWidth - rowGap
		if showGroupControls {
			room -= groupControlsWidth + rowGap
		}
		state.fitHostChips(canvas, room)
	}

	var search, connect, importButton, newHost, toggle, list flex.Box
	var buttons [len(actions)]flex.Box
	var groupControls [2]flex.Box
	f := chrome.header.Begin(canvas, state.Theme(), bounds)
	f.Column().Fill().Children(func() {
		f.Column().Height(headerHeight).Shrink(0).PadSides(14, 0, 0, 0).Children(func() {
			f.Row().Height(toolbarHeight).MarginX(toolbarMargin).Items(flex.Center).Children(func() {
				search = f.Slot().Basis(searchWidth).Shrink(1).MinWidth(searchLeast).Height(toolbarHeight).MarginSides(0, buttonGap, 0, 0)
				connect = f.Slot().Size(connectWidth, toolbarHeight).MarginSides(0, buttonGap, 0, 0)
				f.Spacer()
				importButton = f.Slot().Size(importWidth, toolbarHeight).MarginSides(0, buttonGap, 0, 0)
				newHost = f.Slot().Size(newHostWidth, toolbarHeight)
			})
			// Filters and view controls stay in place when the selection changes.
			f.Row().Height(rowHeight).MarginSides(12, 0, 0, 0).PadSides(0, 18, 0, 18).Gap(12).Items(flex.Center).Children(func() {
				if showChips {
					f.Row().Gap(6).Children(func() {
						chrome.chips = chrome.chips[:0]
						chrome.chips = append(chrome.chips, f.Slot().Size(state.filterChipWidth(canvas, m.HostFilterAll, state.hostTotalDigits), rowHeight))
						for _, group := range state.hostGroups[:state.hostFilterHidden] {
							chrome.chips = append(chrome.chips, f.Slot().Size(state.filterChipWidth(canvas, state.hostGroupLabel(group), group.Digits), rowHeight))
						}
						if state.hostFilterHidden < len(state.hostGroups) {
							chrome.chips = append(chrome.chips, f.Slot().Size(state.filterChipWidth(canvas, m.HostFilterMore, ""), rowHeight))
						}
					})
				}
				f.Spacer()
				if showGroupControls {
					f.Row().Gap(2).Children(func() {
						for index := range groupControls {
							groupControls[index] = f.Slot().Size(26, 26)
						}
					})
				}
				toggle = f.Slot().Size(toggleWidth-2, rowHeight).MarginSides(0, 0, 0, 2)
			})
			if anySelected {
				f.Row().Height(rowHeight).MarginSides(rowGap, 0, 0, 0).PadSides(0, rowPad, 0, rowPad).Gap(rowGap).Items(flex.Center).Children(func() {
					actionsLeft := -quietPadding
					if actionsCondensed {
						actionsLeft = 0
					}
					f.Row().Gap(actionGap).MarginSides(0, 0, 0, actionsLeft).Children(func() {
						for index := range buttons {
							buttons[index] = f.Slot().Size(actionWidths[index], rowHeight)
						}
					})
					f.Spacer()
					f.Text(state.hostSelectionText).FontSize(metrics.FontSizeS).Weight(ui.WeightMedium).Color(pal.muted).Ellipsis().Shrink(1)
				})
			}
		})
		list = f.Slot().Grow(1).MarginSides(0, 18, 18, 18)
	})
	f.End()

	// The box filters the list as you type and is what Connect opens, so it is
	// a real field: a magnifier addon, the placeholder when empty, and the
	// platform editor once it holds text.
	value := state.search.String()
	components.PaintInputGroup(canvas, state.Theme(), search.Rect(), components.InputGroupSnapshot{
		ID: hitSearch, Label: m.NavSearch, Value: value, Placeholder: m.SearchPlaceholder,
		NativeEditing: canvas.Focused() == hitSearch && value != "",
		InlineStart:   components.InputGroupSlot{Kind: components.InputGroupSlotIcon, Icon: lucideIcons[iconSearch]},
	})
	// Connect needs something to connect to.
	state.paintToolbarButtonIn(canvas, hitConnect, connect.Rect(), m.Connect, connectIcon, false, state.typedQuery() != "", condensed)
	state.paintToolbarButtonIn(canvas, hitAddHost, newHost.Rect(), m.NewHost, newHostIcon, true, true, condensed)
	chrome.importButton = importButton.Rect()
	state.paintToolbarButtonIn(canvas, hitImportMenu, chrome.importButton, m.ImportMenu, importIcon, false, true, condensed)

	state.paintHostViewToggle(canvas, toggle.Rect().X, toggle.Rect().Y+toggle.Rect().Height/2)
	if showGroupControls {
		expand, collapse := state.hostGroupsExpansionActions()
		state.paintIconButton(canvas, hitHostGroupsExpandAll, iconChevronDown, groupControls[0].Rect(), m.HostGroupActions.ExpandAll, !expand, iconButtonQuiet, pal.workspace)
		state.paintIconButton(canvas, hitHostGroupsCollapseAll, iconChevronRight, groupControls[1].Rect(), m.HostGroupActions.CollapseAll, !collapse, iconButtonQuiet, pal.workspace)
	}
	if showChips {
		state.paintHostFilterChips(canvas, chrome.chips)
	}
	if anySelected {
		for index, action := range actions {
			theme := state.Theme().WithControlHeight(rowHeight).WithFontSize(metrics.FontSizeS)
			if action.danger {
				theme.Foreground = theme.Danger
			}
			button := components.BeginButton(canvas, theme, action.id, buttons[index].Rect())
			if actionsCondensed {
				button = button.WithIcon(lucideIcons[action.icon]).Icon()
			}
			if !action.enabled {
				button = button.Disabled()
			}
			button.Ghost(action.label)
			if action.id == hitDashboardAddHosts {
				state.dashboard.addAnchor = buttons[index].Rect()
			}
			if action.id == hitMoveHostGroup {
				state.groupMenu.button = buttons[index].Rect()
			}
		}
	}
	return list.Rect()
}

// paintImportMenu lists the three ways to bring hosts in under the Import
// button, right-aligned with it.
func (state *State) paintImportMenu(canvas *ui.Canvas, viewport ui.Rect) {
	m := state.messages
	menu := components.BeginDropdownMenu(canvas, state.Theme(), state.popupBelow(canvas, state.chrome.importButton, 200, 4*32+8+9, flex.End),
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
// below it, from its left edge (flex.Start) or its right edge (flex.End).
func (state *State) popupBelow(canvas *ui.Canvas, anchor ui.Rect, width, height float32, edge flex.Align) ui.Rect {
	var menu flex.Box
	f := state.chrome.popup.Begin(canvas, state.Theme(), anchor)
	f.Column().Fill().Items(edge).Children(func() {
		f.Slot().Height(anchor.Height).Width(0)
		menu = f.Slot().Size(width, height).MarginSides(4, 0, 0, 0)
	})
	f.End()
	return menu.Rect()
}

// popupAt is where a menu width wide and height tall opens at a point of the
// window: at the point, or pushed back inside the window's edge, with edge to spare.
func (state *State) popupAt(canvas *ui.Canvas, viewport ui.Rect, at ui.Point, width, height, edge float32) ui.Rect {
	var menu flex.Box
	f := state.chrome.popup.Begin(canvas, state.Theme(), viewport)
	f.Column().Fill().PadSides(0, 0, edge, 0).Children(func() {
		f.Slot().Height(at.Y).Shrink(1)
		f.Row().Height(height).Shrink(0).PadSides(0, edge, 0, 0).Children(func() {
			f.Slot().Width(at.X).Shrink(1)
			menu = f.Slot().Size(width, height).Shrink(0)
		})
	})
	f.End()
	return menu.Rect()
}

// fitHostChips decides how many group chips fit in room, leaving the rest to the
// More menu: hostFilterHidden is the index of the first group left out.
func (state *State) fitHostChips(canvas *ui.Canvas, room float32) {
	const gap = float32(6)
	moreWidth := state.filterChipWidth(canvas, state.messages.HostFilterMore, "")
	state.hostFilterHidden = len(state.hostGroups)

	// Do they all fit? If not, room is kept for More.
	used := state.filterChipWidth(canvas, state.messages.HostFilterAll, state.hostTotalDigits)
	for _, group := range state.hostGroups {
		used += gap + state.filterChipWidth(canvas, state.hostGroupLabel(group), group.Digits)
	}
	limit := room
	if used > room {
		limit = room - moreWidth - gap
	}
	x := state.filterChipWidth(canvas, state.messages.HostFilterAll, state.hostTotalDigits) + gap
	for index, group := range state.hostGroups {
		width := state.filterChipWidth(canvas, state.hostGroupLabel(group), group.Digits)
		if x+width > limit {
			state.hostFilterHidden = index
			return
		}
		x += width + gap
	}
}

// paintHostFilterChips draws the row of group chips laid out by the header: All,
// then each group with how many hosts it holds, then More when some did not fit.
func (state *State) paintHostFilterChips(canvas *ui.Canvas, chips []flex.Box) {
	m := state.messages
	state.paintFilterChip(canvas, hitHostFilterAll, chips[0].Rect(), m.HostFilterAll, state.hostTotalDigits, !state.hostFilterOn)
	for index, group := range state.hostGroups[:state.hostFilterHidden] {
		state.paintFilterChip(canvas, hitHostFilterList.At(index), chips[1+index].Rect(),
			state.hostGroupLabel(group), group.Digits, state.hostFilterOn && state.hostFilter == group.Name)
		state.openHostGroupActionsOn(canvas, hitHostFilterList.At(index), index)
	}
	if state.hostFilterHidden < len(state.hostGroups) {
		hiddenChosen := false
		if state.hostFilterOn {
			hiddenChosen = state.hostGroupIndex(state.hostFilter) >= state.hostFilterHidden
		}
		state.filterMoreButton = chips[1+state.hostFilterHidden].Rect()
		state.paintFilterChip(canvas, hitHostFilterMore, state.filterMoreButton, m.HostFilterMore, "", hiddenChosen)
	}
}

// filterChipWidth is a chip's width: its label, the count after it and room
// around both.
func (state *State) filterChipWidth(canvas *ui.Canvas, label, digits string) float32 {
	width := 2*float32(12) + canvas.TextWidthWeight(label, metrics.FontSizeS, ui.WeightSemibold)
	if digits != "" {
		width += 6 + canvas.TextWidthWeight(digits, metrics.FontSizeS, ui.WeightMedium)
	}
	return width + 2
}

// paintFilterChip draws one chip: a pill that is filled while it is the choice.
func (state *State) paintFilterChip(canvas *ui.Canvas, id ui.ID, bounds ui.Rect, label, digits string, chosen bool) {
	pal := state.pal
	foreground := pal.muted
	var fill ui.Color
	switch {
	case chosen:
		fill, foreground = pal.surfaceHot, pal.text
	case canvas.Pressed() == id:
		fill = pal.surfaceHot
	case canvas.Hovered() == id:
		fill, foreground = pal.surfaceSoft, pal.text
	}
	f := state.chrome.widget.Begin(canvas, state.Theme(), bounds)
	f.Row().Fill().PadSides(0, 14, 0, 12).Gap(6).Items(flex.Center).Radius(bounds.Height/2).Background(fill).
		HitWith(id, label, ui.HitOptions{Role: ui.RoleButton, Selected: chosen}).Children(func() {
		f.Text(label).FontSize(metrics.FontSizeS).Weight(ui.WeightSemibold).Color(foreground)
		if digits != "" {
			f.Text(digits).FontSize(metrics.FontSizeS).Weight(ui.WeightMedium).Color(pal.faint)
		}
	})
	f.End()
	if canvas.FocusVisible() == id {
		canvas.RoundedBorder(bounds, bounds.Height/2, 1.5, pal.accent, ui.Color{})
	}
}

// paintHostFilterMenu lists the groups whose chips did not fit.
func (state *State) paintHostFilterMenu(canvas *ui.Canvas, viewport ui.Rect) {
	hidden := len(state.hostGroups) - state.hostFilterHidden
	if hidden <= 0 {
		return
	}
	menu := components.BeginDropdownMenu(canvas, state.Theme(), state.popupBelow(canvas, state.filterMoreButton, 202, float32(32*hidden+8), flex.Start),
		components.DropdownMenuOptions{Layer: dialogLayer, Scope: dialogScope, DismissID: hitHostFilterMenuDismiss, Viewport: viewport, Scroll: state.menuScrollFor(hitHostFilterMenuDismiss)})
	state.menuPlaced(menu.Bounds())
	for index := state.hostFilterHidden; index < len(state.hostGroups); index++ {
		menu.AddItem(components.DropdownMenuItem{ID: hitHostFilterMenuList.At(index), Label: state.hostGroupLabel(state.hostGroups[index])})
		state.openHostGroupActionsOn(canvas, hitHostFilterMenuList.At(index), index)
	}
	menu.End()
}

func (state *State) paintHostGroupMenu(canvas *ui.Canvas, bounds, viewport ui.Rect) {
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
	var at ui.Rect
	if anchor.menu {
		at = state.popupAt(canvas, viewport, anchor.point, 202, height, 6)
	} else {
		at = state.popupBelow(canvas, anchor.button, 202, height, flex.Start)
	}
	menu := components.BeginDropdownMenu(canvas, state.Theme(), at,
		components.DropdownMenuOptions{Layer: dialogLayer, Scope: dialogScope, DismissID: hitHostGroupDismiss, Viewport: viewport, Scroll: state.menuScrollFor(hitHostGroupDismiss)})
	state.menuPlaced(menu.Bounds())
	menu.AddItem(components.DropdownMenuItem{ID: hitHostGroupNone, Label: state.messages.MoveGroupNone})
	for index, group := range state.hostGroups {
		if group.Name == "" {
			continue
		}
		menu.AddItem(components.DropdownMenuItem{ID: hitHostGroupItemList.At(index), Label: group.Name})
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
func toolbarButtonWidth(canvas *ui.Canvas, label string, hasIcon bool) float32 {
	width := toolbarPadding*2 + canvas.TextWidthWeight(label, toolbarFont, ui.WeightSemibold) + 4
	if hasIcon {
		width += toolbarIcon + toolbarIconGap
	}
	return width
}

// toolbarButtonLayout is the width a toolbar button takes in a row and the icon
// it shows there: its label and the icon it already had, or the icon alone when
// the row is condensed. compact is the icon a button that has none shows then,
// since a button without words has to say what it does some other way.
func toolbarButtonLayout(canvas *ui.Canvas, label string, icon, compact uint8, condensed bool) (width float32, shown uint8) {
	if condensed {
		return toolbarIconButton, compact
	}
	return toolbarButtonWidth(canvas, label, icon != 0), icon
}

// paintToolbarButton draws a toolbar button whose top left corner is (x, y) and
// that is width wide, for a page that has not yet declared its button's place.
func (state *State) paintToolbarButton(canvas *ui.Canvas, id ui.ID, x, y, width float32, label string, icon uint8, primary, enabled, condensed bool) {
	state.paintToolbarButtonIn(canvas, id, ui.Rect{X: x, Y: y, Width: width, Height: toolbarHeight}, label, icon, primary, enabled, condensed)
}

// paintToolbarButtonIn draws a toolbar button in bounds. Icon and label are
// centered together as one group, so the button reads as balanced at any label
// width. Condensed leaves the label out for a row too narrow to hold the words:
// it becomes the tooltip and the name assistive technology reads, so the button
// gives up its width and not its meaning.
func (state *State) paintToolbarButtonIn(canvas *ui.Canvas, id ui.ID, bounds ui.Rect, label string, icon uint8, primary, enabled, condensed bool) {
	state.paintButtonSized(canvas, id, bounds, label, icon, primary, false, enabled, toolbarFont, condensed)
}

// paintButton is the workbench's one button: a filled surface with a hairline
// edge, or the accent when primary. danger keeps that look and turns the label
// red, so a destructive action is marked without becoming the loudest thing on the
// page.
func (state *State) paintButton(canvas *ui.Canvas, id ui.ID, bounds ui.Rect, label string, icon uint8, primary, danger, enabled bool) {
	state.paintButtonSized(canvas, id, bounds, label, icon, primary, danger, enabled, 12, false)
}

// paintButtonSized is paintButton at another font size, and with the label left
// out when iconOnly: the label is then the tooltip and the accessible name.
func (state *State) paintButtonSized(canvas *ui.Canvas, id ui.ID, bounds ui.Rect, label string, icon uint8, primary, danger, enabled bool, font float32, iconOnly bool) {
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
	f := state.chrome.widget.Begin(canvas, state.Theme(), bounds)
	f.Row().Fill().Center().Radius(radius.control).Border(1, border.Over(pal.workspace)).Background(background.Over(pal.workspace)).
		HitWith(id, label, ui.HitOptions{Role: ui.RoleButton, Disabled: !enabled, Hint: hint}).Children(func() {
		if iconOnly {
			f.Icon(lucideIcons[icon]).Color(foreground)
			return
		}
		if icon == 0 {
			f.Text(label).FontSize(font).Weight(ui.WeightSemibold).Color(foreground)
			return
		}
		f.Icon(lucideIcons[icon]).Color(foreground).MarginSides(0, toolbarIconGap, 0, 0)
		f.Text(label).FontSize(font).Weight(ui.WeightSemibold).Color(foreground).MarginSides(0, 4, 0, 0)
	})
	f.End()
}

// hostStatus is what the table says about a host: the state of its session,
// or idle when no connection has been made to it.
func (state *State) hostStatus(host Host) (string, ui.Color) {
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
func (state *State) sessionStatus(session *Session) (string, ui.Color) {
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

func (state *State) paintHostTable(canvas *ui.Canvas, treeBounds ui.Rect) {
	pal := state.pal
	if cap(state.groupActions.rows) < len(state.hostGroups) {
		state.groupActions.rows = make([]ui.Rect, len(state.hostGroups))
	} else {
		state.groupActions.rows = state.groupActions.rows[:len(state.hostGroups)]
		clear(state.groupActions.rows)
	}
	// The list scrolls under the toolbar: the tree starts above its window by the
	// scrolled distance, and the clip keeps what is out of the window out of sight
	// and out of reach. The bar is Viva's scroll area: it keeps its own strip out
	// of the rows and paints nothing at all while the content fits.
	content := state.hostListHeight(treeBounds.Width)
	area, geometry := hostListScrollArea(canvas, state.Theme(), treeBounds, content, state.hostsScroll)
	if geometry.MaxOffsetY > 0 {
		// Taking the bar's strip narrows the list; the cards pack to the width they
		// are really given, so read the height again at the reserved width.
		content = state.hostListHeight(geometry.Viewport.Width)
		area, geometry = hostListScrollArea(canvas, state.Theme(), treeBounds, content, state.hostsScroll)
	}
	if state.hostRevealID != 0 {
		// The revealed row is found at the width the list really has, bar included.
		state.hostsScroll = state.sessionLogHostOffset(state.hostRevealID, geometry.Viewport.Width)
		canvas.RequestFocus(hitHostList.At(int(state.hostRevealID)))
		state.hostRevealID = 0
		area, geometry = hostListScrollArea(canvas, state.Theme(), treeBounds, content, state.hostsScroll)
	}
	state.hostsContent, state.hostsScroll = content, geometry.OffsetY
	viewport := geometry.Viewport
	canvas.Clip(viewport)
	if state.hostView == hostViewGrid {
		state.paintHostGrid(canvas, viewport)
		state.paintHostGroupDrop(canvas)
		canvas.Unclip()
		area.EndContent()
		return
	}
	tree := components.BeginTree(canvas, state.Theme(), ui.Rect{
		X: viewport.X, Y: viewport.Y - state.hostsScroll, Width: viewport.Width, Height: viewport.Height + state.hostsScroll,
	}).Indent(22)
	filtering := strings.TrimSpace(state.visibleQuery) != ""
	for _, entry := range state.hostTreeRows {
		switch entry.Kind {
		case hostTreeGroup:
			group := state.hostGroups[entry.GroupIndex]
			tree = tree.RowHeight(hostGroupRowHeight)
			row := tree.Item(components.TreeItem{
				ID: hitHostGroupRowList.At(entry.GroupIndex), DisclosureID: hitHostGroupOpenList.At(entry.GroupIndex),
				Label: state.hostGroupLabel(group), Depth: entry.Depth, Branch: true, Expanded: group.Expanded || filtering,
			})
			if row.Bounds.Y >= viewport.Y+viewport.Height {
				state.paintHostGroupDrop(canvas)
				canvas.Unclip()
				area.EndContent()
				return
			}
			content := row.Content
			content.Width = max(0, content.Width-38)
			f := state.chrome.hostRow.Begin(canvas, state.Theme(), content)
			f.Row().Fill().PadSides(0, 0, 0, 2).Gap(6).Items(flex.Center).Children(func() {
				if group.Pinned {
					f.Icon(lucideIcons[iconStar]).Size(14, 14).Color(pal.accent)
				}
				label := group.Label
				if group.Name == "" {
					label = state.messages.HostGroupDefault
				}
				f.Text(label).FontSize(metrics.FontSizeS).Weight(ui.WeightSemibold).Color(pal.text).LineHeight(23).Ellipsis().Shrink(1)
				f.Text(group.Count).FontSize(metrics.FontSizeS).Weight(ui.WeightMedium).Color(pal.faint).LineHeight(21)
			})
			f.End()
			state.hostGroupRowEvents(canvas, hitHostGroupRowList.At(entry.GroupIndex), entry.GroupIndex, row.Bounds)
		case hostTreeHost:
			host := state.hosts[entry.HostIndex]
			id := hitHostList.At(int(host.ID))
			tree = tree.RowHeight(hostRowHeight)
			row := tree.Item(components.TreeItem{ID: id, Label: host.Label, Depth: entry.Depth, Selected: host.ID == state.selectedHostID})
			if row.Bounds.Y >= viewport.Y+viewport.Height {
				state.paintHostGroupDrop(canvas)
				canvas.Unclip()
				area.EndContent()
				return
			}
			state.paintHostTreeRow(canvas, row.Bounds, row.Content, host, id)
			state.hostRowEvents(canvas, id, host)
		}
	}
	state.paintHostGroupDrop(canvas)
	canvas.Unclip()
	area.EndContent()
}

// hostListTop is the base list offset below the toolbar and group filters;
// checked hosts reserve an additional batch-action row.
const hostListTop = 102

const (
	hostGroupRowHeight = 42
	hostRowHeight      = 64
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
func hostListScrollArea(canvas *ui.Canvas, theme ui.Theme, bounds ui.Rect, content, offset float32) (components.ScrollArea, components.ScrollAreaGeometry) {
	// Width zero says the list has no horizontal extent of its own: it is laid out
	// to geometry.Viewport.Width, so no horizontal bar is ever due.
	area := components.BeginScrollArea(canvas, theme, bounds, ui.Size{Height: content}).
		Offset(0, offset).
		ReserveScrollbars().
		VerticalScrollbar(ui.ID{}, "", "")
	return area, area.Geometry()
}

// scrollHosts moves the hosts list with the wheel.
func (state *State) scrollHosts(event ui.ScrollEvent) ui.EventResult {
	step := event.StepY(24)
	before := state.hostsScroll
	visible := state.chrome.hostList.Height
	state.hostsScroll = min(max(before+step, 0), max(state.hostsContent-visible, 0))
	return ui.EventResult{Handled: true, Repaint: state.hostsScroll != before}
}

func (state *State) paintHostTreeRow(canvas *ui.Canvas, row, content ui.Rect, host Host, id ui.ID) {
	pal := state.pal
	// The selection fill is the tree's own accent (theme.Accent is surfaceHot);
	// keep that tone underneath the connection tint so a colored selected row
	// still reads as selected, not as merely hovered.
	rowBackground := pal.workspace
	switch {
	case host.ID == state.selectedHostID:
		rowBackground = pal.surfaceHot
	case canvas.Hovered() == id:
		rowBackground = pal.surfaceSoft
	}
	if hostAccent(host.Color).Alpha > 0 {
		rowBackground = tintHostSurface(rowBackground, host.Color, .10)
		canvas.Rounded(row, radius.control, rowBackground)
		paintHostColorMark(canvas, row, host.Color)
	}
	// The columns follow the row's width: the name, then the address and the tags
	// further right. The status is pinned to the row's right edge so it is never
	// pushed out of a narrow window; the address and the tags take what is left, in
	// proportion, and the tags give way first. Distances are from the content's left.
	const (
		statusLeast = float32(124) // the status in words, with the light before it
		lightWidth  = float32(7)   // the size of the connection light
		leadWidth   = float32(92)  // the check box, the system and the gaps before the name
		nameLeast   = float32(170)
	)
	indent := content.X - row.X
	// A row too narrow to hold the name beside the status in words shows the
	// light alone: the name is what the row is for.
	statusWidth := statusLeast
	if row.Width-statusLeast-indent-leadWidth < nameLeast {
		statusWidth = lightWidth
	}
	statusWords := statusWidth > lightWidth
	statusStart := row.Width - statusWidth - indent
	nameWidth := min(max(row.Width*0.24, nameLeast), max(statusStart-leadWidth-8, nameLeast/2))
	addressStart := min(max(row.Width*0.37, float32(330)), max(statusStart-180, leadWidth+nameWidth+8))
	tagsStart := max(row.Width*0.65, float32(560))
	addressWidth := max(min(row.Width*0.25, statusStart-addressStart-8), 0)
	tagsWidth := min(row.Width*0.18, statusStart-tagsStart-8)
	status, dot := state.hostStatus(host)
	kind, _ := state.hostLight(host)

	// The row is laid out from the content's left edge; the margin on the status
	// takes back the indent, so the status ends at the row's right edge, whatever
	// the columns before it add up to.
	area := content
	area.Width = row.Width
	checkMargin := 46 - 10 - components.CheckboxExtent(state.Theme())
	tagCount, tagsRoom := 0, tagsWidth
	var tagList [maxHostTags]string
	if tagsWidth >= 60 {
		tagCount = state.fitHostTags(canvas, host, tagsWidth, &tagList)
	}
	var avatar, light flex.Box
	f := state.chrome.hostRow.Begin(canvas, state.Theme(), area)
	f.Stack().Fill().Children(func() {
		f.Row().Fill().Items(flex.Center).Children(func() {
			f.Row().Width(addressStart).Shrink(0).Items(flex.Center).Children(func() {
				f.Checkbox(hitHostCheckList.At(int(host.ID)), state.hostSelected(host.ID)).Label(host.Label).MarginSides(0, checkMargin, 0, 10)
				avatar = f.Slot().Size(36, 36).MarginSides(0, 10, 0, 0)
				f.Column().Width(nameWidth).Shrink(0).Gap(2).Children(func() {
					f.Text(host.Label).FontSize(metrics.FontSizeM).Weight(ui.WeightSemibold).Color(pal.text).LineHeight(22).Ellipsis()
					f.Text(state.hostSubtitle(host)).FontSize(metrics.FontSizeS).Color(pal.muted).LineHeight(19).Ellipsis()
				})
			})
			f.Row().Width(tagsStart - addressStart).Shrink(0).Children(func() {
				if addressWidth < 60 { // a sliver of an address says less than nothing
					return
				}
				f.Text(host.target).FontSize(metrics.FontSizeS).Weight(ui.WeightMedium).Color(pal.text).LineHeight(22).Width(addressWidth).Shrink(0).Ellipsis()
			})
			if tagCount > 0 {
				f.Row().Width(tagsRoom).Shrink(0).Gap(5).Items(flex.Center).Children(func() {
					for _, tag := range tagList[:tagCount] {
						f.Row().Height(24).PadX(7).Items(flex.Center).Radius(12).Background(pal.surface).Children(func() {
							f.Text(tag).FontSize(metrics.FontSizeS).Weight(ui.WeightMedium).Color(pal.muted)
						})
					}
				})
			}
		})
		f.Row().Fill().Justify(flex.End).Items(flex.Center).Children(func() {
			if !statusWords {
				// The light alone, still inset from the row's right edge.
				light = f.Slot().Size(lightWidth, lightWidth).MarginSides(0, indent, 3, 0)
				return
			}
			light = f.Slot().Size(lightWidth, lightWidth).MarginSides(0, 8, 3, 0)
			f.Text(status).FontSize(metrics.FontSizeS).Weight(ui.WeightMedium).Color(pal.muted).LineHeight(22).Width(statusWidth-15).Shrink(0).MarginSides(0, indent, 0, 0)
		})
	})
	f.End()
	state.paintHostCatalogIcon(canvas, host, avatar.Rect(), rowBackground)
	lightRect := light.Rect()
	paintLight(canvas, lightRect.X+lightRect.Width/2, lightRect.Y+lightRect.Height/2, kind, dot, rowBackground)
}

// fitHostTags picks the tags of a host whose pills fit whole in width, in order,
// and returns how many there are.
func (state *State) fitHostTags(canvas *ui.Canvas, host Host, width float32, shown *[maxHostTags]string) int {
	count, used := 0, float32(0)
	for remaining := host.Tags; remaining != "" && count < len(shown); {
		tag, tail, found := strings.Cut(remaining, ", ")
		pill := min(canvas.TextWidthWeight(tag, metrics.FontSizeS, ui.WeightMedium)+14, width)
		if pill <= 0 || used+pill > width {
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

func (state *State) paintHostCatalogIcon(canvas *ui.Canvas, host Host, bounds ui.Rect, background ui.Color) {
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
func (state *State) hostBadgeFill(background ui.Color) ui.Color {
	if background == state.pal.accentSoft {
		return state.pal.surface
	}
	return state.pal.accentSoft
}

// paintEmptyHosts is the first-run screen: no hosts are invented, the user is
// invited to add one.
func (state *State) paintEmptyHosts(canvas *ui.Canvas, bounds ui.Rect) {
	m := state.messages
	var icon, add flex.Box
	f := state.chrome.empty.Begin(canvas, state.Theme(), bounds)
	icon, add = state.declareEmptyPage(f, bounds, m.HostsEmpty, m.HostsEmptySub, m.NewHost)
	f.End()
	state.paintEmptyPage(canvas, icon, add, iconHosts, hitAddHostEmpty, m.NewHost, true)
}

// paintNoMatches is what the list shows when the search box hides every host.
func (state *State) paintNoMatches(canvas *ui.Canvas, bounds ui.Rect) {
	m := state.messages
	var icon, add flex.Box
	f := state.chrome.empty.Begin(canvas, state.Theme(), bounds)
	icon, add = state.declareEmptyPage(f, bounds, m.NoMatches, m.NoMatchesSub, "")
	f.End()
	state.paintEmptyPage(canvas, icon, add, iconSearch, ui.ID{}, "", true)
}

// lucideIcons maps the internal icon kinds to compiled Lucide icons
// generated by `viva icons` from the SVG sources in icons/.
var lucideIcons = [...]ui.Icon{
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
	iconSFTPTransfer:   icons.SftpTransfer,
	iconSFTPBrowseSync: icons.SftpSync,
	iconSFTPProfiles:   icons.SftpProfiles,
	iconFileClock:      icons.FileClock,
	iconClock:          icons.Clock3,
}

func paintIcon(canvas *ui.Canvas, kind uint8, bounds ui.Rect, foreground ui.Color) {
	if int(kind) >= len(lucideIcons) {
		return
	}
	canvas.DrawIcon(lucideIcons[kind], bounds, foreground)
}

// paintHostDialog renders the modal New Host form on its own layer.
func (state *State) paintHostDialog(canvas *ui.Canvas, viewport ui.Rect) {
	m, theme := state.messages, state.Theme()
	networkHost := state.editor.protocol != protocolSerial
	sshHost := state.editor.protocol == protocolSSH
	title, submit := m.DialogTitle, m.AddHost
	switch state.hostDialogMode {
	case hostDialogEdit:
		title, submit = m.EditHostTitle, m.SaveHost
	case hostDialogCopy:
		title, submit = m.CopyHostTitle, m.AddHost
	}
	// Not a menu: a press on the dimmed area beside the form does nothing, so a
	// half-filled form is not lost by a click beside it. Escape, the close button
	// and Cancel still cancel it.
	dialog := components.BeginDialog(canvas, theme, viewport, components.DialogOptions{
		Layer:          dialogLayer,
		Scope:          dialogScope,
		CloseID:        hitDialogClose,
		InitialFocusID: hitDialogLabel,
		ContentSize:    ui.Size{Width: 680},
		FitBody:        true, BodyHeight: state.hostFormHeight(),
	})
	layout := dialog.Begin(title, "")

	const rowHeight = hostFormRowHeight
	ssh := sshHost
	vncHost := state.editor.protocol == protocolVNC || state.editor.protocol == protocolRDP
	var vncRows flex.Box
	var protocol, label, endpoint, group, tags, iconField, colorField, transfer, proxy, advanced flex.Box
	form := &state.editor.form
	form.count = 0
	if form.lastError != state.dialogError {
		form.lastError = state.dialogError
		if state.dialogError != "" {
			form.offset = state.hostFormHeight()
		}
	}
	area := components.BeginScrollArea(canvas, theme, layout.Body, ui.Size{Height: state.hostFormHeight()}).Offset(0, form.offset).
		Viewport(hitHostFormViewport, title).VerticalScrollbar(hitHostFormScrollbar, title, "")
	geometry := area.BeginContent()
	if geometry.Viewport != form.geometry.Viewport {
		form.lastFocus = ui.ID{}
	}
	form.geometry, form.offset, form.clipped = geometry, geometry.OffsetY, true
	body := geometry.Content
	if geometry.MaxOffsetY > 0 {
		body.Width -= 12
	}
	f := state.chrome.dialog.Begin(canvas, state.Theme(), body)
	f.Column().Fill().PadSides(2, 4, 0, 4).Children(func() {
		protocol = f.Slot().Height(34).MarginSides(0, 0, 10, 0)
		f.Row().Height(rowHeight).Gap(8).Children(func() {
			label = f.Slot().Grow(1)
			colorField = f.Slot().Grow(1)
		})
		endpoint = f.Slot().Height(float32(state.hostEndpointRows()) * rowHeight)
		f.Row().Height(rowHeight).Gap(8).Children(func() {
			group = f.Slot().Grow(1)
			tags = f.Slot().Grow(1)
		})
		f.Row().Height(rowHeight).Gap(8).Children(func() {
			iconField = f.Slot().Grow(1)
			if ssh {
				transfer = f.Slot().Grow(1)
			}
		})
		if networkHost {
			// The proxy is its own row: sharing the icon row clipped both the
			// icon value and the proxy summary on VNC, RDP and Telnet.
			proxy = f.Slot().Height(rowHeight)
		}
		if ssh {
			advanced = f.Slot().Height(state.advancedSectionHeight())
		}
		if vncHost {
			vncRows = f.Slot().Height(state.desktopRowsHeight())
		}
		if state.dialogError != "" {
			// The serial line's error is long: up to two lines.
			f.Paragraph(state.dialogError, 2).FontSize(metrics.FontSizeS).Weight(ui.WeightMedium).Color(state.pal.danger).LineHeight(16)
		}
	})
	f.End()
	state.paintHostProtocolSwitch(canvas, protocol.Rect())
	state.paintDialogField(canvas, hitDialogLabel, fieldLabel, m.FieldLabel, m.FieldLabelPlaceholder, label.Rect())
	state.paintHostEndpointRows(canvas, endpoint.Rect().X, endpoint.Rect().Y, endpoint.Rect().Width, rowHeight)
	state.paintHostGroupPicker(canvas, group.Rect())
	state.paintHostTags(canvas, tags.Rect())
	state.paintHostIconChoice(canvas, iconField.Rect())
	state.paintHostColorChoice(canvas, colorField.Rect())
	if networkHost {
		state.paintHostProxyRow(canvas, proxy.Rect())
	}
	if vncHost {
		state.paintVNCRows(canvas, vncRows.Rect())
	}
	if ssh {
		state.paintTransferChoice(canvas, transfer.Rect())
		state.paintAdvancedSection(canvas, advanced.Rect())
	}

	area.EndContent()
	form.clipped = false
	state.paintHostColorPicker(canvas)
	dialog.Actions(layout,
		components.ButtonAction{ID: hitDialogCancel, Label: m.Cancel, Variant: components.ButtonSecondary},
		components.ButtonAction{ID: hitDialogSubmit, Label: submit},
	)
	state.paintHostTest(canvas, theme, layout.Footer, m.Cancel, submit)
	if state.hostTestAvailable() {
		form.addStop(hitDialogTest, layout.Footer, false)
	}
	form.addStop(hitDialogCancel, layout.Footer, false)
	form.addStop(hitDialogSubmit, layout.Footer, false)
	dialog.End()
}

func (state *State) paintDeleteHostDialog(canvas *ui.Canvas, viewport ui.Rect) {
	body := float32(0)
	if state.dialogError != "" {
		body = 20
	}
	dialog := components.BeginDialog(canvas, state.Theme(), viewport, components.DialogOptions{
		Layer: dialogLayer, Scope: dialogScope,
		DismissID: hitHostDeleteDismiss, CloseID: hitHostDeleteClose, InitialFocusID: hitHostDeleteCancel,
		ContentSize: ui.Size{Width: 520}, FitBody: true, BodyHeight: body, // the error line, when there is one
	})
	layout := dialog.Begin(state.messages.DeleteHostTitle, state.hostDialogText)
	if state.dialogError != "" {
		f := state.chrome.dialog.Begin(canvas, state.Theme(), layout.Body)
		f.Column().Fill().Children(func() {
			f.Text(state.dialogError).FontSize(metrics.FontSizeS).Weight(ui.WeightMedium).Color(state.pal.danger).LineHeight(20)
		})
		f.End()
	}
	dialog.Actions(layout,
		components.ButtonAction{ID: hitHostDeleteCancel, Label: state.messages.Cancel, Variant: components.ButtonSecondary},
		components.ButtonAction{ID: hitHostDeleteConfirm, Label: state.messages.DeleteHostConfirm, Variant: components.ButtonDestructive},
	)
	dialog.End()
}

func (state *State) paintMoveHostGroupDialog(canvas *ui.Canvas, viewport ui.Rect) {
	if state.groupActions.op == hostGroupDelete {
		state.paintHostGroupDeleteDialog(canvas, viewport)
		return
	}
	// No scrim dismiss: a press beside the form must not discard its input.
	dialog := components.BeginDialog(canvas, state.Theme(), viewport, components.DialogOptions{
		Layer: dialogLayer, Scope: dialogScope,
		CloseID: hitMoveGroupClose, InitialFocusID: hitMoveGroupField,
		ContentSize: ui.Size{Width: 460}, FitBody: true, BodyHeight: 90,
	})
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
		description = elide(canvas, state.groupActions.path, state.Theme().Metrics.FontSizeM, ui.WeightRegular, dialog.Layout("").Header.Width)
	}
	layout := dialog.Begin(title, description)
	var slot flex.Box
	f := state.chrome.dialog.Begin(canvas, state.Theme(), layout.Body)
	f.Column().Fill().Children(func() { slot = f.Slot().Height(90) })
	f.End()
	field := components.BeginField(canvas, state.Theme(), slot.Rect()).For(hitMoveGroupField)
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
		components.PaintInput(canvas, state.Theme(), content, components.InputSnapshot{
			ID: hitMoveGroupField, Label: label, Value: value,
			Placeholder: state.messages.FieldGroupPlaceholder, Focused: focused, PlatformEditing: focused && value != "",
		})
	}
	dialog.Actions(layout,
		components.ButtonAction{ID: hitMoveGroupCancel, Label: state.messages.Cancel, Variant: components.ButtonSecondary},
		components.ButtonAction{ID: hitMoveGroupSubmit, Label: submit},
	)
	dialog.End()
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

// hostIconListRows is how many rows of the icon list show before it scrolls.
const hostIconListRows = 7

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
func (state *State) hostIconArt(icon hostIcon, detected sshconn.RemoteSystem, desktop bool) ui.Icon {
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
	choices := make([]hostIcon, 0, 1+len(hostSystemChoices)+5)
	choices = append(choices, hostIconAuto)
	for _, choice := range hostSystemChoices {
		choices = append(choices, hostIconOf(choice.system))
	}
	return append(choices, hostIconServer, hostIconTerminal, hostIconGlobe, hostIconKey, hostIconDesktop)
}

// paintHostIconChoice paints the list of icons a host can have.
func (state *State) paintHostIconChoice(canvas *ui.Canvas, bounds ui.Rect) {
	m, editor, theme := state.messages, &state.editor, state.Theme()
	content := components.BeginField(canvas, theme, bounds).Paint(m.FieldIcon, "", "")
	state.rememberHostControl(hitDialogIcon, content)
	portal := state.beginHostFormPopup(canvas, state.editor.iconOpen)
	defer state.endHostFormPopup(canvas, portal)
	choices := hostIconChoices()
	window := state.windowRect()
	rows := min(len(choices), hostIconListRows)
	top := content.Y + content.Height + 4
	// The list goes under the field unless that leaves it fewer than four rows
	// and there is more room above, where it hangs from the field's top.
	if room := int((window.Height - top - 8 - 8) / 32); room < min(rows, 4) {
		if above := int((content.Y - 4 - 8 - 8) / 32); above > room {
			rows = min(rows, above)
			top = content.Y - 4 - float32(8+rows*32)
		} else {
			rows = max(room, 1)
		}
	} else {
		rows = min(rows, room)
	}
	list := ui.Rect{X: content.X, Y: top, Width: content.Width, Height: float32(8 + rows*32)}
	var scroll *components.MenuScroll
	if editor.iconOpen {
		scroll = state.menuScrollFor(hitDialogIconDismiss)
		state.menuPlaced(list)
		if editor.iconReveal {
			editor.iconReveal = false
			for index, choice := range choices {
				if choice == editor.icon {
					scroll.Offset = max(min(float32(index*32-64), float32((len(choices)-rows)*32)), 0)
				}
			}
		}
	}
	selectControl := components.BeginSelect(canvas, theme, content, components.SelectOptions{
		TriggerID: hitDialogIcon, Label: m.FieldIcon, Value: state.hostIconLabel(editor.icon), Icon: state.hostIconArt(editor.icon, editor.system, editor.protocol == protocolVNC || editor.protocol == protocolRDP),
		Open: editor.iconOpen, Content: list, Scroll: scroll,
		Layer: dialogLayer + 1, Scope: dialogScope, DismissID: hitDialogIconDismiss, Viewport: window,
	})
	defer selectControl.End()
	for _, choice := range choices {
		selectControl.AddItem(components.SelectItem{
			ID: hitDialogIconItemList.At(int(choice)), Label: state.hostIconLabel(choice), Icon: state.hostIconArt(choice, editor.system, editor.protocol == protocolVNC || editor.protocol == protocolRDP),
			Selected: choice == editor.icon,
		})
	}
}

func (state *State) paintDialogField(canvas *ui.Canvas, id ui.ID, field hostField, title, placeholder string, bounds ui.Rect) {
	state.paintDialogFieldWithSecurity(canvas, id, field, title, placeholder, bounds, false)
}

func (state *State) paintSecureDialogField(canvas *ui.Canvas, id ui.ID, field hostField, title, placeholder string, bounds ui.Rect) {
	state.paintDialogFieldWithSecurity(canvas, id, field, title, placeholder, bounds, true)
}

func (state *State) paintDialogFieldWithSecurity(canvas *ui.Canvas, id ui.ID, field hostField, title, placeholder string, bounds ui.Rect, secure bool) {
	theme := state.Theme()
	content := components.BeginField(canvas, theme, bounds).For(id).Paint(title, "", "")
	state.revealHostField(canvas, id, content)
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

// paintHostKeyDialog asks whether to trust a server's key. The safe choice,
// Reject, holds the initial focus.
func (state *State) paintHostKeyDialog(canvas *ui.Canvas, viewport ui.Rect) {
	m, pal, theme := state.messages, state.pal, state.Theme()
	session := state.promptSession()
	if session == nil {
		return
	}
	dialog := components.BeginDialog(canvas, theme, viewport, components.DialogOptions{
		Layer: dialogLayer, Scope: dialogScope,
		DismissID: hitHostKeyDismiss, CloseID: hitHostKeyClose, InitialFocusID: hitHostKeyReject,
		ContentSize: ui.Size{Width: 540}, FitBody: true, BodyHeight: 3*30 - 8, // three rows of address, type and fingerprint
	})
	layout := dialog.Begin(m.HostKeyTitle, state.hostKeyDescription)
	rows := [...]struct{ label, value string }{
		{m.FieldAddress, session.hostKey.Address},
		{m.HostKeyType, session.hostKey.Type},
		{m.HostKeyFingerprint, session.hostKey.Fingerprint},
	}
	f := state.chrome.dialog.Begin(canvas, theme, layout.Body)
	f.Column().Fill().Gap(8).Children(func() {
		for _, row := range rows {
			f.Row().Height(22).Gap(6).Children(func() {
				f.Text(row.label).FontSize(metrics.FontSizeS).Weight(ui.WeightMedium).Color(pal.muted).LineHeight(22).Width(90).Shrink(0)
				f.Text(row.value).FontSize(metrics.FontSizeS).Weight(ui.WeightRegular).Color(pal.text).LineHeight(22)
			})
		}
	})
	f.End()
	dialog.Actions(layout,
		components.ButtonAction{ID: hitHostKeyReject, Label: m.HostKeyReject, Variant: components.ButtonSecondary},
		components.ButtonAction{ID: hitHostKeyOnce, Label: m.HostKeyOnce, Variant: components.ButtonOutline},
		components.ButtonAction{ID: hitHostKeyTrust, Label: m.HostKeyTrust},
	)
	dialog.End()
}

// paintPasswordDialog asks every question of one authentication round, each in
// a field of its own and in the server's order.
func (state *State) paintPasswordDialog(canvas *ui.Canvas, viewport ui.Rect) {
	m, theme := state.messages, state.Theme()
	dialog := components.BeginDialog(canvas, theme, viewport, components.DialogOptions{
		Layer: dialogLayer, Scope: dialogScope,
		DismissID: hitPasswordDismiss, CloseID: hitPasswordClose, InitialFocusID: hitPasswordField,
		ContentSize: ui.Size{Width: 460}, FitBody: true, BodyHeight: state.passwordBodyHeight(),
	})
	layout := dialog.Begin(m.PasswordTitle, state.passwordDescription)
	fields, save := state.layoutPasswordFields(canvas, layout.Body)
	hint := state.passwordHintText()
	for index, bounds := range fields {
		question := state.passwordQuestions[index]
		label := strings.TrimSpace(question.Text)
		if label == "" {
			label = m.PasswordLabel
		}
		field := components.BeginField(canvas, theme, bounds)
		content := field.Paint(label, "", "")
		value := state.passwordFields[index].String()
		id := hitPasswordFields.At(index + 1)
		focused := canvas.Focused() == id
		state.paintSecretInput(canvas, content, components.InputSnapshot{
			ID: id, Label: label, Value: value, Secure: question.Secret,
			Focused: focused, PlatformEditing: focused && (value != "" || !question.Secret), Invalid: state.passwordError != "",
		})
	}
	if state.secrets.canSave {
		state.paintSaveBoxAt(canvas, save, hitPasswordSave, m.PasswordSave, hint, hitPasswordHint)
	}
	dialog.Actions(layout,
		components.ButtonAction{ID: hitPasswordCancel, Label: m.Cancel, Variant: components.ButtonSecondary},
		components.ButtonAction{ID: hitPasswordSubmit, Label: m.Connect},
	)
	dialog.End()
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

// layoutPasswordFields lays out one field per question and the save box under
// them, and returns where each goes.
func (state *State) layoutPasswordFields(canvas *ui.Canvas, body ui.Rect) (fields []ui.Rect, save ui.Rect) {
	fields = state.passwordRects
	boxes := state.passwordBoxes
	if len(fields) != len(state.passwordQuestions) || len(boxes) != len(state.passwordQuestions) {
		fields = make([]ui.Rect, len(state.passwordQuestions))
		boxes = make([]flex.Box, len(state.passwordQuestions))
		state.passwordRects, state.passwordBoxes = fields, boxes
	}
	multi := len(boxes) > 1
	var saveBox flex.Box
	f := state.chrome.dialog.Begin(canvas, state.Theme(), body)
	f.Column().Fill().PadSides(2, 4, 0, 4).Children(func() {
		for index := range boxes {
			boxes[index] = f.Slot().Height(62).MarginSides(0, 0, 4, 0)
		}
		state.hintLine(f)
		state.problemLine(f, state.passwordError)
		if state.secrets.canSave {
			if multi {
				saveBox = f.Slot().Height(20).MarginSides(12, 0, 0, 0)
			} else {
				saveBox = f.Slot().Height(20).MarginSides(8, 0, 0, 0)
			}
		}
	})
	f.End()
	for index := range boxes {
		fields[index] = boxes[index].Rect()
	}
	if state.secrets.canSave {
		save = saveBox.Rect()
	}
	return fields, save
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

// hintLine declares the hint row under the fields. The field's description
// slot would draw past its fixed 62 pixel slot onto the error line, so the
// hint is a row of its own.
func (state *State) hintLine(f *flex.Frame) {
	if state.secrets.canSave {
		return
	}
	if hint := state.passwordHintText(); hint != "" {
		f.Text(hint).FontSize(metrics.FontSizeS).Weight(ui.WeightRegular).Color(state.pal.muted).LineHeight(18)
	}
}

func (state *State) paintKeyPassphraseDialog(canvas *ui.Canvas, viewport ui.Rect) {
	m, theme := state.messages, state.Theme()
	dialog := components.BeginDialog(canvas, theme, viewport, components.DialogOptions{
		Layer: dialogLayer, Scope: dialogScope,
		DismissID: hitKeyPassphraseDismiss, CloseID: hitKeyPassphraseClose, InitialFocusID: hitKeyPassphraseField,
		ContentSize: ui.Size{Width: 540}, FitBody: true, BodyHeight: state.secretFieldBody(state.keyPassphraseError),
	})
	layout := dialog.Begin(m.KeyPassphraseTitle, state.keyPassphraseDescription)
	field := components.BeginField(canvas, theme, state.layoutSecretField(canvas, layout.Body, state.keyPassphraseError))
	content := field.Paint(m.KeyPassphraseLabel, m.KeyPassphraseHint, "")
	value := state.keyPassphrase.String()
	focused := canvas.Focused() == hitKeyPassphraseField
	state.paintSecretInput(canvas, content, components.InputSnapshot{
		ID: hitKeyPassphraseField, Label: m.KeyPassphraseLabel, Value: value, Secure: true,
		Focused: focused, PlatformEditing: focused && value != "", Invalid: state.keyPassphraseError != "",
	})
	state.paintSaveBox(canvas, layout.Body, hitKeyPassphraseSave, m.KeyPassphraseSave)
	dialog.Actions(layout,
		components.ButtonAction{ID: hitKeyPassphraseSkip, Label: m.KeyPassphraseSkip, Variant: components.ButtonSecondary},
		components.ButtonAction{ID: hitKeyPassphraseSubmit, Label: m.KeyUnlock},
	)
	dialog.End()
}

// layoutSecretField declares the body of a prompt for one secret: the field, and
// under it what went wrong, with a little air around both. It returns where the
// field goes; the box offering to save what is typed is placed by paintSaveBox.
func (state *State) layoutSecretField(canvas *ui.Canvas, body ui.Rect, problem string) ui.Rect {
	var field flex.Box
	f := state.chrome.dialog.Begin(canvas, state.Theme(), body)
	f.Column().Fill().PadSides(2, 4, 0, 4).Children(func() {
		field = f.Slot().Height(62)
		if problem != "" {
			f.Text(problem).FontSize(metrics.FontSizeS).Weight(ui.WeightMedium).Color(state.pal.danger).LineHeight(18).MarginSides(12, 0, 0, 0)
		}
	})
	f.End()
	return field.Rect()
}
