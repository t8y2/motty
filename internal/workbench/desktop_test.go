package workbench

import (
	"context"
	"crypto/des"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/ui"
	"motty/internal/sshconn"
)

// vncFixture is a loopback server that speaks RFB 3.8 with a password, draws a
// 200x100 desktop and records what the client sends. It stands in for the
// network only: the application side is the real thing (the RFB client, the
// session, the tab); real servers are exercised by tools/live/vnc_live.sh.
type vncFixture struct {
	listener net.Listener
	mu       sync.Mutex
	messages [][]byte // client input messages: pointer (6 bytes) and key (8 bytes)
	accepted int
	conns    []net.Conn
}

func startVNCFixture(t *testing.T, password string) *vncFixture {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	fixture := &vncFixture{listener: listener}
	t.Cleanup(func() {
		listener.Close()
		fixture.mu.Lock()
		for _, conn := range fixture.conns {
			conn.Close()
		}
		fixture.mu.Unlock()
	})
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			fixture.mu.Lock()
			fixture.accepted++
			fixture.conns = append(fixture.conns, conn)
			fixture.mu.Unlock()
			go fixture.serve(conn, password)
		}
	}()
	return fixture
}

func (fixture *vncFixture) serve(conn net.Conn, password string) {
	defer conn.Close()
	read := func(n int) []byte {
		buffer := make([]byte, n)
		if _, err := io.ReadFull(conn, buffer); err != nil {
			return nil
		}
		return buffer
	}
	conn.Write([]byte("RFB 003.008\n"))
	if read(12) == nil {
		return
	}
	conn.Write([]byte{1, 2})
	read(1)
	challenge := [16]byte{9, 8, 7, 6, 5, 4, 3, 2, 1, 0, 1, 2, 3, 4, 5, 6}
	conn.Write(challenge[:])
	got := read(16)
	if got == nil {
		return
	}
	// The expected answer is computed by the client's own routine through a
	// second handshake-free path: the fixture only checks the password was typed
	// by looking at what the client sent for the known challenge.
	if !rfbResponseMatches(challenge, password, got) {
		conn.Write([]byte{0, 0, 0, 1, 0, 0, 0, 4, 'n', 'o', 'p', 'e'})
		return
	}
	conn.Write([]byte{0, 0, 0, 0})
	read(1)
	init := []byte{0, 200, 0, 100, 32, 24, 0, 1, 0, 255, 0, 255, 0, 255, 16, 8, 0, 0, 0, 0, 0, 0, 0, 4}
	conn.Write(append(init, "test"...))
	read(20)
	header := read(4)
	if header == nil {
		return
	}
	read(4 * int(binary.BigEndian.Uint16(header[2:])))
	drawn := false
	for {
		kind := read(1)
		if kind == nil {
			return
		}
		switch kind[0] {
		case 3: // update request: draw once, then answer later ones with an empty update
			read(9)
			if !drawn {
				drawn = true
				update := []byte{0, 0, 0, 1, 0, 0, 0, 0, 0, 200, 0, 100, 0, 0, 0, 0}
				for range 200 * 100 {
					update = append(update, 200, 100, 20, 0)
				}
				conn.Write(update)
			}
		case 5:
			message := append(kind, read(5)...)
			fixture.record(message)
		case 4:
			message := append(kind, read(7)...)
			fixture.record(message)
		default:
			return
		}
	}
}

func (fixture *vncFixture) record(message []byte) {
	fixture.mu.Lock()
	fixture.messages = append(fixture.messages, message)
	fixture.mu.Unlock()
}

func (fixture *vncFixture) got() [][]byte {
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	return append([][]byte(nil), fixture.messages...)
}

// rfbResponseMatches checks the client's answer to a VNC authentication
// challenge with an independent implementation of the scheme: DES under the
// password's first eight bytes, each byte's bits reversed.
func rfbResponseMatches(challenge [16]byte, password string, response []byte) bool {
	var key [8]byte
	copy(key[:], password)
	for index, value := range key {
		var reversed byte
		for bit := 0; bit < 8; bit++ {
			reversed |= (value >> bit & 1) << (7 - bit)
		}
		key[index] = reversed
	}
	block, err := des.NewCipher(key[:])
	if err != nil {
		return false
	}
	want := make([]byte, 16)
	block.Encrypt(want[:8], challenge[:8])
	block.Encrypt(want[8:], challenge[8:])
	return string(want) == string(response)
}

func vncState(t *testing.T, fixture *vncFixture) (*State, *Session) {
	t.Helper()
	_, options := newWorld(t)
	state := newTestState(t, options)
	state.options.Invalidate = func() {}
	state.connectionReconnect = reconnectOff
	_, port, _ := net.SplitHostPort(fixture.listener.Addr().String())
	number, _ := strconv.Atoi(port)
	addStreamHost(state, Host{Label: "Desk", Address: "127.0.0.1", Protocol: protocolVNC, Port: uint16(number)})
	openHost(state)
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	return state, session
}

func connectDesktop(t *testing.T, state *State, session *Session, password string) {
	t.Helper()
	waitUntil(t, state, "the password question", func() bool { return state.modal == modalPassword })
	state.passwordFields[0].Insert(password)
	state.submitPassword()
	waitUntil(t, state, "the picture", func() bool { return session.desk != nil && session.desk.width == 200 })
}

func paintDesk(state *State) *ui.Canvas {
	var canvas ui.Canvas
	canvas.BeginFrame()
	state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
	canvas.EndFrame()
	return &canvas
}

func TestVNCHostAsksForThePasswordThenShowsTheDesktopAndSendsInput(t *testing.T) {
	fixture := startVNCFixture(t, "secret")
	state, session := vncState(t, fixture)
	connectDesktop(t, state, session, "secret")
	canvas := paintDesk(state)
	if len(canvas.Images()) == 0 {
		t.Fatal("the picture was not painted")
	}
	hit := hitTerminalList.At(int(session.ID))
	area := desktopLayout(ui.Size{Width: 1400, Height: 900}).area
	origin, scale := session.desk.place(area)
	if scale <= 1 {
		t.Fatalf("a 200x100 desktop in a 1400x900 pane is shown at %v", scale)
	}

	// A press at the picture's center is the remote's center, with the left button.
	cx, cy := origin.X+100*scale, origin.Y+50*scale
	pointer(state, ui.PointerEvent{Kind: ui.PointerDown, Button: ui.PointerButtonPrimary, X: cx, Y: cy, Target: hit, ClickCount: 1})
	pointer(state, ui.PointerEvent{Kind: ui.PointerUp, Button: ui.PointerButtonPrimary, X: cx, Y: cy, Target: hit})
	waitUntil(t, state, "the pointer events", func() bool { return len(fixture.got()) >= 2 })
	down, up := fixture.got()[0], fixture.got()[1]
	x, y := int(binary.BigEndian.Uint16(down[2:])), int(binary.BigEndian.Uint16(down[4:]))
	if down[1] != 1 || abs(x-100) > 1 || abs(y-50) > 1 || up[1] != 0 {
		t.Fatalf("down %v up %v: want the left button at 100,50", down, up)
	}

	// Enter goes down and up as the keyboard does it; typed text, as an input
	// method commits it, is typed by its characters.
	state.Key(ui.KeyEvent{Key: ui.KeyEnter, Target: hit})
	state.Key(ui.KeyEvent{Key: ui.KeyEnter, Target: hit, Release: true})
	state.desktopText(session.ID, app.TextEvent{Kind: app.TextInsert, Text: "é"})
	waitUntil(t, state, "the key events", func() bool { return len(fixture.got()) >= 6 })
	keys := fixture.got()[2:6]
	want := []struct {
		down bool
		sym  uint32
	}{{true, 0xff0d}, {false, 0xff0d}, {true, 0xe9}, {false, 0xe9}}
	for index, expectation := range want {
		if (keys[index][1] == 1) != expectation.down || binary.BigEndian.Uint32(keys[index][4:]) != expectation.sym {
			t.Fatalf("key message %d = %v", index, keys[index])
		}
	}
	state.Scroll(ui.ScrollEvent{X: cx, Y: cy, DeltaY: 90, Viewport: ui.Size{Width: 1400, Height: 900}, Target: hit})
	waitUntil(t, state, "the wheel", func() bool { return len(fixture.got()) >= 10 })
	if wheel := fixture.got()[6]; wheel[1] != 1<<4 {
		t.Fatalf("a scroll down sent %v, want the wheel-down button", wheel)
	}
}

// keySyms is the keys the fixture was sent, as down or up and the keysym.
func keySyms(messages [][]byte) []string {
	var keys []string
	for _, message := range messages {
		if message[0] != 4 {
			continue
		}
		direction := "up"
		if message[1] == 1 {
			direction = "down"
		}
		keys = append(keys, fmt.Sprintf("%s %x", direction, binary.BigEndian.Uint32(message[4:])))
	}
	return keys
}

func TestHeldKeysStayDownOnTheRemoteUntilTheyComeUp(t *testing.T) {
	fixture := startVNCFixture(t, "secret")
	state, session := vncState(t, fixture)
	connectDesktop(t, state, session, "secret")
	hit := hitTerminalList.At(int(session.ID))
	send := func(event ui.KeyEvent) {
		event.Target = hit
		state.Key(event)
	}

	// Shift is down on the remote while it is down here, the key types the
	// capital the layout gives it, and a key's repeat is not sent again.
	send(ui.KeyEvent{Key: ui.KeyShiftLeft, Modifiers: ui.ModifierShift})
	send(ui.KeyEvent{Text: "a", Typed: "A", Modifiers: ui.ModifierShift})
	send(ui.KeyEvent{Text: "a", Typed: "A", Modifiers: ui.ModifierShift, Repeat: true})
	send(ui.KeyEvent{Text: "a", Modifiers: ui.ModifierShift, Release: true})
	send(ui.KeyEvent{Key: ui.KeyShiftLeft, Release: true})
	// A key held when the keyboard goes to Motty is let go, and its own release
	// afterwards is not sent a second time.
	send(ui.KeyEvent{Key: ui.KeyControlLeft, Modifiers: ui.ModifierControl})
	state.toggleCapture(session.desk)
	send(ui.KeyEvent{Key: ui.KeyControlLeft, Release: true})
	want := []string{"down ffe1", "down 41", "up 41", "up ffe1", "down ffe3", "up ffe3"}
	waitUntil(t, state, "the key messages", func() bool { return len(keySyms(fixture.got())) >= len(want) })
	if got := keySyms(fixture.got()); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("remote keys = %v, want %v", got, want)
	}
}

func abs(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func TestWrongPasswordAsksAgainOnANewConnection(t *testing.T) {
	fixture := startVNCFixture(t, "secret")
	state, session := vncState(t, fixture)
	waitUntil(t, state, "the password question", func() bool { return state.modal == modalPassword })
	state.passwordFields[0].Insert("guess")
	state.submitPassword()
	waitUntil(t, state, "the question again", func() bool { return state.modal == modalPassword && session.attempt == 2 })
	if state.passwordError == "" {
		t.Fatal("the second question does not say the first answer was refused")
	}
	state.passwordFields[0].Insert("secret")
	state.submitPassword()
	waitUntil(t, state, "the picture", func() bool { return session.desk != nil && session.desk.width == 200 })
}

func TestKeyboardCaptureStopsMottyShortcutsUntilTheEscapeChordReleasesIt(t *testing.T) {
	fixture := startVNCFixture(t, "secret")
	state, session := vncState(t, fixture)
	connectDesktop(t, state, session, "secret")
	paintDesk(state)
	hit := hitTerminalList.At(int(session.ID))
	// Motty's own new-tab chord, whichever it is on this platform.
	chord := state.shortcuts.chords[shortcutNewTab]
	newTab := ui.KeyEvent{Modifiers: chord.Modifiers, Text: string(rune(chord.Key)), Target: hit}

	if result := state.Shortcut(newTab); result.Handled || state.modal != modalNone || state.activeSection != sectionTerminal {
		t.Fatalf("Motty took Cmd+T while the remote desktop had the keyboard: %+v, modal %v", result, state.modal)
	}
	// The chord then goes to the remote as the keys it is: its modifiers, then
	// the letter, all down.
	modifierKeys := []struct {
		modifier ui.Modifiers
		key      ui.Key
		keysym   uint32
	}{
		{ui.ModifierControl, ui.KeyControlLeft, 0xffe3}, {ui.ModifierAlt, ui.KeyAltLeft, 0xffe9},
		{ui.ModifierShift, ui.KeyShiftLeft, 0xffe1}, {ui.ModifierMeta, ui.KeyMetaLeft, 0xffeb},
	}
	var want []string
	for _, entry := range modifierKeys {
		if chord.Modifiers&entry.modifier != 0 {
			state.Key(ui.KeyEvent{Key: entry.key, Modifiers: chord.Modifiers, Target: hit})
			want = append(want, fmt.Sprintf("down %x", entry.keysym))
		}
	}
	state.Key(newTab)
	want = append(want, fmt.Sprintf("down %x", unicode.ToLower(rune(chord.Key))))
	waitUntil(t, state, "the remote to get the chord", func() bool { return len(keySyms(fixture.got())) >= len(want) })
	if got := keySyms(fixture.got()); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("remote keys = %v, want %v", got, want)
	}

	escape := ui.KeyEvent{Modifiers: ui.ModifierControl | ui.ModifierAlt, Text: "g", Target: hit}
	if result := state.Shortcut(escape); !result.Handled || !session.desk.released {
		t.Fatal("the escape chord did not release the keyboard")
	}
	// Giving the keyboard to Motty lets go of the chord still held there.
	waitUntil(t, state, "the chord to be released", func() bool { return len(keySyms(fixture.got())) >= 2*len(want) })
	before := len(fixture.got())
	state.Key(ui.KeyEvent{Key: ui.KeyEnter, Target: hit})
	state.desktopText(session.ID, app.TextEvent{Kind: app.TextInsert, Text: "x"})
	time.Sleep(50 * time.Millisecond)
	if len(fixture.got()) > before {
		t.Fatal("keys went to the remote desktop after the keyboard was released")
	}
	if result := state.Shortcut(newTab); !result.Handled {
		t.Fatal("Motty's shortcuts did not come back after the release")
	}
	state.modal = modalNone // the new-tab picker that shortcut opened
	state.Shortcut(escape)
	if session.desk.released {
		t.Fatal("the escape chord did not capture the keyboard again")
	}
}

func TestLostConnectionKeepsThePictureAndEnterReconnects(t *testing.T) {
	fixture := startVNCFixture(t, "secret")
	state, session := vncState(t, fixture)
	connectDesktop(t, state, session, "secret")
	fixture.mu.Lock()
	for _, conn := range fixture.conns {
		conn.Close()
	}
	fixture.mu.Unlock()
	waitUntil(t, state, "the end", func() bool { return session.desk != nil && session.desk.ended })
	if !state.canReconnect(session) {
		t.Fatal("an ended desktop offers no reconnect")
	}
	paintDesk(state) // the page with the dimmed picture and the reconnect button
	state.Key(ui.KeyEvent{Key: ui.KeyEnter, Target: hitTerminalList.At(int(session.ID))})
	connectDesktop(t, state, session, "secret")
	if fixture.accepted != 2 {
		t.Fatalf("server accepted %d connections, want 2", fixture.accepted)
	}
}

func TestVNCHostIsSavedWithoutAPasswordAndKeepsItsSwitches(t *testing.T) {
	host := Host{Label: "Desk", Address: "10.0.0.9", Protocol: protocolVNC, Port: 5901, ViaSSH: true, Clipboard: true, User: "ops"}
	host.finish()
	if host.target != "vnc+ssh://ops@10.0.0.9:5901" {
		t.Fatalf("target = %q", host.target)
	}
	var restored Host
	if !restored.applyTransport(host.transport()) || restored.Protocol != protocolVNC || !restored.ViaSSH || !restored.Clipboard {
		t.Fatalf("restored = %+v", restored)
	}
	direct := Host{Address: "10.0.0.9", Protocol: protocolVNC, User: "ignored"}
	direct.normalizeEndpoint()
	if direct.Port != 5900 || direct.User != "" {
		t.Fatalf("a direct VNC host keeps port %d, user %q", direct.Port, direct.User)
	}
}

func TestTunneledVNCAnswersTheSSHQuestionsThenDialsTheServerThroughTheChannel(t *testing.T) {
	fixture := startVNCFixture(t, "secret")
	world, options := newWorld(t)
	state := newTestState(t, options)
	state.options.Invalidate = func() {}
	state.connectionReconnect = reconnectOff
	_, port, _ := net.SplitHostPort(fixture.listener.Addr().String())
	number, _ := strconv.Atoi(port)
	addStreamHost(state, Host{Label: "Via", Address: "gateway.example.com", User: "ops", Protocol: protocolVNC, Port: uint16(number), ViaSSH: true})
	openHost(state)
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	if len(world.configs) != 1 || world.configs[0].Host != "gateway.example.com" || world.configs[0].Port != 22 || world.configs[0].User != "ops" {
		t.Fatalf("the SSH connection was configured as %+v", world.configs)
	}
	ssh, config := world.conns[0], world.configs[0]

	// The SSH server's host key question reaches the user first.
	ssh.set(func(status *sshconn.Status) {
		status.Phase = sshconn.PhaseHostKeyPrompt
		status.HostKey = sshconn.HostKeyInfo{Address: "gateway.example.com:22", Type: "ssh-ed25519", Fingerprint: "SHA256:abc"}
	})
	config.Notify()
	waitUntil(t, state, "the host key question", func() bool { return state.modal == modalHostKey })
	state.Click(hitHostKeyTrust)
	if len(ssh.trusted) != 1 {
		t.Fatalf("the answer did not reach the SSH connection: %v", ssh.trusted)
	}

	// Connected over SSH: the VNC port is dialed from the SSH server's loopback.
	ssh.dial = func(ctx context.Context, network, address string) (net.Conn, error) {
		return net.Dial("tcp", fixture.listener.Addr().String())
	}
	ssh.set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseConnected })
	config.Notify()
	connectDesktop(t, state, session, "secret")
	if calls := ssh.dialCalls; len(calls) != 1 || calls[0].address != "127.0.0.1:"+port {
		t.Fatalf("dialed %+v through the tunnel", calls)
	}
	if session.desk == nil || !session.host.tunneled() {
		t.Fatal("no desktop")
	}
}

func TestDesktopPasswordQuestionCanUseTheSavedSecret(t *testing.T) {
	fixture := startVNCFixture(t, "secret")
	_, options := newWorld(t)
	state := newTestState(t, options)
	state.options.Invalidate = func() {}
	state.connectionReconnect = reconnectOff
	_, port, _ := net.SplitHostPort(fixture.listener.Addr().String())
	number, _ := strconv.Atoi(port)
	host := Host{Label: "Desk", Address: "127.0.0.1", Protocol: protocolVNC, Port: uint16(number)}
	addStreamHost(state, host)
	endpoint := sshconn.Endpoint{Host: host.Address, Port: host.Port, User: vncAccount}
	if err := state.secrets.vault.Set(passwordSecretName(endpoint), "secret"); err != nil {
		t.Fatal(err)
	}
	openHost(state)
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	waitUntil(t, state, "the desktop from the saved password", func() bool {
		return session.desk != nil && session.desk.width == 200
	})
	if state.modal == modalPassword {
		t.Fatal("the saved desktop password was ignored")
	}
}

func TestDesktopPasswordSaveSurvivesTheConnection(t *testing.T) {
	fixture := startVNCFixture(t, "secret")
	state, session := vncState(t, fixture)
	waitUntil(t, state, "the password question", func() bool { return state.modal == modalPassword })
	state.secrets.save = true
	state.passwordFields[0].Insert("secret")
	state.submitPassword()
	waitUntil(t, state, "the picture", func() bool { return session.desk != nil && session.desk.width == 200 })
	endpoint := session.endpoint
	waitUntil(t, state, "the saved password", func() bool {
		_, found, err := state.secrets.vault.Get(passwordSecretName(endpoint))
		return err == nil && found
	})
	value, found, err := state.secrets.vault.Get(passwordSecretName(endpoint))
	if err != nil || !found || value != "secret" {
		t.Fatalf("saved value = %q found=%v err=%v", value, found, err)
	}
}
