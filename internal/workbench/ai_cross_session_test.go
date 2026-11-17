package workbench

import (
	"regexp"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"motty/internal/ai/agent"
	"motty/internal/ai/gate"
	"motty/internal/ai/llm"
	"motty/internal/ai/risk"
)

func TestAIExplicitReferenceExecutesOnBWhileDefaultStaysA(t *testing.T) {
	model := &scriptedModel{turn: func(index int, _ llm.Request) []llm.Event {
		if index == 0 {
			return toolTurn("terminal_exec", `{"session":"2","command":"hostname"}`)
		}
		return textTurn("done")
	}}
	state := targetTestWorld(t, model)
	first, second := &state.sessions[0], &state.sessions[1]
	shellA, shellB := first.term.shell.(*fakeShell), second.term.shell.(*fakeShell)
	state.aiChooseOnlyTarget(hitAITargets.At(int(first.ID)))
	ask(state, "请在@"+mentionSlug(second.Title)+" 执行 hostname", agent.ModeAgent)
	pumpUntil(t, state, "command on B", func() bool { return strings.Contains(shellB.typed(), "hostname") })
	foundDefaultHint := false
	for _, spec := range model.requests[0].Tools {
		if spec.Name == "terminal_exec" {
			foundDefaultHint = strings.Contains(string(spec.Schema), "locked primary session") && !strings.Contains(string(spec.Schema), "Defaults to the active session")
		}
	}
	if !foundDefaultHint {
		t.Fatal("the model received active-tab instructions instead of the locked default")
	}
	// The default scope covers a read-only command, so the reference runs on its
	// own; the card says where.
	if len(state.ai.approvals) != 0 {
		t.Fatalf("a low command on B asked under the default scope: %+v", state.ai.approvals[0].request)
	}
	if strings.Contains(shellA.typed(), "hostname") || state.aiTarget().ID != sessionHandle(first.ID) {
		t.Fatal("cross-session operation executed on or replaced the default")
	}
	label := second.Title + " · " + second.Address
	seen := false
	for _, item := range state.ai.items {
		if item.kind == aiTool && item.call.Arguments == `{"session":"2","command":"hostname"}` {
			seen = item.targetLabel == label
		}
	}
	if !seen {
		t.Fatalf("tool card did not record the connection it ran on: %+v", state.ai.items)
	}
	state.ai.open = true
	state.activeSessionID = second.ID
	state.Wake()
	_, texts := paintAt(state, panelWide)
	if !hasText(texts, state.messages.AIPanel.Target+": "+second.Title) {
		t.Fatal("tool card did not paint the actual target")
	}
	state.aiStop()
	pumpUntil(t, state, "completion", func() bool { return state.ai.run == nil })
	if len(state.aiRequestReferences("continue analyzing")) != 0 {
		t.Fatal("previous references leaked into next request")
	}
	backend, err := state.aiTargetBackend(aiSessionInfo(first))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backend.session(sessionHandle(second.ID)); err == nil {
		t.Fatal("later request could still reach unmentioned B")
	}
}

// answerExec makes a fake shell complete the wrapped command with the exit
// marker the terminal tap waits for.
func answerExec(shell *fakeShell) {
	marker := regexp.MustCompile(`GOPAI_ ([0-9a-f]{16})`)
	shell.onWrite = func(input []byte) {
		if match := marker.FindSubmatch(input); len(match) > 0 {
			shell.say(string(input) + "\r\nfixture-output\r\nGOPAI_" + string(match[1]) + "0\r\n")
		}
	}
}

func TestAICrossConnectionRunsFollowTheConversationScope(t *testing.T) {
	model := &scriptedModel{turn: func(index int, _ llm.Request) []llm.Event {
		if index == 0 {
			return toolTurn("terminal_exec", `{"session":"2","command":"mkdir cross-scope-test"}`)
		}
		return textTurn("done")
	}}
	state := targetTestWorld(t, model)
	first, second := &state.sessions[0], &state.sessions[1]
	shellB := second.term.shell.(*fakeShell)
	state.aiChooseOnlyTarget(hitAITargets.At(int(first.ID)))
	state.aiSetAutoUpTo(risk.High)
	ask(state, "请在@"+mentionSlug(second.Title)+" 上创建目录 cross-scope-test", agent.ModeAgent)
	pumpUntil(t, state, "command on B", func() bool { return strings.Contains(shellB.typed(), "mkdir cross-scope-test") })
	if len(state.ai.approvals) != 0 {
		t.Fatalf("a medium command on B asked under a high scope: %+v", state.ai.approvals[0].request)
	}
	state.aiStop()
	pumpUntil(t, state, "completion", func() bool { return state.ai.run == nil })
}

func TestAICrossConnectionTargetIsKeptInHistory(t *testing.T) {
	model := &scriptedModel{turn: func(index int, _ llm.Request) []llm.Event {
		if index == 0 {
			return toolTurn("terminal_exec", `{"session":"2","command":"hostname"}`)
		}
		return textTurn("done")
	}}
	state := targetTestWorld(t, model)
	first, second := &state.sessions[0], &state.sessions[1]
	answerExec(second.term.shell.(*fakeShell))
	state.aiChooseOnlyTarget(hitAITargets.At(int(first.ID)))
	ask(state, "请在@"+mentionSlug(second.Title)+" 执行 hostname", agent.ModeAgent)
	pumpUntil(t, state, "completion", func() bool { return state.ai.run == nil })
	conversation := state.ai.conv
	label := second.Title + " · " + second.Address
	recorded := false
	for _, item := range state.ai.items {
		if item.kind == aiTool && item.call.ID == "c1" {
			recorded = item.targetLabel == label
		}
	}
	if !recorded {
		t.Fatal("the live tool card lost the connection it ran on")
	}
	state.aiNewConversation()
	state.aiOpenConversation(conversation)
	restored := false
	for _, item := range state.ai.items {
		if item.kind == aiTool && item.call.ID == "c1" {
			restored = item.targetLabel == label
		}
	}
	if !restored {
		t.Fatal("a reopened conversation lost the cross-connection target")
	}
	state.ai.open = true
	_, texts := paintAt(state, panelWide)
	if !hasText(texts, state.messages.AIPanel.Target+": "+second.Title) {
		t.Fatal("restored tool card did not paint the target")
	}
}

func TestAIReferencedApprovalCannotFollowAReconnectedOrClosedSession(t *testing.T) {
	for _, change := range []string{"reconnect", "disconnect", "close", "deny"} {
		t.Run(change, func(t *testing.T) {
			model := &scriptedModel{turn: func(index int, _ llm.Request) []llm.Event {
				if index == 0 {
					return toolTurn("terminal_exec", `{"session":"2","command":"mkdir cross-lock-test"}`)
				}
				return textTurn("done")
			}}
			state := targetTestWorld(t, model)
			first, second := &state.sessions[0], &state.sessions[1]
			idA, idB := first.ID, second.ID
			shellA, shellB := first.term.shell.(*fakeShell), second.term.shell.(*fakeShell)
			state.aiChooseOnlyTarget(hitAITargets.At(int(idA)))
			ask(state, "在@"+mentionSlug(second.Title)+" 执行 mkdir cross-lock-test", agent.ModeAgent)
			pumpUntil(t, state, "B approval", func() bool { return len(state.ai.approvals) == 1 })
			switch change {
			case "reconnect":
				second.term.shell = shellA
			case "disconnect":
				shellB.end(nil)
			case "close":
				state.closeSession(idB)
			}
			state.activeSessionID = idA
			state.aiAnswerApproval(change != "deny")
			pumpUntil(t, state, "completion", func() bool { return state.ai.run == nil })
			if change == "reconnect" {
				if session, ok := state.sessionByIDPointer(idB); ok {
					session.term.shell = shellB
				}
			}
			if strings.Contains(shellA.typed(), "cross-lock-test") || strings.Contains(shellB.typed(), "cross-lock-test") {
				t.Fatal("stale or declined approval executed a command")
			}
			if state.aiTarget().ID != sessionHandle(idA) {
				t.Fatal("default changed")
			}
		})
	}
}

func TestAIReferenceBindingsWithoutADefaultDoNotFollowTheActiveTab(t *testing.T) {
	state := targetTestWorld(t, quietModel{"ok"})
	second := aiSessionInfo(&state.sessions[1])
	backend, err := state.aiTargetBackend(gate.SessionInfo{}, second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = backend.session(""); err == nil {
		t.Fatal("empty default silently used active reference")
	}
	if _, err = backend.session(second.ID); err != nil {
		t.Fatal(err)
	}
}

func TestAIAtPickerAfterProseAndAcceptedReferencesAgree(t *testing.T) {
	state := targetTestWorld(t, quietModel{"ok"})
	state.ai.open = true
	for _, prefix := range []string{"请检查这台服务器", "check-this-host", "对比：", "第一行\n然后查看"} {
		state.ai.input = *text.NewEditor(prefix + "@")
		canvas, _ := paintAt(state, panelWide)
		panelHit(t, canvas, hitAIMentions.At(0))
		result := state.Key(gfx.KeyEvent{Target: hitAIInput, Key: gfx.KeyEnter})
		if !result.Handled || state.ai.run != nil {
			t.Fatal("picker did not accept reference")
		}
		refs := state.aiRequestReferences(state.ai.input.String())
		if len(refs) != 1 || refs[0].ID != sessionHandle(state.sessions[0].ID) {
			t.Fatalf("accepted reference %q is not routed: %+v", state.ai.input.String(), refs)
		}
	}
	if refs := state.aiRequestReferences("联系 user@Staging 或 me@example.com"); len(refs) != 0 {
		t.Fatalf("email became an operation target: %+v", refs)
	}
}
