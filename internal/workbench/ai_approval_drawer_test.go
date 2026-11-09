//go:build !windows

package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
	"motty/internal/ai/agent"
	"motty/internal/ai/llm"
)

// A question the assistant raises on its own must not stack its drawer over the
// monitor's trapped one: opening the assistant closes the monitor first, so the
// narrow window still holds a single drawer. Kept beside the assistant's own
// fixtures, which the shell stand-in makes platform-specific.
func TestApprovalOpeningTheAssistantClosesANarrowMonitorsDrawer(t *testing.T) {
	model := &scriptedModel{turn: func(index int, request llm.Request) []llm.Event {
		if index == 0 {
			return toolTurn("terminal_exec", `{"command":"touch created-by-assistant"}`)
		}
		return textTurn("finished")
	}}
	state, _ := assistantSession(t, model)
	id := state.deck.ActiveWorkspace()
	c, _ := state.deck.Context(id)
	c.MonitorOpen = true
	_ = state.deck.SetContext(id, c)
	state.Resize(gfx.Size{Width: 760, Height: 700})
	if !state.monitorShown(id) || !state.monitorDrawer(state.viewport) {
		t.Fatal("the monitor should be the narrow drawer")
	}

	ask(state, "make a file", agent.ModeAgent)
	pumpUntil(t, state, "the approval question", func() bool { return len(state.ai.approvals) == 1 })
	if state.monitorShown(id) {
		t.Fatal("the question must close the monitor rather than stack a second drawer")
	}
	if !state.aiPanelVisible() {
		t.Fatal("the question should open the assistant")
	}
	state.aiAnswerApproval(false)
	pumpUntil(t, state, "the run to end", func() bool { return state.ai.run == nil })
}
