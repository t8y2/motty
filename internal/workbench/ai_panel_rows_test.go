package workbench

import (
	"fmt"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"motty/internal/ai/llm"
	"motty/internal/ai/risk"
)

func TestApprovalExtrasTinyBudget(t *testing.T) {
	for _, budget := range []float32{0, 1, 7, 8, 9} {
		t.Run(fmt.Sprint(budget), func(t *testing.T) {
			state := agentPanel(t)
			approvalFixture(state, risk.Critical, true)
			state.ai.notice = "Ancillary notice must not overflow"
			panel := &state.ai.panel
			panel.pageScratch.composerBarHeight = aiSendSize + 8
			composerMin := aiComposerMinimum(panel.pageScratch.composerBarHeight, state.Theme().Metrics.FontSizeM)
			approvalMin := aiApprovalMinimum(true)
			reserved := 2*aiComposerMargin + aiBottomTopPad + approvalMin
			panel.rect = gfx.Rect{Width: 352, Height: aiTitleRow + 1 + reserved + composerMin + budget}
			bounds := gfx.Rect{Width: 352, Height: reserved + composerMin + budget}
			canvas := new(gfx.Canvas)
			canvas.SetTree(&tree)
			page := ui.NewPage(ui.PageConfig{Tree: &tree, Root: shellPageRoot, Theme: &state.theme})
			var body aiBody
			page.Draw(canvas, bounds, func(c ui.C) {
				state.declareAIBottom(c, &body, composerMin)
			})
			box := body.box.Rect()
			details := body.approvalDetails.Rect()
			if details.Height < aiApprovalMinDetails-1 {
				t.Errorf("extras squeezed approval details: %+v", details)
			}
			t.Logf("extras budget=%v composer=%+v bounds=%+v", budget, box, bounds)
			if box.Y+box.Height > bounds.Height+0.01 || box.Height < composerMin {
				t.Fatalf("extras overflow or squeeze composer: box=%+v bounds=%+v minimum=%v", box, bounds, composerMin)
			}
		})
	}
}

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
