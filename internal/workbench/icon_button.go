package workbench

import "github.com/vivaui/viva/ui"

// iconButtonStyle selects how a square icon button reads at rest.
type iconButtonStyle uint8

const (
	iconButtonQuiet  iconButtonStyle = iota // muted until the pointer is on it
	iconButtonDanger                        // a destructive or recording action
	iconButtonActive                        // a switch that is on
	iconButtonUpdate                        // a green ring around a solid arrow: an update is waiting
)

// paintIconButton draws a square icon button on the surface behind. The hint is
// both what assistive technology reads and the tooltip that appears when the
// pointer rests on it: an icon alone does not say what it does.
func (state *State) paintIconButton(canvas *ui.Canvas, id ui.ID, icon uint8, bounds ui.Rect, hint string, disabled bool, style iconButtonStyle, behind ui.Color) {
	state.paintIconButtonSized(canvas, id, icon, bounds, hint, disabled, style, behind, 0)
}

// paintIconButtonSized is paintIconButton with an optional explicit glyph size.
// A zero size keeps the component's default for existing compact controls.
func (state *State) paintIconButtonSized(canvas *ui.Canvas, id ui.ID, icon uint8, bounds ui.Rect, hint string, disabled bool, style iconButtonStyle, behind ui.Color, iconSize float32) {
	state.paintIconButtonWithOptions(canvas, id, icon, bounds, hint, disabled, style, behind, iconSize, ui.HitOptions{})
}

func (state *State) paintIconButtonWithOptions(canvas *ui.Canvas, id ui.ID, icon uint8, bounds ui.Rect, hint string, disabled bool, style iconButtonStyle, behind ui.Color, iconSize float32, options ui.HitOptions) {
	pal := state.pal
	if int(icon) >= len(lucideIcons) {
		return
	}
	hovered := !disabled && canvas.Hovered() == id
	pressed := !disabled && canvas.Pressed() == id
	focused := !disabled && canvas.FocusVisible() == id
	foreground := pal.muted
	var soft, wash ui.Color
	switch style {
	case iconButtonDanger:
		foreground = pal.danger
	case iconButtonActive:
		foreground = pal.accent
		soft = pal.accentSoft
	case iconButtonUpdate:
		foreground = updateGreen
	}
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
	shape := radius.control
	if style == iconButtonUpdate {
		shape = min(bounds.Width, bounds.Height) / 2
	}
	f := state.chrome.widget.Begin(canvas, state.Theme(), bounds)
	options.Role, options.Disabled, options.Hint = ui.RoleButton, disabled, hint
	f.Stack().Fill().Center().
		HitWith(id, hint, options).Children(func() {
		if focused {
			f.Row().Fill().Margin(-1.5).Radius(shape + 1.5).Background(withAlpha(pal.accent, 0.5))
			f.Row().Fill().Radius(shape).Background(behind)
		}
		if soft.Alpha > 0 {
			f.Row().Fill().Radius(shape).Background(soft)
		}
		if wash.Alpha > 0 {
			f.Row().Fill().Radius(shape).Background(wash)
		}
		glyphIcon := lucideIcons[icon]
		if style == iconButtonUpdate {
			glyphIcon = updateIcon
		}
		glyph := f.Icon(glyphIcon)
		if iconSize > 0 {
			glyph.Size(iconSize, iconSize)
		}
		glyph.Color(foreground)
	})
	f.End()
}

// updateGreen is the deep green of the update button, the same on both themes:
// dark enough to read as a status rather than a highlight, light enough to keep
// its contrast on the dark window bar.
var updateGreen = ui.RGB(22, 163, 74)

// updateIcon is a ring with a solid down arrow inside it and the space between
// left empty. The ring is the icon's stroke, painted in the tint; the arrow is a
// filled layer, which carries its own color, so both are updateGreen.
var updateIcon = ui.Icon{
	ViewBox: 24,
	Stroke:  2,
	Cap:     ui.CapRound,
	Join:    ui.JoinRound,
	Commands: []ui.PathCommand{
		ui.MoveTo(22, 12),
		ui.CubicTo(22, 17.523, 17.523, 22, 12, 22),
		ui.CubicTo(6.477, 22, 2, 17.523, 2, 12),
		ui.CubicTo(2, 6.477, 6.477, 2, 12, 2),
		ui.CubicTo(17.523, 2, 22, 6.477, 22, 12),
		ui.ClosePath(),
	},
	Layers: []ui.IconLayer{{
		Fill: updateGreen,
		Commands: []ui.PathCommand{
			ui.MoveTo(10.4, 6.5), ui.LineTo(13.6, 6.5), ui.LineTo(13.6, 11.5), ui.LineTo(16.5, 11.5),
			ui.LineTo(12, 16.5), ui.LineTo(7.5, 11.5), ui.LineTo(10.4, 11.5), ui.ClosePath(),
		},
	}},
}
