package workbench

import (
	"fmt"
	"slices"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"motty/internal/sftpclient"
)

var (
	hitTransferConflictCancel     = tree.ID(transferCenterHits, "conflict-cancel")
	hitTransferConflictSkipAll    = tree.ID(transferCenterHits, "conflict-skip-all")
	hitTransferConflictReplaceAll = tree.ID(transferCenterHits, "conflict-replace-all")
	hitTransferConflictReplace    = tree.ID(transferCenterHits, "conflict-replace")
)

// A prompt captures a finite set of conflicts from one connection. New tasks,
// other connections and newer retry attempts cannot inherit its overwrite choice.
func (state *State) pruneTransferConflict(view *sftpView) bool {
	prompt := &view.conflictPrompt
	prompt.targets = slices.DeleteFunc(prompt.targets, func(target transferTarget) bool {
		entry := state.transfers.capturedEntry(target)
		return entry == nil || entry.snapshot.QueueOrder != target.attempt || !transferRetryEligible(entry, true)
	})
	return len(prompt.targets) > 0
}

func (state *State) transferConflictView() *sftpView {
	if !state.transferCenterAvailable() {
		return nil
	}
	// A file tab declares its own prompt; prioritize it so another connection
	// cannot declare a second dialog over that same page.
	if w := state.activeSFTPWindow(); w != nil {
		if !state.transferConflictAllowed(w.view) {
			return nil
		}
		if state.pruneTransferConflict(w.view) {
			return w.view
		}
	}
	if state.activeSection == sectionTerminal && state.filesPanelShown() {
		if session, ok := state.sessionByIDPointer(state.activeSessionID); ok && session.files != nil && session.files.dialog != sftpDialogNone {
			return nil
		}
	}
	for index := range state.transfers.entries {
		view := state.transfers.entries[index].owner
		if view == nil || view.detached != nil || len(view.conflictPrompt.targets) == 0 || !state.transferConflictAllowed(view) {
			continue
		}
		if state.pruneTransferConflict(view) {
			return view
		}
	}
	return nil
}

func (state *State) transferConflictAllowed(view *sftpView) bool {
	if !state.transferCenterAvailable() || view.dialog != sftpDialogNone {
		return false
	}
	w := view.detached
	// A new automatic prompt must wait for an existing choice on this page.
	if (w == nil || w == state.activeSFTPWindow()) && state.transfers.open && len(state.transfers.batch.targets) > 0 {
		return false
	}
	return w == nil || !w.closeConfirm && w.externalConflict == 0 && !w.bookmark.open && !w.fileProperties.open && len(w.ui.queueBatch.targets) == 0
}

func (state *State) transferConflictShowing(view *sftpView) bool {
	if view == nil || len(view.conflictPrompt.targets) == 0 || !state.transferConflictAllowed(view) {
		return false
	}
	for _, target := range view.conflictPrompt.targets {
		entry := state.transfers.capturedEntry(target)
		if entry != nil && entry.snapshot.QueueOrder == target.attempt && transferRetryEligible(entry, true) {
			return true
		}
	}
	return false
}

func (state *State) declareTransferConflict(c ui.C, view *sftpView) {
	if !state.transferConflictAllowed(view) || !state.pruneTransferConflict(view) {
		return
	}
	prompt, m := &view.conflictPrompt, state.messages.Transfers
	entry := state.transfers.capturedEntry(prompt.targets[0])
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title:       m.ConflictTitle,
		Description: fmt.Sprintf(m.ConflictPromptFmt, entry.title, len(prompt.targets)),
		Width:       560, Layer: dialogLayer + 2, Scope: dialogScope,
		Header:  components.AlertDialogHeader(c.Theme().Metrics),
		CloseID: hitTransferConflictCancel, InitialFocusID: hitTransferConflictCancel, NoClose: true,
	})
	dialog.Footer(func() {
		state.dialogActionFooterSmall(c,
			components.ButtonAction{ID: hitTransferConflictCancel, Label: state.messages.Cancel, Variant: components.ButtonOutline},
			components.ButtonAction{ID: hitTransferConflictSkipAll, Label: m.SkipAll, Variant: components.ButtonOutline},
			components.ButtonAction{ID: hitTransferConflictReplaceAll, Label: m.ReplaceAll, Variant: components.ButtonOutline},
			components.ButtonAction{ID: hitTransferConflictReplace, Label: m.Replace, Variant: components.ButtonDefault},
		)
	})
}

func (state *State) clickTransferConflict(view *sftpView, id gfx.ID) bool {
	if !state.transferConflictAllowed(view) || !state.pruneTransferConflict(view) {
		return false
	}
	switch id {
	case hitTransferConflictCancel:
		view.conflictPrompt = transferBatch{}
		view.invalidateDetached()
	case hitTransferConflictSkipAll, hitTransferConflictReplaceAll, hitTransferConflictReplace:
		batch := view.conflictPrompt
		view.conflictPrompt = transferBatch{}
		if id == hitTransferConflictSkipAll {
			batch.policy = sftpclient.ExistingSkip
		}
		if id == hitTransferConflictReplace {
			view.conflictPrompt = batch
			view.conflictPrompt.targets = slices.Clone(batch.targets[1:])
			batch.targets = batch.targets[:1]
		}
		state.dispatchTransferBatch(batch)
		// Conflicts that arrived after this prompt need their own choice.
		state.showTransferConflict(view)
		view.invalidateDetached()
	default:
		return false
	}
	return true
}

func (state *State) transferConflictKey(view *sftpView, event gfx.KeyEvent) (gfx.EventResult, bool) {
	if event.Release || event.Modifiers != 0 || !state.transferConflictAllowed(view) || !state.pruneTransferConflict(view) {
		return gfx.EventResult{}, false
	}
	if event.Key == gfx.KeyEscape {
		state.clickTransferConflict(view, hitTransferConflictCancel)
		return gfx.EventResult{Handled: true, Repaint: true}, true
	}
	if (event.Key == gfx.KeyEnter || event.Key == gfx.KeySpace) && !event.Repeat && state.clickTransferConflict(view, event.Target) {
		return gfx.EventResult{Handled: true, Repaint: true}, true
	}
	return gfx.EventResult{}, false
}
