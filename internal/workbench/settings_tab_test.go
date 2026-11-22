package workbench

import (
	"testing"
	"time"

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

// Switching away from an open settings tab to Hosts or another tab and returning
// preserves the page's scroll offset, category list scroll, and view state.
// Explicitly closing the settings tab and opening settings anew starts at the top.
func TestSettingsTabPreservesStateAcrossTabSwitches(t *testing.T) {
	state, tabs := fourTabs(t)
	size := gfx.Size{Width: 1000, Height: 520}
	var canvas gfx.Canvas
	now := time.Duration(0)
	paint := func() {
		canvas.SetClock(func() time.Duration { return now })
		canvas.BeginFrame()
		state.Paint(&canvas, size)
		canvas.EndFrame()
	}

	state.Click(hitSettings)
	state.selectSettingsCategory(settingsTerminal)
	paint()
	view := &state.settings
	if state.settingsContent <= view.pageView || view.navContent <= view.navView {
		t.Fatalf("settings must overflow to test scrolling: page %v/%v, nav %v/%v",
			state.settingsContent, view.pageView, view.navContent, view.navView)
	}

	// Scroll both the settings body (with a mouse wheel notch, like issue #1338)
	// and the category navigation list.
	for range 6 {
		state.scrollSettings(gfx.ScrollEvent{DeltaY: 48, Viewport: size})
	}
	state.scrollSettings(gfx.ScrollEvent{X: view.navRect.X + 10, Y: view.navRect.Y + 10, DeltaY: 80, Precise: true, Viewport: size})
	now = 40 * time.Millisecond
	paint()
	now = time.Second
	paint()

	wantPageScroll := state.settingsScroll
	wantNavScroll := view.navScroll
	if wantPageScroll <= 0 || wantNavScroll <= 0 {
		t.Fatalf("expected non-zero scroll offsets, got page=%v nav=%v", wantPageScroll, wantNavScroll)
	}

	// Switch to Hosts, paint, and return via the settings tab.
	state.Click(hitHosts)
	now += 100 * time.Millisecond
	paint()
	if state.activeSection != sectionHosts {
		t.Fatalf("after clicking Hosts: section = %d", state.activeSection)
	}

	state.Click(hitSettingsTab)
	now += 100 * time.Millisecond
	paint()
	if state.activeSection != sectionSettings || state.settingsCategory != settingsTerminal {
		t.Fatalf("after returning via settings tab: section=%d category=%d", state.activeSection, state.settingsCategory)
	}
	if state.settingsScroll != wantPageScroll || view.pageShown != wantPageScroll {
		t.Fatalf("page scroll after returning from Hosts: scroll=%v shown=%v, want %v",
			state.settingsScroll, view.pageShown, wantPageScroll)
	}
	if view.navScroll != wantNavScroll || view.navShown != wantNavScroll {
		t.Fatalf("nav scroll after returning from Hosts: scroll=%v shown=%v, want %v",
			view.navScroll, view.navShown, wantNavScroll)
	}
	now += 40 * time.Millisecond
	paint()
	if state.settingsScroll != wantPageScroll || view.pageShown != wantPageScroll || view.navScroll != wantNavScroll || view.navShown != wantNavScroll {
		t.Fatalf("scroll drifted on subsequent frame: page=%v/%v nav=%v/%v, want %v/%v",
			state.settingsScroll, view.pageShown, view.navScroll, view.navShown, wantPageScroll, wantNavScroll)
	}

	// Switching to a terminal tab and returning via shortcuts, sidebar Settings,
	// and window-bar Settings keeps the same state too.
	state.Click(hitTabList.At(int(tabs[0])))
	now += 100 * time.Millisecond
	paint()
	if !state.cycleWindowTabs(-1) {
		t.Fatal("cycling back onto the settings tab failed")
	}
	now += 100 * time.Millisecond
	paint()
	if state.settingsScroll != wantPageScroll || view.navScroll != wantNavScroll {
		t.Fatalf("after cycling tabs: page=%v nav=%v, want %v/%v",
			state.settingsScroll, view.navScroll, wantPageScroll, wantNavScroll)
	}

	// Returning via sidebar Settings or window-bar Settings while the tab is open
	// (including in vertical tab layout, where the sidebar is the way back).
	state.setTabOrientation(tabsVertical)
	state.Click(hitHosts)
	now += 100 * time.Millisecond
	paint()
	state.Click(hitSettings)
	now += 100 * time.Millisecond
	paint()
	if state.settingsScroll != wantPageScroll || view.navScroll != wantNavScroll {
		t.Fatalf("after returning in vertical tabs: page=%v nav=%v, want %v/%v",
			state.settingsScroll, view.navScroll, wantPageScroll, wantNavScroll)
	}
	state.setTabOrientation(tabsHorizontal)
	state.Click(hitHosts)
	state.Click(hitWindowSettings)
	now += 100 * time.Millisecond
	paint()
	if state.settingsScroll != wantPageScroll || view.navScroll != wantNavScroll {
		t.Fatalf("after returning via window settings button: page=%v nav=%v, want %v/%v",
			state.settingsScroll, view.navScroll, wantPageScroll, wantNavScroll)
	}

	// Switching away mid-wheel-animation and returning lands at the target
	// without replaying an incomplete ease.
	state.scrollSettings(gfx.ScrollEvent{DeltaY: 48, Viewport: size})
	midTarget := state.settingsScroll
	now += 20 * time.Millisecond
	paint()
	state.Click(hitHosts)
	now += 20 * time.Millisecond
	paint()
	state.Click(hitSettingsTab)
	now += 20 * time.Millisecond
	paint()
	if state.settingsScroll != midTarget || view.pageShown != midTarget {
		t.Fatalf("after mid-ease switch: scroll=%v shown=%v, want %v",
			state.settingsScroll, view.pageShown, midTarget)
	}

	// Keeping the scheme drawer open across a tab switch lets the user compare
	// colors on a terminal tab and come right back to the drawer.
	state.openSchemeBrowser()
	state.Click(hitTabList.At(int(tabs[0])))
	state.Click(hitSettingsTab)
	if !state.schemeBrowserOpen {
		t.Fatal("returning to the open settings tab closed the scheme browser drawer")
	}

	// Active search query and its scroll offset also survive switching tabs.
	state.schemeBrowserOpen = false
	typeInto(state, hitSettingsSearch, "a")
	now += 100 * time.Millisecond
	paint()
	if !state.settingsSearching() || state.settings.count == 0 {
		t.Fatalf("expected search results for %q", "a")
	}
	state.scrollSettings(gfx.ScrollEvent{DeltaY: 160, Precise: true, Viewport: size})
	wantSearchScroll := state.settingsScroll
	if wantSearchScroll <= 0 {
		t.Fatalf("expected scrollable search results, got scroll=%v (content=%v view=%v)",
			wantSearchScroll, state.settingsContent, view.pageView)
	}
	state.Click(hitHosts)
	now += 100 * time.Millisecond
	paint()
	state.Click(hitSettingsTab)
	now += 100 * time.Millisecond
	paint()
	if !state.settingsSearching() || view.search.String() != "a" || state.settingsScroll != wantSearchScroll {
		t.Fatalf("after returning to search: searching=%t query=%q scroll=%v, want %v",
			state.settingsSearching(), view.search.String(), state.settingsScroll, wantSearchScroll)
	}

	// Closing the settings tab explicitly and opening settings again starts fresh
	// at the top.
	state.Click(hitSettingsTabClose)
	if state.settingsTab || state.schemeBrowserOpen {
		t.Fatalf("after closing settings tab: open=%t drawer=%t", state.settingsTab, state.schemeBrowserOpen)
	}
	state.Click(hitSettings)
	now += 100 * time.Millisecond
	paint()
	if state.settingsScroll != 0 || view.pageShown != 0 {
		t.Fatalf("freshly reopened settings tab: scroll=%v shown=%v, want 0", state.settingsScroll, view.pageShown)
	}
}

// The AI entry points open the assistant page at its top, also when the settings
// tab is already open on that page and scrolled down.
func TestAISettingsEntryOpensAtTheTopOfAnOpenPage(t *testing.T) {
	state, _ := fourTabs(t)
	state.openSettings()
	state.selectSettingsCategory(settingsAssistant)
	state.settingsScroll = 300
	state.openAIModelSettings()
	if state.settingsCategory != settingsAssistant || state.settingsScroll != 0 {
		t.Fatalf("category %d, scroll %g: the entry point did not open at the top", state.settingsCategory, state.settingsScroll)
	}
}
