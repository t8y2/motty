package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
)

func TestWorkspaceLibraryKeyboardAndFocusSemantics(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	state.activeSection = sectionWorkspaces
	template := mustWorkspaceTemplate(t, "Production", []string{
		workspaceHostCatalogID(t, state, "Production"),
	}, 1)
	state.templates = []WorkspaceTemplate{template}

	var canvas gfx.Canvas
	canvas.SetInteraction(gfx.ID{}, gfx.ID{}, hitWorkspaceTemplateRowList.At(0), true)
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	row := panelHit(t, &canvas, hitWorkspaceTemplateRowList.At(0))
	if row.Role != gfx.RoleButton || !row.Selected || !row.Expanded || row.Hint != state.messages.WorkspaceOpenHint {
		t.Fatalf("focused template row semantics = %+v", row)
	}

	result := state.Key(gfx.KeyEvent{Key: gfx.KeyEnter, Target: hitWorkspaceTemplateRowList.At(0)})
	if !result.Handled || len(world.configs) != 1 || state.activeSection != sectionTerminal {
		t.Fatalf("keyboard open = %+v dials=%d section=%d", result, len(world.configs), state.activeSection)
	}
}

func TestWorkspaceModalEscapePrecedenceAndDraftSemantics(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	state.activeSection = sectionWorkspaces
	state.Click(hitWorkspaceNew)

	var canvas gfx.Canvas
	canvas.SetInteraction(gfx.ID{}, gfx.ID{}, hitWorkspaceDraftTemporary, true)
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	mode := panelHit(t, &canvas, hitWorkspaceDraftTemporary)
	if mode.Role != gfx.RoleRadio || !mode.Selected {
		t.Fatalf("selected draft mode = %+v", mode)
	}
	if host := panelHit(t, &canvas, workspaceDraftHostID(0)); host.Role != gfx.RoleCheckbox {
		t.Fatalf("draft host role = %+v", host)
	}

	if result := state.Key(gfx.KeyEvent{Key: gfx.KeyEscape}); !result.Handled || state.modal != modalNone {
		t.Fatalf("Escape left modal=%d result=%+v", state.modal, result)
	}
	if result := state.Key(gfx.KeyEvent{Key: gfx.KeyEscape}); result.Handled {
		t.Fatalf("Escape was handled after the draft closed: %+v", result)
	}
}

func TestWorkspaceHeaderMenusRestoreTerminalFocusOnEscape(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	launchTemporaryWorkspace(t, state, 0, 1)
	paintAt(state, gfx.Size{Width: 1400, Height: 900})

	state.Click(hitWorkspaceHeaderLayout)
	paintAt(state, gfx.Size{Width: 1400, Height: 900})
	result := state.Key(gfx.KeyEvent{Key: gfx.KeyEscape})
	if !result.Handled || state.workspaceHeaderMenu != workspaceHeaderMenuNone ||
		result.Focus != state.activeTerminalFocusHit() {
		t.Fatalf("layout Escape = %+v menu=%d focus=%d want=%d",
			result, state.workspaceHeaderMenu, result.Focus, state.activeTerminalFocusHit())
	}

}
