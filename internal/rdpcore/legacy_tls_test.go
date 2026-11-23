package rdp

import (
	"crypto/tls"
	"errors"
	"net"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"motty/internal/rdpcore/protocol/tpkt"
	"motty/internal/rdpcore/protocol/x224"
)

// legacyTLSServer finishes the X.224 negotiation and then offers exactly one
// suite across the requested TLS versions. The handshake error, if any, says
// whether the client offered that suite.
func legacyTLSServerVersion(t *testing.T, minVer, maxVer uint16, suite uint16) (string, <-chan error) {
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
			MinVersion:   minVer,
			MaxVersion:   maxVer,
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

func legacyTLSServer(t *testing.T, suite uint16) (string, <-chan error) {
	return legacyTLSServerVersion(t, tls.VersionTLS12, tls.VersionTLS12, suite)
}

// A server that leaves only an RSA key exchange suite reachable is refused by
// default and reached when the host allows legacy TLS. This is the Windows
// server of issue #1176: Go's default offer has no suite in common with it.
// Turning the switch on must not downgrade a server that still speaks ECDHE,
// and reaches TLS 1.0 servers (including AES-CBC and 3DES).
func TestLegacyTLSControlsTheRSAKeyExchangeOffer(t *testing.T) {
	const rsaSuite = tls.TLS_RSA_WITH_AES_128_CBC_SHA256
	const ecdheSuite = tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256
	const tls10RsaSuite = tls.TLS_RSA_WITH_AES_128_CBC_SHA
	const tls103DesSuite = tls.TLS_RSA_WITH_3DES_EDE_CBC_SHA
	tests := []struct {
		name        string
		serverSuite uint16
		minVer      uint16
		maxVer      uint16
		legacy      bool
		want        uint16
		wantErr     bool
	}{
		{"default refuses an RSA-only server", rsaSuite, tls.VersionTLS12, tls.VersionTLS12, false, 0, true},
		{"legacy reaches an RSA-only server", rsaSuite, tls.VersionTLS12, tls.VersionTLS12, true, rsaSuite, false},
		{"legacy keeps ECDHE for a modern server", ecdheSuite, tls.VersionTLS12, tls.VersionTLS12, true, ecdheSuite, false},
		{"default allows TLS 1.3 for a modern server", tls.TLS_AES_128_GCM_SHA256, tls.VersionTLS13, tls.VersionTLS13, false, tls.TLS_AES_128_GCM_SHA256, false},
		{"default refuses a TLS 1.0 RSA server", tls10RsaSuite, tls.VersionTLS10, tls.VersionTLS10, false, 0, true},
		{"legacy reaches a TLS 1.0 RSA server", tls10RsaSuite, tls.VersionTLS10, tls.VersionTLS10, true, tls10RsaSuite, false},
		{"default refuses a TLS 1.0 3DES server", tls103DesSuite, tls.VersionTLS10, tls.VersionTLS10, false, 0, true},
		{"legacy reaches a TLS 1.0 3DES server", tls103DesSuite, tls.VersionTLS10, tls.VersionTLS10, true, tls103DesSuite, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			address, serverDone := legacyTLSServerVersion(t, test.minVer, test.maxVer, test.serverSuite)
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

// A server that selects ProtocolSSL and drops connections on TLS versions
// higher than 1.0 (TLS intolerance) is reached when client.Connect() probes
// down to TLS 1.0.
func TestStandardTLSProbesDownToTLS10(t *testing.T) {
	certificateServer := httptest.NewTLSServer(nil)
	certificate := certificateServer.TLS.Certificates[0]
	certificateServer.Close()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })

	host, portText, _ := net.SplitHostPort(listener.Addr().String())
	port, _ := strconv.Atoi(portText)

	var negotiatedVersion uint16
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		connLog := discardLogger
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
			tpktConn := tpkt.NewConn(conn, connLog)
			if _, err := tpktConn.Read(); err != nil {
				conn.Close()
				continue
			}
			if err := tpktConn.Write(connectionConfirm(x224.TypeRDPNegRsp, x224.ProtocolSSL)); err != nil {
				conn.Close()
				continue
			}
			secured := tls.Server(conn, &tls.Config{
				Certificates: []tls.Certificate{certificate},
				GetConfigForClient: func(chi *tls.ClientHelloInfo) (*tls.Config, error) {
					for _, v := range chi.SupportedVersions {
						if v > tls.VersionTLS10 {
							return nil, errors.New("tls intolerant: client offered TLS > 1.0")
						}
					}
					return &tls.Config{
						Certificates: []tls.Certificate{certificate},
						MinVersion:   tls.VersionTLS10,
						MaxVersion:   tls.VersionTLS10,
						CipherSuites: []uint16{tls.TLS_RSA_WITH_AES_128_CBC_SHA},
					}, nil
				},
			})
			if err := secured.Handshake(); err != nil {
				conn.Close()
				continue
			}
			negotiatedVersion = secured.ConnectionState().Version
			// After successful TLS 1.0 handshake, close so Connect() finishes.
			conn.Close()
			return
		}
	}()

	options := DefaultOptions()
	options.Host, options.Port, options.Username = host, port, "alice"
	options.LegacyTLS = true
	client, err := NewClient(options)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	err = client.Connect()
	<-serverDone
	if negotiatedVersion != tls.VersionTLS10 {
		t.Fatalf("server negotiated version = %#x, want TLS 1.0 (%#x)", negotiatedVersion, tls.VersionTLS10)
	}
	if client.tlsConn != nil && client.tlsConn.ConnectionState().Version != tls.VersionTLS10 {
		t.Fatalf("client negotiated version = %#x, want TLS 1.0 (%#x)", client.tlsConn.ConnectionState().Version, tls.VersionTLS10)
	}
}
