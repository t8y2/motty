//go:build !windows

package workbench

import (
	"strings"
	"testing"

	"motty/internal/ai/agent"
	"motty/internal/ai/llm"
)

// An empty turn the provider cut off at its output limit must say so: the
// model used the whole budget before any prose arrived, so there is nothing to
// show, but it is a truncation and not a failed answer.
func TestEmptyAnswerCutOffAtTheOutputLimitSaysSo(t *testing.T) {
	model := &scriptedModel{turn: func(int, llm.Request) []llm.Event {
		return []llm.Event{{Kind: llm.EventDone, StopReason: "length"}}
	}}
	state, _ := assistantSession(t, model)
	ask(state, "tell me everything", agent.ModeAgent)
	pumpUntil(t, state, "the run to end", func() bool { return state.ai.run == nil })

	truncated := false
	for _, item := range state.ai.items {
		if item.kind != aiNotice {
			continue
		}
		if strings.Contains(item.text, state.messages.AIPanel.AnswerTruncated) {
			truncated = true
		}
		if strings.Contains(item.text, "empty answer") {
			t.Fatalf("the truncation was reported as an empty answer: %q", item.text)
		}
	}
	if !truncated {
		t.Fatalf("no truncation notice: %+v", state.ai.items)
	}
}

// An answer the provider cut off at its output limit must say so: half an
// answer read as the whole of it is how a reply looks like it "stopped".
func TestCutOffAnswerSaysSo(t *testing.T) {
	model := &scriptedModel{turn: func(int, llm.Request) []llm.Event {
		return []llm.Event{
			{Kind: llm.EventText, Text: "Half an answer"},
			{Kind: llm.EventDone, StopReason: "length"},
		}
	}}
	state, _ := assistantSession(t, model)
	ask(state, "tell me everything", agent.ModeAgent)
	pumpUntil(t, state, "the run to end", func() bool { return state.ai.run == nil })

	for _, item := range state.ai.items {
		if item.kind == aiNotice && strings.Contains(item.text, state.messages.AIPanel.AnswerTruncated) {
			return
		}
	}
	t.Fatalf("the cut-off answer was presented as complete: %+v", state.ai.items)
}
