package workbench

import (
	"strings"
	"testing"

	"github.com/vivaui/viva/text"
	"motty/internal/i18n"
)

func TestSuggestionRejectsMultilineOrControlCommand(t *testing.T) {
	state, _ := panelState(t, nil)
	entry := state.aiSuggestionPool(aiSuggestionConnected)[0]
	for _, command := range []string{"echo safe\nrm -rf /", "echo safe\x1b[2J"} {
		entry.Command = command
		if err := state.aiSaveSuggestion(entry); err == nil {
			t.Fatalf("accepted non-single-line command %q", command)
		}
	}
}

func TestSuggestionSettingsPersistSourceAndShowAutomaticTransmissionDisclosure(t *testing.T) {
	world := newSettingsWorld(t, newLister())
	state := world.state
	state.setLanguage(i18n.English)
	canvas, _ := world.paint()
	panelHit(t, canvas, hitAISetSuggestAI)
	state.Click(hitAISetSuggestAI)
	if got := settingValue(t, world.db(t), "ai.suggestions.source"); got != "ai" {
		t.Fatalf("persisted source = %q", got)
	}
	_, texts := world.paint()
	for _, want := range []string{"30", "4 KiB", "terminal", "Redaction"} {
		if !hasText(texts, want) {
			t.Errorf("disclosure lacks %q: %q", want, texts)
		}
	}
	state.Click(hitAISetSuggestPreview)
	_, texts = world.paint()
	if !hasText(texts, "Selected terminal status: unavailable") {
		t.Fatalf("preview is not the bounded request snapshot: %q", texts)
	}
	state.Click(hitAISetSuggestLibrary)
	if got := settingValue(t, world.db(t), "ai.suggestions.source"); got != "library" {
		t.Fatalf("restoring default source = %q", got)
	}
}

func TestSuggestionSettingsEditDisableRestoreAndRemove(t *testing.T) {
	world := newSettingsWorld(t, newLister())
	state := world.state
	state.setLanguage(i18n.English)
	state.Click(hitAISetSuggestConnected)
	canvas, _ := world.paint()
	panelHit(t, canvas, hitAISetSuggestEdit.At(0))
	state.Click(hitAISetSuggestEdit.At(0))
	if state.ai.cfg.dialog != aiDialogSuggestionEdit {
		t.Fatal("built-in editor did not open")
	}
	if editor := state.EditorFor(hitAISetSuggestText); editor == nil {
		t.Fatal("prompt field is not editable")
	} else {
		editor.SetSelection(text.Range{Start: 0, End: editor.RuneCount()})
		editor.Insert("Check the newest journal lines")
	}
	if editor := state.EditorFor(hitAISetSuggestCommand); editor == nil {
		t.Fatal("command field is not editable")
	} else {
		editor.Insert("journalctl -n 30")
	}
	state.Click(hitAISetSuggestSave)
	if state.ai.cfg.dialog != aiDialogNone || state.aiSuggestionPool(aiSuggestionConnected)[0].Command != "journalctl -n 30" {
		t.Fatalf("edited built-in not saved: %+v", state.aiSuggestionPool(aiSuggestionConnected))
	}
	state.Click(hitAISetSuggestDisable.At(0))
	if !state.aiSuggestionPool(aiSuggestionConnected)[0].Disabled {
		t.Fatal("built-in not disabled")
	}
	state.Click(hitAISetSuggestReset.At(0))
	if state.aiSuggestionPool(aiSuggestionConnected)[0].Text != state.messages.AIPanel.Example1 || state.aiSuggestionPool(aiSuggestionConnected)[0].Disabled {
		t.Fatal("built-in reset did not restore factory default")
	}
	state.Click(hitAISetSuggestAdd)
	if editor := state.EditorFor(hitAISetSuggestText); editor != nil {
		editor.Insert("Show my disk usage")
	} else {
		t.Fatal("new prompt field is not editable")
	}
	state.Click(hitAISetSuggestSave)
	pool := state.aiSuggestionPool(aiSuggestionConnected)
	if !strings.Contains(pool[len(pool)-1].Text, "disk usage") {
		t.Fatalf("custom entry missing: %+v", pool)
	}
	state.Click(hitAISetSuggestDelete.At(len(pool) - 1))
	if len(state.aiSuggestionPool(aiSuggestionConnected)) != len(pool)-1 {
		t.Fatal("custom entry remained after deletion")
	}
}

func TestSuggestionSettingsReorderSurvivesReload(t *testing.T) {
	world := newSettingsWorld(t, newLister())
	state := world.state
	state.Click(hitAISetSuggestConnected)
	first := state.aiSuggestionPool(aiSuggestionConnected)[0].ID
	state.Click(hitAISetSuggestMoveDown.At(0))
	state.reloadAI()
	pool := state.aiSuggestionPool(aiSuggestionConnected)
	if pool[1].ID != first {
		t.Fatalf("reordered built-in lost its position: %+v", pool)
	}
}
