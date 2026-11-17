package workbench

import (
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"
	"motty/internal/ai/agent"
	"motty/internal/ai/llm"
)

func TestLastAnswerShowsCopyOnHoverAndUsageAtRest(t *testing.T) {
	model := &scriptedModel{turn: func(int, llm.Request) []llm.Event {
		return []llm.Event{
			{Kind: llm.EventText, Text: "the answer"},
			{Kind: llm.EventDone, Usage: llm.Usage{InputTokens: 1991, OutputTokens: 90}},
		}
	}}
	state, _ := assistantTestSession(t, model)
	state.activeSection = sectionTerminal
	state.ai.open = true
	ask(state, "question", agent.ModeAsk)
	pumpUntil(t, state, "the answer to finish", func() bool { return state.ai.run == nil })

	var first gfx.Canvas
	first.SetClock(func() time.Duration { return time.Second })
	state.Paint(&first, gfx.Size{Width: 1400, Height: 900})
	status, ok := canvasHit(first.Hits(), hitAIRunStatus)
	if !ok {
		t.Fatal("usage is missing before hovering the answer")
	}
	for _, want := range []string{"2,081", "1,991", "90"} {
		if !strings.Contains(status.Label, want) {
			t.Fatalf("usage before hover did not show %q: %q", want, status.Label)
		}
	}
	slots := state.ai.panel.rowSlots
	if len(slots) == 0 {
		t.Fatal("the conversation painted no rows")
	}
	last := slots[len(slots)-1].Rect()
	state.ai.panel.pointer = gfx.Point{X: last.X + 20, Y: last.Y + 10}

	var canvas gfx.Canvas
	canvas.SetClock(func() time.Duration { return time.Second })
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})

	if _, ok := canvasHit(canvas.Hits(), hitAICopyAnswer); !ok {
		t.Fatal("the hovered answer has no copy button")
	}
	if _, ok := canvasHit(canvas.Hits(), hitAIRunStatus); !ok {
		t.Fatal("hovering hid the persistent usage summary")
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
	state, _ := assistantTestSession(t, model)
	state.activeSection = sectionTerminal
	state.ai.open = true
	ask(state, "question", agent.ModeAsk)
	pumpUntil(t, state, "the answer to finish", func() bool { return state.ai.run == nil })

	size := gfx.Size{Width: 1400, Height: 900}
	paint := func() gfx.Canvas {
		var canvas gfx.Canvas
		canvas.SetClock(func() time.Duration { return time.Second })
		state.Paint(&canvas, size)
		return canvas
	}
	paint()
	slots := state.ai.panel.rowSlots
	last := slots[len(slots)-1].Rect()
	inside := gfx.Point{X: last.X + 20, Y: last.Y + 10}
	move := func(kind gfx.PointerEventKind, at gfx.Point) gfx.EventResult {
		return state.Pointer(gfx.PointerEvent{Kind: kind, X: at.X, Y: at.Y, Viewport: size})
	}
	for visit := 1; visit <= 3; visit++ {
		if !move(gfx.PointerMove, inside).Repaint {
			t.Fatalf("visit %d: entering the answer asked for no repaint", visit)
		}
		canvas := paint()
		if _, ok := canvasHit(canvas.Hits(), hitAICopyAnswer); !ok {
			t.Fatalf("visit %d: the hovered answer has no copy button", visit)
		}
		if move(gfx.PointerMove, gfx.Point{X: inside.X + 1, Y: inside.Y}).Repaint {
			t.Fatalf("visit %d: moving inside the answer repainted", visit)
		}
		if !move(gfx.PointerExit, gfx.Point{}).Repaint {
			t.Fatalf("visit %d: leaving the window asked for no repaint", visit)
		}
		canvas = paint()
		if _, ok := canvasHit(canvas.Hits(), hitAICopyAnswer); ok {
			t.Fatalf("visit %d: the copy button stayed after the pointer left", visit)
		}
	}
}

func canvasHit(hits []gfx.Hit, id gfx.ID) (gfx.Hit, bool) {
	for _, hit := range hits {
		if hit.ID == id {
			return hit, true
		}
	}
	return gfx.Hit{}, false
}
