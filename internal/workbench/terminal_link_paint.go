package workbench

import (
	"github.com/vivaui/viva/ui"
	"motty/internal/term"
)

func (state *State) paintTerminalLink(canvas *ui.Canvas, terminal *terminal, geometry terminalGeometry, row int) {
	link, ok := terminal.links.hoveredLink(terminal.screen)
	if !ok {
		return
	}
	line := terminal.screen.ViewLine(row)
	from, to, ok := terminalLinkRowSpan(link, line, geometry.cols)
	if !ok {
		return
	}
	left := snap(geometry.left + float32(from)*state.cellWidth)
	right := snap(geometry.left + float32(to)*state.cellWidth)
	y := geometry.top + float32(row+1)*state.cellHeight - 1
	canvas.Fill(ui.Rect{X: left, Y: y, Width: right - left, Height: 1}, state.pal.text)
}

func terminalLinkRowSpan(link term.Link, line, cols int) (int, int, bool) {
	if line < link.Start.Line || line > link.End.Line {
		return 0, 0, false
	}
	from, to := 0, cols
	if line == link.Start.Line {
		from = link.Start.Column
	}
	if line == link.End.Line {
		to = link.End.Column
	}
	from, to = max(from, 0), min(to, cols)
	return from, to, from < to
}
