package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// transferMode says which file protocol a host's file panel uses.
type transferMode uint8

const (
	// transferAuto opens SFTP and, when the server has no SFTP subsystem, falls
	// back to SCP.
	transferAuto transferMode = iota
	// transferSFTP never falls back.
	transferSFTP
	// transferSCP skips SFTP, for a server known to offer only SCP.
	transferSCP
)

// savedTransfer is the state-file and catalog form; automatic is empty.
func (host Host) savedTransfer() string {
	switch host.Transfer {
	case transferSFTP:
		return "sftp"
	case transferSCP:
		return "scp"
	}
	return ""
}

// applyTransfer reads the saved form and reports false for one it does not know.
func (host *Host) applyTransfer(saved string) bool {
	switch saved {
	case "":
		host.Transfer = transferAuto
	case "sftp":
		host.Transfer = transferSFTP
	case "scp":
		host.Transfer = transferSCP
	default:
		return false
	}
	return true
}

// hostTransferChoices is the file protocol choice of the host form.
var hostTransferChoices = [...]transferMode{transferAuto, transferSFTP, transferSCP}

// hostTransferChoiceLabel names one file protocol as the form's choice shows it.
func (state *State) hostTransferChoiceLabel(mode transferMode) string {
	switch mode {
	case transferSFTP:
		return "SFTP"
	case transferSCP:
		return "SCP"
	default:
		return state.messages.Tools.TransferAuto
	}
}

// paintTransferChoice draws the file protocol choice of the host dialog.
func (state *State) paintTransferChoice(canvas *gfx.Canvas, bounds gfx.Rect) {
	m, editor := state.messages.Tools, &state.editor
	content := components.BeginField(canvas, state.Theme(), bounds).Paint(m.TransferField, "", "")
	state.drawDialogChoice(canvas, content, &editor.transferChoice, "host-dialog-transfer", hostTransferChoice)
	state.rememberHostChoice(canvas, content, &editor.transferChoice, state.hostTransferChoiceLabel(editor.transfer))
}

// declareHostTransferChoice is the page of the file protocol choice. The click
// already wrote editor.transfer; only the form's error has to go.
func (state *State) declareHostTransferChoice(c ui.C) {
	choice := ui.Choice(c, &state.editor.transfer, hostTransferChoices[:], state.hostTransferChoiceLabel).Fill()
	if choice.Changed() {
		state.dialogError = ""
	}
}
