package workbench

import (
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
)

// The terminal grid is text the user selects: the pointer over it is the
// I-beam, and stays so while a selection is dragged across it.
func TestTerminalPointerIsTheTextCursor(t *testing.T) {
	state, _, _, shell := liveSession(t)
	shell.say("selectable text\r\n")
	state.Wake()
	driver := paneDriver(t, state, gfx.Size{Width: 1200, Height: 800})
	grid := hitOf(t, driver, terminalTestHitByID(state, state.sessions[0].ID)).Bounds
	x, y := grid.X+grid.Width/2, grid.Y+grid.Height/2
	if err := driver.Move(x, y); err != nil {
		t.Fatal(err)
	}
	if got := driver.Snapshot().Cursor; got != app.CursorText {
		t.Fatalf("over the terminal the cursor is %v, want the text cursor", got)
	}
	if err := driver.Press(x, y, gfx.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	if err := driver.Move(x+state.cellWidth*8, y); err != nil {
		t.Fatal(err)
	}
	if got := driver.Snapshot().Cursor; got != app.CursorText {
		t.Fatalf("while selecting the cursor is %v, want the pressed control's", got)
	}
	if err := driver.Release(x+state.cellWidth*8, y, gfx.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	if !state.sessions[0].term.selection.selected {
		t.Fatal("the drag did not select text")
	}
}

// A program that asked for mouse reports owns the pointer: the terminal shows the
// default arrow over it, as other terminals do under a multiplexer, and goes back
// to the I-beam when the program turns the reports off.
func TestTerminalPointerFollowsMouseReporting(t *testing.T) {
	for _, mode := range []string{"9", "1000", "1002", "1003"} {
		t.Run(mode, func(t *testing.T) {
			state, _, _, shell := liveSession(t)
			driver := paneDriver(t, state, gfx.Size{Width: 1200, Height: 800})
			grid := hitOf(t, driver, terminalTestHitByID(state, state.sessions[0].ID)).Bounds
			x, y := grid.X+grid.Width/2, grid.Y+grid.Height/2
			if err := driver.Move(x, y); err != nil {
				t.Fatal(err)
			}
			shell.say("\x1b[?" + mode + "h")
			state.Wake()
			if err := driver.Paint(); err != nil {
				t.Fatal(err)
			}
			if err := driver.Move(x+1, y); err != nil {
				t.Fatal(err)
			}
			if got := driver.Snapshot().Cursor; got != app.CursorArrow {
				t.Fatalf("with mouse reporting %s the cursor is %v, want the default arrow", mode, got)
			}
			shell.say("\x1b[?" + mode + "l")
			state.Wake()
			if err := driver.Paint(); err != nil {
				t.Fatal(err)
			}
			if err := driver.Move(x, y); err != nil {
				t.Fatal(err)
			}
			if got := driver.Snapshot().Cursor; got != app.CursorText {
				t.Fatalf("after reporting %s is off the cursor is %v, want the text cursor", mode, got)
			}
		})
	}
}
