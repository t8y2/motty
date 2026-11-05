package workbench

import (
	"bufio"
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"motty/internal/sshconn"
)

func TestLocalProxyListenerInspection(t *testing.T) {
	for _, test := range []struct {
		name   string
		output string
		want   bool
	}{
		{"private", "LISTEN 0 128 127.0.0.1:43217 0.0.0.0:*\n", true},
		{"wildcard", "LISTEN 0 128 0.0.0.0:43217 0.0.0.0:*\n", false},
		{"mixed", "LISTEN 0 128 127.0.0.1:43217 0.0.0.0:*\nLISTEN 0 128 [::]:43217 [::]:*\n", false},
		{"empty", "", false},
		{"other port", "LISTEN 0 128 127.0.0.1:43218 0.0.0.0:*\n", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := localProxyPrivateBindings(test.output, 43217); got != test.want {
				t.Fatalf("private bindings = %t, want %t", got, test.want)
			}
		})
	}
}

func TestLocalProxyTunnelClosesAnUnverifiedRemoteListener(t *testing.T) {
	proxyPort, closeProxy := localProxyTestPort(t)
	defer closeProxy()
	var listener net.Listener
	var commands []string
	connection := &fakeConn{
		listen: func(network, address string) (net.Listener, error) {
			var err error
			listener, err = net.Listen(network, address)
			return listener, err
		},
		exec: func(_ context.Context, command string, _ int) (sshconn.ExecResult, error) {
			commands = append(commands, command)
			return sshconn.ExecResult{Output: []byte("LISTEN 0 128 0.0.0.0:43217 0.0.0.0:*\n")}, nil
		},
	}
	forward, err := startLocalProxyTunnel(context.Background(), connection, proxyPort, nil)
	if forward != nil {
		forward.Close()
	}
	if !errors.Is(err, errLocalProxyBinding) {
		t.Fatalf("start error = %v, want an unverified binding", err)
	}
	if listener == nil {
		t.Fatal("remote listener was never opened")
	}
	if client, dialErr := net.DialTimeout("tcp", listener.Addr().String(), 100*time.Millisecond); dialErr == nil {
		_ = client.Close()
		t.Fatal("unverified remote listener remained open")
	}
	if len(commands) != 1 || !strings.HasPrefix(commands[0], "ss -H -ltn ") || strings.Contains(commands[0], "sudo") {
		t.Fatalf("remote inspection commands = %q", commands)
	}
}

func TestLocalProxyTunnelRejectsAStaleLocalPortWithoutOpeningTheRemoteListener(t *testing.T) {
	port, closeProxy := localProxyTestPort(t)
	closeProxy()
	var listens int
	connection := &fakeConn{
		listen: func(network, address string) (net.Listener, error) {
			listens++
			return net.Listen(network, address)
		},
	}
	_, err := startLocalProxyTunnel(context.Background(), connection, port, nil)
	if !errors.Is(err, errLocalProxyProbe) || listens != 0 {
		t.Fatalf("stale port error = %v, remote listens = %d", err, listens)
	}
}

func TestLocalProxyTunnelReportsRefusedRemoteForwarding(t *testing.T) {
	port, closeProxy := localProxyTestPort(t)
	defer closeProxy()
	connection := &fakeConn{
		listen: func(string, string) (net.Listener, error) {
			return nil, errors.New("administratively prohibited")
		},
	}
	_, err := startLocalProxyTunnel(context.Background(), connection, port, nil)
	if !errors.Is(err, errLocalProxyForwarding) || errors.Is(err, errLocalProxyBinding) {
		t.Fatalf("refused forwarding error = %v", err)
	}
}

func TestLocalProxyTunnelForwardsAndCloses(t *testing.T) {
	proxy, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer proxy.Close()
	go func() {
		for {
			connection, err := proxy.Accept()
			if err != nil {
				return
			}
			go func() {
				defer connection.Close()
				_, _ = io.Copy(connection, connection)
			}()
		}
	}()
	_, rawPort, _ := net.SplitHostPort(proxy.Addr().String())
	port, _ := strconv.ParseUint(rawPort, 10, 16)
	var listener net.Listener
	connection := &fakeConn{
		listen: func(network, address string) (net.Listener, error) {
			listener, err = net.Listen(network, address)
			return listener, err
		},
		exec: func(context.Context, string, int) (sshconn.ExecResult, error) {
			return sshconn.ExecResult{Output: []byte(fmt.Sprintf("LISTEN 0 128 %s 0.0.0.0:*\n", listener.Addr()))}, nil
		},
	}
	forward, err := startLocalProxyTunnel(context.Background(), connection, uint16(port), nil)
	if err != nil {
		t.Fatal(err)
	}
	client, err := net.DialTimeout("tcp", forward.Status().ListenAddress, time.Second)
	if err != nil {
		forward.Close()
		t.Fatal(err)
	}
	_ = client.SetDeadline(time.Now().Add(time.Second))
	_, err = client.Write([]byte("test"))
	var reply [4]byte
	if err == nil {
		_, err = io.ReadFull(client, reply[:])
	}
	_ = client.Close()
	forward.Close()
	if err != nil || string(reply[:]) != "test" {
		t.Fatalf("forwarded reply = %q, %v", reply, err)
	}
	if _, err := net.DialTimeout("tcp", forward.Status().ListenAddress, time.Second); err == nil {
		t.Fatal("listener remained open after close")
	}
}

func TestLocalProxyConnectionTestTraversesTheRemoteEndpoint(t *testing.T) {
	target := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodHead {
			response.WriteHeader(http.StatusOK)
			return
		}
		response.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	roots := x509.NewCertPool()
	roots.AddCert(target.Certificate())
	for _, padding := range []int{0, localProxyTestLimit - 128} {
		t.Run(fmt.Sprintf("connect_padding=%d", padding), func(t *testing.T) {
			proxyAddress := startTestConnectProxy(t, target.Listener.Addr().String(), false, "X-Padding: "+strings.Repeat("a", padding)+"\r\n")
			remotePort, dialed, stop := startTestTunnelEndpoint(t, proxyAddress)
			defer stop()
			if err := testLocalProxyTunnelWithRoots(context.Background(), dialed, remotePort, roots); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestLocalProxyConnectionTestBoundsConnectHeaders(t *testing.T) {
	for _, header := range []string{
		"X-Large: " + strings.Repeat("a", 2*localProxyTestLimit) + "\r\n",
		strings.Repeat("X-Repeated: "+strings.Repeat("a", 1024)+"\r\n", 128),
	} {
		client, server := net.Pipe()
		stream := &localProxyCountingConn{Conn: client}
		connection := &fakeConn{dial: func(context.Context, string, string) (net.Conn, error) {
			return stream, nil
		}}
		done := make(chan struct{})
		go func() {
			defer close(done)
			defer server.Close()
			if _, err := http.ReadRequest(bufio.NewReader(server)); err != nil {
				return
			}
			_, _ = io.WriteString(server, "HTTP/1.1 502 Bad Gateway\r\n"+header+"\r\n")
		}()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		err := testLocalProxyTunnelWithRoots(ctx, connection, 43217, nil)
		cancel()
		<-done
		if !errors.Is(err, errLocalProxyTunnel) {
			t.Fatalf("oversized CONNECT response = %v", err)
		}
		if stream.bytesRead > localProxyTestLimit {
			t.Fatalf("CONNECT response consumed %d bytes, limit %d", stream.bytesRead, localProxyTestLimit)
		}
	}
}

type localProxyCountingConn struct {
	net.Conn
	bytesRead int
}

func (conn *localProxyCountingConn) Read(data []byte) (int, error) {
	count, err := conn.Conn.Read(data)
	conn.bytesRead += count
	return count, err
}

func TestLocalProxyConnectionTestDoesNotRequireStreamDeadlines(t *testing.T) {
	target := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodHead {
			response.WriteHeader(http.StatusOK)
		}
	}))
	defer target.Close()

	proxyAddress := startTestConnectProxy(t, target.Listener.Addr().String(), false)
	remotePort, dialed, stop := startTestTunnelEndpoint(t, proxyAddress)
	defer stop()
	dial := dialed.dial
	dialed.dial = func(ctx context.Context, network, address string) (net.Conn, error) {
		connection, err := dial(ctx, network, address)
		if err != nil {
			return nil, err
		}
		return deadlineUnsupportedConn{Conn: connection}, nil
	}

	roots := x509.NewCertPool()
	roots.AddCert(target.Certificate())
	if err := testLocalProxyTunnelWithRoots(context.Background(), dialed, remotePort, roots); err != nil {
		t.Fatal(err)
	}
}

func TestLocalProxyConnectionTestReportsAuthenticationWithoutChangingState(t *testing.T) {
	proxyAddress := startTestConnectProxy(t, "", true)
	remotePort, dialed, stop := startTestTunnelEndpoint(t, proxyAddress)
	defer stop()
	err := testLocalProxyTunnelWithRoots(context.Background(), dialed, remotePort, nil)
	if !errors.Is(err, errLocalProxyAuth) {
		t.Fatalf("test error = %v, want authentication unsupported", err)
	}
}

func TestLocalProxyConnectionTestReportsUpstreamRefusal(t *testing.T) {
	proxyAddress := startTestConnectProxy(t, "127.0.0.1:1", false)
	remotePort, dialed, stop := startTestTunnelEndpoint(t, proxyAddress)
	defer stop()
	err := testLocalProxyTunnelWithRoots(context.Background(), dialed, remotePort, nil)
	if !errors.Is(err, errLocalProxyTunnel) || errors.Is(err, errLocalProxyAuth) {
		t.Fatalf("test error = %v, want an upstream tunnel failure", err)
	}
}

func TestLocalProxyConnectionTestRequiresTheRemoteEntry(t *testing.T) {
	proxyPort, closeProxy := localProxyTestPort(t)
	defer closeProxy()
	dialed := &fakeConn{
		dial: func(context.Context, string, string) (net.Conn, error) {
			return nil, errors.New("remote endpoint unavailable")
		},
	}
	err := testLocalProxyTunnel(context.Background(), dialed, proxyPort)
	if !errors.Is(err, errLocalProxyTunnel) {
		t.Fatalf("test error = %v, want the local proxy not to satisfy the test", err)
	}
	if len(dialed.dialCalls) != 1 || !strings.HasSuffix(dialed.dialCalls[0].address, ":"+strconv.Itoa(int(proxyPort))) {
		t.Fatalf("test dialed %+v, want only the remote entry", dialed.dialCalls)
	}
}

func startTestConnectProxy(t *testing.T, destination string, auth bool, headers ...string) string {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			client, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer client.Close()
				reader := bufio.NewReader(client)
				request, err := http.ReadRequest(reader)
				if err != nil {
					return
				}
				if auth {
					_, _ = io.WriteString(client, "HTTP/1.1 407 Proxy Authentication Required\r\nContent-Length: 0\r\n\r\n")
					return
				}
				if request.Method != http.MethodConnect {
					_, _ = io.WriteString(client, "HTTP/1.1 400 Bad Request\r\nContent-Length: 0\r\n\r\n")
					return
				}
				remote, err := net.Dial("tcp", destination)
				if err != nil {
					_, _ = io.WriteString(client, "HTTP/1.1 502 Bad Gateway\r\nContent-Length: 0\r\n\r\n")
					return
				}
				defer remote.Close()
				_, _ = io.WriteString(client, "HTTP/1.1 200 Connection Established\r\n"+strings.Join(headers, "")+"\r\n")
				done := make(chan struct{}, 1)
				go func() {
					_, _ = io.Copy(remote, reader)
					done <- struct{}{}
				}()
				_, _ = io.Copy(client, remote)
				<-done
			}()
		}
	}()
	return listener.Addr().String()
}

// startTestTunnelEndpoint models the server's loopback listener. A direct
// stream is handed back to the test so the test must enter through it.
func startTestTunnelEndpoint(t *testing.T, proxyAddress string) (uint16, *fakeConn, func()) {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, rawPort, _ := net.SplitHostPort(listener.Addr().String())
	port, _ := strconv.ParseUint(rawPort, 10, 16)
	var streams []net.Conn
	var mu sync.Mutex
	closed := make(chan struct{})
	go func() {
		for {
			remote, err := listener.Accept()
			if err != nil {
				return
			}
			local, err := net.Dial("tcp", proxyAddress)
			if err != nil {
				_ = remote.Close()
				continue
			}
			mu.Lock()
			streams = append(streams, remote, local)
			mu.Unlock()
			go func() {
				defer local.Close()
				_, _ = io.Copy(local, remote)
			}()
			go func() {
				defer remote.Close()
				_, _ = io.Copy(remote, local)
			}()
		}
	}()
	dialed := &fakeConn{
		dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			if strings.TrimSpace(address) != proxyAddress && address != listener.Addr().String() {
				t.Errorf("test entered %q, want the remote endpoint %q", address, listener.Addr().String())
			}
			return net.Dial("tcp", listener.Addr().String())
		},
	}
	stop := func() {
		_ = listener.Close()
		mu.Lock()
		for _, stream := range streams {
			_ = stream.Close()
		}
		clear(streams)
		mu.Unlock()
		select {
		case <-closed:
		default:
			close(closed)
		}
	}
	return uint16(port), dialed, stop
}

type deadlineUnsupportedConn struct {
	net.Conn
}

func (deadlineUnsupportedConn) SetDeadline(time.Time) error {
	return errors.New("ssh: tcpChan: deadline not supported")
}
