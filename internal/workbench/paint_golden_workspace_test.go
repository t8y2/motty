package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
)

// The workspace surfaces that moved to page declarations: the library with an
// expanded template's cards, the arrangement editor, the template card's inline
// command editor, and the draft's expanded command field.
func init() {
	extraPaintScenes = append(extraPaintScenes,
		paintScene{name: "workspace-library", sizes: []gfx.Size{paintDesktop, paintSmall}, open: func(t *testing.T) (*State, func(*gfx.Canvas, gfx.Size)) {
			state := newTestState(t, testOptions(t))
			state.activeSection = sectionWorkspaces
			template := mustWorkspaceTemplate(t, "Production", workspaceCatalogIDs(state, 0, 1, 2), 2)
			state.templates = []WorkspaceTemplate{template}
			state.selectedWorkspaceTemplateID = template.ID
			state.workspaceLibraryExpanded = true
			return state, func(c *gfx.Canvas, size gfx.Size) { state.Paint(c, size) }
		}},
		paintScene{name: "workspace-arrangement", sizes: []gfx.Size{paintDesktop, paintSmall}, open: func(t *testing.T) (*State, func(*gfx.Canvas, gfx.Size)) {
			state := newTestState(t, testOptions(t))
			launchTemporaryWorkspace(t, state, 0, 1, 2)
			state.openWorkspaceArrangement()
			return state, func(c *gfx.Canvas, size gfx.Size) { state.Paint(c, size) }
		}},
		paintScene{name: "workspace-command-inline", sizes: []gfx.Size{paintDesktop, paintSmall}, open: func(t *testing.T) (*State, func(*gfx.Canvas, gfx.Size)) {
			state := templateManagementState(t, 1)
			host := state.templates[0].HostIDs[0]
			state.templates[0].Commands = map[string]WorkspaceCommand{host: {Command: "cd /srv"}}
			state.Click(hitWorkspaceInlineEdit.At(0))
			return state, func(c *gfx.Canvas, size gfx.Size) { state.Paint(c, size) }
		}},
		paintScene{name: "workspace-draft-command", sizes: []gfx.Size{paintDesktop, paintSmall}, open: func(t *testing.T) (*State, func(*gfx.Canvas, gfx.Size)) {
			state := newTestState(t, testOptions(t))
			state.activeSection = sectionWorkspaces
			template := mustWorkspaceTemplate(t, "Production", workspaceCatalogIDs(state, 0, 1), 2)
			template.Commands = map[string]WorkspaceCommand{state.hosts[0].CatalogID: {Command: "cd /srv"}}
			state.templates = []WorkspaceTemplate{template}
			state.openWorkspaceDraftEdit(template.ID)
			state.workspaceDraft.commandHost = state.hosts[0].ID
			return state, func(c *gfx.Canvas, size gfx.Size) { state.Paint(c, size) }
		}},
	)
}
