package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
	layoutcore "motty/internal/workspace"
)

// The docked assistant is a column of the workspace, and the window frame takes
// that same workspace surface, so the rail's empty column below its tools would
// read as part of the panel. The panel is bounded from the rail on its outside;
// the pane divider draws the same line on its inside, and a panel that ends at
// the window's own edge keeps the window's edge.
func TestTheDockedAssistantIsBoundedFromTheSideRail(t *testing.T) {
	state, _ := panelState(t, nil)
	withModel(t, state)
	state.setAIPanelOpen(true)

	canvas, _ := paintAt(state, panelWide)
	if state.ai.panel.mode != aiPanelDocked {
		t.Fatalf("panel mode = %v, want the docked column", state.ai.panel.mode)
	}
	if reserve := state.toolbarDockSideReserveFor(false, true); reserve <= 0 {
		t.Fatalf("the window has no side rail beside the panel, reserve = %v", reserve)
	}
	panel := state.ai.panel.rect
	want := gfx.Rect{X: panel.X + panel.Width - 1, Y: panel.Y, Width: 1, Height: panel.Height}
	if got := lastOpaqueAt(canvas, gfx.Point{X: want.X, Y: panel.Y + panel.Height/2}); !painted(got, want, state.pal.line) {
		t.Fatalf("the panel's edge beside the side rail is %+v, want the line at %v", got, want)
	}

	// With the rail on the other side the panel ends at the window's own edge,
	// where it keeps no line.
	state.terminalToolbarPosition = terminalToolbarLeft
	canvas, _ = paintAt(state, panelWide)
	panel = state.ai.panel.rect
	if panel.X+panel.Width != panelWide.Width {
		t.Fatalf("panel right edge = %v, want the window edge %v", panel.X+panel.Width, panelWide.Width)
	}
	if got := lastOpaqueAt(canvas, gfx.Point{X: panel.X + panel.Width - 1, Y: panel.Y + panel.Height/2}); got.Color == state.pal.line {
		t.Fatalf("with no rail beside it the panel still draws a side line: %+v", got)
	}
}

// The terminal beside a rail has a ground of its own, so closing the assistant
// must leave the terminal's view unlined: the rails keep the one surface they
// always had.
func TestTheClosedAssistantLeavesTheTerminalUnlined(t *testing.T) {
	state, _ := panelState(t, nil)
	if state.aiPanelVisible() {
		t.Fatal("the assistant is open")
	}
	canvas, _ := paintAt(state, panelWide)
	if reserve := state.toolbarDockSideReserveFor(false, true); reserve <= 0 {
		t.Fatalf("the window has no side rail, reserve = %v", reserve)
	}
	rail := state.toolbarDockSideBoundsFor(panelWide, false)
	if got := lastOpaqueAt(canvas, gfx.Point{X: rail.X, Y: rail.Y + rail.Height/2}); got.Color == state.pal.line {
		t.Fatalf("the terminal's view draws a line beside the rail: %+v", got)
	}
}

// The files panel is the assistant's neighbour in the same frame: it takes the
// frame's workspace surface too, and its outside edge beside the rail is drawn
// the same way.
func TestTheFilesPanelIsBoundedFromTheSideRail(t *testing.T) {
	state, _, _, _ := openTestSFTP(t)
	state.terminalToolbarPosition = terminalToolbarLeft

	canvas, _ := paintAt(state, panelWide)
	context, ok := state.deck.Context(state.deck.ActiveWorkspace())
	if !ok || !context.Open || context.Side != layoutcore.ContextLeft {
		t.Fatalf("the files panel is not docked left: %+v", context)
	}
	panel := state.contextRect
	if panel.Width <= 0 || panel.Height <= 0 {
		t.Fatalf("the files panel has no rectangle: %+v", panel)
	}
	rail := state.toolbarDockSideBoundsFor(panelWide, true)
	if panel.X != rail.X+rail.Width {
		t.Fatalf("the files panel starts at %v, want the rail's inner edge %v", panel.X, rail.X+rail.Width)
	}
	want := gfx.Rect{X: panel.X, Y: panel.Y, Width: 1, Height: panel.Height}
	if got := lastOpaqueAt(canvas, gfx.Point{X: want.X, Y: panel.Y + panel.Height/2}); !painted(got, want, state.pal.line) {
		t.Fatalf("the files panel's edge beside the rail is %+v, want the line at %v", got, want)
	}
}

// painted reports whether got is a fill of exactly bounds in color; a clip and
// the layer are not part of what a person sees.
func painted(got gfx.Rectangle, bounds gfx.Rect, color gfx.Color) bool {
	return got.Bounds == bounds && got.Color == color
}

// lastOpaqueAt is the last opaque rectangle painted over point: the surface a
// person sees there.
func lastOpaqueAt(canvas *gfx.Canvas, point gfx.Point) gfx.Rectangle {
	var last gfx.Rectangle
	for _, rectangle := range canvas.Rectangles() {
		bounds := rectangle.Bounds
		if point.X < bounds.X || point.Y < bounds.Y ||
			point.X >= bounds.X+bounds.Width || point.Y >= bounds.Y+bounds.Height {
			continue
		}
		if rectangle.Color.Alpha < 1 {
			continue
		}
		last = rectangle
	}
	return last
}
