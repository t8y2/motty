package sshconn

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func TestReliabilityConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{name: "disabled"},
		{name: "dial timeout", config: Config{DialTimeout: time.Second}},
		{name: "keepalive", config: Config{KeepAlive: KeepAliveConfig{Interval: time.Second, Timeout: time.Second}}},
		{name: "negative dial timeout", config: Config{DialTimeout: -time.Second}, wantErr: true},
		{name: "negative interval", config: Config{KeepAlive: KeepAliveConfig{Interval: -time.Second}}, wantErr: true},
		{name: "negative keepalive timeout", config: Config{KeepAlive: KeepAliveConfig{Timeout: -time.Second}}, wantErr: true},
		{name: "timeout without interval", config: Config{KeepAlive: KeepAliveConfig{Timeout: time.Second}}, wantErr: true},
		{name: "interval without timeout", config: Config{KeepAlive: KeepAliveConfig{Interval: time.Second}}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.config.validate(); (err != nil) != test.wantErr {
				t.Fatalf("validate() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}

func TestInvalidReliabilityConfigFailsBeforeConnecting(t *testing.T) {
	server := startServer(t, func(server *testServer) { server.password = "secret" })
	harness := dial(t, server, func(config *Config) {
		config.KeepAlive.Interval = time.Second
	})
	status := harness.phase(PhaseFailed)
	if status.Err == nil || status.Endpoint.Host != "" {
		t.Fatalf("status = %+v, want config failure before an endpoint starts", status)
	}
	if server.activeConnections() != 0 {
		t.Fatal("invalid config reached the server")
	}
}

func TestKeepAliveDisabledStartsNoWorkerOrRequests(t *testing.T) {
	requests := make(chan string, 1)
	server := startServer(t, func(server *testServer) {
		server.password = "secret"
		server.request = func(_ *ssh.ServerConn, request *ssh.Request) {
			requests <- request.Type
			_ = request.Reply(false, nil)
		}
	})
	harness := connect(t, server)
	if worker := harness.conn.startKeepAlive(nil); worker != nil {
		t.Fatal("disabled keepalive returned a worker completion channel")
	}
	select {
	case request := <-requests:
		t.Fatalf("disabled keepalive sent %q", request)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestKeepAliveSuccessKeepsConnectionHealthy(t *testing.T) {
	requests := make(chan string, 4)
	server := startServer(t, func(server *testServer) {
		server.password = "secret"
		server.request = func(_ *ssh.ServerConn, request *ssh.Request) {
			requests <- request.Type
			_ = request.Reply(false, nil)
		}
	})
	harness := connectWithKeepAlive(t, server, 5*time.Millisecond, 100*time.Millisecond)
	for range 2 {
		select {
		case request := <-requests:
			if request != keepAliveRequest {
				t.Fatalf("request = %q, want %q", request, keepAliveRequest)
			}
		case <-time.After(time.Second):
			t.Fatal("keepalive request was not sent")
		}
	}
	status := harness.conn.Status()
	if status.Phase != PhaseConnected || status.Err != nil {
		t.Fatalf("status after successful probes = %+v", status)
	}
}

func TestKeepAliveTimeoutReportsDisconnected(t *testing.T) {
	requestSeen := make(chan struct{}, 1)
	server := startServer(t, func(server *testServer) {
		server.password = "secret"
		server.request = func(_ *ssh.ServerConn, _ *ssh.Request) {
			requestSeen <- struct{}{}
		}
	})
	harness := connectWithKeepAlive(t, server, 5*time.Millisecond, 20*time.Millisecond)
	select {
	case <-requestSeen:
	case <-time.After(time.Second):
		t.Fatal("keepalive request was not sent")
	}
	status := harness.phase(PhaseClosed)
	if !errors.Is(status.Err, ErrDisconnected) || !errors.Is(status.Err, ErrKeepAliveTimeout) {
		t.Fatalf("error = %v, want disconnected keepalive timeout", status.Err)
	}
	select {
	case <-harness.conn.Done():
	case <-time.After(time.Second):
		t.Fatal("timed-out connection did not release its worker")
	}
}

func TestKeepAliveSendFailureReportsDisconnected(t *testing.T) {
	server := startServer(t, func(server *testServer) {
		server.password = "secret"
		server.request = func(connection *ssh.ServerConn, _ *ssh.Request) {
			_ = connection.Close()
		}
	})
	harness := connectWithKeepAlive(t, server, 5*time.Millisecond, 100*time.Millisecond)
	status := harness.phase(PhaseClosed)
	if !errors.Is(status.Err, ErrDisconnected) {
		t.Fatalf("error = %v, want ErrDisconnected", status.Err)
	}
	if errors.Is(status.Err, ErrKeepAliveTimeout) {
		t.Fatalf("closed server reported a timeout: %v", status.Err)
	}
}

func TestRemoteDisconnectStopsLongIntervalKeepAlive(t *testing.T) {
	connections := make(chan *ssh.ServerConn, 1)
	server := startServer(t, func(server *testServer) {
		server.password = "secret"
		server.connected = func(connection *ssh.ServerConn) {
			connections <- connection
		}
	})
	harness := connectWithKeepAlive(t, server, time.Hour, time.Second)
	connection := <-connections
	started := time.Now()
	if err := connection.Close(); err != nil {
		t.Fatal(err)
	}
	status := harness.phase(PhaseClosed)
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("disconnect took %v with a sleeping keepalive worker", elapsed)
	}
	if !errors.Is(status.Err, ErrDisconnected) || errors.Is(status.Err, ErrKeepAliveTimeout) {
		t.Fatalf("error = %v, want non-timeout disconnect", status.Err)
	}
}

func TestCloseStopsKeepAliveImmediately(t *testing.T) {
	requestSeen := make(chan struct{}, 8)
	var count atomic.Uint32
	server := startServer(t, func(server *testServer) {
		server.password = "secret"
		server.request = func(_ *ssh.ServerConn, request *ssh.Request) {
			count.Add(1)
			requestSeen <- struct{}{}
			_ = request.Reply(false, nil)
		}
	})
	harness := connectWithKeepAlive(t, server, 2*time.Millisecond, 100*time.Millisecond)
	select {
	case <-requestSeen:
	case <-time.After(time.Second):
		t.Fatal("keepalive request was not sent")
	}
	harness.conn.Close()
	afterClose := count.Load()
	time.Sleep(20 * time.Millisecond)
	if got := count.Load(); got != afterClose {
		t.Fatalf("keepalive requests after Close = %d", got-afterClose)
	}
	status := harness.conn.Status()
	if status.Phase != PhaseClosed || status.Err != nil {
		t.Fatalf("status after Close = %+v", status)
	}
}

func TestDialTimeoutAppliesThroughProxyJump(t *testing.T) {
	target := startServer(t, func(server *testServer) { server.password = "target-secret" })
	jump := startServer(t, func(server *testServer) {
		server.password = "jump-secret"
		server.blockDirect = true
	})
	jumpHost, jumpPort := jump.host()
	harness := dial(t, target, func(config *Config) {
		config.ProxyJumps = []Endpoint{{Host: jumpHost, Port: jumpPort, User: "jump-user"}}
		config.DialTimeout = 20 * time.Millisecond
	})
	harness.phase(PhaseHostKeyPrompt)
	harness.conn.TrustHostKey(false)
	harness.phase(PhasePasswordPrompt)
	harness.conn.SubmitPassword("jump-secret")
	status := harness.phase(PhaseFailed)
	if !errors.Is(status.Err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want context deadline exceeded", status.Err)
	}
}

func TestPingNotConnected(t *testing.T) {
	conn := &Conn{}
	if _, err := conn.Ping(context.Background()); err == nil {
		t.Fatal("expected error when pinging disconnected conn")
	}
}

func TestPingConnected(t *testing.T) {
	var requestType string
	var wantReply bool
	server := startServer(t, func(server *testServer) {
		server.password = "secret"
		server.request = func(_ *ssh.ServerConn, request *ssh.Request) {
			requestType, wantReply = request.Type, request.WantReply
			_ = request.Reply(true, nil)
		}
	})
	harness := connect(t, server)
	rtt, err := harness.conn.Ping(context.Background())
	if err != nil {
		t.Fatalf("Ping failed: %v", err)
	}
	if rtt <= 0 {
		t.Fatalf("expected positive rtt, got %v", rtt)
	}
	if requestType != keepAliveRequest || !wantReply {
		t.Fatalf("Ping sent %q (want reply %v)", requestType, wantReply)
	}
	if nilRTT, err := harness.conn.Ping(nil); err != nil || nilRTT <= 0 {
		t.Fatalf("Ping(nil) = %v, %v", nilRTT, err)
	}
}

func TestPingTimesOutWhenServerDoesNotReply(t *testing.T) {
	server := startServer(t, func(server *testServer) {
		server.password = "secret"
		server.request = func(_ *ssh.ServerConn, _ *ssh.Request) {}
	})
	harness := connect(t, server)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := harness.conn.Ping(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Ping error = %v, want deadline exceeded", err)
	}
}

// A probe that timed out is still waiting for its reply inside the SSH client,
// and later global requests queue behind it. Ping must not send another until
// the first is answered, and must measure afresh once it has been.
func TestPingSendsNothingWhileAnEarlierProbeIsUnanswered(t *testing.T) {
	var received atomic.Int32
	held := make(chan *ssh.Request, 4)
	server := startServer(t, func(server *testServer) {
		server.password = "secret"
		server.request = func(_ *ssh.ServerConn, request *ssh.Request) {
			received.Add(1)
			held <- request
		}
	})
	harness := connect(t, server)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := harness.conn.Ping(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("first Ping error = %v, want deadline exceeded", err)
	}
	for range 5 {
		if _, err := harness.conn.Ping(context.Background()); !errors.Is(err, ErrPingPending) {
			t.Fatalf("Ping while unanswered = %v, want ErrPingPending", err)
		}
	}
	if got := received.Load(); got != 1 {
		t.Fatalf("server received %d keepalive requests, want 1", got)
	}

	if err := (<-held).Reply(true, nil); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(5 * time.Second); harness.conn.pingPending.Load(); {
		if time.Now().After(deadline) {
			t.Fatal("the answered probe never released Ping")
		}
		time.Sleep(time.Millisecond)
	}
	go func() { _ = (<-held).Reply(true, nil) }()
	if rtt, err := harness.conn.Ping(context.Background()); err != nil || rtt <= 0 {
		t.Fatalf("Ping after the reply = %v, %v", rtt, err)
	}
}

func BenchmarkKeepAliveDisabled(b *testing.B) {
	conn := Conn{}
	b.ReportAllocs()
	for b.Loop() {
		if worker := conn.startKeepAlive(nil); worker != nil {
			b.Fatal("disabled keepalive started a worker")
		}
	}
}

func connectWithKeepAlive(t *testing.T, server *testServer, interval, timeout time.Duration) *harness {
	t.Helper()
	harness := dial(t, server, func(config *Config) {
		config.KeepAlive = KeepAliveConfig{Interval: interval, Timeout: timeout}
	})
	harness.phase(PhaseHostKeyPrompt)
	harness.conn.TrustHostKey(false)
	harness.phase(PhasePasswordPrompt)
	harness.conn.SubmitPassword("secret")
	harness.phase(PhaseConnected)
	return harness
}
