package workbench

import (
	"fmt"
	"strconv"
	"unicode/utf8"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/flex"
	"github.com/vivaui/viva/ui"
	"motty/internal/term"
)

type terminalClipboardPolicy uint8

const (
	terminalClipboardBlock terminalClipboardPolicy = iota
	terminalClipboardAsk
	terminalClipboardAllow
)

func (policy terminalClipboardPolicy) String() string {
	switch policy {
	case terminalClipboardBlock:
		return "block"
	case terminalClipboardAllow:
		return "allow"
	default:
		return "ask"
	}
}

func terminalClipboardPolicyFromString(value string) terminalClipboardPolicy {
	switch value {
	case terminalClipboardBlock.String():
		return terminalClipboardBlock
	case terminalClipboardAllow.String():
		return terminalClipboardAllow
	default:
		return terminalClipboardAsk
	}
}

type remoteClipboardRequest struct {
	sessionID   uint32
	text        string
	description string
	preview     string
	pending     bool
}

func (state *State) remoteClipboardWrite(sessionID uint32, write term.ClipboardWrite) {
	switch state.terminalClipboard {
	case terminalClipboardBlock:
		state.status = state.messages.StatusClipboardBlocked
	case terminalClipboardAllow:
		state.setRemoteClipboard(write.Text)
	case terminalClipboardAsk:
		if state.clipboardRequest.pending {
			return
		}
		session, ok := state.sessionByIDPointer(sessionID)
		if !ok {
			return
		}
		state.clipboardRequest = remoteClipboardRequest{
			sessionID:   sessionID,
			text:        write.Text,
			description: fmt.Sprintf(state.messages.ClipboardRequestDescriptionFmt, session.Address),
			preview:     clipboardPreview(write.Text),
			pending:     true,
		}
		state.showNextPrompt()
	}
}

func (state *State) setRemoteClipboard(text string) {
	if err := state.options.SetClipboardText(text); err != nil {
		state.status = fmt.Sprintf(state.messages.StatusClipboardFailedFmt, err)
		return
	}
	state.status = state.messages.StatusClipboardCopied
}

func (state *State) clickRemoteClipboard(id ui.ID) {
	request := state.clipboardRequest
	switch id {
	case hitClipboardAllow:
		state.setRemoteClipboard(request.text)
	case hitClipboardDismiss, hitClipboardClose, hitClipboardCancel:
		state.status = state.messages.StatusClipboardBlocked
	default:
		return
	}
	state.closeModal()
	state.showNextPrompt()
}

func clipboardPreview(text string) string {
	const maxPreviewRunes = 160
	if utf8.RuneCountInString(text) > maxPreviewRunes {
		runes := []rune(text)
		text = string(runes[:maxPreviewRunes]) + "…"
	}
	return strconv.QuoteToGraphic(text)
}

func (state *State) paintRemoteClipboardDialog(canvas *ui.Canvas, viewport ui.Rect) {
	messages, palette := state.messages, state.pal
	dialog := components.BeginDialog(canvas, state.Theme(), viewport, components.DialogOptions{
		Layer: dialogLayer, Scope: dialogScope,
		DismissID: hitClipboardDismiss, CloseID: hitClipboardClose, InitialFocusID: hitClipboardCancel,
		ContentSize: ui.Size{Width: 560}, FitBody: true, BodyHeight: 66, // the preview box
	})
	layout := dialog.Begin(messages.ClipboardRequestTitle, state.clipboardRequest.description)
	// The text the program wants to copy, in a box of its own.
	f := state.partLayout.Begin(canvas, state.Theme(), layout.Body)
	f.Column().Fill().Children(func() {
		f.Row().Height(58).Shrink(0).Items(flex.Center).PadX(11).MarginSides(8, 0, 0, 0).
			Border(1, palette.line).Background(palette.surface).Radius(radius.control).Children(func() {
			f.Text(state.clipboardRequest.preview).FontSize(metrics.FontSizeS).Color(palette.text)
		})
	})
	f.End()
	dialog.Actions(layout,
		components.ButtonAction{ID: hitClipboardCancel, Label: messages.Cancel, Variant: components.ButtonSecondary},
		components.ButtonAction{ID: hitClipboardAllow, Label: messages.ClipboardAllowOnce},
	)
	dialog.End()
}
