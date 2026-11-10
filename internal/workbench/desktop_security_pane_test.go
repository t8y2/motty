package workbench

import (
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
	"motty/internal/desktop"
	"motty/internal/sshconn"
	layoutcore "motty/internal/workspace"
)

func TestVNCSecurityActionsOpenTheClickedPaneDetails(t *testing.T) {
	for _, action := range []struct {
		name string
		id   gfx.ID
	}{
		{"status", hitDesktopSecurityStatus},
		{"toolbar", hitDesktopSecurity},
	} {
		t.Run(action.name, func(t *testing.T) {
			state := newTestState(t, testOptions(t))
			openHost(state)
			openHostInSplit(t, state, 2, layoutcore.Horizontal)
			first, second := &state.sessions[0], &state.sessions[1]
			for index, session := range []*Session{first, second} {
				session.term.live = true
				session.phase = sshconn.PhaseConnected
				session.host.Protocol = protocolVNC
				session.host.Address = []string{"first-vnc.example", "second-vnc.example"}[index]
				session.desk = newDesktopView(&securityDesktop{
					Session: newFollowingDesktop(800, 600, false),
					info: desktop.Info{
						Security:          []string{"RA2", "X509None"}[index],
						ProtocolEncrypted: true,
					},
				}, false)
			}
			if !state.ActivatePane(first.ID) {
				t.Fatal("activate the first pane")
			}
			size := gfx.Size{Width: 1800, Height: 900}
			state.Resize(size)
			driver := paneDriver(t, state, size)

			for _, session := range []*Session{first, second} {
				hit := hitOf(t, driver, pagedIdentity(state, session, action.id))
				bounds := paneBoundsOf(t, state, size, session)
				if !rectInside(hit.Bounds, bounds) {
					t.Fatalf("session %d: security action %+v is outside its pane %+v", session.ID, hit.Bounds, bounds)
				}
			}
			if state.activeSessionID != first.ID {
				t.Fatal("the first pane was not active before clicking the second")
			}
			hit := hitOf(t, driver, pagedIdentity(state, second, action.id))
			want := state.vncSecurityDetails(second)
			if err := driver.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
				t.Fatal(err)
			}
			if state.activeSessionID != second.ID || state.modal != modalDesktopSecurity {
				t.Fatalf("security action opened active session %d and modal %v, want session %d and its details", state.activeSessionID, state.modal, second.ID)
			}
			if state.desktopSecurity.text != want || strings.Contains(state.desktopSecurity.text, first.host.Address) {
				t.Fatalf("the second pane's security action showed the wrong connection: %q", state.desktopSecurity.text)
			}
		})
	}
}
