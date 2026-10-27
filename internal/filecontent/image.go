package filecontent

import (
	"bytes"
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"path/filepath"
	"strings"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"
)

const MaxImagePixels = 16 << 20

var ErrUnsupportedImage = errors.New("image format cannot be previewed")
var ErrImageTooLarge = errors.New("image exceeds 16 megapixels")

type Image struct {
	Pix           []byte
	Width, Height int
	Format        string
}

// DecodeImage checks dimensions before allocating the decoded bitmap. A nil
// image with no error means the file is not an image and may be tried as text.
func DecodeImage(data []byte, name string) (*Image, error) {
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		switch strings.ToLower(filepath.Ext(name)) {
		case ".png", ".jpg", ".jpeg", ".gif", ".bmp", ".webp", ".tif", ".tiff", ".ico", ".svg", ".avif", ".heic":
			return nil, ErrUnsupportedImage
		}
		return nil, nil
	}
	if config.Width <= 0 || config.Height <= 0 || config.Width > MaxImagePixels/config.Height {
		return nil, ErrImageTooLarge
	}
	source, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, ErrUnsupportedImage
	}
	bounds := source.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w <= 0 || h <= 0 || w > MaxImagePixels/h {
		return nil, ErrImageTooLarge
	}
	pix := make([]byte, w*h*4)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r, g, b, a := source.At(bounds.Min.X+x, bounds.Min.Y+y).RGBA()
			i := (y*w + x) * 4
			pix[i], pix[i+1], pix[i+2], pix[i+3] = byte(b>>8), byte(g>>8), byte(r>>8), byte(a>>8)
		}
	}
	return &Image{Pix: pix, Width: w, Height: h, Format: format}, nil
}

// KnownBinary recognizes formats that must never be offered as editable text.
func KnownBinary(data []byte) bool {
	return (len(data) >= 64 && bytes.HasPrefix(data, []byte{'M', 'Z'}) && bytes.IndexByte(data[:64], 0) >= 0) || bytes.HasPrefix(data, []byte{0x7f, 'E', 'L', 'F'}) || bytes.HasPrefix(data, []byte{'P', 'K', 3, 4}) || bytes.HasPrefix(data, []byte("%PDF-")) || bytes.HasPrefix(data, []byte{0x1f, 0x8b})
}
