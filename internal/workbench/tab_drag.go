package workbench

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	layoutcore "motty/internal/workspace"
)

// Dragging a tab. Pressed on a tab and moved past a few pixels, a tab follows
// the pointer: over the tab strip it can be dropped between two tabs to move it;
// over the terminals of the tab on show it can be dropped on one side of a pane,
// which docks all of the dragged tab's panes beside that pane and removes the
// tab. A press that does not travel is the click it always was, and Escape
// cancels a drag. The drag changes nothing until it is dropped.
//
// The bar of a pane in a split tab can be dragged too, and dropped among the
// tabs: the pane leaves the split and becomes a tab of its own, its session and
// everything on its screen carried along, the reverse of docking a tab.

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
	// dockCenter is the middle of a pane: a drop there swaps two panes of one
	// workspace, and is no dock for a tab.
	dockCenter
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
	// moveUnit says the drop moves the dragged tab, or the whole group of tabs
	// it stands for, to stand before the tab at the deck index boundary, rather
	// than reordering it among tabs of its own group.
	moveUnit bool
	// zone is what to highlight: the strip's insertion bar or the half of the
	// pane a dock would take.
	zone gfx.Rect
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
	// pane is the pane of workspace being pulled out of its split; zero when the
	// drag carries a whole tab.
	pane layoutcore.PaneID
	// group is every tab of a server group dragged by its header; the first is
	// workspace. A group only moves among the groups of this window.
	group []layoutcore.WorkspaceID
	title string
	// host is the connection the dragged tab shows, so its picture can follow
	// the pointer as it does on the tab; hasHost is false for a tab without one.
	host      Host
	hasHost   bool
	start, at gfx.Point
	drop      tabDrop
	// panes is how many panes the dragged tab brings.
	panes int
	// peer is the other window the pointer is over, if any; elsewhere outside
	// every window is a tear-off.
	peer *State
	// outside says the pointer is out of this window; screen is where it was
	// let go, for a tear-off.
	outside bool
	screen  gfx.Point
}

// tabDragPointer follows a press on a tab. It leaves the press alone until the
// pointer has moved, so a click still activates the tab.
func (state *State) tabDragPointer(event gfx.PointerEvent) (gfx.EventResult, bool) {
	drag := &state.tabDrag
	switch event.Kind {
	case gfx.PointerDown:
		state.closeTabPreview()
		*drag = tabDrag{}
		if state.modal == modalNone && (event.Button == gfx.PointerButtonPrimary || event.Button == gfx.PointerButtonNone) &&
			event.Target.Scope() == hitTabList {
			id := layoutcore.WorkspaceID(item(event.Target))
			for _, tab := range state.workspaceTabs {
				if tab.ID == id {
					*drag = tabDrag{pressed: true, workspace: id, title: state.tabLabel(tab), panes: tab.PaneCount, start: gfx.Point{X: event.X, Y: event.Y}}
					if session := state.tabSession(tab); session != nil {
						drag.host, drag.hasHost = session.host, true
					}
				}
			}
		} else if pane, session, ok := state.paneBarAt(event); ok {
			if event.ClickCount == 2 && (session.term == nil || !session.term.find.open) {
				// A double click on the bar names the pane, as it does in a tab
				// strip or a file manager; the context menu is not the only way.
				state.openTerminalRename(session)
				return gfx.EventResult{Handled: true, Repaint: true}, true
			}
			*drag = tabDrag{pressed: true, workspace: state.deck.ActiveWorkspace(), pane: pane, title: session.Title, panes: 1, start: gfx.Point{X: event.X, Y: event.Y}, host: session.host, hasHost: true}
			if drag.title == "" {
				drag.title = session.Address
			}
		} else if state.modal == modalNone && (event.Button == gfx.PointerButtonPrimary || event.Button == gfx.PointerButtonNone) {
			if members, title, ok := state.verticalTabGroupMembers(event.Target); ok {
				*drag = tabDrag{pressed: true, workspace: members[0], group: members, title: title, start: gfx.Point{X: event.X, Y: event.Y}}
			}
		}
		return gfx.EventResult{}, false
	case gfx.PointerMove:
		if !drag.pressed {
			return gfx.EventResult{}, false
		}
		at := gfx.Point{X: event.X, Y: event.Y}
		if !drag.active {
			dx, dy := at.X-drag.start.X, at.Y-drag.start.Y
			if dx*dx+dy*dy < tabDragThreshold*tabDragThreshold {
				return gfx.EventResult{}, false
			}
			drag.active = true
		}
		drag.at = at
		state.trackTabDrag(drag, event)
		return gfx.EventResult{Handled: true, Repaint: true}, true
	case gfx.PointerUp:
		if !drag.pressed {
			return gfx.EventResult{}, false
		}
		finished := *drag
		*drag = tabDrag{}
		state.closeTabPreview()
		state.clearForeign(finished.peer)
		if !finished.active {
			return gfx.EventResult{}, false
		}
		state.trackTabDragEnd(&finished, event)
		state.dropTab(finished)
		return gfx.EventResult{Handled: true, Repaint: true}, true
	}
	return gfx.EventResult{}, false
}

// trackTabDrag follows the pointer to a new place: it works out the drop in
// this window, or hands the drag to the window it is over, or notes that it has
// left every window.
func (state *State) trackTabDrag(drag *tabDrag, event gfx.PointerEvent) {
	if drag.pane != 0 {
		// A pane only ever lands among this window's tabs.
		drag.outside, drag.peer = false, nil
		state.closeTabPreview()
		drag.drop = state.paneDropAt(drag.workspace, drag.pane, drag.at)
		return
	}
	drag.outside = event.X < 0 || event.Y < 0 || event.X >= state.viewport.Width || event.Y >= state.viewport.Height
	if len(drag.group) > 0 {
		drag.outside = false // a group of connections is never torn off or sent to another window
	}
	var peer *State
	if drag.outside && state.options.Group != nil {
		peer = state.options.Group.windowAt(state, gfx.Point{X: event.ScreenX, Y: event.ScreenY})
	}
	if peer != drag.peer {
		state.clearForeign(drag.peer)
		drag.peer = peer
	}
	state.followTabPreview(drag, gfx.Point{X: event.ScreenX, Y: event.ScreenY})
	switch {
	case peer != nil:
		drag.drop = tabDrop{}
		peer.showForeignDrag(drag, gfx.Point{X: event.ScreenX, Y: event.ScreenY})
	case drag.outside:
		drag.drop = tabDrop{}
	default:
		drag.drop = state.tabDropAt(drag.at, drag.workspace, drag.panes, drag.group)
	}
}

// tabPreview is the picture that follows a tab dragged out of every window.
type tabPreview struct {
	handle DragPreview
	// width is the width the tab ghost was last painted at, so the picture is
	// opened as wide as the one the drag has been showing inside the window.
	width float32
	// refused says the platform cannot show one, so the drag does not ask again.
	refused bool
}

// followTabPreview keeps the picture under the pointer while the drag is out of
// every window, and takes it away when the drag is back in one, since a window
// then draws the tab itself.
func (state *State) followTabPreview(drag *tabDrag, screen gfx.Point) {
	if !drag.outside || drag.peer != nil || len(drag.group) > 0 {
		state.closeTabPreview()
		return
	}
	preview := &state.tabPreview
	if state.options.OpenDragPreview == nil || preview.refused {
		return
	}
	width := preview.width
	if width == 0 {
		width = barTabMax
	}
	origin := gfx.Point{X: screen.X - width/2, Y: screen.Y - barTabHeight/2}
	if preview.handle != nil {
		preview.handle.MoveTo(origin)
		return
	}
	title, host, hasHost := drag.title, drag.host, drag.hasHost
	handle, err := state.options.OpenDragPreview(gfx.Size{Width: width, Height: barTabHeight}, origin, func(canvas *gfx.Canvas, size gfx.Size) {
		state.drawTabGhost(canvas, gfx.Rect{Width: size.Width, Height: size.Height}, title, host, hasHost)
	})
	if err != nil || handle == nil {
		preview.refused = true
		return
	}
	preview.handle = handle
}

func (state *State) closeTabPreview() {
	if state.tabPreview.handle != nil {
		state.tabPreview.handle.Close()
		state.tabPreview.handle = nil
	}
}

// trackTabDragEnd settles the drop at the point of the release.
func (state *State) trackTabDragEnd(drag *tabDrag, event gfx.PointerEvent) {
	drag.at = gfx.Point{X: event.X, Y: event.Y}
	if drag.pane != 0 {
		drag.outside, drag.peer = false, nil
		drag.drop = state.paneDropAt(drag.workspace, drag.pane, drag.at)
		return
	}
	drag.outside = event.X < 0 || event.Y < 0 || event.X >= state.viewport.Width || event.Y >= state.viewport.Height
	if len(drag.group) > 0 {
		drag.outside = false
	}
	drag.peer = nil
	switch {
	case drag.outside && state.options.Group != nil:
		drag.peer = state.options.Group.windowAt(state, gfx.Point{X: event.ScreenX, Y: event.ScreenY})
		if drag.peer != nil {
			drag.drop = drag.peer.foreignDropAt(gfx.Point{X: event.ScreenX, Y: event.ScreenY}, drag.panes)
		}
		drag.screen = gfx.Point{X: event.ScreenX, Y: event.ScreenY}
	case drag.outside:
		drag.screen = gfx.Point{X: event.ScreenX, Y: event.ScreenY}
	default:
		drag.drop = state.tabDropAt(drag.at, drag.workspace, drag.panes, drag.group)
	}
}

// showForeignDrag marks this window as the one another window's tab is over.
func (state *State) showForeignDrag(from *tabDrag, screen gfx.Point) {
	state.foreign = tabDrag{active: true, title: from.title, host: from.host, hasHost: from.hasHost, panes: from.panes, outside: true}
	state.foreign.drop = state.foreignDropAt(screen, from.panes)
	if frame, ok := state.frame(); ok {
		state.foreign.at = gfx.Point{X: screen.X - frame.X, Y: screen.Y - frame.Y}
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
func (state *State) foreignDropAt(screen gfx.Point, panes int) tabDrop {
	frame, ok := state.frame()
	if !ok {
		return tabDrop{}
	}
	at := gfx.Point{X: screen.X - frame.X, Y: screen.Y - frame.Y}
	if drop := state.tabDropAt(at, 0, panes, nil); drop.kind != dropNone {
		return drop
	}
	count := len(state.workspaceTabs)
	rows := count
	if state.verticalTabsGrouped() {
		rows = len(state.verticalTabs.grouped.rows)
	}
	x := state.tabStrip.origin - state.tabStrip.offset + float32(rows)*state.tabStrip.pitch - barGap/2
	return tabDrop{kind: dropReorder, boundary: count, zone: state.tabInsertion(x)}
}

// tabInsertion is the bar that marks a gap among the tabs, centered on x and as
// tall as a tab.
func (state *State) tabInsertion(x float32) gfx.Rect {
	if state.tabStrip.vertical {
		area := state.tabStrip.area
		return gfx.Rect{X: area.X, Y: x - 1, Width: area.Width, Height: 2}
	}
	bar := state.chrome.geometry.row
	bar.X, bar.Width, bar.Height = x-1, 2, barTabHeight
	return bar
}

// tabDropArea is where a drop among the tabs is taken. Across the top the tabs
// span the width of the strip but any height of the window bar, so a drag that
// strays a few pixels above or below the row still reorders instead of tearing
// the tab out; in the sidebar it is the whole sidebar, header and footer included.
func (state *State) tabDropArea() gfx.Rect {
	if state.tabStrip.vertical {
		return state.verticalTabs.bounds
	}
	area := state.tabStrip.area
	return gfx.Rect{X: area.X, Width: area.Width, Height: windowBarHeight}
}

// tabDropAt works out where a drop at window point at would land. source is the
// tab being dragged within this window, or zero for one that comes from another
// window with panes panes. A non-empty unit is a whole server group, which can
// only be dropped among the tabs.
func (state *State) tabDropAt(at gfx.Point, source layoutcore.WorkspaceID, panes int, unit []layoutcore.WorkspaceID) tabDrop {
	strip := &state.tabStrip
	if pointInRect(at.X, at.Y, state.tabDropArea()) && strip.pitch > 0 {
		if state.verticalTabsGrouped() {
			return state.groupedVerticalTabDrop(at, source, unit)
		}
		positionOnAxis := at.X
		if strip.vertical {
			positionOnAxis = at.Y
		}
		count := len(state.workspaceTabs)
		boundary := min(max(int((positionOnAxis-strip.origin+strip.offset)/strip.pitch+0.5), 0), count)
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
	if len(unit) > 0 {
		return tabDrop{}
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
func dockZoneAt(at gfx.Point, bounds gfx.Rect) (dockEdge, gfx.Rect) {
	if bounds.Width <= 0 || bounds.Height <= 0 {
		return dockNone, gfx.Rect{}
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
	half := gfx.Rect{X: bounds.X, Y: bounds.Y, Width: bounds.Width / 2, Height: bounds.Height / 2}
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
		return dockNone, gfx.Rect{}
	}
	return best, half
}

// dropTab carries out a drop.
func (state *State) dropTab(drag tabDrag) {
	if drag.pane != 0 {
		switch drag.drop.kind {
		case dropReorder:
			state.undockPane(drag.workspace, drag.pane, drag.drop.boundary)
		case dropDock:
			state.movePaneBeside(drag.workspace, drag.pane, drag.drop)
		}
		return
	}
	if drag.outside {
		state.moveTabOut(drag)
		return
	}
	switch drag.drop.kind {
	case dropReorder:
		if state.verticalTabsGrouped() {
			if drag.drop.moveUnit {
				unit := drag.group
				if len(unit) == 0 {
					unit = []layoutcore.WorkspaceID{drag.workspace}
				}
				state.moveWorkspaceUnit(unit, drag.drop.boundary)
				state.rebuildTabOptions()
				if len(drag.group) == 0 {
					state.activateWorkspace(drag.workspace)
				}
				state.save()
				return
			}
			if state.reorderVerticalTabGroup(drag.workspace, drag.drop.boundary) {
				state.rebuildTabOptions()
				state.activateWorkspace(drag.workspace)
				state.save()
				return
			}
		}
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
	// The new window's top-left puts its tab strip under the pointer.
	state.tearOffTab(drag.workspace, gfx.Point{X: drag.screen.X - 120, Y: drag.screen.Y - windowBarHeight/2})
}

// tearOffCascade is how far a window opened from the menu sits from the one it
// came from, so the two do not lie exactly over each other.
const tearOffCascade = 32

// canTearOffTab says whether a tab can be moved to a window of its own from the
// menu. A window's only tab stays, since moving it would only swap one window
// for another.
func (state *State) canTearOffTab() bool {
	_, placed := state.windowFrameOnScreen()
	return state.options.TearOff != nil && state.options.Group != nil && placed && len(state.workspaceTabs) > 1
}

// windowFrameOnScreen is where this window is, in screen coordinates.
func (state *State) windowFrameOnScreen() (gfx.Rect, bool) {
	if state.frame == nil {
		return gfx.Rect{}, false
	}
	return state.frame()
}

// tearOffTab moves a tab into a window of its own whose top-left is at origin
// (screen coordinates). The sessions go on; if the window cannot be opened the
// tab goes back where it was.
func (state *State) tearOffTab(workspace layoutcore.WorkspaceID, origin gfx.Point) {
	if state.options.TearOff == nil || state.options.Group == nil {
		return
	}
	position, ok := state.workspaceTab(workspace)
	if !ok {
		return
	}
	payload, ok := state.detachWorkspace(workspace)
	if !ok {
		return
	}
	err := state.options.TearOff(origin, func(window *State) {
		window.attachPayload(payload, tabDrop{kind: dropReorder})
	})
	if err != nil {
		state.attachPayload(payload, tabDrop{kind: dropReorder, boundary: position})
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
	sourceIdentity := state.workspaceIdentity(src)
	if _, err := state.deck.Dock(dst, src, target, edge.axis(), edge.placement()); err != nil {
		state.status = err.Error()
		return false
	}
	state.restoreWorkspaceIdentity(src, workspaceIdentity{})
	state.restoreWorkspaceIdentity(dst, mergeWorkspaceIdentities(state.workspaceIdentity(dst), sourceIdentity))
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
func (state *State) paintTabDrag(canvas *gfx.Canvas, viewport gfx.Rect) {
	drag := &state.tabDrag
	if drag.active && drag.outside && drag.peer == nil && len(drag.group) == 0 {
		state.paintTearOff(canvas, viewport, drag)
		return
	}
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
			f.Column().Fill().Justify(ui.Between).Children(func() {
				f.Row().Height(2).Background(pal.accent)
				f.Row().Height(2).Background(pal.accent)
			})
			f.Row().Fill().Justify(ui.Between).Children(func() {
				f.Column().Width(2).Background(pal.accent)
				f.Column().Width(2).Background(pal.accent)
			})
		})
		f.End()
	}
	// The tab under the pointer follows it, kept inside the window.
	state.paintTabGhost(canvas, viewport, drag)
}

// tabGhostWidth is how wide the dragged tab is drawn: the room a tab spends
// around its name (the picture, the close button and the padding between them)
// and the name itself.
func tabGhostWidth(canvas *gfx.Canvas, title string) float32 {
	return min(barTabMax, max(canvas.TextWidthWeight(title, metrics.FontSizeS, gfx.WeightMedium)+62, barTabMin))
}

// paintTabGhost draws the dragged tab as it is in the bar, centred on the
// pointer and kept inside the window, and returns where it stands.
func (state *State) paintTabGhost(canvas *gfx.Canvas, viewport gfx.Rect, drag *tabDrag) gfx.Rect {
	width := tabGhostWidth(canvas, drag.title)
	state.tabPreview.width = width
	bounds := state.popupAt(canvas, viewport, gfx.Point{X: drag.at.X - width/2, Y: drag.at.Y - barTabHeight/2}, width, barTabHeight, 0)
	state.drawTabGhost(canvas, bounds, drag.title, drag.host, drag.hasHost)
	return bounds
}

// drawTabGhost draws a tab in bounds as the active tab of the bar looks.
func (state *State) drawTabGhost(canvas *gfx.Canvas, bounds gfx.Rect, title string, host Host, hasHost bool) {
	pal := state.pal
	fill, edge := pal.tabActive, pal.windowStroke
	if hasHost {
		fill = tintHostSurface(fill, host.Color, .18)
		edge = tintHostSurface(edge, host.Color, .35)
	}
	var badge ui.Box
	f := state.chrome.widget.Begin(canvas, state.Theme(), bounds)
	f.Row().Fill().PadSides(0, 3, 0, 4).Items(ui.Center).Radius(radius.item).Border(1, edge).Background(fill).Children(func() {
		badge = f.Slot().Size(20, 20).MarginSides(0, 7, 0, 0)
		f.Text(title).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(pal.text).LineHeight(20).Ellipsis()
		f.Spacer()
		f.Row().Size(20, 20).Shrink(0).Center().Children(func() {
			f.Icon(lucideIcons[iconClose]).Size(14, 14).Color(pal.text)
		})
	})
	f.End()
	if hasHost {
		state.paintHostCatalogIcon(canvas, host, badge.Rect(), fill)
	} else {
		canvas.DrawIcon(lucideIcons[iconTerminal], badge.Rect(), pal.muted)
	}
}

// paintTearOff tells a drag that has left every window that letting go opens a
// window of its own. The pointer is out of this window, where nothing can be
// drawn, so the window shows it at its edge: the border lights up, and the tab
// and what releasing does wait at the edge nearest the pointer.
func (state *State) paintTearOff(canvas *gfx.Canvas, viewport gfx.Rect, drag *tabDrag) {
	pal := state.pal
	canvas.PushLayer(tabDragLayer)
	defer canvas.PopLayer()
	// The window's corners are round, so the line turns with them; four straight
	// sides would stop short of every corner.
	const width, overlap = tearOffEdge, float32(0.5)
	const r = windowCornerRadius
	x, y, right, bottom := viewport.X, viewport.Y, viewport.X+viewport.Width, viewport.Y+viewport.Height
	line := pal.accent
	canvas.Fill(gfx.Rect{X: x + r - overlap, Y: y, Width: max(viewport.Width-2*r+2*overlap, 0), Height: width}, line)
	canvas.Fill(gfx.Rect{X: x + r - overlap, Y: bottom - width, Width: max(viewport.Width-2*r+2*overlap, 0), Height: width}, line)
	canvas.Fill(gfx.Rect{X: x, Y: y + r - overlap, Width: width, Height: max(viewport.Height-2*r+2*overlap, 0)}, line)
	canvas.Fill(gfx.Rect{X: right - width, Y: y + r - overlap, Width: width, Height: max(viewport.Height-2*r+2*overlap, 0)}, line)
	style := gfx.PathStyle{Stroke: line, StrokeWidth: width}
	canvas.DrawPath(tearOffCorners[0], gfx.Rect{X: x, Y: y, Width: r, Height: r}, style)
	canvas.DrawPath(tearOffCorners[1], gfx.Rect{X: right - r, Y: y, Width: r, Height: r}, style)
	canvas.DrawPath(tearOffCorners[2], gfx.Rect{X: x, Y: bottom - r, Width: r, Height: r}, style)
	canvas.DrawPath(tearOffCorners[3], gfx.Rect{X: right - r, Y: bottom - r, Width: r, Height: r}, style)

	if state.tabPreview.handle != nil {
		return // the picture under the pointer says it
	}
	ghost := state.paintTabGhost(canvas, viewport, drag)

	hint := state.messages.TabTearOffHint
	hintWidth := canvas.TextWidthWeight(hint, metrics.FontSizeS, gfx.WeightMedium) + 24
	below := gfx.Point{X: ghost.X + ghost.Width/2 - hintWidth/2, Y: ghost.Y + ghost.Height + 6}
	if below.Y+barTabHeight > viewport.Y+viewport.Height {
		below.Y = ghost.Y - barTabHeight - 6
	}
	pill := state.popupAt(canvas, viewport, below, hintWidth, barTabHeight, 0)
	f := state.chrome.widget.Begin(canvas, state.Theme(), pill)
	f.Row().Fill().PadX(8).Items(ui.Center).Justify(ui.Center).Radius(radius.item).Background(pal.accent).Children(func() {
		f.Text(hint).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(pal.onAccent).LineHeight(20).Ellipsis().TextCenter()
	})
	f.End()
}

// tearOffEdge is the thickness of the line round a window a tab is dragged out of.
const tearOffEdge = float32(2)

// tearOffCorners are the arcs that carry that line round the window's four
// corners (top-left, top-right, bottom-left, bottom-right), as strokes whose
// centerline runs half the line's width inside the window's edge. Built once:
// they are painted every frame and must not allocate.
var tearOffCorners = func() [4]gfx.Path {
	const r, k = windowCornerRadius, 0.5522847
	const c = r - tearOffEdge/2 // radius of the centerline
	return [4]gfx.Path{
		// Each box is r x r with the arc's circle centered on the corner of the box
		// that lies inside the window.
		gfx.NewPath(r, r, gfx.MoveTo(r, r-c), gfx.CubicTo(r-c*k, r-c, r-c, r-c*k, r-c, r)),
		gfx.NewPath(r, r, gfx.MoveTo(c, r), gfx.CubicTo(c, r-c*k, c*k, r-c, 0, r-c)),
		gfx.NewPath(r, r, gfx.MoveTo(r, c), gfx.CubicTo(r-c*k, c, r-c, c*k, r-c, 0)),
		gfx.NewPath(r, r, gfx.MoveTo(c, 0), gfx.CubicTo(c, c*k, c*k, c, 0, c)),
	}
}()

// paneBarAt finds the bar of a pane in a split tab under a press on the pane's
// own ground. Only the bar starts a drag; the terminal below it keeps selecting
// text.
func (state *State) paneBarAt(event gfx.PointerEvent) (layoutcore.PaneID, *Session, bool) {
	if state.modal != modalNone || state.activeSection != sectionTerminal ||
		(event.Button != gfx.PointerButtonPrimary && event.Button != gfx.PointerButtonNone) ||
		event.Target.Scope() != hitPaneActivateList {
		return 0, nil, false
	}
	state.refreshWorkspaceViews()
	index := item(event.Target)
	if len(state.panes) < 2 || index >= len(state.panes) {
		return 0, nil, false
	}
	pane := state.panes[index]
	bounds := pixelBounds(state.workspacePixelBounds(state.viewport), pane.Bounds)
	bounds.Height = min(bounds.Height, infoBarHeight)
	if !pointInRect(event.X, event.Y, bounds) {
		return 0, nil, false
	}
	session, ok := state.sessionByIDPointer(uint32(pane.SessionID))
	if !ok {
		return 0, nil, false
	}
	return pane.ID, session, true
}

// paneDropAt is where a pane dragged to window point at would land: among the
// tabs, or beside another pane of its own workspace.
func (state *State) paneDropAt(workspace layoutcore.WorkspaceID, source layoutcore.PaneID, at gfx.Point) tabDrop {
	if drop := state.paneUndockDropAt(at); drop.kind != dropNone {
		return drop
	}
	if state.activeSection != sectionTerminal || state.workspaceTabbed(workspace) || state.deck.ActiveWorkspace() != workspace {
		return tabDrop{}
	}
	state.refreshWorkspaceViews()
	frame := state.workspacePixelBounds(state.viewport)
	for index := range state.panes {
		pane := state.panes[index]
		bounds := pixelBounds(frame, pane.Bounds)
		if pane.ID == source || !pointInRect(at.X, at.Y, bounds) {
			continue
		}
		edge, zone := dockZoneAt(at, bounds)
		if edge == dockNone {
			edge, zone = dockCenter, bounds
		}
		return tabDrop{kind: dropDock, pane: pane.ID, edge: edge, zone: zone}
	}
	return tabDrop{}
}

// movePaneBeside docks a pane against another pane's edge. The pane keeps its
// session, connection and screen; only the split tree changes.
func (state *State) movePaneBeside(workspace layoutcore.WorkspaceID, paneID layoutcore.PaneID, drop tabDrop) {
	var err error
	if drop.edge == dockCenter {
		err = state.deck.SwapPanes(workspace, paneID, drop.pane)
	} else {
		err = state.deck.Move(workspace, paneID, drop.pane, drop.edge.axis(), defaultPaneRatio, drop.edge.placement())
	}
	if err != nil {
		state.status = err.Error()
		return
	}
	state.activatePane(workspace, paneID)
	state.workspaceViewCacheID = 0
	state.rebuildManagedWorkspace(workspace)
}

// paneUndockDropAt is where a pane dragged to window point at would become a
// tab: among the tabs, and nowhere else.
func (state *State) paneUndockDropAt(at gfx.Point) tabDrop {
	if state.tabStrip.pitch <= 0 || !pointInRect(at.X, at.Y, state.tabDropArea()) ||
		len(state.workspaceTabs) >= layoutcore.MaxWorkspaces {
		return tabDrop{}
	}
	drop := state.tabDropAt(at, 0, 1, nil)
	if drop.kind != dropReorder {
		return tabDrop{}
	}
	return drop
}

// undockPane makes a pane of a split tab a tab of its own at index among the
// tabs. The pane keeps its session, and with it the screen and scrollback.
func (state *State) undockPane(workspace layoutcore.WorkspaceID, paneID layoutcore.PaneID, index int) bool {
	pane, ok := state.deck.Pane(workspace, paneID)
	if !ok {
		return false
	}
	title := ""
	if session, ok := state.sessionByIDPointer(uint32(pane.SessionID)); ok {
		title = session.Title
	}
	id, err := state.deck.Undock(workspace, paneID, title, index)
	if err != nil {
		state.status = err.Error()
		return false
	}
	state.syncActiveSession()
	state.activeSection = sectionTerminal
	state.rebuildTabOptions()
	state.refreshWorkspaceViews()
	state.fitWorkspaceTerminals(workspace)
	state.fitWorkspaceTerminals(id)
	state.fitSFTP(state.viewport)
	state.save()
	return true
}
