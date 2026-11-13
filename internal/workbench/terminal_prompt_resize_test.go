package workbench

import (
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"
)

// A shell at its prompt is told its size once the size holds, so a drag cannot
// leave it redrawing the prompt for widths the screen has already left; a
// program that draws its own screen is told at once.
func TestAShellAtItsPromptIsToldItsSizeWhenItHolds(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	clock := time.Unix(1, 0)
	state.options.WallNow = func() time.Time { return clock }
	inputGuardPrompt(state, session, shell, "utf8", "")
	if !session.term.screen.AtPrompt() {
		t.Fatal("the shell is not at the prompt its markers reported")
	}
	// The first paint settles the cell size and the page's viewport.
	size := gfx.Size{Width: 1000, Height: 700}
	var canvas gfx.Canvas
	state.Paint(&canvas, size)
	before := shell.resizeCount()
	// Two changes in a row: the shell must not be told the first one.
	state.Resize(gfx.Size{Width: 1200, Height: 800})
	state.Resize(gfx.Size{Width: 1100, Height: 760})
	if got := shell.resizeCount(); got != before {
		t.Fatalf("the shell was told %d size(s) while the size was still changing", got-before)
	}
	clock = clock.Add(shellResizeDelay)
	state.Paint(&canvas, gfx.Size{Width: 1100, Height: 760})
	cols, rows := session.term.screen.Size()
	if last := shell.lastResize(); last != [2]int{cols, rows} {
		t.Fatalf("the shell was told %v, want the held size %dx%d", last, cols, rows)
	}
	if got := shell.resizeCount(); got != before+1 {
		t.Fatalf("the held size was sent %d times", got-before)
	}
}

// A program that is not at a prompt draws its own screen and gets the size
// without waiting for it to hold.
func TestAProgramGetsItsSizeAtOnce(t *testing.T) {
	state, _, _, shell := liveSession(t)
	inputGuardPrompt(state, &state.sessions[0], shell, "utf8", "")
	shell.say("\x1b]133;C;motty=" + state.sessions[0].term.folderToken + "\x07")
	state.Wake()
	before := shell.resizeCount()
	state.Resize(gfx.Size{Width: 1100, Height: 760})
	if got := shell.resizeCount(); got != before+1 {
		t.Fatalf("a running program was told %d size(s), want 1", got-before)
	}
	cols, rows := state.sessions[0].term.screen.Size()
	if last := shell.lastResize(); last != [2]int{cols, rows} {
		t.Fatalf("the program was told %v, want %dx%d", last, cols, rows)
	}
}

// While the pointer is down the panes can change size on every motion; the
// shells are told once, when it is released.
func TestShellSizesWaitForThePointerRelease(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	inputGuardPrompt(state, session, shell, "utf8", "")
	shell.say("\x1b]133;C;motty=" + session.term.folderToken + "\x07")
	state.Wake()
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerDown, X: 10, Y: 10, Button: gfx.PointerButtonPrimary, Captured: true})
	state.Resize(gfx.Size{Width: 1200, Height: 800})
	state.Resize(gfx.Size{Width: 1100, Height: 760})
	if got := shell.resizeCount(); got != 0 {
		t.Fatalf("the shell was told %d size(s) while the pointer was down", got)
	}
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerUp, X: 1100, Y: 760, Button: gfx.PointerButtonPrimary})
	if got := shell.resizeCount(); got != 1 {
		t.Fatalf("the release told the shell %d size(s), want 1", got)
	}
	cols, rows := session.term.screen.Size()
	if last := shell.lastResize(); last != [2]int{cols, rows} {
		t.Fatalf("the shell was told %v, want the released size %dx%d", last, cols, rows)
	}
}
