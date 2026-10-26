package workbench

import (
	"fmt"
	"testing"

	"github.com/vivaui/viva/ui"
	"motty/internal/ai/llm"
	"motty/internal/ai/risk"
)

// longConversation fills the panel with n messages of every kind.
func longConversation(state *State, n int) {
	for index := 0; index < n; index++ {
		switch index % 5 {
		case 0:
			state.ai.items = append(state.ai.items, aiItem{kind: aiUser, text: "a question with enough words in it that it wraps over more than one line in the column"})
		case 1:
			state.ai.items = append(state.ai.items, aiItem{kind: aiAssistant, text: "An answer with a block:\n```sh\nls -la /var/log\n```\nand more words after it to wrap across a few lines of the panel."})
		case 2:
			state.ai.items = append(state.ai.items, aiItem{kind: aiTool, call: llm.ToolCall{ID: "x", Name: "terminal_exec", Arguments: `{"command":"ls -la /var/log"}`}, status: aiToolDone, risk: risk.Low, result: "out\nput"})
		case 3:
			state.ai.items = append(state.ai.items, aiItem{kind: aiReasoning, text: "thinking it over"})
		default:
			state.ai.items = append(state.ai.items, aiItem{kind: aiNotice, text: "something went wrong"})
		}
	}
}

// A conversation of 500 messages must cost no more to repaint than one of 50:
// only the rows on screen are declared, and a row is measured once per width.
func BenchmarkAIPanelLongConversation(b *testing.B) {
	for _, count := range []int{50, 500} {
		b.Run(fmt.Sprintf("messages-%d", count), func(b *testing.B) { benchAIConversation(b, count) })
	}
}

func benchAIConversation(b *testing.B, count int) {
	state, _ := panelState(b, nil)
	withModel(b, state)
	state.ai.open = true
	longConversation(state, count)
	state.ai.scroll.ScrollToStart()
	var canvas ui.Canvas
	state.Paint(&canvas, panelWide)
	state.ai.scroll.ScrollTo(state.ai.panel.maxScroll / 2)
	paint := func() {
		canvas.Reset()
		state.Paint(&canvas, panelWide)
	}
	b.Run("repaint", func(b *testing.B) {
		paint()
		b.ReportAllocs()
		for b.Loop() {
			paint()
		}
	})
	b.Run("scrolling", func(b *testing.B) {
		paint()
		base := state.ai.scroll.Offset()
		b.ReportAllocs()
		for i := 0; b.Loop(); i++ {
			state.ai.scroll.ScrollTo(base + float32(i%64))
			paint()
		}
	})
	b.Run("resizing", func(b *testing.B) {
		// Every paint is at another width, so every row is measured again.
		b.ReportAllocs()
		sizes := [2]ui.Size{{Width: 1400, Height: 900}, {Width: 400, Height: 900}} // docked, then a drawer 12 pixels narrower
		for i := 0; b.Loop(); i++ {
			canvas.Reset()
			state.Paint(&canvas, sizes[i%2])
		}
	})
}
