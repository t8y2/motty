package workbench

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // the decoders for the formats the picker offers
	_ "image/png"
	"os"
	"path/filepath"
	"sync/atomic"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/flex"
	"github.com/vivaui/viva/ui"
	"motty/internal/i18n"
)

// A picture behind the interface. The file is copied into the data folder (so
// moving the original does not lose it), decoded off the window thread to
// premultiplied BGRA no larger than backgroundMaxEdge, and painted with
// Canvas.DrawBackdrop, cover-fit to the terminal area. It is for the terminals
// only: their ground is drawn translucent by the chosen visibility, and
// everything else (window bar, sidebar, pages, panels, dialogs) stays opaque.
// Nothing is decoded or held when no picture is set.

// backgroundHits is the place of the controls this file paints.
var backgroundHits = tree.Feature("background")

var (
	hitBgChoose = tree.ID(backgroundHits, "bg-choose")
	hitBgRemove = tree.ID(backgroundHits, "bg-remove")
	hitBgSubtle = tree.ID(backgroundHits, "bg-subtle")
	hitBgMedium = tree.ID(backgroundHits, "bg-medium")
	hitBgStrong = tree.ID(backgroundHits, "bg-strong")
)

const (
	backgroundFile      = "background.img"
	backgroundMaxBytes  = 30 << 20
	backgroundMaxPixels = 64 << 20
	// backgroundMaxEdge bounds the decoded picture's longer side: at most about
	// 16 MB of pixels stay in memory.
	backgroundMaxEdge = 2560
)

// backgroundStrength is how much of the picture shows: the opacity of the
// surfaces over it.
type backgroundStrength uint8

const (
	bgMedium backgroundStrength = iota
	bgSubtle
	bgStrong
)

func (strength backgroundStrength) opacity() float32 {
	switch strength {
	case bgSubtle:
		return 0.92
	case bgStrong:
		return 0.62
	}
	return 0.80
}

func (strength backgroundStrength) saved() string {
	switch strength {
	case bgSubtle:
		return "subtle"
	case bgStrong:
		return "strong"
	}
	return ""
}

func backgroundStrengthFromString(value string) backgroundStrength {
	switch value {
	case "subtle":
		return bgSubtle
	case "strong":
		return bgStrong
	}
	return bgMedium
}

type savedBackground struct {
	Name     string `json:"name,omitempty"`
	Strength string `json:"strength,omitempty"`
}

// backgroundImages numbers decoded pictures so a renderer never mistakes a new
// one for the one it holds.
var backgroundImages atomic.Uint64

type backgroundState struct {
	// set is whether a picture is chosen; name is the original file's name.
	set      bool
	name     string
	strength backgroundStrength

	// The decoded picture; ready once pix is non-nil.
	pix           []byte
	width, height int
	id            uint64
	// loading is the request in flight: an answer to an older one is dropped.
	loading uint64
	// waiting says the decode could not start yet.
	waiting bool
	serial  uint64

	// text is the settings row's description, made again only when what it says
	// changes.
	text         string
	textName     string
	textW, textH int
	textLanguage i18n.Language
}

func (state *State) backgroundPath() string {
	return filepath.Join(filepath.Dir(state.options.StatePath), backgroundFile)
}

func (state *State) savedBackgroundValue() *savedBackground {
	if !state.bg.set {
		return nil
	}
	return &savedBackground{Name: state.bg.name, Strength: state.bg.strength.saved()}
}

// restoreBackground applies the saved choice and starts decoding the stored
// copy. A copy that is gone leaves no picture.
func (state *State) restoreBackground(saved *savedBackground) {
	state.bg.serial++
	state.bg = backgroundState{serial: state.bg.serial}
	if saved == nil {
		return
	}
	if _, err := os.Stat(state.backgroundPath()); err != nil {
		return
	}
	state.bg.set, state.bg.name, state.bg.strength = true, saved.Name, backgroundStrengthFromString(saved.Strength)
	state.decodeBackground(state.bg.serial)
}

// decodeBackground reads and decodes the stored copy on a goroutine.
func (state *State) decodeBackground(serial uint64) {
	if state.ai.inbox == nil {
		state.bg.waiting = true // the window thread's inbox is made after the state is restored
		return
	}
	state.bg.waiting = false
	state.bg.loading = serial
	path := state.backgroundPath()
	go func() {
		pix, width, height, err := loadBackgroundImage(path)
		state.aiPost(func(state *State) {
			if state.bg.serial != serial {
				return
			}
			state.bg.loading = 0
			if err != nil {
				state.bg.set = false
				state.status = fmt.Sprintf(state.messages.Tools.BgErrFmt, state.backgroundErrorText(err))
				return
			}
			state.bg.pix, state.bg.width, state.bg.height = pix, width, height
			state.bg.id = 1<<62 | backgroundImages.Add(1)
		})
	}()
}

var (
	errBackgroundBytes  = errors.New("background: file too large")
	errBackgroundPixels = errors.New("background: too many pixels")
)

func (state *State) backgroundErrorText(err error) string {
	m := state.messages.Tools
	switch {
	case errors.Is(err, errBackgroundBytes):
		return m.BgErrTooLarge
	case errors.Is(err, errBackgroundPixels):
		return m.BgErrTooManyPixels
	case errors.Is(err, image.ErrFormat):
		return m.BgErrFormat
	}
	return err.Error()
}

// loadBackgroundImage decodes a PNG or JPEG file to premultiplied BGRA, scaled
// down so its longer side is at most backgroundMaxEdge.
func loadBackgroundImage(path string) (pix []byte, width, height int, err error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, 0, 0, err
	}
	if info.Size() > backgroundMaxBytes {
		return nil, 0, 0, errBackgroundBytes
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, 0, err
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, 0, 0, err
	}
	if config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > backgroundMaxPixels {
		return nil, 0, 0, errBackgroundPixels
	}
	source, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, 0, 0, err
	}
	return scaleToBGRA(source, backgroundMaxEdge)
}

// scaleToBGRA box-filters source down to at most edge pixels on its longer side
// (never up) and returns premultiplied BGRA, top row first.
func scaleToBGRA(source image.Image, edge int) (pix []byte, width, height int, err error) {
	bounds := source.Bounds()
	sw, sh := bounds.Dx(), bounds.Dy()
	if sw <= 0 || sh <= 0 {
		return nil, 0, 0, image.ErrFormat
	}
	width, height = sw, sh
	if long := max(sw, sh); long > edge {
		width, height = max(sw*edge/long, 1), max(sh*edge/long, 1)
	}
	pix = make([]byte, width*height*4)
	for y := 0; y < height; y++ {
		y0, y1 := y*sh/height, max((y+1)*sh/height, y*sh/height+1)
		for x := 0; x < width; x++ {
			x0, x1 := x*sw/width, max((x+1)*sw/width, x*sw/width+1)
			var r, g, b, a, count uint64
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					pr, pg, pb, pa := source.At(bounds.Min.X+sx, bounds.Min.Y+sy).RGBA()
					r, g, b, a, count = r+uint64(pr), g+uint64(pg), b+uint64(pb), a+uint64(pa), count+1
				}
			}
			offset := (y*width + x) * 4
			pix[offset], pix[offset+1], pix[offset+2], pix[offset+3] = byte(b/count>>8), byte(g/count>>8), byte(r/count>>8), byte(a/count>>8)
		}
	}
	return pix, width, height, nil
}

// chooseBackground asks for a picture, copies it into the data folder and starts
// decoding it.
func (state *State) chooseBackground() {
	m := state.messages.Tools
	paths, err := state.options.OpenFiles(app.OpenFileOptions{
		Title:   m.BgPickerTitle,
		Filters: []app.FileFilter{{Name: "PNG, JPEG", Pattern: "*.png;*.jpg;*.jpeg"}},
	})
	if errors.Is(err, app.ErrCanceled) || err == nil && len(paths) == 0 {
		return
	}
	if err != nil {
		state.status = fmt.Sprintf(m.BgErrFmt, err)
		return
	}
	info, err := os.Stat(paths[0])
	if err == nil && info.Size() > backgroundMaxBytes {
		err = errBackgroundBytes
	}
	var data []byte
	if err == nil {
		data, err = os.ReadFile(paths[0])
	}
	if err == nil {
		// Checked before it replaces the current one: a bad file leaves things as they were.
		_, _, err = image.DecodeConfig(bytes.NewReader(data))
	}
	if err != nil {
		state.status = fmt.Sprintf(m.BgErrFmt, state.backgroundErrorText(err))
		return
	}
	if err := writeFileAtomic(state.backgroundPath(), data); err != nil {
		state.status = fmt.Sprintf(m.BgErrFmt, err)
		return
	}
	strength := state.bg.strength
	state.bg.serial++
	state.bg = backgroundState{serial: state.bg.serial, set: true, name: filepath.Base(paths[0]), strength: strength}
	state.decodeBackground(state.bg.serial)
	state.save()
}

func (state *State) removeBackground() {
	if !state.bg.set {
		return
	}
	_ = os.Remove(state.backgroundPath())
	strength := state.bg.strength
	state.bg.serial++
	state.bg = backgroundState{serial: state.bg.serial, strength: strength}
	state.save()
}

func (state *State) clickBackground(id ui.ID) bool {
	switch id {
	case hitBgChoose:
		state.chooseBackground()
	case hitBgRemove:
		state.removeBackground()
	case hitBgSubtle, hitBgMedium, hitBgStrong:
		strength := bgMedium
		switch id {
		case hitBgSubtle:
			strength = bgSubtle
		case hitBgStrong:
			strength = bgStrong
		}
		if state.bg.set && state.bg.strength != strength {
			state.bg.strength = strength
			state.save()
		}
	default:
		return false
	}
	return true
}

// backgroundOn reports whether a decoded picture is being painted.
func (state *State) backgroundOn() bool { return state.bg.set && state.bg.pix != nil }

// ground is the color of a terminal's ground: the color itself, or, while the
// picture is showing, the color at the picture's visibility.
func (state *State) ground(color ui.Color) ui.Color {
	if state.showsBackground() {
		color.Alpha *= state.bg.strength.opacity()
	}
	return color
}

// showsBackground reports whether the picture is on screen: only behind the
// terminals of a workspace, which is the one place it is meant to be.
func (state *State) showsBackground() bool {
	return state.backgroundOn() && state.activeSection == sectionTerminal && state.deck.ActiveWorkspace() != 0
}

// paintBackground puts the picture behind the terminal area, cover-fit below the
// window bar, and reports whether it did.
func (state *State) paintBackground(canvas *ui.Canvas, size ui.Size) bool {
	bg := &state.bg
	if !state.showsBackground() {
		return false
	}
	// The picture fills what lies under the window bar.
	var under flex.Box
	f := state.paneChrome.Begin(canvas, state.Theme(), ui.Rect{Width: size.Width, Height: size.Height})
	f.Column().Fill().PadSides(windowBarHeight, 0, 0, 0).Children(func() {
		under = f.Slot().Fill()
	})
	f.End()
	area := under.Rect()
	if area.Width <= 0 || area.Height <= 0 {
		return false
	}
	size = ui.Size{Width: area.Width, Height: area.Height}
	source := ui.Rect{Width: float32(bg.width), Height: float32(bg.height)}
	if windowAspect, imageAspect := size.Width/size.Height, float32(bg.width)/float32(bg.height); imageAspect > windowAspect {
		source.Width = float32(bg.height) * windowAspect
		source.X = (float32(bg.width) - source.Width) / 2
	} else {
		source.Height = float32(bg.width) / windowAspect
		source.Y = (float32(bg.height) - source.Height) / 2
	}
	canvas.DrawBackdrop(ui.Bitmap{Pix: bg.pix, Width: bg.width, Height: bg.height, Stride: bg.width * 4, ID: bg.id}, source, area)
	return true
}

func (state *State) paintBackgroundSettings(page *settingsPage) {
	m, bg := state.messages.Tools, &state.bg
	if bg.text == "" || bg.textName != bg.name || bg.textW != bg.width || bg.textH != bg.height || bg.textLanguage != state.language || (bg.set && bg.pix == nil) != (bg.text == m.BgLoading) {
		bg.textName, bg.textW, bg.textH, bg.textLanguage = bg.name, bg.width, bg.height, state.language
		switch {
		case bg.set && bg.pix != nil:
			bg.text = fmt.Sprintf(m.BgRowSetFmt, bg.name, bg.width, bg.height)
		case bg.set:
			bg.text = m.BgLoading
		default:
			bg.text = m.BgRowNone
		}
	}
	description := bg.text
	options := [3]components.SegmentOption{
		{ID: hitBgSubtle, Label: m.BgSubtle}, {ID: hitBgMedium, Label: m.BgMedium}, {ID: hitBgStrong, Label: m.BgStrong},
	}
	selected := hitBgMedium
	switch bg.strength {
	case bgSubtle:
		selected = hitBgSubtle
	case bgStrong:
		selected = hitBgStrong
	}
	page.group(m.BgGroup, func() {
		page.actionRow(m.BgRow, description, false,
			components.ButtonAction{ID: hitBgChoose, Label: m.BgChoose, Variant: components.ButtonOutline},
			components.ButtonAction{ID: hitBgRemove, Label: m.BgRemove, Variant: components.ButtonOutline})
		page.row(m.BgVisibility, "", options[:], selected)
	})
}
