package workbench

import (
	"github.com/vivaui/viva/flex"
	"github.com/vivaui/viva/ui"
)

// A terminal color scheme is the terminal's own look: its background and text,
// the cursor and the sixteen named colors. It does not touch the window around
// the terminal. The dark and the light look of the window each keep a scheme of
// their own, so the terminal changes with the window; a light scheme can still
// be chosen for the dark look and the other way round. The first scheme,
// Default, is whatever the application palette already uses.

type colorScheme struct {
	id         string
	name       string
	background [3]uint8
	foreground [3]uint8
	cursor     [3]uint8
	ansi       [16][3]uint8
}

const defaultSchemeID = "default"

// colorSchemes are the choices after Default, in the order Settings shows them.
var colorSchemes = [...]colorScheme{
	{
		id: "dracula", name: "Dracula",
		background: [3]uint8{0x28, 0x2a, 0x36}, foreground: [3]uint8{0xf8, 0xf8, 0xf2}, cursor: [3]uint8{0xf8, 0xf8, 0xf2},
		ansi: [16][3]uint8{
			{0x3b, 0x3d, 0x4d}, {0xff, 0x55, 0x55}, {0x50, 0xfa, 0x7b}, {0xf1, 0xfa, 0x8c},
			{0xbd, 0x93, 0xf9}, {0xff, 0x79, 0xc6}, {0x8b, 0xe9, 0xfd}, {0xf8, 0xf8, 0xf2},
			{0x62, 0x72, 0xa4}, {0xff, 0x6e, 0x6e}, {0x69, 0xff, 0x94}, {0xff, 0xff, 0xa5},
			{0xd6, 0xac, 0xff}, {0xff, 0x92, 0xdf}, {0xa4, 0xff, 0xff}, {0xff, 0xff, 0xff},
		},
	},
	{
		id: "nord", name: "Nord",
		background: [3]uint8{0x2e, 0x34, 0x40}, foreground: [3]uint8{0xd8, 0xde, 0xe9}, cursor: [3]uint8{0xd8, 0xde, 0xe9},
		ansi: [16][3]uint8{
			{0x3b, 0x42, 0x52}, {0xbf, 0x61, 0x6a}, {0xa3, 0xbe, 0x8c}, {0xeb, 0xcb, 0x8b},
			{0x81, 0xa1, 0xc1}, {0xb4, 0x8e, 0xad}, {0x88, 0xc0, 0xd0}, {0xe5, 0xe9, 0xf0},
			{0x4c, 0x56, 0x6a}, {0xd0, 0x7a, 0x82}, {0xb1, 0xcc, 0x9a}, {0xf0, 0xd8, 0xa0},
			{0x92, 0xb0, 0xce}, {0xc2, 0x9d, 0xbd}, {0x8f, 0xbc, 0xbb}, {0xec, 0xef, 0xf4},
		},
	},
	{
		id: "one-dark", name: "One Dark",
		background: [3]uint8{0x28, 0x2c, 0x34}, foreground: [3]uint8{0xab, 0xb2, 0xbf}, cursor: [3]uint8{0x52, 0x8b, 0xff},
		ansi: [16][3]uint8{
			{0x3f, 0x44, 0x51}, {0xe0, 0x6c, 0x75}, {0x98, 0xc3, 0x79}, {0xe5, 0xc0, 0x7b},
			{0x61, 0xaf, 0xef}, {0xc6, 0x78, 0xdd}, {0x56, 0xb6, 0xc2}, {0xab, 0xb2, 0xbf},
			{0x5c, 0x63, 0x70}, {0xf0, 0x7e, 0x87}, {0xa9, 0xd4, 0x8a}, {0xf0, 0xcf, 0x8d},
			{0x74, 0xbd, 0xf5}, {0xd6, 0x8a, 0xea}, {0x68, 0xc5, 0xd0}, {0xff, 0xff, 0xff},
		},
	},
	{
		id: "tokyo-night", name: "Tokyo Night",
		background: [3]uint8{0x1a, 0x1b, 0x26}, foreground: [3]uint8{0xc0, 0xca, 0xf5}, cursor: [3]uint8{0xc0, 0xca, 0xf5},
		ansi: [16][3]uint8{
			{0x32, 0x34, 0x4a}, {0xf7, 0x76, 0x8e}, {0x9e, 0xce, 0x6a}, {0xe0, 0xaf, 0x68},
			{0x7a, 0xa2, 0xf7}, {0xbb, 0x9a, 0xf7}, {0x7d, 0xcf, 0xff}, {0xa9, 0xb1, 0xd6},
			{0x41, 0x48, 0x68}, {0xff, 0x89, 0x9d}, {0xb0, 0xe0, 0x7c}, {0xf0, 0xbd, 0x7a},
			{0x8d, 0xb3, 0xff}, {0xca, 0xab, 0xff}, {0x8f, 0xdb, 0xff}, {0xc0, 0xca, 0xf5},
		},
	},
	{
		id: "gruvbox-dark", name: "Gruvbox Dark",
		background: [3]uint8{0x28, 0x28, 0x28}, foreground: [3]uint8{0xeb, 0xdb, 0xb2}, cursor: [3]uint8{0xeb, 0xdb, 0xb2},
		ansi: [16][3]uint8{
			{0x3c, 0x38, 0x36}, {0xcc, 0x24, 0x1d}, {0x98, 0x97, 0x1a}, {0xd7, 0x99, 0x21},
			{0x45, 0x85, 0x88}, {0xb1, 0x62, 0x86}, {0x68, 0x9d, 0x6a}, {0xa8, 0x99, 0x84},
			{0x92, 0x83, 0x74}, {0xfb, 0x49, 0x34}, {0xb8, 0xbb, 0x26}, {0xfa, 0xbd, 0x2f},
			{0x83, 0xa5, 0x98}, {0xd3, 0x86, 0x9b}, {0x8e, 0xc0, 0x7c}, {0xeb, 0xdb, 0xb2},
		},
	},
	{
		id: "monokai", name: "Monokai",
		background: [3]uint8{0x27, 0x28, 0x22}, foreground: [3]uint8{0xf8, 0xf8, 0xf2}, cursor: [3]uint8{0xf8, 0xf8, 0xf0},
		ansi: [16][3]uint8{
			{0x3e, 0x3d, 0x32}, {0xf9, 0x26, 0x72}, {0xa6, 0xe2, 0x2e}, {0xf4, 0xbf, 0x75},
			{0x66, 0xd9, 0xef}, {0xae, 0x81, 0xff}, {0xa1, 0xef, 0xe4}, {0xf8, 0xf8, 0xf2},
			{0x75, 0x71, 0x5e}, {0xff, 0x4d, 0x8d}, {0xb8, 0xee, 0x4f}, {0xff, 0xd0, 0x8c},
			{0x80, 0xe3, 0xf5}, {0xc0, 0x9d, 0xff}, {0xb8, 0xf6, 0xee}, {0xff, 0xff, 0xff},
		},
	},
	{
		id: "solarized-dark", name: "Solarized Dark",
		background: [3]uint8{0x00, 0x2b, 0x36}, foreground: [3]uint8{0x83, 0x94, 0x96}, cursor: [3]uint8{0x93, 0xa1, 0xa1},
		ansi: [16][3]uint8{
			{0x07, 0x36, 0x42}, {0xdc, 0x32, 0x2f}, {0x85, 0x99, 0x00}, {0xb5, 0x89, 0x00},
			{0x26, 0x8b, 0xd2}, {0xd3, 0x36, 0x82}, {0x2a, 0xa1, 0x98}, {0xee, 0xe8, 0xd5},
			{0x58, 0x6e, 0x75}, {0xec, 0x5f, 0x5c}, {0xa3, 0xb6, 0x00}, {0xd9, 0xa4, 0x00},
			{0x4a, 0xa3, 0xe8}, {0xe0, 0x50, 0x9a}, {0x3c, 0xc0, 0xb5}, {0xfd, 0xf6, 0xe3},
		},
	},
	{
		id: "solarized-light", name: "Solarized Light",
		background: [3]uint8{0xfd, 0xf6, 0xe3}, foreground: [3]uint8{0x65, 0x7b, 0x83}, cursor: [3]uint8{0x58, 0x6e, 0x75},
		ansi: [16][3]uint8{
			{0x07, 0x36, 0x42}, {0xdc, 0x32, 0x2f}, {0x85, 0x99, 0x00}, {0xb5, 0x89, 0x00},
			{0x26, 0x8b, 0xd2}, {0xd3, 0x36, 0x82}, {0x2a, 0xa1, 0x98}, {0xee, 0xe8, 0xd5},
			{0x58, 0x6e, 0x75}, {0xcb, 0x4b, 0x16}, {0x6b, 0x81, 0x00}, {0x9a, 0x73, 0x00},
			{0x1f, 0x77, 0xb8}, {0xb0, 0x2a, 0x6e}, {0x1f, 0x8a, 0x82}, {0x93, 0xa1, 0xa1},
		},
	},
	{
		id: "github-light", name: "GitHub Light",
		background: [3]uint8{0xff, 0xff, 0xff}, foreground: [3]uint8{0x24, 0x29, 0x2f}, cursor: [3]uint8{0x09, 0x69, 0xda},
		ansi: [16][3]uint8{
			{0x24, 0x29, 0x2f}, {0xcf, 0x22, 0x2e}, {0x11, 0x63, 0x29}, {0x4d, 0x2d, 0x00},
			{0x09, 0x69, 0xda}, {0x82, 0x50, 0xdf}, {0x1b, 0x7c, 0x83}, {0x6e, 0x77, 0x81},
			{0x57, 0x60, 0x6a}, {0xa4, 0x0e, 0x26}, {0x1a, 0x7f, 0x37}, {0x63, 0x3c, 0x01},
			{0x21, 0x8b, 0xff}, {0xa4, 0x75, 0xf9}, {0x31, 0x92, 0xaa}, {0x8c, 0x95, 0x9f},
		},
	},
	{
		id: "one-light", name: "One Light",
		background: [3]uint8{0xfa, 0xfa, 0xfa}, foreground: [3]uint8{0x38, 0x3a, 0x42}, cursor: [3]uint8{0x52, 0x6f, 0xff},
		ansi: [16][3]uint8{
			{0x38, 0x3a, 0x42}, {0xe4, 0x56, 0x49}, {0x50, 0xa1, 0x4f}, {0xc1, 0x84, 0x01},
			{0x40, 0x78, 0xf2}, {0xa6, 0x26, 0xa4}, {0x01, 0x84, 0xbc}, {0xa0, 0xa1, 0xa7},
			{0x69, 0x6c, 0x77}, {0xe4, 0x56, 0x49}, {0x50, 0xa1, 0x4f}, {0x98, 0x68, 0x01},
			{0x40, 0x78, 0xf2}, {0xa6, 0x26, 0xa4}, {0x01, 0x84, 0xbc}, {0x56, 0x58, 0x61},
		},
	},
}

// schemeCount is how many choices Settings offers, Default included.
const schemeCount = len(colorSchemes) + 1

func schemeByID(id string) (*colorScheme, bool) {
	for index := range colorSchemes {
		if colorSchemes[index].id == id {
			return &colorSchemes[index], true
		}
	}
	return nil, false
}

// normalizeSchemeID keeps a saved choice that still exists and falls back to
// Default for one a later or earlier release does not know.
func normalizeSchemeID(id string) string {
	if _, ok := schemeByID(id); ok {
		return id
	}
	return defaultSchemeID
}

// activeScheme is the scheme the terminal wears in the look that is showing.
func (state *State) activeScheme() string {
	return state.terminalSchemes[state.appearance]
}

// migrateTerminalScheme places the single scheme an earlier release saved. It
// sat in the terminal whatever the window looked like, so it goes to the look
// its own background suits and the other look keeps Default: nothing changes
// on screen until the user switches.
func migrateTerminalScheme(id string) (dark, light string) {
	dark, light = defaultSchemeID, defaultSchemeID
	scheme, ok := schemeByID(id)
	switch {
	case !ok:
	case luminance(scheme.background) < 0.5:
		dark = id
	default:
		light = id
	}
	return dark, light
}

// rebuildTerminalLook derives the terminal's palette from the window palette,
// the chosen scheme and the highlight rules, and starts a new epoch so every
// prepared line is built again with it.
func (state *State) rebuildTerminalLook() {
	look := *state.pal
	look.termFG = look.text
	if scheme, ok := schemeByID(state.activeScheme()); ok {
		look.dark = luminance(scheme.background) < 0.5
		look.terminal = ui.RGB(scheme.background[0], scheme.background[1], scheme.background[2])
		look.termFG = ui.RGB(scheme.foreground[0], scheme.foreground[1], scheme.foreground[2])
		look.cursor = ui.RGB(scheme.cursor[0], scheme.cursor[1], scheme.cursor[2])
		look.ansi = scheme.ansi
	}
	look.highlight = state.highlights
	state.termEpoch++
	look.epoch = state.termEpoch
	state.termPal = look
	for index := range state.sessions {
		state.reportTerminalColors(state.sessions[index].term)
	}
	if login := state.ai.login; login != nil {
		state.reportTerminalColors(login.term)
	}
}

// reportTerminalColors tells a terminal the colors it is drawn with, which is
// what it answers a program that asks for them (OSC 10 and 11). Programs such as
// Codex or Helix pick a light or a dark theme from that answer.
func (state *State) reportTerminalColors(t *terminal) {
	if t == nil {
		return
	}
	t.screen.SetDefaultColors(rgb8(state.termPal.termFG), rgb8(state.termPal.terminal))
}

// rgb8 is the 8-bit channels of an opaque color, the inverse of ui.RGB.
func rgb8(c ui.Color) [3]uint8 {
	channel := func(v float32) uint8 { return uint8(min(max(v, 0), 1)*255 + 0.5) }
	return [3]uint8{channel(c.Red), channel(c.Green), channel(c.Blue)}
}

func luminance(c [3]uint8) float32 {
	return (0.2126*float32(c[0]) + 0.7152*float32(c[1]) + 0.0722*float32(c[2])) / 255
}

func (state *State) setTerminalScheme(id string) bool {
	id = normalizeSchemeID(id)
	if state.activeScheme() == id {
		return false
	}
	state.terminalSchemes[state.appearance] = id
	state.rebuildTerminalLook()
	return true
}

// Scheme cards in Settings.
const (
	schemeCardHeight = float32(54)
	schemeCardGap    = float32(10)
	schemeColumns    = 4

	// swatchSize and swatchPitch are the dots that show a scheme's four colors, and
	// the distance from one to the next.
	swatchSize  = float32(12)
	swatchPitch = float32(18)
)

// schemeHits is the place of the controls this file paints.
var schemeHits = tree.Feature("scheme")

// hitSettingsSchemeList starts the ids of the scheme cards: Default is the base
// itself, the others follow in colorSchemes order.
var hitSettingsSchemeList = tree.Sub(schemeHits, tree.Name("settings-scheme"))

func schemeHit(index int) ui.ID { return hitSettingsSchemeList.At(index) }

// schemeAt is the id and name of the card at index, Default first.
func (state *State) schemeAt(index int) (id, name string, scheme *colorScheme) {
	if index == 0 {
		return defaultSchemeID, state.messages.Tools.SchemeDefault, nil
	}
	scheme = &colorSchemes[index-1]
	return scheme.id, scheme.name, scheme
}

// paintSchemeSettings is the card grid that picks the terminal's colors.
func (state *State) paintSchemeSettings(page *settingsPage) {
	m, f, pal := state.messages.Tools, page.f, state.pal
	rows := (schemeCount + schemeColumns - 1) / schemeColumns
	page.group(m.SchemeGroup, func() {
		page.rows++
		f.Column().PadSides(14, 18, 22, 18).Children(func() {
			f.Text(m.SchemeDescription).FontSize(metrics.FontSizeS).Color(pal.muted).LineHeight(20).MarginSides(0, 0, 6, 0)
			look := m.SchemeLookDark
			if state.appearance == appearanceLight {
				look = m.SchemeLookLight
			}
			f.Text(look).FontSize(metrics.FontSizeS).Weight(ui.WeightSemibold).Color(pal.text).LineHeight(20).MarginSides(0, 0, 14, 0)
			for row := 0; row < rows; row++ {
				f.Row().Height(schemeCardHeight).Gap(schemeCardGap).MarginSides(0, 0, schemeCardGap, 0).Children(func() {
					for column := 0; column < schemeColumns; column++ {
						if index := row*schemeColumns + column; index < schemeCount {
							state.declareScheme(page, index)
						} else {
							f.Spacer().Basis(0) // an incomplete last row keeps its cards the width of the others
						}
					}
				})
			}
		})
	})
}

// declareScheme declares one color scheme's card: its name and the four colors
// it gives the terminal, on its own background.
func (state *State) declareScheme(page *settingsPage, index int) {
	f, pal := page.f, state.pal
	id, name, scheme := state.schemeAt(index)
	selected := state.activeScheme() == id
	background, foreground, swatches := pal.terminal, pal.text, [4]ui.Color{pal.danger, pal.success, pal.warning, pal.accent}
	if scheme != nil {
		background = ui.RGB(scheme.background[0], scheme.background[1], scheme.background[2])
		foreground = ui.RGB(scheme.foreground[0], scheme.foreground[1], scheme.foreground[2])
		for slot, ansi := range [4]int{9, 10, 11, 12} {
			swatches[slot] = ui.RGB(scheme.ansi[ansi][0], scheme.ansi[ansi][1], scheme.ansi[ansi][2])
		}
	}
	border := withAlpha(pal.line, 0.9)
	width := float32(1)
	if selected {
		border, width = pal.accent, 2
	}
	card := f.Column().Grow(1).Basis(0).PadSides(7-width, 10-width, 0, 10-width).Background(background).Radius(radius.popup).Border(width, border)
	card.Children(func() {
		// The border is the window's accent, which can be the card's own color, so
		// the choice is also marked with a check in the card's text color: that one
		// always reads on the card.
		f.Row().Items(flex.Center).MarginSides(0, 0, 6, 0).Children(func() {
			f.Text(name).FontSize(metrics.FontSizeS).Weight(ui.WeightSemibold).Color(foreground).LineHeight(18).Grow(1)
			if selected {
				f.Icon(lucideIcons[iconCheck]).Size(14, 14).Color(foreground).Shrink(0)
			}
		})
		view := &state.settings
		view.colors = append(view.colors, swatches[:]...)
		page.add(settingsControl{kind: controlSwatches, slot: f.Slot().Size(swatchPitch*3+swatchSize, swatchSize).Shrink(0), first: len(view.colors) - len(swatches), count: len(swatches)})
	})
	page.hitOn(card, schemeHit(index), name, ui.HitOptions{Role: ui.RoleButton, Selected: selected})
}
