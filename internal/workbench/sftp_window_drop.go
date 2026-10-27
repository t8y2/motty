package workbench

import (
	"github.com/vivaui/viva/ui"
	"math"
)

// Match PR #56: a neutral veil, square dashed edges and a quiet icon. No hit
// targets are introduced, so the pointer still resolves the actual folder.
func (state *State) paintSFTPTransferDrop(canvas *ui.Canvas, bounds ui.Rect, label, target string, glyph uint8) {
	if bounds.Width <= 0 || bounds.Height <= 0 {
		return
	}
	canvas.PushLayer(popupLayer)
	defer canvas.PopLayer()
	canvas.Clip(bounds)
	defer canvas.Unclip()
	gray := ui.RGB(128, 128, 128)
	canvas.Fill(bounds, withAlpha(state.pal.workspace, .92))
	canvas.Fill(bounds, withAlpha(gray, .08))
	const stroke, dash, gap = float32(1.5), float32(8), float32(6)
	dashes := func(length float32, paint func(float32, float32)) {
		count := max(int(math.Round(float64((length+gap)/(dash+gap)))), 1)
		part, step := min(dash, length), float32(0)
		if count > 1 {
			step = (length - part) / float32(count-1)
		}
		for i := 0; i < count; i++ {
			paint(float32(i)*step, part)
		}
	}
	dashes(bounds.Width, func(at, size float32) {
		canvas.Fill(ui.Rect{X: bounds.X + at, Y: bounds.Y, Width: size, Height: stroke}, gray)
		canvas.Fill(ui.Rect{X: bounds.X + at, Y: bounds.Y + bounds.Height - stroke, Width: size, Height: stroke}, gray)
	})
	dashes(bounds.Height, func(at, size float32) {
		canvas.Fill(ui.Rect{X: bounds.X, Y: bounds.Y + at, Width: stroke, Height: size}, gray)
		canvas.Fill(ui.Rect{X: bounds.X + bounds.Width - stroke, Y: bounds.Y + at, Width: stroke, Height: size}, gray)
	})
	style := ui.TextStyle{Size: 15, Weight: ui.WeightMedium, Color: state.pal.text}
	icon := ui.Rect{Width: 36, Height: 36, X: bounds.X + (bounds.Width-36)/2, Y: bounds.Y + (bounds.Height-92)/2}
	text := ui.Rect{X: bounds.X + 12, Y: icon.Y + 50, Width: max(bounds.Width-24, 0), Height: 22}
	if bounds.Height < 100 {
		style.Size = 13
		icon.Width, icon.Height = 16, 16
		width := min(canvas.TextWidthWeight(label, style.Size, style.Weight), max(bounds.Width-44, 0))
		icon.X, icon.Y = bounds.X+(bounds.Width-24-width)/2, bounds.Y+(bounds.Height-16)/2
		text = ui.Rect{X: icon.X + 24, Y: bounds.Y + (bounds.Height-20)/2, Width: width, Height: 20}
	} else {
		canvas.LabelCenter(target, ui.Rect{X: text.X, Y: text.Y + 26, Width: text.Width, Height: 20}, ui.TextStyle{Size: 12, Color: state.pal.muted})
	}
	paintIcon(canvas, glyph, icon, state.pal.muted)
	canvas.LabelCenter(label, text, style)
}
