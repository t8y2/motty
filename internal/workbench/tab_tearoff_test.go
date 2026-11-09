package workbench

import (
	"errors"
	"testing"

	"github.com/vivaui/viva/gfx"
)

func showsHint(state *State, hint string) bool {
	_, texts := paintedTexts(state)
	return contains(texts, hint)
}

func TestTabMenuMovesATabToAWindowOfItsOwn(t *testing.T) {
	pair := newTwoWindows(t)
	live := liveTab(t, pair.first, pair.firstW, 0)
	liveTab(t, pair.first, pair.firstW, 1)
	moving := tabOrder(pair.first)[0]

	rightClickTab(pair.first, hitTabList.At(int(moving)))
	var canvas gfx.Canvas
	pair.first.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	hit, ok := hitByID(&canvas, hitTabMenuNewWindow)
	if !ok || hit.Disabled {
		t.Fatalf("the menu does not offer a window of its own: %+v, found %v", hit, ok)
	}
	pair.first.Click(hitTabMenuNewWindow)

	if len(pair.torn) != 1 {
		t.Fatalf("%d windows were opened", len(pair.torn))
	}
	window := pair.torn[0]
	if len(tabOrder(window)) != 1 || len(window.sessions) != 1 || window.sessions[0].conn != Connection(live) {
		t.Fatalf("the new window has %d tabs and %d sessions", len(tabOrder(window)), len(window.sessions))
	}
	if len(tabOrder(pair.first)) != 1 || live.canceled != 0 {
		t.Fatalf("first window tabs %d, canceled %d", len(tabOrder(pair.first)), live.canceled)
	}
	// Whatever the platform's coordinates, the new window must not lie exactly
	// over the one it came from.
	from, _ := pair.first.frame()
	if to, _ := window.frame(); to.X == from.X && to.Y == from.Y {
		t.Fatalf("the new window opened exactly over the old one at %+v", to)
	}
}

func TestTabMenuKeepsAWindowsOnlyTab(t *testing.T) {
	pair := newTwoWindows(t)
	liveTab(t, pair.first, pair.firstW, 0)
	only := tabOrder(pair.first)[0]

	rightClickTab(pair.first, hitTabList.At(int(only)))
	var canvas gfx.Canvas
	pair.first.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	if hit, ok := hitByID(&canvas, hitTabMenuNewWindow); !ok || !hit.Disabled {
		t.Fatalf("a window's only tab can be moved out of it: %+v, found %v", hit, ok)
	}
	pair.first.Click(hitTabMenuNewWindow)
	if len(pair.torn) != 0 {
		t.Fatal("a window was opened for the only tab")
	}
}

func TestADragOutOfEveryWindowSaysItOpensANewWindow(t *testing.T) {
	pair := newTwoWindows(t)
	liveTab(t, pair.first, pair.firstW, 0)
	liveTab(t, pair.first, pair.firstW, 1)
	moving := tabOrder(pair.first)[0]
	hint := pair.first.messages.TabTearOffHint
	target := hitTabList.At(int(moving))
	start := tabCenter(pair.first, moving)

	pair.first.Pointer(gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, Target: target, X: start.X, Y: start.Y, ScreenX: start.X, ScreenY: start.Y, Viewport: pair.first.viewport, ClickCount: 1})
	// Inside the window the drag is a reorder or a dock: no talk of windows.
	pair.first.Pointer(gfx.PointerEvent{Kind: gfx.PointerMove, Target: target, Captured: true, X: start.X + 40, Y: start.Y, ScreenX: start.X + 40, ScreenY: start.Y, Viewport: pair.first.viewport})
	if showsHint(pair.first, hint) {
		t.Fatal("the hint shows while the tab is still inside the window")
	}
	// Past the right edge, clear of the second window at x 1500.
	pair.first.Pointer(gfx.PointerEvent{Kind: gfx.PointerMove, Target: target, Captured: true, X: 1450, Y: 300, ScreenX: 1450, ScreenY: 300, Viewport: pair.first.viewport})
	if !showsHint(pair.first, hint) {
		t.Fatal("a drag that left every window does not say it opens a new window")
	}
	// Over another window that window takes the tab, so the first has nothing to announce.
	pair.first.Pointer(gfx.PointerEvent{Kind: gfx.PointerMove, Target: target, Captured: true, X: 2000, Y: 300, ScreenX: 2000, ScreenY: 300, Viewport: pair.first.viewport})
	if showsHint(pair.first, hint) {
		t.Fatal("the hint shows while another window is under the pointer")
	}
	pair.first.Key(gfx.KeyEvent{Key: gfx.KeyEscape})
	if len(pair.torn) != 0 {
		t.Fatal("cancelling the drag opened a window")
	}
}

type fakePreview struct {
	origins []gfx.Point
	closed  int
	paint   func(*gfx.Canvas, gfx.Size)
	size    gfx.Size
}

func (preview *fakePreview) MoveTo(origin gfx.Point) {
	preview.origins = append(preview.origins, origin)
}
func (preview *fakePreview) Close() { preview.closed++ }

// previewing lets a window open the picture that follows a drag, and returns
// every one it opened.
func previewing(state *State) *[]*fakePreview {
	opened := &[]*fakePreview{}
	state.options.OpenDragPreview = func(size gfx.Size, origin gfx.Point, paint func(*gfx.Canvas, gfx.Size)) (DragPreview, error) {
		preview := &fakePreview{origins: []gfx.Point{origin}, paint: paint, size: size}
		*opened = append(*opened, preview)
		return preview, nil
	}
	return opened
}

func TestADragOutOfEveryWindowIsFollowedByAPictureOfTheTab(t *testing.T) {
	pair := newTwoWindows(t)
	opened := previewing(pair.first)
	liveTab(t, pair.first, pair.firstW, 0)
	liveTab(t, pair.first, pair.firstW, 1)
	moving := tabOrder(pair.first)[0]
	hint := pair.first.messages.TabTearOffHint
	target := hitTabList.At(int(moving))
	start := tabCenter(pair.first, moving)
	move := func(x, y float32) {
		pair.first.Pointer(gfx.PointerEvent{Kind: gfx.PointerMove, Target: target, Captured: true, X: x, Y: y, ScreenX: x, ScreenY: y, Viewport: pair.first.viewport})
	}

	pair.first.Pointer(gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, Target: target, X: start.X, Y: start.Y, ScreenX: start.X, ScreenY: start.Y, Viewport: pair.first.viewport, ClickCount: 1})
	move(start.X+40, start.Y)
	pair.first.Paint(&gfx.Canvas{}, gfx.Size{Width: 1400, Height: 900})
	if len(*opened) != 0 {
		t.Fatal("a picture was opened while the drag was still in the window")
	}

	move(1450, 300)
	if len(*opened) != 1 {
		t.Fatalf("%d pictures opened outside every window", len(*opened))
	}
	preview := (*opened)[0]
	if preview.size.Width < barTabMin || preview.size.Height != barTabHeight {
		t.Fatalf("the picture is %v, not a tab", preview.size)
	}
	// The pointer stays inside the picture, wherever the window puts its corner.
	pointer := gfx.Point{X: 1450, Y: 300}
	at := preview.origins[len(preview.origins)-1]
	if pointer.X < at.X || pointer.X > at.X+preview.size.Width || pointer.Y < at.Y || pointer.Y > at.Y+preview.size.Height {
		t.Fatalf("the pointer %v is outside the picture at %v of %v", pointer, at, preview.size)
	}
	if showsHint(pair.first, hint) {
		t.Fatal("the window also writes its hint while a picture says it")
	}
	// The picture draws the tab with the dragged tab's name.
	var canvas gfx.Canvas
	preview.paint(&canvas, preview.size)
	found := false
	for _, text := range canvas.Texts() {
		found = found || text.Value == pair.first.tabLabel(pair.first.workspaceTabs[0])
	}
	if !found {
		t.Fatal("the picture does not carry the tab's name")
	}

	move(1460, 320)
	if len(*opened) != 1 || len(preview.origins) < 2 {
		t.Fatalf("pictures %d, moves %d: a second move must move the one picture", len(*opened), len(preview.origins))
	}
	// Back in the window the picture goes: the window draws the tab itself.
	move(start.X+60, start.Y)
	if preview.closed != 1 {
		t.Fatalf("closed %d times after the drag came back", preview.closed)
	}
	// Out again, then let go: the picture goes and a window opens.
	move(1450, 300)
	again := (*opened)[len(*opened)-1]
	pair.first.Pointer(gfx.PointerEvent{Kind: gfx.PointerUp, Button: gfx.PointerButtonPrimary, Target: target, Captured: true, X: 1450, Y: 300, ScreenX: 1450, ScreenY: 300, Viewport: pair.first.viewport, ClickCount: 1})
	if again.closed != 1 || len(pair.torn) != 1 {
		t.Fatalf("after the drop the picture closed %d times and %d windows opened", again.closed, len(pair.torn))
	}
}

func TestEscapeTakesTheDragPictureAway(t *testing.T) {
	pair := newTwoWindows(t)
	opened := previewing(pair.first)
	liveTab(t, pair.first, pair.firstW, 0)
	liveTab(t, pair.first, pair.firstW, 1)
	moving := tabOrder(pair.first)[0]
	target := hitTabList.At(int(moving))
	start := tabCenter(pair.first, moving)
	pair.first.Pointer(gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, Target: target, X: start.X, Y: start.Y, ScreenX: start.X, ScreenY: start.Y, Viewport: pair.first.viewport, ClickCount: 1})
	pair.first.Pointer(gfx.PointerEvent{Kind: gfx.PointerMove, Target: target, Captured: true, X: 1450, Y: 300, ScreenX: 1450, ScreenY: 300, Viewport: pair.first.viewport})
	if len(*opened) != 1 {
		t.Fatalf("%d pictures", len(*opened))
	}
	pair.first.Key(gfx.KeyEvent{Key: gfx.KeyEscape})
	if (*opened)[0].closed != 1 || len(pair.torn) != 0 {
		t.Fatalf("picture closed %d times, %d windows opened", (*opened)[0].closed, len(pair.torn))
	}
}

func TestAPlatformWithoutADragPictureStillGetsTheWindowHint(t *testing.T) {
	pair := newTwoWindows(t)
	pair.first.options.OpenDragPreview = func(gfx.Size, gfx.Point, func(*gfx.Canvas, gfx.Size)) (DragPreview, error) {
		return nil, errors.New("unsupported")
	}
	liveTab(t, pair.first, pair.firstW, 0)
	liveTab(t, pair.first, pair.firstW, 1)
	moving := tabOrder(pair.first)[0]
	target := hitTabList.At(int(moving))
	start := tabCenter(pair.first, moving)
	pair.first.Pointer(gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, Target: target, X: start.X, Y: start.Y, ScreenX: start.X, ScreenY: start.Y, Viewport: pair.first.viewport, ClickCount: 1})
	pair.first.Pointer(gfx.PointerEvent{Kind: gfx.PointerMove, Target: target, Captured: true, X: 1450, Y: 300, ScreenX: 1450, ScreenY: 300, Viewport: pair.first.viewport})
	if !showsHint(pair.first, pair.first.messages.TabTearOffHint) {
		t.Fatal("a refused picture leaves the drag with no cue at all")
	}
	pair.first.Key(gfx.KeyEvent{Key: gfx.KeyEscape})
}
