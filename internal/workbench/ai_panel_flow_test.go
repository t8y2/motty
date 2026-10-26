//go:build !windows

package workbench

import (
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/ui"
	"motty/internal/ai/agent"
	"motty/internal/ai/llm"
)

// The whole path through the window's entry points: type, Enter, a tool card,
// the approval card, the Approve button, the answer.
func TestTypingAQuestionRunsAToolAfterApprovalAndTheAnswerAppears(t *testing.T) {
	model := &scriptedModel{turn: func(index int, request llm.Request) []llm.Event {
		if index == 0 {
			return toolTurn("terminal_exec", `{"command":"touch created-by-assistant"}`)
		}
		return textTurn("The file is made.")
	}}
	state, shell := assistantSession(t, model)
	state.Paint(&ui.Canvas{}, panelWide)
	state.Click(hitAIToggle)
	state.Click(hitAIMode)
	if state.ai.mode != agent.ModeAgent {
		t.Fatal("Agent mode not chosen")
	}
	state.Click(hitAIMode)
	if state.ai.mode != agent.ModeAsk {
		t.Fatal("a second click did not return to Ask")
	}
	state.Click(hitAIMode)
	paintAt(state, panelWide)

	input := state.AIPanelInput(app.TextInputContext{ID: hitAIInput})
	if _, err := input.OnEvent(app.TextEvent{Kind: app.TextInsert, Text: "make a file"}); err != nil {
		t.Fatal(err)
	}
	if _, err := input.OnEvent(app.TextEvent{Kind: app.TextCommandEvent, Command: app.TextInsertNewline}); err != nil {
		t.Fatal(err)
	}
	pumpUntil(t, state, "the approval card", func() bool { return len(state.ai.approvals) == 1 })
	canvas, texts := paintAt(state, panelWide)
	if !hasText(texts, "make a file") || !hasText(texts, state.messages.AIPanel.StatusWaiting) || !hasText(texts, "touch created-by-assistant") {
		t.Fatalf("tool card not shown: %q", texts)
	}
	if panelHit(t, canvas, hitAISend).Label != state.messages.AIPanel.Stop {
		t.Fatal("Stop is not offered while running")
	}
	if strings.Contains(shell.typed(), "touch") {
		t.Fatal("ran before the answer")
	}
	state.Click(panelHit(t, canvas, hitAIApprove).ID)
	pumpUntil(t, state, "the run to end", func() bool { return state.ai.run == nil })
	_, texts = paintAt(state, panelWide)
	if !hasText(texts, "The file is made.") || !hasText(texts, state.messages.AIPanel.StatusDone) {
		t.Fatalf("answer not shown: %q", texts)
	}
	if !strings.Contains(shell.typed(), "touch created-by-assistant") {
		t.Fatal("the approved command did not run")
	}
}
