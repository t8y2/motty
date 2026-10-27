package workspace

import "fmt"

// ExportWorkspace returns one tab as persistence data, in the form Import takes.
func (deck *Deck) ExportWorkspace(id WorkspaceID) (WorkspaceDTO, bool) {
	for _, workspace := range deck.DTO().Workspaces {
		if workspace.ID == id {
			return workspace, true
		}
	}
	return WorkspaceDTO{}, false
}

// Import adds a tab from another deck, or from ExportWorkspace, at index among
// the tabs (past the last is the last) and makes it active. The tab and every
// pane get fresh identifiers in this deck. sessions says what each pane's
// session is called here (a session absent from it, or mapped to zero, leaves
// its pane empty), and a session that is already in this deck is refused.
func (deck *Deck) Import(dto WorkspaceDTO, sessions map[SessionID]SessionID, index int) (WorkspaceID, error) {
	if len(deck.workspaces) >= MaxWorkspaces {
		return 0, ErrWorkspaceFull
	}
	if err := (DeckDTO{Version: DeckDTOVersion, ActiveWorkspace: dto.ID, Workspaces: []WorkspaceDTO{dto}}).Validate(); err != nil {
		return 0, err
	}
	// New pane identifiers, and the layout's leaves renamed to them.
	paneIDs := make(map[PaneID]PaneID, len(dto.Panes))
	next := deck.nextPane
	for _, pane := range dto.Panes {
		next++
		paneIDs[pane.ID] = next
	}
	layout := dto.Layout
	layout.Nodes = append([]NodeDTO(nil), layout.Nodes...)
	for index := range layout.Nodes {
		if layout.Nodes[index].Kind == KindLeaf {
			renamed, ok := paneIDs[PaneID(layout.Nodes[index].Session)]
			if !ok {
				return 0, fmt.Errorf("%w: pane %d is not in the tab", ErrInvalidLayout, layout.Nodes[index].Session)
			}
			layout.Nodes[index].Session = SessionID(renamed)
		}
	}
	restored, err := Restore(layout)
	if err != nil {
		return 0, err
	}
	for _, pane := range dto.Panes {
		if session := sessions[pane.SessionID]; pane.SessionID != 0 && session != 0 {
			for _, existing := range deck.workspaces {
				for _, held := range existing.panes {
					if held.SessionID == session {
						return 0, ErrSessionExists
					}
				}
			}
		}
	}
	deck.nextPane = next
	deck.nextWorkspace++
	workspace := workspaceRecord{
		id: deck.nextWorkspace, title: dto.Title, layout: restored, revision: 1,
		context: normalizeContext(ContextState{Kind: dto.Context.Kind, Open: dto.Context.Open, Width: dto.Context.Width, Side: dto.Context.Side}),
	}
	for _, pane := range dto.Panes {
		entry := Pane{ID: paneIDs[pane.ID]}
		if pane.SessionID != 0 {
			entry.SessionID = sessions[pane.SessionID]
		}
		workspace.panes = append(workspace.panes, entry)
	}
	workspace.activePane = paneIDs[dto.ActivePane]
	if workspace.activePane == 0 && len(workspace.panes) > 0 {
		workspace.activePane = workspace.panes[0].ID
	}
	index = min(max(index, 0), len(deck.workspaces))
	deck.workspaces = append(deck.workspaces, workspaceRecord{})
	copy(deck.workspaces[index+1:], deck.workspaces[index:])
	deck.workspaces[index] = workspace
	deck.activeWorkspace = workspace.id
	return workspace.id, nil
}
