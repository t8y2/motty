package rdp

import (
	"encoding/binary"
	"net"
	"testing"
	"time"

	"motty/internal/rdpcore/protocol/pdu"
	"motty/internal/rdpcore/protocol/tpkt"
)

// TestSendKeyboardMarksARepeatAsAKeyAlreadyDown pins the wire flag that makes
// the server repeat a held key. A server does not repeat one by itself, so a
// repeat without KBD_FLAGS_DOWN is dropped there and the key types once.
func TestSendKeyboardMarksARepeatAsAKeyAlreadyDown(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	clientConn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer clientConn.Close()
	serverConn, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer serverConn.Close()
	deadline := time.Now().Add(3 * time.Second)
	clientConn.SetDeadline(deadline)
	serverConn.SetDeadline(deadline)

	client := &Client{
		userChannelID: 1002, ioChannelID: 1003, state: StateConnected,
		tpktConn: tpkt.NewConn(clientConn, discardLogger),
	}
	reader := tpkt.NewConn(serverConn, discardLogger)

	check := func(scancode uint16, pressed, repeat bool, wantFlags, wantCode uint16) {
		t.Helper()
		if err := client.SendKeyboard(scancode, pressed, repeat); err != nil {
			t.Fatalf("send %#x: %v", scancode, err)
		}
		packet, err := reader.Read()
		if err != nil {
			t.Fatalf("read %#x: %v", scancode, err)
		}
		if len(packet) < pdu.ScancodeEventSize {
			t.Fatalf("packet for %#x is %d bytes, want at least an input event", scancode, len(packet))
		}
		event := packet[len(packet)-pdu.ScancodeEventSize:]
		if got := binary.LittleEndian.Uint16(event[4:6]); got != pdu.InputScancode {
			t.Errorf("%#x: event type = %#x, want a scancode", scancode, got)
		}
		if got := binary.LittleEndian.Uint16(event[6:8]); got != wantFlags {
			t.Errorf("%#x: flags = %#x, want %#x", scancode, got, wantFlags)
		}
		if got := binary.LittleEndian.Uint16(event[8:10]); got != wantCode {
			t.Errorf("%#x: key code = %#x, want %#x", scancode, got, wantCode)
		}
	}

	check(0x1E, true, false, 0, 0x1E)
	check(0x1E, true, true, pdu.KbdFlagsDown, 0x1E)
	check(0x1E, false, false, pdu.KbdFlagsRelease, 0x1E)
	// An extended key keeps its flag and sheds the 0xE0 prefix in the code.
	check(0xE047, true, true, pdu.KbdFlagsDown|pdu.KbdFlagsExtended, 0x47)
}
