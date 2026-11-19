package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
)

// The local proxy dialog's main view, with "more" open, and the Docker view.
func init() {
	open := func(setup func(*State)) func(*testing.T) (*State, func(*gfx.Canvas, gfx.Size)) {
		return func(t *testing.T) (*State, func(*gfx.Canvas, gfx.Size)) {
			world, options := newSharingWorld(t)
			options.Invalidate = func() {}
			state := newTestState(t, options)
			world.connectFirst(t, state)
			state.Wake()
			session, ok := state.sessionByIDPointer(state.activeSessionID)
			if !ok {
				t.Fatal("connected session is missing")
			}
			state.openLocalProxy(session)
			setup(state)
			return state, func(c *gfx.Canvas, size gfx.Size) { state.Paint(c, size) }
		}
	}
	both := []gfx.Size{paintDesktop, paintSmall}
	extraPaintScenes = append(extraPaintScenes,
		paintScene{name: "local-proxy", sizes: both, open: open(func(*State) {})},
		paintScene{name: "local-proxy-more", sizes: both, open: open(func(s *State) { s.localProxyUI.more = true })},
		paintScene{name: "local-proxy-docker", sizes: both, open: open(func(s *State) { s.localProxyUI.docker = true })},
	)
}
