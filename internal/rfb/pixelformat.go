package rfb

// PixelFormat is how many bits the server is asked to spend on each pixel. The
// picture the Sink receives is the same in every case (blue, green, red, one
// unused byte); a smaller format only shrinks what crosses the network, at the
// price of fewer colours: the client widens each channel back to eight bits, so
// full intensity stays full intensity.
type PixelFormat uint8

const (
	// Pixel32 is 32 bits per pixel, 8 per colour channel. It is the zero value
	// and needs no conversion.
	Pixel32 PixelFormat = iota
	// Pixel16 is 16 bits per pixel: 5 bits of red, 6 of green and 5 of blue.
	Pixel16
	// Pixel8 is 8 bits per pixel: 3 bits of red, 3 of green and 2 of blue.
	Pixel8
)

// size is the bytes one pixel takes on the wire in Raw, RRE and Hextile.
func (format PixelFormat) size() int {
	switch format {
	case Pixel16:
		return 2
	case Pixel8:
		return 1
	}
	return 4
}

// compressedSize is the bytes of a ZRLE CPIXEL: a 32-bit true-colour format
// whose colours fit in three bytes sends only those three; the narrower formats
// are sent whole.
func (format PixelFormat) compressedSize() int {
	if format == Pixel32 {
		return 3
	}
	return format.size()
}

// message is the SetPixelFormat message that asks the server for the format:
// little-endian true colour, the channel maxima and shifts of the constants
// above.
func (format PixelFormat) message() []byte {
	switch format {
	case Pixel16:
		return []byte{0, 0, 0, 0, 16, 16, 0, 1, 0, 31, 0, 63, 0, 31, 11, 5, 0, 0, 0, 0}
	case Pixel8:
		return []byte{0, 0, 0, 0, 8, 8, 0, 1, 0, 7, 0, 7, 0, 3, 0, 3, 6, 0, 0, 0}
	}
	// 32 bits per pixel, depth 24, red, green and blue in bits 16, 8 and 0:
	// memory order blue, green, red, pad.
	return []byte{0, 0, 0, 0, 32, 24, 0, 1, 0, 255, 0, 255, 0, 255, 16, 8, 0, 0, 0, 0}
}

// ramp widens a channel of 0..top to 0..255, rounding to the nearest value.
func ramp(top int) (table [64]byte) {
	for value := 0; value <= top; value++ {
		table[value] = byte((value*255 + top/2) / top)
	}
	return table
}

var expand2, expand3, expand5, expand6 = ramp(3), ramp(7), ramp(31), ramp(63)

// bgr is the colour of the pixel that starts src, which holds at least
// compressedSize bytes.
func (format PixelFormat) bgr(src []byte) (blue, green, red byte) {
	switch format {
	case Pixel16:
		value := uint16(src[0]) | uint16(src[1])<<8
		return expand5[value&31], expand6[value>>5&63], expand5[value>>11&31]
	case Pixel8:
		value := src[0]
		return expand2[value>>6&3], expand3[value>>3&7], expand3[value&7]
	}
	return src[0], src[1], src[2]
}

// expand writes the pixels in src, whole pixels of the wire format, to dst as
// blue, green, red and a zero byte each; dst has room for four bytes per pixel.
func (format PixelFormat) expand(dst, src []byte) {
	size := format.size()
	for from, to := 0, 0; from+size <= len(src); from, to = from+size, to+4 {
		dst[to], dst[to+1], dst[to+2] = format.bgr(src[from:])
		dst[to+3] = 0
	}
}
