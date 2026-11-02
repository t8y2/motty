package workbench

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/ui"
)

// historyState is a five-row terminal with sixty numbered lines behind it.
func historyState() (*State, *Session, terminalGeometry) {
	state, session, geometry := selectionState()
	var text strings.Builder
	for line := 0; line < 65; line++ {
		fmt.Fprintf(&text, "line %d\r\n", line)
	}
	session.term.write([]byte(text.String()))
	return state, session, geometry
}

func barPointer(state *State, session *Session, kind ui.PointerEventKind, x, y float32) ui.EventResult {
	return state.Pointer(ui.PointerEvent{
		Kind: kind, Button: ui.PointerButtonPrimary, Target: hitTerminalBar.At(int(session.ID)),
		Captured: kind != ui.PointerDown, X: x, Y: y, Viewport: state.viewport,
	})
}

func TestTerminalWithHistoryShowsAScrollbarThatMovesTheView(t *testing.T) {
	state, session, _ := historyState()
	screen := session.term.screen
	history := screen.Scrollback()
	if history < 50 {
		t.Fatalf("fixture has only %d lines of history", history)
	}

	chrome, ok := state.terminalChromeForSession(session.ID)
	if !ok {
		t.Fatal("terminal chrome was not found")
	}
	actualGeometry := terminalLayoutWithChrome(state.viewport, state.cellWidth, state.cellHeight, chrome, state.padding())
	var canvas ui.Canvas
	state.paintGrid(&canvas, session, actualGeometry)
	bar, ok := hitByID(&canvas, hitTerminalBar.At(int(session.ID)))
	if !ok {
		t.Fatal("a terminal with history paints no scrollbar")
	}
	x := bar.Bounds.X + bar.Bounds.Width/2

	// At the live screen the thumb is at the bottom: dragging it up goes back.
	area := state.terminalScrollArea(nil, session, actualGeometry.area, 1).Geometry()
	grab := area.VerticalThumb.Y + area.VerticalThumb.Height/2
	barPointer(state, session, ui.PointerDown, x, grab)
	if result := barPointer(state, session, ui.PointerMove, x, bar.Bounds.Y); !result.Handled || !result.Repaint {
		t.Fatalf("dragging the thumb up = %+v", result)
	}
	if screen.ViewOffset() != history {
		t.Fatalf("thumb at the top shows %d lines back, want the oldest (%d)", screen.ViewOffset(), history)
	}
	barPointer(state, session, ui.PointerUp, x, bar.Bounds.Y)

	// A press on the track below the thumb pages toward the live screen.
	before := screen.ViewOffset()
	barPointer(state, session, ui.PointerDown, x, bar.Bounds.Y+bar.Bounds.Height-2)
	if want := before - (actualGeometry.rows - 1); screen.ViewOffset() != want {
		t.Fatalf("a press below the thumb moved to %d, want %d", screen.ViewOffset(), want)
	}
}

func TestTerminalScrollbarFollowsTheWheelAndNeedsHistory(t *testing.T) {
	state, session, geometry := historyState()
	screen := session.term.screen
	screen.Scroll(10)
	before := state.terminalScrollArea(nil, session, geometry.area, 1).Geometry().VerticalThumb.Y

	screen.Scroll(10)
	after := state.terminalScrollArea(nil, session, geometry.area, 1).Geometry().VerticalThumb.Y
	if after >= before {
		t.Fatalf("scrolling back left the thumb at %v, was %v", after, before)
	}

	var canvas ui.Canvas
	state.paintGrid(&canvas, session, geometry)
	if _, ok := hitByID(&canvas, hitTerminalBar.At(int(session.ID))); !ok {
		t.Fatal("no scrollbar over a terminal with history")
	}

	// The alternate screen has no history, and a terminal that has not scrolled
	// anything off has nothing to show.
	session.term.write([]byte("\x1b[?1049h"))
	canvas = ui.Canvas{}
	state.paintGrid(&canvas, session, geometry)
	if _, ok := hitByID(&canvas, hitTerminalBar.At(int(session.ID))); ok {
		t.Fatal("a scrollbar on the alternate screen")
	}
	fresh, freshSession, freshGeometry := selectionState()
	canvas = ui.Canvas{}
	fresh.paintGrid(&canvas, freshSession, freshGeometry)
	if _, ok := hitByID(&canvas, hitTerminalBar.At(int(freshSession.ID))); ok {
		t.Fatal("a scrollbar with nothing to scroll")
	}
}

// barAlpha paints the terminal with the pointer over hover and returns how
// opaque its scrollbar is.
func barAlpha(canvas *ui.Canvas, state *State, session *Session, geometry terminalGeometry, at time.Duration, hover ui.ID) float32 {
	canvas.SetClock(func() time.Duration { return at })
	canvas.SetInteraction(hover, ui.ID{}, ui.ID{}, false)
	canvas.BeginFrame()
	state.paintGrid(canvas, session, geometry)
	canvas.EndFrame()
	return state.terminalBarFade(canvas, session)
}

func TestTerminalScrollbarShowsWhileThePointerIsOverTheTerminal(t *testing.T) {
	state, session, geometry := historyState()
	var canvas ui.Canvas
	over := hitTerminalList.At(int(session.ID))
	var none ui.ID

	if got := barAlpha(&canvas, state, session, geometry, 0, none); got != 0 {
		t.Fatalf("an idle terminal shows its scrollbar (%v)", got)
	}
	if got := barAlpha(&canvas, state, session, geometry, time.Second, over); got <= 0 {
		t.Fatalf("the scrollbar stays hidden under the pointer (%v)", got)
	}
	// Staying put keeps it, however long.
	if got := barAlpha(&canvas, state, session, geometry, time.Minute, over); got != 1 {
		t.Fatalf("the scrollbar faded with the pointer still over the terminal (%v)", got)
	}
	barAlpha(&canvas, state, session, geometry, time.Minute+time.Second, none)
	if got := barAlpha(&canvas, state, session, geometry, time.Minute+2*time.Second, none); got != 0 {
		t.Fatalf("the scrollbar stayed after the pointer left (%v)", got)
	}
}

func TestTerminalScrollbarLingersWhenTheKeyboardScrolls(t *testing.T) {
	state, session, geometry := historyState()
	var canvas ui.Canvas
	var none ui.ID
	barAlpha(&canvas, state, session, geometry, 0, none)
	session.term.screen.Scroll(10)
	if got := barAlpha(&canvas, state, session, geometry, time.Second, none); got <= 0 {
		t.Fatalf("the scrollbar stays hidden while the view moves (%v)", got)
	}
	if got := barAlpha(&canvas, state, session, geometry, time.Second+terminalBarLinger/2, none); got != 1 {
		t.Fatalf("the scrollbar fades before it has lingered (%v)", got)
	}
	if got := barAlpha(&canvas, state, session, geometry, time.Second+terminalBarLinger*2, none); got != 0 {
		t.Fatalf("the scrollbar never goes away (%v)", got)
	}
}
