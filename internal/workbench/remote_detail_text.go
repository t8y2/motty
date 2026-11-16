package workbench

import (
	"fmt"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/keymap"
	"github.com/vivaui/viva/markdown"
	"motty/internal/remotetools"
)

// remoteDetailText is one scrollable, selectable code document shared by the
// process and container detail dialogs. MarkdownView supplies the text layout
// and hit testing; the detail dialog owns the surrounding tabs and actions.
type remoteDetailText struct {
	mode        remoteDetailMode
	doc         markdown.Document
	view        components.MarkdownView
	table       components.Table
	source      string
	layoutWidth float32
	layoutSize  float32
	lineHeight  float32
	selection   components.TextSelection

	scroll         float32
	scrollBar      components.ScrollAreaGeometry
	scrollDragging bool
	scrollGrab     float32
	bounds         gfx.Rect
	viewport       gfx.Rect
}

type remoteDetailMode uint8

const (
	remoteDetailMarkdown remoteDetailMode = iota
	remoteDetailTable
)

func (text *remoteDetailText) clearSelection() {
	text.selection.Clear()
}

// pointAt is where a window point lies in the text: it has one surface.
func (text *remoteDetailText) pointAt(point gfx.Point) (components.TextPoint, bool) {
	offset, ok := text.offsetAt(point)
	return components.TextPoint{Offset: offset}, ok
}

func (text *remoteDetailText) selectedText() (string, bool) {
	if text.mode == remoteDetailTable {
		if text.table.TextLen() == 0 {
			return "", false
		}
		value := text.table.SelectedText(components.TableSelection{
			Enabled: true, Anchor: text.selection.Anchor.Offset, Focus: text.selection.Focus.Offset,
		})
		return value, value != ""
	}
	if text.view.TextLen() == 0 {
		return "", false
	}
	value := text.view.SelectedText(text.selection.In(0))
	return value, value != ""
}

func (text *remoteDetailText) textLen() int {
	if text.mode == remoteDetailTable {
		return text.table.TextLen()
	}
	return text.view.TextLen()
}

func (text *remoteDetailText) offsetAt(point gfx.Point) (int, bool) {
	if text.mode == remoteDetailTable {
		return text.table.OffsetAt(point)
	}
	return text.view.OffsetAt(point)
}

func (text *remoteDetailText) reset() {
	*text = remoteDetailText{}
}

// remoteDetailSource makes every piece of detail text one fenced source block.
// The selected text therefore keeps tabs, spaces, newlines and markup-looking
// command or environment values exactly as Docker and /proc returned them.
func remoteDetailSource(body string) string {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	run, longest := 0, 0
	for _, r := range body {
		if r == '`' {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}
	fence := strings.Repeat("`", max(3, longest+1))
	return fence + "text\n" + body + "\n" + fence
}

func remoteDetailRows(rows [][2]string) string {
	var source strings.Builder
	for index, row := range rows {
		if index > 0 {
			source.WriteByte('\n')
		}
		source.WriteString(row[0])
		source.WriteByte('\t')
		source.WriteString(row[1])
	}
	return source.String()
}

func (state *State) paintRemoteDetailText(
	canvas *gfx.Canvas,
	bounds gfx.Rect,
	text *remoteDetailText,
	body, textIDLabel, scrollbarLabel string,
	textID, scrollbarID gfx.ID,
) {
	state.paintRemoteDetailTextSource(
		canvas, bounds, text, remoteDetailSource(body),
		textIDLabel, scrollbarLabel, textID, scrollbarID,
		true, false,
	)
}

func (state *State) paintRemoteDetailCode(
	canvas *gfx.Canvas,
	bounds gfx.Rect,
	text *remoteDetailText,
	body, textIDLabel, scrollbarLabel string,
	textID, scrollbarID gfx.ID,
) {
	state.paintRemoteDetailTextSource(
		canvas, bounds, text, remoteDetailSource(body),
		textIDLabel, scrollbarLabel, textID, scrollbarID,
		false, true,
	)
}

func (state *State) paintRemoteDetailTable(
	canvas *gfx.Canvas,
	bounds gfx.Rect,
	text *remoteDetailText,
	columns []components.TableColumn,
	header []string,
	rows [][]string,
	textIDLabel, scrollbarLabel string,
	textID, scrollbarID gfx.ID,
) {
	if bounds.Width <= 0 || bounds.Height <= 0 {
		return
	}
	theme := state.Theme()
	inner := bounds
	width := max(inner.Width-8, 80)
	contentHeight := components.TableHeaderHeight(theme) +
		float32(len(rows))*components.TableRowHeight(theme)
	if text.mode != remoteDetailTable {
		text.mode = remoteDetailTable
		text.source = ""
		text.clearSelection()
		text.scroll = 0
	}
	text.bounds = inner
	content := gfx.Size{Width: width, Height: max(contentHeight, inner.Height)}
	area := components.BeginScrollArea(canvas, theme, inner, content).
		Offset(0, text.scroll).
		Viewport(textID, textIDLabel).
		VerticalScrollbar(scrollbarID, scrollbarLabel, fmt.Sprintf("%d%%", int(100*min(max(text.scroll/max(areaMaxOffset(content.Height, inner.Height), 1), 0), 1))))
	geometry := area.Geometry()
	text.scroll, text.scrollBar, text.viewport = geometry.OffsetY, geometry, geometry.Viewport
	geometry = area.BeginContent()
	tableBounds := gfx.Rect{
		X: geometry.Viewport.X, Y: geometry.Viewport.Y - geometry.OffsetY,
		Width: geometry.Viewport.Width, Height: contentHeight,
	}
	table := components.BeginTable(canvas, theme, tableBounds, columns)
	table.Selection = components.TableSelection{
		Enabled: true,
		Anchor:  text.selection.Anchor.Offset,
		Focus:   text.selection.Focus.Offset,
	}
	table.Header(header)
	for _, row := range rows {
		table.Row(gfx.ID{}, row, false)
	}
	text.table = table
	area.EndContent()
}

func (state *State) paintRemoteDetailTextSource(
	canvas *gfx.Canvas,
	bounds gfx.Rect,
	text *remoteDetailText,
	source, textIDLabel, scrollbarLabel string,
	textID, scrollbarID gfx.ID,
	framed, hideCodeFrame bool,
) {
	if bounds.Width <= 0 || bounds.Height <= 0 {
		return
	}
	theme, pal := state.Theme(), state.pal
	inner := bounds
	if framed {
		canvas.RoundedBorder(bounds, radius.control, 1, pal.line, pal.workspace)
		inner = gfx.Rect{
			X: bounds.X + 1, Y: bounds.Y + 1,
			Width: max(bounds.Width-2, 0), Height: max(bounds.Height-2, 0),
		}
	}
	width := max(inner.Width-28, 80)
	size, lineHeight := theme.Metrics.FontSizeM, theme.Metrics.FontSizeM+6
	if text.mode != remoteDetailMarkdown || source != text.source {
		text.mode = remoteDetailMarkdown
		text.source = source
		text.doc = markdown.Document{}
		text.view = components.MarkdownView{}
		text.layoutWidth = 0
		text.clearSelection()
		text.scroll = 0
	}
	if source != "" && (text.layoutWidth != width || text.layoutSize != size || text.lineHeight != lineHeight) {
		text.doc.Parse(source)
		text.view.Layout(canvas, components.MarkdownStyle{Size: size, LineHeight: lineHeight}, &text.doc, width)
		text.layoutWidth, text.layoutSize, text.lineHeight = width, size, lineHeight
	}
	text.bounds = inner
	content := gfx.Size{Width: width, Height: max(text.view.Height(), inner.Height)}
	area := components.BeginScrollArea(canvas, theme, inner, content).
		Offset(0, text.scroll).
		Viewport(textID, textIDLabel).
		VerticalScrollbar(scrollbarID, scrollbarLabel, fmt.Sprintf("%d%%", int(100*min(max(text.scroll/max(areaMaxOffset(content.Height, inner.Height), 1), 0), 1))))
	geometry := area.Geometry()
	text.scroll, text.scrollBar, text.viewport = geometry.OffsetY, geometry, geometry.Viewport
	geometry = area.BeginContent()
	look := components.MarkdownLookFor(theme)
	look.CodeFill, look.Line = pal.workspace, pal.line
	if hideCodeFrame {
		look.CodeFill, look.Line = gfx.Color{}, gfx.Color{}
	}
	look.Selection = theme.Selection
	text.view.Paint(canvas, components.MarkdownPaint{
		Look:      look,
		X:         geometry.Viewport.X + 12,
		Y:         geometry.Viewport.Y + 10 - geometry.OffsetY,
		Clip:      geometry.Viewport,
		Selection: text.selection.In(0),
	})
	area.EndContent()
}

func (state *State) paintRemoteEnvironmentView(
	canvas *gfx.Canvas,
	bounds gfx.Rect,
	text *remoteDetailText,
	loading, failed bool,
	status, emptyMessage string,
	variables []remotetools.ProcessEnvironmentVariable,
	header [2]string,
	textIDLabel, scrollbarLabel string,
	textID, scrollbarID gfx.ID,
) {
	notice, color := "", state.pal.muted
	switch {
	case loading:
		notice = state.messages.Tools.RemoteProcessEnvironmentLoading
	case failed:
		notice, color = status, state.pal.danger
	case len(variables) == 0:
		notice = emptyMessage
	}
	if notice != "" {
		text.reset()
		canvas.LabelCenter(notice, bounds, gfx.TextStyle{
			Size: state.Theme().Metrics.FontSizeM, Color: color,
		})
		return
	}
	rows := make([][]string, len(variables))
	for index, variable := range variables {
		rows[index] = []string{variable.Name, variable.Value}
	}
	firstColumnWidth := min(float32(300), max(bounds.Width*0.36, 140))
	state.paintRemoteDetailTable(
		canvas, bounds, text,
		[]components.TableColumn{{Width: firstColumnWidth}, {Width: 0}},
		header[:], rows,
		textIDLabel, scrollbarLabel, textID, scrollbarID,
	)
}

func areaMaxOffset(content, viewport float32) float32 {
	return max(content-viewport, 0)
}

func (state *State) remoteDetailTextPointer(event gfx.PointerEvent, text *remoteDetailText, textID gfx.ID) (gfx.EventResult, bool) {
	if textID.IsZero() || !text.selection.Dragging() && !(event.Kind == gfx.PointerDown && event.Target == textID) {
		return gfx.EventResult{}, false
	}
	if event.Kind == gfx.PointerDown && event.Button != gfx.PointerButtonPrimary {
		return gfx.EventResult{}, false
	}
	if event.Kind == gfx.PointerMove && !event.Captured {
		return gfx.EventResult{}, true
	}
	result := text.selection.Pointer(event, text.pointAt)
	if !result.Handled {
		return gfx.EventResult{}, false
	}
	outcome := gfx.EventResult{Handled: true, Repaint: result.Changed}
	if event.Kind == gfx.PointerDown {
		outcome.Focus = textID
	}
	return outcome, true
}

func (state *State) remoteDetailScrollPointer(event gfx.PointerEvent, text *remoteDetailText, scrollbarID gfx.ID) (gfx.EventResult, bool) {
	if scrollbarID.IsZero() ||
		(!text.scrollDragging &&
			!(event.Kind == gfx.PointerDown && event.Button == gfx.PointerButtonPrimary && event.Target == scrollbarID)) {
		return gfx.EventResult{}, false
	}
	if event.Kind == gfx.PointerDown && event.Button != gfx.PointerButtonPrimary {
		return gfx.EventResult{}, false
	}
	geometry := text.scrollBar
	before := text.scroll
	switch event.Kind {
	case gfx.PointerDown:
		text.scrollDragging = true
		text.scrollGrab = geometry.VerticalThumb.Height / 2
		if event.Y >= geometry.VerticalThumb.Y &&
			event.Y < geometry.VerticalThumb.Y+geometry.VerticalThumb.Height {
			text.scrollGrab = event.Y - geometry.VerticalThumb.Y
		} else {
			text.scroll = geometry.VerticalOffsetForThumb(event.Y - text.scrollGrab)
		}
	case gfx.PointerMove:
		if !event.Captured {
			return gfx.EventResult{}, true
		}
		text.scroll = geometry.VerticalOffsetForThumb(event.Y - text.scrollGrab)
	case gfx.PointerUp:
		text.scrollDragging = false
	case gfx.PointerExit:
		if !event.Captured {
			text.scrollDragging = false
		}
	default:
		return gfx.EventResult{}, false
	}
	return gfx.EventResult{Handled: true, Repaint: before != text.scroll}, true
}

func (state *State) remoteDetailWheel(event gfx.ScrollEvent, text *remoteDetailText, textID, scrollbarID gfx.ID) (gfx.EventResult, bool) {
	if textID.IsZero() || text.scrollBar.MaxOffsetY <= 0 {
		return gfx.EventResult{}, false
	}
	inside := pointInRect(event.X, event.Y, text.bounds) ||
		pointInRect(event.X, event.Y, text.scrollBar.Bounds) ||
		event.Target == textID || event.Target == scrollbarID
	if !inside {
		return gfx.EventResult{}, false
	}
	before := text.scroll
	text.scroll = min(max(text.scroll+event.StepY(32), 0), text.scrollBar.MaxOffsetY)
	return gfx.EventResult{Handled: true, Repaint: before != text.scroll}, true
}

func (state *State) remoteDetailKeyboardScroll(
	event gfx.KeyEvent,
	text *remoteDetailText,
	textID, scrollbarID gfx.ID,
) (gfx.EventResult, bool) {
	if event.Target != textID && event.Target != scrollbarID {
		return gfx.EventResult{}, false
	}
	if text.scrollBar.MaxOffsetY <= 0 {
		return gfx.EventResult{}, false
	}
	step := min(max(text.viewport.Height/2, 32), 96)
	page := max(text.viewport.Height-step, step)
	before := text.scroll
	switch event.Key {
	case gfx.KeyUp:
		text.scroll -= step
	case gfx.KeyDown:
		text.scroll += step
	case gfx.KeyPageUp:
		text.scroll -= page
	case gfx.KeyPageDown:
		text.scroll += page
	case gfx.KeyHome:
		text.scroll = 0
	case gfx.KeyEnd:
		text.scroll = text.scrollBar.MaxOffsetY
	default:
		return gfx.EventResult{}, false
	}
	text.scroll = min(max(text.scroll, 0), text.scrollBar.MaxOffsetY)
	return gfx.EventResult{Handled: true, Repaint: before != text.scroll}, true
}

func (state *State) remoteDetailTextShortcut(event gfx.KeyEvent, text *remoteDetailText) (gfx.EventResult, bool) {
	chord, ok := keymap.FromEvent(event)
	if !ok {
		return gfx.EventResult{}, false
	}
	if chord.Modifiers == keymap.Primary && chord.Char == 'a' {
		length := text.textLen()
		if length == 0 {
			return gfx.EventResult{}, false
		}
		text.selection.Select(components.TextPoint{}, components.TextPoint{Offset: length})
		return gfx.EventResult{Handled: true, Repaint: true}, true
	}
	if state.shortcuts.actionFor(chord) != shortcutCopy {
		return gfx.EventResult{}, false
	}
	if !event.Repeat {
		if value, selected := text.selectedText(); selected {
			state.copyRemoteProcessValue(value)
		}
	}
	return gfx.EventResult{Handled: true, Repaint: true}, true
}

func (state *State) remoteDetailShortcut(event gfx.KeyEvent) (gfx.EventResult, bool) {
	if !state.remoteToolsVisible() {
		return gfx.EventResult{}, false
	}
	if state.remote.processDetails.open {
		return state.remoteDetailTextShortcut(event, &state.remote.processDetails.text)
	}
	if state.remote.containerDetails.open {
		return state.remoteDetailTextShortcut(event, &state.remote.containerDetails.text)
	}
	return gfx.EventResult{}, false
}
