package workbench

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"motty/internal/portforward"
	"motty/internal/sshconn"
)

func TestForwardDialogValidatesConfiguration(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(*State)
		want    func(*State) string
	}{
		{
			name:    "name required",
			prepare: func(state *State) { replaceForwardField(state, hitForwardDialogLabel, "") },
			want:    func(state *State) string { return state.messages.ErrForwardNameRequired },
		},
		{
			name:    "host required",
			prepare: func(state *State) { state.forwardEditor.hostID = 9999 },
			want:    func(state *State) string { return state.messages.ErrForwardHostRequired },
		},
		{
			name:    "local port invalid",
			prepare: func(state *State) { replaceForwardField(state, hitForwardDialogLocalPort, "0") },
			want:    func(state *State) string { return state.messages.ErrForwardPortInvalid },
		},
		{
			name:    "destination port invalid",
			prepare: func(state *State) { replaceForwardField(state, hitForwardDialogDestinationPort, "65536") },
			want:    func(state *State) string { return state.messages.ErrForwardPortInvalid },
		},
		{
			name:    "destination required",
			prepare: func(state *State) { replaceForwardField(state, hitForwardDialogDestination, "bad host") },
			want:    func(state *State) string { return state.messages.ErrForwardDestinationRequired },
		},
		{
			name: "name duplicate",
			prepare: func(state *State) {
				state.forwards = append(state.forwards, forwardRecord{Label: "Web", LocalPort: 9000})
			},
			want: func(state *State) string { return state.messages.ErrForwardNameTaken },
		},
		{
			name: "local port duplicate",
			prepare: func(state *State) {
				state.forwards = append(state.forwards, forwardRecord{Label: "Other", LocalPort: 8080})
			},
			want: func(state *State) string { return state.messages.ErrForwardLocalTaken },
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state := newTestState(t, testOptions(t))
			openForwardEditor(t, state)
			replaceForwardField(state, hitForwardDialogLabel, "Web")
			replaceForwardField(state, hitForwardDialogDestination, "internal.example")
			test.prepare(state)
			if got, want := state.forwardEditor.validate(state), test.want(state); got != want {
				t.Fatalf("validation = %q, want %q", got, want)
			}
		})
	}
}

func TestForwardDialogPersistsWithoutStarting(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	addForwardRule(t, state, "Web", freePort(t), "web.internal", 8080)
	if len(state.forwards) != 1 || state.forwards[0].runner != nil || state.forwards[0].pending {
		t.Fatalf("created forward = %+v", state.forwards)
	}

	restarted := newTestState(t, options)
	if len(restarted.forwards) != 1 {
		t.Fatalf("restored forwards = %+v", restarted.forwards)
	}
	restored := restarted.forwards[0]
	if restored.Label != "Web" || restored.Mode != portforward.ModeLocal || restored.DestinationHost != "web.internal" || restored.DestinationPort != 8080 || restored.runner != nil || restored.pending {
		t.Fatalf("restored forward = %+v", restored)
	}
	if restored.cells[4] != restarted.messages.ForwardStopped {
		t.Fatalf("restored status = %q", restored.cells[4])
	}
}

func TestSOCKS5ForwardPersistsAndUsesSSHRemoteDial(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	fake := connect(t, state, world)
	fake.set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseConnected })
	state.Wake()
	target := forwardEchoServer(t)
	fake.dial = func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", target.Addr().String())
	}
	localPort := freePort(t)
	addSOCKS5ForwardRule(t, state, "Browser Proxy", localPort)

	forward := &state.forwards[0]
	if forward.Mode != portforward.ModeSOCKS5 || forward.DestinationHost != "" || forward.DestinationPort != 0 || forward.cells[3] != state.messages.ForwardModeSOCKS5 {
		t.Fatalf("SOCKS5 forward = %+v", *forward)
	}
	state.Click(hitForwardStartStop)
	if forward.runner == nil {
		t.Fatal("SOCKS5 forward did not start")
	}
	client, err := net.Dial("tcp", forward.runner.Status().ListenAddress)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	writeForwardTestBytes(t, client, []byte{5, 1, 0})
	readForwardTestBytes(t, client, []byte{5, 0})
	domain := []byte("echo.internal")
	request := []byte{5, 1, 0, 3, byte(len(domain))}
	request = append(request, domain...)
	request = binary.BigEndian.AppendUint16(request, 4321)
	writeForwardTestBytes(t, client, request)
	reply := make([]byte, 10)
	if _, err := io.ReadFull(client, reply); err != nil {
		t.Fatal(err)
	}
	if reply[1] != 0 {
		t.Fatalf("SOCKS5 reply = %v", reply)
	}
	if _, err := io.WriteString(client, "through ssh"); err != nil {
		t.Fatal(err)
	}
	readForwardTestBytes(t, client, []byte("THROUGH SSH"))

	fake.mu.Lock()
	if len(fake.dialCalls) != 1 || fake.dialCalls[0] != (fakeDialCall{network: "tcp", address: "echo.internal:4321"}) {
		t.Fatalf("remote dial calls = %+v", fake.dialCalls)
	}
	fake.mu.Unlock()
	state.Click(hitForwardStartStop)

	restarted := newTestState(t, options)
	if len(restarted.forwards) != 1 || restarted.forwards[0].Mode != portforward.ModeSOCKS5 || restarted.forwards[0].cells[3] != restarted.messages.ForwardModeSOCKS5 {
		t.Fatalf("restored SOCKS5 forwards = %+v", restarted.forwards)
	}
}

func TestHTTPConnectForwardPersistsAndUsesSSHRemoteDial(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	fake := connect(t, state, world)
	fake.set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseConnected })
	state.Wake()
	target := forwardEchoServer(t)
	fake.dial = func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", target.Addr().String())
	}
	localPort := freePort(t)
	addHTTPConnectForwardRule(t, state, "HTTP Proxy", localPort)

	forward := &state.forwards[0]
	if forward.Mode != portforward.ModeHTTPConnect || forward.DestinationHost != "" || forward.DestinationPort != 0 || forward.cells[3] != state.messages.ForwardModeHTTPConnect {
		t.Fatalf("HTTP CONNECT forward = %+v", *forward)
	}
	state.Click(hitForwardStartStop)
	if forward.runner == nil {
		t.Fatal("HTTP CONNECT forward did not start")
	}
	client, err := net.Dial("tcp", forward.runner.Status().ListenAddress)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	destination := "echo.remote.test:8443"
	request := "CONNECT " + destination + " HTTP/1.1\r\nHost: " + destination + "\r\n\r\nthrough ssh"
	if _, err := io.WriteString(client, request); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(client)
	readForwardHTTPConnectResponse(t, reader, "HTTP/1.1 200 Connection Established")
	readForwardTestBytes(t, reader, []byte("THROUGH SSH"))

	fake.mu.Lock()
	if len(fake.dialCalls) != 1 || fake.dialCalls[0] != (fakeDialCall{network: "tcp", address: destination}) {
		t.Fatalf("remote dial calls = %+v", fake.dialCalls)
	}
	fake.mu.Unlock()
	state.Click(hitForwardStartStop)

	restarted := newTestState(t, options)
	if len(restarted.forwards) != 1 || restarted.forwards[0].Mode != portforward.ModeHTTPConnect || restarted.forwards[0].cells[3] != restarted.messages.ForwardModeHTTPConnect {
		t.Fatalf("restored HTTP CONNECT forwards = %+v", restarted.forwards)
	}
}

func TestRemoteForwardPersistsAndBridgesServerListenerToLocalTarget(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	fake := connect(t, state, world)
	fake.set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseConnected })
	state.Wake()

	target := forwardEchoServer(t)
	targetPort := uint16(target.Addr().(*net.TCPAddr).Port)
	remotePort := freePort(t)
	fake.listen = net.Listen
	addRemoteForwardRule(t, state, "Remote Web", remotePort, "127.0.0.1", targetPort)

	forward := &state.forwards[0]
	if forward.Mode != portforward.ModeRemote || forward.DestinationHost != "127.0.0.1" || forward.DestinationPort != targetPort {
		t.Fatalf("remote forward = %+v", *forward)
	}
	if got, want := forward.cells[2], fmt.Sprintf(state.messages.ForwardRemoteListenerFmt, forward.listenAddress()); got != want {
		t.Fatalf("remote listener cell = %q, want %q", got, want)
	}
	restarted := newTestState(t, options)
	if len(restarted.forwards) != 1 || restarted.forwards[0].Mode != portforward.ModeRemote || restarted.forwards[0].runner != nil || restarted.forwards[0].starting {
		t.Fatalf("restored remote forwards = %+v", restarted.forwards)
	}

	state.Click(hitForwardStartStop)
	waitForward(t, state, func() bool { return forward.runner != nil })
	fake.mu.Lock()
	listenCalls := append([]fakeListenCall(nil), fake.listenCalls...)
	dialCalls := append([]fakeDialCall(nil), fake.dialCalls...)
	fake.mu.Unlock()
	if len(listenCalls) != 1 || listenCalls[0] != (fakeListenCall{network: "tcp", address: forward.listenAddress()}) {
		t.Fatalf("remote listen calls = %+v", listenCalls)
	}
	if len(dialCalls) != 0 {
		t.Fatalf("remote forward used SSH dial = %+v", dialCalls)
	}

	client, err := net.Dial("tcp", forward.listenAddress())
	if err != nil {
		t.Fatal(err)
	}
	writeForwardTestBytes(t, client, []byte("through remote ssh"))
	readForwardTestBytes(t, client, []byte("THROUGH REMOTE SSH"))
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	waitForward(t, state, func() bool {
		status := forward.runner.Status()
		return status.Accepted == 1 && status.Active == 0 && status.Uploaded > 0 && status.Downloaded > 0
	})

	state.Click(hitForwardStartStop)
	if forward.runner != nil || forward.starting || forward.pending {
		t.Fatalf("stopped remote forward = %+v", *forward)
	}
}

func TestRemoteForwardStartDoesNotBlockAndStopClosesLateListener(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	fake := connect(t, state, world)
	fake.set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseConnected })
	state.Wake()
	target := forwardEchoServer(t)
	remotePort := freePort(t)
	addRemoteForwardRule(t, state, "Remote", remotePort, "127.0.0.1", uint16(target.Addr().(*net.TCPAddr).Port))

	entered := make(chan struct{})
	release := make(chan struct{})
	created := make(chan net.Listener, 1)
	fake.listen = func(network, address string) (net.Listener, error) {
		close(entered)
		<-release
		listener, err := net.Listen(network, address)
		if err == nil {
			created <- listener
		}
		return listener, err
	}

	state.Click(hitForwardStartStop)
	forward := &state.forwards[0]
	if !forward.starting || forward.runner != nil {
		t.Fatalf("starting remote forward = %+v", *forward)
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("remote Listen was not started in the background")
	}
	state.Click(hitForwardStartStop)
	if forward.starting || forward.startID != 0 {
		t.Fatalf("stopped pending start = %+v", *forward)
	}
	close(release)
	listener := <-created
	waitForwardResult(t, state)
	deadline := time.Now().Add(time.Second)
	for {
		connection, err := net.DialTimeout("tcp", listener.Addr().String(), 20*time.Millisecond)
		if err != nil {
			break
		}
		connection.Close()
		if time.Now().After(deadline) {
			t.Fatal("late remote listener remained open after stop")
		}
		time.Sleep(time.Millisecond)
	}
	if forward.runner != nil || forward.lastError != "" {
		t.Fatalf("late remote result changed stopped forward = %+v", *forward)
	}
}

func TestRemoteForwardListenFailureCanRetry(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	fake := connect(t, state, world)
	fake.set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseConnected })
	state.Wake()
	target := forwardEchoServer(t)
	addRemoteForwardRule(t, state, "Remote", freePort(t), "127.0.0.1", uint16(target.Addr().(*net.TCPAddr).Port))

	wantErr := errors.New("remote port unavailable")
	attempt := 0
	fake.listen = func(network, address string) (net.Listener, error) {
		attempt++
		if attempt == 1 {
			return nil, wantErr
		}
		return net.Listen(network, address)
	}
	forward := &state.forwards[0]
	state.Click(hitForwardStartStop)
	waitForward(t, state, func() bool { return !forward.starting && forward.lastError != "" })
	if !strings.Contains(forward.lastError, wantErr.Error()) || forward.runner != nil || forward.cells[4] != state.messages.ForwardStoppedError {
		t.Fatalf("failed remote start = %+v", *forward)
	}

	state.Click(hitForwardStartStop)
	waitForward(t, state, func() bool { return forward.runner != nil })
	if forward.lastError != "" || attempt != 2 {
		t.Fatalf("retried remote start = %+v attempts=%d", *forward, attempt)
	}
	state.Click(hitForwardStartStop)
}

func TestRemoteForwardStopDoesNotWaitForServerCancellation(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	fake := connect(t, state, world)
	fake.set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseConnected })
	state.Wake()
	target := forwardEchoServer(t)
	addRemoteForwardRule(t, state, "Remote", freePort(t), "127.0.0.1", uint16(target.Addr().(*net.TCPAddr).Port))

	closeStarted := make(chan struct{})
	releaseClose := make(chan struct{})
	fake.listen = func(network, address string) (net.Listener, error) {
		listener, err := net.Listen(network, address)
		if err != nil {
			return nil, err
		}
		return &delayedCloseListener{Listener: listener, started: closeStarted, release: releaseClose}, nil
	}
	forward := &state.forwards[0]
	state.Click(hitForwardStartStop)
	waitForward(t, state, func() bool { return forward.runner != nil })

	stopped := make(chan struct{})
	go func() {
		state.Click(hitForwardStartStop)
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(100 * time.Millisecond):
		close(releaseClose)
		<-stopped
		t.Fatal("stopping a remote forward blocked on listener cancellation")
	}
	select {
	case <-closeStarted:
	case <-time.After(time.Second):
		close(releaseClose)
		t.Fatal("remote listener close did not start")
	}
	if forward.runner != nil || forward.starting {
		close(releaseClose)
		t.Fatalf("stopped remote forward = %+v", *forward)
	}
	close(releaseClose)
}

func TestForwardDialogModesShowDestinationsAndSupportKeyboard(t *testing.T) {
	state := newTestState(t, testOptions(t))
	openForwardEditor(t, state)
	canvas, _ := paintedTexts(state)
	if !hasHit(canvas, hitForwardModeLocal) || !hasHit(canvas, hitForwardModeRemote) || !hasHit(canvas, hitForwardModeSOCKS5) || !hasHit(canvas, hitForwardModeHTTPConnect) || !hasHit(canvas, hitForwardDialogDestination) {
		t.Fatalf("local forward dialog hits = %+v", canvas.Hits())
	}
	result := state.Key(gfx.KeyEvent{Key: gfx.KeyRight, Target: hitForwardModeLocal})
	if !result.Handled || result.Focus != hitForwardModeRemote || state.forwardEditor.mode != portforward.ModeRemote {
		t.Fatalf("remote keyboard result=%+v mode=%d", result, state.forwardEditor.mode)
	}
	canvas, texts := paintedTexts(state)
	if !hasHit(canvas, hitForwardDialogDestination) || !hasHit(canvas, hitForwardDialogDestinationPort) || !contains(texts, state.messages.ForwardFieldRemotePort) {
		t.Fatalf("remote dialog texts=%v hits=%+v", texts, canvas.Hits())
	}
	result = state.Key(gfx.KeyEvent{Key: gfx.KeyRight, Target: hitForwardModeRemote})
	if !result.Handled || result.Focus != hitForwardModeSOCKS5 || state.forwardEditor.mode != portforward.ModeSOCKS5 {
		t.Fatalf("SOCKS5 keyboard result=%+v mode=%d", result, state.forwardEditor.mode)
	}
	canvas, texts = paintedTexts(state)
	if hasHit(canvas, hitForwardDialogDestination) || hasHit(canvas, hitForwardDialogDestinationPort) || !contains(texts, state.messages.ForwardSOCKS5Hint) {
		t.Fatalf("SOCKS5 dialog texts=%v hits=%+v", texts, canvas.Hits())
	}
	result = state.Key(gfx.KeyEvent{Key: gfx.KeyRight, Target: hitForwardModeSOCKS5})
	if !result.Handled || result.Focus != hitForwardModeHTTPConnect || state.forwardEditor.mode != portforward.ModeHTTPConnect {
		t.Fatalf("HTTP CONNECT keyboard result=%+v mode=%d", result, state.forwardEditor.mode)
	}
	canvas, texts = paintedTexts(state)
	if hasHit(canvas, hitForwardDialogDestination) || hasHit(canvas, hitForwardDialogDestinationPort) || !contains(texts, state.messages.ForwardHTTPConnectHint) {
		t.Fatalf("HTTP CONNECT dialog texts=%v hits=%+v", texts, canvas.Hits())
	}
	replaceForwardField(state, hitForwardDialogLabel, "Proxy")
	replaceForwardField(state, hitForwardDialogLocalPort, fmt.Sprint(freePort(t)))
	if got := state.forwardEditor.validate(state); got != "" {
		t.Fatalf("HTTP CONNECT validation = %q", got)
	}
	result = state.Key(gfx.KeyEvent{Key: gfx.KeyLeft, Target: hitForwardModeHTTPConnect})
	if !result.Handled || result.Focus != hitForwardModeSOCKS5 || state.forwardEditor.mode != portforward.ModeSOCKS5 {
		t.Fatalf("SOCKS5 keyboard result=%+v mode=%d", result, state.forwardEditor.mode)
	}
	result = state.Key(gfx.KeyEvent{Key: gfx.KeyLeft, Target: hitForwardModeSOCKS5})
	if !result.Handled || result.Focus != hitForwardModeRemote || state.forwardEditor.mode != portforward.ModeRemote {
		t.Fatalf("remote keyboard result=%+v mode=%d", result, state.forwardEditor.mode)
	}
	result = state.Key(gfx.KeyEvent{Key: gfx.KeyLeft, Target: hitForwardModeRemote})
	if !result.Handled || result.Focus != hitForwardModeLocal || state.forwardEditor.mode != portforward.ModeLocal {
		t.Fatalf("local keyboard result=%+v mode=%d", result, state.forwardEditor.mode)
	}
}

func TestRemoteForwardDuplicatePortsAreScopedToSSHHost(t *testing.T) {
	tests := []struct {
		name     string
		existing forwardRecord
		want     func(*State) string
	}{
		{
			name:     "same remote host",
			existing: forwardRecord{Label: "Other", Mode: portforward.ModeRemote, HostAddress: sampleHosts[0].Address, HostUser: sampleHosts[0].User, HostPort: sampleHosts[0].Port, LocalPort: 8080},
			want:     func(state *State) string { return state.messages.ErrForwardRemoteTaken },
		},
		{
			name:     "different remote host",
			existing: forwardRecord{Label: "Other", Mode: portforward.ModeRemote, HostAddress: sampleHosts[1].Address, HostUser: sampleHosts[1].User, HostPort: sampleHosts[1].Port, LocalPort: 8080},
			want:     func(*State) string { return "" },
		},
		{
			name:     "local listener",
			existing: forwardRecord{Label: "Other", Mode: portforward.ModeLocal, LocalPort: 8080},
			want:     func(*State) string { return "" },
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state := newTestState(t, testOptions(t))
			openForwardEditor(t, state)
			state.Click(hitForwardModeRemote)
			replaceForwardField(state, hitForwardDialogLabel, "Remote")
			replaceForwardField(state, hitForwardDialogDestination, "127.0.0.1")
			state.forwards = append(state.forwards, test.existing)
			if got, want := state.forwardEditor.validate(state), test.want(state); got != want {
				t.Fatalf("validation = %q, want %q", got, want)
			}
		})
	}
}

func TestRestoreForwardsValidatesModesAndCanonicalizesProxyModes(t *testing.T) {
	state := newTestState(t, testOptions(t))
	host := state.hosts[0]
	state.restoreForwards([]savedForward{
		{
			Label: "Remote", Mode: "remote", HostLabel: host.Label, HostAddress: host.Address,
			HostUser: host.User, HostPort: host.Port, LocalPort: 9000,
			DestinationHost: "127.0.0.1", DestinationPort: 3000,
		},
		{
			Label: "Proxy", Mode: "socks5", HostLabel: host.Label, HostAddress: host.Address,
			HostUser: host.User, HostPort: host.Port, LocalPort: 1080,
			DestinationHost: "stale.example", DestinationPort: 80,
		},
		{
			Label: "HTTP Proxy", Mode: "http-connect", HostLabel: host.Label, HostAddress: host.Address,
			HostUser: host.User, HostPort: host.Port, LocalPort: 8080,
			DestinationHost: "stale.example", DestinationPort: 443,
		},
		{
			Label: "Future", Mode: "future", HostLabel: host.Label, HostAddress: host.Address,
			HostUser: host.User, HostPort: host.Port, LocalPort: 1081,
		},
	})
	if len(state.forwards) != 3 {
		t.Fatalf("restored forwards = %+v", state.forwards)
	}
	remote := state.forwards[0]
	if remote.Mode != portforward.ModeRemote || remote.DestinationHost != "127.0.0.1" || remote.DestinationPort != 3000 {
		t.Fatalf("restored remote forward = %+v", remote)
	}
	for index, mode := range []portforward.Mode{portforward.ModeSOCKS5, portforward.ModeHTTPConnect} {
		forward := state.forwards[index+1]
		if forward.Mode != mode || forward.DestinationHost != "" || forward.DestinationPort != 0 {
			t.Fatalf("canonical proxy forward = %+v", forward)
		}
	}
}

func TestForwardWaitsForSSHStartsAndStopsWithSession(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	addForwardRule(t, state, "Database", freePort(t), "database.internal", 5432)
	state.Click(hitForwardStartStop)

	forward := &state.forwards[0]
	if !forward.pending || forward.runner != nil || len(state.sessions) != 1 || state.activeSection != sectionForwards {
		t.Fatalf("waiting forward=%+v sessions=%d section=%d", *forward, len(state.sessions), state.activeSection)
	}
	fake := world.conns[0]
	fake.set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseConnected })
	state.Wake()
	if forward.pending || forward.runner == nil || forward.sessionID != state.sessions[0].ID {
		t.Fatalf("started forward = %+v", *forward)
	}

	fake.set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseClosed })
	state.Wake()
	if forward.runner != nil || forward.pending || forward.lastError != state.messages.ForwardConnectionClosed {
		t.Fatalf("closed forward = %+v", *forward)
	}
	if forward.cells[4] != state.messages.ForwardStoppedError {
		t.Fatalf("closed status = %q", forward.cells[4])
	}
}

// startedForwardOverReconnectingSession is a forward that resumes after a
// reconnect, running over an established tab whose automatic reconnect setting
// is policy.
func startedForwardOverReconnectingSession(t *testing.T, policy reconnectPolicy) (*State, *world, *fakeConn, *time.Duration, *forwardRecord) {
	t.Helper()
	state, world, connection, clock := reconnectingSession(t, policy)
	addForwardRule(t, state, "Database", freePort(t), "database.internal", 5432)
	forward := &state.forwards[0]
	state.Click(hitForwardEdit)
	state.Click(hitForwardDialogResume)
	state.Click(hitForwardDialogSubmit)
	if state.modal != modalNone || !forward.Resume {
		t.Fatalf("forward after choosing resume: modal=%d forward=%+v", state.modal, *forward)
	}
	state.Click(hitForwardStartStop)
	if forward.runner == nil || forward.sessionID != state.sessions[0].ID {
		t.Fatalf("started forward = %+v", *forward)
	}
	return state, world, connection, clock, forward
}

func TestForwardResumesAfterAutomaticReconnect(t *testing.T) {
	state, world, connection, clock, forward := startedForwardOverReconnectingSession(t, reconnectThree)
	sessionID := forward.sessionID

	disconnectEstablished(connection, state)
	if forward.runner != nil || !forward.pending || forward.sessionID != sessionID || forward.lastError != "" {
		t.Fatalf("waiting forward = %+v", *forward)
	}
	if forward.cells[4] != state.messages.ForwardWaiting {
		t.Fatalf("waiting status = %q", forward.cells[4])
	}

	*clock += time.Second
	state.Wake()
	if len(world.conns) != 2 || !forward.pending || forward.sessionID != sessionID {
		t.Fatalf("automatic dial conns=%d forward=%+v", len(world.conns), *forward)
	}
	world.conns[1].set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseConnected })
	state.Wake()
	if forward.runner == nil || forward.pending || forward.sessionID != sessionID || forward.lastError != "" {
		t.Fatalf("resumed forward = %+v", *forward)
	}
	// The listener is bound again on the same port.
	if got := forward.runner.Status().ListenAddress; got != forward.listenAddress() {
		t.Fatalf("resumed listener = %q, want %q", got, forward.listenAddress())
	}
}

func TestForwardStopsOnLostConnectionWhenAutomaticReconnectIsOff(t *testing.T) {
	state, _, connection, _, forward := startedForwardOverReconnectingSession(t, reconnectOff)
	disconnectEstablished(connection, state)
	if forward.runner != nil || forward.pending || forward.sessionID != 0 || forward.lastError != state.messages.ForwardConnectionClosed {
		t.Fatalf("forward without automatic reconnect = %+v", *forward)
	}
}

func TestForwardStopsWhenAutomaticReconnectGivesUp(t *testing.T) {
	state, world, connection, clock, forward := startedForwardOverReconnectingSession(t, reconnectThree)
	disconnectEstablished(connection, state)
	for _, delay := range []time.Duration{time.Second, 2 * time.Second, 4 * time.Second} {
		if !forward.pending {
			t.Fatalf("forward stopped before the retries ran out: %+v", *forward)
		}
		*clock += delay
		state.Wake()
		world.conns[len(world.conns)-1].set(func(status *sshconn.Status) {
			status.Phase = sshconn.PhaseFailed
			status.Err = errors.New("temporary network failure")
		})
		state.Wake()
	}
	if !state.sessions[0].reconnectExhausted {
		t.Fatal("retries did not run out")
	}
	if forward.runner != nil || forward.pending || forward.lastError != state.messages.ForwardConnectionClosed {
		t.Fatalf("forward after the last retry = %+v", *forward)
	}
	if forward.cells[4] != state.messages.ForwardStoppedError {
		t.Fatalf("status after the last retry = %q", forward.cells[4])
	}
}

func TestForwardWithoutResumeStopsDespiteAutomaticReconnect(t *testing.T) {
	state, world, connection, clock, forward := startedForwardOverReconnectingSession(t, reconnectForever)
	state.Click(hitForwardEdit)
	state.Click(hitForwardDialogResume)
	state.Click(hitForwardDialogSubmit)
	if state.modal != modalNone || forward.Resume {
		t.Fatalf("edited forward modal=%d forward=%+v", state.modal, *forward)
	}
	// Saving stopped it, as any edit does; start it again before the drop.
	state.Click(hitForwardStartStop)
	if forward.runner == nil {
		t.Fatalf("restarted forward = %+v", *forward)
	}

	disconnectEstablished(connection, state)
	if !state.sessions[0].reconnectPending {
		t.Fatal("the tab did not plan a reconnect")
	}
	if forward.runner != nil || forward.pending || forward.lastError != state.messages.ForwardConnectionClosed {
		t.Fatalf("forward without resume = %+v", *forward)
	}
	*clock += time.Second
	state.Wake()
	world.conns[len(world.conns)-1].set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseConnected })
	state.Wake()
	if forward.runner != nil || forward.pending {
		t.Fatalf("forward without resume started after the reconnect: %+v", *forward)
	}
}

func TestForwardResumeChoiceIsOffByDefaultAndPersists(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	addForwardRule(t, state, "Web", freePort(t), "web.internal", 8080)
	addForwardRule(t, state, "Database", freePort(t), "db.internal", 5432)
	if state.forwards[0].Resume || state.forwards[1].Resume {
		t.Fatalf("new forwards = %+v", state.forwards)
	}

	state.selectedForwardID = state.forwards[1].ID
	state.Click(hitForwardEdit)
	if state.forwardEditor.resume {
		t.Fatal("edit dialog shows resume on for a forward without it")
	}
	state.Click(hitForwardDialogResume)
	state.Click(hitForwardDialogSubmit)
	if !state.forwards[1].Resume {
		t.Fatalf("edited forward = %+v", state.forwards[1])
	}

	restarted := newTestState(t, options)
	resume := map[string]bool{}
	for _, forward := range restarted.forwards {
		resume[forward.Label] = forward.Resume
	}
	if len(resume) != 2 || resume["Web"] || !resume["Database"] {
		t.Fatalf("restored resume choices = %v", resume)
	}
	restarted.selectedForwardID = restarted.forwards[1].ID
	restarted.Click(hitForwardEdit)
	if !restarted.forwardEditor.resume {
		t.Fatal("edit dialog did not load the saved resume choice")
	}
}

func TestForwardSavedBeforeTheResumeChoiceDoesNotResume(t *testing.T) {
	state := newTestState(t, testOptions(t))
	host := state.hosts[0]
	state.restoreForwards([]savedForward{{
		ID: 1, Label: "Old", HostLabel: host.Label, HostAddress: host.Address, HostUser: host.User, HostPort: host.Port,
		LocalPort: freePort(t), DestinationHost: "db.internal", DestinationPort: 5432,
	}})
	if len(state.forwards) != 1 || state.forwards[0].Resume {
		t.Fatalf("restored forwards = %+v", state.forwards)
	}
}

func TestWaitingForwardFollowsTheRetryItWaitsFor(t *testing.T) {
	stopped := func(t *testing.T, state *State, forward *forwardRecord) {
		t.Helper()
		if forward.runner != nil || forward.pending || forward.sessionID != 0 || forward.lastError != state.messages.ForwardConnectionClosed {
			t.Fatalf("forward = %+v", *forward)
		}
	}

	t.Run("cancel button", func(t *testing.T) {
		state, _, connection, _, forward := startedForwardOverReconnectingSession(t, reconnectForever)
		disconnectEstablished(connection, state)
		state.Click(hitConnCancelReconnect)
		if state.sessions[0].reconnectPending {
			t.Fatal("cancel did not stop the retry")
		}
		stopped(t, state, forward)
	})

	t.Run("setting turned off", func(t *testing.T) {
		state, _, connection, _, forward := startedForwardOverReconnectingSession(t, reconnectForever)
		disconnectEstablished(connection, state)
		state.setReconnectPolicy(reconnectOff)
		stopped(t, state, forward)
	})

	t.Run("tab closed", func(t *testing.T) {
		state, _, connection, _, forward := startedForwardOverReconnectingSession(t, reconnectForever)
		disconnectEstablished(connection, state)
		state.closeSession(state.sessions[0].ID)
		if forward.runner != nil || forward.pending || forward.sessionID != 0 {
			t.Fatalf("forward of a closed tab = %+v", *forward)
		}
	})

	t.Run("manual reconnect keeps it waiting", func(t *testing.T) {
		state, world, connection, _, forward := startedForwardOverReconnectingSession(t, reconnectForever)
		sessionID := forward.sessionID
		disconnectEstablished(connection, state)
		state.reconnect(&state.sessions[0])
		state.Wake()
		if len(world.conns) != 2 || !forward.pending || forward.sessionID != sessionID {
			t.Fatalf("manual dial conns=%d forward=%+v", len(world.conns), *forward)
		}
		world.conns[1].set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseConnected })
		state.Wake()
		if forward.runner == nil || forward.pending {
			t.Fatalf("forward after manual reconnect = %+v", *forward)
		}
	})
}

func TestForwardCanStopRestartAndRemove(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	fake := connect(t, state, world)
	fake.set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseConnected })
	state.Wake()
	addForwardRule(t, state, "Cache", freePort(t), "cache.internal", 6379)

	state.Click(hitForwardStartStop)
	if state.forwards[0].runner == nil {
		t.Fatal("connected session did not start the forward")
	}
	state.Click(hitForwardStartStop)
	if state.forwards[0].runner != nil || state.forwards[0].lastError != "" {
		t.Fatalf("stopped forward = %+v", state.forwards[0])
	}
	state.Click(hitForwardStartStop)
	if state.forwards[0].runner == nil {
		t.Fatal("forward did not restart")
	}
	state.Click(hitForwardRemove)
	if len(state.forwards) != 0 || state.selectedForwardID != 0 {
		t.Fatalf("remove left forwards=%+v selected=%d", state.forwards, state.selectedForwardID)
	}
	if restarted := newTestState(t, options); len(restarted.forwards) != 0 {
		t.Fatalf("removed forward was restored: %+v", restarted.forwards)
	}
}

func TestForwardPagePaintsRowsDialogAndHostOptions(t *testing.T) {
	state := newTestState(t, testOptions(t))
	addForwardRule(t, state, "Web", freePort(t), "web.internal", 8080)
	canvas, texts := paintedTexts(state)
	if !contains(texts, state.messages.ForwardTitle) || !contains(texts, "Web") || !contains(texts, "web.internal") {
		t.Fatalf("forward page texts = %v", texts)
	}
	if !hasHit(canvas, hitForwardRowList.At(int(state.forwards[0].ID))) || !hasHit(canvas, hitForwardStartStop) || !hasHit(canvas, hitForwardRemove) {
		t.Fatalf("forward page hits = %+v", canvas.Hits())
	}

	state.Click(hitForwardAdd)
	if state.modal != modalNewForward || state.EditorFor(hitForwardDialogLabel) == nil {
		t.Fatalf("forward dialog modal=%d editor=%v", state.modal, state.EditorFor(hitForwardDialogLabel))
	}
	state.Click(hitForwardDialogHost)
	canvas, texts = paintedTexts(state)
	if !contains(texts, state.messages.ForwardDialogTitle) || !hasHit(canvas, hitForwardHostItemList.At(0)) || !hasHit(canvas, hitForwardHostDismiss) {
		t.Fatalf("forward dialog texts=%v hits=%+v", texts, canvas.Hits())
	}
}

// The host picker filters by what identifies a host, not only its label:
// typing an address or an account narrows the list to the host that has it.
func TestForwardHostSelectFiltersByAddressAndChoosesStableHostID(t *testing.T) {
	state := newTestState(t, testOptions(t))
	for index := len(state.hosts); index < 9; index++ {
		host := Host{ID: state.nextHostID, Label: fmt.Sprintf("Host %d", index), Address: fmt.Sprintf("host-%d.example", index), User: "deploy", Port: 22}
		host.finish()
		state.hosts = append(state.hosts, host)
		state.nextHostID++
	}
	openForwardEditor(t, state)
	state.Click(hitForwardDialogHost)
	state.forwardEditor.hostSelect.Query.Insert("host-8")
	choices := state.forwardEditor.hostSelect.Choices()
	if len(choices) != 1 || choices[0].Label != "Host 8" {
		t.Fatalf("address filter = %+v", choices)
	}
	state.Click(hitForwardHostItemList.At(0))
	last := state.hosts[len(state.hosts)-1]
	if state.forwardEditor.hostID != last.ID || state.forwardEditor.hostSelect.Open {
		t.Fatalf("selected host=%d open=%v, want %d closed", state.forwardEditor.hostID, state.forwardEditor.hostSelect.Open, last.ID)
	}
	if state.forwardEditor.hostSelect.Query.String() != "" {
		t.Fatalf("the query survived choosing a host: %q", state.forwardEditor.hostSelect.Query.String())
	}
}

func TestForwardsSteadyStatePaintDoesNotAllocate(t *testing.T) {
	state := newTestState(t, testOptions(t))
	addForwardRule(t, state, "Web", freePort(t), "web.internal", 8080)
	var canvas gfx.Canvas
	size := gfx.Size{Width: 1400, Height: 900}
	state.Paint(&canvas, size)
	allocations := testing.AllocsPerRun(100, func() {
		canvas.Reset()
		state.Paint(&canvas, size)
	})
	if allocations != 0 {
		t.Fatalf("forwards steady-state paint allocations = %v", allocations)
	}
}

func TestSOCKS5ForwardsSteadyStatePaintDoesNotAllocate(t *testing.T) {
	state := newTestState(t, testOptions(t))
	addSOCKS5ForwardRule(t, state, "Browser Proxy", freePort(t))
	var canvas gfx.Canvas
	size := gfx.Size{Width: 1400, Height: 900}
	state.Paint(&canvas, size)
	allocations := testing.AllocsPerRun(100, func() {
		canvas.Reset()
		state.Paint(&canvas, size)
	})
	if allocations != 0 {
		t.Fatalf("SOCKS5 forwards steady-state paint allocations = %v", allocations)
	}
}

func TestHTTPConnectForwardsSteadyStatePaintDoesNotAllocate(t *testing.T) {
	state := newTestState(t, testOptions(t))
	addHTTPConnectForwardRule(t, state, "HTTP Proxy", freePort(t))
	var canvas gfx.Canvas
	size := gfx.Size{Width: 1400, Height: 900}
	state.Paint(&canvas, size)
	allocations := testing.AllocsPerRun(100, func() {
		canvas.Reset()
		state.Paint(&canvas, size)
	})
	if allocations != 0 {
		t.Fatalf("HTTP CONNECT forwards steady-state paint allocations = %v", allocations)
	}
}

func TestRemoteForwardsSteadyStatePaintDoesNotAllocate(t *testing.T) {
	state := newTestState(t, testOptions(t))
	addRemoteForwardRule(t, state, "Remote", freePort(t), "127.0.0.1", 8080)
	var canvas gfx.Canvas
	size := gfx.Size{Width: 1400, Height: 900}
	state.Paint(&canvas, size)
	allocations := testing.AllocsPerRun(100, func() {
		canvas.Reset()
		state.Paint(&canvas, size)
	})
	if allocations != 0 {
		t.Fatalf("remote forwards steady-state paint allocations = %v", allocations)
	}
}

func BenchmarkPaintForwardsPage(b *testing.B) {
	state := newTestState(b, testOptions(b))
	addForwardRule(b, state, "Web", freePort(b), "web.internal", 8080)
	var canvas gfx.Canvas
	size := gfx.Size{Width: 1400, Height: 900}
	state.Paint(&canvas, size)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		canvas.Reset()
		state.Paint(&canvas, size)
	}
}

func openForwardEditor(t testing.TB, state *State) {
	t.Helper()
	state.Click(hitPortForwarding)
	state.Click(hitForwardAdd)
	if state.modal != modalNewForward {
		t.Fatalf("forward modal = %d", state.modal)
	}
}

func addForwardRule(t testing.TB, state *State, label string, localPort uint16, destination string, destinationPort uint16) {
	t.Helper()
	openForwardEditor(t, state)
	replaceForwardField(state, hitForwardDialogLabel, label)
	replaceForwardField(state, hitForwardDialogLocalPort, fmt.Sprint(localPort))
	replaceForwardField(state, hitForwardDialogDestination, destination)
	replaceForwardField(state, hitForwardDialogDestinationPort, fmt.Sprint(destinationPort))
	if result := state.Key(gfx.KeyEvent{Key: gfx.KeyEnter, Target: hitForwardDialogDestinationPort}); !result.Handled {
		t.Fatal("Enter did not submit the forward dialog")
	}
	if state.modal != modalNone || state.forwardDialogError != "" {
		t.Fatalf("forward submit modal=%d error=%q", state.modal, state.forwardDialogError)
	}
}

func addSOCKS5ForwardRule(t testing.TB, state *State, label string, localPort uint16) {
	t.Helper()
	openForwardEditor(t, state)
	state.Click(hitForwardModeSOCKS5)
	replaceForwardField(state, hitForwardDialogLabel, label)
	replaceForwardField(state, hitForwardDialogLocalPort, fmt.Sprint(localPort))
	if result := state.Key(gfx.KeyEvent{Key: gfx.KeyEnter, Target: hitForwardDialogLocalPort}); !result.Handled {
		t.Fatal("Enter did not submit the SOCKS5 forward dialog")
	}
	if state.modal != modalNone || state.forwardDialogError != "" {
		t.Fatalf("SOCKS5 forward submit modal=%d error=%q", state.modal, state.forwardDialogError)
	}
}

func addRemoteForwardRule(t testing.TB, state *State, label string, remotePort uint16, destination string, destinationPort uint16) {
	t.Helper()
	openForwardEditor(t, state)
	state.Click(hitForwardModeRemote)
	replaceForwardField(state, hitForwardDialogLabel, label)
	replaceForwardField(state, hitForwardDialogLocalPort, fmt.Sprint(remotePort))
	replaceForwardField(state, hitForwardDialogDestination, destination)
	replaceForwardField(state, hitForwardDialogDestinationPort, fmt.Sprint(destinationPort))
	if result := state.Key(gfx.KeyEvent{Key: gfx.KeyEnter, Target: hitForwardDialogDestinationPort}); !result.Handled {
		t.Fatal("Enter did not submit the remote forward dialog")
	}
	if state.modal != modalNone || state.forwardDialogError != "" {
		t.Fatalf("remote forward submit modal=%d error=%q", state.modal, state.forwardDialogError)
	}
}

func waitForward(t testing.TB, state *State, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !ready() {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for forward state")
		}
		state.Wake()
		time.Sleep(time.Millisecond)
	}
}

func waitForwardResult(t testing.TB, state *State) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for len(state.forwardStarts) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for forward start result")
		}
		time.Sleep(time.Millisecond)
	}
	state.Wake()
}

func addHTTPConnectForwardRule(t testing.TB, state *State, label string, localPort uint16) {
	t.Helper()
	openForwardEditor(t, state)
	state.Click(hitForwardModeHTTPConnect)
	replaceForwardField(state, hitForwardDialogLabel, label)
	replaceForwardField(state, hitForwardDialogLocalPort, fmt.Sprint(localPort))
	if result := state.Key(gfx.KeyEvent{Key: gfx.KeyEnter, Target: hitForwardDialogLocalPort}); !result.Handled {
		t.Fatal("Enter did not submit the HTTP CONNECT forward dialog")
	}
	if state.modal != modalNone || state.forwardDialogError != "" {
		t.Fatalf("HTTP CONNECT forward submit modal=%d error=%q", state.modal, state.forwardDialogError)
	}
}

func forwardEchoServer(t testing.TB) net.Listener {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
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

func writeForwardTestBytes(t testing.TB, writer io.Writer, data []byte) {
	t.Helper()
	for _, value := range data {
		if _, err := writer.Write([]byte{value}); err != nil {
			t.Fatal(err)
		}
	}
}

func readForwardTestBytes(t testing.TB, reader io.Reader, want []byte) {
	t.Helper()
	got := make([]byte, len(want))
	if _, err := io.ReadFull(reader, got); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("bytes = %v, want %v", got, want)
	}
}

func TestForwardEditModifiesAndPersists(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	port1 := freePort(t)
	port2 := freePort(t)
	addForwardRule(t, state, "Web", port1, "web.internal", 8080)
	addForwardRule(t, state, "Database", port2, "db.internal", 5432)

	// Select first forward and edit
	state.selectedForwardID = state.forwards[0].ID
	state.Click(hitForwardEdit)
	if state.modal != modalNewForward || !state.forwardEditor.editing || state.forwardEditor.forwardID != state.forwards[0].ID {
		t.Fatalf("edit forward modal=%d editing=%v forwardID=%d", state.modal, state.forwardEditor.editing, state.forwardEditor.forwardID)
	}
	if state.forwardEditor.value(forwardFieldLabel) != "Web" || state.forwardEditor.value(forwardFieldLocalPort) != fmt.Sprint(port1) {
		t.Fatalf("editor prefill: label=%q port=%q", state.forwardEditor.value(forwardFieldLabel), state.forwardEditor.value(forwardFieldLocalPort))
	}

	// Keeping own label and port should be valid
	if err := state.forwardEditor.validate(state); err != "" {
		t.Fatalf("validating own unchanged forward failed: %q", err)
	}

	// Renaming to another existing forward's label should fail validation
	replaceForwardField(state, hitForwardDialogLabel, "Database")
	if err := state.forwardEditor.validate(state); err != state.messages.ErrForwardNameTaken {
		t.Fatalf("expected ErrForwardNameTaken, got %q", err)
	}

	// Modify to unique values
	newPort := freePort(t)
	replaceForwardField(state, hitForwardDialogLabel, "Web Service")
	replaceForwardField(state, hitForwardDialogLocalPort, fmt.Sprint(newPort))
	replaceForwardField(state, hitForwardDialogDestination, "web2.internal")
	replaceForwardField(state, hitForwardDialogDestinationPort, "8443")

	state.Click(hitForwardDialogSubmit)
	if state.modal != modalNone || state.forwardDialogError != "" {
		t.Fatalf("submit edit forward: modal=%d error=%q", state.modal, state.forwardDialogError)
	}
	if len(state.forwards) != 2 {
		t.Fatalf("forwards count = %d, want 2", len(state.forwards))
	}
	edited := state.forwards[0]
	if edited.Label != "Web Service" || edited.LocalPort != newPort || edited.DestinationHost != "web2.internal" || edited.DestinationPort != 8443 {
		t.Fatalf("edited forward = %+v", edited)
	}
	if !strings.HasPrefix(state.status, state.messages.ForwardUpdated) {
		t.Fatalf("status = %q, want prefix %q", state.status, state.messages.ForwardUpdated)
	}

	// Verify persistence
	restarted := newTestState(t, options)
	if len(restarted.forwards) != 2 {
		t.Fatalf("restored forwards count = %d, want 2: %+v", len(restarted.forwards), restarted.forwards)
	}
	var restored forwardRecord
	for _, f := range restarted.forwards {
		if f.Label == "Web Service" {
			restored = f
			break
		}
	}
	if restored.Label != "Web Service" || restored.LocalPort != newPort || restored.DestinationHost != "web2.internal" || restored.DestinationPort != 8443 {
		t.Fatalf("restored forward = %+v", restored)
	}
}

// A forward keeps the number it was saved with, so a window that reloads the
// catalog does not re-point a dialog that was opened on it at another forward.
func TestAForwardKeepsItsSavedNumberAcrossAReload(t *testing.T) {
	state := newTestState(t, testOptions(t))
	host := state.hosts[0]
	saved := func(id uint32, label string, port uint16) savedForward {
		return savedForward{
			ID: id, Label: label, Mode: "local", HostLabel: host.Label, HostAddress: host.Address,
			HostUser: host.User, HostPort: host.Port, LocalPort: port,
			DestinationHost: "127.0.0.1", DestinationPort: 80,
		}
	}
	state.restoreForwards([]savedForward{saved(7, "Seven", 9107), saved(3, "Three", 9103)})
	if got := [2]uint32{state.forwards[0].ID, state.forwards[1].ID}; got != [2]uint32{7, 3} {
		t.Fatalf("restored numbers = %v, want the saved 7 and 3", got)
	}
	if state.nextForwardID != 8 {
		t.Fatalf("next forward number = %d, want one past the largest saved", state.nextForwardID)
	}

	// A record older than the number gets the lowest free one, and the numbers
	// already in use are not handed out again.
	state.restoreForwards([]savedForward{saved(7, "Seven", 9107), saved(0, "Legacy", 9108)})
	if got := [2]uint32{state.forwards[0].ID, state.forwards[1].ID}; got != [2]uint32{7, 1} {
		t.Fatalf("numbers after a legacy record = %v, want 7 and the free 1", got)
	}
}

// Saving an edit whose forward is gone reports it instead of quietly adding a
// second copy under a new number.
func TestEditingAForwardThatIsGoneReportsItInsteadOfAddingACopy(t *testing.T) {
	state := newTestState(t, testOptions(t))
	port := freePort(t)
	addForwardRule(t, state, "Web", port, "web.internal", 8080)

	state.selectedForwardID = state.forwards[0].ID
	state.Click(hitForwardEdit)
	if state.modal != modalNewForward || !state.forwardEditor.editing {
		t.Fatalf("edit dialog = %d editing=%v", state.modal, state.forwardEditor.editing)
	}
	// The record goes away under the open dialog, as it does when another window
	// removes it.
	state.forwards = nil
	state.Click(hitForwardDialogSubmit)
	if state.forwardDialogError != state.messages.ErrForwardEditMissing {
		t.Fatalf("error = %q, want %q", state.forwardDialogError, state.messages.ErrForwardEditMissing)
	}
	if len(state.forwards) != 0 {
		t.Fatalf("forwards = %+v, want no copy appended", state.forwards)
	}
	if state.modal != modalNewForward {
		t.Fatalf("the dialog closed on a failed save: modal = %d", state.modal)
	}
}

// Editing a forward whose SSH host was removed asks for a host rather than
// moving the tunnel to whichever host happens to be first.
func TestEditingAForwardWhoseHostIsGoneAsksForAHost(t *testing.T) {
	state := newTestState(t, testOptions(t))
	addForwardRule(t, state, "Web", freePort(t), "web.internal", 8080)

	state.selectedForwardID = state.forwards[0].ID
	state.Click(hitForwardEdit)
	if state.modal != modalNewForward {
		t.Fatalf("edit dialog = %d", state.modal)
	}
	// The dialog opens on the host the forward travels through, not on some
	// other one: another host would move the tunnel as soon as the form is saved.
	if state.forwardEditor.hostID != state.hosts[0].ID {
		t.Fatalf("the dialog opened on host %d, want the saved one %d", state.forwardEditor.hostID, state.hosts[0].ID)
	}
	// With that host gone, saving reports why instead of picking a replacement.
	state.hosts = nil
	state.forwardEditor.hostID = 0
	if err := state.forwardEditor.validate(state); err != state.messages.ErrForwardHostMissing {
		t.Fatalf("error = %q, want %q", err, state.messages.ErrForwardHostMissing)
	}
}

func readForwardHTTPConnectResponse(t testing.TB, reader *bufio.Reader, wantStatus string) {
	t.Helper()
	status, err := reader.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimRight(status, "\r\n") != wantStatus {
		t.Fatalf("status = %q, want %q", status, wantStatus)
	}
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if line == "\r\n" {
			return
		}
	}
}

func replaceForwardField(state *State, hit gfx.ID, value string) {
	editor := state.EditorFor(hit)
	editor.SetSelection(text.Range{Start: 0, End: editor.RuneCount()})
	editor.Insert(value)
}

func freePort(t testing.TB) uint16 {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := uint16(listener.Addr().(*net.TCPAddr).Port)
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return port
}

func hasHit(canvas *gfx.Canvas, id gfx.ID) bool {
	for _, hit := range canvas.Hits() {
		if hit.ID == id {
			return true
		}
	}
	return false
}

type delayedCloseListener struct {
	net.Listener
	started chan struct{}
	release chan struct{}
}

func (listener *delayedCloseListener) Close() error {
	close(listener.started)
	<-listener.release
	return listener.Listener.Close()
}

// The open host list belongs to the keyboard too: arrows move the highlighted
// row past disabled hosts, and Enter chooses it.
func TestForwardHostSelectTakesTheArrowKeys(t *testing.T) {
	state := newTestState(t, testOptions(t))
	openForwardEditor(t, state)
	state.Click(hitForwardDialogHost)
	if result := state.Key(gfx.KeyEvent{Key: gfx.KeyDown, Target: hitForwardDialogHost}); !result.Handled {
		t.Fatalf("Down was not taken: %+v", result)
	}
	result := state.Key(gfx.KeyEvent{Key: gfx.KeyEnter, Target: hitForwardDialogHost})
	if !result.Handled || state.forwardEditor.hostID != state.hosts[1].ID || state.forwardEditor.hostSelect.Open {
		t.Fatalf("Enter = %+v host=%d open=%v, want the second host closed", result, state.forwardEditor.hostID, state.forwardEditor.hostSelect.Open)
	}
}

// Typing filters the open list instead of closing it: only the hosts whose
// name, address or account match stay, and Enter chooses the one left.
func TestForwardHostSelectFiltersWhileTyping(t *testing.T) {
	state := newTestState(t, testOptions(t))
	openForwardEditor(t, state)
	state.Click(hitForwardDialogHost)
	var staging uint32
	for _, host := range state.hosts {
		if host.Label == "Staging" {
			staging = host.ID
		}
	}
	state.forwardEditor.hostSelect.Query.Insert("stag")
	choices := state.forwardEditor.hostSelect.Choices()
	if len(choices) != 1 || choices[0].Label != "Staging" {
		t.Fatalf("filter = %+v, want only Staging", choices)
	}
	result := state.Key(gfx.KeyEvent{Key: gfx.KeyEnter, Target: hitForwardDialogHost})
	if !result.Handled || state.forwardEditor.hostSelect.Open || state.forwardEditor.hostID != staging {
		t.Fatalf("Enter = %+v host=%d open=%v, want Staging closed", result, state.forwardEditor.hostID, state.forwardEditor.hostSelect.Open)
	}
	if query := state.forwardEditor.hostSelect.Query.String(); query != "" {
		t.Fatalf("the chosen host kept its query %q", query)
	}
}

// Tab leaves an open host list: the picker asks the runtime to settle the close
// first, then traverse the page, instead of cycling through the options.
func TestTabClosesAnOpenSelectBeforeTraversal(t *testing.T) {
	state := newTestState(t, testOptions(t))
	openForwardEditor(t, state)
	state.Click(hitForwardDialogHost)
	paintAt(state, gfx.Size{Width: 1400, Height: 900})
	result := state.Key(gfx.KeyEvent{Key: gfx.KeyTab, Target: hitForwardDialogHost})
	if !result.ContinueTraversal || !result.Handled || state.forwardEditor.hostSelect.Open {
		t.Fatalf("Tab = %+v open=%v, want close then traversal", result, state.forwardEditor.hostSelect.Open)
	}
}
