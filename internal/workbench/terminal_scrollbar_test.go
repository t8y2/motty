package workbench

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"
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

func barPointer(state *State, session *Session, kind gfx.PointerEventKind, x, y float32) gfx.EventResult {
	return state.Pointer(gfx.PointerEvent{
		Kind: kind, Button: gfx.PointerButtonPrimary, Target: hitTerminalBar.At(int(session.ID)),
		Captured: kind != gfx.PointerDown, X: x, Y: y, Viewport: state.viewport,
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
	var canvas gfx.Canvas
	state.paintGrid(&canvas, session, actualGeometry)
	bar, ok := hitByID(&canvas, hitTerminalBar.At(int(session.ID)))
	if !ok {
		t.Fatal("a terminal with history paints no scrollbar")
	}
	x := bar.Bounds.X + bar.Bounds.Width/2

	// At the live screen the thumb is at the bottom: dragging it up goes back.
	area := state.terminalScrollArea(nil, session, actualGeometry.area, 1).Geometry()
	grab := area.VerticalThumb.Y + area.VerticalThumb.Height/2
	barPointer(state, session, gfx.PointerDown, x, grab)
	if result := barPointer(state, session, gfx.PointerMove, x, bar.Bounds.Y); !result.Handled || !result.Repaint {
		t.Fatalf("dragging the thumb up = %+v", result)
	}
	if screen.ViewOffset() != history {
		t.Fatalf("thumb at the top shows %d lines back, want the oldest (%d)", screen.ViewOffset(), history)
	}
	barPointer(state, session, gfx.PointerUp, x, bar.Bounds.Y)

	// A press on the track below the thumb pages toward the live screen.
	before := screen.ViewOffset()
	barPointer(state, session, gfx.PointerDown, x, bar.Bounds.Y+bar.Bounds.Height-2)
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

	var canvas gfx.Canvas
	state.paintGrid(&canvas, session, geometry)
	if _, ok := hitByID(&canvas, hitTerminalBar.At(int(session.ID))); !ok {
		t.Fatal("no scrollbar over a terminal with history")
	}

	// The alternate screen has no history, and a terminal that has not scrolled
	// anything off has nothing to show.
	session.term.write([]byte("\x1b[?1049h"))
	canvas = gfx.Canvas{}
	state.paintGrid(&canvas, session, geometry)
	if _, ok := hitByID(&canvas, hitTerminalBar.At(int(session.ID))); ok {
		t.Fatal("a scrollbar on the alternate screen")
	}
	fresh, freshSession, freshGeometry := selectionState()
	canvas = gfx.Canvas{}
	fresh.paintGrid(&canvas, freshSession, freshGeometry)
	if _, ok := hitByID(&canvas, hitTerminalBar.At(int(freshSession.ID))); ok {
		t.Fatal("a scrollbar with nothing to scroll")
	}
}

func TestTerminalRowsKeepTheirTextIdentityAcrossScrolling(t *testing.T) {
	state, session, geometry := historyState()
	// Move off the live screen so every terminal row holds a written line; the
	// blank row under the cursor paints no label.
	session.term.screen.Scroll(2)
	var canvas gfx.Canvas
	paint := func() []gfx.TextIdentity {
		canvas.Reset()
		state.paintGrid(&canvas, session, geometry)
		identities := make([]gfx.TextIdentity, 0, geometry.rows)
		for _, text := range canvas.Texts() {
			if text.Identity.Owner == session.ID && text.Identity.Item != 0 {
				identities = append(identities, text.Identity)
			}
		}
		return identities
	}
	before := paint()
	_, rows := session.term.presentedSize()
	visible := 0
	for row := 0; row < rows; row++ {
		if session.term.screen.Row(row).ID != 0 {
			visible++
		}
	}
	if len(before) != visible {
		t.Fatalf("grid labels = %d, want one per visible row (%d)", len(before), visible)
	}
	for row, identity := range before {
		if want := session.term.screen.Row(row).ID; identity != (gfx.TextIdentity{Owner: session.ID, Item: want}) {
			t.Fatalf("row %d identity = %+v, want owner %d item %d", row, identity, session.ID, want)
		}
	}

	// One line of wheel scrolling moves every row up: the rows that remain
	// visible must keep their identities so the backend can reuse their textures.
	session.term.screen.Scroll(1)
	after := paint()
	if len(after) != len(before) {
		t.Fatalf("labels after scrolling = %d, want %d", len(after), len(before))
	}
	if !slices.Equal(after[1:], before[:len(before)-1]) {
		t.Fatalf("identities after scrolling = %+v, want moved %+v", after, before)
	}
}

// barAlpha paints the terminal with the pointer over hover and returns how
// opaque its scrollbar is.
func barAlpha(canvas *gfx.Canvas, state *State, session *Session, geometry terminalGeometry, at time.Duration, hover gfx.ID) float32 {
	canvas.SetClock(func() time.Duration { return at })
	canvas.SetInteraction(hover, gfx.ID{}, gfx.ID{}, false)
	canvas.BeginFrame()
	state.paintGrid(canvas, session, geometry)
	canvas.EndFrame()
	return state.terminalBarFade(canvas, session)
}

func TestTerminalScrollbarShowsWhileThePointerIsOverTheTerminal(t *testing.T) {
	state, session, geometry := historyState()
	var canvas gfx.Canvas
	over := hitTerminalList.At(int(session.ID))
	var none gfx.ID

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
	var canvas gfx.Canvas
	var none gfx.ID
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
