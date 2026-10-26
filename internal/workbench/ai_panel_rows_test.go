package workbench

import (
	"testing"

	"github.com/vivaui/viva/flex"
	"github.com/vivaui/viva/ui"
	"motty/internal/ai/llm"
	"motty/internal/ai/risk"
)

// The list places a message by the height measured for it, and declares it with
// the sizes the measure was made of. When the two disagree a message is drawn over
// the next one or leaves a hole: whatever a message holds, open or closed, at any
// width, the declared message must be exactly as tall as it was measured.
func TestMessageIsDeclaredAsTallAsItWasMeasured(t *testing.T) {
	state, _ := panelState(t, nil)
	long := "a longer sentence that has to wrap over several lines in the narrow column of the assistant, 中文也可以。"
	items := []aiItem{
		{kind: aiUser, text: long},
		{kind: aiAssistant, text: "text\n\n- one\n- two [link](https://one.example/a)\n\n```sh\nls -la /var/log/some/very/long/path/that/wraps/around/the/edge\n```\n" + long + "\n```\nsecond\n```"},
		{kind: aiReasoning, text: long},
		{kind: aiNotice, text: long},
		{kind: aiTool, call: llm.ToolCall{ID: "1", Name: "terminal_exec", Arguments: `{"command":"` + long + `"}`}, status: aiToolWaiting, risk: risk.High},
		{kind: aiTool, call: llm.ToolCall{ID: "2", Name: "sftp_remove", Arguments: `{"path":"/a"}`}, status: aiToolDenied},
		{kind: aiTool, call: llm.ToolCall{ID: "3", Name: "terminal_exec", Arguments: `{"command":"ls"}`}, status: aiToolDone, risk: risk.Low, result: "one\ntwo\nthree\n" + long},
		{kind: aiTool, call: llm.ToolCall{ID: "4", Name: "terminal_read"}, status: aiToolDone, result: "output only"},
	}
	var canvas ui.Canvas
	var view flex.View
	cell, cellHeight := canvas.MonoCellSize(aiMonoSize, state.terminalFontFace)
	for _, width := range []float32{352, 160} {
		for _, open := range []bool{false, true} {
			for index := range items {
				item := &items[index]
				row := aiRowView{expanded: open}
				state.aiBuild(&canvas, &row, item, width, cell, cellHeight, "title")
				canvas.Reset()
				f := view.Begin(&canvas, state.Theme(), ui.Rect{Width: width, Height: 4000})
				declared := state.declareAIRow(f, 0, &row, item)
				f.End()
				if got := declared.Rect().Height; got != row.height {
					t.Errorf("kind %d item %d open=%v width %v: declared %v tall, measured %v", item.kind, index, open, width, got, row.height)
				}
			}
		}
	}
}
