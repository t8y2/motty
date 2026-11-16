package sshconn

import (
	"context"
	"errors"
	"net"
)

// ListenContext cancels the caller's wait without closing other SSH sessions.
// check may reject the listener before ownership transfers to the caller.
// Canceled or rejected listeners finish closing before another request starts.
func (conn *Conn) ListenContext(ctx context.Context, network, address string, check func(net.Listener) error) (net.Listener, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	client := conn.Client()
	if client == nil {
		return nil, errors.New("sshconn: not connected")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if conn.ctx != nil {
		stop := context.AfterFunc(conn.ctx, cancel)
		defer stop()
	}
	owner := conn
	if conn.owner != nil {
		owner = conn.owner
	}
	owner.mu.Lock()
	if owner.listenPending == nil {
		owner.listenPending = make(chan struct{}, 1)
	}
	pending := owner.listenPending
	owner.mu.Unlock()

	// SSH global requests have no individual cancellation. Keep only one
	// outstanding listener request per transport, including canceled attempts.
	select {
	case pending <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		<-pending
		return nil, err
	}
	type result struct {
		listener net.Listener
		err      error
	}
	opened := make(chan result)
	go func() {
		defer func() { <-pending }()
		listener, err := client.Listen(network, address)
		if err == nil {
			err = ctx.Err()
			if err == nil && check != nil {
				err = check(listener)
			}
		}
		var ready net.Listener
		if err == nil {
			ready = listener
		}
		select {
		case opened <- result{ready, err}:
			if err == nil {
				listener = nil
			}
		case <-ctx.Done():
		}
		// Publish rejection first, but retain the transport's request slot
		// while cancel-tcpip-forward waits for its SSH reply.
		if listener != nil {
			_ = listener.Close()
		}
	}()
	select {
	case result := <-opened:
		return result.listener, result.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
