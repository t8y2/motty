package workbench

import (
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/keymap"
	"github.com/vivaui/viva/text"
	"motty/internal/i18n"
)

func searchSettings(t *testing.T, state *State, query string) *gfx.Canvas {
	t.Helper()
	state.Click(hitSettings)
	typeInto(state, hitSettingsSearch, query)
	canvas, _ := paintedTexts(state)
	return canvas
}

func TestSearchOpensLockShortcutSettingsRow(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	state.setLanguage(i18n.Chinese)
	name := state.messages.AppLock.Lock
	searchSettings(t, state, name)
	found := false
	for index, row := range state.settings.results[:state.settings.count] {
		if row.category == settingsShortcuts && row.label == name {
			state.Click(hitSettingsResultList.At(index))
			found = true
			break
		}
	}
	if !found || state.settingsCategory != settingsShortcuts {
		t.Fatalf("could not reach the keyboard shortcut for %q", name)
	}
	var canvas gfx.Canvas
	size := gfx.Size{Width: 1200, Height: 840}
	state.Paint(&canvas, size)
	canvas = gfx.Canvas{}
	state.Paint(&canvas, size)
	chord := keymap.Chord{Modifiers: primaryModifier | gfx.ModifierShift, Char: 'l'}
	action := state.shortcuts.actionFor(chord)
	if action == shortcutNone || state.shortcutActionLabel(action) != name {
		t.Fatal("the default lock shortcut is missing")
	}
	row, ok := hitByID(&canvas, hitShortcutRowList.At(int(action-1)))
	if !ok || row.Label != chord.Label() || row.Bounds.Y < 0 || row.Bounds.Y >= size.Height {
		t.Fatal("the lock shortcut is not visible after selecting its search result")
	}
	state.Click(row.ID)
	if state.modal != modalShortcutCapture || state.shortcutCapture != action {
		t.Fatal("the lock shortcut row did not open the key recorder")
	}
}

func TestSearchingSettingsListsMatchesFromEveryCategoryWithTheirCategory(t *testing.T) {
	state := newTestState(t, testOptions(t))
	canvas := searchSettings(t, state, "font")
	if !state.settingsSearching() {
		t.Fatal("searching for a font found nothing")
	}
	// The rows a reader looks for by that word are among the results; how many
	// others mention it in their description is not the page's promise.
	for _, want := range []string{
		state.messages.SettingsInterfaceFont, state.messages.SettingsTerminalFontSize,
		state.messages.SettingsTerminalFontFamily, state.messages.Tools.FontFallbackRow,
	} {
		found := false
		for index := 0; index < state.settings.count; index++ {
			if state.settings.results[index].label == want {
				found = true
			}
		}
		if !found {
			t.Errorf("searching for a font did not find %q", want)
		}
	}
	for index := 0; index < state.settings.count; index++ {
		hit, ok := hitByID(canvas, hitSettingsResultList.At(index))
		if want := state.settingsCategoryName(state.settings.results[index].category); !ok || hit.Label != want {
			t.Fatalf("result %d hit = %+v, %v: it should be a button named for its category %q", index, hit, ok, want)
		}
	}
	// No category looks selected while the results are up.
	for category := settingsCategory(0); category < settingsCategoryCount; category++ {
		if hit, _ := hitByID(canvas, hitSettingsCategoryList.At(int(category))); hit.Selected {
			t.Errorf("category %d is highlighted during a search", category)
		}
	}
	// Case does not matter, and a setting whose name is the rest of the word is
	// still found.
	typeInto(state, hitSettingsSearch, " FONT SIZE")
	paintedTexts(state)
	if !state.settingsSearching() || state.settings.count == 0 {
		t.Fatal("an upper-case search found nothing")
	}
	found := false
	for index := 0; index < state.settings.count; index++ {
		if state.settings.results[index].label == state.messages.SettingsTerminalFontSize {
			found = true
		}
	}
	if !found {
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
	size := gfx.Size{Width: 1200, Height: 520}
	typeInto(state, hitSettingsSearch, state.messages.AISettings.ClearHistory)
	var canvas gfx.Canvas
	state.Paint(&canvas, size)
	// Other "clear" actions are listed too; pick the assistant's own row rather
	// than relying on it being the first match.
	result := -1
	for index := 0; index < state.settings.count; index++ {
		if state.settings.results[index].category == settingsAssistant {
			result = index
			break
		}
	}
	if result < 0 {
		t.Fatalf("results = %+v", state.settings.results[:state.settings.count])
	}
	state.Click(hitSettingsResultList.At(result))
	if state.settingsCategory != settingsAssistant || state.settings.search.RuneCount() != 0 || state.settingsSearching() {
		t.Fatalf("category %d, query %q", state.settingsCategory, state.settings.search.String())
	}
	// Two paints: the first scrolls, the second shows it there and asks for no more.
	canvas = gfx.Canvas{}
	state.Paint(&canvas, size)
	if _, scheduled := canvas.NextFrame(); !scheduled {
		t.Fatal("scrolling to a result did not ask for the paint that shows it")
	}
	canvas = gfx.Canvas{}
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
	if result := state.Key(gfx.KeyEvent{Key: gfx.KeyEscape, Target: hitSettingsSearch}); !result.Handled || state.settings.search.RuneCount() != 0 {
		t.Fatalf("Escape: %+v, text %q", result, state.settings.search.String())
	}
	typeInto(state, hitSettingsSearch, "keepalive")
	paintedTexts(state)
	if result := state.Key(gfx.KeyEvent{Key: gfx.KeyEnter, Target: hitSettingsSearch}); !result.Handled || state.settingsCategory != settingsConnection {
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
	var canvas gfx.Canvas
	size := gfx.Size{Width: 1400, Height: 900}
	state.Paint(&canvas, size)
	if allocations := testing.AllocsPerRun(100, func() {
		canvas.Reset()
		state.Paint(&canvas, size)
	}); allocations != 0 {
		t.Fatalf("search results paint allocations = %v (%d results)", allocations, state.settings.count)
	}
}

func TestCustomEditorSettingAppearsInSearchWhenAvailable(t *testing.T) {
	state := newTestState(t, testOptions(t))
	if !state.customEditorSettingVisible() {
		state.defaultEditor = editorCustom
		state.customEditorPath = "/Applications/Editor.app"
	}
	searchSettings(t, state, state.messages.Tools.FilesCustomEditor)
	if state.settings.count != 1 || state.settings.results[0].category != settingsFiles ||
		state.settings.results[0].label != state.messages.Tools.FilesCustomEditor {
		t.Fatalf("custom editor results = %+v", state.settings.results[:state.settings.count])
	}
}

func TestFilesSettingsPlaceDirectoryTreeAfterOpenMode(t *testing.T) {
	state := newTestState(t, testOptions(t))
	var labels []string
	state.eachSettingsEntry(func(entry settingsEntry) {
		if entry.category == settingsFiles {
			labels = append(labels, entry.label)
		}
	})
	openMode, directoryTree := -1, -1
	for index, label := range labels {
		switch label {
		case state.messages.Tools.FilesOpenMode:
			openMode = index
		case state.messages.Tools.FilesDirectoryTree:
			directoryTree = index
		}
	}
	if openMode < 0 || directoryTree != openMode+1 {
		t.Fatalf("files settings order = %q; directory tree must immediately follow open mode", labels)
	}
}

func TestSearchingSessionLogSettingsDoesNotShowFormatVerbs(t *testing.T) {
	state := newTestState(t, testOptions(t))
	searchSettings(t, state, state.messages.SessionLogs.SettingsStorageRow)
	found := false
	for _, result := range state.settings.results[:state.settings.count] {
		if result.label == state.messages.SessionLogs.SettingsStorageRow {
			found = true
			if strings.Contains(result.description, "%d") || strings.Contains(result.description, "%s") {
				t.Fatalf("search result renders an unformatted storage string: %q", result.description)
			}
		}
	}
	if !found {
		t.Fatal("session log storage setting is absent from search")
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
	size := gfx.Size{Width: 1400, Height: 3400}
	for _, entry := range entries {
		state.selectSettingsCategory(entry.category)
		var canvas gfx.Canvas
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

// TestSearchFindsTheLineSpacingSetting keeps the new terminal row in the search
// index; it paints even when the setting cannot be found by name.
func TestSearchFindsTheLineSpacingSetting(t *testing.T) {
	for _, language := range []i18n.Language{i18n.English, i18n.Chinese} {
		t.Run(language.String(), func(t *testing.T) {
			state := newTestState(t, testOptions(t))
			state.setLanguage(language)
			searchSettings(t, state, state.messages.Tools.LineHeightRow)
			if state.settings.count == 0 || state.settings.results[0].label != state.messages.Tools.LineHeightRow {
				t.Fatalf("searching %q returned %+v", state.messages.Tools.LineHeightRow, state.settings.results[:state.settings.count])
			}
		})
	}
}

// TestSearchFindsTheCloseWindowQuestion keeps the switch that decides whether
// closing a window with sessions asks first on the page's terminal category.
func TestSearchFindsTheCloseWindowQuestion(t *testing.T) {
	for _, language := range []i18n.Language{i18n.English, i18n.Chinese} {
		t.Run(language.String(), func(t *testing.T) {
			state := newTestState(t, testOptions(t))
			state.setLanguage(language)
			searchSettings(t, state, state.messages.Tools.CloseWindowConfirmRow)
			if state.settings.count != 1 || state.settings.results[0].label != state.messages.Tools.CloseWindowConfirmRow {
				t.Fatalf("searching %q returned %+v", state.messages.Tools.CloseWindowConfirmRow, state.settings.results[:state.settings.count])
			}
			if category := state.settings.results[0].category; category != settingsTerminal {
				t.Fatalf("the row is in category %d, want the terminal settings", category)
			}
		})
	}
}
