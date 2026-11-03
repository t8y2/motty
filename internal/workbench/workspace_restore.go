package workbench

import layoutcore "motty/internal/workspace"

// Workspace identity belongs to this window's restore state, alongside its
// deck. It is independent of template names and of whether sessions are live.
type savedWorkspaceLayout struct {
	ID        layoutcore.WorkspaceID `json:"id"`
	Columns   uint8                  `json:"columns"`
	Temporary bool                   `json:"temporary,omitempty"`
}

func (state *State) savedWorkspaceLayouts() []savedWorkspaceLayout {
	layouts := make([]savedWorkspaceLayout, 0, len(state.managedWorkspaceColumns))
	for _, workspace := range state.deck.Workspaces(nil) {
		columns, managed := state.managedWorkspaceColumns[workspace.ID]
		if !managed {
			continue
		}
		_, temporary := state.temporaryWorkspaceColumns[workspace.ID]
		layouts = append(layouts, savedWorkspaceLayout{ID: workspace.ID, Columns: columns, Temporary: temporary})
	}
	return layouts
}

func (state *State) restoreWorkspaceLayouts(layouts []savedWorkspaceLayout) {
	clear(state.managedWorkspaceColumns)
	clear(state.temporaryWorkspaceColumns)
	if layouts == nil {
		state.restoreLegacyWorkspaceLayouts()
		return
	}
	for _, layout := range layouts {
		if layout.Columns < layoutcore.MinGridColumns || int(layout.Columns) > layoutcore.MaxGridColumns {
			continue
		}
		if _, exists := state.deck.Layout(layout.ID); !exists {
			continue
		}
		state.managedWorkspaceColumns[layout.ID] = layout.Columns
		if layout.Temporary {
			state.temporaryWorkspaceColumns[layout.ID] = layout.Columns
		}
	}
}

func (state *State) restoreLegacyWorkspaceLayouts() {
	// Older snapshots retained only the split tree. Treat their multi-terminal
	// tabs as temporary workspaces without rebuilding or flattening that tree.
	for _, workspace := range state.deck.Workspaces(nil) {
		count := len(state.deck.Panes(workspace.ID, nil))
		if count > 1 {
			columns := DefaultWorkspaceColumns(count)
			state.managedWorkspaceColumns[workspace.ID] = columns
			state.temporaryWorkspaceColumns[workspace.ID] = columns
		}
	}
}
