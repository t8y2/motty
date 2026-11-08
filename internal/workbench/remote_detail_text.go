package workbench

import (
	"fmt"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/keymap"
	"github.com/vivaui/viva/markdown"
	"github.com/vivaui/viva/ui"
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
	bounds         ui.Rect
	viewport       ui.Rect
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
func (text *remoteDetailText) pointAt(point ui.Point) (components.TextPoint, bool) {
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

func (text *remoteDetailText) offsetAt(point ui.Point) (int, bool) {
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
	canvas *ui.Canvas,
	bounds ui.Rect,
	text *remoteDetailText,
	body, textIDLabel, scrollbarLabel string,
	textID, scrollbarID ui.ID,
) {
	state.paintRemoteDetailTextSource(
		canvas, bounds, text, remoteDetailSource(body),
		textIDLabel, scrollbarLabel, textID, scrollbarID,
		true, false,
	)
}

func (state *State) paintRemoteDetailCode(
	canvas *ui.Canvas,
	bounds ui.Rect,
	text *remoteDetailText,
	body, textIDLabel, scrollbarLabel string,
	textID, scrollbarID ui.ID,
) {
	state.paintRemoteDetailTextSource(
		canvas, bounds, text, remoteDetailSource(body),
		textIDLabel, scrollbarLabel, textID, scrollbarID,
		false, true,
	)
}

func (state *State) paintRemoteDetailTable(
	canvas *ui.Canvas,
	bounds ui.Rect,
	text *remoteDetailText,
	columns []components.TableColumn,
	header []string,
	rows [][]string,
	textIDLabel, scrollbarLabel string,
	textID, scrollbarID ui.ID,
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
	content := ui.Size{Width: width, Height: max(contentHeight, inner.Height)}
	area := components.BeginScrollArea(canvas, theme, inner, content).
		Offset(0, text.scroll).
		Viewport(textID, textIDLabel).
		VerticalScrollbar(scrollbarID, scrollbarLabel, fmt.Sprintf("%d%%", int(100*min(max(text.scroll/max(areaMaxOffset(content.Height, inner.Height), 1), 0), 1))))
	geometry := area.Geometry()
	text.scroll, text.scrollBar, text.viewport = geometry.OffsetY, geometry, geometry.Viewport
	geometry = area.BeginContent()
	tableBounds := ui.Rect{
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
		table.Row(ui.ID{}, row, false)
	}
	text.table = table
	area.EndContent()
}

func (state *State) paintRemoteDetailTextSource(
	canvas *ui.Canvas,
	bounds ui.Rect,
	text *remoteDetailText,
	source, textIDLabel, scrollbarLabel string,
	textID, scrollbarID ui.ID,
	framed, hideCodeFrame bool,
) {
	if bounds.Width <= 0 || bounds.Height <= 0 {
		return
	}
	theme, pal := state.Theme(), state.pal
	inner := bounds
	if framed {
		canvas.RoundedBorder(bounds, radius.control, 1, pal.line, pal.workspace)
		inner = ui.Rect{
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
	content := ui.Size{Width: width, Height: max(text.view.Height(), inner.Height)}
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
		look.CodeFill, look.Line = ui.Color{}, ui.Color{}
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
	canvas *ui.Canvas,
	bounds ui.Rect,
	text *remoteDetailText,
	loading, failed bool,
	status, emptyMessage string,
	variables []remotetools.ProcessEnvironmentVariable,
	header [2]string,
	textIDLabel, scrollbarLabel string,
	textID, scrollbarID ui.ID,
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
		canvas.LabelCenter(notice, bounds, ui.TextStyle{
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

func (state *State) remoteDetailTextPointer(event ui.PointerEvent, text *remoteDetailText, textID ui.ID) (ui.EventResult, bool) {
	if textID.IsZero() || !text.selection.Dragging() && !(event.Kind == ui.PointerDown && event.Target == textID) {
		return ui.EventResult{}, false
	}
	if event.Kind == ui.PointerDown && event.Button != ui.PointerButtonPrimary {
		return ui.EventResult{}, false
	}
	if event.Kind == ui.PointerMove && !event.Captured {
		return ui.EventResult{}, true
	}
	result := text.selection.Pointer(event, text.pointAt)
	if !result.Handled {
		return ui.EventResult{}, false
	}
	outcome := ui.EventResult{Handled: true, Repaint: result.Changed}
	if event.Kind == ui.PointerDown {
		outcome.Focus = textID
	}
	return outcome, true
}

func (state *State) remoteDetailScrollPointer(event ui.PointerEvent, text *remoteDetailText, scrollbarID ui.ID) (ui.EventResult, bool) {
	if scrollbarID.IsZero() ||
		(!text.scrollDragging &&
			!(event.Kind == ui.PointerDown && event.Button == ui.PointerButtonPrimary && event.Target == scrollbarID)) {
		return ui.EventResult{}, false
	}
	if event.Kind == ui.PointerDown && event.Button != ui.PointerButtonPrimary {
		return ui.EventResult{}, false
	}
	geometry := text.scrollBar
	before := text.scroll
	switch event.Kind {
	case ui.PointerDown:
		text.scrollDragging = true
		text.scrollGrab = geometry.VerticalThumb.Height / 2
		if event.Y >= geometry.VerticalThumb.Y &&
			event.Y < geometry.VerticalThumb.Y+geometry.VerticalThumb.Height {
			text.scrollGrab = event.Y - geometry.VerticalThumb.Y
		} else {
			text.scroll = geometry.VerticalOffsetForThumb(event.Y - text.scrollGrab)
		}
	case ui.PointerMove:
		if !event.Captured {
			return ui.EventResult{}, true
		}
		text.scroll = geometry.VerticalOffsetForThumb(event.Y - text.scrollGrab)
	case ui.PointerUp:
		text.scrollDragging = false
	case ui.PointerExit:
		if !event.Captured {
			text.scrollDragging = false
		}
	default:
		return ui.EventResult{}, false
	}
	return ui.EventResult{Handled: true, Repaint: before != text.scroll}, true
}

func (state *State) remoteDetailWheel(event ui.ScrollEvent, text *remoteDetailText, textID, scrollbarID ui.ID) (ui.EventResult, bool) {
	if textID.IsZero() || text.scrollBar.MaxOffsetY <= 0 {
		return ui.EventResult{}, false
	}
	inside := pointInRect(event.X, event.Y, text.bounds) ||
		pointInRect(event.X, event.Y, text.scrollBar.Bounds) ||
		event.Target == textID || event.Target == scrollbarID
	if !inside {
		return ui.EventResult{}, false
	}
	before := text.scroll
	text.scroll = min(max(text.scroll+event.StepY(32), 0), text.scrollBar.MaxOffsetY)
	return ui.EventResult{Handled: true, Repaint: before != text.scroll}, true
}

func (state *State) remoteDetailKeyboardScroll(
	event ui.KeyEvent,
	text *remoteDetailText,
	textID, scrollbarID ui.ID,
) (ui.EventResult, bool) {
	if event.Target != textID && event.Target != scrollbarID {
		return ui.EventResult{}, false
	}
	if text.scrollBar.MaxOffsetY <= 0 {
		return ui.EventResult{}, false
	}
	step := min(max(text.viewport.Height/2, 32), 96)
	page := max(text.viewport.Height-step, step)
	before := text.scroll
	switch event.Key {
	case ui.KeyUp:
		text.scroll -= step
	case ui.KeyDown:
		text.scroll += step
	case ui.KeyPageUp:
		text.scroll -= page
	case ui.KeyPageDown:
		text.scroll += page
	case ui.KeyHome:
		text.scroll = 0
	case ui.KeyEnd:
		text.scroll = text.scrollBar.MaxOffsetY
	default:
		return ui.EventResult{}, false
	}
	text.scroll = min(max(text.scroll, 0), text.scrollBar.MaxOffsetY)
	return ui.EventResult{Handled: true, Repaint: before != text.scroll}, true
}

func (state *State) remoteDetailTextShortcut(event ui.KeyEvent, text *remoteDetailText) (ui.EventResult, bool) {
	chord, ok := keymap.FromEvent(event)
	if !ok {
		return ui.EventResult{}, false
	}
	if chord.Modifiers == keymap.Primary && chord.Char == 'a' {
		length := text.textLen()
		if length == 0 {
			return ui.EventResult{}, false
		}
		text.selection.Select(components.TextPoint{}, components.TextPoint{Offset: length})
		return ui.EventResult{Handled: true, Repaint: true}, true
	}
	if state.shortcuts.actionFor(chord) != shortcutCopy {
		return ui.EventResult{}, false
	}
	if !event.Repeat {
		if value, selected := text.selectedText(); selected {
			state.copyRemoteProcessValue(value)
		}
	}
	return ui.EventResult{Handled: true, Repaint: true}, true
}

func (state *State) remoteDetailShortcut(event ui.KeyEvent) (ui.EventResult, bool) {
	if !state.remoteToolsVisible() {
		return ui.EventResult{}, false
	}
	if state.remote.processDetails.open {
		return state.remoteDetailTextShortcut(event, &state.remote.processDetails.text)
	}
	if state.remote.containerDetails.open {
		return state.remoteDetailTextShortcut(event, &state.remote.containerDetails.text)
	}
	return ui.EventResult{}, false
}
