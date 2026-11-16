package workbench

import (
	"errors"
	"io/fs"
	"log"

	"github.com/vivaui/viva/components"
)

// Explicit actions need feedback on pages that do not display state.status.
// Share SFTP's bounded, deduplicated Viva Toast queue and request a repaint for
// keyboard and command-palette actions as well as pointer clicks.
func (state *State) showActionWarning(title, detail string) {
	state.showActionNotice(title, detail, components.ToastWarning)
}

func (state *State) showActionError(title, detail string) {
	state.showActionNotice(title, detail, components.ToastError)
}

func (state *State) showActionInfo(title, detail string) {
	state.showActionNotice(title, detail, components.ToastInfo)
}

// Keep native errors and parser details in the diagnostic log. Notices only
// receive localized explanations and a useful next step.
func (state *State) showActionFailure(title string, err error, detail string) {
	log.Printf("motty: %s: %v", title, err)
	state.showActionError(title, detail)
}

func (state *State) showFileReadFailure(title string, err error, fallback string) {
	m := state.messages.ActionFeedback
	switch {
	case errors.Is(err, fs.ErrNotExist):
		fallback = m.FileMissing
	case errors.Is(err, fs.ErrPermission):
		fallback = m.ReadDenied
	}
	state.showActionFailure(title, err, fallback)
}

func (state *State) showFileSaveFailure(title string, err error) {
	m := state.messages.ActionFeedback
	detail := m.SaveFailed
	switch {
	case errors.Is(err, fs.ErrNotExist):
		detail = m.SaveFolderMissing
	case errors.Is(err, fs.ErrPermission):
		detail = m.SaveDenied
	}
	state.showActionFailure(title, err, detail)
}

func (state *State) showActionNotice(title, detail string, kind components.ToastType) {
	state.status = detail
	state.fileNotices.show(title, detail, kind)
	if state.options.Invalidate != nil {
		state.options.Invalidate()
	}
}
