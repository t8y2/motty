package workbench

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/text"
	"motty/internal/configstore"
	"motty/internal/sshconn"
	"motty/internal/systemproxy"
)

type localProxyTestLender struct {
	*fakeConn
	shared *fakeConn
}

func (lender *localProxyTestLender) Share(func()) (Connection, error) {
	if lender.shared != nil {
		return &localProxyTestLender{fakeConn: lender.shared}, nil
	}
	return lender.fakeConn, nil
}

func localProxyTestPort(t *testing.T) (uint16, func()) {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			_ = connection.Close()
		}
	}()
	_, rawPort, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.ParseUint(rawPort, 10, 16)
	if err != nil {
		t.Fatal(err)
	}
	return uint16(port), func() {
		_ = listener.Close()
		<-done
	}
}

func waitLocalProxyPhase(t *testing.T, manager *localProxyManager, target localProxyTarget, want localProxyPhase) localProxySnapshot {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		snapshot := manager.Snapshot(target)
		if snapshot.Phase == want {
			return snapshot
		}
		if time.Now().After(deadline) {
			t.Fatalf("local proxy phase = %v, want %v: %+v", snapshot.Phase, want, snapshot)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestLocalProxyManagerSharesOneOperationAndReleasesTheLastUser(t *testing.T) {
	target := localProxyTarget{
		Host: "host", Port: 22, User: "me",
		HostKeyFingerprint: "SHA256:test", ConnectionGeneration: 11,
	}
	port, closeProxy := localProxyTestPort(t)
	defer closeProxy()
	source := &fakeConn{status: sshconn.Status{
		Version: 1, Phase: sshconn.PhaseConnected, ConnectionGeneration: 11,
		HostKey: sshconn.HostKeyInfo{Fingerprint: "SHA256:test"},
	}}
	lenderSource := &localProxyTestLender{fakeConn: source}
	var listeners []net.Listener
	source.listen = func(network, address string) (net.Listener, error) {
		listener, err := net.Listen(network, address)
		if err == nil {
			listeners = append(listeners, listener)
		}
		return listener, err
	}
	source.exec = func(context.Context, string, int) (sshconn.ExecResult, error) {
		if len(listeners) == 0 {
			return sshconn.ExecResult{ExitCode: 1}, nil
		}
		return sshconn.ExecResult{Output: []byte(fmt.Sprintf("LISTEN 0 128 %s 0.0.0.0:*\n", listeners[len(listeners)-1].Addr()))}, nil
	}
	manager := newLocalProxyManager(nil)
	t.Cleanup(manager.Close)

	first, _ := manager.Start(target, lenderSource, port, localProxySubscription{})
	waitLocalProxyPhase(t, manager, target, localProxyConnected)
	second, _ := manager.Start(target, lenderSource, port, localProxySubscription{})
	manager.Leave(first)
	if manager.Snapshot(target).Phase != localProxyConnected {
		t.Fatal("closing one user stopped the shared tunnel")
	}
	manager.Leave(second)
	if snapshot := manager.Snapshot(target); snapshot.Phase != localProxyDisconnected {
		t.Fatalf("last user left with phase %v", snapshot.Phase)
	}
	waitForSharedCanceled(t, source)
}

func TestLocalProxyManagerKeepsIndependentConnectionsSeparate(t *testing.T) {
	port, closeProxy := localProxyTestPort(t)
	defer closeProxy()
	manager := newLocalProxyManager(nil)
	t.Cleanup(manager.Close)
	start := func(generation uint64) (localProxyTarget, localProxySubscription) {
		target := localProxyTarget{
			Host: "host", Port: 22, User: "me",
			HostKeyFingerprint: "SHA256:test", ConnectionGeneration: generation,
		}
		source := &fakeConn{status: sshconn.Status{
			Version: 1, Phase: sshconn.PhaseConnected, ConnectionGeneration: generation,
			HostKey: sshconn.HostKeyInfo{Fingerprint: "SHA256:test"},
		}}
		lenderSource := &localProxyTestLender{fakeConn: source}
		var listener net.Listener
		source.listen = func(network, address string) (net.Listener, error) {
			var err error
			listener, err = net.Listen(network, address)
			return listener, err
		}
		source.exec = func(context.Context, string, int) (sshconn.ExecResult, error) {
			if listener == nil {
				return sshconn.ExecResult{ExitCode: 1}, nil
			}
			return sshconn.ExecResult{Output: []byte(fmt.Sprintf("LISTEN 0 128 %s 0.0.0.0:*\n", listener.Addr()))}, nil
		}
		subscription, _ := manager.Start(target, lenderSource, port, localProxySubscription{})
		waitLocalProxyPhase(t, manager, target, localProxyConnected)
		return target, subscription
	}
	firstTarget, first := start(21)
	secondTarget, second := start(22)
	if firstTarget == secondTarget || first.token == second.token {
		t.Fatal("independent transports collapsed into one operation")
	}
	manager.Stop(firstTarget)
	if manager.Snapshot(firstTarget).Phase != localProxyDisconnected || manager.Snapshot(secondTarget).Phase != localProxyConnected {
		t.Fatal("stopping one connection changed the other")
	}
	manager.Stop(secondTarget)
}

func TestLocalProxyTunnelSurvivesSessionMoveAndStopsWithItsLastUser(t *testing.T) {
	proxyPort, closeProxy := localProxyTestPort(t)
	defer closeProxy()

	group := NewGroup()
	known, options := newWorld(t)
	options.Group = group
	connection := &fakeConn{}
	known.conns = append(known.conns, connection)
	connection.listen = net.Listen
	connection.exec = func(_ context.Context, command string, _ int) (sshconn.ExecResult, error) {
		colon := strings.LastIndex(command, ":")
		if colon < 0 {
			return sshconn.ExecResult{ExitCode: 1}, nil
		}
		rawPort := strings.TrimSpace(strings.TrimSuffix(command[colon+1:], "'"))
		return sshconn.ExecResult{
			Output: []byte(fmt.Sprintf("LISTEN 0 128 127.0.0.1:%s 0.0.0.0:*\n", rawPort)),
		}, nil
	}
	options.Dial = func(sshconn.Config) Connection {
		return &localProxyTestLender{fakeConn: connection, shared: connection}
	}

	source := newTestState(t, options)
	target := newTestState(t, options)
	openHost(source)
	connection.set(func(status *sshconn.Status) {
		status.Phase = sshconn.PhaseConnected
		status.ConnectionGeneration = 71
		status.HostKey.Fingerprint = "SHA256:test"
	})
	source.Wake()
	source.Click(hitPaneSplitHorizontal)
	source.Wake()
	if len(source.sessions) != 2 {
		t.Fatalf("split sessions = %d", len(source.sessions))
	}

	session, ok := source.sessionByIDPointer(source.activeSessionID)
	if !ok {
		t.Fatal("connected session is missing")
	}
	source.openLocalProxy(session)
	source.localProxyUI.port = text.Editor{}
	source.localProxyUI.port.Insert(strconv.Itoa(int(proxyPort)))
	source.startLocalProxy()
	source.Wake()
	targetSnapshot := waitLocalProxyPhase(t, group.shares, session.localProxy.target, localProxyConnected)
	if targetSnapshot.RemotePort == 0 {
		t.Fatal("connected tunnel has no remote port")
	}

	firstID := source.sessions[0].ID
	source.closeSession(firstID)
	if snapshot := source.localProxySnapshot(&source.sessions[0]); snapshot.Phase != localProxyConnected {
		t.Fatalf("closing one related session changed the tunnel: %+v", snapshot)
	}

	payload, ok := source.detachWorkspace(source.deck.ActiveWorkspace())
	if !ok || len(payload.sessions) != 1 {
		t.Fatalf("detached workspace = %+v/%t", payload, ok)
	}
	if !target.attachPayload(payload, tabDrop{kind: dropReorder}) {
		t.Fatal("target window did not accept the proxy session")
	}
	target.Wake()
	source.Close()
	if snapshot := target.localProxySnapshot(&target.sessions[0]); snapshot.Phase != localProxyConnected {
		t.Fatalf("moving the last window changed the tunnel: %+v", snapshot)
	}

	target.closeSession(target.sessions[0].ID)
	if snapshot := group.shares.Snapshot(session.localProxy.target); snapshot.Phase != localProxyDisconnected {
		t.Fatalf("last session left the tunnel at %v", snapshot.Phase)
	}
}

func TestLocalProxyManagerRetriesWithANewOperationAndDropsLateResults(t *testing.T) {
	target := localProxyTarget{
		Host: "host", Port: 22, User: "me",
		HostKeyFingerprint: "SHA256:test", ConnectionGeneration: 31,
	}
	port, closeProxy := localProxyTestPort(t)
	defer closeProxy()
	manager := newLocalProxyManager(nil)
	t.Cleanup(manager.Close)
	oldCtx, oldCancel := context.WithCancel(context.Background())
	defer oldCancel()
	old := &localProxyOperation{
		target: target, ctx: oldCtx, cancel: oldCancel,
		phase: localProxyFailed, err: errLocalProxyForwarding,
		users: make(map[localProxySubscription]struct{}),
	}
	subscription := localProxySubscription{target: target, token: 1}
	old.users[subscription] = struct{}{}
	manager.operations[target] = old

	source := &fakeConn{status: sshconn.Status{
		Version: 1, Phase: sshconn.PhaseConnected, ConnectionGeneration: 31,
		HostKey: sshconn.HostKeyInfo{Fingerprint: "SHA256:test"},
	}}
	lenderSource := &localProxyTestLender{fakeConn: source}
	var listener net.Listener
	source.listen = func(network, address string) (net.Listener, error) {
		var err error
		listener, err = net.Listen(network, address)
		return listener, err
	}
	source.exec = func(context.Context, string, int) (sshconn.ExecResult, error) {
		if listener == nil {
			return sshconn.ExecResult{ExitCode: 1}, nil
		}
		return sshconn.ExecResult{Output: []byte(fmt.Sprintf("LISTEN 0 128 %s 0.0.0.0:*\n", listener.Addr()))}, nil
	}
	gotSubscription, _ := manager.Start(target, lenderSource, port, subscription)
	if gotSubscription != subscription {
		t.Fatalf("retry replaced the session subscription: got %+v want %+v", gotSubscription, subscription)
	}
	waitLocalProxyPhase(t, manager, target, localProxyConnected)
	current := manager.operations[target]
	if current == old {
		t.Fatal("retry reused the failed operation")
	}
	if !errors.Is(oldCtx.Err(), context.Canceled) {
		t.Fatal("retry did not cancel the stale operation")
	}
	// A late result for the old pointer cannot replace the new operation.
	manager.mu.Lock()
	if manager.operations[target] != current {
		t.Fatal("late operation replaced current")
	}
	current.phase = localProxyFailed
	manager.mu.Unlock()
	if snapshot := manager.Snapshot(target); snapshot.Phase != localProxyFailed {
		t.Fatalf("current phase = %v", snapshot.Phase)
	}
}

func waitForSharedCanceled(t *testing.T, connection *fakeConn) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		connection.mu.Lock()
		canceled := connection.canceled > 0 || connection.status.Phase == sshconn.PhaseClosed
		connection.mu.Unlock()
		if canceled {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("shared transport was not released")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestLocalProxySystemPortSelection(t *testing.T) {
	for _, test := range []struct {
		name     string
		settings systemproxy.Settings
		lookup   func() systemproxy.Settings
		want     uint16
	}{
		{name: "direct HTTP", settings: systemproxy.Settings{HTTP: "http://127.0.0.1:8123"}, want: 8123},
		{name: "HTTPS fallback", settings: systemproxy.Settings{HTTPS: "http://127.0.0.1:9123"}, want: 9123},
		{name: "SOCKS only", settings: systemproxy.Settings{All: "socks5://127.0.0.1:1080"}, want: defaultLocalProxyPort},
		{name: "non-loopback", settings: systemproxy.Settings{HTTP: "http://192.0.2.1:8123"}, want: defaultLocalProxyPort},
		{name: "credentials", settings: systemproxy.Settings{HTTP: "http://user:pass@127.0.0.1:8123"}, want: defaultLocalProxyPort},
		{name: "missing lookup", want: defaultLocalProxyPort},
	} {
		t.Run(test.name, func(t *testing.T) {
			lookup := test.lookup
			if lookup == nil && test.settings.HTTP != "" || test.settings.HTTPS != "" || test.settings.All != "" {
				lookup = func() systemproxy.Settings { return test.settings }
			}
			if got := localProxySystemPort(lookup); got != test.want {
				t.Fatalf("port = %d, want %d", got, test.want)
			}
		})
	}
}

func TestLocalProxyPreferencesStayLocalAndRestoreStopped(t *testing.T) {
	options := freshOptions(t)
	options.StatePath = filepath.Join(t.TempDir(), "config.db")
	state := newTestState(t, options)
	state.localProxyPort, state.localProxyPortSet = 9345, true
	state.save()

	var local map[string]any
	if err := json.Unmarshal(localConfigValue(t, options.StatePath, localKey), &local); err != nil {
		t.Fatal(err)
	}
	if local["local_proxy_port"] != float64(9345) {
		t.Fatalf("local proxy port was not local: %v", local["local_proxy_port"])
	}
	store, err := configstore.Open(options.StatePath, configFormat)
	if err != nil {
		t.Fatal(err)
	}
	entries, _, err := store.Load()
	if closeErr := store.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.Contains(string(entry.Payload), "local_proxy_port") {
			t.Fatalf("shared record contains local proxy port: %s", entry.Payload)
		}
	}

	restored := newTestState(t, options)
	if restored.localProxyPort != 9345 || !restored.localProxyPortSet || restored.modal == modalLocalProxy {
		t.Fatalf("restored proxy = %d/%t modal=%v", restored.localProxyPort, restored.localProxyPortSet, restored.modal)
	}
	if snapshot := restored.localProxySnapshot(nil); snapshot.Phase != localProxyDisconnected {
		t.Fatalf("restore started a tunnel: %+v", snapshot)
	}
	restored.restoreLocal(savedState{})
	if restored.localProxyPort != defaultLocalProxyPort || restored.localProxyPortSet {
		t.Fatalf("invalid state kept %d/%t", restored.localProxyPort, restored.localProxyPortSet)
	}
}

func TestLocalProxyUnselectedPortIsNotRememberedByOrdinarySave(t *testing.T) {
	options := freshOptions(t)
	options.StatePath = filepath.Join(t.TempDir(), "config.db")
	state := newTestState(t, options)
	state.save()
	restored := newTestState(t, options)
	if restored.localProxyPortSet {
		t.Fatal("saving unrelated state turned the default port into a user preference")
	}
}

func TestLocalProxyCopyOnlyChangesClipboard(t *testing.T) {
	world, options := newSharingWorld(t)
	var clipboard string
	options.SetClipboardText = func(value string) error {
		clipboard = value
		return nil
	}
	state := newTestState(t, options)
	world.connectFirst(t, state)
	state.Wake()
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	if !ok {
		t.Fatal("connected session is missing")
	}
	state.openLocalProxy(session)
	if state.modal != modalLocalProxy {
		t.Fatal("local proxy panel did not open")
	}
	target, _ := state.localProxyTarget(session)
	state.localProxyManager().operations[target] = &localProxyOperation{
		target: target, cancel: func() {}, remotePort: 53217,
		phase: localProxyConnected, users: make(map[localProxySubscription]struct{}),
	}
	state.Click(hitLocalProxyEnableCopy)
	if !strings.Contains(clipboard, "127.0.0.1:53217") {
		t.Fatalf("clipboard = %q", clipboard)
	}
	if typed := state.sessions[0].term.shell.(*fakeShell).typed(); typed != "" {
		t.Fatalf("copy wrote to the PTY: %q", typed)
	}
	if state.modal != modalNone {
		t.Fatal("copying setup did not return to the original terminal")
	}
}

func TestLocalProxyPanelPaintsControlsOnFirstFrame(t *testing.T) {
	world, options := newSharingWorld(t)
	options.Invalidate = func() {}
	state := newTestState(t, options)
	world.connectFirst(t, state)
	state.Wake()
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	if !ok {
		t.Fatal("connected session is missing")
	}
	state.openLocalProxy(session)

	canvas, _ := paintAt(state, gfx.Size{Width: 1400, Height: 900})
	for _, id := range []gfx.ID{hitLocalProxyPort, hitLocalProxyStart} {
		if _, ok := hitByID(canvas, id); !ok {
			t.Fatalf("first frame did not paint hit %d: texts=%+v hits=%+v", id, canvas.Texts(), canvas.Hits())
		}
	}
}

func TestLocalProxyDockerPanelPaintsNavigationOnFirstFrame(t *testing.T) {
	world, options := newSharingWorld(t)
	options.Invalidate = func() {}
	state := newTestState(t, options)
	world.connectFirst(t, state)
	state.Wake()
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	if !ok {
		t.Fatal("connected session is missing")
	}
	state.openLocalProxy(session)
	state.localProxyUI.docker = true
	state.localProxyUI.lastRemotePort = 43217
	target, _ := state.localProxyTarget(session)
	state.localProxyManager().operations[target] = &localProxyOperation{
		target: target, cancel: func() {}, remotePort: 43217,
		phase: localProxyConnected, users: make(map[localProxySubscription]struct{}),
	}

	canvas, _ := paintAt(state, gfx.Size{Width: 1400, Height: 900})
	foundBack := false
	foundCancel := false
	foundHTTP := false
	foundHTTPS := false
	for _, hit := range canvas.Hits() {
		if hit.ID == hitLocalProxyDockerBack && hit.Label == state.messages.LocalProxy.TerminalUsage && hit.Bounds.Width > 0 && hit.Bounds.Height > 0 {
			foundBack = true
		}
		if hit.ID == hitLocalProxyClose && hit.Label == state.messages.LocalProxy.BackToTerminal && hit.Bounds.Width > 0 && hit.Bounds.Height > 0 {
			foundCancel = true
		}
	}
	if !foundBack || !foundCancel {
		t.Fatalf("Docker first frame navigation = back:%t cancel:%t, hits=%+v", foundBack, foundCancel, canvas.Hits())
	}
	for _, text := range canvas.Texts() {
		if text.Value == "HTTP_PROXY=http://127.0.0.1:43217" {
			foundHTTP = true
		}
		if text.Value == "HTTPS_PROXY=http://127.0.0.1:43217" {
			foundHTTPS = true
		}
		if strings.Contains(text.Value, "%!") {
			t.Fatalf("Docker text contains a formatting error: %q", text.Value)
		}
	}
	if !foundHTTP || !foundHTTPS {
		t.Fatalf("Docker proxy lines = HTTP:%t HTTPS:%t", foundHTTP, foundHTTPS)
	}
}

func TestLocalProxyPanelFitsSmallWindowsInBothLanguages(t *testing.T) {
	for _, language := range []string{"en", "zh-Hans"} {
		t.Run(language, func(t *testing.T) {
			world, options := newSharingWorld(t)
			options.Invalidate = func() {}
			options.PreferredLanguages = func() []string { return []string{language} }
			state := newTestState(t, options)
			world.connectFirst(t, state)
			state.Wake()
			session, ok := state.sessionByIDPointer(state.activeSessionID)
			if !ok {
				t.Fatal("connected session is missing")
			}
			state.openLocalProxy(session)
			state.localProxyUI.lastRemotePort = 43217
			state.localProxyUI.lastStopped = true

			for _, docker := range []bool{false, true} {
				state.localProxyUI.docker = docker
				size := gfx.Size{Width: 900, Height: 640}
				canvas, _ := paintAt(state, size)
				snapshot := app.Snapshot{Width: int(size.Width), Height: int(size.Height), Texts: canvas.Texts(), Hits: canvas.Hits()}
				if problems := headless.CheckLayout(snapshot); len(problems) > 0 {
					t.Fatalf("docker=%t layout problems = %+v", docker, problems)
				}
			}
		})
	}
}

func TestLocalProxyPanelKeyboardAndHiddenActiveEntry(t *testing.T) {
	world, options := newSharingWorld(t)
	options.Invalidate = func() {}
	state := newTestState(t, options)
	world.connectFirst(t, state)
	state.Wake()
	state.sessions[0].term.shell.(*fakeShell).say("ready")
	state.Wake()
	if state.showsProgress(&state.sessions[0]) {
		t.Fatal("connected session still shows its progress page")
	}
	state.Click(hitLocalProxyOpen)
	if state.modal != modalLocalProxy {
		t.Fatal("toolbar did not open the local proxy panel")
	}
	if state.localProxyUI.port.String() != strconv.Itoa(int(defaultLocalProxyPort)) {
		t.Fatalf("initial port = %q", state.localProxyUI.port.String())
	}
	if result := state.Key(gfx.KeyEvent{Key: gfx.KeyEscape}); !result.Handled || state.modal != modalNone || result.Focus != state.activeTerminalFocusHit() {
		t.Fatalf("Escape = %+v modal=%v", result, state.modal)
	}
	state.toolbars.customize(toolbarTerminal)
	for index := range state.toolbars.layouts[toolbarTerminal] {
		if state.toolbars.layouts[toolbarTerminal][index].Key == "local-proxy" {
			state.toolbars.layouts[toolbarTerminal][index].Enabled = false
		}
	}
	state.localProxyUI.port.Insert("43217")
	state.localProxyUI.session = state.activeSessionID
	state.activeSection = sectionTerminal
	if session, ok := state.sessionByIDPointer(state.activeSessionID); ok {
		state.localProxyUI.session = session.ID
	}
	target, ok := state.localProxyTarget(state.localProxySession())
	if !ok {
		t.Fatal("connected SSH session has no local proxy target")
	}
	state.localProxyManager().operations[target] = &localProxyOperation{
		target: target, cancel: func() {}, phase: localProxyConnected, users: make(map[localProxySubscription]struct{}),
	}
	var tools [maxBarTools]barTool
	if got := state.toolbarDockTools(state.defaultToolbarDockZone(toolbarTerminal), &tools); !containsBarTool(got, hitLocalProxyOpen) {
		t.Fatal("active tunnel has no entry while its toolbar action is hidden")
	}
}

func TestLocalProxyCloseButtonReturnsFocusToTerminal(t *testing.T) {
	world, options := newSharingWorld(t)
	options.Invalidate = func() {}
	state := newTestState(t, options)
	world.connectFirst(t, state)
	state.Wake()
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	if !ok {
		t.Fatal("connected session is missing")
	}
	state.openLocalProxy(session)
	result := state.Pointer(gfx.PointerEvent{
		Kind: gfx.PointerUp, Button: gfx.PointerButtonPrimary, Target: hitLocalProxyClose,
	})
	if state.modal != modalNone {
		t.Fatal("close button did not close the panel")
	}
	if got, want := result.Focus, state.activeTerminalFocusHit(); !result.Handled || got != want {
		t.Fatalf("close result = %+v, want terminal focus %v", result, want)
	}
}

func containsBarTool(tools []barTool, id gfx.ID) bool {
	for _, tool := range tools {
		if tool.id == id {
			return true
		}
	}
	return false
}

func TestLocalProxyInvalidStateDoesNotStartBackgroundWork(t *testing.T) {
	manager := newLocalProxyManager(nil)
	t.Cleanup(manager.Close)
	target := localProxyTarget{ConnectionGeneration: 1}
	subscription, snapshot := manager.Start(target, nil, 0, localProxySubscription{})
	if subscription.token != 0 || !errors.Is(snapshot.Err, errLocalProxyUnavailable) {
		t.Fatalf("invalid start = %+v/%+v", subscription, snapshot)
	}
	if snapshot.TestPhase != localProxyTestIdle {
		t.Fatalf("disabled state started a test: %+v", snapshot)
	}
}

func TestLocalProxyTestCancellationDoesNotWaitForSSHCloseOrQueueRetries(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	stream := &localProxyDelayedCloseConn{Conn: client}
	connection := &fakeConn{
		status: sshconn.Status{Phase: sshconn.PhaseConnected},
		dial:   func(context.Context, string, string) (net.Conn, error) { return stream, nil },
	}
	entered := make(chan struct{})
	go func() {
		_, _ = http.ReadRequest(bufio.NewReader(server))
		close(entered)
	}()
	manager := newLocalProxyManager(nil)
	defer manager.Close()
	target := localProxyTarget{ConnectionGeneration: 8004}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	operation := &localProxyOperation{
		target: target, ctx: ctx, cancel: cancel, transport: connection,
		phase: localProxyConnected, remotePort: 43217,
	}
	manager.operations[target] = operation
	manager.Test(target)
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("CONNECT request did not start")
	}
	cancel()
	deadline := time.Now().Add(time.Second)
	for manager.Snapshot(target).TestPhase != localProxyTestFailed {
		if time.Now().After(deadline) {
			t.Fatal("canceled test waited for the SSH channel to acknowledge Close")
		}
		time.Sleep(time.Millisecond)
	}
	manager.Stop(target)
	retryCtx, retryCancel := context.WithCancel(context.Background())
	defer retryCancel()
	manager.operations[target] = &localProxyOperation{
		target: target, ctx: retryCtx, cancel: retryCancel, transport: connection,
		phase: localProxyConnected, remotePort: 43218,
	}
	for range 3 {
		manager.Test(target)
	}
	connection.mu.Lock()
	dials := len(connection.dialCalls)
	connection.mu.Unlock()
	if dials != 1 || manager.Snapshot(target).TestPhase == localProxyTestRunning {
		t.Fatalf("retry queued work behind a stuck SSH channel: dials=%d", dials)
	}
}

// SSH Channel.Close sends a packet but may leave Read waiting for the peer.
type localProxyDelayedCloseConn struct{ net.Conn }

func (conn *localProxyDelayedCloseConn) Close() error { return nil }

func TestLocalProxyRestartKeepsExistingSessionsSubscribed(t *testing.T) {
	port, closeProxy := localProxyTestPort(t)
	defer closeProxy()
	connection := &fakeConn{status: sshconn.Status{Phase: sshconn.PhaseConnected, ConnectionGeneration: 8001}}
	connection.listen = net.Listen
	connection.exec = func(_ context.Context, command string, _ int) (sshconn.ExecResult, error) {
		raw := strings.TrimSuffix(command[strings.LastIndex(command, ":")+1:], "'")
		return sshconn.ExecResult{Output: []byte(fmt.Sprintf("LISTEN 0 128 127.0.0.1:%s 0.0.0.0:*\n", raw))}, nil
	}
	source := &localProxyTestLender{fakeConn: connection}
	state := &State{sessions: []Session{
		{ID: 1, Address: "host", host: Host{User: "me", Port: 22}, phase: sshconn.PhaseConnected, conn: source},
		{ID: 2, Address: "host", host: Host{User: "me", Port: 22}, phase: sshconn.PhaseConnected, conn: source},
	}}
	manager := state.localProxyManager()
	defer manager.Close()
	target, _ := state.localProxyTarget(&state.sessions[0])
	for range 2 {
		state.sessions[0].localProxy, _ = manager.Start(target, source, port, state.sessions[0].localProxy)
		waitLocalProxyPhase(t, manager, target, localProxyConnected)
		state.syncLocalProxySessions()
		manager.Stop(target)
	}
	state.sessions[0].localProxy, _ = manager.Start(target, source, port, state.sessions[0].localProxy)
	waitLocalProxyPhase(t, manager, target, localProxyConnected)
	state.syncLocalProxySessions()
	state.leaveLocalProxy(&state.sessions[0])
	if snapshot := manager.Snapshot(target); snapshot.Phase != localProxyConnected {
		t.Fatalf("closing one session stopped the other session's restarted tunnel: %+v", snapshot)
	}
	state.leaveLocalProxy(&state.sessions[1])
	if snapshot := manager.Snapshot(target); snapshot.Phase != localProxyDisconnected {
		t.Fatalf("last session did not release the restarted tunnel: %+v", snapshot)
	}
}

type pendingLocalProxyConnection struct {
	*fakeConn
	entered chan struct{}
	release chan struct{}
}

func (connection *pendingLocalProxyConnection) Share(func()) (Connection, error) {
	return connection, nil
}

func (connection *pendingLocalProxyConnection) Listen(string, string) (net.Listener, error) {
	close(connection.entered)
	<-connection.release
	return nil, context.Canceled
}

func (connection *pendingLocalProxyConnection) ListenContext(ctx context.Context, _, _ string, _ func(net.Listener) error) (net.Listener, error) {
	close(connection.entered)
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-connection.release:
		return nil, context.Canceled
	}
}

func TestLocalProxyStopReleasesTheShareDuringStartup(t *testing.T) {
	port, closeProxy := localProxyTestPort(t)
	defer closeProxy()
	connection := &pendingLocalProxyConnection{
		fakeConn: &fakeConn{}, entered: make(chan struct{}), release: make(chan struct{}),
	}
	defer close(connection.release)
	manager := newLocalProxyManager(nil)
	defer manager.Close()
	target := localProxyTarget{ConnectionGeneration: 8002}
	manager.Start(target, connection, port, localProxySubscription{})
	select {
	case <-connection.entered:
	case <-time.After(time.Second):
		t.Fatal("remote listener request did not start")
	}
	manager.Stop(target)
	connection.mu.Lock()
	canceled := connection.canceled
	connection.mu.Unlock()
	if canceled == 0 {
		t.Fatal("stopping during listener setup retained the internal SSH share")
	}
}

func TestLocalProxyStartupKeepsExistingSessionsWithoutAnotherWake(t *testing.T) {
	for _, separateWindow := range []bool{false, true} {
		t.Run(fmt.Sprintf("separate_window=%t", separateWindow), func(t *testing.T) {
			port, closeProxy := localProxyTestPort(t)
			defer closeProxy()
			connection := &pendingLocalProxyConnection{
				fakeConn: &fakeConn{status: sshconn.Status{Phase: sshconn.PhaseConnected, ConnectionGeneration: 8003}},
				entered:  make(chan struct{}), release: make(chan struct{}),
			}
			defer close(connection.release)
			session := Session{ID: 1, Address: "host", host: Host{User: "me", Port: 22}, phase: sshconn.PhaseConnected, conn: connection}
			state := &State{sessions: []Session{session}}
			other := state
			if separateWindow {
				other = &State{sessions: []Session{session}}
				group := NewGroup()
				group.members = []*State{state, other}
				state.options.Group, other.options.Group = group, group
			} else {
				session.ID = 2
				state.sessions = append(state.sessions, session)
			}
			state.localProxyUI.session = 1
			state.localProxyUI.port.Insert(strconv.Itoa(int(port)))
			manager := state.localProxyManager()
			defer manager.Close()
			if !state.startLocalProxy() {
				t.Fatal("proxy did not start")
			}
			select {
			case <-connection.entered:
			case <-time.After(time.Second):
				t.Fatal("remote listener request did not start")
			}
			target, _ := state.localProxyTarget(&state.sessions[0])
			state.leaveLocalProxy(&state.sessions[0])
			if snapshot := manager.Snapshot(target); snapshot.Phase != localProxyConnecting {
				t.Fatalf("closing the initiator canceled the other session's startup: %+v", snapshot)
			}
			other.leaveLocalProxy(&other.sessions[len(other.sessions)-1])
			if snapshot := manager.Snapshot(target); snapshot.Phase != localProxyDisconnected {
				t.Fatalf("closing the last session retained the startup: %+v", snapshot)
			}
		})
	}
}

func TestLocalProxyNewSharedSessionJoinsBeforeItsFirstWake(t *testing.T) {
	world, options := newSharingWorld(t)
	state := newTestState(t, options)
	world.connectFirst(t, state)
	first, _ := state.sessionByIDPointer(state.activeSessionID)
	firstID := first.ID
	target, _ := state.localProxyTarget(first)
	manager := state.localProxyManager()
	manager.operations[target] = &localProxyOperation{
		target: target, cancel: func() {}, remotePort: 43217,
		phase: localProxyConnected, users: make(map[localProxySubscription]struct{}),
	}
	state.joinLocalProxy(first)
	state.Click(hitPaneSplitHorizontal)
	if len(state.sessions) != 2 {
		t.Fatal("shared split did not open")
	}
	state.closeSession(firstID)
	if snapshot := manager.Snapshot(target); snapshot.Phase != localProxyConnected {
		t.Fatalf("closing the original session stopped the new shared session's tunnel: %+v", snapshot)
	}
	state.closeSession(state.sessions[0].ID)
	if snapshot := manager.Snapshot(target); snapshot.Phase != localProxyDisconnected {
		t.Fatalf("the last shared session retained its tunnel: %+v", snapshot)
	}
}

func TestLocalProxyPanelWheelScrollsBothDirections(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	state.localProxyUI.scroll = 100
	state.localProxyUI.scrollGeometry = components.ScrollAreaGeometry{
		Bounds: gfx.Rect{Width: 600, Height: 200}, Content: gfx.Rect{Width: 600, Height: 470},
	}
	state.scrollLocalProxy(gfx.ScrollEvent{DeltaY: -40, Precise: true})
	if got := state.localProxyUI.scroll; got != 60 {
		t.Fatalf("upward scroll = %v, want 60", got)
	}
	state.scrollLocalProxy(gfx.ScrollEvent{DeltaY: 20, Precise: true})
	if got := state.localProxyUI.scroll; got != 80 {
		t.Fatalf("downward scroll = %v, want 80", got)
	}
}

func TestLocalProxyReopenedPanelShowsTheRunningPort(t *testing.T) {
	world, options := newSharingWorld(t)
	state := newTestState(t, options)
	world.connectFirst(t, state)
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	target, _ := state.localProxyTarget(session)
	state.localProxyPort, state.localProxyPortSet = 7897, true
	state.localProxyManager().operations[target] = &localProxyOperation{
		target: target, cancel: func() {}, localPort: 8123, remotePort: 43217,
		phase: localProxyConnected, users: make(map[localProxySubscription]struct{}),
	}
	state.openLocalProxy(session)
	if got := state.localProxyUI.port.String(); got != "8123" {
		t.Fatalf("running local port is 8123 but locked field shows %q", got)
	}
}

func TestLocalProxyStoppedPanelKeepsRestoreWithoutOfferingStaleSetup(t *testing.T) {
	world, options := newSharingWorld(t)
	var clipboard string
	options.SetClipboardText = func(value string) error { clipboard = value; return nil }
	state := newTestState(t, options)
	world.connectFirst(t, state)
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	state.openLocalProxy(session)
	state.localProxyUI.lastRemotePort = 43217
	state.localProxyUI.lastStopped = true
	state.Click(hitLocalProxyEnableCopy)
	if clipboard != "" {
		t.Fatal("stopped tunnel copied a setup command using its old port")
	}
	state.closeModal()
	state.openLocalProxy(session)
	state.Click(hitLocalProxyMore)
	canvas, _ := paintAt(state, gfx.Size{Width: 1400, Height: 900})
	if hit, ok := hitByID(canvas, hitLocalProxyDisableCopy); !ok || hit.Disabled {
		t.Fatal("reopened stopped panel has no usable restore action")
	}
	state.Click(hitLocalProxyDisableCopy)
	if clipboard != localProxyDisableCommand() {
		t.Fatalf("restore clipboard = %q", clipboard)
	}
}

func TestLocalProxyPortCanBeEditedBeforeStartAndAfterStop(t *testing.T) {
	world, options := newSharingWorld(t)
	state := newTestState(t, options)
	world.connectFirst(t, state)
	state.lookupSystemProxy = nil
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	state.openLocalProxy(session)
	var nativeInput app.TextInput
	driver, err := app.NewDriver(app.Config{
		Title: "local proxy port editing", Width: 1400, Height: 900, Tree: Tree(),
		Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnShortcut: state.Shortcut,
		TextInput: func(context app.TextInputContext) *app.TextInput {
			if editor := state.EditorFor(context.ID); editor != nil {
				nativeInput = app.TextInput{Editor: editor, NavigationKeys: context.NavigationKeys}
				return &nativeInput
			}
			return nil
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer driver.Close()
	edit := func(value string) {
		t.Helper()
		var bounds gfx.Rect
		for _, hit := range driver.Snapshot().Hits {
			if hit.ID == hitLocalProxyPort {
				bounds = hit.Bounds
			}
		}
		if bounds.Width == 0 || bounds.Height == 0 {
			t.Fatal("port input is missing")
		}
		if err := driver.Click(bounds.X+bounds.Width/2, bounds.Y+bounds.Height/2); err != nil {
			t.Fatal(err)
		}
		if _, err := driver.Shortcut('a'); err != nil {
			t.Fatal(err)
		}
		if err := driver.Key(gfx.KeyBackspace, 0); err != nil {
			t.Fatal(err)
		}
		if err := driver.Type(value); err != nil {
			t.Fatal(err)
		}
		if got := state.localProxyUI.port.String(); got != value {
			t.Fatalf("edited port = %q, want %q", got, value)
		}
	}
	edit("8123")
	target, _ := state.localProxyTarget(session)
	state.localProxyManager().operations[target] = &localProxyOperation{
		target: target, cancel: func() {}, localPort: 8123, remotePort: 43217,
		phase: localProxyConnected, users: make(map[localProxySubscription]struct{}),
	}
	if err := driver.Resize(1400, 900); err != nil {
		t.Fatal(err)
	}
	if state.EditorFor(hitLocalProxyPort) != nil {
		t.Fatal("running tunnel allowed its local destination to change")
	}
	lockExplained := false
	for _, item := range driver.Snapshot().Texts {
		if strings.Contains(item.Value, "Stop the tunnel to change this port.") {
			lockExplained = true
		}
	}
	if !lockExplained {
		t.Fatal("locked port does not explain how to edit it")
	}
	state.stopLocalProxy()
	if err := driver.Resize(1400, 900); err != nil {
		t.Fatal(err)
	}
	edit("10809")
}

func TestLocalProxyStartCopiesOnlyAfterConnectingAndCanRetryClipboard(t *testing.T) {
	for _, failCopy := range []bool{false, true} {
		t.Run(fmt.Sprintf("clipboard_failure=%t", failCopy), func(t *testing.T) {
			world, options := newWorld(t)
			connection := &fakeConn{}
			world.conns = append(world.conns, connection)
			connection.listen = net.Listen
			connection.exec = func(_ context.Context, command string, _ int) (sshconn.ExecResult, error) {
				raw := strings.TrimSuffix(command[strings.LastIndex(command, ":")+1:], "'")
				return sshconn.ExecResult{Output: []byte(fmt.Sprintf("LISTEN 0 128 127.0.0.1:%s 0.0.0.0:*\n", raw))}, nil
			}
			options.Dial = func(sshconn.Config) Connection {
				return &localProxyTestLender{fakeConn: connection, shared: connection}
			}
			var clipboard string
			options.SetClipboardText = func(value string) error {
				if failCopy {
					return errors.New("clipboard unavailable")
				}
				clipboard = value
				return nil
			}
			state := newTestState(t, options)
			openHost(state)
			connection.set(func(status *sshconn.Status) {
				status.Phase, status.ConnectionGeneration = sshconn.PhaseConnected, 9001
			})
			state.Wake()
			session, _ := state.sessionByIDPointer(state.activeSessionID)
			state.openLocalProxy(session)
			port, closeProxy := localProxyTestPort(t)
			defer closeProxy()
			state.localProxyUI.port = text.Editor{}
			state.localProxyUI.port.Insert(strconv.Itoa(int(port)))
			target, _ := state.localProxyTarget(session)
			state.Click(hitLocalProxyStart)
			if clipboard != "" {
				t.Fatal("start copied a command before receiving the remote port")
			}
			snapshot := waitLocalProxyPhase(t, state.localProxyManager(), target, localProxyConnected)
			state.Wake()
			if failCopy {
				if state.modal != modalLocalProxy || state.localProxyUI.copyError == "" {
					t.Fatal("clipboard failure did not stay in the connected panel with an error")
				}
				failCopy = false
				state.Click(hitLocalProxyEnableCopy)
			}
			if clipboard != localProxyEnableCommand(snapshot.RemotePort) || state.modal != modalNone {
				t.Fatal("connected tunnel did not copy its actual port and return to the terminal")
			}
			if typed := session.term.shell.(*fakeShell).typed(); typed != "" {
				t.Fatalf("setup was injected into the terminal: %q", typed)
			}
			if !strings.Contains(state.status, "Paste") {
				t.Fatalf("copy feedback does not explain execution: %q", state.status)
			}
			canvas, _ := paintAt(state, gfx.Size{Width: 1400, Height: 900})
			if got, want := canvas.FocusRequest(), state.activeTerminalFocusHit(); got != want {
				t.Fatalf("returned focus = %v, want %v", got, want)
			}
		})
	}
}

func TestLocalProxyMoreKeepsRecoveryAvailableAndMainPanelCompact(t *testing.T) {
	world, options := newSharingWorld(t)
	state := newTestState(t, options)
	world.connectFirst(t, state)
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	state.openLocalProxy(session)
	canvas, _ := paintAt(state, gfx.Size{Width: 900, Height: 640})
	for _, id := range []gfx.ID{hitLocalProxyDisableCopy, hitLocalProxyTest} {
		if _, ok := hitByID(canvas, id); ok {
			t.Fatal("secondary action is competing with the initial start action")
		}
	}
	for _, hit := range canvas.Hits() {
		if hit.Label == "More options" {
			state.Click(hit.ID)
			break
		}
	}
	canvas, _ = paintAt(state, gfx.Size{Width: 900, Height: 640})
	if hit, ok := hitByID(canvas, hitLocalProxyDisableCopy); !ok || hit.Disabled {
		t.Fatal("expanded options have no restore command before starting a tunnel")
	}
}

func TestLocalProxyStopCopiesRestorationWithoutExecutingIt(t *testing.T) {
	world, options := newSharingWorld(t)
	var clipboard string
	options.SetClipboardText = func(value string) error { clipboard = value; return nil }
	state := newTestState(t, options)
	world.connectFirst(t, state)
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	state.openLocalProxy(session)
	target, _ := state.localProxyTarget(session)
	state.localProxyManager().operations[target] = &localProxyOperation{
		target: target, cancel: func() {}, remotePort: 43217,
		phase: localProxyConnected, users: make(map[localProxySubscription]struct{}),
	}
	state.Click(hitLocalProxyStop)
	if state.localProxyManager().Snapshot(target).Phase != localProxyDisconnected {
		t.Fatal("stop left the tunnel enabled")
	}
	if clipboard != localProxyDisableCommand() || state.modal != modalNone {
		t.Fatal("stop did not copy restoration and return to the terminal")
	}
	if session.term.shell.(*fakeShell).typed() != "" {
		t.Fatal("stop executed restoration in the terminal")
	}
}

func TestLocalProxyCopyReturnsToTheBoundTerminalFromOtherPages(t *testing.T) {
	for _, section := range []uint8{sectionHosts, sectionSettings, sectionSFTP} {
		t.Run(fmt.Sprintf("section=%d", section), func(t *testing.T) {
			world, options := newSharingWorld(t)
			var clipboard string
			options.SetClipboardText = func(value string) error { clipboard = value; return nil }
			state := newTestState(t, options)
			world.connectFirst(t, state)
			session, _ := state.sessionByIDPointer(state.activeSessionID)
			state.activeSection = section
			state.openLocalProxy(session)
			target, _ := state.localProxyTarget(session)
			state.localProxyManager().operations[target] = &localProxyOperation{
				target: target, cancel: func() {}, remotePort: 43217,
				phase: localProxyConnected, users: make(map[localProxySubscription]struct{}),
			}
			state.Click(hitLocalProxyEnableCopy)
			if clipboard != localProxyEnableCommand(43217) || state.activeSection != sectionTerminal || state.activeSessionID != session.ID || len(state.sessions) != 1 {
				t.Fatalf("copy returned to section=%d session=%d, want the existing terminal", state.activeSection, state.activeSessionID)
			}
			canvas, _ := paintAt(state, gfx.Size{Width: 1400, Height: 900})
			if got, want := canvas.FocusRequest(), state.activeTerminalFocusHit(); got != want || got.IsZero() {
				t.Fatalf("terminal focus = %v, want %v", got, want)
			}
		})
	}
}

func TestLocalProxyEnteringDockerCancelsLateShellCopy(t *testing.T) {
	world, options := newSharingWorld(t)
	var clipboard string
	options.SetClipboardText = func(value string) error { clipboard = value; return nil }
	state := newTestState(t, options)
	world.connectFirst(t, state)
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	state.openLocalProxy(session)
	target, _ := state.localProxyTarget(session)
	state.localProxyUI.copyTarget = target
	state.Click(hitLocalProxyDocker)
	state.Click(hitLocalProxyDockerRestore)
	state.localProxyManager().operations[target] = &localProxyOperation{
		target: target, cancel: func() {}, remotePort: 43217,
		phase: localProxyConnected, users: make(map[localProxySubscription]struct{}),
	}
	state.Wake()
	if clipboard != localProxyDockerRestoreCommand() || state.modal != modalLocalProxy || !state.localProxyUI.docker {
		t.Fatal("late startup replaced the Docker command or dismissed its page")
	}
}

func TestLocalProxyDisconnectCleansUpAndExplainsTheFailure(t *testing.T) {
	world, options := newSharingWorld(t)
	state := newTestState(t, options)
	connection := world.connectFirst(t, state)
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	state.openLocalProxy(session)
	target, _ := state.localProxyTarget(session)
	manager := state.localProxyManager()
	manager.operations[target] = &localProxyOperation{
		target: target, cancel: func() {}, remotePort: 43217,
		phase: localProxyConnected, users: make(map[localProxySubscription]struct{}),
	}
	state.joinLocalProxy(session)
	connection.set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseClosed })
	state.Wake()
	if manager.Snapshot(target).Phase != localProxyDisconnected || session.localProxy.token != 0 {
		t.Fatal("disconnected transport retained the proxy operation")
	}
	if state.modal != modalLocalProxy || state.localProxyUI.copyError != state.messages.LocalProxy.SSHDisconnected {
		t.Fatal("SSH disconnect silently reset the proxy panel instead of explaining the failure")
	}
}

func TestLocalProxyClosedOrReopenedPanelDoesNotCopyLateStartup(t *testing.T) {
	for _, reopen := range []bool{false, true} {
		t.Run(fmt.Sprintf("reopen=%t", reopen), func(t *testing.T) {
			world, options := newSharingWorld(t)
			clipboard := "keep my clipboard"
			options.SetClipboardText = func(value string) error { clipboard = value; return nil }
			state := newTestState(t, options)
			world.connectFirst(t, state)
			session, _ := state.sessionByIDPointer(state.activeSessionID)
			state.openLocalProxy(session)
			target, _ := state.localProxyTarget(session)
			state.localProxyUI.copyTarget = target
			state.closeModal()
			state.localProxyManager().operations[target] = &localProxyOperation{
				target: target, cancel: func() {}, remotePort: 43217,
				phase: localProxyConnected, users: make(map[localProxySubscription]struct{}),
			}
			if reopen {
				state.openLocalProxy(session)
			}
			state.Wake()
			if clipboard != "keep my clipboard" {
				t.Fatal("a closed panel's startup replaced the clipboard")
			}
			if reopen && state.modal != modalLocalProxy {
				t.Fatal("late startup dismissed a different panel")
			}
		})
	}
}

func TestLocalProxyStopStillStopsWhenClipboardFailsAndOffersRestoreRetry(t *testing.T) {
	world, options := newSharingWorld(t)
	failCopy := true
	var clipboard string
	options.SetClipboardText = func(value string) error {
		if failCopy {
			return errors.New("clipboard unavailable")
		}
		clipboard = value
		return nil
	}
	state := newTestState(t, options)
	world.connectFirst(t, state)
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	state.openLocalProxy(session)
	target, _ := state.localProxyTarget(session)
	state.localProxyManager().operations[target] = &localProxyOperation{
		target: target, cancel: func() {}, remotePort: 43217,
		phase: localProxyConnected, users: make(map[localProxySubscription]struct{}),
	}
	state.Click(hitLocalProxyStop)
	if state.localProxyManager().Snapshot(target).Phase != localProxyDisconnected || state.modal != modalLocalProxy {
		t.Fatal("clipboard failure prevented stopping or hid the failure")
	}
	canvas, _ := paintAt(state, gfx.Size{Width: 900, Height: 640})
	if hit, ok := hitByID(canvas, hitLocalProxyDisableCopy); !ok || hit.Disabled {
		t.Fatal("stopped panel did not offer a restore retry after clipboard failure")
	}
	failCopy = false
	state.Click(hitLocalProxyDisableCopy)
	if clipboard != localProxyDisableCommand() || state.modal != modalNone {
		t.Fatal("restore retry did not copy and return")
	}
}
