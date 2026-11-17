//go:build linux || darwin

package rfb

import (
	"errors"
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// The live tests talk to a real VNC server. They run only when MOTTY_VNC_ADDR
// names one, because nothing in this repository starts it:
//
//	MOTTY_VNC_ADDR     host:port of the server
//	MOTTY_VNC_USERNAME the system account, for a server that offers only Apple
//	                   Remote Desktop authentication (macOS Screen Sharing)
//	MOTTY_VNC_PASSWORD its VNC password; empty for a server without security
//	MOTTY_VNC_DISPLAY  its X display (":2"), when the test runs on the server's
//	                   own machine: it then drives and inspects the desktop with
//	                   xdotool and xrandr to prove input and updates have effect
//	MOTTY_VNC_XEVLOG   a file an "xev -event keyboard -event button" window on
//	                   that display writes to
//
// See tools/live/vnc_live.sh.

func liveAddr(t *testing.T) string {
	t.Helper()
	addr := os.Getenv("MOTTY_VNC_ADDR")
	if addr == "" {
		t.Skip("MOTTY_VNC_ADDR is not set: no live VNC server")
	}
	return addr
}

func x(t *testing.T, name string, args ...string) string {
	t.Helper()
	display := os.Getenv("MOTTY_VNC_DISPLAY")
	if display == "" {
		t.Skip("MOTTY_VNC_DISPLAY is not set")
	}
	command := exec.Command(name, args...)
	command.Env = append(os.Environ(), "DISPLAY="+display)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, output)
	}
	return strings.TrimSpace(string(output))
}

type liveSession struct {
	client *Client
	sink   *canvas
	done   <-chan error
}

func liveConnect(t *testing.T, password string, encodings []int32) *liveSession {
	t.Helper()
	conn, err := net.DialTimeout("tcp", liveAddr(t), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	client, err := Connect(conn, Config{Username: os.Getenv("MOTTY_VNC_USERNAME"), Password: password, Encodings: encodings})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	sink := &canvas{}
	return &liveSession{client: client, sink: sink, done: run(client, sink)}
}

func (live *liveSession) waitFor(t *testing.T, what string, condition func() bool) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(25 * time.Millisecond) {
		if condition() {
			return
		}
		select {
		case err := <-live.done:
			t.Fatalf("the connection ended while waiting for %s: %v", what, err)
		default:
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}

// distinct counts the colors on the picture, to tell a drawn desktop from a
// blank one.
func (c *canvas) distinct() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	seen := map[[3]byte]bool{}
	for offset := 0; offset+3 < len(c.pix); offset += 4 {
		seen[[3]byte{c.pix[offset], c.pix[offset+1], c.pix[offset+2]}] = true
		if len(seen) > 8 {
			break
		}
	}
	return len(seen)
}

func (c *canvas) snapshot() (width, height int, pix []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.width, c.height, append([]byte(nil), c.pix...)
}

func TestLiveEveryEncodingDrawsTheRealDesktop(t *testing.T) {
	password := os.Getenv("MOTTY_VNC_PASSWORD")
	for name, encodings := range map[string][]int32{
		"default":      nil,
		"raw":          {EncodingRaw, encodingDesktopSize},
		"copyrect+raw": {EncodingCopyRect, EncodingRaw, encodingDesktopSize},
		"rre":          {EncodingRRE, EncodingRaw, encodingDesktopSize},
		"hextile":      {EncodingHextile, EncodingRaw, encodingDesktopSize},
		"zrle":         {EncodingZRLE, EncodingRaw, encodingDesktopSize},
	} {
		t.Run(name, func(t *testing.T) {
			live := liveConnect(t, password, encodings)
			defer live.client.Close()
			live.waitFor(t, "a drawn desktop", func() bool { return live.sink.distinct() >= 2 })
			width, height, _ := live.sink.snapshot()
			t.Logf("%s: %dx%d %q, security %d, version 3.%d, %d colors", name, width, height, live.client.Name, live.client.Security, live.client.Version, live.sink.distinct())
			if width < 100 || height < 100 {
				t.Fatalf("desktop size %dx%d", width, height)
			}
		})
	}
}

func TestLiveAuthentication(t *testing.T) {
	addr, password := liveAddr(t), os.Getenv("MOTTY_VNC_PASSWORD")
	try := func(password string) error {
		conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		_, err = Connect(conn, Config{Username: os.Getenv("MOTTY_VNC_USERNAME"), Password: password})
		return err
	}
	if password == "" {
		if err := try(""); err != nil {
			t.Fatalf("a server without security refused: %v", err)
		}
		return
	}
	if err := try(""); !errors.Is(err, ErrAuthRequired) {
		t.Fatalf("no password: %v, want ErrAuthRequired", err)
	}
	if err := try("X" + password[1:]); !errors.Is(err, ErrAuthFailed) {
		t.Fatalf("wrong password: %v, want ErrAuthFailed", err)
	}
	if err := try(password); err != nil {
		t.Fatalf("right password: %v", err)
	}
}

func TestLiveWindowMovesAreDrawnAndPointerAndKeysReachTheDesktop(t *testing.T) {
	logPath := os.Getenv("MOTTY_VNC_XEVLOG")
	if logPath == "" {
		t.Skip("MOTTY_VNC_XEVLOG is not set")
	}
	password := os.Getenv("MOTTY_VNC_PASSWORD")
	for name, encodings := range map[string][]int32{"zrle": {EncodingZRLE, encodingDesktopSize}, "hextile": {EncodingHextile, encodingDesktopSize}, "raw": {EncodingRaw, encodingDesktopSize}} {
		t.Run(name, func(t *testing.T) {
			live := liveConnect(t, password, encodings)
			defer live.client.Close()
			live.waitFor(t, "a drawn desktop", func() bool { return live.sink.distinct() >= 2 })

			// A window moved on the server is a framebuffer update on the client.
			window := x(t, "xdotool", "search", "--name", "Event Tester")
			window = strings.Fields(window)[0]
			x(t, "xdotool", "windowmove", window, "20", "20")
			time.Sleep(300 * time.Millisecond)
			_, _, before := live.sink.snapshot()
			x(t, "xdotool", "windowmove", window, "400", "300")
			live.waitFor(t, "the moved window to be drawn", func() bool {
				_, _, after := live.sink.snapshot()
				return string(after) != string(before)
			})

			// The pointer, on servers that follow RFC 6143. KasmVNC, the server on the
			// Linux test machine, reads a PointerEvent as 12 bytes (a 16-bit button
			// mask and two scroll deltas) instead of 6, so it never completes a
			// standard one and drops the connection at the next: set
			// MOTTY_VNC_POINTER=1 only for a server that does not do that. Pointer
			// buttons and the wheel are proven against TigerVNC through the headless
			// application (docs/remote-desktop.md).
			if os.Getenv("MOTTY_VNC_POINTER") != "" {
				live.client.SendPointer(600, 450, 0)
				live.client.SendPointer(610, 460, 1<<2)
				live.client.SendPointer(610, 460, 0)
				deadline := time.Now().Add(5 * time.Second)
				for !strings.Contains(x(t, "xdotool", "getmouselocation"), "x:610 y:460") && time.Now().Before(deadline) {
					time.Sleep(50 * time.Millisecond)
				}
				if got := x(t, "xdotool", "getmouselocation"); !strings.Contains(got, "x:610 y:460") {
					t.Fatalf("pointer is at %q, want 610,460", got)
				}
			}

			// Keys go to the window under the pointer (no window manager runs). The
			// pointer is placed from the server's own side, because of the KasmVNC
			// quirk above.
			x(t, "xdotool", "mousemove", "500", "400")
			// Keys: "hi" and Return, as keysyms.
			for _, keysym := range []uint32{'h', 'i', 0xff0d} {
				live.client.SendKey(keysym, true)
				live.client.SendKey(keysym, false)
			}
			deadline := time.Now().Add(5 * time.Second)
			var log string
			for time.Now().Before(deadline) {
				data, _ := os.ReadFile(logPath)
				log = string(data)
				if strings.Contains(log, "(keysym 0xff0d, Return)") {
					break
				}
				time.Sleep(50 * time.Millisecond)
			}
			for _, want := range []string{"(keysym 0x68, h)", "(keysym 0x69, i)", "(keysym 0xff0d, Return)"} {
				if !strings.Contains(log, want) {
					t.Errorf("the desktop never saw %q", want)
				}
			}
			if t.Failed() {
				t.Logf("xev log tail:\n%s", log[max(0, len(log)-1500):])
			}
		})
	}
}

func TestLiveDesktopSizeChange(t *testing.T) {
	if os.Getenv("MOTTY_VNC_RESIZE") == "" {
		t.Skip("MOTTY_VNC_RESIZE is not set")
	}
	live := liveConnect(t, os.Getenv("MOTTY_VNC_PASSWORD"), nil)
	defer live.client.Close()
	live.waitFor(t, "a drawn desktop", func() bool { return live.sink.distinct() >= 2 })
	x(t, "xrandr", "--fb", "1280x800")
	defer x(t, "xrandr", "--fb", "1024x768")
	live.waitFor(t, "the DesktopSize message", func() bool {
		width, height, _ := live.sink.snapshot()
		return width == 1280 && height == 800
	})
	// The desktop is still being drawn after the resize.
	x(t, "xdotool", "search", "--name", "Event Tester", "windowmove", "50", "50")
	live.waitFor(t, "an update after the resize", func() bool { return live.sink.distinct() >= 2 })
}
