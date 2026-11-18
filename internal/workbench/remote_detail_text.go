package workbench

import (
	"fmt"
	"sort"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/keymap"
	"github.com/vivaui/viva/markdown"
	"github.com/vivaui/viva/ui"
	"motty/internal/remotetools"
)

// remoteDetailText is one scrollable, selectable code or table document shared
// by the process and container detail dialogs. MarkdownView supplies the text
// layout and hit testing; the table mode keeps its own serialized TSV so copied
// values stay lossless. The surrounding dialog owns the tabs and actions.
type remoteDetailText struct {
	mode        remoteDetailMode
	doc         markdown.Document
	view        components.MarkdownView
	source      string
	layoutWidth float32
	layoutSize  float32
	lineHeight  float32
	selection   components.TextSelection

	// The custom canvas scroll area the dialog paints: the old bars stay
	// always visible with their localized labels and percentage value, so the
	// application keeps the offset and the bar drag itself.
	scroll         float32
	scrollBar      components.ScrollAreaGeometry
	scrollDragging bool
	scrollGrab     float32
	draw           func(*ui.DrawContext)
	notice         string
	noticeTone     gfx.Color
	noticeDraw     func(*ui.DrawContext)
	textID         gfx.ID
	textIDLabel    string
	scrollbarID    gfx.ID
	scrollbarLabel string
	// bounds is the content's painted rect, scrollBounds the scroll area's,
	// and viewport the visible clip.
	bounds       gfx.Rect
	scrollBounds gfx.Rect
	viewport     gfx.Rect

	// The table model: the serialized TSV, one entry per painted cell, and the
	// rows' bounds for point hit testing.
	columns   []components.TableColumn
	header    []string
	rows      [][]string
	tableRows []remoteDetailTableRow
	cells     []remoteDetailTableCell
	plain     []byte
	canvas    *gfx.Canvas
	size      float32
}

type remoteDetailMode uint8

const (
	remoteDetailMarkdown remoteDetailMode = iota
	remoteDetailTable
)

type remoteDetailTableCell struct {
	bounds               gfx.Rect
	textX, textWidth     float32
	value, label         string
	weight               float32
	textLo, textHi       int
	contentLo, contentHi int
}

type remoteDetailTableRow struct {
	bounds gfx.Rect
	lo, hi int
}

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
		if len(text.plain) == 0 {
			return "", false
		}
		start, end := text.selection.Anchor.Offset, text.selection.Focus.Offset
		if start > end {
			start, end = end, start
		}
		start = min(max(start, 0), len(text.plain))
		end = min(max(end, 0), len(text.plain))
		for _, cell := range text.cells {
			quoted := cell.textLo != cell.contentLo || cell.textHi != cell.contentHi
			if !quoted {
				continue
			}
			if start > cell.textLo && start < cell.textHi {
				start = cell.textLo
			}
			if end > cell.textLo && end < cell.textHi {
				end = cell.textHi
			}
		}
		return string(text.plain[start:end]), end > start
	}
	if text.view.TextLen() == 0 {
		return "", false
	}
	value := text.view.SelectedText(text.selection.In(0))
	return value, value != ""
}

func (text *remoteDetailText) textLen() int {
	if text.mode == remoteDetailTable {
		return len(text.plain)
	}
	return text.view.TextLen()
}

func (text *remoteDetailText) offsetAt(point gfx.Point) (int, bool) {
	if text.mode == remoteDetailTable {
		return text.tableOffsetAt(point)
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

// remoteDetailSpec says what a detail body shows: markdown source, a table, or
// a notice in place of either, and the identities the old scroll area exposed.
type remoteDetailSpec struct {
	textID         gfx.ID
	textIDLabel    string
	scrollbarID    gfx.ID
	scrollbarLabel string
	source         string
	columns        []components.TableColumn
	header         []string
	rows           [][]string
	notice         string
	noticeTone     gfx.Color
}

// declareRemoteDetail declares the detail body as a page slot the application
// paints with its own scroll area: the old bars stay always visible with their
// localized labels and percentage value, and keep their drag and keys.
func (state *State) declareRemoteDetail(c ui.C, text *remoteDetailText, spec remoteDetailSpec) {
	if text.draw == nil {
		text.draw = func(d *ui.DrawContext) { state.paintRemoteDetailScroll(d, text) }
	}
	if spec.notice != "" {
		text.reset()
		text.textID, text.textIDLabel = spec.textID, spec.textIDLabel
		text.scrollbarID, text.scrollbarLabel = spec.scrollbarID, spec.scrollbarLabel
		text.notice, text.noticeTone = spec.notice, spec.noticeTone
		text.noticeDraw = func(d *ui.DrawContext) { state.paintRemoteDetailNotice(d, text) }
		ui.Slot(c).Grow(1).FillWidth().Draw(text.noticeDraw)
		return
	}
	text.notice = ""
	text.textID, text.textIDLabel = spec.textID, spec.textIDLabel
	text.scrollbarID, text.scrollbarLabel = spec.scrollbarID, spec.scrollbarLabel
	if len(spec.columns) > 0 {
		text.setTableModel(spec.columns, spec.header, spec.rows)
	} else {
		if spec.source != text.source {
			text.mode = remoteDetailMarkdown
			text.source = spec.source
			text.doc = markdown.Document{}
			text.view = components.MarkdownView{}
			text.layoutWidth = 0
			text.clearSelection()
			text.scroll = 0
		}
	}
	ui.Slot(c).Grow(1).FillWidth().MinHeight(0).Draw(text.draw)
}

// layoutMarkdown lays the source out at the width the viewport gives it.
func (text *remoteDetailText) layoutMarkdown(canvas *gfx.Canvas, theme gfx.Theme, source string, viewportWidth float32) {
	width := max(viewportWidth-28, 80)
	size, lineHeight := theme.Metrics.FontSizeM, theme.Metrics.FontSizeM+6
	text.mode = remoteDetailMarkdown
	if source != text.source {
		text.source = source
		text.doc = markdown.Document{}
		text.view = components.MarkdownView{}
		text.layoutWidth = 0
		text.clearSelection()
	}
	text.size, text.lineHeight = size, lineHeight
	if text.layoutWidth != width || text.layoutSize != size {
		text.doc.Parse(source)
		text.view.Layout(canvas, components.MarkdownStyle{Size: size, LineHeight: lineHeight}, &text.doc, width)
		text.layoutWidth, text.layoutSize = width, size
	}
}

// paintRemoteDetailScroll paints the detail content in the dialog's custom
// scroll area: the bars are always visible, named, and show the scroll position.
func (state *State) paintRemoteDetailScroll(d *ui.DrawContext, text *remoteDetailText) {
	bounds := d.Rect
	if bounds.Width <= 0 || bounds.Height <= 0 {
		return
	}
	theme := state.Theme()
	text.canvas, text.bounds, text.scrollBounds = d.Canvas, bounds, bounds
	contentHeight := components.TableHeaderHeight(theme) + float32(len(text.rows))*components.TableRowHeight(theme)
	var content gfx.Size
	if text.mode == remoteDetailTable {
		content = gfx.Size{Width: max(bounds.Width-8, 80), Height: max(contentHeight, bounds.Height)}
	} else {
		width := max(bounds.Width-28, 80)
		text.layoutMarkdown(d.Canvas, theme, text.source, bounds.Width)
		content = gfx.Size{Width: width, Height: max(text.view.Height(), bounds.Height)}
	}
	area := components.BeginScrollArea(d.Canvas, theme, bounds, content).
		Offset(0, text.scroll).
		Viewport(text.textID, text.textIDLabel).
		VerticalScrollbar(text.scrollbarID, text.scrollbarLabel, remoteScrollPercent(text.scroll, content.Height, bounds.Height))
	geometry := area.Geometry()
	text.scroll, text.scrollBar, text.viewport = geometry.OffsetY, geometry, geometry.Viewport
	geometry = area.BeginContent()
	if text.mode == remoteDetailTable {
		text.bounds = gfx.Rect{
			X: geometry.Viewport.X, Y: geometry.Viewport.Y - geometry.OffsetY,
			Width: geometry.Viewport.Width, Height: contentHeight,
		}
		state.paintRemoteDetailTableSlot(d.Canvas, text)
	} else {
		look := components.MarkdownLookFor(theme)
		look.CodeFill, look.Line = gfx.Color{}, gfx.Color{}
		look.Selection = theme.Selection
		text.view.Paint(d.Canvas, components.MarkdownPaint{
			Look:      look,
			X:         geometry.Viewport.X + 12,
			Y:         geometry.Viewport.Y + 10 - geometry.OffsetY,
			Clip:      geometry.Viewport,
			Selection: text.selection.In(0),
		})
	}
	area.EndContent()
}

// remoteScrollPercent is the value an always-visible bar shows.
func remoteScrollPercent(offset, content, viewport float32) string {
	return fmt.Sprintf("%d%%", int(100*min(max(offset/max(areaMaxOffset(content, viewport), 1), 0), 1)))
}

func areaMaxOffset(content, viewport float32) float32 {
	return max(content-viewport, 0)
}

// paintRemoteDetailNotice paints the loading, failure or empty notice that
// replaces the text.
func (state *State) paintRemoteDetailNotice(d *ui.DrawContext, text *remoteDetailText) {
	if text.notice == "" {
		return
	}
	d.Canvas.LabelCenter(text.notice, d.Rect, gfx.TextStyle{
		Size: state.Theme().Metrics.FontSizeM, Color: text.noticeTone,
	})
}

// setTableModel records the table a paint draws: its columns, header and rows.
func (text *remoteDetailText) setTableModel(columns []components.TableColumn, header []string, rows [][]string) {
	if cap(text.columns) < len(columns) {
		text.columns = make([]components.TableColumn, len(columns))
	}
	text.columns = text.columns[:len(columns)]
	copy(text.columns, columns)
	if cap(text.header) < len(header) {
		text.header = make([]string, len(header))
	}
	text.header = text.header[:len(header)]
	copy(text.header, header)
	text.rows = rows
	text.mode = remoteDetailTable
	text.source = ""
	text.clearSelection()
}

// paintRemoteDetailTableSlot paints the header and all rows at components.Table's
// geometry and records the cell text spans selection and copy use.
func (state *State) paintRemoteDetailTableSlot(canvas *gfx.Canvas, text *remoteDetailText) {
	theme := state.Theme()
	headerHeight, rowHeight := components.TableHeaderHeight(theme), components.TableRowHeight(theme)
	bounds := text.bounds
	bottom := bounds.Y + headerHeight + float32(len(text.rows))*rowHeight
	text.plain = text.plain[:0]
	text.cells = text.cells[:0]
	text.tableRows = text.tableRows[:0]
	y := bounds.Y
	rows := append([][]string{text.header}, text.rows...)
	for index, values := range rows {
		weight, height := gfx.WeightRegular, rowHeight
		if index == 0 {
			weight, height = gfx.WeightMedium, headerHeight
		}
		if index > 0 {
			text.plain = append(text.plain, '\n')
		}
		row := gfx.Rect{X: bounds.X, Y: y, Width: bounds.Width, Height: height}
		state.paintDetailCells(canvas, theme, text, row, values, weight)
		y += height
		if index < len(rows)-1 && y < bottom {
			canvas.Fill(gfx.Rect{X: bounds.X, Y: y - 1, Width: bounds.Width, Height: 1}, theme.Border)
		}
	}
}

// appendRemoteDetailCell appends one RFC 4180-style TSV field, keeping tabs,
// newlines and quotes lossless when copied.
func appendRemoteDetailCell(plain *[]byte, index int, value string) (field struct {
	lo, hi, contentLo, contentHi int
}) {
	if index > 0 {
		*plain = append(*plain, '\t')
	}
	field.lo = len(*plain)
	quoted := strings.ContainsAny(value, "\t\n\r\"")
	if quoted {
		*plain = append(*plain, '"')
	}
	field.contentLo = len(*plain)
	if quoted {
		for index := 0; index < len(value); index++ {
			if value[index] == '"' {
				*plain = append(*plain, '"')
			}
			*plain = append(*plain, value[index])
		}
	} else {
		*plain = append(*plain, value...)
	}
	field.contentHi = len(*plain)
	if quoted {
		*plain = append(*plain, '"')
	}
	field.hi = len(*plain)
	return field
}

// paintDetailCells paints one table row's cells and appends their spans.
func (state *State) paintDetailCells(canvas *gfx.Canvas, theme gfx.Theme, text *remoteDetailText, row gfx.Rect, values []string, weight float32) {
	size, pad := theme.Metrics.FontSizeM, theme.Metrics.GapItem
	rowLo := len(text.cells)
	rects := components.TableCellBounds(text.columns, row, nil)
	for index, cell := range rects {
		value := ""
		if index < len(values) {
			value = values[index]
		}
		textBounds := cell.Inset(gfx.Insets{Left: pad, Right: pad})
		field := appendRemoteDetailCell(&text.plain, index, value)
		label := canvas.FitLine(value, size, weight, textBounds.Width)
		textWidth := canvas.TextWidthWeight(label, size, weight)
		style := gfx.TextStyle{Size: size, Weight: weight, Color: theme.Foreground}
		alignment := gfx.AlignStart
		if index < len(text.columns) {
			alignment = tableAlignment(text.columns[index].Alignment)
		}
		textX := canvas.LabelOrigin(label, textBounds, style, alignment)
		cellValue := remoteDetailTableCell{
			bounds: cell, textX: textX, textWidth: textWidth, value: value, label: label, weight: weight,
			textLo: field.lo, textHi: field.hi, contentLo: field.contentLo, contentHi: field.contentHi,
		}
		spans, foreground := paintDetailCellSelection(canvas, theme, text, cellValue, textBounds)
		if label != "" {
			canvas.Clip(textBounds)
			tableLabelAligned(canvas, label, textBounds, alignment, style)
			canvas.Unclip()
			if len(spans) > 0 {
				selected := style
				selected.Color = foreground
				for _, span := range spans {
					if span.Width <= 0 {
						continue
					}
					canvas.Clip(gfx.Rect{X: textX + span.X, Y: textBounds.Y, Width: span.Width, Height: textBounds.Height})
					tableLabelAligned(canvas, label, textBounds, alignment, selected)
					canvas.Unclip()
				}
			}
		}
		text.cells = append(text.cells, cellValue)
	}
	text.tableRows = append(text.tableRows, remoteDetailTableRow{bounds: row, lo: rowLo, hi: len(text.cells)})
}

// paintDetailCellSelection paints the selected part of one cell and returns its
// spans, the selection background and the selected text colour.
func paintDetailCellSelection(canvas *gfx.Canvas, theme gfx.Theme, text *remoteDetailText, cell remoteDetailTableCell, bounds gfx.Rect) ([]gfx.TextSpan, gfx.Color) {
	background, foreground := remoteDetailSelectionColors(theme)
	if cell.contentLo == cell.contentHi {
		return nil, foreground
	}
	start, end := text.selection.Anchor.Offset, text.selection.Focus.Offset
	if start > end {
		start, end = end, start
	}
	lo, hi := max(start, cell.contentLo), min(end, cell.contentHi)
	if lo >= hi {
		return nil, foreground
	}
	visualLo := remoteSerializedColumnOffset(cell.value, lo-cell.contentLo)
	visualHi := remoteSerializedColumnOffset(cell.value, hi-cell.contentLo)
	if visualLo >= visualHi {
		return nil, foreground
	}
	spans := canvas.TextSelection(nil, cell.value, text.size, cell.weight, visualLo, visualHi)
	canvas.Clip(gfx.Rect{X: cell.textX, Y: bounds.Y, Width: cell.textWidth, Height: bounds.Height})
	for _, span := range spans {
		canvas.Fill(gfx.Rect{X: cell.textX + span.X, Y: bounds.Y, Width: span.Width, Height: bounds.Height}, background)
	}
	canvas.Unclip()
	return spans, foreground
}

func remoteDetailSelectionColors(theme gfx.Theme) (background, foreground gfx.Color) {
	background, foreground = theme.Selection, theme.SelectionForeground
	if background.Alpha == 0 {
		background = theme.Primary
	}
	if foreground.Alpha == 0 {
		foreground = theme.PrimaryForeground
	}
	return background, foreground
}

func tableAlignment(alignment components.TableAlignment) gfx.Alignment {
	switch alignment {
	case components.TableAlignCenter:
		return gfx.AlignCenter
	case components.TableAlignRight:
		return gfx.AlignRight
	default:
		return gfx.AlignStart
	}
}

func tableLabelAligned(canvas *gfx.Canvas, value string, bounds gfx.Rect, alignment gfx.Alignment, style gfx.TextStyle) {
	switch alignment {
	case gfx.AlignCenter:
		canvas.LabelAligned(value, bounds, style, gfx.AlignCenter)
	case gfx.AlignRight:
		canvas.LabelAligned(value, bounds, style, gfx.AlignRight)
	default:
		canvas.Label(value, bounds, style)
	}
}

func remoteSerializedColumnOffset(value string, offset int) int {
	offset = min(max(offset, 0), len(value)+strings.Count(value, `"`))
	serialized := 0
	for index := 0; index < len(value); index++ {
		width := 1
		if value[index] == '"' {
			width = 2
		}
		if offset <= serialized {
			return index
		}
		if offset < serialized+width {
			return index
		}
		serialized += width
	}
	return len(value)
}

// tableOffsetAt maps a window point to the nearest TSV byte offset.
func (text *remoteDetailText) tableOffsetAt(point gfx.Point) (int, bool) {
	if text.canvas == nil || len(text.tableRows) == 0 || len(text.cells) == 0 {
		return 0, false
	}
	row := &text.tableRows[nearestDetailRow(text.tableRows, point.Y)]
	cells := text.cells[row.lo:row.hi]
	if len(cells) == 0 {
		return row.lo, true
	}
	cell := &cells[nearestDetailCell(cells, point.X)]
	if cell.contentHi <= cell.contentLo || cell.bounds.Width <= 0 {
		return cell.textLo, true
	}
	localX := point.X - cell.textX
	if localX <= 0 {
		return cell.contentLo, true
	}
	if cell.textWidth <= 0 || localX >= cell.textWidth {
		return cell.contentHi, true
	}
	local := text.canvas.TextOffsetAt(cell.value, text.size, cell.weight, localX)
	local = min(max(local, 0), len(cell.value))
	local += strings.Count(cell.value[:local], `"`)
	return min(cell.contentLo+local, cell.contentHi), true
}

func nearestDetailRow(rows []remoteDetailTableRow, y float32) int {
	if y <= rows[0].bounds.Y {
		return 0
	}
	index := sort.Search(len(rows), func(index int) bool { return y < rows[index].bounds.Bottom() })
	if index < len(rows) {
		return index
	}
	return len(rows) - 1
}

func nearestDetailCell(cells []remoteDetailTableCell, x float32) int {
	if x <= cells[0].bounds.X {
		return 0
	}
	index := sort.Search(len(cells), func(index int) bool { return x < cells[index].bounds.Right() })
	if index < len(cells) {
		return index
	}
	return len(cells) - 1
}

// remoteDetailTextPointer starts or extends a text selection under the pointer.
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

// remoteDetailScrollPointer drags the always-visible scrollbar with stable
// pointer capture and thumb geometry.
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
	inside := pointInRect(event.X, event.Y, text.scrollBounds) ||
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

// remoteDetailNotice picks the notice a detail body shows instead of its text,
// and the tone it paints in.
func (state *State) remoteDetailNotice(loading, failed bool, status, empty string) (string, gfx.Color) {
	switch {
	case loading:
		return state.messages.Tools.RemoteProcessEnvironmentLoading, state.pal.muted
	case failed:
		return status, state.pal.danger
	case empty != "":
		return empty, state.pal.muted
	}
	return "", gfx.Color{}
}

// remoteEnvironmentTable is the environment list as the detail table's columns,
// header and rows.
func remoteEnvironmentTable(
	variables []remotetools.ProcessEnvironmentVariable,
	header [2]string,
	width float32,
) ([]components.TableColumn, []string, [][]string) {
	firstColumnWidth := min(float32(300), max(width*0.36, 140))
	rows := make([][]string, len(variables))
	for index, variable := range variables {
		rows[index] = []string{variable.Name, variable.Value}
	}
	return []components.TableColumn{{Width: firstColumnWidth}, {Width: 0}}, header[:], rows
}
