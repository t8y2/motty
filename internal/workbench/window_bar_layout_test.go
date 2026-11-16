package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
)

// However many tabs are open, the window bar's own buttons stay inside the window
// and apart: the tabs shrink and then scroll, they do not push the new-tab button
// or the buttons on the right out of reach.
func TestWindowBarButtonsStayInsideANarrowWindowWithManyTabs(t *testing.T) {
	state := newTestState(t, testOptions(t))
	for index := 0; index < 12; index++ {
		state.newHostSession(state.hosts[index%len(state.hosts)])
	}
	const width = 640
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: width, Height: 520})

	var bounds []gfx.Rect
	for _, id := range []gfx.ID{hitHome, hitNewTab, hitWindowLocalTerminal, hitCommandPalette, hitTheme, hitGitHub, hitWindowSettings} {
		hit, ok := hitByID(&canvas, id)
		if !ok {
			t.Fatalf("button %d is not in the window bar", id)
		}
		if hit.Bounds.X < 0 || hit.Bounds.X+hit.Bounds.Width > width {
			t.Errorf("%q spans %v to %v, outside the %d wide window", hit.Label, hit.Bounds.X, hit.Bounds.X+hit.Bounds.Width, width)
		}
		for _, other := range bounds {
			if hit.Bounds.X < other.X+other.Width && other.X < hit.Bounds.X+hit.Bounds.Width {
				t.Errorf("%q at %v overlaps another button at %v", hit.Label, hit.Bounds, other)
			}
		}
		bounds = append(bounds, hit.Bounds)
	}
	newTab, _ := hitByID(&canvas, hitNewTab)
	for _, tab := range state.workspaceTabs {
		if hit, ok := hitByID(&canvas, hitTabList.At(int(tab.ID))); ok && hit.Bounds.X < newTab.Bounds.X && hit.Bounds.X+hit.Bounds.Width > newTab.Bounds.X {
			t.Errorf("tab %q runs under the new-tab button", hit.Label)
		}
	}
}

// A menu opened at the edge of the window is pushed back inside it instead of
// hanging out of it.
func TestMenusOpenedAtTheEdgeOfTheWindowStayInsideIt(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.newHostSession(state.hosts[0])
	size := gfx.Size{Width: 900, Height: 600}
	edge := gfx.Point{X: size.Width - 2, Y: size.Height - 2}

	state.tabMenu = tabMenu{open: true, workspace: state.workspaceTabs[0].ID, at: edge}
	var canvas gfx.Canvas
	state.Paint(&canvas, size)
	rename, ok := hitByID(&canvas, hitTabMenuRename)
	if !ok {
		t.Fatal("the tab menu was not painted")
	}
	if rename.Bounds.X+rename.Bounds.Width > size.Width || rename.Bounds.Y+rename.Bounds.Height > size.Height {
		t.Errorf("the tab menu's first row %v hangs out of the %vx%v window", rename.Bounds, size.Width, size.Height)
	}
	state.tabMenu.open = false

	state.activeSection = sectionHosts
	state.hostMenu = hostMenu{open: true, host: state.hosts[0].ID, at: edge}
	canvas.Reset()
	state.Paint(&canvas, size)
	remove, ok := hitByID(&canvas, hitHostMenuDelete)
	if !ok {
		t.Fatal("the host menu was not painted")
	}
	if remove.Bounds.X+remove.Bounds.Width > size.Width || remove.Bounds.Y+remove.Bounds.Height > size.Height {
		t.Errorf("the host menu's last row %v hangs out of the %vx%v window", remove.Bounds, size.Width, size.Height)
	}
}
