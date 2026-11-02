package workbench

import (
	"context"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/vivaui/viva/ui"
	"motty/internal/desktop"
)

// followingDesktop is the picture of a server that changes its desktop to the
// size it is asked for, as an RDP server with a display control channel does.
type followingDesktop struct {
	mu      sync.Mutex
	done    chan struct{}
	once    sync.Once
	width   int
	height  int
	shown   [2]int
	can     bool
	resizes []desktopTarget
	pointer [][2]int
}

func newFollowingDesktop(width, height int, can bool) *followingDesktop {
	return &followingDesktop{done: make(chan struct{}), width: width, height: height, can: can}
}

func (s *followingDesktop) Info() desktop.Info { return desktop.Info{Protocol: "RDP", Encrypted: true} }
func (s *followingDesktop) Size() (int, int)   { return s.width, s.height }
func (s *followingDesktop) Present(dst desktop.Presenter) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.shown == [2]int{s.width, s.height} {
		return false
	}
	s.shown = [2]int{s.width, s.height}
	dst.Resize(s.width, s.height)
	return true
}
func (s *followingDesktop) Done() <-chan struct{} { return s.done }
func (s *followingDesktop) Err() error            { return nil }
func (s *followingDesktop) SendPointer(x, y int, _ desktop.Buttons) error {
	s.mu.Lock()
	s.pointer = append(s.pointer, [2]int{x, y})
	s.mu.Unlock()
	return nil
}
func (s *followingDesktop) SendWheel(int, int, desktop.Wheel, desktop.Buttons) error { return nil }
func (s *followingDesktop) SendKey(desktop.KeyEvent) error                           { return nil }
func (s *followingDesktop) SendText(string) error                                    { return nil }
func (s *followingDesktop) SetClipboard(string) error                                { return nil }
func (s *followingDesktop) RemoteClipboard() (string, bool)                          { return "", false }
func (s *followingDesktop) Bells() int                                               { return 0 }
func (s *followingDesktop) SetActive(bool)                                           {}
func (s *followingDesktop) Close() error                                             { s.once.Do(func() { close(s.done) }); return nil }
func (s *followingDesktop) CanResize() bool                                          { return s.can }
func (s *followingDesktop) Resize(width, height int, scale float32) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resizes = append(s.resizes, desktopTarget{width: width, height: height, scale: scale})
	s.width, s.height = width, height
	return nil
}
func (s *followingDesktop) asked() []desktopTarget {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]desktopTarget(nil), s.resizes...)
}

// pictureTiles are the tiles of the remote picture among a frame's images; the
// window's own icons and backgrounds are bitmaps too.
func pictureTiles(canvas *ui.Canvas) []ui.Image {
	var tiles []ui.Image
	for _, image := range canvas.Images() {
		if image.Bitmap.ID >= 1<<61 {
			tiles = append(tiles, image)
		}
	}
	return tiles
}

// frames paints a state the way a window does: one canvas that knows the
// display scale and a clock the test moves.
type frames struct {
	state  *State
	canvas ui.Canvas
	now    time.Duration
}

func newFrames(state *State, scale float32) *frames {
	f := &frames{state: state}
	f.canvas.SetPixelScale(scale)
	f.canvas.SetClock(func() time.Duration { return f.now })
	return f
}

func (f *frames) paint(size ui.Size, at time.Duration) {
	f.now = at
	f.state.Resize(size)
	f.canvas.BeginFrame()
	f.state.Paint(&f.canvas, size)
	f.canvas.EndFrame()
}

func followState(t *testing.T, scale float32, can bool, size ui.Size) (*State, *Session, *followingDesktop, *desktopRequest, *frames) {
	t.Helper()
	_, options := newWorld(t)
	state := newTestState(t, options)
	state.options.Invalidate = func() {}
	state.connectionReconnect = reconnectOff
	var server *followingDesktop
	var asked desktopRequest
	state.dialDesktop = func(_ context.Context, request desktopRequest) (desktop.Session, error) {
		asked = request
		width, height := request.Width, request.Height
		if width == 0 {
			width, height = 1600, 900
		}
		server = newFollowingDesktop(width, height, can)
		return server, nil
	}
	screen := newFrames(state, scale)
	screen.paint(size, 0)
	eventCanvases[state] = &screen.canvas // events go through the canvas that knows the scale
	addStreamHost(state, Host{Label: "Win", Address: "win.example.com", User: "alice", Protocol: protocolRDP})
	openHost(state)
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	waitUntil(t, state, "the picture", func() bool { return session.desk != nil && session.desk.width > 0 })
	return state, session, server, &asked, screen
}

func TestRDPDesktopIsAskedForThePanesPixelsAndFollowsItOnceItSettles(t *testing.T) {
	size := ui.Size{Width: 1400, Height: 900}
	state, session, server, request, screen := followState(t, 1.25, true, size)

	area := desktopLayout(size).area
	wantWidth, wantHeight := int(math.Floor(float64(area.Width)*1.25))&^1, int(math.Floor(float64(area.Height)*1.25))
	if request.Width != wantWidth || request.Height != wantHeight || request.Scale != 1.25 {
		t.Fatalf("the server was asked for %dx%d at %v, want the pane's %dx%d pixels at 1.25", request.Width, request.Height, request.Scale, wantWidth, wantHeight)
	}

	// The picture reaches the screen as the pane's own device pixels.
	screen.paint(size, 10*time.Millisecond)
	if len(pictureTiles(&screen.canvas)) == 0 {
		t.Fatal("the picture was not painted")
	}
	covered := 0
	for _, image := range pictureTiles(&screen.canvas) {
		if !image.DevicePixels {
			t.Fatalf("a tile of a picture the size of the pane is scaled: %+v", image.Bounds)
		}
		covered += image.Bitmap.Width * image.Bitmap.Height
	}
	if covered != wantWidth*wantHeight {
		t.Fatalf("%d of %d picture pixels are drawn", covered, wantWidth*wantHeight)
	}
	if got := server.asked(); len(got) != 0 {
		t.Fatalf("a pane that did not change asked the server for %v", got)
	}

	// A press at logical (100, 50) of the picture is the remote pixel (125, 62).
	origin, scale := session.desk.place(area)
	if math.Abs(float64(scale)-0.8) > 1e-6 {
		t.Fatalf("one picture pixel is %v logical pixels, want 0.8", scale)
	}
	pointer(state, ui.PointerEvent{Kind: ui.PointerMove, X: origin.X + 100, Y: origin.Y + 50, Target: hitTerminalList.At(int(session.ID)), Viewport: size})
	waitUntil(t, state, "the pointer", func() bool { server.mu.Lock(); defer server.mu.Unlock(); return len(server.pointer) > 0 })
	if got := server.pointer[len(server.pointer)-1]; got != [2]int{125, 62} {
		t.Fatalf("the press reached the remote at %v, want 125,62", got)
	}

	// A window being dragged asks for nothing until it holds a size.
	shrunk := ui.Size{Width: 1000, Height: 700}
	screen.paint(shrunk, 1000*time.Millisecond)
	screen.paint(ui.Size{Width: 900, Height: 650}, 1100*time.Millisecond)
	screen.paint(ui.Size{Width: 800, Height: 600}, 1200*time.Millisecond)
	if got := server.asked(); len(got) != 0 {
		t.Fatalf("a pane still being dragged asked for %v", got)
	}
	if delay, ok := screen.canvas.NextFrame(); !ok || delay <= 0 || delay > desktopResizeDelay {
		t.Fatalf("no frame is wanted to end the wait: %v %v", delay, ok)
	}
	screen.paint(ui.Size{Width: 800, Height: 600}, 1200*time.Millisecond+desktopResizeDelay)
	settled := desktopLayout(ui.Size{Width: 800, Height: 600}).area
	want := desktopTarget{width: int(math.Floor(float64(settled.Width)*1.25)) &^ 1, height: int(math.Floor(float64(settled.Height) * 1.25)), scale: 1.25}
	if got := server.asked(); len(got) != 1 || got[0] != want {
		t.Fatalf("the settled pane asked for %v, want exactly %v", got, want)
	}
	screen.paint(ui.Size{Width: 800, Height: 600}, 3*time.Second)
	if got := server.asked(); len(got) != 1 {
		t.Fatalf("a settled pane asked again: %v", got)
	}

	// Moving to a display of another scale is a new desktop too.
	screen.canvas.SetPixelScale(2)
	screen.paint(ui.Size{Width: 800, Height: 600}, 4*time.Second)
	screen.paint(ui.Size{Width: 800, Height: 600}, 4*time.Second+desktopResizeDelay)
	if got := server.asked(); len(got) != 2 || got[1].scale != 2 || got[1].width != int(math.Floor(float64(settled.Width)*2))&^1 {
		t.Fatalf("a new display scale asked for %v", got)
	}
}

func TestDesktopThatCannotFollowIsScaledAndNeverAsked(t *testing.T) {
	size := ui.Size{Width: 1400, Height: 900}
	_, _, server, _, screen := followState(t, 1.25, false, size)
	screen.paint(size, 10*time.Millisecond)
	screen.paint(ui.Size{Width: 900, Height: 700}, 20*time.Millisecond)
	screen.paint(ui.Size{Width: 900, Height: 700}, time.Second)
	if got := server.asked(); len(got) != 0 {
		t.Fatalf("a server that cannot resize was asked for %v", got)
	}
	if len(pictureTiles(&screen.canvas)) == 0 {
		t.Fatal("the picture was not painted")
	}
	for _, image := range pictureTiles(&screen.canvas) {
		if image.DevicePixels {
			t.Fatal("a picture that has to be scaled was drawn as device pixels")
		}
	}
}
