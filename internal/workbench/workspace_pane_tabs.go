package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	layoutcore "motty/internal/workspace"
)

// workspacePaneTabsHeight is the strip of tabs a one-pane-at-a-time workspace
// keeps above its terminal.
const workspacePaneTabsHeight = float32(36)

// workspacePaneTabWidth is how wide each tab is while the strip has room.
const workspacePaneTabWidth = float32(160)

var hitWorkspacePaneTabs = tree.Sub(workspaceHeaderHits, tree.Name("pane-tabs"))

// workspaceTabbed reports whether the workspace shows one pane at a time.
func (state *State) workspaceTabbed(workspaceID layoutcore.WorkspaceID) bool {
	return state.managedWorkspaceLayouts[workspaceID].Tabbed()
}

// paintWorkspacePaneTabs draws the strip that picks the pane a tabbed workspace
// shows. The panes keep running, and keep their size, while another is in front.
func (state *State) paintWorkspacePaneTabs(canvas *gfx.Canvas, frame gfx.Rect) {
	workspaceID := state.deck.ActiveWorkspace()
	if !state.workspaceTabbed(workspaceID) || state.activeSection != sectionTerminal {
		return
	}
	labels, active := state.workspacePaneTabLabels[:0], -1
	for index, pane := range state.panes {
		label := state.messages.LabelNewTab
		if session, ok := state.sessionByIDPointer(pane.SessionID); ok {
			label = session.Title
		}
		labels = append(labels, label)
		if pane.Active {
			active = index
		}
	}
	state.workspacePaneTabLabels = labels
	bounds := gfx.Rect{X: frame.X, Y: frame.Y - workspacePaneTabsHeight, Width: frame.Width, Height: workspacePaneTabsHeight}
	canvas.Fill(bounds, state.pal.workspace)
	canvas.Fill(gfx.Rect{X: bounds.X, Y: bounds.Y + bounds.Height - 1, Width: bounds.Width, Height: 1}, state.pal.line)
	list := bounds.Inset(gfx.Insets{Top: 3, Right: 10, Bottom: 4, Left: 10})
	list.Width = min(list.Width, float32(len(labels))*workspacePaneTabWidth)
	components.BeginTabList(canvas, state.Theme(), list, hitWorkspacePaneTabs, labels, active).Line().Paint()
}
