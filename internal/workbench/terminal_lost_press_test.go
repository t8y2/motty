package workbench

import (
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
)

// A press the window loses ends the drag the shells wait for. The panes hold
// their sizes while the pointer is down; a press the platform takes away (the
// window is deactivated, as when another window takes the pointer) must not
// leave them waiting forever, or a terminal keeps the width it had when the
// drag began until the user clicks again (#1734).
func TestAPressTheWindowLosesStillFitsThePanes(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	inputGuardPrompt(state, session, shell, "utf8", "")
	// A program that draws its own screen is told its size at once, unless a
	// press is held.
	shell.say("\x1b]133;C;motty=" + session.term.folderToken + "\x07")
	state.Wake()
	driver, err := app.NewDriver(state.WindowConfig(app.Config{
		Title: "lost press", Width: 1200, Height: 800, Tree: Tree(),
		Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnScroll: state.Scroll,
	}), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(driver.Close)

	// Press in the terminal and drag, which captures the pointer: the shells
	// now wait for the release.
	hit, found := paintedBounds(driver, terminalHit(state, 0))
	if !found {
		t.Fatal("the terminal control was not painted")
	}
	at := gfx.Point{X: hit.X + hit.Width/2, Y: hit.Y + hit.Height/2}
	if err := driver.Move(at.X, at.Y); err != nil {
		t.Fatal(err)
	}
	if err := driver.Press(at.X, at.Y, gfx.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	if err := driver.Move(at.X+8, at.Y+8); err != nil {
		t.Fatal(err)
	}
	// The platform takes the press: no release follows.
	if err := driver.DeactivateWindow(); err != nil {
		t.Fatal(err)
	}
	before := shell.resizeCount()
	state.Resize(gfx.Size{Width: 1000, Height: 700})
	if got := shell.resizeCount(); got != before+1 {
		t.Fatalf("the panes were fitted %d time(s) after a lost press, want 1", got-before)
	}
	cols, rows := session.term.screen.Size()
	if last := shell.lastResize(); last != [2]int{cols, rows} {
		t.Fatalf("the shell was told %v, want %dx%d", last, cols, rows)
	}
}

// paintedBounds is the painted rectangle of a control, for the pointer to press
// inside it.
func paintedBounds(driver *app.Driver, id gfx.ID) (gfx.Rect, bool) {
	for _, hit := range driver.Snapshot().Hits {
		if hit.ID == id && !hit.IsLabel && hit.Bounds.Width > 0 && hit.Bounds.Height > 0 {
			return hit.Bounds, true
		}
	}
	return gfx.Rect{}, false
}
