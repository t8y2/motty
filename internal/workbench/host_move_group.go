package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
)

var hitHostMoveGroupScroll = tree.ID(shellHits, "host-move-group-scroll")

// A move captures its hosts when opened. Its tree and fixed commands can then
// navigate without changing that captured set or any host's destination.
type hostMoveGroupMenu struct {
	tree              components.HierarchyMenuState
	leading, trailing [1]components.HierarchyMenuAction
}

func (state *State) openMoveHostGroupMenu() {
	state.hostGroupMenu = len(state.hostActionIDs) > 0
	if !state.hostGroupMenu {
		return
	}
	if state.moveGroups == nil {
		state.moveGroups = &hostMoveGroupMenu{}
	}
	values := make([]string, 0, len(state.hostGroups))
	for _, group := range state.hostGroups {
		if group.Name != "" {
			values = append(values, group.Name)
		}
	}
	state.moveGroups.tree.Build("/", "", nil, len(values), func(index int) components.HierarchyMenuItem {
		return components.HierarchyMenuItem{Value: values[index]}
	})
}

func (state *State) moveGroupTree() *components.HierarchyMenuState {
	if state.moveGroups != nil && state.moveGroups.tree.Usable() {
		return &state.moveGroups.tree
	}
	return nil
}

func (state *State) hostMoveGroupKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	if !state.hostGroupMenu || event.Release {
		return gfx.EventResult{}, false
	}
	if event.Key == gfx.KeyTab {
		state.hostGroupMenu = false
		return gfx.EventResult{Handled: true, Repaint: true, ContinueTraversal: true}, true
	}
	if menu := state.moveGroupTree(); menu != nil {
		result := menu.Key(event, true)
		if result.Picked {
			state.hostGroupMenu = false
			state.applyHostGroup(result.Chosen)
		} else if !result.ActionID.IsZero() {
			state.clickHostManagement(result.ActionID)
		}
		if result.Handled {
			return result.EventResult, true
		}
	}
	if event.Key == gfx.KeyEscape {
		state.hostGroupMenu = false
		return gfx.EventResult{Handled: true, Repaint: true}, true
	}
	return gfx.EventResult{}, false
}

func (state *State) paintHostGroupMenu(canvas *gfx.Canvas, bounds, viewport gfx.Rect) {
	menu := state.moveGroupTree()
	if menu == nil {
		// Existing catalogs may exceed the hierarchy's supported depth. Keep
		// every destination reachable through the previous flat presentation.
		state.paintFlatHostGroupMenu(canvas, bounds, viewport)
		return
	}
	anchor := state.groupMenu.button
	if state.groupMenu.menu {
		anchor = gfx.Rect{X: state.groupMenu.point.X, Y: state.groupMenu.point.Y - 6, Width: 202}
	}
	picker := state.moveGroups
	picker.leading[0] = components.HierarchyMenuAction{ID: hitHostGroupNone, Label: state.messages.MoveGroupNone}
	picker.trailing[0] = components.HierarchyMenuAction{ID: hitHostGroupNew, Label: state.messages.MoveGroupNew}
	components.PaintHierarchyMenu(canvas, state.Theme(), anchor, menu, components.HierarchyMenuOptions{
		OptionIDs: hitHostGroupItemList, ScrollID: hitHostMoveGroupScroll, Scope: dialogScope, Layer: dialogLayer,
		Label: state.messages.MoveHostGroup, Viewport: viewport, Overlay: true, DismissID: hitHostGroupDismiss,
		Leading: picker.leading[:], Trailing: picker.trailing[:], Typeahead: true,
	})
}
