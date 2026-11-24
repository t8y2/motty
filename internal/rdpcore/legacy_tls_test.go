package rdp

import (
	"crypto/tls"
	"errors"
	"net"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"motty/internal/rdpcore/protocol/tpkt"
	"motty/internal/rdpcore/protocol/x224"
)

// legacyTLSServerVersion finishes the X.224 negotiation and then offers exactly
// one suite across the requested TLS versions, as a Windows server whose cipher
// policy leaves only that suite does. The handshake error, if any, says whether
// the client offered it.
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

// legacyTLSServer is legacyTLSServerVersion pinned to TLS 1.2.
func legacyTLSServer(t *testing.T, suite uint16) (string, <-chan error) {
	return legacyTLSServerVersion(t, tls.VersionTLS12, tls.VersionTLS12, suite)
}

// A server that leaves only an RSA key exchange suite reachable is refused by
// default and reached when the host allows legacy TLS. This is the Windows
// server of issue #1176: Go's default offer has no suite in common with it.
// Turning the switch on must not downgrade a server that still speaks ECDHE or
// TLS 1.3, and it reaches TLS 1.0 servers (AES-CBC and 3DES).
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
		{"legacy keeps TLS 1.3 for a modern server", tls.TLS_AES_128_GCM_SHA256, tls.VersionTLS13, tls.VersionTLS13, true, tls.TLS_AES_128_GCM_SHA256, false},
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
					t.Fatalf("negotiated cipher %#x, want %#x", got, test.want)
				}
				if test.minVer == test.maxVer {
					if gotVer := client.tlsConn.ConnectionState().Version; gotVer != test.minVer {
						t.Fatalf("negotiated version %#x, want %#x", gotVer, test.minVer)
					}
				}
			}
			if serverErr := <-serverDone; !test.wantErr && serverErr != nil {
				t.Fatalf("server handshake: %v", serverErr)
			}
		})
	}
}

func startStandardTLSServer(t *testing.T, cert tls.Certificate, configForClient func(chi *tls.ClientHelloInfo) (*tls.Config, error)) (string, *atomic.Int32, *atomic.Uint32, func()) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var connections atomic.Int32
	var negotiatedVersion atomic.Uint32
	closed := make(chan struct{})
	go func() {
		defer close(closed)
		connLog := discardLogger
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			connections.Add(1)
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
				Certificates:       []tls.Certificate{cert},
				GetConfigForClient: configForClient,
			})
			if err := secured.Handshake(); err != nil {
				conn.Close()
				continue
			}
			negotiatedVersion.Store(uint32(secured.ConnectionState().Version))
			conn.Close()
			return
		}
	}()
	cleanup := func() {
		_ = listener.Close()
		<-closed
	}
	return listener.Addr().String(), &connections, &negotiatedVersion, cleanup
}

// When LegacyTLS is disabled, a server that rejects TLS versions higher than
// 1.0 is rejected immediately with ErrTLSFailed without probing.
func TestStandardTLSRefusesTLS10WithoutProbingWhenLegacyDisabled(t *testing.T) {
	certificateServer := httptest.NewTLSServer(nil)
	certificate := certificateServer.TLS.Certificates[0]
	certificateServer.Close()

	address, connections, _, cleanup := startStandardTLSServer(t, certificate, func(chi *tls.ClientHelloInfo) (*tls.Config, error) {
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
	})
	defer cleanup()

	host, portText, _ := net.SplitHostPort(address)
	port, _ := strconv.Atoi(portText)

	options := DefaultOptions()
	options.Host, options.Port, options.Username = host, port, "alice"
	options.LegacyTLS = false
	client, err := NewClient(options)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	err = client.Connect()
	if !errors.Is(err, ErrTLSFailed) {
		t.Fatalf("Connect() error = %v, want ErrTLSFailed", err)
	}
	if got := connections.Load(); got != 1 {
		t.Fatalf("server received %d connections, want 1 (probing must not occur when LegacyTLS is disabled)", got)
	}
}

// A server that selects ProtocolSSL and drops connections on TLS versions
// higher than 1.0 (TLS intolerance) is reached when client.Connect() probes
// down to TLS 1.0 with LegacyTLS enabled.
func TestStandardTLSProbesDownToTLS10(t *testing.T) {
	certificateServer := httptest.NewTLSServer(nil)
	certificate := certificateServer.TLS.Certificates[0]
	certificateServer.Close()

	address, connections, negotiatedVersion, cleanup := startStandardTLSServer(t, certificate, func(chi *tls.ClientHelloInfo) (*tls.Config, error) {
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
	})
	defer cleanup()

	host, portText, _ := net.SplitHostPort(address)
	port, _ := strconv.Atoi(portText)

	options := DefaultOptions()
	options.Host, options.Port, options.Username = host, port, "alice"
	options.LegacyTLS = true
	client, err := NewClient(options)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	err = client.Connect()
	if errors.Is(err, ErrTLSFailed) {
		t.Fatalf("Connect() error = %v, unexpected ErrTLSFailed when LegacyTLS is enabled", err)
	}
	if !errors.Is(err, ErrMCSConnectFailed) {
		t.Fatalf("Connect() error = %v, want ErrMCSConnectFailed after successful TLS probe", err)
	}
	if got := connections.Load(); got != 4 {
		t.Fatalf("server received %d connections, want 4 (initial + 3 probes: 1.2, 1.1, 1.0)", got)
	}
	if uint16(negotiatedVersion.Load()) != tls.VersionTLS10 {
		t.Fatalf("server negotiated version = %#x, want TLS 1.0 (%#x)", negotiatedVersion.Load(), tls.VersionTLS10)
	}
	if client.tlsConn == nil || client.tlsConn.ConnectionState().Version != tls.VersionTLS10 {
		t.Fatalf("client negotiated version = %#x, want TLS 1.0 (%#x)", client.tlsConn.ConnectionState().Version, tls.VersionTLS10)
	}
}

// When LegacyTLS is enabled, connecting to a modern TLS 1.3 server must
// still negotiate TLS 1.3 and must not be capped at TLS 1.2.
func TestStandardTLSAllowsTLS13WhenLegacyEnabled(t *testing.T) {
	certificateServer := httptest.NewTLSServer(nil)
	certificate := certificateServer.TLS.Certificates[0]
	certificateServer.Close()

	address, connections, negotiatedVersion, cleanup := startStandardTLSServer(t, certificate, func(chi *tls.ClientHelloInfo) (*tls.Config, error) {
		return &tls.Config{
			Certificates: []tls.Certificate{certificate},
			MinVersion:   tls.VersionTLS13,
			MaxVersion:   tls.VersionTLS13,
		}, nil
	})
	defer cleanup()

	host, portText, _ := net.SplitHostPort(address)
	port, _ := strconv.Atoi(portText)

	options := DefaultOptions()
	options.Host, options.Port, options.Username = host, port, "alice"
	options.LegacyTLS = true
	client, err := NewClient(options)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	err = client.Connect()
	if errors.Is(err, ErrTLSFailed) {
		t.Fatalf("Connect() error = %v, unexpected ErrTLSFailed for TLS 1.3 server with LegacyTLS enabled", err)
	}
	if !errors.Is(err, ErrMCSConnectFailed) {
		t.Fatalf("Connect() error = %v, want ErrMCSConnectFailed after successful TLS 1.3 handshake", err)
	}
	if got := connections.Load(); got != 1 {
		t.Fatalf("server received %d connections, want 1 (no probing for TLS 1.3 server)", got)
	}
	if uint16(negotiatedVersion.Load()) != tls.VersionTLS13 {
		t.Fatalf("server negotiated version = %#x, want TLS 1.3 (%#x)", negotiatedVersion.Load(), tls.VersionTLS13)
	}
	if client.tlsConn == nil || client.tlsConn.ConnectionState().Version != tls.VersionTLS13 {
		t.Fatalf("client negotiated version = %#x, want TLS 1.3 (%#x)", client.tlsConn.ConnectionState().Version, tls.VersionTLS13)
	}
}
