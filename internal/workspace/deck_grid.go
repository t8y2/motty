package workspace

// SetLayout replaces one workspace's split tree with spec applied to the panes
// in order. The order must contain every pane exactly once; pane IDs, their session
// bindings, and the active pane remain unchanged. Nothing changes on error.
func (deck *Deck) SetLayout(workspaceID WorkspaceID, order []PaneID, spec LayoutSpec) error {
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
	layout, err := NewLayout(sessions, spec)
	if err != nil {
		return err
	}
	workspace.layout = layout
	workspace.revision++
	return nil
}

// SwapPanes exchanges the sessions of two panes of one workspace, leaving the
// split tree as it is. The active pane follows its session.
func (deck *Deck) SwapPanes(workspaceID WorkspaceID, a, b PaneID) error {
	workspace, ok := deck.workspace(workspaceID)
	if !ok {
		return ErrWorkspaceNotFound
	}
	first, okA := workspace.pane(a)
	second, okB := workspace.pane(b)
	if !okA || !okB {
		return ErrPaneNotFound
	}
	if a == b {
		return ErrInvalidLayout
	}
	first.SessionID, second.SessionID = second.SessionID, first.SessionID
	switch workspace.activePane {
	case a:
		workspace.activePane = b
	case b:
		workspace.activePane = a
	}
	workspace.revision++
	return nil
}
