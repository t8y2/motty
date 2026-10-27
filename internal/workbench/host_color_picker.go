package workbench

import (
	"fmt"
	"strings"

	"motty/internal/workbench/icons"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
)

var hostPickerHits = tree.Feature("host-color-picker")
var hostPickerScope = tree.ID(hostPickerHits, "scope")
var hostPickerIDs = components.ColorPickerIDs{
	Trigger: tree.ID(hostPickerHits, "trigger"), Dismiss: tree.ID(hostPickerHits, "dismiss"),
	Saturation: tree.ID(hostPickerHits, "sv"), Hue: tree.ID(hostPickerHits, "hue"),
	Hex: tree.ID(hostPickerHits, "hex"), Clear: tree.ID(hostPickerHits, "clear"),
	Cancel: tree.ID(hostPickerHits, "cancel"), Apply: tree.ID(hostPickerHits, "apply"),
}

type hostColorPicker struct {
	open, restoreFocus bool
	draft              components.ColorPickerValue
	hex                text.Editor
	seen, summary      string
	invalid            bool
	trigger            ui.Rect
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

func (state *State) hostPickerOptions(canvas *ui.Canvas) components.ColorPickerOptions {
	p, m := &state.editor.colorPicker, state.messages
	p.syncHex()
	value, empty := p.draft, false
	if !p.open {
		color, valid := components.ParseColorHex(state.editor.color.String(), false)
		value, empty = value.WithColor(color), !valid
		for _, preset := range hostColors {
			if strings.EqualFold(state.editor.value(fieldColor), preset) {
				empty = true
				break
			}
		}
	}
	focused := canvas.Focused() == hostPickerIDs.Hex
	o := components.ColorPickerOptions{
		RoundTrigger: true, TriggerIcon: &icons.Pipette,
		IDs:    hostPickerIDs,
		Labels: components.ColorPickerLabels{Color: m.ColorPickerChoose, Saturation: m.ColorPickerSV, Hue: m.ColorPickerHue, Hex: m.FieldColorCustom, Clear: m.ColorPickerClear, Cancel: m.Cancel, Apply: m.ColorPickerApply},
		Value:  value, Open: p.open, Empty: empty,
		Hex:            components.InputSnapshot{ID: hostPickerIDs.Hex, Label: m.FieldColorCustom, Value: p.hex.String(), Placeholder: "#3B82F6", Focused: focused, PlatformEditing: focused && p.hex.String() != "", Invalid: p.invalid},
		SaturationText: p.summary, Viewport: state.windowRect(), Layer: dialogLayer + 1, Scope: hostPickerScope,
	}
	if p.open && p.invalid {
		o.Error = m.ColorPickerInvalid
	}
	return o
}

func (state *State) paintHostColorPicker(canvas *ui.Canvas) {
	p := &state.editor.colorPicker
	if p.restoreFocus {
		canvas.RequestFocus(hostPickerIDs.Trigger)
		p.restoreFocus = false
	}
	if p.open {
		p.layout = components.BeginColorPicker(canvas, state.Theme(), p.trigger, state.hostPickerOptions(canvas)).PaintPanel()
	}
}

func (p *hostColorPicker) close() { p.open = false; p.restoreFocus = true }

func (state *State) clickHostColorPicker(id ui.ID) bool {
	p := &state.editor.colorPicker
	switch id {
	case hostPickerIDs.Trigger:
		if p.open {
			p.close()
			return true
		}
		color, valid := components.ParseColorHex(state.editor.color.String(), false)
		if !valid {
			color = state.pal.accent
		}
		p.setValue(components.ColorPickerValue{}.WithColor(color))
		p.open = true
		state.editor.iconOpen, state.editor.authOpen, state.editor.identityOpen, state.editor.portsOpen = false, false, false, false
		return true
	case hostPickerIDs.Cancel, hostPickerIDs.Dismiss:
		if p.open {
			p.close()
		}
		return true
	case hostPickerIDs.Clear:
		if p.open {
			state.editor.color = text.Editor{}
			p.close()
		}
		return true
	case hostPickerIDs.Apply:
		if p.open {
			p.syncHex()
			if !p.invalid {
				var buf [9]byte
				state.editor.color = text.Editor{}
				state.editor.color.Insert(string(p.draft.AppendHex(buf[:0], false)))
				p.close()
			}
		}
		return true
	}
	return false
}

func (state *State) hostPickerPointer(event ui.PointerEvent) ui.EventResult {
	p := &state.editor.colorPicker
	if state.modal != modalNewHost || !p.open {
		return ui.EventResult{}
	}
	value, result := p.layout.Pointer(event, p.draft)
	if value != p.draft {
		p.setValue(value)
	}
	return result
}

func (state *State) hostPickerKey(event ui.KeyEvent) ui.EventResult {
	p := &state.editor.colorPicker
	if state.modal != modalNewHost || !p.open || event.Release {
		return ui.EventResult{}
	}
	if event.Key == ui.KeyEscape || event.Key == ui.KeyEnter && event.Target == hostPickerIDs.Hex && event.Modifiers == 0 {
		if _, composing := p.hex.Composition(); composing {
			return ui.EventResult{}
		}
		if !event.Repeat {
			if event.Key == ui.KeyEscape {
				p.close()
			} else {
				state.clickHostColorPicker(hostPickerIDs.Apply)
			}
		}
		return ui.EventResult{Handled: true, Repaint: true}
	}
	value, result := p.layout.Key(event, p.draft)
	if value != p.draft {
		p.setValue(value)
	}
	return result
}
