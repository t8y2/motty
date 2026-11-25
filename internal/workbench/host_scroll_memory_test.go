package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
)

// The hosts page owns one scroll area, and another page takes its slot while the
// user works in a terminal. The area comes back at its top, so the page has to
// ask for the row the reader had reached: opening a host and coming back must not
// send them hunting down the list again.
func TestTheHostsListKeepsItsScrollWhenThePageComesBack(t *testing.T) {
	for _, opened := range []bool{false, true} {
		state := manyHostsState(t, 40)
		size := gfx.Size{Width: 1400, Height: 900}
		var canvas gfx.Canvas
		state.Paint(&canvas, size)
		for step := 0; step < 8; step++ {
			state.Scroll(gfx.ScrollEvent{DeltaY: 120, Precise: true, X: 600, Y: 400, Viewport: size})
			canvas = gfx.Canvas{}
			state.Paint(&canvas, size)
		}
		saved := state.hostsScroll
		if saved <= 0 {
			t.Fatalf("opened=%v: the list did not scroll: %v", opened, saved)
		}
		if opened {
			state.selectedHostID = state.hosts[10].ID
			openHost(state)
		} else {
			state.activeSection = sectionTerminal
		}
		canvas = gfx.Canvas{}
		state.Paint(&canvas, size)
		state.Click(hitHosts)
		canvas = gfx.Canvas{}
		state.Paint(&canvas, size)
		// The row the reader reached is put at the top of the list, so the offset
		// may land anywhere within that row of where it was.
		if difference := saved - state.hostsScroll; difference < -hostRowHeight || difference > hostRowHeight {
			t.Fatalf("opened=%v: the scroll came back at %v, want about %v", opened, state.hostsScroll, saved)
		}
		if _, visible := hostListHostHit(&canvas, state, state.hosts[0].ID); visible {
			t.Fatalf("opened=%v: the first host is in sight again (scroll %v)", opened, state.hostsScroll)
		}
		canvas = gfx.Canvas{}
		state.Paint(&canvas, size)
		if state.hostsScroll <= 0 {
			t.Fatalf("opened=%v: the next paint dropped the scroll to %v", opened, state.hostsScroll)
		}
	}
}
