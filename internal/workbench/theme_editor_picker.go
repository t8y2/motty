package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"strings"
)

var hitThemePickerDismiss = tree.ID(themeEditorHits, "picker-dismiss")

// 色盘只编辑草稿，由编辑器统一保存；点击外部不吞掉保存或切换色块的操作。
func (state *State) declareThemeColorPicker(c ui.C, anchor ui.Box) {
	e := &state.themeEditor
	picker := &e.pickers[e.colorIndex]
	if e.openPicker {
		picker.SetDraftValue(components.ColorPickerValue{}.WithColor(e.colors[e.colorIndex]))
		picker.SetOpenState(true)
		e.liveColorHex.SetText(e.hex.Text())
		e.openPicker = false
	}
	open := picker.Open()
	if !open {
		return
	}
	e.syncLiveThemeColor()
	e.liveColorInput.Pointer = e.themeColorPointer
	e.liveColorInput.Key = e.themeColorKey
	ui.Popover(c, anchor, &open, ui.PopoverSpec{
		Scope: dialogScope, DismissID: hitThemePickerDismiss, Width: 280,
		Align: ui.End, Offset: 6, Title: state.themeColorLabel(e.colorIndex), Instant: true,
	}, func() {
		ui.Column(c).FillWidth().Gap(10).Children(func() {
			ui.Control(c, state.messages.ColorPickerSV, &e.liveColorInput).FillWidth().Height(190).Draw(state.paintLiveThemeColor)
			field := ui.TextField(c, &e.liveColorHex, "HEX", "#RRGGBB").FillWidth()
			if field.Changed() {
				if color, ok := components.ParseColorHex(strings.TrimSpace(e.liveColorHex.Text()), false); ok && len(strings.TrimSpace(e.liveColorHex.Text())) == 7 {
					picker.SetDraftValue(components.ColorPickerValue{}.WithColor(color))
					e.syncLiveThemeColor()
				}
			}
		})
	})
	if !open {
		picker.ClosePanel()
	}
}

func (e *themeEditorState) syncLiveThemeColor() {
	if color, ok := e.pickers[e.colorIndex].Draft(); ok && rgb8(color) != rgb8(e.colors[e.colorIndex]) {
		e.colors[e.colorIndex] = schemeRGB(rgb8(color))
		e.syncColorFields()
		e.liveColorHex.SetText(e.hex.Text())
	}
}

func (e *themeEditorState) themeColorPointer(event gfx.PointerEvent) gfx.EventResult {
	picker := &e.pickers[e.colorIndex]
	value, result := e.liveColorLayout.Pointer(event, picker.GetDraft())
	if result.Handled {
		picker.SetDraftValue(value)
		e.syncLiveThemeColor()
	}
	return result
}

func (e *themeEditorState) themeColorKey(event gfx.KeyEvent) gfx.EventResult {
	picker := &e.pickers[e.colorIndex]
	value, result := e.liveColorLayout.Key(event, picker.GetDraft())
	if result.Handled {
		picker.SetDraftValue(value)
		e.syncLiveThemeColor()
	}
	return result
}

func (state *State) paintLiveThemeColor(d *ui.DrawContext) {
	e := &state.themeEditor
	c, r := d.Canvas, d.Rect
	v := e.pickers[e.colorIndex].GetDraft()
	parts := e.liveColorInput.Parts()
	plane := gfx.Rect{X: r.X, Y: r.Y, Width: r.Width, Height: 160}
	hue := gfx.Rect{X: r.X, Y: r.Y + 174, Width: r.Width, Height: 16}
	e.liveColorLayout = components.ColorPickerLayout{Content: r, Saturation: plane, Hue: hue,
		IDs: components.ColorPickerIDs{Saturation: parts.At(0), Hue: parts.At(1)}}
	e.pickers[e.colorIndex].SetLayoutValue(e.liveColorLayout)
	pure := components.ColorPickerValue{Hue: v.Hue, Saturation: 1, Brightness: 1, Alpha: 1}.Color()
	c.FillLinearGradient(plane, gfx.LinearGradient{Start: gfx.Point{X: r.X, Y: r.Y}, End: gfx.Point{X: r.X + r.Width, Y: r.Y}, From: gfx.RGB(255, 255, 255), To: pure})
	c.FillLinearGradient(plane, gfx.LinearGradient{Start: gfx.Point{X: r.X, Y: r.Y}, End: gfx.Point{X: r.X, Y: r.Y + 160}, From: gfx.Color{}, To: gfx.Color{Alpha: 1}})
	x, y := r.X+v.Saturation*r.Width, r.Y+(1-v.Brightness)*160
	c.Rounded(gfx.Rect{X: x - 5, Y: y - 5, Width: 10, Height: 10}, 5, gfx.RGB(255, 255, 255))
	c.Rounded(gfx.Rect{X: x - 3, Y: y - 3, Width: 6, Height: 6}, 3, v.Color())
	c.HitWithOptions(parts.At(0), plane, state.messages.ColorPickerSV, gfx.HitOptions{Role: gfx.RoleSlider, Range: gfx.ValueRange{Min: 0, Max: 100, Now: float64(v.Saturation * 100), Step: 1, Set: true}})
	for i := 0; i < 6; i++ {
		a, b := hue.X+float32(i)*hue.Width/6, hue.X+float32(i+1)*hue.Width/6
		from := components.ColorPickerValue{Hue: float32(i) * 60, Saturation: 1, Brightness: 1, Alpha: 1}.Color()
		to := components.ColorPickerValue{Hue: float32(i+1) * 60, Saturation: 1, Brightness: 1, Alpha: 1}.Color()
		c.FillLinearGradient(gfx.Rect{X: a, Y: hue.Y, Width: b - a, Height: hue.Height}, gfx.LinearGradient{Start: gfx.Point{X: a, Y: hue.Y}, End: gfx.Point{X: b, Y: hue.Y}, From: from, To: to})
	}
	c.Rounded(gfx.Rect{X: hue.X + v.Hue/360*hue.Width - 2, Y: hue.Y, Width: 4, Height: hue.Height}, 2, gfx.RGB(255, 255, 255))
	c.HitWithOptions(parts.At(1), hue, state.messages.ColorPickerHue, gfx.HitOptions{Role: gfx.RoleSlider, Range: gfx.ValueRange{Min: 0, Max: 360, Now: float64(v.Hue), Step: 1, Set: true}})
}
