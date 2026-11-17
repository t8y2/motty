package workbench

import (
	"math"
	"strings"
	"unicode/utf8"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
)

// aiPlainTextLine is one painted row of a selectable, non-Markdown text block.
// start and end are byte offsets into the block's source. A soft wrap has no
// bytes between rows; a hard break remains in the source and is therefore copied.
type aiPlainTextLine struct {
	start, end int
	text       string
}

// aiPlainTextView maps the lines painted by flex back to their source text. Its
// geometry comes from the last row layout, just as MarkdownView's does from Paint.
type aiPlainTextView struct {
	source string
	lines  []aiPlainTextLine

	canvas           *gfx.Canvas
	rect             gfx.Rect
	size, lineHeight float32
	cell             float32
	mono             bool
	selectionScratch []gfx.TextSpan
}

func (v *aiPlainTextView) reset(source string) {
	v.source = source
	v.lines = v.lines[:0]
	v.canvas = nil
	v.rect = gfx.Rect{}
}

func (v *aiPlainTextView) layoutBody(canvas *gfx.Canvas, source string, size, lineHeight, width float32, maxLines int) {
	v.reset(source)
	v.size, v.lineHeight, v.mono = size, lineHeight, false
	if source == "" || maxLines <= 0 {
		return
	}
	from := 0
	for from >= 0 && len(v.lines) < maxLines {
		row, next := canvas.WrapRow(source, from, size, gfx.WeightRegular, width)
		// WrapLines, which this block replaces for user messages, does not add
		// another empty row after a final newline.
		if next < 0 && row.Start == len(source) && len(v.lines) > 0 {
			break
		}
		text := strings.TrimRight(source[row.Start:row.End], " ")
		v.lines = append(v.lines, aiPlainTextLine{start: row.Start, end: row.End, text: text})
		from = next
	}
	if len(v.lines) == maxLines && from >= 0 && from < len(source) {
		// WrapLines marked the last row it kept; the block must not look whole.
		last := &v.lines[len(v.lines)-1]
		last.text = strings.TrimRight(last.text, " ") + "…"
	}
}

// layoutMono wraps source by terminal cells and reports whether maxLines left
// text out. Unlike layoutBody it preserves every displayed byte, including spaces.
func (v *aiPlainTextView) layoutMono(source string, cols, maxLines int, cell, lineHeight float32) (more bool) {
	v.reset(source)
	v.cell, v.lineHeight, v.mono = cell, lineHeight, true
	for from := 0; ; {
		end := len(source)
		next := -1
		if newline := strings.IndexByte(source[from:], '\n'); newline >= 0 {
			end = from + newline
			next = end + 1
		}
		displayEnd := end
		if displayEnd > from && source[displayEnd-1] == '\r' {
			displayEnd--
		}
		for start := from; ; {
			if len(v.lines) == maxLines {
				// Selection copies visible text, not the hidden remainder. Keep
				// only the source covered by the rows that made it into the block.
				if len(v.lines) == 0 {
					v.source = ""
				} else {
					v.source = v.source[:v.lines[len(v.lines)-1].end]
				}
				return true
			}
			count, done := monoBreak(source[start:displayEnd], cols)
			v.lines = append(v.lines, aiPlainTextLine{start: start, end: start + count, text: source[start : start+count]})
			start += count
			if done {
				break
			}
		}
		if next < 0 {
			return false
		}
		from = next
	}
}

func (v *aiPlainTextView) appendSynthetic(text string) {
	v.lines = append(v.lines, aiPlainTextLine{start: len(v.source), end: len(v.source), text: text})
}

func (v *aiPlainTextView) appendLines(dst []string) []string {
	for _, line := range v.lines {
		dst = append(dst, line.text)
	}
	return dst
}

func (v *aiPlainTextView) TextLen() int { return len(v.source) }

func (v *aiPlainTextView) SelectedText(selection components.MarkdownSelection) string {
	start, end := aiPlainTextRange(v.source, selection)
	return v.source[start:end]
}

// OffsetAt returns the source byte offset nearest a window point. Points beyond
// the block clamp to its nearest row so a captured drag can leave the text.
func (v *aiPlainTextView) OffsetAt(point gfx.Point) (int, bool) {
	if v.canvas == nil || len(v.lines) == 0 || v.lineHeight <= 0 {
		return 0, false
	}
	row := int(math.Floor(float64((point.Y - v.rect.Y) / v.lineHeight)))
	row = min(max(row, 0), len(v.lines)-1)
	line := &v.lines[row]
	if line.start == line.end {
		return line.start, true
	}
	x := point.X - v.rect.X
	if !v.mono {
		offset := v.canvas.TextOffsetAt(line.text, v.size, gfx.WeightRegular, x)
		if offset == len(line.text) {
			// Trailing spaces are not painted, but they belong to the source row
			// and a drag to its end must include them.
			return line.end, true
		}
		return line.start + offset, true
	}
	if x <= 0 || v.cell <= 0 {
		return line.start, true
	}
	at, display := 0, float32(0)
	for at < len(line.text) {
		r, size := utf8.DecodeRuneInString(line.text[at:])
		end := display + float32(runeCells(r))*v.cell
		if x < end {
			if x-display < end-x {
				return line.start + at, true
			}
			return line.start + at + size, true
		}
		display, at = end, at+size
	}
	return line.start + len(line.text), true
}

// paintSelection paints the translucent selection over flex's labels. The alpha
// matches assistant selections and leaves the already-painted text readable.
func (v *aiPlainTextView) paintSelection(canvas *gfx.Canvas, selection components.MarkdownSelection, color gfx.Color) {
	start, end := aiPlainTextRange(v.source, selection)
	if start >= end || color.Alpha == 0 {
		return
	}
	for index, line := range v.lines {
		lo, hi := max(line.start, start), min(line.end, end)
		from, to := lo-line.start, hi-line.start
		from, to = max(from, 0), min(to, len(line.text))
		if from >= to {
			continue
		}
		y := v.rect.Y + float32(index)*v.lineHeight
		if v.mono {
			x := v.rect.X + aiMonoTextWidth(line.text[:from], v.cell)
			width := aiMonoTextWidth(line.text[from:to], v.cell)
			canvas.Fill(gfx.Rect{X: x, Y: y, Width: width, Height: v.lineHeight}, color)
			continue
		}
		v.selectionScratch = canvas.TextSelection(v.selectionScratch[:0], line.text, v.size, gfx.WeightRegular, from, to)
		for _, span := range v.selectionScratch {
			canvas.Fill(gfx.Rect{X: v.rect.X + span.X, Y: y, Width: span.Width, Height: v.lineHeight}, color)
		}
	}
}

func aiMonoTextWidth(text string, cell float32) float32 {
	cells := 0
	for _, r := range text {
		cells += runeCells(r)
	}
	return float32(cells) * cell
}

func aiPlainTextRange(text string, selection components.MarkdownSelection) (start, end int) {
	start, end = selection.Anchor, selection.Focus
	if start > end {
		start, end = end, start
	}
	start, end = min(max(start, 0), len(text)), min(max(end, 0), len(text))
	for start > 0 && start < len(text) && text[start]&0xc0 == 0x80 {
		start--
	}
	for end > 0 && end < len(text) && text[end]&0xc0 == 0x80 {
		end--
	}
	return start, end
}

// textBounds is where the last paint that reached the row put a block's text.
func (v *aiRowView) textBounds(part aiTextPart) (gfx.Rect, bool) {
	if v.kind == aiAssistant {
		return v.textRect, part == aiTextPrimary
	}
	if part < aiTextPartCount && v.textSlotSet[part] {
		return v.plain[part].rect, true
	}
	return gfx.Rect{}, false
}

// textLayout returns the selectable block identified by part, or nil when the
// row has no such block. Empty tool blocks remain available so rebuilds clamp
// and clear selections whose text was collapsed or removed.
func (v *aiRowView) textLayout(part aiTextPart) aiTextLayout {
	switch v.kind {
	case aiAssistant:
		if part == aiTextPrimary {
			return &v.md
		}
	case aiUser:
		if part == aiTextPrimary {
			return &v.plain[aiTextPrimary]
		}
	case aiTool:
		if part < aiTextPartCount {
			return &v.plain[part]
		}
	}
	return nil
}
