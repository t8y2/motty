package portforward

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestForwardCopiesBothDirectionsAndStops(t *testing.T) {
	target := echoServer(t)
	var notifications atomic.Int64
	forward, err := Start(Config{
		ListenAddress: "127.0.0.1:0",
		Destination:   target.Addr().String(),
		Dial:          (&net.Dialer{}).DialContext,
		Notify:        func() { notifications.Add(1) },
	})
	if err != nil {
		t.Fatal(err)
	}
	client, err := net.Dial("tcp", forward.Status().ListenAddress)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(client, "forward me"); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, len("FORWARD ME"))
	if _, err := io.ReadFull(client, buffer); err != nil {
		t.Fatal(err)
	}
	if string(buffer) != "FORWARD ME" {
		t.Fatalf("echo = %q", buffer)
	}
	_ = client.Close()
	waitFor(t, func(status Status) bool {
		return status.Active == 0 && status.Accepted == 1 && status.Uploaded == 10 && status.Downloaded == 10
	}, forward)
	forward.Close()
	select {
	case <-forward.Done():
	default:
		t.Fatal("Done stayed open after Close")
	}
	if !forward.Status().Closed || notifications.Load() == 0 {
		t.Fatalf("status=%+v notifications=%d", forward.Status(), notifications.Load())
	}
	if connection, err := net.DialTimeout("tcp", forward.Status().ListenAddress, 100*time.Millisecond); err == nil {
		connection.Close()
		t.Fatal("closed forward still accepted a connection")
	}
}

func TestRemoteForwardUsesProvidedListenerAndLocalDestination(t *testing.T) {
	target, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = target.Close() })
	go func() {
		connection, acceptErr := target.Accept()
		if acceptErr != nil {
			return
		}
		defer connection.Close()
		buffer := make([]byte, len("download"))
		if _, readErr := io.ReadFull(connection, buffer); readErr == nil {
			_, _ = io.WriteString(connection, "UP")
		}
	}()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	requested := make(chan string, 1)
	forward, err := Start(Config{
		ListenAddress: "127.0.0.1:43210",
		Destination:   target.Addr().String(),
		Mode:          ModeRemote,
		Listen: func(network, address string) (net.Listener, error) {
			if network != "tcp" {
				t.Errorf("network = %q", network)
			}
			requested <- address
			return listener, nil
		},
		Dial: (&net.Dialer{}).DialContext,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := <-requested; got != "127.0.0.1:43210" {
		t.Fatalf("listen address = %q", got)
	}
	client, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(client, "download"); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, len("UP"))
	if _, err := io.ReadFull(client, buffer); err != nil {
		t.Fatal(err)
	}
	if string(buffer) != "UP" {
		t.Fatalf("reply = %q", buffer)
	}
	_ = client.Close()
	waitFor(t, func(status Status) bool {
		return status.Active == 0 && status.Accepted == 1 && status.Uploaded == 2 && status.Downloaded == 8
	}, forward)
	forward.Close()
	if connection, err := net.DialTimeout("tcp", listener.Addr().String(), 100*time.Millisecond); err == nil {
		connection.Close()
		t.Fatal("closed remote forward still accepted a connection")
	}
}

func TestForwardReportsRemoteDialFailureAndKeepsListening(t *testing.T) {
	var attempts atomic.Int64
	forward, err := Start(Config{
		ListenAddress: "127.0.0.1:0",
		Destination:   "example.invalid:80",
		Dial: func(context.Context, string, string) (net.Conn, error) {
			if attempts.Add(1) == 1 {
				return nil, errors.New("remote refused")
			}
			client, server := net.Pipe()
			go func() {
				defer server.Close()
				_, _ = io.Copy(io.Discard, server)
			}()
			return client, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer forward.Close()
	connection, err := net.Dial("tcp", forward.Status().ListenAddress)
	if err != nil {
		t.Fatal(err)
	}
	_ = connection.Close()
	waitFor(t, func(status Status) bool {
		return status.Active == 0 && status.Accepted == 1 && status.LastError != nil
	}, forward)
	if !strings.Contains(forward.Status().LastError.Error(), "remote refused") {
		t.Fatalf("last error = %v", forward.Status().LastError)
	}
	recovered, err := net.Dial("tcp", forward.Status().ListenAddress)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func(status Status) bool {
		return status.Accepted == 2 && status.LastError == nil
	}, forward)
	_ = recovered.Close()
}

func TestForwardRejectsUnsafeOrInvalidConfiguration(t *testing.T) {
	tests := []struct {
		name   string
		config Config
	}{
		{name: "no dialer", config: Config{ListenAddress: "127.0.0.1:0", Destination: "example.com:80"}},
		{name: "wildcard", config: Config{ListenAddress: "0.0.0.0:8080", Destination: "example.com:80", Dial: (&net.Dialer{}).DialContext}},
		{name: "hostname listener", config: Config{ListenAddress: "localhost:8080", Destination: "example.com:80", Dial: (&net.Dialer{}).DialContext}},
		{name: "missing destination port", config: Config{ListenAddress: "127.0.0.1:0", Destination: "example.com", Dial: (&net.Dialer{}).DialContext}},
		{name: "zero destination port", config: Config{ListenAddress: "127.0.0.1:0", Destination: "example.com:0", Dial: (&net.Dialer{}).DialContext}},
		{name: "SOCKS5 fixed destination", config: Config{ListenAddress: "127.0.0.1:0", Destination: "example.com:80", Mode: ModeSOCKS5, Dial: (&net.Dialer{}).DialContext}},
		{name: "HTTP CONNECT fixed destination", config: Config{ListenAddress: "127.0.0.1:0", Destination: "example.com:80", Mode: ModeHTTPConnect, Dial: (&net.Dialer{}).DialContext}},
		{name: "remote missing listener", config: Config{ListenAddress: "127.0.0.1:8080", Destination: "127.0.0.1:80", Mode: ModeRemote, Dial: (&net.Dialer{}).DialContext}},
		{name: "remote wildcard listener", config: Config{ListenAddress: "0.0.0.0:8080", Destination: "127.0.0.1:80", Mode: ModeRemote, Listen: net.Listen, Dial: (&net.Dialer{}).DialContext}},
		{name: "remote missing destination", config: Config{ListenAddress: "127.0.0.1:8080", Mode: ModeRemote, Listen: net.Listen, Dial: (&net.Dialer{}).DialContext}},
		{name: "unknown mode", config: Config{ListenAddress: "127.0.0.1:0", Mode: Mode(99), Dial: (&net.Dialer{}).DialContext}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if forward, err := Start(test.config); err == nil {
				forward.Close()
				t.Fatal("invalid configuration was accepted")
			}
		})
	}
}

func TestSOCKS5ConfigurationDoesNotNeedDestination(t *testing.T) {
	forward, err := Start(Config{ListenAddress: "127.0.0.1:0", Mode: ModeSOCKS5, Dial: (&net.Dialer{}).DialContext})
	if err != nil {
		t.Fatal(err)
	}
	forward.Close()
}

func TestHTTPConnectConfigurationDoesNotNeedDestination(t *testing.T) {
	forward, err := Start(Config{ListenAddress: "127.0.0.1:0", Mode: ModeHTTPConnect, Dial: (&net.Dialer{}).DialContext})
	if err != nil {
		t.Fatal(err)
	}
	forward.Close()
}

func TestForwardRejectsStreamsBeyondItsLimit(t *testing.T) {
	dialed := make(chan struct{})
	release := make(chan struct{})
	forward, err := Start(Config{
		ListenAddress: "127.0.0.1:0",
		Destination:   "example.invalid:80",
		MaxStreams:    1,
		Dial: func(context.Context, string, string) (net.Conn, error) {
			close(dialed)
			<-release
			near, far := net.Pipe()
			go func() {
				defer far.Close()
				_, _ = io.Copy(io.Discard, far)
			}()
			return near, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		close(release)
		forward.Close()
	}()

	first, err := net.Dial("tcp", forward.Status().ListenAddress)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	<-dialed

	second, err := net.Dial("tcp", forward.Status().ListenAddress)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	_ = second.SetReadDeadline(time.Now().Add(time.Second))
	var buffer [1]byte
	if _, err := second.Read(buffer[:]); err == nil {
		t.Fatal("a stream beyond the limit stayed open")
	}
	if status := forward.Status(); status.Active != 1 || status.Accepted != 1 {
		t.Fatalf("limited status = %+v", status)
	}
}

func TestCloseCancelsRemoteDial(t *testing.T) {
	started := make(chan struct{})
	forward, err := Start(Config{
		ListenAddress: "127.0.0.1:0",
		Destination:   "example.invalid:80",
		Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
			close(started)
			<-ctx.Done()
			return nil, ctx.Err()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	connection, err := net.Dial("tcp", forward.Status().ListenAddress)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	<-started
	closed := make(chan struct{})
	go func() {
		forward.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("Close did not cancel the remote dial")
	}
}

func TestForwardUsesRemoteCloseWriteCapability(t *testing.T) {
	target := echoServer(t)
	var remote *recordingTCPConn
	forward, err := Start(Config{
		ListenAddress: "127.0.0.1:0",
		Destination:   target.Addr().String(),
		Dial: func(context.Context, string, string) (net.Conn, error) {
			connection, err := net.Dial("tcp", target.Addr().String())
			if err != nil {
				return nil, err
			}
			remote = &recordingTCPConn{TCPConn: connection.(*net.TCPConn)}
			return remote, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer forward.Close()
	client, err := net.Dial("tcp", forward.Status().ListenAddress)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(client, "finish"); err != nil {
		t.Fatal(err)
	}
	response := make([]byte, len("FINISH"))
	if _, err := io.ReadFull(client, response); err != nil {
		t.Fatal(err)
	}
	_ = client.Close()
	waitFor(t, func(status Status) bool { return status.Active == 0 }, forward)
	if remote == nil || !remote.closeWriteCalled.Load() {
		t.Fatal("remote CloseWrite was not called")
	}
}

func BenchmarkStatus(b *testing.B) {
	target := echoServer(b)
	forward, err := Start(Config{ListenAddress: "127.0.0.1:0", Destination: target.Addr().String(), Dial: (&net.Dialer{}).DialContext})
	if err != nil {
		b.Fatal(err)
	}
	defer forward.Close()
	b.ReportAllocs()
	for b.Loop() {
		_ = forward.Status()
	}
}

type recordingTCPConn struct {
	*net.TCPConn
	closeWriteCalled atomic.Bool
}

func (connection *recordingTCPConn) CloseWrite() error {
	connection.closeWriteCalled.Store(true)
	return connection.TCPConn.CloseWrite()
}

func echoServer(t testing.TB) net.Listener {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	go func() {
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer connection.Close()
				buffer := make([]byte, 4096)
				for {
					count, err := connection.Read(buffer)
					if count > 0 {
						_, _ = connection.Write([]byte(strings.ToUpper(string(buffer[:count]))))
					}
					if err != nil {
						return
					}
				}
			}()
		}
	}()
	return listener
}

func waitFor(t testing.TB, ready func(Status) bool, forward *Forward) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if ready(forward.Status()) {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("forward status = %+v", forward.Status())
}

// stuckConn is a stream over a dead connection: closing it waits for the
// network, which is not coming.
type stuckConn struct {
	net.Conn
	release chan struct{}
}

func (conn *stuckConn) Close() error {
	<-conn.release
	return conn.Conn.Close()
}

func TestCloseAsyncFreesThePortWithoutWaitingForStuckStreams(t *testing.T) {
	release := make(chan struct{})
	opened := make(chan struct{})
	forward, err := Start(Config{
		ListenAddress: "127.0.0.1:0",
		Destination:   "example.invalid:80",
		Dial: func(context.Context, string, string) (net.Conn, error) {
			near, far := net.Pipe()
			t.Cleanup(func() { far.Close() })
			close(opened)
			return &stuckConn{Conn: near, release: release}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	address := forward.Status().ListenAddress
	client, err := net.Dial("tcp", address)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	<-opened

	returned := make(chan struct{})
	go func() {
		forward.CloseAsync()
		close(returned)
	}()
	select {
	case <-returned:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("CloseAsync waited for a stuck stream")
	}
	again, err := net.Listen("tcp", address)
	if err != nil {
		close(release)
		t.Fatalf("the listen address was not free when CloseAsync returned: %v", err)
	}
	again.Close()

	close(release)
	select {
	case <-forward.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("the forward never finished closing once its stream let go")
	}
}

type delayedCloseListener struct {
	net.Listener
	release <-chan struct{}
}

func (listener *delayedCloseListener) Close() error {
	<-listener.release
	return listener.Listener.Close()
}

func TestRemoteCloseAsyncDoesNotWaitForTheServer(t *testing.T) {
	release := make(chan struct{})
	opened := make(chan struct{})
	destinationClosed := make(chan struct{})
	forward, err := Start(Config{
		Mode: ModeRemote, ListenAddress: "127.0.0.1:0", Destination: "127.0.0.1:7897",
		Listen: func(network, address string) (net.Listener, error) {
			listener, err := net.Listen(network, address)
			if err != nil {
				return nil, err
			}
			return &delayedCloseListener{Listener: listener, release: release}, nil
		},
		Dial: func(context.Context, string, string) (net.Conn, error) {
			near, far := net.Pipe()
			t.Cleanup(func() { _ = far.Close() })
			go func() {
				_, _ = far.Read(make([]byte, 1))
				close(destinationClosed)
			}()
			close(opened)
			return near, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	client, err := net.Dial("tcp", forward.Status().ListenAddress)
	if err != nil {
		close(release)
		forward.Close()
		t.Fatal(err)
	}
	defer client.Close()
	<-opened
	returned := make(chan struct{})
	go func() {
		forward.CloseAsync()
		close(returned)
	}()
	select {
	case <-returned:
	case <-time.After(200 * time.Millisecond):
		close(release)
		<-returned
		forward.Close()
		t.Fatal("closing a remote forward waited for the server")
	}
	select {
	case <-destinationClosed:
	case <-time.After(200 * time.Millisecond):
		close(release)
		forward.Close()
		t.Fatal("an existing proxy stream stayed open while the server delayed cancellation")
	}
	close(release)
	forward.Close()
}
