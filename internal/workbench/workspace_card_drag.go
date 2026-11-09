package workbench

import (
	"slices"

	"github.com/vivaui/viva/gfx"
	layoutcore "motty/internal/workspace"
)

func (state *State) workspaceCardsPointer(event gfx.PointerEvent) (gfx.EventResult, bool) {
	drag := &state.workspaceCardDrag
	switch event.Kind {
	case gfx.PointerDown:
		state.startWorkspaceCardDrag(event)
	case gfx.PointerMove:
		return state.trackWorkspaceCardDrag(event)

	case gfx.PointerUp:
		finished := *drag
		*drag = workspaceCardDrag{}
		if finished.active {
			at := gfx.Point{X: event.X, Y: event.Y}
			target, zone := state.workspaceCardDropIndex(finished.region, at)
			if slices.Equal(finished.order, state.workspaceItemsForRegion(finished.region)) {
				if zone == zoneSwap {
					state.moveWorkspaceCard(finished.region, target)
				} else {
					state.dockWorkspaceCard(finished.region, target, zone)
				}
			}
			return gfx.EventResult{Handled: true, Repaint: true}, true
		}
	case gfx.PointerExit:
		*drag = workspaceCardDrag{}
	}
	return gfx.EventResult{}, false
}

func (state *State) startWorkspaceCardDrag(event gfx.PointerEvent) {
	drag := &state.workspaceCardDrag

	*drag = workspaceCardDrag{}
	if event.Button != gfx.PointerButtonPrimary && event.Button != gfx.PointerButtonNone {
		return
	}
	for _, region := range state.workspaceCardRegions {
		if region.id == event.Target {
			*drag = workspaceCardDrag{pressed: true, region: region, start: gfx.Point{X: event.X, Y: event.Y}, target: -1, order: slices.Clone(state.workspaceItemsForRegion(region))}
			break
		}
	}
}

func (state *State) trackWorkspaceCardDrag(event gfx.PointerEvent) (gfx.EventResult, bool) {
	drag := &state.workspaceCardDrag

	if !drag.pressed {
		return gfx.EventResult{}, false
	}
	dx, dy := event.X-drag.start.X, event.Y-drag.start.Y
	if !drag.active && dx*dx+dy*dy < tabDragThreshold*tabDragThreshold {
		return gfx.EventResult{}, false
	}
	drag.active, drag.at = true, gfx.Point{X: event.X, Y: event.Y}
	drag.target, drag.zone = state.workspaceCardDropIndex(drag.region, drag.at)
	return gfx.EventResult{Handled: true, Repaint: true}, true
}

// workspaceCardDropIndex is the card under at and the part of it. A card is not
// docked beside itself, so over its own edges the drop is a plain swap.
func (state *State) workspaceCardDropIndex(source workspaceCardRegion, at gfx.Point) (int, workspaceCardZone) {
	for _, region := range state.workspaceCardRegions {
		if region.view == source.view && region.template == source.template &&
			pointInRect(at.X, at.Y, region.bounds) && pointInRect(at.X, at.Y, region.viewport) {
			if region.index == source.index {
				return region.index, zoneSwap
			}
			return region.index, workspaceCardZoneAt(region.bounds, at)
		}
	}
	return -1, zoneSwap
}

// dockWorkspaceCard redraws the layout so the dragged card sits beside the
// target on the side zone names. The cards keep their order.
func (state *State) dockWorkspaceCard(source workspaceCardRegion, target int, zone workspaceCardZone) {
	count := len(state.workspaceItemsForRegion(source))
	axis, placement := zone.dock()
	current := state.workspaceCardLayout(source)
	spec, order, err := current.MovePosition(count, source.index+1, target+1, axis, placement)
	if err != nil {
		return
	}
	switch source.view {
	case workspaceCardsRuntime:
		draft := &state.workspaceLayoutDraft
		draft.items, draft.layout = permuteWorkspaceItems(draft.items, order), spec
		state.applyWorkspaceArrangement()
	case workspaceCardsTemplate:
		state.dockWorkspaceTemplateCard(source.template, spec, order)
	case workspaceCardsDraft:
		state.dockWorkspaceDraftCard(spec, order)
	}
}

func (state *State) moveWorkspaceCard(source workspaceCardRegion, target int) {
	switch source.view {
	case workspaceCardsRuntime:
		if placeWorkspaceItem(state.workspaceLayoutDraft.items, source.index, target, state.workspaceLayoutDraft.layout) {
			state.applyWorkspaceArrangement()
		}
	case workspaceCardsTemplate:
		state.moveWorkspaceTemplateCard(source.template, source.index, target)
	case workspaceCardsDraft:
		state.moveWorkspaceDraftItem(source.index, target)
	}
}

func (state *State) workspaceCardsKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	if event.Release {
		return gfx.EventResult{}, false
	}
	if event.Key == gfx.KeyEscape && state.workspaceCardDrag.pressed {
		state.workspaceCardDrag = workspaceCardDrag{}
		return gfx.EventResult{Handled: true, Repaint: true}, true
	}
	if event.Modifiers != gfx.ModifierAlt {
		return gfx.EventResult{}, false
	}
	for _, region := range state.workspaceCardRegions {
		if region.id == event.Target {
			return state.workspaceCardKey(event, region)
		}
	}
	return gfx.EventResult{}, false
}

func (state *State) workspaceCardKey(event gfx.KeyEvent, source workspaceCardRegion) (gfx.EventResult, bool) {
	spec := state.workspaceCardLayout(source)
	delta, ok := workspaceCardKeyDelta(event.Key, spec.GridColumns())
	if !ok {
		return gfx.EventResult{}, false
	}

	target := source.index + delta
	count := len(state.workspaceItemsForRegion(source))
	if target < 0 || target >= count {
		return gfx.EventResult{Handled: true}, true
	}
	state.moveWorkspaceCard(source, target)
	state.revealWorkspaceCard(source, target, count, spec)
	focus := hitWorkspaceLayoutCards.At(target)
	switch source.view {
	case workspaceCardsTemplate:
		templateIndex := item(source.id) / maxWorkspaceTemplateHosts
		focus = hitWorkspaceTemplateCards.At(templateIndex*maxWorkspaceTemplateHosts + target)
	case workspaceCardsDraft:
		focus = hitWorkspaceDraftCards.At(target)
	}
	return gfx.EventResult{Handled: true, Repaint: true, Focus: focus, FocusVisible: true}, true
}

func (state *State) workspaceCardLayout(region workspaceCardRegion) layoutcore.LayoutSpec {
	switch region.view {
	case workspaceCardsRuntime:
		return state.workspaceLayoutDraft.layout
	case workspaceCardsDraft:
		return state.workspaceDraft.layout
	case workspaceCardsTemplate:
		for _, template := range state.templates {
			if template.ID == region.template {
				return template.Layout
			}
		}
	}
	return layoutcore.GridSpec(1)
}

// Keyboard moves can land beyond a scrolled viewport. Reveal the destination
// before the runtime resolves its focus request in the next paint.
func (state *State) revealWorkspaceCard(source workspaceCardRegion, target, count int, spec layoutcore.LayoutSpec) {
	cells := workspaceLayoutCells(count, spec)
	height := workspaceCardContentHeight(count, spec)
	origin := source.bounds.Y - 4 - cells[source.index].Y*height
	box := gfx.Rect{Y: origin + cells[target].Y*height + 4, Height: workspaceCardHeight - 8}
	delta := float32(0)
	if box.Y < source.viewport.Y {
		delta = box.Y - source.viewport.Y
	}
	if box.Y+box.Height > source.viewport.Y+source.viewport.Height {
		delta = box.Y + box.Height - source.viewport.Y - source.viewport.Height
	}
	switch source.view {
	case workspaceCardsRuntime:
		state.workspaceLayoutDraft.scroll = max(0, state.workspaceLayoutDraft.scroll+delta)
	case workspaceCardsTemplate:
		state.workspaceLibraryScroll = max(0, state.workspaceLibraryScroll+delta)
	case workspaceCardsDraft:
		state.workspaceDraft.previewScroll = max(0, state.workspaceDraft.previewScroll+delta)
	}
}

func workspaceCardKeyDelta(key gfx.Key, columns uint8) (int, bool) {
	switch key {
	case gfx.KeyLeft:
		return -1, true
	case gfx.KeyRight:
		return 1, true
	case gfx.KeyUp:
		return -int(columns), true
	case gfx.KeyDown:
		return int(columns), true
	default:
		return 0, false
	}
}
