package workbench

import (
	"testing"

	"motty/internal/ai/gate"
	"motty/internal/i18n"
)

func TestSuggestionLibraryRotatesThreeWithoutChangingOnRepaint(t *testing.T) {
	state, _ := panelState(t, nil)
	state.setLanguage(i18n.English)
	first := state.aiSuggestionVisible(aiSuggestionConnected, 0)
	if len(first) != 3 || first[0].Text != state.messages.AIPanel.Example1 {
		t.Fatalf("first selection = %+v", first)
	}
	stable := state.aiSuggestionVisible(aiSuggestionConnected, 0)
	if stable[0].ID != first[0].ID {
		t.Fatal("repaint changed the recommendations")
	}
	second := state.aiSuggestionVisible(aiSuggestionConnected, 1)
	if len(second) != 3 || second[0].ID == first[0].ID {
		t.Fatalf("next rotation did not change: %+v", second)
	}
	third := state.aiSuggestionVisible(aiSuggestionConnected, 2)
	if len(third) != 3 || third[0].ID != first[0].ID {
		t.Fatalf("rotation did not cycle: %+v", third)
	}
}

func TestSuggestionLibraryPersistsBuiltinEditsAndCustomCommands(t *testing.T) {
	state, _ := panelState(t, nil)
	state.setLanguage(i18n.Chinese)
	base := state.aiSuggestionPool(aiSuggestionConnected)
	if len(base) < 6 {
		t.Fatalf("built-in pool has %d entries, cannot rotate", len(base))
	}
	modified := base[0]
	modified.Text = "查看错误日志"
	modified.Command = "journalctl -n 50"
	modified.Order = 99
	if err := state.aiSaveSuggestion(modified); err != nil {
		t.Fatal(err)
	}
	disabled := base[1]
	disabled.Disabled = true
	if err := state.aiSaveSuggestion(disabled); err != nil {
		t.Fatal(err)
	}
	custom := aiSuggestion{ID: "custom-1", Locale: "zh", Context: aiSuggestionConnected, Text: "查看服务状态", Command: "systemctl status nginx", Order: 1}
	if err := state.aiSaveSuggestion(custom); err != nil {
		t.Fatal(err)
	}
	state.reloadAI()
	pool := state.aiSuggestionPool(aiSuggestionConnected)
	if pool[0].ID != custom.ID || pool[len(pool)-1].Text != modified.Text || pool[len(pool)-1].Command != modified.Command {
		t.Fatalf("reloaded edits = %+v", pool)
	}
	if err := state.aiResetBuiltinSuggestion(modified.ID, "zh"); err != nil {
		t.Fatal(err)
	}
	state.reloadAI()
	pool = state.aiSuggestionPool(aiSuggestionConnected)
	for _, entry := range pool {
		if entry.ID == modified.ID && (entry.Text != base[0].Text || entry.Command != "") {
			t.Fatalf("builtin reset did not restore defaults: %+v", entry)
		}
	}
	state.setLanguage(i18n.English)
	for _, entry := range state.aiSuggestionPool(aiSuggestionConnected) {
		if entry.Text == "查看错误日志" || entry.ID == custom.ID {
			t.Fatal("Chinese edits leaked into English recommendations")
		}
	}
}

func TestSuggestionRefreshCyclesCardsAndClickOnlyFillsComposer(t *testing.T) {
	state, _ := panelState(t, nil)
	withModel(t, state)
	state.ai.open = true
	canvas, texts := paintAt(state, panelWide)
	if !hasText(texts, state.messages.AIPanel.Example1) {
		t.Fatalf("first suggestions absent: %q", texts)
	}
	panelHit(t, canvas, hitAIExamplesRefresh)
	state.Click(hitAIExamplesRefresh)
	_, texts = paintAt(state, panelWide)
	if !hasText(texts, state.messages.AIPanel.Example4) || hasText(texts, state.messages.AIPanel.Example1) {
		t.Fatalf("refresh did not rotate the displayed cards: %q", texts)
	}
	state.Click(hitAIExamples.At(0))
	if state.ai.input.String() != state.messages.AIPanel.Example4 || state.ai.run != nil {
		t.Fatalf("click sent or selected the wrong prompt: %q", state.ai.input.String())
	}
	state.aiNewConversation()
	_, texts = paintAt(state, panelWide)
	if !hasText(texts, state.messages.AIPanel.Example1) {
		t.Fatalf("new conversation did not continue the cycle: %q", texts)
	}
}

func TestSuggestionLibraryChangeUnarmsOldCommand(t *testing.T) {
	state, _ := panelState(t, nil)
	state.ai.suggestions.armed = aiSuggestion{ID: "connected-1", Text: "old", Command: "echo stale"}
	entry := state.aiSuggestionPool(aiSuggestionConnected)[0]
	entry.Text = "new"
	if err := state.aiSaveSuggestion(entry); err != nil {
		t.Fatal(err)
	}
	if state.ai.suggestions.armed.Command != "" {
		t.Fatalf("library edit left an old command armed: %+v", state.ai.suggestions.armed)
	}
}

func TestSuggestionClickCannotSelectAStaleContextCard(t *testing.T) {
	state, _ := panelState(t, nil)
	withModel(t, state)
	state.ai.open = true
	paintAt(state, panelWide)
	if state.ai.suggestions.visible[0].Context != aiSuggestionConnected {
		t.Fatal("test needs a connected card on screen")
	}
	state.ai.targetSet = true
	state.ai.target = gate.SessionInfo{}
	state.Click(hitAIExamples.At(0))
	if state.ai.input.String() != "" || state.ai.suggestions.armed.Command != "" {
		t.Fatal("a stale on-screen card selected a new context's question")
	}
}

func TestSuggestionEmptyLibraryOffersManagement(t *testing.T) {
	state, _ := panelState(t, nil)
	withModel(t, state)
	for _, entry := range state.aiSuggestionPool(aiSuggestionConnected) {
		entry.Disabled = true
		if err := state.aiSaveSuggestion(entry); err != nil {
			t.Fatal(err)
		}
	}
	state.ai.open = true
	canvas, texts := paintAt(state, panelWide)
	if !hasText(texts, state.messages.AIPanel.NoSuggestions) {
		t.Fatalf("empty library gives no guidance: %q", texts)
	}
	panelHit(t, canvas, hitAIManageSuggestions)
	state.Click(hitAIManageSuggestions)
	if state.activeSection != sectionSettings || state.settingsCategory != settingsAssistant {
		t.Fatal("empty library cannot be managed from the panel")
	}
}

func TestSuggestionBindingDoesNotSurviveSwitchingAwayAndBack(t *testing.T) {
	state := targetTestWorld(t, quietModel{"ok"})
	state.activeSection = sectionTerminal
	if !state.activateTab(0) {
		t.Fatal("could not select the first terminal tab")
	}
	state.ai.suggestions.armed = aiSuggestion{Text: "inspect", Command: "echo old-target"}
	state.ai.suggestions.armedKey = state.aiSuggestionContextKey()
	if !state.activateTab(1) || !state.activateTab(0) {
		t.Fatal("could not switch terminal tabs")
	}
	if state.ai.suggestions.armed.Command != "" {
		t.Fatal("switching away and back kept a previously bound command")
	}
}
