package workbench

import (
	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// The window bar every standalone content window declares for itself. The
// platform keeps the strip's two ends for its window buttons (app.TitlebarInsets:
// macOS's traffic lights at the leading end, Viva's controls at the trailing
// end on Windows and Linux), so the text starts after the leading end and stops
// before the trailing one.
const (
	titlebarTextPad = float32(16) // room between the strip's text and the window edge
	titlebarGap     = float32(14) // room between the window's name and its context
)

// declareWindowTitlebar declares a custom-titlebar window's strip: the window's
// name at the leading end, the muted context it names after it, and the rest of
// the strip as the place the window is dragged from. Every standalone content
// window uses it, so all of them are titled, moved and kept clear of the
// platform's window buttons alike.
func declareWindowTitlebar(c ui.C, pal *palette, title, subtitle string) {
	leading, trailing := app.TitlebarInsets()
	fontSize := c.Theme().Metrics.FontSizeS
	ui.Row(c).Height(TitlebarHeight).Shrink(0).
		PadSides(0, trailing+titlebarTextPad, 0, leading+titlebarTextPad).
		Items(ui.Center).MovesWindow().Children(func() {
		ui.Text(c, title).FontSize(fontSize).Weight(gfx.WeightMedium).Color(pal.text).
			Ellipsis().Shrink(1)
		if subtitle != "" {
			ui.Text(c, subtitle).FontSize(fontSize).Color(pal.muted).
				MarginLeft(titlebarGap).Ellipsis().Grow(1)
		}
	})
}

// windowClearColor is the color a custom-titlebar window is cleared with: the
// surface its page fills, so the edges and rounded corners the system exposes
// before a repaint match the content instead of the framework's default.
func windowClearColor(color gfx.Color) app.Color {
	return app.Color{Red: float64(color.Red), Green: float64(color.Green), Blue: float64(color.Blue), Alpha: 1}
}
