package workbench

import (
	"slices"
	"sync"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
)

// iconColor is a device preference, independent of the interface's light/dark
// appearance and terminal colors. Stable names keep saved choices portable.
type iconColor uint8

const (
	iconMint iconColor = iota
	iconBlue
	iconRed
	iconViolet
	iconOrange
	iconGraphite
	iconColorCount
)

var iconColorNames = [...]string{"mint", "blue", "red", "violet", "orange", "graphite"}

func iconColorFromString(name string) iconColor {
	// Test builds offered sky blue in this slot before China red replaced it.
	if name == "sky" {
		return iconRed
	}
	for color, candidate := range iconColorNames {
		if name == candidate {
			return iconColor(color)
		}
	}
	return iconMint
}

func (color iconColor) normalized() iconColor {
	if color >= iconColorCount {
		return iconMint
	}
	return color
}

func (color iconColor) saved() string { return iconColorNames[color.normalized()] }

func (state *State) iconColorLabel(color iconColor) string {
	m := state.messages
	return [...]string{m.SettingsIconMint, m.SettingsIconBlue, m.SettingsIconRed,
		m.SettingsIconViolet, m.SettingsIconOrange, m.SettingsIconGraphite}[color.normalized()]
}

type iconArtwork struct {
	window      app.WindowIcon
	dark, light gfx.Bitmap
}

// Keep brand variants apart from file icons' sequential IDs under 1<<60.
func (color iconColor) bitmapID(part uint64) uint64 {
	return uint64(1<<60|1<<55) + uint64(color.normalized())*3 + part
}

var iconPreviews [iconColorCount]struct {
	once   sync.Once
	bitmap gfx.Bitmap
}

// previewBitmap keeps opening the picker cheap: only its small thumbnails are
// generated. High-resolution window icons and wordmarks wait until a color is
// actually applied.
func (color iconColor) previewBitmap() gfx.Bitmap {
	color = color.normalized()
	cache := &iconPreviews[color]
	cache.once.Do(func() {
		bitmap := aboutIconTile()
		if color != iconMint {
			bitmap.Pix = slices.Clone(bitmap.Pix)
			color.recolor(bitmap.Pix, true)
			bitmap.ID = color.bitmapID(2)
		}
		cache.bitmap = bitmap
	})
	return cache.bitmap
}

var iconArt [iconColorCount]struct {
	once sync.Once
	art  iconArtwork
}

// artwork recolors the existing artwork once per choice. The mark's shading,
// alpha, neutral background and wordmark are preserved, and painting reuses the
// cached bitmaps with distinct renderer identities.
func (color iconColor) artwork() *iconArtwork {
	color = color.normalized()
	cache := &iconArt[color]
	cache.once.Do(func() {
		art := iconArtwork{window: AppIcon(), dark: lockupDark(), light: lockupLight()}
		if color != iconMint {
			art.window.RGBA = slices.Clone(art.window.RGBA)
			color.recolor(art.window.RGBA, false)
			art.dark.Pix, art.light.Pix = slices.Clone(art.dark.Pix), slices.Clone(art.light.Pix)
			color.recolor(art.dark.Pix, true)
			color.recolor(art.light.Pix, true)
			art.dark.ID, art.light.ID = color.bitmapID(0), color.bitmapID(1)
		}
		cache.art = art
	})
	return &cache.art
}

// recolor changes only the green mark, preserving its value, alpha, highlights
// and shadows, including premultiplied BGRA. China red increases saturation.
func (color iconColor) recolor(pixels []byte, bgra bool) {
	r, b := 0, 2
	if bgra {
		r, b = b, r
	}
	for offset := 0; offset+4 <= len(pixels); offset += 4 {
		p := pixels[offset : offset+4]
		red, green, blue := float32(p[r]), float32(p[1]), float32(p[b])
		if green <= red || green <= blue || green-min(red, blue) < 4 {
			continue
		}
		low := min(red, blue)
		chroma := green - low
		var nr, ng, nb float32
		switch color {
		case iconBlue: // hue 220
			nr, ng, nb = low, low+chroma/3, green
		case iconRed: // warm, saturated China red, hue about 7
			base := max(0, low-chroma*0.75)
			nr, ng, nb = green, base+(green-base)*0.12, base
		case iconViolet: // hue 265
			nr, ng, nb = low+chroma*5/12, low, green
		case iconOrange: // hue 28
			nr, ng, nb = green, low+chroma*7/15, low
		case iconGraphite:
			nr = red*0.2126 + green*0.7152 + blue*0.0722
			ng, nb = nr, nr
		default:
			continue
		}
		p[r], p[1], p[b] = byte(nr+0.5), byte(ng+0.5), byte(nb+0.5)
	}
}

// WindowIcon is the chosen artwork for a new or running window.
func (state *State) WindowIcon() app.WindowIcon { return state.iconColor.artwork().window }

func (state *State) setIconColor(color iconColor) bool {
	color = color.normalized()
	if state.iconColor == color {
		return false
	}
	state.iconColor = color
	if state.options.SetAppIcon != nil {
		state.options.SetAppIcon(state.WindowIcon())
	}
	return true
}
