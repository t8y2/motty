//go:build !windows

package workbench

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/text"
	"motty/internal/ai/agent"
	"motty/internal/ai/attach"
	"motty/internal/ai/llm"
	"motty/internal/ai/risk"
)

func boundSuggestionSession(t *testing.T, command string) (*State, *fakeShell, *scriptedModel) {
	t.Helper()
	model := &scriptedModel{turn: func(int, llm.Request) []llm.Event { return textTurn("analyzed") }}
	state, shell := assistantSession(t, model)
	state.ai.open = true
	entry := state.aiSuggestionPool(aiSuggestionConnected)[0]
	entry.Command = command
	if err := state.aiSaveSuggestion(entry); err != nil {
		t.Fatal(err)
	}
	state.ai.mode = agent.ModeAgent
	paintAt(state, panelWide)
	state.Click(hitAIExamples.At(0))
	return state, shell, model
}

func TestBoundSuggestionRunsOnlyAfterAgentSendAndGivesOutputToModel(t *testing.T) {
	state, shell, model := boundSuggestionSession(t, "echo bound-probe")
	if strings.Contains(shell.typed(), "bound-probe") {
		t.Fatal("clicking a suggestion executed the command")
	}
	state.ai.mode = agent.ModeAgent
	state.aiSend()
	pumpUntil(t, state, "bound run to end", func() bool { return state.ai.run == nil })
	if strings.Count(shell.typed(), "echo bound-probe") != 1 {
		t.Fatalf("bound command not executed exactly once: %q", shell.typed())
	}
	if len(model.requests) != 1 || len(model.requests[0].Messages) < 3 {
		t.Fatalf("model did not receive the tool exchange: %+v", model.requests)
	}
	message := model.requests[0].Messages[len(model.requests[0].Messages)-1]
	if message.Role != llm.RoleTool || !strings.Contains(message.Text, "bound-probe") {
		t.Fatalf("model did not get the actual command output: %+v", message)
	}
	if len(state.ai.history) < 4 || state.ai.history[1].ToolCalls[0].Name != "terminal_exec" {
		t.Fatalf("command execution missing from conversation history: %+v", state.ai.history)
	}
}

func TestBoundSuggestionRespectsRiskApprovalAndDenial(t *testing.T) {
	state, shell, model := boundSuggestionSession(t, "touch /tmp/motty-suggestion-risk-probe")
	state.ai.mode = agent.ModeAgent
	state.aiSend()
	pumpUntil(t, state, "bound command approval", func() bool { return len(state.ai.approvals) == 1 })
	if strings.Contains(shell.typed(), "motty-suggestion-risk-probe") {
		t.Fatal("command ran before approval")
	}
	if state.ai.approvals[0].request.Summary != "touch /tmp/motty-suggestion-risk-probe" {
		t.Fatalf("approval did not show the exact command: %+v", state.ai.approvals[0].request)
	}
	state.aiAnswerApproval(false)
	pumpUntil(t, state, "denial to end", func() bool { return state.ai.run == nil })
	if strings.Contains(shell.typed(), "motty-suggestion-risk-probe") || len(model.requests) != 0 {
		t.Fatal("denied command was run or reported to the model")
	}
}

func TestBoundSuggestionDoesNotRunInAskModeOrAfterEditing(t *testing.T) {
	for _, scenario := range []struct {
		name string
		edit bool
		mode agent.Mode
	}{
		{name: "ask mode", mode: agent.ModeAsk},
		{name: "edited prompt", mode: agent.ModeAgent, edit: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			state, shell, model := boundSuggestionSession(t, "echo should-not-run")
			state.ai.mode = scenario.mode
			if scenario.edit {
				state.ai.input.Insert(" with a change")
			}
			state.aiSend()
			pumpUntil(t, state, "answer to end", func() bool { return state.ai.run == nil })
			if strings.Contains(shell.typed(), "should-not-run") || len(model.requests) != 1 {
				t.Fatalf("unexpected command execution: %q; requests %+v", shell.typed(), model.requests)
			}
		})
	}
}

func TestBoundSuggestionDoesNotFollowAChangedTerminal(t *testing.T) {
	state, shell, model := boundSuggestionSession(t, "echo wrong-terminal")
	state.ai.mode = agent.ModeAgent
	state.ai.suggestions.armedKey = "another-terminal"
	state.aiSend()
	pumpUntil(t, state, "answer to end", func() bool { return state.ai.run == nil })
	if strings.Contains(shell.typed(), "wrong-terminal") || len(model.requests) != 1 {
		t.Fatalf("stale binding followed another target: typed=%q requests=%+v", shell.typed(), model.requests)
	}
}

func TestBoundSuggestionHonorsAssistantAutoRunThreshold(t *testing.T) {
	path := filepath.Join(t.TempDir(), "created-by-bound-command")
	state, _, model := boundSuggestionSession(t, fmt.Sprintf("touch %q", path))
	state.ai.settings.autoUpTo = risk.Medium
	state.aiSend()
	pumpUntil(t, state, "auto-approved bound run", func() bool { return state.ai.run == nil })
	if len(state.ai.approvals) != 0 || len(model.requests) != 1 {
		t.Fatalf("medium auto-run did not use assistant policy: approvals=%d requests=%d", len(state.ai.approvals), len(model.requests))
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("auto-approved command did not run: %v", err)
	}
}

func TestTypingSameQuestionWithoutSelectingCardCannotRunBoundCommand(t *testing.T) {
	state, shell, model := boundSuggestionSession(t, "echo typed-but-not-armed")
	state.ai.suggestions.disarm()
	question := state.ai.input.String()
	state.ai.input = text.Editor{}
	state.ai.input.Insert(question)
	state.aiSend()
	pumpUntil(t, state, "typed question to finish", func() bool { return state.ai.run == nil })
	if strings.Contains(shell.typed(), "typed-but-not-armed") || len(model.requests) != 1 {
		t.Fatal("manually entered question bypassed card selection")
	}
}

func TestBoundCommandOutputIsRedactedBeforeModelEvenWhenConversationRedactionIsOff(t *testing.T) {
	state, _, model := boundSuggestionSession(t, "echo password=secretvalue")
	state.ai.settings.redact = false
	state.aiSend()
	pumpUntil(t, state, "bound command answer", func() bool { return state.ai.run == nil })
	if len(model.requests) != 1 {
		t.Fatalf("model requests = %d", len(model.requests))
	}
	tool := model.requests[0].Messages[len(model.requests[0].Messages)-1]
	if tool.Role != llm.RoleTool || strings.Contains(tool.Text, "secretvalue") || !strings.Contains(tool.Text, "***") {
		t.Fatalf("model received sensitive bound output: %+v", tool)
	}
}

func TestBoundSuggestionEditThenUndoCannotRearmCommand(t *testing.T) {
	state, shell, model := boundSuggestionSession(t, "echo never-after-edit")
	state.aiPanelText(app.TextEvent{Kind: app.TextInsert, Text: " extra"})
	state.aiPanelText(app.TextEvent{Kind: app.TextCommandEvent, Command: app.TextDeleteWordBackward})
	state.aiPanelText(app.TextEvent{Kind: app.TextCommandEvent, Command: app.TextDeleteBackward})
	if state.ai.input.String() != state.ai.suggestions.visible[0].Text {
		t.Fatalf("test did not restore original prompt: %q", state.ai.input.String())
	}
	state.aiSend()
	pumpUntil(t, state, "edited answer", func() bool { return state.ai.run == nil })
	if strings.Contains(shell.typed(), "never-after-edit") || len(model.requests) != 1 {
		t.Fatal("editing and undoing rearmed the command")
	}
}

func TestBoundSuggestionPreservesAttachedImageInPreModelExchange(t *testing.T) {
	state, _, model := boundSuggestionSession(t, "echo image-context")
	state.ai.attachments = []aiAttachment{{file: attach.File{
		Name: "shot.png", Kind: attach.KindImage,
		Image: llm.Image{MediaType: llm.ImagePNG, Data: redSquarePNG(t)},
	}}}
	state.aiSend()
	pumpUntil(t, state, "image-backed answer", func() bool { return state.ai.run == nil })
	if len(model.requests) != 1 || len(model.requests[0].Messages) == 0 || len(model.requests[0].Messages[0].Images) != 1 {
		t.Fatalf("bound command dropped attached image in %d requests", len(model.requests))
	}
}
