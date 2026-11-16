package sshconn

import (
	"context"
	"errors"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func TestListenContextCancellationBoundsRequestsAndClosesLateListener(t *testing.T) {
	requests := make(chan *ssh.Request, 8)
	server := startServer(t, func(server *testServer) {
		server.password = "pw"
		server.request = func(_ *ssh.ServerConn, request *ssh.Request) { requests <- request }
		server.command = func(_ string, channel ssh.Channel) {
			_, _ = io.WriteString(channel, "still connected")
			_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
			_ = channel.Close()
		}
	})
	harness := dial(t, server, nil)
	harness.phase(PhaseHostKeyPrompt)
	harness.conn.TrustHostKey(false)
	harness.phase(PhasePasswordPrompt)
	harness.conn.SubmitPassword("pw")
	harness.phase(PhaseConnected)
	shared, err := harness.conn.Share(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer shared.Cancel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var inspections atomic.Int32
	completed := make(chan error, 1)
	go func() {
		listener, err := shared.ListenContext(ctx, "tcp", "127.0.0.1:0", func(net.Listener) error {
			inspections.Add(1)
			return nil
		})
		if listener != nil {
			_ = listener.Close()
		}
		completed <- err
	}()
	request := nextListenRequest(t, requests, "tcpip-forward")
	cancel()
	select {
	case err := <-completed:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled listener error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("listener cancellation waited for the server")
	}
	for range 3 {
		retryCtx, retryCancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		listener, err := harness.conn.ListenContext(retryCtx, "tcp", "127.0.0.1:0", nil)
		retryCancel()
		if listener != nil || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("retry while the canceled request is pending = %v/%v", listener, err)
		}
	}
	select {
	case request := <-requests:
		t.Fatalf("retry queued an extra request: %s", request.Type)
	default:
	}
	if err := request.Reply(true, ssh.Marshal(struct{ Port uint32 }{43217})); err != nil {
		t.Fatal(err)
	}
	cleanup := nextListenRequest(t, requests, "cancel-tcpip-forward")
	if inspections.Load() != 0 {
		t.Fatal("canceled listener request ran its inspection after returning")
	}
	if err := cleanup.Reply(true, nil); err != nil {
		t.Fatal(err)
	}
	go func() {
		listener, err := harness.conn.ListenContext(context.Background(), "tcp", "127.0.0.1:0", nil)
		if listener != nil {
			_ = listener.Close()
		}
		completed <- err
	}()
	nextListenRequest(t, requests, "tcpip-forward").Reply(false, nil)
	select {
	case err := <-completed:
		if err == nil {
			t.Fatal("refused listener succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("late listener cleanup did not release the next request")
	}
	shared.Cancel()
	output, err := harness.conn.RunCommand(context.Background(), "probe", 1024)
	if err != nil || string(output) != "still connected" {
		t.Fatalf("listener cancellation affected the user's SSH connection: %q/%v", output, err)
	}
}

func nextListenRequest(t *testing.T, requests <-chan *ssh.Request, kind string) *ssh.Request {
	t.Helper()
	select {
	case request := <-requests:
		if request.Type != kind {
			t.Fatalf("request = %s, want %s", request.Type, kind)
		}
		return request
	case <-time.After(time.Second):
		t.Fatalf("did not receive %s", kind)
		return nil
	}
}

func TestListenContextStopsWaitingWhenItsShareCloses(t *testing.T) {
	requests := make(chan *ssh.Request, 1)
	server := startServer(t, func(server *testServer) {
		server.password = "pw"
		server.request = func(_ *ssh.ServerConn, request *ssh.Request) { requests <- request }
	})
	harness := dial(t, server, nil)
	harness.phase(PhaseHostKeyPrompt)
	harness.conn.TrustHostKey(false)
	harness.phase(PhasePasswordPrompt)
	harness.conn.SubmitPassword("pw")
	harness.phase(PhaseConnected)
	shared, err := harness.conn.Share(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer shared.Cancel()
	completed := make(chan error, 1)
	go func() {
		_, err := shared.ListenContext(context.Background(), "tcp", "127.0.0.1:0", nil)
		completed <- err
	}()
	nextListenRequest(t, requests, "tcpip-forward")
	shared.Cancel()
	select {
	case err := <-completed:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("closed share listener error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("closing the share did not cancel the listener wait")
	}
}

func TestListenContextPublishesRejectionBeforeCleanupAndBoundsRetries(t *testing.T) {
	requests := make(chan *ssh.Request, 8)
	server := startServer(t, func(server *testServer) {
		server.password = "pw"
		server.request = func(_ *ssh.ServerConn, request *ssh.Request) { requests <- request }
	})
	harness := dial(t, server, nil)
	harness.phase(PhaseHostKeyPrompt)
	harness.conn.TrustHostKey(false)
	harness.phase(PhasePasswordPrompt)
	harness.conn.SubmitPassword("pw")
	harness.phase(PhaseConnected)
	rejected := errors.New("listener failed inspection")
	completed := make(chan error, 1)
	go func() {
		listener, err := harness.conn.ListenContext(context.Background(), "tcp", "127.0.0.1:0", func(net.Listener) error {
			return rejected
		})
		if listener != nil {
			_ = listener.Close()
		}
		completed <- err
	}()
	request := nextListenRequest(t, requests, "tcpip-forward")
	if err := request.Reply(true, ssh.Marshal(struct{ Port uint32 }{43219})); err != nil {
		t.Fatal(err)
	}
	cleanup := nextListenRequest(t, requests, "cancel-tcpip-forward")
	select {
	case err := <-completed:
		if !errors.Is(err, rejected) {
			t.Fatalf("listener rejection = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("listener rejection waited for the server to acknowledge cleanup")
	}
	for range 3 {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		listener, err := harness.conn.ListenContext(ctx, "tcp", "127.0.0.1:0", nil)
		cancel()
		if listener != nil || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("retry during cleanup = %v/%v", listener, err)
		}
	}
	select {
	case request := <-requests:
		t.Fatalf("retry queued an extra SSH request during cleanup: %s", request.Type)
	default:
	}
	if err := cleanup.Reply(true, nil); err != nil {
		t.Fatal(err)
	}
}
