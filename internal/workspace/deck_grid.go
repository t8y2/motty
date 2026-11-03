package workspace

// SetGrid replaces one workspace's split tree with a row-major grid. The
// order must contain every pane exactly once; pane IDs, their session
// bindings, and the active pane remain unchanged. Nothing changes on error.
func (deck *Deck) SetGrid(workspaceID WorkspaceID, order []PaneID, columns int) error {
	workspace, ok := deck.workspace(workspaceID)
	if !ok {
		return ErrWorkspaceNotFound
	}
	if len(order) != len(workspace.panes) {
		return ErrInvalidLayout
	}
	seen := make(map[PaneID]struct{}, len(order))
	sessions := make([]SessionID, len(order))
	for index, paneID := range order {
		if _, ok := workspace.pane(paneID); !ok {
			return ErrPaneNotFound
		}
		if _, exists := seen[paneID]; exists {
			return ErrInvalidLayout
		}
		seen[paneID] = struct{}{}
		sessions[index] = SessionID(paneID)
	}
	layout, err := NewGrid(sessions, columns)
	if err != nil {
		return err
	}
	workspace.layout = layout
	workspace.revision++
	return nil
}
