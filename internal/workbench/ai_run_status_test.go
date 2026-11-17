package workbench

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"
	"motty/internal/ai/agent"
	"motty/internal/ai/llm"
	"motty/internal/ai/tools"
	"motty/internal/i18n"
)

func TestAIRunShowsActivityBeforeFirstOutputAndSchedulesRepaint(t *testing.T) {
	state, _ := panelState(t, nil)
	withModel(t, state)
	state.setLanguage(i18n.English)
	state.ai.open = true
	state.ai.items = []aiItem{{kind: aiUser, text: "test conversation"}}
	state.ai.run = &aiRun{cancel: func() {}, open: -1, stats: aiRunStats{startedAt: 2 * time.Second}}
	state.options.Now = func() time.Duration { return 10 * time.Second }
	canvas, texts := paintAt(state, panelWide)
	for _, want := range []string{"Waiting for AI", "8.0s"} {
		if !hasText(texts, want) {
			t.Errorf("missing %q before first output: %q", want, texts)
		}
	}
	if hasText(texts, "Tokens") || len(state.ai.panel.usageLines) != 0 {
		t.Fatalf("running request shows token details: %q", texts)
	}
	if _, scheduled := canvas.NextFrame(); !scheduled {
		t.Fatal("silent provider left activity and elapsed time frozen")
	}
}

func TestAIRunShowsFinalTimeAndTokensWithoutHover(t *testing.T) {
	for _, outcome := range []struct {
		name string
		err  error
		want string
	}{
		{name: "completed", want: "Completed"},
		{name: "cancelled", err: context.Canceled, want: "Stopped"},
		{name: "failed", err: errors.New("connection lost"), want: "Failed"},
	} {
		t.Run(outcome.name, func(t *testing.T) {
			state, _ := panelState(t, nil)
			withModel(t, state)
			state.setLanguage(i18n.English)
			state.ai.open = true
			state.ai.items = []aiItem{{kind: aiUser, text: "question"}, {kind: aiAssistant, text: "answer"}}
			run := &aiRun{cancel: func() {}, open: -1, stats: aiRunStats{startedAt: 2 * time.Second}}
			state.ai.run = run
			state.options.Now = func() time.Duration { return 10 * time.Second }
			state.aiEvent(run, agent.Event{Kind: agent.EventDone, Usage: llm.Usage{InputTokens: 1200, OutputTokens: 84}})
			state.aiFinished(run, nil, outcome.err)
			state.options.Now = func() time.Duration { return time.Hour }
			state.ai.panel.pointer = gfx.Point{}
			_, texts := paintAt(state, panelWide)
			for _, want := range []string{outcome.want, "Total time 8.0s", "1,284", "1,200", "84"} {
				if !hasText(texts, want) {
					t.Errorf("missing %q without hover: %q", want, texts)
				}
			}
			state.aiNewConversation()
			_, texts = paintAt(state, panelWide)
			if hasText(texts, "Total time") {
				t.Fatal("new conversation kept previous request statistics")
			}
		})
	}
}

func TestAIRunDurationIncludesToolAndApprovalWaitsAndFreezesAtFinish(t *testing.T) {
	state, _ := panelState(t, nil)
	now := 2 * time.Second
	state.options.Now = func() time.Duration { return now }
	run := &aiRun{cancel: func() {}, open: -1, stats: aiRunStats{startedAt: now}}
	state.ai.run = run
	state.aiEvent(run, agent.Event{Kind: agent.EventReasoning, Text: "checking"})
	if run.stats.phase != aiRunAnalyzing {
		t.Fatal("reasoning did not update activity")
	}
	state.aiEvent(run, agent.Event{Kind: agent.EventToolStart, Call: llm.ToolCall{ID: "tool", Name: tools.NameTerminalRead}})
	if run.stats.phase != aiRunUsingTool {
		t.Fatal("tool did not update activity")
	}
	now = 10 * time.Second
	state.aiFinished(run, nil, nil)
	now = time.Hour
	if state.ai.lastRun == nil || state.ai.lastRun.duration != 8*time.Second || state.ai.lastRun.phase != aiRunCompleted {
		t.Fatalf("finished metrics = %+v", state.ai.lastRun)
	}
	state.aiNewConversation()
	if state.ai.lastRun != nil || state.ai.totalUsage != (llm.Usage{}) {
		t.Fatal("new conversation kept the previous request's statistics")
	}
}

func clockedEscalationState(t *testing.T) (*State, *scriptedModel, *time.Duration) {
	t.Helper()
	model := &scriptedModel{turn: func(index int, _ llm.Request) []llm.Event {
		if index == 0 {
			return []llm.Event{
				{Kind: llm.EventToolCall, ToolCall: &llm.ToolCall{
					ID: "switch", Name: tools.NameRequestAgentMode,
					Arguments: `{"reason":"inspect host","command":"inspect"}`,
				}},
				{Kind: llm.EventDone, Usage: llm.Usage{InputTokens: 30, OutputTokens: 5}},
			}
		}
		return []llm.Event{
			{Kind: llm.EventText, Text: "finished"},
			{Kind: llm.EventDone, Usage: llm.Usage{InputTokens: 40, OutputTokens: 6}},
		}
	}}
	state, _ := assistantTestSession(t, model)
	state.setLanguage(i18n.English)
	state.ai.open = true
	now := 2 * time.Second
	state.options.Now = func() time.Duration { return now }
	ask(state, "inspect host", agent.ModeAsk)
	pumpUntil(t, state, "the escalation approval", func() bool {
		return state.ai.run == nil && state.ai.pendingRun != nil && len(state.ai.approvals) == 1
	})
	return state, model, &now
}

func TestAIRunPendingEscalationKeepsElapsedTimeAdvancing(t *testing.T) {
	state, _, now := clockedEscalationState(t)
	*now = 11 * time.Second

	status, usage := state.aiRunStatusText()
	if !strings.Contains(status, "Waiting for your approval") || !strings.Contains(status, "Elapsed 9.0s") {
		t.Fatalf("pending status = %q", status)
	}
	if usage != "" {
		t.Fatalf("pending approval exposed final usage: %q", usage)
	}
	if state.ai.lastUsage != (llm.Usage{}) || state.ai.totalUsage != (llm.Usage{}) {
		t.Fatalf("pending leg was accounted as a finished request: last=%+v total=%+v", state.ai.lastUsage, state.ai.totalUsage)
	}
	canvas, _ := paintAt(state, panelWide)
	if _, scheduled := canvas.NextFrame(); !scheduled {
		t.Fatal("pending approval left elapsed time frozen")
	}
}

func TestAIRunApprovedEscalationKeepsOneRequestTimeline(t *testing.T) {
	state, _, now := clockedEscalationState(t)
	*now = 7 * time.Second
	state.aiAnswerApproval(true)
	status, _ := state.aiRunStatusText()
	if !strings.Contains(status, "Elapsed 5.0s") {
		t.Fatalf("approved retry reset elapsed time: %q", status)
	}
	*now = 13 * time.Second
	pumpUntil(t, state, "the approved retry", func() bool { return state.ai.run == nil })

	wantUsage := llm.Usage{InputTokens: 70, OutputTokens: 11}
	if state.ai.pendingRun != nil || state.ai.mode != agent.ModeAgent {
		t.Fatalf("approved lifecycle is still pending: mode=%v pending=%+v", state.ai.mode, state.ai.pendingRun)
	}
	if state.ai.lastRun == nil || state.ai.lastRun.duration != 11*time.Second || state.ai.lastRun.phase != aiRunCompleted {
		t.Fatalf("approved final metrics = %+v", state.ai.lastRun)
	}
	if state.ai.lastUsage != wantUsage || state.ai.totalUsage != wantUsage {
		t.Fatalf("approved usage last=%+v total=%+v, want %+v", state.ai.lastUsage, state.ai.totalUsage, wantUsage)
	}
	status, _ = state.aiRunStatusText()
	if !strings.Contains(status, "Total time 11.0s") {
		t.Fatalf("approved final status = %q", status)
	}
}

func TestAIRunDeclinedEscalationKeepsOneRequestTimeline(t *testing.T) {
	state, model, now := clockedEscalationState(t)
	*now = 9 * time.Second
	state.aiAnswerApproval(false)
	status, _ := state.aiRunStatusText()
	if !strings.Contains(status, "Elapsed 7.0s") {
		t.Fatalf("declined retry reset elapsed time: %q", status)
	}
	*now = 15 * time.Second
	pumpUntil(t, state, "the declined retry", func() bool { return state.ai.run == nil })

	if state.ai.pendingRun != nil || state.ai.mode != agent.ModeAsk {
		t.Fatalf("declined lifecycle is still pending: mode=%v pending=%+v", state.ai.mode, state.ai.pendingRun)
	}
	if len(model.requests) != 2 || len(model.requests[1].Tools) != 0 {
		t.Fatalf("declined retry requests = %+v", model.requests)
	}
	if state.ai.lastRun == nil || state.ai.lastRun.duration != 13*time.Second || state.ai.lastRun.phase != aiRunCompleted {
		t.Fatalf("declined final metrics = %+v", state.ai.lastRun)
	}
	wantUsage := llm.Usage{InputTokens: 70, OutputTokens: 11}
	if state.ai.lastUsage != wantUsage || state.ai.totalUsage != wantUsage {
		t.Fatalf("declined usage last=%+v total=%+v, want %+v", state.ai.lastUsage, state.ai.totalUsage, wantUsage)
	}
	status, _ = state.aiRunStatusText()
	if !strings.Contains(status, "Total time 13.0s") {
		t.Fatalf("declined final status = %q", status)
	}
}

func TestAIRunRetainsReportedUsageOnEveryExit(t *testing.T) {
	for _, scenario := range []struct {
		name  string
		event agent.Event
		err   error
	}{
		{name: "completed", event: agent.Event{Kind: agent.EventDone}},
		{name: "failed", event: agent.Event{Kind: agent.EventError, Text: "connection lost"}, err: errors.New("connection lost")},
		{name: "cancelled during tool", event: agent.Event{Kind: agent.EventToolStart, Call: llm.ToolCall{ID: "tool", Name: tools.NameTerminalRead}}, err: context.Canceled},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			state, _ := panelState(t, nil)
			run := &aiRun{cancel: func() {}, open: -1}
			state.ai.run = run
			state.ai.totalUsage = llm.Usage{InputTokens: 100, OutputTokens: 20}
			scenario.event.Usage = llm.Usage{InputTokens: 30, OutputTokens: 5, CacheReadTokens: 10}
			state.aiEvent(run, scenario.event)
			state.aiFinished(run, nil, scenario.err)
			if got := state.ai.lastUsage; got != scenario.event.Usage {
				t.Fatalf("last usage = %+v, want %+v", got, scenario.event.Usage)
			}
			want := llm.Usage{InputTokens: 130, OutputTokens: 25, CacheReadTokens: 10}
			if got := state.ai.totalUsage; got != want {
				t.Fatalf("conversation usage = %+v, want %+v", got, want)
			}
			state.aiFinished(run, nil, scenario.err)
			if state.ai.totalUsage != want {
				t.Fatal("late completion counted the same request twice")
			}
		})
	}
}

func TestAIRunCancelledToolDoesNotRemainRunning(t *testing.T) {
	state, _ := panelState(t, nil)
	run := &aiRun{cancel: func() {}, open: -1}
	state.ai.run = run
	state.aiEvent(run, agent.Event{Kind: agent.EventToolStart, Call: llm.ToolCall{ID: "tool", Name: tools.NameTerminalRead}})
	state.aiStop()
	state.aiFinished(run, nil, context.Canceled)
	if state.ai.items[0].status == aiToolRunning || state.ai.items[0].status == aiToolWaiting {
		t.Fatal("cancelled request left its tool looking active")
	}
}

func TestAIRunTokensStayHiddenUntilTheRequestEnds(t *testing.T) {
	state, _ := panelState(t, nil)
	state.setLanguage(i18n.English)
	state.ai.totalUsage = llm.Usage{InputTokens: 1000, OutputTokens: 200}
	state.ai.lastUsage = state.ai.totalUsage
	run := &aiRun{cancel: func() {}, open: -1}
	state.ai.run = run
	_, usage := state.aiRunStatusText()
	if usage != "" {
		t.Fatalf("new request shows token details: %q", usage)
	}
	state.aiEvent(run, agent.Event{Kind: agent.EventToolStart, Call: llm.ToolCall{ID: "tool", Name: tools.NameTerminalRead}, Usage: llm.Usage{InputTokens: 30, OutputTokens: 5, CacheReadTokens: 10}})
	status, usage := state.aiRunStatusText()
	if usage != "" {
		t.Fatalf("running tool shows token details: %q", usage)
	}
	if !strings.Contains(status, "Running:") {
		t.Fatalf("tool activity = %q", status)
	}
	state.ai.approvals = []*aiApproval{{reply: make(chan aiAnswer, 1)}}
	status, _ = state.aiRunStatusText()
	if !strings.Contains(status, "Waiting for your approval") {
		t.Fatalf("approval activity = %q", status)
	}
	state.aiFinished(run, nil, nil)
	_, usage = state.aiRunStatusText()
	for _, want := range []string{"35", "30 in", "5 out", "10 cached", "1,235"} {
		if !strings.Contains(usage, want) {
			t.Errorf("missing %q in final usage %q", want, usage)
		}
	}
}

func TestAIRunDroppingEscalationUpdatesFinalStatus(t *testing.T) {
	state, _ := panelState(t, nil)
	state.setLanguage(i18n.English)
	run := &aiRun{cancel: func() {}, open: -1}
	state.ai.run = run
	state.aiEvent(run, agent.Event{Kind: agent.EventAgentModeRequest, Call: llm.ToolCall{Name: tools.NameRequestAgentMode, Arguments: `{"reason":"inspect host"}`}})
	state.aiFinished(run, nil, nil)
	status, _ := state.aiRunStatusText()
	if !strings.Contains(status, "Waiting for your approval") {
		t.Fatalf("missing pending escalation: %q", status)
	}
	state.setAIMode(agent.ModeAgent)
	status, _ = state.aiRunStatusText()
	if len(state.ai.approvals) != 0 || strings.Contains(status, "Waiting for your approval") {
		t.Fatalf("dismissed escalation still needs approval: %q", status)
	}
}

func TestAIRunInterruptedUsageRemainsClearlyIncomplete(t *testing.T) {
	for _, stopError := range []error{context.Canceled, errors.New("connection lost")} {
		t.Run(stopError.Error(), func(t *testing.T) {
			state, _ := panelState(t, nil)
			state.setLanguage(i18n.English)
			run := &aiRun{cancel: func() {}, open: -1}
			state.ai.run = run
			state.aiEvent(run, agent.Event{Kind: agent.EventToolStart, Call: llm.ToolCall{ID: "tool", Name: tools.NameTerminalRead}, Usage: llm.Usage{InputTokens: 40, OutputTokens: 6}})
			state.aiEvent(run, agent.Event{Kind: agent.EventText, Text: "partial second turn"})
			state.aiFinished(run, nil, stopError)
			_, usage := state.aiRunStatusText()
			if !strings.Contains(usage, "46") || !strings.Contains(usage, "may be incomplete") {
				t.Fatalf("interrupted usage looks complete: %q", usage)
			}
		})
	}
}

func TestAIRunDeclinedEscalationDoesNotWaitWhenRetryCannotStart(t *testing.T) {
	state, _ := panelState(t, nil)
	state.setLanguage(i18n.English)
	run := &aiRun{cancel: func() {}, open: -1}
	state.ai.run = run
	state.ai.history = []llm.Message{{Role: llm.RoleUser, Text: "inspect host"}}
	state.aiEvent(run, agent.Event{Kind: agent.EventAgentModeRequest, Call: llm.ToolCall{Name: tools.NameRequestAgentMode, Arguments: `{"reason":"inspect host"}`}})
	state.aiFinished(run, nil, nil)
	state.aiAnswerApproval(false)
	status, _ := state.aiRunStatusText()
	if len(state.ai.approvals) != 0 || strings.Contains(status, "Waiting for your approval") {
		t.Fatalf("declined escalation left an impossible approval: %q", status)
	}
}

func TestAIRunMixedReportedAndUnknownTurnsRemainIncomplete(t *testing.T) {
	model := &scriptedModel{turn: func(index int, request llm.Request) []llm.Event {
		if index == 0 {
			return []llm.Event{
				{Kind: llm.EventToolCall, ToolCall: &llm.ToolCall{ID: "tool", Name: tools.NameTerminalRead, Arguments: `{}`}},
				{Kind: llm.EventDone, Usage: llm.Usage{InputTokens: 40, OutputTokens: 6}},
			}
		}
		return textTurn("answer without reported usage")
	}}
	state, _ := assistantTestSession(t, model)
	state.setLanguage(i18n.English)
	ask(state, "inspect terminal", agent.ModeAgent)
	pumpUntil(t, state, "the answer to finish", func() bool { return state.ai.run == nil })
	_, usage := state.aiRunStatusText()
	if !strings.Contains(usage, "46") || !strings.Contains(usage, "may be incomplete") {
		t.Fatalf("mixed reported and unknown turns look complete: %q", usage)
	}
}
