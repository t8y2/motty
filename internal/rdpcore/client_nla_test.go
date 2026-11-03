package rdp

import (
	"bytes"
	"crypto/tls"
	"encoding/asn1"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"motty/internal/rdpcore/protocol/nla"
)

type nlaTestRequest struct {
	Version    int `asn1:"explicit,tag:0"`
	NegoTokens []struct {
		Data []byte `asn1:"explicit,tag:0"`
	} `asn1:"explicit,optional,tag:1"`
	AuthInfo    []byte `asn1:"explicit,optional,tag:2"`
	PubKeyAuth  []byte `asn1:"explicit,optional,tag:3"`
	ErrorCode   int    `asn1:"explicit,optional,tag:4"`
	ClientNonce []byte `asn1:"explicit,optional,tag:5"`
}

func readNLATestRequest(conn net.Conn) (*nlaTestRequest, error) {
	var header [2]byte
	if _, err := io.ReadFull(conn, header[:]); err != nil {
		return nil, err
	}
	encoded := append([]byte(nil), header[:]...)
	size := int(header[1])
	if size&0x80 != 0 {
		count := size & 0x7f
		if count == 0 || count > 4 {
			return nil, errors.New("invalid test request length")
		}
		length := make([]byte, count)
		if _, err := io.ReadFull(conn, length); err != nil {
			return nil, err
		}
		encoded = append(encoded, length...)
		size = 0
		for _, b := range length {
			size = size<<8 | int(b)
		}
	}
	if size > 65536 {
		return nil, errors.New("test request too large")
	}
	body := make([]byte, size)
	if _, err := io.ReadFull(conn, body); err != nil {
		return nil, err
	}
	var request nlaTestRequest
	_, err := asn1.Unmarshal(append(encoded, body...), &request)
	return &request, err
}

func writeNLATestRequest(conn net.Conn, request nlaTestRequest) error {
	data, err := asn1.Marshal(request)
	if err != nil {
		return err
	}
	_, err = conn.Write(data)
	return err
}

func clientTestNTLMChallenge() []byte {
	challenge := make([]byte, 72)
	copy(challenge, "NTLMSSP\x00")
	binary.LittleEndian.PutUint32(challenge[8:12], 2)
	binary.LittleEndian.PutUint32(challenge[20:24], 0xe2888231)
	copy(challenge[24:32], []byte{1, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef})
	binary.LittleEndian.PutUint16(challenge[40:42], 16)
	binary.LittleEndian.PutUint16(challenge[42:44], 16)
	binary.LittleEndian.PutUint32(challenge[44:48], 56)
	challenge[48], challenge[55] = 10, 15
	binary.LittleEndian.PutUint16(challenge[56:58], 7)
	binary.LittleEndian.PutUint16(challenge[58:60], 8)
	binary.LittleEndian.PutUint64(challenge[60:68], 132271296000000000)
	return challenge
}

func TestCredSSPFallbackDoesNotRetryCredentials(t *testing.T) {
	tests := []struct {
		name          string
		fallback      bool
		firstError    int
		authError     int
		rejectRawCert bool
		wantAuthError bool
		wantDials     int32
		wantAttempts  int32
	}{
		{"raw protocol error", true, 0x80090308, 0x80090308, false, false, 2, 1},
		{"raw password refusal", true, 0x80090308, 0xC000006D, false, true, 2, 1},
		{"SPNEGO protocol error", false, 0, 0x80090308, false, false, 1, 1},
		{"SPNEGO password refusal", false, 0, 0xC000006D, false, true, 1, 1},
		{"explicit refusal before challenge", false, 0xC000006D, 0, false, true, 1, 0},
		{"raw fallback certificate refused", true, 0x80090308, 0, true, false, 2, 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			certificateServer := httptest.NewTLSServer(nil)
			certificate := certificateServer.TLS.Certificates[0]
			certificateServer.Close()
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			var dials, attempts, certificateChecks atomic.Int32
			serverErrors := make(chan error, 20)
			serverDone := make(chan struct{})
			go func() {
				defer close(serverDone)
				for {
					conn, err := listener.Accept()
					if err != nil {
						return
					}
					index := dials.Add(1)
					_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
					err = func() error {
						defer conn.Close()
						var header [4]byte
						if _, err := io.ReadFull(conn, header[:]); err != nil {
							return err
						}
						size := int(binary.BigEndian.Uint16(header[2:4])) - 4
						if size < 8 || size > 4096 {
							return errors.New("bad X.224 request")
						}
						request := make([]byte, size)
						if _, err := io.ReadFull(conn, request); err != nil {
							return err
						}
						if binary.LittleEndian.Uint32(request[len(request)-4:])&2 == 0 {
							return errors.New("client downgraded from NLA")
						}
						_, err := conn.Write([]byte{3, 0, 0, 19, 14, 0xd0, 0, 0, 0, 0, 0, 2, 0, 8, 0, 2, 0, 0, 0})
						if err != nil {
							return err
						}
						secured := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12, MaxVersion: tls.VersionTLS12})
						if err := secured.Handshake(); err != nil {
							if test.rejectRawCert && index == 2 {
								return nil
							}
							return err
						}
						first, err := readNLATestRequest(secured)
						if err != nil || len(first.NegoTokens) != 1 {
							return errors.New("missing first CredSSP token")
						}
						raw := test.fallback && index > 1
						if bytes.HasPrefix(first.NegoTokens[0].Data, []byte("NTLMSSP\x00")) != raw {
							return fmt.Errorf("connection %d: unexpected first token format", index)
						}
						if index == 1 && test.firstError != 0 {
							return writeNLATestRequest(secured, nlaTestRequest{Version: 6, ErrorCode: test.firstError})
						}
						token := clientTestNTLMChallenge()
						if !raw {
							body, _ := asn1.Marshal(struct {
								State asn1.Enumerated `asn1:"explicit,tag:0"`
								Token []byte          `asn1:"explicit,tag:2"`
							}{State: 1, Token: token})
							token, _ = asn1.Marshal(asn1.RawValue{Class: asn1.ClassContextSpecific, Tag: 1, IsCompound: true, Bytes: body})
						}
						challenge := nlaTestRequest{Version: 6}
						challenge.NegoTokens = append(challenge.NegoTokens, struct {
							Data []byte `asn1:"explicit,tag:0"`
						}{Data: token})
						if err := writeNLATestRequest(secured, challenge); err != nil {
							return err
						}
						auth, err := readNLATestRequest(secured)
						if err != nil || len(auth.NegoTokens) != 1 || len(auth.PubKeyAuth) == 0 {
							return errors.New("missing Authenticate message")
						}
						attempts.Add(1)
						return writeNLATestRequest(secured, nlaTestRequest{Version: 6, ErrorCode: test.authError})
					}()
					if err != nil {
						serverErrors <- err
					}
				}
			}()
			host, port, _ := net.SplitHostPort(listener.Addr().String())
			options := DefaultOptions()
			options.Host, options.Port = host, mustTestPort(t, port)
			options.Username, options.Domain, options.Password = "User", "Domain", "Password"
			options.VerifyCertificate = func(der []byte) error {
				count := certificateChecks.Add(1)
				if !bytes.Equal(der, certificate.Certificate[0]) || test.rejectRawCert && count == 2 {
					return errors.New("certificate refused")
				}
				return nil
			}
			client, err := NewClient(options)
			if err != nil {
				t.Fatal(err)
			}
			err = client.Connect()
			client.Close()
			listener.Close()
			<-serverDone
			close(serverErrors)
			for serverErr := range serverErrors {
				t.Errorf("test server: %v", serverErr)
			}
			if err == nil || errors.Is(err, ErrAuthFailed) != test.wantAuthError {
				t.Errorf("Connect error = %v, credential refusal = %t", err, test.wantAuthError)
			}
			if test.rejectRawCert && !errors.Is(err, ErrCertificateRejected) {
				t.Errorf("certificate rejection was lost: %v", err)
			}
			if test.wantAttempts != 0 && !errors.Is(err, nla.ErrCredentialsSent) {
				t.Errorf("post-Authenticate error lost its stage: %v", err)
			}
			if dials.Load() != test.wantDials || attempts.Load() != test.wantAttempts || certificateChecks.Load() != test.wantDials {
				t.Errorf("dials=%d attempts=%d certificateChecks=%d; want %d/%d/%d", dials.Load(), attempts.Load(), certificateChecks.Load(), test.wantDials, test.wantAttempts, test.wantDials)
			}
		})
	}
}

func mustTestPort(t *testing.T, text string) int {
	t.Helper()
	port, err := strconv.Atoi(text)
	if err != nil {
		t.Fatal(err)
	}
	return port
}
