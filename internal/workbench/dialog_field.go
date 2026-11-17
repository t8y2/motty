package workbench

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
)

// dialogField is a labelled single-line field of a dialog the page declares: its
// row, its text bound to an editor the application owns, and, for a secret, whether
// the text shows.
type dialogField struct {
	row   ui.FormFieldState
	input ui.Field
	bound bool
	shown bool
}

// dialogFieldSpec says what a dialogField shows: Description is the line under
// it, Error replaces that line and paints the field as wrong, and Invalid paints
// it as wrong with no line of its own. A Secret field has the button that shows
// its text.
type dialogFieldSpec struct {
	ID                                     gfx.ID
	Label, Description, Placeholder, Error string
	Secret, Invalid, Disabled              bool
	Height                                 float32
}

// declare declares the field over editor, the one the application reads, and
// returns the text's box.
func (f *dialogField) declare(c ui.C, state *State, editor *text.Editor, spec dialogFieldSpec) {
	if !f.bound {
		f.input.Bind(editor)
		f.bound = true
	}
	f.input.Secure = spec.Secret && !f.shown
	f.input.Invalid = spec.Invalid || spec.Error != ""
	f.input.Disabled = spec.Disabled
	f.row.For, f.row.Description, f.row.Error = spec.ID, spec.Description, spec.Error
	f.row.Invalid = spec.Invalid || spec.Error != ""
	ui.FormField(c, &f.row, spec.Label).FillWidth().Height(spec.Height).Children(func() {
		box := ui.TextFieldID(c, spec.ID, &f.input, spec.Label, spec.Placeholder)
		if spec.Secret {
			icon, name := lucideIcons[iconEye], state.messages.ShowSecret
			if f.shown {
				icon, name = lucideIcons[iconEyeOff], state.messages.HideSecret
			}
			box.Action(icon, name)
			if box.ActionClicked() {
				f.shown = !f.shown
			}
		}
	})
}
