package workbench

import (
	"context"
	"errors"
	"net"
	"strconv"
	"sync"
	"time"

	"motty/internal/portforward"
	"motty/internal/sshconn"
)

type localProxyTarget struct {
	Host                 string
	Port                 uint16
	User                 string
	HostKeyFingerprint   string
	ConnectionGeneration uint64
}

type localProxySubscription struct {
	target localProxyTarget
	token  uint64
}

type localProxyPhase uint8

const (
	localProxyDisconnected localProxyPhase = iota
	localProxyConnecting
	localProxyConnected
	localProxyFailed
)

type localProxyTestPhase uint8

const (
	localProxyTestIdle localProxyTestPhase = iota
	localProxyTestRunning
	localProxyTestSucceeded
	localProxyTestFailed
)

type localProxySnapshot struct {
	Target     localProxyTarget
	Phase      localProxyPhase
	LocalPort  uint16
	RemotePort uint16
	Err        error
	TestPhase  localProxyTestPhase
	TestErr    error
}

type localProxyOperation struct {
	target     localProxyTarget
	source     Connection
	transport  Connection
	forward    *portforward.Forward
	ctx        context.Context
	cancel     context.CancelFunc
	localPort  uint16
	remotePort uint16
	phase      localProxyPhase
	err        error
	testPhase  localProxyTestPhase
	testErr    error
	testSerial uint64
	users      map[localProxySubscription]struct{}
}

// localProxyManager owns the process's explicit proxy tunnels. Its mutex
// protects state shared with startup/test workers; network work never runs
// while it is held.
type localProxyManager struct {
	mu         sync.Mutex
	operations map[localProxyTarget]*localProxyOperation
	tests      map[localProxyTarget]struct{}
	nextToken  uint64
	closed     bool
	wake       func()
}

func newLocalProxyManager(wake func()) *localProxyManager {
	return &localProxyManager{
		operations: make(map[localProxyTarget]*localProxyOperation),
		tests:      make(map[localProxyTarget]struct{}), wake: wake,
	}
}

func (manager *localProxyManager) Start(target localProxyTarget, source Connection, localPort uint16, existing localProxySubscription) (localProxySubscription, localProxySnapshot) {
	if manager == nil || source == nil || validateLocalProxyPort(localPort) != nil || target.ConnectionGeneration == 0 {
		return localProxySubscription{}, localProxySnapshot{Target: target, Phase: localProxyFailed, LocalPort: localPort, Err: errLocalProxyUnavailable}
	}
	manager.mu.Lock()
	if manager.closed {
		manager.mu.Unlock()
		return localProxySubscription{}, localProxySnapshot{Target: target, Phase: localProxyFailed, LocalPort: localPort, Err: errLocalProxyUnavailable}
	}
	subscription := existing
	if subscription.token == 0 || subscription.target != target {
		manager.nextToken++
		subscription = localProxySubscription{target: target, token: manager.nextToken}
	}
	operation := manager.operations[target]
	if operation != nil && (operation.phase == localProxyConnecting || operation.phase == localProxyConnected) {
		operation.users[subscription] = struct{}{}
		snapshot := localProxySnapshotFromOperation(operation)
		manager.mu.Unlock()
		return subscription, snapshot
	}
	var stale *localProxyOperation
	if operation == nil {
		ctx, cancel := context.WithCancel(context.Background())
		operation = &localProxyOperation{
			target: target, source: source, ctx: ctx, cancel: cancel, localPort: localPort,
			phase: localProxyConnecting, users: make(map[localProxySubscription]struct{}),
		}
		manager.operations[target] = operation
	} else {
		// Replace a failed operation rather than reusing it. A late startup or
		// test result keeps its old pointer and is discarded by runStart/Test.
		stale = operation
		ctx, cancel := context.WithCancel(context.Background())
		operation = &localProxyOperation{
			target: target, source: source, ctx: ctx, cancel: cancel, localPort: localPort,
			phase: localProxyConnecting, users: make(map[localProxySubscription]struct{}, len(stale.users)+1),
		}
		for user := range stale.users {
			operation.users[user] = struct{}{}
		}
		manager.operations[target] = operation
	}
	operation.users[subscription] = struct{}{}
	snapshot := localProxySnapshotFromOperation(operation)
	manager.mu.Unlock()

	if stale != nil {
		stale.cancel()
		closeLocalProxyResources(stale.forward, stale.transport)
	}
	go manager.runStart(operation)
	return subscription, snapshot
}

func (manager *localProxyManager) runStart(operation *localProxyOperation) {
	source := operation.source
	lender, sharable := source.(sharer)
	var transport Connection
	var err error
	if !sharable {
		err = errLocalProxyUnavailable
	} else {
		transport, err = lender.Share(manager.wake)
	}
	var forward *portforward.Forward
	if err == nil {
		manager.mu.Lock()
		if manager.closed || manager.operations[operation.target] != operation || operation.ctx.Err() != nil {
			manager.mu.Unlock()
			transport.Cancel()
			return
		}
		operation.transport = transport
		manager.mu.Unlock()
		forward, err = startLocalProxyTunnel(operation.ctx, transport, operation.localPort, manager.wake)
	}

	manager.mu.Lock()
	current := manager.operations[operation.target]
	if manager.closed || current != operation || operation.ctx.Err() != nil {
		manager.mu.Unlock()
		closeLocalProxyResources(forward, transport)
		return
	}
	if err != nil {
		operation.phase = localProxyFailed
		operation.err = err
		operation.transport = nil
		manager.mu.Unlock()
		if transport != nil {
			transport.Cancel()
		}
		manager.wakeWindows()
		return
	}
	operation.transport = transport
	operation.forward = forward
	operation.remotePort = localProxyPortFromAddress(forward.Status().ListenAddress)
	operation.phase = localProxyConnected
	operation.err = nil
	manager.mu.Unlock()
	manager.wakeWindows()
}

// JoinExisting attaches a passive session only when its exact transport already
// owns a tunnel. Opening or reconnecting a tab never starts one.
func (manager *localProxyManager) JoinExisting(target localProxyTarget, source Connection, existing localProxySubscription) localProxySubscription {
	if manager == nil || source == nil || target.ConnectionGeneration == 0 {
		return localProxySubscription{}
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	operation := manager.operations[target]
	if manager.closed || operation == nil {
		return localProxySubscription{}
	}
	subscription := existing
	if subscription.token == 0 || subscription.target != target {
		manager.nextToken++
		subscription = localProxySubscription{target: target, token: manager.nextToken}
	}
	operation.users[subscription] = struct{}{}
	return subscription
}

func (manager *localProxyManager) Leave(subscription localProxySubscription) {
	if manager == nil || subscription.token == 0 {
		return
	}
	manager.mu.Lock()
	operation := manager.operations[subscription.target]
	if operation == nil {
		manager.mu.Unlock()
		return
	}
	delete(operation.users, subscription)
	if len(operation.users) > 0 {
		manager.mu.Unlock()
		return
	}
	delete(manager.operations, subscription.target)
	operation.cancel()
	forward, transport := operation.forward, operation.transport
	manager.mu.Unlock()
	closeLocalProxyResources(forward, transport)
	manager.wakeWindows()
}

func (manager *localProxyManager) Stop(target localProxyTarget) {
	if manager == nil {
		return
	}
	manager.mu.Lock()
	operation := manager.operations[target]
	if operation == nil {
		manager.mu.Unlock()
		return
	}
	delete(manager.operations, target)
	operation.cancel()
	operation.testSerial++
	forward, transport := operation.forward, operation.transport
	manager.mu.Unlock()
	closeLocalProxyResources(forward, transport)
	manager.wakeWindows()
}

func (manager *localProxyManager) Snapshot(target localProxyTarget) localProxySnapshot {
	if manager == nil {
		return localProxySnapshot{Target: target, Phase: localProxyDisconnected}
	}
	manager.mu.Lock()
	operation := manager.operations[target]
	if operation == nil {
		manager.mu.Unlock()
		return localProxySnapshot{Target: target, Phase: localProxyDisconnected}
	}
	var failedForward *portforward.Forward
	var failedTransport Connection
	if operation.transport != nil {
		status := operation.transport.Status()
		if status.Phase != sshconn.PhaseConnected && operation.phase == localProxyConnected {
			operation.phase = localProxyFailed
			operation.err = errLocalProxySSHDisconnected
			failedForward, failedTransport = operation.forward, operation.transport
			operation.forward, operation.transport = nil, nil
			operation.cancel()
		}
	}
	snapshot := localProxySnapshotFromOperation(operation)
	manager.mu.Unlock()
	closeLocalProxyResources(failedForward, failedTransport)
	return snapshot
}

func (manager *localProxyManager) Test(target localProxyTarget) {
	manager.mu.Lock()
	operation := manager.operations[target]
	if manager.closed || operation == nil || operation.phase != localProxyConnected || operation.transport == nil || operation.remotePort == 0 || operation.testPhase == localProxyTestRunning {
		manager.mu.Unlock()
		return
	}
	// A timed-out SSH read may still await its peer after Close. Keep its slot
	// across stop/restart so retries cannot accumulate workers on that transport.
	if _, pending := manager.tests[target]; pending {
		operation.testPhase, operation.testErr = localProxyTestFailed, errLocalProxyTunnel
		manager.mu.Unlock()
		manager.wakeWindows()
		return
	}
	manager.tests[target] = struct{}{}
	operation.testSerial++
	serial := operation.testSerial
	operation.testPhase = localProxyTestRunning
	operation.testErr = nil
	transport, remotePort := operation.transport, operation.remotePort
	ctx, cancel := context.WithTimeout(operation.ctx, localProxyTestTime)
	manager.mu.Unlock()
	manager.wakeWindows()

	result := make(chan error, 1)
	go func() {
		err := testLocalProxyTunnel(ctx, transport, remotePort)
		manager.mu.Lock()
		delete(manager.tests, target)
		manager.mu.Unlock()
		result <- err
	}()
	go func() {
		defer cancel()
		var err error
		select {
		case err = <-result:
		case <-ctx.Done():
			err = errLocalProxyTunnel
		}
		manager.mu.Lock()
		if manager.closed || manager.operations[target] != operation || operation.testSerial != serial || operation.phase != localProxyConnected {
			manager.mu.Unlock()
			return
		}
		if err != nil {
			operation.testPhase, operation.testErr = localProxyTestFailed, err
		} else {
			operation.testPhase, operation.testErr = localProxyTestSucceeded, nil
		}
		manager.mu.Unlock()
		manager.wakeWindows()
	}()
}

func (manager *localProxyManager) Close() {
	if manager == nil {
		return
	}
	manager.mu.Lock()
	if manager.closed {
		manager.mu.Unlock()
		return
	}
	manager.closed = true
	operations := make([]*localProxyOperation, 0, len(manager.operations))
	for _, operation := range manager.operations {
		operations = append(operations, operation)
	}
	clear(manager.operations)
	manager.mu.Unlock()
	for _, operation := range operations {
		operation.cancel()
		closeLocalProxyResources(operation.forward, operation.transport)
	}
}

func (manager *localProxyManager) wakeWindows() {
	if manager != nil && manager.wake != nil {
		manager.wake()
	}
}

func (state *State) localProxyManager() *localProxyManager {
	if state.options.Group != nil {
		state.options.Group.shareOnce.Do(func() {
			state.options.Group.shares = newLocalProxyManager(state.options.Group.LocalProxyChanged)
		})
		return state.options.Group.shares
	}
	state.localProxyOnce.Do(func() {
		state.localProxyFallback = newLocalProxyManager(func() {
			if state.options.Invalidate != nil {
				state.options.Invalidate()
			}
		})
	})
	return state.localProxyFallback
}

func (state *State) closeLocalProxySessions() {
	for index := range state.sessions {
		state.leaveLocalProxy(&state.sessions[index])
	}
	if state.options.Group == nil && state.localProxyFallback != nil {
		state.localProxyFallback.Close()
		state.localProxyFallback = nil
	}
}

func (state *State) leaveLocalProxy(session *Session) {
	if session == nil || session.localProxy.token == 0 {
		return
	}
	subscription := session.localProxy
	session.localProxy = localProxySubscription{}
	state.localProxyManager().Leave(subscription)
}

func (state *State) localProxyTarget(session *Session) (localProxyTarget, bool) {
	if session == nil || session.conn == nil || session.isLocal() || !session.host.isSSH() || session.phase != sshconn.PhaseConnected {
		return localProxyTarget{}, false
	}
	status := session.conn.Status()
	if status.Phase != sshconn.PhaseConnected || status.ConnectionGeneration == 0 {
		return localProxyTarget{}, false
	}
	return localProxyTarget{
		Host: session.Address, Port: session.host.Port, User: state.login(session.host),
		HostKeyFingerprint: status.HostKey.Fingerprint, ConnectionGeneration: status.ConnectionGeneration,
	}, true
}

func (state *State) joinLocalProxy(session *Session) {
	target, ok := state.localProxyTarget(session)
	if !ok {
		return
	}
	if session.localProxy.token != 0 && session.localProxy.target != target {
		state.leaveLocalProxy(session)
	}
	session.localProxy = state.localProxyManager().JoinExisting(target, session.conn, session.localProxy)
}

// Register existing tabs before the initiating tab can close, without waiting
// for the other windows to observe the new tunnel in Wake.
func (state *State) joinLocalProxyUsers(target localProxyTarget) {
	members := []*State{state}
	if group := state.options.Group; group != nil {
		group.mu.Lock()
		members = append([]*State(nil), group.members...)
		group.mu.Unlock()
	}
	for _, member := range members {
		if member.closed {
			continue
		}
		for index := range member.sessions {
			session := &member.sessions[index]
			if current, ok := member.localProxyTarget(session); ok && current == target {
				member.joinLocalProxy(session)
			}
		}
	}
}

func (state *State) syncLocalProxySessions() {
	for index := range state.sessions {
		session := &state.sessions[index]
		if session.localProxy.token != 0 {
			target, ok := state.localProxyTarget(session)
			if !ok || target != session.localProxy.target {
				if state.modal == modalLocalProxy && state.localProxyUI.session == session.ID {
					state.localProxyUI.copyTarget = localProxyTarget{}
					state.localProxyUI.copyError = state.messages.LocalProxy.SSHDisconnected
				}
				state.leaveLocalProxy(session)
			}
		}
		state.joinLocalProxy(session)
	}
	if state.modal == modalLocalProxy && state.localProxySession() == nil {
		state.closeModal()
	}
	state.finishLocalProxyStart()
}

func (state *State) localProxySnapshot(session *Session) localProxySnapshot {
	target, ok := state.localProxyTarget(session)
	if !ok {
		return localProxySnapshot{Phase: localProxyDisconnected}
	}
	return state.localProxyManager().Snapshot(target)
}

func localProxySnapshotFromOperation(operation *localProxyOperation) localProxySnapshot {
	return localProxySnapshot{
		Target: operation.target, Phase: operation.phase, LocalPort: operation.localPort,
		RemotePort: operation.remotePort, Err: operation.err,
		TestPhase: operation.testPhase, TestErr: operation.testErr,
	}
}

func closeLocalProxyResources(forward *portforward.Forward, transport Connection) {
	if forward != nil {
		forward.CloseAsync()
	}
	if transport != nil {
		transport.Cancel()
	}
}

func localProxyPortFromAddress(address string) uint16 {
	_, text, err := net.SplitHostPort(address)
	if err != nil {
		return 0
	}
	port, err := strconv.ParseUint(text, 10, 16)
	if err != nil {
		return 0
	}
	return uint16(port)
}

var (
	errLocalProxyUnavailable     = errors.New("local proxy: the current SSH connection cannot be shared")
	errLocalProxyInvalidPort     = errors.New("local proxy: port must be between 1 and 65535")
	errLocalProxyProbe           = errors.New("local proxy: no HTTP proxy is listening on the selected loopback port")
	errLocalProxyForwarding      = errors.New("local proxy: the SSH server refused remote forwarding")
	errLocalProxySSHDisconnected = errors.New("local proxy: the SSH connection was lost")
	errLocalProxyAuth            = errors.New("local proxy: authentication is not supported")
	errLocalProxyTunnel          = errors.New("local proxy: connection test through the tunnel failed")
)

func validateLocalProxyPort(port uint16) error {
	if port == 0 {
		return errLocalProxyInvalidPort
	}
	return nil
}

func localProxyAddress(port uint16) string {
	return net.JoinHostPort("127.0.0.1", strconv.Itoa(int(port)))
}

const (
	localProxyTestTarget = "example.com:443"
	localProxyTestLimit  = 64 << 10
	localProxyTestTime   = 12 * time.Second
	localProxyMaxStreams = 64
)
