package workbench

import (
	"strings"
	"testing"

	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	"motty/internal/i18n"
)

func searchSettings(t *testing.T, state *State, query string) *ui.Canvas {
	t.Helper()
	state.Click(hitSettings)
	typeInto(state, hitSettingsSearch, query)
	canvas, _ := paintedTexts(state)
	return canvas
}

func TestSearchingSettingsListsMatchesFromEveryCategoryWithTheirCategory(t *testing.T) {
	state := newTestState(t, testOptions(t))
	canvas := searchSettings(t, state, "font")
	if !state.settingsSearching() || state.settings.count != 2 {
		t.Fatalf("results = %d, want the font size and the font family", state.settings.count)
	}
	for index := 0; index < state.settings.count; index++ {
		hit, ok := hitByID(canvas, hitSettingsResultList.At(index))
		if !ok || hit.Label != state.messages.Tools.CatTerminal {
			t.Fatalf("result %d hit = %+v, %v: it should be a button named for its category", index, hit, ok)
		}
	}
	// No category looks selected while the results are up.
	for category := settingsCategory(0); category < settingsCategoryCount; category++ {
		if hit, _ := hitByID(canvas, hitSettingsCategoryList.At(int(category))); hit.Selected {
			t.Errorf("category %d is highlighted during a search", category)
		}
	}
	// Words may come in any order, and case does not matter.
	typeInto(state, hitSettingsSearch, " SIZE")
	paintedTexts(state)
	if !state.settingsSearching() || state.settings.count != 1 || state.settings.results[0].label != state.messages.SettingsTerminalFontSize {
		t.Fatalf("results = %+v", state.settings.results[:state.settings.count])
	}
}

func TestSearchFindsShortcutsByNameOrChordAndTextInTheOtherLanguage(t *testing.T) {
	state := newTestState(t, testOptions(t))
	searchSettings(t, state, state.shortcuts.labels[shortcutCloseTab])
	if state.settings.count == 0 || state.settings.results[0].category != settingsShortcuts {
		t.Fatalf("results = %+v", state.settings.results[:state.settings.count])
	}

	state.setLanguage(i18n.Chinese)
	state.settings.search = *text.NewEditor("保活")
	paintedTexts(state)
	if state.settings.count != 1 || state.settings.results[0].label != state.messages.SettingsConnectionKeepAlive {
		t.Fatalf("results = %+v", state.settings.results[:state.settings.count])
	}
	// Changing language while the box is full rebuilds the results in it.
	state.setLanguage(i18n.English)
	paintedTexts(state)
	if state.settings.count != 0 {
		t.Fatalf("Chinese text matched English results: %+v", state.settings.results[:state.settings.count])
	}
}

func TestChoosingAResultOpensItsCategoryScrolledToItAndLeavesTheSearch(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitSettings)
	size := ui.Size{Width: 1200, Height: 520}
	typeInto(state, hitSettingsSearch, state.messages.AISettings.ClearHistory)
	var canvas ui.Canvas
	state.Paint(&canvas, size)
	if state.settings.count == 0 || state.settings.results[0].category != settingsAssistant {
		t.Fatalf("results = %+v", state.settings.results[:state.settings.count])
	}
	state.Click(hitSettingsResultList.At(0))
	if state.settingsCategory != settingsAssistant || state.settings.search.RuneCount() != 0 || state.settingsSearching() {
		t.Fatalf("category %d, query %q", state.settingsCategory, state.settings.search.String())
	}
	// Two paints: the first scrolls, the second shows it there and asks for no more.
	canvas = ui.Canvas{}
	state.Paint(&canvas, size)
	if _, scheduled := canvas.NextFrame(); !scheduled {
		t.Fatal("scrolling to a result did not ask for the paint that shows it")
	}
	canvas = ui.Canvas{}
	state.Paint(&canvas, size)
	if state.settingsScroll == 0 {
		t.Fatal("the page was not scrolled to the group")
	}
	found := false
	for _, text := range canvas.Texts() {
		if text.Value == state.messages.AISettings.BehaviourTitle && text.Bounds.Y >= 0 && text.Bounds.Y < size.Height/2 {
			found = true
		}
	}
	if !found {
		t.Fatalf("the Behaviour group is not near the top (scroll %v)", state.settingsScroll)
	}
	if _, scheduled := canvas.NextFrame(); scheduled {
		t.Fatal("the page kept asking for frames after it had scrolled")
	}
}

func TestSearchBoxKeys(t *testing.T) {
	state := newTestState(t, testOptions(t))
	searchSettings(t, state, "keepalive")
	if result := state.Key(ui.KeyEvent{Key: ui.KeyEscape, Target: hitSettingsSearch}); !result.Handled || state.settings.search.RuneCount() != 0 {
		t.Fatalf("Escape: %+v, text %q", result, state.settings.search.String())
	}
	typeInto(state, hitSettingsSearch, "keepalive")
	paintedTexts(state)
	if result := state.Key(ui.KeyEvent{Key: ui.KeyEnter, Target: hitSettingsSearch}); !result.Handled || state.settingsCategory != settingsConnection {
		t.Fatalf("Enter: %+v, category %d", result, state.settingsCategory)
	}
	// Choosing a category by hand also leaves a search.
	typeInto(state, hitSettingsSearch, "font")
	paintedTexts(state)
	state.Click(hitSettingsCategoryList.At(int(settingsGeneral)))
	if state.settings.search.RuneCount() != 0 || state.settingsCategory != settingsGeneral {
		t.Fatalf("query %q category %d", state.settings.search.String(), state.settingsCategory)
	}
}

func TestSearchResultsSteadyPaintDoesNotAllocate(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitSettings)
	typeInto(state, hitSettingsSearch, "Con")
	var canvas ui.Canvas
	size := ui.Size{Width: 1400, Height: 900}
	state.Paint(&canvas, size)
	if allocations := testing.AllocsPerRun(100, func() {
		canvas.Reset()
		state.Paint(&canvas, size)
	}); allocations != 0 {
		t.Fatalf("search results paint allocations = %v (%d results)", allocations, state.settings.count)
	}
}

func TestEveryEntryOfTheSearchIndexPointsAtSomethingThePageDraws(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitSettings)
	var entries []settingsEntry
	state.eachSettingsEntry(func(entry settingsEntry) { entries = append(entries, entry) })
	if len(entries)*4 > maxSettingsEntries*3 {
		t.Fatalf("%d entries fill more than three quarters of the %d-slot index; raise it before the rest spills out", len(entries), maxSettingsEntries)
	}
	size := ui.Size{Width: 1400, Height: 3400}
	for _, entry := range entries {
		state.selectSettingsCategory(entry.category)
		var canvas ui.Canvas
		state.Paint(&canvas, size)
		drawn := false
		for _, text := range canvas.Texts() {
			if text.Value == entry.anchor {
				drawn = true
			}
		}
		if !drawn {
			t.Errorf("category %d never draws %q, so a result for %q could not scroll to it", entry.category, entry.anchor, entry.label)
		}
		if strings.TrimSpace(entry.label) == "" {
			t.Errorf("an entry has no label: %+v", entry)
		}
	}
}
