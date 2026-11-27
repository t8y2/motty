package workbench

import (
	"context"
	"sync"
	"testing"
	"time"

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

func TestEndedDesktopPresentsPendingPictureWithoutRestoringRemotePointer(t *testing.T) {
	remote := &pointingDesktop{followingDesktop: newFollowingDesktop(640, 480, false)}
	view := newDesktopView(remote, false)
	t.Cleanup(view.close)
	remote.say(desktop.PointerHidden, desktop.PointerShape{})
	view.ended = true
	if !view.present() || view.width != 640 || view.height != 480 {
		t.Fatalf("pending picture was not presented after disconnect: %dx%d", view.width, view.height)
	}
	if cursor, image := view.pointerCursor(1); cursor != gfx.PointerCursorDefault || image != nil {
		t.Fatalf("disconnected picture restored remote cursor: %v, %v", cursor, image)
	}
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

	// An ended connection restores the system cursor so the disconnected screen and reconnect
	// button can be navigated even if the remote had hidden the pointer before disconnecting.
	remote.say(desktop.PointerHidden, desktop.PointerShape{})
	if hit := cursorOverPicture(); hit.Cursor != gfx.PointerCursorHidden {
		t.Fatalf("expected hidden pointer before disconnect, got %v", hit.Cursor)
	}
	state.endDesktop(session)
	if hit := cursorOverPicture(); hit.Cursor != gfx.PointerCursorDefault || hit.CursorImage != nil {
		t.Fatalf("after connection ends: cursor=%v image=%v, want default arrow cursor", hit.Cursor, hit.CursorImage)
	}
}

func TestVNCPointerIsSystemByDefaultAndBitmapWhenNamed(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	state.options.Invalidate = func() {}
	state.connectionReconnect = reconnectOff
	var remote *pointingDesktop
	state.dialDesktop = func(context.Context, desktopRequest) (desktop.Session, error) {
		remote = &pointingDesktop{
			followingDesktop: newFollowingDesktop(1024, 768, false),
			kind:             desktop.PointerSystem,
			version:          1,
		}
		return remote, nil
	}
	size := gfx.Size{Width: 1400, Height: 900}
	screen := newFrames(state, 2)
	screen.paint(size, 0)
	eventCanvases[state] = &screen.canvas
	addStreamHost(state, Host{Label: "VNC Host", Address: "vnc.example.com:5900", Protocol: protocolVNC})
	openHost(state)
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	waitUntil(t, state, "the picture", func() bool { return session.desk != nil && session.desk.width > 0 })

	cursorOverPicture := func() gfx.Hit {
		t.Helper()
		state.Wake()
		screen.paint(size, 0)
		hits := screen.canvas.Hits()
		for index := len(hits) - 1; index >= 0; index-- {
			if hits[index].ID == hitTerminalList.At(int(session.ID)) {
				return hits[index]
			}
		}
		t.Fatal("the desktop page has no hit")
		return gfx.Hit{}
	}

	if hit := cursorOverPicture(); hit.Cursor != gfx.PointerCursorDefault || hit.CursorImage != nil {
		t.Fatalf("VNC initial cursor: %v, %v; want PointerCursorDefault", hit.Cursor, hit.CursorImage)
	}

	remote.say(desktop.PointerBitmap, pointerShapeOf(32, 32, 2, 2))
	if hit := cursorOverPicture(); hit.Cursor != gfx.PointerCursorImage || hit.CursorImage == nil {
		t.Fatalf("VNC named shape cursor: %v, %v; want PointerCursorImage", hit.Cursor, hit.CursorImage)
	}
}

// The remote's inverting pointer (the Windows "Inverted" scheme) shows the
// picture through its body: white over a dark picture, black over a light one.
// A system cursor image cannot do that, so the workbench makes one for the
// pointer's place from the shape and the picture under it and hands it to the
// system, which draws and moves it like any pointer. Issue 1603: the body was
// drawn opaque black with a white edge whatever lay behind it.
func TestAnInvertingRemotePointerBecomesASystemPictureOfThePictureUnderIt(t *testing.T) {
	size := gfx.Size{Width: 1400, Height: 900}
	_, options := newWorld(t)
	state := newTestState(t, options)
	state.options.Invalidate = func() {}
	state.connectionReconnect = reconnectOff
	var remote *pointingDesktop
	state.dialDesktop = func(_ context.Context, request desktopRequest) (desktop.Session, error) {
		width, height := request.Width, request.Height
		if width == 0 {
			width, height = 1600, 900
		}
		remote = &pointingDesktop{followingDesktop: newFollowingDesktop(width, height, true)}
		return remote, nil
	}
	screen := newFrames(state, 1)
	screen.paint(size, 0)
	eventCanvases[state] = &screen.canvas
	addStreamHost(state, Host{Label: "Win", Address: "win.example.com", User: "alice", Protocol: protocolRDP})
	openHost(state)
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	waitUntil(t, state, "the picture", func() bool { return session.desk != nil && session.desk.width > 0 })

	// A grey picture, so every inverted pixel has one known value.
	width, height := session.desk.width, session.desk.height
	picture := make([]byte, width*height*4)
	for at := 0; at < len(picture); at += 4 {
		picture[at], picture[at+1], picture[at+2], picture[at+3] = 200, 200, 200, 255
	}
	session.desk.surface.Update(0, 0, width, height, picture, width*4)

	// 4x1: an opaque black pixel, two inverting body pixels, one transparent one.
	shape := pointerShapeOf(4, 1, 0, 0)
	shape.BGRA[3] = 255
	shape.Invert = make([]bool, 4)
	shape.Invert[1], shape.Invert[2] = true, true
	remote.say(desktop.PointerBitmap, shape)
	screen.canvas.Reset()
	screen.paint(size, time.Millisecond) // the page learns the shape as it presents

	// The picture hit asks the system to draw a picture made at the pointer's
	// place rather than naming one fixed image.
	pictureHit := hitTerminalList.At(int(session.ID))
	hit := lastCanvasHit(t, &screen.canvas, pictureHit)
	if hit.Cursor != gfx.PointerCursorImage || hit.CursorImage != nil || hit.CursorImageAt == 0 {
		t.Fatalf("inverting pointer hit: cursor %v image %v imageAt %d", hit.Cursor, hit.CursorImage, hit.CursorImageAt)
	}
	imageAt := screen.canvas.CursorImageAt(&hit)
	if imageAt == nil {
		t.Fatal("the picture hit declared no position-dependent cursor")
	}

	area := desktopLayout(size, false).area
	origin, scale := session.desk.place(area)
	at := gfx.Point{X: origin.X + float32(width)*scale/2, Y: origin.Y + float32(height)*scale/2}
	image := imageAt(at)
	if image == nil {
		t.Fatal("the inverting pointer has no picture")
	}
	if width, height := image.Size(); width != 4 || height != 1 {
		t.Fatalf("picture %dx%d, want 4x1", width, height)
	}
	if x, y := image.Hotspot(); x != 0 || y != 0 {
		t.Fatalf("hotspot %d,%d", x, y)
	}
	if scale := image.Scale(); scale != 1 {
		t.Fatalf("scale %v, want the display's 1", scale)
	}
	pixels := image.Pixels()
	if pixels[0] != 0 || pixels[1] != 0 || pixels[2] != 0 || pixels[3] != 0xFF {
		t.Fatalf("outline pixel = %v, want opaque black", pixels[:4])
	}
	for _, column := range []int{1, 2} {
		at := column * 4
		if pixels[at] != 55 || pixels[at+1] != 55 || pixels[at+2] != 55 || pixels[at+3] != 0xFF {
			t.Fatalf("inverting pixel %d = %v, want the inverse of the grey picture", column, pixels[at:at+4])
		}
	}
	if pixels[15] != 0 {
		t.Fatalf("transparent pixel alpha = %d", pixels[15])
	}
	// The same picture further along one surface is the same pointer: no new
	// cursor is made while moving.
	if again := imageAt(gfx.Point{X: at.X + 3, Y: at.Y + 1}); again != image {
		t.Fatal("moving over one surface made a new pointer picture")
	}
	// A move over the picture asks for no repaint: the system moves the pointer,
	// so the remote picture is not painted again for it.
	move := gfx.PointerEvent{Kind: gfx.PointerMove, X: at.X + 20, Y: at.Y, Target: pictureHit, Viewport: size}
	if result := state.desktopPointer(session, move); result.Repaint {
		t.Fatal("a move of the inverting pointer asked for a repaint")
	}
	// A picture that changes under the pointer gives it a new colour, and the
	// picture the system already has stays as it was.
	before := append([]byte(nil), image.Pixels()...)
	picture = make([]byte, width*height*4)
	for at := 0; at < len(picture); at += 4 {
		picture[at], picture[at+1], picture[at+2], picture[at+3] = 10, 20, 30, 255
	}
	session.desk.surface.Update(0, 0, width, height, picture, width*4)
	updated := imageAt(gfx.Point{X: at.X + 3, Y: at.Y + 1})
	if updated == nil || updated == image {
		t.Fatal("a changed picture under the pointer kept the same cursor")
	}
	if got := updated.Pixels()[4:8]; got[0] != 245 || got[1] != 235 || got[2] != 225 || got[3] != 255 {
		t.Fatalf("inverting pixel over the new picture = %v, want its inverse", got)
	}
	if string(image.Pixels()) != string(before) {
		t.Fatalf("the pointer picture the system has was changed: %v -> %v", before, image.Pixels())
	}
}

// The picture of the inverting pointer is read from the remote pixels it
// covers, at any zoom: not from the rectangle where it starts.
func TestTheInvertingPointerPictureSamplesThePictureUnderIt(t *testing.T) {
	var view desktopView
	view.surface.Resize(10, 10)
	picture := make([]byte, 10*10*4)
	for at := 0; at < len(picture); at += 4 {
		picture[at], picture[at+1], picture[at+2], picture[at+3] = 200, 200, 200, 255
	}
	// The pixel the pointer's own pixels cover at (3,4), and its neighbour.
	at := (4*10 + 3) * 4
	picture[at], picture[at+1], picture[at+2] = 10, 20, 30
	at = (4*10 + 4) * 4
	picture[at], picture[at+1], picture[at+2] = 200, 100, 50
	view.surface.Update(0, 0, 10, 10, picture, 10*4)

	view.width, view.height = 10, 10
	view.fit = true
	view.area = gfx.Rect{Width: 20, Height: 20} // one remote pixel is two page pixels
	view.pixelScale = 1
	view.inverted = true
	view.pointerKind = desktop.PointerBitmap
	view.pointerShape = desktop.PointerShape{Width: 2, Height: 1, Invert: []bool{true, true},
		BGRA: make([]byte, 2*4)}

	image := view.cursorImageAt(gfx.Point{X: 6, Y: 8})
	if image == nil {
		t.Fatal("the pointer has no picture")
	}
	if width, height := image.Size(); width != 2 || height != 1 {
		t.Fatalf("picture %dx%d, want 2x1", width, height)
	}
	for column := range 2 {
		got := image.Pixels()[column*4 : column*4+4]
		if got[0] != 245 || got[1] != 235 || got[2] != 225 || got[3] != 255 {
			t.Fatalf("pixel %d = %v, want the inverse of the remote pixel (10,20,30)", column, got)
		}
	}
}

// One picture stays one pointer while the pixels it would show do not change,
// and a new one is made only when they do; the picture already handed to the
// system is never rewritten.
func TestTheInvertingPointerPictureIsKeptWhileItsPixelsAre(t *testing.T) {
	var view desktopView
	view.surface.Resize(10, 10)
	picture := make([]byte, 10*10*4)
	for at := 0; at < len(picture); at += 4 {
		picture[at], picture[at+1], picture[at+2], picture[at+3] = 200, 200, 200, 255
	}
	view.surface.Update(0, 0, 10, 10, picture, 10*4)
	view.width, view.height = 10, 10
	view.fit = true
	view.area = gfx.Rect{Width: 20, Height: 20}
	view.pixelScale = 1
	view.inverted = true
	view.pointerKind = desktop.PointerBitmap
	view.pointerShape = desktop.PointerShape{Width: 2, Height: 1, Invert: []bool{true, true}, BGRA: make([]byte, 8)}

	first := view.cursorImageAt(gfx.Point{X: 6, Y: 8})
	if first == nil {
		t.Fatal("the pointer has no picture")
	}
	snapshot := append([]byte(nil), first.Pixels()...)
	if same := view.cursorImageAt(gfx.Point{X: 8, Y: 8}); same != first {
		t.Fatal("one unchanged surface made a new pointer picture")
	}
	// The picture under the pointer changes.
	picture = make([]byte, 10*10*4)
	for at := 0; at < len(picture); at += 4 {
		picture[at], picture[at+1], picture[at+2], picture[at+3] = 0, 0, 0, 255
	}
	view.surface.Update(0, 0, 10, 10, picture, 10*4)
	next := view.cursorImageAt(gfx.Point{X: 6, Y: 8})
	if next == nil || next == first {
		t.Fatal("a changed picture under the pointer kept the same cursor")
	}
	if got := next.Pixels()[0:4]; got[0] != 255 || got[1] != 255 || got[2] != 255 || got[3] != 255 {
		t.Fatalf("inverting pixel over black = %v, want white", got)
	}
	if string(first.Pixels()) != string(snapshot) {
		t.Fatalf("the picture the system holds was changed: %v -> %v", snapshot, first.Pixels())
	}
}

// lastCanvasHit is the front-most hit with that identity.
func lastCanvasHit(t *testing.T, canvas *gfx.Canvas, id gfx.ID) gfx.Hit {
	t.Helper()
	hits := canvas.Hits()
	for index := len(hits) - 1; index >= 0; index-- {
		if hits[index].ID == id {
			return hits[index]
		}
	}
	t.Fatalf("the canvas has no hit %v", id)
	return gfx.Hit{}
}
