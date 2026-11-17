package workbench

import (
	"context"
	"strings"
	"testing"
	"time"

	"motty/internal/desktop"
	"motty/internal/desktop/pins"
	"motty/internal/rfb"
)

func TestVNCAuthenticationRetryReportsOnlyCurrentHandshakeIdentity(t *testing.T) {
	for _, kind := range []string{vncServerKeyType, vncCertificateType} {
		for _, repeatIdentity := range []bool{false, true} {
			name := kind + "/traditional VNC"
			if repeatIdentity {
				name = kind + "/same identity"
			}
			t.Run(name, func(t *testing.T) {
				host := Host{Protocol: protocolVNC, Address: "pi.example.com", Port: 5900}
				identity := []byte("verified server identity")
				fingerprint := pins.Fingerprint(identity)
				attempts := 0
				var conn *desktopConn
				opener := func(_ context.Context, request desktopRequest) (desktop.Session, error) {
					attempts++
					if got := conn.verifiedServerIdentity(); got.Fingerprint != "" {
						t.Fatalf("new handshake retained the previous identity: %+v", got)
					}
					mode, encrypted := "VNC Authentication", false
					if attempts == 1 || repeatIdentity {
						verify := request.VerifyServerKey
						mode = "RA2"
						if kind == vncCertificateType {
							verify, mode = request.Verify, "VeNCrypt X509Vnc"
						}
						if err := verify("pi.example.com:5900", identity); err != nil {
							return nil, err
						}
						encrypted = true
					}
					if attempts == 1 {
						return nil, rfb.ErrAuthFailed
					}
					remote := &securityDesktop{Session: newPictureOnly(), info: desktop.Info{
						Protocol: "VNC", Security: mode, ProtocolEncrypted: encrypted, Encrypted: encrypted,
					}}
					_ = remote.Close()
					return remote, nil
				}
				conn = newDesktopConn(host, nil, opener, time.Second, nil, nil)
				defer conn.cancel()
				ctx, cancel := context.WithTimeout(conn.ctx, time.Second)
				defer cancel()
				conn.ctx = ctx
				// The first identity is trusted once. A retry must reuse that trust
				// without asking again, while recording the new handshake's identity.
				conn.certAnswers <- certReply{accept: true}
				conn.run()
				if attempts != 2 || conn.Desktop() == nil {
					t.Fatalf("authentication retry did not open a desktop: attempts=%d, status=%+v", attempts, conn.Status())
				}
				if conn.certAsked != 1 {
					t.Fatalf("authentication retry discarded trusted identity: asked %d times", conn.certAsked)
				}
				got := conn.verifiedServerIdentity()
				if repeatIdentity {
					if got.Fingerprint != fingerprint || got.Type != kind || got.Address != "pi.example.com:5900" {
						t.Fatalf("trusted retry lost the current identity: %+v", got)
					}
				} else if got.Fingerprint != "" || got.Type != "" || got.Address != "" {
					t.Fatalf("traditional VNC reported a previous encrypted handshake's identity: %+v", got)
				}
				state := newTestState(t, testOptions(t))
				session := &Session{host: host, conn: conn, desk: &desktopView{session: conn.Desktop()}}
				if details := state.vncSecurityDetails(session); strings.Contains(details, fingerprint) != repeatIdentity {
					t.Fatalf("security details reported an identity from another handshake: %q", details)
				}
			})
		}
	}
}
