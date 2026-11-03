package workbench

import (
	"strings"

	"github.com/vivaui/viva/ui"
)

type hostGroupDrag struct {
	pressed, active, valid bool
	path, parent           string
	start                  ui.Point
	zone                   ui.Rect
}

func (state *State) hostGroupDragPointer(event ui.PointerEvent) (ui.EventResult, bool) {
	a, drag := &state.groupActions, &state.groupActions.drag
	if a.open && event.Kind == ui.PointerDown && event.Button == ui.PointerButtonSecondary && event.Target == hitGroupActionDismiss {
		a.open = false
		return ui.EventResult{Handled: true, Repaint: true}, true
	}
	switch event.Kind {
	case ui.PointerDown:
		*drag = hostGroupDrag{}
		if state.activeSection != sectionHosts || state.modal != modalNone || a.open || event.Button != ui.PointerButtonPrimary {
			return ui.EventResult{}, false
		}
		if event.Target.Scope() != hitHostGroupRowList && event.Target.Scope() != hitHostGroupOpenList {
			return ui.EventResult{}, false
		}
		index := item(event.Target)
		if index >= 0 && index < len(state.hostGroups) && state.hostGroups[index].Name != "" {
			*drag = hostGroupDrag{pressed: true, path: state.hostGroups[index].Name, start: ui.Point{X: event.X, Y: event.Y}}
		}
	case ui.PointerMove:
		if !drag.pressed {
			return ui.EventResult{}, false
		}
		dx, dy := event.X-drag.start.X, event.Y-drag.start.Y
		if !drag.active && dx*dx+dy*dy < tabDragThreshold*tabDragThreshold {
			return ui.EventResult{}, false
		}
		drag.active = true
		state.trackHostGroupDrop(event.X, event.Y)
		return ui.EventResult{Handled: true, Repaint: true}, true
	case ui.PointerUp:
		if !drag.pressed {
			return ui.EventResult{}, false
		}
		if !drag.active {
			*drag = hostGroupDrag{}
			return ui.EventResult{}, false
		}
		state.trackHostGroupDrop(event.X, event.Y)
		finished := *drag
		*drag = hostGroupDrag{}
		if finished.valid && state.activeSection == sectionHosts && state.modal == modalNone {
			if !state.moveHostGroup(finished.path, finished.parent) {
				state.status, state.dialogError = state.dialogError, ""
			}
		}
		return ui.EventResult{Handled: true, Repaint: true}, true
	}
	return ui.EventResult{}, false
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

func (state *State) paintHostGroupDrop(canvas *ui.Canvas) {
	drag := &state.groupActions.drag
	if drag.active && drag.valid {
		canvas.Fill(ui.Rect{X: drag.zone.X, Y: drag.zone.Y, Width: drag.zone.Width, Height: 2}, state.pal.accent)
	}
}
