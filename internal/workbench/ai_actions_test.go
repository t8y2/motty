//go:build !windows

package workbench

import (
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/ui"
	"motty/internal/ai/agent"
	"motty/internal/ai/llm"
)

// TestLastAnswerShowsCopyAndUsageOnHover proves the action row belongs to the
// last answer: hovering the message reveals a copy button and the run's token
// usage, and the button copies the answer.
func TestLastAnswerShowsCopyAndUsageOnHover(t *testing.T) {
	model := &scriptedModel{turn: func(int, llm.Request) []llm.Event {
		return []llm.Event{
			{Kind: llm.EventText, Text: "the answer"},
			{Kind: llm.EventDone, Usage: llm.Usage{InputTokens: 1991, OutputTokens: 90}},
		}
	}}
	state, _ := assistantSession(t, model)
	state.activeSection = sectionTerminal
	state.ai.open = true
	ask(state, "question", agent.ModeAsk)
	pumpUntil(t, state, "the answer to finish", func() bool { return state.ai.run == nil })

	var first ui.Canvas
	first.SetClock(func() time.Duration { return time.Second })
	state.Paint(&first, ui.Size{Width: 1400, Height: 900})
	slots := state.ai.panel.rowSlots
	if len(slots) == 0 {
		t.Fatal("the conversation painted no rows")
	}
	last := slots[len(slots)-1].Rect()
	state.ai.panel.pointer = ui.Point{X: last.X + 20, Y: last.Y + 10}

	var canvas ui.Canvas
	canvas.SetClock(func() time.Duration { return time.Second })
	state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})

	if _, ok := canvasHit(canvas.Hits(), hitAICopyAnswer); !ok {
		t.Fatal("the hovered answer has no copy button")
	}
	usage := state.aiUsageText()
	found := false
	for _, text := range canvas.Texts() {
		if strings.Contains(text.Value, usage) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("the answer action row did not show %q", usage)
	}

	var copied string
	state.options.SetClipboardText = func(value string) error { copied = value; return nil }
	state.Click(hitAICopyAnswer)
	if copied != "the answer" {
		t.Fatalf("copied %q, want the last answer", copied)
	}
}

// TestAnswerActionsFollowThePointer drives the real pointer events: the text of
// an answer is not a hit, so the runtime repaints for it only when the window is
// told to, and the copy button has to come back on every visit, not just the first.
func TestAnswerActionsFollowThePointer(t *testing.T) {
	model := &scriptedModel{turn: func(int, llm.Request) []llm.Event {
		return []llm.Event{{Kind: llm.EventText, Text: "the answer"}, {Kind: llm.EventDone}}
	}}
	state, _ := assistantSession(t, model)
	state.activeSection = sectionTerminal
	state.ai.open = true
	ask(state, "question", agent.ModeAsk)
	pumpUntil(t, state, "the answer to finish", func() bool { return state.ai.run == nil })

	size := ui.Size{Width: 1400, Height: 900}
	paint := func() ui.Canvas {
		var canvas ui.Canvas
		canvas.SetClock(func() time.Duration { return time.Second })
		state.Paint(&canvas, size)
		return canvas
	}
	paint()
	slots := state.ai.panel.rowSlots
	last := slots[len(slots)-1].Rect()
	inside := ui.Point{X: last.X + 20, Y: last.Y + 10}
	move := func(kind ui.PointerEventKind, at ui.Point) ui.EventResult {
		return state.Pointer(ui.PointerEvent{Kind: kind, X: at.X, Y: at.Y, Viewport: size})
	}
	for visit := 1; visit <= 3; visit++ {
		if !move(ui.PointerMove, inside).Repaint {
			t.Fatalf("visit %d: entering the answer asked for no repaint", visit)
		}
		canvas := paint()
		if _, ok := canvasHit(canvas.Hits(), hitAICopyAnswer); !ok {
			t.Fatalf("visit %d: the hovered answer has no copy button", visit)
		}
		if move(ui.PointerMove, ui.Point{X: inside.X + 1, Y: inside.Y}).Repaint {
			t.Fatalf("visit %d: moving inside the answer repainted", visit)
		}
		if !move(ui.PointerExit, ui.Point{}).Repaint {
			t.Fatalf("visit %d: leaving the window asked for no repaint", visit)
		}
		canvas = paint()
		if _, ok := canvasHit(canvas.Hits(), hitAICopyAnswer); ok {
			t.Fatalf("visit %d: the copy button stayed after the pointer left", visit)
		}
	}
}

func canvasHit(hits []ui.Hit, id ui.ID) (ui.Hit, bool) {
	for _, hit := range hits {
		if hit.ID == id {
			return hit, true
		}
	}
	return ui.Hit{}, false
}
