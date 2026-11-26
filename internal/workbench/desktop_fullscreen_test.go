package workbench

import (
	"errors"
	"image/png"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"motty/internal/desktop"
)

func desktopDriver(t *testing.T, state *State) *app.Driver {
	t.Helper()
	var invalidator app.Invalidator
	state.options.Invalidate = invalidator.Invalidate
	canvas := &gfx.Canvas{}
	driver, err := app.NewDriver(state.WindowConfig(app.Config{
		Title: "Remote desktop", Width: 1400, Height: 900,
		Invalidator: &invalidator,
		WindowMode:  app.WindowCustomTitlebar, TitlebarHeight: TitlebarHeight,
		Paint: state.Paint, OnWake: state.Wake, OnResize: state.Resize,
		OnKey: state.Key, OnShortcut: state.Shortcut, OnPointer: state.Pointer,
		OnFullscreen: state.SetWindowFullscreen,
		TextInput: func(context app.TextInputContext) *app.TextInput {
			return state.TerminalInput(context)
		},
	}), canvas)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(driver.Close)
	state.SetFullscreenControl(driver.SetFullscreen)
	eventCanvases[state] = canvas
	return driver
}

func repaintDesktopDriver(driver *app.Driver, state *State) error {
	state.options.Invalidate()
	return driver.Pump()
}

func TestNativeWindowFullscreenKeepsTheDesktopWorkspace(t *testing.T) {
	fixture := startVNCFixture(t, "secret")
	state, session := vncState(t, fixture)
	connectDesktop(t, state, session, "secret")
	driver := desktopDriver(t, state)
	if err := driver.SetFullscreen(true); err != nil {
		t.Fatal(err)
	}
	if !state.winChrome.fullscreen || state.winChrome.fullscreenSession != 0 || session.desk.fullscreen {
		t.Fatal("native fullscreen entered the remote desktop's fullscreen mode")
	}
	if _, ok := hitByIDInSnapshot(driver.Snapshot(), hitDesktopFit); !ok {
		t.Fatal("native fullscreen hid the desktop toolbar")
	}
	if err := driver.SetFullscreen(false); err != nil {
		t.Fatal(err)
	}
	if state.winChrome.fullscreen || state.winChrome.fullscreenSession != 0 || session.desk.fullscreen {
		t.Fatal("exiting native fullscreen left the desktop in fullscreen mode")
	}
}

func TestDesktopFullscreenOverLoopbackPreservesInputAndRestoresChrome(t *testing.T) {
	fixture := startVNCFixture(t, "secret")
	state, session := vncState(t, fixture)
	connectDesktop(t, state, session, "secret")
	driver := desktopDriver(t, state)
	button, ok := hitByIDInSnapshot(driver.Snapshot(), hitDesktopFullscreen)
	if !ok {
		t.Fatal("no fullscreen button")
	}
	if err := driver.Click(button.Bounds.X+button.Bounds.Width/2, button.Bounds.Y+button.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	snapshot := driver.Snapshot()
	if !driver.Fullscreen() || !session.desk.fullscreen {
		t.Fatal("the fullscreen action did not reach the window")
	}
	var area gfx.Rect
	exit := false
	for _, hit := range snapshot.Hits {
		if hit.ID == hitTerminalList.At(int(session.ID)) {
			area = hit.Bounds
		}
		if hit.ID == hitDesktopFullscreen {
			exit = true
		}
		if hit.ID == hitDesktopFit || hit.ID.Scope() == hitTabList {
			t.Fatalf("fullscreen kept workspace chrome: %+v", hit)
		}
	}
	if area.X != 0 || area.Y != 0 || area.Width != state.viewport.Width || area.Height != state.viewport.Height || !exit {
		t.Fatalf("picture bounds %+v in %+v; exit=%v", area, state.viewport, exit)
	}
	if snapshot.Focused != hitTerminalList.At(int(session.ID)) {
		t.Fatalf("fullscreen keyboard focus = %v, want desktop %v", snapshot.Focused, hitTerminalList.At(int(session.ID)))
	}
	before := len(keySyms(fixture.got()))
	if err := driver.KeyDown(gfx.KeyEnter, ""); err != nil {
		t.Fatal(err)
	}
	if err := driver.KeyUp(gfx.KeyEnter, ""); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, state, "fullscreen key over loopback", func() bool { return len(keySyms(fixture.got())) >= before+2 })
	remoteKeys := keySyms(fixture.got())[before:]
	if strings.Join(remoteKeys, ",") != "down ff0d,up ff0d" {
		t.Fatalf("fullscreen keys = %v", remoteKeys)
	}
	if err := driver.Key(gfx.KeyEnter, gfx.ModifierControl|gfx.ModifierAlt); err != nil {
		t.Fatal(err)
	}
	if driver.Fullscreen() || session.desk.fullscreen {
		t.Fatal("Ctrl+Alt+Enter did not exit fullscreen")
	}
	if _, ok := hitByIDInSnapshot(driver.Snapshot(), hitDesktopFit); !ok {
		t.Fatal("exiting fullscreen did not restore the desktop toolbar")
	}
	if !session.desk.fit || session.desk.chromeless {
		t.Fatal("fullscreen changed the desktop's saved presentation choices")
	}
}

func hitByIDInSnapshot(snapshot app.Snapshot, id gfx.ID) (gfx.Hit, bool) {
	for _, hit := range snapshot.Hits {
		if hit.ID == id {
			return hit, true
		}
	}
	return gfx.Hit{}, false
}

func TestRDPFullscreenFollowsMonitorPixelsAndLeavesForDialogs(t *testing.T) {
	state, session, server, _, _ := followState(t, 1, true, gfx.Size{Width: 1400, Height: 900})
	driver := desktopDriver(t, state)
	state.toggleDesktopFullscreen(session)
	if err := repaintDesktopDriver(driver, state); err != nil {
		t.Fatal(err)
	}
	if err := driver.Advance(desktopResizeDelay + time.Millisecond); err != nil {
		t.Fatal(err)
	}
	wanted := desktopTargetFor(gfx.Rect{Width: state.viewport.Width, Height: state.viewport.Height}, state.pixelScale)
	resizes := server.asked()
	if len(resizes) == 0 || resizes[len(resizes)-1] != wanted {
		t.Fatalf("fullscreen resized to %v, want %+v", resizes, wanted)
	}
	state.modal = modalPassword
	state.Wake()
	if driver.Fullscreen() || state.winChrome.fullscreenSession != 0 {
		t.Fatal("a Motty dialog was hidden behind fullscreen")
	}
}

func TestDesktopFullscreenFailureHasVisibleFeedbackAndDoesNotChangeLayout(t *testing.T) {
	state, session, _, _, _ := followState(t, 1, false, gfx.Size{Width: 1400, Height: 900})
	state.SetFullscreenControl(func(bool) error { return errors.New("window manager refused") })
	state.toggleDesktopFullscreen(session)
	if state.winChrome.fullscreenSession != 0 || session.desk.fullscreen || len(state.fileNotices.items) != 1 {
		t.Fatalf("failed fullscreen: chrome=%+v view=%v notices=%v", state.winChrome, session.desk.fullscreen, state.fileNotices.items)
	}
}

type measuredDesktop struct {
	*followingDesktop
	rtt     time.Duration
	sampled time.Time
}

func (s *measuredDesktop) NetworkRTT() (time.Duration, time.Time) { return s.rtt, s.sampled }

var _ desktop.LatencySource = (*measuredDesktop)(nil)

func TestRDPLatencyShowsActualSampleOrExplicitlyUnavailableInEveryPresentation(t *testing.T) {
	state, session, server, _, screen := followState(t, 1, false, gfx.Size{Width: 1400, Height: 900})
	measured := &measuredDesktop{followingDesktop: server}
	session.desk.session = measured
	state.terminalShowLatency = true
	driver := desktopDriver(t, state)
	label, _, hint := state.desktopLatency(session.desk)
	if label != "RTT —" || hint != state.messages.Desktop.LatencyUnavailable {
		t.Fatalf("unmeasured RTT: %q / %q", label, hint)
	}

	measured.rtt, measured.sampled = 137*time.Millisecond, time.Now()
	for _, mode := range []string{"toolbar", "picture-only", "fullscreen"} {
		session.desk.chromeless = mode == "picture-only"
		if mode == "fullscreen" {
			state.toggleDesktopFullscreen(session)
		}
		if err := repaintDesktopDriver(driver, state); err != nil {
			t.Fatal(err)
		}
		label, color, hint := state.desktopLatency(session.desk)
		if label != "RTT 137ms" || color != state.pal.warning || !strings.Contains(hint, measured.sampled.Local().Format("15:04:05")) {
			t.Fatalf("%s measurement: %q / %v / %q", mode, label, color, hint)
		}
		if _, ok := hitByIDInSnapshot(driver.Snapshot(), hitDesktopLatency); !ok {
			t.Fatalf("%s has no latency badge", mode)
		}
	}
	state.toggleDesktopFullscreen(session)
	session.desk.chromeless = false
	rightClickTab(state, hitTabList.At(int(state.deck.ActiveWorkspace())))
	screen.paint(gfx.Size{Width: 1400, Height: 900}, time.Second)
	chooseMenu(t, state, hitTabMenuLatency)
	if state.terminalShowLatency {
		t.Fatal("the desktop menu did not disable RTT display")
	}
	if err := repaintDesktopDriver(driver, state); err != nil {
		t.Fatal(err)
	}
	if _, ok := hitByIDInSnapshot(driver.Snapshot(), hitDesktopLatency); ok {
		t.Fatal("disabled RTT still has a badge")
	}
}

func TestLiveRDPFullscreenUsesTheRealSession(t *testing.T) {
	address := os.Getenv("MOTTY_RDP_ADDR")
	if address == "" {
		t.Skip("MOTTY_RDP_ADDR not set")
	}
	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.ParseUint(portText, 10, 16)
	if err != nil {
		t.Fatal(err)
	}
	_, options := newWorld(t)
	state := newTestState(t, options)
	state.connectionReconnect = reconnectOff
	driver := desktopDriver(t, state)
	account := os.Getenv("MOTTY_RDP_USER")
	if domain := os.Getenv("MOTTY_RDP_DOMAIN"); domain != "" {
		account = domain + `\` + account
	}
	addStreamHost(state, Host{Label: "Live RDP", Address: host, Port: uint16(port), User: account, Protocol: protocolRDP})
	openHost(state)
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	waitUntil(t, state, "RDP certificate question", func() bool { return state.modal == modalHostKey })
	state.Click(hitHostKeyOnce)
	waitUntil(t, state, "RDP password question", func() bool { return state.modal == modalPassword })
	state.passwordFields[0].Insert(os.Getenv("MOTTY_RDP_PASSWORD"))
	state.submitPassword()
	waitUntil(t, state, "RDP picture", func() bool { return session.desk != nil && session.desk.width > 0 })
	state.terminalShowLatency = true
	if err := repaintDesktopDriver(driver, state); err != nil {
		t.Fatal(err)
	}
	state.toggleDesktopFullscreen(session)
	if err := repaintDesktopDriver(driver, state); err != nil {
		t.Fatal(err)
	}
	if err := driver.Advance(desktopResizeDelay + time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if state.fullscreenDesktop() != session || len(pictureTiles(eventCanvases[state])) == 0 {
		t.Fatal("the real RDP session was not painted fullscreen")
	}
	if _, ok := hitByIDInSnapshot(driver.Snapshot(), hitDesktopLatency); !ok {
		t.Fatal("the real RDP session has no RTT badge or unavailable indicator")
	}
	directory := os.Getenv("MOTTY_DESKTOP_SCREENSHOT_DIR")
	if directory == "" {
		directory = t.TempDir()
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	preview, err := headless.Open(app.Config{
		Title: "RDP fullscreen", Width: int(state.viewport.Width), Height: int(state.viewport.Height),
		WindowMode: app.WindowCustomTitlebar, Paint: state.Paint,
	}, nil, headless.Options{Scale: float64(state.pixelScale)})
	if err != nil {
		t.Fatal(err)
	}
	defer preview.Close()
	if err := preview.Driver().SetFullscreen(true); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "rdp-fullscreen.png")
	deadline := time.Now().Add(40 * time.Second)
	for {
		state.Wake()
		if err := preview.Driver().Resize(int(state.viewport.Width), int(state.viewport.Height)); err != nil {
			t.Fatal(err)
		}
		if err := preview.Driver().Advance(desktopResizeDelay + time.Millisecond); err != nil {
			t.Fatal(err)
		}
		if err := preview.Screenshot(path); err != nil {
			t.Fatal(err)
		}
		file, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		image, decodeErr := png.Decode(file)
		file.Close()
		if decodeErr != nil {
			t.Fatal(decodeErr)
		}
		colors := make(map[[3]uint32]struct{})
		bounds := image.Bounds()
		for y := bounds.Min.Y + 100; y < bounds.Max.Y && len(colors) < 16; y += 2 {
			for x := bounds.Min.X; x < bounds.Max.X && len(colors) < 16; x += 2 {
				r, g, b, _ := image.At(x, y).RGBA()
				colors[[3]uint32{r, g, b}] = struct{}{}
			}
		}
		target := desktopTargetFor(session.desk.area, session.desk.pixelScale)
		resized := !session.desk.pixelPerfect() || session.desk.width == target.width && session.desk.height == target.height
		if len(colors) >= 16 && resized {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("RDP fullscreen did not render and resize: picture %dx%d, target %dx%d, canResize=%v",
				session.desk.width, session.desk.height, target.width, target.height, session.desk.pixelPerfect())
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err := driver.KeyDown(gfx.KeyEnter, ""); err != nil {
		t.Fatal(err)
	}
	if err := driver.KeyUp(gfx.KeyEnter, ""); err != nil {
		t.Fatal(err)
	}
	state.toggleDesktopFullscreen(session)
	if err := repaintDesktopDriver(driver, state); err != nil {
		t.Fatal(err)
	}
	if driver.Fullscreen() || session.desk.fullscreen {
		t.Fatal("the real RDP session did not leave fullscreen")
	}
	t.Logf("live RDP picture %dx%d, RTT source=%T (server-dependent)", session.desk.width, session.desk.height, session.desk.session)
}

func TestDesktopFullscreenBarIsCentredAtTheTopEdge(t *testing.T) {
	fixture := startVNCFixture(t, "secret")
	state, session := vncState(t, fixture)
	connectDesktop(t, state, session, "secret")
	driver := desktopDriver(t, state)
	button, ok := hitByIDInSnapshot(driver.Snapshot(), hitDesktopFullscreen)
	if !ok {
		t.Fatal("no fullscreen button")
	}
	if err := driver.Click(button.Bounds.X+button.Bounds.Width/2, button.Bounds.Y+button.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	snapshot := driver.Snapshot()
	var left, right float32
	first := true
	for _, id := range []gfx.ID{hitDesktopBarPin, hitDesktopFullscreen, hitDesktopCapture, hitDesktopBarDock} {
		hit, ok := hitByIDInSnapshot(snapshot, id)
		if !ok {
			t.Fatalf("fullscreen bar has no %v", id)
		}
		if first || hit.Bounds.X < left {
			left, first = hit.Bounds.X, false
		}
		if end := hit.Bounds.X + hit.Bounds.Width; end > right {
			right = end
		}
	}
	centre, want := (left+right)/2, state.viewport.Width/2
	if centre < want-1 || centre > want+1 {
		t.Fatalf("fullscreen bar centre = %g, want %g (bar %g..%g of %g)", centre, want, left, right, state.viewport.Width)
	}
}

func TestUnpinnedDesktopFullscreenBarHidesAndReturnsAtTheTopEdge(t *testing.T) {
	fixture := startVNCFixture(t, "secret")
	state, session := vncState(t, fixture)
	connectDesktop(t, state, session, "secret")
	driver := desktopDriver(t, state)
	button, ok := hitByIDInSnapshot(driver.Snapshot(), hitDesktopFullscreen)
	if !ok {
		t.Fatal("no fullscreen button")
	}
	if err := driver.Click(button.Bounds.X+button.Bounds.Width/2, button.Bounds.Y+button.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	pin, ok := hitByIDInSnapshot(driver.Snapshot(), hitDesktopBarPin)
	if !ok {
		t.Fatal("the pinned fullscreen bar has no pin")
	}
	if err := driver.Click(pin.Bounds.X+pin.Bounds.Width/2, pin.Bounds.Y+pin.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	if !state.desktopBar.autoHide || !state.desktopBar.over {
		t.Fatalf("unpinning: autoHide=%v over=%v", state.desktopBar.autoHide, state.desktopBar.over)
	}
	move := func(x, y float32) {
		state.Pointer(gfx.PointerEvent{Kind: gfx.PointerMove, X: x, Y: y, Viewport: state.viewport})
		if err := repaintDesktopDriver(driver, state); err != nil {
			t.Fatal(err)
		}
	}
	move(state.viewport.Width/2, state.viewport.Height/2)
	if _, ok := hitByIDInSnapshot(driver.Snapshot(), hitDesktopBarPin); ok {
		t.Fatal("the unpinned bar stayed visible away from the top edge")
	}
	move(state.viewport.Width/2, 2)
	if _, ok := hitByIDInSnapshot(driver.Snapshot(), hitDesktopBarPin); !ok {
		t.Fatal("the unpinned bar did not return at the top edge")
	}
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerExit})
	if err := repaintDesktopDriver(driver, state); err != nil {
		t.Fatal(err)
	}
	if _, ok := hitByIDInSnapshot(driver.Snapshot(), hitDesktopBarPin); ok {
		t.Fatal("the unpinned bar stayed after the pointer left the window")
	}
	if err := driver.Key(gfx.KeyEnter, gfx.ModifierControl|gfx.ModifierAlt); err != nil {
		t.Fatal(err)
	}
	if driver.Fullscreen() || session.desk.fullscreen {
		t.Fatal("a hidden bar made the exit chord unreachable")
	}
}

func TestDesktopFullscreenBarPinSurvivesARestart(t *testing.T) {
	state := newTestState(t, testOptions(t))
	if !state.setDesktopFullscreenBarAutoHide(true) {
		t.Fatal("the pin did not change")
	}
	if saved := state.snapshot(); !saved.FullscreenBarAutoHide {
		t.Fatal("the unpinned choice was not saved")
	}
	restored := newTestState(t, testOptions(t))
	restored.restoreShared(state.snapshot())
	if !restored.desktopBar.autoHide {
		t.Fatal("the unpinned choice was not restored")
	}
}

// openFullscreenDesktop opens a loopback desktop and enters its fullscreen page.
func openFullscreenDesktop(t *testing.T) (*State, *Session, *app.Driver) {
	t.Helper()
	fixture := startVNCFixture(t, "secret")
	state, session := vncState(t, fixture)
	connectDesktop(t, state, session, "secret")
	driver := desktopDriver(t, state)
	button, ok := hitByIDInSnapshot(driver.Snapshot(), hitDesktopFullscreen)
	if !ok {
		t.Fatal("no fullscreen button")
	}
	if err := driver.Click(button.Bounds.X+button.Bounds.Width/2, button.Bounds.Y+button.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	return state, session, driver
}

// moveDesktopPointer moves the pointer and paints what follows, as the window
// does between events.
func moveDesktopPointer(t *testing.T, driver *app.Driver, state *State, x, y float32) {
	t.Helper()
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerMove, X: x, Y: y, Viewport: state.viewport})
	if err := repaintDesktopDriver(driver, state); err != nil {
		t.Fatal(err)
	}
}

// unpinDesktopBar clicks the fullscreen bar's pin so the bar auto-hides.
func unpinDesktopBar(t *testing.T, driver *app.Driver, state *State) {
	t.Helper()
	pin, ok := hitByIDInSnapshot(driver.Snapshot(), hitDesktopBarPin)
	if !ok {
		t.Fatal("no pin in the fullscreen bar")
	}
	if err := driver.Click(pin.Bounds.X+pin.Bounds.Width/2, pin.Bounds.Y+pin.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	if !state.desktopBar.autoHide {
		t.Fatal("the bar did not unpin")
	}
}

// barShown reports whether the next paint declares the fullscreen bar.
func barShown(t *testing.T, driver *app.Driver) bool {
	t.Helper()
	_, ok := hitByIDInSnapshot(driver.Snapshot(), hitDesktopBarPin)
	return ok
}

// snapshotHasMenuRow reports whether the painted menu has a row named label.
func snapshotHasMenuRow(snapshot app.Snapshot, label string) bool {
	for _, hit := range snapshot.Hits {
		if hit.Role == gfx.RoleMenuItem && hit.Label == label {
			return true
		}
	}
	return false
}

// TestUnpinnedFullscreenBarRevealsOnlyFromTheEdgeStrip is the guard for issue
// 1591: the reveal must be the thin strip at the edge, not the whole band the
// bar and its slack cover. Movement over the picture just inside that band used
// to bring the bar up over the remote content there.
func TestUnpinnedFullscreenBarRevealsOnlyFromTheEdgeStrip(t *testing.T) {
	state, _, driver := openFullscreenDesktop(t)
	unpinDesktopBar(t, driver, state)
	width, height := state.viewport.Width, state.viewport.Height
	moveDesktopPointer(t, driver, state, width/2, height/2)
	if barShown(t, driver) {
		t.Fatal("the unpinned bar stayed visible away from the edge")
	}
	bar := state.desktopBar.bar
	oldBand := bar.Y + bar.Height + fullscreenBarRevealSlack
	if desktopBarRevealDepth >= oldBand {
		t.Fatalf("the edge strip %g reaches the old band %g", desktopBarRevealDepth, oldBand)
	}
	moveDesktopPointer(t, driver, state, width/2, (desktopBarRevealDepth+oldBand)/2)
	if barShown(t, driver) {
		t.Fatal("the bar came up from inside the picture, not from the edge")
	}
	moveDesktopPointer(t, driver, state, width/2, desktopBarRevealDepth/2)
	if !barShown(t, driver) {
		t.Fatal("the bar did not return at the edge strip")
	}
	moveDesktopPointer(t, driver, state, width/2, bar.Y+bar.Height/2)
	if !barShown(t, driver) {
		t.Fatal("the bar hid while the pointer was on it")
	}
	moveDesktopPointer(t, driver, state, width/2, bar.Y+bar.Height+fullscreenBarRevealSlack+1)
	if barShown(t, driver) {
		t.Fatal("the bar stayed after the pointer left the bar")
	}
}

// TestDesktopFullscreenBarDocksToEachEdge checks the bar hugs the chosen edge,
// stays centred on the other axis, and reveals only from that edge's strip.
func TestDesktopFullscreenBarDocksToEachEdge(t *testing.T) {
	state, session, driver := openFullscreenDesktop(t)
	unpinDesktopBar(t, driver, state)
	area := session.desk.area
	centreX, centreY := area.X+area.Width/2, area.Y+area.Height/2
	closeTo := func(got, want float32) bool { return got >= want-1 && got <= want+1 }
	for _, tc := range []struct {
		name   string
		dock   desktopBarDock
		reveal gfx.Point
	}{
		{"top", desktopBarDockTop, gfx.Point{X: centreX, Y: area.Y + 1}},
		{"bottom", desktopBarDockBottom, gfx.Point{X: centreX, Y: area.Y + area.Height - 1}},
		{"left", desktopBarDockLeft, gfx.Point{X: area.X + 1, Y: centreY}},
		{"right", desktopBarDockRight, gfx.Point{X: area.X + area.Width - 1, Y: centreY}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state.setDesktopFullscreenBarDock(tc.dock)
			if err := repaintDesktopDriver(driver, state); err != nil {
				t.Fatal(err)
			}
			moveDesktopPointer(t, driver, state, centreX, centreY)
			if barShown(t, driver) {
				t.Fatal("the bar stayed visible away from the edge")
			}
			bar, zone := state.desktopBar.bar, state.desktopBar.zone
			switch tc.dock {
			case desktopBarDockTop:
				if bar.Y != area.Y+desktopBarEdgeMargin || !closeTo(bar.X+bar.Width/2, centreX) {
					t.Fatalf("top dock bar %+v in %+v", bar, area)
				}
				if zone != (gfx.Rect{X: area.X, Y: area.Y, Width: area.Width, Height: desktopBarRevealDepth}) {
					t.Fatalf("top dock strip %+v", zone)
				}
			case desktopBarDockBottom:
				if bar.Y+bar.Height != area.Y+area.Height-desktopBarEdgeMargin || !closeTo(bar.X+bar.Width/2, centreX) {
					t.Fatalf("bottom dock bar %+v in %+v", bar, area)
				}
				if zone != (gfx.Rect{X: area.X, Y: area.Y + area.Height - desktopBarRevealDepth, Width: area.Width, Height: desktopBarRevealDepth}) {
					t.Fatalf("bottom dock strip %+v", zone)
				}
			case desktopBarDockLeft:
				if bar.X != area.X+desktopBarEdgeMargin || !closeTo(bar.Y+bar.Height/2, centreY) {
					t.Fatalf("left dock bar %+v in %+v", bar, area)
				}
				if zone != (gfx.Rect{X: area.X, Y: area.Y, Width: desktopBarRevealDepth, Height: area.Height}) {
					t.Fatalf("left dock strip %+v", zone)
				}
			case desktopBarDockRight:
				if bar.X+bar.Width != area.X+area.Width-desktopBarEdgeMargin || !closeTo(bar.Y+bar.Height/2, centreY) {
					t.Fatalf("right dock bar %+v in %+v", bar, area)
				}
				if zone != (gfx.Rect{X: area.X + area.Width - desktopBarRevealDepth, Y: area.Y, Width: desktopBarRevealDepth, Height: area.Height}) {
					t.Fatalf("right dock strip %+v", zone)
				}
			}
			if _, ok := hitByIDInSnapshot(driver.Snapshot(), hitDesktopBarDock); ok {
				t.Fatal("the hidden bar still declares its dock button")
			}
			// The bar's own rect does not reveal it while it is hidden.
			moveDesktopPointer(t, driver, state, bar.X+bar.Width/2, bar.Y+bar.Height/2)
			if barShown(t, driver) {
				t.Fatal("the bar came up from its own rectangle")
			}
			moveDesktopPointer(t, driver, state, tc.reveal.X, tc.reveal.Y)
			if !barShown(t, driver) {
				t.Fatal("the bar did not return at the docked edge")
			}
			if _, ok := hitByIDInSnapshot(driver.Snapshot(), hitDesktopBarDock); !ok {
				t.Fatal("the placed bar has no dock button")
			}
			// Along the strip, away from the bar, it stays out.
			stripAway := gfx.Point{X: area.X + 1, Y: area.Y + 1}
			if tc.dock == desktopBarDockBottom {
				stripAway.Y = area.Y + area.Height - 1
			}
			if tc.dock == desktopBarDockRight {
				stripAway.X = area.X + area.Width - 1
			}
			moveDesktopPointer(t, driver, state, stripAway.X, stripAway.Y)
			if !barShown(t, driver) {
				t.Fatal("the bar hid while the pointer was still in the strip")
			}
			var away gfx.Point
			switch tc.dock {
			case desktopBarDockBottom:
				away = gfx.Point{X: centreX, Y: bar.Y - fullscreenBarRevealSlack - 1}
			case desktopBarDockLeft:
				away = gfx.Point{X: bar.X + bar.Width + fullscreenBarRevealSlack + 1, Y: centreY}
			case desktopBarDockRight:
				away = gfx.Point{X: bar.X - fullscreenBarRevealSlack - 1, Y: centreY}
			default:
				away = gfx.Point{X: centreX, Y: bar.Y + bar.Height + fullscreenBarRevealSlack + 1}
			}
			moveDesktopPointer(t, driver, state, away.X, away.Y)
			if barShown(t, driver) {
				t.Fatal("the bar stayed after the pointer left it")
			}
		})
	}
}

// TestDesktopFullscreenBarDockMenuChoosesAnEdge drives the dock menu: the four
// rows it offers, the choice it applies, and the keyboard returning to the
// button it hangs from.
func TestDesktopFullscreenBarDockMenuChoosesAnEdge(t *testing.T) {
	state, session, driver := openFullscreenDesktop(t)
	m := state.messages.Desktop
	openMenu := func() {
		t.Helper()
		dockHit, ok := hitByIDInSnapshot(driver.Snapshot(), hitDesktopBarDock)
		if !ok {
			t.Fatal("no dock button in the fullscreen bar")
		}
		if err := driver.Click(dockHit.Bounds.X+dockHit.Bounds.Width/2, dockHit.Bounds.Y+dockHit.Bounds.Height/2); err != nil {
			t.Fatal(err)
		}
		if !session.desk.dockMenu {
			t.Fatal("the dock menu did not open")
		}
	}
	openMenu()
	for _, label := range []string{m.FullscreenBarDockTop, m.FullscreenBarDockBottom, m.FullscreenBarDockLeft, m.FullscreenBarDockRight} {
		if !snapshotHasMenuRow(driver.Snapshot(), label) {
			t.Fatalf("the dock menu has no row %q", label)
		}
	}
	clickPageMenuRow(t, state, m.FullscreenBarDockLeft)
	if err := repaintDesktopDriver(driver, state); err != nil {
		t.Fatal(err)
	}
	if session.desk.dockMenu {
		t.Fatal("choosing an edge left the menu open")
	}
	if state.desktopBar.dock != desktopBarDockLeft {
		t.Fatalf("dock = %v, want left", state.desktopBar.dock)
	}
	area := session.desk.area
	if bar := state.desktopBar.bar; bar.X != area.X+desktopBarEdgeMargin {
		t.Fatalf("left dock bar %+v in %+v", bar, area)
	}

	// Escape closes the drawn menu so a press away is not needed.
	openMenu()
	if err := driver.Key(gfx.KeyEscape, 0); err != nil {
		t.Fatal(err)
	}
	if session.desk.dockMenu {
		t.Fatal("Escape left the dock menu open")
	}
	if err := repaintDesktopDriver(driver, state); err != nil {
		t.Fatal(err)
	}
	if snapshotHasMenuRow(driver.Snapshot(), m.FullscreenBarDockTop) {
		t.Fatal("the dock menu was still painted after Escape")
	}

	// An unpinned bar stays visible while its menu is open, even as the pointer
	// leaves the bar for the menu.
	unpinDesktopBar(t, driver, state)
	openMenu()
	moveDesktopPointer(t, driver, state, state.viewport.Width/2, state.viewport.Height/2)
	if !session.desk.dockMenu {
		t.Fatal("moving the pointer closed the dock menu")
	}
	if !barShown(t, driver) {
		t.Fatal("the unpinned bar hid while its menu was open")
	}
	if err := driver.Key(gfx.KeyEscape, 0); err != nil {
		t.Fatal(err)
	}
	moveDesktopPointer(t, driver, state, state.viewport.Width/2, state.viewport.Height/2)
	if barShown(t, driver) {
		t.Fatal("the unpinned bar stayed after its menu closed")
	}
}

// TestDesktopFullscreenBarDragsToAnEdgeAndStaysWhereItIsDropped drives the
// drag interaction: the bar follows the pointer, snaps to a nearby edge, and
// stays where it is dropped anywhere else.
func TestDesktopFullscreenBarDragsToAnEdgeAndStaysWhereItIsDropped(t *testing.T) {
	state, session, driver := openFullscreenDesktop(t)
	area := session.desk.area
	drag := func(to gfx.Point) {
		t.Helper()
		hit := state.desktopBar.dragHit
		if hit.IsZero() {
			t.Fatal("the fullscreen bar has no drag surface")
		}
		bar := state.desktopBar.bar
		state.Pointer(gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary,
			X: bar.X + 2, Y: bar.Y + 2, Viewport: state.viewport, Target: hit})
		if !state.desktopBar.drag.active {
			t.Fatal("a press on the bar did not start a drag")
		}
		state.Pointer(gfx.PointerEvent{Kind: gfx.PointerMove, X: to.X, Y: to.Y, Viewport: state.viewport})
		if err := repaintDesktopDriver(driver, state); err != nil {
			t.Fatal(err)
		}
		state.Pointer(gfx.PointerEvent{Kind: gfx.PointerUp, Button: gfx.PointerButtonPrimary, X: to.X, Y: to.Y, Viewport: state.viewport})
		if state.desktopBar.drag.active {
			t.Fatal("letting go left the drag active")
		}
		if err := repaintDesktopDriver(driver, state); err != nil {
			t.Fatal(err)
		}
	}
	// Dropped in the middle of the picture, the bar stays there.
	drag(gfx.Point{X: area.X + area.Width/2, Y: area.Y + area.Height/2})
	if state.desktopBar.dock != desktopBarDockFree {
		t.Fatalf("dock = %v, want free", state.desktopBar.dock)
	}
	bar := state.desktopBar.bar
	// The press was 2 px into the bar, so the bar's corner follows the pointer.
	if bar.X < area.X+area.Width/2-2-2 || bar.X > area.X+area.Width/2-2+2 ||
		bar.Y < area.Y+area.Height/2-2-2 || bar.Y > area.Y+area.Height/2-2+2 {
		t.Fatalf("dropped bar %+v, want its corner at the pointer", bar)
	}
	// Dropped near the right edge, it snaps flush to that edge.
	drag(gfx.Point{X: area.X + area.Width - bar.Width, Y: area.Y + area.Height/2})
	if state.desktopBar.dock != desktopBarDockRight {
		t.Fatalf("dock = %v after a drop by the right edge, want right", state.desktopBar.dock)
	}
	if bar := state.desktopBar.bar; bar.X+bar.Width != area.X+area.Width-desktopBarEdgeMargin {
		t.Fatalf("right dock bar %+v in %+v", bar, area)
	}
	// Dropped near the top edge, it snaps up.
	drag(gfx.Point{X: area.X + area.Width/2, Y: area.Y + 4})
	if state.desktopBar.dock != desktopBarDockTop {
		t.Fatalf("dock = %v after a drop by the top edge, want top", state.desktopBar.dock)
	}
	if bar := state.desktopBar.bar; bar.Y != area.Y+desktopBarEdgeMargin {
		t.Fatalf("top dock bar %+v in %+v", bar, area)
	}

	// The pointer leaving the window finishes a drag where the bar is.
	bar = state.desktopBar.bar
	hit := state.desktopBar.dragHit
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary,
		X: bar.X + 2, Y: bar.Y + 2, Viewport: state.viewport, Target: hit})
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerMove, X: area.X + area.Width/2, Y: area.Y + area.Height/2, Viewport: state.viewport})
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerExit})
	if state.desktopBar.drag.active {
		t.Fatal("leaving the window left the drag active")
	}
	if state.desktopBar.dock != desktopBarDockFree {
		t.Fatalf("dock = %v after leaving the window mid-drag, want free", state.desktopBar.dock)
	}

	// The wide exit label is a grab area too: a press that moves drags the bar,
	// while a press that does not still exits fullscreen.
	if err := repaintDesktopDriver(driver, state); err != nil {
		t.Fatal(err)
	}
	labelID := state.desktopBar.labelHit
	label, ok := hitByIDInSnapshot(driver.Snapshot(), labelID)
	if !ok {
		t.Fatal("the fullscreen bar has no exit label")
	}
	labelX, labelY := label.Bounds.X+label.Bounds.Width/2, label.Bounds.Y+label.Bounds.Height/2
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary,
		X: labelX, Y: labelY, Viewport: state.viewport, Target: labelID})
	if state.desktopBar.drag.active || !state.desktopBar.drag.pending {
		t.Fatal("a press on the exit label took the pointer before moving")
	}
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerMove, X: labelX + 80, Y: labelY + 120, Viewport: state.viewport})
	if !state.desktopBar.drag.active {
		t.Fatal("moving the pressed exit label did not start a drag")
	}
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerUp, Button: gfx.PointerButtonPrimary, X: labelX + 80, Y: labelY + 120, Viewport: state.viewport})
	if err := repaintDesktopDriver(driver, state); err != nil {
		t.Fatal(err)
	}
	if !driver.Fullscreen() || !session.desk.fullscreen {
		t.Fatal("dragging the exit label left fullscreen")
	}
	if state.desktopBar.dock != desktopBarDockFree {
		t.Fatalf("dock = %v after dragging the exit label, want free", state.desktopBar.dock)
	}
	label, ok = hitByIDInSnapshot(driver.Snapshot(), state.desktopBar.labelHit)
	if !ok {
		t.Fatal("the moved bar has no exit label")
	}
	if err := driver.Click(label.Bounds.X+label.Bounds.Width/2, label.Bounds.Y+label.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	if driver.Fullscreen() || session.desk.fullscreen {
		t.Fatal("a click on the exit label did not leave fullscreen")
	}
}

// TestFreeDesktopFullscreenBarReturnsWhereItWasLeft checks a free bar, hidden,
// comes back over the place it was left rather than an edge.
func TestFreeDesktopFullscreenBarReturnsWhereItWasLeft(t *testing.T) {
	state, session, driver := openFullscreenDesktop(t)
	unpinDesktopBar(t, driver, state)
	state.desktopBar.dock = desktopBarDockFree
	state.desktopBar.free = gfx.Point{X: 0.5, Y: 0.5}
	if err := repaintDesktopDriver(driver, state); err != nil {
		t.Fatal(err)
	}
	area := session.desk.area
	bar := state.desktopBar.bar
	centre := gfx.Point{X: bar.X + bar.Width/2, Y: bar.Y + bar.Height/2}
	moveDesktopPointer(t, driver, state, area.X+4, area.Y+4)
	if barShown(t, driver) {
		t.Fatal("a free bar appeared at an edge")
	}
	moveDesktopPointer(t, driver, state, centre.X, centre.Y)
	if !barShown(t, driver) {
		t.Fatal("a free bar did not return where it was left")
	}
	moveDesktopPointer(t, driver, state, centre.X, centre.Y+bar.Height+fullscreenBarRevealSlack+1)
	if barShown(t, driver) {
		t.Fatal("a free bar stayed after the pointer left it")
	}
}

// TestDesktopFullscreenExitClosesTheDockMenu checks the chord still leaves
// fullscreen while the dock menu is open, and the menu does not outlive it.
func TestDesktopFullscreenExitClosesTheDockMenu(t *testing.T) {
	state, session, driver := openFullscreenDesktop(t)
	m := state.messages.Desktop
	dockHit, ok := hitByIDInSnapshot(driver.Snapshot(), hitDesktopBarDock)
	if !ok {
		t.Fatal("no dock button in the fullscreen bar")
	}
	if err := driver.Click(dockHit.Bounds.X+dockHit.Bounds.Width/2, dockHit.Bounds.Y+dockHit.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	if !session.desk.dockMenu {
		t.Fatal("the dock menu did not open")
	}
	if err := driver.Key(gfx.KeyEnter, gfx.ModifierControl|gfx.ModifierAlt); err != nil {
		t.Fatal(err)
	}
	if driver.Fullscreen() || session.desk.fullscreen {
		t.Fatal("the exit chord did not leave fullscreen while the dock menu was open")
	}
	if session.desk.dockMenu {
		t.Fatal("the dock menu outlived fullscreen")
	}
	if snapshotHasMenuRow(driver.Snapshot(), m.FullscreenBarDockTop) {
		t.Fatal("the dock menu was still painted after fullscreen")
	}
}

func TestDesktopFullscreenBarDockSurvivesARestart(t *testing.T) {
	state := newTestState(t, testOptions(t))
	if !state.setDesktopFullscreenBarDock(desktopBarDockLeft) {
		t.Fatal("the dock did not change")
	}
	if saved := state.snapshot(); saved.FullscreenBarDock != "left" {
		t.Fatalf("saved dock = %q, want left", saved.FullscreenBarDock)
	}
	restored := newTestState(t, testOptions(t))
	restored.restoreShared(state.snapshot())
	if restored.desktopBar.dock != desktopBarDockLeft {
		t.Fatalf("restored dock = %v, want left", restored.desktopBar.dock)
	}
	state.desktopBar.dock = desktopBarDockFree
	state.desktopBar.free = gfx.Point{X: 0.25, Y: 0.75}
	state.save()
	if saved := state.snapshot(); saved.FullscreenBarDock != "free" || saved.FullscreenBarX != 0.25 || saved.FullscreenBarY != 0.75 {
		t.Fatalf("saved free bar = %q at %v,%v", saved.FullscreenBarDock, saved.FullscreenBarX, saved.FullscreenBarY)
	}
	free := newTestState(t, testOptions(t))
	free.restoreShared(state.snapshot())
	if free.desktopBar.dock != desktopBarDockFree || free.desktopBar.free != (gfx.Point{X: 0.25, Y: 0.75}) {
		t.Fatalf("restored free bar = %v at %+v", free.desktopBar.dock, free.desktopBar.free)
	}
	for value, want := range map[string]desktopBarDock{
		"":         desktopBarDockTop,
		"top":      desktopBarDockTop,
		"bottom":   desktopBarDockBottom,
		"left":     desktopBarDockLeft,
		"right":    desktopBarDockRight,
		"free":     desktopBarDockFree,
		" left ":   desktopBarDockLeft,
		"nonsense": desktopBarDockTop,
	} {
		if got := desktopBarDockFromString(value); got != want {
			t.Fatalf("desktopBarDockFromString(%q) = %v, want %v", value, got, want)
		}
	}
	for dock, want := range map[desktopBarDock]string{
		desktopBarDockTop: "top", desktopBarDockBottom: "bottom",
		desktopBarDockLeft: "left", desktopBarDockRight: "right", desktopBarDockFree: "free",
	} {
		if got := dock.String(); got != want {
			t.Fatalf("dock %v saves as %q, want %q", dock, got, want)
		}
	}
}
