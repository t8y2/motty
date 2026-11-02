package workbench

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/vivaui/viva/ui"
	"motty/internal/completion"
	"motty/internal/configstore"
	"motty/internal/i18n"
)

func TestInlineSettingDisablesGhostButKeepsManualCompletion(t *testing.T) {
	w := newCompletionWorld(t)
	w.type_("git statu")
	if _, ok := w.state.commandGhost(w.session); !ok {
		t.Fatal("the catalog did not offer a suggestion before disabling inline suggestions")
	}
	var saved savedState
	if err := json.Unmarshal([]byte(`{"command_inline_suggest_off":true}`), &saved); err != nil {
		t.Fatal(err)
	}
	w.state.restoreShared(saved)
	if rest, ok := w.state.commandGhost(w.session); ok {
		t.Fatalf("inline suggestions are disabled but %q is still offered", rest)
	}
	w.state.openCompletions()
	if !w.state.completion.list.active || len(w.state.completion.list.candidates) == 0 {
		t.Fatal("disabling inline suggestions also disabled the manual completion list")
	}
	// Off turns off both modes.
	w.state.setCompletionMode(completionOff)
	if w.state.completion.list.active {
		t.Fatal("turning command completion off left the list open")
	}
	w.state.openCompletions()
	if w.state.completion.list.active {
		t.Fatal("command completion is disabled but the list reopened")
	}
}

func TestCompletionControlsAndKeysHaveSeparateSettingsHomes(t *testing.T) {
	for _, language := range []i18n.Language{i18n.English, i18n.Chinese} {
		t.Run(language.String(), func(t *testing.T) {
			state := newTestState(t, testOptions(t))
			state.setLanguage(language)
			state.Click(hitSettings)
			state.selectSettingsCategory(settingsTerminal)
			var canvas ui.Canvas
			state.Paint(&canvas, ui.Size{Width: 1400, Height: 2400})
			for _, option := range state.settingsCompletionOptions() {
				hit, ok := hitByID(&canvas, option.ID)
				if !ok || !canvasHasText(&canvas, option.Label) {
					t.Fatalf("completion mode %q is not offered", option.Label)
				}
				if want := option.ID == hitSettingsCompletionBoth; hit.Selected != want {
					t.Errorf("mode %q selected=%v, want %v by default", option.Label, hit.Selected, want)
				}
			}
			state.selectSettingsCategory(settingsShortcuts)
			canvas.Reset()
			state.Paint(&canvas, ui.Size{Width: 1400, Height: 2400})
			if _, ok := hitByID(&canvas, hitShortcutRowList.At(int(shortcutCompletion-1))); !ok {
				t.Fatal("the shortcut for opening completions is not visible")
			}
			if !canvasHasText(&canvas, state.messages.Completion.Open) {
				t.Fatal("the configurable shortcut does not explain that it opens completions")
			}
			for _, help := range state.completionKeys() {
				if !canvasHasText(&canvas, help.label) || !canvasHasText(&canvas, help.keys) {
					t.Errorf("keyboard shortcuts do not explain %q: %q", help.label, help.keys)
				}
				for _, hit := range canvas.Hits() {
					if hit.Label == help.label {
						t.Errorf("fixed completion behavior %q is presented as an interactive shortcut", help.label)
					}
				}
			}
		})
	}
}

func TestCompletionModeIsChosenAndPersistsInSettings(t *testing.T) {
	options := testOptions(t)
	state := newTestState(t, options)
	state.Click(hitSettings)
	state.selectSettingsCategory(settingsTerminal)
	cases := []struct {
		id                    ui.ID
		mode                  completionMode
		suggestOff, inlineOff bool
	}{
		{hitSettingsCompletionList, completionList, false, true},
		{hitSettingsCompletionOff, completionOff, true, false},
		{hitSettingsCompletionBoth, completionBoth, false, false},
	}
	for _, c := range cases {
		state.Click(c.id)
		if state.completionMode() != c.mode || state.history.suggestOff != c.suggestOff || state.history.inlineOff != c.inlineOff {
			t.Fatalf("mode after %v = %v (suggestOff=%v inlineOff=%v)", c.id, state.completionMode(), state.history.suggestOff, state.history.inlineOff)
		}
		again := newTestState(t, options)
		if again.completionMode() != c.mode {
			t.Fatalf("mode %v did not survive a restart: %v", c.mode, again.completionMode())
		}
	}
}

func TestEarlierCompletionPreferencesKeepTheirMeaning(t *testing.T) {
	for _, c := range []struct {
		record string
		want   completionMode
	}{
		{`{}`, completionBoth},
		{`{"command_suggest_off":true}`, completionOff},
		{`{"command_inline_suggest_off":true}`, completionList},
	} {
		var saved savedState
		if err := json.Unmarshal([]byte(c.record), &saved); err != nil {
			t.Fatal(err)
		}
		state := newTestState(t, testOptions(t))
		state.restoreShared(saved)
		if state.completionMode() != c.want {
			t.Errorf("%s reads as mode %v, want %v", c.record, state.completionMode(), c.want)
		}
	}
}

func TestCompletionCatalogStaysUnloadedWhileCompletionIsOff(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.setCompletionMode(completionOff)
	state.completion = completionUI{}
	state.Wake()
	if state.completion.catalogLoading || state.completion.store != nil || state.completion.catalog != nil {
		t.Fatal("the catalog was decoded although completion is off")
	}
	state.setCompletionMode(completionList)
	if !state.completion.catalogLoading {
		t.Fatal("turning completion on did not load the catalog")
	}
	state.closeCompletionWorkers()
}

func TestInlinePreferenceUsesAnIndependentSharedRecord(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.history.inlineOff = true
	entries, local, err := splitState(state.snapshot(), newConfigBase(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var record *configstore.Entry
	for index := range entries {
		if entry := &entries[index]; entry.Kind == kindSetting && entry.ID == "command_inline_suggest_off" {
			record = entry
		}
	}
	if record == nil {
		t.Fatal("the inline preference is not its own shared setting")
	}
	saved, _, err := joinState(entries, local)
	if err != nil || !saved.CommandInlineSuggestOff || saved.CommandSuggestOff {
		t.Fatalf("shared record round trip: inlineOff=%v completionOff=%v err=%v", saved.CommandInlineSuggestOff, saved.CommandSuggestOff, err)
	}
	var own map[string]json.RawMessage
	if err := json.Unmarshal(local, &own); err != nil {
		t.Fatal(err)
	}
	if _, ok := own[record.ID]; ok {
		t.Fatal("a shared inline preference was also written as device-local state")
	}
}

func TestCompletionKeyHelpIsSearchableInShortcuts(t *testing.T) {
	state := newTestState(t, testOptions(t))
	searchSettings(t, state, "Alt+→")
	if state.settings.count != 1 || state.settings.results[0].category != settingsShortcuts || state.settings.results[0].label != state.messages.Completion.KeysAcceptWord {
		t.Fatalf("word acceptance search results: %+v", state.settings.results[:state.settings.count])
	}
}

// loadCompletionBaseline gives the state a real store and catalog generation so
// a queued update result looks like the install it reports.
func loadCompletionBaseline(t *testing.T, state *State) *completion.CatalogStore {
	t.Helper()
	store := completion.NewCatalogStore("")
	if _, _, _, err := store.Load(); err != nil {
		t.Fatal(err)
	}
	state.completion.store = store
	state.completion.catalogGeneration = 0
	return store
}

func queueCompletionUpdate(state *State, stats completion.ImportStats) {
	state.completion.updateReady = make(chan completionUpdateResult, 1)
	state.completion.updateReady <- completionUpdateResult{stats: stats}
	state.adoptCompletionUpdate()
}

func TestCompletionUpdateReportsRepairedAndSkippedSources(t *testing.T) {
	w := newCompletionWorld(t)
	state := w.state
	loadCompletionBaseline(t, state)
	queueCompletionUpdate(state, completion.ImportStats{RepairedFiles: 9, SkippedSources: 2})
	catalog := state.completion.catalog
	if catalog == nil {
		t.Fatal("the update result left no catalog")
	}
	m := state.messages.Completion
	if want := fmt.Sprintf(m.UpdatedRepairedFmt, catalog.Release, 9, 2); state.status != want {
		t.Fatalf("status = %q, want %q", state.status, want)
	}
	status, err := state.completionCatalogStatus()
	if err != nil {
		t.Fatal(err)
	}
	if summary := fmt.Sprintf(m.RepairSummaryFmt, 9, 2); !strings.Contains(status, summary) {
		t.Fatalf("settings status %q does not explain %q", status, summary)
	}
	if checked := state.completion.checked.Format("2006-01-02 15:04"); !strings.Contains(status, checked) {
		t.Fatalf("settings status %q lost its check time %q", status, checked)
	}
}

func TestCompletionUpdateWithoutDefectsKeepsThePlainMessage(t *testing.T) {
	w := newCompletionWorld(t)
	state := w.state
	loadCompletionBaseline(t, state)
	queueCompletionUpdate(state, completion.ImportStats{})
	m := state.messages.Completion
	if want := fmt.Sprintf(m.UpdatedFmt, state.completion.catalog.Release); state.status != want {
		t.Fatalf("status = %q, want %q", state.status, want)
	}
	status, err := state.completionCatalogStatus()
	if err != nil {
		t.Fatal(err)
	}
	if summary := fmt.Sprintf(m.RepairSummaryFmt, 0, 0); strings.Contains(status, summary) {
		t.Fatalf("settings status %q reports a defect-free update", status)
	}
}

func TestCompletionUpdateWithoutANewVersionStaysUpToDate(t *testing.T) {
	w := newCompletionWorld(t)
	state := w.state
	queueCompletionUpdate(state, completion.ImportStats{})
	if state.status != state.messages.Completion.UpToDate {
		t.Fatalf("status = %q, want the up-to-date message", state.status)
	}
}

func canvasHasText(canvas *ui.Canvas, value string) bool {
	for _, text := range canvas.Texts() {
		if text.Value == value {
			return true
		}
	}
	return false
}
