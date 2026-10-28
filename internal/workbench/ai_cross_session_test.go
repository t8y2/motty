package workbench

import (
	"strings"
	"testing"

	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	"motty/internal/ai/agent"
	"motty/internal/ai/gate"
	"motty/internal/ai/llm"
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
	state.aiChooseTarget(hitAITargets.At(int(first.ID)))
	ask(state, "请在@"+mentionSlug(second.Title)+" 执行 hostname", agent.ModeAgent)
	pumpUntil(t, state, "actual B approval", func() bool { return len(state.ai.approvals) == 1 })
	request := state.ai.approvals[0].request
	if request.Session != sessionHandle(second.ID) || request.SessionLabel != second.Title+" · "+second.Address {
		t.Fatalf("incorrect cross-session approval: %+v", request)
	}
	if strings.Contains(shellB.typed(), "hostname") {
		t.Fatal("cross-session command executed before approval")
	}
	state.ai.open = true
	_, texts := paintAt(state, panelWide)
	if !hasText(texts, second.Address) {
		t.Fatal("approval card did not paint actual target address")
	}
	state.activeSessionID = second.ID
	state.aiAnswerApproval(true)
	pumpUntil(t, state, "command on B", func() bool { return strings.Contains(shellB.typed(), "hostname") })
	if strings.Contains(shellA.typed(), "hostname") || state.aiTarget().ID != sessionHandle(first.ID) {
		t.Fatal("cross-session operation executed on or replaced the default")
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

func TestAIReferencedApprovalCannotFollowAReconnectedOrClosedSession(t *testing.T) {
	for _, change := range []string{"reconnect", "disconnect", "close", "deny"} {
		t.Run(change, func(t *testing.T) {
			model := &scriptedModel{turn: func(index int, _ llm.Request) []llm.Event {
				if index == 0 {
					return toolTurn("terminal_exec", `{"session":"2","command":"hostname"}`)
				}
				return textTurn("done")
			}}
			state := targetTestWorld(t, model)
			first, second := &state.sessions[0], &state.sessions[1]
			idA, idB := first.ID, second.ID
			shellA, shellB := first.term.shell.(*fakeShell), second.term.shell.(*fakeShell)
			state.aiChooseTarget(hitAITargets.At(int(idA)))
			ask(state, "在@"+mentionSlug(second.Title)+" 执行 hostname", agent.ModeAgent)
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
			if strings.Contains(shellA.typed(), "hostname") || strings.Contains(shellB.typed(), "hostname") {
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
		result := state.Key(ui.KeyEvent{Target: hitAIInput, Key: ui.KeyEnter})
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
