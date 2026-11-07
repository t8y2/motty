// Package serial talks to a serial port: the line settings a port is opened
// with, the ports a machine has, and a session with the shape of a remote shell
// (see sshconn.Shell), so a terminal can show one like any other connection.
// Opening and listing are small per-platform files; everything here is shared.
package serial

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"

	"motty/internal/shellkind"
)

// Parity is how a frame's parity bit is chosen.
type Parity uint8

const (
	ParityNone Parity = iota
	ParityOdd
	ParityEven
	ParityMark
	ParitySpace
)

// Flow is how the two ends hold each other back.
type Flow uint8

const (
	FlowNone Flow = iota
	// FlowHardware is RTS/CTS.
	FlowHardware
	// FlowSoftware is XON/XOFF.
	FlowSoftware
)

// Settings are the line parameters a port is opened with.
type Settings struct {
	Baud     int32
	DataBits uint8 // 5 to 8
	StopBits uint8 // 1 or 2
	Parity   Parity
	Flow     Flow
}

// Default is the line most devices still start with.
var Default = Settings{Baud: 115200, DataBits: 8, Parity: ParityNone, StopBits: 1}

// MaxBaud is the highest rate accepted; no common adapter goes beyond it.
const MaxBaud = 4_000_000

// Validate reports why the settings cannot be used, or nil.
func (settings Settings) Validate() error {
	switch {
	case settings.Baud < 50 || settings.Baud > MaxBaud:
		return fmt.Errorf("serial: baud rate %d is outside 50 to %d", settings.Baud, MaxBaud)
	case settings.DataBits < 5 || settings.DataBits > 8:
		return fmt.Errorf("serial: %d data bits is not 5 to 8", settings.DataBits)
	case settings.StopBits != 1 && settings.StopBits != 2:
		return fmt.Errorf("serial: %d stop bits is not 1 or 2", settings.StopBits)
	case settings.Parity > ParitySpace:
		return errors.New("serial: unknown parity")
	case settings.Flow > FlowSoftware:
		return errors.New("serial: unknown flow control")
	}
	return nil
}

const parityLetters = "NOEMS"

// String is the conventional short form: "115200 8N1".
func (settings Settings) String() string {
	letter := byte('?')
	if int(settings.Parity) < len(parityLetters) {
		letter = parityLetters[settings.Parity]
	}
	return fmt.Sprintf("%d %d%c%d", settings.Baud, settings.DataBits, letter, settings.StopBits)
}

// ParseLine reads "115200", "9600 7E2" or "115200,8,n,1" into the line
// parameters, keeping flow control as it was. A missing frame means 8N1.
func ParseLine(text string, flow Flow) (Settings, error) {
	fields := strings.FieldsFunc(text, func(r rune) bool { return r == ' ' || r == ',' || r == '\t' })
	settings := Settings{DataBits: 8, StopBits: 1, Flow: flow}
	if len(fields) == 0 {
		return Settings{}, errors.New("serial: no baud rate")
	}
	baud, err := strconv.Atoi(fields[0])
	if err != nil || baud < 0 || baud > MaxBaud {
		return Settings{}, fmt.Errorf("serial: %q is not a baud rate", fields[0])
	}
	settings.Baud = int32(baud)
	frame := strings.ToUpper(strings.Join(fields[1:], ""))
	if frame != "" {
		if len(frame) != 3 {
			return Settings{}, fmt.Errorf("serial: %q is not a frame like 8N1", frame)
		}
		settings.DataBits = frame[0] - '0'
		index := strings.IndexByte(parityLetters, frame[1])
		if index < 0 {
			return Settings{}, fmt.Errorf("serial: %q is not a parity (N, O, E, M or S)", frame[1:2])
		}
		settings.Parity = Parity(index)
		settings.StopBits = frame[2] - '0'
	}
	return settings, settings.Validate()
}

// Port is one serial port a machine has.
type Port struct {
	// Device is what Open takes: /dev/ttyUSB0, /dev/cu.usbserial-10, COM3.
	Device string
	// Description is a friendlier name when the system has one.
	Description string
}

// Config describes the session to start.
type Config struct {
	Device   string
	Settings Settings
	// Notify is called after output arrives and when the session ends, from any
	// goroutine; it must not block.
	Notify func()
}

const (
	// maxPendingOutput is how much output may wait for the window thread before
	// the reader pauses.
	maxPendingOutput = 1 << 20
	// maxPendingInput is how much typed or pasted input may wait to be written.
	maxPendingInput = 4 << 20
)

// Session is an open port with a reader and a writer behind it. Output is
// drained by the window thread and input is queued without blocking it.
type Session struct {
	port   io.ReadWriteCloser
	notify func()
	done   chan struct{}

	mu       sync.Mutex
	space    *sync.Cond
	data     []byte
	finished bool
	err      error
	closed   bool

	input   chan []byte
	pending int
}

// Start opens the port and starts its reader and writer.
func Start(config Config) (*Session, error) {
	if err := config.Settings.Validate(); err != nil {
		return nil, err
	}
	port, err := open(config.Device, config.Settings)
	if err != nil {
		return nil, err
	}
	return newSession(port, config.Notify), nil
}

func newSession(port io.ReadWriteCloser, notify func()) *Session {
	session := &Session{port: port, notify: notify, done: make(chan struct{}), input: make(chan []byte, 256)}
	session.space = sync.NewCond(&session.mu)
	go session.readLoop()
	go session.writeLoop()
	return session
}

func (session *Session) wake() {
	if session.notify != nil {
		session.notify()
	}
}

// readLoop moves the port's output into the buffer, pausing while it is full.
func (session *Session) readLoop() {
	buffer := make([]byte, 16<<10)
	var failure error
	for {
		n, err := session.port.Read(buffer)
		if n > 0 {
			session.mu.Lock()
			for len(session.data) >= maxPendingOutput && !session.closed {
				session.space.Wait()
			}
			first := len(session.data) == 0
			session.data = append(session.data, buffer[:n]...)
			session.mu.Unlock()
			if first {
				session.wake()
			}
		}
		if err != nil {
			failure = err
			break
		}
	}
	session.mu.Lock()
	if !session.closed && !errors.Is(failure, io.EOF) {
		session.err = failure
	}
	session.finished = true
	session.mu.Unlock()
	session.space.Broadcast()
	session.port.Close()
	close(session.done)
	session.wake()
}

func (session *Session) writeLoop() {
	for {
		select {
		case data := <-session.input:
			session.mu.Lock()
			session.pending -= len(data)
			session.mu.Unlock()
			if _, err := session.port.Write(data); err != nil {
				return
			}
		case <-session.done:
			return
		}
	}
}

// Drain appends the output received since the last call to dst. It never blocks.
func (session *Session) Drain(dst []byte) []byte {
	session.mu.Lock()
	dst = append(dst, session.data...)
	if cap(session.data) > 4*maxPendingOutput {
		session.data = nil
	} else {
		session.data = session.data[:0]
	}
	session.mu.Unlock()
	session.space.Broadcast()
	return dst
}

// Write queues input for the device. It reports false once the session has
// ended or the backlog is full; it never blocks the caller.
func (session *Session) Write(p []byte) bool {
	if len(p) == 0 {
		return true
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.closed || session.finished || session.pending+len(p) > maxPendingInput {
		return false
	}
	select {
	case session.input <- append([]byte(nil), p...):
		session.pending += len(p)
		return true
	default:
		return false
	}
}

// Resize does nothing: a serial line has no notion of the terminal's size.
func (session *Session) Resize(cols, rows int) {}

// Done is closed once the port has gone and its output is complete.
func (session *Session) Done() <-chan struct{} { return session.done }

// Err is why the session ended: nil when it was closed, an error when the device
// went away or failed.
func (session *Session) Err() error {
	session.mu.Lock()
	defer session.mu.Unlock()
	return session.err
}

// Close ends the session and releases the port.
func (session *Session) Close() {
	session.mu.Lock()
	session.closed = true
	session.mu.Unlock()
	session.space.Broadcast()
	session.port.Close()
}

// Kind reports that this is a raw terminal, not a known shell. A serial line may
// reach a device console or a login shell; the command is sent exactly as typed
// and the prompt returning marks the end.
func (session *Session) Kind() shellkind.Kind { return shellkind.Console }
