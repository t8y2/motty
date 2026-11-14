package workbench

import (
	"slices"
	"testing"

	"github.com/vivaui/viva/gfx"
	"motty/internal/i18n"
)

// The Sidebar row of Settings opens the picker; a switch takes that page's row
// away and leaves the rest of the application alone, and Show all brings back
// every row at once.
func TestSidebarHidesOnlyThePageRow(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.setLanguage(i18n.Chinese)
	size := gfx.Size{Width: 1400, Height: 900}
	paint := func() *gfx.Canvas {
		canvas := &gfx.Canvas{}
		state.Paint(canvas, size)
		return canvas
	}
	if _, ok := hitByID(paint(), hitWorkspaces); !ok {
		t.Fatal("the Workspaces row is not painted before it is hidden")
	}

	state.Click(hitSettings)
	state.Click(hitSidebarSettingsOpen)
	if state.modal != modalSidebarSettings {
		t.Fatal("the Sidebar row did not open the page picker")
	}
	toggle := hitSidebarSettingsToggles.At(int(navPageWorkspaces))
	state.Click(toggle)
	if !state.hiddenNav.has(navPageWorkspaces) {
		t.Fatal("the picker's switch did not hide Workspaces")
	}
	state.Click(hitSidebarSettingsClose)
	if state.modal != modalNone {
		t.Fatal("the picker did not close")
	}
	canvas := paint()
	if _, ok := hitByID(canvas, hitWorkspaces); ok {
		t.Fatal("a hidden page still has a sidebar row")
	}
	if state.activeSection != sectionSettings {
		t.Fatal("hiding a page moved the page on show")
	}
	if _, ok := hitByID(canvas, hitSettings); !ok {
		t.Fatal("hiding a page took away the Settings row")
	}
	if _, ok := hitByID(canvas, hitHosts); !ok {
		t.Fatal("hiding a page took away the Hosts row")
	}

	state.Click(hitSidebarSettingsOpen)
	state.Click(toggle)
	state.Click(hitSidebarSettingsClose)
	if state.hiddenNav.has(navPageWorkspaces) {
		t.Fatal("the picker's switch did not show Workspaces again")
	}
	if _, ok := hitByID(paint(), hitWorkspaces); !ok {
		t.Fatal("the Workspaces row did not come back")
	}

	state.Click(hitSidebarSettingsOpen)
	state.Click(hitSidebarSettingsToggles.At(int(navPageNotes)))
	state.Click(hitSidebarSettingsShowAll)
	if state.hiddenNav != 0 {
		t.Fatal("Show all left a page hidden")
	}
	state.Click(hitSidebarSettingsClose)
	if _, ok := hitByID(paint(), hitNotes); !ok {
		t.Fatal("Show all did not bring back every sidebar row")
	}
}

// The hidden set is saved by stable page keys, not section numbers: a row this
// build does not know is dropped, and Hosts is never hidden.
func TestHiddenSidebarPagesRoundTrip(t *testing.T) {
	var hidden navHidden
	hidden = hidden.set(navPageNotes, true)
	hidden = hidden.set(navPageKeychain, true)
	hidden = hidden.set(navPageHosts, true)
	keys := hidden.savedKeys()
	if want := []string{"keychain", "notes"}; !slices.Equal(keys, want) {
		t.Fatalf("saved keys = %v, want %v", keys, want)
	}
	restored := hiddenNavPages(append(keys, "from-a-newer-build", "hosts"))
	if !restored.has(navPageNotes) || !restored.has(navPageKeychain) {
		t.Fatal("a saved page did not come back hidden")
	}
	if restored.has(navPageHosts) {
		t.Fatal("Hosts was hidden from a saved key")
	}
	if got := restored.savedKeys(); !slices.Equal(got, keys) {
		t.Fatalf("restored keys = %v, want %v", got, keys)
	}
	if restored.set(navPageNotes, false).has(navPageNotes) {
		t.Fatal("a page shown again stayed hidden")
	}
}
