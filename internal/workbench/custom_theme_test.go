package workbench

import (
	"github.com/vivaui/viva/gfx"
	"testing"
)

func TestCustomThemeCopyRenameAndRestore(t *testing.T) {
	options := testOptions(t)
	state := newTestState(t, options)
	state.setTerminalScheme("dracula")
	state.openThemeEditor(true, false)
	e := &state.themeEditor
	e.name.SetText("My violet")
	e.colors[0] = gfx.RGB(24, 12, 40)
	e.colors[3], e.colors[4] = gfx.RGB(90, 60, 120), gfx.RGB(250, 245, 255)
	e.opacity.Value = 75
	state.clickThemeEditor(hitThemeApply)
	id := state.activeScheme()
	if id == "dracula" || state.modal != modalNone {
		t.Fatalf("apply: %s modal=%d", id, state.modal)
	}
	if state.termPal.terminal != gfx.RGB(24, 12, 40) || !state.termPal.customSelection {
		t.Fatal("custom palette was not applied")
	}
	original, _ := state.schemeByID("dracula")
	if original.background == [3]uint8{24, 12, 40} {
		t.Fatal("copy mutated built-in")
	}
	state.openThemeEditor(false, false)
	state.themeEditor.name.SetText("Renamed violet")
	state.clickThemeEditor(hitThemeSave)
	if state.activeScheme() != id {
		t.Fatal("rename changed ID")
	}
	again := newTestState(t, options)
	scheme, ok := again.schemeByID(id)
	if !ok || scheme.name != "Renamed violet" || *scheme.opacity != 75 {
		t.Fatalf("restore: %+v", scheme)
	}
	if again.activeScheme() != id || again.termPal.selectionText != gfx.RGB(250, 245, 255) {
		t.Fatal("active custom scheme not restored")
	}
}

func TestCustomThemeCancelAndResetKeepAppliedPalette(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.setTerminalScheme("nord")
	before := state.termPal
	state.openThemeEditor(true, false)
	original := state.themeEditor.colors[0]
	state.themeEditor.colors[0] = gfx.RGB(120, 10, 30)
	state.clickThemeEditor(hitThemeReset)
	if state.themeEditor.colors[0] != original {
		t.Fatal("reset did not restore draft")
	}
	state.themeEditor.colors[0] = gfx.RGB(10, 30, 50)
	state.closeModal()
	if state.termPal != before || len(state.managedSchemes) != 0 {
		t.Fatal("cancel changed saved appearance")
	}
}

func TestCustomThemeListAndDelete(t *testing.T) {
	state := newTestState(t, testOptions(t))
	total := state.schemeTotal()
	state.openThemeEditor(false, true)
	state.themeEditor.name.SetText("My daylight")
	state.clickThemeEditor(hitThemeApply)
	id := state.activeScheme()
	if state.schemeTotal() != total+1 {
		t.Fatal("custom theme missing from list")
	}
	index, ok := state.schemeIndexByID(id)
	if !ok {
		t.Fatal("custom theme missing from lookup")
	}
	found, name, _ := state.schemeAt(index)
	if found != id || name != "My daylight" {
		t.Fatal("list and lookup disagree")
	}
	state.deleteCustomScheme(id)
	if state.activeScheme() != defaultSchemeID || state.schemeTotal() != total {
		t.Fatal("delete did not restore default")
	}
	again := newTestState(t, state.options)
	if _, ok := again.schemeByID(id); ok {
		t.Fatal("deleted theme restored")
	}
}
