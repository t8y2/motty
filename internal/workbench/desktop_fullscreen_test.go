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
	driver, err := app.NewDriver(app.Config{
		Title: "Remote desktop", Width: 1400, Height: 900,
		Invalidator: &invalidator,
		WindowMode:  app.WindowCustomTitlebar, TitlebarHeight: TitlebarHeight,
		Paint: state.Paint, OnWake: state.Wake, OnResize: state.Resize,
		OnKey: state.Key, OnShortcut: state.Shortcut, OnPointer: state.Pointer,
		OnFullscreen: state.SetWindowFullscreen,
		TextInput: func(context app.TextInputContext) *app.TextInput {
			return state.TerminalInput(context)
		},
	}, canvas)
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
	state.Click(hitTabMenuLatency)
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
