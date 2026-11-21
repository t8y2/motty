package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
	layoutcore "motty/internal/workspace"
)

// paintTabs paints once and returns the tab strip's current geometry.
func paintTabs(t *testing.T, state *State) gfx.Canvas {
	t.Helper()
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	return canvas
}

// dragTab drags a tab from one point to another and lets go.
func dragTab(state *State, target gfx.ID, from, to gfx.Point) {
	press(state, target, from)
	dragTo(state, target, to)
	release(state, target, to)
}

// windowTabOrder names the bar's tabs in display order, for tests.
func windowTabOrder(state *State) []windowTab {
	tabs := make([]windowTab, 0, state.windowTabCount())
	for index := 0; index < state.windowTabCount(); index++ {
		if tab, ok := state.windowTabAt(index); ok {
			tabs = append(tabs, tab)
		}
	}
	return tabs
}

// The settings tab follows the pointer along the bar like any other tab, and
// the workspace order is left alone when it passes between them.
func TestDraggingTheSettingsTabMovesItAlongTheBar(t *testing.T) {
	state, tabs := fourTabs(t)
	state.Click(hitSettings)
	canvas := paintTabs(t, state)
	settings := hitCenter(t, &canvas, hitSettingsTab)
	first := hitCenter(t, &canvas, hitTabList.At(int(tabs[0])))

	before := state.settingsTabIndex()
	dragTab(state, hitSettingsTab, settings, gfx.Point{X: first.X - state.tabStrip.tabWidth/2, Y: first.Y})
	if state.settingsTabIndex() != 0 {
		t.Fatalf("settings tab stood at %d after the drag, want 0 (was %d)", state.settingsTabIndex(), before)
	}
	if got := tabOrder(state); len(got) != len(tabs) || got[0] != tabs[0] || got[3] != tabs[3] {
		t.Fatalf("the settings drag reordered the workspaces: %v", got)
	}
	if state.activeSection != sectionSettings {
		t.Fatalf("the dragged settings tab left the page: section %d", state.activeSection)
	}

	// Then put it between the second and third workspace tabs.
	canvas = paintTabs(t, state)
	settings = hitCenter(t, &canvas, hitSettingsTab)
	second := hitCenter(t, &canvas, hitTabList.At(int(tabs[1])))
	dragTab(state, hitSettingsTab, settings, gfx.Point{X: second.X + state.tabStrip.tabWidth/2, Y: second.Y})
	if got, want := state.settingsTabIndex(), 2; got != want {
		t.Fatalf("settings tab stood at %d, want %d", got, want)
	}
	order := windowTabOrder(state)
	if len(order) != len(tabs)+1 || order[1].id != uint32(tabs[1]) || order[2].kind != windowTabSettings {
		t.Fatalf("order after the second drag: %+v", order)
	}
}

// A workspace tab can move past a settings tab that sits among the workspaces,
// and the settings tab still follows the tab it was put after.
func TestDraggingAWorkspaceTabPastTheSettingsTabKeepsTheAnchor(t *testing.T) {
	state, tabs := fourTabs(t)
	state.Click(hitSettings)
	canvas := paintTabs(t, state)

	// Put settings after the second workspace tab.
	settings := hitCenter(t, &canvas, hitSettingsTab)
	second := hitCenter(t, &canvas, hitTabList.At(int(tabs[1])))
	dragTab(state, hitSettingsTab, settings, gfx.Point{X: second.X + state.tabStrip.tabWidth/2, Y: second.Y})
	if got := state.settingsTabIndex(); got != 2 {
		t.Fatalf("settings tab stood at %d, want 2", got)
	}

	// Bring the first workspace tab to the end of the bar.
	canvas = paintTabs(t, state)
	from := hitCenter(t, &canvas, hitTabList.At(int(tabs[0])))
	last := hitCenter(t, &canvas, hitTabList.At(int(tabs[3])))
	dragTab(state, hitTabList.At(int(tabs[0])), from, gfx.Point{X: last.X + state.tabStrip.tabWidth/2, Y: last.Y})

	if got := tabOrder(state); len(got) != len(tabs) || got[0] != tabs[1] || got[3] != tabs[0] {
		t.Fatalf("workspace order %v, want %v", got, []layoutcore.WorkspaceID{tabs[1], tabs[2], tabs[3], tabs[0]})
	}
	order := windowTabOrder(state)
	if len(order) != len(tabs)+1 || order[0].id != uint32(tabs[1]) || order[1].kind != windowTabSettings || order[2].id != uint32(tabs[2]) {
		t.Fatalf("the settings tab did not stay after its anchor: %+v", order)
	}
}

// The arrow keys between tabs walk the bar in the order it shows, with the
// settings tab where it was dragged.
func TestArrowKeyTabOrderFollowsTheSettingsTab(t *testing.T) {
	state, tabs := fourTabs(t)
	state.Click(hitSettings)
	canvas := paintTabs(t, state)
	second := hitCenter(t, &canvas, hitTabList.At(int(tabs[1])))
	dragTab(state, hitSettingsTab, hitCenter(t, &canvas, hitSettingsTab), gfx.Point{X: second.X + state.tabStrip.tabWidth/2, Y: second.Y})

	options := state.windowTabOptionsOrdered()
	if len(options) != len(tabs)+1 {
		t.Fatalf("%d tab options, want %d", len(options), len(tabs)+1)
	}
	if options[1].ID != hitTabList.At(int(tabs[1])) || options[2].ID != hitSettingsTab || options[3].ID != hitTabList.At(int(tabs[2])) {
		t.Fatalf("option order after the settings drag: %+v", options)
	}
}

func TestControlTabLeavesTheSettingsButtonPage(t *testing.T) {
	state, _ := fourTabs(t)
	state.Click(hitWindowSettings)
	state.Paint(&gfx.Canvas{}, gfx.Size{Width: 1400, Height: 900})
	if state.activeSection != sectionSettings || !state.settingsTab {
		t.Fatalf("the settings button did not open settings: section %d tab %t", state.activeSection, state.settingsTab)
	}
	if result := state.Key(gfx.KeyEvent{Key: gfx.KeyTab, Modifiers: gfx.ModifierControl}); !result.Handled {
		t.Fatalf("Control+Tab was not handled: %+v", result)
	}
	if state.activeSection != sectionTerminal {
		t.Fatalf("Control+Tab stayed on section %d", state.activeSection)
	}
}

// The settings tab can also pass a file-manager tab, which keeps its own order
// among the other file managers.
func TestDraggingTheSettingsTabPastAFileTab(t *testing.T) {
	state, _, file, _, _ := openSFTPTabFixture(t)
	state.Click(hitSettings)
	canvas := paintTabs(t, state)
	if got, want := state.settingsTabIndex(), state.baseTabCount(); got != want {
		t.Fatalf("settings tab stood at %d, want the end %d", got, want)
	}

	fileID := sftpFileTabID(file.session.ID)
	fileHit := hitCenter(t, &canvas, fileID)
	dragTab(state, hitSettingsTab, hitCenter(t, &canvas, hitSettingsTab), gfx.Point{X: fileHit.X - state.tabStrip.tabWidth/2, Y: fileHit.Y})
	order := windowTabOrder(state)
	if len(order) < 2 || order[len(order)-2].kind != windowTabSettings || order[len(order)-1].id != file.session.ID {
		t.Fatalf("settings did not move before the file tab: %+v", order)
	}

	canvas = paintTabs(t, state)
	fileHit = hitCenter(t, &canvas, fileID)
	dragTab(state, hitSettingsTab, hitCenter(t, &canvas, hitSettingsTab), gfx.Point{X: fileHit.X + state.tabStrip.tabWidth/2, Y: fileHit.Y})
	order = windowTabOrder(state)
	if len(order) < 2 || order[len(order)-2].id != file.session.ID || order[len(order)-1].kind != windowTabSettings {
		t.Fatalf("settings did not move after the file tab: %+v", order)
	}
}
