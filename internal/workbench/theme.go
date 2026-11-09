package workbench

import (
	"github.com/vivaui/viva/gfx"
	"motty/internal/highlight"
)

// appearance selects one of the two palettes.
type appearance uint8

const (
	appearanceDark appearance = iota
	appearanceLight
)

func (a appearance) String() string {
	if a == appearanceLight {
		return "light"
	}
	return "dark"
}

func (a appearance) toggled() appearance {
	if a == appearanceLight {
		return appearanceDark
	}
	return appearanceLight
}

// appearanceMode is how the look is chosen: one the user picked, or the system's.
type appearanceMode uint8

const (
	// appearancePinnedDark is the default: an SSH window opens dark whatever
	// the system is showing, until the user asks for another look.
	appearancePinnedDark appearanceMode = iota
	appearancePinnedLight
	appearanceFollowSystem
)

// saved is the mode as the state file keeps it. Dark is the default and is kept
// as nothing, so a state written before the default was dark, or one that never
// chose, opens dark again. Following the system is a choice and is written as
// "system", or it would read back as the default on the next start.
func (m appearanceMode) saved() string {
	switch m {
	case appearancePinnedLight:
		return "light"
	case appearanceFollowSystem:
		return "system"
	}
	return ""
}

func appearanceModeFromString(value string) appearanceMode {
	switch value {
	case "light":
		return appearancePinnedLight
	case "dark":
		return appearancePinnedDark
	case "system":
		return appearanceFollowSystem
	}
	return appearancePinnedDark
}

// switchTheme is the theme a switch paints with. Its on track is the palette's
// success green rather than the accent the primary buttons use, so an enabled
// setting reads at a glance; the thumb keeps the contrast it has on a primary
// background.
func (state *State) switchTheme() gfx.Theme {
	theme := state.Theme()
	theme.Primary = state.pal.success
	// The thumb keeps the colour it has when the switch is off. Without this the
	// dark theme turns it dark on a light green track, which reads as a hole.
	theme.PrimaryForeground = theme.Foreground
	return theme
}

// palette is every color the workbench paints with. Switching the theme swaps
// one pointer; nothing else about painting changes.
type palette struct {
	dark bool

	shell        gfx.Color // window bar, sidebar and the frame around the workspace
	workspace    gfx.Color // main content, dialogs
	surface      gfx.Color // inputs, buttons, cards
	surfaceSoft  gfx.Color // selected or hovered rows
	surfaceHot   gfx.Color // pressed and active controls
	line         gfx.Color
	windowStroke gfx.Color
	terminal     gfx.Color

	// Window-bar tabs and buttons sit on shell, so their fills are chosen
	// against it: a resting tab is a quiet pill, hover lifts it, the active
	// tab is the brightest.
	tab       gfx.Color
	tabHover  gfx.Color
	tabActive gfx.Color
	// tabCloseHot fills the close button under the pointer on the active tab,
	// whose own fill is too close to line for that button to show a change.
	tabCloseHot gfx.Color

	text  gfx.Color
	muted gfx.Color
	faint gfx.Color

	accent     gfx.Color
	accentHot  gfx.Color
	accentSoft gfx.Color // tinted backgrounds for accent icons
	onAccent   gfx.Color

	success gfx.Color
	warning gfx.Color
	danger  gfx.Color

	// cursor is the terminal's cursor block; ansi are its sixteen named
	// colors, chosen to stay legible on the terminal background.
	cursor gfx.Color
	ansi   [16][3]uint8

	// The rest is the terminal's own look, filled in by rebuildTerminalLook: the
	// color of its text, the words it colors, and a number that changes with the
	// look so a prepared line knows it is stale.
	termFG    gfx.Color
	highlight *highlight.Set
	intense   terminalIntense
	epoch     uint16
}

// darkPalette is cool graphite with a near-white accent. The blue-leaning
// neutrals keep the window chrome crisp without approaching pure black, and the
// accent is the same ink the light palette uses, inverted, so primary actions
// and focus stay easy to find without a hue of their own.
var darkPalette = palette{
	dark: true,

	shell:        gfx.RGB(24, 28, 35),
	workspace:    gfx.RGB(17, 21, 27),
	surface:      gfx.RGB(31, 36, 45),
	surfaceSoft:  gfx.RGB(37, 43, 53),
	surfaceHot:   gfx.RGB(60, 70, 87),
	line:         gfx.RGB(51, 60, 74),
	windowStroke: gfx.RGB(67, 78, 95),
	terminal:     gfx.RGB(11, 14, 19),
	tab:          gfx.RGB(31, 36, 44),
	tabHover:     gfx.RGB(40, 47, 58),
	tabActive:    gfx.RGB(51, 60, 73),
	tabCloseHot:  gfx.RGB(71, 83, 101),

	text:  gfx.RGB(226, 231, 238),
	muted: gfx.RGB(158, 168, 182),
	faint: gfx.RGB(113, 124, 140),

	accent:     gfx.RGB(226, 231, 238),
	accentHot:  gfx.RGB(255, 255, 255),
	accentSoft: gfx.RGB(44, 50, 60),
	onAccent:   gfx.RGB(17, 21, 27),

	success: gfx.RGB(98, 206, 136),
	warning: gfx.RGB(232, 192, 72),
	danger:  gfx.RGB(240, 120, 120),

	cursor: gfx.RGB(226, 231, 238),
	termFG: gfx.RGB(226, 231, 238),
	ansi: [16][3]uint8{
		{84, 82, 78}, {240, 120, 120}, {98, 206, 136}, {232, 192, 72},
		{110, 160, 240}, {198, 140, 230}, {88, 200, 210}, {218, 215, 209},
		{122, 118, 111}, {255, 150, 150}, {140, 230, 168}, {245, 214, 110},
		{150, 190, 255}, {222, 170, 248}, {130, 224, 232}, {240, 238, 233},
	},
}

// lightPalette is daylight, kept off pure white: every ground sits a few steps
// below 255 so a large light window does not glare, and the content stays one
// step lighter than the chrome around it. The ink is the same near-black accent
// the dark palette inverts, a little softened. It is meant to sit quietly beside
// other light windows rather than to carry a tint of its own.
var lightPalette = palette{
	dark: false,

	shell:        gfx.RGB(231, 231, 235),
	workspace:    gfx.RGB(240, 240, 243),
	surface:      gfx.RGB(248, 248, 250),
	surfaceSoft:  gfx.RGB(235, 235, 239),
	surfaceHot:   gfx.RGB(221, 221, 226),
	line:         gfx.RGB(220, 220, 225),
	windowStroke: gfx.RGB(204, 204, 210),
	terminal:     gfx.RGB(244, 244, 247),
	tab:          gfx.RGB(238, 238, 241),
	tabHover:     gfx.RGB(243, 243, 246),
	tabActive:    gfx.RGB(248, 248, 250),
	tabCloseHot:  gfx.RGB(228, 228, 233),

	text:  gfx.RGB(38, 38, 42),
	muted: gfx.RGB(94, 94, 100),
	faint: gfx.RGB(134, 134, 140),

	accent:     gfx.RGB(38, 38, 42),
	accentHot:  gfx.RGB(0, 0, 0),
	accentSoft: gfx.RGB(226, 226, 231),
	onAccent:   gfx.RGB(248, 248, 250),

	success: gfx.RGB(21, 145, 71),
	warning: gfx.RGB(150, 90, 4),
	danger:  gfx.RGB(214, 40, 40),

	cursor: gfx.RGB(38, 38, 42),
	termFG: gfx.RGB(38, 38, 42),
	ansi: [16][3]uint8{
		{38, 38, 42}, {205, 49, 49}, {22, 130, 70}, {152, 104, 0},
		{36, 98, 198}, {160, 60, 170}, {0, 128, 140}, {110, 110, 112},
		{130, 130, 133}, {230, 70, 70}, {20, 160, 80}, {190, 130, 0},
		{66, 128, 232}, {188, 90, 200}, {20, 150, 164}, {90, 90, 92},
	},
}

func (a appearance) palette() *palette {
	if a == appearanceLight {
		return &lightPalette
	}
	return &darkPalette
}

// metrics are the size tokens of every framework component in the workbench, and
// radius is every corner the workbench draws itself, derived from them, so one
// change to the corners reaches the framework's controls and the workbench's own
// tabs, rows and cards alike.
// textScaleFactors are the interface text sizes a user can choose: the whole
// window's text drawn a step smaller or larger, its layout intact. The terminal
// has a font size of its own and does not follow.
const (
	textScaleSmallFactor   = float32(0.9)
	textScaleRegularFactor = float32(1)
	textScaleLargeFactor   = float32(1.15)
)

type radii struct {
	item    float32 // a small row, chip or close button
	control float32 // a button, tab, field or list row
	popup   float32 // a floating panel or a card holding rows
	surface float32 // a dialog or a large panel
}

var metrics, radius = workbenchMetrics(textScaleRegularFactor)

// barTitleSize is the size of the name in a pane's bar. It follows the text
// scale, as every text the window paints does.
var barTitleSize = float32(13)

func workbenchMetrics(scale float32) (gfx.Metrics, radii) {
	m := gfx.DefaultMetrics()
	if scale != textScaleRegularFactor {
		m.FontSizeS *= scale
		m.FontSizeM *= scale
		m.FontSizeL *= scale
	}
	return m, radii{item: m.RadiusItem(), control: m.RadiusControl, popup: m.RadiusPopup(), surface: m.RadiusSurface}
}

// applyTextMetrics rebuilds the size tokens every component paints with, for a
// scale the user has just chosen. Everything that follows the metrics or the
// tokens derived from them changes in the next paint; nothing cached survives.
func applyTextMetrics(scale float32) {
	metrics, radius = workbenchMetrics(scale)
	barTitleSize = 13 * scale
}

// theme maps the palette onto the component theme tokens so framework
// components follow the same colors without a runtime style parser.
func (pal *palette) theme() gfx.Theme {
	theme := gfx.DarkTheme()
	if !pal.dark {
		theme = gfx.LightTheme()
	}
	theme.Metrics = metrics
	theme.Background = pal.workspace
	theme.Foreground = pal.text
	theme.Card = pal.surface
	theme.CardForeground = pal.text
	theme.CardElevated = pal.surface
	theme.Popover = pal.surface
	theme.PopoverForeground = pal.text
	theme.Muted = pal.surfaceSoft
	theme.MutedForeground = pal.muted
	theme.Border = pal.line
	theme.Hover = pal.surfaceSoft
	theme.Primary = pal.accent
	theme.PrimaryForeground = pal.onAccent
	theme.Selection = gfx.RGB(pal.ansi[4][0], pal.ansi[4][1], pal.ansi[4][2])
	theme.Selection.Alpha = 0.30
	if !pal.dark {
		theme.Selection.Alpha = 0.22
	}
	theme.SelectionForeground = pal.text
	theme.Secondary = pal.surfaceHot
	theme.SecondaryForeground = pal.text
	theme.Accent = pal.surfaceHot
	theme.AccentForeground = pal.text
	theme.Ring = pal.accent
	theme.Success = pal.success
	theme.Danger = pal.danger
	if pal.dark {
		theme.Input = gfx.Color{Red: 1, Green: 1, Blue: 1, Alpha: 0.16}
	} else {
		theme.Input = pal.line
	}
	return theme
}
