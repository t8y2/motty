package workbench

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
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
	if cursor, image := view.pointerCursor(1, false); cursor != gfx.PointerCursorDefault || image != nil {
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
// No system cursor image can express that, so the workbench hides the system
// pointer over the picture and paints the shape itself, inverting the picture
// pixel by pixel. Issue 1603: the body was painted opaque black with a white
// edge, which is what the reporter saw on a dark Windows Settings page.
func TestAnInvertingRemotePointerIsPaintedAgainstThePicture(t *testing.T) {
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

	// 4x1: the black outline, two inverting body pixels, one transparent pixel.
	shape := pointerShapeOf(4, 1, 0, 0)
	shape.BGRA[3] = 255
	shape.Invert = make([]bool, 4)
	shape.Invert[1], shape.Invert[2] = true, true
	remote.say(desktop.PointerBitmap, shape)
	screen.canvas.Reset()
	screen.paint(size, time.Millisecond) // the page learns the shape as it presents
	pictureHit := hitTerminalList.At(int(session.ID))

	// A move of the pointer over the picture asks for the frame that shows it.
	area := desktopLayout(size, false).area
	origin, _ := session.desk.place(area)
	move := gfx.PointerEvent{Kind: gfx.PointerMove, X: origin.X + 300, Y: origin.Y + 200, Target: pictureHit, Viewport: size}
	if result := state.desktopPointer(session, move); !result.Repaint {
		t.Fatal("a move of an inverting pointer did not ask for a repaint")
	}
	if !session.desk.inverting || !session.desk.pointerInside {
		t.Fatalf("inverting %v, inside %v", session.desk.inverting, session.desk.pointerInside)
	}

	// Over the picture the system's pointer is hidden, because the workbench
	// paints the remote's; off it the ordinary pointer is what is left.
	if cursor, image := session.desk.pointerCursor(1, true); cursor != gfx.PointerCursorHidden || image != nil {
		t.Fatalf("over the picture: cursor %v, image %v", cursor, image)
	}
	if cursor, image := session.desk.pointerCursor(1, false); cursor != gfx.PointerCursorDefault || image != nil {
		t.Fatalf("off the picture: cursor %v, image %v", cursor, image)
	}

	screen.canvas.Reset()
	screen.canvas.SetInteraction(pictureHit, gfx.ID{}, gfx.ID{}, false)
	screen.paint(size, time.Millisecond)

	cursor := gfx.PointerCursorDefault
	for _, hit := range screen.canvas.Hits() {
		if hit.ID == pictureHit {
			cursor = hit.Cursor
		}
	}
	if cursor != gfx.PointerCursorHidden {
		t.Fatalf("the picture's cursor is %v, want hidden", cursor)
	}

	var painted gfx.Image
	found := false
	for _, image := range screen.canvas.Images() {
		if image.Bitmap.Width == 4 && image.Bitmap.Height == 1 && image.Bounds.X == origin.X+300 {
			painted, found = image, true
		}
	}
	if !found {
		t.Fatal("the inverting pointer was not painted over the picture")
	}
	pixels := painted.Bitmap.Pix
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

	// Leaving the picture takes the painted pointer with it.
	if result := state.desktopPointer(session, gfx.PointerEvent{Kind: gfx.PointerExit, Viewport: size}); !result.Repaint {
		t.Fatal("leaving the picture did not ask for a repaint")
	}
	if session.desk.pointerInside {
		t.Fatal("the pointer is still inside the picture after it left")
	}
	screen.canvas.Reset()
	screen.canvas.SetInteraction(gfx.ID{}, gfx.ID{}, gfx.ID{}, false)
	screen.paint(size, 2*time.Millisecond)
	for _, image := range screen.canvas.Images() {
		if image.Bitmap.Width == 4 && image.Bitmap.Height == 1 {
			t.Fatalf("the pointer was still painted after it left the picture: %d,%d %v", int(image.Bounds.X), int(image.Bounds.Y), image.Bitmap.ID)
		}
	}
}

// The painted inverting pointer follows the picture at any zoom: it reads the
// remote pixel under each of its own pixels, not the rectangle where it starts.
func TestThePaintedInvertingPointerSamplesThePictureUnderIt(t *testing.T) {
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

	view.inverting, view.pointerInside = true, true
	view.pixelScale = 1
	view.pointer = gfx.Point{X: 6, Y: 8}
	view.pointerShape = desktop.PointerShape{Width: 2, Height: 1, Invert: []bool{true, true},
		BGRA: make([]byte, 2*4)}

	var canvas gfx.Canvas
	canvas.SetPixelScale(1)
	view.paintPointer(&canvas, gfx.Point{}, 2, true) // one remote pixel is two page pixels
	images := canvas.Images()
	if len(images) != 1 {
		t.Fatalf("%d images painted, want the pointer", len(images))
	}
	pointer := images[0]
	if pointer.Bounds.X != 6 || pointer.Bounds.Y != 8 || pointer.Bitmap.Width != 2 || pointer.Bitmap.Height != 1 {
		t.Fatalf("pointer at %+v, %dx%d", pointer.Bounds, pointer.Bitmap.Width, pointer.Bitmap.Height)
	}
	// Its pixels are the screen's: the system cursor path is drawn the same way.
	if !pointer.DevicePixels {
		t.Fatal("the painted pointer is scaled to logical pixels")
	}
	for column := range 2 {
		got := pointer.Bitmap.Pix[column*4 : column*4+4]
		if got[0] != 245 || got[1] != 235 || got[2] != 225 || got[3] != 255 {
			t.Fatalf("pixel %d = %v, want the inverse of the remote pixel (10,20,30)", column, got)
		}
	}

	// Nothing is painted when the pointer is not over the picture, or when a
	// control under the press owns the pointer.
	canvas.Reset()
	view.pointerInside = false
	view.paintPointer(&canvas, gfx.Point{}, 2, true)
	if len(canvas.Images()) != 0 {
		t.Fatal("a pointer outside the picture was painted")
	}
	view.pointerInside = true
	view.paintPointer(&canvas, gfx.Point{}, 2, false)
	if len(canvas.Images()) != 0 {
		t.Fatal("the pointer was painted while a press belonged to another control")
	}
}

type splitPictureDesktop struct {
	*pointingDesktop
	picMu   sync.Mutex
	dirty   bool
	rightOn bool // false: left half white, right half black; true: whole picture white
}

func (s *splitPictureDesktop) Present(dst desktop.Presenter) bool {
	changed := s.followingDesktop.Present(dst)
	s.picMu.Lock()
	dirty, rightOn := s.dirty || changed, s.rightOn
	s.dirty = false
	s.picMu.Unlock()
	if !dirty {
		return changed
	}
	w, h := s.Size()
	pix := make([]byte, w*h*4)
	for y := range h {
		for x := range w {
			at := (y*w + x) * 4
			if x < w/2 || rightOn {
				pix[at], pix[at+1], pix[at+2], pix[at+3] = 255, 255, 255, 255
			} else {
				pix[at+3] = 255
			}
		}
	}
	dst.Update(0, 0, w, h, pix, w*4)
	return true
}

func TestInvertingPointerUpdatesRenderedPixelsAcrossMovesAndSurfaceChanges(t *testing.T) {
	for _, scale := range []float64{1, 2} {
		t.Run(fmt.Sprintf("scale-%g", scale), func(t *testing.T) {
			_, options := newWorld(t)
			state := newTestState(t, options)
			var invalidator app.Invalidator
			state.options.Invalidate = invalidator.Invalidate
			state.connectionReconnect = reconnectOff
			var remote *splitPictureDesktop
			state.dialDesktop = func(_ context.Context, request desktopRequest) (desktop.Session, error) {
				width, height := request.Width, request.Height
				if width == 0 {
					width, height = 1000, 700
				}
				remote = &splitPictureDesktop{
					pointingDesktop: &pointingDesktop{followingDesktop: newFollowingDesktop(width, height, false)},
					dirty:           true,
				}
				return remote, nil
			}

			view, err := headless.Open(state.WindowConfig(app.Config{
				Title: "Inverting pointer", Width: 1000, Height: 700,
				Invalidator: &invalidator,
				WindowMode:  app.WindowCustomTitlebar, TitlebarHeight: TitlebarHeight,
				Tree: Tree(), Paint: state.Paint, OnWake: state.Wake, OnResize: state.Resize,
				OnPointer: state.Pointer, OnKey: state.Key, OnShortcut: state.Shortcut, OnScroll: state.Scroll,
			}), nil, headless.Options{Scale: scale})
			if err != nil {
				t.Fatal(err)
			}
			defer view.Close()
			if err := view.Rest(); err != nil {
				t.Fatal(err)
			}

			addStreamHost(state, Host{Label: "Win", Address: "win.example.com", User: "alice", Protocol: protocolRDP})
			openHost(state)
			session, _ := state.sessionByIDPointer(state.activeSessionID)
			waitUntil(t, state, "the picture", func() bool { return session.desk != nil && session.desk.width > 0 })

			shape := pointerShapeOf(8, 8, 0, 0)
			shape.Invert = make([]bool, 8*8)
			for i := range shape.Invert {
				shape.Invert[i] = true
			}
			remote.say(desktop.PointerBitmap, shape)
			state.Wake()
			if err := view.Frame(); err != nil {
				t.Fatal(err)
			}
			if err := view.Rest(); err != nil {
				t.Fatal(err)
			}

			area := session.desk.area
			origin, picScale := session.desk.place(area)
			leftX, leftY := origin.X+float32(session.desk.width)*picScale*0.25, origin.Y+float32(session.desk.height)*picScale*0.5
			if err := view.Move(leftX, leftY); err != nil {
				t.Fatal(err)
			}
			img, err := view.Image()
			if err != nil {
				// The offscreen renderer exists only in some builds; a machine
				// without one skips the pixel check rather than failing the suite.
				t.Skipf("no offscreen renderer on this machine: %v", err)
			}
			px := func(lx, ly float32) (int, int) {
				return int(lx*float32(scale)) + 4*int(scale), int(ly*float32(scale)) + 4*int(scale)
			}
			gx, gy := px(leftX, leftY)
			gotLeft := img.RGBAAt(gx, gy)
			if gotLeft.R > 20 || gotLeft.G > 20 || gotLeft.B > 20 {
				t.Fatalf("over white half: pointer pixel = %v, want near black", gotLeft)
			}
			firstBitmap := session.desk.pointerBitmapVersion
			firstID := session.desk.pointerBitmapID
			if firstID == 0 || firstID >= 1<<61 || firstBitmap == 0 {
				t.Fatalf("pointer bitmap identity = (%d, v%d), want non-zero < 1<<61", firstID, firstBitmap)
			}

			rightX, rightY := origin.X+float32(session.desk.width)*picScale*0.75, origin.Y+float32(session.desk.height)*picScale*0.5
			if err := view.Move(rightX, rightY); err != nil {
				t.Fatal(err)
			}
			img, err = view.Image()
			if err != nil {
				t.Fatal(err)
			}
			gx, gy = px(rightX, rightY)
			gotRight := img.RGBAAt(gx, gy)
			if gotRight.R < 235 || gotRight.G < 235 || gotRight.B < 235 {
				t.Fatalf("over black half after move: pointer pixel = %v, want near white", gotRight)
			}
			if session.desk.pointerBitmapVersion <= firstBitmap {
				t.Fatalf("pointer bitmap version did not advance after pixels changed: %d -> %d", firstBitmap, session.desk.pointerBitmapVersion)
			}
			secondBitmap := session.desk.pointerBitmapVersion

			// Keep the pointer stationary while the surface underneath turns white.
			remote.picMu.Lock()
			remote.rightOn = true
			remote.dirty = true
			remote.picMu.Unlock()
			state.Wake()
			if err := view.Frame(); err != nil {
				t.Fatal(err)
			}
			if err := view.Rest(); err != nil {
				t.Fatal(err)
			}
			img, err = view.Image()
			if err != nil {
				t.Fatal(err)
			}
			gotUpdatedBg := img.RGBAAt(int(rightX*float32(scale))-10, int(rightY*float32(scale))-10)
			gotUpdated := img.RGBAAt(gx, gy)
			if gotUpdatedBg.R < 235 || gotUpdatedBg.G < 235 || gotUpdatedBg.B < 235 {
				t.Fatalf("surface background under stationary pointer did not turn white: %v", gotUpdatedBg)
			}
			if gotUpdated.R > 20 || gotUpdated.G > 20 || gotUpdated.B > 20 {
				t.Fatalf("after surface under stationary pointer turned white: pointer pixel = %v (bg=%v), want near black", gotUpdated, gotUpdatedBg)
			}
			if session.desk.pointerBitmapVersion <= secondBitmap {
				t.Fatalf("pointer bitmap version did not advance after surface changed: %d -> %d", secondBitmap, session.desk.pointerBitmapVersion)
			}
		})
	}
}

func TestPaintedPointerKeepsPreviousBitmapImmutableAndAdvancesVersionOnChange(t *testing.T) {
	var view desktopView
	view.surface.Resize(10, 10)
	picture := make([]byte, 10*10*4)
	for y := range 10 {
		for x := range 10 {
			at := (y*10 + x) * 4
			if x < 5 {
				picture[at], picture[at+1], picture[at+2] = 255, 255, 255
			}
			picture[at+3] = 255
		}
	}
	view.surface.Update(0, 0, 10, 10, picture, 10*4)
	view.inverting, view.pointerInside = true, true
	view.pointerShape = desktop.PointerShape{
		Width: 2, Height: 1,
		// Pixel 0: semi-transparent non-inverting blue (straight alpha=128) to verify premultiplication.
		// Pixel 1: inverting pixel.
		BGRA:   []byte{255, 0, 0, 128, 0, 0, 0, 0},
		Invert: []bool{false, true},
	}

	var canvas gfx.Canvas
	canvas.SetPixelScale(1)

	// Frame 1: over white half -> inverting pixel is black.
	view.pointer = gfx.Point{X: 1, Y: 2}
	view.paintPointer(&canvas, gfx.Point{}, 1, true)
	first := canvas.Images()[0].Bitmap
	if first.ID == 0 || first.Version != 1 {
		t.Fatalf("first bitmap identity = (%d, v%d), want non-zero v1", first.ID, first.Version)
	}
	if first.Pix[0] != 128 || first.Pix[1] != 0 || first.Pix[2] != 0 || first.Pix[3] != 128 {
		t.Fatalf("non-inverting premultiplied pixel = %v, want [128 0 0 128]", first.Pix[:4])
	}
	if first.Pix[4] != 0 || first.Pix[5] != 0 || first.Pix[6] != 0 || first.Pix[7] != 255 {
		t.Fatalf("first inverting pixel = %v, want black", first.Pix[4:8])
	}
	snapshot := append([]byte(nil), first.Pix...)

	// Frame 2: moved within white half -> identical pixels keep the same version and buffer.
	canvas.Reset()
	view.pointer = gfx.Point{X: 2, Y: 2}
	view.paintPointer(&canvas, gfx.Point{}, 1, true)
	same := canvas.Images()[0].Bitmap
	if same.ID != first.ID || same.Version != first.Version || &same.Pix[0] != &first.Pix[0] {
		t.Fatalf("unchanged pixels changed bitmap identity/version: (%d, v%d) -> (%d, v%d)", first.ID, first.Version, same.ID, same.Version)
	}

	// Frame 3: moved onto black half -> new buffer & bumped version; first.Pix remains untouched.
	canvas.Reset()
	view.pointer = gfx.Point{X: 7, Y: 2}
	view.paintPointer(&canvas, gfx.Point{}, 1, true)
	second := canvas.Images()[0].Bitmap
	if second.ID != first.ID || second.Version != 2 {
		t.Fatalf("second bitmap identity = (%d, v%d), want (%d, v2)", second.ID, second.Version, first.ID)
	}
	if &second.Pix[0] == &first.Pix[0] {
		t.Fatal("changed pointer reused the previous frame's backing slice in place")
	}
	if string(first.Pix) != string(snapshot) {
		t.Fatalf("previous frame's Pix was mutated: %v -> %v", snapshot, first.Pix)
	}
	if second.Pix[4] != 255 || second.Pix[5] != 255 || second.Pix[6] != 255 || second.Pix[7] != 255 {
		t.Fatalf("second inverting pixel = %v, want white", second.Pix[4:8])
	}
}
