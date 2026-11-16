package vnc

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/binary"
	"errors"
	"io"
	"math/big"
	"net"
	"testing"
	"time"

	"motty/internal/desktop"
)

// rsaIdentityPeer stops before encrypted authentication. It lets the session
// boundary verify that public-key confirmation precedes any client key or
// credentials, and that cancellation closes the original transport.
func rsaIdentityPeer(t *testing.T) (desktop.Dialer, []byte, <-chan error) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	size := key.Size()
	wire := make([]byte, 4+size*2)
	binary.BigEndian.PutUint32(wire, uint32(key.N.BitLen()))
	key.N.FillBytes(wire[4 : 4+size])
	big.NewInt(int64(key.E)).FillBytes(wire[4+size:])
	client, peer := net.Pipe()
	t.Cleanup(func() { client.Close(); peer.Close() })
	finished := make(chan error, 1)
	go func() {
		defer peer.Close()
		peer.SetDeadline(time.Now().Add(5 * time.Second))
		if _, err := peer.Write([]byte("RFB 005.000\n")); err != nil {
			finished <- err
			return
		}
		banner := make([]byte, 12)
		if _, err := io.ReadFull(peer, banner); err != nil {
			finished <- err
			return
		}
		if string(banner) != "RFB 003.008\n" {
			finished <- errors.New("client did not negotiate RFB 3.8")
			return
		}
		if _, err := peer.Write([]byte{1, 5}); err != nil {
			finished <- err
			return
		}
		choice := make([]byte, 1)
		if _, err := io.ReadFull(peer, choice); err != nil || choice[0] != 5 {
			finished <- errors.New("client did not select RSA-AES")
			return
		}
		if _, err := peer.Write(wire); err != nil {
			finished <- err
			return
		}
		n, err := peer.Read(choice)
		if n != 0 || err == nil {
			finished <- errors.New("client sent handshake data despite identity rejection")
			return
		}
		finished <- nil
	}()
	return func(context.Context, string, string) (net.Conn, error) { return client, nil }, der, finished
}

func TestRSAIdentityRejectionPrecedesCredentialsAndPreservesItsError(t *testing.T) {
	dial, expected, finished := rsaIdentityPeer(t)
	refused := errors.New("identity refused")
	checked := false
	session, err := Open(context.Background(), dial, "pi.example.com:5900", desktop.Options{
		Username: "alice",
		VerifyServerKey: func(address string, der []byte) error {
			checked = true
			if address != "pi.example.com:5900" || !bytes.Equal(der, expected) {
				t.Errorf("incorrect identity target: %q, canonical key match=%v", address, bytes.Equal(der, expected))
			}
			return refused
		},
		AskPassword: func() (string, error) {
			t.Error("password requested before identity acceptance")
			return "", refused
		},
		AskCredentials: func() (string, string, error) {
			t.Error("system credentials requested before identity acceptance")
			return "", "", refused
		},
	})
	if session != nil || !errors.Is(err, refused) || !checked {
		t.Fatalf("identity rejection: session=%v error=%v checked=%v", session, err, checked)
	}
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
}

func TestRSACancelDuringIdentityQuestionClosesTheConnection(t *testing.T) {
	dial, _, finished := rsaIdentityPeer(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	asked := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		_, err := Open(ctx, dial, "pi.example.com:5900", desktop.Options{
			VerifyServerKey: func(string, []byte) error {
				close(asked)
				<-ctx.Done()
				return ctx.Err()
			},
		})
		result <- err
	}()
	select {
	case <-asked:
	case err := <-result:
		t.Fatalf("connection ended before identity question: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("identity question was not raised")
	}
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled handshake: %v", err)
	}
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
}
