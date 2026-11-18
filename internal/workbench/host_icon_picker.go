package workbench

import (
	"github.com/vivaui/viva/ui"

	"motty/internal/sshconn"
)

// hostIconArtKey is what an option's art depends on: the system the automatic
// icon shows, whether the host is a remote desktop, and the theme.
type hostIconArtKey struct {
	system  sshconn.RemoteSystem
	desktop bool
	dark    bool
}

// declareHostIconPicker declares the host's icon as a page select over the
// fixed choices, each option painted with the badge or glyph it stands for. A
// choice marks the icon as chosen, as the searchable field did.
func (state *State) declareHostIconPicker(c ui.C, parent ui.Box, rowHeight float32) {
	editor, form := &state.editor, &state.editor.form
	desktop := editor.protocol == protocolVNC || editor.protocol == protocolRDP
	choices := hostIconChoices()
	form.iconChoices = append(form.iconChoices[:0], choices...)
	form.iconIcons = form.iconIcons[:0]
	for _, choice := range form.iconChoices {
		form.iconIcons = append(form.iconIcons, state.hostIconArt(choice, editor.system, desktop))
	}
	form.iconChoice = editor.icon
	label := state.messages.FieldIcon
	parent.Children(func() {
		ui.FormField(c, &form.iconField, label).FillWidth().Height(rowHeight).Children(func() {
			form.iconBox = ui.Select(c, &form.iconChoice, form.iconChoices, state.hostIconLabel).
				Label(label).Icons(form.iconIcons).Hit(hitDialogIcon, label)
			if form.iconBox.Changed() {
				editor.icon, editor.iconChosen = form.iconChoice, true
				state.dialogError = ""
			}
		})
	})
}
