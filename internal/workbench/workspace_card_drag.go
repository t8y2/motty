package workbench

import (
	"github.com/vivaui/viva/ui"
	"slices"
)

func (state *State) workspaceCardsPointer(event ui.PointerEvent) (ui.EventResult, bool) {
	drag := &state.workspaceCardDrag
	switch event.Kind {
	case ui.PointerDown:
		state.startWorkspaceCardDrag(event)
	case ui.PointerMove:
		return state.trackWorkspaceCardDrag(event)

	case ui.PointerUp:
		finished := *drag
		*drag = workspaceCardDrag{}
		if finished.active {
			target := state.workspaceCardDropIndex(finished.region, ui.Point{X: event.X, Y: event.Y})
			if slices.Equal(finished.order, state.workspaceItemsForRegion(finished.region)) {
				state.moveWorkspaceCard(finished.region, target)
			}
			return ui.EventResult{Handled: true, Repaint: true}, true
		}
	case ui.PointerExit:
		*drag = workspaceCardDrag{}
	}
	return ui.EventResult{}, false
}

func (state *State) startWorkspaceCardDrag(event ui.PointerEvent) {
	drag := &state.workspaceCardDrag

	*drag = workspaceCardDrag{}
	if event.Button != ui.PointerButtonPrimary && event.Button != ui.PointerButtonNone {
		return
	}
	for _, region := range state.workspaceCardRegions {
		if region.id == event.Target {
			*drag = workspaceCardDrag{pressed: true, region: region, start: ui.Point{X: event.X, Y: event.Y}, target: -1, order: slices.Clone(state.workspaceItemsForRegion(region))}
			break
		}
	}
}

func (state *State) trackWorkspaceCardDrag(event ui.PointerEvent) (ui.EventResult, bool) {
	drag := &state.workspaceCardDrag

	if !drag.pressed {
		return ui.EventResult{}, false
	}
	dx, dy := event.X-drag.start.X, event.Y-drag.start.Y
	if !drag.active && dx*dx+dy*dy < tabDragThreshold*tabDragThreshold {
		return ui.EventResult{}, false
	}
	drag.active, drag.at = true, ui.Point{X: event.X, Y: event.Y}
	drag.target = state.workspaceCardDropIndex(drag.region, drag.at)
	return ui.EventResult{Handled: true, Repaint: true}, true
}

func (state *State) workspaceCardDropIndex(source workspaceCardRegion, at ui.Point) int {
	for _, region := range state.workspaceCardRegions {
		if region.view == source.view && region.template == source.template &&
			pointInRect(at.X, at.Y, region.bounds) && pointInRect(at.X, at.Y, region.viewport) {
			return region.index
		}
	}
	return -1
}

func (state *State) moveWorkspaceCard(source workspaceCardRegion, target int) {
	switch source.view {
	case workspaceCardsRuntime:
		if moveWorkspaceItem(state.workspaceLayoutDraft.items, source.index, target) {
			state.applyWorkspaceArrangement()
		}
	case workspaceCardsTemplate:
		state.moveWorkspaceTemplateCard(source.template, source.index, target)
	case workspaceCardsDraft:
		state.moveWorkspaceDraftItem(source.index, target)
	}
}

func (state *State) workspaceCardsKey(event ui.KeyEvent) (ui.EventResult, bool) {
	if event.Release {
		return ui.EventResult{}, false
	}
	if event.Key == ui.KeyEscape && state.workspaceCardDrag.pressed {
		state.workspaceCardDrag = workspaceCardDrag{}
		return ui.EventResult{Handled: true, Repaint: true}, true
	}
	if event.Modifiers != ui.ModifierAlt {
		return ui.EventResult{}, false
	}
	for _, region := range state.workspaceCardRegions {
		if region.id == event.Target {
			return state.workspaceCardKey(event, region)
		}
	}
	return ui.EventResult{}, false
}

func (state *State) workspaceCardKey(event ui.KeyEvent, source workspaceCardRegion) (ui.EventResult, bool) {
	columns := state.workspaceCardColumns(source)
	delta, ok := workspaceCardKeyDelta(event.Key, columns)
	if !ok {
		return ui.EventResult{}, false
	}

	target := source.index + delta
	count := len(state.workspaceItemsForRegion(source))
	if target < 0 || target >= count {
		return ui.EventResult{Handled: true}, true
	}
	state.moveWorkspaceCard(source, target)
	state.revealWorkspaceCard(source, target, count, columns)
	focus := hitWorkspaceLayoutCards.At(target)
	switch source.view {
	case workspaceCardsTemplate:
		templateIndex := item(source.id) / maxWorkspaceTemplateHosts
		focus = hitWorkspaceTemplateCards.At(templateIndex*maxWorkspaceTemplateHosts + target)
	case workspaceCardsDraft:
		focus = hitWorkspaceDraftCards.At(target)
	}
	return ui.EventResult{Handled: true, Repaint: true, Focus: focus, FocusVisible: true}, true
}

func (state *State) workspaceCardColumns(region workspaceCardRegion) uint8 {
	switch region.view {
	case workspaceCardsRuntime:
		return state.workspaceLayoutDraft.columns
	case workspaceCardsDraft:
		return state.workspaceDraft.columns
	case workspaceCardsTemplate:
		for _, template := range state.templates {
			if template.ID == region.template {
				return template.Columns
			}
		}
	}
	return 1
}

// Keyboard moves can land beyond a scrolled viewport. Reveal the destination
// before the runtime resolves its focus request in the next paint.
func (state *State) revealWorkspaceCard(source workspaceCardRegion, target, count int, columns uint8) {
	cells := workspaceGridCells(count, columns)
	height := workspaceCardContentHeight(count, columns)
	origin := source.bounds.Y - 4 - cells[source.index].Y*height
	box := ui.Rect{Y: origin + cells[target].Y*height + 4, Height: workspaceCardHeight - 8}
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

func workspaceCardKeyDelta(key ui.Key, columns uint8) (int, bool) {
	switch key {
	case ui.KeyLeft:
		return -1, true
	case ui.KeyRight:
		return 1, true
	case ui.KeyUp:
		return -int(columns), true
	case ui.KeyDown:
		return int(columns), true
	default:
		return 0, false
	}
}
