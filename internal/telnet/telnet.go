// Package telnet is a Telnet client for a terminal: the NVT with the options a
// terminal needs (remote echo, suppress go-ahead, terminal type, window size,
// binary). It speaks to a net.Conn and knows nothing of windows or screens.
//
// Telnet is not encrypted and has no authentication of its own; what is typed
// travels in the clear. It is here for devices that offer nothing else.
package telnet

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"time"

	"motty/internal/shellkind"
)

// Telnet commands (RFC 854) and options.
const (
	iac  = 255
	dont = 254
	do   = 253
	wont = 252
	will = 251
	sb   = 250
	se   = 240

	optBinary = 0
	optEcho   = 1
	optSGA    = 3
	optTType  = 24
	optNAWS   = 31

	ttypeIs   = 0
	ttypeSend = 1

	// maxSubnegotiation bounds a subnegotiation a server sends.
	maxSubnegotiation = 256
	// maxPendingInput bounds the input queued for a server that does not read.
	maxPendingInput = 64 << 10
	// maxPendingOutput bounds output not yet drained by the owner.
	maxPendingOutput = 8 << 20
)

// Dialer opens the TCP connection, directly or through a proxy.
type Dialer func(ctx context.Context, address string) (net.Conn, error)

// Dial connects to address (host:port) and starts the session.
func Dial(ctx context.Context, address string, timeout time.Duration, dialer Dialer, config Config) (*Session, error) {
	if dialer == nil {
		direct := net.Dialer{Timeout: timeout}
		dialer = func(ctx context.Context, address string) (net.Conn, error) {
			return direct.DialContext(ctx, "tcp", address)
		}
	}
	socket, err := dialer(ctx, address)
	if err != nil {
		return nil, err
	}
	return Start(socket, config), nil
}

// Config describes the terminal the server is talking to.
type Config struct {
	Cols, Rows int
	// Term is the terminal type offered. Empty means xterm-256color.
	Term string
	// Notify is called after output arrives and when the session ends. It may
	// be called from any goroutine and must not block.
	Notify func()
}

// Session is one Telnet session. It satisfies the shell the workbench drives.
type Session struct {
	socket net.Conn
	config Config

	mu           sync.Mutex
	output       []byte
	err          error
	closed       bool
	cols         int
	rows         int
	us           [256]bool // options we have enabled (we WILL)
	him          [256]bool // options the server has enabled (it WILLs)
	crSeen       bool      // the last data byte was CR, so a following NUL or LF belongs to it
	pending      [][]byte  // negotiation replies and input waiting for the writer
	pendingBytes int
	wake         chan struct{}
	done         chan struct{}
}

// Start runs a session over an open connection.
func Start(socket net.Conn, config Config) *Session {
	if config.Term == "" {
		config.Term = "xterm-256color"
	}
	session := &Session{
		socket: socket, config: config, cols: max(config.Cols, 1), rows: max(config.Rows, 1),
		wake: make(chan struct{}, 1), done: make(chan struct{}),
	}
	go session.read()
	go session.write()
	return session
}

func (session *Session) notify() {
	if session.config.Notify != nil {
		session.config.Notify()
	}
}

// finish records why the session ended; the first reason wins.
func (session *Session) finish(err error) {
	session.mu.Lock()
	if session.closed {
		session.mu.Unlock()
		return
	}
	session.closed = true
	if err != nil && !errors.Is(err, net.ErrClosed) {
		session.err = err
	}
	session.mu.Unlock()
	session.socket.Close()
	close(session.done)
	select {
	case session.wake <- struct{}{}:
	default:
	}
	session.notify()
}

// queue puts bytes on the line to the server.
func (session *Session) queue(data []byte, force bool) bool {
	session.mu.Lock()
	if session.closed || !force && session.pendingBytes+len(data) > maxPendingInput {
		session.mu.Unlock()
		return false
	}
	session.enqueueLocked(data)
	session.mu.Unlock()
	return true
}

// enqueueLocked adds bytes to the line to the server and wakes the writer. The
// caller holds the lock.
func (session *Session) enqueueLocked(data []byte) {
	session.pending = append(session.pending, append([]byte(nil), data...))
	session.pendingBytes += len(data)
	select {
	case session.wake <- struct{}{}:
	default:
	}
}

func (session *Session) write() {
	for {
		select {
		case <-session.wake:
		case <-session.done:
			return
		}
		for {
			session.mu.Lock()
			if len(session.pending) == 0 {
				session.mu.Unlock()
				break
			}
			chunk := session.pending[0]
			session.pending = session.pending[1:]
			session.pendingBytes -= len(chunk)
			session.mu.Unlock()
			_ = session.socket.SetWriteDeadline(time.Now().Add(30 * time.Second))
			if _, err := session.socket.Write(chunk); err != nil {
				session.finish(err)
				return
			}
		}
	}
}

// Write sends what the user typed: IAC doubled, and a bare carriage return as CR
// NUL unless the connection is in binary mode in that direction.
func (session *Session) Write(p []byte) bool {
	if len(p) == 0 {
		return true
	}
	session.mu.Lock()
	binary := session.us[optBinary]
	session.mu.Unlock()
	encoded := make([]byte, 0, len(p)+4)
	for index, b := range p {
		switch {
		case b == iac:
			encoded = append(encoded, iac, iac)
		case b == '\r' && !binary && (index+1 == len(p) || p[index+1] != '\n'):
			encoded = append(encoded, '\r', 0)
		default:
			encoded = append(encoded, b)
		}
	}
	return session.queue(encoded, false)
}

// Resize tells the server the new window size, if it asked for it.
func (session *Session) Resize(cols, rows int) {
	session.mu.Lock()
	session.cols, session.rows = max(cols, 1), max(rows, 1)
	enabled := session.us[optNAWS]
	session.mu.Unlock()
	if enabled {
		session.sendWindowSize()
	}
}

func (session *Session) sendWindowSize() {
	session.mu.Lock()
	cols, rows := session.cols, session.rows
	session.mu.Unlock()
	message := []byte{iac, sb, optNAWS}
	for _, value := range []int{cols, rows} {
		for _, b := range []byte{byte(value >> 8), byte(value)} {
			message = append(message, b)
			if b == iac {
				message = append(message, iac)
			}
		}
	}
	session.queue(append(message, iac, se), true)
}

// Drain appends the output received since the last call to dst.
func (session *Session) Drain(dst []byte) []byte {
	session.mu.Lock()
	defer session.mu.Unlock()
	dst = append(dst, session.output...)
	session.output = session.output[:0]
	return dst
}

// Done is closed once the session has ended and its output is drainable.
func (session *Session) Done() <-chan struct{} { return session.done }

// Err says why the session ended; nil for a clean close.
func (session *Session) Err() error {
	session.mu.Lock()
	defer session.mu.Unlock()
	return session.err
}

// Close ends the session.
func (session *Session) Close() { session.finish(nil) }

// Kind reports that this is a raw terminal, not a known shell. A Telnet host may
// serve a network device CLI or a login shell; without probing the far end (which
// would type a command the user did not ask for) the only safe assumption is to
// send a command exactly as typed and wait for the prompt to return.
func (session *Session) Kind() shellkind.Kind { return shellkind.Console }

type parseState uint8

const (
	stData parseState = iota
	stIAC
	stOption // after WILL/WONT/DO/DONT, holding the command
	stSB
	stSBData
	stSBIAC
)

func (session *Session) read() {
	var (
		buffer  [4096]byte
		state   parseState
		command byte
		sbBytes []byte
	)
	for {
		n, err := session.socket.Read(buffer[:])
		session.mu.Lock()
		for _, b := range buffer[:n] {
			switch state {
			case stData:
				if b == iac {
					state = stIAC
					continue
				}
				session.data(b)
			case stIAC:
				switch b {
				case iac:
					session.data(iac)
					state = stData
				case will, wont, do, dont:
					command, state = b, stOption
				case sb:
					sbBytes, state = sbBytes[:0], stSB
				default: // NOP, GA, DM and the like need no answer
					state = stData
				}
			case stOption:
				session.option(command, b)
				state = stData
			case stSB:
				sbBytes = append(sbBytes[:0], b)
				state = stSBData
			case stSBData:
				if b == iac {
					state = stSBIAC
				} else if len(sbBytes) < maxSubnegotiation {
					sbBytes = append(sbBytes, b)
				}
			case stSBIAC:
				switch b {
				case se:
					session.subnegotiation(sbBytes)
					state = stData
				case iac:
					if len(sbBytes) < maxSubnegotiation {
						sbBytes = append(sbBytes, iac)
					}
					state = stSBData
				default:
					state = stSBData
				}
			}
		}
		overflow := len(session.output) > maxPendingOutput
		session.mu.Unlock()
		if n > 0 {
			session.notify()
		}
		if overflow {
			session.finish(errors.New("telnet: the server sent more than the terminal could take"))
			return
		}
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				err = nil
			}
			if errors.Is(err, io.EOF) {
				err = nil
			}
			session.finish(err)
			return
		}
	}
}

// data handles one byte of the data stream. Callers hold the lock.
func (session *Session) data(b byte) {
	if session.crSeen {
		session.crSeen = false
		if b == 0 && !session.him[optBinary] {
			return // CR NUL is a bare carriage return, already delivered
		}
	}
	if b == '\r' {
		session.crSeen = true
	}
	session.output = append(session.output, b)
}

func (session *Session) reply(command, option byte) {
	session.enqueueLocked([]byte{iac, command, option})
}

// option answers a WILL, WONT, DO or DONT. An option is answered only when its
// state changes, so a server that repeats itself cannot start a loop. Callers
// hold the lock.
func (session *Session) option(command, option byte) {
	switch command {
	case will:
		switch option {
		case optEcho, optSGA, optBinary:
			if !session.him[option] {
				session.him[option] = true
				session.reply(do, option)
			}
		default:
			session.reply(dont, option)
		}
	case wont:
		if session.him[option] {
			session.him[option] = false
			session.reply(dont, option)
		}
	case do:
		switch option {
		case optSGA, optBinary, optTType, optNAWS:
			if !session.us[option] {
				session.us[option] = true
				session.reply(will, option)
				if option == optNAWS {
					go session.sendWindowSize()
				}
			}
		default:
			session.reply(wont, option)
		}
	case dont:
		if session.us[option] {
			session.us[option] = false
			session.reply(wont, option)
		}
	}
}

func (session *Session) subnegotiation(data []byte) {
	if len(data) >= 2 && data[0] == optTType && data[1] == ttypeSend && session.us[optTType] {
		message := []byte{iac, sb, optTType, ttypeIs}
		message = append(message, session.config.Term...)
		message = append(message, iac, se)
		session.enqueueLocked(message)
	}
}
