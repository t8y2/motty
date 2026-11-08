package workbench

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/http/httptest"
	"testing"
	"time"

	rdpcore "motty/internal/rdpcore"
)

func TestRDPTunnelVerifiesTheTypedServerBeforeAuthentication(t *testing.T) {
	certificateServer := httptest.NewTLSServer(nil)
	certificate := certificateServer.TLS.Certificates[0]
	certificateServer.Close()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	serverDone := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			serverDone <- err
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		var header [4]byte
		if _, err := io.ReadFull(conn, header[:]); err != nil {
			serverDone <- err
			return
		}
		size := int(binary.BigEndian.Uint16(header[2:4])) - len(header)
		if size < 8 || size > 4096 {
			serverDone <- errors.New("invalid X.224 negotiation request")
			return
		}
		if _, err := io.CopyN(io.Discard, conn, int64(size)); err != nil {
			serverDone <- err
			return
		}
		// Select NLA; its certificate check precedes credential authentication.
		if _, err := conn.Write([]byte{3, 0, 0, 19, 14, 0xd0, 0, 0, 0, 0, 0, 2, 0, 8, 0, 2, 0, 0, 0}); err != nil {
			serverDone <- err
			return
		}
		secured := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12, MaxVersion: tls.VersionTLS12})
		serverDone <- secured.Handshake()
	}()
	tunnel := &fakeConn{dial: func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, listener.Addr().String())
	}}
	var verifiedAddress string
	var verifiedCertificate []byte
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = openDesktopSession(ctx, desktopRequest{
		Host:   Host{Address: "windows.example.com", Port: 3389, User: "alice", Protocol: protocolRDP, ViaSSH: true},
		Tunnel: tunnel,
		Ask:    func() (string, error) { return "test password", nil },
		Verify: func(address string, der []byte) error {
			verifiedAddress, verifiedCertificate = address, append([]byte(nil), der...)
			return errors.New("test refuses this certificate")
		},
	})
	if !errors.Is(err, rdpcore.ErrCertificateRejected) {
		t.Fatalf("certificate rejection = %v", err)
	}
	if verifiedAddress != "windows.example.com:3389" || !bytes.Equal(verifiedCertificate, certificate.Certificate[0]) {
		t.Fatalf("verified address %q and certificate %x", verifiedAddress, verifiedCertificate)
	}
	tunnel.mu.Lock()
	defer tunnel.mu.Unlock()
	if len(tunnel.dialCalls) != 1 || tunnel.dialCalls[0].address != "127.0.0.1:3389" {
		t.Fatalf("tunnel dials = %+v", tunnel.dialCalls)
	}
	if err := <-serverDone; err == nil {
		t.Fatal("TLS proceeded after certificate rejection")
	}
}
