package filecontent

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"testing"

	"golang.org/x/image/bmp"
	"golang.org/x/image/tiff"
)

func TestPNGPreviewAndTextAreDistinguished(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	source.SetNRGBA(0, 0, color.NRGBA{R: 200, G: 100, B: 50, A: 128})
	var data bytes.Buffer
	if err := png.Encode(&data, source); err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeImage(data.Bytes(), "image.png")
	if err != nil || decoded == nil || decoded.Width != 2 || decoded.Height != 1 {
		t.Fatalf("PNG: %+v %v", decoded, err)
	}
	if decoded.Pix[3] != 128 || decoded.Pix[2] > 101 {
		t.Fatal("bitmap is not premultiplied BGRA")
	}
	if image, err := DecodeImage([]byte("key=value\n"), "config.properties"); err != nil || image != nil {
		t.Fatal("text misidentified as image")
	}
	if _, err := DecodeImage([]byte("broken"), "broken.png"); !errors.Is(err, ErrUnsupportedImage) {
		t.Fatal("broken image not reported")
	}
	if KnownBinary([]byte("MZ_SETTING=true\n")) {
		t.Fatal("text starting with MZ rejected")
	}
	if !KnownBinary([]byte("%PDF-1.7")) {
		t.Fatal("PDF treated as editable text")
	}
}

func TestImageFormatsAndPixelLimitBeforeDecode(t *testing.T) {
	source := image.NewRGBA(image.Rect(0, 0, 3, 2))
	encoders := map[string]func(io.Writer, image.Image) error{"jpg": func(w io.Writer, i image.Image) error { return jpeg.Encode(w, i, nil) }, "gif": func(w io.Writer, i image.Image) error { return gif.Encode(w, i, nil) }, "bmp": bmp.Encode, "tiff": func(w io.Writer, i image.Image) error { return tiff.Encode(w, i, nil) }}
	for ext, encode := range encoders {
		var data bytes.Buffer
		if err := encode(&data, source); err != nil {
			t.Fatal(err)
		}
		decoded, err := DecodeImage(data.Bytes(), "image."+ext)
		if err != nil || decoded.Width != 3 || decoded.Height != 2 {
			t.Fatalf("%s: %+v %v", ext, decoded, err)
		}
	}
	var data bytes.Buffer
	png.Encode(&data, source)
	huge := bytes.Clone(data.Bytes())
	binary.BigEndian.PutUint32(huge[16:20], MaxImagePixels)
	binary.BigEndian.PutUint32(huge[20:24], 2)
	binary.BigEndian.PutUint32(huge[29:33], crc32.ChecksumIEEE(huge[12:29]))
	if _, err := DecodeImage(huge, "huge.png"); !errors.Is(err, ErrImageTooLarge) {
		t.Fatal("pixel cap was not checked before decode", err)
	}
}
