package workbench

import (
	"image"
	stdcolor "image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/ui"
)

func writePNG(t testing.TB, path string, width, height int) {
	t.Helper()
	picture := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			picture.SetNRGBA(x, y, stdcolor.NRGBA{R: byte(x * 255 / width), G: byte(y * 255 / height), B: 40, A: 255})
		}
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := png.Encode(file, picture); err != nil {
		t.Fatal(err)
	}
}

func backgroundTestState(t *testing.T, picked string) (*State, Options) {
	t.Helper()
	_, options := newWorld(t)
	options.OpenFiles = func(app.OpenFileOptions) ([]string, error) { return []string{picked}, nil }
	state := newTestState(t, options)
	state.activeSection, state.settingsCategory = sectionSettings, settingsTerminal
	return state, options
}

// inTerminal opens a local terminal tab, where the picture is shown.
func inTerminal(t *testing.T, state *State) {
	t.Helper()
	state.activeSection = sectionHosts
	state.Click(hitLocalTerminal)
	if state.activeSection != sectionTerminal || state.deck.ActiveWorkspace() == 0 {
		t.Fatalf("no terminal: section %d", state.activeSection)
	}
}

func backdropOf(canvas *ui.Canvas) (ui.Image, bool) {
	for _, image := range canvas.Images() {
		if image.Backdrop {
			return image, true
		}
	}
	return ui.Image{}, false
}

func TestABackgroundPictureIsCopiedDecodedAndShownOnlyBehindTheTerminals(t *testing.T) {
	picked := filepath.Join(t.TempDir(), "wall.png")
	writePNG(t, picked, 400, 200)
	state, options := backgroundTestState(t, picked)
	size := ui.Size{Width: 1400, Height: 900}

	state.Click(hitBgChoose)
	waitFor(t, state, state.backgroundOn)
	if state.bg.width != 400 || state.bg.height != 200 || state.bg.name != "wall.png" {
		t.Fatalf("decoded %dx%d %q", state.bg.width, state.bg.height, state.bg.name)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(options.StatePath), backgroundFile)); err != nil {
		t.Fatalf("no copy in the data folder: %v", err)
	}

	// Outside the terminal the window is exactly what it was: no picture, every
	// surface opaque.
	var canvas ui.Canvas
	state.Paint(&canvas, size)
	if _, ok := backdropOf(&canvas); ok {
		t.Fatal("a backdrop on the settings page")
	}
	for _, rectangle := range canvas.Rectangles() {
		if rectangle.Color.Alpha < 1 && rectangle.Layer == 0 && rectangle.Color == state.pal.workspace {
			t.Fatalf("a translucent workspace surface on the settings page: %+v", rectangle)
		}
	}

	inTerminal(t, state)
	canvas = ui.Canvas{}
	state.Paint(&canvas, size)
	backdrop, ok := backdropOf(&canvas)
	if !ok {
		t.Fatal("no backdrop behind the terminal")
	}
	// A 2:1 picture in a 14:8.4 area is cropped at the sides, centered, and sits
	// below the window bar.
	area := ui.Rect{Y: windowBarHeight, Width: size.Width, Height: size.Height - windowBarHeight}
	want := float32(200) * area.Width / area.Height
	if backdrop.Bounds != area || backdrop.Source.Height != 200 || backdrop.Source.Width < want-1 || backdrop.Source.Width > want+1 ||
		backdrop.Source.X < (400-want)/2-1 || backdrop.Source.X > (400-want)/2+1 {
		t.Fatalf("backdrop %+v, wanted a centered %v-wide crop of %+v", backdrop, want, area)
	}
	// The window bar is opaque shell; nothing translucent covers the whole window.
	translucentTerminal := false
	for _, rectangle := range canvas.Rectangles() {
		if rectangle.Bounds.Width >= size.Width-terminalToolRailExtent && rectangle.Bounds.Height > size.Height/2 && rectangle.Color.Alpha < 1 {
			translucentTerminal = true
			if rectangle.Color.Alpha != bgMedium.opacity() {
				t.Fatalf("terminal ground alpha %v", rectangle.Color.Alpha)
			}
		}
		if rectangle.Bounds.Y == 0 && rectangle.Bounds.Height == windowBarHeight && rectangle.Color.Alpha < 1 {
			t.Fatalf("the window bar is translucent: %+v", rectangle)
		}
	}
	if !translucentTerminal {
		t.Fatal("the terminal ground is opaque, so the picture cannot show")
	}
	// Popups stay opaque: the component theme is built from the opaque palette.
	if state.Theme().Background.Alpha != 1 || state.WindowColor().Alpha != 1 {
		t.Fatalf("theme background %v, window color %v", state.Theme().Background, state.WindowColor())
	}

	state.Click(hitBgStrong) // not on this page: the click is ignored while the terminal is up
	state.bg.strength = bgStrong
	canvas = ui.Canvas{}
	state.Paint(&canvas, size)
	found := false
	for _, rectangle := range canvas.Rectangles() {
		if rectangle.Bounds.Width >= size.Width-terminalToolRailExtent && rectangle.Bounds.Height > size.Height/2 && rectangle.Color.Alpha == bgStrong.opacity() {
			found = true
		}
	}
	if !found {
		t.Fatal("the strong setting does not reach the terminal ground")
	}
}

func TestTheBackgroundSurvivesARestartAndRemovingItDeletesTheCopy(t *testing.T) {
	picked := filepath.Join(t.TempDir(), "wall.png")
	writePNG(t, picked, 64, 64)
	state, options := backgroundTestState(t, picked)
	state.Click(hitBgChoose)
	waitFor(t, state, state.backgroundOn)
	state.Click(hitBgSubtle)
	state.Close()

	again := newTestState(t, options)
	waitFor(t, again, again.backgroundOn)
	if again.bg.strength != bgSubtle || again.bg.name != "wall.png" {
		t.Fatalf("restored %+v", again.bg)
	}
	again.activeSection, again.settingsCategory = sectionSettings, settingsTerminal
	again.Click(hitBgRemove)
	if again.backgroundOn() || again.bg.set {
		t.Fatal("still set after removing")
	}
	if _, err := os.Stat(again.backgroundPath()); !os.IsNotExist(err) {
		t.Fatalf("the copy stays: %v", err)
	}
	saved, _, _ := loadState(options.StatePath)
	if saved.Background != nil {
		t.Fatalf("saved %+v", saved.Background)
	}
	inTerminal(t, again)
	var canvas ui.Canvas
	again.Paint(&canvas, ui.Size{Width: 800, Height: 600})
	if _, ok := backdropOf(&canvas); ok {
		t.Fatal("a backdrop after removing")
	}
}

func TestAnUnusableFileLeavesTheCurrentBackgroundAlone(t *testing.T) {
	good := filepath.Join(t.TempDir(), "wall.png")
	writePNG(t, good, 32, 32)
	state, options := backgroundTestState(t, good)
	state.Click(hitBgChoose)
	waitFor(t, state, state.backgroundOn)

	bad := filepath.Join(t.TempDir(), "notes.png")
	if err := os.WriteFile(bad, []byte("not an image"), 0o600); err != nil {
		t.Fatal(err)
	}
	state.options.OpenFiles = func(app.OpenFileOptions) ([]string, error) { return []string{bad}, nil }
	state.Click(hitBgChoose)
	if !state.backgroundOn() || state.bg.name != "wall.png" || !strings.Contains(state.status, state.messages.Tools.BgErrFormat) {
		t.Fatalf("name %q, status %q", state.bg.name, state.status)
	}
	if data, _ := os.ReadFile(filepath.Join(filepath.Dir(options.StatePath), backgroundFile)); len(data) < 20 || string(data) == "not an image" {
		t.Fatal("the stored copy was replaced")
	}
}

func TestAStoredCopyThatCannotBeReadSwitchesTheBackgroundOff(t *testing.T) {
	picked := filepath.Join(t.TempDir(), "wall.png")
	writePNG(t, picked, 32, 32)
	state, options := backgroundTestState(t, picked)
	state.Click(hitBgChoose)
	waitFor(t, state, state.backgroundOn)
	state.Close()
	if err := os.WriteFile(filepath.Join(filepath.Dir(options.StatePath), backgroundFile), []byte("damaged"), 0o600); err != nil {
		t.Fatal(err)
	}
	again := newTestState(t, options)
	waitFor(t, again, func() bool { return !again.bg.set && again.status != "" })
	if again.backgroundOn() {
		t.Fatal("a damaged picture is shown")
	}
}

func TestLargePicturesAreScaledDownAndCannotBeBombs(t *testing.T) {
	t.Parallel() // it encodes an 81-megapixel picture
	picture := image.NewNRGBA(image.Rect(0, 0, 5120, 1000))
	pix, width, height, err := scaleToBGRA(picture, backgroundMaxEdge)
	if err != nil || width != 2560 || height != 500 || len(pix) != 2560*500*4 {
		t.Fatalf("%dx%d, %d bytes, %v", width, height, len(pix), err)
	}
	small := image.NewNRGBA(image.Rect(10, 10, 30, 20))
	small.SetNRGBA(10, 10, stdcolor.NRGBA{R: 200, G: 100, B: 50, A: 255})
	pix, width, height, _ = scaleToBGRA(small, backgroundMaxEdge)
	if width != 20 || height != 10 || pix[0] != 50 || pix[1] != 100 || pix[2] != 200 || pix[3] != 255 {
		t.Fatalf("%dx%d pixel %v", width, height, pix[:4])
	}

	huge := filepath.Join(t.TempDir(), "huge.png")
	file, _ := os.Create(huge)
	_ = png.Encode(file, image.NewGray(image.Rect(0, 0, 9000, 9000)))
	file.Close()
	if _, _, _, err := loadBackgroundImage(huge); err != errBackgroundPixels {
		t.Fatalf("81 million pixels: %v", err)
	}
}
