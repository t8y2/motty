package workbench

import (
	"github.com/vivaui/viva/ui"
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
func (state *State) switchTheme() ui.Theme {
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

	shell        ui.Color // window bar, sidebar and the frame around the workspace
	workspace    ui.Color // main content, dialogs
	surface      ui.Color // inputs, buttons, cards
	surfaceSoft  ui.Color // selected or hovered rows
	surfaceHot   ui.Color // pressed and active controls
	line         ui.Color
	windowStroke ui.Color
	terminal     ui.Color

	// Window-bar tabs and buttons sit on shell, so their fills are chosen
	// against it: a resting tab is a quiet pill, hover lifts it, the active
	// tab is the brightest.
	tab       ui.Color
	tabHover  ui.Color
	tabActive ui.Color
	// tabCloseHot fills the close button under the pointer on the active tab,
	// whose own fill is too close to line for that button to show a change.
	tabCloseHot ui.Color

	text  ui.Color
	muted ui.Color
	faint ui.Color

	accent     ui.Color
	accentHot  ui.Color
	accentSoft ui.Color // tinted backgrounds for accent icons
	onAccent   ui.Color

	success ui.Color
	warning ui.Color
	danger  ui.Color

	// cursor is the terminal's cursor block; ansi are its sixteen named
	// colors, chosen to stay legible on the terminal background.
	cursor ui.Color
	ansi   [16][3]uint8

	// The rest is the terminal's own look, filled in by rebuildTerminalLook: the
	// color of its text, the words it colors, and a number that changes with the
	// look so a prepared line knows it is stale.
	termFG    ui.Color
	highlight *highlight.Set
	epoch     uint16
}

// darkPalette is cool graphite with a near-white accent. The blue-leaning
// neutrals keep the window chrome crisp without approaching pure black, and the
// accent is the same ink the light palette uses, inverted, so primary actions
// and focus stay easy to find without a hue of their own.
var darkPalette = palette{
	dark: true,

	shell:        ui.RGB(24, 28, 35),
	workspace:    ui.RGB(17, 21, 27),
	surface:      ui.RGB(31, 36, 45),
	surfaceSoft:  ui.RGB(37, 43, 53),
	surfaceHot:   ui.RGB(60, 70, 87),
	line:         ui.RGB(51, 60, 74),
	windowStroke: ui.RGB(67, 78, 95),
	terminal:     ui.RGB(11, 14, 19),
	tab:          ui.RGB(31, 36, 44),
	tabHover:     ui.RGB(40, 47, 58),
	tabActive:    ui.RGB(51, 60, 73),
	tabCloseHot:  ui.RGB(71, 83, 101),

	text:  ui.RGB(226, 231, 238),
	muted: ui.RGB(158, 168, 182),
	faint: ui.RGB(113, 124, 140),

	accent:     ui.RGB(226, 231, 238),
	accentHot:  ui.RGB(255, 255, 255),
	accentSoft: ui.RGB(44, 50, 60),
	onAccent:   ui.RGB(17, 21, 27),

	success: ui.RGB(98, 206, 136),
	warning: ui.RGB(232, 192, 72),
	danger:  ui.RGB(240, 120, 120),

	cursor: ui.RGB(226, 231, 238),
	termFG: ui.RGB(226, 231, 238),
	ansi: [16][3]uint8{
		{84, 82, 78}, {240, 120, 120}, {98, 206, 136}, {232, 192, 72},
		{110, 160, 240}, {198, 140, 230}, {88, 200, 210}, {218, 215, 209},
		{122, 118, 111}, {255, 150, 150}, {140, 230, 168}, {245, 214, 110},
		{150, 190, 255}, {222, 170, 248}, {130, 224, 232}, {240, 238, 233},
	},
}

// lightPalette is daylight: a cool gray chrome around white content, with the
// same near-black accent the dark palette inverts. It is meant to sit quietly
// beside other light windows rather than to carry a tint of its own.
var lightPalette = palette{
	dark: false,

	shell:        ui.RGB(239, 239, 242),
	workspace:    ui.RGB(246, 246, 248),
	surface:      ui.RGB(255, 255, 255),
	surfaceSoft:  ui.RGB(241, 241, 244),
	surfaceHot:   ui.RGB(227, 227, 231),
	line:         ui.RGB(232, 232, 236),
	windowStroke: ui.RGB(214, 214, 219),
	terminal:     ui.RGB(250, 250, 252),
	tab:          ui.RGB(245, 245, 247),
	tabHover:     ui.RGB(249, 249, 251),
	tabActive:    ui.RGB(255, 255, 255),
	tabCloseHot:  ui.RGB(236, 236, 240),

	text:  ui.RGB(29, 29, 31),
	muted: ui.RGB(107, 107, 112),
	faint: ui.RGB(141, 141, 146),

	accent:     ui.RGB(29, 29, 31),
	accentHot:  ui.RGB(0, 0, 0),
	accentSoft: ui.RGB(233, 233, 237),
	onAccent:   ui.RGB(255, 255, 255),

	success: ui.RGB(21, 145, 71),
	warning: ui.RGB(161, 98, 7),
	danger:  ui.RGB(214, 40, 40),

	cursor: ui.RGB(29, 29, 31),
	termFG: ui.RGB(29, 29, 31),
	ansi: [16][3]uint8{
		{30, 30, 32}, {205, 49, 49}, {22, 130, 70}, {152, 104, 0},
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
var metrics, radius = workbenchMetrics()

type radii struct {
	item    float32 // a small row, chip or close button
	control float32 // a button, tab, field or list row
	popup   float32 // a floating panel or a card holding rows
	surface float32 // a dialog or a large panel
}

func workbenchMetrics() (ui.Metrics, radii) {
	m := ui.DefaultMetrics()
	return m, radii{item: m.RadiusItem(), control: m.RadiusControl, popup: m.RadiusPopup(), surface: m.RadiusSurface}
}

// theme maps the palette onto the component theme tokens so framework
// components follow the same colors without a runtime style parser.
func (pal *palette) theme() ui.Theme {
	theme := ui.DarkTheme()
	if !pal.dark {
		theme = ui.LightTheme()
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
	theme.Selection = ui.RGB(pal.ansi[4][0], pal.ansi[4][1], pal.ansi[4][2])
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
		theme.Input = ui.Color{Red: 1, Green: 1, Blue: 1, Alpha: 0.16}
	} else {
		theme.Input = pal.line
	}
	return theme
}
