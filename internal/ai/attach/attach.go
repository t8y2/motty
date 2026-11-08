// Package attach turns a file the user dropped on the assistant into something a
// model can read: its text (from a text file or a Word, Excel or PowerPoint
// document), or a picture in a format every vision endpoint takes.
// It decides nothing about the conversation; the caller says where the result goes.
package attach

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"

	_ "image/gif"

	_ "golang.org/x/image/bmp"
	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"

	"motty/internal/ai/llm"
	"motty/internal/filecontent"
)

// Limits. A text file is refused rather than cut: a model told it read a file
// that was silently truncated gives answers about a file that does not exist.
const (
	// MaxTextBytes is the default text file limit.
	MaxTextBytes             = 4 << 20
	MaxConfigurableTextBytes = 8 << 20
	// MaxImageBytes is the most one picture sends. The Anthropic API refuses a
	// picture over 5 MiB once it is base64, which adds a third.
	MaxImageBytes = 3584 << 10
	// MaxImageEdge is the longest side the APIs take.
	MaxImageEdge = 8000
	// resizeEdge is what a picture over the byte limit is brought down to first:
	// the models scale anything larger themselves.
	resizeEdge = 2048
	// maxReadBytes bounds what is read of a file before it is known to be either.
	maxReadBytes = 32 << 20
	MaxReadBytes = maxReadBytes
)

var (
	ErrFolder       = errors.New("attach: a folder cannot be attached")
	ErrEmpty        = errors.New("attach: the file is empty")
	ErrUnsupported  = errors.New("attach: neither a text file nor a picture")
	ErrReadTooLarge = errors.New("attach: file exceeds the read limit")
	// ErrTooLarge is a picture that cannot be brought under the limit, or a file
	// too big to read at all. ErrTextTooLarge is a text file over MaxTextBytes.
	ErrTooLarge     = errors.New("attach: the file is too large")
	ErrTextTooLarge = errors.New("attach: the text is too large")
)

// Kind says what a File is.
type Kind uint8

const (
	KindText Kind = iota
	KindImage
)

// File is what one dropped path became.
type File struct {
	Name  string // the base name, which is all the model and the panel are told
	Kind  Kind
	Text  string    // KindText: the content, with line ends made "\n"
	Lines int       // KindText: how many lines Text has
	Image llm.Image // KindImage
}

// Load reads path and classifies it by what its bytes are, never by its name.
func Load(path string) (File, error) {
	return LoadWithLimit(path, MaxTextBytes)
}

func LoadWithLimit(path string, maxTextBytes int) (File, error) {
	if maxTextBytes < 1 || maxTextBytes > MaxConfigurableTextBytes {
		return File{}, errors.New("attach: invalid text limit")
	}
	info, err := os.Stat(path)
	if err != nil {
		return File{}, err
	}
	if info.IsDir() {
		return File{}, ErrFolder
	}
	if !info.Mode().IsRegular() {
		return File{}, ErrUnsupported
	}
	if info.Size() > maxReadBytes {
		return File{}, ErrReadTooLarge
	}
	handle, err := os.Open(path)
	if err != nil {
		return File{}, err
	}
	defer handle.Close()
	data, err := io.ReadAll(io.LimitReader(handle, maxReadBytes+1))
	if err != nil {
		return File{}, err
	}
	return DecodeWithLimit(filepath.Base(path), data, maxTextBytes)
}

// Decode classifies the bytes of a file called name.
func Decode(name string, data []byte) (File, error) {
	return DecodeWithLimit(name, data, MaxTextBytes)
}

func DecodeWithLimit(name string, data []byte, maxTextBytes int) (File, error) {
	if maxTextBytes < 1 || maxTextBytes > MaxConfigurableTextBytes {
		return File{}, errors.New("attach: invalid text limit")
	}
	file := File{Name: name}
	if len(data) == 0 {
		return file, ErrEmpty
	}
	if len(data) > maxReadBytes {
		return file, ErrReadTooLarge
	}
	picture, isPicture, err := preparePicture(data)
	if isPicture {
		if err != nil {
			return file, err
		}
		file.Kind, file.Image = KindImage, picture
		return file, nil
	}
	text, err := officeText(data, maxTextBytes)
	if errors.Is(err, errNotOffice) {
		text, err = decodeTextWithLimit(data, maxTextBytes)
	}
	if err != nil {
		return file, err
	}
	file.Text, file.Lines = text, strings.Count(text, "\n")+1
	if strings.HasSuffix(text, "\n") {
		file.Lines--
	}
	return file, nil
}

// decodeText reads UTF-8 and the Unicode forms with a byte-order mark, and falls
// back to GB18030 for a file that is not UTF-8, which is what Chinese Windows
// tools still write. filecontent turns away anything that does not decode back to
// the same bytes or holds control characters, so a binary file is not mistaken.
func decodeText(data []byte) (string, error) {
	return decodeTextWithLimit(data, MaxTextBytes)
}

func decodeTextWithLimit(data []byte, maxTextBytes int) (string, error) {
	if len(data) > maxTextBytes {
		// Say it is too long only of a file that is text; a big archive is not.
		if sniffText(data[:min(len(data), 8<<10)]) {
			return "", ErrTextTooLarge
		}
		return "", ErrUnsupported
	}
	decoded, err := filecontent.Decode(data, "Auto")
	if errors.Is(err, filecontent.ErrChooseEncoding) {
		decoded, err = filecontent.Decode(data, "GB18030")
	}
	if err != nil {
		return "", ErrUnsupported
	}
	return decoded.Value, nil
}

// sniffText says whether the start of a file has no byte a text file does not.
func sniffText(prefix []byte) bool {
	for _, b := range prefix {
		if b < 0x20 && b != '\n' && b != '\r' && b != '\t' && b != '\f' || b == 0x7f {
			return false
		}
	}
	return true
}

// preparePicture says whether data is a picture and, if so, returns it in a form
// that can be sent. The supported formats go as they are when they fit; anything
// else the decoders know (BMP, TIFF), or a picture over the limits, is decoded,
// brought down and written again.
func preparePicture(data []byte) (llm.Image, bool, error) {
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return llm.Image{}, false, nil
	}
	if config.Width <= 0 || config.Height <= 0 || config.Width > filecontent.MaxImagePixels/config.Height {
		return llm.Image{}, true, ErrTooLarge
	}
	if mediaType := llm.DetectImage(data); mediaType != "" && len(data) <= MaxImageBytes && max(config.Width, config.Height) <= MaxImageEdge {
		return llm.Image{MediaType: mediaType, Data: data}, true, nil
	}
	source, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return llm.Image{}, true, ErrUnsupported
	}
	source = orient(source, jpegOrientation(data))
	for edge := resizeEdge; ; edge /= 2 {
		out, ok := encode(fit(source, edge))
		if ok {
			return out, true, nil
		}
		if edge <= 256 {
			return llm.Image{}, true, ErrTooLarge
		}
	}
}

// fit scales source down so that neither side is above edge.
func fit(source image.Image, edge int) image.Image {
	bounds := source.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if max(width, height) <= edge {
		return source
	}
	if width >= height {
		height = max(height*edge/width, 1)
		width = edge
	} else {
		width = max(width*edge/height, 1)
		height = edge
	}
	scaled := image.NewNRGBA(image.Rect(0, 0, width, height))
	xdraw.CatmullRom.Scale(scaled, scaled.Bounds(), source, bounds, xdraw.Src, nil)
	return scaled
}

// encode writes source as PNG when it has transparency, so it is not painted
// black, and as JPEG otherwise, which is far smaller for a photograph. It reports
// false when the result is still over the limit.
func encode(source image.Image) (llm.Image, bool) {
	var out bytes.Buffer
	if opaque(source) {
		if jpeg.Encode(&out, source, &jpeg.Options{Quality: 85}) == nil && out.Len() <= MaxImageBytes {
			return llm.Image{MediaType: llm.ImageJPEG, Data: out.Bytes()}, true
		}
		return llm.Image{}, false
	}
	if png.Encode(&out, source) == nil && out.Len() <= MaxImageBytes {
		return llm.Image{MediaType: llm.ImagePNG, Data: out.Bytes()}, true
	}
	return llm.Image{}, false
}

// opaque reports whether no pixel of source is transparent.
func opaque(source image.Image) bool {
	if known, ok := source.(interface{ Opaque() bool }); ok {
		return known.Opaque()
	}
	bounds := source.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if _, _, _, alpha := source.At(x, y).RGBA(); alpha != 0xffff {
				return false
			}
		}
	}
	return true
}

// orient applies an EXIF orientation (1 to 8), which a JPEG written again by
// this package would otherwise lose, leaving a photo taken upright on its side.
func orient(source image.Image, orientation int) image.Image {
	if orientation < 2 || orientation > 8 {
		return source
	}
	bounds := source.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	swap := orientation >= 5
	target := image.NewNRGBA(image.Rect(0, 0, width, height))
	if swap {
		target = image.NewNRGBA(image.Rect(0, 0, height, width))
	}
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			var tx, ty int
			switch orientation {
			case 2:
				tx, ty = width-1-x, y
			case 3:
				tx, ty = width-1-x, height-1-y
			case 4:
				tx, ty = x, height-1-y
			case 5:
				tx, ty = y, x
			case 6:
				tx, ty = height-1-y, x
			case 7:
				tx, ty = height-1-y, width-1-x
			case 8:
				tx, ty = y, width-1-x
			}
			target.Set(tx, ty, color.NRGBAModel.Convert(source.At(bounds.Min.X+x, bounds.Min.Y+y)))
		}
	}
	return target
}

// jpegOrientation reads the EXIF orientation of a JPEG, or 0 when there is none.
func jpegOrientation(data []byte) int {
	if len(data) < 4 || data[0] != 0xff || data[1] != 0xd8 {
		return 0
	}
	for offset := 2; offset+4 <= len(data); {
		if data[offset] != 0xff {
			return 0
		}
		marker := data[offset+1]
		if marker == 0xff {
			offset++
			continue
		}
		if marker == 0xda || marker == 0xd9 { // the picture itself begins
			return 0
		}
		length := int(data[offset+2])<<8 | int(data[offset+3])
		if length < 2 || offset+2+length > len(data) {
			return 0
		}
		segment := data[offset+4 : offset+2+length]
		if marker == 0xe1 && bytes.HasPrefix(segment, []byte("Exif\x00\x00")) {
			return tiffOrientation(segment[6:])
		}
		offset += 2 + length
	}
	return 0
}

func tiffOrientation(tiff []byte) int {
	if len(tiff) < 8 {
		return 0
	}
	var order binary.ByteOrder
	switch string(tiff[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return 0
	}
	ifd := int(order.Uint32(tiff[4:]))
	if ifd < 8 || ifd+2 > len(tiff) {
		return 0
	}
	count := int(order.Uint16(tiff[ifd:]))
	for index := 0; index < count; index++ {
		entry := ifd + 2 + 12*index
		if entry+12 > len(tiff) {
			return 0
		}
		if order.Uint16(tiff[entry:]) == 0x0112 {
			return int(order.Uint16(tiff[entry+8:]))
		}
	}
	return 0
}
