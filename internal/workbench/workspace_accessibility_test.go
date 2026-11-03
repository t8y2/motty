package workbench

import (
	"testing"

	"github.com/vivaui/viva/ui"
)

func TestWorkspaceLibraryKeyboardAndFocusSemantics(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	state.activeSection = sectionWorkspaces
	template := mustWorkspaceTemplate(t, "Production", []string{
		workspaceHostCatalogID(t, state, "Production"),
	}, 1)
	state.templates = []WorkspaceTemplate{template}

	var canvas ui.Canvas
	canvas.SetInteraction(ui.ID{}, ui.ID{}, hitWorkspaceTemplateRowList.At(0), true)
	state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
	row := panelHit(t, &canvas, hitWorkspaceTemplateRowList.At(0))
	if row.Role != ui.RoleButton || !row.Selected || !row.Expanded || row.Hint != state.messages.WorkspaceOpenHint {
		t.Fatalf("focused template row semantics = %+v", row)
	}

	result := state.Key(ui.KeyEvent{Key: ui.KeyEnter, Target: hitWorkspaceTemplateRowList.At(0)})
	if !result.Handled || len(world.configs) != 1 || state.activeSection != sectionTerminal {
		t.Fatalf("keyboard open = %+v dials=%d section=%d", result, len(world.configs), state.activeSection)
	}
}

func TestWorkspaceModalEscapePrecedenceAndDraftSemantics(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	state.activeSection = sectionWorkspaces
	state.Click(hitWorkspaceNew)

	var canvas ui.Canvas
	canvas.SetInteraction(ui.ID{}, ui.ID{}, hitWorkspaceDraftTemporary, true)
	state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
	mode := panelHit(t, &canvas, hitWorkspaceDraftTemporary)
	if mode.Role != ui.RoleRadio || !mode.Selected {
		t.Fatalf("selected draft mode = %+v", mode)
	}
	if host := panelHit(t, &canvas, workspaceDraftHostID(0)); host.Role != ui.RoleCheckbox {
		t.Fatalf("draft host role = %+v", host)
	}

	if result := state.Key(ui.KeyEvent{Key: ui.KeyEscape}); !result.Handled || state.modal != modalNone {
		t.Fatalf("Escape left modal=%d result=%+v", state.modal, result)
	}
	if result := state.Key(ui.KeyEvent{Key: ui.KeyEscape}); result.Handled {
		t.Fatalf("Escape was handled after the draft closed: %+v", result)
	}
}

func TestWorkspaceHeaderMenusRestoreTerminalFocusOnEscape(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	launchTemporaryWorkspace(t, state, 0, 1)
	paintAt(state, ui.Size{Width: 1400, Height: 900})

	state.Click(hitWorkspaceHeaderLayout)
	paintAt(state, ui.Size{Width: 1400, Height: 900})
	result := state.Key(ui.KeyEvent{Key: ui.KeyEscape})
	if !result.Handled || state.workspaceHeaderMenu != workspaceHeaderMenuNone ||
		result.Focus != state.activeTerminalFocusHit() {
		t.Fatalf("layout Escape = %+v menu=%d focus=%d want=%d",
			result, state.workspaceHeaderMenu, result.Focus, state.activeTerminalFocusHit())
	}

}
