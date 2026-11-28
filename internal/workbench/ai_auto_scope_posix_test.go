//go:build !windows

package workbench

import (
	"strings"
	"testing"

	"motty/internal/ai/agent"
	"motty/internal/ai/llm"
	"motty/internal/ai/risk"
	"motty/internal/ai/tools"
)

// The commands in this test run in a real POSIX shell (assistantSession), as
// the other command-execution tests do, so it is not built for Windows.
// The scope stays live while a request runs (issue #1703). A pick made while
// the request waits on a question leaves that question to the user, and the
// request's next action runs under the new scope.
func TestApprovalScopeChangeReachesTheRunningRequest(t *testing.T) {
	model := &scriptedModel{turn: func(index int, _ llm.Request) []llm.Event {
		switch index {
		case 0:
			return toolTurn(tools.NameTerminalExec, `{"command":"cp /dev/null /dev/null #first"}`)
		case 1:
			return toolTurn(tools.NameTerminalExec, `{"command":"cp /dev/null /dev/null #second"}`)
		default:
			return textTurn("done")
		}
	}}
	state, shell := assistantSession(t, model)
	state.ai.open = true
	ask(state, "make two files", agent.ModeAgent)
	pumpUntil(t, state, "the first question", func() bool { return len(state.ai.approvals) == 1 })

	canvas, _ := paintAt(state, panelWide)
	if chip := panelHit(t, canvas, hitAIAutoScope); chip.Disabled || chip.Hint != state.messages.AIPanel.AutoScopeHint {
		t.Fatalf("chip while a request waits: disabled=%v hint=%q", chip.Disabled, chip.Hint)
	}
	state.Click(hitAIAutoScope)
	paintAt(state, panelWide)
	clickAIMenuRow(t, state, state.messages.AIPanel.RiskMedium)
	if state.aiAutoUpTo() != risk.Medium {
		t.Fatalf("scope is %v after choosing medium mid-request", state.aiAutoUpTo())
	}
	if len(state.ai.approvals) != 1 || strings.Contains(shell.typed(), "cp /dev/null /dev/null #first") {
		t.Fatal("changing the scope answered the question already asked")
	}

	state.aiAnswerApproval(true)
	pumpUntil(t, state, "the run to finish or ask again", func() bool { return state.ai.run == nil || len(state.ai.approvals) > 0 })
	if len(state.ai.approvals) > 0 {
		t.Fatalf("the next medium action still asked: %+v", state.ai.approvals[0].request)
	}
	if typed := shell.typed(); !strings.Contains(typed, "cp /dev/null /dev/null #first") || !strings.Contains(typed, "cp /dev/null /dev/null #second") {
		t.Fatalf("the commands did not both run: %q", typed)
	}
}
