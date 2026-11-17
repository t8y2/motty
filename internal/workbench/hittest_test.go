package workbench

import "github.com/vivaui/viva/gfx"

// hitText is the text geometry of a hit a test just read from canvas.
func hitText(canvas *gfx.Canvas, hit *gfx.Hit) gfx.HitText {
	text, _ := canvas.HitText(hit)
	return text
}
