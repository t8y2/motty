package workspace

import "fmt"

// DeckDTOVersion is the stable multi-workspace persistence format.
const DeckDTOVersion uint16 = 1

// DeckDTO is the persistence boundary for workspace tabs and pane bindings.
type DeckDTO struct {
	Version         uint16         `json:"version"`
	ActiveWorkspace WorkspaceID    `json:"active_workspace,omitempty"`
	Workspaces      []WorkspaceDTO `json:"workspaces,omitempty"`
}

// WorkspaceDTO stores one independent split tree.
type WorkspaceDTO struct {
	ID         WorkspaceID     `json:"id"`
	Title      string          `json:"title"`
	ActivePane PaneID          `json:"active_pane,omitempty"`
	Layout     DTO             `json:"layout"`
	Panes      []PaneDTO       `json:"panes,omitempty"`
	Context    ContextStateDTO `json:"context,omitempty"`
}

// ContextStateDTO persists one workspace's shared panel presentation.
type ContextStateDTO struct {
	Kind  ContextKind `json:"kind,omitempty"`
	Open  bool        `json:"open,omitempty"`
	Width float32     `json:"width,omitempty"`
	Side  ContextSide `json:"side,omitempty"`
}

// PaneDTO stores one stable pane and its optional session association.
type PaneDTO struct {
	ID        PaneID    `json:"id"`
	SessionID SessionID `json:"session,omitempty"`
}

// DTO returns a deep, deterministic snapshot in workspace and visual pane
// order. Runtime slot generations are canonicalized by Layout.DTO.
func (deck *Deck) DTO() DeckDTO {
	dto := DeckDTO{Version: DeckDTOVersion, ActiveWorkspace: deck.activeWorkspace}
	if len(deck.workspaces) == 0 {
		return dto
	}
	dto.Workspaces = make([]WorkspaceDTO, len(deck.workspaces))
	for index := range deck.workspaces {
		workspace := &deck.workspaces[index]
		entry := WorkspaceDTO{
			ID: workspace.id, Title: workspace.title, ActivePane: workspace.activePane,
			Layout:  workspace.layout.DTO(),
			Context: ContextStateDTO{Kind: workspace.context.Kind, Open: workspace.context.Open, Width: normalizeContext(workspace.context).Width, Side: workspace.context.Side},
		}
		visual := workspace.layout.Sessions(nil)
		entry.Panes = make([]PaneDTO, 0, len(visual))
		for _, item := range visual {
			pane, _ := workspace.pane(PaneID(item))
			entry.Panes = append(entry.Panes, PaneDTO{ID: pane.ID, SessionID: pane.SessionID})
		}
		dto.Workspaces[index] = entry
	}
	return dto
}

// RestoreDeck validates and reconstructs a deck. Callers can safely fall back
// to a zero Deck when this returns an error; no partial state is returned.
func RestoreDeck(dto DeckDTO) (Deck, error) {
	if err := dto.Validate(); err != nil {
		return Deck{}, err
	}
	deck := Deck{activeWorkspace: dto.ActiveWorkspace}
	deck.workspaces = make([]workspaceRecord, len(dto.Workspaces))
	for index := range dto.Workspaces {
		entry := dto.Workspaces[index]
		layout, err := Restore(entry.Layout)
		if err != nil {
			return Deck{}, err
		}
		workspace := workspaceRecord{
			id: entry.ID, title: entry.Title, activePane: entry.ActivePane, layout: layout,
			panes:    make([]Pane, len(entry.Panes)),
			context:  normalizeContext(ContextState{Kind: entry.Context.Kind, Open: entry.Context.Open, Width: entry.Context.Width, Side: entry.Context.Side}),
			revision: 1,
		}
		for paneIndex, pane := range entry.Panes {
			workspace.panes[paneIndex] = Pane{ID: pane.ID, SessionID: pane.SessionID}
			deck.nextPane = max(deck.nextPane, pane.ID)
		}
		deck.nextWorkspace = max(deck.nextWorkspace, entry.ID)
		deck.workspaces[index] = workspace
	}
	if err := deck.Validate(); err != nil {
		return Deck{}, err
	}
	return deck, nil
}

// Validate rejects malformed, oversized, duplicated, disconnected, or
// inconsistently bound workspace persistence.
func (dto DeckDTO) Validate() error {
	if dto.Version != DeckDTOVersion || len(dto.Workspaces) > MaxWorkspaces {
		return fmt.Errorf("%w: invalid deck header", ErrInvalidLayout)
	}
	if len(dto.Workspaces) == 0 {
		if dto.ActiveWorkspace != 0 {
			return fmt.Errorf("%w: empty deck has active workspace", ErrInvalidLayout)
		}
		return nil
	}
	workspaceIDs := make(map[WorkspaceID]struct{}, len(dto.Workspaces))
	paneIDs := make(map[PaneID]struct{})
	sessionIDs := make(map[SessionID]struct{})
	activeWorkspace := false
	for _, workspace := range dto.Workspaces {
		if workspace.ID == 0 || len(workspace.Panes) > MaxPanes {
			return ErrInvalidLayout
		}
		if _, exists := workspaceIDs[workspace.ID]; exists {
			return ErrInvalidLayout
		}
		workspaceIDs[workspace.ID] = struct{}{}
		if workspace.ID == dto.ActiveWorkspace {
			activeWorkspace = true
		}
		if err := workspace.Layout.Validate(); err != nil {
			return err
		}
		if err := validateContext(ContextState{Kind: workspace.Context.Kind, Open: workspace.Context.Open, Width: workspace.Context.Width, Side: workspace.Context.Side}); err != nil {
			return err
		}
		if len(workspace.Layout.Nodes) != 0 && len(workspace.Panes)*2-1 != len(workspace.Layout.Nodes) || len(workspace.Layout.Nodes) == 0 && len(workspace.Panes) != 0 {
			return ErrInvalidLayout
		}
		activePane := workspace.ActivePane == 0 && len(workspace.Panes) == 0
		listed := make(map[PaneID]struct{}, len(workspace.Panes))
		for _, pane := range workspace.Panes {
			if pane.ID == 0 {
				return ErrInvalidLayout
			}
			if _, exists := paneIDs[pane.ID]; exists {
				return ErrInvalidLayout
			}
			paneIDs[pane.ID] = struct{}{}
			listed[pane.ID] = struct{}{}
			if pane.ID == workspace.ActivePane {
				activePane = true
			}
			if pane.SessionID != 0 {
				if _, exists := sessionIDs[pane.SessionID]; exists {
					return ErrInvalidLayout
				}
				sessionIDs[pane.SessionID] = struct{}{}
			}
		}
		for _, node := range workspace.Layout.Nodes {
			if node.Kind != KindLeaf {
				continue
			}
			if _, ok := listed[PaneID(node.Session)]; !ok {
				return ErrInvalidLayout
			}
		}
		if !activePane {
			return ErrInvalidLayout
		}
	}
	if !activeWorkspace {
		return ErrInvalidLayout
	}
	return nil
}
