package workbench

import (
	"fmt"
	"path/filepath"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
)

// stateNoticeLines is how many lines of text the dialog has room for.
const stateNoticeLines = 6

const stateNoticeLine float32 = 21

// stateNotice is the dialog about the saved state: what the title and the body
// say, built when it is painted, in the language then in use.
type stateNotice struct {
	tooNew       bool
	updateError  string // why an update did not finish, when that is what this says
	workspaceErr string // why the development workspace could not be restored
	crashReport  string // the report the previous run left when it ended abnormally
	title, body  string
	// file names what the body is about; found and supported are the formats of a
	// file that is too new.
	file, found, supported string
}

// explainStateFile opens the dialog for a state file that was not loaded as it
// was. The status line is not painted anywhere, so this is the only way the user
// learns of it; the dialog has to be dismissed.
func (state *State) explainStateFile(report loadReport) {
	switch report.outcome {
	case loadTooNew:
		state.saveBlocked = true
		state.stateNotice = stateNotice{
			tooNew: true, file: filepath.Base(state.options.StatePath), found: report.found, supported: report.supported,
		}
	case loadUnreadable:
		state.stateNotice = stateNotice{file: filepath.Base(report.kept)}
	default:
		if report.workspaceErr != nil {
			state.stateNotice = stateNotice{workspaceErr: report.workspaceErr.Error()}
			state.modal = modalStateNotice
		}
		return
	}
	state.modal = modalStateNotice
}

// buildStateNotice writes the dialog's text in the current language.
func (state *State) buildStateNotice() {
	notice, messages := &state.stateNotice, state.messages
	if notice.workspaceErr != "" {
		notice.title = messages.DevelopmentWorkspaceRestoreTitle
		notice.body = fmt.Sprintf(messages.DevelopmentWorkspaceRestoreFailedFmt, notice.workspaceErr)
		return
	}
	if notice.crashReport != "" {
		notice.title = messages.Tools.PreviousCrashTitle
		notice.body = fmt.Sprintf(messages.Tools.PreviousCrashBodyFmt, notice.crashReport)
		return
	}
	if notice.updateError != "" {
		notice.title = messages.Tools.UpdateFailedTitle
		notice.body = fmt.Sprintf(messages.Tools.UpdateFailedBodyFmt, notice.updateError)
		return
	}
	if notice.tooNew {
		notice.title = messages.StateTooNewTitle
		notice.body = fmt.Sprintf(messages.StateTooNewBodyFmt, notice.file, notice.found, notice.supported)
		return
	}
	notice.title = messages.StateUnreadableTitle
	notice.body = fmt.Sprintf(messages.StateUnreadableBodyFmt, notice.file)
}

func (state *State) paintStateNotice(canvas *gfx.Canvas, viewport gfx.Rect) {
	state.buildStateNotice() // in the current language: it can change while the dialog is up
	// The body is as tall as its text, so a short message leaves no gap above the button.
	const contentWidth float32 = 560
	bodyWidth := contentWidth - 2*(metrics.PadSurface+metrics.GapItem)
	lines := min(wrappedLines(canvas, state.stateNotice.body, metrics.FontSizeM, bodyWidth), stateNoticeLines)
	dialog := components.BeginDialog(canvas, state.Theme(), viewport, components.DialogOptions{
		Layer: dialogLayer, Scope: dialogScope,
		DismissID: hitStateNoticeDismiss, CloseID: hitStateNoticeClose, InitialFocusID: hitStateNoticeOK,
		ContentSize: gfx.Size{Width: contentWidth}, FitBody: true, BodyHeight: float32(lines) * stateNoticeLine,
	})
	// The dialog's own description is a single line; this text is longer.
	layout := dialog.Begin(state.stateNotice.title, "")
	f := state.chrome.dialog.Begin(canvas, state.Theme(), layout.Body)
	f.Column().Fill().Children(func() {
		f.Paragraph(state.stateNotice.body, lines).FontSize(metrics.FontSizeM).Color(state.pal.muted).LineHeight(stateNoticeLine)
	})
	f.End()
	dialog.Actions(layout, components.ButtonAction{ID: hitStateNoticeOK, Label: state.messages.StateNoticeOK})
	dialog.End()
}

func (state *State) clickStateNotice(id gfx.ID) {
	if id != hitStateNoticeDismiss && id != hitStateNoticeClose && id != hitStateNoticeOK {
		return
	}
	state.closeModal()
	state.offerDevelopmentImport()
	state.showNextPrompt()
}

// explainPreviousCrash tells the user, once, that the last run ended abnormally
// and where it left its report. Another dialog already up goes first; the report
// stays on disk and in the diagnostics report either way.
func (state *State) explainPreviousCrash() {
	if state.options.PreviousCrash == "" || state.modal != modalNone {
		return
	}
	state.stateNotice = stateNotice{crashReport: state.options.PreviousCrash}
	state.modal = modalStateNotice
}

// explainFailedUpdate tells the user that the last update did not finish, once.
// A dialog already up (a saved state that could not be read) goes first; the
// record of the failure stays for the next start.
func (state *State) explainFailedUpdate() {
	if state.options.Updater == nil || state.modal != modalNone {
		return
	}
	reason := state.options.Updater.TakeFailure()
	if reason == "" {
		return
	}
	state.stateNotice = stateNotice{updateError: reason}
	state.modal = modalStateNotice
}
