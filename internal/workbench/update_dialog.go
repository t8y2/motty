package workbench

import (
	"fmt"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/markdown"
	"github.com/vivaui/viva/ui"
	"motty/internal/update"
)

const (
	// updateNotesMaxHeight is the most room the release notes take; longer notes
	// scroll inside it, so fifty entries do not fill the window.
	updateNotesMaxHeight float32 = 9 * updateNotesLine
	updateNotesLine      float32 = 20
	// updateNotesGutter keeps text clear of the scrollbar (6 wide) once there is one.
	updateNotesGutter float32 = 14
	// The notes sit in a bordered box; these are the room between its edge and
	// the text, and the right edge is nearer so the scrollbar hugs the border.
	updateNotesPadX     float32 = 12
	updateNotesPadY     float32 = 8
	updateNotesPadRight float32 = 4
	updateWhyLines              = 2
)

var (
	hitUpdateNotes          = tree.ID(launchHits, "update-notes")
	hitUpdateNotesScrollbar = tree.ID(launchHits, "update-notes-scrollbar")
	// Every link in the notes is one identity of its own, so a click can open it.
	hitUpdateNotesLinks = tree.Sub(launchHits, tree.Name("update-notes-links"))
)

// updateNotes is the release notes as the dialog shows them: the Markdown
// document laid out for the width it has, and where it is scrolled to.
type updateNotes struct {
	text string // the notes as published, in Markdown; empty when the release has none
	doc  markdown.Document
	view components.MarkdownView

	measuredText  string
	measuredWidth float32
	scrolls       bool

	scroll float32
	// viewHeight, contentHeight and max are as of the last paint, which the
	// wheel and the keys act on.
	viewHeight, contentHeight, max float32
	// bar is the last painted scrollbar geometry. dragging and grab keep the
	// thumb under the pointer while the primary button is held down.
	bar      components.MessageScrollerGeometry
	dragging bool
	grab     float32
}

// updateNotesStyle is the type the notes are laid out and painted in: the
// dialog's body size, in the terminal's fixed-pitch face for code.
func (state *State) updateNotesStyle() components.MarkdownStyle {
	return components.MarkdownStyle{
		Size: metrics.FontSizeS, LineHeight: updateNotesLine,
		MonoFace: state.terminalFontFace, Highlight: highlightCode,
	}
}

// updateNotesText is what the box shows: the release's notes, or a line saying
// there are none.
func (state *State) updateNotesText() string {
	if state.update.notes.text != "" {
		return state.update.notes.text
	}
	return state.messages.Tools.UpdateNotesEmpty
}

// wrappedLines is how many lines text takes in width, counting a blank line as one.
func wrappedLines(canvas *ui.Canvas, text string, size, width float32) int {
	total := 0
	var scratch []string
	for _, line := range strings.Split(text, "\n") {
		scratch = scratch[:0]
		if line != "" {
			scratch = canvas.WrapLines(scratch, line, size, ui.WeightRegular, width, 1<<12)
		}
		total += max(len(scratch), 1)
	}
	return total
}

// measureNotes lays the notes out in the width the dialog gives them and records
// their height, plus whether they are more than fit, in which case the scrollbar
// takes some of that width. Markdown shapes the height (headings and code blocks
// are taller than their lines), so it is measured rather than counted in lines.
func (state *State) measureNotes(canvas *ui.Canvas, width float32) {
	notes := &state.update.notes
	text := state.updateNotesText()
	if notes.contentHeight > 0 && notes.measuredWidth == width && notes.measuredText == text {
		return
	}
	notes.measuredText, notes.measuredWidth = text, width
	notes.doc.Parse(text)
	style := state.updateNotesStyle()
	height := notes.view.Layout(canvas, style, &notes.doc, width)
	notes.scrolls = height > updateNotesMaxHeight
	if notes.scrolls {
		height = notes.view.Layout(canvas, style, &notes.doc, width-updateNotesGutter)
	}
	notes.contentHeight = height
}

func (state *State) paintUpdateDialog(canvas *ui.Canvas, viewport ui.Rect) {
	m := state.messages.Tools
	notes := &state.update.notes
	install := state.canInstallUpdate()
	why := state.whyNoInstall()
	primary := components.ButtonAction{ID: hitUpdateDownload, Label: m.GeneralUpdateDownload}
	description := fmt.Sprintf(m.UpdateDialogDescriptionFmt, update.Current)
	if install {
		primary = components.ButtonAction{ID: hitUpdateInstall, Label: m.UpdateInstall}
		description = fmt.Sprintf(m.UpdateInstallDescriptionFmt, update.Current)
	}

	// The body is as tall as what it holds, up to the cap, so a short note does
	// not leave a gap above the buttons. The dialog narrows with the window, and
	// the notes are laid out in the width it ends up with.
	contentWidth := min(float32(560), viewport.Width-32)
	bodyWidth := contentWidth - 2*(metrics.PadSurface+metrics.GapItem)
	viewWidth := bodyWidth - updateNotesPadX - updateNotesPadRight
	state.measureNotes(canvas, viewWidth)
	notes.viewHeight = min(notes.contentHeight, updateNotesMaxHeight)
	boxHeight := notes.viewHeight + 2*updateNotesPadY
	whyHeight := float32(0)
	if why != "" {
		whyLines := min(wrappedLines(canvas, why, metrics.FontSizeS, bodyWidth), updateWhyLines)
		whyHeight = 12 + float32(whyLines)*updateNotesLine
	}

	dialog := components.BeginDialog(canvas, state.Theme(), viewport, components.DialogOptions{
		Layer: dialogLayer, Scope: dialogScope,
		DismissID: hitUpdateDismiss, CloseID: hitUpdateClose, InitialFocusID: primary.ID,
		ContentSize: ui.Size{Width: contentWidth}, FitBody: true, BodyHeight: boxHeight + whyHeight,
	})
	layout := dialog.Begin(fmt.Sprintf(m.UpdateDialogTitleFmt, state.update.release.Version), description)

	box := ui.Rect{X: layout.Body.X, Y: layout.Body.Y, Width: layout.Body.Width, Height: boxHeight}
	canvas.RoundedBorder(box, radius.control, 1, state.pal.line, state.pal.surface)
	view := ui.Rect{X: box.X + updateNotesPadX, Y: box.Y + updateNotesPadY, Width: box.Width - updateNotesPadX - updateNotesPadRight, Height: notes.viewHeight}
	if notes.scrolls {
		canvas.HitWithOptions(hitUpdateNotes, view, m.UpdateNotesLabel, ui.HitOptions{Role: ui.RoleGroup})
	}
	scroller := components.BeginMessageScroller(canvas, state.Theme(), view, notes.contentHeight, notes.scroll).
		Scrollbar(hitUpdateNotesScrollbar, m.UpdateNotesLabel, "")
	geometry := scroller.BeginContent()
	look := components.MarkdownLookFor(state.Theme())
	look.Text, look.Muted = state.pal.muted, state.pal.faint
	look.Line, look.CodeFill, look.InlineCodeFill = state.pal.line, state.pal.terminal, state.pal.surfaceSoft
	blue := state.pal.ansi[4] // the accent is nearly white; a link has to stand out from the text
	look.Link = ui.RGB(blue[0], blue[1], blue[2])
	look.Selection = withAlpha(state.pal.text, 0.25)
	ansi := func(index int) ui.Color {
		return ui.RGB(state.pal.ansi[index][0], state.pal.ansi[index][1], state.pal.ansi[index][2])
	}
	look.Code = [components.MarkdownCodeClasses]ui.Color{
		components.CodeComment: state.pal.faint, components.CodeString: ansi(2), components.CodeNumber: ansi(3),
		components.CodeKeyword: ansi(5), components.CodeFunction: ansi(4), components.CodeVariable: ansi(6),
		components.CodeFlag: ansi(6), components.CodeKey: ansi(4), components.CodeAdded: ansi(2), components.CodeRemoved: ansi(1),
	}
	notes.view.Paint(canvas, components.MarkdownPaint{
		Look: look, X: geometry.Content.X, Y: geometry.Content.Y, Clip: view, Links: hitUpdateNotesLinks,
	})
	geometry = scroller.EndContent()
	notes.scroll, notes.max = geometry.Offset, geometry.MaxOffset
	notes.bar = geometry

	if why != "" {
		f := state.partLayout.Begin(canvas, state.Theme(), ui.Rect{X: box.X, Y: box.Y + box.Height + 12, Width: box.Width, Height: whyHeight - 12})
		f.Paragraph(why, updateWhyLines).FontSize(metrics.FontSizeS).Color(state.pal.text).LineHeight(updateNotesLine)
		f.End()
	}
	dialog.Actions(layout,
		components.ButtonAction{ID: hitUpdateSkip, Label: m.UpdateSkip, Variant: components.ButtonOutline},
		components.ButtonAction{ID: hitUpdateLater, Label: m.UpdateLater, Variant: components.ButtonSecondary},
		primary,
	)
	dialog.End()
}

// scrollUpdateNotes is the wheel over the update dialog: the notes move, and
// nothing behind the dialog does.
func (state *State) scrollUpdateNotes(event ui.ScrollEvent) ui.EventResult {
	notes := &state.update.notes
	step := event.StepY(updateNotesLine)
	before := notes.scroll
	notes.scroll = min(max(before+step, 0), notes.max)
	return ui.EventResult{Handled: true, Repaint: notes.scroll != before}
}

// updateDialogPointer drags the release-notes thumb. MessageScroller supplies
// the geometry and offset mapping; the runtime pointer session belongs here.
func (state *State) updateDialogPointer(event ui.PointerEvent) (ui.EventResult, bool) {
	if state.modal != modalUpdate {
		state.update.notes.dragging = false
		return ui.EventResult{}, false
	}
	notes := &state.update.notes
	if event.Target != hitUpdateNotesScrollbar && !notes.dragging {
		return ui.EventResult{}, false
	}

	switch event.Kind {
	case ui.PointerDown:
		if event.Button != ui.PointerButtonPrimary && event.Button != ui.PointerButtonNone {
			return ui.EventResult{}, false
		}
		thumb := notes.bar.Thumb
		notes.grab = thumb.Height / 2
		if event.Y >= thumb.Y && event.Y < thumb.Y+thumb.Height {
			notes.grab = event.Y - thumb.Y
		}
		notes.dragging = true
		notes.scroll = notes.bar.OffsetForThumb(event.Y - notes.grab)
		return ui.EventResult{Handled: true, Repaint: true, Focus: event.Target}, true
	case ui.PointerMove, ui.PointerUp:
		if !notes.dragging || event.Kind == ui.PointerMove && !event.Captured {
			return ui.EventResult{}, false
		}
		before := notes.scroll
		notes.scroll = notes.bar.OffsetForThumb(event.Y - notes.grab)
		if event.Kind == ui.PointerUp {
			notes.dragging = false
		}
		return ui.EventResult{Handled: true, Repaint: notes.scroll != before}, true
	case ui.PointerExit:
		if !notes.dragging {
			return ui.EventResult{}, false
		}
		notes.dragging = false
		return ui.EventResult{Handled: true}, true
	default:
		return ui.EventResult{}, false
	}
}

func (state *State) updateKey(event ui.KeyEvent) (ui.EventResult, bool) {
	if state.modal != modalUpdate || event.Repeat && event.Key == ui.KeyEnter {
		return ui.EventResult{}, false
	}
	notes := &state.update.notes
	if event.Target == hitUpdateNotes || event.Target == hitUpdateNotesScrollbar {
		if offset, ok := notes.offsetForKey(event.Key); ok {
			notes.scroll = offset
			return ui.EventResult{Handled: true, Repaint: true}, true
		}
		return ui.EventResult{}, false
	}
	if event.Key != ui.KeyEnter {
		return ui.EventResult{}, false
	}
	switch event.Target {
	case hitUpdateInstall, hitUpdateDownload, hitUpdateSkip, hitUpdateLater:
		state.clickUpdateDialog(event.Target)
		return ui.EventResult{Handled: true, Repaint: true}, true
	}
	return ui.EventResult{}, false
}

// offsetForKey is where the keyboard moves the notes to: a line or two for the
// arrows, a screen for the page keys, the ends for Home and End.
func (notes *updateNotes) offsetForKey(key ui.Key) (float32, bool) {
	step := 2 * updateNotesLine
	page := max(notes.viewHeight*0.9, step)
	offset := notes.scroll
	switch key {
	case ui.KeyUp:
		offset -= step
	case ui.KeyDown:
		offset += step
	case ui.KeyPageUp:
		offset -= page
	case ui.KeyPageDown:
		offset += page
	case ui.KeyHome:
		offset = 0
	case ui.KeyEnd:
		offset = notes.max
	default:
		return offset, false
	}
	return min(max(offset, 0), notes.max), true
}
