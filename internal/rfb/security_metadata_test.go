package rfb

import (
	"crypto/tls"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
)

func TestUnsupportedSecurityRetainsOffersAndPermissionReason(t *testing.T) {
	for _, test := range []struct {
		name     string
		version  string
		types    []uint32
		authOnly bool
	}{
		{"unknown RFB 3.8", "RFB 003.008\n", []uint32{42, 255}, false},
		{"unknown RFB 3.3", "RFB 003.003\n", []uint32{0xffffffff}, false},
		{"authentication-only without permission", "RFB 003.008\n", []uint32{2, 130, 6}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := connect(t, Config{
				AskPassword: func() (string, error) { t.Error("unexpected credential request"); return "", nil },
			}, func(s *server) {
				s.write([]byte(test.version))
				s.read(12)
				if test.version == "RFB 003.003\n" {
					s.write(u32(int(test.types[0])))
					return
				}
				offer := []byte{byte(len(test.types))}
				for _, value := range test.types {
					offer = append(offer, byte(value))
				}
				s.write(offer)
			})
			var unsupported *UnsupportedSecurityError
			if !errors.Is(err, ErrUnsupported) || !errors.As(err, &unsupported) || unsupported.VeNCrypt ||
				unsupported.AuthenticationOnly != test.authOnly || !slices.Equal(unsupported.Types, test.types) {
				t.Fatalf("incorrect unsupported security classification: %v", err)
			}
			if test.authOnly {
				if !strings.Contains(err.Error(), "RA256ne") || !strings.Contains(err.Error(), "permission") {
					t.Fatalf("authentication-only refusal omitted the offered mode or permission: %v", err)
				}
			} else if !strings.Contains(err.Error(), fmt.Sprintf("#%d", test.types[0])) {
				t.Fatalf("unknown offered security number was lost: %v", err)
			}
		})
	}
}

func TestRFB41BannerRemainsUnsupported(t *testing.T) {
	_, err := connect(t, Config{}, func(s *server) {
		s.write([]byte("RFB 004.001\n"))
	})
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("unexpected RFB 4.1 negotiation: %v", err)
	}
}

func TestVeNCryptMetadataReportsActualTLSNegotiation(t *testing.T) {
	certs := vencryptCerts(t)
	for _, version := range []uint16{tls.VersionTLS12, tls.VersionTLS13} {
		t.Run(tls.VersionName(version), func(t *testing.T) {
			negotiated := make(chan tls.ConnectionState, 1)
			client, err := connect(t, Config{
				ServerName:     "vnc.example.test",
				TLSConfig:      &tls.Config{RootCAs: certs.roots},
				AskCredentials: func() (string, string, error) { t.Error("X509None requested credentials"); return "", "", nil },
				AskPassword:    func() (string, error) { t.Error("X509None requested a password"); return "", nil },
			}, func(s *server) {
				secured, _, err := vencryptPeer(t, s.conn, vencryptPeerOptions{
					rfbTypes: []byte{19, 42, 2}, subtypes: []uint32{999, vencryptX509None},
					serverTLS: &tls.Config{
						Certificates: []tls.Certificate{certs.serverCert},
						MinVersion:   version, MaxVersion: version,
						CipherSuites: []uint16{tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384},
					},
				})
				if err != nil {
					t.Error(err)
					return
				}
				negotiated <- secured.ConnectionState()
				s.conn = secured
				finishVeNCryptPeer(s, "TLS metadata")
			})
			if err != nil {
				t.Fatal(err)
			}
			actual := <-negotiated
			if client.SecurityMode != "VeNCrypt X509None" || client.Authentication != "none" || !client.Encrypted ||
				client.SecurityTypes != "VeNCrypt, #42, VNC Authentication" || client.SecuritySubtypes != "#999, X509None" ||
				client.TLSVersion != tls.VersionName(actual.Version) || client.Cipher != tls.CipherSuiteName(actual.CipherSuite) || actual.Version != version {
				t.Fatalf("metadata does not match the actual TLS handshake: %+v, server=%+v", client, actual)
			}
		})
	}
}
