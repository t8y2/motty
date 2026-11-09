package workbench

import (
	"fmt"
	"testing"

	"github.com/vivaui/viva/gfx"
	"motty/internal/sshconn"
)

func TestChangedDesktopIdentityPaintDoesNotAllocate(t *testing.T) {
	if raceDetector {
		t.Skip("allocation counts are not comparable under the race detector")
	}
	for _, appearance := range []appearance{appearanceDark, appearanceLight} {
		for _, chinese := range []bool{false, true} {
			for _, protocol := range []hostProtocol{protocolRDP, protocolVNC} {
				t.Run(fmt.Sprintf("%v/chinese=%t/protocol=%v", appearance, chinese, protocol), func(t *testing.T) {
					world, options := newWorld(t)
					state := newTestState(t, options)
					state.setAppearance(appearance)
					if chinese {
						useChinese(t, state)
					}
					conn := connect(t, state, world)
					state.sessions[0].host = Host{Address: "desktop.example.com", Port: 5900, Protocol: protocol}
					conn.set(func(status *sshconn.Status) {
						status.Phase = sshconn.PhaseFailed
						status.Err = sshconn.ErrHostKeyChanged
						status.HostKey = sshconn.HostKeyInfo{
							Address: "desktop.example.com:5900", Type: vncServerKeyType,
							Fingerprint: "SHA256:new", Known: []string{"SHA256:old"},
						}
						if protocol == protocolRDP {
							status.Phase, status.Err = sshconn.PhaseHostKeyPrompt, nil
							status.HostKey.Type = rdpCertificateType
						}
					})
					state.Wake()
					var canvas gfx.Canvas
					size := gfx.Size{Width: 1400, Height: 900}
					state.Paint(&canvas, size)
					forgetID := hitConnForget
					if protocol == protocolRDP {
						forgetID = hitHostKeyForget
					}
					if _, ok := hitByID(&canvas, forgetID); !ok {
						t.Fatal("changed identity does not expose its forget action")
					}
					if allocations := testing.AllocsPerRun(50, func() {
						canvas.Reset()
						state.Paint(&canvas, size)
					}); allocations != 0 {
						t.Fatalf("changed identity paint allocated %v times", allocations)
					}
				})
			}
		}
	}
}

func TestForgetServerActionPreservesTypedEndpointComparison(t *testing.T) {
	for _, test := range []struct {
		address, keyAddress string
		want                bool
	}{
		{"pi.example.com", "pi.example.com:5900", true},
		{"pi.example.com", "another.example.com:5900", false},
		{"pi.example.com", "pi.example.com:5901", false},
		{"pi.example.com", "pi.example.com:05900", false},
		{"pi.example.com", "pi.example.com:+5900", false},
		{"2001:db8::1", "[2001:db8::1]:5900", true},
		{"fe80::1%eth0", "[fe80::1%eth0]:5900", true},
		{"2001:db8::1", "2001:db8::1:5900", false},
	} {
		t.Run(test.keyAddress, func(t *testing.T) {
			session := &Session{
				host: Host{Protocol: protocolVNC, Address: test.address, Port: 5900},
				hostKey: sshconn.HostKeyInfo{
					Address: test.keyAddress, Type: vncServerKeyType, Known: []string{"saved identity"},
				},
			}
			if got := canForgetServerIdentity(session); got != test.want {
				t.Fatalf("canForgetServerIdentity = %t, want %t", got, test.want)
			}
			session.role = sshconn.EndpointProxyJump
			if canForgetServerIdentity(session) {
				t.Fatal("gateway can forget desktop trust")
			}
		})
	}
}
