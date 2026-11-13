package workbench

import (
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"

	"motty/internal/charset"
)

var (
	hostEncodingPickerHits       = tree.Feature("host-encoding-picker")
	hitHostEncodingTrigger       = tree.ID(hostEncodingPickerHits, "trigger")
	hitHostEncodingClear         = tree.ID(hostEncodingPickerHits, "clear")
	hitHostEncodingPickerDismiss = tree.ID(hostEncodingPickerHits, "dismiss")
	hitHostEncodingPickerScroll  = tree.ID(hostEncodingPickerHits, "scroll")
	hostEncodingPickerScope      = tree.ID(hostEncodingPickerHits, "scope")
	hitHostEncodingChoices       = tree.Sub(hostEncodingPickerHits, tree.Name("option"))
)

// hostEncodingPicker is the searchable remote-charset field of the host dialog.
// Its value is a charset name; the UTF-8 default is stored as an empty string
// on Host but shown as charset.UTF8 here.
type hostEncodingPicker struct {
	control    components.SingleSelectState
	serialized string
	ready      bool
	popup      gfx.Rect
}

func (state *State) hostEncodingLabel(name string) string {
	if name == "" || name == charset.UTF8 {
		return state.messages.EncodingUTF8Default
	}
	return name
}

// pickerValue maps a Host encoding to the picker's value.
func pickerEncodingValue(encoding string) string {
	if encoding == "" {
		return charset.UTF8
	}
	return encoding
}

// hostEncoding turns the picker's value back into what Host stores.
func hostEncoding(value string) string {
	if value == "" || value == charset.UTF8 {
		return ""
	}
	return value
}

// hostShowsEncoding reports whether the form asks for the remote encoding: it
// belongs to the byte stream of a shell, which SSH, Telnet and Serial open and
// the desktop protocols do not.
func (state *State) hostShowsEncoding() bool {
	switch state.editor.protocol {
	case protocolSSH, protocolTelnet, protocolSerial:
		return true
	}
	return false
}

func (state *State) hostEncodingPicker() *hostEncodingPicker {
	p := &state.editor.encodingPicker
	if !p.ready {
		// Encodings are a fixed set: searching filters them but never creates a
		// new one, so AllowCreate stays off.
		p.control.MatchCaseInsensitive = true
		options := make([]components.SingleSelectOption, 0, len(charset.Encodings))
		for _, name := range charset.Encodings {
			options = append(options, components.SingleSelectOption{
				Value:  name,
				Label:  state.hostEncodingLabel(name),
				Search: charset.Search(name),
			})
		}
		p.control.SetOptions(options)
		// Rebuilding the options (a language change) must not reset the value or
		// the search the user is typing, so keep the control's current value as
		// the serialization baseline instead of a sentinel.
		p.ready, p.serialized = true, p.control.Value()
	}
	value := pickerEncodingValue(state.editor.encoding)
	if value != p.serialized {
		p.control.SetValue(value)
		p.serialized = value
	}
	return p
}

func (state *State) syncHostEncoding() {
	p := &state.editor.encodingPicker
	value := p.control.Value()
	// Clear() leaves the value empty; the empty picker value is the UTF-8
	// default, so normalize it to the same value the options carry.
	if value == "" {
		value = charset.UTF8
		p.control.SetValue(value)
	}
	if value != p.serialized {
		state.editor.encoding = hostEncoding(value)
		p.serialized = value
	}
}

// commitHostEncodingQuery confirms a typed encoding on form submit. Unlike the
// group picker there is nothing to create: an unmatched query is rejected.
func (state *State) commitHostEncodingQuery() bool {
	p := state.hostEncodingPicker()
	if _, composing := p.control.Query.Composition(); composing {
		return true
	}
	query := strings.TrimSpace(p.control.Query.String())
	if query == "" {
		return true
	}
	if !p.control.CommitQuery() {
		return false
	}
	state.syncHostEncoding()
	return true
}

func (state *State) clickHostEncodingPicker(id gfx.ID) bool {
	p := state.hostEncodingPicker()
	switch {
	case id == hitDialogEncoding:
		state.closeOtherHostPickers()
		p.control.Open = true
	case id == hitHostEncodingTrigger:
		open := !p.control.Open
		state.closeOtherHostPickers()
		p.control.Open = open
	case id == hitHostEncodingClear:
		p.control.Clear()
		state.syncHostEncoding()
	case id == hitHostEncodingPickerDismiss:
		p.control.Open = false
	case id.Scope() == hitHostEncodingChoices:
		p.control.Choose(item(id))
		state.syncHostEncoding()
	default:
		return false
	}
	return true
}

// closeOtherHostPickers keeps only one searchable host-form popup open at a
// time. Each picker closes every other one before it opens itself.
func (state *State) closeOtherHostPickers() {
	state.editor.groupPicker.control.Open = false
	state.editor.tagPicker.control.Open = false
	state.editor.encodingPicker.control.Open = false
	state.editor.iconPicker.control.Open = false
}

func (state *State) hostEncodingPickerKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	if state.modal != modalNewHost {
		return gfx.EventResult{}, false
	}
	p := state.hostEncodingPicker()
	if event.Target != hitDialogEncoding && (!p.control.Open || event.Key != gfx.KeyEscape && event.Key != gfx.KeyTab) {
		return gfx.EventResult{}, false
	}
	// Enter with a typed query confirms it; with nothing typed it belongs to
	// the form's save action, so let it through.
	if event.Target == hitDialogEncoding && event.Key == gfx.KeyEnter && event.Modifiers == 0 && !event.Release {
		if _, composing := p.control.Query.Composition(); composing || event.Repeat {
			return gfx.EventResult{Handled: true, Repaint: true}, true
		}
		if strings.TrimSpace(p.control.Query.String()) != "" {
			state.commitHostEncodingQuery()
			return gfx.EventResult{Handled: true, Repaint: true}, true
		}
		if p.control.Open {
			p.control.Dismiss()
			return gfx.EventResult{Handled: true, Repaint: true}, true
		}
		return gfx.EventResult{}, false
	}
	result := p.control.Key(event)
	state.syncHostEncoding()
	return result, result.Handled || result.Repaint
}

func isHostEncodingPickerTarget(id gfx.ID) bool {
	return id == hitDialogEncoding || id == hitHostEncodingTrigger || id == hitHostEncodingClear ||
		id == hitHostEncodingPickerScroll || id.Scope() == hitHostEncodingChoices
}

func (state *State) paintHostEncodingPicker(canvas *gfx.Canvas, bounds gfx.Rect) {
	p := state.hostEncodingPicker()
	label := state.messages.FieldEncoding
	input := components.BeginField(canvas, state.Theme(), bounds).For(hitDialogEncoding).Paint(label, "", "")
	state.revealHostField(canvas, hitDialogEncoding, input)
	viewport := gfx.Rect{Width: state.viewport.Width, Height: state.viewport.Height}
	if state.editor.form.clipped {
		viewport = state.editor.form.geometry.Viewport
	}
	layout := components.PaintSingleSelect(canvas, state.Theme(), input, &p.control, components.SingleSelectOptions{
		InputID: hitDialogEncoding, TriggerID: hitHostEncodingTrigger, ClearID: hitHostEncodingClear, DismissID: hitHostEncodingPickerDismiss, Scope: hostEncodingPickerScope, ScrollID: hitHostEncodingPickerScroll, OptionIDs: hitHostEncodingChoices,
		Label: label, Placeholder: state.messages.FieldEncodingPlaceholder, EmptyLabel: state.messages.HostEncodingsEmpty, ClearLabel: state.messages.EncodingUTF8Default,
		Viewport: viewport, Layer: dialogLayer + 2,
	})
	p.popup = layout.Popup
	if clear, ok := canvas.HitBounds(hitHostEncodingClear); ok {
		state.rememberHostControl(hitHostEncodingClear, clear)
	}
}

// resetHostEncodingPicker asks the picker to rebuild its options, which carry
// a localised UTF-8 label, when the language changes. The control keeps its
// value and query; only the option list is stale.
func (state *State) resetHostEncodingPicker() {
	state.editor.encodingPicker.ready = false
}
