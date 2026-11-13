package mosh

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"sync"
	"syscall"
	"time"
)

// InputID identifies one piece of user input. It increases with every call to
// SendInput, so a caller can ask later whether the server has applied it
// (AckedInput). The terminal's local prediction uses that to keep a guess on
// screen while a slow link still has the keystroke in flight.
type InputID uint64

// Config describes one mosh client session. The caller owns the network
// policy: it resolves the server and opens the datagram socket (direct,
// IPv4 or IPv6), and the client only speaks the protocol over it.
type Config struct {
	// Conn is the datagram socket. It should not be connected to the server:
	// an unconnected socket keeps working when the local address changes, and
	// its writes are retried by the protocol. The client closes it when the
	// session ends, including when Dial rejects the key.
	Conn net.PacketConn
	// Server is the address mosh-server announced (or the address the SSH
	// connection reached). Datagrams are sent here.
	Server net.Addr
	// Key is the 22-character base64 key from the MOSH CONNECT line, with or
	// without padding.
	Key string
	// Cols, Rows are the initial terminal size. Zero means 80x24.
	Cols, Rows int
	// Notify is called after host output arrives and when the client ends. It
	// may be called from any goroutine and must not block.
	Notify func()
}

// Client is a mosh client session: SSP transport, key, resize, input and
// server output. It runs one goroutine that blocks on the datagram socket
// with the protocol's own deadlines; there is no periodic ticker.
type Client struct {
	conn      net.PacketConn
	server    net.Addr
	transport *Transport
	notify    func()

	done chan struct{}
	once sync.Once

	mu      sync.Mutex
	output  []byte
	actions []UserInstruction
	// ackedActions is how many actions the newest acknowledged state carried.
	ackedActions int
	// sentActionCounts maps a sent state number to the action count it carried,
	// so an acknowledgement can advance ackedActions.
	sentActionCounts map[uint64]int
	// pendingInputs are input actions not yet included in a state. inputStates
	// are those that are in a sent state, waiting for the server's echo ack.
	pendingInputs []pendingInput
	inputStates   []sentInput
	inputID       InputID
	ackedInput    InputID
	// transportAcked is the newest user state the SSP layer acknowledged. It
	// advances ackedActions: a new user diff is relative to a state the server
	// has, and basing it on the echo ack instead would resend keystrokes whose
	// state was received but whose echo ack had not arrived.
	transportAcked uint64

	closing  bool
	wake     bool
	finishAt time.Time
	lastRecv time.Time
	err      error
}

type pendingInput struct {
	id        InputID
	actionEnd int
}

type sentInput struct {
	id    InputID
	state uint64
}

// Dial starts a session. It does not wait for the server to answer: the first
// datagrams (the initial resize) are sent by the session's goroutine.
func Dial(config Config) (*Client, error) {
	if config.Conn == nil {
		return nil, errors.New("mosh: nil datagram connection")
	}
	if config.Server == nil {
		return nil, errors.New("mosh: nil server address")
	}
	key := config.Key
	for len(key)%4 != 0 {
		key += "="
	}
	rawKey, err := base64.StdEncoding.DecodeString(key)
	if err != nil {
		_ = config.Conn.Close()
		return nil, fmt.Errorf("mosh: bad key: %w", err)
	}
	ocb, err := NewOCB(rawKey)
	if err != nil {
		_ = config.Conn.Close()
		return nil, err
	}
	cols, rows := config.Cols, config.Rows
	if cols <= 0 || rows <= 0 {
		cols, rows = 80, 24
	}

	c := &Client{
		conn:             config.Conn,
		server:           config.Server,
		transport:        NewTransport(ocb, false),
		notify:           config.Notify,
		done:             make(chan struct{}),
		sentActionCounts: make(map[uint64]int),
		actions:          []UserInstruction{{Width: int32(cols), Height: int32(rows)}},
	}
	go c.run()
	return c, nil
}

// SendInput queues keystrokes and returns the id of this input. It never
// blocks and may be called from any goroutine.
func (c *Client) SendInput(p []byte) InputID {
	if len(p) == 0 {
		return 0
	}
	c.mu.Lock()
	if c.closing {
		c.mu.Unlock()
		return 0
	}
	c.inputID++
	id := c.inputID
	c.actions = append(c.actions, UserInstruction{Keys: append([]byte(nil), p...)})
	c.pendingInputs = append(c.pendingInputs, pendingInput{id: id, actionEnd: len(c.actions)})
	c.wake = true
	c.mu.Unlock()
	c.wakeRead()
	return id
}

// Resize queues a terminal size change. The protocol coalesces it with any
// input waiting in the same state.
func (c *Client) Resize(cols, rows int) {
	if cols <= 0 || rows <= 0 {
		return
	}
	c.mu.Lock()
	if c.closing {
		c.mu.Unlock()
		return
	}
	if n := len(c.actions); n > 0 && c.actions[n-1].Keys == nil && c.actions[n-1].Control == nil {
		c.actions[n-1].Width, c.actions[n-1].Height = int32(cols), int32(rows)
	} else {
		c.actions = append(c.actions, UserInstruction{Width: int32(cols), Height: int32(rows)})
	}
	c.wake = true
	c.mu.Unlock()
	c.wakeRead()
}

// AckedInput is the newest input the server has acknowledged applying. A
// caller that recorded the id returned by SendInput can compare it with this.
func (c *Client) AckedInput() InputID {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ackedInput
}

// LastRecv is when a valid datagram last arrived. It is the link's liveness,
// not the server's screen state.
func (c *Client) LastRecv() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastRecv
}

// Drain appends output received since the last call to dst.
func (c *Client) Drain(dst []byte) []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.output) == 0 {
		return dst
	}
	dst = append(dst, c.output...)
	c.output = c.output[:0]
	return dst
}

// Done is closed once the session has ended and its output is drainable.
func (c *Client) Done() <-chan struct{} { return c.done }

// Err says why the session ended; nil for a clean shutdown or local close.
func (c *Client) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

// Close starts the shutdown handshake. It returns at once; Done closes when
// the remote acknowledged the shutdown or the handshake timed out. A session
// that never heard from the server is closed immediately.
func (c *Client) Close() {
	select {
	case <-c.done:
		return
	default:
	}
	c.mu.Lock()
	if c.closing {
		c.mu.Unlock()
		return
	}
	c.closing = true
	c.wake = true
	c.mu.Unlock()
	c.wakeRead()
}

// run is the session's only goroutine. It owns the transport and the socket.
func (c *Client) run() {
	defer c.finish(c.Err())
	buf := make([]byte, 64*1024)
	shutdownStarted := false
	for {
		now := time.Now()
		c.processAcks()
		c.fillPending(now)
		if c.isClosing() && !shutdownStarted {
			shutdownStarted = true
			if !c.transport.Heard() {
				return
			}
			c.transport.Shutdown(now)
		}
		if c.transport.ShutdownTimedOut(now) {
			return
		}
		for _, datagram := range c.transport.Tick(now) {
			// A failed write is not fatal: the network may be between
			// addresses, and the protocol retransmits.
			_, _ = c.conn.WriteTo(datagram, c.server)
		}
		if c.transport.ShutdownAcked() {
			return
		}
		if c.transport.CounterpartyShutdown() {
			if c.finishAt.IsZero() {
				c.finishAt = now.Add(100 * time.Millisecond)
			} else if !now.Before(c.finishAt) {
				return
			}
		}
		wait := c.transport.NextWake(now)
		if !c.finishAt.IsZero() {
			if remaining := c.finishAt.Sub(now); remaining < wait {
				wait = remaining
			}
		}
		if wait < 0 {
			wait = 0
		}

		_ = c.conn.SetReadDeadline(now.Add(wait))
		// SendInput/Close may have run between computing wait and setting the
		// deadline. Re-check the flag while holding the same lock they use, so
		// an input cannot wait for the whole heartbeat.
		c.mu.Lock()
		if c.wake {
			c.wake = false
			c.mu.Unlock()
			_ = c.conn.SetReadDeadline(time.Now())
		} else {
			c.mu.Unlock()
		}

		n, _, err := c.conn.ReadFrom(buf)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			if errors.Is(err, net.ErrClosed) {
				return
			}
			if errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.ECONNRESET) {
				// Windows reports an ICMP "port unreachable" or "reset" on a
				// later read of a UDP socket. The server may be between
				// addresses; keep the session and let the protocol retry.
				time.Sleep(100 * time.Millisecond)
				continue
			}
			c.setErr(err)
			return
		}
		now = time.Now()
		diff := c.transport.Recv(buf[:n], now)
		c.mu.Lock()
		if c.transport.Heard() {
			c.lastRecv = c.transport.LastRecv()
		}
		c.mu.Unlock()
		if len(diff) > 0 {
			c.applyHostMessage(diff)
		}
	}
}

func (c *Client) isClosing() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closing
}

// processAcks advances the action baseline to the newest state the SSP layer
// acknowledged. Only then is the next cumulative user diff relative to a state
// the server actually has; using the echo ack here would send the same keys
// again whenever the server's screen acknowledgement lagged its transport ack.
func (c *Client) processAcks() {
	c.mu.Lock()
	defer c.mu.Unlock()
	acked := c.transport.AckedByRemote()
	if acked <= c.transportAcked {
		return
	}
	c.transportAcked = acked
	for state := range c.sentActionCounts {
		if state <= acked {
			if count := c.sentActionCounts[state]; count > c.ackedActions {
				c.ackedActions = count
			}
			delete(c.sentActionCounts, state)
		}
	}
	c.compactActions()
}

// fillPending builds the next cumulative user state from actions that the
// server has not acknowledged yet. Only one state is in flight at a time.
func (c *Client) fillPending(now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closing {
		return
	}
	if c.transport.HasPending() && !c.transport.HasUnsentPending() {
		return
	}
	if len(c.actions) <= c.ackedActions {
		return
	}
	diff := MarshalUserMessage(c.actions[c.ackedActions:])
	state := c.transport.SentNum() + 1
	c.transport.SetPending(now, diff)
	c.sentActionCounts[state] = len(c.actions)
	for len(c.pendingInputs) > 0 && c.pendingInputs[0].actionEnd <= len(c.actions) {
		c.inputStates = append(c.inputStates, sentInput{id: c.pendingInputs[0].id, state: state})
		c.pendingInputs = c.pendingInputs[1:]
	}
	// A server that never reports an echo ack must not make this list grow for
	// the life of the session; the oldest entries are the ones furthest behind.
	if len(c.inputStates) > maxInputStates {
		c.inputStates = c.inputStates[len(c.inputStates)-maxInputStates:]
	}
}

// applyHostMessage delivers the server's output and records what it has
// applied. EchoAck is the state number the server last applied to the
// terminal; it retires input ids, not the screen text (the scrollback is
// judged by the terminal itself).
func (c *Client) applyHostMessage(diff []byte) {
	instructions, err := UnmarshalHostMessage(diff)
	if err != nil {
		return
	}
	var output []byte
	echoAck := int64(-1)
	for i := range instructions {
		output = append(output, instructions[i].Hoststring...)
		if instructions[i].EchoAckNum > echoAck {
			echoAck = instructions[i].EchoAckNum
		}
	}
	notify := false
	c.mu.Lock()
	if len(output) > 0 {
		c.output = append(c.output, output...)
		notify = true
	}
	if echoAck >= 0 {
		c.applyEchoAck(uint64(echoAck))
		// An acknowledgement may arrive without new screen text; the owner
		// still has to learn that its input was applied.
		notify = true
	}
	c.mu.Unlock()
	if notify && c.notify != nil {
		c.notify()
	}
}

// applyEchoAck retires input ids carried by states at or below ack. Caller
// holds c.mu.
func (c *Client) applyEchoAck(ack uint64) {
	for len(c.inputStates) > 0 && c.inputStates[0].state <= ack {
		if c.inputStates[0].id > c.ackedInput {
			c.ackedInput = c.inputStates[0].id
		}
		c.inputStates = c.inputStates[1:]
	}
}

// maxInputStates bounds the ids waiting for an echo ack. A conforming server
// retires them on every applied input state; this only caps a misbehaving one.
const maxInputStates = 1024

// compactActions drops the acknowledged prefix of the action log. Without it a
// long-lived session would keep every keystroke it ever sent.
func (c *Client) compactActions() {
	if c.ackedActions < 256 || c.ackedActions < len(c.actions)/2 {
		return
	}
	drop := c.ackedActions
	remaining := len(c.actions) - drop
	copy(c.actions, c.actions[drop:])
	for i := remaining; i < len(c.actions); i++ {
		c.actions[i] = UserInstruction{} // release the key bytes in the tail
	}
	c.actions = c.actions[:remaining]
	c.ackedActions = 0
	for i := range c.pendingInputs {
		c.pendingInputs[i].actionEnd -= drop
	}
	for state, count := range c.sentActionCounts {
		c.sentActionCounts[state] = count - drop
	}
}

func (c *Client) wakeRead() {
	// Setting the read deadline to the present wakes a blocked ReadFrom; the
	// loop then sees the wake flag (or simply recomputes what is due).
	_ = c.conn.SetReadDeadline(time.Now())
}

func (c *Client) setErr(err error) {
	c.mu.Lock()
	if c.err == nil {
		c.err = err
	}
	c.mu.Unlock()
}

// finish closes the socket and publishes the end of the session once.
func (c *Client) finish(err error) {
	c.once.Do(func() {
		if err != nil {
			c.setErr(err)
		}
		_ = c.conn.Close()
		close(c.done)
		if c.notify != nil {
			c.notify()
		}
	})
}
