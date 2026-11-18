package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
)

// The monitor panel's own layouts, as separate scenes: its docked and drawer
// forms, and the empty state of each, so its bands, margins, divider hit and
// placeholder stay where the hand-painted panel had them.
func init() {
	docked := paintScene{name: "monitor-docked", sizes: []gfx.Size{{Width: 1600, Height: 900}, {Width: 1400, Height: 800}}, open: func(t *testing.T) (*State, func(*gfx.Canvas, gfx.Size)) {
		state, _ := monitorFixture(t)
		return state, func(c *gfx.Canvas, size gfx.Size) { state.Paint(c, size) }
	}}
	drawer := paintScene{name: "monitor-drawer", sizes: []gfx.Size{{Width: 760, Height: 800}}, open: func(t *testing.T) (*State, func(*gfx.Canvas, gfx.Size)) {
		state, _ := monitorFixture(t)
		return state, func(c *gfx.Canvas, size gfx.Size) { state.Paint(c, size) }
	}}
	emptyDocked := paintScene{name: "monitor-docked-empty", sizes: []gfx.Size{{Width: 1600, Height: 900}}, open: func(t *testing.T) (*State, func(*gfx.Canvas, gfx.Size)) {
		state, session := monitorFixture(t)
		session.stats = nil
		return state, func(c *gfx.Canvas, size gfx.Size) { state.Paint(c, size) }
	}}
	emptyDrawer := paintScene{name: "monitor-drawer-empty", sizes: []gfx.Size{{Width: 760, Height: 800}}, open: func(t *testing.T) (*State, func(*gfx.Canvas, gfx.Size)) {
		state, session := monitorFixture(t)
		session.stats = nil
		return state, func(c *gfx.Canvas, size gfx.Size) { state.Paint(c, size) }
	}}
	extraPaintScenes = append(extraPaintScenes, docked, drawer, emptyDocked, emptyDrawer)
}
