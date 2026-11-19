package workbench

import (
	"fmt"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
)

// hostColorPicker is the hand-painted colour panel the terminal's find
// preferences still use. The host form's own colour picker is the page control
// in host_color.go.
type hostColorPicker struct {
	open, restoreFocus bool
	draft              components.ColorPickerValue
	hex                text.Editor
	seen, summary      string
	invalid            bool
	trigger            gfx.Rect
	layout             components.ColorPickerLayout
}

func (p *hostColorPicker) setValue(v components.ColorPickerValue) {
	p.draft = v
	var buf [9]byte
	value := string(v.AppendHex(buf[:0], false))
	if p.hex.String() != value {
		p.hex = text.Editor{}
		p.hex.Insert(value)
	}
	p.seen, p.invalid = value, false
	p.summary = fmt.Sprintf("%.0f%% / %.0f%%", v.Saturation*100, v.Brightness*100)
}

func (p *hostColorPicker) syncHex() {
	value := p.hex.String()
	if value == p.seen {
		return
	}
	p.seen = value
	color, valid := components.ParseColorHex(value, false)
	p.invalid = !valid
	if valid {
		p.draft = p.draft.WithColor(color)
		p.summary = fmt.Sprintf("%.0f%% / %.0f%%", p.draft.Saturation*100, p.draft.Brightness*100)
	}
}
