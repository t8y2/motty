package workbench

import (
	"strings"
	"testing"

	"motty/internal/ai/agent"
	"motty/internal/ai/llm"
)

func TestAIApprovalUsesTheDialedAddressAfterSavedHostEdits(t *testing.T) {
	model := &scriptedModel{turn: func(index int, _ llm.Request) []llm.Event {
		if index == 0 {
			return toolTurn("terminal_exec", `{"session":"2","command":"hostname"}`)
		}
		return textTurn("done")
	}}
	state := targetTestWorld(t, model)
	a, b := &state.sessions[0], &state.sessions[1]
	dialed := b.Address
	previous := state.hosts[1]
	updated := previous
	updated.Address, updated.User, updated.Port = "next.example.com", "next-user", 2222
	state.replaceHost(1, previous, updated)
	if b.Address != dialed || aiSessionInfo(b).Address != dialed || b.host != updated {
		t.Fatal("editing the next dial relabelled the live transport")
	}
	state.aiChooseTarget(hitAITargets.At(int(a.ID)))
	ask(state, "请在@"+mentionSlug(b.Title)+" 执行 hostname", agent.ModeAgent)
	pumpUntil(t, state, "B approval", func() bool { return len(state.ai.approvals) == 1 })
	request := state.ai.approvals[0].request
	if request.SessionLabel != b.Title+" · "+dialed || strings.Contains(request.SessionLabel, updated.Address) {
		t.Fatalf("approval names a future host instead of the actual transport: %+v", request)
	}
	state.aiAnswerApproval(false)
	pumpUntil(t, state, "completion", func() bool { return state.ai.run == nil })
}

func TestEditedHostDoesNotReuseTheOldTransportAndReconnectUpdatesAddress(t *testing.T) {
	for _, field := range []string{"address", "user", "port"} {
		t.Run(field, func(t *testing.T) {
			world, options := newSharingWorld(t)
			state := newTestState(t, options)
			world.connectFirst(t, state)
			id, dialed := state.sessions[0].ID, state.sessions[0].Address
			previous, updated := state.hosts[0], state.hosts[0]
			switch field {
			case "address":
				updated.Address = "next.example.com"
			case "user":
				updated.User = "next-user"
			case "port":
				updated.Port = 2222
			}
			state.replaceHost(0, previous, updated)
			state.newHostSession(updated)
			if len(world.conns) != 2 || len(world.shared) != 0 {
				t.Fatal("the new destination reused an old authenticated transport")
			}
			original, _ := state.sessionByIDPointer(id)
			if original.Address != dialed {
				t.Fatal("live target lost its dialed address")
			}
			state.startConnection(original, updated)
			if original.Address != state.sessionAddress(updated) {
				t.Fatal("reconnect did not adopt the new destination")
			}
		})
	}
}
