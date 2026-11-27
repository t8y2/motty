package rdp

import (
	"bytes"
	"encoding/binary"
	"net"
	"testing"
	"time"

	"motty/internal/rdpcore/protocol/tpkt"
	"motty/internal/rdpcore/protocol/x224"
)

const (
	blockMonitor    = 0xC005 // TS_UD_CS_MONITOR
	blockMsgChannel = 0xC006 // TS_UD_CS_MCS_MSGCHANNEL
)

// clientDataBlockTypes lists the GCC client data blocks of an MCS Connect
// Initial, in order.
func clientDataBlockTypes(t *testing.T, connectInitial []byte) []uint16 {
	t.Helper()
	at := bytes.Index(connectInitial, []byte("Duca"))
	if at < 0 {
		t.Fatalf("no client data in the Connect Initial: %x", connectInitial)
	}
	data := connectInitial[at+4:]
	if data[0]&0x80 != 0 { // two-byte PER length
		data = data[2:]
	} else {
		data = data[1:]
	}
	var types []uint16
	for len(data) >= 4 {
		size := int(binary.LittleEndian.Uint16(data[2:]))
		if size < 4 || size > len(data) {
			t.Fatalf("client data block of %d bytes in %d: %x", size, len(data), data)
		}
		types = append(types, binary.LittleEndian.Uint16(data))
		data = data[size:]
	}
	return types
}

// connectInitialSentTo runs the client's X.224 negotiation and MCS Connect
// Initial against a server whose Connection Confirm carries confirm, and
// returns the Connect Initial it received. With monitors, the client lays out
// a multi-monitor desktop.
func connectInitialSentTo(t *testing.T, confirm []byte, monitors bool) []byte {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	received := make(chan []byte, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
		server := tpkt.NewConn(conn, discardLogger)
		if _, err := server.Read(); err != nil {
			return
		}
		if err := server.Write(confirm); err != nil {
			return
		}
		if packet, err := server.Read(); err == nil {
			received <- packet
		}
	}()
	client := clientAt(t, listener.Addr().String(), SecurityStandard)
	if monitors {
		client.opts.Monitors = []MonitorConfig{
			{X: 0, Y: 0, Width: 800, Height: 600, Primary: true},
			{X: 800, Y: 0, Width: 800, Height: 600},
		}
	}
	if err := client.negotiateX224WithFallback(); err != nil {
		t.Fatal(err)
	}
	_ = client.mcsConnect() // the server closes instead of answering
	select {
	case packet := <-received:
		return packet
	case <-time.After(3 * time.Second):
		t.Fatal("the server received no MCS Connect Initial")
		return nil
	}
}

func contains(types []uint16, want uint16) bool {
	for _, got := range types {
		if got == want {
			return true
		}
	}
	return false
}

// Windows Server 2003 answers standard RDP security with RDP_NEG_RSP flags 0, or
// with no RDP_NEG_RSP at all, and resets a connection whose Connect Initial
// carries the extended client data blocks. Those go only to a server that set
// EXTENDED_CLIENT_DATA_SUPPORTED (MS-RDPBCGR 2.2.1.2.1).
func TestExtendedClientDataGoesOnlyToServersThatAcceptIt(t *testing.T) {
	negRsp := func(flags byte) []byte {
		confirm := connectionConfirm(x224.TypeRDPNegRsp, x224.ProtocolRDP)
		confirm[8] = flags
		return confirm
	}
	cases := []struct {
		name    string
		confirm []byte
		want    bool
	}{
		{"flag set", negRsp(x224.ExtendedClientDataSupported), true},
		{"flag clear", negRsp(0), false},
		{"other flags only", negRsp(0x02 | 0x08), false},
		{"no negotiation response", []byte{6, x224.TypeConnectionConfirm, 0, 0, 0, 0, 0}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			types := clientDataBlockTypes(t, connectInitialSentTo(t, tc.confirm, true))
			if got := contains(types, blockMsgChannel); got != tc.want {
				t.Errorf("message channel block sent = %v, want %v (blocks %#x)", got, tc.want, types)
			}
			if got := contains(types, blockMonitor); got != tc.want {
				t.Errorf("monitor block sent = %v, want %v (blocks %#x)", got, tc.want, types)
			}
			// The blocks every server reads stay in either way.
			for _, required := range []uint16{0xC001, 0xC002, 0xC003, 0xC004} {
				if !contains(types, required) {
					t.Errorf("block %#x missing (blocks %#x)", required, types)
				}
			}
		})
	}
}
