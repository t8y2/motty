package workbench

import (
	"context"
	"sync"
	"testing"

	"github.com/vivaui/viva/gfx"
	"motty/internal/desktop"
)

// pointingDesktop is a session whose remote names pointer shapes.
type pointingDesktop struct {
	*followingDesktop
	pointerMu sync.Mutex
	kind      desktop.PointerKind
	shape     desktop.PointerShape
	version   int
}

func (s *pointingDesktop) PointerShape() (desktop.PointerKind, desktop.PointerShape, int) {
	s.pointerMu.Lock()
	defer s.pointerMu.Unlock()
	return s.kind, s.shape, s.version
}

func (s *pointingDesktop) say(kind desktop.PointerKind, shape desktop.PointerShape) {
	s.pointerMu.Lock()
	s.kind, s.shape = kind, shape
	s.version++
	s.pointerMu.Unlock()
}

func pointerShapeOf(width, height, hotX, hotY int) desktop.PointerShape {
	return desktop.PointerShape{Width: width, Height: height, HotX: hotX, HotY: hotY, BGRA: make([]byte, width*height*4)}
}

// TestTheRemotePointerIsTheCursorOverThePicture follows what the remote says
// about its pointer to the cursor the page asks the system for, at the display
// scale, as the window paints it.
func TestTheRemotePointerIsTheCursorOverThePicture(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	state.options.Invalidate = func() {}
	state.connectionReconnect = reconnectOff
	var remote *pointingDesktop
	state.dialDesktop = func(context.Context, desktopRequest) (desktop.Session, error) {
		remote = &pointingDesktop{followingDesktop: newFollowingDesktop(1600, 900, false)}
		return remote, nil
	}
	size := gfx.Size{Width: 1400, Height: 900}
	screen := newFrames(state, 2)
	screen.paint(size, 0)
	eventCanvases[state] = &screen.canvas
	addStreamHost(state, Host{Label: "Win", Address: "win.example.com", User: "alice", Protocol: protocolRDP})
	openHost(state)
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	waitUntil(t, state, "the picture", func() bool { return session.desk != nil && session.desk.width > 0 })

	cursorOverPicture := func() gfx.Hit {
		t.Helper()
		state.Wake()
		screen.paint(size, 0)
		// The frames of one canvas stack their hits, and the pointer follows the last.
		hits := screen.canvas.Hits()
		for index := len(hits) - 1; index >= 0; index-- {
			if hits[index].ID == hitTerminalList.At(int(session.ID)) {
				return hits[index]
			}
		}
		t.Fatal("the desktop page has no hit")
		return gfx.Hit{}
	}

	if hit := cursorOverPicture(); hit.Cursor != gfx.PointerCursorCrosshair || hit.CursorImage != nil {
		t.Fatalf("before the remote names a pointer: %v, %v", hit.Cursor, hit.CursorImage)
	}

	// A shape becomes the system's pointer, as big as it was drawn in the screen's own pixels.
	remote.say(desktop.PointerBitmap, pointerShapeOf(32, 32, 4, 5))
	hit := cursorOverPicture()
	if hit.Cursor != gfx.PointerCursorImage || hit.CursorImage == nil {
		t.Fatalf("a named shape: %v, %v", hit.Cursor, hit.CursorImage)
	}
	if width, height := hit.CursorImage.Size(); width != 32 || height != 32 {
		t.Fatalf("size %dx%d", width, height)
	}
	if x, y := hit.CursorImage.Hotspot(); x != 4 || y != 5 {
		t.Fatalf("hotspot %d,%d", x, y)
	}
	if hit.CursorImage.Scale() != 2 {
		t.Fatalf("scale %v, want the display's 2", hit.CursorImage.Scale())
	}
	// An unchanged shape is the same cursor, so the system is not asked again.
	if again := cursorOverPicture(); again.CursorImage != hit.CursorImage {
		t.Fatal("an unchanged shape made a new cursor")
	}

	remote.say(desktop.PointerBitmap, pointerShapeOf(16, 16, 0, 0))
	if next := cursorOverPicture(); next.CursorImage == nil || next.CursorImage == hit.CursorImage {
		t.Fatal("a new shape kept the old cursor")
	}

	// A hotspot outside its picture, which a server can send, is pulled inside it.
	remote.say(desktop.PointerBitmap, pointerShapeOf(8, 8, 40, 40))
	if clamped := cursorOverPicture(); clamped.CursorImage == nil {
		t.Fatal("a shape with a stray hotspot was refused")
	} else if x, y := clamped.CursorImage.Hotspot(); x != 7 || y != 7 {
		t.Fatalf("clamped hotspot %d,%d", x, y)
	}

	remote.say(desktop.PointerHidden, desktop.PointerShape{})
	if hit := cursorOverPicture(); hit.Cursor != gfx.PointerCursorHidden || hit.CursorImage != nil {
		t.Fatalf("a hidden pointer: %v, %v", hit.Cursor, hit.CursorImage)
	}
	remote.say(desktop.PointerSystem, desktop.PointerShape{})
	if hit := cursorOverPicture(); hit.Cursor != gfx.PointerCursorDefault || hit.CursorImage != nil {
		t.Fatalf("the ordinary pointer: %v, %v", hit.Cursor, hit.CursorImage)
	}

	// A picture no system could draw is not worth losing the pointer over.
	remote.say(desktop.PointerBitmap, pointerShapeOf(gfx.MaxCursorImageSize+1, 1, 0, 0))
	if hit := cursorOverPicture(); hit.Cursor != gfx.PointerCursorDefault || hit.CursorImage != nil {
		t.Fatalf("an oversized shape: %v, %v", hit.Cursor, hit.CursorImage)
	}
}
