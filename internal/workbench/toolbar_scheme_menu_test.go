//go:build darwin || windows || linux

package workbench

import (
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
)

// The theme button's right-click menu chooses a terminal color scheme in place.
// Opening Settings used to cover the terminal, so a scheme could not be judged
// until that page was closed.
func TestThemeButtonMenuChoosesColorScheme(t *testing.T) {
	options := freshOptions(t)
	state := newTestState(t, options)
	view := openToolbarSchemeView(t, state)

	rightClickToolbarTool(t, view, hitTheme)
	if err := view.ClickLabel(state.messages.Tools.SchemeGroup); err != nil {
		t.Fatal(err)
	}
	tools := state.messages.Tools
	if _, ok := view.Hit(tools.SchemeCommunity + " · " + tools.SchemeDark); !ok {
		t.Fatal("the scheme menu did not offer the community collection")
	}
	if err := view.ClickLabel("One Dark"); err != nil {
		t.Fatal(err)
	}
	if state.toolbarDock.menu {
		t.Fatal("choosing a scheme left the menu open")
	}
	if state.activeScheme() != "one-dark" || state.terminalSchemes[appearanceDark] != "one-dark" || state.terminalSchemes[appearanceLight] != defaultSchemeID {
		t.Fatalf("schemes = %q, want one-dark for the dark look only", state.terminalSchemes)
	}
	if state.termPal.terminal != gfx.RGB(0x28, 0x2c, 0x34) {
		t.Fatalf("terminal background = %v, want One Dark", state.termPal.terminal)
	}
	restarted := newTestState(t, options)
	if restarted.terminalSchemes[appearanceDark] != "one-dark" {
		t.Fatalf("saved dark scheme = %q, want one-dark", restarted.terminalSchemes[appearanceDark])
	}

	rightClickToolbarTool(t, view, hitWindowSettings)
	for _, hit := range view.Snapshot().Hits {
		if hit.Label == state.messages.Tools.SchemeGroup {
			t.Fatal("a toolbar button other than theme offered color schemes")
		}
	}
}

// Resting on a scheme wears it immediately and does not save it. Leaving the
// row puts the scheme from when the menu opened back.
func TestThemeButtonMenuPreviewsSchemeWhileHovered(t *testing.T) {
	options := freshOptions(t)
	state := newTestState(t, options)
	view := openToolbarSchemeView(t, state)

	rightClickToolbarTool(t, view, hitTheme)
	if err := view.ClickLabel(state.messages.Tools.SchemeGroup); err != nil {
		t.Fatal(err)
	}
	hit, err := view.Locate("One Dark")
	if err != nil {
		t.Fatal(err)
	}
	if err := view.Move(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	if err := view.Frame(); err != nil {
		t.Fatal(err)
	}
	if state.activeScheme() != defaultSchemeID || state.termPal.terminal != gfx.RGB(0x28, 0x2c, 0x34) {
		t.Fatalf("hover selection = %q background %v, want unsaved One Dark preview", state.activeScheme(), state.termPal.terminal)
	}
	state.save()
	restarted := newTestState(t, options)
	if restarted.activeScheme() != defaultSchemeID {
		t.Fatalf("hover saved %q", restarted.activeScheme())
	}
	if err := view.Move(8, 400); err != nil {
		t.Fatal(err)
	}
	if err := view.Frame(); err != nil {
		t.Fatal(err)
	}
	if state.activeScheme() != defaultSchemeID || state.termPal.terminal == gfx.RGB(0x28, 0x2c, 0x34) {
		t.Fatalf("leaving the row kept %q", state.activeScheme())
	}
}

func TestThemeButtonPreviewDoesNotChangeOtherAppearance(t *testing.T) {
	options := freshOptions(t)
	state := newTestState(t, options)
	state.setAppearance(appearanceLight)
	state.setTerminalScheme("nord")
	state.setAppearance(appearanceDark)
	darkBackground := state.termPal.terminal
	view := openToolbarSchemeView(t, state)

	rightClickToolbarTool(t, view, hitTheme)
	if err := view.ClickLabel(state.messages.Tools.SchemeGroup); err != nil {
		t.Fatal(err)
	}
	hit, err := view.Locate("One Dark")
	if err != nil {
		t.Fatal(err)
	}
	if err := view.Move(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	if err := view.Frame(); err != nil {
		t.Fatal(err)
	}
	if state.termPal.terminal != gfx.RGB(0x28, 0x2c, 0x34) {
		t.Fatal("One Dark was not previewed")
	}
	state.setAppearance(appearanceLight)
	nord, _ := state.schemeByID("nord")
	if state.termPal.terminal != gfx.RGB(nord.background[0], nord.background[1], nord.background[2]) {
		t.Fatal("switching appearance kept the previous preview")
	}
	state.save()
	if err := view.Click(8, 400); err != nil {
		t.Fatal(err)
	}
	state.setAppearance(appearanceDark)
	if state.termPal.terminal != darkBackground {
		t.Fatal("closing the menu changed the dark scheme")
	}
	restarted := newTestState(t, options)
	if restarted.terminalSchemes[appearanceDark] != defaultSchemeID || restarted.terminalSchemes[appearanceLight] != "nord" {
		t.Fatalf("saved schemes = %q, want default dark and nord light", restarted.terminalSchemes)
	}
}

func TestThemeButtonPreviewSelectingSavedScheme(t *testing.T) {
	options := freshOptions(t)
	state := newTestState(t, options)
	originalBackground := state.termPal.terminal
	view := openToolbarSchemeView(t, state)
	rightClickToolbarTool(t, view, hitTheme)
	if err := view.ClickLabel(state.messages.Tools.SchemeGroup); err != nil {
		t.Fatal(err)
	}
	hit, err := view.Locate("One Dark")
	if err != nil {
		t.Fatal(err)
	}
	if err := view.Move(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	if err := view.Frame(); err != nil {
		t.Fatal(err)
	}
	if err := view.ClickLabel(state.messages.Tools.SchemeDefault); err != nil {
		t.Fatal(err)
	}
	if state.activeScheme() != defaultSchemeID || state.termPal.terminal != originalBackground {
		t.Fatal("selecting the saved scheme left a preview visible")
	}
}

func openToolbarSchemeView(t *testing.T, state *State) *headless.Session {
	t.Helper()
	view, err := headless.Open(app.Config{
		Title: "theme schemes", Width: 1200, Height: 800,
		WindowMode: app.WindowCustomTitlebar, TitlebarHeight: TitlebarHeight,
		Tree: Tree(), Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnScroll: state.Scroll,
	}, nil, headless.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(view.Close)
	return view
}

func rightClickToolbarTool(t *testing.T, view *headless.Session, id gfx.ID) {
	t.Helper()
	hit, ok := hitByIDSnapshot(view.Snapshot().Hits, id)
	if !ok {
		t.Fatalf("%v is not painted", id)
	}
	if err := view.RightClick(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
}
