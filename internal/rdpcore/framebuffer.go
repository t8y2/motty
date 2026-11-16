package rdp

// MaxDesktopSize bounds one side of a desktop and of a surface. RDP carries
// sizes in 16 bits, so a server can name 65535; a client only has to hold what
// a display can show, and 7680 is the 8K width (7680x4320) the frame buffer is
// built for. Callers that ask for a larger desktop, and servers that send one,
// are held to this.
const MaxDesktopSize = 7680

// Framebuffer holds the client-side screen buffer in bottom-up RGBA 32bpp format.
type Framebuffer struct {
	Width   int
	Height  int
	Stride  int          // bytes per row (Width * 4)
	Pixels  []byte       // bottom-up RGBA
	Palette [256][3]byte // 8bpp color palette (R, G, B)
}

// NewFramebuffer allocates a framebuffer of the given dimensions.
func NewFramebuffer(w, h int) *Framebuffer {
	stride := w * 4
	fb := &Framebuffer{
		Width:  w,
		Height: h,
		Stride: stride,
		Pixels: make([]byte, stride*h),
	}
	fb.fillAlpha()
	return fb
}

// Resize reallocates the framebuffer for new dimensions.
// Previous contents are discarded (server redraws after reactivation).
func (fb *Framebuffer) Resize(w, h int) {
	fb.Width = w
	fb.Height = h
	fb.Stride = w * 4
	need := fb.Stride * h
	if cap(fb.Pixels) >= need {
		fb.Pixels = fb.Pixels[:need]
		clear(fb.Pixels)
	} else {
		fb.Pixels = make([]byte, need)
	}
	fb.fillAlpha()
}

// fillAlpha sets the alpha byte of every pixel to 0xFF.
// The framebuffer is always fully opaque; stale alpha=0 from allocation
// or clear causes transparency artifacts when read back for the display.
func (fb *Framebuffer) fillAlpha() {
	for i := 3; i < len(fb.Pixels); i += 4 {
		fb.Pixels[i] = 0xFF
	}
}

// clipRect clips a rectangle to framebuffer bounds. Returns clipped x, y, w, h
// and the src byte offset adjustment for left/top clipping.
// If the result is empty, w or h will be <= 0.
func (fb *Framebuffer) clipRect(x, y, w, h int) (int, int, int, int) {
	if x < 0 {
		w += x
		x = 0
	}
	if y < 0 {
		h += y
		y = 0
	}
	if x+w > fb.Width {
		w = fb.Width - x
	}
	if y+h > fb.Height {
		h = fb.Height - y
	}
	return x, y, w, h
}

// WriteRect copies bottom-up RGBA 32bpp src data into the framebuffer at (x, y).
// src contains h rows of w*4 bytes each, bottom-up (same as framebuffer).
// Out-of-bounds pixels are clipped.
func (fb *Framebuffer) WriteRect(x, y, w, h int, src []byte) {
	srcStride := w * 4
	if len(src) < srcStride*h {
		return
	}

	x, y, w, h = fb.clipRect(x, y, w, h)
	if w <= 0 || h <= 0 {
		return
	}

	// Both src and framebuffer are bottom-up.
	// After clipping, all rows are in-range — no per-row checks needed.
	// fbBaseRow = fb.Height - y - h (row index for r=0)
	copyBytes := w * 4
	fbBaseRow := fb.Height - y - h
	fbBaseOff := fbBaseRow*fb.Stride + x*4
	for r := 0; r < h; r++ {
		fbOff := fbBaseOff + r*fb.Stride
		srcOff := r * srcStride
		copy(fb.Pixels[fbOff:fbOff+copyBytes], src[srcOff:srcOff+copyBytes])
	}
}

// BlendRect copies bottom-up RGBA 32bpp src data into the framebuffer at (x, y),
// skipping fully transparent pixels (alpha == 0). Use this for text rendering
// where background pixels should remain unchanged.
func (fb *Framebuffer) BlendRect(x, y, w, h int, src []byte) {
	srcStride := w * 4
	if len(src) < srcStride*h {
		return
	}

	x, y, w, h = fb.clipRect(x, y, w, h)
	if w <= 0 || h <= 0 {
		return
	}

	fbBaseRow := fb.Height - y - h
	fbBaseOff := fbBaseRow*fb.Stride + x*4
	for r := 0; r < h; r++ {
		fbOff := fbBaseOff + r*fb.Stride
		srcOff := r * srcStride
		for px := 0; px < w; px++ {
			si := srcOff + px*4
			if src[si+3] == 0 {
				continue // skip transparent pixels
			}
			di := fbOff + px*4
			fb.Pixels[di] = src[si]
			fb.Pixels[di+1] = src[si+1]
			fb.Pixels[di+2] = src[si+2]
			fb.Pixels[di+3] = src[si+3]
		}
	}
}

// WriteRectBpp copies bottom-up src data into the framebuffer at (x, y),
// performing inline BPP conversion to RGBA. srcW is the source data width (stride
// in pixels); it may be larger than w when the bitmap has alignment padding
// (MS-RDPBCGR 2.2.9.1.1.3.1.2.2). Iterates cx×cy pixels using the full
// bitmap width as the source stride.
func (fb *Framebuffer) WriteRectBpp(x, y, w, h, bpp, srcW int, src []byte) {
	bytesPP := bppToBytes(bpp)
	if bytesPP == 0 {
		return
	}
	if srcW < w {
		srcW = w
	}
	srcStride := srcW * bytesPP

	x, y, w, h = fb.clipRect(x, y, w, h)
	if w <= 0 || h <= 0 {
		return
	}

	// Validate total src size once upfront to avoid per-pixel checks
	srcNeed := (h-1)*srcStride + w*bytesPP
	if srcNeed > len(src) {
		return
	}

	fbBaseRow := fb.Height - y - h
	fbBaseOff := fbBaseRow*fb.Stride + x*4

	switch bpp {
	case 32:
		copyBytes := w * 4
		for r := 0; r < h; r++ {
			fbOff := fbBaseOff + r*fb.Stride
			srcOff := r * srcStride
			copy(fb.Pixels[fbOff:fbOff+copyBytes], src[srcOff:srcOff+copyBytes])
		}
	case 24:
		for r := 0; r < h; r++ {
			di := fbBaseOff + r*fb.Stride
			si := r * srcStride
			for px := 0; px < w; px++ {
				fb.Pixels[di] = src[si+2]   // R
				fb.Pixels[di+1] = src[si+1] // G
				fb.Pixels[di+2] = src[si]   // B
				fb.Pixels[di+3] = 0xFF
				si += 3
				di += 4
			}
		}
	case 16:
		for r := 0; r < h; r++ {
			di := fbBaseOff + r*fb.Stride
			si := r * srcStride
			for px := 0; px < w; px++ {
				v := uint16(src[si]) | uint16(src[si+1])<<8
				rv := uint8((v >> 11) & 0x1F)
				g := uint8((v >> 5) & 0x3F)
				b := uint8(v & 0x1F)
				fb.Pixels[di] = (rv << 3) | (rv >> 2)
				fb.Pixels[di+1] = (g << 2) | (g >> 4)
				fb.Pixels[di+2] = (b << 3) | (b >> 2)
				fb.Pixels[di+3] = 0xFF
				si += 2
				di += 4
			}
		}
	case 15:
		for r := 0; r < h; r++ {
			di := fbBaseOff + r*fb.Stride
			si := r * srcStride
			for px := 0; px < w; px++ {
				v := uint16(src[si]) | uint16(src[si+1])<<8
				rv := uint8((v >> 10) & 0x1F)
				g := uint8((v >> 5) & 0x1F)
				b := uint8(v & 0x1F)
				fb.Pixels[di] = (rv << 3) | (rv >> 2)
				fb.Pixels[di+1] = (g << 3) | (g >> 2)
				fb.Pixels[di+2] = (b << 3) | (b >> 2)
				fb.Pixels[di+3] = 0xFF
				si += 2
				di += 4
			}
		}
	case 8:
		for r := 0; r < h; r++ {
			di := fbBaseOff + r*fb.Stride
			si := r * srcStride
			for px := 0; px < w; px++ {
				c := fb.Palette[src[si]]
				fb.Pixels[di] = c[0]   // R
				fb.Pixels[di+1] = c[1] // G
				fb.Pixels[di+2] = c[2] // B
				fb.Pixels[di+3] = 0xFF
				si++
				di += 4
			}
		}
	}
}

// SetPalette updates the 8bpp color palette. Each entry is (R, G, B).
func (fb *Framebuffer) SetPalette(entries [][3]byte) {
	for i := 0; i < len(entries) && i < 256; i++ {
		fb.Palette[i] = entries[i]
	}
}

// WriteRectTopDown copies top-down RGBA 32bpp src data into the framebuffer at (x, y).
// The framebuffer is bottom-up, so rows are flipped during the copy.
func (fb *Framebuffer) WriteRectTopDown(x, y, w, h int, src []byte) {
	srcStride := w * 4
	if len(src) < srcStride*h {
		return
	}

	x, y, w, h = fb.clipRect(x, y, w, h)
	if w <= 0 || h <= 0 {
		return
	}

	copyBytes := w * 4
	fbBaseRow := fb.Height - y - h
	fbBaseOff := fbBaseRow*fb.Stride + x*4
	for r := 0; r < h; r++ {
		fbOff := fbBaseOff + r*fb.Stride
		// top-down src: row 0 is the top of the image, which maps to bottom-up row h-1-r
		srcOff := (h - 1 - r) * srcStride
		copy(fb.Pixels[fbOff:fbOff+copyBytes], src[srcOff:srcOff+copyBytes])
	}
}

// WriteRectStridedTopDown copies top-down RGBA 32bpp src data with an arbitrary
// stride into the framebuffer at (x, y). This avoids an intermediate copy when
// reading directly from a surface buffer that is wider than the update rect.
func (fb *Framebuffer) WriteRectStridedTopDown(x, y, w, h int, src []byte, srcStride int) {
	if srcStride < w*4 || len(src) < (h-1)*srcStride+w*4 {
		return
	}

	x, y, w, h = fb.clipRect(x, y, w, h)
	if w <= 0 || h <= 0 {
		return
	}

	copyBytes := w * 4
	fbBaseRow := fb.Height - y - h
	fbBaseOff := fbBaseRow*fb.Stride + x*4
	for r := 0; r < h; r++ {
		fbOff := fbBaseOff + r*fb.Stride
		srcOff := (h - 1 - r) * srcStride
		copy(fb.Pixels[fbOff:fbOff+copyBytes], src[srcOff:srcOff+copyBytes])
	}
}

// ReadRect extracts a rectangle from the framebuffer as bottom-up RGBA 32bpp.
// dst must be at least w*h*4 bytes. Returns the number of bytes written.
func (fb *Framebuffer) ReadRect(dst []byte, x, y, w, h int) int {
	x, y, w, h = fb.clipRect(x, y, w, h)
	if w <= 0 || h <= 0 {
		return 0
	}

	copyBytes := w * 4
	need := h * copyBytes
	if len(dst) < need {
		return 0
	}

	fbBaseRow := fb.Height - y - h
	fbBaseOff := fbBaseRow*fb.Stride + x*4
	for r := 0; r < h; r++ {
		fbOff := fbBaseOff + r*fb.Stride
		dstOff := r * copyBytes
		copy(dst[dstOff:dstOff+copyBytes], fb.Pixels[fbOff:fbOff+copyBytes])
	}
	return need
}

// CopyRect performs a screen-to-screen copy (ScrBlt). It reads the source
// rectangle into scratch, then writes it to the destination. scratch is
// grown if needed and returned for reuse (zero allocs in steady state).
// Handles overlapping src/dst correctly via the intermediate scratch buffer.
func (fb *Framebuffer) CopyRect(dstX, dstY, srcX, srcY, w, h int, scratch []byte) []byte {
	if w <= 0 || h <= 0 {
		return scratch
	}

	need := w * h * 4
	if cap(scratch) >= need {
		scratch = scratch[:need]
	} else {
		scratch = make([]byte, need)
	}

	fb.ReadRect(scratch, srcX, srcY, w, h)
	fb.WriteRect(dstX, dstY, w, h, scratch)
	return scratch
}

func bppToBytes(bpp int) int {
	switch bpp {
	case 32:
		return 4
	case 24:
		return 3
	case 16, 15:
		return 2
	case 8:
		return 1
	default:
		return 0
	}
}
