package rdp

import (
	"errors"
	"log/slog"
	"net"
	"testing"
	"time"

	"motty/internal/rdpcore/protocol/tpkt"
)

// newHeartbeatClient is a client whose connection is a pipe: nothing arrives
// from it unless a test says so, and every PDU it writes is handed to packets.
func newHeartbeatClient(t *testing.T, timeout time.Duration) (*Client, <-chan []byte) {
	t.Helper()
	server, conn := net.Pipe()
	t.Cleanup(func() {
		conn.Close()
		server.Close()
	})
	log := slog.New(slog.DiscardHandler)
	client := &Client{
		opts:          &Options{HeartbeatTimeout: timeout},
		log:           log,
		done:          make(chan struct{}),
		tpktConn:      tpkt.NewConn(conn, log),
		userChannelID: 1002,
		ioChannelID:   1003,
		framebuf:      NewFramebuffer(64, 64),
	}
	client.setState(StateConnected)
	packets := make(chan []byte, 16)
	go func() {
		defer close(packets)
		buf := make([]byte, 4096)
		for {
			n, err := server.Read(buf)
			if err != nil {
				return
			}
			packets <- append([]byte(nil), buf[:n]...)
		}
	}()
	return client, packets
}

func expectPacket(t *testing.T, packets <-chan []byte, what string) {
	t.Helper()
	select {
	case _, ok := <-packets:
		if !ok {
			t.Fatalf("%s: the connection was closed", what)
		}
	case <-time.After(time.Second):
		t.Fatalf("%s: no PDU was written", what)
	}
}

// A hidden desktop tells the server not to send picture updates, so the server
// may go silent indefinitely; that silence must not end the connection. Once
// the picture is visible again the heartbeat measures silence again, and the
// repaint the server then owes proves the link.
func TestHeartbeatWaitsWhileOutputIsSuppressed(t *testing.T) {
	client, _ := newHeartbeatClient(t, 500*time.Millisecond)
	ended := make(chan error, 1)
	client.OnDisconnect(func(err error) { ended <- err })

	if err := client.SetOutputSuppressed(true); err != nil {
		t.Fatalf("suppress output: %v", err)
	}
	// The server has not been heard from since long before it was suppressed.
	client.lastReceived.Store(time.Now().Add(-time.Hour).UnixNano())
	go client.heartbeatLoop()

	select {
	case err := <-ended:
		t.Fatalf("silence while output was suppressed ended the connection: %v", err)
	case <-time.After(2 * time.Second): // several heartbeat intervals
	}

	if err := client.SetOutputSuppressed(false); err != nil {
		t.Fatalf("resume output: %v", err)
	}
	select {
	case err := <-ended:
		t.Fatalf("resuming output ended the connection before the server could repaint: %v", err)
	case <-time.After(200 * time.Millisecond):
	}

	select {
	case err := <-ended:
		if !errors.Is(err, ErrHeartbeatTimeout) {
			t.Fatalf("silence after output resumed ended the connection with %v, want a heartbeat timeout", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("silence after output resumed did not end the connection")
	}
}

// The keepalive exists to measure the link, and a suppressed server must not
// answer it: sending one would be work with nothing to prove.
func TestKeepAlivePausesWhileOutputIsSuppressed(t *testing.T) {
	client, packets := newHeartbeatClient(t, time.Hour)

	if err := client.SetOutputSuppressed(true); err != nil {
		t.Fatalf("suppress output: %v", err)
	}
	expectPacket(t, packets, "suppress output")

	client.sendKeepAlive()
	select {
	case got := <-packets:
		t.Fatalf("a suppressed connection still sent %d bytes of keepalive", len(got))
	case <-time.After(100 * time.Millisecond):
	}

	if err := client.SetOutputSuppressed(false); err != nil {
		t.Fatalf("resume output: %v", err)
	}
	expectPacket(t, packets, "resume output")

	client.sendKeepAlive()
	expectPacket(t, packets, "keepalive after output resumed")
}
