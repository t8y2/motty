package workbench

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
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

// declareWorkspacePaneTabs declares the strip that picks the pane a tabbed
// workspace shows. The panes keep running, and keep their size, while another
// is in front. The strip is a page Tabs: a click or the arrow keys move its
// choice, and the paint that found a new choice makes that pane active.
func (state *State) declareWorkspacePaneTabs(c ui.C, frame gfx.Rect) {
	workspaceID := state.deck.ActiveWorkspace()
	if state.deck.PaneMaximized(workspaceID) || !state.workspaceTabbed(workspaceID) || state.activeSection != sectionTerminal {
		return
	}
	labels, indices := state.workspacePaneTabLabels[:0], state.workspacePaneTabIndices[:0]
	active := -1
	for index, pane := range state.panes {
		label := state.messages.LabelNewTab
		if session, ok := state.sessionByIDPointer(pane.SessionID); ok {
			label = session.Title
		}
		labels = append(labels, label)
		indices = append(indices, index)
		if pane.Active {
			active = index
		}
	}
	state.workspacePaneTabLabels, state.workspacePaneTabIndices = labels, indices
	if active < 0 {
		return
	}
	state.workspacePaneTabChoice = active
	width := float32(len(labels)) * workspacePaneTabWidth
	width = min(width, max(frame.Width-20, 0))
	pal := state.pal
	strip := ui.Stack(c).Absolute().
		Left(frame.X).Top(frame.Y - workspacePaneTabsHeight).Width(frame.Width).Height(workspacePaneTabsHeight).
		Background(pal.workspace)
	strip.Draw(func(d *ui.DrawContext) {
		d.Canvas.Fill(gfx.Rect{X: d.Rect.X, Y: d.Rect.Y + d.Rect.Height - 1, Width: d.Rect.Width, Height: 1}, pal.line)
	})
	strip.Children(func() {
		ui.Row(c).Fill().PadSides(3, 10, 4, 10).Items(ui.Start).Children(func() {
			ui.Tabs(c, &state.workspacePaneTabChoice, state.workspacePaneTabIndices, state.workspacePaneTabLabel).
				Line().Width(width)
		})
	})
	if choice := state.workspacePaneTabChoice; choice != active && choice >= 0 && choice < len(state.panes) {
		pane := state.panes[choice].ID
		c.After(func() { state.activatePane(workspaceID, pane) })
	}
}

// workspacePaneTabLabel names one tab of the strip by its pane index.
func (state *State) workspacePaneTabLabel(index int) string {
	if index < 0 || index >= len(state.workspacePaneTabLabels) {
		return ""
	}
	return state.workspacePaneTabLabels[index]
}
