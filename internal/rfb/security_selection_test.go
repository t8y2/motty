package rfb

import (
	"fmt"
	"testing"
)

func TestSecuritySelectionPrefersSessionEncryptionAndRequiresAuthOnlyConsent(t *testing.T) {
	for _, test := range []struct {
		name                     string
		offered                  []byte
		password, account, allow bool
		want                     byte
	}{
		{"all variants", []byte{2, 1, 6, 30, 130, 19, 5, 129, 129}, true, true, true, 129},
		{"RA2 before auth only", []byte{130, 6, 5, 2, 1}, true, false, true, 5},
		{"RA2 before ARD", []byte{30, 5, 2, 1}, true, true, false, 5},
		{"TLS before auth only", []byte{6, 130, 19, 2, 1}, true, false, true, 19},
		{"TLS before ARD", []byte{30, 19, 2, 1}, true, true, false, 19},
		{"256 auth only explicit", []byte{6, 2, 130, 1}, true, false, true, 130},
		{"128 auth only explicit", []byte{1, 6, 2}, true, false, true, 6},
		{"auth only before ARD when permitted", []byte{30, 6, 2}, true, true, true, 6},
		{"auth only rejects legacy fallback", []byte{6, 2, 1}, true, false, false, 0},
		{"256 auth only rejects legacy fallback", []byte{130, 1, 2}, false, false, false, 0},
		{"auth only rejects ARD fallback", []byte{30, 130, 2}, true, true, false, 0},
		{"ARD with an account", []byte{2, 1, 30}, true, true, false, 30},
		{"ARD without an account falls back to the password", []byte{2, 1, 30}, true, false, false, 2},
		{"ARD only", []byte{30, 33, 36, 35}, false, true, false, 30},
		{"ARD only without an account", []byte{30, 33, 36, 35}, true, false, false, 30},
		{"legacy password", []byte{1, 2}, true, false, false, 2},
		{"legacy none", []byte{1, 2}, false, false, false, 1},
		{"legacy password question", []byte{2}, false, false, false, 2},
		{"unknown", []byte{13, 18, 33}, true, true, true, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := chooseSecurity(test.offered, test.password, test.account, test.allow); got != test.want {
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
