package rfb

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"testing"
)

func TestRSAAESVariantsAuthenticateAndCarrySession(t *testing.T) {
	for _, security := range []byte{5, 129, 6, 130} {
		for _, subtype := range []byte{ra2UserPass, ra2Password} {
			t.Run(fmt.Sprintf("type%d/subtype%d", security, subtype), func(t *testing.T) {
				encrypted := security == 5 || security == 129
				mode := map[byte]string{5: "RA2", 129: "RA256", 6: "RA2ne", 130: "RA256ne"}[security]
				var offered []byte
				wantOffers := "#13, " + mode + ", None, VNC Authentication"
				if encrypted {
					offered = []byte{19, security, 42}
					wantOffers = "VeNCrypt, " + mode + ", #42"
				}
				verified, asked := 0, 0
				credentials := make(chan [2]string, 1)
				finished := make(chan struct{})
				config := Config{
					AllowUnencrypted: !encrypted,
					VerifyServerKey:  func([]byte) error { verified++; return nil },
					AskCredentials: func() (string, string, error) {
						if verified != 1 {
							t.Error("credentials requested before verifying server identity")
						}
						asked++
						// Deliberately empty for a WayVNC password configuration.
						return "", "correct password", nil
					},
					AskPassword: func() (string, error) {
						if verified != 1 {
							t.Error("password requested before verifying server identity")
						}
						asked++
						return "correct password", nil
					},
				}
				client, err := connect(t, config, func(s *server) {
					defer close(finished)
					raw := s.conn
					secured, err := ra2Peer(t, raw, ra2PeerOptions{
						security: security, subtype: subtype, rfbTypes: offered,
						combinedSubtype: true, splitProof: true,
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
					if encrypted {
						s.conn = secured
					}
					finishRA2Peer(s)
					s.readRequest()
					s.write(update(rect(0, 0, 1, 1, EncodingRaw, pixels(1, 19, 20, 21)...)))
					s.readRequest()
					s.finish()
				})
				if err != nil {
					t.Fatal(err)
				}
				if got := <-credentials; got != [2]string{"", "correct password"} || verified != 1 || asked != 1 {
					t.Fatalf("credentials=%q verified=%d asked=%d", got, verified, asked)
				}
				if client.Security != int(security) || client.Encrypted != encrypted {
					t.Fatalf("security=%d encrypted=%t", client.Security, client.Encrypted)
				}
				wantAuth := "vnc-password"
				if subtype == ra2UserPass {
					wantAuth = "user-password"
				}
				wantCipher := "AES-128-EAX"
				if security == 129 || security == 130 {
					wantCipher = "AES-256-EAX"
				}
				if client.SecurityMode != mode || client.Authentication != wantAuth || client.Cipher != wantCipher || client.TLSVersion != "" ||
					client.SecurityTypes != wantOffers || client.SecuritySubtypes != "" {
					t.Fatalf("incorrect negotiated security metadata: %+v", client)
				}
				sink := &canvas{}
				done := run(client, sink)
				<-finished
				if err := wait(t, done); errors.Is(err, ErrProtocol) {
					t.Fatal(err)
				}
				if got := sink.at(0, 0); got != [3]byte{19, 20, 21} {
					t.Fatalf("pixel=%v", got)
				}
			})
		}
	}
}

func TestRSAAESAuthenticationOnlyPreservesRawBufferedStatus(t *testing.T) {
	for _, security := range []byte{6, 130} {
		t.Run(fmt.Sprint(security), func(t *testing.T) {
			client, err := connect(t, Config{
				Password: "password", AllowUnencrypted: true,
				VerifyServerKey: func([]byte) error { return nil },
			}, func(s *server) {
				secured, err := ra2Peer(t, s.conn, ra2PeerOptions{
					security: security, subtype: ra2Password, combinedSubtype: true, prefetchRawStatus: true,
				})
				if err != nil {
					t.Error(err)
					return
				}
				if _, _, err := readPeerCredentials(secured); err != nil {
					t.Error(err)
					return
				}
				if got := s.read(1); !bytes.Equal(got, []byte{1}) {
					t.Errorf("raw ClientInit=%x", got)
				}
				s.write(serverInit(10, 10, "raw after authentication"))
				format := s.read(24)
				s.read(4 * int(binary.BigEndian.Uint16(format[22:])))
			})
			if err != nil {
				t.Fatal(err)
			}
			if client.Encrypted || client.Name != "raw after authentication" {
				t.Fatalf("encrypted=%t name=%q", client.Encrypted, client.Name)
			}
		})
	}
}

func TestRSAAESAuthenticationOnlyRejectsEncryptedTailBeforeCredentials(t *testing.T) {
	for _, security := range []byte{6, 130} {
		t.Run(fmt.Sprint(security), func(t *testing.T) {
			asked := 0
			peer := make(chan net.Conn, 1)
			received := make(chan int, 1)
			_, err := connect(t, Config{
				AllowUnencrypted: true,
				VerifyServerKey:  func([]byte) error { return nil },
				AskCredentials:   func() (string, string, error) { asked++; return "pi", "secret", nil },
			}, func(s *server) {
				peer <- s.conn
				secured, err := ra2Peer(t, s.conn, ra2PeerOptions{
					security: security, subtype: ra2UserPass, combinedSubtype: true, extraEncrypted: true,
				})
				if err != nil {
					received <- 0
					return
				}
				var data [600]byte
				n, _ := secured.Read(data[:])
				received <- n
			})
			if !errors.Is(err, ErrProtocol) || asked != 0 {
				t.Fatalf("error=%v asked=%d", err, asked)
			}
			_ = (<-peer).Close()
			if n := <-received; n != 0 {
				t.Fatalf("sent %d credential bytes", n)
			}
		})
	}
}

func TestRSAAESVariantsRejectIdentityAndProofWithoutCredentials(t *testing.T) {
	for _, security := range []byte{129, 6, 130} {
		for _, failure := range []string{"rejected identity", "bad proof"} {
			t.Run(fmt.Sprintf("type%d/%s", security, failure), func(t *testing.T) {
				stop := errors.New("identity rejected")
				asked := 0
				peer := make(chan net.Conn, 1)
				received := make(chan int, 1)
				_, err := connect(t, Config{
					AllowUnencrypted: true,
					VerifyServerKey: func([]byte) error {
						if failure == "rejected identity" {
							return stop
						}
						return nil
					},
					AskCredentials: func() (string, string, error) { asked++; return "pi", "secret", nil },
				}, func(s *server) {
					peer <- s.conn
					secured, err := ra2Peer(t, s.conn, ra2PeerOptions{
						security: security, subtype: ra2UserPass, badProof: failure == "bad proof",
					})
					if err != nil {
						received <- 0
						return
					}
					var data [600]byte
					n, _ := secured.Read(data[:])
					received <- n
				})
				want := error(ErrProtocol)
				if failure == "rejected identity" {
					want = stop
				}
				if !errors.Is(err, want) || asked != 0 {
					t.Fatalf("error=%v asked=%d", err, asked)
				}
				_ = (<-peer).Close()
				if n := <-received; n != 0 {
					t.Fatalf("sent %d credential bytes", n)
				}
			})
		}
	}
}
