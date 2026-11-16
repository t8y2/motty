package rdp

import (
	"crypto/tls"
	"log/slog"
	"net"
	"os"
	"testing"
	"time"

	"motty/internal/rdpcore/protocol/nla"
	"motty/internal/rdpcore/protocol/tpkt"
	"motty/internal/rdpcore/protocol/x224"
)

// TestLiveCredSSPTokenLegs runs the CredSSP handshake against a server that
// requires NLA, once with SPNEGO-wrapped tokens and once with raw NTLM.
//
// It exists because the two token encodings take different paths through the
// server: FreeRDP's Negotiate package (GNOME Remote Desktop) answers the
// NegTokenInit with request-mic, signs the mechanism list, and reads the
// public-key binding only in a later TSRequest, while raw NTLM completes in one
// message. The unit tests model both peers; this test proves the model against
// a real libfreerdp server. Such a server can be started in a throwaway
// container, for example:
//
//	docker run -d --name nla -p 3389:3389 ubuntu:24.04 sleep infinity
//	docker exec nla bash -c 'apt-get update && DEBIAN_FRONTEND=noninteractive \
//	  apt-get install -y freerdp3-shadow-x11 openssl xvfb'
//	docker exec nla bash -c 'mkdir -p /root/.config/freerdp/shadow && cd ... && \
//	  openssl req -x509 -newkey rsa:2048 -keyout shadow.key -out shadow.crt \
//	    -days 30 -nodes -subj /CN=shadow && \
//	  printf "User:::<nt hash of the password>:\n" > /root/sam && \
//	  (Xvfb :99 &) && DISPLAY=:99 freerdp-shadow-cli3 /sec:nla \
//	    /sam-file:/root/sam /port:3389'
//
// The NT hash is MD4 of the UTF-16LE password; for "Password" it is
// a4f49c406510bdcab6824ee7c30fd852.
func TestLiveCredSSPTokenLegs(t *testing.T) {
	addr := os.Getenv("MOTTY_RDP_NLA_ADDR")
	user := os.Getenv("MOTTY_RDP_NLA_USER")
	password := os.Getenv("MOTTY_RDP_NLA_PASSWORD")
	if addr == "" || user == "" || password == "" {
		t.Skip("MOTTY_RDP_NLA_ADDR, MOTTY_RDP_NLA_USER and MOTTY_RDP_NLA_PASSWORD not set")
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []bool{false, true} {
		name := "spnego"
		if raw {
			name = "raw"
		}
		t.Run(name, func(t *testing.T) {
			conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			_ = conn.SetDeadline(time.Now().Add(20 * time.Second))
			tp := tpkt.NewConn(conn, logger)
			request := &x224.ConnectionRequest{
				Cookie:          user,
				SendCookie:      true,
				RequestedProtos: x224.ProtocolSSL | x224.ProtocolHybrid,
			}
			if err := tp.Write(request.Encode()); err != nil {
				t.Fatal(err)
			}
			response, err := tp.Read()
			if err != nil {
				t.Fatal(err)
			}
			confirm, err := x224.DecodeConnectionConfirm(logger, response)
			if err != nil {
				t.Fatal(err)
			}
			if confirm.SelectedProto != x224.ProtocolHybrid {
				t.Fatalf("server selected %s, not NLA", x224.ProtocolName(confirm.SelectedProto))
			}
			// CredSSP runs over TLS 1.2. The certificate is the caller's
			// business in the real client; this test only exercises the
			// handshake.
			secured := tls.Client(conn, &tls.Config{
				InsecureSkipVerify: true,
				ServerName:         host,
				MinVersion:         tls.VersionTLS12,
				MaxVersion:         tls.VersionTLS12,
			})
			if err := secured.Handshake(); err != nil {
				t.Fatal(err)
			}
			authenticate := nla.Authenticate
			if raw {
				authenticate = nla.AuthenticateRawNTLM
			}
			if err := authenticate(secured, logger, host, "", user, password, nla.MaxCredSSPVersion); err != nil {
				t.Fatalf("CredSSP failed: %v", err)
			}
		})
	}
}
