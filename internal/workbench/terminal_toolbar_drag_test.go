package workbench

import (
	"sync/atomic"
	"testing"

	"github.com/vivaui/viva/ui"
)

func TestTerminalRailDragHandlesCompactAndDisabledControls(t *testing.T) {
	tests := []struct {
		name  string
		id    ui.ID
		setup func(*State)
	}{
		{
			name: "connection capsule",
			id:   hitTerminalConnectionColor,
			setup: func(state *State) {
				state.sessions[0].host.Color = "#3b82f6"
			},
		},
		{
			name: "latency label",
			id:   hitTerminalLatency,
			setup: func(state *State) {
				state.toolbars.setTerminalEntryEnabled("latency", true)
				state.syncTerminalStatusEntries()
				atomic.StoreInt64(&state.sessions[0].rttMs, 42)
				atomic.StoreInt32(&state.sessions[0].rttAvailable, 1)
			},
		},
		{
			name: "disabled action",
			id:   hitTerminalRecordingExport,
			setup: func(state *State) {
				state.sessions[0].term.recording.events = nil
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state, _, _, _ := liveSession(t)
			setAllTerminalToolbarSides(state, terminalToolbarRight)
			test.setup(state)
			size := ui.Size{Width: 1100, Height: 760}
			state.Resize(size)

			var canvas ui.Canvas
			state.Paint(&canvas, size)
			handle, ok := hitByID(&canvas, terminalToolbarDragHandle(test.id))
			if !ok {
				t.Fatalf("drag handle for %v is not painted", test.id)
			}
			x, y := handle.Bounds.X+handle.Bounds.Width/2, handle.Bounds.Y+handle.Bounds.Height/2
			state.Pointer(ui.PointerEvent{
				Kind: ui.PointerDown, Button: ui.PointerButtonPrimary,
				Target: handle.ID, X: x, Y: y, Viewport: size,
			})
			full := state.fullWorkspacePixelBounds(size)
			leftX := full.X + state.terminalToolbarMetrics().railExtent/2
			result := state.Pointer(ui.PointerEvent{
				Kind: ui.PointerMove, Button: ui.PointerButtonPrimary, Captured: true,
				Target: handle.ID, X: leftX, Y: y, Viewport: size,
			})
			if !result.Handled || !state.terminalToolbarDrag.active {
				t.Fatalf("compact/disabled control did not start dragging: result=%+v drag=%+v", result, state.terminalToolbarDrag)
			}
			state.Pointer(ui.PointerEvent{
				Kind: ui.PointerUp, Button: ui.PointerButtonPrimary, Captured: true,
				Target: handle.ID, X: leftX, Y: y, Viewport: size,
			})
			if got := state.terminalToolbarSideFor(test.id); got != terminalToolbarLeft {
				t.Fatalf("dragged control side=%v, want left", got)
			}
		})
	}
}

func TestTerminalRailDragRevealsEmptyRailAndMovesOnDrop(t *testing.T) {
	state, _, _, _ := liveSession(t)
	setAllTerminalToolbarSides(state, terminalToolbarRight)
	size := ui.Size{Width: 1100, Height: 760}
	state.Resize(size)

	var canvas ui.Canvas
	state.Paint(&canvas, size)
	control, ok := hitByID(&canvas, hitSFTPOpen)
	if !ok {
		t.Fatal("files control is missing")
	}
	before := state.baseWorkspacePixelBounds(size)
	x, y := control.Bounds.X+control.Bounds.Width/2, control.Bounds.Y+control.Bounds.Height/2
	if result := state.Pointer(ui.PointerEvent{
		Kind: ui.PointerDown, Button: ui.PointerButtonPrimary,
		Target: control.ID, X: x, Y: y, Viewport: size,
	}); result.Handled {
		t.Fatalf("a press without movement consumed the control click: %+v", result)
	}

	full := state.fullWorkspacePixelBounds(size)
	leftX := full.X + state.terminalToolbarMetrics().railExtent/2
	result := state.Pointer(ui.PointerEvent{
		Kind: ui.PointerMove, Button: ui.PointerButtonPrimary, Captured: true,
		Target: control.ID, X: leftX, Y: y, Viewport: size,
	})
	if !result.Handled || !state.terminalToolbarDrag.active || state.terminalToolbarDrag.zone.Side() != terminalToolbarLeft {
		t.Fatalf("drag did not reveal the left rail: result=%+v drag=%+v", result, state.terminalToolbarDrag)
	}
	preview := state.baseWorkspacePixelBounds(size)
	if preview.X != before.X+state.terminalToolbarMetrics().railExtent {
		t.Fatalf("preview workspace=%+v, before=%+v", preview, before)
	}

	canvas = ui.Canvas{}
	state.Paint(&canvas, size)
	if state.sessions[0].term.rails[terminalToolbarLeft].bounds.Width != state.terminalToolbarMetrics().railExtent {
		t.Fatal("the revealed destination rail was not painted")
	}

	result = state.Pointer(ui.PointerEvent{
		Kind: ui.PointerUp, Button: ui.PointerButtonPrimary, Captured: true,
		Target: control.ID, X: leftX, Y: y, Viewport: size,
	})
	if !result.Handled || state.terminalToolbarDrag.pressed {
		t.Fatalf("drop result=%+v drag=%+v", result, state.terminalToolbarDrag)
	}
	if got := state.terminalToolbarSideFor(hitSFTPOpen); got != terminalToolbarLeft {
		t.Fatalf("files control side=%v, want left", got)
	}
}

func TestTerminalRailDragMovesAcrossSideAndDockAtOnce(t *testing.T) {
	state, _, _, _ := liveSession(t)
	setAllTerminalToolbarSides(state, terminalToolbarRight)
	size := ui.Size{Width: 1100, Height: 760}
	state.Resize(size)

	var canvas ui.Canvas
	state.Paint(&canvas, size)
	control, ok := hitByID(&canvas, hitSFTPOpen)
	if !ok {
		t.Fatal("files control is missing")
	}
	x, y := control.Bounds.X+control.Bounds.Width/2, control.Bounds.Y+control.Bounds.Height/2
	state.Pointer(ui.PointerEvent{
		Kind: ui.PointerDown, Button: ui.PointerButtonPrimary,
		Target: control.ID, X: x, Y: y, Viewport: size,
	})

	full := state.fullWorkspacePixelBounds(size)
	targetX := full.X + state.terminalToolbarMetrics().railExtent/2
	targetY := full.Y + full.Height - 20
	result := state.Pointer(ui.PointerEvent{
		Kind: ui.PointerMove, Button: ui.PointerButtonPrimary, Captured: true,
		Target: control.ID, X: targetX, Y: targetY, Viewport: size,
	})
	if !result.Handled || state.terminalToolbarDrag.zone != terminalToolbarZoneLeftBottom {
		t.Fatalf("cross-quadrant preview result=%+v drag=%+v", result, state.terminalToolbarDrag)
	}
	result = state.Pointer(ui.PointerEvent{
		Kind: ui.PointerUp, Button: ui.PointerButtonPrimary, Captured: true,
		Target: control.ID, X: targetX, Y: targetY, Viewport: size,
	})
	if !result.Handled {
		t.Fatalf("cross-quadrant drop was not handled: %+v", result)
	}
	if got := state.terminalToolbarZoneFor(hitSFTPOpen); got != terminalToolbarZoneLeftBottom {
		t.Fatalf("dragged files control zone=%v, want left-bottom", got)
	}
}

func TestTerminalRailDragReordersAndEscapeCancelsPreview(t *testing.T) {
	state, _, _, _ := liveSession(t)
	setAllTerminalToolbarSides(state, terminalToolbarRight)
	size := ui.Size{Width: 1100, Height: 760}
	state.Resize(size)

	paint := func() *ui.Canvas {
		var canvas ui.Canvas
		state.Paint(&canvas, size)
		return &canvas
	}
	canvas := paint()
	control, ok := hitByID(canvas, hitSFTPOpen)
	if !ok {
		t.Fatal("files control is missing")
	}
	source := ui.Point{X: control.Bounds.X + control.Bounds.Width/2, Y: control.Bounds.Y + control.Bounds.Height/2}

	// Escape restores an empty-rail preview without saving the move.
	entriesBefore := append([]toolbarEntry(nil), state.toolbars.entries(toolbarTerminal)...)
	state.Pointer(ui.PointerEvent{
		Kind: ui.PointerDown, Button: ui.PointerButtonPrimary,
		Target: control.ID, X: source.X, Y: source.Y, Viewport: size,
	})
	full := state.fullWorkspacePixelBounds(size)
	state.Pointer(ui.PointerEvent{
		Kind: ui.PointerMove, Button: ui.PointerButtonPrimary, Captured: true,
		Target: control.ID, X: full.X + 8, Y: source.Y, Viewport: size,
	})
	cancelled := state.Key(ui.KeyEvent{Target: control.ID, Key: ui.KeyEscape})
	if !cancelled.Handled || state.terminalToolbarDrag.pressed {
		t.Fatalf("escape cancellation=%+v drag=%+v", cancelled, state.terminalToolbarDrag)
	}
	entriesAfter := state.toolbars.entries(toolbarTerminal)
	for index := range entriesBefore {
		if entriesBefore[index] != entriesAfter[index] {
			t.Fatalf("escape saved toolbar changes: before=%+v after=%+v", entriesBefore, entriesAfter)
		}
	}

	// Dragging to the bottom of the same rail places the source last.
	canvas = paint()
	control, _ = hitByID(canvas, hitSFTPOpen)
	source = ui.Point{X: control.Bounds.X + control.Bounds.Width/2, Y: control.Bounds.Y + control.Bounds.Height/2}
	right, _ := state.terminalSideRailBoundsFor(size, terminalToolbarRight)
	targetY := right.Y + state.terminalToolbarMetrics().inset + right.Height - state.terminalToolbarMetrics().inset*2 - 1
	state.Pointer(ui.PointerEvent{
		Kind: ui.PointerDown, Button: ui.PointerButtonPrimary,
		Target: control.ID, X: source.X, Y: source.Y, Viewport: size,
	})
	state.Pointer(ui.PointerEvent{
		Kind: ui.PointerMove, Button: ui.PointerButtonPrimary, Captured: true,
		Target: control.ID, X: source.X, Y: targetY, Viewport: size,
	})
	state.Pointer(ui.PointerEvent{
		Kind: ui.PointerUp, Button: ui.PointerButtonPrimary, Captured: true,
		Target: control.ID, X: source.X, Y: targetY, Viewport: size,
	})

	var toolList [maxBarTools]barTool
	tools := state.activeTerminalTools(&state.sessions[0], &toolList)
	if len(tools) == 0 || tools[len(tools)-1].id != hitSFTPOpen {
		t.Fatalf("dragged control is not last in the visible rail: %+v", tools)
	}
}
