//go:build dev

package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
)

// debugLanguage is what the gallery's highlighting grammar is chosen for. The
// text is Go, so the editor gets the same rules a .go file window would.
const debugLanguage = "go"

// devDeclareWorkspace declares the gallery's heading and the area its editor
// fills in the workbench page. The editor is painted into that area after the
// page, by devPaintPage.
func (state *State) devDeclareWorkspace(c ui.C) {
	if state.activeSection != sectionDebug {
		return
	}
	pal, messages := state.pal, state.messages
	ui.Column(c).Fill().PadSides(12, 18, 12, 18).Children(func() {
		ui.Column(c).MarginSides(4, 0, 13, 0).Gap(3).Children(func() {
			ui.Text(c, messages.DebugTitle).FontSize(18).Weight(gfx.WeightSemibold).Color(pal.text)
			ui.Paragraph(c, messages.DebugDescription, 2).FontSize(metrics.FontSizeS).Color(pal.muted).LineHeight(22)
		})
		ui.Divider(c).Background(pal.line)
		ui.Column(c).Grow(1).MarginSides(4, 0, 3, 0).Children(func() {
			state.dev.area = ui.Slot(c).Grow(1)
		})
	})
}

// paintDebugEditor paints the editor in bounds. The text, the caret and the
// selection come from the page's editor; the grid, the line numbers, the
// highlighting and the scrollbars are Viva's.
func (state *State) paintDebugEditor(canvas *gfx.Canvas, theme gfx.Theme, bounds gfx.Rect, editor *text.Editor) {
	selection := editor.Selection()
	caret := editor.ByteOffset(editor.Caret())
	focused := canvas.Focused() == hitDebugEditor
	snapshot := components.CodeEditorSnapshot{
		ID: hitDebugEditor, Label: state.messages.NavDebug, Value: editor.String(),
		Focused:        focused,
		ShowCaret:      focused && selection.Start == selection.End,
		SelectionStart: editor.ByteOffset(selection.Start),
		SelectionEnd:   editor.ByteOffset(selection.End),
		Caret:          caret,
		CaretColor:     state.pal.cursor,
		FontSize:       state.terminalFontSize,
		MonoFace:       state.terminalFontFace,
		TabWidth:       4,
		// Without Wrap each line is one row and the view scrolls sideways, which
		// is what exercises the horizontal scrollbar.
		LineNumbers:           true,
		CurrentLine:           true,
		MatchBrackets:         true,
		Language:              debugLanguage,
		Highlight:             state.dev.highlight,
		ScrollbarID:           hitDebugScroll,
		HorizontalScrollbarID: hitDebugScrollX,
	}
	if marked, ok := editor.Composition(); ok {
		snapshot.CompositionStart = editor.ByteOffset(marked.Start)
		snapshot.CompositionEnd = editor.ByteOffset(marked.End)
	}
	state.dev.code.Paint(canvas, theme, bounds, snapshot)
}

// debugHighlight colors one line of the gallery's editor with the same grammar
// and the same palette a file window uses.
func (state *State) debugHighlight(language, line string, dst []components.CodeEditorRun) []components.CodeEditorRun {
	state.dev.marks = highlightCode(language, line, state.dev.marks[:0])
	colors := state.debugCodeColors()
	for _, run := range state.dev.marks {
		if int(run.Class) < len(colors) {
			dst = append(dst, components.CodeEditorRun{Start: run.Start, End: run.End, Color: colors[run.Class]})
		}
	}
	return dst
}

// debugCodeColors is the palette the gallery's highlighting draws from, taken
// from the theme's ANSI colors as the file window takes them.
func (state *State) debugCodeColors() [components.MarkdownCodeClasses]gfx.Color {
	pal := state.pal
	ansi := func(index int) gfx.Color { return gfx.RGB(pal.ansi[index][0], pal.ansi[index][1], pal.ansi[index][2]) }
	return [components.MarkdownCodeClasses]gfx.Color{
		components.CodeComment: pal.faint, components.CodeString: ansi(2), components.CodeNumber: ansi(3),
		components.CodeKeyword: ansi(5), components.CodeFunction: ansi(4), components.CodeVariable: ansi(6),
		components.CodeFlag: ansi(6), components.CodeKey: ansi(4), components.CodeAdded: ansi(2), components.CodeRemoved: ansi(1),
	}
}
