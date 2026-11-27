package term

import "testing"

// TestTheUserShapeStandsWhenAProgramMayNotChangeIt covers the "Always use my
// style" switch, on by default: with it on, a DECSCUSR from the program
// (PowerShell asks for a block) must not replace the shape the user chose, so a
// light theme keeps a bar or underline instead of a black block.
func TestTheUserShapeStandsWhenAProgramMayNotChangeIt(t *testing.T) {
	screen := New(20, 3, 10)
	screen.SetDefaultCursor(CursorBar)
	screen.SetCursorFromProgram(false)

	screen.Write([]byte("\x1b[1 q")) // DECSCUSR 1: a blinking block
	if got := screen.Cursor().Style; got != CursorBar {
		t.Fatalf("the program changed the user's shape: cursor = %v, want the bar", got)
	}
	if screen.Cursor().Steady {
		t.Fatal("the program's steady request changed the user's cursor")
	}

	screen.SetCursorFromProgram(true)
	screen.Write([]byte("\x1b[1 q"))
	if got := screen.Cursor().Style; got != CursorBlock {
		t.Fatalf("the program could not change the shape again: cursor = %v, want the block", got)
	}
}

// TestAProgramsSteadyAndBlinkStillApplyToTheUserShape checks that keeping the
// shape does not swallow the steady/blink half of DECSCUSR.
func TestAProgramsSteadyAndBlinkStillApplyToTheUserShape(t *testing.T) {
	screen := New(20, 3, 10)
	screen.SetDefaultCursor(CursorBar)
	screen.SetCursorFromProgram(false)

	screen.Write([]byte("\x1b[2 q")) // DECSCUSR 2: a steady block
	if got := screen.Cursor().Style; got != CursorBar {
		t.Fatalf("the program changed the user's shape: cursor = %v, want the bar", got)
	}
	if !screen.Cursor().Steady {
		t.Fatal("the program's steady request was dropped with its shape")
	}

	screen.Write([]byte("\x1b[1 q")) // DECSCUSR 1: back to a blinking block
	if screen.Cursor().Steady {
		t.Fatal("the program's blink request was dropped with its shape")
	}
}

// TestTurningProgramsOffDropsTheShapeTheySet checks that the switch takes
// effect at once on a cursor a program already styled, while the steady request
// the program made still holds.
func TestTurningProgramsOffDropsTheShapeTheySet(t *testing.T) {
	screen := New(20, 3, 10)
	screen.SetDefaultCursor(CursorUnderline)
	screen.Write([]byte("\x1b[6 q")) // DECSCUSR 6: a steady bar
	if got := screen.Cursor().Style; got != CursorBar {
		t.Fatalf("the program's bar was not taken: cursor = %v", got)
	}
	if !screen.Cursor().Steady {
		t.Fatal("the program's steady request was not taken")
	}

	screen.SetCursorFromProgram(false)
	if got := screen.Cursor().Style; got != CursorUnderline {
		t.Fatalf("the user's shape did not come back: cursor = %v, want the underline", got)
	}
	if !screen.Cursor().Steady {
		t.Fatal("flipping the switch dropped the program's steady request")
	}
}
