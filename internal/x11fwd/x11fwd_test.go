package x11fwd

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func TestParseDisplay(t *testing.T) {
	cases := []struct {
		value, goos string
		want        Display
		fail        bool
	}{
		{":0", "linux", Display{Network: "unix", Address: "/tmp/.X11-unix/X0"}, false},
		{":12.1", "linux", Display{Network: "unix", Address: "/tmp/.X11-unix/X12", Number: 12, Screen: 1}, false},
		{"unix:3", "linux", Display{Network: "unix", Address: "/tmp/.X11-unix/X3", Number: 3}, false},
		{"/private/tmp/com.apple.launchd.AbC/org.xquartz:0", "darwin",
			Display{Network: "unix", Address: "/private/tmp/com.apple.launchd.AbC/org.xquartz:0"}, false},
		{"/private/tmp/com.apple.launchd.AbC/org.xquartz:0.2", "darwin",
			Display{Network: "unix", Address: "/private/tmp/com.apple.launchd.AbC/org.xquartz:0", Screen: 2}, false},
		{"gateway:2.0", "linux", Display{Network: "tcp", Address: "gateway:6002", Host: "gateway", Number: 2}, false},
		{"[::1]:1", "linux", Display{Network: "tcp", Address: "[::1]:6001", Host: "::1", Number: 1}, false},
		{":0", "windows", Display{Network: "tcp", Address: "127.0.0.1:6000"}, false},
		{"localhost:10.0", "windows", Display{Network: "tcp", Address: "localhost:6010", Host: "localhost", Number: 10}, false},
		{"", "linux", Display{}, true},
		{"nocolon", "linux", Display{}, true},
		{":", "linux", Display{}, true},
		{":x", "linux", Display{}, true},
		{":99999", "linux", Display{}, true},
		{":0.", "linux", Display{}, true},
		{"a b:0", "linux", Display{}, true},
	}
	for _, c := range cases {
		got, err := ParseDisplay(c.value, c.goos)
		if (err != nil) != c.fail || got != c.want {
			t.Errorf("ParseDisplay(%q, %s) = %+v, %v; want %+v fail=%v", c.value, c.goos, got, err, c.want, c.fail)
		}
	}
	if _, err := ParseDisplay("", "linux"); !errors.Is(err, ErrNoDisplay) {
		t.Errorf("empty DISPLAY: %v", err)
	}
}

func xauthEntry(family uint16, address, number, name string, data []byte) []byte {
	out := binary.BigEndian.AppendUint16(nil, family)
	for _, field := range [][]byte{[]byte(address), []byte(number), []byte(name), data} {
		out = binary.BigEndian.AppendUint16(out, uint16(len(field)))
		out = append(out, field...)
	}
	return out
}

func TestXauthorityLookupPicksTheEntryForThisMachineAndDisplay(t *testing.T) {
	file := bytes.Join([][]byte{
		xauthEntry(FamilyLocal, "other-host", "0", cookieProtocol, []byte("wrong-host-cookie")),
		xauthEntry(FamilyLocal, "Laptop", "7", cookieProtocol, []byte("wrong-number!!!!")),
		xauthEntry(FamilyLocal, "laptop.example.org", "0", "XDM-AUTHORIZATION-1", []byte("wrong-protocol!!")),
		xauthEntry(FamilyWild, "", "", cookieProtocol, []byte("wildcard-cookie!")),
		xauthEntry(FamilyLocal, "laptop", "0", cookieProtocol, []byte("exact-cookie-16b")),
	}, nil)
	entries, err := ParseXauthority(file)
	if err != nil || len(entries) != 5 {
		t.Fatalf("entries=%d err=%v", len(entries), err)
	}
	auth, ok := Lookup(entries, Display{Network: "unix"}, "Laptop.example.org")
	if !ok || string(auth.Data) != "exact-cookie-16b" || auth.Name != cookieProtocol {
		t.Fatalf("auth=%+v ok=%v", auth, ok)
	}
	// Another display number falls back to the wildcard entry.
	auth, ok = Lookup(entries, Display{Network: "unix", Number: 3}, "laptop")
	if !ok || string(auth.Data) != "wildcard-cookie!" {
		t.Fatalf("fallback auth=%+v ok=%v", auth, ok)
	}
	if _, ok := Lookup(entries[:3], Display{Network: "unix", Number: 3}, "laptop"); ok {
		t.Fatal("matched an entry for another host and display")
	}
}

func TestXauthorityInternetEntryMatchesTCPHost(t *testing.T) {
	file := xauthEntry(FamilyInternet, string([]byte{192, 0, 2, 9}), "1", cookieProtocol, []byte("tcp-cookie-16byt"))
	entries, _ := ParseXauthority(file)
	auth, ok := Lookup(entries, Display{Network: "tcp", Host: "192.0.2.9", Number: 1}, "laptop")
	if !ok || string(auth.Data) != "tcp-cookie-16byt" {
		t.Fatalf("auth=%+v ok=%v", auth, ok)
	}
	if _, ok := Lookup(entries, Display{Network: "tcp", Host: "192.0.2.10", Number: 1}, "laptop"); ok {
		t.Fatal("matched another address")
	}
}

func TestXauthorityTruncatedFileKeepsEntriesBeforeTheDamage(t *testing.T) {
	first := xauthEntry(FamilyLocal, "h", "0", cookieProtocol, []byte("0123456789abcdef"))
	file := append(append([]byte(nil), first...), xauthEntry(FamilyLocal, "h", "1", cookieProtocol, []byte("fedcba9876543210"))...)
	for cut := range len(file) + 1 {
		entries, err := ParseXauthority(file[:cut])
		clean := cut == 0 || cut == len(first) || cut == len(file)
		if clean != (err == nil) {
			t.Fatalf("cut=%d: err=%v", cut, err)
		}
		if want := min(cut/len(first), 2); cut >= len(first) && len(entries) != want {
			t.Fatalf("cut=%d: %d entries, want %d", cut, len(entries), want)
		}
	}
}

func TestResolveReadsTheRealCookieAndToleratesItsAbsence(t *testing.T) {
	file := xauthEntry(FamilyLocal, "laptop", "0", cookieProtocol, []byte("real-cookie-16by"))
	env := Environ{Display: ":0", Xauthority: "/x/auth", Hostname: "laptop", GOOS: "linux",
		ReadFile: func(path string) ([]byte, error) {
			if path != "/x/auth" {
				t.Errorf("read %q", path)
			}
			return file, nil
		}}
	local, err := Resolve(env)
	if err != nil || string(local.Auth.Data) != "real-cookie-16by" {
		t.Fatalf("local=%+v err=%v", local, err)
	}
	env.ReadFile = func(string) ([]byte, error) { return nil, io.ErrUnexpectedEOF }
	if local, err = Resolve(env); err != nil || local.Auth.Name != "" {
		t.Fatalf("missing file: local=%+v err=%v", local, err)
	}
	env.Display = ""
	if _, err = Resolve(env); !errors.Is(err, ErrNoDisplay) {
		t.Fatalf("no display: %v", err)
	}
	// The default file is ~/.Xauthority.
	env = Environ{Display: ":0", Home: "/home/u", GOOS: "linux", Hostname: "laptop",
		ReadFile: func(path string) ([]byte, error) {
			if want := filepath.Join("/home/u", ".Xauthority"); path != want {
				t.Errorf("read %q, want %q", path, want)
			}
			return file, nil
		}}
	if local, _ = Resolve(env); string(local.Auth.Data) != "real-cookie-16by" {
		t.Fatalf("home file: %+v", local)
	}
}

func setupPacket(order binary.ByteOrder, mark byte, name string, data []byte) []byte {
	return Setup{Order: mark, Major: 11, AuthName: name, AuthData: data}.Marshal()
}

func TestSetupRoundTripsInBothByteOrders(t *testing.T) {
	for _, mark := range []byte{'B', 'l'} {
		packet := Setup{Order: mark, Major: 11, Minor: 0, AuthName: cookieProtocol, AuthData: []byte("0123456789abcdef")}.Marshal()
		// 12-byte header, name padded 18 -> 20, data 16.
		if len(packet) != 12+20+16 {
			t.Fatalf("mark %c: packet is %d bytes", mark, len(packet))
		}
		reader := bytes.NewReader(append(packet, "trailing request bytes"...))
		setup, err := ReadSetup(reader)
		if err != nil || setup.AuthName != cookieProtocol || string(setup.AuthData) != "0123456789abcdef" || setup.Major != 11 {
			t.Fatalf("mark %c: setup=%+v err=%v", mark, setup, err)
		}
		if reader.Len() != len("trailing request bytes") {
			t.Fatalf("mark %c: ReadSetup consumed %d bytes beyond the packet", mark, len("trailing request bytes")-reader.Len())
		}
		if !bytes.Equal(setup.Marshal(), packet) {
			t.Fatalf("mark %c: marshal changed the packet", mark)
		}
	}
}

func TestSetupRejectsMalformedPackets(t *testing.T) {
	good := setupPacket(binary.BigEndian, 'B', cookieProtocol, make([]byte, 16))
	for cut := range len(good) {
		if _, err := ReadSetup(bytes.NewReader(good[:cut])); err == nil {
			t.Fatalf("a packet cut at %d bytes was accepted", cut)
		}
	}
	bad := append([]byte(nil), good...)
	bad[0] = 'X'
	if _, err := ReadSetup(bytes.NewReader(bad)); err == nil {
		t.Fatal("unknown byte order accepted")
	}
	// Lengths that would make us buffer 64 KiB are refused before reading.
	huge := make([]byte, 12)
	huge[0] = 'B'
	binary.BigEndian.PutUint16(huge[6:], 0xffff)
	binary.BigEndian.PutUint16(huge[8:], 0xffff)
	reader := &countingReader{}
	if _, err := ReadSetup(io.MultiReader(bytes.NewReader(huge), reader)); err == nil || reader.n != 0 {
		t.Fatalf("giant lengths: err=%v, read %d more bytes", err, reader.n)
	}
}

type countingReader struct{ n int }

func (r *countingReader) Read(p []byte) (int, error) { r.n += len(p); return len(p), nil }

func FuzzReadSetup(f *testing.F) {
	f.Add(setupPacket(binary.BigEndian, 'B', cookieProtocol, make([]byte, 16)))
	f.Add(setupPacket(binary.LittleEndian, 'l', "", nil))
	f.Add([]byte{'l', 0, 11, 0, 0, 0, 255, 255, 255, 255, 0, 0})
	f.Fuzz(func(t *testing.T, data []byte) {
		setup, err := ReadSetup(bytes.NewReader(data))
		if err != nil {
			return
		}
		again, err := ReadSetup(bytes.NewReader(setup.Marshal()))
		if err != nil || again.AuthName != setup.AuthName || !bytes.Equal(again.AuthData, setup.AuthData) {
			t.Fatalf("round trip: %+v -> %+v, %v", setup, again, err)
		}
	})
}

func FuzzParseXauthority(f *testing.F) {
	f.Add(xauthEntry(FamilyLocal, "host", "0", cookieProtocol, make([]byte, 16)))
	f.Add([]byte{0xff, 0xff, 0xff, 0xff, 0xff})
	f.Fuzz(func(t *testing.T, data []byte) {
		entries, _ := ParseXauthority(data)
		Lookup(entries, Display{Network: "unix"}, "host")
	})
}

func FuzzParseDisplay(f *testing.F) {
	for _, seed := range []string{":0", "host:1.2", "[::1]:0", "/tmp/x/org.xquartz:0", "::", ""} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, value string) {
		for _, goos := range []string{"linux", "windows"} {
			if display, err := ParseDisplay(value, goos); err == nil && (display.Network == "" || display.Address == "") {
				t.Fatalf("%q parsed to an empty target %+v", value, display)
			}
		}
	})
}

// --- the hub, over a real SSH connection ---

type pair struct {
	client *ssh.Client
	server *ssh.ServerConn
}

func sshPair(t *testing.T) pair {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	left, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	right, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	_, private, _ := ed25519Key()
	config := &ssh.ServerConfig{NoClientAuth: true}
	config.AddHostKey(private)
	type result struct {
		conn *ssh.ServerConn
		err  error
	}
	done := make(chan result, 1)
	go func() {
		conn, channels, requests, err := ssh.NewServerConn(right, config)
		if err == nil {
			go ssh.DiscardRequests(requests)
			go func() {
				for c := range channels {
					c.Reject(ssh.Prohibited, "")
				}
			}()
		}
		done <- result{conn, err}
	}()
	clientConn, channels, requests, err := ssh.NewClientConn(left, "test", &ssh.ClientConfig{
		User: "u", HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	})
	if err != nil {
		t.Fatal(err)
	}
	server := <-done
	if server.err != nil {
		t.Fatal(server.err)
	}
	client := ssh.NewClient(clientConn, channels, requests)
	t.Cleanup(func() { client.Close(); server.conn.Close() })
	return pair{client, server.conn}
}

// fakeXServer records the setup packet of each connection and answers it.
type fakeXServer struct {
	listener net.Listener
	setups   chan Setup
}

func startFakeXServer(t *testing.T) *fakeXServer {
	t.Helper()
	listener, err := net.Listen("unix", filepath.Join(t.TempDir(), "X7"))
	if err != nil {
		t.Skipf("no unix sockets: %v", err)
	}
	server := &fakeXServer{listener: listener, setups: make(chan Setup, 8)}
	t.Cleanup(func() { listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				setup, err := ReadSetup(conn)
				if err != nil {
					return
				}
				server.setups <- setup
				conn.Write([]byte("X-ready"))
				io.Copy(conn, conn) // echo requests
			}()
		}
	}()
	return server
}

func startHub(t *testing.T, p pair) *Hub {
	hub := NewHub()
	go hub.Serve(p.client.HandleChannelOpen("x11"))
	return hub
}

func TestChannelWithTheFakeCookieReachesTheDisplayWithTheRealOne(t *testing.T) {
	p := sshPair(t)
	x := startFakeXServer(t)
	hub := startHub(t, p)
	grant, err := hub.registerLocal(Local{
		Display: Display{Network: "unix", Address: x.listener.Addr().String(), Screen: 1},
		Auth:    Auth{Name: cookieProtocol, Data: []byte("real-cookie-16by")},
	})
	if err != nil {
		t.Fatal(err)
	}
	fake, _ := hexDecode(grant.Cookie())
	if len(fake) != 16 || bytes.Equal(fake, []byte("real-cookie-16by")) || grant.Screen() != 1 {
		t.Fatalf("fake cookie %q screen %d", grant.Cookie(), grant.Screen())
	}
	for _, mark := range []byte{'B', 'l'} {
		channel, _, err := p.server.OpenChannel("x11", ssh.Marshal(struct {
			Host string
			Port uint32
		}{"127.0.0.1", 40000}))
		if err != nil {
			t.Fatal(err)
		}
		channel.Write(Setup{Order: mark, Major: 11, AuthName: cookieProtocol, AuthData: fake}.Marshal())
		select {
		case got := <-x.setups:
			if got.Order != mark || got.AuthName != cookieProtocol || string(got.AuthData) != "real-cookie-16by" {
				t.Fatalf("the X server received %+v", got)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("the X server received nothing")
		}
		reply := make([]byte, len("X-ready"))
		if _, err := io.ReadFull(channel, reply); err != nil || string(reply) != "X-ready" {
			t.Fatalf("reply %q err=%v", reply, err)
		}
		// After the setup, bytes pass untouched in both directions.
		channel.Write([]byte("request-bytes"))
		echo := make([]byte, len("request-bytes"))
		if _, err := io.ReadFull(channel, echo); err != nil || string(echo) != "request-bytes" {
			t.Fatalf("echo %q err=%v", echo, err)
		}
		channel.Close()
	}
}

func TestChannelWithAnyOtherCookieIsClosedAndNeverReachesTheDisplay(t *testing.T) {
	p := sshPair(t)
	x := startFakeXServer(t)
	hub := startHub(t, p)
	grant, _ := hub.registerLocal(Local{Display: Display{Network: "unix", Address: x.listener.Addr().String()}})
	released, _ := hub.registerLocal(Local{Display: Display{Network: "unix", Address: x.listener.Addr().String()}})
	staleCookie, _ := hexDecode(released.Cookie())
	released.Release()
	real, _ := hexDecode(grant.Cookie())
	for name, data := range map[string][]byte{
		"guessed":   []byte("0000000000000000"),
		"released":  staleCookie,
		"truncated": real[:15],
		"empty":     nil,
	} {
		channel, _, err := p.server.OpenChannel("x11", nil)
		if err != nil {
			t.Fatal(err)
		}
		channel.Write(Setup{Order: 'B', AuthName: cookieProtocol, AuthData: data}.Marshal())
		if _, err := channel.Read(make([]byte, 1)); err != io.EOF {
			t.Fatalf("%s: read err=%v, want EOF", name, err)
		}
	}
	select {
	case got := <-x.setups:
		t.Fatalf("the X server received %+v", got)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestMoreThanMaxChannelsAreRefused(t *testing.T) {
	p := sshPair(t)
	startHub(t, p)
	var open []ssh.Channel
	defer func() {
		for _, c := range open {
			c.Close()
		}
	}()
	refused := 0
	for range MaxChannels + 3 {
		channel, _, err := p.server.OpenChannel("x11", nil) // sends no setup, so the slot stays taken
		if err != nil {
			if !strings.Contains(err.Error(), "too many") {
				t.Fatal(err)
			}
			refused++
			continue
		}
		open = append(open, channel)
	}
	if len(open) != MaxChannels || refused != 3 {
		t.Fatalf("open=%d refused=%d", len(open), refused)
	}
}

func TestDefaultDisplayFindsTheServerThePlatformRuns(t *testing.T) {
	none := func(string) ([]string, error) { return nil, nil }
	absent := func(string) bool { return false }
	xquartz := func(string) ([]string, error) {
		return []string{"/private/tmp/com.apple.launchd.b/org.xquartz:1", "/private/tmp/com.apple.launchd.a/org.xquartz:0"}, nil
	}
	if got := DefaultDisplay("windows", none, absent); got != ":0" {
		t.Errorf("windows = %q", got)
	}
	if got := DefaultDisplay("darwin", xquartz, absent); got != "/private/tmp/com.apple.launchd.a/org.xquartz:0" {
		t.Errorf("darwin with XQuartz = %q", got)
	}
	if got := DefaultDisplay("darwin", none, absent); got != "" {
		t.Errorf("darwin without XQuartz = %q", got)
	}
	if got := DefaultDisplay("linux", none, func(path string) bool { return path == "/tmp/.X11-unix/X0" }); got != ":0" {
		t.Errorf("linux with display 0 = %q", got)
	}
	if got := DefaultDisplay("linux", none, absent); got != "" {
		t.Errorf("linux without X = %q", got)
	}
}

func TestRegisterFailsWhenNothingListensOnTheDisplay(t *testing.T) {
	_, err := NewHub().Register(Environ{Display: "/nonexistent/org.xquartz:0", GOOS: "darwin"})
	var unreachable *UnreachableError
	if !errors.As(err, &unreachable) || unreachable.Address != "/nonexistent/org.xquartz:0" {
		t.Fatalf("err = %v", err)
	}
}
