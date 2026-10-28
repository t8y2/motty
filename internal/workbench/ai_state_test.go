//go:build !windows

package workbench

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"

	"motty/internal/ai/agent"
	"motty/internal/ai/llm"
	"motty/internal/ai/tools"
)

// shellThatRuns makes a fake shell that runs what it is typed with /bin/sh and
// echoes the input like a terminal does, as a real remote shell would.
func shellThatRuns(shell *fakeShell) {
	shell.onWrite = func(p []byte) {
		out, _ := exec.Command("/bin/sh", "-c", string(p)).CombinedOutput()
		shell.say(strings.ReplaceAll(string(p), "\n", "\r\n") + strings.ReplaceAll(string(out), "\n", "\r\n"))
	}
}

// The command-execution tests keep their real POSIX shell dependency here.
func assistantSession(t *testing.T, model *scriptedModel) (*State, *fakeShell) {
	t.Helper()
	state, shell := assistantTestSession(t, model)
	shellThatRuns(shell)
	return state, shell
}

func TestAssistantRunsALowRiskCommandInTheVisibleTerminalAndAnswers(t *testing.T) {
	model := &scriptedModel{turn: func(index int, request llm.Request) []llm.Event {
		if index == 0 {
			return toolTurn("terminal_exec", `{"command":"echo hello-from-the-assistant"}`)
		}
		return textTurn("It printed hello.")
	}}
	state, shell := assistantSession(t, model)
	ask(state, "say hello on the server", agent.ModeAgent)
	pumpUntil(t, state, "the run to end", func() bool { return state.ai.run == nil })

	// The model got the real output and exit status back.
	last := model.requests[len(model.requests)-1]
	result := last.Messages[len(last.Messages)-1]
	if result.Role != llm.RoleTool || !strings.Contains(result.Text, "hello-from-the-assistant") || !strings.Contains(result.Text, "exit status 0") {
		t.Fatalf("tool result sent to the model = %+v", result)
	}
	// The user watched it happen, without our bookkeeping marker.
	screen := screenText(state, 0)
	if !strings.Contains(screen, "echo hello-from-the-assistant") || !strings.Contains(screen, "hello-from-the-assistant") {
		t.Fatalf("the command is not visible in the terminal:\n%s", screen)
	}
	if strings.Contains(screen, "GOPAI_") || strings.Contains(screen, "printf") || strings.Contains(screen, "};") {
		t.Fatalf("the capture wrapper leaked into the terminal:\n%s", screen)
	}
	if !strings.Contains(shell.typed(), "echo hello-from-the-assistant") {
		t.Fatal("the command was not typed into the shell")
	}
	// The command's screen line carries the AI gutter mark.
	term := state.sessions[0].term
	marked := false
	_, rows := term.screen.Size()
	for row := 0; row < rows; row++ {
		if _, ok := term.aiRows[term.screen.Row(row).ID]; ok {
			marked = true
			break
		}
	}
	if !marked {
		t.Fatal("the assistant command line has no AI gutter mark")
	}
	canvas, _ := paintAt(state, panelWide)
	bar := withAlpha(state.pal.accent, 0.85)
	painted := false
	for _, rectangle := range canvas.Rectangles() {
		if rectangle.Color == bar && rectangle.Bounds.Width == 3 {
			painted = true
			break
		}
	}
	if !painted {
		t.Fatal("the AI gutter mark was not painted")
	}
	// The conversation is shown and saved. The target separator is a
	// presentation row, not part of the conversation.
	var kinds []aiItemKind
	for _, item := range state.ai.items {
		if item.kind != aiTargetChange {
			kinds = append(kinds, item.kind)
		}
	}
	if len(kinds) < 3 || kinds[0] != aiUser || kinds[len(kinds)-1] != aiAssistant {
		t.Fatalf("items = %+v", state.ai.items)
	}
	conversations, err := state.ai.store.Conversations("", 10)
	if err != nil || len(conversations) != 1 {
		t.Fatalf("saved conversations = %v, %v", conversations, err)
	}
	stored, _ := state.ai.store.Messages(conversations[0].ID)
	if len(stored) != len(state.ai.history) || stored[0].Role != "user" {
		t.Fatalf("stored %d messages, history has %d", len(stored), len(state.ai.history))
	}
}

func TestAMediumRiskCommandWaitsForTheUserAndNothingRunsUntilTheyAnswer(t *testing.T) {
	model := &scriptedModel{turn: func(index int, request llm.Request) []llm.Event {
		if index == 0 {
			return toolTurn("terminal_exec", `{"command":"touch created-by-assistant"}`)
		}
		return textTurn("finished")
	}}
	state, shell := assistantSession(t, model)
	ask(state, "make a file", agent.ModeAgent)
	pumpUntil(t, state, "the approval question", func() bool { return len(state.ai.approvals) == 1 })
	if strings.Contains(shell.typed(), "touch") {
		t.Fatal("the command was typed before the user answered")
	}
	request := state.ai.approvals[0].request
	if request.NeedsText || !strings.Contains(request.Summary, "touch created-by-assistant") {
		t.Fatalf("question = %+v", request)
	}
	state.aiAnswerApproval(true)
	pumpUntil(t, state, "the run to end", func() bool { return state.ai.run == nil })
	if !strings.Contains(shell.typed(), "touch created-by-assistant") {
		t.Fatal("the approved command did not run")
	}
}

func TestDecliningStopsTheCommandAndTheModelIsTold(t *testing.T) {
	model := &scriptedModel{turn: func(index int, request llm.Request) []llm.Event {
		if index == 0 {
			return toolTurn("terminal_exec", `{"command":"rm -rf build"}`)
		}
		return textTurn("understood")
	}}
	state, shell := assistantSession(t, model)
	ask(state, "clean up", agent.ModeAgent)
	pumpUntil(t, state, "the approval question", func() bool { return len(state.ai.approvals) == 1 })
	state.aiAnswerApproval(false)
	pumpUntil(t, state, "the run to end", func() bool { return state.ai.run == nil })
	if strings.Contains(shell.typed(), "rm -rf") {
		t.Fatal("a declined command was typed")
	}
	last := model.requests[len(model.requests)-1]
	if text := last.Messages[len(last.Messages)-1].Text; !strings.Contains(text, "declined") {
		t.Fatalf("the model was told %q", text)
	}
}

func TestACriticalCommandNeedsTheConfirmationWordTyped(t *testing.T) {
	model := &scriptedModel{turn: func(index int, request llm.Request) []llm.Event {
		if index == 0 {
			return toolTurn("terminal_exec", `{"command":"rm -rf /"}`)
		}
		return textTurn("ok")
	}}
	state, shell := assistantSession(t, model)
	ask(state, "wipe it", agent.ModeAgent)
	pumpUntil(t, state, "the approval question", func() bool { return len(state.ai.approvals) == 1 })
	if !state.ai.approvals[0].request.NeedsText {
		t.Fatal("a critical command did not ask for the word")
	}
	state.aiAnswerApproval(true) // nothing typed: ignored
	if len(state.ai.approvals) != 1 {
		t.Fatal("approved without the confirmation word")
	}
	state.aiAnswerApproval(false)
	pumpUntil(t, state, "the run to end", func() bool { return state.ai.run == nil })
	if strings.Contains(shell.typed(), "rm -rf /") {
		t.Fatal("the critical command ran")
	}
}

func TestAskModeOffersOnlyEscalationAndSendsTheTerminalAsContext(t *testing.T) {
	model := &scriptedModel{turn: func(int, llm.Request) []llm.Event { return textTurn("it failed because of X") }}
	state, shell := assistantSession(t, model)
	shell.say("connection refused on port 5432\r\n")
	state.Wake()
	ask(state, "why did it fail?", agent.ModeAsk)
	pumpUntil(t, state, "the run to end", func() bool { return state.ai.run == nil })
	request := model.requests[0]
	if len(request.Tools) != 1 || request.Tools[0].Name != tools.NameRequestAgentMode {
		t.Fatalf("Ask mode tools = %+v", request.Tools)
	}
	user := request.Messages[len(request.Messages)-1].Text
	if !strings.Contains(user, "why did it fail?") || !strings.Contains(user, "connection refused on port 5432") {
		t.Fatalf("the question did not carry the terminal: %q", user)
	}
	if shown := state.ai.items[0].text; strings.Contains(shown, "connection refused") {
		t.Fatalf("the terminal excerpt is shown as if the user typed it: %q", shown)
	}
}

func TestACommandThatNeverEndsIsInterruptedAtItsTimeout(t *testing.T) {
	t.Parallel() // it waits out a real second
	model := &scriptedModel{turn: func(index int, request llm.Request) []llm.Event {
		if index == 0 {
			return toolTurn("terminal_exec", `{"command":"sleep 30","timeout_seconds":1}`)
		}
		return textTurn("it timed out")
	}}
	state, shell := assistantSession(t, model)
	shell.onWrite = func([]byte) {} // a shell that never finishes the command
	ask(state, "wait", agent.ModeAgent)
	pumpUntil(t, state, "the command to be typed", func() bool { return strings.Contains(shell.typed(), "sleep 30") })
	pumpUntil(t, state, "the timeout to interrupt it", func() bool { return strings.Contains(shell.typed(), "\x03") })
	pumpUntil(t, state, "the run to end", func() bool { return state.ai.run == nil })
	if state.sessions[0].term.exec != nil {
		t.Fatal("the terminal is still marked busy")
	}
	last := model.requests[len(model.requests)-1]
	if text := last.Messages[len(last.Messages)-1].Text; !strings.Contains(text, "timed out") {
		t.Fatalf("the model was told %q", text)
	}
}

func TestToolCallsReachTheBackendAsJSONTheModelWrote(t *testing.T) {
	model := &scriptedModel{turn: func(index int, request llm.Request) []llm.Event {
		if index == 0 {
			return toolTurn("list_sessions", `{}`)
		}
		return textTurn("done")
	}}
	state, _ := assistantSession(t, model)
	ask(state, "what is open?", agent.ModeAgent)
	pumpUntil(t, state, "the run to end", func() bool { return state.ai.run == nil })
	last := model.requests[len(model.requests)-1]
	text := last.Messages[len(last.Messages)-1].Text
	if !strings.Contains(text, "ready") || !strings.Contains(text, "(active)") {
		t.Fatalf("list_sessions result = %q", text)
	}
	var sink json.RawMessage
	_ = sink
}

func TestThePromptAfterAnAICommandIsNotMarkedAsAI(t *testing.T) {
	term := newTerminal(80, 24)
	term.screen.Write([]byte("$ "))
	start := term.screen.Row(term.screen.Cursor().Y).ID
	term.aiRows = map[uint64]struct{}{start: {}}
	term.aiStartID = start
	term.screen.Write([]byte("ai-command\r\nresult\r\nPROMPT $ "))
	term.markAIBlock(true)
	prompt := term.screen.Row(term.screen.Cursor().Y).ID
	if _, ok := term.aiRows[prompt]; ok {
		t.Fatal("the prompt after the AI command is marked as AI")
	}
	if _, ok := term.aiRows[start]; !ok {
		t.Fatal("the AI command row lost its mark")
	}
}

func TestAskEscalationSwitchesAndRetriesInAgent(t *testing.T) {
	model := &scriptedModel{turn: func(index int, request llm.Request) []llm.Event {
		switch index {
		case 0:
			return toolTurn(tools.NameRequestAgentMode, `{"reason":"listing files needs a terminal","command":"ls"}`)
		case 1:
			return toolTurn(tools.NameTerminalExec, `{"command":"echo escalated"}`)
		default:
			return textTurn("The command ran.")
		}
	}}
	state, shell := assistantSession(t, model)
	ask(state, "list the files", agent.ModeAsk)
	pumpUntil(t, state, "the escalation card", func() bool { return len(state.ai.approvals) == 1 && state.ai.run == nil })

	if state.ai.mode != agent.ModeAsk {
		t.Fatal("escalation changed the mode before the user approved it")
	}
	approval := state.ai.approvals[0]
	if !approval.escalation || approval.request.Summary != "ls" {
		t.Fatalf("escalation approval = %+v", approval)
	}
	if !strings.Contains(approval.request.Summary, "ls") || len(approval.request.Assessment.Reasons) == 0 {
		t.Fatalf("escalation details = %+v", approval.request)
	}
	if strings.Contains(shell.typed(), "echo escalated") {
		t.Fatal("a command ran in Ask mode")
	}
	state.ai.open = true
	canvas, texts := paintAt(state, panelWide)
	joined := strings.Join(texts, "\n")
	if state.messages.AIPanel.EscalationTitle == "" || state.messages.AIPanel.EscalationApprove == "" {
		t.Fatal("escalation labels are empty")
	}
	if !strings.Contains(joined, state.messages.AIPanel.EscalationTitle) || !strings.Contains(joined, state.messages.AIPanel.EscalationApprove) {
		t.Fatalf("escalation card is not painted: %v", texts)
	}

	state.Click(panelHit(t, canvas, hitAIApprove).ID)
	pumpUntil(t, state, "the retried Agent run", func() bool { return state.ai.run == nil })
	if state.ai.mode != agent.ModeAgent {
		t.Fatal("approving did not switch to Agent")
	}
	if !strings.Contains(shell.typed(), "echo escalated") {
		t.Fatal("the retried Agent run did not execute the command")
	}
	if len(state.ai.approvals) != 0 {
		t.Fatalf("approval was not consumed: %+v", state.ai.approvals)
	}
	users := 0
	for _, item := range state.ai.items {
		if item.kind == aiUser {
			users++
		}
	}
	if users != 1 {
		t.Fatalf("retry duplicated the user message: %d user items", users)
	}
	retry := model.requests[1]
	if len(retry.Tools) == 0 {
		t.Fatal("the retried run was not offered Agent tools")
	}
	userMessages := 0
	for _, message := range retry.Messages {
		if message.Role == llm.RoleUser {
			userMessages++
		}
	}
	if userMessages != 1 {
		t.Fatalf("retry history has %d user messages: %+v", userMessages, retry.Messages)
	}
}

func TestAskEscalationCanStayInAsk(t *testing.T) {
	model := &scriptedModel{turn: func(index int, request llm.Request) []llm.Event {
		if index == 0 {
			return toolTurn(tools.NameRequestAgentMode, `{"reason":"needs a terminal"}`)
		}
		return textTurn("I cannot inspect the files without switching modes, but the screen shows the prompt.")
	}}
	state, shell := assistantSession(t, model)
	ask(state, "list the files", agent.ModeAsk)
	pumpUntil(t, state, "the escalation card", func() bool { return len(state.ai.approvals) == 1 && state.ai.run == nil })
	state.aiAnswerApproval(false)
	pumpUntil(t, state, "the answer after declining", func() bool { return state.ai.run == nil })
	if len(state.ai.approvals) != 0 || state.ai.mode != agent.ModeAsk {
		t.Fatalf("decline left mode %v approvals %+v", state.ai.mode, state.ai.approvals)
	}
	if len(model.requests) < 2 || len(model.requests[1].Tools) != 0 {
		t.Fatalf("declined Ask run offered tools: %+v", model.requests)
	}
	if len(state.ai.items) == 0 || state.ai.items[len(state.ai.items)-1].text != "I cannot inspect the files without switching modes, but the screen shows the prompt." {
		t.Fatalf("decline answer = %+v", state.ai.items)
	}
	if shell.typed() != "" {
		t.Fatalf("decline typed into the shell: %q", shell.typed())
	}
}

// Streamed deltas grow one answer in place. A second streamed item must not
// corrupt the first: its text is a view of the builder buffer, so switching
// kinds has to start a fresh builder rather than reset the old one.
func TestStreamedAnswerKeepsEarlierTextAcrossKinds(t *testing.T) {
	state := &State{}
	run := &aiRun{open: -1}
	state.ai.run = run
	for _, delta := range []string{"one ", "two ", "three"} {
		state.aiEvent(run, agent.Event{Kind: agent.EventText, Text: delta})
	}
	state.aiEvent(run, agent.Event{Kind: agent.EventReasoning, Text: "because"})
	state.aiEvent(run, agent.Event{Kind: agent.EventText, Text: "four"})
	if len(state.ai.items) != 3 {
		t.Fatalf("items = %d, want 3", len(state.ai.items))
	}
	if got := state.ai.items[0].text; got != "one two three" {
		t.Fatalf("first answer = %q", got)
	}
	if got := state.ai.items[1].text; got != "because" {
		t.Fatalf("reasoning = %q", got)
	}
	if got := state.ai.items[2].text; got != "four" {
		t.Fatalf("second answer = %q", got)
	}
}
