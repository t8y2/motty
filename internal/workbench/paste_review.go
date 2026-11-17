package workbench

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
)

// Multi-line paste review: a paste that contains a line break is held in a
// dialog until the user chooses to send it as the program expects (the same
// bytes as a normal paste) or line by line. The dialog's preview is editable,
// so what is sent is what the dialog shows. Single-line text, images and the
// AI login terminal keep their old paths.

var pasteReviewHits = tree.Feature("terminal-paste-review")

var (
	hitPasteReviewEditor  = tree.ID(pasteReviewHits, "preview")
	hitPasteReviewScroll  = tree.ID(pasteReviewHits, "preview-scroll")
	hitPasteReviewCancel  = tree.ID(pasteReviewHits, "cancel")
	hitPasteReviewWhole   = tree.ID(pasteReviewHits, "paste")
	hitPasteReviewLines   = tree.ID(pasteReviewHits, "line-by-line")
	hitPasteReviewClose   = tree.ID(pasteReviewHits, "close")
	hitPasteReviewDismiss = tree.ID(pasteReviewHits, "dismiss")

	hitSettingsPasteConfirmToggle = tree.ID(pasteReviewHits, "settings-paste-confirm")
)

const (
	// Above this size the dialog shows a read-only preview of the beginning and
	// sends the full text anyway; laying out a huge paste on every keystroke
	// would stall the window.
	pasteReviewEditableBytes = 64 << 10
	pasteReviewEditableLines = 4096
)

// pasteReviewUI is the open review. It snapshots the session so a reconnect or
// a closed tab invalidates the actions instead of sending to a replaced shell.
type pasteReviewUI struct {
	sessionID  uint32
	owner      *terminal
	token      string // folderToken when the dialog opened
	normalized string // the full text, line endings normalized to \n
	readOnly   bool
	preview    string // the truncated text shown when readOnly
	editor     text.Editor
	code       components.CodeEditor
	box        ui.Box
	bounds     gfx.Rect
}

// reviewTerminalPaste holds a multi-line paste for review and reports whether it
// did. A paste that is not multiple lines, a disabled setting, a session without
// a live shell, and an already-open modal all keep the old path.
func (state *State) reviewTerminalPaste(session *Session, value string) bool {
	if state.pasteConfirmOff || state.modal != modalNone || session == nil || session.term == nil ||
		session.term.shell == nil || session.term.ended {
		return false
	}
	normalized := normalizePasteText(value)
	if !strings.Contains(normalized, "\n") {
		return false
	}
	state.cancelPasteQueue(session.term)
	review := &state.pasteReview
	*review = pasteReviewUI{sessionID: session.ID, owner: session.term, token: session.term.folderToken}
	if len(normalized) > pasteReviewEditableBytes || pasteLineCount(normalized) > pasteReviewEditableLines {
		review.readOnly = true
		review.normalized = normalized
		review.preview = truncatePastePreview(normalized, pasteReviewEditableBytes, pasteReviewEditableLines)
	} else {
		review.editor = *text.NewEditor(normalized)
	}
	state.modalSession = session.ID
	state.modal = modalPasteReview
	return true
}

// pasteReviewSession returns the session the review belongs to, or nil when the
// terminal has been replaced, disconnected or ended since the dialog opened.
func (state *State) pasteReviewSession() *Session {
	review := &state.pasteReview
	session, ok := state.sessionByIDPointer(review.sessionID)
	if !ok || session.term == nil || session.term != review.owner ||
		session.term.folderToken != review.token || session.term.shell == nil || session.term.ended {
		return nil
	}
	return session
}

// pasteReviewText is what the actions would send: the edited preview, or the
// full text when the preview is read-only.
func (state *State) pasteReviewText() string {
	if state.pasteReview.readOnly {
		return state.pasteReview.normalized
	}
	return state.pasteReview.editor.String()
}

func (state *State) pasteReviewDisplayText() string {
	if state.pasteReview.readOnly {
		return state.pasteReview.preview
	}
	return state.pasteReview.editor.String()
}

func (state *State) pasteReviewComposing() bool {
	if state.pasteReview.readOnly {
		return false
	}
	_, composing := state.pasteReview.editor.Composition()
	return composing
}

func (state *State) pasteReviewCanSend() bool {
	return state.pasteReviewText() != "" && !state.pasteReviewComposing()
}

// pasteReviewSummary counts the lines that line-by-line would submit and the
// characters shown in the preview. It follows the editor while the user edits.
func (state *State) pasteReviewSummary() (lines, chars int) {
	value := state.pasteReviewText()
	return pasteLineCount(value), utf8.RuneCountInString(value)
}

// normalizePasteText makes the line endings the dialog works with. Sending the
// whole text goes through term.EncodePaste, which normalizes again to CR.
func normalizePasteText(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	return strings.ReplaceAll(value, "\r", "\n")
}

// pasteLineCount counts logical lines after dropping one trailing newline, so
// "a\n" is one line, "\n" is one empty line and "a\n\n" keeps its blank line.
func pasteLineCount(value string) int {
	value = normalizePasteText(value)
	if value == "" {
		return 0
	}
	value = strings.TrimSuffix(value, "\n")
	return strings.Count(value, "\n") + 1
}

// splitPasteLines prepares the line-by-line queue: each logical line is
// submitted with its own Enter, and a trailing newline does not add one more.
func splitPasteLines(value string) []string {
	value = normalizePasteText(value)
	if value == "" {
		return nil
	}
	value = strings.TrimSuffix(value, "\n")
	return strings.Split(value, "\n")
}

// truncatePastePreview cuts the read-only preview at a natural boundary.
func truncatePastePreview(value string, maxBytes, maxLines int) string {
	if len(value) > maxBytes {
		value = value[:maxBytes]
		for len(value) > 0 && !utf8.ValidString(value) {
			value = value[:len(value)-1]
		}
	}
	if lines := strings.Split(value, "\n"); len(lines) > maxLines {
		value = strings.Join(lines[:maxLines], "\n")
	}
	return value
}

func (state *State) declarePasteReviewDialog(c ui.C) {
	review := &state.pasteReview
	m := state.messages.PasteReview
	lines, chars := state.pasteReviewSummary()
	summary := fmt.Sprintf(m.SummaryFmt, lines, chars)
	viewport := dialogViewport(c)
	width := min(float32(560), max(float32(320), viewport.Width-24))
	previewHeight := min(float32(240), max(float32(120), viewport.Height*0.3))
	hintLines := max(1, wrappedLines(c.Canvas(), m.Hint, metrics.FontSizeS, width-48))
	bodyHeight := previewHeight + float32(hintLines)*16 + 6
	if review.readOnly {
		noteLines := max(1, wrappedLines(c.Canvas(), fmt.Sprintf(m.TooLargeFmt, lines), metrics.FontSizeS, width-48))
		bodyHeight += float32(noteLines)*16 + 6
	}
	height := dialogContentHeight(c.Canvas(), *c.Theme(), viewport, width, summary, bodyHeight)
	initial := hitPasteReviewEditor
	if review.readOnly {
		initial = hitPasteReviewCancel
	}
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: m.Title, Description: summary, Width: width, Height: height, Layer: dialogLayer,
		Scope: dialogScope, DismissID: hitPasteReviewDismiss, CloseID: hitPasteReviewClose, InitialFocusID: initial,
	})
	dialog.Body(func() {
		column := ui.Column(c).Fill().Gap(6)
		column.Children(func() {
			if review.readOnly {
				ui.Paragraph(c, fmt.Sprintf(m.TooLargeFmt, lines), 0).FontSize(metrics.FontSizeS).Color(state.pal.muted).LineHeight(16)
			}
			review.box = ui.Slot(c).Height(previewHeight)
			ui.Paragraph(c, m.Hint, 0).FontSize(metrics.FontSizeS).Color(state.pal.muted).LineHeight(16)
		})
		column.DrawOver(func(d *ui.DrawContext) {
			state.drawPasteReviewPreview(d)
		})
	})
	dialog.Footer(func() {
		disabled := !state.pasteReviewCanSend()
		state.dialogActionFooter(c,
			components.ButtonAction{ID: hitPasteReviewCancel, Label: state.messages.Cancel, Variant: components.ButtonSecondary},
			components.ButtonAction{ID: hitPasteReviewLines, Label: m.Lines, Variant: components.ButtonOutline, Disabled: disabled},
			components.ButtonAction{ID: hitPasteReviewWhole, Label: m.Whole, Variant: components.ButtonDefault, Disabled: disabled},
		)
	})
}

func (state *State) drawPasteReviewPreview(d *ui.DrawContext) {
	review := &state.pasteReview
	bounds := review.box.Rect()
	if bounds.Width <= 0 || bounds.Height <= 0 {
		return
	}
	review.bounds = bounds
	focused := d.Canvas.Focused() == hitPasteReviewEditor && !review.readOnly
	snapshot := components.CodeEditorSnapshot{
		ID: hitPasteReviewEditor, Label: state.messages.PasteReview.EditorLabel,
		Value: state.pasteReviewDisplayText(), ReadOnly: review.readOnly, Focused: focused,
		FontSize: state.terminalFontSize, MonoFace: state.terminalFontFace,
		CaretColor: state.pal.cursor, TabWidth: 4, Wrap: true,
		ScrollbarID: hitPasteReviewScroll,
	}
	if !review.readOnly {
		selection := review.editor.Selection()
		snapshot.ShowCaret = focused && selection.Start == selection.End
		snapshot.SelectionStart = review.editor.ByteOffset(selection.Start)
		snapshot.SelectionEnd = review.editor.ByteOffset(selection.End)
		snapshot.Caret = review.editor.ByteOffset(review.editor.Caret())
		if marked, ok := review.editor.Composition(); ok {
			snapshot.CompositionStart = review.editor.ByteOffset(marked.Start)
			snapshot.CompositionEnd = review.editor.ByteOffset(marked.End)
		}
	}
	review.code.Paint(d.Canvas, *d.Theme, bounds, snapshot)
}

func (state *State) clickPasteReview(id gfx.ID) {
	switch id {
	case hitPasteReviewCancel, hitPasteReviewClose, hitPasteReviewDismiss:
		state.closeModal()
	case hitPasteReviewWhole:
		state.confirmPasteWhole()
	case hitPasteReviewLines:
		state.confirmPasteLines()
	}
}

func (state *State) confirmPasteWhole() {
	session := state.pasteReviewSession()
	if session == nil {
		state.finishStalePasteReview()
		return
	}
	payload := state.pasteReviewText()
	if payload == "" || state.pasteReviewComposing() {
		return
	}
	state.closeModal()
	// The preview may have been edited down to one line with a smart character;
	// the guard sees the text that is actually about to be sent.
	if state.guardTerminalText(session, app.TextEvent{Kind: app.TextPaste, Text: payload}) {
		return
	}
	if !state.typed(session, encodeTerminalPaste(payload, session.term)) {
		state.status = state.messages.PasteReview.Stopped
	}
}

func (state *State) confirmPasteLines() {
	session := state.pasteReviewSession()
	if session == nil {
		state.finishStalePasteReview()
		return
	}
	value := state.pasteReviewText()
	if value == "" || state.pasteReviewComposing() {
		return
	}
	state.closeModal()
	state.startPasteQueue(session, splitPasteLines(value))
}

func (state *State) finishStalePasteReview() {
	state.closeModal()
	state.status = state.messages.PasteReview.Stale
}

// pasteReviewKey routes the dialog's keys. The editor keeps every key the text
// system needs (characters, Enter, the input method); only movement inside the
// wrapped preview is handled here. Tab is always left to the focus scope.
func (state *State) pasteReviewKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	if state.modal != modalPasteReview {
		return gfx.EventResult{}, false
	}
	review := &state.pasteReview
	if event.Target == hitPasteReviewEditor && !review.readOnly {
		if state.pasteReviewComposing() {
			return gfx.EventResult{}, false
		}
		if !event.Repeat && !event.Release && event.Modifiers == 0 && event.Key == gfx.KeyEscape {
			state.clickPasteReview(hitPasteReviewCancel)
			return gfx.EventResult{Handled: true, Repaint: true}, true
		}
		if movement, extend := components.TextareaMovementForKey(event.Key, event.Modifiers); movement != components.TextareaMoveNone {
			value := review.editor.String()
			offset := review.code.Move(value, review.editor.ByteOffset(review.editor.Caret()), movement)
			review.editor.MoveTo(utf8.RuneCountInString(value[:offset]), extend)
			review.code.Reveal(offset)
			return gfx.EventResult{Handled: true, Repaint: true}, true
		}
		return gfx.EventResult{}, false
	}
	if !event.Repeat && !event.Release && event.Modifiers == 0 {
		switch {
		case event.Key == gfx.KeyEscape:
			state.clickPasteReview(hitPasteReviewCancel)
		case (event.Key == gfx.KeyEnter || event.Key == gfx.KeySpace) &&
			(event.Target == hitPasteReviewWhole || event.Target == hitPasteReviewLines || event.Target == hitPasteReviewCancel):
			state.clickPasteReview(event.Target)
		}
	}
	return gfx.EventResult{Handled: event.Key != gfx.KeyTab, Repaint: true}, true
}

// scrollPasteReview keeps the wheel inside the dialog: over the preview it
// scrolls the preview, everywhere else it is swallowed.
func (state *State) scrollPasteReview(event gfx.ScrollEvent) gfx.EventResult {
	if pointInRect(event.X, event.Y, state.pasteReview.bounds) {
		return state.pasteReview.code.Scroll(event)
	}
	return gfx.EventResult{Handled: true}
}
