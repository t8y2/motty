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

	viewport := state.remote.processScrollBar.Viewport
	_, rowHeight := state.remoteProcessTableMetrics()
	if state.remote.processScrollBar.MaxOffsetY <= 0 {
		t.Fatal("the scrolled snapshot has no scroll range")
	}
	first := state.remote.tableBodyFirst
	visible := int(viewport.Height/rowHeight) + 2
	if first <= 0 || first+visible < len(rows) {
		t.Fatalf("the last rows are not painted at the bottom: first=%d visible=%d rows=%d", first, visible, len(rows))
	}
}
