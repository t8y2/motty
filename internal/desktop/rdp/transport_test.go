package rdp

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"
)

// deadlineConn records the deadlines it is given.
type deadlineConn struct {
	net.Conn
	deadline time.Time
	closed   bool
}

func (c *deadlineConn) SetDeadline(t time.Time) error {
	c.deadline = t
	return nil
}

func (c *deadlineConn) Close() error {
	c.closed = true
	return c.Conn.Close()
}

// The first handshake shares one window with the password prompt, and the
// prompt's time is not taken from it. A connection the library makes later on
// its own, such as following the redirection of GNOME Remote Desktop's remote
// login, gets a window of its own: left with the first window's deadline it
// would be cut off when that expires, soon after the session moved. Once a
// session is up there is no deadline: the library's heartbeat watches the link.
func TestEachHandshakeGetsItsOwnWindow(t *testing.T) {
	var mu sync.Mutex
	var conns []*deadlineConn
	dial := func(context.Context, string, string) (net.Conn, error) {
		client, server := net.Pipe()
		server.Close()
		conn := &deadlineConn{Conn: client}
		mu.Lock()
		conns = append(conns, conn)
		mu.Unlock()
		return conn, nil
	}
	last := func() *deadlineConn {
		mu.Lock()
		defer mu.Unlock()
		return conns[len(conns)-1]
	}
	// aWindow says the deadline is one handshakeTimeout from now, give or take
	// the time the check itself takes.
	aWindow := func(what string) time.Time {
		t.Helper()
		until := time.Until(last().deadline)
		if until > handshakeTimeout || until < handshakeTimeout-2*time.Second {
			t.Fatalf("%s: deadline in %v, want about %v", what, until, handshakeTimeout)
		}
		return last().deadline
	}

	transport := newSessionTransport(dial, "example:3389")
	redial := func(what string) time.Time {
		if _, err := transport.dialConn(context.Background(), "tcp", "example:3389"); err != nil {
			t.Fatal(err)
		}
		return aWindow(what)
	}
	first := redial("the first handshake")

	// The password prompt suspends the window and restarts it when answered.
	resume := transport.suspend()
	if !last().deadline.IsZero() {
		t.Fatal("the handshake was still bounded while the user was asked")
	}
	time.Sleep(10 * time.Millisecond)
	resume()
	if restarted := aWindow("the answered prompt").Sub(first); restarted < 5*time.Millisecond {
		t.Errorf("the window moved by %v, want the time the prompt took", restarted)
	}

	transport.connected()
	if !last().deadline.IsZero() {
		t.Fatal("the open session still has a handshake deadline")
	}

	// A redirection or reconnection well into the session: a window of its
	// own, not what is left of the first one.
	time.Sleep(10 * time.Millisecond)
	if moved := redial("the reconnection").Sub(first); moved < 5*time.Millisecond {
		t.Errorf("the reconnection kept the first window (moved by %v)", moved)
	}
	transport.connected()
	if !last().deadline.IsZero() {
		t.Fatal("the redirected session still has a handshake deadline")
	}

	transport.close()
	if !last().closed {
		t.Fatal("close left the library's connection open")
	}
}
