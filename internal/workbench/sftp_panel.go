package workbench

import (
	"github.com/vivaui/viva/gfx"
	layoutcore "motty/internal/workspace"
)

var hitSFTPPanelSide = tree.ID(sftpHits, "panel-side")

func sftpPanelSideString(side layoutcore.ContextSide) string {
	if side == layoutcore.ContextRight {
		return "right"
	}
	return ""
}

// Defaults initialize a workspace once. Selecting a tab, reconnecting or adding
// a split must not overwrite the presentation already chosen for it.
// A cold application restore deliberately starts from the configured defaults.
func (state *State) initializeSFTPPanel(id layoutcore.WorkspaceID, restoring bool) {
	context, ok := state.deck.Context(id)
	if !ok || context.Kind != layoutcore.ContextNone && (!restoring || context.Kind != layoutcore.ContextSFTP) {
		return
	}
	pane, _ := state.deck.Pane(id, state.deck.ActivePane(id))
	session, capable := state.sessionByIDPointer(uint32(pane.SessionID))
	capable = capable && session.canBrowseFiles()
	if !capable && context.Kind == layoutcore.ContextNone {
		return // an empty tab can initialize when a file-capable host is bound
	}
	context.Kind, context.Open, context.Side = layoutcore.ContextSFTP, capable && state.sftpPanelAutoOpen, state.sftpPanelSide
	if !restoring {
		if state.sftpPanelWidth > 0 {
			context.Width = state.sftpPanelWidth
		}
		if state.monitorWidth > 0 {
			context.MonitorWidth = state.monitorWidth
		}
	}
	_ = state.deck.SetContext(id, context)
}

func (state *State) sftpPanelMoveAction() (uint8, string) {
	context, _ := state.deck.Context(state.deck.ActiveWorkspace())
	if context.Side == layoutcore.ContextRight {
		return iconChevronLeft, state.messages.Tools.FilesMoveLeft
	}
	return iconChevronRight, state.messages.Tools.FilesMoveRight
}

func (state *State) toggleSFTPPanelSide() {
	id := state.deck.ActiveWorkspace()
	context, ok := state.deck.Context(id)
	if !ok || !context.Open || context.Kind != layoutcore.ContextSFTP {
		return
	}
	if context.Side == layoutcore.ContextLeft {
		context.Side = layoutcore.ContextRight
	} else {
		context.Side = layoutcore.ContextLeft
	}
	if state.deck.SetContext(id, context) != nil {
		return
	}
	state.draggedContextDivider = false
	state.contextRect = gfx.Rect{}
	state.fitWorkspaceTerminals(id)
	state.fitSFTP(state.viewport)
	state.save()
}

// contextPanelLayout is the geometry the docked files panel and its divider
// share: the workspace the panel sits in, its width and the widths it may take.
// AI keeps the outer right edge, so a right-docked files panel ends before the
// AI panel's divider.
func (state *State) contextPanelLayout(size gfx.Size, context layoutcore.ContextState) (base gfx.Rect, width, minimum, maximum float32) {
	base = state.baseWorkspacePixelBounds(size)
	width = state.contextPanelWidth(base, context)
	minimum = state.contextPanelExtent(base, layoutcore.MinContextWidth)
	maximum = state.contextPanelExtent(base, layoutcore.MaxContextWidth)
	base.Width -= state.aiReserve(state.deck.ActiveWorkspace(), size)
	base.Width -= state.independentMonitorReserve(state.deck.ActiveWorkspace(), size)
	if context.MonitorLeft {
		base.X += state.independentMonitorReserve(state.deck.ActiveWorkspace(), size)
	}
	return base, width, minimum, maximum
}

// contextPanelRect is where the docked panel sits in base, and handleX is the
// one-pixel divider between it and the workspace.
func contextPanelRect(base gfx.Rect, width float32, side layoutcore.ContextSide) (panel gfx.Rect, handleX float32) {
	panel = gfx.Rect{X: base.X, Y: base.Y, Width: width, Height: base.Height}
	handleX = base.X + width
	if side == layoutcore.ContextRight {
		panel.X = base.X + base.Width - width
		handleX = base.X + base.Width - 1 - width
	}
	return panel, handleX
}
