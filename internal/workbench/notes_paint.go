package workbench

import (
	"time"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text/find"
)

func (s *State) paintNoteSource(c *gfx.Canvas, n *notesUI, b gfx.Rect) {
	n.sourceBounds = b
	value := n.body.String()
	sel, _ := n.noteSelections()
	snap := components.CodeEditorSnapshot{ID: hitNoteBody, Label: s.noteText("Markdown source", "Markdown 源码"), Value: value, Focused: c.Focused() == hitNoteBody, Wrap: true, LineNumbers: true, SelectionStart: aiByteOffset(value, sel.Start), SelectionEnd: aiByteOffset(value, sel.End), Caret: aiByteOffset(value, sel.End), ShowCaret: sel.Start == sel.End, Language: "markdown", Highlight: s.highlightNoteMarkdown, ScrollbarID: hitNoteSourceScroll}
	snap.CurrentLine, snap.MatchBrackets = true, true
	snap.FontSize, snap.MonoFace, snap.TabWidth = noteFontSize, s.terminalFontFace, 4
	if n.editing.findOpen {
		snap.Marks = n.editing.find.Matches()
		if index := find.Index(snap.Marks, snap.SelectionStart, snap.SelectionEnd); index >= 0 {
			snap.CurrentMark = snap.Marks[index]
		}
	}
	if r, ok := n.body.Composition(); ok {
		snap.CompositionStart = aiByteOffset(value, r.Start)
		snap.CompositionEnd = aiByteOffset(value, r.End)
	}
	n.code.Paint(c, s.Theme(), b, snap)
}

// paintNotePreviewContent paints the note's Markdown at content, clipped to
// viewport, and the images below it.
func (s *State) paintNotePreviewContent(c *gfx.Canvas, b gfx.Rect, n *notesUI) {
	s.notePreviewLayout(c, n, b.Width)
	s.paintNotePreviewAt(c, b, b, n)
}

// paintNotePreviewAt paints the preview's content rect, clipped to its viewport.
func (s *State) paintNotePreviewAt(c *gfx.Canvas, content, viewport gfx.Rect, n *notesUI) {
	if source := n.body.String(); source != n.view.previewSource {
		n.view.previewSource = source
		n.view.clearPreviewSelection()
	}
	height := n.previewLayout.height
	c.HitWithOptions(hitNotePreviewText, gfx.Rect{X: viewport.X, Y: viewport.Y, Width: viewport.Width, Height: min(viewport.Height, max(0, content.Y+height+12-viewport.Y))}, s.noteText("Preview text", "预览文本"), gfx.HitOptions{Role: gfx.RoleGroup, SkipTab: true, ReadOnly: true, Cursor: gfx.PointerCursorText})
	look := components.MarkdownLookFor(s.Theme())
	look.Code = s.codeColors()
	n.rendered.Paint(c, components.MarkdownPaint{Selection: n.view.previewSelect.In(0), Look: look, X: content.X + 6, Y: content.Y + 6, Clip: viewport, Links: hitNoteLinks})
	s.paintNoteCodeCopies(c, n, gfx.Point{X: content.X + 6, Y: content.Y + 6}, viewport)
	s.paintNoteImagesContent(c, gfx.Rect{X: content.X, Y: content.Y, Width: viewport.Width - 12, Height: viewport.Height}, content.Y+height+12, n)
}

func (s *State) noteModeLabel(mode noteMode) string {
	switch mode {
	case notePreview:
		return s.noteText("Preview", "预览")
	case noteSplit:
		return s.noteText("Split", "分屏")
	default:
		return s.noteText("Source", "源码")
	}
}

func (s *State) noteUpdatedLabel() string {
	status := s.noteContent().draft.Updated
	if date, err := time.Parse(time.RFC3339Nano, status); err == nil {
		status = date.Local().Format("2006-01-02 15:04")
	}
	return s.noteText("Updated ", "更新于 ") + status
}
