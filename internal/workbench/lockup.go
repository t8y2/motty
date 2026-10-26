package workbench

import (
	"bytes"
	"image/png"
	"sync"

	"github.com/vivaui/viva/ui"
	"motty/assets"
)

const (
	lockupWidth  = 138
	lockupHeight = 36
)

// lockupBitmap decodes one embedded lockup picture to the premultiplied BGRA the
// canvas draws. The pictures are fixed and embedded, so a bad one is a build error.
func lockupBitmap(data []byte, id uint64) func() ui.Bitmap {
	return sync.OnceValue(func() ui.Bitmap {
		picture, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			panic("workbench: the embedded logo is not a PNG: " + err.Error())
		}
		pix, width, height, err := scaleToBGRA(picture, 1<<14)
		if err != nil {
			panic("workbench: the embedded logo is unusable: " + err.Error())
		}
		return ui.Bitmap{Pix: pix, Width: width, Height: height, Stride: width * 4, ID: id}
	})
}

var (
	lockupDark  = lockupBitmap(assets.LockupDark, 1<<61|1)
	lockupLight = lockupBitmap(assets.LockupLight, 1<<61|2)
)

// paintLockup draws the logo with the name beside it into the slot the sidebar
// header kept for it.
func (state *State) paintLockup(canvas *ui.Canvas, bounds ui.Rect) {
	bitmap := lockupLight()
	if state.pal.dark {
		bitmap = lockupDark()
	}
	canvas.DrawBitmap(bitmap, bounds)
}
