package workbench

import (
	"github.com/vivaui/viva/flex"
	"github.com/vivaui/viva/ui"
	layoutcore "motty/internal/workspace"
)

// Dragging a tab. Pressed on a tab and moved past a few pixels, a tab follows
// the pointer: over the tab strip it can be dropped between two tabs to move it;
// over the terminals of the tab on show it can be dropped on one side of a pane,
// which docks all of the dragged tab's panes beside that pane and removes the
// tab. A press that does not travel is the click it always was, and Escape
// cancels a drag. The drag changes nothing until it is dropped.

const (
	tabDragThreshold = float32(6)
	tabDragLayer     = uint16(700)
	// dockZone is how far in from a pane's edge a pointer still chooses that edge.
	dockZone = float32(0.30)
)

type dockEdge uint8

const (
	dockNone dockEdge = iota
	dockLeft
	dockRight
	dockTop
	dockBottom
)

func (edge dockEdge) axis() layoutcore.Axis {
	if edge == dockTop || edge == dockBottom {
		return layoutcore.Vertical
	}
	return layoutcore.Horizontal
}

func (edge dockEdge) placement() layoutcore.Placement {
	if edge == dockLeft || edge == dockTop {
		return layoutcore.Before
	}
	return layoutcore.After
}

// tabDrop is where a drag would land if it were dropped now.
type tabDrop struct {
	kind tabDropKind
	// boundary is the gap among the tabs, counted from the first, a reorder drop
	// goes into.
	boundary int
	pane     layoutcore.PaneID
	edge     dockEdge
	// zone is what to highlight: the strip's insertion bar or the half of the
	// pane a dock would take.
	zone ui.Rect
}

type tabDropKind uint8

const (
	dropNone tabDropKind = iota
	dropReorder
	dropDock
)

type tabDrag struct {
	pressed   bool
	active    bool
	workspace layoutcore.WorkspaceID
	title     string
	start, at ui.Point
	drop      tabDrop
	// panes is how many panes the dragged tab brings.
	panes int
	// peer is the other window the pointer is over, if any; elsewhere outside
	// every window is a tear-off.
	peer *State
	// outside says the pointer is out of this window; screen is where it was
	// let go, for a tear-off.
	outside bool
	screen  ui.Point
}

// tabDragPointer follows a press on a tab. It leaves the press alone until the
// pointer has moved, so a click still activates the tab.
func (state *State) tabDragPointer(event ui.PointerEvent) (ui.EventResult, bool) {
	drag := &state.tabDrag
	switch event.Kind {
	case ui.PointerDown:
		*drag = tabDrag{}
		if state.modal == modalNone && (event.Button == ui.PointerButtonPrimary || event.Button == ui.PointerButtonNone) &&
			event.Target.Scope() == hitTabList {
			id := layoutcore.WorkspaceID(item(event.Target))
			for _, tab := range state.workspaceTabs {
				if tab.ID == id {
					*drag = tabDrag{pressed: true, workspace: id, title: tab.Title, panes: tab.PaneCount, start: ui.Point{X: event.X, Y: event.Y}}
				}
			}
		}
		return ui.EventResult{}, false
	case ui.PointerMove:
		if !drag.pressed {
			return ui.EventResult{}, false
		}
		at := ui.Point{X: event.X, Y: event.Y}
		if !drag.active {
			dx, dy := at.X-drag.start.X, at.Y-drag.start.Y
			if dx*dx+dy*dy < tabDragThreshold*tabDragThreshold {
				return ui.EventResult{}, false
			}
			drag.active = true
		}
		drag.at = at
		state.trackTabDrag(drag, event)
		return ui.EventResult{Handled: true, Repaint: true}, true
	case ui.PointerUp:
		if !drag.pressed {
			return ui.EventResult{}, false
		}
		finished := *drag
		*drag = tabDrag{}
		state.clearForeign(finished.peer)
		if !finished.active {
			return ui.EventResult{}, false
		}
		state.trackTabDragEnd(&finished, event)
		state.dropTab(finished)
		return ui.EventResult{Handled: true, Repaint: true}, true
	}
	return ui.EventResult{}, false
}

// trackTabDrag follows the pointer to a new place: it works out the drop in
// this window, or hands the drag to the window it is over, or notes that it has
// left every window.
func (state *State) trackTabDrag(drag *tabDrag, event ui.PointerEvent) {
	drag.outside = event.X < 0 || event.Y < 0 || event.X >= state.viewport.Width || event.Y >= state.viewport.Height
	var peer *State
	if drag.outside && state.options.Group != nil {
		peer = state.options.Group.windowAt(state, ui.Point{X: event.ScreenX, Y: event.ScreenY})
	}
	if peer != drag.peer {
		state.clearForeign(drag.peer)
		drag.peer = peer
	}
	switch {
	case peer != nil:
		drag.drop = tabDrop{}
		peer.showForeignDrag(drag, ui.Point{X: event.ScreenX, Y: event.ScreenY})
	case drag.outside:
		drag.drop = tabDrop{}
	default:
		drag.drop = state.tabDropAt(drag.at, drag.workspace, drag.panes)
	}
}

// trackTabDragEnd settles the drop at the point of the release.
func (state *State) trackTabDragEnd(drag *tabDrag, event ui.PointerEvent) {
	drag.at = ui.Point{X: event.X, Y: event.Y}
	drag.outside = event.X < 0 || event.Y < 0 || event.X >= state.viewport.Width || event.Y >= state.viewport.Height
	drag.peer = nil
	switch {
	case drag.outside && state.options.Group != nil:
		drag.peer = state.options.Group.windowAt(state, ui.Point{X: event.ScreenX, Y: event.ScreenY})
		if drag.peer != nil {
			drag.drop = drag.peer.foreignDropAt(ui.Point{X: event.ScreenX, Y: event.ScreenY}, drag.panes)
		}
		drag.screen = ui.Point{X: event.ScreenX, Y: event.ScreenY}
	case drag.outside:
		drag.screen = ui.Point{X: event.ScreenX, Y: event.ScreenY}
	default:
		drag.drop = state.tabDropAt(drag.at, drag.workspace, drag.panes)
	}
}

// showForeignDrag marks this window as the one another window's tab is over.
func (state *State) showForeignDrag(from *tabDrag, screen ui.Point) {
	state.foreign = tabDrag{active: true, title: from.title, panes: from.panes, outside: true}
	state.foreign.drop = state.foreignDropAt(screen, from.panes)
	if frame, ok := state.frame(); ok {
		state.foreign.at = ui.Point{X: screen.X - frame.X, Y: screen.Y - frame.Y}
	}
	if state.options.Invalidate != nil {
		state.options.Invalidate()
	}
}

func (state *State) clearForeign(peer *State) {
	if peer != nil && peer.foreign.active {
		peer.foreign = tabDrag{}
		if peer.options.Invalidate != nil {
			peer.options.Invalidate()
		}
	}
}

// foreignDropAt is where a tab of panes panes would land here if dropped at the
// screen point: docked beside a pane, among the tabs, or at the end of them.
func (state *State) foreignDropAt(screen ui.Point, panes int) tabDrop {
	frame, ok := state.frame()
	if !ok {
		return tabDrop{}
	}
	at := ui.Point{X: screen.X - frame.X, Y: screen.Y - frame.Y}
	if drop := state.tabDropAt(at, 0, panes); drop.kind != dropNone {
		return drop
	}
	count := len(state.workspaceTabs)
	x := state.tabStrip.origin - state.tabStrip.offset + float32(count)*state.tabStrip.pitch - barGap/2
	return tabDrop{kind: dropReorder, boundary: count, zone: state.tabInsertion(x)}
}

// tabInsertion is the bar that marks a gap among the tabs, centered on x and as
// tall as a tab.
func (state *State) tabInsertion(x float32) ui.Rect {
	bar := state.chrome.geometry.row
	bar.X, bar.Width, bar.Height = x-1, 2, barTabHeight
	return bar
}

// tabDropAt works out where a drop at window point at would land. source is the
// tab being dragged within this window, or zero for one that comes from another
// window with panes panes.
func (state *State) tabDropAt(at ui.Point, source layoutcore.WorkspaceID, panes int) tabDrop {
	strip := &state.tabStrip
	if at.Y >= 0 && at.Y < windowBarHeight && at.X >= strip.area.X && at.X <= strip.area.X+strip.area.Width && strip.pitch > 0 {
		count := len(state.workspaceTabs)
		boundary := min(max(int((at.X-strip.origin+strip.offset)/strip.pitch+0.5), 0), count)
		position := -1
		for index, tab := range state.workspaceTabs {
			if tab.ID == source {
				position = index
			}
		}
		if source != 0 && (position < 0 || boundary == position || boundary == position+1) {
			return tabDrop{}
		}
		x := strip.origin - strip.offset + float32(boundary)*strip.pitch - barGap/2
		return tabDrop{kind: dropReorder, boundary: boundary, zone: state.tabInsertion(x)}
	}
	workspaceID := state.deck.ActiveWorkspace()
	if state.activeSection != sectionTerminal || workspaceID == 0 || workspaceID == source {
		return tabDrop{}
	}
	if source != 0 && !state.deck.CanDock(workspaceID, source) ||
		source == 0 && len(state.deck.Panes(workspaceID, nil))+panes > layoutcore.MaxPanes {
		return tabDrop{}
	}
	state.refreshWorkspaceViews()
	frame := state.workspacePixelBounds(state.viewport)
	for index := range state.panes {
		bounds := pixelBounds(frame, state.panes[index].Bounds)
		if !pointInRect(at.X, at.Y, bounds) {
			continue
		}
		edge, zone := dockZoneAt(at, bounds)
		if edge == dockNone {
			return tabDrop{}
		}
		return tabDrop{kind: dropDock, pane: state.panes[index].ID, edge: edge, zone: zone}
	}
	return tabDrop{}
}

// dockZoneAt picks the pane edge nearest to at, if at is within dockZone of it,
// and the half of bounds on that side.
func dockZoneAt(at ui.Point, bounds ui.Rect) (dockEdge, ui.Rect) {
	if bounds.Width <= 0 || bounds.Height <= 0 {
		return dockNone, ui.Rect{}
	}
	fx, fy := (at.X-bounds.X)/bounds.Width, (at.Y-bounds.Y)/bounds.Height
	best, nearest := dockNone, dockZone
	for _, candidate := range [...]struct {
		edge     dockEdge
		distance float32
	}{{dockLeft, fx}, {dockRight, 1 - fx}, {dockTop, fy}, {dockBottom, 1 - fy}} {
		if candidate.distance < nearest {
			best, nearest = candidate.edge, candidate.distance
		}
	}
	half := ui.Rect{X: bounds.X, Y: bounds.Y, Width: bounds.Width / 2, Height: bounds.Height / 2}
	switch best {
	case dockLeft:
		half.Height = bounds.Height
	case dockRight:
		half.X, half.Height = bounds.X+bounds.Width/2, bounds.Height
	case dockTop:
		half.Width = bounds.Width
	case dockBottom:
		half.Y, half.Width = bounds.Y+bounds.Height/2, bounds.Width
	default:
		return dockNone, ui.Rect{}
	}
	return best, half
}

// dropTab carries out a drop.
func (state *State) dropTab(drag tabDrag) {
	if drag.outside {
		state.moveTabOut(drag)
		return
	}
	switch drag.drop.kind {
	case dropReorder:
		position := -1
		for index, tab := range state.workspaceTabs {
			if tab.ID == drag.workspace {
				position = index
			}
		}
		if position < 0 {
			return
		}
		final := drag.drop.boundary
		if final > position {
			final--
		}
		if state.deck.MoveWorkspace(drag.workspace, final) == nil {
			state.rebuildTabOptions()
			state.activateWorkspace(drag.workspace)
			state.save()
		}
	case dropDock:
		state.dockWorkspace(drag.workspace, drag.drop.pane, drag.drop.edge)
	}
}

// moveTabOut carries a tab dropped outside this window to the window under it,
// or to a window of its own where nothing is.
func (state *State) moveTabOut(drag tabDrag) {
	if drag.peer != nil {
		payload, ok := state.detachWorkspace(drag.workspace)
		if !ok {
			return
		}
		if drag.peer.attachPayload(payload, drag.drop) {
			if drag.peer.options.Raise != nil {
				drag.peer.options.Raise()
			}
			state.closeIfEmptied()
			return
		}
		state.attachPayload(payload, tabDrop{kind: dropReorder, boundary: len(state.workspaceTabs)})
		return
	}
	if state.options.TearOff == nil || state.options.Group == nil {
		return
	}
	payload, ok := state.detachWorkspace(drag.workspace)
	if !ok {
		return
	}
	// The new window's top-left puts its tab strip under the pointer.
	origin := ui.Point{X: drag.screen.X - 120, Y: drag.screen.Y - windowBarHeight/2}
	err := state.options.TearOff(origin, func(window *State) {
		window.attachPayload(payload, tabDrop{kind: dropReorder})
	})
	if err != nil {
		state.attachPayload(payload, tabDrop{kind: dropReorder, boundary: len(state.workspaceTabs)})
		state.status = err.Error()
		return
	}
	state.closeIfEmptied()
}

// closeIfEmptied closes a window whose last tab was carried away, as a browser
// does, so that moving a tab never leaves an empty window behind.
func (state *State) closeIfEmptied() {
	if len(state.deck.Workspaces(nil)) == 0 && len(state.sessions) == 0 && state.options.CloseWindow != nil {
		state.options.CloseWindow()
	}
}

// dockWorkspace moves every pane of the src tab beside a pane of the tab on
// show, on the side edge names, and removes the src tab.
func (state *State) dockWorkspace(src layoutcore.WorkspaceID, target layoutcore.PaneID, edge dockEdge) bool {
	dst := state.deck.ActiveWorkspace()
	if dst == 0 || edge == dockNone {
		return false
	}
	if _, err := state.deck.Dock(dst, src, target, edge.axis(), edge.placement()); err != nil {
		state.status = err.Error()
		return false
	}
	state.syncActiveSession()
	state.activeSection = sectionTerminal
	state.rebuildTabOptions()
	state.refreshWorkspaceViews()
	state.fitWorkspaceTerminals(dst)
	state.fitSFTP(state.viewport)
	state.save()
	return true
}

// paintTabDrag draws what a drag is doing: where it would land, and the tab
// under the pointer. It declares no hit, so nothing under it stops reacting.
func (state *State) paintTabDrag(canvas *ui.Canvas, viewport ui.Rect) {
	drag := &state.tabDrag
	if !drag.active || drag.outside {
		// A drag that left this window is painted by the window it is over.
		drag = &state.foreign
	}
	if !drag.active {
		return
	}
	pal := state.pal
	canvas.PushLayer(tabDragLayer)
	defer canvas.PopLayer()
	switch drag.drop.kind {
	case dropReorder:
		canvas.Rounded(drag.drop.zone, 1, pal.accent)
	case dropDock:
		// A translucent wash with four thin edges: RoundedBorder paints its border
		// colour as a solid rectangle under the fill, which would hide the
		// terminal this marks.
		f := state.chrome.widget.Begin(canvas, state.Theme(), drag.drop.zone)
		f.Stack().Fill().Children(func() {
			f.Row().Fill().Background(withAlpha(pal.accent, 0.18))
			f.Column().Fill().Justify(flex.Between).Children(func() {
				f.Row().Height(2).Background(pal.accent)
				f.Row().Height(2).Background(pal.accent)
			})
			f.Row().Fill().Justify(flex.Between).Children(func() {
				f.Column().Width(2).Background(pal.accent)
				f.Column().Width(2).Background(pal.accent)
			})
		})
		f.End()
	}
	// The tab under the pointer follows it, kept inside the window.
	width := min(barTabMax, max(canvas.TextWidthWeight(drag.title, metrics.FontSizeS, ui.WeightMedium)+28, barTabMin))
	ghost := state.popupAt(canvas, viewport, ui.Point{X: drag.at.X - width/2, Y: drag.at.Y - barTabHeight/2}, width, barTabHeight, 0)
	f := state.chrome.widget.Begin(canvas, state.Theme(), ghost)
	f.Row().Fill().PadX(8).Items(flex.Center).Justify(flex.Center).Radius(radius.item).Border(1, pal.accent).Background(pal.tabActive.Over(pal.shell)).Children(func() {
		f.Text(drag.title).FontSize(metrics.FontSizeS).Weight(ui.WeightMedium).Color(pal.text).LineHeight(20).Ellipsis().TextCenter()
	})
	f.End()
}
