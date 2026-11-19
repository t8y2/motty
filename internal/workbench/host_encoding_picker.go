package workbench

import (
	"github.com/vivaui/viva/ui"

	"motty/internal/charset"
)

// hostEncodingLabel names one encoding as the form's picker shows it.
func (state *State) hostEncodingLabel(name string) string {
	if name == "" || name == charset.UTF8 {
		return state.messages.EncodingUTF8Default
	}
	return name
}

// pickerEncodingValue maps a Host encoding to the picker's value.
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
	if state.editor.moshTerminal() {
		return false // mosh-server needs UTF-8
	}
	switch state.editor.protocol {
	case protocolSSH, protocolTelnet, protocolSerial:
		return true
	}
	return false
}

// declareHostEncodingPicker declares the remote encoding as a page combobox:
// the fixed set filters as the user types and never creates an encoding. A
// choice writes what Host stores.
func (state *State) declareHostEncodingPicker(c ui.C, parent ui.Box, rowHeight float32) {
	editor, form := &state.editor, &state.editor.form
	form.encodingChoice = pickerEncodingValue(editor.encoding)
	label := state.messages.FieldEncoding
	parent.Children(func() {
		ui.FormField(c, &form.encodingField, label).FillWidth().Height(rowHeight).Children(func() {
			form.encodingBox = ui.Combobox(c, &form.encodingChoice, charset.Encodings, state.hostEncodingLabel, label).
				Placeholder(state.messages.FieldEncodingPlaceholder).Empty(state.messages.HostEncodingsEmpty).
				Clearable().Hit(hitDialogEncoding, label)
			if form.encodingBox.Changed() {
				editor.encoding = hostEncoding(form.encodingChoice)
				state.dialogError = ""
			}
		})
	})
}
