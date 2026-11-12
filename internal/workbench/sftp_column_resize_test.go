package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
)

func TestSFTPColumnMetricsFollowTheRoomUntilChosen(t *testing.T) {
	state := newTestState(t, testOptions(t))
	metrics := state.sftpColumnMetrics(500, 0)
	if metrics.size != 64 || metrics.modified != 108 || metrics.name != 328 {
		t.Fatalf("default metrics = %+v, want 328/64/108", metrics)
	}
	if state.sftpColumnWidthsChosen {
		t.Fatal("reading the defaults marked the widths chosen")
	}
	withGap := state.sftpColumnMetrics(500, sftpColumnNameGap)
	if withGap.name != 312 {
		t.Fatalf("gapped name = %v, want 312", withGap.name)
	}
	state.sftpColumnSizeWidth = 120
	state.sftpColumnWidthsChosen = true
	narrow := state.sftpColumnMetrics(360, 0)
	if narrow.size != 120 || narrow.name != 132 {
		t.Fatalf("chosen metrics = %+v, want 132/120", narrow)
	}
}

func TestSFTPColumnFitTakesTheDifferenceFromTheNeighbour(t *testing.T) {
	state := newTestState(t, testOptions(t))
	metrics := sftpColumnMetrics{name: 328, size: 64, modified: 108}
	metrics = state.applySFTPColumnFit(metrics, 0, 160)
	if metrics.name != 160 || metrics.size != 232 || metrics.modified != 108 {
		t.Fatalf("fit name = %+v, want 160/232/108", metrics)
	}
	if !state.sftpColumnWidthsChosen || state.sftpColumnSizeWidth != 232 {
		t.Fatalf("chosen widths = %v chosen=%v", state.sftpColumnSizeWidth, state.sftpColumnWidthsChosen)
	}
	metrics = sftpColumnMetrics{name: 312, size: 64, modified: 108}
	metrics = state.applySFTPColumnFit(metrics, 1, 70)
	if metrics.size != 70 || metrics.modified != 102 {
		t.Fatalf("fit size = %+v, want 70/102", metrics)
	}
	metrics = state.applySFTPColumnFit(sftpColumnMetrics{name: 328, size: 64, modified: 108}, 0, 1000)
	if metrics.size != sftpColumnMinSize || metrics.name != 338 {
		t.Fatalf("oversized fit = %+v, want the size minimum", metrics)
	}
	metrics = state.applySFTPColumnFit(sftpColumnMetrics{name: 328, size: 64, modified: 108}, 1, 1)
	if metrics.size != sftpColumnMinSize || metrics.modified != 118 {
		t.Fatalf("undersized fit = %+v, want the size minimum", metrics)
	}
}

func TestResizeSFTPColumnsMovesTheDividers(t *testing.T) {
	state := newTestState(t, testOptions(t))
	area := gfx.Rect{X: 10, Width: 500}
	if !state.resizeSFTPColumns(0, 210, area, 0) {
		t.Fatal("the name divider refused the drag")
	}
	// Drag to x=210: the name becomes 200, its neighbour takes the rest of
	// their 392 pixels.
	if state.sftpColumnSizeWidth != 192 || !state.sftpColumnWidthsChosen {
		t.Fatalf("size after dragging name = %v chosen=%v, want 192", state.sftpColumnSizeWidth, state.sftpColumnWidthsChosen)
	}
	state.sftpColumnWidthsChosen = false
	state.sftpColumnSizeWidth, state.sftpColumnModifiedWidth = 0, 0
	if !state.resizeSFTPColumns(1, 400, area, 0) {
		t.Fatal("the size divider refused the drag")
	}
	if state.sftpColumnSizeWidth != 62 || state.sftpColumnModifiedWidth != 110 {
		t.Fatalf("widths after dragging size = %v/%v, want 62/110", state.sftpColumnSizeWidth, state.sftpColumnModifiedWidth)
	}
	if state.resizeSFTPColumns(2, 400, area, 0) {
		t.Fatal("a divider past the last column was accepted")
	}
}

func TestSFTPWindowColumnDividerRoutesItsDrag(t *testing.T) {
	state := newTestState(t, testOptions(t))
	w := &sftpWindow{owner: state}
	w.ui.columnArea[0] = gfx.Rect{X: 10, Width: 500}
	w.ui.columnWidths[0] = [3]float32{328, 64, 108}
	press := gfx.PointerEvent{
		Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, ClickCount: 1,
		Target: sftpWindowColumnDividers.At(0), X: 210,
	}
	if result, handled := w.columnResizePointer(press); !handled || !result.Handled {
		t.Fatal("the window's name divider did not take the press")
	}
	if !w.ui.columnDragging[0] || w.ui.columnDrag[0] != 0 {
		t.Fatalf("drag state = %v/%v, want side 0 divider 0", w.ui.columnDragging[0], w.ui.columnDrag[0])
	}
	move := press
	move.Kind, move.Captured = gfx.PointerMove, true
	move.X = 210
	if _, handled := w.columnResizePointer(move); !handled {
		t.Fatal("the window's drag did not take the move")
	}
	if state.sftpColumnSizeWidth != 180 {
		t.Fatalf("size after the window drag = %v, want 180", state.sftpColumnSizeWidth)
	}
	up := press
	up.Kind = gfx.PointerUp
	if _, handled := w.columnResizePointer(up); !handled || w.ui.columnDragging[0] {
		t.Fatal("the window's drag did not end")
	}
	double := press
	double.ClickCount = 2
	if _, handled := w.columnResizePointer(double); !handled || !w.ui.columnFitPending[0] {
		t.Fatal("the window's double click did not ask for a fit")
	}
}

func TestSFTPColumnResizeNeedsBothFixedColumns(t *testing.T) {
	state := newTestState(t, testOptions(t))
	if !state.sftpColumnResizeAvailable() {
		t.Fatal("the default columns are not resizable")
	}
	state.sftpColumns.setEnabled(sftpColumnSize, false)
	if state.sftpColumnResizeAvailable() {
		t.Fatal("a hidden size column left the dividers available")
	}
	state.sftpColumns.setEnabled(sftpColumnSize, true)
	state.sftpColumns.setEnabled(sftpColumnModified, false)
	if state.sftpColumnResizeAvailable() {
		t.Fatal("a hidden modified column left the dividers available")
	}
}
