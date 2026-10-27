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
	iconFilm
	iconFileDown
	iconColumns2
	iconRows2
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
)

// Paint renders the application from state using stable hit identities.
func (state *State) Paint(canvas *ui.Canvas, size ui.Size) {
	canvas.SetTree(&tree)
	// Motty keeps one central click handler. The event pass of a click names its
	// target here; the handler runs once this paint has returned, outside it,
	// because it may open a native dialog or block. The repaint that follows
	// shows the result. Asking also makes the runtime deliver clicks at all.
	if id, ok := canvas.ClickTarget(); ok {
		canvas.AfterPaint(func() { state.Click(id) })
	}
	state.syncVisible()
	state.contextRect = ui.Rect{}
	state.hostsBounds, state.chrome.hostList = ui.Rect{}, ui.Rect{}
	state.devBeginPaint()
	state.ai.cfg.mcpSelectBounds = ui.Rect{}
	if !state.aiSettingsVisible() || state.settingsCategory != settingsMCP || state.settingsSearching() {
		state.ai.cfg.mcpSelectOpen = false
	}
	state.aiPanelBeginPaint()
	// The terminal layout follows the window and the font; remember what it
	// was painted with so a resize can work out the rows and columns.
	state.viewport = size
	state.cellWidth, state.cellHeight = canvas.MonoCellSize(state.terminalFontSize, state.terminalFontFace)
	pal := state.pal
	// The window is a bar over a body. The terminals show the picture through their
	// own ground, so with one set only the bar is shell-colored; the shell behind
	// them must not cover it.
	picture := state.paintBackground(canvas, size)
	terminal := state.activeSection == sectionTerminal && state.deck.ActiveWorkspace() != 0
	chrome := &state.chrome
	geometry := state.windowBarGeometry(canvas, size.Width)
	active := layoutcore.WorkspaceID(0)
	if state.activeSection == sectionTerminal {
		active = state.deck.ActiveWorkspace()
	}
	scroll := state.tabStrip.layout(geometry.area, float32(len(state.workspaceTabs))*(geometry.tabWidth+barGap), state.workspaceTabs, geometry.tabWidth+barGap, geometry.tabWidth, active)
	var workspaceSlot flex.Box
	// The section and assistant are sibling cards, separated by the same margin
	// the cards keep from the window's right and bottom edges.
	cardRight := sectionCardMargin
	if reserve := state.aiSectionReserve(size); reserve > 0 {
		cardRight = reserve
	}
	f := chrome.window.Begin(canvas, state.Theme(), ui.Rect{Width: size.Width, Height: size.Height})
	root := f.Column().Fill()
	if !picture {
		root.Background(pal.shell)
	}
	root.Children(func() {
		state.declareWindowBar(f, &chrome.bar, false, picture, geometry, scroll)
		if !terminal {
			f.Row().Grow(1).Children(func() {
				state.declareNavigation(f)
				workspaceSlot = f.Slot().Grow(1).MarginSides(0, cardRight, sectionCardMargin, 0).
					Radius(radius.surface).Border(1, pal.windowStroke).Background(pal.workspace)
			})
		}
	})
	f.End()
	state.paintWindowBarParts(canvas, geometry.area)
	if !terminal {
		state.paintLockup(canvas, chrome.lockup.Rect())
	}

	if terminal {
		state.paintWorkspacePanes(canvas, size)
		state.paintAIPanel(canvas, size)
	} else {
		workspace := workspaceSlot.Rect()
		canvas.Clip(workspace)
		if !state.devPaintPage(canvas, workspace) {
			switch state.activeSection {
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
			default:
				state.paintHostsWorkspace(canvas, workspace)
			}
		}
		canvas.Unclip()
		state.paintAIPanel(canvas, size)
	}
	viewport := state.windowRect()
	state.beginMenus()
	state.paintMCPClientSelect(canvas)
	state.paintTransferCenter(canvas)
	// The gallery's select is painted here, outside the workspace's clip and
	// inside the frame's menu pass, so its popup can leave the page and keeps the
	// menu state its keyboard handling needs.
	state.devPaintOverlay(canvas)
	switch state.modal {
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
	case modalHostImportResult:
		state.paintHostImportResult(canvas, viewport)
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
	case modalMasterPassword:
		state.paintMasterPasswordDialog(canvas, viewport)
	case modalVaultForget:
		state.paintVaultForgetDialog(canvas, viewport)
	case modalHighlightRule:
		state.paintHighlightDialog(canvas, viewport)
	case modalAutofillRule:
		state.paintAutofillDialog(canvas, viewport)
	case modalAutofillConfirm:
		state.paintAutofillConfirm(canvas, viewport)
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
	case modalUpdate:
		state.paintUpdateDialog(canvas, viewport)
	case modalUpdateProgress:
		state.paintUpdateProgress(canvas, viewport)
	case modalCommandPalette:
		state.paintCommandPalette(canvas, viewport)
	case modalHistory:
		state.paintHistory(canvas, viewport)
	case modalShortcutCapture:
		state.paintShortcutCaptureDialog(canvas, viewport)
	}
	if state.knownHosts.action != knownHostsActionNone {
		state.paintKnownHostsDialog(canvas, viewport)
	}
	state.paintAIDialogs(canvas, viewport)
	state.paintTerminalMenu(canvas, viewport)
	state.paintTabDrag(canvas, viewport)
	state.paintTabMenu(canvas, viewport)
	state.paintHostMenu(canvas, viewport)
	state.paintSFTPMenu(canvas, viewport)
	state.paintHostMenus(canvas, viewport)
	state.endMenus()
	state.requestReconnectFrames(canvas)
	state.requestAutofillFrames(canvas)
	// Last, so a tooltip is never covered by what it describes.
	state.tooltip.Paint(canvas, state.Theme(), viewport, components.TooltipOptions{Layer: tooltipLayer, SideOffset: tooltipGap}, components.HoverTooltipDelay)
	state.fileNotices.paint(canvas, state.Theme(), ui.Rect{Y: windowBarHeight, Width: size.Width, Height: max(size.Height-windowBarHeight, 0)})
}

// windowRect is the whole window as last painted: the viewport a popup gives
// its Viva component so a press anywhere outside it dismisses it.
func (state *State) windowRect() ui.Rect {
	return ui.Rect{Width: state.viewport.Width, Height: state.viewport.Height}
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
// out: the assistant's button and, for each tab, where its system and its light go.
type windowBar struct {
	assistant flex.Box
	transfers flex.Box
	tabs      []barTab
}

type barTab struct {
	badge, light flex.Box
	workspace    layoutcore.WorkspaceInfo
	id, closeID  ui.ID
}

// barGeometry is what a first pass over the window bar says about its tabs: the
// room they and the new-tab button share (area), the row they clip to, and how
// wide each is. It depends only on the window's width and the number of tabs, so
// it is kept until one changes.
type barGeometry struct {
	width             float32
	tabCount          int
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
	geometry := &chrome.geometry
	if geometry.valid && geometry.width == width && geometry.tabCount == len(state.workspaceTabs) &&
		geometry.leading == leading && geometry.trailing == trailing {
		return *geometry
	}
	f := chrome.probe.Begin(canvas, state.Theme(), ui.Rect{Width: width, Height: windowBarHeight})
	f.Column().Fill().Children(func() {
		state.declareWindowBar(f, &chrome.probeBar, true, false, barGeometry{}, 0)
	})
	f.End()
	*geometry = barGeometry{
		width: width, tabCount: len(state.workspaceTabs), leading: leading, trailing: trailing, valid: true,
		area: chrome.probeArea.Rect(), row: chrome.probeRow.Rect(), tabWidth: barTabMax,
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
	themeIcon, themeLabel := iconSun, m.LabelThemeToLight
	if state.appearance == appearanceLight {
		themeIcon, themeLabel = iconMoon, m.LabelThemeToDark
	}
	activeSection := state.activeSection == sectionTerminal
	bar.tabs = bar.tabs[:0]
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
		// The room of the tabs and the new-tab button. The tabs stay as wide as they
		// like (barTabMax) and shrink together to barTabMin; the new-tab button
		// follows the last of them, or ends the room when they do not fit.
		f.Stack().Grow(1).MarginSides(0, barGap, 0, 0).Children(func() {
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
						workspace := state.workspaceTabs[index]
						id, closeID := hitTabList.At(int(workspace.ID)), hitTabCloseList.At(int(workspace.ID))
						var tab flex.Box
						if probe {
							tab = f.Slot().Width(barTabMax).MinWidth(barTabMin).Height(barTabHeight).Shrink(1).MarginSides(0, barGap, 0, 0)
							chrome.probeTabs = append(chrome.probeTabs, tab)
							bar.tabs = append(bar.tabs, barTab{})
							continue
						}
						entry := barTab{workspace: workspace, id: id, closeID: closeID}
						active := activeSection && state.deck.ActiveWorkspace() == workspace.ID
						hovered := f.Canvas().Hovered() == id || f.Canvas().Hovered() == closeID
						foreground := pal.muted
						fill, edge := pal.tab, pal.line
						switch {
						case active:
							fill, edge, foreground = pal.tabActive, pal.windowStroke, pal.text
						case hovered:
							fill, foreground = pal.tabHover, pal.text
						}
						first := float32(0)
						if index == 0 {
							first = -scroll
						}
						f.Row().Size(geometry.tabWidth, barTabHeight).Shrink(0).MarginSides(0, barGap, 0, first).
							PadSides(0, 3, 0, 4).Items(flex.Center).Radius(radius.item).Border(1, edge).Background(fill).
							HitWith(id, workspace.Title, ui.HitOptions{Role: ui.RoleTab, Selected: active}).Children(func() {
							entry.badge = f.Slot().Size(20, 20).MarginSides(0, 7, 0, 0)
							f.Text(workspace.Title).FontSize(metrics.FontSizeS).Weight(ui.WeightMedium).Color(foreground).LineHeight(20).Ellipsis()
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
		f.Row().Shrink(0).MarginSides(0, 0, 0, 10).Gap(2).Children(func() {
			transferLabel := m.Transfers.Title
			if state.transfers.buttonLabel != "" {
				transferLabel = state.transfers.buttonLabel
			}
			bar.transfers = state.barButton(f, probe, hitTransferCenter, iconTransfer, transferLabel)
			bar.assistant = f.Slot().Size(barButtonSize, barButtonSize)
			state.barButton(f, probe, hitWindowLocalTerminal, iconTerminal, m.LocalTerminal)
			state.barButton(f, probe, hitCommandPalette, iconSearch, m.CommandPaletteTitle)
			state.barButton(f, probe, hitLanguage, iconGlobe, m.LabelLanguage)
			state.barButton(f, probe, hitTheme, themeIcon, themeLabel)
			state.barButton(f, probe, hitWindowSettings, iconSettings, m.NavSettings)
		})
	})
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
// the layout kept for them.
func (state *State) paintWindowBarParts(canvas *ui.Canvas, strip ui.Rect) {
	bar := &state.chrome.bar
	canvas.Clip(strip)
	for index := range bar.tabs {
		tab := &bar.tabs[index]
		var session *Session
		if pane, ok := state.deck.Pane(tab.workspace.ID, tab.workspace.ActivePane); ok && pane.SessionID != 0 {
			session, _ = state.sessionByIDPointer(uint32(pane.SessionID))
		}
		if session != nil {
			fill := state.pal.tab
			switch active := state.activeSection == sectionTerminal && state.deck.ActiveWorkspace() == tab.workspace.ID; {
			case active:
				fill = state.pal.tabActive
			case canvas.Hovered() == tab.id || canvas.Hovered() == tab.closeID:
				fill = state.pal.tabHover
			}
			state.paintHostCatalogIcon(canvas, session.host, tab.badge.Rect(), fill)
			kind, dot := state.sessionLight(session)
			light := tab.light.Rect()
			paintLight(canvas, light.X+light.Width/2, light.Y+light.Height/2, kind, dot, fill)
		}
		state.openTabMenuOn(canvas, tab.workspace.ID, tab.id, tab.closeID)
	}
	canvas.Unclip()
	state.paintAIBarButton(canvas, state.chrome.bar.assistant.Rect().X)
	state.paintTransferBadge(canvas)
}

// paintBarButton is a square icon button of the window bar whose left edge is at x,
// for a caller that has its own place for one (the assistant's).
func (state *State) paintBarButton(canvas *ui.Canvas, id ui.ID, x float32, icon uint8, label string) {
	f := state.chrome.widget.Begin(canvas, state.Theme(), ui.Rect{X: x, Width: barButtonSize, Height: windowBarHeight})
	f.Column().Fill().Center().Children(func() {
		state.barButton(f, false, id, icon, label)
	})
	f.End()
}

// elide shortens text to fit width, ending it with an ellipsis. Weight is the
// one the text is drawn at, since a heavier line is wider.
func elide(canvas *ui.Canvas, value string, size, weight, width float32) string {
	if canvas.TextWidthWeight(value, size, weight) <= width {
		return value
	}
	runes := []rune(value)
	for len(runes) > 1 {
		runes = runes[:len(runes)-1]
		if candidate := string(runes) + "…"; canvas.TextWidthWeight(candidate, size, weight) <= width {
			return candidate
		}
	}
	return "…"
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
		{hitKeychain, iconKey, m.NavKeychain, state.activeSection == sectionKeys},
		{hitProxies, iconGlobe, m.NavProxies, state.activeSection == sectionProxies},
		{hitPortForwarding, iconForward, m.NavPortForwarding, state.activeSection == sectionForwards},
		{hitKnownHosts, iconKnown, m.NavKnownHosts, state.activeSection == sectionKnownHosts},
	}
	f.Column().Width(navigationWidth).Shrink(0).Children(func() {
		// The logo with the name beside it, painted into this slot after layout.
		state.chrome.lockup = f.Slot().Size(lockupWidth, lockupHeight).MarginSides(15, 0, 20, 0).Self(flex.Center)
		for _, item := range items {
			state.navigationItem(f, item.id, item.icon, item.label, item.active, 4)
		}
		// A shell on this computer needs no host, so its way in is always at hand:
		// above the Settings, at the bottom of the sidebar. A dev build also puts
		// the component gallery there (see declareDevNavigation).
		f.Spacer()
		state.navigationItem(f, hitLocalTerminal, iconTerminal, m.LocalTerminal, false, 4)
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
		f.Icon(lucideIcons[icon]).Color(pal.text)
		f.Text(label).FontSize(metrics.FontSizeM).Weight(ui.WeightMedium).Color(pal.text)
	})
}

func (state *State) paintHostsWorkspace(canvas *ui.Canvas, bounds ui.Rect) {
	list := state.paintHostsHeader(canvas, bounds)
	state.chrome.hostList = list

	state.hostsContent = 0
	switch {
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

// paintHostsHeader is the two rows over the host list: the toolbar (search,
// Connect, Import, New Host) and the row of group chips or, while something is
// checked, of the actions on what is checked. Hits are added in the order the
// controls were painted before the page was declared: Tab follows it. It returns
// where the list goes under them.
func (state *State) paintHostsHeader(canvas *ui.Canvas, bounds ui.Rect) ui.Rect {
	m, pal := state.messages, state.pal
	chrome := &state.chrome
	// The buttons keep their natural width, so the search box gives way to them
	// rather than the other way round: a label that is wider than expected (a
	// heavier or wider font) must not slide a button under its neighbour.
	connectWidth := toolbarButtonWidth(canvas, m.Connect, false)
	newHostWidth := toolbarButtonWidth(canvas, m.NewHost, true)
	importWidth := toolbarButtonWidth(canvas, m.ImportMenu, true)
	searchWidth := min(max(bounds.Width*0.41, 330), 485)

	const (
		rowHeight   = float32(28)
		toggleWidth = float32(56)
	)
	anySelected := len(state.hostSelections) > 0
	// While something is checked the first label lines up with the list's left
	// edge, so the button's padding hangs outside it (a negative margin).
	const quietPadding = float32(12)
	actions := [...]struct {
		id      ui.ID
		label   string
		enabled bool
		danger  bool
	}{
		{hitExportHosts, m.ExportHosts, len(state.hosts) > 0, false},
		{hitEditHost, m.EditHost, false, false},
		{hitCopyHost, m.CopyHost, false, false},
		{hitMoveHostGroup, m.MoveHostGroup, false, false},
		{hitDeleteHost, m.DeleteHost, false, true},
	}
	var actionWidths [len(actions)]float32
	if anySelected {
		enabled := state.selectedHostID != 0
		single := enabled && len(state.hostSelections) <= 1
		actions[1].enabled, actions[2].enabled, actions[3].enabled, actions[4].enabled = single, single, enabled, enabled
		for index, action := range actions {
			actionWidths[index] = canvas.TextWidthWeight(action.label, metrics.FontSizeS, ui.WeightSemibold) + 4 + 2*quietPadding
		}
	}
	// Which chips fit decides what is declared; where they sit is the row's.
	showChips := !anySelected && state.hasHostFilter()
	if showChips {
		state.fitHostChips(canvas, bounds.Width-2*18-toggleWidth-12)
	}

	var search, connect, importButton, newHost, toggle, list flex.Box
	var buttons [len(actions)]flex.Box
	f := chrome.header.Begin(canvas, state.Theme(), bounds)
	f.Column().Fill().Children(func() {
		f.Column().Height(hostListTop).Shrink(0).PadSides(14, 0, 0, 0).Children(func() {
			f.Row().Height(toolbarHeight).MarginX(18).Items(flex.Center).Children(func() {
				search = f.Slot().Basis(searchWidth).Shrink(1).MinWidth(140).Height(toolbarHeight).MarginSides(0, 10, 0, 0)
				connect = f.Slot().Size(connectWidth, toolbarHeight).MarginSides(0, 10, 0, 0)
				f.Spacer()
				importButton = f.Slot().Size(importWidth, toolbarHeight).MarginSides(0, 10, 0, 0)
				newHost = f.Slot().Size(newHostWidth, toolbarHeight)
			})
			// One row under the toolbar. On the left, either the groups to choose from
			// or what can be done with what is checked; on the right, how much is
			// checked, then the choice of view.
			f.Row().Height(rowHeight).MarginSides(12, 0, 0, 0).PadSides(0, 18, 0, 18).Gap(12).Items(flex.Center).Children(func() {
				switch {
				case anySelected:
					f.Row().Gap(4).MarginSides(0, 0, 0, -quietPadding).Children(func() {
						for index := range buttons {
							buttons[index] = f.Slot().Size(actionWidths[index], rowHeight)
						}
					})
					f.Spacer()
					f.Text(state.hostSelectionText).FontSize(metrics.FontSizeS).Weight(ui.WeightMedium).Color(pal.muted).MarginSides(0, 4, 0, 0)
				case showChips:
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
					f.Spacer()
				default:
					f.Spacer()
				}
				toggle = f.Slot().Size(toggleWidth-2, rowHeight).MarginSides(0, 0, 0, 2)
			})
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
	state.paintToolbarButtonIn(canvas, hitConnect, connect.Rect(), m.Connect, 0, false, state.typedQuery() != "")
	state.paintToolbarButtonIn(canvas, hitAddHost, newHost.Rect(), m.NewHost, iconPlus, true, true)
	chrome.importButton = importButton.Rect()
	state.paintToolbarButtonIn(canvas, hitImportMenu, chrome.importButton, m.ImportMenu, iconDownload, false, true)

	state.paintHostViewToggle(canvas, toggle.Rect().X, toggle.Rect().Y+toggle.Rect().Height/2)
	if anySelected {
		for index, action := range actions {
			state.paintQuietButton(canvas, action.id, buttons[index].Rect(), action.label, action.danger, action.enabled)
			if action.id == hitMoveHostGroup {
				state.groupMenu.button = buttons[index].Rect()
			}
		}
		return list.Rect()
	}
	if showChips {
		state.paintHostFilterChips(canvas, chrome.chips)
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
	}
	menu.End()
}

// paintQuietButton is a text button with no edge or fill until the pointer is on
// it: for a row of actions on a selection, where five bordered boxes in a line
// would be louder than the list they act on.
func (state *State) paintQuietButton(canvas *ui.Canvas, id ui.ID, bounds ui.Rect, label string, danger, enabled bool) {
	pal := state.pal
	foreground := pal.text
	if danger {
		foreground = pal.danger
	}
	var fill ui.Color
	switch {
	case !enabled:
		foreground = pal.faint
	case canvas.Pressed() == id:
		fill = pal.surfaceHot
	case canvas.Hovered() == id:
		fill = pal.surfaceSoft
	}
	f := state.chrome.widget.Begin(canvas, state.Theme(), bounds)
	f.Row().Fill().Center().Radius(radius.control).Background(fill).
		HitWith(id, label, ui.HitOptions{Role: ui.RoleButton, Disabled: !enabled}).Children(func() {
		f.Text(label).FontSize(metrics.FontSizeS).Weight(ui.WeightSemibold).Color(foreground)
	})
	f.End()
	if canvas.FocusVisible() == id {
		canvas.RoundedBorder(bounds, radius.control, 1.5, pal.accent, ui.Color{})
	}
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
	toolbarPadding = float32(16)
	toolbarIconGap = float32(8)
	toolbarIcon    = float32(16)
	toolbarHeight  = float32(36) // the search box and its buttons: 13 pt labels in 36, not 12 in 40
	toolbarFont    = float32(13)
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

// paintToolbarButton draws a toolbar button whose top left corner is (x, y) and
// that is width wide, for a page that has not yet declared its button's place.
func (state *State) paintToolbarButton(canvas *ui.Canvas, id ui.ID, x, y, width float32, label string, icon uint8, primary, enabled bool) {
	state.paintToolbarButtonIn(canvas, id, ui.Rect{X: x, Y: y, Width: width, Height: toolbarHeight}, label, icon, primary, enabled)
}

// paintToolbarButtonIn draws a toolbar button in bounds. Icon and label are centered
// together as one group, so the button reads as balanced at any label width.
func (state *State) paintToolbarButtonIn(canvas *ui.Canvas, id ui.ID, bounds ui.Rect, label string, icon uint8, primary, enabled bool) {
	state.paintButtonSized(canvas, id, bounds, label, icon, primary, false, enabled, toolbarFont)
}

// paintButton is the workbench's one button: a filled surface with a hairline
// edge, or the accent when primary. danger keeps that look and turns the label
// red, so a destructive action is marked without becoming the loudest thing on the
// page.
func (state *State) paintButton(canvas *ui.Canvas, id ui.ID, bounds ui.Rect, label string, icon uint8, primary, danger, enabled bool) {
	state.paintButtonSized(canvas, id, bounds, label, icon, primary, danger, enabled, 12)
}

func (state *State) paintButtonSized(canvas *ui.Canvas, id ui.ID, bounds ui.Rect, label string, icon uint8, primary, danger, enabled bool, font float32) {
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
	f := state.chrome.widget.Begin(canvas, state.Theme(), bounds)
	f.Row().Fill().Center().Radius(radius.control).Border(1, border.Over(pal.workspace)).Background(background.Over(pal.workspace)).
		HitWith(id, label, ui.HitOptions{Role: ui.RoleButton, Disabled: !enabled}).Children(func() {
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
	// The list scrolls under the toolbar: the tree starts above its window by the
	// scrolled distance, and the clip keeps what is out of the window out of sight
	// and out of reach.
	state.hostsContent = state.hostListHeight(treeBounds.Width)
	state.hostsScroll = min(max(state.hostsScroll, 0), max(state.hostsContent-treeBounds.Height, 0))
	canvas.Clip(treeBounds)
	if state.hostView == hostViewGrid {
		state.paintHostGrid(canvas, treeBounds)
		canvas.Unclip()
		return
	}
	tree := components.BeginTree(canvas, state.Theme(), ui.Rect{
		X: treeBounds.X, Y: treeBounds.Y - state.hostsScroll, Width: treeBounds.Width, Height: treeBounds.Height + state.hostsScroll,
	}).Indent(22)
	filtering := strings.TrimSpace(state.visibleQuery) != ""
	for _, entry := range state.hostTreeRows {
		switch entry.Kind {
		case hostTreeGroup:
			group := state.hostGroups[entry.GroupIndex]
			tree = tree.RowHeight(hostGroupRowHeight)
			row := tree.Item(components.TreeItem{
				ID: hitHostGroupRowList.At(entry.GroupIndex), DisclosureID: hitHostGroupOpenList.At(entry.GroupIndex),
				Label: state.hostGroupLabel(group), Branch: true, Expanded: group.Expanded || filtering,
			})
			if row.Bounds.Y >= treeBounds.Y+treeBounds.Height {
				canvas.Unclip()
				return
			}
			f := state.chrome.hostRow.Begin(canvas, state.Theme(), row.Content)
			f.Row().Fill().PadSides(0, 0, 0, 2).Gap(6).Items(flex.Center).Children(func() {
				f.Text(state.hostGroupLabel(group)).FontSize(metrics.FontSizeS).Weight(ui.WeightSemibold).Color(pal.text).LineHeight(23)
				f.Text(group.Count).FontSize(metrics.FontSizeS).Weight(ui.WeightMedium).Color(pal.faint).LineHeight(21)
			})
			f.End()
		case hostTreeHost:
			host := state.hosts[entry.HostIndex]
			id := hitHostList.At(int(host.ID))
			tree = tree.RowHeight(hostRowHeight)
			depth := uint8(1)
			if state.hostsFlat() {
				depth = 0
			}
			row := tree.Item(components.TreeItem{ID: id, Label: host.Label, Depth: depth, Selected: host.ID == state.selectedHostID})
			if row.Bounds.Y >= treeBounds.Y+treeBounds.Height {
				canvas.Unclip()
				return
			}
			state.paintHostTreeRow(canvas, row.Bounds, row.Content, host, id)
			state.hostRowEvents(canvas, id, host)
		}
	}
	canvas.Unclip()
}

// hostListTop is where the list starts under the hosts workspace's top edge: below the toolbar and the row of chips or actions.
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

// scrollHosts moves the hosts list with the wheel.
func (state *State) scrollHosts(event ui.ScrollEvent) ui.EventResult {
	step := event.DeltaY
	if !event.Precise && step > 0 && step < 24 {
		step = 24
	} else if !event.Precise && step < 0 && step > -24 {
		step = -24
	}
	before := state.hostsScroll
	visible := state.chrome.hostList.Height
	state.hostsScroll = min(max(before+step, 0), max(state.hostsContent-visible, 0))
	return ui.EventResult{Handled: true, Repaint: state.hostsScroll != before}
}

func (state *State) paintHostTreeRow(canvas *ui.Canvas, row, content ui.Rect, host Host, id ui.ID) {
	pal := state.pal
	rowBackground := pal.workspace
	if host.ID == state.selectedHostID || canvas.Hovered() == id {
		rowBackground = pal.surfaceSoft
	}
	// The columns follow the row's width: the name, then the address and the tags
	// further right. The status is pinned to the row's right edge so it is never
	// pushed out of a narrow window; the address and the tags take what is left, in
	// proportion, and the tags give way first. Distances are from the content's left.
	const (
		statusWidth = float32(124)
		leadWidth   = float32(92) // the check box, the system and the gaps before the name
	)
	indent := content.X - row.X
	nameWidth := max(row.Width*0.24, float32(170))
	statusStart := row.Width - statusWidth - indent
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
					f.Text(host.Label).FontSize(metrics.FontSizeM).Weight(ui.WeightSemibold).Color(pal.text).LineHeight(22)
					f.Text(state.hostSubtitle(host)).FontSize(metrics.FontSizeS).Color(pal.muted).LineHeight(19)
				})
			})
			f.Row().Width(tagsStart - addressStart).Shrink(0).Children(func() {
				f.Text(host.target).FontSize(metrics.FontSizeS).Weight(ui.WeightMedium).Color(pal.text).LineHeight(22).Width(addressWidth).Shrink(0)
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
			light = f.Slot().Size(7, 7).MarginSides(0, 8, 3, 0)
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
	pal, m := state.pal, state.messages
	width := toolbarButtonWidth(canvas, m.NewHost, true)
	var add flex.Box
	f := state.chrome.empty.Begin(canvas, state.Theme(), bounds)
	f.Column().Fill().Items(flex.Center).Children(func() {
		// The icon's top is 46 above the point 40% down the page.
		f.Slot().HeightPercent(40).MinHeight(46)
		f.Row().Size(62, 62).MarginSides(-46, 0, 0, 0).Center().Radius(15).Background(pal.surface).Children(func() {
			f.Icon(lucideIcons[iconHosts]).Size(24, 24).Color(pal.muted)
		})
		f.Text(m.HostsEmpty).FontSize(17).Weight(ui.WeightSemibold).Color(pal.text).MarginSides(20, 0, 0, 0)
		f.Text(m.HostsEmptySub).FontSize(metrics.FontSizeM).Color(pal.muted).MarginSides(8, 0, 0, 0)
		add = f.Slot().Size(width, toolbarHeight).MarginSides(20, 0, 0, 0)
	})
	f.End()
	state.paintToolbarButtonIn(canvas, hitAddHostEmpty, add.Rect(), m.NewHost, iconPlus, true, true)
}

// paintNoMatches is what the list shows when the search box hides every host.
func (state *State) paintNoMatches(canvas *ui.Canvas, bounds ui.Rect) {
	pal, m := state.pal, state.messages
	f := state.chrome.empty.Begin(canvas, state.Theme(), bounds)
	f.Column().Fill().Items(flex.Center).Children(func() {
		f.Slot().HeightPercent(40)
		f.Text(m.NoMatches).FontSize(17).Weight(ui.WeightSemibold).Color(pal.text).MarginSides(2, 0, 0, 0)
		f.Text(m.NoMatchesSub).FontSize(metrics.FontSizeM).Color(pal.muted).MarginSides(8, 0, 0, 0)
	})
	f.End()
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
	iconFilm:       icons.Film,
	iconFileDown:   icons.FileDown,
	iconColumns2:   icons.Columns2,
	iconRows2:      icons.Rows2,

	iconBot:          icons.Bot,
	iconHistory:      icons.History,
	iconCopy:         icons.Copy,
	iconChevronRight: icons.ChevronRight,
	iconChevronDown:  icons.ChevronDown,
	iconSliders:      icons.SlidersHorizontal,
	iconAlert:        icons.CircleAlert,
	iconCircleCheck:  icons.CircleCheck,
	iconRadio:        icons.RadioTower,
	iconKeyboard:     icons.Keyboard,
	iconStar:         icons.Star,
	iconStarSolid:    icons.StarSolid,
	iconArchive:      icons.Archive,
	iconHardDrive:    icons.HardDrive,
	iconExternal:     icons.SquarePen,
	iconPlug:         icons.Plug,
	iconPaste:        icons.ClipboardPaste,
	iconSelectAll:    icons.SquareDashed,
	iconEraser:       icons.Eraser,
	iconWrench:       icons.Wrench,
	iconCloseOthers:  icons.ListX,
	iconCloseRight:   icons.ArrowRightToLine,
	iconSparkles:     icons.Sparkles,
	iconTransfer:     icons.ArrowUpDown,
	iconBug:          icons.Bug,
	iconInfo:         icons.Info,
	iconQQ:           icons.Qq,
	iconWeChat:       icons.Wechat,
	iconDiscord:      icons.Discord,
	iconGitHub:       icons.Github,
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
	proxyHeight := float32(0)
	proxyRowHeight := float32(hostFormRowHeight)
	if networkHost {
		proxyHeight = hostFormRowHeight
		if !sshHost {
			// A compact full-width row fits the short host dialog where sharing
			// the icon row used to clip both values.
			proxyHeight, proxyRowHeight = 48, 48
		}
	}
	title, submit := m.DialogTitle, m.AddHost
	switch state.hostDialogMode {
	case hostDialogEdit:
		title, submit = m.EditHostTitle, m.SaveHost
	case hostDialogCopy:
		title, submit = m.CopyHostTitle, m.AddHost
	}
	dialog := components.BeginDialog(canvas, theme, viewport, components.DialogOptions{
		Layer:          dialogLayer,
		Scope:          dialogScope,
		DismissID:      hitDialogDismiss,
		CloseID:        hitDialogClose,
		InitialFocusID: hitDialogLabel,
		ContentSize:    ui.Size{Width: 460, Height: state.hostDialogHeight() + proxyHeight},
	})
	layout := dialog.Begin(title, "")

	const rowHeight = hostFormRowHeight
	ssh := sshHost
	vncHost := state.editor.protocol == protocolVNC || state.editor.protocol == protocolRDP
	var vncRows flex.Box
	var protocol, label, endpoint, group, tags, iconField, transfer, proxy, advanced flex.Box
	f := state.chrome.dialog.Begin(canvas, state.Theme(), layout.Body)
	f.Column().Fill().PadSides(2, 4, 0, 4).Children(func() {
		protocol = f.Slot().Height(34).MarginSides(0, 0, 10, 0)
		label = f.Slot().Height(rowHeight)
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
			proxy = f.Slot().Height(proxyRowHeight)
		}
		if ssh {
			advanced = f.Slot().Height(state.advancedSectionHeight())
		}
		if vncHost {
			vncRows = f.Slot().Height(vncRowsHeight)
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
	state.paintDialogField(canvas, hitDialogGroup, fieldGroup, m.FieldGroup, m.FieldGroupPlaceholder, group.Rect())
	state.paintDialogField(canvas, hitDialogTags, fieldTags, m.FieldTags, m.FieldTagsPlaceholder, tags.Rect())
	state.paintHostIconChoice(canvas, iconField.Rect())
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

	dialog.Actions(layout,
		components.ButtonAction{ID: hitDialogCancel, Label: m.Cancel, Variant: components.ButtonSecondary},
		components.ButtonAction{ID: hitDialogSubmit, Label: submit},
	)
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
	dialog := components.BeginDialog(canvas, state.Theme(), viewport, components.DialogOptions{
		Layer: dialogLayer, Scope: dialogScope,
		DismissID: hitMoveGroupDismiss, CloseID: hitMoveGroupClose, InitialFocusID: hitMoveGroupField,
		ContentSize: ui.Size{Width: 460}, FitBody: true, BodyHeight: 62,
	})
	layout := dialog.Begin(state.messages.MoveGroupTitle, "")
	var slot flex.Box
	f := state.chrome.dialog.Begin(canvas, state.Theme(), layout.Body)
	f.Column().Fill().Children(func() { slot = f.Slot().Height(62) })
	f.End()
	field := components.BeginField(canvas, state.Theme(), slot.Rect()).For(hitMoveGroupField)
	if state.dialogError != "" {
		field = field.Invalid()
	}
	content := field.Paint(state.messages.MoveGroupField, "", state.dialogError)
	value := state.hostGroupEditor.String()
	focused := canvas.Focused() == hitMoveGroupField
	content.Height = 32
	components.PaintInput(canvas, state.Theme(), content, components.InputSnapshot{
		ID: hitMoveGroupField, Label: state.messages.MoveGroupField, Value: value,
		Placeholder: state.messages.FieldGroupPlaceholder, Focused: focused, PlatformEditing: focused && value != "",
	})
	dialog.Actions(layout,
		components.ButtonAction{ID: hitMoveGroupCancel, Label: state.messages.Cancel, Variant: components.ButtonSecondary},
		components.ButtonAction{ID: hitMoveGroupSubmit, Label: state.messages.MoveGroupSubmit},
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
	{sshconn.SystemManjaro, "Manjaro"}, {sshconn.SystemFreeBSD, "FreeBSD"}, {sshconn.SystemLinux, "Linux"},
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
	value := state.editor.field(field).String()
	focused := canvas.Focused() == id
	components.PaintInput(canvas, theme, content, components.InputSnapshot{
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
	hint := m.PasswordHint
	if len(state.passwordQuestions) > 1 {
		// One hint under every field would run into the label below it; the
		// labels and the buttons are enough to explain a multi-field round.
		hint = ""
	}
	for index, bounds := range fields {
		question := state.passwordQuestions[index]
		label := strings.TrimSpace(question.Text)
		if label == "" {
			label = m.PasswordLabel
		}
		fieldHint := hint
		if state.secrets.canSave {
			fieldHint = "" // the save box carries it as a help icon
		}
		field := components.BeginField(canvas, theme, bounds)
		content := field.Paint(label, fieldHint, "")
		value := state.passwordFields[index].String()
		id := hitPasswordFields.At(index + 1)
		focused := canvas.Focused() == id
		components.PaintInput(canvas, theme, content, components.InputSnapshot{
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
		// One field is 62 high plus its 4px bottom margin. The error line, when
		// present, and the save box follow directly; no large fixed gap is
		// reserved for an error that may never appear.
		height := float32(66)
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
	components.PaintInput(canvas, theme, content, components.InputSnapshot{
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
