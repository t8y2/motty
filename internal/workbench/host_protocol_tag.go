package workbench

import "github.com/vivaui/viva/gfx"

// hostProtocolTagLabel names a host's protocol the way the host form's protocol
// choice does, so a tag on the hosts page follows the selection made there.
func (state *State) hostProtocolTagLabel(host Host) string {
	if host.mosh() {
		return state.messages.Tools.ProtocolMosh
	}
	return state.hostProtocolChoiceLabel(host.Protocol)
}

// hostProtocolTagColor is one protocol's tag colour. The palette's ANSI colours
// stay legible on both appearances, and red is left to danger states: SSH takes
// blue, Telnet cyan, serial green, VNC purple and RDP amber.
func hostProtocolTagColor(pal *palette, protocol hostProtocol) gfx.Color {
	index := 4 // SSH
	switch protocol {
	case protocolTelnet:
		index = 6
	case protocolSerial:
		index = 2
	case protocolVNC:
		index = 5
	case protocolRDP:
		index = 3
	}
	c := pal.ansi[index]
	return gfx.RGB(c[0], c[1], c[2])
}

// paintHostProtocolTag paints a protocol's pill. The caller sizes the pill; the
// text keeps the inset the user tags beside it use.
func paintHostProtocolTag(canvas *gfx.Canvas, bounds gfx.Rect, label string, color gfx.Color) {
	canvas.Rounded(bounds, bounds.Height/2, withAlpha(color, 0.14))
	canvas.Label(label, gfx.Rect{X: bounds.X + 7, Y: bounds.Y, Width: max(bounds.Width-14, 0), Height: bounds.Height},
		gfx.TextStyle{Size: metrics.FontSizeS, Weight: gfx.WeightMedium, Color: color})
}
