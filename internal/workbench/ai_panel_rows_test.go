package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"motty/internal/ai/llm"
	"motty/internal/ai/risk"
)

// The List measures a row from what the row declares. Every kind of message must
// declare exactly the height its own layout measured, or the List's estimate for
// the rows it has not shown disagrees with them and the conversation jumps. A
// wrapped long line is included: its height is its line count, not one line.
func TestMessageRowIsMeasuredAtItsBuiltHeight(t *testing.T) {
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
	var builtCanvas gfx.Canvas
	cell, cellHeight := builtCanvas.MonoCellSize(aiMonoSize, state.terminalFontFace)
	for _, width := range []float32{352, 160} {
		for _, open := range []bool{false, true} {
			for index := range items {
				item := items[index]
				row := aiRowView{expanded: open}
				state.aiBuild(&builtCanvas, &row, &item, width, cell, cellHeight, "title")
				state.ai.panel.copySlots = state.ai.panel.copySlots[:0]

				var list ui.ListState
				canvas := new(gfx.Canvas)
				canvas.SetTree(&tree)
				page := ui.NewPage(ui.PageConfig{Tree: &tree, Root: shellPageRoot, Theme: &state.theme})
				page.Draw(canvas, gfx.Rect{Width: width, Height: 4000}, func(c ui.C) {
					ui.List(c, &list, 1, func(lrow ui.ListRow) {
						root := lrow.Element.PadSides(aiGap, aiPad, aiGap, aiPad)
						state.declareAIRow(root, aiRowFrame{c: lrow.C}, 0, &row, &item)
					}).Fill()
				})
				// The row keeps the gap above the conversation and below every row.
				want := row.height + 2*aiGap
				if got := list.RowHeightAt(0); got != want {
					t.Errorf("kind %d item %d open=%v width %v: List measured %v, built %v", item.kind, index, open, width, got, want)
				}
			}
		}
	}
}
