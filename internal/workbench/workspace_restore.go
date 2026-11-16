package workbench

import layoutcore "motty/internal/workspace"

// Workspace identity belongs to this window's restore state, alongside its
// deck. It is independent of template names and of whether sessions are live.
type savedWorkspaceLayout struct {
	ID layoutcore.WorkspaceID `json:"id"`
	// Layout is the recorded arrangement. Files from before layouts existed
	// hold only Columns, a grid width, which stands for GridSpec(Columns). A zero
	// Layout is left out when written: it has no kind to name, and encoding it
	// fails, which made every save fail while such a file was still stored.
	Layout    layoutcore.LayoutSpec `json:"layout,omitzero"`
	Columns   uint8                 `json:"columns,omitempty"`
	Temporary bool                  `json:"temporary,omitempty"`
}

func (state *State) savedWorkspaceLayouts() []savedWorkspaceLayout {
	layouts := make([]savedWorkspaceLayout, 0, len(state.managedWorkspaceLayouts))
	for _, workspace := range state.deck.Workspaces(nil) {
		if _, managed := state.managedWorkspaceLayouts[workspace.ID]; !managed {
			continue
		}
		_, temporary := state.temporaryWorkspaceLayouts[workspace.ID]
		layouts = append(layouts, savedWorkspaceLayout{ID: workspace.ID, Layout: state.workspaceLayoutSpec(workspace.ID), Temporary: temporary})
	}
	return layouts
}

func (state *State) restoreWorkspaceLayouts(layouts []savedWorkspaceLayout) {
	clear(state.managedWorkspaceLayouts)
	clear(state.temporaryWorkspaceLayouts)
	if layouts == nil {
		state.restoreLegacyWorkspaceLayouts()
		return
	}
	for _, layout := range layouts {
		spec := layout.Layout
		if spec.Kind == 0 && layout.Columns != 0 {
			spec = layoutcore.GridSpec(layout.Columns)
		}
		live, exists := state.deck.Layout(layout.ID)
		if !exists || spec.Validate(live.Len()) != nil {
			continue
		}
		state.managedWorkspaceLayouts[layout.ID] = spec
		if layout.Temporary {
			state.temporaryWorkspaceLayouts[layout.ID] = spec
		}
	}
}

func (state *State) restoreLegacyWorkspaceLayouts() {
	// Older snapshots retained only the split tree. Treat their multi-terminal
	// tabs as temporary workspaces without rebuilding or flattening that tree.
	for _, workspace := range state.deck.Workspaces(nil) {
		count := len(state.deck.Panes(workspace.ID, nil))
		if count > 1 {
			spec := DefaultWorkspaceLayout(count)
			state.managedWorkspaceLayouts[workspace.ID] = spec
			state.temporaryWorkspaceLayouts[workspace.ID] = spec
		}
	}
}
