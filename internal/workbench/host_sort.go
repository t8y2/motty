package workbench

import (
	"time"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
)

type hostSortState struct {
	enabled bool
	grid    components.ReorderGridState
	surface components.ReorderGrid
}

func (state *State) toggleHostSort() {
	sort := &state.hostSort
	if sort.enabled {
		sort.enabled = false
		sort.grid.Cancel()
		return
	}
	if state.activeSection != sectionHosts || state.modal != modalNone || state.hostOrder != hostOrderManual || len(state.hosts) < 2 || state.typedQuery() != "" {
		return
	}
	sort.enabled = true
	sort.grid.Cancel()
	state.selectedHostID = 0
	state.hostSelections = state.hostSelections[:0]
	state.refreshHostSelection()
	state.hostMenu.open = false
	state.groupActions.open = false
	state.hostGroupMenu = false
}

func (state *State) beginHostSortPaint(canvas *gfx.Canvas, viewport gfx.Rect) {
	if !state.hostSort.enabled {
		return
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
	if state.hostSort.enabled {
		state.hostSort.surface.End()
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
	state.hostSort.surface.Item(hitHostSortHandles.At(int(host.ID)), host.Label, lane, order, bounds, handle, false)
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
	move, result := state.hostSort.grid.Pointer(event)
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
