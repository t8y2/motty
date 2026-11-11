package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

var (
	hitSettingsTabsHorizontal       = tree.ID(settingsHits, "tabs-horizontal")
	hitSettingsTabsVertical         = tree.ID(settingsHits, "tabs-vertical")
	hitSettingsVerticalTabsAutoHide = tree.ID(settingsHits, "vertical-tabs-auto-hide")
	hitSettingsVerticalTabsGrouping = tree.ID(settingsHits, "vertical-tabs-grouping")
	hitVerticalTabs                 = tree.ID(shellHits, "vertical-tabs")
	hitVerticalTabsBar              = tree.ID(shellHits, "vertical-tabs-scrollbar")
	hitVerticalTabsPin              = tree.ID(shellHits, "vertical-tabs-pin")
	hitVerticalTabsNew              = tree.ID(shellHits, "vertical-tabs-new")
	hitVerticalTabGroups            = tree.Sub(shellHits, tree.Name("vertical-tab-group"))
)

const (
	verticalTabsWidth  = navigationWidth
	verticalTabsRail   = float32(48)
	verticalTabHeight  = float32(34)
	verticalTabsHeader = float32(40)
	verticalTabsFooter = float32(42)
	// verticalTabsNavInset is the room the sidebar list keeps from each side of
	// the navigation rail when it is declared inside it.
	verticalTabsNavInset = float32(9)
)

type tabOrientation uint8

const (
	tabsHorizontal tabOrientation = iota
	tabsVertical
)

func (orientation tabOrientation) String() string {
	if orientation == tabsVertical {
		return "vertical"
	}
	return "horizontal"
}

func tabOrientationFromString(value string) tabOrientation {
	if value == "vertical" {
		return tabsVertical
	}
	return tabsHorizontal
}

type verticalTabs struct {
	autoHide bool
	grouping bool
	grouped  verticalTabGrouping
	over     bool
	barDrag  bool
	barGrab  float32
	// barGeometry and bounds are what the last paint laid out: the scrollbar the
	// pointer drags, and the sidebar the wheel and the file tab's rail test.
	barGeometry components.ScrollAreaGeometry
	bounds      gfx.Rect
	// slot is the navigation rail's list declaration, kept so Paint can read its
	// settled rectangle for the next paint's page-coordinate geometry. slotRect is
	// that rectangle from the previous paint.
	slot     ui.Box
	slotRect gfx.Rect
	// pageCompact, pageTerminal and pageSideRail are what the declared rows and
	// chrome read for the paint in progress; a Draw callback takes no arguments.
	// scroll and content are the list's offset and extent.
	pageCompact  bool
	pageTerminal bool
	pageSideRail bool
	scroll       float32
	content      float32
	// The Draw callbacks are method values kept once, so a warm paint allocates
	// none.
	headerDraw    func(*ui.DrawContext)
	footerDraw    func(*ui.DrawContext)
	partsDraw     func(*ui.DrawContext)
	scrollbarDraw func(*ui.DrawContext)
	separatorDraw func(*ui.DrawContext)
	tabs          []barTab
}

func (state *State) setTabOrientation(orientation tabOrientation) bool {
	if state.tabOrientation == orientation {
		return false
	}
	state.tabOrientation = orientation
	state.applyTabOrientation()
	return true
}

// applyTabOrientation lays the tabs out for the orientation the page's choice
// already wrote.
func (state *State) applyTabOrientation() {
	state.tabStrip = tabStrip{vertical: state.tabOrientation == tabsVertical}
	state.chrome.geometry.valid = false
	state.verticalTabs.over, state.verticalTabs.barDrag = false, false
	state.windowBarScroll = windowBarScroll{}
	state.fitTerminals()
	state.fitSFTP(state.viewport)
}

func (state *State) setVerticalTabsAutoHide(autoHide bool) bool {
	if state.verticalTabs.autoHide == autoHide {
		return false
	}
	state.verticalTabs.autoHide = autoHide
	state.verticalTabs.over = false
	state.fitTerminals()
	state.fitSFTP(state.viewport)
	return true
}

// Hover expansion overlays the workspace; it must never resize a running PTY.
func (state *State) verticalTabsReserve(size gfx.Size) float32 {
	if state.deck.ActiveWorkspace() == 0 {
		return 0
	}
	return state.verticalTabsColumn(size)
}

// verticalTabsColumn is the width the sidebar takes from the window's own
// content, whether or not a session is open: a page without a deck still keeps
// it beside the workspace. The window bar centres the session's name over what
// is left of the bar, so it needs the column even before a session exists.
func (state *State) verticalTabsColumn(size gfx.Size) float32 {
	if state.tabOrientation != tabsVertical {
		return 0
	}
	width := verticalTabsWidth
	if state.verticalTabs.autoHide {
		width = verticalTabsRail
	}
	return min(width, size.Width*0.4)
}

// trackVerticalTabs records whether the pointer is over the sidebar. The
// sidebar expands under it when auto-hide is on, and the tab list reveals its
// hover scrollbar; both only need the pointer inside the bounds, not a hit.
func (state *State) trackVerticalTabs(event gfx.PointerEvent) bool {
	view := &state.verticalTabs
	if state.tabOrientation != tabsVertical ||
		state.modal != modalNone || state.tabMenu.open || state.tabDrag.pressed || event.Captured {
		return false
	}
	over := view.over
	switch event.Kind {
	case gfx.PointerDown, gfx.PointerMove:
		over = pointInRect(event.X, event.Y, view.bounds)
	case gfx.PointerExit:
		over = false
	}
	changed := view.over != over
	view.over = over
	return changed
}

// verticalTabsScrollPointer drags the sidebar's hover scrollbar. The bar paints
// only while the pointer is over the sidebar, so a press on it pins the reveal
// until the drag ends: the list must not lose its bar under the pointer.
func (state *State) verticalTabsScrollPointer(event gfx.PointerEvent) (gfx.EventResult, bool) {
	view := &state.verticalTabs
	if state.tabOrientation != tabsVertical || (event.Target != hitVerticalTabsBar && !view.barDrag) {
		return gfx.EventResult{}, false
	}
	if event.Kind == gfx.PointerDown && event.Button != gfx.PointerButtonPrimary {
		return gfx.EventResult{}, false
	}
	switch event.Kind {
	case gfx.PointerDown:
		thumb := view.barGeometry.VerticalThumb
		if thumb.Height <= 0 {
			return gfx.EventResult{}, false
		}
		view.barDrag = true
		view.barGrab = thumb.Height / 2
		if pointInRect(event.X, event.Y, thumb) {
			view.barGrab = event.Y - thumb.Y
		}
	case gfx.PointerMove:
		if !view.barDrag {
			return gfx.EventResult{}, false
		}
		state.tabStrip.offset = view.barGeometry.VerticalOffsetForThumb(event.Y - view.barGrab)
	case gfx.PointerUp:
		view.barDrag = false
	default:
		return gfx.EventResult{}, false
	}
	return gfx.EventResult{Handled: true, Repaint: true}, true
}

// fileTabVerticalRailAt reports whether a window point belongs to the vertical
// rail painted over a file-tab page. When auto-hide expands the rail, most of
// its width overlaps the page's fixed content origin; input must stop at the
// rail rather than reaching the file manager beneath it.
func (state *State) fileTabVerticalRailAt(x, y float32) bool {
	return state.tabOrientation == tabsVertical && state.activeSFTPWindow() != nil &&
		pointInRect(x, y, state.verticalTabs.bounds)
}

// verticalTabsKeyboardFocused says the keyboard has moved into the sidebar. A
// rail of icons is no place to arrive at with Tab, so the sidebar opens while the
// focus ring is on one of its controls, as it does under the pointer. Only a
// visible (keyboard) focus counts: a click leaves focus on the tab and must not
// pin the sidebar open.
func verticalTabsKeyboardFocused(canvas *gfx.Canvas) bool {
	focus := canvas.FocusVisible()
	return focus.Scope() == hitTabList || focus.Scope() == hitVerticalTabGroups || focus == hitVerticalTabsPin || focus == hitVerticalTabsNew
}

// declareVerticalTabsOverlay declares the sidebar as a page overlay over the
// workspace. Its first element is a full-bounds group: it is the accessible
// region of the sidebar and the press target of its blank surface, so a press
// there stays out of the terminal or file page beneath. The rows and chrome
// declared after it take their own presses and their own Tab places; the group
// takes no Tab stop. The overlay paints on LayerPaneChrome, under every menu and
// dialog Viva or the window paints. Wheel routing stays the window's own: a
// press target stops presses only (see State.Scroll).
func (state *State) declareVerticalTabsOverlay(c ui.C) {
	if state.tabOrientation != tabsVertical {
		return
	}
	terminal := state.terminalPage()
	fileManager := state.activeSFTPWindow() != nil
	if !terminal && !fileManager {
		return
	}
	view := &state.verticalTabs
	size := gfx.Size{Width: c.Bounds().Width, Height: c.Bounds().Height}
	width := state.verticalTabsReserve(size)
	compact := view.autoHide && !view.over && !state.tabMenu.open && !state.tabDrag.pressed && !verticalTabsKeyboardFocused(c.Canvas())
	if view.autoHide && !compact {
		width = min(verticalTabsWidth, size.Width*0.6)
	}
	if width <= 0 {
		view.bounds = gfx.Rect{}
		return
	}
	bounds := gfx.Rect{Y: windowBarHeight, Width: width, Height: max(size.Height-windowBarHeight, 0)}
	view.bounds, view.pageTerminal, view.pageSideRail = bounds, terminal, true
	view.ensureDraws(state)
	ui.Overlay(c, ui.OverlaySpec{Band: ui.LayerPaneChrome, Bounds: bounds}).
		Background(state.pal.shell).Clip().Children(func() {
		ui.Stack(c).Fill().Children(func() {
			// The group is declared first, so the rows and chrome after it win
			// their own presses; SkipFocus keeps the rail out of Tab.
			ui.Slot(c).Fill().HitWith(hitVerticalTabs, state.messages.TrayTitle,
				gfx.HitOptions{Role: gfx.RoleGroup, SkipFocus: true})
			state.declareVerticalTabsContent(c, bounds.Width, bounds.Height, gfx.Point{X: bounds.X, Y: bounds.Y}, compact)
		})
	})
}

// declareVerticalTabsNav declares the sidebar in the navigation rail, where it
// needs no overlay: the section page is painted after the rail and starts to its
// right, so nothing covers it and no press can slip through to the workspace.
// The rows are still page elements, and their page-coordinate geometry is read
// back from the previous paint's slot.
func (state *State) declareVerticalTabsNav(c ui.C) {
	view := &state.verticalTabs
	width := navigationWidth - 2*verticalTabsNavInset
	bounds := view.slotRect
	if bounds.Width <= 0 {
		bounds = gfx.Rect{X: verticalTabsNavInset, Width: width}
	}
	view.bounds, view.pageTerminal, view.pageSideRail = bounds, false, false
	view.ensureDraws(state)
	view.slot = ui.Column(c).Grow(1).Basis(0).MarginSides(8, 9, 8, 9).Clip().Background(state.pal.shell)
	view.slot.Children(func() {
		state.declareVerticalTabsContent(c, width, bounds.Height, gfx.Point{X: bounds.X, Y: bounds.Y}, false)
	})
}

// declareVerticalTabsContent declares the header, the scrolling row list and the
// footer inside c, whose content box is width wide. origin is that box's page
// origin, which the scroll strip keeps for wheel and drag routing; the rows
// themselves are laid out by the page.
func (state *State) declareVerticalTabsContent(c ui.C, width, height float32, origin gfx.Point, compact bool) {
	view := &state.verticalTabs
	list := gfx.Rect{
		X: origin.X + 6, Y: origin.Y + verticalTabsHeader,
		Width: max(width-14, 0), Height: max(height-verticalTabsHeader-verticalTabsFooter, 0),
	}
	active := state.deck.ActiveWorkspace()
	pitch := verticalTabHeight + barGap
	rows := state.workspaceTabs
	if view.grouping {
		state.rebuildVerticalTabGroups(compact)
		rows = view.grouped.layout
	}
	content := max(float32(len(rows))*pitch-barGap, 0)
	// The navigation rail list is declared before the page has laid it out, so
	// its height comes from the previous paint. A first paint after the rail
	// changed has none: give the strip the window's height, which cannot hide a
	// row this paint. The settled rectangle is read back in the separator draw.
	extent := list
	if extent.Height <= 0 {
		extent.Height = max(c.Bounds().Height, 0)
	}
	scroll := state.tabStrip.layout(extent, content+barGap, rows, pitch, verticalTabHeight, active)
	view.pageCompact = compact
	view.scroll, view.content = scroll, content
	view.tabs = view.tabs[:0]
	ui.Column(c).Fill().Clip().Children(func() {
		ui.Slot(c).Height(verticalTabsHeader).Shrink(0).Draw(view.headerDraw)
		ui.Column(c).Grow(1).Basis(0).Shrink(1).MarginSides(0, 8, 0, 6).Clip().Children(func() {
			ui.Column(c).Absolute().Left(0).Top(-scroll).Width(list.Width).Children(func() {
				state.declareVerticalTabsRows(c, list.Width, compact)
			})
		})
		ui.Slot(c).Height(verticalTabsFooter).Shrink(0).Draw(view.footerDraw)
		// Painted after the rows, at the page rectangle they occupy: the row
		// details the window draws, the hover scrollbar and the rail's line.
		ui.Slot(c).Absolute().Left(6).Right(8).Top(verticalTabsHeader).Bottom(verticalTabsFooter).Draw(view.partsDraw)
		ui.Slot(c).Absolute().Left(6).Right(1).Top(verticalTabsHeader).Bottom(verticalTabsFooter).Draw(view.scrollbarDraw)
		ui.Slot(c).Absolute().Inset(0).Draw(view.separatorDraw)
	})
}

// ensureDraws makes the sidebar's Draw method values once. A method value in a
// paint would allocate one per frame.
func (view *verticalTabs) ensureDraws(state *State) {
	if view.headerDraw != nil {
		return
	}
	view.headerDraw = state.drawVerticalTabsHeader
	view.footerDraw = state.drawVerticalTabsFooter
	view.partsDraw = state.drawVerticalTabsParts
	view.scrollbarDraw = state.drawVerticalTabsScrollbar
	view.separatorDraw = state.drawVerticalTabsSeparator
}

// declareVerticalTabsRows declares the sidebar's tab rows in the page. width and
// compact are the list's this paint.
func (state *State) declareVerticalTabsRows(c ui.C, width float32, compact bool) {
	view := &state.verticalTabs
	ui.Column(c).Fill().Children(func() {
		if view.grouping {
			state.declareGroupedVerticalTabs(c, width, compact)
			return
		}
		for index, workspace := range state.workspaceTabs {
			view.tabs = append(view.tabs, state.declareWorkspaceTab(c, workspace, width, verticalTabHeight, 0, true, compact, index+1))
		}
	})
}

// drawVerticalTabsHeader paints the sidebar's name and the auto-hide pin.
func (state *State) drawVerticalTabsHeader(d *ui.DrawContext) {
	view, pal, m := &state.verticalTabs, state.pal, state.messages
	bounds := d.Rect
	if !view.pageCompact {
		d.Canvas.Label(m.TrayTitle, gfx.Rect{X: bounds.X + 8, Y: bounds.Y, Width: max(bounds.Width-48, 0), Height: verticalTabsHeader},
			gfx.TextStyle{Size: metrics.FontSizeS, Color: pal.muted, Weight: gfx.WeightSemibold})
	}
	if view.pageTerminal {
		hint, icon := m.VerticalTabsAutoHide, uint8(iconChevronLeft)
		if view.autoHide {
			hint, icon = m.VerticalTabsPin, iconColumns2
		}
		state.paintIconButton(d.Canvas, hitVerticalTabsPin, icon,
			gfx.Rect{X: bounds.X + bounds.Width - 36, Y: bounds.Y + 6, Width: 28, Height: 28}, hint, false, iconButtonQuiet, pal.shell)
	}
}

// drawVerticalTabsFooter paints the sidebar's new-tab button.
func (state *State) drawVerticalTabsFooter(d *ui.DrawContext) {
	view, m := &state.verticalTabs, state.messages
	bounds := d.Rect
	button := gfx.Rect{X: bounds.X + 6, Y: bounds.Y + 6, Width: max(bounds.Width-12, 0), Height: 30}
	if view.pageCompact {
		state.paintIconButton(d.Canvas, hitVerticalTabsNew, iconPlus, button, m.LabelNewTab, false, iconButtonQuiet, state.pal.shell)
		return
	}
	components.BeginButton(d.Canvas, state.Theme(), hitVerticalTabsNew, button).Secondary(m.LabelNewTab)
}

// drawVerticalTabsParts paints what follows the rows: the path under a tab's
// name, its host badge and light, and a group header's host badge.
func (state *State) drawVerticalTabsParts(d *ui.DrawContext) {
	view := &state.verticalTabs
	state.paintWorkspaceTabParts(d.Canvas, d.Rect, view.tabs)
	if view.grouping {
		state.paintVerticalTabGroupIcons(d.Canvas)
	}
}

// drawVerticalTabsScrollbar paints the hover scrollbar over the row list. The
// window keeps the offset and routes the wheel; this only paints and names the
// bar the pointer drags.
func (state *State) drawVerticalTabsScrollbar(d *ui.DrawContext) {
	view, m := &state.verticalTabs, state.messages
	area := components.BeginScrollArea(d.Canvas, state.Theme(), d.Rect, gfx.Size{Width: d.Rect.Width, Height: view.content}).
		Offset(0, view.scroll).
		VerticalScrollbar(hitVerticalTabsBar, m.VerticalTabsScrollbar, "").
		HoverScrollbars(view.over || view.barDrag)
	view.barGeometry = area.EndContent()
}

// drawVerticalTabsSeparator paints the line that parts the sidebar from the rest
// of the window: the window's right edge for the rail over the workspace, the
// navigation rail's top for the list declared in it.
func (state *State) drawVerticalTabsSeparator(d *ui.DrawContext) {
	view, pal := &state.verticalTabs, state.pal
	bounds := d.Rect
	if !view.pageSideRail {
		// The rail list is laid out now; the declaration had only the previous
		// paint's rectangle. Keep the page-coordinate geometry current for the
		// wheel and the drag, without moving the rows this paint already placed.
		view.bounds = bounds
		list := gfx.Rect{X: bounds.X + 6, Y: bounds.Y + verticalTabsHeader, Width: max(bounds.Width-14, 0), Height: max(bounds.Height-verticalTabsHeader-verticalTabsFooter, 0)}
		state.tabStrip.area, state.tabStrip.origin = list, list.Y
		state.tabStrip.max = max(view.content-list.Height, 0)
	}
	if view.pageSideRail {
		d.Canvas.Fill(gfx.Rect{X: bounds.X + bounds.Width - 1, Y: bounds.Y, Width: 1, Height: bounds.Height}, pal.line)
		return
	}
	d.Canvas.Fill(gfx.Rect{X: bounds.X, Y: bounds.Y, Width: bounds.Width, Height: 1}, pal.line)
}

func (state *State) paintWorkspaceTabParts(canvas *gfx.Canvas, strip gfx.Rect, tabs []barTab) {
	canvas.Clip(strip)
	defer canvas.Unclip()
	for index := range tabs {
		tab := &tabs[index]
		if tab.pathLabel != "" && tab.output == outputAlertNone {
			bounds := tab.title.Rect()
			label := elideTabDirectory(tab.pathLabel, func(value string) bool {
				return canvas.TextWidthWeight(value, metrics.FontSizeS, gfx.WeightMedium) <= bounds.Width
			})
			canvas.Label(label, bounds, gfx.TextStyle{Size: metrics.FontSizeS, Weight: gfx.WeightMedium, Color: tab.foreground})
		}
		var session *Session
		if tab.fileSession != 0 {
			if manager := state.sftpWindows[tab.fileSession]; manager != nil && !manager.closed {
				session = &manager.session
			}
		} else if pane, ok := state.deck.Pane(tab.workspace.ID, tab.workspace.ActivePane); ok && pane.SessionID != 0 {
			session, _ = state.sessionByIDPointer(uint32(pane.SessionID))
		}
		if tab.output != outputAlertNone && !tab.compact {
			state.paintOutputAlertTitle(canvas, tab, strip)
		}
		badge := tab.badge.Rect()
		if session != nil {
			state.paintHostCatalogIcon(canvas, session.host, badge, tab.background)
			kind, dot := state.sessionLight(session)
			var light gfx.Rect
			if tab.compact {
				light = gfx.Rect{X: badge.X + badge.Width - 5, Y: badge.Y + badge.Height - 5, Width: 5, Height: 5}
			} else {
				light = tab.light.Rect()
			}
			paintLight(canvas, light.X+light.Width/2, light.Y+light.Height/2, kind, dot, tab.background)
		} else if state.tabOrientation == tabsVertical {
			canvas.DrawIcon(lucideIcons[iconTerminal], badge, state.pal.muted)
		}
		if tab.workspace.ID != 0 {
			state.openTabMenuOn(canvas, tab.workspace.ID, tab.id, tab.closeID)
		}
	}
}
