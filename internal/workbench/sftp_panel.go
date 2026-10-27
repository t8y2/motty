package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/ui"
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
	state.contextRect = ui.Rect{}
	state.fitWorkspaceTerminals(id)
	state.fitSFTP(state.viewport)
	state.save()
}

// Share the same geometry for painting and pointer resize. AI keeps the outer
// right edge, so a right-docked files panel ends before the AI panel's divider.
func (state *State) sftpPanelResizable(canvas *ui.Canvas, size ui.Size, context layoutcore.ContextState) components.Resizable {
	base := state.baseWorkspacePixelBounds(size)
	width := state.contextPanelExtent(base, context.Width)
	minimum := state.contextPanelExtent(base, layoutcore.MinContextWidth)
	maximum := state.contextPanelExtent(base, layoutcore.MaxContextWidth)
	base.Width -= state.aiReserve(state.deck.ActiveWorkspace(), size)
	usable := max(base.Width-1, 1)
	fraction, least, most := width/usable, minimum/usable, maximum/usable
	if context.Side == layoutcore.ContextRight {
		fraction, least, most = 1-fraction, 1-most, 1-least
	}
	return components.BeginResizable(canvas, state.Theme(), hitContextDivider, base, fraction).
		Limits(least, most).HandleSize(paneDividerHitWidth)
}
