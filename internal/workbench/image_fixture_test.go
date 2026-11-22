package workbench

import (
	"bytes"
	"image"
	"image/png"
	"testing"
)

// redSquarePNG is the smallest picture the clipboard and desktop-file tests
// need. It lives outside the platform-specific assistant tests because the
// desktop tests that paste it compile on every platform.
func redSquarePNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	for i := 0; i < len(img.Pix); i += 4 {
		copy(img.Pix[i:], []byte{220, 20, 20, 255})
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}
