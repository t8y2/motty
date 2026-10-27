package sshconn

import (
	"encoding/binary"
	"encoding/hex"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"motty/internal/x11fwd"
)

// xauthFile is a one-entry Xauthority file for MIT-MAGIC-COOKIE-1.
func xauthFile(hostname, number string, cookie []byte) []byte {
	out := binary.BigEndian.AppendUint16(nil, x11fwd.FamilyLocal)
	for _, field := range [][]byte{[]byte(hostname), []byte(number), []byte("MIT-MAGIC-COOKIE-1"), cookie} {
		out = binary.BigEndian.AppendUint16(out, uint16(len(field)))
		out = append(out, field...)
	}
	return out
}

// localX starts a stand-in X server on a Unix socket and returns the
// environment that finds it, plus the cookie it will be sent.
func localX(t *testing.T) (env x11fwd.Environ, setups <-chan x11fwd.Setup) {
	t.Helper()
	dir, err := os.MkdirTemp("", "motty-x11-")
	if err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(dir, "X9:9")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		os.RemoveAll(dir)
		t.Skipf("no unix sockets: %v", err)
	}
	t.Cleanup(func() {
		listener.Close()
		os.RemoveAll(dir)
	})
	received := make(chan x11fwd.Setup, 4)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				setup, err := x11fwd.ReadSetup(conn)
				if err != nil {
					return
				}
				received <- setup
				conn.Write([]byte("hello from X"))
			}()
		}
	}()
	file := xauthFile("testhost", "9", []byte("the-real-cookie!"))
	env = x11fwd.Environ{
		// The absolute form names the socket itself, as XQuartz's launchd path does.
		Display: socket, Hostname: "testhost", GOOS: "linux", Xauthority: "/test/xauthority",
		ReadFile: func(string) ([]byte, error) { return file, nil },
	}
	return env, received
}

func TestX11ChannelOfTheShellReachesTheLocalDisplayWithItsRealCookie(t *testing.T) {
	env, setups := localX(t)
	server := startServer(t, func(server *testServer) {
		server.password = "secret"
		server.x11 = true
		server.shell = echoShell
	})
	h := connect(t, server)
	w := openShellWith(t, h, ShellConfig{Cols: 80, Rows: 24, X11: &env})
	if note := w.shell.X11Note(); note != "" {
		t.Fatalf("X11 not active: %s", note)
	}
	w.until("welcome", func(output string) bool { return len(output) > 0 })
	server.mu.Lock()
	shell, serverConn := server.lastShell, server.lastConn
	server.mu.Unlock()
	if shell == nil || shell.x11 == nil || shell.x11.SingleConnection || shell.x11.AuthProtocol != "MIT-MAGIC-COOKIE-1" {
		t.Fatalf("x11-req = %+v", shell)
	}
	fake, err := hex.DecodeString(shell.x11.AuthCookie)
	if err != nil || len(fake) != 16 || string(fake) == "the-real-cookie!" {
		t.Fatalf("cookie %q: %v", shell.x11.AuthCookie, err)
	}
	channel, _, err := serverConn.OpenChannel("x11", ssh.Marshal(struct {
		Host string
		Port uint32
	}{"127.0.0.1", 1234}))
	if err != nil {
		t.Fatal(err)
	}
	defer channel.Close()
	channel.Write(x11fwd.Setup{Order: 'l', Major: 11, AuthName: shell.x11.AuthProtocol, AuthData: fake}.Marshal())
	select {
	case got := <-setups:
		if got.AuthName != "MIT-MAGIC-COOKIE-1" || string(got.AuthData) != "the-real-cookie!" {
			t.Fatalf("the display received %+v", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the display received nothing")
	}
	reply := make([]byte, len("hello from X"))
	if _, err := io.ReadFull(channel, reply); err != nil || string(reply) != "hello from X" {
		t.Fatalf("reply %q, %v", reply, err)
	}
}

func TestShellOpensWithAReasonWhenX11CannotBeForwarded(t *testing.T) {
	cases := map[string]struct {
		env    x11fwd.Environ
		refuse bool
	}{
		"no display": {env: x11fwd.Environ{GOOS: "linux"}},
		"refused":    {env: x11fwd.Environ{Display: ":0", GOOS: "linux"}, refuse: true},
	}
	for name, c := range cases {
		server := startServer(t, func(server *testServer) {
			server.password = "secret"
			server.x11, server.refuseX11 = true, c.refuse
			server.shell = echoShell
		})
		h := connect(t, server)
		env := c.env
		w := openShellWith(t, h, ShellConfig{Cols: 80, Rows: 24, X11: &env})
		if w.shell.X11Note() == "" {
			t.Errorf("%s: no reason given", name)
		}
		w.until("the shell greets", func(output string) bool { return len(output) > 0 })
	}
}
