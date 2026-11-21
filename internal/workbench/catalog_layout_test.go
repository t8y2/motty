package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
)

// Issue #1059: the forwards and proxies pages share the same catalog skeleton
// (header toolbar, body, footer band). Their header buttons, empty state and
// footer must land at the same positions so switching between the two tabs
// does not shift the content up or down.
func TestForwardsAndProxiesShareHeaderAndFooterGeometry(t *testing.T) {
	state := newTestState(t, testOptions(t))
	size := gfx.Size{Width: 1200, Height: 800}

	// Two frames: the first establishes the layout, the second is measured.
	paint := func() *gfx.Canvas {
		var canvas gfx.Canvas
		state.Paint(&canvas, size)
		canvas = gfx.Canvas{}
		state.Paint(&canvas, size)
		return &canvas
	}
	buttonBounds := func(canvas *gfx.Canvas, id gfx.ID) gfx.Rect {
		for _, h := range canvas.Hits() {
			if h.ID == id {
				return h.Bounds
			}
		}
		t.Fatalf("toolbar button %v was not painted", id)
		return gfx.Rect{}
	}
	assertToolbarAligned := func(forward, proxy gfx.Rect) {
		t.Helper()
		if forward.Y != proxy.Y || forward.Height != proxy.Height {
			t.Fatalf("header toolbar jumps between tabs: forwards=%+v proxies=%+v", forward, proxy)
		}
		if forward.X+forward.Width != proxy.X+proxy.Width {
			t.Fatalf("header toolbar right edge jumps between tabs: forwards=%+v proxies=%+v", forward, proxy)
		}
	}

	// Empty pages: the toolbar and the whole empty body must coincide.
	state.activeSection = sectionForwards
	forwardCanvas := paint()
	forwardAdd := buttonBounds(forwardCanvas, hitForwardAdd)
	forwardEmpty := state.sectionBodies.forwards.empty.Rect()

	state.activeSection = sectionProxies
	proxyCanvas := paint()
	proxyAdd := buttonBounds(proxyCanvas, hitProxyAdd)
	proxyEmpty := state.sectionBodies.proxies.empty.Rect()

	assertToolbarAligned(forwardAdd, proxyAdd)
	if forwardEmpty != proxyEmpty {
		t.Fatalf("empty body jumps between tabs: forwards=%+v proxies=%+v", forwardEmpty, proxyEmpty)
	}

	// Populated pages: the toolbar stays put and the list bodies end at the
	// same footer line. (The proxies list may start lower when its hub filter
	// tabs are shown; that row is content, not a layout shift.)
	addForwardRule(t, state, "Web", freePort(t), "web.internal", 8080)
	state.proxies = []proxyRecord{{ID: 1, TargetID: state.hosts[1].ID, JumpID: state.hosts[0].ID}}
	state.nextProxyID = 2
	state.refreshProxies()

	state.activeSection = sectionForwards
	forwardCanvas = paint()
	forwardAdd = buttonBounds(forwardCanvas, hitForwardAdd)
	forwardTable := state.sectionBodies.forwards.tableBox.Rect()

	state.activeSection = sectionProxies
	proxyCanvas = paint()
	proxyAdd = buttonBounds(proxyCanvas, hitProxyAdd)
	proxyBody := state.sectionBodies.proxies.listBox.Rect()

	assertToolbarAligned(forwardAdd, proxyAdd)
	if forwardTable.Width <= 0 || proxyBody.Width <= 0 {
		t.Fatalf("list bodies were not laid out: forwards=%+v proxies=%+v", forwardTable, proxyBody)
	}
	if forwardTable.X != proxyBody.X || forwardTable.Width != proxyBody.Width {
		t.Fatalf("list body edges jump between tabs: forwards=%+v proxies=%+v", forwardTable, proxyBody)
	}
	if forwardTable.Y+forwardTable.Height != proxyBody.Y+proxyBody.Height {
		t.Fatalf("footer line jumps between tabs: forwards=%+v proxies=%+v", forwardTable, proxyBody)
	}
}
