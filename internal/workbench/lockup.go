package workbench

import (
	"bytes"
	"image/png"
	"sync"

	"github.com/vivaui/viva/gfx"
	"motty/assets"
)

const (
	lockupWidth  = 138
	lockupHeight = 36
)

// embeddedBitmap decodes one embedded picture to the premultiplied BGRA the
// canvas draws. The pictures are fixed and embedded, so a bad one is a build
// error.
func embeddedBitmap(data []byte, id uint64) func() gfx.Bitmap {
	return sync.OnceValue(func() gfx.Bitmap {
		picture, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			panic("workbench: the embedded logo is not a PNG: " + err.Error())
		}
		pix, width, height, err := scaleToBGRA(picture, 1<<14)
		if err != nil {
			panic("workbench: the embedded logo is unusable: " + err.Error())
		}
		return gfx.Bitmap{Pix: pix, Width: width, Height: height, Stride: width * 4, ID: id}
	})
}

var (
	lockupDark  = embeddedBitmap(assets.LockupDark, 1<<60|1)
	lockupLight = embeddedBitmap(assets.LockupLight, 1<<60|2)
)

// paintLockup draws the logo with the name beside it into the slot the sidebar
// header kept for it. Every build paints the same picture: a development build
// says which copy it is on the Dock icon (see buildinfo.DockBadge), not by
// renaming the product inside its own interface.
func (state *State) paintLockup(canvas *gfx.Canvas, bounds gfx.Rect) {
	bitmap := lockupLight()
	if state.pal.dark {
		bitmap = lockupDark()
	}
	canvas.DrawBitmap(bitmap, bounds)
}
