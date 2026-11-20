package workbench

import (
	"fmt"
	"testing"

	"github.com/vivaui/viva/gfx"
)

func init() {
	extraPaintScenes = append(extraPaintScenes,
		plainScene("last-proxies-page", func(t *testing.T, s *State) {
			jump, target := s.hosts[0], s.hosts[1]
			s.batchAssociateProxies(jump.ID, []uint32{target.ID}, false, false, "")
			if len(s.proxies) > 0 {
				s.proxies[0].ViaShell = true
				s.refreshProxies()
			}
			s.activeSection = sectionProxies
			s.selectedProxyID = s.proxies[0].ID
		}),
		plainScene("last-proxy-dialog", func(t *testing.T, s *State) { s.openProxyDialog() }),
		plainScene("last-forward-dialog", func(t *testing.T, s *State) { s.openForwardDialog() }),
		plainScene("last-command-palette", func(t *testing.T, s *State) {
			for id := uint32(1); id <= 14; id++ {
				s.sessions = append(s.sessions, Session{ID: id, Title: fmt.Sprintf("Session %02d", id), restored: true})
			}
			s.openCommandPalette()
		}),
		inputNoticeScene("last-input-notice"),
	)
}

// inputNoticeScene is a window with one shell session whose locale notice is
// showing, so the notice's words and buttons are painted.
func inputNoticeScene(name string) paintScene {
	return paintScene{name: name, sizes: []gfx.Size{paintDesktop, paintSmall}, open: func(t *testing.T) (*State, func(*gfx.Canvas, gfx.Size)) {
		state, _, _, _ := liveSession(t)
		if len(state.sessions) == 0 || state.sessions[0].term == nil {
			t.Fatal("the fixture has no terminal session")
		}
		session := &state.sessions[0]
		session.term.commandReporting = true
		session.term.inputGuard.environment.Charset = "other"
		session.term.inputGuard.environment.Locale = "C"
		return state, func(c *gfx.Canvas, size gfx.Size) { state.Paint(c, size) }
	}}
}
