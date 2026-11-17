package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
)

// The font picker's search field, empty and with a query, once the list has loaded.
func init() {
	open := func(query string) func(*testing.T) (*State, func(*gfx.Canvas, gfx.Size)) {
		return func(t *testing.T) (*State, func(*gfx.Canvas, gfx.Size)) {
			state := newTestState(t, fontOptions(t, testFonts))
			state.openFontPicker(false)
			waitForPicker(t, state, func() bool { return !state.fonts.loading })
			if query != "" {
				state.fonts.search.Insert(query)
			}
			return state, func(c *gfx.Canvas, size gfx.Size) { state.Paint(c, size) }
		}
	}
	extraPaintScenes = append(extraPaintScenes,
		paintScene{name: "font-picker", sizes: []gfx.Size{paintDesktop, paintSmall}, open: open("")},
		paintScene{name: "font-picker-query", sizes: []gfx.Size{paintDesktop, paintSmall}, open: open("io")},
	)
}
