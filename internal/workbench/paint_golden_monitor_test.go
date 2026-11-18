package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
)

// The monitor dashboard is a page of its own: its header controls, its
// hand-painted host cards and its first-run state. The scenes here pin what it
// paints, so a change to the declaration is seen as pixel or draw-order drift
// rather than silently rewiring the page.
func init() {
	extraPaintScenes = append(extraPaintScenes,
		monitorPaintScene("monitor-dashboard", func(t *testing.T) *State {
			return dashboardNativeFixture(t)
		}),
		monitorPaintScene("monitor-dashboard-empty", func(t *testing.T) *State {
			state := newTestState(t, testOptions(t))
			state.activeSection = sectionDashboard
			return state
		}),
	)
}

func monitorPaintScene(name string, build func(*testing.T) *State) paintScene {
	return paintScene{name: name, sizes: []gfx.Size{paintDesktop}, open: func(t *testing.T) (*State, func(*gfx.Canvas, gfx.Size)) {
		state := build(t)
		return state, func(c *gfx.Canvas, size gfx.Size) { state.Paint(c, size) }
	}}
}
