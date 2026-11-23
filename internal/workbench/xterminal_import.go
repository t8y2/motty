package workbench

import (
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	"motty/internal/sessionimport"
)

type xterminalImportDialog struct {
	editor text.Editor
	area   dialogArea
}

var hitXterminalInput = tree.ID(sessionImportHits, "xterminal-input")
var hitXterminalClose = tree.ID(sessionImportHits, "xterminal-close")

func (state *State) submitXterminalImport() {
	result, err := sessionimport.ParseXterminalText(state.xterminal.editor.String())
	if err != nil {
		state.dialogError = err.Error()
		return
	}
	state.xterminal = xterminalImportDialog{}
	state.dialogError = ""
	state.acceptSessionImport(sessionimport.Xterminal, result)
}

func (state *State) declareXterminalImport(c ui.C) {
	if state.modal != modalXterminalImport {
		return
	}
	m := state.messages.Tools
	dialog := ui.Dialog(c, ui.DialogSpec{Title: m.XterminalPasteTitle, Description: m.XterminalPasteBody, Width: 640, Layer: dialogLayer, Scope: dialogScope, CloseID: hitXterminalClose})
	dialog.Body(func() {
		state.xterminal.area.declare(c, hitXterminalInput, &state.xterminal.editor, m.XterminalPasteLabel, "host=example.com port=22 user=root authType=password password=example title=Server", 240)
		if state.dialogError != "" {
			ui.Text(c, state.dialogError)
		}
	})
	dialog.Footer(func() {
		ui.Spacer(c).Grow(1)
		if ui.Button(c, state.messages.Cancel).Secondary().Clicked() {
			c.After(state.cancelXterminalImport)
		}
		if ui.Button(c, m.XterminalPasteSubmit).Clicked() {
			c.After(state.submitXterminalImport)
		}
	})
	if dialog.Dismissed() {
		c.After(state.cancelXterminalImport)
	}
}

func (state *State) cancelXterminalImport() {
	state.xterminal = xterminalImportDialog{}
	state.dialogError = ""
	state.modal = modalNone
}
