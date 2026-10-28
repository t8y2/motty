package workbench

import (
	"fmt"
	"strings"
	"testing"

	"github.com/vivaui/viva/ui"
	"motty/internal/ai/agent"
)

func targetChanges(state *State) []string {
	var labels []string
	for _, item := range state.ai.items {
		if item.kind == aiTargetChange {
			labels = append(labels, item.text)
		}
	}
	return labels
}

func TestAITargetChangeSeparatorsAndHistory(t *testing.T) {
	state := targetTestWorld(t, quietModel{"ok"})
	first, second := state.sessions[0].ID, state.sessions[1].ID
	send := func(id uint32) {
		t.Helper()
		state.aiChooseTarget(hitAITargets.At(int(id)))
		ask(state, "analyze", agent.ModeAgent)
		pumpUntil(t, state, "answer", func() bool { return state.ai.run == nil })
	}
	send(first)
	send(first)
	state.aiChooseTarget(hitAITargets.At(int(second)))
	state.aiChooseTarget(hitAITargets.At(int(first)))
	send(first)
	if changes := targetChanges(state); len(changes) != 1 || changes[0] != state.sessions[0].Title {
		t.Fatal("missing first-target separator or repeated separators for the same target")
	}
	send(second)
	send(second)
	state.ai.open = true
	_, texts := paintAt(state, panelWide)
	changes := targetChanges(state)
	if len(changes) != 2 || changes[1] != state.sessions[1].Title {
		t.Fatalf("boundaries = %q", changes)
	}
	label := fmt.Sprintf(state.messages.AIPanel.TargetSwitchedFmt, state.sessions[1].Title)
	if !hasText(texts, label) || hasText(texts, state.messages.AIPanel.Target+": ") {
		t.Fatalf("missing boundary or repeated connection labels: %q", texts)
	}
	conversation := state.ai.conv
	state.aiOpenConversation(conversation)
	if got := targetChanges(state); strings.Join(got, "|") != strings.Join(changes, "|") {
		t.Fatalf("restored boundaries = %q, want %q", got, changes)
	}
	// Simulate a later window lifetime with the same numeric session handles.
	state.ai.targetNamespace = "next-window"
	send(second)
	if len(targetChanges(state)) != 3 {
		t.Fatal("a new terminal lifetime was treated as the persisted target")
	}
	state.aiOpenConversation(conversation)
	if len(targetChanges(state)) != 3 {
		t.Fatal("reopening history lost a connection lifetime boundary")
	}
	send(0)
	if got := targetChanges(state); len(got) != 4 || got[3] != state.messages.AIPanel.TargetNone {
		t.Fatalf("general-question boundary = %q", got)
	}
}

func TestAITargetFollowsRenamedTabWithoutChangingIdentity(t *testing.T) {
	state := targetTestWorld(t, quietModel{"ok"})
	id := state.sessions[0].ID
	state.aiChooseTarget(hitAITargets.At(int(id)))
	state.renameSession(&state.sessions[0], "Renamed target")
	if got := state.aiTarget(); got.ID != sessionHandle(id) || got.Title != "Renamed target" {
		t.Fatalf("target snapshot = %+v", got)
	}
	state.aiChooseTarget(hitAITargets.At(0))
	state.activeSection = sectionTerminal
	if state.aiTarget().ID != "" {
		t.Fatal("explicit no-target choice followed the active tab")
	}
	state.ai.open = true
	_, texts := paintAt(state, ui.Size{Width: 1400, Height: 900})
	if !hasText(texts, state.messages.AIPanel.TargetChoose) {
		t.Fatal("missing compact no-target label")
	}
}

func TestAITargetLongCaptionKeepsTheSessionNumber(t *testing.T) {
	var panel aiPanel
	var canvas ui.Canvas
	for _, title := range []string{"Production database in a very long region-2", "服务器连接名称很长需要区分会话-3"} {
		caption := panel.targetCaption(&canvas, title, 13, 130)
		if !strings.HasSuffix(caption, title[len(title)-2:]) || !strings.Contains(caption, "…") ||
			canvas.TextWidthWeight(caption, 13, ui.WeightRegular) > 130 {
			t.Fatalf("long caption = %q", caption)
		}
		if got := testing.AllocsPerRun(20, func() { panel.targetCaption(&canvas, title, 13, 130) }); got != 0 {
			t.Fatalf("cached caption allocations = %g", got)
		}
	}
}
