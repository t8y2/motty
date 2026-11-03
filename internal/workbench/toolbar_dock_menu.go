package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/ui"
)

var toolbarDockHits = tree.Feature("toolbar-dock")
var (
	hitToolbarDockDismiss     = tree.ID(toolbarDockHits, "dismiss")
	hitToolbarDockTop         = tree.ID(toolbarDockHits, "move-top")
	hitToolbarDockSideTop     = tree.ID(toolbarDockHits, "move-side-top")
	hitToolbarDockSideBottom  = tree.ID(toolbarDockHits, "move-side-bottom")
	hitToolbarDockLeftTop     = tree.ID(toolbarDockHits, "move-left-top")
	hitToolbarDockLeftBottom  = tree.ID(toolbarDockHits, "move-left-bottom")
	hitToolbarDockBottom      = tree.ID(toolbarDockHits, "move-bottom")
	hitToolbarDockBottomRight = tree.ID(toolbarDockHits, "move-bottom-right")
	hitToolbarDockHide        = tree.ID(toolbarDockHits, "hide")
	hitToolbarDockCustomize   = tree.ID(toolbarDockHits, "customize")
	hitToolbarDockReset       = tree.ID(toolbarDockHits, "reset")
	hitToolbarDockMore        = tree.ID(toolbarDockHits, "more")
)

func (state *State) paintToolbarDockMenu(canvas *ui.Canvas, viewport ui.Rect) {
	dock, m := &state.toolbarDock, state.messages.Toolbars
	if !dock.menu || state.modal != modalNone {
		return
	}
	var rows menuRows
	if dock.overflow {
		var buffer [maxBarTools]barTool
		tools := state.toolbarDockTools(toolbarDockTop, &buffer)
		for _, tool := range tools[min(len(tools), state.toolbarTopLimit()-1):] {
			rows.add(components.ContextMenuItem{ID: tool.id, Label: tool.hint, Icon: lucideIcons[tool.icon], Disabled: tool.disabled})
		}
	} else {
		rows.add(components.ContextMenuItem{ID: hitToolbarDockTop, Label: m.MoveTop, Icon: lucideIcons[iconRows2]})
		rows.add(components.ContextMenuItem{ID: hitToolbarDockLeftTop, Label: m.MoveLeftTop, Icon: lucideIcons[iconArrowUp]})
		rows.add(components.ContextMenuItem{ID: hitToolbarDockLeftBottom, Label: m.MoveLeftBottom, Icon: lucideIcons[iconChevronDown]})
		rows.add(components.ContextMenuItem{ID: hitToolbarDockSideTop, Label: m.MoveSideTop, Icon: lucideIcons[iconArrowUp]})
		rows.add(components.ContextMenuItem{ID: hitToolbarDockSideBottom, Label: m.MoveSideBottom, Icon: lucideIcons[iconChevronDown]})
		rows.add(components.ContextMenuItem{ID: hitToolbarDockBottom, Label: m.MoveBottom, Icon: lucideIcons[iconRows2]})
		rows.add(components.ContextMenuItem{ID: hitToolbarDockBottomRight, Label: m.MoveBottomRight, Icon: lucideIcons[iconRows2]})
		rows.separate()
		rows.add(components.ContextMenuItem{ID: hitToolbarDockHide, Label: m.Hide, Icon: lucideIcons[iconEyeOff]})
	}
	rows.separate()
	rows.add(components.ContextMenuItem{ID: hitToolbarDockCustomize, Label: m.Customize, Icon: lucideIcons[iconSliders]})
	rows.add(components.ContextMenuItem{ID: hitToolbarDockReset, Label: m.ResetLayout, Icon: lucideIcons[iconRefresh]})
	width, height := rows.size(canvas, state.contextMenuTheme())
	bounds := state.popupAt(canvas, viewport, dock.menuAt, width, height, 6)
	menu := components.BeginContextMenu(canvas, state.contextMenuTheme(), bounds, components.ContextMenuOptions{Layer: dialogLayer, Scope: dialogScope, DismissID: hitToolbarDockDismiss, Viewport: viewport, Scroll: state.menuScrollFor(hitToolbarDockDismiss), RowHeight: contextMenuRow})
	state.menuPlaced(menu.Bounds())
	rows.paint(&menu, nil)
	menu.End()
}

func (state *State) clickToolbarDockMenu(id ui.ID) bool {
	dock := &state.toolbarDock
	if id == hitToolbarDockMore && !dock.menu {
		dock.menu, dock.overflow = true, true
		dock.menuAt = ui.Point{X: max(state.viewport.Width-200, 0), Y: windowBarHeight}
		return true
	}
	if !dock.menu {
		return false
	}
	dock.menu = false
	source := dock.menuTool
	switch id {
	case hitToolbarDockTop:
		state.moveToolbarDockTool(source, toolbarDockTop, ui.ID{})
	case hitToolbarDockSideTop:
		state.moveToolbarDockTool(source, toolbarDockSideTop, ui.ID{})
	case hitToolbarDockSideBottom:
		state.moveToolbarDockTool(source, toolbarDockSideBottom, ui.ID{})
	case hitToolbarDockLeftTop:
		state.moveToolbarDockTool(source, toolbarDockLeftTop, ui.ID{})
	case hitToolbarDockLeftBottom:
		state.moveToolbarDockTool(source, toolbarDockLeftBottom, ui.ID{})
	case hitToolbarDockBottom:
		state.moveToolbarDockTool(source, toolbarDockBottom, ui.ID{})
	case hitToolbarDockBottomRight:
		state.moveToolbarDockTool(source, toolbarDockBottomRight, ui.ID{})
	case hitToolbarDockHide:
		if entry, ok := toolbarDockIdentity(source); ok {
			for i := range state.toolbars.customize(entry.Group) {
				if state.toolbars.layouts[entry.Group][i].Key == entry.Key {
					state.toolbars.layouts[entry.Group][i].Enabled = false
				}
			}
			state.toolbarDockChanged()
		}
	case hitToolbarDockCustomize:
		group := toolbarWindow
		if entry, ok := toolbarDockIdentity(source); ok && !dock.overflow {
			group = entry.Group
		}
		state.openToolbarSettings(group)
	case hitToolbarDockReset:
		state.toolbars.dock = nil
		state.toolbars.layouts[toolbarWindow], state.toolbars.layouts[toolbarTerminal] = nil, nil
		state.terminalToolbarSize = terminalToolbarSmall
		state.toolbarDockChanged()
	case hitToolbarDockDismiss:
	default:
		if dock.overflow {
			return false
		}
	}
	return true
}
