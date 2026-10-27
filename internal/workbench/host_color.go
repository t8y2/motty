package workbench

import (
	"strconv"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
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

func hostAccent(value string) ui.Color {
	if len(value) != 7 || value[0] != '#' {
		return ui.Color{}
	}
	rgb, err := strconv.ParseUint(value[1:], 16, 24)
	if err != nil {
		return ui.Color{}
	}
	return ui.Color{Red: float32(rgb>>16) / 255, Green: float32((rgb>>8)&255) / 255, Blue: float32(rgb&255) / 255, Alpha: 1}
}

func (state *State) clickHostColor(id ui.ID) bool {
	index, ok := id.In(hitHostColors)
	if !ok || index < 0 || index >= len(hostColors) {
		return false
	}
	state.editor.color = text.Editor{}
	state.editor.color.Insert(hostColors[index])
	return true
}

func (state *State) paintHostColorChoice(canvas *ui.Canvas, bounds ui.Rect) {
	m := state.messages
	labels := [...]string{m.ColorNone, m.ColorGreen, m.ColorYellow, m.ColorOrange, m.ColorRed, m.ColorBlue, m.ColorPurple}
	palette := bounds
	palette.Width = 246
	content := components.BeginField(canvas, state.Theme(), palette).LabelGap(hostLabelGap).Paint(m.FieldColor, "", "")
	selected := state.editor.value(fieldColor)
	for index, color := range hostColors {
		swatch := ui.Rect{X: content.X + float32(index)*35, Y: content.Y, Width: 32, Height: 32}
		components.BeginColorSwatch(canvas, state.Theme(), hitHostColors.At(index), swatch, hostAccent(color)).Label(labels[index]).Selected(strings.EqualFold(selected, color)).Paint()
	}
	p := &state.editor.colorPicker
	p.trigger = ui.Rect{X: content.X + float32(len(hostColors))*35, Y: content.Y, Width: 32, Height: 32}
	components.BeginColorPicker(canvas, state.Theme(), p.trigger, state.hostPickerOptions(canvas)).PaintTrigger()
}

// Color stays on the header, never on the terminal cells or status light.
func (state *State) terminalBarColors(session *Session, inactive bool) (ui.Color, ui.Color) {
	background, border := state.pal.workspace, state.pal.line
	accent := hostAccent(session.host.Color)
	if accent.Alpha == 0 {
		return background, border
	}
	amount := float32(0.16)
	if inactive {
		amount = 0.08
	}
	return background.Mix(accent, amount), border.Mix(accent, 0.45)
}

func terminalToolAccent(id ui.ID) ui.Color {
	switch id {
	case hitPaneSplitVertical, hitPaneSplitHorizontal:
		return hostAccent("#3b82f6")
	case hitBroadcast:
		return hostAccent("#a855f7")
	case hitRemoteStats:
		return hostAccent("#22c55e")
	case hitSFTPOpen:
		return hostAccent("#eab308")
	case hitSFTPDetach:
		return hostAccent("#06b6d4")
	case hitConnExport, hitTerminalRecordingExport:
		return hostAccent("#a855f7")
	case hitTerminalRecord:
		return hostAccent("#ef4444")
	default:
		return ui.Color{}
	}
}
