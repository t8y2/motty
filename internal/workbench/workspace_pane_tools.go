package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

var (
	hitWorkspacePaneTools    = tree.ID(workspaceHeaderHits, "pane-tools")
	hitWorkspacePaneToolList = tree.Sub(workspaceHeaderHits, tree.Name("pane-tool"))
)

type workspacePaneTools struct {
	session uint32
	anchor  gfx.Rect
	items   [maxBarTools]barTool
	count   int
}

// Keep frequent session actions beside the name. Everything else keeps its
// configured order and availability in a menu, rather than squeezing the name.
// workspacePaneToolsCapacity is how many compact actions fit beside a pane
// title; the rail and the menu both count with it.
func workspacePaneToolsCapacity(width float32) int {
	return min(3, max(0, int((width-144)/(barToolSize+barToolGap))-1))
}

func (state *State) compactWorkspacePaneTools(tools []barTool, session uint32, width float32) []barTool {
	menu := &state.workspacePaneTools
	if menu.session != session {
		if state.workspaceHeaderMenu == workspaceHeaderMenuTools {
			state.workspaceHeaderMenu = workspaceHeaderMenuNone
		}
		menu.session = session
	}
	menu.count = 0
	capacity := workspacePaneToolsCapacity(width)
	var visible [maxBarTools]barTool
	count := 0
	for _, tool := range tools {
		// The latency readout is a status, not an action: it never disappears
		// into the compact pane-tools menu.
		if tool.id == hitToolbarLatency || (count < capacity && (tool.id == hitSFTPOpen || tool.id == hitRemoteStats || tool.id == hitPaneClose)) {
			tool.groupEnd = false
			visible[count] = tool
			count++
		} else {
			menu.items[menu.count] = tool
			menu.count++
		}
	}
	tools = append(tools[:0], visible[:count]...)
	if menu.count > 0 {
		tools = append(tools, barTool{id: hitWorkspacePaneTools, icon: iconEllipsis, hint: state.messages.WorkspacePaneTools, style: iconButtonQuiet})
	}
	return tools
}

// declareWorkspacePaneTools declares the compact pane-tools menu under its button.
func (state *State) declareWorkspacePaneTools(c ui.C) {
	menu := &state.workspacePaneTools
	if state.workspaceHeaderMenu != workspaceHeaderMenuTools {
		return
	}
	if menu.session != state.activeSessionID || menu.count == 0 || !state.workspaceManaged(state.deck.ActiveWorkspace()) {
		state.workspaceHeaderMenu = workspaceHeaderMenuNone
		return
	}
	var rows menuRows
	for index := menu.count - 1; index >= 0; index-- {
		tool := menu.items[index]
		rows.add(components.ContextMenuItem{ID: hitWorkspacePaneToolList.At(index), Label: tool.hint, Icon: lucideIcons[tool.icon], Disabled: tool.disabled})
	}
	open := true
	state.declareMenuAt(c, &open, menu.anchor, false, &rows, func(id gfx.ID) {
		if index, ok := id.In(hitWorkspacePaneToolList); ok {
			state.runWorkspacePaneTool(index)
		}
	})
	if !open {
		state.workspaceHeaderMenu = workspaceHeaderMenuNone
		// The keyboard goes back to the terminal the menu acted on.
		c.Canvas().RequestFocus(state.activeTerminalFocusHit())
	}
}

func (state *State) clickWorkspacePaneTools(id gfx.ID) bool {
	switch {
	case id == hitWorkspacePaneTools:
		if state.workspacePaneTools.count == 0 || state.workspacePaneTools.session != state.activeSessionID {
			return true
		}
		if state.workspaceHeaderMenu == workspaceHeaderMenuTools {
			state.workspaceHeaderMenu = workspaceHeaderMenuNone
		} else {
			state.workspaceHeaderMenu = workspaceHeaderMenuTools
		}
	default:
		return false
	}
	return true
}

// runWorkspacePaneTool runs a choice of the menu, which closed with it.
func (state *State) runWorkspacePaneTool(index int) {
	menu := &state.workspacePaneTools
	if menu.session != state.activeSessionID || index < 0 || index >= menu.count {
		return
	}
	if tool := menu.items[index]; !tool.disabled {
		state.Click(tool.id)
	}
}
