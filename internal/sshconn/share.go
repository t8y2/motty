package sshconn

import (
	"context"
	"errors"
	"slices"
)

// ErrNotShareable is why Share refused: the connection is not established, or
// it has ended.
var ErrNotShareable = errors.New("sshconn: the connection cannot be shared")

// Share opens another tab on this connection's authenticated SSH client, so a
// split or a copied tab needs no second handshake and no second password. The
// returned Conn is connected at once and has no goroutine of its own: its shells,
// file transfers and forwards are channels on the shared client, and it ends when
// its tab lets go (Cancel or Close) or when the connection itself ends.
//
// The connection stays up until every tab on it has let go, the one that dialed
// included. notify is called, like Config.Notify, after each change of the new
// Conn's status.
func (conn *Conn) Share(notify func()) (*Conn, error) {
	owner := conn
	if conn.owner != nil {
		owner = conn.owner
	}
	owner.mu.Lock()
	defer owner.mu.Unlock()
	if owner.terminated || owner.client == nil || owner.ctx.Err() != nil || !owner.retired && owner.status.Phase != PhaseConnected {
		return nil, ErrNotShareable
	}
	ctx, cancel := context.WithCancel(context.Background())
	config := owner.config
	config.Notify = notify
	status := owner.status
	status.Version = 1
	status.Phase = PhaseConnected
	status.Attempt, status.Questions, status.IdentityPath, status.Skipped, status.Err = 0, nil, "", nil, nil
	child := &Conn{
		config: config, ctx: ctx, cancel: cancel, done: make(chan struct{}),
		status: status, client: owner.client, owner: owner, probes: owner.probes,
	}
	owner.shares = append(owner.shares, child)
	return child, nil
}

// Shared reports whether this Conn borrows another's client, or lends its own to
// other tabs.
func (conn *Conn) Shared() bool {
	owner := conn
	if conn.owner != nil {
		owner = conn.owner
	}
	owner.mu.Lock()
	defer owner.mu.Unlock()
	return conn.owner != nil || len(owner.shares) > 0
}

// letGo is what Cancel does for a connection that others share: this tab stops
// using it, and the client closes only when no tab is left. It reports whether
// that was all there was to do.
func (conn *Conn) letGo() bool {
	if conn.owner != nil {
		conn.owner.release(conn)
		conn.endShare(nil)
		return true
	}
	conn.mu.Lock()
	if conn.client == nil || conn.retired || len(conn.shares) == 0 || conn.terminated {
		conn.mu.Unlock()
		return false
	}
	conn.retired = true
	conn.status.Version++
	conn.status.Phase = PhaseClosed
	conn.status.Err = nil
	conn.mu.Unlock()
	conn.notify()
	conn.finish()
	return true
}

// release removes a share from its owner, and ends the owner's connection when
// the owner's own tab has already gone and this was the last share.
func (conn *Conn) release(child *Conn) {
	conn.mu.Lock()
	for index, share := range conn.shares {
		if share == child {
			conn.shares = slices.Delete(conn.shares, index, index+1)
			break
		}
	}
	last := conn.retired && len(conn.shares) == 0
	conn.mu.Unlock()
	if last {
		conn.cancel()
	}
}

// endShare is the end of a shared tab: closed, with the reason if it did not
// choose to leave.
func (conn *Conn) endShare(err error) {
	conn.mu.Lock()
	if conn.status.Phase == PhaseClosed {
		conn.mu.Unlock()
		return
	}
	conn.status.Version++
	conn.status.Phase = PhaseClosed
	conn.status.Err = err
	conn.status.ProbeRefused = err != nil && conn.probeState().refused()
	conn.mu.Unlock()
	conn.cancel()
	conn.notify()
	conn.finish()
}

// endShares ends every tab that borrowed this connection, now that it is gone.
func (conn *Conn) endShares(err error) {
	conn.mu.Lock()
	conn.terminated = true
	shares := conn.shares
	conn.shares = nil
	conn.mu.Unlock()
	for _, share := range shares {
		share.endShare(err)
	}
}
