package workbench

import (
	"strings"

	"github.com/vivaui/viva/gfx"
)

type hostGroupDrag struct {
	pressed, active, valid bool
	path, parent           string
	start                  gfx.Point
	zone                   gfx.Rect
}

func (state *State) hostGroupDragPointer(event gfx.PointerEvent) (gfx.EventResult, bool) {
	a, drag := &state.groupActions, &state.groupActions.drag
	if a.open && event.Kind == gfx.PointerDown && event.Button == gfx.PointerButtonSecondary && event.Target == hitGroupActionDismiss {
		a.open = false
		return gfx.EventResult{Handled: true, Repaint: true}, true
	}
	switch event.Kind {
	case gfx.PointerDown:
		*drag = hostGroupDrag{}
		if state.activeSection != sectionHosts || state.modal != modalNone || state.hostSort.enabled || a.open || event.Button != gfx.PointerButtonPrimary {
			return gfx.EventResult{}, false
		}
		if event.Target.Scope() != hitHostGroupRowList && event.Target.Scope() != hitHostGroupOpenList {
			return gfx.EventResult{}, false
		}
		index := item(event.Target)
		if index >= 0 && index < len(state.hostGroups) && state.hostGroups[index].Name != "" {
			*drag = hostGroupDrag{pressed: true, path: state.hostGroups[index].Name, start: gfx.Point{X: event.X, Y: event.Y}}
		}
	case gfx.PointerMove:
		if !drag.pressed {
			return gfx.EventResult{}, false
		}
		dx, dy := event.X-drag.start.X, event.Y-drag.start.Y
		if !drag.active && dx*dx+dy*dy < tabDragThreshold*tabDragThreshold {
			return gfx.EventResult{}, false
		}
		drag.active = true
		state.trackHostGroupDrop(event.X, event.Y)
		return gfx.EventResult{Handled: true, Repaint: true}, true
	case gfx.PointerUp:
		if !drag.pressed {
			return gfx.EventResult{}, false
		}
		if !drag.active {
			*drag = hostGroupDrag{}
			return gfx.EventResult{}, false
		}
		state.trackHostGroupDrop(event.X, event.Y)
		finished := *drag
		*drag = hostGroupDrag{}
		if finished.valid && state.activeSection == sectionHosts && state.modal == modalNone {
			if !state.moveHostGroup(finished.path, finished.parent) {
				state.status, state.dialogError = state.dialogError, ""
			}
		}
		return gfx.EventResult{Handled: true, Repaint: true}, true
	}
	return gfx.EventResult{}, false
}

func (state *State) trackHostGroupDrop(x, y float32) {
	drag := &state.groupActions.drag
	drag.valid = false
	if !pointInRect(x, y, state.chrome.hostList) {
		return
	}
	for index, bounds := range state.groupActions.rows {
		if bounds.Width <= 0 || !pointInRect(x, y, bounds) || index >= len(state.hostGroups) {
			continue
		}
		parent := state.hostGroups[index].Name
		if hostGroupWithin(parent, drag.path) {
			return
		}
		target := joinHostGroup(parent, hostGroupLeaf(drag.path))
		for _, group := range state.hostGroups {
			if strings.EqualFold(group.Name, target) && group.Name != drag.path {
				return
			}
		}
		drag.parent, drag.zone, drag.valid = parent, bounds, true
		return
	}
}

func (state *State) paintHostGroupDrop(canvas *gfx.Canvas) {
	drag := &state.groupActions.drag
	if drag.active && drag.valid {
		canvas.Fill(gfx.Rect{X: drag.zone.X, Y: drag.zone.Y, Width: drag.zone.Width, Height: 2}, state.pal.accent)
	}
}
