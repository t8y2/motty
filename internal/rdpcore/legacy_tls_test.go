package rdp

import (
	"crypto/tls"
	"errors"
	"net"
	"net/http/httptest"
	"testing"
	"time"

	"motty/internal/rdpcore/protocol/tpkt"
	"motty/internal/rdpcore/protocol/x224"
)

// legacyTLSServer finishes the X.224 negotiation and then offers exactly one
// TLS 1.2 suite, as a Windows server whose cipher policy leaves only
// TLS_RSA_WITH_AES_128_CBC_SHA256 does. The handshake error, if any, says
// whether the client offered that suite.
func legacyTLSServer(t *testing.T, suite uint16) (string, <-chan error) {
	t.Helper()
	certificateServer := httptest.NewTLSServer(nil)
	certificate := certificateServer.TLS.Certificates[0]
	certificateServer.Close()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	done := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			done <- err
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		connLog := discardLogger
		if _, err := tpkt.NewConn(conn, connLog).Read(); err != nil {
			done <- err
			return
		}
		if err := tpkt.NewConn(conn, connLog).Write(connectionConfirm(x224.TypeRDPNegRsp, x224.ProtocolSSL)); err != nil {
			done <- err
			return
		}
		secured := tls.Server(conn, &tls.Config{
			Certificates: []tls.Certificate{certificate},
			MinVersion:   tls.VersionTLS12,
			MaxVersion:   tls.VersionTLS12,
			CipherSuites: []uint16{suite},
		})
		if err := secured.Handshake(); err != nil {
			done <- err
			return
		}
		if got := secured.ConnectionState().CipherSuite; got != suite {
			done <- errors.New("the server's only suite was not negotiated")
			return
		}
		done <- nil
	}()
	return listener.Addr().String(), done
}

// A server that leaves only an RSA key exchange suite reachable is refused by
// default and reached when the host allows legacy TLS. This is the Windows
// server of issue #1176: Go's default offer has no suite in common with it.
// Turning the switch on must not downgrade a server that still speaks ECDHE.
func TestLegacyTLSControlsTheRSAKeyExchangeOffer(t *testing.T) {
	const rsaSuite = tls.TLS_RSA_WITH_AES_128_CBC_SHA256
	const ecdheSuite = tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256
	tests := []struct {
		name        string
		serverSuite uint16
		legacy      bool
		want        uint16
		wantErr     bool
	}{
		{"default refuses an RSA-only server", rsaSuite, false, 0, true},
		{"legacy reaches an RSA-only server", rsaSuite, true, rsaSuite, false},
		{"legacy keeps ECDHE for a modern server", ecdheSuite, true, ecdheSuite, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			address, serverDone := legacyTLSServer(t, test.serverSuite)
			client := clientAt(t, address, SecurityNegotiate)
			client.opts.LegacyTLS = test.legacy
			if err := client.negotiateX224(x224.ProtocolSSL); err != nil {
				t.Fatalf("negotiation: %v", err)
			}
			err := client.upgradeTLS()
			if test.wantErr {
				if !errors.Is(err, ErrTLSFailed) {
					t.Fatalf("offer error = %v, want a TLS failure", err)
				}
			} else {
				if err != nil {
					t.Fatalf("TLS: %v", err)
				}
				if got := client.tlsConn.ConnectionState().CipherSuite; got != test.want {
					t.Fatalf("negotiated %#x, want %#x", got, test.want)
				}
			}
			if serverErr := <-serverDone; !test.wantErr && serverErr != nil {
				t.Fatalf("server handshake: %v", serverErr)
			}
		})
	}
}
