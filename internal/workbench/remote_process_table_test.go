package workbench

import (
	"fmt"
	"testing"

	"github.com/vivaui/viva/gfx"
)

func TestRemoteProcessTableFillsTheViewportAfterScrolling(t *testing.T) {
	state, _, _ := openRemoteToolsDialog(t)
	rows := make([]remoteRow, 80)
	for index := range rows {
		rows[index] = remoteRow{
			key: fmt.Sprintf("process-%02d", index),
			cells: [6]string{
				fmt.Sprint(3000 + index), "motty", "0.0", "0.0", "00:01",
				fmt.Sprintf("/tmp/motty-probe-%02d", index),
			},
		}
	}
	state.remote.rows = rows
	state.remote.processScroll = 1_000_000

	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	state.remote.processScroll = state.remote.processScrollBar.MaxOffsetY
	canvas = gfx.Canvas{}
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})

	last, found := hitByID(&canvas, hitRemoteRowList.At(len(rows)-1))
	if !found {
		t.Fatal("the last process row is not painted at the bottom of the viewport")
	}
	viewport := state.remote.processScrollBar.Viewport
	_, rowHeight := state.remoteProcessTableMetrics()
	if last.Bounds.Y < viewport.Y-rowHeight ||
		last.Bounds.Bottom() < viewport.Bottom()-1 {
		t.Fatalf("last row bounds=%+v do not reach the viewport bottom=%v", last.Bounds, viewport.Bottom())
	}
}
