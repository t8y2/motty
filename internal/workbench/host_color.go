package workbench

import (
	"strconv"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
)

var hitHostColors = tree.Sub(shellHits, tree.Name("host-colors"))

// Keep the same familiar palette as DBX's connection editor.
var hostColors = [...]string{"", "#22c55e", "#eab308", "#f97316", "#ef4444", "#3b82f6", "#a855f7"}

func normalizeHostColor(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", true
	}
	if len(value) != 7 || value[0] != '#' {
		return "", false
	}
	if _, err := strconv.ParseUint(value[1:], 16, 24); err != nil {
		return "", false
	}
	return strings.ToLower(value), true
}

func hostAccent(value string) gfx.Color {
	if len(value) != 7 || value[0] != '#' {
		return gfx.Color{}
	}
	rgb, err := strconv.ParseUint(value[1:], 16, 24)
	if err != nil {
		return gfx.Color{}
	}
	return gfx.Color{Red: float32(rgb>>16) / 255, Green: float32((rgb>>8)&255) / 255, Blue: float32(rgb&255) / 255, Alpha: 1}
}

func (state *State) clickHostColor(id gfx.ID) bool {
	index, ok := id.In(hitHostColors)
	if !ok || index < 0 || index >= len(hostColors) {
		return false
	}
	state.editor.color = text.Editor{}
	state.editor.color.Insert(hostColors[index])
	return true
}

func (state *State) paintHostColorChoice(canvas *gfx.Canvas, bounds gfx.Rect) {
	m := state.messages
	labels := [...]string{m.ColorNone, m.ColorGreen, m.ColorYellow, m.ColorOrange, m.ColorRed, m.ColorBlue, m.ColorPurple}
	palette := bounds
	palette.Width = 246
	content := components.BeginField(canvas, state.Theme(), palette).Paint(m.FieldColor, "", "")
	selected := state.editor.value(fieldColor)
	for index, color := range hostColors {
		swatch := gfx.Rect{X: content.X + float32(index)*35, Y: content.Y, Width: 32, Height: 32}
		components.BeginColorSwatch(canvas, state.Theme(), hitHostColors.At(index), swatch, hostAccent(color)).Label(labels[index]).Selected(strings.EqualFold(selected, color)).Paint()
		state.rememberHostControl(hitHostColors.At(index), swatch)
	}
	p := &state.editor.colorPicker
	p.trigger = gfx.Rect{X: content.X + float32(len(hostColors))*35, Y: content.Y, Width: 32, Height: 32}
	state.rememberHostControl(hostPickerIDs.Trigger, p.trigger)
	components.BeginColorPicker(canvas, state.Theme(), p.trigger, state.hostPickerOptions(canvas)).PaintTrigger()
}

func tintHostSurface(background gfx.Color, color string, amount float32) gfx.Color {
	if accent := hostAccent(color); accent.Alpha > 0 {
		return background.Mix(accent, amount)
	}
	return background
}

func paintHostColorMark(canvas *gfx.Canvas, bounds gfx.Rect, color string) {
	if accent := hostAccent(color); accent.Alpha > 0 {
		canvas.Rounded(gfx.Rect{X: bounds.X + 3, Y: bounds.Y + 10, Width: 3, Height: max(bounds.Height-20, 0)}, 1.5, accent)
	}
}
