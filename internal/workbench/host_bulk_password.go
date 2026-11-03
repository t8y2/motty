package workbench

import (
	"fmt"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	"motty/internal/vault"
)

var bulkPasswordHits = tree.Feature("bulk-host-password")

var (
	hitBulkPasswordDismiss = tree.ID(bulkPasswordHits, "dismiss")
	hitBulkPasswordClose   = tree.ID(bulkPasswordHits, "close")
	hitBulkPasswordCancel  = tree.ID(bulkPasswordHits, "cancel")
	hitBulkPasswordConfirm = tree.ID(bulkPasswordHits, "confirm")
	hitBulkPasswordField   = tree.ID(bulkPasswordHits, "password")
)

type bulkHostPassword struct {
	ids      []uint32
	eligible int
	skipped  int
	editor   text.Editor
}

func (state *State) openBulkHostPassword() {
	state.captureHostActionIDs()
	var batch bulkHostPassword
	for _, id := range state.hostActionIDs {
		host, ok := state.hostByID(id)
		if !ok {
			continue
		}
		batch.ids = append(batch.ids, id)
		if _, eligible := hostPasswordSecretName(host); eligible {
			batch.eligible++
		} else {
			batch.skipped++
		}
	}
	if batch.eligible == 0 {
		state.showActionWarning(state.messages.BulkPasswordTitle, state.messages.BulkPasswordNoHosts)
		return
	}
	state.bulkPassword = batch
	state.dialogError = ""
	state.modal = modalBulkHostPassword
}

func (state *State) clickBulkHostPassword(id ui.ID) {
	switch id {
	case hitBulkPasswordDismiss, hitBulkPasswordClose, hitBulkPasswordCancel:
		state.bulkPassword = bulkHostPassword{}
		state.dialogError = ""
		state.reveal.reset()
		state.modal = modalNone
	case hitBulkPasswordConfirm:
		state.submitBulkHostPassword()
	}
}

func (state *State) submitBulkHostPassword() {
	password := state.bulkPassword.editor.String()
	if password == "" {
		state.dialogError = state.messages.BulkPasswordEmpty
		return
	}
	if state.secrets.vault == nil {
		state.dialogError = state.hostPasswordSaveError(vault.ErrUnavailable)
		return
	}
	changes := make([]vault.Change, 0, state.bulkPassword.eligible)
	for _, id := range state.bulkPassword.ids {
		host, ok := state.hostByID(id)
		if !ok {
			continue
		}
		if name, eligible := hostPasswordSecretName(host); eligible {
			changes = append(changes, vault.Change{Name: name, Value: password})
		}
	}
	if len(changes) == 0 {
		state.dialogError = state.messages.BulkPasswordNoHosts
		return
	}
	if err := state.secrets.vault.Apply(changes); err != nil {
		state.dialogError = state.hostPasswordSaveError(err)
		return
	}
	state.status = fmt.Sprintf(state.messages.BulkPasswordSavedFmt, len(changes), state.bulkPassword.skipped)
	state.bulkPassword = bulkHostPassword{}
	state.dialogError = ""
	state.reveal.reset()
	state.modal = modalNone
}

func (state *State) paintBulkHostPassword(canvas *ui.Canvas, viewport ui.Rect) {
	m, theme := state.messages, state.Theme()
	dialog := components.BeginDialog(canvas, theme, viewport, components.DialogOptions{
		Layer: dialogLayer, Scope: dialogScope,
		DismissID: hitBulkPasswordDismiss, CloseID: hitBulkPasswordClose, InitialFocusID: hitBulkPasswordField,
		ContentSize: ui.Size{Width: 500}, FitBody: true, BodyHeight: withError(66, state.dialogError),
	})
	layout := dialog.Begin(m.BulkPasswordTitle,
		fmt.Sprintf(m.BulkPasswordDescriptionFmt, state.bulkPassword.eligible, state.bulkPassword.skipped))
	field := components.BeginField(canvas, theme, state.layoutSecretField(canvas, layout.Body, state.dialogError))
	content := field.Paint(m.FieldPassword, "", "")
	value := state.bulkPassword.editor.String()
	focused := canvas.Focused() == hitBulkPasswordField
	state.paintSecretInput(canvas, content, components.InputSnapshot{
		ID: hitBulkPasswordField, Label: m.FieldPassword, Value: value, Secure: true,
		Focused: focused, PlatformEditing: focused && value != "", Invalid: state.dialogError != "",
	})
	dialog.Actions(layout,
		components.ButtonAction{ID: hitBulkPasswordCancel, Label: m.Cancel, Variant: components.ButtonSecondary},
		components.ButtonAction{ID: hitBulkPasswordConfirm, Label: m.BulkPasswordConfirm},
	)
	dialog.End()
}
