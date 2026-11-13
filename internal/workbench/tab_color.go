package workbench

import (
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	layoutcore "motty/internal/workspace"
)

const (
	tabColorNone = "none"
	// The swatches must not decide how wide the menu is: they shrink to the
	// width the menu already has, so the menu keeps the width its labels ask for
	// (about 166px in Chinese) instead of being stretched by a row of colours.
	// 21px is the cap, where a swatch nearly fills a 24px menu row.
	tabColorSwatchSize   = float32(21)
	tabColorSwatchGap    = float32(2)
	tabColorPaletteInset = float32(2)
	// The footer holds the swatches in a single row of the same height as a menu
	// item, with the swatches centred vertically inside it.
	tabColorFooterRows   = 1
	tabColorFooterHeight = contextMenuRow * tabColorFooterRows

	// A tab's tint is its colour mixed with the tab's own fill, and the mix
	// amount is the share of the pure colour in the result. The old amounts were
	// 8%/12%/18%, which mixed all six saturated colours into the same grey — users
	// read that as the colour having been faded out. Every state is now clearly
	// higher, so a tab really shows which colour was picked. The ceiling comes from
	// ink contrast: with coloured tabs taking the strong ink, the worst of the six
	// offered colours still clears 4.9:1, and brighter connection colours lose one
	// step of colour in tabFill to stay readable.
	tabColorIdleTint   = float32(.24)
	tabColorHoverTint  = float32(.34)
	tabColorActiveTint = float32(.46)
	// The border sits closer to the pure colour than the fill does, so the tab's
	// outline stays the darkest stroke of that colour.
	tabColorBorderTint = float32(.55)

	// tabColorInkFloor is the least contrast a tab's name may fall to over its
	// tint. Painted names are small and medium weight, where 4.5:1 is the
	// readable floor. tabColorSoftening is the step a too-bright colour is mixed
	// in less by, and the steps bound how far that search runs.
	tabColorInkFloor       = float32(4.5)
	tabColorSoftening      = float32(.85)
	tabColorSofteningSteps = 12
)

// tabColorSwatchBorder is the border a swatch paints. The disc sits flush inside
// it: a background ring between border and disc reads as an empty centre and
// leaves the colour smaller than the space the swatch was given.
const tabColorSwatchBorder = float32(1)

// tabColorAmount is how much of a tab's colour its fill shows. The states stay
// apart — a hovered or active tab shows more of its colour, so a pointer and the
// current tab still read — but every state shows enough that the tab is the
// colour the user picked rather than a grey with a hint of it.
func tabColorAmount(active, hovered bool) float32 {
	switch {
	case active:
		return tabColorActiveTint
	case hovered:
		return tabColorHoverTint
	}
	return tabColorIdleTint
}

// tabColored reports whether a tab draws a colour at all. An unset or malformed
// colour leaves the tab in the palette's ordinary greys.
func tabColored(color string) bool {
	return hostAccent(color).Alpha > 0
}

// tabFill is the painted fill of a tab in a state whose ordinary fill is base:
// the tab's colour mixed into base by the state's amount.
//
// A name sits on that fill with the palette's strong ink, so a colour bright
// enough to sink the name below tabColorInkFloor is mixed in a step at a time
// less. Tinting a tab is for telling tabs apart; a tab whose name cannot be read
// tells the user less than the grey it replaced. Every colour the tab menu offers
// stays at full amount in both palettes, so this only softens the brightest
// connection colours.
func tabFill(pal *palette, base gfx.Color, color string, active, hovered bool) gfx.Color {
	if !tabColored(color) {
		return base
	}
	amount := tabColorAmount(active, hovered)
	for range tabColorSofteningSteps {
		fill := tintHostSurface(base, color, amount)
		if inkContrast(fill, pal.text) >= tabColorInkFloor {
			return fill
		}
		amount *= tabColorSoftening
	}
	return base
}

// tabInk is the ink of a tab's name over fill. A coloured tab takes the strong
// ink in every state, not the muted rest-state one: the tinted fills of the light
// palette are too bright for muted ink to stay readable, and the colour already
// says which tab the user is on, so the name does not have to stay quiet.
func tabInk(pal *palette, color string, rest gfx.Color) gfx.Color {
	if tabColored(color) {
		return pal.text
	}
	return rest
}

// tabNumberInk dims a tab's number beside its name, which keeps the number
// quiet. A coloured tab has no contrast to spare for that: the fill already caps
// what the ink can reach, so the number keeps the name's ink instead of fading
// into it.
func tabNumberInk(foreground gfx.Color, color string) gfx.Color {
	if tabColored(color) {
		return foreground
	}
	return tabNumberColor(foreground)
}

// inkContrast is the WCAG contrast of two colours, which is how a name over a
// tinted tab is judged. It compares the painted 8-bit colours, so a threshold
// means the same thing here as it does to an eye on the screen.
func inkContrast(a, b gfx.Color) float32 {
	return contrastRatio(rgb8(a), rgb8(b))
}

// tabColorSwatchGeometry is the border circle and the colour disc of a swatch.
// Both are square and concentric, and the disc is inset by exactly the border, so
// there is no gap between them however the row was sized.
func tabColorSwatchGeometry(bounds gfx.Rect) (border, disc gfx.Rect) {
	side := min(bounds.Width, bounds.Height)
	border = gfx.Rect{X: bounds.X + (bounds.Width-side)/2, Y: bounds.Y + (bounds.Height-side)/2, Width: side, Height: side}
	inner := max(side-2*tabColorSwatchBorder, 0)
	disc = gfx.Rect{X: border.X + tabColorSwatchBorder, Y: border.Y + tabColorSwatchBorder, Width: inner, Height: inner}
	return border, disc
}

var hitTabColors = tree.Sub(tabMenuHits, tree.Name("tab-colors"))

// The order matches the reference design; the empty swatch clears the colour
// explicitly rather than reverting to the inherited one.
var tabColors = [...]string{tabColorNone, "#df302a", "#859900", "#b58900", "#268bd2", "#d33682", "#2aa198"}

// tabColorSwatchLayout is the side of one disc and the gap between discs for a
// row of room px. A label in a short language sets the menu's width, so seven
// swatches have to fit inside it rather than push it wider — the colour row must
// not decide how wide the menu is. tabColorSwatchSize is the largest side that
// still sits inside the 24px menu row; a menu narrower than that shows
// proportionally smaller discs and keeps its labels whole.
func tabColorSwatchLayout(room float32) (size, gap float32) {
	// Too small a disc is not worth clicking; below it the gaps tighten too, which
	// buys back more width than shrinking the disc further would.
	const minimum = float32(12)
	count := float32(len(tabColors))
	gap = tabColorSwatchGap
	size = (room - (count-1)*gap) / count
	if size > tabColorSwatchSize {
		return tabColorSwatchSize, gap
	}
	if size < minimum {
		gap = 2
		size = max((room-(count-1)*gap)/count, minimum)
	}
	return size, gap
}

func (state *State) tabColor(workspace layoutcore.WorkspaceInfo) string {
	if workspace.Color == tabColorNone {
		return ""
	}
	if workspace.Color != "" {
		return workspace.Color
	}
	// An older workspace keeps its connection's colour; only picking a swatch
	// overrides it.
	if session := state.tabSession(workspace); session != nil {
		return session.host.Color
	}
	return ""
}

func (state *State) clickTabColor(workspace layoutcore.WorkspaceID, id gfx.ID) bool {
	index, ok := id.In(hitTabColors)
	if !ok || index < 0 || index >= len(tabColors) {
		return false
	}
	if state.deck.SetColor(workspace, tabColors[index]) == nil {
		state.rebuildTabOptions()
		state.save()
	}
	return true
}

func (state *State) paintTabColorChoices(canvas *gfx.Canvas, menu *components.ContextMenu) {
	menu.AddSeparator()
	footer := menu.NextItemBounds()
	footer.Height = tabColorFooterHeight
	for range tabColorFooterRows {
		menu.AddItem(components.ContextMenuItem{})
	}
	index, ok := state.workspaceTab(state.tabMenu.workspace)
	if !ok {
		return
	}
	m := state.messages
	labels := [...]string{m.ColorNone, m.ColorRed, m.ColorGreen, m.ColorYellow, m.ColorBlue, m.ColorPink, m.ColorTeal}
	selected := state.tabColor(state.workspaceTabs[index])
	bounds := menu.Bounds()
	menu.BeginContent()
	defer menu.EndContent()
	// Hand-drawn content must honour the menu's scroll clip too, so a small window
	// cannot receive clicks on a swatch outside the menu.
	canvas.Clip(gfx.Rect{X: bounds.X, Y: bounds.Y + contextMenuPadding/2, Width: bounds.Width, Height: bounds.Height - contextMenuPadding})
	defer canvas.Unclip()
	// The swatches lay themselves out across the row's own width and stay centred:
	// the labels set the menu's width, and the swatch row never widens it.
	room := footer.Width - 2*tabColorPaletteInset
	size, gap := tabColorSwatchLayout(room)
	palette := float32(len(tabColors))*size + float32(len(tabColors)-1)*gap
	left := footer.X + (footer.Width-palette)/2
	for index, color := range tabColors {
		swatch := gfx.Rect{X: left + float32(index)*(size+gap), Y: footer.Y + (footer.Height-size)/2, Width: size, Height: size}
		checked := strings.EqualFold(selected, color) || selected == "" && color == tabColorNone
		state.paintTabColorSwatch(canvas, hitTabColors.At(index), swatch, labels[index], color, checked)
	}
}

// paintTabColorSwatch paints one colour choice. Viva's ColorSwatch draws a
// background-coloured ring between border and disc, which at this size leaves the
// colour visibly smaller than the circle around it; here the disc fills the ring
// instead, so a swatch reads as one solid colour inside a hairline border. The
// selected border is the foreground colour at the same weight as any other, not a
// heavier ring of its own.
func (state *State) paintTabColorSwatch(canvas *gfx.Canvas, id gfx.ID, bounds gfx.Rect, label, color string, selected bool) {
	border, disc := tabColorSwatchGeometry(bounds)
	if disc.Width <= 0 {
		return
	}
	theme := state.Theme()
	borderColor := theme.Input
	if selected {
		borderColor = theme.Foreground
	}
	hovered := canvas.Hovered() == id
	if hovered && canvas.HoverCrossed() {
		// The menu records its "current item" as the row holding focus: while the
		// pointer rests on a swatch, the row it left still holds focus, so every
		// repaint lights that row again and the rows blink as the pointer moves
		// between swatches. Handing focus to the swatch at the moment the pointer
		// crosses onto it makes the swatch the current item instead. Requesting it
		// only on the crossing matches how Viva treats a menu row: while the pointer
		// stays put, the arrow keys can still move focus back to a menu row.
		canvas.RequestFocus(id)
	}
	if canvas.FocusVisible() == id || hovered {
		borderColor = theme.Ring
	}
	// The border circle is painted whole and the disc over its middle, so the ring
	// is exactly tabColorSwatchBorder wide whatever the swatch was sized to.
	canvas.Rounded(border, border.Width/2, borderColor)
	fill := hostAccent(color)
	if fill.Alpha == 0 {
		fill = theme.Muted
	}
	canvas.Rounded(disc, disc.Width/2, fill)
	if hostAccent(color).Alpha == 0 {
		canvas.DrawIcon(noTabColorIcon, insetRect(disc, 0.2), theme.MutedForeground)
	}
	canvas.HitWithOptions(id, bounds, label, gfx.HitOptions{Role: gfx.RoleRadio, Checked: selected, Selected: selected})
}

// noTabColorIcon is the diagonal a swatch without a colour carries, so "no
// colour" is told apart from "not selected" without relying on the ring alone.
var noTabColorIcon = gfx.Icon{ViewBox: 24, Stroke: 1.5, Cap: gfx.CapRound, Commands: []gfx.PathCommand{gfx.MoveTo(6, 18), gfx.LineTo(18, 6)}}
