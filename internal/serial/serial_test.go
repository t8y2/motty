package serial

import (
	"bytes"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"motty/internal/shellkind"
)

func TestASerialSessionIsADeviceConsole(t *testing.T) {
	if got := (&Session{}).Kind(); got != shellkind.Console {
		t.Fatalf("Kind() = %v; want a device console", got)
	}
}

func TestParseLine(t *testing.T) {
	for _, test := range []struct {
		text string
		want Settings
		bad  bool
	}{
		{"115200", Settings{Baud: 115200, DataBits: 8, StopBits: 1}, false},
		{"9600 7E2", Settings{Baud: 9600, DataBits: 7, Parity: ParityEven, StopBits: 2}, false},
		{"57600,8,n,1", Settings{Baud: 57600, DataBits: 8, StopBits: 1}, false},
		{" 19200  8o1 ", Settings{Baud: 19200, DataBits: 8, Parity: ParityOdd, StopBits: 1}, false},
		{"", Settings{}, true},
		{"fast", Settings{}, true},
		{"9600 9N1", Settings{}, true},
		{"9600 8X1", Settings{}, true},
		{"9600 8N3", Settings{}, true},
		{"9600 8N", Settings{}, true},
		{"10 8N1", Settings{}, true},
		{"99999999 8N1", Settings{}, true},
	} {
		got, err := ParseLine(test.text, FlowNone)
		if (err != nil) != test.bad || (err == nil && got != test.want) {
			t.Errorf("ParseLine(%q) = %+v, %v; want %+v, bad=%v", test.text, got, err, test.want, test.bad)
		}
	}
	if got := (Settings{Baud: 9600, DataBits: 7, Parity: ParityEven, StopBits: 2}).String(); got != "9600 7E2" {
		t.Errorf("String = %q", got)
	}
	flow, _ := ParseLine("9600", FlowHardware)
	if flow.Flow != FlowHardware {
		t.Error("flow control was dropped")
	}
}

// fakePort is a port the test plays the device on.
type fakePort struct {
	mu       sync.Mutex
	toHost   chan []byte
	fromHost bytes.Buffer
	closed   chan struct{}
}

func newFakePort() *fakePort {
	return &fakePort{toHost: make(chan []byte, 8), closed: make(chan struct{})}
}

func (p *fakePort) Read(buffer []byte) (int, error) {
	select {
	case data, ok := <-p.toHost:
		if !ok {
			return 0, io.ErrUnexpectedEOF
		}
		return copy(buffer, data), nil
	case <-p.closed:
		return 0, io.EOF
	}
}
func (p *fakePort) Write(data []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.fromHost.Write(data)
}
func (p *fakePort) received() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.fromHost.Len()
}
func (p *fakePort) Close() error {
	select {
	case <-p.closed:
	default:
		close(p.closed)
	}
	return nil
}

func TestASessionMovesBytesBothWaysAndEndsCleanlyWhenClosed(t *testing.T) {
	port := newFakePort()
	session := newSession(port, func() {})
	port.toHost <- []byte("boot ok\r\n")
	deadline := time.Now().Add(2 * time.Second)
	var got []byte
	for time.Now().Before(deadline) && len(got) < 9 {
		got = session.Drain(got)
		time.Sleep(time.Millisecond)
	}
	if string(got) != "boot ok\r\n" {
		t.Fatalf("output %q", got)
	}
	if !session.Write([]byte("help\r")) {
		t.Fatal("input refused")
	}
	for time.Now().Before(deadline) && port.received() < 5 {
		time.Sleep(time.Millisecond)
	}
	session.Close()
	select {
	case <-session.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("not done after Close")
	}
	if session.Err() != nil {
		t.Fatalf("closing is an error: %v", session.Err())
	}
	if session.Write([]byte("x")) {
		t.Fatal("a closed session took input")
	}
}

func TestAnUnpluggedDeviceEndsTheSessionWithAnError(t *testing.T) {
	port := newFakePort()
	session := newSession(port, nil)
	close(port.toHost)
	select {
	case <-session.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("not done when the device went away")
	}
	if err := session.Err(); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("Err = %v", err)
	}
}

func TestStartRefusesSettingsThatCannotWork(t *testing.T) {
	if _, err := Start(Config{Device: "x", Settings: Settings{Baud: 1}}); err == nil {
		t.Fatal("a 1 baud line was accepted")
	}
}
