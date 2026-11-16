package workbench

import (
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

// declareHostTransferChoice declares the file protocol choice in the form's page
// at the slot the body reserved for it. The click already wrote editor.transfer;
// only the form's error has to go.
func (state *State) declareHostTransferChoice(c ui.C) ui.Box {
	choice := hostFormChoice(c, state.messages.Tools.TransferField, hostFormRowHeight, &state.editor.transfer, hostTransferChoices[:], state.hostTransferChoiceLabel)
	if choice.Changed() {
		state.dialogError = ""
	}
	return choice
}
