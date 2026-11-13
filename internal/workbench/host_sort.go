package workbench

import (
	"time"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
)

// hostDragSlop is how far a press must travel before it becomes a drag. A
// shorter press stays the click that selects or opens the host.
const hostDragSlop = float32(5)

// hostSortPress is a press on a host that a move turns into a drag.
type hostSortPress struct {
	active bool
	hostID uint32
	start  gfx.Point
}

// hostSortPickup is the drag waiting for the frame that declares the handles.
// The component can only start a drag on a handle it has just been given, so the
// first move after that paint begins it.
type hostSortPickup struct {
	hostID uint32
	x, y   float32
	ready  bool
}

type hostSortState struct {
	enabled bool
	grid    components.ReorderGridState
	surface components.ReorderGrid
	press   hostSortPress
	pickup  hostSortPickup
	// geo is where the visible rows or cards of this frame were painted, for the
	// drag's ghost and drop line.
	geo map[uint32]gfx.Rect
}

// endHostSort leaves the drag and forgets a pending press.
func (state *State) endHostSort() {
	state.hostSort.enabled = false
	state.hostSort.grid.Cancel()
	state.hostSort.press = hostSortPress{}
	state.hostSort.pickup = hostSortPickup{}
}

// hostPressPointer turns a press on a host row or card into a drag when the
// pointer moves far enough. It never handles the press itself: a click that
// does not move remains the selection or open it always was.
func (state *State) hostPressPointer(event gfx.PointerEvent) (gfx.EventResult, bool) {
	press := &state.hostSort.press
	if state.hostSort.enabled {
		return gfx.EventResult{}, false
	}
	switch event.Kind {
	case gfx.PointerDown:
		press.active = false
		if state.activeSection != sectionHosts || state.modal != modalNone ||
			state.typedQuery() != "" || len(state.hosts) < 2 {
			return gfx.EventResult{}, false
		}
		if event.Button != gfx.PointerButtonPrimary && event.Button != gfx.PointerButtonNone {
			return gfx.EventResult{}, false
		}
		host, ok := state.hostForRow(event.Target)
		if !ok {
			return gfx.EventResult{}, false
		}
		*press = hostSortPress{active: true, hostID: host.ID, start: gfx.Point{X: event.X, Y: event.Y}}
	case gfx.PointerMove:
		if !press.active {
			return gfx.EventResult{}, false
		}
		dx, dy := event.X-press.start.X, event.Y-press.start.Y
		if dx*dx+dy*dy <= hostDragSlop*hostDragSlop {
			return gfx.EventResult{}, false
		}
		hostID, start := press.hostID, press.start
		*press = hostSortPress{}
		state.beginHostPressDrag(hostID, start)
	case gfx.PointerUp, gfx.PointerExit:
		press.active = false
	}
	return gfx.EventResult{}, false
}

// beginHostPressDrag enters the reorder mode the drag asked for. A display order
// other than Manual gives way to manual order, which is what the move will
// write.
func (state *State) beginHostPressDrag(hostID uint32, start gfx.Point) {
	if state.activeSection != sectionHosts || state.modal != modalNone {
		return
	}
	if _, ok := state.hostByID(hostID); !ok {
		return
	}
	if state.hostOrder != hostOrderManual {
		state.hostOrder = hostOrderManual
		state.visible = nil
		state.refreshHostGroups()
		state.syncVisible()
		state.save()
	}
	sort := &state.hostSort
	sort.enabled = true
	sort.grid.Cancel()
	sort.pickup = hostSortPickup{hostID: hostID, x: start.X, y: start.Y}
	state.selectedHostID = 0
	state.hostSelections = state.hostSelections[:0]
	state.refreshHostSelection()
	state.hostMenu.open = false
	state.groupActions.open = false
	state.hostGroupMenu = false
	state.hostOrderMenu = false
	state.hostSort.press = hostSortPress{}
}

func (state *State) beginHostSortPaint(canvas *gfx.Canvas, viewport gfx.Rect) {
	if !state.hostSort.enabled {
		return
	}
	if state.hostSort.geo == nil {
		state.hostSort.geo = make(map[uint32]gfx.Rect)
	} else {
		clear(state.hostSort.geo)
	}
	state.hostSort.surface = components.BeginReorderGrid(canvas, state.Theme(), viewport, &state.hostSort.grid).HandleHitSlop(10)
	for order, host := range state.hosts {
		lane := state.hostGroupIndex(host.Group)
		if lane < 0 {
			lane = 0
		}
		state.hostSort.surface.Order(hitHostSortHandles.At(int(host.ID)), lane, order, false)
	}
}

func (state *State) endHostSortPaint() {
	if !state.hostSort.enabled {
		return
	}
	state.hostSort.surface.End()
	if state.hostSort.pickup.hostID != 0 {
		// The handles this frame declared can now be pressed.
		state.hostSort.pickup.ready = true
	}
}

func (state *State) declareHostSortItem(host Host, bounds, handle gfx.Rect) {
	if !state.hostSort.enabled {
		return
	}
	lane := state.hostGroupIndex(host.Group)
	if lane < 0 {
		lane = 0
	}
	order := state.hostIndex(host.ID)
	state.hostSort.geo[host.ID] = bounds
	state.hostSort.surface.Item(hitHostSortHandles.At(int(host.ID)), state.hostPrivacySemanticLabel(host), lane, order, bounds, handle, false)
}

func (state *State) hostSortDragging(id uint32) bool {
	return state.hostSort.enabled && state.hostSort.grid.Dragging() &&
		state.hostSort.grid.Active() == hitHostSortHandles.At(int(id))
}

func (state *State) hostSortHovered(canvas *gfx.Canvas, id uint32) bool {
	return state.hostSort.enabled && canvas.Hovered() == hitHostSortHandles.At(int(id))
}

func (state *State) hostSortTarget(id uint32) bool {
	return state.hostSort.enabled && state.hostSort.grid.Dragging() &&
		state.hostSort.grid.Target() == hitHostSortHandles.At(int(id)) &&
		state.hostSort.grid.Active() != hitHostSortHandles.At(int(id))
}

func (state *State) hostSortPointer(event gfx.PointerEvent) (gfx.EventResult, bool) {
	if !state.hostSort.enabled || state.activeSection != sectionHosts || state.modal != modalNone {
		return gfx.EventResult{}, false
	}
	sort := &state.hostSort
	if sort.pickup.hostID != 0 {
		handle := hitHostSortHandles.At(int(sort.pickup.hostID))
		switch event.Kind {
		case gfx.PointerMove:
			if !sort.pickup.ready {
				return gfx.EventResult{Handled: true, Repaint: true}, true
			}
			// The frame's handles exist, so the component can start the drag the
			// long press picked up. The current move is fed after this press.
			sort.grid.Pointer(gfx.PointerEvent{
				Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, Target: handle,
				X: sort.pickup.x, Y: sort.pickup.y, Viewport: event.Viewport,
			})
			sort.pickup = hostSortPickup{}
			if sort.grid.Active() != handle {
				state.endHostSort()
				return gfx.EventResult{Handled: true, Repaint: true}, true
			}
		case gfx.PointerUp, gfx.PointerExit:
			// The hold was let go before anything moved: leave without a move.
			state.endHostSort()
			return gfx.EventResult{Handled: true, Repaint: true}, true
		default:
			return gfx.EventResult{Handled: true, Repaint: true}, true
		}
	}
	move, result := sort.grid.Pointer(event)
	if !result.Handled {
		return result, false
	}
	if event.Kind == gfx.PointerMove && state.hostSort.grid.Dragging() {
		if state.autoScrollHostSortIn(state.chrome.hostList, state.hostsContent, event.Y) {
			result.Repaint = true
		}
	}
	if move.Valid() && state.applyHostSortMove(move) {
		state.save()
	}
	if event.Kind == gfx.PointerUp || event.Kind == gfx.PointerExit {
		state.endHostSort()
	}
	return result, true
}

func (state *State) autoScrollHostSortIn(viewport gfx.Rect, content, y float32) bool {
	if viewport.Height <= 0 || content <= viewport.Height {
		return false
	}
	const (
		edge = float32(48)
		step = float32(22)
	)
	var delta float32
	switch {
	case y < viewport.Y+edge:
		factor := min(max((viewport.Y+edge-y)/edge, 0), 1)
		delta = -step * factor
	case y > viewport.Y+viewport.Height-edge:
		factor := min(max((y-(viewport.Y+viewport.Height-edge))/edge, 0), 1)
		delta = step * factor
	default:
		return false
	}
	before := state.hostsScroll
	state.hostsScroll = min(max(before+delta, 0), max(content-viewport.Height, 0))
	return state.hostsScroll != before
}

// hostSortAutoScrollFrame keeps scrolling while a held drag rests at an edge.
// RequestFrameIn must be repeated by every frame that still needs another one,
// so releasing the pointer or leaving the edge stops the loop without a timer.
func (state *State) hostSortAutoScrollFrame(canvas *gfx.Canvas, viewport gfx.Rect, content float32) bool {
	if !state.hostSort.enabled || !state.hostSort.grid.Dragging() {
		return false
	}
	point, held := state.hostSort.grid.PointerPosition()
	if !held || !state.autoScrollHostSortIn(viewport, content, point.Y) {
		return false
	}
	canvas.RequestFrameIn(time.Second / 60)
	return true
}

// paintHostSortDrag draws the feedback of a drag in progress: the source row or
// card dimmed where it was, a line where it would land, and a ghost of the host
// under the pointer. The reorder component owns the geometry; this is the part
// the user needs to see to know a drag is happening.
func (state *State) paintHostSortDrag(canvas *gfx.Canvas, viewport gfx.Rect) {
	sort := &state.hostSort
	if !sort.enabled || !sort.grid.Dragging() || state.hostView == hostViewGrid && len(sort.geo) == 0 {
		return
	}
	sourceID := uint32(item(sort.grid.Active()))
	targetID := uint32(item(sort.grid.Target()))
	source, ok := sort.geo[sourceID]
	if !ok || sourceID == 0 {
		return
	}
	host, ok := state.hostByID(sourceID)
	if !ok {
		return
	}
	point, held := sort.grid.PointerPosition()
	if !held {
		return
	}
	pal := state.pal
	canvas.Clip(viewport)
	defer canvas.Unclip()

	// The source keeps its place as a placeholder, without its content.
	canvas.Rounded(source, radius.control, withAlpha(pal.workspace, 0.55))
	// The host follows the pointer.
	width, height := source.Width, source.Height
	x := min(max(point.X+12, viewport.X), max(viewport.X+viewport.Width-width, viewport.X))
	y := min(max(point.Y-height/2, viewport.Y), max(viewport.Y+viewport.Height-height, viewport.Y))
	ghost := gfx.Rect{X: x, Y: y, Width: width, Height: height}
	canvas.Elevate(ghost, radius.control, gfx.ShadowMD)
	canvas.RoundedBorder(ghost, radius.control, 1, pal.accent, withAlpha(pal.surfaceSoft, 0.97))
	line := gfx.Rect{X: ghost.X + 12, Y: ghost.Y, Width: max(ghost.Width-24, 0), Height: ghost.Height}
	label := canvas.FitLine(state.hostPrivacySemanticLabel(host), metrics.FontSizeM, gfx.WeightSemibold, line.Width)
	canvas.Label(label, gfx.Rect{X: line.X, Y: line.Y + (line.Height-22)/2, Width: line.Width, Height: 22},
		gfx.TextStyle{Size: metrics.FontSizeM, Weight: gfx.WeightSemibold, Color: pal.text})
	if subtitle := state.hostSubtitle(host); subtitle != "" && line.Height > 36 && !state.hostPrivacy.enabled {
		subtitle = canvas.FitLine(subtitle, metrics.FontSizeS, gfx.WeightRegular, line.Width)
		canvas.Label(subtitle, gfx.Rect{X: line.X, Y: line.Y + (line.Height-22)/2 + 22, Width: line.Width, Height: 19},
			gfx.TextStyle{Size: metrics.FontSizeS, Color: pal.muted})
	}
	// Where it will land. After the ghost, so the ghost cannot hide the place.
	if target, ok := sort.geo[targetID]; ok && targetID != sourceID && targetID != 0 {
		if state.hostView == hostViewGrid {
			x := target.X
			if point.X > target.X+target.Width/2 {
				x = target.X + target.Width - 2
			}
			canvas.Rounded(gfx.Rect{X: x, Y: target.Y, Width: 2, Height: target.Height}, 1, pal.accent)
		} else {
			y := target.Y
			if point.Y > target.Y+target.Height/2 {
				y = target.Y + target.Height - 2
			}
			canvas.Rounded(gfx.Rect{X: target.X, Y: y, Width: target.Width, Height: 2}, 1, pal.accent)
		}
	}
}

// hostRowReorderKey moves the focused host one place within its group with
// Alt+Up/Down: the keyboard path to the manual order a long press writes.
func (state *State) hostRowReorderKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	if state.activeSection != sectionHosts || state.modal != modalNone || state.hostSort.enabled ||
		event.Release || event.Repeat || event.Modifiers != gfx.ModifierAlt {
		return gfx.EventResult{}, false
	}
	if event.Key != gfx.KeyUp && event.Key != gfx.KeyDown {
		return gfx.EventResult{}, false
	}
	host, ok := state.hostForRow(event.Target)
	if !ok {
		return gfx.EventResult{}, false
	}
	from := state.hostIndex(host.ID)
	if from < 0 {
		return gfx.EventResult{}, false
	}
	step := -1
	if event.Key == gfx.KeyDown {
		step = 1
	}
	target := -1
	for index := from + step; index >= 0 && index < len(state.hosts); index += step {
		if state.hosts[index].Group == host.Group {
			target = index
			break
		}
	}
	if target < 0 {
		return gfx.EventResult{}, false
	}
	if state.hostOrder != hostOrderManual {
		state.hostOrder = hostOrderManual
		state.visible = nil
		state.refreshHostGroups()
		state.syncVisible()
	}
	move := components.ReorderGridMove{
		ID:     hitHostSortHandles.At(int(host.ID)),
		Target: hitHostSortHandles.At(int(state.hosts[target].ID)),
		From:   from, To: target,
	}
	if !state.applyHostSortMove(move) {
		return gfx.EventResult{}, false
	}
	state.save()
	return gfx.EventResult{Handled: true, Repaint: true}, true
}

func (state *State) applyHostSortMove(move components.ReorderGridMove) bool {
	if state.hostOrder != hostOrderManual || !move.Valid() || move.ID.Scope() != hitHostSortHandles || move.Target.Scope() != hitHostSortHandles {
		return false
	}
	sourceID := uint32(item(move.ID))
	targetID := uint32(item(move.Target))
	source := state.hostIndex(sourceID)
	target := state.hostIndex(targetID)
	if source < 0 || target < 0 || source == target || state.hosts[source].Group != state.hosts[target].Group {
		return false
	}
	host := state.hosts[source]
	if source < target {
		copy(state.hosts[source:target], state.hosts[source+1:target+1])
		state.hosts[target] = host
	} else {
		copy(state.hosts[target+1:source+1], state.hosts[target:source])
		state.hosts[target] = host
	}
	state.visible = nil
	state.syncVisible()
	return true
}
