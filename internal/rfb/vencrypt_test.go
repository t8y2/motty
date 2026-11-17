package rfb

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

type vencryptCertBundle struct {
	caCert     *x509.Certificate
	caKey      *ecdsa.PrivateKey
	roots      *x509.CertPool
	serverCert tls.Certificate
	serverDER  []byte
	expired    tls.Certificate
}

var vencryptTestCerts = sync.OnceValues(func() (*vencryptCertBundle, error) {
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "VeNCrypt Test Root CA"},
		NotBefore:             now.Add(-24 * time.Hour),
		NotAfter:              now.Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		return nil, err
	}
	caCert, err := x509.ParseCertificate(caDER)
	if err != nil {
		return nil, err
	}
	roots := x509.NewCertPool()
	roots.AddCert(caCert)

	serverKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	serverTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "vnc.example.test"},
		DNSNames:     []string{"vnc.example.test", "localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	serverDER, err := x509.CreateCertificate(rand.Reader, serverTemplate, caCert, &serverKey.PublicKey, caKey)
	if err != nil {
		return nil, err
	}
	serverLeaf, err := x509.ParseCertificate(serverDER)
	if err != nil {
		return nil, err
	}

	expiredTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(3),
		Subject:      pkix.Name{CommonName: "vnc.example.test"},
		DNSNames:     []string{"vnc.example.test"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore:    now.Add(-4 * time.Hour),
		NotAfter:     now.Add(-2 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	expiredDER, err := x509.CreateCertificate(rand.Reader, expiredTemplate, caCert, &serverKey.PublicKey, caKey)
	if err != nil {
		return nil, err
	}

	return &vencryptCertBundle{
		caCert: caCert,
		caKey:  caKey,
		roots:  roots,
		serverCert: tls.Certificate{
			Certificate: [][]byte{serverDER},
			PrivateKey:  serverKey,
			Leaf:        serverLeaf,
		},
		serverDER: serverDER,
		expired: tls.Certificate{
			Certificate: [][]byte{expiredDER},
			PrivateKey:  serverKey,
		},
	}, nil
})

func vencryptCerts(t *testing.T) *vencryptCertBundle {
	t.Helper()
	bundle, err := vencryptTestCerts()
	if err != nil {
		t.Fatal(err)
	}
	return bundle
}

type vencryptPeerOptions struct {
	banner        string
	clientVersion string
	rfbTypes      []byte
	subtypes      []uint32
	serverTLS     *tls.Config
	serverVersion [2]byte
}

func vencryptPeer(t *testing.T, raw net.Conn, options vencryptPeerOptions) (*tls.Conn, uint32, error) {
	t.Helper()
	_ = raw.SetDeadline(time.Now().Add(5 * time.Second))
	banner, clientVersion := options.banner, options.clientVersion
	if banner == "" {
		banner = "RFB 003.008\n"
	}
	if clientVersion == "" {
		clientVersion = "RFB 003.008\n"
	}
	rfbTypes := options.rfbTypes
	if len(rfbTypes) == 0 {
		rfbTypes = []byte{19, 2, 1}
	}
	subtypes := options.subtypes
	if len(subtypes) == 0 {
		subtypes = []uint32{vencryptX509Plain, vencryptX509Vnc, vencryptX509None}
	}
	if _, err := raw.Write([]byte(banner)); err != nil {
		return nil, 0, err
	}
	var version [12]byte
	if _, err := io.ReadFull(raw, version[:]); err != nil {
		return nil, 0, err
	}
	if string(version[:]) != clientVersion {
		return nil, 0, fmt.Errorf("client version %q want %q", version, clientVersion)
	}
	offer := append([]byte{byte(len(rfbTypes))}, rfbTypes...)
	if _, err := raw.Write(offer); err != nil {
		return nil, 0, err
	}
	var selectedSec [1]byte
	if _, err := io.ReadFull(raw, selectedSec[:]); err != nil {
		return nil, 0, err
	}
	if selectedSec[0] != 19 {
		return nil, 0, fmt.Errorf("client selected RFB security %d, want 19", selectedSec[0])
	}
	serverVersion := options.serverVersion
	if serverVersion == [2]byte{} {
		serverVersion = [2]byte{0, 2}
	}
	if _, err := raw.Write(serverVersion[:]); err != nil {
		return nil, 0, err
	}
	var clientVeNCryptVersion [2]byte
	if _, err := io.ReadFull(raw, clientVeNCryptVersion[:]); err != nil {
		return nil, 0, err
	}
	if clientVeNCryptVersion != [2]byte{0, 2} {
		return nil, 0, fmt.Errorf("client VeNCrypt version %v", clientVeNCryptVersion)
	}
	subtypeMsg := []byte{0, byte(len(subtypes))}
	for _, st := range subtypes {
		subtypeMsg = binary.BigEndian.AppendUint32(subtypeMsg, st)
	}
	if _, err := raw.Write(subtypeMsg); err != nil {
		return nil, 0, err
	}
	var chosenBytes [4]byte
	if _, err := io.ReadFull(raw, chosenBytes[:]); err != nil {
		return nil, 0, err
	}
	chosen := binary.BigEndian.Uint32(chosenBytes[:])
	serverTLS := options.serverTLS
	if serverTLS == nil {
		certs := vencryptCerts(t)
		serverTLS = &tls.Config{
			Certificates: []tls.Certificate{certs.serverCert},
			MinVersion:   tls.VersionTLS12,
		}
	}
	if _, err := raw.Write([]byte{1}); err != nil {
		return nil, chosen, err
	}
	tlsSrv := tls.Server(raw, serverTLS)
	if err := tlsSrv.Handshake(); err != nil {
		return nil, chosen, err
	}
	return tlsSrv, chosen, nil
}

func readVeNCryptPlainCredentials(reader io.Reader) (username, password string, err error) {
	var lengths [8]byte
	if _, err = io.ReadFull(reader, lengths[:]); err != nil {
		return "", "", err
	}
	userLen := binary.BigEndian.Uint32(lengths[0:4])
	passLen := binary.BigEndian.Uint32(lengths[4:8])
	if userLen > 1024 || passLen > 1024 {
		return "", "", fmt.Errorf("unbounded credential lengths %d/%d", userLen, passLen)
	}
	user := make([]byte, userLen)
	if _, err = io.ReadFull(reader, user); err != nil {
		return "", "", err
	}
	pass := make([]byte, passLen)
	if _, err = io.ReadFull(reader, pass); err != nil {
		return "", "", err
	}
	return string(user), string(pass), nil
}

func finishVeNCryptPeer(s *server, name string) {
	s.t.Helper()
	s.write(u32(0))
	if got := s.read(1); got[0] != 1 {
		s.t.Errorf("ClientInit shared = %d, want 1", got[0])
	}
	s.write(serverInit(10, 10, name))
	format := s.read(24)
	if format[0] != 0 || format[20] != 2 {
		s.t.Errorf("client format/encoding header = %v", format)
	}
	s.read(4 * int(binary.BigEndian.Uint16(format[22:])))
}

func TestVeNCryptAllX509SubtypesAndEncryptedSession(t *testing.T) {
	certs := vencryptCerts(t)
	for _, version := range []struct {
		banner, clientVersion string
		minor                 int
	}{
		{"RFB 003.007\n", "RFB 003.007\n", 7},
		{"RFB 003.008\n", "RFB 003.008\n", 8},
		{"RFB 003.009\n", "RFB 003.008\n", 8},
		{"RFB 005.000\n", "RFB 003.008\n", 8},
	} {
		for _, subtype := range []uint32{vencryptX509None, vencryptX509Vnc, vencryptX509Plain} {
			t.Run(fmt.Sprintf("%s/subtype%d", strings.TrimSpace(version.banner), subtype), func(t *testing.T) {
				var verified, asked int
				gotCreds := make(chan [2]string, 1)
				readyForInput := make(chan struct{})
				clientInput := make(chan []byte, 1)
				serverDone := make(chan struct{})

				config := Config{
					ServerName: "vnc.example.test",
					TLSConfig:  &tls.Config{RootCAs: certs.roots},
				}
				switch subtype {
				case vencryptX509None:
					config.AskPassword = func() (string, error) {
						t.Error("AskPassword called for X509None")
						return "", errors.New("unexpected AskPassword")
					}
					config.AskCredentials = func() (string, string, error) {
						t.Error("AskCredentials called for X509None")
						return "", "", errors.New("unexpected AskCredentials")
					}
				case vencryptX509Vnc:
					config.AskPassword = func() (string, error) {
						asked++
						return "vncpass!", nil
					}
					config.AskCredentials = func() (string, string, error) {
						t.Error("AskCredentials called for X509Vnc")
						return "", "", errors.New("unexpected AskCredentials")
					}
				case vencryptX509Plain:
					config.Username = "ignored-user"
					config.Password = "ignored-pass"
					config.VerifyCertificate = func(der []byte) error {
						verified++
						if !bytes.Equal(der, certs.serverDER) {
							return errors.New("unexpected server certificate DER")
						}
						return nil
					}
					config.AskCredentials = func() (string, string, error) {
						if verified != 1 {
							t.Error("AskCredentials called before VerifyCertificate")
						}
						asked++
						return "管理员", "口令🔒", nil
					}
				}

				client, err := connect(t, config, func(s *server) {
					defer close(serverDone)
					offered := []uint32{vencryptX509None, vencryptX509Vnc, vencryptX509Plain}
					switch subtype {
					case vencryptX509None:
						offered = []uint32{vencryptX509None}
					case vencryptX509Vnc:
						offered = []uint32{vencryptX509None, vencryptX509Vnc}
					}
					tlsConn, chosen, err := vencryptPeer(t, s.conn, vencryptPeerOptions{
						banner:        version.banner,
						clientVersion: version.clientVersion,
						subtypes:      offered,
					})
					if err != nil {
						t.Error(err)
						return
					}
					if chosen != subtype {
						t.Errorf("chosen subtype = %d, want %d", chosen, subtype)
						return
					}
					s.conn = tlsConn
					switch subtype {
					case vencryptX509None:
						gotCreds <- [2]string{"", ""}
					case vencryptX509Vnc:
						challenge := [16]byte{9, 8, 7, 6, 5, 4, 3, 2, 1, 10, 11, 12, 13, 14, 15, 16}
						s.write(challenge[:])
						resp := s.read(16)
						want := vncResponse(challenge, "vncpass!")
						if !bytes.Equal(resp, want[:]) {
							t.Errorf("X509Vnc response mismatch")
							return
						}
						gotCreds <- [2]string{"", "vncpass!"}
					case vencryptX509Plain:
						u, p, err := readVeNCryptPlainCredentials(tlsConn)
						if err != nil {
							t.Error(err)
							return
						}
						gotCreds <- [2]string{u, p}
					}
					finishVeNCryptPeer(s, "VeNCrypt desktop")
					s.readRequest()
					s.write(update(rect(0, 0, 1, 1, EncodingRaw, pixels(1, 44, 55, 66)...)))
					s.readRequest()
					close(readyForInput)
					clientInput <- s.read(6)
					s.finish()
				})
				if err != nil {
					t.Fatal(err)
				}
				if client.Version != version.minor || client.Security != 19 || client.SecuritySubtype != int(subtype) || !client.Encrypted || client.Name != "VeNCrypt desktop" {
					t.Fatalf("client = %+v", client)
				}
				wantAuth := map[uint32]string{vencryptX509None: "none", vencryptX509Vnc: "vnc-password", vencryptX509Plain: "user-password"}[subtype]
				wantSubtypes := map[uint32]string{
					vencryptX509None: "X509None", vencryptX509Vnc: "X509None, X509Vnc", vencryptX509Plain: "X509None, X509Vnc, X509Plain",
				}[subtype]
				if client.Authentication != wantAuth || client.SecurityTypes != "VeNCrypt, VNC Authentication, None" || client.SecuritySubtypes != wantSubtypes ||
					client.Cipher == "" || client.TLSVersion == "" {
					t.Fatalf("incorrect VeNCrypt security metadata: %+v", client)
				}
				creds := <-gotCreds
				switch subtype {
				case vencryptX509None:
					if creds != ([2]string{"", ""}) || asked != 0 {
						t.Fatalf("X509None creds=%q asked=%d", creds, asked)
					}
				case vencryptX509Vnc:
					if creds != ([2]string{"", "vncpass!"}) || asked != 1 {
						t.Fatalf("X509Vnc creds=%q asked=%d", creds, asked)
					}
				case vencryptX509Plain:
					if creds != ([2]string{"管理员", "口令🔒"}) || verified != 1 || asked != 1 {
						t.Fatalf("X509Plain creds=%q verified=%d asked=%d", creds, verified, asked)
					}
				}

				sink := &canvas{}
				done := run(client, sink)
				<-readyForInput
				if err := client.SendPointer(3, 4, 1); err != nil {
					t.Fatal(err)
				}
				if got := <-clientInput; !bytes.Equal(got, []byte{5, 1, 0, 3, 0, 4}) {
					t.Fatalf("encrypted pointer = %v", got)
				}
				<-serverDone
				if err := wait(t, done); errors.Is(err, ErrProtocol) {
					t.Fatalf("Run error = %v", err)
				}
				if got := sink.at(0, 0); got != [3]byte{44, 55, 66} {
					t.Fatalf("pixel = %v, want [44 55 66]", got)
				}
			})
		}
	}
}

func TestVeNCryptSubtypeSelectionAndWayVNCEmptyUsername(t *testing.T) {
	certs := vencryptCerts(t)
	baseTLS := &tls.Config{RootCAs: certs.roots}

	t.Run("prefers X509Plain when username exists", func(t *testing.T) {
		got := chooseVeNCryptSubtype([]uint32{vencryptX509None, vencryptX509Vnc, vencryptX509Plain}, Config{
			Username: "alice",
			Password: "secret",
		})
		if got != vencryptX509Plain {
			t.Fatalf("got %d, want X509Plain", got)
		}
	})

	t.Run("prefers X509Vnc when only password exists", func(t *testing.T) {
		got := chooseVeNCryptSubtype([]uint32{vencryptX509None, vencryptX509Vnc, vencryptX509Plain}, Config{
			Password: "secret",
		})
		if got != vencryptX509Vnc {
			t.Fatalf("got %d, want X509Vnc", got)
		}
	})

	t.Run("both desktop callbacks keep password-only authentication", func(t *testing.T) {
		config := Config{
			AskPassword:    func() (string, error) { return "vnc-secret", nil },
			AskCredentials: func() (string, string, error) { return "", "account-secret", nil },
		}
		offered := []uint32{vencryptX509Plain, vencryptX509Vnc, vencryptX509None}
		if got := chooseVeNCryptSubtype(offered, config); got != vencryptX509Vnc {
			t.Fatalf("both callbacks selected %d, want X509Vnc", got)
		}
		config.Username = "alice"
		if got := chooseVeNCryptSubtype(offered, config); got != vencryptX509Plain {
			t.Fatalf("explicit account selected %d, want X509Plain", got)
		}
	})

	t.Run("prefers X509None over asking when no credentials configured", func(t *testing.T) {
		got := chooseVeNCryptSubtype([]uint32{vencryptX509Vnc, vencryptX509Plain, vencryptX509None}, Config{})
		if got != vencryptX509None {
			t.Fatalf("got %d, want X509None", got)
		}
	})

	t.Run("falls back to X509Vnc to ask when X509None not offered", func(t *testing.T) {
		got := chooseVeNCryptSubtype([]uint32{vencryptX509Plain, vencryptX509Vnc}, Config{})
		if got != vencryptX509Vnc {
			t.Fatalf("got %d, want X509Vnc", got)
		}
	})

	t.Run("WayVNC only X509Plain with static password sends empty username", func(t *testing.T) {
		gotCreds := make(chan [2]string, 1)
		client, err := connect(t, Config{
			ServerName: "vnc.example.test",
			TLSConfig:  baseTLS,
			Password:   "wayvnc-secret",
		}, func(s *server) {
			tlsConn, chosen, err := vencryptPeer(t, s.conn, vencryptPeerOptions{
				subtypes: []uint32{vencryptTLSPlain, vencryptX509Plain},
			})
			if err != nil {
				t.Error(err)
				return
			}
			if chosen != vencryptX509Plain {
				t.Errorf("chosen = %d, want X509Plain", chosen)
				return
			}
			u, p, err := readVeNCryptPlainCredentials(tlsConn)
			if err != nil {
				t.Error(err)
				return
			}
			gotCreds <- [2]string{u, p}
			s.conn = tlsConn
			finishVeNCryptPeer(s, "WayVNC")
		})
		if err != nil {
			t.Fatal(err)
		}
		if creds := <-gotCreds; creds != ([2]string{"", "wayvnc-secret"}) {
			t.Fatalf("creds = %q, want empty username and wayvnc-secret", creds)
		}
		if client.SecuritySubtype != int(vencryptX509Plain) || !client.Encrypted {
			t.Fatalf("client = %+v", client)
		}
	})

	t.Run("WayVNC only X509Plain with AskCredentials allows empty username", func(t *testing.T) {
		gotCreds := make(chan [2]string, 1)
		client, err := connect(t, Config{
			ServerName: "vnc.example.test",
			TLSConfig:  baseTLS,
			AskCredentials: func() (string, string, error) {
				return "", "callback-secret", nil
			},
		}, func(s *server) {
			tlsConn, chosen, err := vencryptPeer(t, s.conn, vencryptPeerOptions{
				subtypes: []uint32{vencryptX509Plain},
			})
			if err != nil {
				t.Error(err)
				return
			}
			if chosen != vencryptX509Plain {
				t.Errorf("chosen = %d, want X509Plain", chosen)
				return
			}
			u, p, err := readVeNCryptPlainCredentials(tlsConn)
			if err != nil {
				t.Error(err)
				return
			}
			gotCreds <- [2]string{u, p}
			s.conn = tlsConn
			finishVeNCryptPeer(s, "WayVNC")
		})
		if err != nil {
			t.Fatal(err)
		}
		if creds := <-gotCreds; creds != ([2]string{"", "callback-secret"}) || !client.Encrypted {
			t.Fatalf("creds = %q client = %+v", creds, client)
		}
	})

	t.Run("X509None offered with X509Plain and no credentials never calls callbacks", func(t *testing.T) {
		client, err := connect(t, Config{
			ServerName: "vnc.example.test",
			TLSConfig:  baseTLS,
		}, func(s *server) {
			tlsConn, chosen, err := vencryptPeer(t, s.conn, vencryptPeerOptions{
				subtypes: []uint32{vencryptX509Plain, vencryptX509None},
			})
			if err != nil {
				t.Error(err)
				return
			}
			if chosen != vencryptX509None {
				t.Errorf("chosen = %d, want X509None", chosen)
				return
			}
			s.conn = tlsConn
			finishVeNCryptPeer(s, "None over TLS")
		})
		if err != nil {
			t.Fatal(err)
		}
		if client.SecuritySubtype != int(vencryptX509None) || !client.Encrypted {
			t.Fatalf("client = %+v", client)
		}
	})
}

func TestVeNCryptNewerServerVersionNegotiates02(t *testing.T) {
	certs := vencryptCerts(t)
	for _, version := range [][2]byte{{0, 3}, {1, 0}, {2, 2}} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			client, err := connect(t, Config{
				ServerName: "vnc.example.test", TLSConfig: &tls.Config{RootCAs: certs.roots},
			}, func(s *server) {
				tlsConn, _, err := vencryptPeer(t, s.conn, vencryptPeerOptions{
					serverVersion: version, subtypes: []uint32{vencryptX509None},
				})
				if err != nil {
					t.Error(err)
					return
				}
				s.conn = tlsConn
				finishVeNCryptPeer(s, "negotiated 0.2")
			})
			if err != nil || !client.Encrypted {
				t.Fatalf("client=%+v error=%v", client, err)
			}
		})
	}
}

type vencryptRemoteAddrConn struct {
	net.Conn
	address net.Addr
}

func (conn vencryptRemoteAddrConn) RemoteAddr() net.Addr { return conn.address }

func TestVeNCryptCertificateVerificationAndRemoteAddrDefault(t *testing.T) {
	certs := vencryptCerts(t)

	t.Run("loopback RemoteAddr host verified when ServerName is empty", func(t *testing.T) {
		rawClient, rawServer := net.Pipe()
		defer rawClient.Close()
		defer rawServer.Close()

		serverErr := make(chan error, 1)
		go func() {
			raw := rawServer
			defer raw.Close()
			s := &server{t: t, conn: raw}
			tlsConn, chosen, err := vencryptPeer(t, raw, vencryptPeerOptions{
				subtypes: []uint32{vencryptX509None},
			})
			if err != nil {
				serverErr <- err
				return
			}
			if chosen != vencryptX509None {
				serverErr <- fmt.Errorf("chosen %d", chosen)
				return
			}
			s.conn = tlsConn
			finishVeNCryptPeer(s, "Loopback TLS")
			serverErr <- nil
		}()

		// Test identity derivation independently of OS loopback socket policy.
		remote := &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)}
		client, err := Connect(vencryptRemoteAddrConn{Conn: rawClient, address: remote}, Config{
			TLSConfig: &tls.Config{RootCAs: certs.roots},
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := <-serverErr; err != nil {
			t.Fatal(err)
		}
		if !client.Encrypted || client.SecuritySubtype != int(vencryptX509None) || client.Name != "Loopback TLS" {
			t.Fatalf("client = %+v", client)
		}
	})

	for _, tc := range []struct {
		name       string
		serverName string
		roots      *x509.CertPool
		serverCert tls.Certificate
		skipVerify bool
	}{
		{
			name:       "untrusted root",
			serverName: "vnc.example.test",
			roots:      x509.NewCertPool(),
			serverCert: certs.serverCert,
		},
		{
			name:       "wrong hostname",
			serverName: "wrong.example.test",
			roots:      certs.roots,
			serverCert: certs.serverCert,
		},
		{
			name:       "expired certificate",
			serverName: "vnc.example.test",
			roots:      certs.roots,
			serverCert: certs.expired,
		},
		{
			name:       "InsecureSkipVerify cannot bypass untrusted root",
			serverName: "vnc.example.test",
			roots:      x509.NewCertPool(),
			serverCert: certs.serverCert,
			skipVerify: true,
		},
		{
			name:       "InsecureSkipVerify cannot bypass wrong hostname",
			serverName: "wrong.example.test",
			roots:      certs.roots,
			serverCert: certs.serverCert,
			skipVerify: true,
		},
		{
			name:       "InsecureSkipVerify cannot bypass expired certificate",
			serverName: "vnc.example.test",
			roots:      certs.roots,
			serverCert: certs.expired,
			skipVerify: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			asked := 0
			callerTLS := &tls.Config{
				RootCAs:            tc.roots,
				InsecureSkipVerify: tc.skipVerify,
			}
			_, err := connect(t, Config{
				ServerName: tc.serverName,
				TLSConfig:  callerTLS,
				AskCredentials: func() (string, string, error) {
					asked++
					return "user", "pass", nil
				},
			}, func(s *server) {
				_, _, _ = vencryptPeer(t, s.conn, vencryptPeerOptions{
					subtypes: []uint32{vencryptX509Plain},
					serverTLS: &tls.Config{
						Certificates: []tls.Certificate{tc.serverCert},
						MinVersion:   tls.VersionTLS12,
					},
				})
			})
			if err == nil {
				t.Fatal("expected certificate verification failure")
			}
			if asked != 0 {
				t.Fatalf("AskCredentials called %d times on untrusted cert", asked)
			}
			if callerTLS.InsecureSkipVerify != tc.skipVerify {
				t.Fatal("caller TLSConfig was mutated")
			}
		})
	}
}

func TestVeNCryptVerifyCertificateCallbackAndCustomVerifiers(t *testing.T) {
	certs := vencryptCerts(t)

	t.Run("VerifyCertificate rejection stops before credentials even with InsecureSkipVerify", func(t *testing.T) {
		rejected := errors.New("certificate pin mismatch")
		asked := 0
		peer := make(chan net.Conn, 1)
		bytesRead := make(chan int, 1)

		_, err := connect(t, Config{
			TLSConfig: &tls.Config{InsecureSkipVerify: true},
			VerifyCertificate: func(der []byte) error {
				if !bytes.Equal(der, certs.serverDER) {
					t.Error("wrong certificate DER passed to callback")
				}
				return rejected
			},
			AskCredentials: func() (string, string, error) {
				asked++
				return "user", "pass", nil
			},
		}, func(s *server) {
			peer <- s.conn
			tlsConn, _, err := vencryptPeer(t, s.conn, vencryptPeerOptions{
				rfbTypes: []byte{19, 2, 1},
				subtypes: []uint32{vencryptX509Plain},
			})
			if err != nil {
				bytesRead <- 0
				return
			}
			var buf [64]byte
			n, _ := tlsConn.Read(buf[:])
			bytesRead <- n
		})
		if !errors.Is(err, rejected) {
			t.Fatalf("err = %v, want %v", err, rejected)
		}
		if asked != 0 {
			t.Fatalf("asked = %d, want 0", asked)
		}
		_ = (<-peer).Close()
		if n := <-bytesRead; n != 0 {
			t.Fatalf("server received %d bytes after cert rejection", n)
		}
	})

	t.Run("caller VerifyPeerCertificate and VerifyConnection rejections are preserved", func(t *testing.T) {
		for _, mode := range []string{"VerifyPeerCertificate", "VerifyConnection"} {
			t.Run(mode, func(t *testing.T) {
				customErr := errors.New("caller hook rejected peer")
				tlsCfg := &tls.Config{RootCAs: certs.roots}
				if mode == "VerifyPeerCertificate" {
					tlsCfg.VerifyPeerCertificate = func([][]byte, [][]*x509.Certificate) error {
						return customErr
					}
				} else {
					tlsCfg.VerifyConnection = func(tls.ConnectionState) error {
						return customErr
					}
				}
				verified := 0
				_, err := connect(t, Config{
					ServerName: "vnc.example.test",
					TLSConfig:  tlsCfg,
					VerifyCertificate: func([]byte) error {
						verified++
						return nil
					},
				}, func(s *server) {
					_, _, _ = vencryptPeer(t, s.conn, vencryptPeerOptions{
						subtypes: []uint32{vencryptX509None},
					})
				})
				if !errors.Is(err, customErr) {
					t.Fatalf("err = %v, want %v", err, customErr)
				}
				if verified != 0 {
					t.Fatalf("VerifyCertificate called %d times after caller hook failed", verified)
				}
			})
		}
	})

	t.Run("TLS below 1.2 is rejected", func(t *testing.T) {
		_, err := connect(t, Config{
			ServerName: "vnc.example.test",
			TLSConfig: &tls.Config{
				RootCAs:    certs.roots,
				MinVersion: tls.VersionTLS10,
			},
		}, func(s *server) {
			_, _, _ = vencryptPeer(t, s.conn, vencryptPeerOptions{
				subtypes: []uint32{vencryptX509None},
				serverTLS: &tls.Config{
					Certificates: []tls.Certificate{certs.serverCert},
					MinVersion:   tls.VersionTLS10,
					MaxVersion:   tls.VersionTLS11,
				},
			})
		})
		if err == nil {
			t.Fatal("expected TLS 1.1 server to be rejected")
		}

		_, err = connect(t, Config{
			ServerName: "vnc.example.test",
			TLSConfig: &tls.Config{
				RootCAs:    certs.roots,
				MaxVersion: tls.VersionTLS11,
			},
		}, func(s *server) {
			_, _, _ = vencryptPeer(t, s.conn, vencryptPeerOptions{
				subtypes: []uint32{vencryptX509None},
			})
		})
		if !errors.Is(err, ErrProtocol) {
			t.Fatalf("err = %v, want ErrProtocol for MaxVersion < 1.2", err)
		}
	})
}

func TestVeNCryptRejectsUnsupportedAndMalformedHandshakes(t *testing.T) {
	t.Run("unsupported subtypes never ask for credentials or cert verification", func(t *testing.T) {
		for _, offered := range [][]uint32{
			{vencryptPlain},
			{vencryptTLSNone},
			{vencryptTLSVnc},
			{vencryptTLSPlain},
			{vencryptPlain, vencryptTLSNone, vencryptTLSVnc, vencryptTLSPlain},
			{19},
			{0},
			{999},
		} {
			t.Run(fmt.Sprint(offered), func(t *testing.T) {
				_, err := connect(t, Config{
					Username: "alice",
					Password: "secret",
					VerifyCertificate: func([]byte) error {
						t.Error("VerifyCertificate called for unsupported subtype")
						return nil
					},
					AskPassword: func() (string, error) {
						t.Error("AskPassword called for unsupported subtype")
						return "", nil
					},
					AskCredentials: func() (string, string, error) {
						t.Error("AskCredentials called for unsupported subtype")
						return "", "", nil
					},
				}, func(s *server) {
					s.write([]byte("RFB 003.008\n"))
					s.read(12)
					s.write([]byte{1, 19})
					s.read(1)
					s.write([]byte{0, 2})
					s.read(2)
					msg := []byte{0, byte(len(offered))}
					for _, st := range offered {
						msg = binary.BigEndian.AppendUint32(msg, st)
					}
					s.write(msg)
				})
				if !errors.Is(err, ErrUnsupported) {
					t.Fatalf("err = %v, want ErrUnsupported", err)
				}
				var unsupported *UnsupportedSecurityError
				if !errors.As(err, &unsupported) || !unsupported.VeNCrypt || unsupported.AuthenticationOnly || !slices.Equal(unsupported.Types, offered) {
					t.Fatalf("unsupported subtypes were not retained: %v", err)
				}
			})
		}
	})

	t.Run("unsupported VeNCrypt versions are rejected", func(t *testing.T) {
		for _, ver := range [][2]byte{{0, 0}, {0, 1}} {
			t.Run(fmt.Sprintf("%d.%d", ver[0], ver[1]), func(t *testing.T) {
				_, err := connect(t, Config{}, func(s *server) {
					s.write([]byte("RFB 003.008\n"))
					s.read(12)
					s.write([]byte{1, 19})
					s.read(1)
					s.write(ver[:])
				})
				if !errors.Is(err, ErrUnsupported) || !errors.Is(err, ErrProtocol) {
					t.Fatalf("version %v: err = %v", ver, err)
				}
			})
		}
	})

	t.Run("non-zero version status is rejected", func(t *testing.T) {
		for _, status := range []byte{1, 0xff} {
			_, err := connect(t, Config{}, func(s *server) {
				s.write([]byte("RFB 003.008\n"))
				s.read(12)
				s.write([]byte{1, 19})
				s.read(1)
				s.write([]byte{0, 2})
				s.read(2)
				s.write([]byte{status})
			})
			if !errors.Is(err, ErrProtocol) {
				t.Fatalf("status %d: err = %v, want ErrProtocol", status, err)
			}
		}
	})

	t.Run("zero subtype count is rejected", func(t *testing.T) {
		_, err := connect(t, Config{}, func(s *server) {
			s.write([]byte("RFB 003.008\n"))
			s.read(12)
			s.write([]byte{1, 19})
			s.read(1)
			s.write([]byte{0, 2})
			s.read(2)
			s.write([]byte{0, 0})
		})
		if !errors.Is(err, ErrUnsupported) || !errors.Is(err, ErrProtocol) {
			t.Fatalf("err = %v, want ErrUnsupported/ErrProtocol", err)
		}
	})

	t.Run("subtype acceptance != 1 is rejected", func(t *testing.T) {
		for _, acc := range []byte{0, 2, 0xff} {
			_, err := connect(t, Config{}, func(s *server) {
				s.write([]byte("RFB 003.008\n"))
				s.read(12)
				s.write([]byte{1, 19})
				s.read(1)
				s.write([]byte{0, 2})
				s.read(2)
				s.write([]byte{0, 1}, u32(int(vencryptX509None)))
				s.read(4)
				s.write([]byte{acc})
			})
			if !errors.Is(err, ErrProtocol) {
				t.Fatalf("acceptance %d: err = %v, want ErrProtocol", acc, err)
			}
		}
	})

	t.Run("corrupt TLS record after subtype acceptance fails cleanly", func(t *testing.T) {
		_, err := connect(t, Config{
			VerifyCertificate: func([]byte) error {
				t.Error("VerifyCertificate called on corrupt TLS")
				return nil
			},
		}, func(s *server) {
			s.write([]byte("RFB 003.008\n"))
			s.read(12)
			s.write([]byte{1, 19})
			s.read(1)
			s.write([]byte{0, 2})
			s.read(2)
			s.write([]byte{0, 1}, u32(int(vencryptX509None)))
			s.read(4)
			s.write([]byte{1})
			var clientHello [2048]byte
			_, _ = s.conn.Read(clientHello[:])
			s.write([]byte("not a valid TLS record"))
			s.finish()
		})
		if err == nil {
			t.Fatal("expected corrupt TLS handshake to fail")
		}
	})

	t.Run("truncated VeNCrypt frames and challenges fail with EOF", func(t *testing.T) {
		certs := vencryptCerts(t)
		for _, stage := range []string{"short version", "short subtypes", "short challenge", "short security result"} {
			t.Run(stage, func(t *testing.T) {
				_, err := connect(t, Config{
					ServerName: "vnc.example.test",
					TLSConfig:  &tls.Config{RootCAs: certs.roots},
					Password:   "secret",
				}, func(s *server) {
					defer s.finish()
					s.write([]byte("RFB 003.008\n"))
					s.read(12)
					s.write([]byte{1, 19})
					s.read(1)
					if stage == "short version" {
						s.write([]byte{0})
						return
					}
					s.write([]byte{0, 2})
					s.read(2)
					if stage == "short subtypes" {
						s.write([]byte{0, 2, 0, 0, 1})
						return
					}
					s.write([]byte{0, 1}, u32(int(vencryptX509Vnc)))
					s.read(4)
					s.write([]byte{1})
					tlsSrv := tls.Server(s.conn, &tls.Config{
						Certificates: []tls.Certificate{certs.serverCert},
						MinVersion:   tls.VersionTLS12,
					})
					if err := tlsSrv.Handshake(); err != nil {
						t.Error(err)
						return
					}
					s.conn = tlsSrv
					if stage == "short challenge" {
						s.write([]byte{1, 2, 3, 4, 5})
						return
					}
					s.write(make([]byte, 16))
					s.read(16)
					s.write([]byte{0, 0})
				})
				if err == nil {
					t.Fatalf("%s: expected error", stage)
				}
			})
		}
	})
}

func TestVeNCryptAuthenticationFailuresAndCredentialBounds(t *testing.T) {
	certs := vencryptCerts(t)
	baseConfig := func() Config {
		return Config{
			ServerName: "vnc.example.test",
			TLSConfig:  &tls.Config{RootCAs: certs.roots},
		}
	}

	for _, version := range []struct {
		banner, clientVersion string
		hasReason             bool
	}{
		{"RFB 003.007\n", "RFB 003.007\n", false},
		{"RFB 003.008\n", "RFB 003.008\n", true},
	} {
		for _, subtype := range []uint32{vencryptX509None, vencryptX509Vnc, vencryptX509Plain} {
			t.Run(fmt.Sprintf("authFailed/%s/subtype%d", strings.TrimSpace(version.banner), subtype), func(t *testing.T) {
				cfg := baseConfig()
				cfg.Username = "alice"
				cfg.Password = "wrong"
				_, err := connect(t, cfg, func(s *server) {
					tlsConn, _, err := vencryptPeer(t, s.conn, vencryptPeerOptions{
						banner:        version.banner,
						clientVersion: version.clientVersion,
						subtypes:      []uint32{subtype},
					})
					if err != nil {
						t.Error(err)
						return
					}
					s.conn = tlsConn
					switch subtype {
					case vencryptX509Vnc:
						s.write(make([]byte, 16))
						s.read(16)
					case vencryptX509Plain:
						if _, _, err := readVeNCryptPlainCredentials(tlsConn); err != nil {
							t.Error(err)
							return
						}
					}
					s.write(u32(1))
					if version.hasReason {
						s.write(u32(len("bad credentials")), []byte("bad credentials"))
					}
					s.finish()
				})
				if !errors.Is(err, ErrAuthFailed) || (version.hasReason && !strings.Contains(err.Error(), "bad credentials")) {
					t.Fatalf("err = %v, want ErrAuthFailed", err)
				}
			})
		}
	}

	t.Run("missing credentials return ErrAuthRequired after TLS verification", func(t *testing.T) {
		for _, subtype := range []uint32{vencryptX509Vnc, vencryptX509Plain} {
			verified := 0
			cfg := baseConfig()
			cfg.VerifyCertificate = func([]byte) error {
				verified++
				return nil
			}
			_, err := connect(t, cfg, func(s *server) {
				_, _, _ = vencryptPeer(t, s.conn, vencryptPeerOptions{
					subtypes: []uint32{subtype},
				})
			})
			if !errors.Is(err, ErrAuthRequired) || verified != 1 {
				t.Fatalf("subtype %d: err = %v, verified = %d", subtype, err, verified)
			}
		}
	})

	t.Run("plain credentials enforce 255-byte UTF-8 bounds without truncation", func(t *testing.T) {
		client := &Client{conn: &eaxMemoryConn{}}
		for _, tc := range []struct {
			name, user, pass string
			wantErr          error
		}{
			{"valid ASCII", "alice", "secret", nil},
			{"valid UTF-8 max", strings.Repeat("中", 85), strings.Repeat("p", 255), nil},
			{"empty user valid", "", "secret", nil},
			{"user > 255 bytes", strings.Repeat("中", 86), "secret", ErrProtocol},
			{"pass > 255 bytes", "alice", strings.Repeat("p", 256), ErrProtocol},
			{"invalid UTF-8 user", "alice\xff", "secret", ErrProtocol},
			{"invalid UTF-8 pass", "alice", "secret\xff", ErrProtocol},
			{"empty password", "alice", "", ErrAuthRequired},
		} {
			t.Run(tc.name, func(t *testing.T) {
				wire, err := client.vencryptPlainCredentials(Config{Username: tc.user, Password: tc.pass})
				if tc.wantErr != nil {
					if !errors.Is(err, tc.wantErr) {
						t.Fatalf("err = %v, want %v", err, tc.wantErr)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				u, p, err := readVeNCryptPlainCredentials(bytes.NewReader(wire))
				if err != nil || u != tc.user || p != tc.pass {
					t.Fatalf("decoded %q/%q err=%v", u, p, err)
				}
			})
		}
	})
}

func TestVeNCryptUserWaitTimeoutPauseFragmentationAndPrefetch(t *testing.T) {
	certs := vencryptCerts(t)

	t.Run("userWait pauses handshake deadline during VerifyCertificate and AskCredentials", func(t *testing.T) {
		client, err := connect(t, Config{
			Timeout: 80 * time.Millisecond,
			VerifyCertificate: func([]byte) error {
				time.Sleep(150 * time.Millisecond)
				return nil
			},
			AskCredentials: func() (string, string, error) {
				time.Sleep(150 * time.Millisecond)
				return "alice", "secret", nil
			},
		}, func(s *server) {
			tlsConn, _, err := vencryptPeer(t, s.conn, vencryptPeerOptions{
				subtypes: []uint32{vencryptX509Plain},
			})
			if err != nil {
				t.Error(err)
				return
			}
			if _, _, err := readVeNCryptPlainCredentials(tlsConn); err != nil {
				t.Error(err)
				return
			}
			s.conn = tlsConn
			finishVeNCryptPeer(s, "Paused deadline")
		})
		if err != nil {
			t.Fatal(err)
		}
		if !client.Encrypted {
			t.Fatal("client not marked encrypted")
		}
	})

	t.Run("deadline is restored after userWait and cancels cleanly on callback error or conn close", func(t *testing.T) {
		_, err := connect(t, Config{
			Timeout: 60 * time.Millisecond,
			VerifyCertificate: func([]byte) error {
				return nil
			},
			AskCredentials: func() (string, string, error) {
				return "alice", "secret", nil
			},
		}, func(s *server) {
			tlsConn, _, err := vencryptPeer(t, s.conn, vencryptPeerOptions{
				subtypes: []uint32{vencryptX509Plain},
			})
			if err != nil {
				return
			}
			_, _, _ = readVeNCryptPlainCredentials(tlsConn)
			// Stall without sending SecurityResult so restored deadline triggers.
			time.Sleep(200 * time.Millisecond)
		})
		if err == nil {
			t.Fatal("expected timeout when server stalls after credentials")
		}

		canceled := errors.New("dialog canceled")
		_, err = connect(t, Config{
			ServerName:  "vnc.example.test",
			TLSConfig:   &tls.Config{RootCAs: certs.roots},
			AskPassword: func() (string, error) { return "", canceled },
		}, func(s *server) {
			tlsConn, _, err := vencryptPeer(t, s.conn, vencryptPeerOptions{
				subtypes: []uint32{vencryptX509Vnc},
			})
			if err != nil {
				return
			}
			_, _ = tlsConn.Write(make([]byte, 16))
		})
		if !errors.Is(err, canceled) {
			t.Fatalf("err = %v, want %v", err, canceled)
		}

		// Closing the original connection during userWait ends the handshake when it resumes.
		clientSide, serverSide := pair(t)
		go func() {
			s := &server{t: t, conn: serverSide}
			_, _, _ = vencryptPeer(t, s.conn, vencryptPeerOptions{
				subtypes: []uint32{vencryptX509None},
			})
		}()
		_, err = Connect(clientSide, Config{
			VerifyCertificate: func([]byte) error {
				_ = clientSide.Close()
				return nil
			},
		})
		if err == nil {
			t.Fatal("expected error when original connection closes during VerifyCertificate")
		}
	})

	t.Run("single-byte fragmented reads succeed across VeNCrypt TLS handshake and session", func(t *testing.T) {
		clientSide, serverSide := pair(t)
		done := make(chan struct{})
		go func() {
			defer close(done)
			s := &server{t: t, conn: serverSide}
			tlsConn, _, err := vencryptPeer(t, s.conn, vencryptPeerOptions{
				subtypes: []uint32{vencryptX509Vnc},
			})
			if err != nil {
				t.Error(err)
				return
			}
			s.conn = tlsConn
			var challenge [16]byte
			s.write(challenge[:])
			s.read(16)
			finishVeNCryptPeer(s, "Fragmented TLS")
		}()
		client, err := Connect(slowConn{clientSide}, Config{
			ServerName: "vnc.example.test",
			TLSConfig:  &tls.Config{RootCAs: certs.roots},
			Password:   "secret",
		})
		if err != nil {
			t.Fatal(err)
		}
		<-done
		if !client.Encrypted || client.SecuritySubtype != int(vencryptX509Vnc) {
			t.Fatalf("client = %+v", client)
		}
	})

	t.Run("readerConn preserves bytes already buffered in client.r when TLS starts", func(t *testing.T) {
		client, err := connect(t, Config{
			ServerName: "vnc.example.test",
			TLSConfig:  &tls.Config{RootCAs: certs.roots},
		}, func(s *server) {
			s.write([]byte("RFB 003.008\n"))
			s.read(12)
			s.write([]byte{1, 19})
			s.read(1)
			s.write([]byte{0, 2})
			s.read(2)
			s.write([]byte{0, 1}, u32(int(vencryptX509None)))
			s.read(4)
			// Coalesce the acceptance byte (1) with the first byte of the TLS
			// ServerHello record (0x16 = Handshake) so client.r buffers 0x16 before
			// tls.Client starts.
			s.write([]byte{1, 0x16})
			tlsSrv := tls.Server(&dropFirstWriteByteConn{Conn: s.conn, want: 0x16}, &tls.Config{
				Certificates: []tls.Certificate{certs.serverCert},
				MinVersion:   tls.VersionTLS12,
			})
			if err := tlsSrv.Handshake(); err != nil {
				t.Error(err)
				return
			}
			s.conn = tlsSrv
			finishVeNCryptPeer(s, "Prefetched TLS")
		})
		if err != nil {
			t.Fatal(err)
		}
		if !client.Encrypted || client.SecuritySubtype != int(vencryptX509None) || client.Name != "Prefetched TLS" {
			t.Fatalf("client = %+v", client)
		}
	})
}

type dropFirstWriteByteConn struct {
	net.Conn
	dropped bool
	want    byte
}

func (c *dropFirstWriteByteConn) Write(p []byte) (int, error) {
	if !c.dropped && len(p) > 0 {
		c.dropped = true
		if p[0] != c.want {
			return 0, fmt.Errorf("first TLS server byte = %#x, want %#x", p[0], c.want)
		}
		n, err := c.Conn.Write(p[1:])
		if err != nil {
			return n, err
		}
		return n + 1, nil
	}
	return c.Conn.Write(p)
}
