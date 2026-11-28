package workbench

import "github.com/vivaui/viva/gfx"
import "github.com/vivaui/viva/ui"

// 使用终端模拟器和实际行缓存渲染预览，不启动进程也不修改活动会话。
func (state *State) paintThemePreviewTerminal(d *ui.DrawContext) {
	e, canvas := &state.themeEditor, d.Canvas
	if e.preview == nil {
		e.preview = newTerminalWithScrollback(80, 16, 0)
		_, _ = e.preview.screen.Write([]byte(themePreviewSample))
	}
	s := e.previewScheme()
	p := *state.appearance.palette()
	p.terminal, p.termFG, p.cursor, p.ansi = schemeRGB(s.background), schemeRGB(s.foreground), schemeRGB(s.cursor), s.ansi
	e.previewVersion++
	p.epoch = e.previewVersion
	canvas.Clip(d.Rect)
	defer canvas.Unclip()
	canvas.Fill(d.Rect, state.pal.workspace)
	if state.backgroundOn() {
		bg := &state.bg
		bitmap := gfx.Bitmap{Pix: bg.pix, Width: bg.width, Height: bg.height, Stride: bg.width * 4, ID: bg.id}
		canvas.DrawBitmapRegion(bitmap, gfx.Rect{Width: float32(bg.width), Height: float32(bg.height)}, d.Rect)
	}
	ground := p.terminal
	ground.Alpha = float32(*s.opacity) / 100
	if state.backgroundOn() {
		ground.Alpha *= 1 - state.bg.visibility.Value/100
	}
	canvas.Fill(d.Rect, ground)
	area := d.Rect.Inset(gfx.All(12))
	cw, ch := state.cellWidth, state.cellHeight
	if cw <= 0 {
		cw = 8
	}
	if ch <= 0 {
		ch = 20
	}
	scale := min(float32(1), area.Width/(64*cw))
	cw, ch = cw*scale, ch*scale
	style := gfx.TextStyle{Size: state.terminalFontSize * scale, Color: p.termFG, MonoFace: state.terminalFontFace}
	t := e.preview
	t.cache.begin()
	defer t.cache.end(16)
	cursor := t.screen.Cursor()
	for row := 0; row < 16; row++ {
		column := -1
		if row == cursor.Y {
			column = cursor.X
		}
		entry := t.cache.row(t.screen.Row(row), 80, column, cellSpan{}, &p, false)
		bounds := gfx.Rect{X: area.X, Y: area.Y + float32(row)*ch, Width: 80 * cw, Height: ch}
		for _, span := range entry.spans {
			canvas.Fill(gfx.Rect{X: bounds.X + float32(span.from)*cw, Y: bounds.Y, Width: float32(span.to-span.from) * cw, Height: ch}, span.color)
		}
		if row == 7 {
			canvas.Fill(gfx.Rect{X: bounds.X, Y: bounds.Y, Width: 27 * cw, Height: ch}, schemeRGB(s.selection))
		}
		canvas.LabelRuns(entry.text, bounds, style, cw, entry.runs)
		if row == 7 {
			paintThemeSelectionText(canvas, entry, bounds, style, cw, 0, 27, schemeRGB(s.selectionText))
		}
	}
}

func paintThemeSelectionText(canvas *gfx.Canvas, entry *rowEntry, bounds gfx.Rect, style gfx.TextStyle, cellWidth float32, from, to int, color gfx.Color) {
	canvas.Clip(gfx.Rect{X: bounds.X + float32(from)*cellWidth, Y: bounds.Y, Width: float32(to-from) * cellWidth, Height: bounds.Height})
	runs := append([]gfx.TextRun(nil), entry.runs...)
	for i := range runs {
		runs[i].Color = color
	}
	style.Color = color
	canvas.LabelRuns(entry.text, bounds, style, cellWidth, runs)
	canvas.Unclip()
}
