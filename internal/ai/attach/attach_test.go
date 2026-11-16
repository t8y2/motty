package attach

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/image/bmp"

	"motty/internal/ai/llm"
)

func solid(width, height int, c color.NRGBA) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.SetNRGBA(x, y, c)
		}
	}
	return img
}

func pngBytes(t testing.TB, img image.Image) []byte {
	t.Helper()
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

// noise cannot be compressed, so its encoded size follows its area.
func noise(width, height int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	random := rand.New(rand.NewSource(1))
	random.Read(img.Pix)
	for i := 3; i < len(img.Pix); i += 4 {
		img.Pix[i] = 0xff
	}
	return img
}

func TestTextIsReadByContentNotByName(t *testing.T) {
	file, err := Decode("notes.bin", []byte("first\r\nsecond\r\n"))
	if err != nil || file.Kind != KindText || file.Text != "first\nsecond\n" || file.Lines != 2 {
		t.Fatalf("file = %+v, err = %v", file, err)
	}
}

func TestChineseTextInGB18030IsDecoded(t *testing.T) {
	// "你好" in GB18030.
	file, err := Decode("a.txt", []byte{0xc4, 0xe3, 0xba, 0xc3})
	if err != nil || file.Text != "你好" {
		t.Fatalf("file = %+v, err = %v", file, err)
	}
}

func TestBinaryAndOversizeAndEmptyFilesAreTurnedAway(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		want error
	}{
		{"binary", []byte("\x7fELF\x02\x01\x01\x00\x00\x00\x00\x00"), ErrUnsupported},
		{"pdf", []byte("%PDF-1.7\n%\xe2\xe3\xcf\xd3\n1 0 obj\x00"), ErrUnsupported},
		{"empty", nil, ErrEmpty},
		{"over the text limit", bytes.Repeat([]byte("a"), MaxTextBytes+1), ErrTextTooLarge},
		{"a big binary is not a long text", append([]byte("PK\x03\x04\x00"), make([]byte, MaxTextBytes)...), ErrUnsupported},
	}
	for _, c := range cases {
		if _, err := Decode(c.name, c.data); !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v, want %v", c.name, err, c.want)
		}
	}
}

func TestConfiguredTextLimitCanGrowWithoutAcceptingArbitrarySizes(t *testing.T) {
	data := bytes.Repeat([]byte("a"), MaxTextBytes+1)
	if _, err := Decode("artifact.txt", data); !errors.Is(err, ErrTextTooLarge) {
		t.Fatalf("default limit: %v", err)
	}
	if file, err := DecodeWithLimit("artifact.txt", data, 8<<20); err != nil || len(file.Text) != len(data) {
		t.Fatalf("higher configured limit: %v", err)
	}
	if _, err := DecodeWithLimit("artifact.txt", data, 1<<20); !errors.Is(err, ErrTextTooLarge) {
		t.Fatalf("lower configured limit: %v", err)
	}
	if _, err := DecodeWithLimit("artifact.txt", data, MaxConfigurableTextBytes+1); err == nil {
		t.Fatal("an unbounded text limit was accepted")
	}
}

func TestOverTheReadLimitIsNotMistakenForTheConfigurableTextLimit(t *testing.T) {
	if _, err := DecodeWithLimit("build.bin", make([]byte, MaxReadBytes+1), MaxConfigurableTextBytes); !errors.Is(err, ErrReadTooLarge) {
		t.Fatalf("read limit: %v", err)
	}
}

func TestASupportedPictureGoesAsItIs(t *testing.T) {
	data := pngBytes(t, solid(40, 30, color.NRGBA{200, 0, 0, 255}))
	file, err := Decode("shot.png", data)
	if err != nil || file.Kind != KindImage || file.Image.MediaType != llm.ImagePNG || !bytes.Equal(file.Image.Data, data) {
		t.Fatalf("file = %+v, err = %v", file, err)
	}
}

func TestAPictureInAnotherFormatIsWrittenAsPNGOrJPEG(t *testing.T) {
	var out bytes.Buffer
	if err := bmp.Encode(&out, solid(16, 16, color.NRGBA{0, 0, 200, 255})); err != nil {
		t.Fatal(err)
	}
	file, err := Decode("a.bmp", out.Bytes())
	if err != nil || file.Kind != KindImage || llm.DetectImage(file.Image.Data) != file.Image.MediaType {
		t.Fatalf("file = %+v, err = %v", file, err)
	}
}

func TestAPictureOverTheByteLimitIsBroughtDown(t *testing.T) {
	big := noise(2400, 1600) // about 15 MB of PNG
	data := pngBytes(t, big)
	if len(data) <= MaxImageBytes {
		t.Fatalf("the test picture is only %d bytes", len(data))
	}
	file, err := Decode("big.png", data)
	if err != nil {
		t.Fatal(err)
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(file.Image.Data))
	if err != nil || len(file.Image.Data) > MaxImageBytes || llm.DetectImage(file.Image.Data) != file.Image.MediaType {
		t.Fatalf("result is %d bytes (%v)", len(file.Image.Data), err)
	}
	if max(config.Width, config.Height) > resizeEdge || config.Width*2 != config.Height*3 && abs(config.Width*2-config.Height*3) > 3 {
		t.Fatalf("result is %dx%d", config.Width, config.Height)
	}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func TestATransparentPictureStaysPNG(t *testing.T) {
	img := noise(2400, 1600)
	img.SetNRGBA(0, 0, color.NRGBA{1, 2, 3, 0})
	file, err := Decode("clear.png", pngBytes(t, img))
	if err != nil || file.Image.MediaType != llm.ImagePNG {
		t.Fatalf("media type = %q, err = %v", file.Image.MediaType, err)
	}
}

func TestAPictureWithTooManyPixelsIsRefusedBeforeItIsDecoded(t *testing.T) {
	// Only the header of a 20000x20000 picture: DecodeConfig reads no more.
	header := make([]byte, 13)
	binary.BigEndian.PutUint32(header[0:], 20000)
	binary.BigEndian.PutUint32(header[4:], 20000)
	header[8], header[9] = 8, 2
	chunk := append([]byte("IHDR"), header...)
	data := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\x0d")
	data = append(data, chunk...)
	data = binary.BigEndian.AppendUint32(data, crc32.ChecksumIEEE(chunk))
	if _, err := Decode("huge.png", data); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v", err)
	}
}

// exifJPEG makes a JPEG with an EXIF segment declaring orientation.
func exifJPEG(t testing.TB, img image.Image, orientation byte) []byte {
	t.Helper()
	var plain bytes.Buffer
	if err := jpeg.Encode(&plain, img, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	tiff := []byte{'M', 'M', 0, 42, 0, 0, 0, 8, 0, 1, 0x01, 0x12, 0, 3, 0, 0, 0, 1, 0, orientation, 0, 0, 0, 0, 0, 0}
	segment := append([]byte("Exif\x00\x00"), tiff...)
	length := len(segment) + 2
	out := []byte{0xff, 0xd8, 0xff, 0xe1, byte(length >> 8), byte(length)}
	out = append(out, segment...)
	return append(out, plain.Bytes()[2:]...)
}

func TestExifOrientationIsReadAndSurvivesAResize(t *testing.T) {
	// Wide red left half, green right half; orientation 6 means "rotate 90 clockwise to show".
	img := image.NewNRGBA(image.Rect(0, 0, 3000, 2000))
	for y := 0; y < 2000; y++ {
		for x := 0; x < 3000; x++ {
			if x < 1500 {
				img.SetNRGBA(x, y, color.NRGBA{255, 0, 0, 255})
			} else {
				img.SetNRGBA(x, y, color.NRGBA{0, 255, 0, 255})
			}
		}
	}
	data := exifJPEG(t, img, 6)
	if got := jpegOrientation(data); got != 6 {
		t.Fatalf("orientation = %d", got)
	}
	oriented := orient(img, 6)
	if b := oriented.Bounds(); b.Dx() != 2000 || b.Dy() != 3000 {
		t.Fatalf("rotated size = %v", b)
	}
	// Rotated clockwise, the left (red) half is the top half.
	top := color.NRGBAModel.Convert(oriented.At(1000, 100)).(color.NRGBA)
	bottom := color.NRGBAModel.Convert(oriented.At(1000, 2900)).(color.NRGBA)
	if top.R < 200 || bottom.G < 200 {
		t.Fatalf("top = %v, bottom = %v", top, bottom)
	}
}

func TestLoadRefusesFoldersAndReadsFiles(t *testing.T) {
	dir := t.TempDir()
	if _, err := Load(dir); !errors.Is(err, ErrFolder) {
		t.Fatalf("folder: %v", err)
	}
	path := filepath.Join(dir, "x.md")
	if err := os.WriteFile(path, []byte("# title\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := Load(path)
	if err != nil || file.Name != "x.md" || !strings.HasPrefix(file.Text, "# title") {
		t.Fatalf("file = %+v, err = %v", file, err)
	}
	if _, err := Load(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("a missing file loaded")
	}
}

func TestAPhotoWrittenAgainStaysUpright(t *testing.T) {
	data := exifJPEG(t, noise(3000, 2000), 6)
	if len(data) <= MaxImageBytes {
		t.Fatalf("the test photo is only %d bytes", len(data))
	}
	file, err := Decode("photo.jpg", data)
	if err != nil {
		t.Fatal(err)
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(file.Image.Data))
	if err != nil || config.Height <= config.Width {
		t.Fatalf("a picture the camera marked as portrait came out %dx%d (%v)", config.Width, config.Height, err)
	}
}
