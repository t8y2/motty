package workbench

import (
	"math"
	"math/rand/v2"
	"strconv"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"

	"motty/internal/workbench/icons"
)

var hitHostColors = tree.Sub(shellHits, tree.Name("host-colors"))
var hitHostRandomColor = tree.ID(hitHostColors, "random")

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
	if id == hitHostRandomColor {
		state.setHostColor(randomHostColor(state.editor.value(fieldColor)))
		return true
	}
	index, ok := id.In(hitHostColors)
	if !ok || index < 0 || index >= len(hostColors) {
		return false
	}
	state.setHostColor(hostColors[index])
	return true
}

// setHostColor replaces the form's colour with value, a preset or a hex string.
func (state *State) setHostColor(value string) {
	state.editor.color = text.Editor{}
	state.editor.color.Insert(value)
}

// randomHostColor creates a vivid colour at a random hue, with a saturation and
// brightness that stay clear on the host list. It never returns exclude, the
// colour the form already has: a pick that lands on it turns the hue half way
// round instead.
func randomHostColor(exclude string) string {
	pick := components.ColorPickerValue{Hue: rand.Float32() * 360, Saturation: 0.72, Brightness: 0.88, Alpha: 1}
	var buf [9]byte
	color := strings.ToLower(string(pick.AppendHex(buf[:0], false)))
	if strings.EqualFold(color, exclude) {
		pick.Hue = float32(math.Mod(float64(pick.Hue)+180, 360))
		color = strings.ToLower(string(pick.AppendHex(buf[:0], false)))
	}
	return color
}

// declareHostColorChoice declares the colour row in the page: the palette's
// swatches and the picker that stages a custom colour. A swatch click writes the
// preset; the picker writes what it applied.
func (state *State) declareHostColorChoice(c ui.C, parent ui.Box, rowHeight float32) {
	editor, form := &state.editor, &state.editor.form
	m := state.messages
	labels := [...]string{m.ColorNone, m.ColorGreen, m.ColorYellow, m.ColorOrange, m.ColorRed, m.ColorBlue, m.ColorPurple}
	selected := editor.value(fieldColor)
	empty := true
	if _, valid := components.ParseColorHex(selected, false); valid {
		empty = false
	}
	for _, preset := range hostColors {
		if strings.EqualFold(selected, preset) {
			empty = true
		}
	}
	form.colorValue = hostAccent(selected)
	parent.Children(func() {
		ui.FormField(c, &form.colorField, m.FieldColor).FillWidth().Height(rowHeight).Children(func() {
			ui.Row(c).Fill().Items(ui.Center).Gap(3).Children(func() {
				for index, color := range hostColors {
					swatch := ui.ColorSwatch(c, hostAccent(color), strings.EqualFold(selected, color), labels[index]).Hit(hitHostColors.At(index), labels[index]).Size(32, 32)
					form.colorSwatchBoxes[index] = swatch
					if swatch.Clicked() {
						state.setHostColor(color)
					}
				}
				form.randomColorBox = state.declareIconButton(c, hitHostRandomColor, iconDices, m.ColorRandom, false, iconButtonQuiet).Size(32, 32)
				form.colorPickerBox = ui.ColorPicker(c, &form.colorState, &form.colorValue, m.FieldColor).RoundTrigger().TriggerIcon(&icons.Pipette).NoColor(empty).
					PanelLabels(components.ColorPickerLabels{
						Color: m.ColorPickerChoose, Saturation: m.ColorPickerSV, Hue: m.ColorPickerHue,
						Hex: m.FieldColorCustom, Clear: m.ColorPickerClear, Cancel: m.Cancel, Apply: m.ColorPickerApply,
					})
				if form.colorState.Applied() {
					editor.color = text.Editor{}
					if form.colorValue.Alpha > 0 {
						var buf [9]byte
						editor.color.Insert(string(components.ColorPickerValue{}.WithColor(form.colorValue).AppendHex(buf[:0], false)))
					}
				}
			})
		})
	})
}

// rememberHostColor keeps the colour palette in the form's tab ring: the swatches
// by their own identities, and the picker by the trigger it paints.
func (state *State) rememberHostColor(canvas *gfx.Canvas) {
	form := &state.editor.form
	for index := range form.colorSwatchBoxes {
		if form.colorSwatchBoxes[index].Valid() {
			state.rememberHostControl(hitHostColors.At(index), form.colorSwatchBoxes[index].Rect())
		}
	}
	if form.randomColorBox.Valid() {
		state.rememberHostControl(hitHostRandomColor, form.randomColorBox.Rect())
	}
	if !form.colorPickerBox.Valid() {
		return
	}
	rect := form.colorPickerBox.Rect()
	for _, hit := range canvas.Hits() {
		if hit.Role != gfx.RoleButton || hit.Label != state.messages.ColorPickerChoose {
			continue
		}
		if hit.Bounds.X >= rect.X && hit.Bounds.Y >= rect.Y &&
			hit.Bounds.X < rect.X+rect.Width && hit.Bounds.Y < rect.Y+rect.Height {
			state.rememberHostControl(hit.ID, hit.Bounds)
			return
		}
	}
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
