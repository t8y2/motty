package workbench

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivaui/viva/ui"
	"motty/internal/ai/agent"
	"motty/internal/ai/gate"
	"motty/internal/ai/llm"
	"motty/internal/ai/store"
	"motty/internal/sshconn"
)

func targetTestWorld(t *testing.T, model llm.Provider) *State {
	t.Helper()
	world, options := newWorld(t)
	options.Invalidate = func() {}
	options.AIStorePath = filepath.Join(t.TempDir(), "ai.db")
	options.AIProvider = func(store.Provider) llm.Provider { return model }
	state := newTestState(t, options)
	t.Cleanup(state.Close)
	for index := 0; index < 2; index++ {
		state.activeSection = sectionHosts
		state.selectedHostID = state.hosts[index].ID
		fake := connect(t, state, world)
		fake.set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseConnected })
		state.Wake()
		fake.shell(t).say("\x1b[0m")
		state.Wake()
	}
	withModel(t, state)
	return state
}

func TestAITargetLocksDuringRequestAndSurvivesTabSwitch(t *testing.T) {
	started, release := make(chan struct{}, 1), make(chan struct{})
	model := &scriptedModel{turn: func(index int, request llm.Request) []llm.Event {
		if index == 0 {
			started <- struct{}{}
			<-release
			return toolTurn("terminal_read", `{}`)
		}
		return textTurn("done")
	}}
	state := targetTestWorld(t, model)
	first, second := state.sessions[0].ID, state.sessions[1].ID
	state.sessions[0].term.write([]byte("TARGET_A_ONLY"))
	state.sessions[1].term.write([]byte("TARGET_B_ONLY"))
	state.aiChooseTarget(hitAITargets.At(int(first)))
	ask(state, "read this terminal", agent.ModeAgent)
	pumpUntil(t, state, "model start", func() bool { return len(started) > 0 })
	state.activeSessionID = second
	if state.aiChooseTarget(hitAITargets.At(int(second))) {
		t.Fatal("changed the target of a running request")
	}
	close(release)
	pumpUntil(t, state, "request finish", func() bool { return state.ai.run == nil })
	if state.aiTarget().ID != sessionHandle(first) {
		t.Fatal("tab switch changed conversation target")
	}
	var output string
	for _, message := range state.ai.history {
		if message.Role == llm.RoleTool {
			output += message.Text
		}
	}
	if !strings.Contains(output, "TARGET_A_ONLY") || strings.Contains(output, "TARGET_B_ONLY") {
		t.Fatalf("read wrong terminal: %q", output)
	}
	conversation := state.ai.conv
	label := state.ai.items[0].targetLabel
	state.aiNewConversation()
	if state.aiTarget().ID != sessionHandle(second) {
		t.Fatal("new conversation did not default to visible connection")
	}
	state.aiOpenConversation(conversation)
	if state.aiTarget().ID != "" || !state.ai.targetSet {
		t.Fatal("history silently reused a connection ID")
	}
	if state.ai.items[0].targetLabel != label {
		t.Fatal("history lost original target label")
	}
}

func TestAITargetHomeAndSelectionDoNotNavigate(t *testing.T) {
	state := targetTestWorld(t, quietModel{"ok"})
	active := state.activeSessionID
	state.activeSection = sectionHosts
	state.toggleAIPanel()
	if state.activeSection != sectionHosts || state.activeSessionID != active || !state.aiPanelVisible() {
		t.Fatal("opening assistant navigated away")
	}
	if state.aiTarget().ID != "" {
		t.Fatal("home silently targeted a background connection")
	}
	state.aiChooseTarget(hitAITargets.At(int(state.sessions[0].ID)))
	if state.activeSection != sectionHosts || state.activeSessionID != active {
		t.Fatal("choosing target navigated away")
	}
	state.aiChooseTarget(hitAITargets.At(0))
	ask(state, "general question", agent.ModeAsk)
	pumpUntil(t, state, "general answer", func() bool { return state.ai.run == nil })
	if state.aiTarget().ID != "" {
		t.Fatal("general question picked an implicit target")
	}
}

func TestAITargetReferencesDoNotExpandToolScope(t *testing.T) {
	state := targetTestWorld(t, quietModel{"ok"})
	first, second := &state.sessions[0], &state.sessions[1]
	second.term.write([]byte("REFERENCE_B_ONLY"))
	state.aiChooseTarget(hitAITargets.At(int(first.ID)))
	context := state.aiMentionContext("@"+mentionSlug(second.Title), 0)
	if !strings.Contains(context, "REFERENCE_B_ONLY") {
		t.Fatal("cross-session reference stopped working")
	}
	if state.aiTarget().ID != sessionHandle(first.ID) {
		t.Fatal("reference changed target")
	}
	state.aiExplainOutput(second)
	if !strings.Contains(state.ai.input.String(), "@"+mentionSlug(second.Title)) || state.aiTarget().ID != sessionHandle(first.ID) {
		t.Fatal("terminal-menu explanation lost its source or changed the target")
	}
}

func TestAITargetBackendRejectsReconnectAndClosedTarget(t *testing.T) {
	state := targetTestWorld(t, quietModel{"ok"})
	first := &state.sessions[0]
	backend, err := state.aiTargetBackend(aiSessionInfo(first))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backend.session(sessionHandle(first.ID)); err != nil {
		t.Fatal(err)
	}
	oldShell := first.term.shell
	first.term.shell = state.sessions[1].term.shell // same tab, a new shell lifetime
	if _, err := backend.session(sessionHandle(first.ID)); err == nil {
		t.Fatal("old request followed a reconnected shell")
	}
	first.term.shell = oldShell
	id := first.ID
	state.closeSession(id)
	if _, err := backend.session(sessionHandle(id)); err == nil {
		t.Fatal("closed target fell back to another connection")
	}
}

func TestAITargetApprovalUsesOriginalConnection(t *testing.T) {
	for _, change := range []string{"tab", "reconnect", "disconnect"} {
		t.Run(change, func(t *testing.T) {
			model := &scriptedModel{turn: func(index int, _ llm.Request) []llm.Event {
				if index == 0 {
					return toolTurn("terminal_exec", `{"command":"touch target-lock-test"}`)
				}
				return textTurn("done")
			}}
			state := targetTestWorld(t, model)
			first, second := &state.sessions[0], &state.sessions[1]
			shellA, shellB := first.term.shell.(*fakeShell), second.term.shell.(*fakeShell)
			state.aiChooseTarget(hitAITargets.At(int(first.ID)))
			ask(state, "create the test file", agent.ModeAgent)
			pumpUntil(t, state, "approval", func() bool { return len(state.ai.approvals) == 1 })
			request := state.ai.approvals[0].request
			if request.Session != sessionHandle(first.ID) || !strings.Contains(request.SessionLabel, first.Address) {
				t.Fatalf("incorrect approval target: %+v", request)
			}
			state.activeSessionID = second.ID
			switch change {
			case "reconnect":
				first.term.shell = shellB
			case "disconnect":
				shellA.end(nil)
			}
			state.aiAnswerApproval(true)
			if change == "tab" {
				pumpUntil(t, state, "command on A", func() bool { return strings.Contains(shellA.typed(), "touch target-lock-test") })
				state.aiStop()
			}
			pumpUntil(t, state, "completion", func() bool { return state.ai.run == nil })
			first.term.shell = shellA
			if strings.Contains(shellB.typed(), "target-lock-test") {
				t.Fatal("approval executed on another connection")
			}
			if change != "tab" && strings.Contains(shellA.typed(), "target-lock-test") {
				t.Fatal("stale approval executed after connection changed")
			}
		})
	}
}

func TestAITargetSelectLayoutAndKeyboard(t *testing.T) {
	state := targetTestWorld(t, quietModel{"ok"})
	state.toggleAIPanel()
	for _, size := range []ui.Size{panelWide, panelNarrow} {
		canvas, _ := paintAt(state, size)
		trigger := panelHit(t, canvas, hitAITarget)
		input := panelHit(t, canvas, hitAIInput)
		if trigger.Role != ui.RoleCombobox || trigger.Bounds.Y+trigger.Bounds.Height > input.Bounds.Y {
			t.Fatal("target selector overlaps composer")
		}
		state.Key(ui.KeyEvent{Target: hitAITarget, Key: ui.KeyDown})
		canvas, _ = paintAt(state, size)
		panelHit(t, canvas, hitAITargets.At(int(state.sessions[0].ID)))
		state.Click(hitAITargets.At(int(state.sessions[0].ID)))
		if state.ai.panel.targetOpen {
			t.Fatal("select did not close")
		}
	}
	state.ai.run = &aiRun{target: gate.SessionInfo{}}
	canvas, _ := paintAt(state, panelWide)
	if !panelHit(t, canvas, hitAITarget).Disabled {
		t.Fatal("running request selector is enabled")
	}
	state.ai.run = nil
}
