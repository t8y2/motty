package workbench

import (
	"strings"
	"testing"
	"unicode/utf8"

	"motty/internal/ai/agent"
	"motty/internal/ai/llm"
)

func TestSuggestionSnapshotRedactsAndBoundsSelectedTerminalOutput(t *testing.T) {
	state, shell := panelState(t, nil)
	state.ai.settings.redact = false
	shell.say("\r\n" + strings.Repeat("long-output-", 500) + "\r\npassword=secretvalue\r\nlatest result\r\n")
	state.Wake()
	snapshot := state.aiSuggestionSnapshot()
	if len(snapshot) > 4096+150 || !utf8.ValidString(snapshot) || strings.Contains(snapshot, "secretvalue") || !strings.Contains(snapshot, "latest result") {
		t.Fatalf("snapshot failed bound/redaction: bytes=%d text=%q", len(snapshot), snapshot)
	}
	if strings.Count(snapshot, "\n") > 31 {
		t.Fatalf("snapshot exceeded 30 output lines: %d", strings.Count(snapshot, "\n"))
	}
}

func TestSuggestionSnapshotUsesOnlyTheExplicitlySelectedTerminal(t *testing.T) {
	state := targetTestWorld(t, quietModel{"ok"})
	first, second := state.sessions[0].ID, state.sessions[1].ID
	state.sessions[0].term.write([]byte("FIRST_SESSION_ONLY"))
	state.sessions[1].term.write([]byte("SECOND_SESSION_ONLY"))
	state.aiChooseOnlyTarget(hitAITargets.At(int(first)))
	state.activeSessionID = second
	snapshot := state.aiSuggestionSnapshot()
	if !strings.Contains(snapshot, "FIRST_SESSION_ONLY") || strings.Contains(snapshot, "SECOND_SESSION_ONLY") {
		t.Fatalf("suggestion context crossed selected terminals: %q", snapshot)
	}
}

func TestSuggestionSnapshotNeverExportsConcealedCells(t *testing.T) {
	state, shell := panelState(t, nil)
	shell.say("\x1b[8mHIDDEN_SENSITIVE_VALUE\x1b[0m visible output\r\n")
	state.Wake()
	snapshot := state.aiSuggestionSnapshot()
	if strings.Contains(snapshot, "HIDDEN_SENSITIVE_VALUE") || !strings.Contains(snapshot, "visible output") {
		t.Fatalf("concealed terminal cells leaked or visible data lost: %q", snapshot)
	}
}

func TestSuggestionSnapshotByteLimitPreservesUTF8(t *testing.T) {
	clipped := aiSuggestionClip(strings.Repeat("测", 1500), 4096)
	if len(clipped) > 4096 || !utf8.ValidString(clipped) {
		t.Fatalf("invalid bounded UTF-8 output: %d bytes", len(clipped))
	}
}

func TestSuggestionResponseRequiresThreeShortDistinctQuestions(t *testing.T) {
	for _, input := range []string{
		`["one", "two"]`,
		`["one", "one", "two"]`,
		`["one", "two", "three", "four"]`,
		"```json\n[\"one\",\"two\",\"three\"]\n```",
		`{"questions":["one","two","three"],"command":"rm -rf /"}`,
	} {
		if got := parseAISuggestions(input); len(got) != 0 {
			t.Fatalf("accepted malformed suggestions %q: %+v", input, got)
		}
	}
	if got := parseAISuggestions(`["Why did SSH fail?","Which service is using this port?","What does this log show?"]`); len(got) != 3 {
		t.Fatalf("valid suggestions = %+v", got)
	}
}

func TestSuggestionGenerationUsesNoToolsOrConversationHistory(t *testing.T) {
	model := &scriptedModel{turn: func(_ int, request llm.Request) []llm.Event {
		return textTurn(`["Inspect current output?","Check disk usage?","Review the latest error?"]`)
	}}
	state, _ := assistantTestSession(t, model)
	state.ai.open = true
	state.ai.settings.suggestionSource = aiSuggestionAI
	state.aiStartSuggestions()
	pumpUntil(t, state, "suggestions to finish", func() bool { return !state.ai.suggestions.generating })
	if len(model.requests) != 1 || len(model.requests[0].Tools) != 0 || len(model.requests[0].Messages) != 1 {
		t.Fatalf("generation request includes tools/history: %+v", model.requests)
	}
	if len(state.ai.suggestions.generated) != 3 || state.ai.suggestions.generated[0].Command != "" || len(state.ai.history) != 0 {
		t.Fatalf("generated suggestions/history = %+v / %+v", state.ai.suggestions.generated, state.ai.history)
	}
	state.aiSuggestionsDone(state.ai.suggestions.generationID-1, []string{"stale", "stale2", "stale3"})
	if state.ai.suggestions.generated[0].Text != "Inspect current output?" {
		t.Fatal("stale generation replaced the selected context's suggestions")
	}
}

func TestSuggestionModeSwitchInvalidatesPendingGeneration(t *testing.T) {
	state, _ := panelState(t, nil)
	state.ai.open = true
	state.ai.settings.suggestionSource = aiSuggestionAI
	state.aiStartSuggestions()
	previous := state.ai.suggestions.generationID
	state.setAIMode(agent.ModeAgent)
	if state.ai.suggestions.generationID == previous {
		t.Fatal("mode switch left the earlier generation eligible to update the screen")
	}
}

func TestSuggestionReopeningUnchangedEmptyPanelDoesNotRegenerate(t *testing.T) {
	model := &scriptedModel{turn: func(_ int, request llm.Request) []llm.Event {
		return textTurn(`["Inspect current output?","Check disk usage?","Review the latest error?"]`)
	}}
	state, _ := assistantTestSession(t, model)
	state.ai.settings.suggestionSource = aiSuggestionAI
	state.Click(hitAIToggle)
	pumpUntil(t, state, "first recommendations", func() bool { return len(state.ai.suggestions.generated) == 3 })
	state.Click(hitAIToggle)
	state.Click(hitAIToggle)
	state.Wake()
	if len(model.requests) != 1 || len(state.ai.suggestions.generated) != 3 {
		t.Fatalf("unchanged empty panel regenerated: requests=%d suggestions=%d", len(model.requests), len(state.ai.suggestions.generated))
	}
}

func TestSuggestionChangingDefaultModelInvalidatesGeneratedQuestions(t *testing.T) {
	model := &scriptedModel{turn: func(_ int, request llm.Request) []llm.Event {
		return textTurn(`["Inspect current output?","Check disk usage?","Review the latest error?"]`)
	}}
	state, _ := assistantTestSession(t, model)
	state.ai.open = true
	if err := state.ai.store.SetSetting("ai.suggestions.source", "ai"); err != nil {
		t.Fatal(err)
	}
	state.reloadAI()
	state.aiStartSuggestions()
	pumpUntil(t, state, "first recommendations", func() bool { return len(state.ai.suggestions.generated) == 3 })
	previous := state.ai.suggestions.generationID
	if err := state.ai.store.SetDefaultModel("p", "another-model"); err != nil {
		t.Fatal(err)
	}
	state.aiWrote()
	if state.ai.suggestions.generationID == previous {
		t.Fatal("switching the model did not invalidate prior recommendations")
	}
}

func TestSuggestionDisablingProviderInvalidatesGeneratedQuestions(t *testing.T) {
	model := &scriptedModel{turn: func(_ int, request llm.Request) []llm.Event {
		return textTurn(`["Inspect current output?","Check disk usage?","Review the latest error?"]`)
	}}
	state, _ := assistantTestSession(t, model)
	state.ai.open = true
	if err := state.ai.store.SetSetting("ai.suggestions.source", "ai"); err != nil {
		t.Fatal(err)
	}
	state.reloadAI()
	state.aiStartSuggestions()
	pumpUntil(t, state, "recommendations", func() bool { return len(state.ai.suggestions.generated) == 3 })
	previous := state.ai.suggestions.generationID
	provider := state.ai.providers[0]
	provider.Enabled = false
	if err := state.ai.store.SaveProvider(provider); err != nil {
		t.Fatal(err)
	}
	state.aiWrote()
	if state.ai.suggestions.generationID == previous || len(state.ai.suggestions.generated) != 0 {
		t.Fatal("disabled provider left old-model suggestions visible")
	}
}

func TestSuggestionInvalidModelResponseFallsBackUntilExplicitRefresh(t *testing.T) {
	model := &scriptedModel{turn: func(_ int, request llm.Request) []llm.Event { return textTurn("not JSON") }}
	state, _ := assistantTestSession(t, model)
	state.ai.open = true
	state.ai.settings.suggestionSource = aiSuggestionAI
	state.aiStartSuggestions()
	pumpUntil(t, state, "invalid suggestion response", func() bool { return state.ai.suggestions.settled })
	_, _, suggestions := state.aiEmptyTexts()
	if suggestions[0] != state.messages.AIPanel.Example1 || len(state.ai.suggestions.generated) != 0 || len(state.ai.history) != 0 {
		t.Fatalf("invalid response did not fall back to library: %+v", suggestions)
	}
	state.aiEnsureSuggestions()
	state.Wake()
	if len(model.requests) != 1 {
		t.Fatal("failed generation retried without a refresh")
	}
}
