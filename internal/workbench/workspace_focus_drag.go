package workbench

import (
	"github.com/vivaui/viva/gfx"
	layoutcore "motty/internal/workspace"
	"slices"
)

type workspaceFocusDrag struct {
	workspace layoutcore.WorkspaceID
	pane      layoutcore.PaneID
	revision  uint64
	start, at gfx.Point
	active    bool
	boundary  int
}

func (state *State) workspaceFocusDragValid() bool {
	drag := &state.focusTabs.drag
	revision, ok := state.deck.Revision(drag.workspace)
	return ok && revision == drag.revision && drag.workspace == state.deck.ActiveWorkspace() &&
		state.deck.PaneMaximized(drag.workspace) && state.terminalPage() && !state.dialogOrMenuOpen()
}

func (state *State) workspaceFocusTabsPointer(event gfx.PointerEvent) (gfx.EventResult, bool) {
	drag := &state.focusTabs.drag
	if drag.pane != 0 && !state.workspaceFocusDragValid() {
		*drag = workspaceFocusDrag{}
	}
	switch event.Kind {
	case gfx.PointerDown:
		*drag = workspaceFocusDrag{}
		workspace := state.deck.ActiveWorkspace()
		if !state.terminalPage() || state.dialogOrMenuOpen() || !state.deck.PaneMaximized(workspace) ||
			event.Target.Scope() != hitWorkspaceFocusTab || event.ClickCount == 2 ||
			(event.Button != gfx.PointerButtonPrimary && event.Button != gfx.PointerButtonNone) {
			break
		}
		pane := layoutcore.PaneID(item(event.Target))
		if _, ok := state.deck.Pane(workspace, pane); !ok {
			break
		}
		revision, _ := state.deck.Revision(workspace)
		at := gfx.Point{X: event.X, Y: event.Y}
		*drag = workspaceFocusDrag{workspace: workspace, pane: pane, revision: revision, start: at, at: at, boundary: -1}
	case gfx.PointerMove:
		if drag.pane == 0 {
			break
		}
		drag.at = gfx.Point{X: event.X, Y: event.Y}
		dx, dy := event.X-drag.start.X, event.Y-drag.start.Y
		if !drag.active && dx*dx+dy*dy < tabDragThreshold*tabDragThreshold {
			break
		}
		drag.active = true
		drag.boundary = state.workspaceFocusDropBoundary(drag.at)
		return gfx.EventResult{Handled: true, Repaint: true}, true
	case gfx.PointerUp:
		finished := *drag
		*drag = workspaceFocusDrag{}
		if !finished.active {
			break
		}
		boundary := state.workspaceFocusDropBoundary(gfx.Point{X: event.X, Y: event.Y})
		state.reorderWorkspaceFocusTab(finished.workspace, finished.pane, boundary)
		return gfx.EventResult{Handled: true, Repaint: true, Focus: state.activePaneContentFocus()}, true
	case gfx.PointerExit:
		active := drag.active
		*drag = workspaceFocusDrag{}
		if active {
			return gfx.EventResult{Handled: true, Repaint: true}, true
		}
	}
	return gfx.EventResult{}, false
}

func (state *State) workspaceFocusDropBoundary(at gfx.Point) int {
	frame := state.workspaceFocusTabsBounds(state.deck.ActiveWorkspace(), state.viewport)
	if !pointInRect(at.X, at.Y, frame) {
		return -1
	}
	for index := range state.panes {
		bounds := state.focusTabs.bounds[index]
		if at.X < bounds.X+bounds.Width/2 {
			return index
		}
	}
	return len(state.panes)
}

func (state *State) reorderWorkspaceFocusTab(workspace layoutcore.WorkspaceID, source layoutcore.PaneID, boundary int) {
	panes := state.deck.Panes(workspace, nil)
	from := slices.IndexFunc(panes, func(p layoutcore.Pane) bool { return p.ID == source })
	if from < 0 || boundary < 0 || boundary > len(panes) {
		return
	}
	to := boundary
	if to > from {
		to--
	}
	if !moveWorkspaceItem(panes, from, to) {
		return
	}
	order := make([]layoutcore.PaneID, len(panes))
	for index, pane := range panes {
		order[index] = pane.ID
	}
	layout, ok := state.deck.Layout(workspace)
	if !ok {
		return
	}
	// 必须使用实际分屏比例，布局预设只描述形状。
	spec := layout.Spec()
	tabbed := state.workspaceTabbed(workspace)
	if err := state.deck.SetLayout(workspace, order, spec); err != nil {
		return
	}
	if tabbed {
		spec = layoutcore.LayoutSpec{Kind: layoutcore.LayoutTabs}
	}
	// 调整布局不应把普通终端标签转换成带工具栏的工作区。
	if state.workspaceManaged(workspace) {
		state.managedWorkspaceLayouts[workspace] = spec
	}
	if _, ok := state.temporaryWorkspaceLayouts[workspace]; ok {
		state.temporaryWorkspaceLayouts[workspace] = spec
	}
	state.focusTabs.pane = 0
	state.rebuildManagedWorkspace(workspace)
}
