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

	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	scroll := state.remote.tableScroll
	if scroll == nil || scroll.Max() <= 0 {
		t.Fatal("the scrolled snapshot has no scroll range")
	}
	scroll.ScrollToEnd()
	canvas = gfx.Canvas{}
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})

	list := &state.remote.lists[remoteProcesses]
	first, last := list.Visible()
	if first <= 0 || last < len(rows)-1 {
		t.Fatalf("the last rows are not painted at the bottom: first=%d last=%d rows=%d", first, last, len(rows))
	}
	if !remoteCanvasHasText(&canvas, rows[len(rows)-1].cells[0]) {
		t.Fatal("the last row is not painted after scrolling to the end")
	}
}
