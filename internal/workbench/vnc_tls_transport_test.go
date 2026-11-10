package workbench

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"testing"
	"time"

	"motty/internal/rfb"
)

func vncTLSIdentityCertificate(t *testing.T) tls.Certificate {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), DNSNames: []string{"pi.example.com"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage:    x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

func vncTLSIdentityPeer(t *testing.T) (net.Conn, []byte, <-chan error) {
	t.Helper()
	certificate := vncTLSIdentityCertificate(t)
	der := certificate.Certificate[0]
	client, peer := net.Pipe()
	t.Cleanup(func() { client.Close(); peer.Close() })
	finished := make(chan error, 1)
	go func() {
		defer peer.Close()
		peer.SetDeadline(time.Now().Add(5 * time.Second))
		write := func(data []byte) bool {
			if _, err := peer.Write(data); err != nil {
				finished <- err
				return false
			}
			return true
		}
		read := func(size int, expected []byte) bool {
			data := make([]byte, size)
			if _, err := io.ReadFull(peer, data); err != nil {
				finished <- err
				return false
			}
			if expected != nil && !bytes.Equal(data, expected) {
				finished <- errors.New("unexpected VNC negotiation")
				return false
			}
			return true
		}
		if !write([]byte("RFB 003.008\n")) || !read(12, []byte("RFB 003.008\n")) ||
			!write([]byte{1, 19}) || !read(1, []byte{19}) || !write([]byte{0, 2}) || !read(2, []byte{0, 2}) {
			return
		}
		if !write([]byte{0, 1, 0, 0, 1, 6}) || !read(4, binary.BigEndian.AppendUint32(nil, 262)) || !write([]byte{1}) {
			return
		}
		secured := tls.Server(peer, &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{certificate}})
		if err := secured.Handshake(); err != nil {
			finished <- nil
			return
		}
		var credential [1]byte
		if n, _ := secured.Read(credential[:]); n != 0 {
			finished <- errors.New("credentials sent after rejected/canceled certificate")
			return
		}
		finished <- nil
	}()
	return client, der, finished
}

func TestVNCTLSMixedAuthenticationUsesVNCPasswordThroughSSH(t *testing.T) {
	certificate := vncTLSIdentityCertificate(t)
	client, peer := net.Pipe()
	t.Cleanup(func() { client.Close(); peer.Close() })
	finished := make(chan error, 1)
	go func() {
		finished <- func() error {
			defer peer.Close()
			peer.SetDeadline(time.Now().Add(5 * time.Second))
			read := func(size int, expected []byte) error {
				data := make([]byte, size)
				if _, err := io.ReadFull(peer, data); err != nil {
					return err
				}
				if !bytes.Equal(data, expected) {
					return fmt.Errorf("VNC negotiation=%x want %x", data, expected)
				}
				return nil
			}
			for _, exchange := range []struct{ send, receive []byte }{
				{[]byte("RFB 003.008\n"), []byte("RFB 003.008\n")},
				{[]byte{1, 19}, []byte{19}},
				{[]byte{0, 2}, []byte{0, 2}},
				{[]byte{0, 2, 0, 0, 1, 6, 0, 0, 1, 5}, binary.BigEndian.AppendUint32(nil, 261)},
			} {
				if _, err := peer.Write(exchange.send); err != nil {
					return err
				}
				if err := read(len(exchange.receive), exchange.receive); err != nil {
					return err
				}
			}
			if _, err := peer.Write([]byte{1}); err != nil {
				return err
			}
			secured := tls.Server(peer, &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{certificate}})
			if err := secured.Handshake(); err != nil {
				return err
			}
			challenge := [16]byte{1, 2, 3, 4, 5, 6, 7, 8}
			if _, err := secured.Write(challenge[:]); err != nil {
				return err
			}
			response := make([]byte, 16)
			if _, err := io.ReadFull(secured, response); err != nil {
				return err
			}
			if !rfbResponseMatches(challenge, "saved VNC password", response) {
				return errors.New("wrong VNC password")
			}
			// Authentication refusal finishes this transport test without a desktop.
			_, err := secured.Write([]byte{0, 0, 0, 1, 0, 0, 0, 0})
			return err
		}()
	}()
	tunnel := &fakeConn{dial: func(context.Context, string, string) (net.Conn, error) { return client, nil }}
	verified, asked := false, 0
	_, err := openDesktopSession(context.Background(), desktopRequest{
		Host: Host{Address: "pi.example.com", Port: 5900, User: "ssh-user", Protocol: protocolVNC, ViaSSH: true}, Tunnel: tunnel,
		Verify: func(address string, der []byte) error {
			if address != "pi.example.com:5900" || !bytes.Equal(der, certificate.Certificate[0]) {
				return errors.New("wrong server identity")
			}
			verified = true
			return nil
		},
		Ask: func() (string, error) {
			if !verified {
				return "", errors.New("password asked before server verification")
			}
			asked++
			return "saved VNC password", nil
		},
		AskCredentials: func() (string, string, error) {
			t.Error("account callback displaced VNC password authentication")
			return "", "", errors.New("unexpected account callback")
		},
	})
	if !errors.Is(err, rfb.ErrAuthFailed) || asked != 1 || !verified {
		t.Fatalf("verified=%t asked=%d error=%v", verified, asked, err)
	}
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
}

func TestVNCTLSCertificateUsesTypedAddressThroughSSHAndCancellation(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		t.Run(map[bool]string{false: "reject", true: "cancel"}[canceled], func(t *testing.T) {
			client, certificate, finished := vncTLSIdentityPeer(t)
			tunnel := &fakeConn{dial: func(context.Context, string, string) (net.Conn, error) { return client, nil }}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			refused := errors.New("VNC certificate refused")
			checked := false
			_, err := openDesktopSession(ctx, desktopRequest{
				Host: Host{Address: "pi.example.com", Port: 5900, User: "ssh-user", Protocol: protocolVNC, ViaSSH: true}, Tunnel: tunnel,
				Verify: func(address string, der []byte) error {
					checked = true
					if address != "pi.example.com:5900" || !bytes.Equal(der, certificate) {
						t.Error("certificate identity used tunnel loopback")
					}
					if canceled {
						cancel()
						<-ctx.Done()
						return ctx.Err()
					}
					return refused
				},
				Ask: func() (string, error) {
					t.Error("password requested before certificate acceptance")
					return "", refused
				},
				AskCredentials: func() (string, string, error) {
					t.Error("account requested before certificate acceptance")
					return "", "", refused
				},
			})
			want := error(refused)
			if canceled {
				want = context.Canceled
			}
			if !checked || !errors.Is(err, want) {
				t.Fatalf("checked=%t error=%v", checked, err)
			}
			if len(tunnel.dialCalls) != 1 || tunnel.dialCalls[0].address != "127.0.0.1:5900" {
				t.Fatalf("dial calls: %+v", tunnel.dialCalls)
			}
			if err := <-finished; err != nil {
				t.Fatal(err)
			}
		})
	}
}
