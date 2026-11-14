package rdp

import (
	"encoding/binary"
	"errors"
	"net"
	"strconv"
	"testing"
	"time"

	"motty/internal/rdpcore/protocol/tpkt"
	"motty/internal/rdpcore/protocol/x224"
)

// negotiationServer answers X.224 connection requests with reply and records
// the protocols each one asked for. reply gets the request to decide.
func negotiationServer(t *testing.T, reply func(requested uint32) []byte) (string, <-chan uint32) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	requests := make(chan uint32, 8)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(3 * time.Second))
				packet, err := tpkt.NewConn(conn, discardLogger).Read()
				if err != nil {
					return
				}
				if len(packet) < 8 || packet[len(packet)-8] != x224.TypeRDPNegReq {
					t.Errorf("request is not an RDP negotiation request: %x", packet)
					return
				}
				requested := binary.LittleEndian.Uint32(packet[len(packet)-4:])
				requests <- requested
				_ = tpkt.NewConn(conn, discardLogger).Write(reply(requested))
			}(conn)
		}
	}()
	return listener.Addr().String(), requests
}

// connectionConfirm builds the X.224 Connection Confirm a server sends for the
// negotiation result.
func connectionConfirm(negType byte, value uint32) []byte {
	payload := make([]byte, 15)
	payload[0] = 14 // length indicator, not counting itself
	payload[1] = x224.TypeConnectionConfirm
	payload[7] = negType
	binary.LittleEndian.PutUint16(payload[9:], 8)
	binary.LittleEndian.PutUint32(payload[11:], value)
	return payload
}

// clientAt makes a client whose first connection is already open, as
// negotiateX224WithFallback expects after the TCP connection step.
func clientAt(t *testing.T, address string, security SecurityMode) *Client {
	t.Helper()
	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	opts := DefaultOptions()
	opts.Host, opts.Port, opts.Username = host, port, "alice"
	opts.Security = security
	client, err := NewClient(opts)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := net.Dial("tcp", address)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	client.conn = conn
	client.tpktConn = tpkt.NewConn(conn, client.logTpkt)
	return client
}

// A server that offers only standard RDP security is asked for it only when the
// caller chose it; the default asks for TLS and refuses to fall back to the
// unauthenticated layer.
func TestStandardSecurityIsRequestedOnlyWhenChosen(t *testing.T) {
	address, requests := negotiationServer(t, func(uint32) []byte {
		return connectionConfirm(x224.TypeRDPNegFail, x224.FailSSLRequiredByServer)
	})
	client := clientAt(t, address, SecurityStandard)
	if err := client.negotiateX224WithFallback(); !errors.Is(err, ErrNegotiationFailed) {
		t.Fatalf("negotiation error = %v, want a negotiation failure", err)
	}
	select {
	case requested := <-requests:
		if requested != x224.ProtocolRDP {
			t.Fatalf("standard security asked for %#x, want standard RDP security alone", requested)
		}
	default:
		t.Fatal("no connection request was sent")
	}
}

// The default never offers standard RDP security, even after TLS is refused.
func TestNegotiateNeverFallsBackToStandardSecurity(t *testing.T) {
	address, requests := negotiationServer(t, func(uint32) []byte {
		return connectionConfirm(x224.TypeRDPNegFail, x224.FailSSLRequiredByServer)
	})
	client := clientAt(t, address, SecurityNegotiate)
	if err := client.negotiateX224WithFallback(); !errors.Is(err, ErrNegotiationFailed) {
		t.Fatalf("negotiation error = %v, want a negotiation failure", err)
	}
	want := []uint32{x224.ProtocolSSL | x224.ProtocolHybrid, x224.ProtocolSSL}
	for _, protocol := range want {
		select {
		case requested := <-requests:
			if requested != protocol {
				t.Fatalf("asked for %#x, want %#x", requested, protocol)
			}
		case <-time.After(time.Second):
			t.Fatalf("only %d requests were made", len(want))
		}
	}
	select {
	case requested := <-requests:
		t.Fatalf("a further request asked for %#x", requested)
	case <-time.After(100 * time.Millisecond):
	}
}

// Connect refuses standard RDP security the caller did not ask for, and a
// standard-only connection the server did not select.
func TestConnectHoldsTheSecurityChoice(t *testing.T) {
	t.Run("server offers standard security", func(t *testing.T) {
		address, _ := negotiationServer(t, func(uint32) []byte {
			return connectionConfirm(x224.TypeRDPNegRsp, x224.ProtocolRDP)
		})
		client := clientAt(t, address, SecurityNegotiate)
		// The transport is already open, but Connect dials its own; point the
		// options at the same server.
		if err := client.Connect(); !errors.Is(err, ErrNegotiationFailed) {
			t.Fatalf("connect error = %v, want a negotiation failure", err)
		}
	})
	t.Run("server selects TLS", func(t *testing.T) {
		address, _ := negotiationServer(t, func(uint32) []byte {
			return connectionConfirm(x224.TypeRDPNegRsp, x224.ProtocolSSL)
		})
		client := clientAt(t, address, SecurityStandard)
		if err := client.Connect(); !errors.Is(err, ErrNegotiationFailed) {
			t.Fatalf("connect error = %v, want a negotiation failure", err)
		}
	})
}
