package rfb

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

var ra2TestKey = sync.OnceValues(func() (*rsa.PrivateKey, error) {
	return rsa.GenerateKey(rand.Reader, 2048)
})

func ra2Key(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := ra2TestKey()
	if err != nil {
		t.Fatal(err)
	}
	return key
}

type ra2PeerOptions struct {
	banner          string
	clientVersion   string
	subtype         byte
	badProof        bool
	splitProof      bool
	combinedSubtype bool
	badRandomLength bool
}

// ra2PeerHash implements the peer's SHA-1 digest independently of the client.
func ra2PeerHash(parts ...[]byte) []byte {
	h := sha1.New()
	for _, part := range parts {
		_, _ = h.Write(part)
	}
	return h.Sum(nil)
}

// ra2Peer is a scripted server for authentication and encrypted session tests.
// Independent published EAX vectors and the credential-free live RealVNC
// probe supplement the two-sided integration coverage here.
func ra2Peer(t *testing.T, raw net.Conn, options ra2PeerOptions) (*eaxConn, error) {
	t.Helper()
	_ = raw.SetDeadline(time.Now().Add(5 * time.Second))
	key := ra2Key(t)
	wire := ra2PublicKeyWire(&key.PublicKey)
	banner, clientVersion := options.banner, options.clientVersion
	if banner == "" {
		banner = "RFB 005.000\n"
	}
	if clientVersion == "" {
		clientVersion = "RFB 003.008\n"
	}
	if _, err := raw.Write([]byte(banner)); err != nil {
		return nil, err
	}
	var version [12]byte
	if _, err := io.ReadFull(raw, version[:]); err != nil {
		return nil, err
	}
	if string(version[:]) != clientVersion {
		return nil, fmt.Errorf("client version %q", version)
	}
	// This matches the Pi offer and also checks that None/DES are not preferred
	// over available authenticated encryption.
	if _, err := raw.Write([]byte{4, 13, 5, 1, 2}); err != nil {
		return nil, err
	}
	var selection [1]byte
	if _, err := io.ReadFull(raw, selection[:]); err != nil {
		return nil, err
	}
	if selection[0] != 5 {
		return nil, fmt.Errorf("client security %d", selection[0])
	}
	if _, err := raw.Write(wire); err != nil {
		return nil, err
	}
	clientKey, clientWire, err := readRA2PublicKey(raw)
	if err != nil {
		return nil, err
	}
	var length [2]byte
	if _, err := io.ReadFull(raw, length[:]); err != nil {
		return nil, err
	}
	if int(binary.BigEndian.Uint16(length[:])) != key.Size() {
		return nil, errors.New("wrong client random size")
	}
	encrypted := make([]byte, key.Size())
	if _, err := io.ReadFull(raw, encrypted); err != nil {
		return nil, err
	}
	clientRandom, err := rsa.DecryptPKCS1v15(rand.Reader, key, encrypted)
	if err != nil || len(clientRandom) != 16 {
		return nil, errors.New("invalid client random")
	}
	serverRandom := []byte("server random!!!")
	encrypted, err = rsa.EncryptPKCS1v15(rand.Reader, clientKey, serverRandom)
	if err != nil {
		return nil, err
	}
	message := binary.BigEndian.AppendUint16(nil, uint16(len(encrypted)))
	if options.badRandomLength {
		binary.BigEndian.PutUint16(message, uint16(len(encrypted)-1))
	}
	message = append(message, encrypted...)
	if _, err := raw.Write(message); err != nil {
		return nil, err
	}
	secured, err := newEAXConn(raw, ra2PeerHash(serverRandom, clientRandom)[:16], ra2PeerHash(clientRandom, serverRandom)[:16])
	if err != nil {
		return nil, err
	}
	proof := make([]byte, sha1.Size)
	if _, err := io.ReadFull(secured, proof); err != nil {
		return nil, err
	}
	if !bytes.Equal(proof, ra2PeerHash(clientWire, wire)) {
		return nil, errors.New("client public key proof mismatch")
	}
	proof = ra2PeerHash(wire, clientWire)
	if options.badProof {
		proof[0] ^= 1
	}
	if options.combinedSubtype {
		proof = append(proof, options.subtype)
	}
	if options.splitProof {
		if _, err := secured.Write(proof[:7]); err != nil {
			return nil, err
		}
		proof = proof[7:]
	}
	if _, err := secured.Write(proof); err != nil {
		return nil, err
	}
	if !options.badProof && !options.combinedSubtype {
		if _, err := secured.Write([]byte{options.subtype}); err != nil {
			return nil, err
		}
	}
	return secured.(*eaxConn), nil
}

func readPeerCredentials(reader io.Reader) (username, password string, err error) {
	var count [1]byte
	if _, err = io.ReadFull(reader, count[:]); err != nil {
		return
	}
	user := make([]byte, int(count[0]))
	if _, err = io.ReadFull(reader, user); err != nil {
		return
	}
	if _, err = io.ReadFull(reader, count[:]); err != nil {
		return
	}
	secret := make([]byte, int(count[0]))
	if _, err = io.ReadFull(reader, secret); err != nil {
		return
	}
	return string(user), string(secret), nil
}

func finishRA2Peer(s *server) {
	s.t.Helper()
	s.write(u32(0)) // SecurityResult is encrypted too
	if got := s.read(1); got[0] != 1 {
		s.t.Error("exclusive session requested")
	}
	s.write(serverInit(10, 10, "RSA-AES desktop"))
	format := s.read(24)
	if format[0] != 0 || format[20] != 2 {
		s.t.Errorf("client format/encoding header = %v", format)
	}
	s.read(4 * int(binary.BigEndian.Uint16(format[22:])))
}

func TestRA2EncryptedAuthenticationAndSession(t *testing.T) {
	for _, variant := range []string{"userpass", "password", "split proof", "combined proof and subtype"} {
		t.Run(variant, func(t *testing.T) {
			options := ra2PeerOptions{subtype: ra2UserPass, splitProof: variant == "split proof", combinedSubtype: variant == "combined proof and subtype"}
			if variant == "password" {
				options.subtype = ra2Password
			}
			var verified, asked int
			credentials := make(chan [2]string, 1)
			serverFinished := make(chan struct{})
			config := Config{Username: "wrong cached user", Password: "wrong cached password",
				VerifyServerKey: func(der []byte) error {
					verified++
					parsed, err := x509.ParsePKIXPublicKey(der)
					if err != nil {
						return err
					}
					if !parsed.(*rsa.PublicKey).Equal(&ra2Key(t).PublicKey) {
						return errors.New("wrong verification key")
					}
					return nil
				},
				AskCredentials: func() (string, string, error) {
					if verified != 1 {
						t.Error("credentials asked before verifying key")
					}
					asked++
					return "用户", "密码🔐", nil
				},
			}
			if options.subtype == ra2Password {
				config.Password = ""
				config.AskPassword = func() (string, error) { asked++; return "password only", nil }
				config.AskCredentials = func() (string, string, error) {
					t.Error("UserPass callback used for password-only authentication")
					return "", "", errors.New("wrong callback")
				}
			}
			client, err := connect(t, config, func(s *server) {
				defer close(serverFinished)
				secured, err := ra2Peer(t, s.conn, options)
				if err != nil {
					t.Error(err)
					return
				}
				s.conn = secured
				user, password, err := readPeerCredentials(secured)
				if err != nil {
					t.Error(err)
					return
				}
				credentials <- [2]string{user, password}
				finishRA2Peer(s)
				s.readRequest()
				s.write(update(rect(0, 0, 1, 1, EncodingRaw, pixels(1, 11, 22, 33)...)))
				s.readRequest()
				s.finish()
			})
			if err != nil {
				t.Fatal(err)
			}
			got := <-credentials
			want := [2]string{"用户", "密码🔐"}
			if options.subtype == ra2Password {
				want = [2]string{"", "password only"}
			}
			if got != want || verified != 1 || asked != 1 {
				t.Fatalf("credentials=%q want=%q;verified=%d asked=%d", got, want, verified, asked)
			}
			if client.Version != 8 || client.Security != 5 || !client.Encrypted || client.Name != "RSA-AES desktop" {
				t.Fatalf("connected client = %+v", client)
			}
			sink := &canvas{}
			done := run(client, sink)
			<-serverFinished
			if err := wait(t, done); errors.Is(err, ErrProtocol) {
				t.Fatalf("encrypted update: %v", err)
			}
			if got := sink.at(0, 0); got != [3]byte{11, 22, 33} {
				t.Fatalf("encrypted pixel=%v", got)
			}
		})
	}
}

func TestRA2DoesNotSendCredentialsToUnverifiedOrUnprovedServer(t *testing.T) {
	for _, failure := range []string{"no verifier", "rejected key", "bad proof", "unknown subtype", "cancel credentials"} {
		t.Run(failure, func(t *testing.T) {
			stop := errors.New("user refused")
			asked := 0
			config := Config{VerifyServerKey: func([]byte) error { return nil }, AskCredentials: func() (string, string, error) { asked++; return "", "", stop }}
			if failure == "no verifier" {
				config.VerifyServerKey = nil
			}
			if failure == "rejected key" {
				config.VerifyServerKey = func([]byte) error { return stop }
			}
			options := ra2PeerOptions{subtype: ra2UserPass, badProof: failure == "bad proof"}
			if failure == "unknown subtype" {
				options.subtype = 99
			}
			bytesReceived := make(chan int, 1)
			peer := make(chan net.Conn, 1)
			_, err := connect(t, config, func(s *server) {
				peer <- s.conn
				secured, err := ra2Peer(t, s.conn, options)
				if err != nil {
					bytesReceived <- 0
					return
				}
				data := make([]byte, 600)
				n, _ := secured.Read(data)
				bytesReceived <- n
			})
			switch failure {
			case "no verifier":
				if !errors.Is(err, ErrServerKeyRequired) {
					t.Fatalf("error=%v", err)
				}
			case "rejected key", "cancel credentials":
				if !errors.Is(err, stop) {
					t.Fatalf("error=%v", err)
				}
			case "bad proof":
				if !errors.Is(err, ErrProtocol) {
					t.Fatalf("error=%v", err)
				}
			case "unknown subtype":
				if !errors.Is(err, ErrUnsupported) {
					t.Fatalf("error=%v", err)
				}
			}
			wantAsked := 0
			if failure == "cancel credentials" {
				wantAsked = 1
			}
			if asked != wantAsked {
				t.Fatalf("credentials callback called %d times", asked)
			}
			// Close the peer explicitly; Connect leaves ownership with its caller.
			// This wakes the server without waiting for its read deadline.
			_ = (<-peer).Close()
			if n := <-bytesReceived; n != 0 {
				t.Errorf("sent %d credential bytes", n)
			}
		})
	}
}

func TestRA2WrongPasswordRemainsAuthFailure(t *testing.T) {
	for _, version := range []struct {
		banner, clientVersion string
		hasReason             bool
	}{
		{"RFB 003.007\n", "RFB 003.007\n", false},
		{"RFB 003.008\n", "RFB 003.008\n", true},
		{"RFB 003.009\n", "RFB 003.008\n", true},
		{"RFB 005.000\n", "RFB 003.008\n", true},
	} {
		for _, subtype := range []byte{ra2UserPass, ra2Password} {
			t.Run(fmt.Sprintf("%s/subtype%d", strings.TrimSpace(version.banner), subtype), func(t *testing.T) {
				_, err := connect(t, Config{
					Username: "pi", Password: "wrong", VerifyServerKey: func([]byte) error { return nil },
				}, func(s *server) {
					secured, err := ra2Peer(t, s.conn, ra2PeerOptions{
						banner: version.banner, clientVersion: version.clientVersion, subtype: subtype,
					})
					if err != nil {
						t.Error(err)
						return
					}
					if _, _, err = readPeerCredentials(secured); err != nil {
						t.Error(err)
						return
					}
					s.conn = secured
					s.write(u32(1))
					if version.hasReason {
						s.write(u32(len("bad password")), []byte("bad password"))
					}
					s.finish()
				})
				if !errors.Is(err, ErrAuthFailed) || version.hasReason && !strings.Contains(err.Error(), "bad password") {
					t.Fatalf("error=%v want authentication failure", err)
				}
			})
		}
	}
}

func TestRA2StandardVersionsAndCredentialSubtypes(t *testing.T) {
	for _, version := range []struct {
		banner string
		minor  int
	}{
		{"RFB 003.007\n", 7},
		{"RFB 003.008\n", 8},
		{"RFB 003.009\n", 8},
	} {
		for _, subtype := range []byte{ra2UserPass, ra2Password} {
			t.Run(fmt.Sprintf("%s/subtype%d", strings.TrimSpace(version.banner), subtype), func(t *testing.T) {
				credentials := make(chan [2]string, 1)
				client, err := connect(t, Config{
					Username: "pi", Password: "system password",
					VerifyServerKey: func([]byte) error { return nil },
				}, func(s *server) {
					secured, err := ra2Peer(t, s.conn, ra2PeerOptions{
						banner: version.banner, clientVersion: fmt.Sprintf("RFB 003.%03d\n", version.minor), subtype: subtype,
					})
					if err != nil {
						t.Error(err)
						return
					}
					user, password, err := readPeerCredentials(secured)
					if err != nil {
						t.Error(err)
						return
					}
					credentials <- [2]string{user, password}
					s.conn = secured
					finishRA2Peer(s)
				})
				if err != nil {
					t.Fatal(err)
				}
				want := [2]string{"pi", "system password"}
				if subtype == ra2Password {
					want[0] = ""
				}
				if got := <-credentials; got != want {
					t.Fatalf("credentials=%q want=%q", got, want)
				}
				if client.Version != version.minor || client.Security != 5 || !client.Encrypted {
					t.Fatalf("version=%d security=%d encrypted=%t", client.Version, client.Security, client.Encrypted)
				}
			})
		}
	}
}

func TestRA2SecurityOfferOrderAndDuplicatesNeverWeakenKeyRejection(t *testing.T) {
	for _, offered := range [][]byte{
		{5}, {5, 1, 2, 13}, {1, 2, 13, 5}, {2, 5, 1}, {13, 5, 5, 2, 1},
	} {
		for _, havePassword := range []bool{false, true} {
			t.Run(fmt.Sprintf("%v/password=%t", offered, havePassword), func(t *testing.T) {
				rejected := errors.New("server key rejected")
				config := Config{
					VerifyServerKey: func([]byte) error { return rejected },
					AskCredentials: func() (string, string, error) {
						t.Error("credentials requested after rejecting identity")
						return "", "", rejected
					},
				}
				if havePassword {
					config.Password = "cached legacy password"
				}
				_, err := connect(t, config, func(s *server) {
					s.write([]byte("RFB 003.008\n"))
					s.read(12)
					s.write(append([]byte{byte(len(offered))}, offered...))
					if got := s.read(1); got[0] != 5 {
						t.Errorf("selected security %d, want RSA-AES", got[0])
						s.finish()
						return
					}
					s.write(ra2PublicKeyWire(&ra2Key(t).PublicKey))
				})
				if !errors.Is(err, rejected) {
					t.Fatalf("error=%v want rejected identity", err)
				}
			})
		}
	}
}

func TestRA2UnsupportedSecurityOffersDoNotAskForCredentials(t *testing.T) {
	for _, offered := range [][]byte{{13}, {6}, {129}, {130}, {18}, {19}, {13, 6, 129, 130, 18, 19}} {
		t.Run(fmt.Sprint(offered), func(t *testing.T) {
			_, err := connect(t, Config{
				Password: "cached password",
				AskPassword: func() (string, error) {
					t.Error("password requested for unsupported security")
					return "", nil
				},
				AskCredentials: func() (string, string, error) {
					t.Error("system credentials requested for unsupported security")
					return "", "", nil
				},
				VerifyServerKey: func([]byte) error {
					t.Error("identity requested for unsupported security")
					return nil
				},
			}, func(s *server) {
				s.write([]byte("RFB 003.008\n"))
				s.read(12)
				s.write(append([]byte{byte(len(offered))}, offered...))
			})
			if !errors.Is(err, ErrUnsupported) {
				t.Fatalf("error=%v want unsupported security", err)
			}
		})
	}
}

func TestRA2RejectsMalformedKeysBeforeVerification(t *testing.T) {
	good := ra2PublicKeyWire(&ra2Key(t).PublicKey)
	for _, test := range []struct {
		name string
		wire []byte
	}{
		{"short", u32(1023)}, {"huge", u32(8193)}, {"overflow", u32(-1)},
		{"truncated", good[:len(good)-1]},
		{"zero modulus", append(u32(2048), make([]byte, 512)...)},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := readRA2PublicKey(bytes.NewReader(test.wire))
			if err == nil {
				t.Fatal("malformed key accepted")
			}
		})
	}
	for _, exponent := range []int64{0, 1, 2, 1 << 32} {
		t.Run(fmt.Sprint("exponent ", exponent), func(t *testing.T) {
			wire := bytes.Clone(good)
			clear(wire[4+256:])
			big.NewInt(exponent).FillBytes(wire[4+256:])
			_, _, err := readRA2PublicKey(bytes.NewReader(wire))
			if !errors.Is(err, ErrProtocol) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestRA2RejectsEncryptedRandomLengthBeforeCredentials(t *testing.T) {
	asked := 0
	_, err := connect(t, Config{
		VerifyServerKey: func([]byte) error { return nil },
		AskCredentials:  func() (string, string, error) { asked++; return "pi", "secret", nil },
	}, func(s *server) {
		_, _ = ra2Peer(t, s.conn, ra2PeerOptions{subtype: ra2UserPass, badRandomLength: true})
	})
	if !errors.Is(err, ErrProtocol) || asked != 0 {
		t.Fatalf("error=%v asked=%d", err, asked)
	}
}

func TestRealVNCBannerDowngradePreservesLegacyAuthentication(t *testing.T) {
	for _, security := range []byte{1, 2} {
		t.Run(fmt.Sprint(security), func(t *testing.T) {
			client, err := connect(t, Config{Password: "legacy"}, func(s *server) {
				s.write([]byte("RFB 005.000\n"))
				if got := s.read(12); string(got) != "RFB 003.008\n" {
					t.Errorf("version=%q", got)
				}
				s.write([]byte{1, security})
				s.read(1)
				if security == 2 {
					challenge := [16]byte{1, 2, 3}
					s.write(challenge[:])
					want := vncResponse(challenge, "legacy")
					if got := s.read(16); !bytes.Equal(got, want[:]) {
						t.Error("legacy response changed")
					}
				}
				finishRA2Peer(s)
			})
			if err != nil || client.Version != 8 || client.Security != int(security) || client.Encrypted {
				t.Fatalf("client=%v error=%v", client, err)
			}
		})
	}
	for _, banner := range []string{"RFB 004.000\n", "RFB 004.001\n", "RFB 005.001\n", "RFB 006.000\n"} {
		_, err := connect(t, Config{}, func(s *server) { s.write([]byte(banner)) })
		if !errors.Is(err, ErrUnsupported) {
			t.Errorf("%q:error=%v", banner, err)
		}
	}
}

func TestRA2CredentialsByteLimitsAndCallbacks(t *testing.T) {
	for _, test := range []struct {
		name, user, password string
		subtype              byte
		valid                bool
	}{
		{"system", "pi", "secret", 1, true}, {"VNC password", "ignored", "secret", 2, true},
		{"max UTF8", strings.Repeat("中", 85), strings.Repeat("p", 255), 1, true},
		{"user too long", strings.Repeat("中", 86), "secret", 1, false},
		{"password too long", "pi", strings.Repeat("p", 256), 1, false},
		{"invalid UTF8", "pi", "\xff", 1, false}, {"empty account", "", "secret", 1, true},
		{"missing password", "pi", "", 1, false}, {"unknown subtype", "pi", "secret", 99, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &Client{}
			data, err := client.ra2Credentials(Config{Username: test.user, Password: test.password}, test.subtype)
			if (err == nil) != test.valid {
				t.Fatalf("error=%v", err)
			}
			if test.valid {
				user, password, err := readPeerCredentials(bytes.NewReader(data))
				want := test.user
				if test.subtype == 2 {
					want = ""
				}
				if err != nil || user != want || password != test.password {
					t.Fatalf("credentials=%q/%q error=%v", user, password, err)
				}
			}
		})
	}
}

func FuzzRA2PublicKey(f *testing.F) {
	// This is only a parser seed; the modulus need not have known factors.
	modulus := new(big.Int).Lsh(big.NewInt(1), 1023)
	modulus.Add(modulus, big.NewInt(3))
	f.Add(ra2PublicKeyWire(&rsa.PublicKey{N: modulus, E: 65537}))
	f.Add(u32(8193))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, input []byte) {
		key, wire, err := readRA2PublicKey(bytes.NewReader(input))
		if err != nil {
			return
		}
		if key == nil || key.N.BitLen() < ra2MinKeyBits || key.N.BitLen() > ra2MaxKeyBits || len(wire) > 4+2*(ra2MaxKeyBits/8) {
			t.Fatal("unbounded or invalid key accepted")
		}
		if len(wire) != 4+2*key.Size() {
			t.Fatal("accepted key has inconsistent serialized size")
		}
		if _, err := x509.MarshalPKIXPublicKey(key); err != nil {
			t.Fatalf("accepted key cannot be represented for identity verification: %v", err)
		}
	})
}
