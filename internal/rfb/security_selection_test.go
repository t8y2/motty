package rfb

import (
	"fmt"
	"testing"
)

func TestSecuritySelectionPrefersSessionEncryptionAndRequiresAuthOnlyConsent(t *testing.T) {
	for _, test := range []struct {
		name            string
		offered         []byte
		password, allow bool
		want            byte
	}{
		{"all variants", []byte{2, 1, 6, 130, 19, 5, 129, 129}, true, true, 129},
		{"RA2 before auth only", []byte{130, 6, 5, 2, 1}, true, true, 5},
		{"TLS before auth only", []byte{6, 130, 19, 2, 1}, true, true, 19},
		{"256 auth only explicit", []byte{6, 2, 130, 1}, true, true, 130},
		{"128 auth only explicit", []byte{1, 6, 2}, true, true, 6},
		{"auth only rejects legacy fallback", []byte{6, 2, 1}, true, false, 0},
		{"256 auth only rejects legacy fallback", []byte{130, 1, 2}, false, false, 0},
		{"legacy password", []byte{1, 2}, true, false, 2},
		{"legacy none", []byte{1, 2}, false, false, 1},
		{"legacy password question", []byte{2}, false, false, 2},
		{"unknown", []byte{13, 18, 30}, true, true, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := chooseSecurity(test.offered, test.password, test.allow); got != test.want {
				t.Fatalf("selected type %d, want %d", got, test.want)
			}
		})
	}
}

func TestKnownRealVNCBannerNegotiatesStandardRFB(t *testing.T) {
	for _, security := range []byte{1, 2} {
		t.Run(fmt.Sprintf("type%d", security), func(t *testing.T) {
			client, err := connect(t, Config{Password: "legacy"}, func(s *server) {
				s.write([]byte("RFB 005.000\n"))
				if got := s.read(12); string(got) != "RFB 003.008\n" {
					t.Errorf("negotiated %q", got)
				}
				s.write([]byte{1, security})
				s.read(1)
				if security == 2 {
					s.write(make([]byte, 16))
					s.read(16)
				}
				finishRA2Peer(s)
			})
			if err != nil || client.Version != 8 || client.Security != int(security) {
				t.Fatalf("client=%+v error=%v", client, err)
			}
			wantMode, wantAuth := "None", "none"
			if security == 2 {
				wantMode, wantAuth = "VNC Authentication", "vnc-password"
			}
			if client.SecurityMode != wantMode || client.Authentication != wantAuth || client.Cipher != "" || client.TLSVersion != "" ||
				client.SecurityTypes != wantMode || client.SecuritySubtypes != "" || client.Encrypted {
				t.Fatalf("incorrect legacy security metadata: %+v", client)
			}
		})
	}
}
