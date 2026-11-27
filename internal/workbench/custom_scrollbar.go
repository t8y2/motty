package workbench

import (
	"github.com/vivaui/viva/base"
	"github.com/vivaui/viva/gfx"
)

// paintScrollbarBar paints a caller-owned bar from its pure geometry, exactly as
// components.ScrollArea painted the same bar: the rounded thumb, the focus ring,
// the hover and press tint, and the separator hit over the track. revealed false
// leaves a hover bar unpainted and unhit, as the old HoverScrollbars did. hot is
// the pointer on the bar as the caller tracks it, for a bar that moves under the
// pointer before the canvas has found the hit it is about to declare.
func paintScrollbarBar(canvas *gfx.Canvas, theme gfx.Theme, geometry base.ScrollAreaGeometry, vertical bool, id gfx.ID, label, value string, revealed, hot bool) {
	if !revealed {
		return
	}
	track, thumb := geometry.VerticalTrack, geometry.VerticalThumb
	if !vertical {
		track, thumb = geometry.HorizontalTrack, geometry.HorizontalThumb
	}
	if id.IsZero() && label == "" {
		return
	}
	if track.Width <= 0 || track.Height <= 0 || thumb.Width <= 0 || thumb.Height <= 0 {
		return
	}
	color := theme.Border
	if hot || canvas.Hovered() == id || canvas.Pressed() == id {
		color = theme.MutedForeground
		color.Alpha *= 0.72
	}
	if canvas.FocusVisible() == id {
		ring := theme.Ring
		ring.Alpha *= 0.50
		canvas.Rounded(scrollbarBarExpand(thumb, 2), min(thumb.Width, thumb.Height)/2+2, ring)
	}
	canvas.Rounded(thumb, min(thumb.Width, thumb.Height)/2, color)
	if !id.IsZero() {
		canvas.HitWithOptions(id, track, label, gfx.HitOptions{
			Role: gfx.RoleSeparator, Value: value, SkipFocus: true,
		})
	}
}

func scrollbarBarExpand(bounds gfx.Rect, amount float32) gfx.Rect {
	return gfx.Rect{
		X: bounds.X - amount, Y: bounds.Y - amount,
		Width: bounds.Width + amount*2, Height: bounds.Height + amount*2,
	}
}
