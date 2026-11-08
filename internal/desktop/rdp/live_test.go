package rdp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/png"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"motty/internal/desktop"
)

// The live tests talk to a real RDP server and are skipped without it:
//
//	MOTTY_RDP_ADDR      host:port of the server
//	MOTTY_RDP_USER      account (MOTTY_RDP_DOMAIN: its domain, optional)
//	MOTTY_RDP_PASSWORD  its password
//	MOTTY_RDP_PNG       file name prefix for the pictures received (optional)
//	MOTTY_RDP_PRESS     keys to press once the first picture is up, comma separated
//	                    (enter, left, tab, esc, win), to answer a question the
//	                    server asks first (optional)
//	MOTTY_RDP_CLICK     "x,y": a click there once the first picture is up, instead of keys
//	MOTTY_RDP_PROMPT    seconds the certificate question takes to answer (optional)
//
// See tools/live/rdp_live.sh and docs/remote-desktop.md.

type liveConfig struct {
	address, user, domain, password, png string
}

func liveEnv(t *testing.T) liveConfig {
	t.Helper()
	config := liveConfig{
		address: os.Getenv("MOTTY_RDP_ADDR"), user: os.Getenv("MOTTY_RDP_USER"), domain: os.Getenv("MOTTY_RDP_DOMAIN"),
		password: os.Getenv("MOTTY_RDP_PASSWORD"), png: os.Getenv("MOTTY_RDP_PNG"),
	}
	if config.address == "" {
		t.Skip("MOTTY_RDP_ADDR not set")
	}
	return config
}

// screen is a Presenter that keeps the picture.
type screen struct {
	mu            sync.Mutex
	width, height int
	pix           []byte
}

func (s *screen) Resize(width, height int) {
	s.mu.Lock()
	s.width, s.height, s.pix = width, height, make([]byte, width*height*4)
	s.mu.Unlock()
}

func (s *screen) Update(x, y, width, height int, pix []byte, stride int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for row := 0; row < height; row++ {
		copy(s.pix[((y+row)*s.width+x)*4:], pix[row*stride:][:width*4])
	}
}

// colors counts the distinct pixels, up to limit.
func (s *screen) colors(limit int) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	seen := map[[3]byte]struct{}{}
	for i := 0; i+3 < len(s.pix) && len(seen) < limit; i += 4 {
		seen[[3]byte{s.pix[i], s.pix[i+1], s.pix[i+2]}] = struct{}{}
	}
	return len(seen)
}

func (s *screen) snapshot() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]byte(nil), s.pix...)
}

func (s *screen) savePNG(t *testing.T, name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	img := image.NewRGBA(image.Rect(0, 0, s.width, s.height))
	for i := 0; i+3 < len(s.pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = s.pix[i+2], s.pix[i+1], s.pix[i], 0xFF
	}
	file, err := os.Create(name)
	if err != nil {
		t.Logf("png: %v", err)
		return
	}
	defer file.Close()
	if err := png.Encode(file, img); err != nil {
		t.Logf("png: %v", err)
	}
}

// changed counts the pixels that differ between two snapshots.
func changed(a, b []byte) int {
	count := 0
	for i := 0; i+3 < min(len(a), len(b)); i += 4 {
		if a[i] != b[i] || a[i+1] != b[i+1] || a[i+2] != b[i+2] {
			count++
		}
	}
	return count
}

func direct(ctx context.Context, network, address string) (net.Conn, error) {
	var dialer net.Dialer
	return dialer.DialContext(ctx, network, address)
}

// pump presents the session's picture to s until the condition holds or time is up.
func pump(t *testing.T, session *Session, s *screen, within time.Duration, done func() bool) bool {
	t.Helper()
	end := time.Now().Add(within)
	for time.Now().Before(end) {
		session.Present(s)
		if done() {
			return true
		}
		select {
		case <-session.Done():
			session.Present(s)
			return done()
		case <-time.After(100 * time.Millisecond):
		}
	}
	session.Present(s)
	return done()
}

func pressNamed(t *testing.T, session *Session, names string) {
	keys := map[string]desktop.Key{"enter": desktop.KeyEnter, "left": desktop.KeyLeft, "right": desktop.KeyRight,
		"tab": desktop.KeyTab, "esc": desktop.KeyEscape, "win": desktop.KeyMeta}
	for _, name := range strings.Split(names, ",") {
		key, ok := keys[strings.TrimSpace(name)]
		if !ok {
			continue
		}
		for _, down := range []bool{true, false} {
			if err := session.SendKey(desktop.KeyEvent{Key: key, Down: down}); err != nil {
				t.Fatalf("press %s: %v", name, err)
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
}

func TestLiveSessionPaintsAndTakesInput(t *testing.T) {
	config := liveEnv(t)
	var pinned string
	var verified atomic.Int32
	prompt, _ := strconv.Atoi(os.Getenv("MOTTY_RDP_PROMPT"))
	opts := desktop.Options{
		Username: config.user, Domain: config.domain, Password: config.password, Clipboard: true,
		VerifyCertificate: func(address string, der []byte) error {
			sum := sha256.Sum256(der)
			pinned = hex.EncodeToString(sum[:])
			verified.Add(1)
			time.Sleep(time.Duration(prompt) * time.Second) // the user reading the fingerprint
			return nil
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	started := time.Now()
	session, err := Open(ctx, direct, config.address, opts)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer session.Close()
	t.Logf("connected in %v, certificate sha256 %s, verified %d time(s), protocol %s", time.Since(started), pinned, verified.Load(), session.Info().Protocol)
	if pinned == "" {
		t.Fatal("the certificate was never offered for verification")
	}

	view := &screen{}
	if !pump(t, session, view, 40*time.Second, func() bool { return view.colors(64) >= 16 }) {
		t.Fatalf("no picture: %d colours (session ended: %v)", view.colors(64), session.Err())
	}
	width, height := session.Size()
	t.Logf("picture %dx%d, %d+ colours", width, height, view.colors(1000))
	save := func(name string) {
		if config.png != "" {
			view.savePNG(t, config.png+"-"+name+".png")
		}
	}
	// Let the first paint settle.
	pump(t, session, view, 3*time.Second, func() bool { return false })
	save("first")
	// What the server said about its pointer, which the workbench shows as the system cursor:
	// a server that names none leaves the kind unset, which is not a failure.
	_ = session.SendPointer(width/2, height/2, 0)
	pump(t, session, view, 2*time.Second, func() bool { return false })
	kind, shape, version := session.PointerShape()
	t.Logf("pointer: kind %d, %dx%d, hotspot %d,%d, %d statement(s)", kind, shape.Width, shape.Height, shape.HotX, shape.HotY, version)

	liveAnswerFirstPrompt(t, session, view, config, save)

	// The pointer and a key must have a visible effect: the Windows key opens the
	// Start menu of a desktop, and moving over the taskbar highlights it.
	before := view.snapshot()
	// Where the session's window is, so that a focus-follows-pointer X session
	// sends the keys to it.
	_ = session.SendPointer(width/4, height/8, 0)
	time.Sleep(300 * time.Millisecond)
	for step := 0; step < 20; step++ {
		x, y := width*(step+1)/21, height-8
		if err := session.SendPointer(x, y, 0); err != nil {
			t.Fatal(err)
		}
		time.Sleep(30 * time.Millisecond)
	}
	pressNamed(t, session, "win")
	_ = session.SendPointer(width/4, height/8, 0)
	time.Sleep(300 * time.Millisecond)
	// Keys that have a key on a US keyboard go as scancodes, with Shift where the
	// character needs it; the rest of the text as Unicode events.
	for _, r := range "echo Key-Typed_1 " {
		for _, down := range []bool{true, false} {
			if err := session.SendKey(desktop.KeyEvent{Rune: r, Down: down}); err != nil {
				t.Fatal(err)
			}
		}
	}
	pressNamed(t, session, "enter")
	moved := pump(t, session, view, 15*time.Second, func() bool { return changed(before, view.snapshot()) > 500 })
	pump(t, session, view, 3*time.Second, func() bool { return false })
	save("input")
	t.Logf("pointer and Windows key: %d pixels changed", changed(before, view.snapshot()))
	if !moved {
		t.Error("input had no visible effect")
	}
	pressNamed(t, session, "esc")

	if err := session.SendText("héllo 世界"); err != nil {
		t.Errorf("send text: %v", err)
	}
	if err := session.SetClipboard("motty clipboard"); err != nil {
		t.Errorf("set clipboard: %v", err)
	}
	session.SetActive(false)
	time.Sleep(time.Second)
	session.SetActive(true)
	pump(t, session, view, 4*time.Second, func() bool { return false })
	save("last")

	if err := session.Close(); err != nil {
		t.Errorf("close: %v", err)
	}
	select {
	case <-session.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("Done is not closed after Close")
	}
	if err := session.Err(); err != nil {
		t.Errorf("a close we asked for reports %v", err)
	}
}

// liveAnswerFirstPrompt answers a question the server asks before the desktop
// (typically "the user is already signed in elsewhere; sign them out?") with
// MOTTY_RDP_CLICK or MOTTY_RDP_PRESS, then waits for the desktop behind it. It
// reports whether there was anything to answer; without either variable a
// server that shows no such question continues unchanged.
func liveAnswerFirstPrompt(t *testing.T, session *Session, view *screen, config liveConfig, save func(string)) bool {
	t.Helper()
	keys, click := os.Getenv("MOTTY_RDP_PRESS"), os.Getenv("MOTTY_RDP_CLICK")
	if keys == "" && click == "" {
		return false
	}
	before := view.snapshot()
	if click != "" {
		var x, y int
		if _, err := fmt.Sscanf(click, "%d,%d", &x, &y); err != nil {
			t.Fatalf("MOTTY_RDP_CLICK %q: %v", click, err)
		}
		for _, buttons := range []desktop.Buttons{0, desktop.ButtonLeft, 0} {
			if err := session.SendPointer(x, y, buttons); err != nil {
				t.Fatal(err)
			}
			time.Sleep(150 * time.Millisecond)
		}
	}
	pressNamed(t, session, keys)
	// A first sign-in builds the profile and takes a while to show the desktop.
	pump(t, session, view, 120*time.Second, func() bool { return view.colors(400) >= 400 })
	pump(t, session, view, 8*time.Second, func() bool { return false })
	if save != nil {
		save("answered")
	}
	t.Logf("after the keys %q: %d pixels changed, %d+ colours", keys, changed(before, view.snapshot()), view.colors(1000))
	return true
}

func TestLiveRefusedCertificateStopsAtOnce(t *testing.T) {
	config := liveEnv(t)
	var dials atomic.Int32
	count := func(ctx context.Context, network, address string) (net.Conn, error) {
		dials.Add(1)
		return direct(ctx, network, address)
	}
	errRefused := errors.New("refused by the test")
	_, err := Open(context.Background(), count, config.address, desktop.Options{
		Username: config.user, Domain: config.domain, Password: config.password,
		VerifyCertificate: func(string, []byte) error { return errRefused },
	})
	if !errors.Is(err, errRefused) {
		t.Fatalf("err = %v, want the refusal", err)
	}
	if got := dials.Load(); got > 2 {
		t.Errorf("a refused certificate made %d connections", got)
	}
	t.Logf("a refused certificate: %v after %d connection(s)", err, dials.Load())
}

func TestLiveWrongPasswordIsRefusedOnce(t *testing.T) {
	config := liveEnv(t)
	var dials atomic.Int32
	count := func(ctx context.Context, network, address string) (net.Conn, error) {
		dials.Add(1)
		return direct(ctx, network, address)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	_, err := Open(ctx, count, config.address, desktop.Options{
		Username: config.user, Domain: config.domain, Password: config.password + "-wrong",
	})
	t.Logf("wrong password: err = %v, %d connection(s)", err, dials.Load())
	if err == nil {
		// xrdp checks the password after the handshake and shows its own login
		// screen; only a server with NLA refuses it where the client can see.
		t.Skip("the server does not check the password during the handshake")
	}
	if !errors.Is(err, ErrAuthFailed) {
		t.Fatalf("err = %v, want ErrAuthFailed", err)
	}
}
