package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
)

// Opening settings adds a tab to the window bar, and the tab is how the page is
// left: the other tabs stay reachable while it is open, and closing it hands the
// window back to the page before it.
func TestSettingsOpenAsATabOfTheWindowBar(t *testing.T) {
	state, tabs := fourTabs(t)
	var canvas gfx.Canvas
	paint := func() {
		canvas = gfx.Canvas{}
		state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	}
	painted := func() bool { _, ok := hitByID(&canvas, hitSettingsTab); return ok }
	paint()
	if painted() {
		t.Fatal("settings tab painted before settings were opened")
	}

	state.Click(hitSettings)
	paint()
	if state.activeSection != sectionSettings || !state.settingsTab || !painted() {
		t.Fatalf("settings: section %d, tab open %t, painted %t", state.activeSection, state.settingsTab, painted())
	}

	// Another tab in front leaves the settings tab on the bar.
	state.Click(hitTabList.At(int(tabs[0])))
	paint()
	if state.activeSection != sectionTerminal || !painted() {
		t.Fatalf("after switching: section %d, painted %t", state.activeSection, painted())
	}
	state.Click(hitSettingsTab)
	if state.activeSection != sectionSettings {
		t.Fatalf("pressing the settings tab: section %d", state.activeSection)
	}

	// With the tabs in the sidebar the top bar has no tab strip: the settings
	// page is left through the sidebar, and its tab is back with the strip.
	state.setTabOrientation(tabsVertical)
	paint()
	if painted() || state.settingsTabCount() != 0 {
		t.Fatal("settings tab painted with the tabs in the sidebar")
	}
	state.setTabOrientation(tabsHorizontal)
	paint()
	if !painted() {
		t.Fatal("settings tab did not return with the horizontal tab strip")
	}

	// Closing it from the page returns to a workspace tab, and the bar no
	// longer has it.
	state.Click(hitSettingsTabClose)
	paint()
	if state.settingsTab || state.activeSection != sectionTerminal || painted() {
		t.Fatalf("after close: section %d, tab open %t, painted %t", state.activeSection, state.settingsTab, painted())
	}
}

// The tab-number shortcuts and the next-tab shortcut count the settings tab
// after the workspace tabs, and the close-tab shortcut closes it.
func TestSettingsTabTakesPartInTabShortcuts(t *testing.T) {
	state, tabs := fourTabs(t)
	state.Click(hitSettings)
	if got, want := state.topTabCount(), len(tabs)+1; got != want {
		t.Fatalf("topTabCount = %d, want %d", got, want)
	}
	state.activateTop(0, t)
	if state.activeSection != sectionTerminal {
		t.Fatalf("first tab: section %d", state.activeSection)
	}
	if !state.activateTopTab(len(tabs)) || state.activeSection != sectionSettings {
		t.Fatalf("last tab did not open settings: section %d", state.activeSection)
	}
	if !state.cycleWindowTabs(1) || state.activeSection != sectionTerminal {
		t.Fatalf("next tab from settings: section %d", state.activeSection)
	}
	if !state.cycleWindowTabs(-1) || state.activeSection != sectionSettings {
		t.Fatalf("previous tab onto settings: section %d", state.activeSection)
	}
	state.closeActivePaneOrTab()
	if state.settingsTab || state.activeSection != sectionTerminal {
		t.Fatalf("close tab on settings: section %d, tab open %t", state.activeSection, state.settingsTab)
	}
}

func (state *State) activateTop(index int, t *testing.T) {
	t.Helper()
	if !state.activateTopTab(index) {
		t.Fatalf("tab %d did not activate", index)
	}
}
