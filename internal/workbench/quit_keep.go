package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// The question a quit asks when the user chose "ask" for keeping windows. It is
// shown once, on the window the user is looking at, before the application
// closes every window.

var quitKeepWindowsHits = tree.Feature("quit-keep-windows")

var (
	hitQuitKeepWindowsDismiss = tree.ID(quitKeepWindowsHits, "dismiss")
	hitQuitKeepWindowsClose   = tree.ID(quitKeepWindowsHits, "close")
	hitQuitKeepWindowsCancel  = tree.ID(quitKeepWindowsHits, "cancel")
	hitQuitKeepWindowsDiscard = tree.ID(quitKeepWindowsHits, "discard")
	hitQuitKeepWindowsKeep    = tree.ID(quitKeepWindowsHits, "keep")
)

type quitKeepWindowsPrompt struct {
	answer func(keep bool)
	cancel func()
	// previous is the dialog the question replaced. Dismissing the question puts
	// it back, so a quit request made while a form is open does not throw the
	// form away.
	previous modalKind
}

// KeepsWindowsOnExit reports whether a quit leaves this window's record for the
// next launch.
func (state *State) KeepsWindowsOnExit() bool { return state.keepWindows == keepWindowsAlways }

// AsksAboutWindowsOnExit reports whether a quit with windows open must ask
// whether to keep them.
func (state *State) AsksAboutWindowsOnExit() bool { return state.keepWindows == keepWindowsAsk }

// DiscardsWindowsOnExit reports whether a quit throws the windows away without
// asking.
func (state *State) DiscardsWindowsOnExit() bool { return state.keepWindows == keepWindowsNever }

// AskKeepWindowsOnExit shows the question and reports whether it took over. A
// setting that does not ask leaves the caller to quit directly. answer runs
// after the question closes with the user's choice; cancel runs when the user
// dismisses it, leaving the application running.
func (state *State) AskKeepWindowsOnExit(answer func(keep bool), cancelled func()) bool {
	if !state.AsksAboutWindowsOnExit() {
		return false
	}
	if state.options.Raise != nil {
		state.options.Raise()
	}
	state.quitKeepWindows = quitKeepWindowsPrompt{answer: answer, cancel: cancelled, previous: state.modal}
	state.modal = modalQuitKeepWindows
	return true
}

func (state *State) clickQuitKeepWindows(id gfx.ID) {
	if state.modal != modalQuitKeepWindows {
		return
	}
	prompt := state.quitKeepWindows
	switch id {
	case hitQuitKeepWindowsKeep, hitQuitKeepWindowsDiscard:
		state.quitKeepWindows = quitKeepWindowsPrompt{}
		state.closeModal()
		if prompt.answer != nil {
			prompt.answer(id == hitQuitKeepWindowsKeep)
		}
	case hitQuitKeepWindowsCancel, hitQuitKeepWindowsClose, hitQuitKeepWindowsDismiss:
		// The question only borrowed the modal slot: put the dialog it replaced
		// back untouched. closeModal would clear the form it belongs to, the
		// password fields and the session the prompt was opened on, so a quit
		// the user cancels would lose a half-filled form.
		state.quitKeepWindows = quitKeepWindowsPrompt{}
		state.modal = prompt.previous
		if prompt.cancel != nil {
			prompt.cancel()
		}
	}
}

// CancelKeepWindowsOnExit dismisses an open quit question as if the user
// cancelled it, restoring the dialog it replaced. It reports whether a question
// was open. A window that closes or hides while holding the question must call
// it, or the process-wide guard would keep every later quit request waiting for
// an answer that can never come.
func (state *State) CancelKeepWindowsOnExit() bool {
	if state.modal != modalQuitKeepWindows {
		return false
	}
	state.clickQuitKeepWindows(hitQuitKeepWindowsCancel)
	return true
}

// declareQuitKeepWindowsDialog declares the question as a dialog of the
// workbench page.
func (state *State) declareQuitKeepWindowsDialog(c ui.C) {
	messages := state.messages.Tools
	description := messages.QuitKeepWindowsDescription
	viewport := dialogViewport(c)
	height := dialogContentHeight(c.Canvas(), *c.Theme(), viewport, 520, description, 0)
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: messages.QuitKeepWindowsTitle, Description: description, Width: 520, Height: height, Layer: dialogLayer,
		Scope: dialogScope, DismissID: hitQuitKeepWindowsDismiss, CloseID: hitQuitKeepWindowsClose, InitialFocusID: hitQuitKeepWindowsKeep,
	})
	dialog.Footer(func() {
		state.dialogActionFooter(c,
			components.ButtonAction{ID: hitQuitKeepWindowsCancel, Label: state.messages.Cancel, Variant: components.ButtonSecondary},
			components.ButtonAction{ID: hitQuitKeepWindowsDiscard, Label: messages.QuitKeepWindowsDiscard, Variant: components.ButtonOutline},
			components.ButtonAction{ID: hitQuitKeepWindowsKeep, Label: messages.QuitKeepWindowsKeep, Variant: components.ButtonDefault},
		)
	})
}
