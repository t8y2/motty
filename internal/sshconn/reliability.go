package sshconn

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

const keepAliveRequest = "keepalive@openssh.com"

type keepAliveWorker struct {
	stop chan struct{}
	done chan struct{}
	once sync.Once
}

func (worker *keepAliveWorker) Close() error {
	worker.once.Do(func() { close(worker.stop) })
	<-worker.done
	return nil
}

func (config Config) validate() error {
	switch {
	case config.DialTimeout < 0:
		return errors.New("sshconn: dial timeout must not be negative")
	case config.KeepAlive.Interval < 0:
		return errors.New("sshconn: keepalive interval must not be negative")
	case config.KeepAlive.Timeout < 0:
		return errors.New("sshconn: keepalive timeout must not be negative")
	case config.KeepAlive.Interval == 0 && config.KeepAlive.Timeout != 0:
		return errors.New("sshconn: keepalive timeout requires an interval")
	case config.KeepAlive.Interval > 0 && config.KeepAlive.Timeout == 0 && config.KeepAlive.CountMax == nil:
		return errors.New("sshconn: keepalive timeout must be positive when enabled")
	case config.KeepAlive.CountMax != nil && (*config.KeepAlive.CountMax < 0 || config.KeepAlive.Timeout != 0):
		return errors.New("sshconn: keepalive count must be non-negative and cannot be combined with a timeout")
	default:
		for _, keepAlive := range config.EndpointKeepAlive {
			if err := (Config{KeepAlive: keepAlive}).validate(); err != nil {
				return err
			}
		}
		return config.validateRelays()
	}
}

// validateRelays refuses a relay that cannot be used: a bad command, or one for
// an endpoint that is not reached through a hop.
func (config Config) validateRelays() error {
	if len(config.Relays) == 0 {
		return nil
	}
	hopped := make(map[Endpoint]bool, len(config.ProxyJumps))
	for index, jump := range config.ProxyJumps {
		if index > 0 {
			hopped[jump.normalized()] = true
		}
	}
	if len(config.ProxyJumps) > 0 {
		hopped[Endpoint{Host: config.Host, Port: config.Port, User: config.User}.normalized()] = true
	}
	for endpoint, relay := range config.Relays {
		if !hopped[endpoint] {
			return fmt.Errorf("sshconn: relay for %s, which is not reached through a jump host", endpoint.address())
		}
		if err := CheckRelay(relay); err != nil {
			return fmt.Errorf("sshconn: relay for %s: %w", endpoint.address(), err)
		}
	}
	return nil
}

func (conn *Conn) dialThrough(client *ssh.Client, address string) (net.Conn, error) {
	ctx := conn.ctx
	cancel := func() {}
	if conn.config.DialTimeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, conn.config.DialTimeout)
	}
	defer cancel()
	return client.DialContext(ctx, "tcp", address)
}

// usesTCPKeepAlive reports whether the target transport is probed by the
// operating system's TCP keepalive. No keepalive@openssh.com request may be
// sent on such a transport: a restricted gateway can end the connection when it
// receives one. A shared tab borrows its owner's transport, so it asks the
// owner, and every read takes the same lock as the handshake's write.
func (conn *Conn) usesTCPKeepAlive() bool {
	owner := conn
	if conn.owner != nil {
		owner = conn.owner
	}
	owner.mu.Lock()
	defer owner.mu.Unlock()
	return owner.tcpKeepAlive
}

func (conn *Conn) startKeepAlive(client *ssh.Client) *keepAliveWorker {
	if conn.config.KeepAlive.Interval == 0 || conn.config.KeepAlive.CountMax != nil || conn.usesTCPKeepAlive() {
		return nil
	}
	worker := &keepAliveWorker{stop: make(chan struct{}), done: make(chan struct{})}
	go func() {
		defer close(worker.done)
		conn.keepAlive(client, worker.stop)
	}()
	return worker
}

func configureTCPKeepAlive(socket net.Conn, config KeepAliveConfig) bool {
	if config.Interval <= 0 {
		return false
	}
	tcp, ok := socket.(interface {
		SetKeepAliveConfig(net.KeepAliveConfig) error
	})
	if !ok {
		return false
	}
	settings := net.KeepAliveConfig{Enable: true, Idle: config.Interval, Interval: config.Interval, Count: 1}
	if config.Timeout > 0 {
		settings.Interval = config.Timeout
	}
	if config.CountMax != nil {
		settings.Count = max(1, *config.CountMax)
	}
	return tcp.SetKeepAliveConfig(settings) == nil
}

type aliveSocket struct {
	net.Conn
	received chan struct{}
}

func (socket *aliveSocket) Read(buffer []byte) (int, error) {
	count, err := socket.Conn.Read(buffer)
	if count > 0 {
		select {
		case socket.received <- struct{}{}:
		default:
		}
	}
	return count, err
}

func (conn *Conn) keepAliveFor(endpoint Endpoint) KeepAliveConfig {
	if config, ok := conn.config.EndpointKeepAlive[endpoint.normalized()]; ok {
		return config
	}
	if endpoint.normalized() == (Endpoint{Host: conn.config.Host, Port: conn.config.Port, User: conn.config.User}).normalized() {
		return conn.config.KeepAlive
	}
	return KeepAliveConfig{}
}

func (conn *Conn) startCountKeepAlive(client *ssh.Client, socket *aliveSocket, config KeepAliveConfig) {
	worker := &keepAliveWorker{stop: make(chan struct{}), done: make(chan struct{})}
	go func() {
		defer close(worker.done)
		timer := time.NewTimer(config.Interval)
		defer timer.Stop()
		replies := make(chan error, 1)
		pending := false
		missed := 0
		for {
			select {
			case <-conn.ctx.Done():
				return
			case <-worker.stop:
				return
			case <-socket.received:
				timer.Reset(config.Interval)
			case err := <-replies:
				pending = false
				if err != nil {
					return
				}
				missed = 0
				timer.Reset(config.Interval)
			case <-timer.C:
				if pending {
					// This worker's own probe is still unanswered: count the
					// round exactly as before, so an unresponsive server is
					// still declared dead after CountMax missed rounds.
					missed++
					if missed > *config.CountMax {
						if conn.recordLocalDisconnect(fmt.Errorf("%w: %w", ErrDisconnected, ErrKeepAliveTimeout)) {
							conn.cancel()
							_ = client.Close()
						}
						return
					}
					timer.Reset(config.Interval)
					continue
				}
				// A latency Ping holds the one global request the transport
				// may have out. Waiting behind it would measure that wait, so
				// this worker claims the same slot sendKeepAlive does and only
				// counts a missed round when it owns the request.
				owner := conn
				if conn.owner != nil {
					owner = conn.owner
				}
				if claimed, busy := owner.beginProbe(); !claimed {
					if busy >= config.Interval*time.Duration(*config.CountMax) {
						if conn.recordLocalDisconnect(fmt.Errorf("%w: %w", ErrDisconnected, ErrKeepAliveTimeout)) {
							conn.cancel()
							_ = client.Close()
						}
						return
					}
					timer.Reset(config.Interval)
					continue
				}
				missed++
				if missed > *config.CountMax {
					owner.endProbe()
					if conn.recordLocalDisconnect(fmt.Errorf("%w: %w", ErrDisconnected, ErrKeepAliveTimeout)) {
						conn.cancel()
						_ = client.Close()
					}
					return
				}
				pending = true
				go func() {
					_, _, err := client.SendRequest(keepAliveRequest, true, nil)
					owner.endProbe()
					replies <- err
				}()
				timer.Reset(config.Interval)
			}
		}
	}()
	conn.addCloser(worker)
}

func (conn *Conn) keepAlive(client *ssh.Client, stop <-chan struct{}) {
	ticker := time.NewTicker(conn.config.KeepAlive.Interval) //appaudit:allow SSH keepalive the user configured, while connected
	defer ticker.Stop()
	for {
		select {
		case <-conn.ctx.Done():
			return
		case <-stop:
			return
		case <-ticker.C:
			select {
			case <-conn.ctx.Done():
				return
			case <-stop:
				return
			default:
			}
			if !conn.sendKeepAlive(client) {
				return
			}
		}
	}
}

func (conn *Conn) sendKeepAlive(client *ssh.Client) bool {
	owner := conn
	if conn.owner != nil {
		owner = conn.owner
	}
	// The SSH client answers global requests one at a time, so a probe sent
	// while another is still out only waits behind it. Measuring this probe's
	// timeout from here would then measure that wait and call a transport that
	// answers as needed dead. Instead the transport is judged by the age of the
	// oldest unanswered request: skipping the round costs nothing while the
	// request is young, and an old one is the same failure this probe looks for.
	claimed, busy := owner.beginProbe()
	if !claimed {
		if busy >= conn.config.KeepAlive.Timeout {
			if conn.recordLocalDisconnect(fmt.Errorf("%w: %w", ErrDisconnected, ErrKeepAliveTimeout)) {
				_ = client.Close()
			}
			return false
		}
		return true
	}
	defer owner.endProbe()
	timedOut := make(chan struct{})
	timer := time.AfterFunc(conn.config.KeepAlive.Timeout, func() {
		if conn.recordLocalDisconnect(fmt.Errorf("%w: %w", ErrDisconnected, ErrKeepAliveTimeout)) {
			_ = client.Close()
		}
		close(timedOut)
	})
	_, _, err := client.SendRequest(keepAliveRequest, true, nil)
	if !timer.Stop() {
		<-timedOut
		return false
	}
	if err == nil {
		return true
	}
	if conn.ctx.Err() == nil && conn.recordDisconnect(fmt.Errorf("%w: keepalive: %w", ErrDisconnected, err)) {
		_ = client.Close()
	}
	return false
}

// beginProbe claims the one global request a transport may have outstanding.
// It reports false when an earlier request is still waiting for its answer, and
// busy is then how long that request has been out (zero if its start was not
// recorded, which only leaves it to the round after).
func (conn *Conn) beginProbe() (claimed bool, busy time.Duration) {
	if conn.pingPending.CompareAndSwap(false, true) {
		conn.probeSince.Store(time.Now().UnixNano())
		return true, 0
	}
	started := conn.probeSince.Load()
	if started == 0 {
		return false, 0
	}
	return false, time.Since(time.Unix(0, started))
}

// endProbe releases the request beginProbe claimed.
func (conn *Conn) endProbe() {
	conn.probeSince.Store(0)
	conn.pingPending.Store(false)
}

func (conn *Conn) recordDisconnect(err error) bool {
	return conn.recordDisconnectWithOrigin(err, false)
}

func (conn *Conn) recordLocalDisconnect(err error) bool {
	return conn.recordDisconnectWithOrigin(err, true)
}

func (conn *Conn) recordDisconnectWithOrigin(err error, local bool) bool {
	conn.mu.Lock()
	defer conn.mu.Unlock()
	if conn.ctx.Err() != nil || conn.disconnectErr != nil {
		return false
	}
	conn.disconnectErr = err
	conn.disconnectLocal = local
	return true
}

func (conn *Conn) isLocalDisconnect() bool {
	conn.mu.Lock()
	defer conn.mu.Unlock()
	return conn.disconnectLocal
}

func (conn *Conn) closedError(waitErr error) error {
	conn.mu.Lock()
	err := conn.disconnectErr
	conn.mu.Unlock()
	if err != nil {
		return err
	}
	if conn.ctx.Err() != nil {
		return nil
	}
	if waitErr == nil {
		return ErrDisconnected
	}
	return fmt.Errorf("%w: %w", ErrDisconnected, waitErr)
}

// ErrKeepAliveUnsupported reports that this transport is kept alive by the
// operating system's TCP keepalive instead of keepalive@openssh.com requests.
// A restricted gateway can end the connection on such a request, so Ping
// cannot measure its round trip and Verify has nothing to ask; both treat the
// operating-system probes as the liveness check instead.
var ErrKeepAliveUnsupported = errors.New("sshconn: transport uses TCP keepalive")

// verifyTimeout bounds how long Verify waits for the server's answer when the
// caller set no deadline of its own. It is shorter than a keepalive interval,
// because the caller is asking after a known event rather than on a schedule.
const verifyTimeout = 3 * time.Second

// Verify asks the server whether this connection still works, and records a
// disconnect when it does not.
//
// A changed network path calls this: the keepalive ticker may be a full
// interval away, and the answer is needed now. The request is the one keepalive
// sends, so it waits behind a probe already in flight (ErrPingPending) instead
// of queueing a second one. A failure ends the connection the way a keepalive
// timeout does — ErrDisconnected wrapping the cause — which is what starts a
// reconnect; a success changes nothing. A transport kept alive by the operating
// system is never asked: there is no SSH request to send, and its liveness is
// already watched, so Verify reports that nothing changed.
func (conn *Conn) Verify(ctx context.Context) error {
	owner := conn
	if conn.owner != nil {
		owner = conn.owner
	}
	owner.mu.Lock()
	client := owner.client
	phase := owner.status.Phase
	owner.mu.Unlock()
	if client == nil || phase != PhaseConnected {
		return errors.New("sshconn: not connected")
	}
	if owner.usesTCPKeepAlive() {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, verifyTimeout)
		defer cancel()
	}
	if claimed, _ := owner.beginProbe(); !claimed {
		return ErrPingPending
	}
	replied := make(chan error, 1)
	go func() {
		_, _, err := client.SendRequest(keepAliveRequest, true, nil)
		owner.endProbe()
		replied <- err
	}()
	var err error
	select {
	case err = <-replied:
	case <-ctx.Done():
		err = ctx.Err()
	}
	if err == nil {
		return nil
	}
	// One reason per transport, and no close when the connection was already
	// ending for another one: recordDisconnect answers false in both cases.
	if owner.recordLocalDisconnect(fmt.Errorf("%w: link changed: %w", ErrDisconnected, err)) {
		_ = client.Close()
	}
	return err
}

// ErrPingPending is returned by Ping while an earlier request has not been
// answered yet. Global requests are sent one at a time, so a new probe would
// only queue behind the old one, waiting on a reply that may never come and
// reporting the wait as round-trip time.
var ErrPingPending = errors.New("sshconn: previous keepalive request is still unresolved")

// Ping measures the round-trip time (RTT) to the SSH server using
// keepalive@openssh.com.
//
// A context without a deadline gets the default two-second one. The request
// itself cannot be canceled: after a timeout the client keeps waiting for the
// reply before it can send another global request. Ping therefore sends no new
// request until the previous one has been answered or the connection has ended,
// and returns ErrPingPending meanwhile. Tabs that share a client share this
// state. A transport kept alive by the operating system's TCP keepalive sends
// no request at all and returns ErrKeepAliveUnsupported, because a restricted
// gateway can end the connection on one.
func (conn *Conn) Ping(ctx context.Context) (time.Duration, error) {
	conn.mu.Lock()
	client := conn.client
	phase := conn.status.Phase
	conn.mu.Unlock()
	if client == nil || phase != PhaseConnected {
		return 0, errors.New("sshconn: not connected")
	}
	if conn.usesTCPKeepAlive() {
		return 0, ErrKeepAliveUnsupported
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
	}
	type pingResult struct {
		rtt time.Duration
		err error
	}
	owner := conn
	if conn.owner != nil {
		owner = conn.owner
	}
	if claimed, _ := owner.beginProbe(); !claimed {
		return 0, ErrPingPending
	}
	ch := make(chan pingResult, 1)
	start := time.Now()
	go func() {
		_, _, err := client.SendRequest(keepAliveRequest, true, nil)
		owner.endProbe()
		rtt := time.Since(start)
		if rtt <= 0 {
			rtt = time.Nanosecond
		}
		ch <- pingResult{rtt: rtt, err: err}
	}()
	select {
	case res := <-ch:
		return res.rtt, res.err
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}
