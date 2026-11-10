package workbench

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// iconButtonStyle selects how a square icon button reads at rest.
type iconButtonStyle uint8

const (
	iconButtonQuiet  iconButtonStyle = iota // muted until the pointer is on it
	iconButtonDanger                        // a destructive or recording action
	iconButtonActive                        // a switch that is on
	iconButtonUpdate                        // a green ring around a solid arrow: an update is waiting
)

// iconButtonLook is how one iconButtonStyle draws at rest: the glyph, the fill a
// switch keeps while it is on, and the shape of the button.
type iconButtonLook struct {
	foreground gfx.Color
	soft       gfx.Color
	shape      float32
}

// iconButtonAppearance resolves the resting colors and shape of a style.
func (state *State) iconButtonAppearance(style iconButtonStyle) iconButtonLook {
	pal := state.pal
	look := iconButtonLook{foreground: pal.muted, shape: radius.control}
	switch style {
	case iconButtonDanger:
		look.foreground = pal.danger
	case iconButtonActive:
		look.foreground, look.soft = pal.accent, pal.accentSoft
	case iconButtonUpdate:
		look.foreground, look.shape = updateGreen, 999
	}
	return look
}

// declareIconButton declares a square icon button in a page, at the place the
// caller's layout gives it: the page form of paintIconButton, so the control is
// in the page's hit and Tab order. The caller sizes and margins it. The hint is
// both what assistive technology reads and the tooltip that appears when the
// pointer rests on it: an icon alone does not say what it does.
func (state *State) declareIconButton(c ui.C, id gfx.ID, icon uint8, hint string, disabled bool, style iconButtonStyle) ui.Box {
	return state.declareIconButtonWithOptions(c, id, icon, hint, disabled, style, 0, gfx.HitOptions{})
}

func (state *State) declareIconButtonWithOptions(c ui.C, id gfx.ID, icon uint8, hint string, disabled bool, style iconButtonStyle, iconSize float32, options gfx.HitOptions) ui.Box {
	pal := state.pal
	look := state.iconButtonAppearance(style)
	foreground, soft := look.foreground, look.soft
	if disabled {
		foreground.Alpha *= 0.4
		soft.Alpha *= 0.4
	}
	options.Role, options.Disabled, options.Hint = gfx.RoleButton, disabled, hint
	button := ui.ButtonBase(c, hint).Radius(look.shape).Shrink(0)
	if soft.Alpha > 0 {
		button = button.Background(soft)
	}
	if !disabled {
		hover := withAlpha(pal.surfaceHot, 0.7)
		pressed := pal.surfaceHot
		if soft.Alpha > 0 {
			hover = hover.Over(soft)
			pressed = pressed.Over(soft)
		}
		hoverLook := ui.Look{}.Background(hover)
		pressedLook := ui.Look{}.Background(pressed)
		if style == iconButtonQuiet {
			hoverLook = hoverLook.Color(pal.text)
			pressedLook = pressedLook.Color(pal.text)
		}
		button = button.Hover(hoverLook).Pressed(pressedLook).
			Focus(ui.Look{}.Ring(1.5, withAlpha(pal.accent, 0.5)))
	}
	button = button.HitWith(id, hint, options)
	button.Children(func() {
		glyphIcon := lucideIcons[icon]
		if style == iconButtonUpdate {
			glyphIcon = updateIcon
		}
		glyph := ui.Icon(c, glyphIcon).Color(foreground)
		if iconSize > 0 {
			glyph.Size(iconSize, iconSize)
		}
		if style == iconButtonQuiet && !disabled {
			// The wordless button brightens with the pointer; the glyph follows
			// the button's state rather than its own, which the pointer never hits.
			glyph.StateFrom(button).
				Hover(ui.Look{}.Color(pal.text)).
				Pressed(ui.Look{}.Color(pal.text))
		}
	})
	return button
}

// paintIconButton draws a square icon button on the surface behind for a control
// whose place is computed while painting, such as a scrolling tool rail or a
// menu row. A control the page lays out is declared with declareIconButton
// instead, so it belongs to the page's hit and Tab order.
func (state *State) paintIconButton(canvas *gfx.Canvas, id gfx.ID, icon uint8, bounds gfx.Rect, hint string, disabled bool, style iconButtonStyle, behind gfx.Color) {
	state.paintIconButtonSized(canvas, id, icon, bounds, hint, disabled, style, behind, 0)
}

// paintIconButtonSized is paintIconButton with an optional explicit glyph size.
// A zero size keeps the component's default for existing compact controls.
func (state *State) paintIconButtonSized(canvas *gfx.Canvas, id gfx.ID, icon uint8, bounds gfx.Rect, hint string, disabled bool, style iconButtonStyle, behind gfx.Color, iconSize float32) {
	state.paintIconButtonWithOptions(canvas, id, icon, bounds, hint, disabled, style, behind, iconSize, gfx.HitOptions{})
}

func (state *State) paintIconButtonWithOptions(canvas *gfx.Canvas, id gfx.ID, icon uint8, bounds gfx.Rect, hint string, disabled bool, style iconButtonStyle, behind gfx.Color, iconSize float32, options gfx.HitOptions) {
	if bounds.Width <= 0 || bounds.Height <= 0 || int(icon) >= len(lucideIcons) {
		return
	}
	pal := state.pal
	look := state.iconButtonAppearance(style)
	foreground, soft, shape := look.foreground, look.soft, look.shape
	if style == iconButtonUpdate {
		shape = min(bounds.Width, bounds.Height) / 2
	}
	hovered := !disabled && canvas.Hovered() == id
	pressed := !disabled && canvas.Pressed() == id
	focused := !disabled && canvas.FocusVisible() == id
	var wash gfx.Color
	switch {
	case pressed:
		wash = pal.surfaceHot
		if style == iconButtonQuiet {
			foreground = pal.text
		}
	case hovered:
		wash = withAlpha(pal.surfaceHot, 0.7)
		if style == iconButtonQuiet {
			foreground = pal.text
		}
	}
	if disabled {
		foreground.Alpha *= 0.4
		soft.Alpha *= 0.4
	}
	// The layers sit on one another: the focus ring a little outside the button, the
	// surface behind it, then what the style and the pointer add, and the glyph.
	if focused {
		ring := gfx.Rect{X: bounds.X - 1.5, Y: bounds.Y - 1.5, Width: bounds.Width + 3, Height: bounds.Height + 3}
		canvas.RoundedBorder(ring, shape+1.5, 1.5, withAlpha(pal.accent, 0.5), gfx.Color{})
	}
	if soft.Alpha > 0 {
		canvas.Rounded(bounds, shape, soft)
	}
	if wash.Alpha > 0 {
		canvas.Rounded(bounds, shape, wash)
	}
	glyphIcon := lucideIcons[icon]
	if style == iconButtonUpdate {
		glyphIcon = updateIcon
	}
	size := iconSize
	if size <= 0 {
		size = 16
	}
	glyph := gfx.Rect{
		X:      bounds.X + (bounds.Width-size)/2,
		Y:      bounds.Y + (bounds.Height-size)/2,
		Width:  size,
		Height: size,
	}
	canvas.DrawIcon(glyphIcon, glyph, foreground)
	options.Role, options.Disabled, options.Hint = gfx.RoleButton, disabled, hint
	canvas.HitWithOptions(id, bounds, hint, options)
}

// updateGreen is the deep green of the update button, the same on both themes:
// dark enough to read as a status rather than a highlight, light enough to keep
// its contrast on the dark window bar.
var updateGreen = gfx.RGB(22, 163, 74)

// updateIcon is a ring with a solid down arrow inside it and the space between
// left empty. The ring is the icon's stroke, painted in the tint; the arrow is a
// filled layer, which carries its own color, so both are updateGreen.
var updateIcon = gfx.Icon{
	ViewBox: 24,
	Stroke:  2,
	Cap:     gfx.CapRound,
	Join:    gfx.JoinRound,
	Commands: []gfx.PathCommand{
		gfx.MoveTo(22, 12),
		gfx.CubicTo(22, 17.523, 17.523, 22, 12, 22),
		gfx.CubicTo(6.477, 22, 2, 17.523, 2, 12),
		gfx.CubicTo(2, 6.477, 6.477, 2, 12, 2),
		gfx.CubicTo(17.523, 2, 22, 6.477, 22, 12),
		gfx.ClosePath(),
	},
	Layers: []gfx.IconLayer{{
		Fill: updateGreen,
		Commands: []gfx.PathCommand{
			gfx.MoveTo(10.4, 6.5), gfx.LineTo(13.6, 6.5), gfx.LineTo(13.6, 11.5), gfx.LineTo(16.5, 11.5),
			gfx.LineTo(12, 16.5), gfx.LineTo(7.5, 11.5), gfx.LineTo(10.4, 11.5), gfx.ClosePath(),
		},
	}},
}
