package workbench

import (
	"time"

	"github.com/vivaui/viva/base"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/style"
	"github.com/vivaui/viva/ui"
)

func init() {
	installSelectPresenceFix()
}

// installSelectPresenceFix closes the presence group opened by ui.Select when
// Viva's select painter leaves components.BeginSelect without a matching End().
// Without closing the group on the select's popup, an enclosing Dialog, Drawer
// or Popover closes the select's presence in place of its own, applying the
// dropdown's entrance/exit transform and snapshot replay to every control
// painted after the select.
//
// Probing once on an off-screen canvas ensures ui's select hook is installed
// first and leaves a future Viva revision that already closes its own presence
// untouched.
func installSelectPresenceFix() {
	if !selectPainterLeavesPresenceOpen() {
		return
	}
	prev := base.PaintSelect
	if prev == nil {
		return
	}
	base.PaintSelect = func(f *base.Frame, n *base.Node) {
		opened := selectOpensPresence(f, n)
		prev(f, n)
		if opened {
			f.Surface.EndPresence()
		}
	}
}

func selectPainterLeavesPresenceOpen() bool {
	var now time.Duration
	canvas := &gfx.Canvas{}
	canvas.SetClock(func() time.Duration { return now })
	canvas.SetMeasurer(func(value string, size, _ float32) float32 {
		return float32(len(value)) * size / 2
	})
	theme := gfx.LightTheme()
	choices := []int{0}
	choice := 0
	labelOf := func(int) string { return "A" }
	view := func(c ui.C) {
		ui.Select(c, &choice, choices, labelOf).Size(120, 32)
	}
	page := ui.NewPage(ui.PageConfig{Theme: &theme, View: view})

	outerID := gfx.Item(1)
	outerPresence := gfx.Presence{Motion: gfx.Motion{Duration: 100 * time.Millisecond}, Fade: true}
	bounds := gfx.Rect{Width: 320, Height: 240}

	canvas.BeginFrame()
	canvas.BeginPresence(outerID, 0, outerPresence, gfx.Point{})
	page.Draw(canvas, bounds, view)
	canvas.EndPresence()
	canvas.EndFrame()

	var triggerID gfx.ID
	for _, hit := range canvas.Hits() {
		if hit.Role == gfx.RoleCombobox {
			triggerID = hit.ID
			break
		}
	}
	if triggerID.IsZero() {
		return false
	}

	// Settle the outer presence, then open the select in an event pass.
	now = time.Second
	canvas.Reset()
	canvas.BeginEventPass(triggerID, gfx.ClickInfo{})
	page.Draw(canvas, bounds, view)
	canvas.EndEventPass()

	sentinel := gfx.Rect{X: 280, Y: 200, Width: 10, Height: 10}
	canvas.Reset()
	canvas.BeginFrame()
	canvas.BeginPresence(outerID, 0, outerPresence, gfx.Point{})
	page.Draw(canvas, bounds, view)
	canvas.Fill(sentinel, gfx.Color{Red: 1, Alpha: 1})
	canvas.EndPresence()
	canvas.EndFrame()

	rects := canvas.Rectangles()
	return len(rects) > 0 && rects[len(rects)-1].Color.Alpha < 1
}

// selectOpensPresence mirrors the exact conditions under which paintSelect
// causes components.BeginSelect to call Canvas.BeginPresence.
func selectOpensPresence(f *base.Frame, n *base.Node) bool {
	if f == nil || f.Surface == nil || n == nil {
		return false
	}
	selects := base.WidgetsOf(f).GetSelects()
	if int(n.Aux) < 0 || int(n.Aux) >= len(selects) {
		return false
	}
	popup := selects[n.Aux].GetPopup()
	if popup < 0 || int(popup) >= len(f.Nodes) {
		return false
	}
	height := n.Pref[base.AxisY]
	if n.Size[base.AxisY].Kind != base.DimAuto || n.Flags&base.FlagThemed != 0 {
		height = n.Rect.Height
	}
	if height <= 0 {
		size := style.SizeDefault
		switch {
		case n.ButtonSize == components.ButtonSizeXSmall:
			size = style.SizeXSmall
		case n.Flags&base.FlagSmall != 0:
			size = style.SizeSmall
		}
		height, _ = style.SelectGeometry(f.Palette.Metrics, size)
	}
	return n.Rect.Width > 0 && height > 0 && f.Nodes[popup].Rect.Height > 0
}
