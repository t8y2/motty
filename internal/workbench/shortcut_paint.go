package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

func (state *State) shortcutActionLabel(action shortcutAction) string {
	messages := state.messages
	switch action {
	case shortcutCommandPalette:
		return messages.ShortcutCommandPalette
	case shortcutNewWindow:
		return messages.NewWindow
	case shortcutFind:
		return messages.ShortcutFind
	case shortcutCopy:
		return messages.ShortcutCopy
	case shortcutPaste:
		return messages.ShortcutPaste
	case shortcutNewTab:
		return messages.ShortcutNewTab
	case shortcutLocalTerminal:
		return messages.ShortcutLocalTerminal
	case shortcutCloseTab:
		return messages.ShortcutCloseTab
	case shortcutNextTab:
		return messages.ShortcutNextTab
	case shortcutPreviousTab:
		return messages.ShortcutPreviousTab
	case shortcutTabNumber:
		return messages.ShortcutTabNumber
	case shortcutPanePrevious:
		return messages.PanePrevious
	case shortcutPaneNext:
		return messages.PaneNext
	case shortcutPaneSplitHorizontal:
		return messages.PaneSplitHorizontal
	case shortcutPaneSplitVertical:
		return messages.PaneSplitVertical
	case shortcutPaneClose:
		return messages.PaneClose
	case shortcutFilesPath:
		return messages.ShortcutFilesPath
	case shortcutSFTP:
		return messages.SFTPFiles
	case shortcutAssistant:
		return messages.AIPanel.Toggle
	case shortcutZoomIn:
		return messages.Tools.ZoomIn
	case shortcutZoomOut:
		return messages.Tools.ZoomOut
	case shortcutZoomReset:
		return messages.Tools.ZoomReset
	case shortcutHistory:
		return messages.History.Title
	case shortcutClearScreen:
		return messages.ShortcutClearScreen
	case shortcutCompletion:
		return messages.Completion.Open
	default:
		return ""
	}
}

func (state *State) openShortcutCapture(action shortcutAction) {
	if action <= shortcutNone || action >= shortcutActionCount {
		return
	}
	state.shortcutCapture = action
	state.shortcutError = ""
	state.modal = modalShortcutCapture
}

func (state *State) clickShortcutCapture(id gfx.ID) {
	switch id {
	case hitShortcutCaptureReset:
		defaults := defaultShortcuts()
		state.captureShortcut(gfx.KeyEvent{}, defaults.chords.Chord(state.shortcutCapture))
	case hitShortcutCaptureDelete:
		state.clearShortcut(state.shortcutCapture)
	case hitShortcutCaptureDismiss, hitShortcutCaptureClose, hitShortcutCaptureCancel:
		state.modal = modalNone
		state.shortcutCapture = shortcutNone
		state.shortcutError = ""
	}
}

// shortcutRowHeight leaves room for each action and its chord.
const shortcutRowHeight = float32(54)

type completionKeyHelp struct {
	label, description, keys string
}

// The list navigation keys are fixed terminal behavior, shared by the page and
// its search index so each result leads to the explanation the user can see.
func (state *State) completionKeys() [6]completionKeyHelp {
	m := state.messages.Completion
	return [6]completionKeyHelp{
		{m.KeysAccept, "", "→ / End"},
		{m.KeysAcceptWord, "", "Alt+→ / Ctrl+→"},
		{m.KeysSelect, "", "↑/↓ · PgUp/PgDn · Home/End"},
		{m.KeysInsert, m.KeysInsertDescription, "Enter"},
		{m.KeysClose, "", "Esc"},
		{m.KeysShell, m.KeysShellDescription, "Tab"},
	}
}

// paintShortcutSettings is the Keyboard shortcuts category: one row per action
// with its chord on a button that opens the capture dialog. The page header
// holds the way back to the defaults.
func (state *State) paintShortcutSettings(page *settingsPage) {
	messages := state.messages
	page.rowHeight = shortcutRowHeight
	page.groups(func() {
		page.group("", func() {
			for action := shortcutAction(1); action < shortcutActionCount; action++ {
				page.actionRow(state.shortcutActionLabel(action), "", false,
					components.ButtonAction{ID: hitShortcutRowList.At(int(action - 1)), Label: state.shortcuts.labels[action], Variant: components.ButtonOutline})
			}
		})
		page.group(messages.Completion.KeysGroup, func() {
			for _, help := range state.completionKeys() {
				page.line(help.label, help.description, 180).Children(func() {
					ui.Text(page.c, help.keys).FontSize(metrics.FontSizeS).Color(state.pal.muted).LineHeight(21)
				})
			}
		})
	})
}

func (state *State) declareShortcutCaptureDialog(c ui.C) {
	messages := state.messages
	// The name of the action, and under it the reason a key was refused.
	body := float32(34)
	if state.shortcutError != "" {
		body = 88
	}
	viewport := dialogViewport(c)
	height := dialogContentHeight(c.Canvas(), *c.Theme(), viewport, 480, messages.ShortcutsCaptureDescription, body)
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: messages.ShortcutsCaptureTitle, Description: messages.ShortcutsCaptureDescription,
		Width: 480, Height: height, Layer: dialogLayer,
		Scope: dialogScope, DismissID: hitShortcutCaptureDismiss, CloseID: hitShortcutCaptureClose,
	})
	dialog.Body(func() {
		ui.Column(c).Fill().PadX(4).Children(func() {
			ui.Text(c, state.shortcutActionLabel(state.shortcutCapture)).FontSize(metrics.FontSizeM).Bold().Color(state.pal.text).LineHeight(24).MarginSides(8, 0, 0, 0)
			if state.shortcutError != "" {
				ui.Text(c, state.shortcutError).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(state.pal.danger).LineHeight(40).MarginSides(14, 0, 0, 0)
			}
		})
	})
	dialog.Footer(func() {
		var actions [3]components.ButtonAction
		actions[0] = components.ButtonAction{ID: hitShortcutCaptureReset, Label: messages.ShortcutsReset, Variant: components.ButtonOutline}
		count := 1
		if state.shortcuts.chords.Chord(state.shortcutCapture).Valid() {
			actions[count] = components.ButtonAction{ID: hitShortcutCaptureDelete, Label: messages.ShortcutsDelete, Variant: components.ButtonOutline}
			count++
		}
		actions[count] = components.ButtonAction{ID: hitShortcutCaptureCancel, Label: messages.Cancel, Variant: components.ButtonSecondary}
		count++
		state.dialogActionFooter(c, actions[:count]...)
	})
}
