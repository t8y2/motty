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
	verticalTabsLayer  = uint16(200)
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
	autoHide    bool
	grouping    bool
	grouped     verticalTabGrouping
	over        bool
	barDrag     bool
	barGrab     float32
	barGeometry components.ScrollAreaGeometry
	bounds      gfx.Rect
	slot        ui.Box
	view        ui.View
	tabs        []barTab
}

func (state *State) settingsTabOrientationOptions() [2]components.SegmentOption {
	return [...]components.SegmentOption{
		{ID: hitSettingsTabsHorizontal, Label: state.messages.SettingsTabsHorizontal},
		{ID: hitSettingsTabsVertical, Label: state.messages.SettingsTabsVertical},
	}
}

func (state *State) setTabOrientation(orientation tabOrientation) bool {
	if state.tabOrientation == orientation {
		return false
	}
	state.tabOrientation = orientation
	state.tabStrip = tabStrip{vertical: orientation == tabsVertical}
	state.chrome.geometry.valid = false
	state.verticalTabs.over, state.verticalTabs.barDrag = false, false
	state.fitTerminals()
	state.fitSFTP(state.viewport)
	return true
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

func (state *State) paintVerticalTabs(canvas *gfx.Canvas, size gfx.Size) {
	if state.tabOrientation != tabsVertical {
		state.verticalTabs.bounds = gfx.Rect{}
		return
	}
	view, pal, m := &state.verticalTabs, state.pal, state.messages
	terminal := state.terminalPage()
	fileManager := state.activeSFTPWindow() != nil
	sideRail := terminal || fileManager
	var bounds gfx.Rect
	compact := false
	if sideRail {
		width := state.verticalTabsReserve(size)
		compact = view.autoHide && !view.over && !state.tabMenu.open && !state.tabDrag.pressed && !verticalTabsKeyboardFocused(canvas)
		if view.autoHide && !compact {
			width = min(verticalTabsWidth, size.Width*0.6)
		}
		bounds = gfx.Rect{Y: windowBarHeight, Width: width, Height: max(size.Height-windowBarHeight, 0)}
		canvas.PushLayer(verticalTabsLayer)
		defer canvas.PopLayer()
	} else if len(state.workspaceTabs) > 0 {
		bounds = view.slot.Rect()
	}
	if bounds.Width <= 0 || bounds.Height <= 0 {
		view.bounds = gfx.Rect{}
		return
	}
	view.bounds = bounds
	canvas.Clip(bounds)
	defer canvas.Unclip()
	canvas.Fill(bounds, pal.shell)
	canvas.HitWithOptions(hitVerticalTabs, bounds, m.TrayTitle, gfx.HitOptions{Role: gfx.RoleGroup, SkipFocus: true})
	if sideRail {
		canvas.Fill(gfx.Rect{X: bounds.X + bounds.Width - 1, Y: bounds.Y, Width: 1, Height: bounds.Height}, pal.line)
	} else {
		canvas.Fill(gfx.Rect{X: bounds.X, Y: bounds.Y, Width: bounds.Width, Height: 1}, pal.line)
	}
	if !compact {
		canvas.Label(m.TrayTitle, gfx.Rect{X: bounds.X + 8, Y: bounds.Y, Width: max(bounds.Width-48, 0), Height: verticalTabsHeader}, gfx.TextStyle{Size: metrics.FontSizeS, Color: pal.muted, Weight: gfx.WeightSemibold})
	}
	if terminal {
		hint, icon := m.VerticalTabsAutoHide, uint8(iconChevronLeft)
		if view.autoHide {
			hint, icon = m.VerticalTabsPin, iconColumns2
		}
		state.paintIconButton(canvas, hitVerticalTabsPin, icon, gfx.Rect{X: bounds.X + bounds.Width - 36, Y: bounds.Y + 6, Width: 28, Height: 28}, hint, false, iconButtonQuiet, pal.shell)
	}
	// The right inset is 2px wider than the left one: the hover scrollbar lives
	// in that gutter, so the rows end just before it instead of under it.
	list := gfx.Rect{X: bounds.X + 6, Y: bounds.Y + verticalTabsHeader, Width: max(bounds.Width-14, 0), Height: max(bounds.Height-verticalTabsHeader-verticalTabsFooter, 0)}
	active := state.deck.ActiveWorkspace()
	pitch := verticalTabHeight + barGap
	rows := state.workspaceTabs
	if view.grouping {
		state.rebuildVerticalTabGroups(compact)
		rows = view.grouped.layout
	}
	content := max(float32(len(rows))*pitch-barGap, 0)
	scroll := state.tabStrip.layout(list, content+barGap, rows, pitch, verticalTabHeight, active)
	// The bar floats in the shell margin beside the rows instead of over them:
	// it appears while the pointer is over the sidebar (or a drag is in
	// progress), hands the tabs the width the always-on bar used to take, and
	// never covers a row's border or its close button.
	barBounds := gfx.Rect{X: list.X, Y: list.Y, Width: max(bounds.Width-7, 0), Height: list.Height}
	area := components.BeginScrollArea(canvas, state.Theme(), barBounds, gfx.Size{Width: barBounds.Width, Height: content}).
		Offset(0, scroll).
		VerticalScrollbar(hitVerticalTabsBar, m.VerticalTabsScrollbar, "").
		HoverScrollbars(view.over || view.barDrag)
	geometry := area.BeginContent()
	view.barGeometry = geometry
	tabWidth := list.Width
	view.tabs = view.tabs[:0]
	f := view.view.Begin(canvas, state.Theme(), geometry.Content)
	f.Column().Fill().Children(func() {
		if view.grouping {
			state.declareGroupedVerticalTabs(f, tabWidth, compact)
			return
		}
		for index, workspace := range state.workspaceTabs {
			view.tabs = append(view.tabs, state.declareWorkspaceTab(f, workspace, tabWidth, verticalTabHeight, 0, true, compact, index+1))
		}
	})
	f.End()
	state.paintWorkspaceTabParts(canvas, list, view.tabs)
	if view.grouping {
		state.paintVerticalTabGroupIcons(canvas)
	}
	area.EndContent()
	button := gfx.Rect{X: bounds.X + 6, Y: bounds.Y + bounds.Height - verticalTabsFooter + 6, Width: max(bounds.Width-12, 0), Height: 30}
	if compact {
		state.paintIconButton(canvas, hitVerticalTabsNew, iconPlus, button, m.LabelNewTab, false, iconButtonQuiet, pal.shell)
	} else {
		components.BeginButton(canvas, state.Theme(), hitVerticalTabsNew, button).Secondary(m.LabelNewTab)
	}
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
