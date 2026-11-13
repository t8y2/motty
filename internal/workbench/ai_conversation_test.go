//go:build !windows

package workbench

import (
	"strings"
	"testing"

	"github.com/vivaui/viva/text"
	"motty/internal/ai/agent"
	"motty/internal/ai/llm"
)

// TestUsageAccumulatesAcrossRuns proves the token accounting of the finished
// run and the conversation total are kept and shown.
func TestUsageAccumulatesAcrossRuns(t *testing.T) {
	model := &scriptedModel{turn: func(index int, request llm.Request) []llm.Event {
		if index == 1 {
			return []llm.Event{
				{Kind: llm.EventText, Text: "two"},
				{Kind: llm.EventDone, Usage: llm.Usage{InputTokens: 30, OutputTokens: 5}},
			}
		}
		return textTurn("one")
	}}
	state, _ := assistantSession(t, model)
	ask(state, "first question", agent.ModeAsk)
	pumpUntil(t, state, "the first run to end", func() bool { return state.ai.run == nil })
	if state.ai.lastUsage.InputTokens != 0 {
		t.Fatalf("a run without usage must not report tokens: %+v", state.ai.lastUsage)
	}
	ask(state, "second question", agent.ModeAsk)
	pumpUntil(t, state, "the second run to end", func() bool { return state.ai.run == nil })
	if state.ai.lastUsage.InputTokens != 30 || state.ai.lastUsage.OutputTokens != 5 {
		t.Fatalf("last usage = %+v", state.ai.lastUsage)
	}
	if state.ai.totalUsage.InputTokens != 30 {
		t.Fatalf("total usage = %+v", state.ai.totalUsage)
	}
	usage, ok := state.aiRunUsage()
	if !ok || usage.usage.InputTokens != 30 || usage.usage.OutputTokens != 5 {
		t.Fatalf("usage = %+v (present %v)", usage, ok)
	}
}

// TestUsageCardShowsCachedTokens proves a reader can see how much of the prompt
// the provider served from its cache, and sees nothing when it served none.
func TestUsageCardShowsCachedTokens(t *testing.T) {
	state, _ := assistantSession(t, &scriptedModel{turn: func(int, llm.Request) []llm.Event { return textTurn("x") }})
	state.ai.lastRun = &aiRunStats{phase: aiRunCompleted, usage: llm.Usage{InputTokens: 12000, OutputTokens: 90}}
	usage, ok := state.aiRunUsage()
	if !ok {
		t.Fatal("a finished request has no usage to show")
	}
	for _, row := range usage.rows(state.messages.AIPanel) {
		if strings.Contains(row.value, "9,800") {
			t.Fatalf("usage card mentions a cache that was not hit: %+v", row)
		}
	}
	state.ai.lastRun.usage.CacheReadTokens = 9800
	usage, _ = state.aiRunUsage()
	if rate, hit := usage.hitRate(); !hit || rate != "81.7%" {
		t.Fatalf("cache hit rate = %q (reported %v)", rate, hit)
	}
	cached := false
	for _, row := range usage.rows(state.messages.AIPanel) {
		cached = cached || strings.Contains(row.value, "9,800")
	}
	if !cached {
		t.Fatalf("usage card misses the cached tokens: %+v", usage.rows(state.messages.AIPanel))
	}
}

// TestRenameConversationDialogRenames proves the saved conversation's title
// changes through the dialog.
func TestRenameConversationDialogRenames(t *testing.T) {
	state, _ := assistantSession(t, &scriptedModel{turn: func(int, llm.Request) []llm.Event {
		return textTurn("answer")
	}})
	ask(state, "a question with a long enough title", agent.ModeAsk)
	pumpUntil(t, state, "the run to end", func() bool { return state.ai.run == nil })
	state.aiPanelLoadHistory()
	if len(state.ai.conversations) != 1 {
		t.Fatalf("saved conversations: %d", len(state.ai.conversations))
	}
	state.openAIRename(0)
	if state.modal != modalAIRename {
		t.Fatal("the rename dialog did not open")
	}
	state.ai.rename.editor = text.Editor{}
	state.ai.rename.editor.Insert("renamed title")
	state.clickAIRenameDialog(hitAIRenameSubmit)
	if state.modal != modalNone {
		t.Fatalf("dialog still open: %v", state.modal)
	}
	list, err := state.ai.store.Conversations("", 10)
	if err != nil || len(list) != 1 {
		t.Fatalf("conversations after rename: %d, %v", len(list), err)
	}
	if list[0].Title != "renamed title" {
		t.Fatalf("title = %q", list[0].Title)
	}
}
