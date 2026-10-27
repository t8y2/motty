package workbench

import (
	"github.com/vivaui/viva/components"
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

var (
	hitDialogTransferAuto = tree.ID(hostProtocolUiHits, "dialog-transfer-auto")
	hitDialogTransferSFTP = tree.ID(hostProtocolUiHits, "dialog-transfer-sftp")
	hitDialogTransferSCP  = tree.ID(hostProtocolUiHits, "dialog-transfer-scp")
)

// paintTransferChoice draws the file protocol choice of the host dialog.
func (state *State) paintTransferChoice(canvas *ui.Canvas, bounds ui.Rect) {
	m := state.messages.Tools
	content := components.BeginField(canvas, state.Theme(), bounds).Paint(m.TransferField, "", "")
	options := [...]components.SegmentOption{
		{ID: hitDialogTransferAuto, Label: m.TransferAuto},
		{ID: hitDialogTransferSFTP, Label: "SFTP"},
		{ID: hitDialogTransferSCP, Label: "SCP"},
	}
	components.BeginSegmentedControl(canvas, state.Theme(), content, options[:], options[state.editor.transfer].ID).Paint()
	state.rememberHostControl(options[state.editor.transfer].ID, content)
}

// clickTransferChoice handles a click on the choice and reports whether it was one.
func (state *State) clickTransferChoice(id ui.ID) bool {
	switch id {
	case hitDialogTransferAuto:
		state.editor.transfer = transferAuto
	case hitDialogTransferSFTP:
		state.editor.transfer = transferSFTP
	case hitDialogTransferSCP:
		state.editor.transfer = transferSCP
	default:
		return false
	}
	return true
}
