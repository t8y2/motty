package workbench

import "github.com/vivaui/viva/ui"

// iconButtonStyle selects how a square icon button reads at rest.
type iconButtonStyle uint8

const (
	iconButtonQuiet  iconButtonStyle = iota // muted until the pointer is on it
	iconButtonDanger                        // a destructive or recording action
	iconButtonActive                        // a switch that is on
)

// paintIconButton draws a square icon button on the surface behind. The hint is
// both what assistive technology reads and the tooltip that appears when the
// pointer rests on it: an icon alone does not say what it does.
func (state *State) paintIconButton(canvas *ui.Canvas, id ui.ID, icon uint8, bounds ui.Rect, hint string, disabled bool, style iconButtonStyle, behind ui.Color) {
	state.paintTintedIconButton(canvas, id, icon, bounds, hint, disabled, style, behind, ui.Color{})
}

func (state *State) paintTintedIconButton(canvas *ui.Canvas, id ui.ID, icon uint8, bounds ui.Rect, hint string, disabled bool, style iconButtonStyle, behind, tint ui.Color) {
	pal := state.pal
	if int(icon) >= len(lucideIcons) {
		return
	}
	hovered := !disabled && canvas.Hovered() == id
	pressed := !disabled && canvas.Pressed() == id
	focused := !disabled && canvas.FocusVisible() == id
	foreground := pal.muted
	var soft, wash ui.Color
	if tint.Alpha > 0 {
		soft = withAlpha(tint, 0.13)
		foreground = tint.Mix(pal.text, 0.45)
	}
	switch style {
	case iconButtonDanger:
		foreground = pal.danger
	case iconButtonActive:
		foreground = pal.accent
		soft = pal.accentSoft
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
	f := state.chrome.widget.Begin(canvas, state.Theme(), bounds)
	f.Stack().Fill().Center().
		HitWith(id, hint, ui.HitOptions{Role: ui.RoleButton, Disabled: disabled, Hint: hint}).Children(func() {
		if focused {
			f.Row().Fill().Margin(-1.5).Radius(radius.control + 1.5).Background(withAlpha(pal.accent, 0.5))
			f.Row().Fill().Radius(radius.control).Background(behind)
		}
		if soft.Alpha > 0 {
			f.Row().Fill().Radius(radius.control).Background(soft)
		}
		if wash.Alpha > 0 {
			f.Row().Fill().Radius(radius.control).Background(wash)
		}
		f.Icon(lucideIcons[icon]).Color(foreground)
	})
	f.End()
}
