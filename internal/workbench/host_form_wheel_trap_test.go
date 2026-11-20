package workbench

import (
	"fmt"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
)

// The host dialog's body lies over the host list. A wheel over the body moves
// neither the list behind it nor anything else behind the dialog, whether or not
// the body itself has room to scroll. The dialog's focus trap decides it.
func TestWheelOverTheNewHostFormBodyKeepsTheHostListStill(t *testing.T) {
	state := newTestState(t, testOptions(t))
	group := state.hosts[0].Group
	for i := 0; i < 60; i++ {
		state.hosts = append(state.hosts, Host{ID: 1000 + uint32(i), Label: fmt.Sprintf("host-%d", i), Address: "192.0.2.1", Port: 22, Group: group})
	}
	state.syncVisible()
	state.WindowConfig(app.Config{OnScroll: state.Scroll})
	paintedTexts(state)
	clickHostAction(state, hitAddHost)
	paintedTexts(state)
	body := state.editor.form.viewport
	if body.Width <= 0 || body.Height <= 0 {
		t.Fatalf("the form body did not lay out: %+v", body)
	}
	before := state.hostList.scroll.Offset
	for _, p := range []gfx.Point{
		{X: body.X + 4, Y: body.Y + 4},
		{X: body.X + body.Width/2, Y: body.Y + body.Height/2},
		{X: body.X + body.Width - 4, Y: body.Y + body.Height - 4},
	} {
		for _, dy := range []float32{-120, 120} {
			if result := state.scrollWindow(gfx.ScrollEvent{X: p.X, Y: p.Y, DeltaY: dy}); !result.Handled {
				t.Fatalf("the wheel over the form body at %v was not handled", p)
			}
			if got := state.hostList.scroll.Offset; got != before {
				t.Fatalf("the wheel over the form body at %v moved the host list: %v -> %v", p, before, got)
			}
		}
	}
}
